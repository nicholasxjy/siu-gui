package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Eco identifies the package ecosystem for MCP server updates.
type Eco int

const (
	EcoNpm Eco = iota
	EcoPypi
)

// UpdateRemote abstracts network lookups and downloads for updates.
type UpdateRemote interface {
	Latest(ctx context.Context, eco Eco, name string) (string, error)
	Pull(ctx context.Context, image string) error
	Download(ctx context.Context, repo, ref string) (tempDir string, root string, err error)
}

// NetRemote implements UpdateRemote against npm, PyPI, Docker, and GitHub.
type NetRemote struct{}

func (NetRemote) Latest(ctx context.Context, eco Eco, name string) (string, error) {
	switch eco {
	case EcoNpm:
		type release struct {
			Version string `json:"version"`
		}
		rawURL := "https://registry.npmjs.org/" + strings.ReplaceAll(name, "/", "%2f") + "/latest"
		r, err := HTTPGetJSON[release](ctx, rawURL)
		if err != nil {
			return "", err
		}
		return r.Version, nil
	case EcoPypi:
		type pypi struct {
			Info struct {
				Version string `json:"version"`
			} `json:"info"`
		}
		r, err := HTTPGetJSON[pypi](ctx, fmt.Sprintf("https://pypi.org/pypi/%s/json", name))
		if err != nil {
			return "", err
		}
		return r.Info.Version, nil
	default:
		return "", fmt.Errorf("unknown ecosystem %d", eco)
	}
}

func (NetRemote) Pull(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", "-q", image)
	if err := ConfigureCommandProxy(cmd); err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return fmt.Errorf("couldn't run docker: %w", err)
	}
	return nil
}

func (NetRemote) Download(ctx context.Context, repo, ref string) (string, string, error) {
	return DownloadGitHub(ctx, repo, ref)
}

// UpdatePlanKind describes how a server can be updated.
type UpdatePlanKind int

const (
	PlanRemote UpdatePlanKind = iota
	PlanUnknown
	PlanFloating
	PlanPackage
	PlanImage
)

// UpdatePlan is the parsed update plan for an MCP server.
type UpdatePlan struct {
	Kind     UpdatePlanKind
	Spec     string // Floating spec or Docker image
	Eco      Eco
	PkgName  string
	Version  string
	HasVer   bool
	ArgIndex int
	Prefix   string
	Base     string
	Sep      string
}

// PlanServerUpdate inspects a server's command and args to decide how to update it.
func PlanServerUpdate(s *Server) UpdatePlan {
	if s.Remote() {
		return UpdatePlan{Kind: PlanRemote}
	}
	base := filepath.Base(s.Command)
	ext := filepath.Ext(base)
	program := strings.TrimSuffix(base, ext)
	switch program {
	case "npx":
		return npxPlan(s.Args)
	case "uvx":
		return uvxPlan(s.Args)
	case "docker":
		return dockerPlan(s.Args)
	default:
		return UpdatePlan{Kind: PlanUnknown}
	}
}

func npxPlan(args []string) UpdatePlan {
	for i, a := range args {
		switch a {
		case "-p", "--package":
			if i+1 < len(args) {
				return npmPackagePlan(args[i+1], i+1, "")
			}
			return UpdatePlan{Kind: PlanUnknown}
		case "-c", "--call":
			return UpdatePlan{Kind: PlanUnknown}
		}
		if v, ok := strings.CutPrefix(a, "--package="); ok {
			return npmPackagePlan(v, i, "--package=")
		}
		if strings.HasPrefix(a, "--call=") {
			return UpdatePlan{Kind: PlanUnknown}
		}
		if !strings.HasPrefix(a, "-") {
			return npmPackagePlan(a, i, "")
		}
	}
	return UpdatePlan{Kind: PlanUnknown}
}

func npmPackagePlan(spec string, at int, prefix string) UpdatePlan {
	split := -1
	if rest, ok := strings.CutPrefix(spec, "@"); ok {
		if i := strings.IndexByte(rest, '@'); i >= 0 {
			split = i + 1
		}
	} else {
		split = strings.IndexByte(spec, '@')
	}
	name := spec
	version := ""
	hasVer := false
	if split >= 0 {
		name = spec[:split]
		version = spec[split+1:]
		hasVer = true
	}
	partValid := func(p string) bool {
		if p == "" || p[0] == '.' || p[0] == '_' {
			return false
		}
		for i := 0; i < len(p); i++ {
			c := p[i]
			if !isASCIIAlnum(c) && c != '-' && c != '.' && c != '_' && c != '~' {
				return false
			}
		}
		return true
	}
	valid := false
	if scoped, ok := strings.CutPrefix(name, "@"); ok {
		scope, pkg, hasSlash := strings.Cut(scoped, "/")
		valid = hasSlash && partValid(scope) && partValid(pkg)
	} else {
		valid = partValid(name)
	}
	if !valid {
		return UpdatePlan{Kind: PlanUnknown}
	}
	if hasVer && !isExactNpmVersion(version) {
		return UpdatePlan{Kind: PlanFloating, Spec: spec}
	}
	return UpdatePlan{
		Kind:     PlanPackage,
		Eco:      EcoNpm,
		PkgName:  name,
		Version:  version,
		HasVer:   hasVer,
		ArgIndex: at,
		Prefix:   prefix,
		Base:     name,
		Sep:      "@",
	}
}

