package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StringSet is a sorted set of strings that serializes as a sorted JSON array,
// matching Rust's BTreeSet<String>.
type StringSet map[string]struct{}

func NewStringSet(items ...string) StringSet {
	s := make(StringSet, len(items))
	for _, it := range items {
		s[it] = struct{}{}
	}
	return s
}

func (s StringSet) Has(v string) bool {
	_, ok := s[v]
	return ok
}

func (s StringSet) Add(v string) {
	s[v] = struct{}{}
}

func (s StringSet) Delete(v string) {
	delete(s, v)
}

func (s StringSet) Clone() StringSet {
	out := make(StringSet, len(s))
	for k := range s {
		out[k] = struct{}{}
	}
	return out
}

func (s StringSet) Sorted() []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s StringSet) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Sorted())
}

func (s *StringSet) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	out := make(StringSet, len(arr))
	for _, item := range arr {
		out[item] = struct{}{}
	}
	*s = out
	return nil
}

// McpEntry is a managed MCP server entry in library.json.
type McpEntry struct {
	Server  Server
	Agents  StringSet
	Enabled bool
	Origin  string
}

type mcpEntryWire struct {
	Name      string            `json:"name"`
	Transport Transport         `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Agents    StringSet         `json:"agents"`
	Enabled   *bool             `json:"enabled,omitempty"`
	Origin    string            `json:"origin,omitempty"`
}

func (e McpEntry) MarshalJSON() ([]byte, error) {
	enabled := e.Enabled
	t := e.Server.Transport
	if t == "" {
		t = TransportStdio
	}
	agents := e.Agents
	if agents == nil {
		agents = StringSet{}
	}
	return json.Marshal(mcpEntryWire{
		Name:      e.Server.Name,
		Transport: t,
		Command:   e.Server.Command,
		Args:      e.Server.Args,
		Env:       e.Server.Env,
		URL:       e.Server.URL,
		Headers:   e.Server.Headers,
		Agents:    agents,
		Enabled:   &enabled,
		Origin:    e.Origin,
	})
}

func (e *McpEntry) UnmarshalJSON(data []byte) error {
	var w mcpEntryWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	t := w.Transport
	if t == "" {
		t = TransportStdio
	}
	enabled := true
	if w.Enabled != nil {
		enabled = *w.Enabled
	}
	agents := w.Agents
	if agents == nil {
		agents = StringSet{}
	}
	e.Server = Server{
		Name:      w.Name,
		Transport: t,
		Command:   w.Command,
		Args:      w.Args,
		Env:       w.Env,
		URL:       w.URL,
		Headers:   w.Headers,
	}
	e.Agents = agents
	e.Enabled = enabled
	e.Origin = w.Origin
	return nil
}

func (e McpEntry) Clone() McpEntry {
	return McpEntry{
		Server:  e.Server.Clone(),
		Agents:  e.Agents.Clone(),
		Enabled: e.Enabled,
		Origin:  e.Origin,
	}
}

// Source describes where a managed skill originated.
type Source struct {
	Kind string `json:"kind"` // "github" or "local"
	Repo string `json:"repo,omitempty"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
	Dir  string `json:"dir,omitempty"`
}

func (s Source) Label() string {
	switch s.Kind {
	case "github":
		if s.Path == "" {
			return "github.com/" + s.Repo
		}
		return "github.com/" + s.Repo + "/" + s.Path
	case "local":
		return s.Dir
	default:
		return ""
	}
}

func (s Source) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case "github":
		return json.Marshal(struct {
			Kind string `json:"kind"`
			Repo string `json:"repo"`
			Ref  string `json:"ref"`
			Path string `json:"path"`
		}{
			Kind: "github",
			Repo: s.Repo,
			Ref:  s.Ref,
			Path: s.Path,
		})
	case "local":
		return json.Marshal(struct {
			Kind string `json:"kind"`
			Dir  string `json:"dir"`
		}{
			Kind: "local",
			Dir:  s.Dir,
		})
	default:
		return nil, fmt.Errorf("unknown skill source kind %q", s.Kind)
	}
}

