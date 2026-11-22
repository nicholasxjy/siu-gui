package core

import (
	"encoding/json"
	"fmt"
)

// ServerDTO matches docs/protocol.md's Server shape with guaranteed non-nil defaults.
type ServerDTO struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
}

func EncodeServerDTO(s Server) ServerDTO {
	args := s.Args
	if args == nil {
		args = []string{}
	}
	env := s.Env
	if env == nil {
		env = map[string]string{}
	}
	headers := s.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	return ServerDTO{
		Name:      s.Name,
		Transport: s.Transport.Label(),
		Command:   s.Command,
		Args:      args,
		Env:       env,
		URL:       s.URL,
		Headers:   headers,
	}
}

func DecodeServerRaw(raw json.RawMessage) (Server, error) {
	var s Server
	if err := json.Unmarshal(raw, &s); err != nil {
		return Server{}, fmt.Errorf("invalid server: %w", err)
	}
	if s.Transport == "" {
		s.Transport = TransportStdio
	}
	return s, nil
}

// AgentDTO matches docs/protocol.md's Agent shape.
type AgentDTO struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Home       string   `json:"home"`
	MCPPath    *string  `json:"mcpPath"`
	SkillsPath *string  `json:"skillsPath"`
	Detected   bool     `json:"detected"`
	Transports []string `json:"transports"`
}

func transportsFor(file *McpFile) []string {
	if file == nil {
		return []string{}
	}
	all := []struct {
		label string
		t     Transport
	}{
		{"stdio", TransportStdio},
		{"http", TransportHttp},
		{"sse", TransportSse},
	}
	out := []string{}
	for _, item := range all {
		probe := Server{
			Name:      "probe",
			Transport: item.t,
			Command:   "x",
			URL:       "https://x",
		}
		if UnsupportedMCP(file.Format, &probe) == "" {
			out = append(out, item.label)
		}
	}
	return out
}

func EncodeAgentDTO(a Agent) AgentDTO {
	var mcpPath *string
	if a.MCP != nil {
		p := a.MCP.Path
		mcpPath = &p
	}
	var skillsPath *string
	if a.Skills != "" {
		p := a.Skills
		skillsPath = &p
	}
	return AgentDTO{
		ID:         a.ID,
		Name:       a.Name,
		Home:       a.Home,
		MCPPath:    mcpPath,
		SkillsPath: skillsPath,
		Detected:   a.Detected(),
		Transports: transportsFor(a.MCP),
	}
}

// McpEntryDTO matches docs/protocol.md's McpEntry shape.
type McpEntryDTO struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Agents    []string          `json:"agents"`
	Enabled   bool              `json:"enabled"`
	Origin    *string           `json:"origin"`
	States    map[string]string `json:"states"`
}

func EncodeMcpEntryDTO(e McpEntry, states map[string]string) McpEntryDTO {
	srv := EncodeServerDTO(e.Server)
	var origin *string
	if e.Origin != "" {
		o := e.Origin
		origin = &o
	}
	return McpEntryDTO{
		Name:      srv.Name,
		Transport: srv.Transport,
		Command:   srv.Command,
		Args:      srv.Args,
		Env:       srv.Env,
		URL:       srv.URL,
		Headers:   srv.Headers,
		Agents:    e.Agents.Sorted(),
		Enabled:   e.Enabled,
		Origin:    origin,
		States:    states,
	}
}

// SkillEntryDTO matches docs/protocol.md's SkillEntry shape.
type SkillEntryDTO struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Source      *Source           `json:"source"`
	Agents      []string          `json:"agents"`
	Enabled     bool              `json:"enabled"`
	Hash        string            `json:"hash"`
	Installed   uint64            `json:"installed"`
	Updated     uint64            `json:"updated"`
	States      map[string]string `json:"states"`
}

func EncodeSkillEntryDTO(e SkillEntry, states map[string]string) SkillEntryDTO {
	return SkillEntryDTO{
		Name:        e.Name,
		Description: e.Description,
		Source:      e.Source,
		Agents:      e.Agents.Sorted(),
		Enabled:     e.Enabled,
		Hash:        e.Hash,
		Installed:   e.Installed,
		Updated:     e.Updated,
		States:      states,
	}
}

