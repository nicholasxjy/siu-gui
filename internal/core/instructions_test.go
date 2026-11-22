package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestServiceAgentInstructionLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv(TestHomeVar, home)
	for _, dir := range []string{".claude", ".codex", ".cursor", ".grok"} {
		if err := os.Mkdir(filepath.Join(home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	claudePath := filepath.Join(home, ".claude", "CLAUDE.md")
	codexPath := filepath.Join(home, ".codex", "AGENTS.md")
	overridePath := filepath.Join(home, ".codex", "AGENTS.override.md")
	grokPath := filepath.Join(home, ".grok", "AGENTS.md")
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	check := func(path, want string) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("read %s = %q, %v, want %q", path, got, err, want)
		}
	}
	write(claudePath, "# 已有规则\n")
	write(overridePath, "# Override\n")
	if err := os.Mkdir(grokPath, 0o700); err != nil {
		t.Fatal(err)
	}
	svc := NewService("test")
	defer svc.Close()

	entries := map[string]map[string]any{}
	for _, row := range callOK(t, svc, "snapshot", nil)["instructions"].([]any) {
		entry := row.(map[string]any)
		entries[entry["path"].(string)] = entry
	}
	if len(entries) != 4 || entries[claudePath]["status"] != "available" || entries[codexPath]["status"] != "missing" ||
		entries[overridePath]["status"] != "available" || entries[grokPath]["status"] != "unavailable" {
		t.Fatalf("unexpected instruction entries: %v", entries)
	}
	codex := entries[codexPath]["id"]
	original := "# 中文\n\n- 保留末尾空格  \n"
	create := map[string]any{"id": codex, "content": original}
	callOK(t, svc, "save_instruction", create)
	callErr(t, svc, "save_instruction", map[string]any{"id": codex, "content": "should not overwrite"})
	check(codexPath, original)
	read := func() map[string]any { return callOK(t, svc, "instruction_content", map[string]any{"id": codex}) }
	first := read()
	if first["content"] != original || first["revision"] == "" {
		t.Fatalf("unexpected read: %v", first)
	}

	updated := "# Edited\n"
	callOK(t, svc, "save_instruction", map[string]any{"id": codex, "content": updated, "revision": first["revision"]})
	check(codexPath, updated)
	second := read()
	write(codexPath, "# Changed elsewhere\n")
	err := callErr(t, svc, "save_instruction", map[string]any{"id": codex, "content": "stale edit", "revision": second["revision"]})
	if !strings.Contains(err, "其他程序更改") {
		t.Fatalf("unexpected stale edit error: %q", err)
	}
	check(codexPath, "# Changed elsewhere\n")
	callOK(t, svc, "delete_instruction", map[string]any{"id": codex})
	if _, err := os.Stat(codexPath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, got %v", err)
	}
	callErr(t, svc, "delete_instruction", map[string]any{"id": entries[grokPath]["id"]})
	callErr(t, svc, "delete_instruction", map[string]any{"id": "codex:" + claudePath})
	check(overridePath, "# Override\n")
	check(claudePath, "# 已有规则\n")
}

func TestAgentInstructionNativePaths(t *testing.T) {
	home := t.TempDir()
	env := EnvRooted(home)
	env.GeminiHome = filepath.Join(home, "custom-gemini")
	env.OpenCodeDir = filepath.Join(home, "custom-opencode")
	env.CopilotHome = filepath.Join(home, "custom-copilot")
	env.PiDir = filepath.Join(home, "custom-pi")
	geminiDir := filepath.Join(env.GeminiHome, ".gemini")
	antigravityDir := filepath.Join(home, ".gemini", "config")
	for _, dir := range []string{geminiDir, env.OpenCodeDir, env.CopilotHome, env.PiDir, antigravityDir, filepath.Join(home, ".factory"), filepath.Join(home, ".agents")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(geminiDir, "settings.json"), []byte(`{ /* user settings */ "context":{"fileName":["AGENTS.md","MY-RULES.md","../UNRELATED.md"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(env.PiDir, "AGENTS.MD"), filepath.Join(home, ".agents", "AGENTS.md"), filepath.Join(antigravityDir, "GEMINI.md")} {
		if err := os.WriteFile(path, []byte("# Rules\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string][]string{}
	for _, file := range listAgentInstructions(AllAgents(env), env) {
		got[file.Agent] = append(got[file.Agent], file.Path)
	}
	want := map[string][]string{
		"gemini":      {filepath.Join(geminiDir, "AGENTS.md"), filepath.Join(geminiDir, "MY-RULES.md")},
		"opencode":    {filepath.Join(env.OpenCodeDir, "AGENTS.md")},
		"copilot":     {filepath.Join(env.CopilotHome, "copilot-instructions.md")},
		"droid":       {filepath.Join(home, ".factory", "AGENTS.md"), filepath.Join(home, ".agents", "AGENTS.md")},
		"pi":          {filepath.Join(env.PiDir, "AGENTS.MD")},
		"antigravity": {filepath.Join(antigravityDir, "AGENTS.md"), filepath.Join(antigravityDir, "GEMINI.md")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("instruction paths = %v, want %v", got, want)
	}
}
