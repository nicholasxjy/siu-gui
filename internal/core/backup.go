package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	backupFormat       = "siu-gui-backup" // Shared with older releases and upstream siu.
	MaxBackupBytes     = 128 * 1024 * 1024
	maxBackupContent   = 64 * 1024 * 1024
	maxBackupFiles     = 20_000
	maxBackupPathDepth = 64
	maxAppliedBytes    = 4096
)

type portableBackup struct {
	Format     string        `json:"format"`
	Version    uint32        `json:"version"`
	ExportedAt uint64        `json:"exportedAt"`
	SourceHome string        `json:"sourceHome"`
	SourceRoot string        `json:"sourceRoot"`
	Platform   string        `json:"platform"`
	MCP        []McpEntry    `json:"mcp"`
	Skills     []savedSkill  `json:"skills"`
	Proxy      portableProxy `json:"proxy"`
	Theme      *string       `json:"theme,omitempty"`
}

type savedSkill struct {
	Entry SkillEntry  `json:"entry"`
	Files []savedFile `json:"files"`
}

type savedFile struct {
	Path       string `json:"path"`
	Data       string `json:"data"`
	Executable bool   `json:"executable"`
}

type portableProxy struct {
	Mode    ProxyMode `json:"mode"`
	URL     string    `json:"url"`
	NoProxy string    `json:"noProxy"`
}

func loadPortableProxy(root string) (portableProxy, error) {
	path := filepath.Join(root, "proxy.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return portableProxy{Mode: ProxyModeSystem}, nil
		}
		return portableProxy{}, fmt.Errorf("%s: %w", path, err)
	}
	var p portableProxy
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return portableProxy{}, fmt.Errorf("%s isn't valid", path)
	}
	v, err := p.validated()
	if err != nil {
		return portableProxy{}, fmt.Errorf("%s isn't valid", path)
	}
	return v, nil
}

func (p portableProxy) validated() (portableProxy, error) {
	if p.Mode == "" {
		p.Mode = ProxyModeSystem
	}
	switch p.Mode {
	case ProxyModeSystem, ProxyModeCustom, ProxyModeDirect:
	default:
		return portableProxy{}, errors.New("invalid proxy mode")
	}
	p.URL = strings.TrimSpace(p.URL)
	p.NoProxy = strings.TrimSpace(p.NoProxy)
	if len(p.URL) > 4096 || len(p.NoProxy) > 8192 {
		return portableProxy{}, errors.New("the proxy settings are too long")
	}
	if p.Mode == ProxyModeCustom {
		scheme, rest, ok := strings.Cut(p.URL, "://")
		host := strings.TrimSuffix(rest, "/")
		validScheme := scheme == "http" || scheme == "https" || scheme == "socks5" || scheme == "socks5h"
		if !ok || !validScheme || host == "" || strings.ContainsAny(host, "/?#") {
			return portableProxy{}, errors.New("the proxy address isn't an http, https or socks5 one")
		}
	}
	return p, nil
}

// AppliedConfig records the last applied root/config.json in config.applied.json.
type AppliedConfig struct {
	Digest string  `json:"digest"`
	Theme  *string `json:"theme,omitempty"`
}

// ExportedBackup summarizes a completed backup export.
type ExportedBackup struct {
	MCP    int
	Skills int
}

// BackupPreview summarizes a validated backup before restoration.
type BackupPreview struct {
	Digest     string
	ExportedAt uint64
	MCP        int
	Skills     int
	Theme      *string
	Issues     []string
}

// RestoredBackup summarizes a completed restoration.
type RestoredBackup struct {
	Digest string
	Report Report
	Saved  string
	Theme  *string
}

func validBackupTheme(theme *string) error {
	if theme == nil {
		return nil
	}
	switch *theme {
	case "system", "light", "dark":
		return nil
	default:
		return errors.New("the backup's theme isn't system, light or dark")
	}
}