// FoundMcpDTO matches docs/protocol.md's FoundMcp shape.
type FoundMcpDTO struct {
	Server ServerDTO `json:"server"`
	Agents []string  `json:"agents"`
}

// FoundSkillAtDTO matches FoundSkill.at items.
type FoundSkillAtDTO struct {
	Agent string `json:"agent"`
	Path  string `json:"path"`
}

// FoundSkillDTO matches docs/protocol.md's FoundSkill shape.
type FoundSkillDTO struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	At          []FoundSkillAtDTO `json:"at"`
}

// SnapshotDTO matches docs/protocol.md's Snapshot shape.
type SnapshotDTO struct {
	AutoConfig   *AppliedConfig        `json:"autoConfig,omitempty"`
	Home         string                `json:"home"`
	Root         string                `json:"root"`
	Platform     string                `json:"platform"`
	Version      string                `json:"version"`
	Agents       []AgentDTO            `json:"agents"`
	Instructions []AgentInstructionDTO `json:"instructions,omitzero"`
	MCP          []McpEntryDTO         `json:"mcp"`
	Skills       []SkillEntryDTO       `json:"skills"`
	FoundMCP     []FoundMcpDTO         `json:"foundMcp"`
	FoundSkills  []FoundSkillDTO       `json:"foundSkills"`
	Issues       []string              `json:"issues"`
}

// ReportDTO matches MutationResult.report.
type ReportDTO struct {
	Changed int      `json:"changed"`
	Issues  []string `json:"issues"`
}

// MutationResultDTO matches docs/protocol.md's MutationResult shape.
type MutationResultDTO struct {
	Snapshot SnapshotDTO `json:"snapshot"`
	Report   ReportDTO   `json:"report"`
	Message  string      `json:"message"`
	Log      *string     `json:"log,omitempty"`
	Theme    *string     `json:"theme,omitempty"`
}

// MarketServerDTO matches docs/protocol.md's MarketServer shape.
type MarketServerDTO struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Publisher   string        `json:"publisher"`
	Homepage    string        `json:"homepage"`
	Server      ServerDTO     `json:"server"`
	Inputs      []MarketInput `json:"inputs"`
	Featured    bool          `json:"featured"`
}

func EncodeMarketServerDTO(m MarketServer) MarketServerDTO {
	inputs := m.Inputs
	if inputs == nil {
		inputs = []MarketInput{}
	}
	return MarketServerDTO{
		ID:          m.ID,
		Title:       m.Title,
		Description: m.Description,
		Publisher:   m.Publisher,
		Homepage:    m.Homepage,
		Server:      EncodeServerDTO(m.Server),
		Inputs:      inputs,
		Featured:    m.Featured,
	}
}

// DiscoveryDTO matches docs/protocol.md's discover response.
type DiscoveryDTO struct {
	Servers  []MarketServerDTO `json:"servers"`
	Skills   []MarketSkill     `json:"skills"`
	Warnings []string          `json:"warnings"`
	Cached   bool              `json:"cached"`
}

// ScanSkillDTO matches ScanResult.skills items.
type ScanSkillDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Installed   bool   `json:"installed"`
}

// ScanResultDTO matches docs/protocol.md's ScanResult shape.
type ScanResultDTO struct {
	Token  string         `json:"token"`
	Source string         `json:"source"`
	Skills []ScanSkillDTO `json:"skills"`
}

func EncodeScanResultDTO(token, source string, found []FoundSkillFile, store *Store) ScanResultDTO {
	skills := make([]ScanSkillDTO, 0, len(found))
	for _, f := range found {
		skills = append(skills, ScanSkillDTO{
			Name:        f.Name(),
			Description: f.Meta.Description,
			Path:        f.Path,
			Installed:   store.Skill(f.Name()) != nil,
		})
	}
	return ScanResultDTO{
		Token:  token,
		Source: source,
		Skills: skills,
	}
}

