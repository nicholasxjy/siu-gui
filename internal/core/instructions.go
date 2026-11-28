package core

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	maxInstructionBytes       = 1 << 20
	disabledInstructionSuffix = ".siu-disabled"
)

func disabledInstructionPath(path string) string {
	return path + disabledInstructionSuffix
}

type AgentInstructionDTO struct {
	ID     string `json:"id"`
	Agent  string `json:"agent"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Issue  string `json:"issue,omitempty"`
}

type instructionContent struct {
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

type instructionGroup struct {
	dir     string
	primary []string
	aliases []string
}

// Primary names can be created. Compatibility aliases are exposed when present.
func instructionGroups(a Agent, env Env) []instructionGroup {
	dir := a.Home
	home := func(name string) string { return filepath.Join(env.Home, name) }
	switch a.ID {
	case "gemini":
		if env.GeminiHome != "" {
			dir = filepath.Join(env.GeminiHome, ".gemini")
		}
	case "opencode":
		if env.OpenCodeDir != "" {
			dir = env.OpenCodeDir
		} else if env.XDGConfig != "" {
			dir = filepath.Join(env.XDGConfig, "opencode")
		}
	case "copilot":
		if env.CopilotHome != "" {
			dir = env.CopilotHome
		}
	}
	if !filepath.IsAbs(dir) || !isDir(dir) {
		return nil
	}
	group := func(name string, aliases ...string) instructionGroup {
		return instructionGroup{dir: dir, primary: []string{name}, aliases: aliases}
	}
	switch a.ID {
	case "claude":
		return []instructionGroup{group("CLAUDE.md")}
	case "codex":
		return []instructionGroup{group("AGENTS.md", "AGENTS.override.md")}
	case "gemini":
		return []instructionGroup{{dir: dir, primary: geminiInstructionNames(dir)}}
	case "opencode":
		return []instructionGroup{group("AGENTS.md")}
	case "copilot":
		return []instructionGroup{group("copilot-instructions.md")}
	case "droid":
		aliases := []string{"agents.md", "Agents.md", "CLAUDE.md", "Claude.md"}
		compatible := append([]string{"AGENTS.md"}, aliases...)
		return []instructionGroup{
			group("AGENTS.md", aliases...),
			{dir: home(".agents"), aliases: compatible},
			{dir: home(".agent"), aliases: compatible},
		}
	case "pi":
		return []instructionGroup{group("AGENTS.md", "AGENTS.override.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD")}
	case "grok":
		return []instructionGroup{group("AGENTS.md", "Agents.md", "AGENT.md", "CLAUDE.md", "Claude.md", "CLAUDE.local.md")}
	case "antigravity":
		return []instructionGroup{
			{dir: home(filepath.Join(".gemini", "config")), primary: []string{"AGENTS.md"}, aliases: []string{"GEMINI.md"}},
			{dir: home(".gemini"), aliases: []string{"AGENTS.md", "GEMINI.md"}},
		}
	}
	return nil
}

func geminiInstructionNames(dir string) []string {
	var settings struct {
		Context struct {
			FileName json.RawMessage `json:"fileName"`
		} `json:"context"`
	}
	path := filepath.Join(dir, "settings.json")
	if status, _ := fileStatus(path); status != "available" {
		return []string{"GEMINI.md"}
	}
	data, err := readBounded(path, maxInstructionBytes)
	if err != nil || json.Unmarshal([]byte(StripJSONC(string(data))), &settings) != nil || len(settings.Context.FileName) == 0 {
		return []string{"GEMINI.md"}
	}
	var names []string
	var name string
	if json.Unmarshal(settings.Context.FileName, &name) == nil {
		if name == "" {
			return []string{"GEMINI.md"}
		}
		names = []string{name}
	} else if json.Unmarshal(settings.Context.FileName, &names) != nil || len(names) == 0 {
		return []string{"GEMINI.md"}
	}
	var files []string
	for _, name := range names {
		if filepath.IsLocal(name) && !strings.ContainsAny(name, "/\\:\x00") && strings.EqualFold(filepath.Ext(name), ".md") && !slices.Contains(files, name) {
			files = append(files, name)
		}
	}
	return files
}

func listAgentInstructions(agents []Agent, env Env) []AgentInstructionDTO {
	files := []AgentInstructionDTO{}
	for _, agent := range agents {
		for _, group := range instructionGroups(agent, env) {
			for _, name := range group.fileNames() {
				path := filepath.Join(group.dir, name)
				status, _, issue := instructionEntryStatus(path)
				files = append(files, AgentInstructionDTO{
					ID: agent.ID + ":" + path, Agent: agent.ID, Name: name, Path: path,
					Status: status, Issue: issue,
				})
			}
		}
	}
	return files
}

func (g instructionGroup) fileNames() []string {
	if len(g.aliases) == 0 {
		return g.primary
	}
	// Match the directory entry's spelling so aliases don't appear twice on case-insensitive filesystems.
	entries, _ := os.ReadDir(g.dir)
	var actual, aliases []string
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), disabledInstructionSuffix)
		if !slices.Contains(actual, name) {
			actual = append(actual, name)
		}
		if slices.Contains(g.aliases, name) && !slices.Contains(aliases, name) {
			aliases = append(aliases, name)
		}
	}
	var names []string
	for _, name := range g.primary {
		if slices.Contains(actual, name) || !slices.ContainsFunc(aliases, func(alias string) bool {
			return strings.EqualFold(name, alias)
		}) {
			names = append(names, name)
		}
	}
	return append(names, aliases...)
}

func fileStatus(path string) (status, issue string) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", ""
	}
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(path)
	}
	if err != nil {
		return "unavailable", err.Error()
	}
	if !info.Mode().IsRegular() {
		return "unavailable", "指令文件路径指向的不是普通文件。"
	}
	return "available", ""
}

func instructionEntryStatus(path string) (status, storagePath, issue string) {
	disabledPath := disabledInstructionPath(path)
	activeStatus, activeIssue := fileStatus(path)
	disabledStatus, disabledIssue := fileStatus(disabledPath)

	switch {
	case activeStatus == "missing" && disabledStatus == "missing":
		return "missing", path, ""
	case activeStatus == "available" && disabledStatus == "missing":
		return "available", path, ""
	case activeStatus == "missing" && disabledStatus == "available":
		return "disabled", disabledPath, ""
	case activeStatus != "missing" && disabledStatus != "missing":
		return "unavailable", "", fmt.Sprintf("同时存在指令文件和停用文件（%s），请先手动处理冲突。", filepath.Base(disabledPath))
	case activeStatus == "unavailable":
		return "unavailable", "", activeIssue
	default:
		return "unavailable", "", disabledIssue
	}
}

func requireInstructionStorage(path string) (status, storagePath string, err error) {
	status, storagePath, issue := instructionEntryStatus(path)
	switch status {
	case "available", "disabled":
		return status, storagePath, nil
	case "missing":
		return "", "", fmt.Errorf("指令文件已不存在：%s", path)
	default:
		return "", "", errors.New(issue)
	}
}

func readInstructionFile(storagePath string) (instructionContent, error) {
	data, err := readBounded(storagePath, maxInstructionBytes)
	if err != nil {
		return instructionContent{}, err
	}
	if !utf8.Valid(data) {
		return instructionContent{}, fmt.Errorf("指令文件必须是 UTF-8 文本：%s", storagePath)
	}
	return instructionContent{Content: string(data), Revision: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}

func readInstruction(path string) (instructionContent, error) {
	_, storagePath, err := requireInstructionStorage(path)
	if err != nil {
		return instructionContent{}, err
	}
	return readInstructionFile(storagePath)
}

func createInstruction(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

func (s *Service) hInstruction(method string, params map[string]json.RawMessage) (any, error) {
	id, err := paramStr(params, "id")
	if err != nil {
		return nil, err
	}
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	files := listAgentInstructions(store.Agents, resolved.Env)
	index := slices.IndexFunc(files, func(file AgentInstructionDTO) bool { return file.ID == id })
	if index < 0 {
		return nil, errors.New("找不到这个 Agent 的指令文件，请刷新列表。")
	}
	path := files[index].Path
	switch method {
	case "instruction_content":
		return readInstruction(path)
	case "set_instruction_enabled":
		enabled, err := paramBool(params, "enabled")
		if err != nil {
			return nil, err
		}
		status, _, issue := instructionEntryStatus(path)
		disabledPath := disabledInstructionPath(path)
		switch status {
		case "missing":
			if enabled {
				return nil, errors.New("指令文件尚未创建，无法启用。")
			}
			return nil, errors.New("指令文件尚未创建，无法停用。")
		case "unavailable":
			return nil, errors.New(issue)
		case "available":
			if enabled {
				return BuildMutationResult(store, resolved.Env.Home, s.version, Report{Changed: 0}, "已启用指令文件", nil), nil
			}
			if _, err := os.Lstat(disabledPath); !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("停用文件已存在，请先手动处理：%s", disabledPath)
			}
			if err := os.Rename(path, disabledPath); err != nil {
				return nil, err
			}
			return BuildMutationResult(store, resolved.Env.Home, s.version, Report{Changed: 1}, "已停用指令文件", nil), nil
		case "disabled":
			if !enabled {
				return BuildMutationResult(store, resolved.Env.Home, s.version, Report{Changed: 0}, "已停用指令文件", nil), nil
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("指令文件已存在，请先手动处理：%s", path)
			}
			if err := os.Rename(disabledPath, path); err != nil {
				return nil, err
			}
			return BuildMutationResult(store, resolved.Env.Home, s.version, Report{Changed: 1}, "已启用指令文件", nil), nil
		default:
			return nil, fmt.Errorf("未知指令文件状态：%s", status)
		}
	case "delete_instruction":
		_, storagePath, err := requireInstructionStorage(path)
		if err != nil {
			return nil, err
		}
		if err := os.Remove(storagePath); err != nil {
			return nil, err
		}
		return BuildMutationResult(store, resolved.Env.Home, s.version, Report{Changed: 1}, "已删除指令文件", nil), nil
	default:
		content, err := paramStr(params, "content")
		if err != nil {
			return nil, err
		}
		if len(content) > maxInstructionBytes {
			return nil, errors.New("指令文件不能超过 1 MiB。")
		}
		if _, editing := params["revision"]; editing {
			revision, err := paramStr(params, "revision")
			if err != nil {
				return nil, err
			}
			_, storagePath, err := requireInstructionStorage(path)
			if err != nil {
				return nil, err
			}
			current, err := readInstructionFile(storagePath)
			if err != nil {
				return nil, err
			}
			if current.Revision != revision {
				return nil, errors.New("指令文件已被其他程序更改。请保留草稿并重新打开文件。")
			}
			if err := WriteAtomic(storagePath, []byte(content)); err != nil {
				return nil, err
			}
		} else {
			status, _, issue := instructionEntryStatus(path)
			switch status {
			case "disabled":
				return nil, fmt.Errorf("指令文件已存在（当前已停用）：%s", path)
			case "unavailable":
				return nil, errors.New(issue)
			case "available":
				return nil, fmt.Errorf("指令文件已存在：%s", path)
			}
			if err := createInstruction(path, content); err != nil {
				return nil, err
			}
		}
		return BuildMutationResult(store, resolved.Env.Home, s.version, Report{Changed: 1}, "已保存指令文件", nil), nil
	}
}