func validateRelativeBackupPath(p string) (string, error) {
	if p == "" || len(p) > 4096 {
		return "", errors.New("the backup has an invalid file path")
	}
	parts := strings.Split(p, "/")
	if len(parts) > maxBackupPathDepth {
		return "", errors.New("the backup's folders nest too deep")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." ||
			strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return "", fmt.Errorf("the backup has a file path siu can't restore: %s", p)
		}
		for _, r := range part {
			if r < 0x20 || r == 0x7f || strings.ContainsRune(`\:<>\"|?*`, r) || r == '"' {
				return "", fmt.Errorf("the backup has a file path siu can't restore: %s", p)
			}
		}
		stem, _, _ := strings.Cut(part, ".")
		upper := strings.ToUpper(stem)
		switch upper {
		case "CON", "PRN", "AUX", "NUL":
			return "", fmt.Errorf("the backup has a file path siu can't restore: %s", p)
		}
		if len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) &&
			upper[3] >= '1' && upper[3] <= '9' {
			return "", fmt.Errorf("the backup has a file path siu can't restore: %s", p)
		}
	}
	return filepath.Join(parts...), nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is too big", path)
	}
	return data, nil
}

func backupDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func collectBackupFiles(root, dir string, files *[]savedFile, count *int, size *int, depth int) error {
	if depth > maxBackupPathDepth {
		return fmt.Errorf("%s nests too deep", root)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		p := filepath.Join(dir, entry.Name())
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, err := validateRelativeBackupPath(rel); err != nil {
			return err
		}
		mode := entry.Type()
		if mode.IsDir() {
			if err := collectBackupFiles(root, p, files, count, size, depth+1); err != nil {
				return err
			}
			continue
		}
		if mode&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(p)
			if err != nil {
				return err
			}
			info, err := os.Stat(target)
			if err != nil || !info.Mode().IsRegular() || !pathHasPrefix(target, root) {
				return fmt.Errorf("%s links outside its skill or to a folder", p)
			}
		} else if !mode.IsRegular() {
			return fmt.Errorf("%s isn't a file or folder", p)
		}

		*count++
		if *count > maxBackupFiles {
			return fmt.Errorf("the skills have over %d files", maxBackupFiles)
		}
		remaining := int64(maxBackupContent - *size)
		data, err := readBounded(p, remaining)
		if err != nil {
			return err
		}
		*size += len(data)
		executable := false
		if runtime.GOOS != "windows" {
			if info, err := os.Stat(p); err == nil {
				executable = info.Mode().Perm()&0o111 != 0
			}
		}
		*files = append(*files, savedFile{
			Path:       rel,
			Data:       base64.StdEncoding.EncodeToString(data),
			Executable: executable,
		})
	}
	return nil
}

func captureBackup(store *Store, home string) (*portableBackup, error) {
	saved := make([]savedSkill, 0, len(store.Lib.Skills))
	count := 0
	size := 0
	for _, entry := range store.Lib.Skills {
		if err := ValidName(entry.Name); err != nil {
			return nil, err
		}
		if _, err := validateRelativeBackupPath(entry.Name); err != nil {
			return nil, err
		}
		root, err := filepath.EvalSymlinks(store.SkillDir(entry.Name))
		if err != nil {
			return nil, fmt.Errorf("couldn't read skill %s: %w", entry.Name, err)
		}
		files := []savedFile{}
		if err := collectBackupFiles(root, root, &files, &count, &size, 0); err != nil {
			return nil, err
		}
		hasSkillMD := false
		for _, f := range files {
			if f.Path == "SKILL.md" {
				hasSkillMD = true
				break
			}
		}
		if !hasSkillMD {
			return nil, fmt.Errorf("skill %s has no SKILL.md", entry.Name)
		}
		saved = append(saved, savedSkill{
			Entry: entry.Clone(),
			Files: files,
		})
	}
	proxy, err := loadPortableProxy(store.Root)
	if err != nil {
		return nil, err
	}
	mcpCopy := make([]McpEntry, len(store.Lib.MCP))
	for i, e := range store.Lib.MCP {
		mcpCopy[i] = e.Clone()
	}
	return &portableBackup{
		Format:     backupFormat,
		Version:    1,
		ExportedAt: NowUnix(),
		SourceHome: home,
		SourceRoot: store.Root,
		Platform:   platformName(),
		MCP:        mcpCopy,
		Skills:     saved,
		Proxy:      proxy,
	}, nil
}

func encodeBackup(b *portableBackup) ([]byte, error) {
	raw, err := marshalPrettyJSON(b)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > MaxBackupBytes {
		return nil, errors.New("the backup would be over 128 MiB")
	}
	return raw, nil
}