func isExactNpmVersion(v string) bool {
	core := v
	if idx := strings.IndexAny(core, "-+"); idx >= 0 {
		core = core[:idx]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				return false
			}
		}
	}
	return true
}

func uvxPlan(args []string) UpdatePlan {
	valued := map[string]bool{
		"-w": true, "--with": true, "--with-editable": true, "--with-requirements": true,
		"-p": true, "--python": true, "--index": true, "--index-url": true,
		"--extra-index-url": true, "--default-index": true, "-f": true, "--find-links": true,
		"-c": true, "--constraints": true, "--overrides": true, "--directory": true,
		"--project": true, "--config-file": true, "--cache-dir": true,
	}
	for i := 0; i < len(args); {
		a := args[i]
		if a == "--from" {
			if i+1 < len(args) {
				return pypiPackagePlan(args[i+1], i+1, "", "==")
			}
			return UpdatePlan{Kind: PlanUnknown}
		}
		if v, ok := strings.CutPrefix(a, "--from="); ok {
			return pypiPackagePlan(v, i, "--from=", "==")
		}
		if valued[a] {
			i += 2
			continue
		}
		if !strings.HasPrefix(a, "-") {
			return pypiPackagePlan(a, i, "", "@")
		}
		i++
	}
	return UpdatePlan{Kind: PlanUnknown}
}

func pypiPackagePlan(spec string, at int, prefix, defaultSep string) UpdatePlan {
	end := len(spec)
	for i := 0; i < len(spec); i++ {
		c := spec[i]
		if !isASCIIAlnum(c) && c != '-' && c != '_' && c != '.' {
			end = i
			break
		}
	}
	name := spec[:end]
	rest := spec[end:]
	if name == "" {
		return UpdatePlan{Kind: PlanUnknown}
	}
	if strings.HasPrefix(rest, "[") {
		closeIdx := strings.IndexByte(rest, ']')
		if closeIdx < 0 {
			return UpdatePlan{Kind: PlanUnknown}
		}
		rest = rest[closeIdx+1:]
	}
	base := spec[:len(spec)-len(rest)]
	var version string
	hasVer := false
	sep := defaultSep
	switch {
	case rest == "":
	case strings.HasPrefix(rest, "=="):
		version = rest[2:]
		hasVer = true
		sep = "=="
	case strings.HasPrefix(rest, "@"):
		version = rest[1:]
		hasVer = true
		sep = "@"
	case strings.ContainsRune("<>~!=", rune(rest[0])):
		return UpdatePlan{Kind: PlanFloating, Spec: spec}
	default:
		return UpdatePlan{Kind: PlanUnknown}
	}
	if hasVer && !isExactPypiVersion(version) {
		return UpdatePlan{Kind: PlanFloating, Spec: spec}
	}
	return UpdatePlan{
		Kind:     PlanPackage,
		Eco:      EcoPypi,
		PkgName:  name,
		Version:  version,
		HasVer:   hasVer,
		ArgIndex: at,
		Prefix:   prefix,
		Base:     base,
		Sep:      sep,
	}
}

func isExactPypiVersion(v string) bool {
	if len(v) == 0 || v[0] < '0' || v[0] > '9' {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !isASCIIAlnum(c) && c != '.' && c != '-' && c != '+' && c != '!' {
			return false
		}
	}
	return true
}

func dockerPlan(args []string) UpdatePlan {
	bare := map[string]bool{
		"-i": true, "-t": true, "-it": true, "-ti": true, "-d": true,
		"-P": true, "-q": true, "--rm": true, "--init": true,
		"--interactive": true, "--tty": true, "--detach": true,
		"--privileged": true, "--publish-all": true, "--read-only": true, "--quiet": true,
	}
	if len(args) == 0 || args[0] != "run" {
		return UpdatePlan{Kind: PlanUnknown}
	}
	for i := 1; i < len(args); {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return UpdatePlan{Kind: PlanImage, Spec: a}
		}
		if strings.Contains(a, "=") || bare[a] {
			i++
		} else {
			i += 2
		}
	}
	return UpdatePlan{Kind: PlanUnknown}
}

// UpdateLine is the result of updating a single named resource.
type UpdateLine struct {
	Line   string
	Failed bool
}