// BuildSnapshot constructs SnapshotDTO with observed states and issues.
func BuildSnapshot(store *Store, home, version string) SnapshotDTO {
	return buildSnapshot(store, home, version, true)
}

func (s *Service) buildSnapshot(store *Store, home string) SnapshotDTO {
	return buildSnapshot(store, home, s.version, !s.terminal)
}

func buildSnapshot(store *Store, home, version string, includeInstructions bool) SnapshotDTO {
	issues := []string{}
	mcpList := make([]McpEntryDTO, 0, len(store.Lib.MCP))
	for i := range store.Lib.MCP {
		e := &store.Lib.MCP[i]
		states := MCPStates(store, e)
		CollectStateIssues("MCP server", e.Server.Name, states, store.Agents, &issues)
		mcpList = append(mcpList, EncodeMcpEntryDTO(*e, states))
	}
	skillList := make([]SkillEntryDTO, 0, len(store.Lib.Skills))
	for i := range store.Lib.Skills {
		e := &store.Lib.Skills[i]
		states := SkillStates(store, e)
		CollectStateIssues("Skill", e.Name, states, store.Agents, &issues)
		skillList = append(skillList, EncodeSkillEntryDTO(*e, states))
	}
	agents := make([]AgentDTO, 0, len(store.Agents))
	for _, a := range store.Agents {
		agents = append(agents, EncodeAgentDTO(a))
	}
	foundMCP := []FoundMcpDTO{}
	for _, f := range store.FoundServers() {
		ag := f.Agents
		if ag == nil {
			ag = []string{}
		}
		foundMCP = append(foundMCP, FoundMcpDTO{
			Server: EncodeServerDTO(f.Server),
			Agents: ag,
		})
	}
	foundSkills := []FoundSkillDTO{}
	for _, f := range store.FoundSkills() {
		at := make([]FoundSkillAtDTO, 0, len(f.At))
		for _, loc := range f.At {
			at = append(at, FoundSkillAtDTO{Agent: loc.Agent, Path: loc.Path})
		}
		foundSkills = append(foundSkills, FoundSkillDTO{
			Name:        f.Name,
			Description: f.Description,
			At:          at,
		})
	}
	CollectOrphanIssues(store, &issues)
	for _, agent := range store.Agents {
		if !agent.Detected() || agent.MCP == nil {
			continue
		}
		if _, err := ReadMCP(agent.MCP); err != nil {
			issues = append(issues, fmt.Sprintf("%s: cannot read MCP configuration at %s", agent.Name, agent.MCP.Path))
		}
	}
	var instructions []AgentInstructionDTO
	if includeInstructions {
		instructions = listAgentInstructions(store.Agents, store.env)
	}
	return SnapshotDTO{
		Home:         home,
		Root:         store.Root,
		Platform:     platformName(),
		Version:      version,
		Agents:       agents,
		Instructions: instructions,
		MCP:          mcpList,
		Skills:       skillList,
		FoundMCP:     foundMCP,
		FoundSkills:  foundSkills,
		Issues:       issues,
	}
}

// BuildMutationResult builds MutationResultDTO merging report issues and observed snapshot issues.
func BuildMutationResult(store *Store, home, version string, report Report, message string, logText *string) MutationResultDTO {
	return buildMutationResult(BuildSnapshot(store, home, version), report, message, logText)
}

func (s *Service) buildMutationResult(store *Store, home string, report Report, message string, logText *string) MutationResultDTO {
	return buildMutationResult(s.buildSnapshot(store, home), report, message, logText)
}

func buildMutationResult(snap SnapshotDTO, report Report, message string, logText *string) MutationResultDTO {
	issues := append([]string{}, report.Issues...)
	seen := map[string]bool{}
	for _, iss := range issues {
		seen[iss] = true
	}
	for _, iss := range snap.Issues {
		if !seen[iss] {
			seen[iss] = true
			issues = append(issues, iss)
		}
	}
	if issues == nil {
		issues = []string{}
	}
	return MutationResultDTO{
		Snapshot: snap,
		Report:   ReportDTO{Changed: report.Changed, Issues: issues},
		Message:  message,
		Log:      logText,
	}
}