func (s *Source) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind string `json:"kind"`
		Repo string `json:"repo"`
		Ref  string `json:"ref"`
		Path string `json:"path"`
		Dir  string `json:"dir"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch raw.Kind {
	case "github":
		*s = Source{Kind: "github", Repo: raw.Repo, Ref: raw.Ref, Path: raw.Path}
	case "local":
		*s = Source{Kind: "local", Dir: raw.Dir}
	default:
		return fmt.Errorf("unknown skill source kind %q", raw.Kind)
	}
	return nil
}

// SkillEntry is a managed skill entry in library.json.
type SkillEntry struct {
	Name        string
	Description string
	Source      *Source
	Agents      StringSet
	Enabled     bool
	Hash        string
	Installed   uint64
	Updated     uint64
}

type skillEntryWire struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Source      *Source   `json:"source,omitempty"`
	Agents      StringSet `json:"agents"`
	Enabled     *bool     `json:"enabled,omitempty"`
	Hash        string    `json:"hash"`
	Installed   uint64    `json:"installed"`
	Updated     uint64    `json:"updated"`
}

func (e SkillEntry) MarshalJSON() ([]byte, error) {
	enabled := e.Enabled
	agents := e.Agents
	if agents == nil {
		agents = StringSet{}
	}
	return json.Marshal(skillEntryWire{
		Name:        e.Name,
		Description: e.Description,
		Source:      e.Source,
		Agents:      agents,
		Enabled:     &enabled,
		Hash:        e.Hash,
		Installed:   e.Installed,
		Updated:     e.Updated,
	})
}

func (e *SkillEntry) UnmarshalJSON(data []byte) error {
	var w skillEntryWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	enabled := true
	if w.Enabled != nil {
		enabled = *w.Enabled
	}
	agents := w.Agents
	if agents == nil {
		agents = StringSet{}
	}
	e.Name = w.Name
	e.Description = w.Description
	e.Source = w.Source
	e.Agents = agents
	e.Enabled = enabled
	e.Hash = w.Hash
	e.Installed = w.Installed
	e.Updated = w.Updated
	return nil
}

func (e SkillEntry) Clone() SkillEntry {
	var src *Source
	if e.Source != nil {
		cp := *e.Source
		src = &cp
	}
	return SkillEntry{
		Name:        e.Name,
		Description: e.Description,
		Source:      src,
		Agents:      e.Agents.Clone(),
		Enabled:     e.Enabled,
		Hash:        e.Hash,
		Installed:   e.Installed,
		Updated:     e.Updated,
	}
}

// Applied records what siu last wrote into one agent.
type Applied struct {
	MCP    StringSet `json:"mcp,omitempty"`
	Skills StringSet `json:"skills,omitempty"`
}

func (a Applied) Clone() Applied {
	return Applied{
		MCP:    a.MCP.Clone(),
		Skills: a.Skills.Clone(),
	}
}

// Library is the persisted state of library.json.
type Library struct {
	MCP     []McpEntry         `json:"mcp"`
	Skills  []SkillEntry       `json:"skills"`
	Applied map[string]Applied `json:"applied"`
}

func (l Library) Clone() Library {
	out := Library{
		MCP:     make([]McpEntry, len(l.MCP)),
		Skills:  make([]SkillEntry, len(l.Skills)),
		Applied: make(map[string]Applied, len(l.Applied)),
	}
	for i, e := range l.MCP {
		out.MCP[i] = e.Clone()
	}
	for i, e := range l.Skills {
		out.Skills[i] = e.Clone()
	}
	for k, v := range l.Applied {
		out.Applied[k] = v.Clone()
	}
	return out
}

// FoundServer is an unmanaged server found in one or more agents' config files.
type FoundServer struct {
	Server Server
	Agents []string
}

// FoundSkillAt identifies one agent location holding an unmanaged skill.
type FoundSkillAt struct {
	Agent string
	Path  string
}

// FoundSkill is an unmanaged skill found in one or more agents' skills folders.
type FoundSkill struct {
	Name        string
	Description string
	At          []FoundSkillAt
}

// DesiredStateKind is what the library desires for an (entry, agent) pair.
type DesiredStateKind int

const (
	StateOn DesiredStateKind = iota
	StateOff
	StatePaused
	StateUnsupported
)

type DesiredState struct {
	Kind   DesiredStateKind
	Reason string
}

// Report summarizes the outcome of syncing the library into agents.
type Report struct {
	Changed int
	Issues  []string
}

// UpdateOutcome reports whether a skill changed during an update.
type UpdateOutcome int

const (
	UpdateOutcomeUpdated UpdateOutcome = iota
	UpdateOutcomeUpToDate
)

// Store manages reading, modifying, and syncing the siu library.
type Store struct {
	Root   string
	Agents []Agent
	Lib    Library
}

// NowUnix returns the current Unix timestamp in seconds.
func NowUnix() uint64 {
	n := time.Now().Unix()
	if n < 0 {
		return 0
	}
	return uint64(n)
}

// OpenStore opens library.json under root (defaulting to an empty library if absent).
func OpenStore(env Env, root string) (*Store, error) {
	path := filepath.Join(root, "library.json")
	var lib Library
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	} else {
		if err := json.Unmarshal(raw, &lib); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if lib.MCP == nil {
		lib.MCP = []McpEntry{}
	}
	if lib.Skills == nil {
		lib.Skills = []SkillEntry{}
	}
	if lib.Applied == nil {
		lib.Applied = map[string]Applied{}
	}
	return &Store{
		Root:   root,
		Agents: AllAgents(env),
		Lib:    lib,
	}, nil
}

func (s *Store) SkillsDir() string {
	return filepath.Join(s.Root, "skills")
}

func (s *Store) SkillDir(name string) string {
	return filepath.Join(s.SkillsDir(), name)
}

func (s *Store) Detected() []Agent {
	var out []Agent
	for _, a := range s.Agents {
		if a.Detected() {
			out = append(out, a)
		}
	}
	return out
}

func (s *Store) McpAgents() []Agent {
	var out []Agent
	for _, a := range s.Agents {
		if a.Detected() && a.MCP != nil {
			out = append(out, a)
		}
	}
	return out
}

func (s *Store) SkillAgents() []Agent {
	var out []Agent
	for _, a := range s.Agents {
		if a.Detected() && a.Skills != "" {
			out = append(out, a)
		}
	}
	return out
}

func (s *Store) Agent(id string) *Agent {
	for i := range s.Agents {
		if s.Agents[i].ID == id {
			return &s.Agents[i]
		}
	}
	return nil
}

func (s *Store) Save() error {
	sort.Slice(s.Lib.MCP, func(i, j int) bool {
		return s.Lib.MCP[i].Server.Name < s.Lib.MCP[j].Server.Name
	})
	sort.Slice(s.Lib.Skills, func(i, j int) bool {
		return s.Lib.Skills[i].Name < s.Lib.Skills[j].Name
	})
	if s.Lib.MCP == nil {
		s.Lib.MCP = []McpEntry{}
	}
	if s.Lib.Skills == nil {
		s.Lib.Skills = []SkillEntry{}
	}
	if s.Lib.Applied == nil {
		s.Lib.Applied = map[string]Applied{}
	}
	// Clean up empty sets inside Applied so omitempty works consistently.
	for k, a := range s.Lib.Applied {
		if len(a.MCP) == 0 {
			a.MCP = nil
		}
		if len(a.Skills) == 0 {
			a.Skills = nil
		}
		s.Lib.Applied[k] = a
	}
	raw, err := marshalPrettyJSON(s.Lib)
	if err != nil {
		return err
	}
	return WriteAtomic(filepath.Join(s.Root, "library.json"), raw)
}

// Commit saves the library, syncs into every detected agent, and saves applied state.
func (s *Store) Commit() (Report, error) {
	if err := s.Save(); err != nil {
		return Report{}, err
	}
	report := s.Sync()
	if err := s.Save(); err != nil {
		return Report{}, err
	}
	return report, nil
}

// ---- Desired states ----

func (s *Store) ServerState(e *McpEntry, agent Agent) DesiredState {
	if agent.MCP == nil {
		return DesiredState{Kind: StateUnsupported, Reason: "no MCP config"}
	}
	if why := UnsupportedMCP(agent.MCP.Format, &e.Server); why != "" {
		return DesiredState{Kind: StateUnsupported, Reason: why}
	}
	if e.Agents.Has(agent.ID) {
		if e.Enabled {
			return DesiredState{Kind: StateOn}
		}
		return DesiredState{Kind: StatePaused}
	}
	return DesiredState{Kind: StateOff}
}

func (s *Store) SkillState(e *SkillEntry, agent Agent) DesiredState {
	if agent.Skills == "" {
		return DesiredState{Kind: StateUnsupported, Reason: "no skills folder"}
	}
	if e.Agents.Has(agent.ID) {
		if e.Enabled {
			return DesiredState{Kind: StateOn}
		}
		return DesiredState{Kind: StatePaused}
	}
	return DesiredState{Kind: StateOff}
}

// ---- MCP servers ----

func (s *Store) Server(name string) *McpEntry {
	for i := range s.Lib.MCP {
		if s.Lib.MCP[i].Server.Name == name {
			return &s.Lib.MCP[i]
		}
	}
	return nil
}

func (s *Store) serverMut(name string) (*McpEntry, error) {
	e := s.Server(name)
	if e == nil {
		return nil, fmt.Errorf("no server called %s", name)
	}
	return e, nil
}

func (s *Store) DefaultServerAgents(srv *Server) StringSet {
	out := StringSet{}
	for _, a := range s.McpAgents() {
		if a.MCP != nil && UnsupportedMCP(a.MCP.Format, srv) == "" {
			out.Add(a.ID)
		}
	}
	return out
}

func (s *Store) AddServer(srv Server, agents StringSet, origin string) error {
	if err := srv.Validate(); err != nil {
		return err
	}
	if s.Server(srv.Name) != nil {
		return fmt.Errorf("the library already has a server called %s", srv.Name)
	}
	if agents == nil {
		agents = StringSet{}
	}
	s.Lib.MCP = append(s.Lib.MCP, McpEntry{
		Server:  srv,
		Agents:  agents,
		Enabled: true,
		Origin:  origin,
	})
	return nil
}

func (s *Store) EditServer(oldName string, srv Server) error {
	if err := srv.Validate(); err != nil {
		return err
	}
	if srv.Name != oldName && s.Server(srv.Name) != nil {
		return fmt.Errorf("the library already has a server called %s", srv.Name)
	}
	e, err := s.serverMut(oldName)
	if err != nil {
		return err
	}
	e.Server = srv
	return nil
}

func (s *Store) ToggleServerAgent(name, agent string) (bool, error) {
	e, err := s.serverMut(name)
	if err != nil {
		return false, err
	}
	if !e.Agents.Has(agent) {
		e.Agents.Add(agent)
		e.Enabled = true
		return true, nil
	}
	e.Agents.Delete(agent)
	return false, nil
}

func (s *Store) SetServerAgents(name string, agents StringSet) error {
	e, err := s.serverMut(name)
	if err != nil {
		return err
	}
	if agents == nil {
		agents = StringSet{}
	}
	e.Agents = agents
	return nil
}

func (s *Store) ToggleServerEnabled(name string) (bool, error) {
	e, err := s.serverMut(name)
	if err != nil {
		return false, err
	}
	e.Enabled = !e.Enabled
	return e.Enabled, nil
}

func (s *Store) RemoveServer(name string) error {
	for i := range s.Lib.MCP {
		if s.Lib.MCP[i].Server.Name == name {
			s.Lib.MCP = append(s.Lib.MCP[:i], s.Lib.MCP[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("no server called %s", name)
}

func (s *Store) FoundServers() []FoundServer {
	byName := map[string]*FoundServer{}
	for _, a := range s.McpAgents() {
		servers, err := ReadMCP(a.MCP)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(servers))
		for n := range servers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			if s.Server(name) != nil {
				continue
			}
			f, exists := byName[name]
			if !exists {
				f = &FoundServer{Server: servers[name]}
				byName[name] = f
			}
			f.Agents = append(f.Agents, a.ID)
		}
	}
	keys := make([]string, 0, len(byName))
	for k := range byName {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]FoundServer, 0, len(keys))
	for _, k := range keys {
		out = append(out, *byName[k])
	}
	return out
}

func (s *Store) ImportServer(found FoundServer) error {
	agents := NewStringSet(found.Agents...)
	if err := s.AddServer(found.Server.Clone(), agents.Clone(), "import"); err != nil {
		return err
	}
	for a := range agents {
		ap := s.Lib.Applied[a]
		if ap.MCP == nil {
			ap.MCP = StringSet{}
		}
		ap.MCP.Add(found.Server.Name)
		s.Lib.Applied[a] = ap
	}
	return nil
}

// ---- Skills ----

func (s *Store) Skill(name string) *SkillEntry {
	for i := range s.Lib.Skills {
		if s.Lib.Skills[i].Name == name {
			return &s.Lib.Skills[i]
		}
	}
	return nil
}

func (s *Store) skillMut(name string) (*SkillEntry, error) {
	e := s.Skill(name)
	if e == nil {
		return nil, fmt.Errorf("no skill called %s", name)
	}
	return e, nil
}

func (s *Store) DefaultSkillAgents() StringSet {
	all := s.SkillAgents()
	var own []Agent
	for _, a := range all {
		if a.ID != "agents" {
			own = append(own, a)
		}
	}
	pick := own
	if len(pick) == 0 {
		pick = all
	}
	out := StringSet{}
	for _, a := range pick {
		out.Add(a.ID)
	}
	return out
}

func (s *Store) InstallSkills(found []FoundSkillFile, sourceFn func(FoundSkillFile) *Source, agents StringSet) ([]string, error) {
	names := make([]string, 0, len(found))
	seen := map[string]bool{}
	for _, f := range found {
		name := f.Name()
		if err := ValidName(name); err != nil {
			return nil, err
		}
		if s.Skill(name) != nil {
			return nil, fmt.Errorf("the library already has a skill called %s", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("two of the skills picked are called %s", name)
		}
		seen[name] = true
		names = append(names, name)
	}
	for i, f := range found {
		name := names[i]
		dst := s.SkillDir(name)
		if err := ReplaceDir(f.Dir, dst); err != nil {
			return nil, err
		}
		stamp := NowUnix()
		s.Lib.Skills = append(s.Lib.Skills, SkillEntry{
			Name:        name,
			Description: f.Meta.Description,
			Source:      sourceFn(f),
			Agents:      agents.Clone(),
			Enabled:     true,
			Hash:        HashDir(dst),
			Installed:   stamp,
			Updated:     stamp,
		})
	}
	return names, nil
}

func (s *Store) UpdateSkillFrom(name, tree string) (UpdateOutcome, error) {
	e := s.Skill(name)
	if e == nil {
		return 0, fmt.Errorf("no skill called %s", name)
	}
	if e.Source == nil {
		return 0, fmt.Errorf("%s has no source to update from", name)
	}
	want := ""
	if e.Source.Kind == "github" {
		want = e.Source.Path
	}
	found := FindSkills(tree)
	var picked *FoundSkillFile
	for i := range found {
		if found[i].Path == want {
			picked = &found[i]
			break
		}
	}
	if picked == nil {
		for i := range found {
			if found[i].Name() == name {
				picked = &found[i]
				break
			}
		}
	}
	if picked == nil {
		return 0, fmt.Errorf("%s is no longer at its source", name)
	}
	dst := s.SkillDir(name)
	if HashDir(picked.Dir) == HashDir(dst) {
		return UpdateOutcomeUpToDate, nil
	}
	if err := ReplaceDir(picked.Dir, dst); err != nil {
		return 0, err
	}
	hash := HashDir(dst)
	entry, err := s.skillMut(name)
	if err != nil {
		return 0, err
	}
	entry.Hash = hash
	entry.Updated = NowUnix()
	if picked.Meta.Description != "" {
		entry.Description = picked.Meta.Description
	}
	return UpdateOutcomeUpdated, nil
}

func (s *Store) ToggleSkillAgent(name, agent string) (bool, error) {
	e, err := s.skillMut(name)
	if err != nil {
		return false, err
	}
	if !e.Agents.Has(agent) {
		e.Agents.Add(agent)
		e.Enabled = true
		return true, nil
	}
	e.Agents.Delete(agent)
	return false, nil
}

func (s *Store) SetSkillAgents(name string, agents StringSet) error {
	e, err := s.skillMut(name)
	if err != nil {
		return err
	}
	if agents == nil {
		agents = StringSet{}
	}
	e.Agents = agents
	return nil
}

func (s *Store) ToggleSkillEnabled(name string) (bool, error) {
	e, err := s.skillMut(name)
	if err != nil {
		return false, err
	}
	e.Enabled = !e.Enabled
	return e.Enabled, nil
}

func (s *Store) RemoveSkill(name string) error {
	for i := range s.Lib.Skills {
		if s.Lib.Skills[i].Name == name {
			s.Lib.Skills = append(s.Lib.Skills[:i], s.Lib.Skills[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("no skill called %s", name)
}

func (s *Store) FoundSkills() []FoundSkill {
	byName := map[string]*FoundSkill{}
	for _, a := range s.SkillAgents() {
		entries, err := os.ReadDir(a.Skills)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			p := filepath.Join(a.Skills, name)
			if strings.HasPrefix(name, ".") || !isFile(filepath.Join(p, "SKILL.md")) || s.Skill(name) != nil {
				continue
			}
			if LinksTo(p, s.SkillDir(name)) {
				continue
			}
			b, _ := os.ReadFile(filepath.Join(p, "SKILL.md"))
			meta := ParseSkillMeta(string(b))
			f, exists := byName[name]
			if !exists {
				f = &FoundSkill{
					Name:        name,
					Description: meta.Description,
				}
				byName[name] = f
			}
			f.At = append(f.At, FoundSkillAt{Agent: a.ID, Path: p})
		}
	}
	keys := make([]string, 0, len(byName))
	for k := range byName {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]FoundSkill, 0, len(keys))
	for _, k := range keys {
		out = append(out, *byName[k])
	}
	return out
}

func (s *Store) ImportSkill(found FoundSkill) error {
	name := SanitizeSkillName(found.Name)
	if err := ValidName(name); err != nil {
		return err
	}
	if s.Skill(name) != nil {
		return fmt.Errorf("the library already has a skill called %s", name)
	}
	if len(found.At) == 0 {
		return errors.New("found nowhere")
	}
	src, err := filepath.EvalSymlinks(found.At[0].Path)
	if err != nil {
		return err
	}
	dst := s.SkillDir(name)
	if err := ReplaceDir(src, dst); err != nil {
		return err
	}
	stamp := NowUnix()
	agents := StringSet{}
	for _, loc := range found.At {
		if isLink(loc.Path) {
			if err := Unlink(loc.Path); err != nil {
				return err
			}
		} else {
			keep := filepath.Join(s.Root, "backups", "skills", loc.Agent, fmt.Sprintf("%s-%d", found.Name, stamp))
			if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
				return err
			}
			if err := os.Rename(loc.Path, keep); err != nil {
				if copyErr := CopyDir(loc.Path, keep); copyErr != nil {
					return copyErr
				}
				if rmErr := os.RemoveAll(loc.Path); rmErr != nil {
					return rmErr
				}
			}
		}
		agents.Add(loc.Agent)
		ap := s.Lib.Applied[loc.Agent]
		if ap.Skills == nil {
			ap.Skills = StringSet{}
		}
		ap.Skills.Add(name)
		s.Lib.Applied[loc.Agent] = ap
	}
	s.Lib.Skills = append(s.Lib.Skills, SkillEntry{
		Name:        name,
		Description: found.Description,
		Source:      nil,
		Agents:      agents,
		Enabled:     true,
		Hash:        HashDir(dst),
		Installed:   stamp,
		Updated:     stamp,
	})
	return nil
}

// ---- Syncing into agents ----

func (s *Store) Sync() Report {
	report := Report{Issues: []string{}}
	detected := s.Detected()
	for _, a := range detected {
		if a.MCP != nil {
			s.syncMCP(a, a.MCP, &report)
		}
		if a.Skills != "" {
			s.syncSkills(a, a.Skills, &report)
		}
	}
	for id, ap := range s.Lib.Applied {
		if len(ap.MCP) == 0 && len(ap.Skills) == 0 {
			delete(s.Lib.Applied, id)
		}
	}
	return report
}

func (s *Store) syncMCP(a Agent, file *McpFile, report *Report) {
	var want []*Server
	names := StringSet{}
	for i := range s.Lib.MCP {
		if s.ServerState(&s.Lib.MCP[i], a).Kind == StateOn {
			want = append(want, &s.Lib.MCP[i].Server)
			names.Add(s.Lib.MCP[i].Server.Name)
		}
	}
	applied := s.Lib.Applied[a.ID].MCP
	var remove []string
	for name := range applied {
		if !names.Has(name) {
			remove = append(remove, name)
		}
	}
	sort.Strings(remove)

	if err := s.backupAgentFile(a.ID, file.Path); err != nil {
		report.Issues = append(report.Issues, fmt.Sprintf("%s: couldn't back up before writing: %v", a.Name, err))
		return
	}
	changed, err := WriteMCP(file, want, remove)
	if err != nil {
		report.Issues = append(report.Issues, fmt.Sprintf("%s: %v", a.Name, err))
		return
	}
	if changed {
		report.Changed++
	}
	ap := s.Lib.Applied[a.ID]
	ap.MCP = names
	s.Lib.Applied[a.ID] = ap
}

func (s *Store) syncSkills(a Agent, dir string, report *Report) {
	var want []string
	wantSet := StringSet{}
	for i := range s.Lib.Skills {
		if s.SkillState(&s.Lib.Skills[i], a).Kind == StateOn {
			name := s.Lib.Skills[i].Name
			want = append(want, name)
			wantSet.Add(name)
		}
	}
	applied := s.Lib.Applied[a.ID].Skills
	have := StringSet{}
	for _, name := range want {
		target := s.SkillDir(name)
		at := filepath.Join(dir, name)
		if LinksTo(at, target) {
			have.Add(name)
			continue
		}
		if _, err := os.Lstat(at); err == nil {
			if applied.Has(name) && isLink(at) {
				_ = Unlink(at)
			} else {
				report.Issues = append(report.Issues, fmt.Sprintf("%s: already has its own skill called %s", a.Name, name))
				continue
			}
		}
		if err := Link(target, at); err != nil {
			report.Issues = append(report.Issues, fmt.Sprintf("%s: %v", a.Name, err))
		} else {
			report.Changed++
			have.Add(name)
		}
	}

	var toRemove []string
	for name := range applied {
		if !have.Has(name) && !wantSet.Has(name) {
			toRemove = append(toRemove, name)
		}
	}
	sort.Strings(toRemove)
	skillsRoot := s.SkillsDir()
	for _, name := range toRemove {
		at := filepath.Join(dir, name)
		if t, err := os.Readlink(at); err == nil {
			ours := pathHasPrefix(t, skillsRoot) || filepath.Clean(t) == filepath.Clean(s.SkillDir(name))
			if ours {
				if err := Unlink(at); err != nil {
					report.Issues = append(report.Issues, fmt.Sprintf("%s: %v", a.Name, err))
				} else {
					report.Changed++
				}
			}
		}
	}

	ap := s.Lib.Applied[a.ID]
	ap.Skills = have
	s.Lib.Applied[a.ID] = ap
}

func pathHasPrefix(path, prefix string) bool {
	rel, err := filepath.Rel(prefix, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (s *Store) backupAgentFile(agentID, path string) error {
	base := filepath.Base(path)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return nil
	}
	to := filepath.Join(s.Root, "backups", agentID, base)
	if isFile(path) {
		if _, err := os.Stat(to); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}
			if err := copyFile(path, to); err != nil {
				return err
			}
		}
	}
	return nil
}

// Prune removes skill folders under root/skills that no entry references.
func (s *Store) Prune() {
	entries, err := os.ReadDir(s.SkillsDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		p := filepath.Join(s.SkillsDir(), name)
		if s.Skill(name) == nil && isDir(p) {
			_ = os.RemoveAll(p)
		}
	}
}