// UpdateOneMCP updates a single named MCP server without touching unrelated entries.
func UpdateOneMCP(ctx context.Context, store *Store, remote UpdateRemote, name string) (UpdateLine, error) {
	entry := store.Server(name)
	if entry == nil {
		return UpdateLine{}, fmt.Errorf("no server called %s", name)
	}
	s := entry.Server.Clone()
	p := PlanServerUpdate(&s)
	var line string
	var failed, changed bool
	switch p.Kind {
	case PlanRemote:
		line = "remote, nothing to update"
	case PlanUnknown:
		line = fmt.Sprintf("runs %s, not a package siu can update", s.Command)
	case PlanFloating:
		line = fmt.Sprintf("%s follows its releases already", p.Spec)
	case PlanImage:
		if err := remote.Pull(ctx, p.Spec); err != nil {
			line = fmt.Sprintf("couldn't pull %s: %v", p.Spec, err)
			failed = true
		} else {
			line = fmt.Sprintf("pulled %s", p.Spec)
		}
	case PlanPackage:
		latest, err := remote.Latest(ctx, p.Eco, p.PkgName)
		if err != nil {
			line = fmt.Sprintf("couldn't look up %s: %v", p.PkgName, err)
			failed = true
		} else if p.HasVer && p.Version == latest {
			line = fmt.Sprintf("%s %s is the latest", p.PkgName, latest)
		} else {
			next := s.Clone()
			next.Args[p.ArgIndex] = fmt.Sprintf("%s%s%s%s", p.Prefix, p.Base, p.Sep, latest)
			if err := store.EditServer(name, next); err != nil {
				return UpdateLine{}, err
			}
			from := "unpinned"
			if p.HasVer {
				from = p.Version
			}
			line = fmt.Sprintf("%s %s → %s", p.PkgName, from, latest)
			changed = true
		}
	}
	if !changed {
		return UpdateLine{Line: line, Failed: failed}, nil
	}
	report, err := store.Commit()
	if err != nil {
		return UpdateLine{}, err
	}
	if len(report.Issues) > 0 {
		failed = true
		line = fmt.Sprintf("%s (%d issue(s) writing it in)", line, len(report.Issues))
	}
	return UpdateLine{Line: line, Failed: failed}, nil
}

// UpdateOneSkill updates a single named skill from its source.
func UpdateOneSkill(ctx context.Context, store *Store, remote UpdateRemote, name string) (UpdateLine, error) {
	entry := store.Skill(name)
	if entry == nil {
		return UpdateLine{}, fmt.Errorf("no skill called %s", name)
	}
	src := entry.Source
	resolveTree := func(tree string) (string, bool) {
		outcome, err := store.UpdateSkillFrom(name, tree)
		if err != nil {
			return err.Error(), true
		}
		if outcome == UpdateOutcomeUpdated {
			return "updated", false
		}
		return "up to date", false
	}
	var line string
	var failed bool
	switch {
	case src == nil:
		line = "imported, no source to update from"
	case src.Kind == "local":
		line, failed = resolveTree(src.Dir)
	case src.Kind == "github":
		tmp, root, err := remote.Download(ctx, src.Repo, src.Ref)
		if err != nil {
			line = fmt.Sprintf("couldn't download %s: %v", src.Repo, err)
			failed = true
		} else {
			defer os.RemoveAll(tmp)
			line, failed = resolveTree(root)
		}
	}
	if err := store.Save(); err != nil {
		return UpdateLine{}, err
	}
	return UpdateLine{Line: line, Failed: failed}, nil
}