func validateBackup(b *portableBackup) error {
	if b.Format != backupFormat || b.Version != 1 {
		return errors.New("not a siu backup, or one from a newer version")
	}
	if b.SourceHome == "" || b.SourceRoot == "" {
		return errors.New("the backup doesn't say which folders it came from")
	}
	if err := validBackupTheme(b.Theme); err != nil {
		return err
	}
	if _, err := b.Proxy.validated(); err != nil {
		return err
	}
	names := map[string]bool{}
	for _, entry := range b.MCP {
		if err := entry.Server.Validate(); err != nil {
			return fmt.Errorf("server %s in the backup: %w", entry.Server.Name, err)
		}
		lower := strings.ToLower(entry.Server.Name)
		if names[lower] {
			return fmt.Errorf("the backup has two servers called %s", entry.Server.Name)
		}
		names[lower] = true
	}
	names = map[string]bool{}
	count := 0
	size := 0
	for _, saved := range b.Skills {
		if err := ValidName(saved.Entry.Name); err != nil {
			return err
		}
		if _, err := validateRelativeBackupPath(saved.Entry.Name); err != nil {
			return err
		}
		lowerName := strings.ToLower(saved.Entry.Name)
		if names[lowerName] {
			return fmt.Errorf("the backup has two skills called %s", saved.Entry.Name)
		}
		names[lowerName] = true

		paths := map[string]bool{}
		hasSkillMD := false
		for _, f := range saved.Files {
			if _, err := validateRelativeBackupPath(f.Path); err != nil {
				return err
			}
			lp := strings.ToLower(f.Path)
			if paths[lp] {
				return fmt.Errorf("skill %s has %s twice", saved.Entry.Name, f.Path)
			}
			paths[lp] = true
			if f.Path == "SKILL.md" {
				hasSkillMD = true
			}
			count++
			if count > maxBackupFiles {
				return fmt.Errorf("the skills have over %d files", maxBackupFiles)
			}
			decoded, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil {
				return errors.New("a file in the backup isn't valid base64")
			}
			size += len(decoded)
			if size > maxBackupContent {
				return errors.New("the skills' files are over 64 MiB")
			}
		}
		if !hasSkillMD {
			return fmt.Errorf("skill %s has no SKILL.md", saved.Entry.Name)
		}
		for _, f := range saved.Files {
			lp := strings.ToLower(f.Path)
			for i := 0; i < len(lp); i++ {
				if lp[i] == '/' && paths[lp[:i]] {
					return fmt.Errorf("skill %s has %s as both a file and a folder", saved.Entry.Name, f.Path[:i])
				}
			}
		}
	}
	return nil
}

func loadBackupFile(path string, ignoreInstructions bool) (*portableBackup, string, error) {
	raw, err := readBounded(path, MaxBackupBytes)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	var b portableBackup
	var target any = &b
	if ignoreInstructions {
		target = &struct {
			*portableBackup
			Instructions json.RawMessage `json:"instructions"`
		}{portableBackup: &b}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return nil, "", fmt.Errorf("%s isn't a siu backup", path)
	}
	if err := validateBackup(&b); err != nil {
		return nil, "", err
	}
	return &b, backupDigest(raw), nil
}

