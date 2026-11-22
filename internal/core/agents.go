package core

import (
	"os"
	"path/filepath"
)

// McpFormat identifies how an agent's MCP configuration file spells a server.
type McpFormat int

const (
	FormatClaude McpFormat = iota
	FormatCursor
	FormatDesktop
	FormatCodex
	FormatGemini
	FormatOpenCode
	FormatCopilot
	FormatPi
	FormatGrok
	FormatAntigravity
)

// McpFile locates an agent's MCP configuration file and its format.
type McpFile struct {
	Path   string
	Format McpFormat
}

// Agent describes a coding agent that siu can manage.
type Agent struct {
	ID     string
	Name   string
	Home   string
	MCP    *McpFile
	Skills string // empty if agent has no skills folder
}

// Detected reports whether the agent's home folder exists on disk.
func (a Agent) Detected() bool {
	info, err := os.Stat(a.Home)
	return err == nil && info.IsDir()
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// AllAgents returns every coding agent siu knows for env.
func AllAgents(env Env) []Agent {
	h := func(p string) string { return filepath.Join(env.Home, p) }

	claudeDir := env.ClaudeDir
	if claudeDir == "" {
		claudeDir = h(".claude")
	}
	codexDir := env.CodexHome
	if codexDir == "" {
		codexDir = h(".codex")
	}
	var claudeJSON string
	if env.ClaudeDir != "" {
		claudeJSON = filepath.Join(env.ClaudeDir, ".claude.json")
	} else {
		claudeJSON = h(".claude.json")
	}

	opencodeDir := h(filepath.Join(".config", "opencode"))
	opencodeFile := filepath.Join(opencodeDir, "opencode.json")
	if isFile(filepath.Join(opencodeDir, "opencode.jsonc")) && !isFile(opencodeFile) {
		opencodeFile = filepath.Join(opencodeDir, "opencode.jsonc")
	}

	piDir := env.PiDir
	if piDir == "" {
		piDir = h(filepath.Join(".pi", "agent"))
	}
	grokDir := env.GrokHome
	if grokDir == "" {
		grokDir = h(".grok")
	}

	agyDir := h(filepath.Join(".gemini", "config"))
	agyHome := agyDir
	for _, cand := range []string{
		h(filepath.Join(".gemini", "antigravity-cli")),
		h(filepath.Join(".gemini", "antigravity")),
	} {
		if isDir(cand) {
			agyHome = cand
			break
		}
	}

	desktopDir := filepath.Join(env.Config, "Claude")
	mcpFile := func(path string, format McpFormat) *McpFile {
		return &McpFile{Path: path, Format: format}
	}

	return []Agent{
		{
			ID:     "claude",
			Name:   "Claude Code",
			Home:   claudeDir,
			MCP:    mcpFile(claudeJSON, FormatClaude),
			Skills: filepath.Join(claudeDir, "skills"),
		},
		{
			ID:     "codex",
			Name:   "Codex",
			Home:   codexDir,
			MCP:    mcpFile(filepath.Join(codexDir, "config.toml"), FormatCodex),
			Skills: filepath.Join(codexDir, "skills"),
		},
		{
			ID:     "gemini",
			Name:   "Gemini CLI",
			Home:   h(".gemini"),
			MCP:    mcpFile(h(filepath.Join(".gemini", "settings.json")), FormatGemini),
			Skills: h(filepath.Join(".gemini", "skills")),
		},
		{
			ID:     "opencode",
			Name:   "OpenCode",
			Home:   opencodeDir,
			MCP:    mcpFile(opencodeFile, FormatOpenCode),
			Skills: filepath.Join(opencodeDir, "skills"),
		},
		{
			ID:     "cursor",
			Name:   "Cursor",
			Home:   h(".cursor"),
			MCP:    mcpFile(h(filepath.Join(".cursor", "mcp.json")), FormatCursor),
			Skills: h(filepath.Join(".cursor", "skills")),
		},
		{
			ID:     "copilot",
			Name:   "Copilot CLI",
			Home:   h(".copilot"),
			MCP:    mcpFile(h(filepath.Join(".copilot", "mcp-config.json")), FormatCopilot),
			Skills: h(filepath.Join(".copilot", "skills")),
		},
		{
			ID:     "droid",
			Name:   "Droid",
			Home:   h(".factory"),
			MCP:    mcpFile(h(filepath.Join(".factory", "mcp.json")), FormatClaude),
			Skills: h(filepath.Join(".factory", "skills")),
		},
		{
			ID:     "pi",
			Name:   "Pi",
			Home:   piDir,
			MCP:    mcpFile(filepath.Join(piDir, "mcp.json"), FormatPi),
			Skills: filepath.Join(piDir, "skills"),
		},
		{
			ID:     "grok",
			Name:   "Grok",
			Home:   grokDir,
			MCP:    mcpFile(filepath.Join(grokDir, "config.toml"), FormatGrok),
			Skills: filepath.Join(grokDir, "skills"),
		},
		{
			ID:     "antigravity",
			Name:   "Antigravity",
			Home:   agyHome,
			MCP:    mcpFile(filepath.Join(agyDir, "mcp_config.json"), FormatAntigravity),
			Skills: filepath.Join(agyDir, "skills"),
		},
		{
			ID:     "claude-desktop",
			Name:   "Claude Desktop",
			Home:   desktopDir,
			MCP:    mcpFile(filepath.Join(desktopDir, "claude_desktop_config.json"), FormatDesktop),
			Skills: "",
		},
		{
			ID:     "agents",
			Name:   "~/.agents (shared)",
			Home:   h(".agents"),
			MCP:    nil,
			Skills: h(filepath.Join(".agents", "skills")),
		},
	}
}