// UpdateAllMCP updates every managed MCP server and writes a summary log to out.
func UpdateAllMCP(ctx context.Context, store *Store, remote UpdateRemote, out io.Writer) (int, error) {
	_, _ = fmt.Fprintln(out, "MCP servers")
	servers := make([]Server, len(store.Lib.MCP))
	width := 0
	for i, e := range store.Lib.MCP {
		servers[i] = e.Server.Clone()
		if len(e.Server.Name) > width {
			width = len(e.Server.Name)
		}
	}
	if len(servers) == 0 {
		_, _ = fmt.Fprintln(out, "  none in the library")
		return 0, nil
	}
	failed := 0
	changed := false
	for _, s := range servers {
		p := PlanServerUpdate(&s)
		var line string
		switch p.Kind {
		case PlanRemote:
			line = "remote, nothing to update"
		case PlanUnknown:
			line = fmt.Sprintf("runs %s, not a package siu can update", s.Command)
		case PlanFloating:
			line = fmt.Sprintf("%s follows its releases already", p.Spec)
		case PlanImage:
			if err := remote.Pull(ctx, p.Spec); err != nil {
				failed++
				line = fmt.Sprintf("couldn't pull %s: %v", p.Spec, err)
			} else {
				line = fmt.Sprintf("pulled %s", p.Spec)
			}
		case PlanPackage:
			latest, err := remote.Latest(ctx, p.Eco, p.PkgName)
			if err != nil {
				failed++
				line = fmt.Sprintf("couldn't look up %s: %v", p.PkgName, err)
			} else if p.HasVer && p.Version == latest {
				line = fmt.Sprintf("%s %s is the latest", p.PkgName, latest)
			} else {
				next := s.Clone()
				next.Args[p.ArgIndex] = fmt.Sprintf("%s%s%s%s", p.Prefix, p.Base, p.Sep, latest)
				if err := store.EditServer(s.Name, next); err != nil {
					return 0, err
				}
				changed = true
				from := "unpinned"
				if p.HasVer {
					from = p.Version
				}
				line = fmt.Sprintf("%s %s → %s", p.PkgName, from, latest)
			}
		}
		_, _ = fmt.Fprintf(out, "  %-*s  %s\n", width, s.Name, line)
	}
	if changed {
		report, err := store.Commit()
		if err != nil {
			return 0, err
		}
		for _, issue := range report.Issues {
			_, _ = fmt.Fprintf(out, "  ! %s\n", issue)
		}
		failed += len(report.Issues)
	}
	return failed, nil
}

// UpdateAllSkills refreshes every managed skill, downloading each GitHub repo once.
func UpdateAllSkills(ctx context.Context, store *Store, remote UpdateRemote, out io.Writer) (int, error) {
	_, _ = fmt.Fprintln(out, "Skills")
	if len(store.Lib.Skills) == 0 {
		_, _ = fmt.Fprintln(out, "  none in the library")
		return 0, nil
	}
	type skillRef struct {
		name   string
		source *Source
	}
	items := make([]skillRef, len(store.Lib.Skills))
	for i, s := range store.Lib.Skills {
		var cp *Source
		if s.Source != nil {
			v := *s.Source
			cp = &v
		}
		items[i] = skillRef{name: s.Name, source: cp}
	}
	type repoKey struct {
		repo string
		ref  string
	}
	type lineRes struct {
		text string
		err  bool
	}
	lines := map[string]lineRes{}
	byRepo := map[repoKey][]string{}
	resolveTree := func(name, tree string) lineRes {
		outcome, err := store.UpdateSkillFrom(name, tree)
		if err != nil {
			return lineRes{text: err.Error(), err: true}
		}
		if outcome == UpdateOutcomeUpdated {
			return lineRes{text: "updated"}
		}
		return lineRes{text: "up to date"}
	}

	for _, it := range items {
		switch {
		case it.source == nil:
			lines[it.name] = lineRes{text: "imported, no source to update from"}
		case it.source.Kind == "local":
			lines[it.name] = resolveTree(it.name, it.source.Dir)
		case it.source.Kind == "github":
			k := repoKey{repo: it.source.Repo, ref: it.source.Ref}
			byRepo[k] = append(byRepo[k], it.name)
		}
	}

	repoKeys := make([]repoKey, 0, len(byRepo))
	for k := range byRepo {
		repoKeys = append(repoKeys, k)
	}
	sort.Slice(repoKeys, func(i, j int) bool {
		if repoKeys[i].repo != repoKeys[j].repo {
			return repoKeys[i].repo < repoKeys[j].repo
		}
		return repoKeys[i].ref < repoKeys[j].ref
	})

	for _, rk := range repoKeys {
		names := byRepo[rk]
		tmp, root, err := remote.Download(ctx, rk.repo, rk.ref)
		if err != nil {
			for _, n := range names {
				lines[n] = lineRes{text: fmt.Sprintf("couldn't download %s: %v", rk.repo, err), err: true}
			}
			continue
		}
		for _, n := range names {
			lines[n] = resolveTree(n, root)
		}
		_ = os.RemoveAll(tmp)
	}

	if err := store.Save(); err != nil {
		return 0, err
	}
	names := make([]string, 0, len(lines))
	width := 0
	for n := range lines {
		names = append(names, n)
		if len(n) > width {
			width = len(n)
		}
	}
	sort.Strings(names)
	failed := 0
	for _, n := range names {
		r := lines[n]
		if r.err {
			failed++
		}
		_, _ = fmt.Fprintf(out, "  %-*s  %s\n", width, n, r.text)
	}
	return failed, nil
}

// RequireKnown checks that name exists under kind ("mcp" or "skills").
func RequireKnown(store *Store, kind, name string) error {
	switch kind {
	case "mcp":
		if store.Server(name) != nil {
			return nil
		}
	case "skills":
		if store.Skill(name) != nil {
			return nil
		}
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}
	return fmt.Errorf("no %s entry called %s", kind, name)
}