func rebasePathValue(value string, b *portableBackup, store *Store, home string) string {
	pairs := [][2]string{
		{b.SourceRoot, store.Root},
		{b.SourceHome, home},
	}
	for _, pair := range pairs {
		oldBase := strings.TrimRight(pair[0], `/\`)
		if oldBase == "" {
			continue
		}
		if tail, ok := strings.CutPrefix(value, oldBase); ok &&
			(tail == "" || tail[0] == '/' || tail[0] == '\\') {
			parts := strings.FieldsFunc(tail, func(r rune) bool { return r == '/' || r == '\\' })
			all := make([]string, 0, 1+len(parts))
			all = append(all, pair[1])
			all = append(all, parts...)
			return filepath.Join(all...)
		}
	}
	if flag, p, ok := strings.Cut(value, "="); ok && strings.HasPrefix(flag, "-") {
		return flag + "=" + rebasePathValue(p, b, store, home)
	}
	return value
}

func restoreBackupEntries(b *portableBackup, store *Store, home string) ([]McpEntry, []SkillEntry) {
	servers := make([]McpEntry, len(b.MCP))
	for i, e := range b.MCP {
		cp := e.Clone()
		cp.Server.Command = rebasePathValue(cp.Server.Command, b, store, home)
		for j := range cp.Server.Args {
			cp.Server.Args[j] = rebasePathValue(cp.Server.Args[j], b, store, home)
		}
		for k, v := range cp.Server.Env {
			cp.Server.Env[k] = rebasePathValue(v, b, store, home)
		}
		servers[i] = cp
	}
	entries := make([]SkillEntry, len(b.Skills))
	for i, s := range b.Skills {
		cp := s.Entry.Clone()
		if cp.Source != nil && cp.Source.Kind == "local" {
			cp.Source.Dir = rebasePathValue(cp.Source.Dir, b, store, home)
		}
		entries[i] = cp
	}
	return servers, entries
}

func checkBackupMCPCollision(store *Store, name string, agents StringSet) error {
	for _, id := range agents.Sorted() {
		agent := store.Agent(id)
		if agent == nil || agent.MCP == nil {
			continue
		}
		if store.Lib.Applied[id].MCP.Has(name) {
			continue
		}
		if have, err := ReadMCP(agent.MCP); err == nil {
			if _, exists := have[name]; exists {
				return fmt.Errorf("%s already has its own server called %s; import it first", agent.Name, name)
			}
		}
	}
	return nil
}

func checkBackupTargets(store *Store, servers []McpEntry, entries []SkillEntry) ([]string, error) {
	issueSet := StringSet{}
	for _, e := range servers {
		for _, id := range e.Agents.Sorted() {
			agent := store.Agent(id)
			if agent == nil {
				return nil, fmt.Errorf("the backup names an agent siu doesn't know: %s", id)
			}
			if agent.MCP == nil || id == "agents" {
				return nil, fmt.Errorf("the backup gives server %s to %s, which has no MCP config", e.Server.Name, agent.Name)
			}
			if why := UnsupportedMCP(agent.MCP.Format, &e.Server); why != "" {
				issueSet.Add(fmt.Sprintf("server %s: %s can't use it (%s); kept picked", e.Server.Name, agent.Name, why))
			}
			if !agent.Detected() && id != "agents" {
				issueSet.Add(fmt.Sprintf("server %s: %s isn't installed here; kept picked for when it is", e.Server.Name, agent.Name))
			}
		}
		if err := checkBackupMCPCollision(store, e.Server.Name, e.Agents); err != nil {
			return nil, err
		}
		if filepath.IsAbs(e.Server.Command) {
			if _, err := os.Stat(e.Server.Command); err != nil {
				issueSet.Add(fmt.Sprintf("server %s: %s isn't on this machine; edit its command", e.Server.Name, e.Server.Command))
			}
		}
	}
	for _, e := range entries {
		for _, id := range e.Agents.Sorted() {
			agent := store.Agent(id)
			if agent == nil {
				return nil, fmt.Errorf("the backup names an agent siu doesn't know: %s", id)
			}
			if agent.Skills == "" {
				return nil, fmt.Errorf("the backup gives skill %s to %s, which has no skills folder", e.Name, agent.Name)
			}
			at := filepath.Join(agent.Skills, e.Name)
			ownedLink := store.Lib.Applied[id].Skills.Has(e.Name) && isLink(at)
			if _, err := os.Lstat(at); err == nil && !LinksTo(at, store.SkillDir(e.Name)) && !ownedLink {
				issueSet.Add(fmt.Sprintf("skill %s: %s already has its own; it stays until you import or move it", e.Name, agent.Name))
			}
			if !agent.Detected() && id != "agents" {
				issueSet.Add(fmt.Sprintf("skill %s: %s isn't installed here; kept picked for when it is", e.Name, agent.Name))
			}
		}
	}
	return issueSet.Sorted(), nil
}

func writePrivateAtomic(path string, data []byte) error {
	if err := WriteAtomic(path, data); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// resolveRealPath resolves symlinks as far up the path as exists.
func resolveRealPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir := filepath.Dir(p)
	base := filepath.Base(p)
	if dir == p || base == "" {
		return p
	}
	return filepath.Join(resolveRealPath(dir), base)
}

// ExportBackup exports the managed library, skill files, proxy, and optional theme to path.
func ExportBackup(store *Store, home, path string, theme *string) (ExportedBackup, error) {
	if !filepath.IsAbs(path) {
		return ExportedBackup{}, errors.New("give the backup an absolute path")
	}
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		return ExportedBackup{}, errors.New("the backup's name has to end in .json")
	}
	parent := filepath.Dir(path)
	if !isDir(parent) {
		return ExportedBackup{}, fmt.Errorf("there's no folder %s", parent)
	}
	target := resolveRealPath(path)
	root := resolveRealPath(store.Root)
	if pathHasPrefix(target, root) && target != filepath.Join(root, "config.json") {
		return ExportedBackup{}, errors.New("inside the library only config.json can be written; pick another place")
	}
	for _, agent := range store.Agents {
		if agent.MCP != nil && target == resolveRealPath(agent.MCP.Path) {
			return ExportedBackup{}, fmt.Errorf("that's %s's MCP config", agent.Name)
		}
		if agent.Skills != "" && pathHasPrefix(target, resolveRealPath(agent.Skills)) {
			return ExportedBackup{}, fmt.Errorf("that's inside %s's skills folder", agent.Name)
		}
	}
	b, err := captureBackup(store, home)
	if err != nil {
		return ExportedBackup{}, err
	}
	b.Theme = theme
	if err := validateBackup(b); err != nil {
		return ExportedBackup{}, err
	}
	data, err := encodeBackup(b)
	if err != nil {
		return ExportedBackup{}, err
	}
	if err := writeSyncedFile(target, data); err != nil {
		return ExportedBackup{}, err
	}
	return ExportedBackup{MCP: len(b.MCP), Skills: len(b.Skills)}, nil
}

func writeSyncedFile(target string, data []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".siu-export-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// PreviewBackup validates a backup file and checks destination collisions without mutating disk.
func PreviewBackup(store *Store, home, path string) (BackupPreview, error) {
	return previewBackup(store, home, path, false)
}

func previewBackup(store *Store, home, path string, ignoreInstructions bool) (BackupPreview, error) {
	b, digest, err := loadBackupFile(path, ignoreInstructions)
	if err != nil {
		return BackupPreview{}, err
	}
	servers, entries := restoreBackupEntries(b, store, home)
	issues, err := checkBackupTargets(store, servers, entries)
	if err != nil {
		return BackupPreview{}, err
	}
	return BackupPreview{
		Digest:     digest,
		ExportedAt: b.ExportedAt,
		MCP:        len(servers),
		Skills:     len(entries),
		Theme:      b.Theme,
		Issues:     issues,
	}, nil
}

func existingFileBytes(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func restoreOptionalFile(path string, data []byte, existed bool) error {
	if existed {
		return WriteAtomic(path, data)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ImportBackup replaces all managed resources with the backup's desired state,
// verifying expectedDigest, taking a safety backup first, and rolling back on local failure.
func ImportBackup(store *Store, home, path, expectedDigest string) (RestoredBackup, error) {
	return importBackup(store, home, path, expectedDigest, false)
}

func importBackup(store *Store, home, path, expectedDigest string, ignoreInstructions bool) (RestoredBackup, error) {
	b, actual, err := loadBackupFile(path, ignoreInstructions)
	if err != nil {
		return RestoredBackup{}, err
	}
	if actual != expectedDigest {
		return RestoredBackup{}, fmt.Errorf("%s changed since it was opened; open it again", path)
	}
	servers, entries := restoreBackupEntries(b, store, home)
	warnings, err := checkBackupTargets(store, servers, entries)
	if err != nil {
		return RestoredBackup{}, err
	}
	if err := os.MkdirAll(store.Root, 0o700); err != nil {
		return RestoredBackup{}, err
	}
	workDir, err := os.MkdirTemp(store.Root, ".restore-*")
	if err != nil {
		return RestoredBackup{}, err
	}
	keepWork := false
	defer func() {
		if !keepWork {
			_ = os.RemoveAll(workDir)
		}
	}()

	staged := filepath.Join(workDir, "new")
	for i, saved := range b.Skills {
		dir := filepath.Join(staged, entries[i].Name)
		for _, f := range saved.Files {
			relPath, err := validateRelativeBackupPath(f.Path)
			if err != nil {
				return RestoredBackup{}, err
			}
			to := filepath.Join(dir, relPath)
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return RestoredBackup{}, err
			}
			raw, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil {
				return RestoredBackup{}, err
			}
			perm := os.FileMode(0o600)
			if f.Executable {
				perm = 0o700
			}
			if err := os.WriteFile(to, raw, perm); err != nil {
				return RestoredBackup{}, err
			}
			if runtime.GOOS != "windows" {
				if err := os.Chmod(to, perm); err != nil {
					return RestoredBackup{}, err
				}
			}
		}
		entries[i].Hash = HashDir(dir)
	}

	prevLibBytes, hadPrevLib, err := existingFileBytes(filepath.Join(store.Root, "library.json"))
	if err != nil {
		return RestoredBackup{}, err
	}
	prevProxyBytes, hadPrevProxy, err := existingFileBytes(filepath.Join(store.Root, "proxy.json"))
	if err != nil {
		return RestoredBackup{}, err
	}
	prevBackup, err := captureBackup(store, home)
	if err != nil {
		return RestoredBackup{}, err
	}
	if err := validateBackup(prevBackup); err != nil {
		return RestoredBackup{}, err
	}
	safetyBytes, err := encodeBackup(prevBackup)
	if err != nil {
		return RestoredBackup{}, err
	}
	backupsDir := filepath.Join(store.Root, "backups")
	if err := os.MkdirAll(backupsDir, 0o755); err != nil {
		return RestoredBackup{}, err
	}
	safetyFile, err := os.CreateTemp(backupsDir, fmt.Sprintf("config-%d-*.json", NowUnix()))
	if err != nil {
		return RestoredBackup{}, err
	}
	savedPath := safetyFile.Name()
	if _, err := safetyFile.Write(safetyBytes); err != nil {
		_ = safetyFile.Close()
		_ = os.Remove(savedPath)
		return RestoredBackup{}, err
	}
	if err := safetyFile.Sync(); err != nil {
		_ = safetyFile.Close()
		_ = os.Remove(savedPath)
		return RestoredBackup{}, err
	}
	if err := safetyFile.Close(); err != nil {
		_ = os.Remove(savedPath)
		return RestoredBackup{}, err
	}

	skillsDir := store.SkillsDir()
	if info, err := os.Lstat(skillsDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return RestoredBackup{}, fmt.Errorf("%s has to be a real folder", skillsDir)
		}
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return RestoredBackup{}, err
	}
	oldDir := filepath.Join(workDir, "old")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		return RestoredBackup{}, err
	}

	var swapped []string
	previousLib := store.Lib.Clone()
	persistErr := func() error {
		for _, entry := range entries {
			target := store.SkillDir(entry.Name)
			if info, err := os.Lstat(target); err == nil {
				if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
					return fmt.Errorf("%s isn't a skill folder", target)
				}
				if err := os.Rename(target, filepath.Join(oldDir, entry.Name)); err != nil {
					return err
				}
			}
			swapped = append(swapped, entry.Name)
			if err := os.Rename(filepath.Join(staged, entry.Name), target); err != nil {
				return err
			}
		}
		store.Lib.MCP = servers
		store.Lib.Skills = entries
		validatedProxy, err := b.Proxy.validated()
		if err != nil {
			return err
		}
		proxyJSON, err := marshalPrettyJSON(validatedProxy)
		if err != nil {
			return err
		}
		if err := writePrivateAtomic(filepath.Join(store.Root, "proxy.json"), proxyJSON); err != nil {
			return err
		}
		return store.Save()
	}()

	if persistErr != nil {
		store.Lib = previousLib
		var rollbackErrs []string
		for i := len(swapped) - 1; i >= 0; i-- {
			name := swapped[i]
			target := store.SkillDir(name)
			if _, err := os.Lstat(target); err == nil {
				if err := os.RemoveAll(target); err != nil {
					rollbackErrs = append(rollbackErrs, err.Error())
				}
			}
			oldCopy := filepath.Join(oldDir, name)
			if _, err := os.Lstat(oldCopy); err == nil {
				if err := os.Rename(oldCopy, target); err != nil {
					rollbackErrs = append(rollbackErrs, err.Error())
				}
			}
		}
		if err := restoreOptionalFile(filepath.Join(store.Root, "library.json"), prevLibBytes, hadPrevLib); err != nil {
			rollbackErrs = append(rollbackErrs, err.Error())
		}
		if err := restoreOptionalFile(filepath.Join(store.Root, "proxy.json"), prevProxyBytes, hadPrevProxy); err != nil {
			rollbackErrs = append(rollbackErrs, err.Error())
		}
		if len(rollbackErrs) > 0 {
			keepWork = true
			return RestoredBackup{}, fmt.Errorf(
				"restore failed: %v; undoing it failed too: %s; the previous setup is in %s and the half-restored files in %s",
				persistErr, strings.Join(rollbackErrs, "; "), savedPath, workDir,
			)
		}
		return RestoredBackup{}, fmt.Errorf(
			"restore failed and was undone: %v; the previous setup is also in %s",
			persistErr, savedPath,
		)
	}

	var setupIssues []string
	needSharedAgents := false
	for _, e := range store.Lib.Skills {
		if e.Enabled && e.Agents.Has("agents") {
			needSharedAgents = true
			break
		}
	}
	if needSharedAgents {
		if ag := store.Agent("agents"); ag != nil && ag.Skills != "" {
			if err := os.MkdirAll(ag.Skills, 0o755); err != nil {
				setupIssues = append(setupIssues, fmt.Sprintf("couldn't make %s: %v", ag.Skills, err))
			}
		}
	}

	report := store.Sync()
	report.Issues = append(report.Issues, setupIssues...)
	if err := store.Save(); err != nil {
		report.Issues = append(report.Issues, fmt.Sprintf("couldn't save what was written: %v; reload to retry", err))
	}
	pruneIfSafe(store, report)

	// Mark default config.json as applied when restoring from it.
	if selReal, err1 := filepath.EvalSymlinks(path); err1 == nil {
		if defReal, err2 := filepath.EvalSymlinks(filepath.Join(store.Root, "config.json")); err2 == nil && selReal == defReal {
			applied := AppliedConfig{Digest: actual, Theme: b.Theme}
			if rawApplied, err := json.Marshal(applied); err == nil {
				if err := WriteAtomic(filepath.Join(store.Root, "config.applied.json"), rawApplied); err != nil {
					report.Issues = append(report.Issues, fmt.Sprintf("couldn't note config.json as applied: %v", err))
				}
			}
		}
	}

	report.Issues = append(report.Issues, warnings...)
	report.Changed += len(b.MCP) + len(b.Skills)
	return RestoredBackup{
		Digest: actual,
		Report: report,
		Saved:  savedPath,
		Theme:  b.Theme,
	}, nil
}

// ReadAppliedConfig reads root/config.applied.json, returning nil if absent.
func ReadAppliedConfig(root string) (*AppliedConfig, error) {
	path := filepath.Join(root, "config.applied.json")
	raw, err := readBounded(path, maxAppliedBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var applied AppliedConfig
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&applied); err != nil {
		return nil, fmt.Errorf("%s isn't valid; move it away to apply config.json again", path)
	}
	if err := validBackupTheme(applied.Theme); err != nil {
		return nil, err
	}
	return &applied, nil
}

// ApplyDefaultBackup restores root/config.json if present and different from config.applied.json.
func ApplyDefaultBackup(store *Store, home string) (*RestoredBackup, error) {
	return applyDefaultBackup(store, home, false)
}

func applyDefaultBackup(store *Store, home string, ignoreInstructions bool) (*RestoredBackup, error) {
	path := filepath.Join(store.Root, "config.json")
	raw, err := readBounded(path, MaxBackupBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	d := backupDigest(raw)
	applied, err := ReadAppliedConfig(store.Root)
	if err != nil {
		return nil, err
	}
	if applied != nil && applied.Digest == d {
		return nil, nil
	}
	restored, err := importBackup(store, home, path, d, ignoreInstructions)
	if err != nil {
		return nil, err
	}
	return &restored, nil
}

func pruneIfSafe(store *Store, report Report) {
	if len(report.Issues) > 0 {
		return
	}
	for _, ap := range store.Lib.Applied {
		for name := range ap.Skills {
			if store.Skill(name) == nil {
				return
			}
		}
	}
	store.Prune()
}
