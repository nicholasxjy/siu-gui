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

func TestServiceAgentInstructionDisableEnable(t *testing.T) {
	home := t.TempDir()
	t.Setenv(TestHomeVar, home)
	piDir := filepath.Join(home, ".pi", "agent")
	for _, dir := range []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".codex"),
		filepath.Join(home, ".grok"),
		filepath.Join(home, ".agents"),
		filepath.Join(home, ".factory"),
		piDir,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	codexPath := filepath.Join(home, ".codex", "AGENTS.md")
	overridePath := filepath.Join(home, ".codex", "AGENTS.override.md")
	claudePath := filepath.Join(home, ".claude", "CLAUDE.md")
	piPath := filepath.Join(piDir, "AGENTS.MD")
	sharedPath := filepath.Join(home, ".agents", "AGENTS.md")
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
	assertMissing := func(path string) {
		t.Helper()
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be absent, got err=%v", path, err)
		}
	}
	snapshotEntries := func(svc *Service) map[string]map[string]any {
		t.Helper()
		out := map[string]map[string]any{}
		for _, row := range callOK(t, svc, "snapshot", nil)["instructions"].([]any) {
			entry := row.(map[string]any)
			out[entry["id"].(string)] = entry
		}
		return out
	}

	write(codexPath, "# Codex rules\n")
	write(overridePath, "# Override rules\n")
	write(piPath, "# Pi alias\n")
	write(sharedPath, "# Shared droid alias\n")
	if err := os.Mkdir(grokPath, 0o700); err != nil {
		t.Fatal(err)
	}

	svc := NewService("test")
	defer svc.Close()

	entries := snapshotEntries(svc)
	codexID := "codex:" + codexPath
	overrideID := "codex:" + overridePath
	claudeID := "claude:" + claudePath
	piID := "pi:" + piPath
	sharedID := "droid:" + sharedPath
	grokID := "grok:" + grokPath

	for _, id := range []string{codexID, overrideID, claudeID, piID, sharedID, grokID} {
		if _, ok := entries[id]; !ok {
			t.Fatalf("missing expected entry %s in %v", id, entries)
		}
	}

	// Disable an available instruction file.
	res := callOK(t, svc, "set_instruction_enabled", map[string]any{"id": codexID, "enabled": false})
	if res["report"].(map[string]any)["changed"] != float64(1) {
		t.Fatalf("expected changed=1, got %v", res["report"])
	}
	assertMissing(codexPath)
	check(disabledInstructionPath(codexPath), "# Codex rules\n")

	entries = snapshotEntries(svc)
	if entries[codexID]["status"] != "disabled" || entries[codexID]["path"] != codexPath || entries[codexID]["name"] != "AGENTS.md" {
		t.Fatalf("unexpected disabled codex entry: %v", entries[codexID])
	}

	// Idempotent disable returns changed=0.
	res = callOK(t, svc, "set_instruction_enabled", map[string]any{"id": codexID, "enabled": false})
	if res["report"].(map[string]any)["changed"] != float64(0) {
		t.Fatalf("expected changed=0 on idempotent disable, got %v", res["report"])
	}

	// Reading and editing while disabled updates .siu-disabled and preserves disabled state.
	readDisabled := callOK(t, svc, "instruction_content", map[string]any{"id": codexID})
	if readDisabled["content"] != "# Codex rules\n" || readDisabled["revision"] == "" {
		t.Fatalf("unexpected disabled read: %v", readDisabled)
	}
	callErr(t, svc, "save_instruction", map[string]any{"id": codexID, "content": "# Stale", "revision": "wrong"})
	callOK(t, svc, "save_instruction", map[string]any{"id": codexID, "content": "# Updated while disabled\n", "revision": readDisabled["revision"]})
	assertMissing(codexPath)
	check(disabledInstructionPath(codexPath), "# Updated while disabled\n")
	if entries = snapshotEntries(svc); entries[codexID]["status"] != "disabled" {
		t.Fatalf("expected disabled status after edit, got %v", entries[codexID])
	}

	// Create mode without revision must fail when disabled file exists.
	callErr(t, svc, "save_instruction", map[string]any{"id": codexID, "content": "# Conflict\n"})
	assertMissing(codexPath)

	// Re-enable restores active file with updated content and removes .siu-disabled.
	res = callOK(t, svc, "set_instruction_enabled", map[string]any{"id": codexID, "enabled": true})
	if res["report"].(map[string]any)["changed"] != float64(1) {
		t.Fatalf("expected changed=1 on re-enable, got %v", res["report"])
	}
	check(codexPath, "# Updated while disabled\n")
	assertMissing(disabledInstructionPath(codexPath))
	if entries = snapshotEntries(svc); entries[codexID]["status"] != "available" {
		t.Fatalf("expected available status after re-enable, got %v", entries[codexID])
	}

	// Idempotent enable returns changed=0.
	res = callOK(t, svc, "set_instruction_enabled", map[string]any{"id": codexID, "enabled": true})
	if res["report"].(map[string]any)["changed"] != float64(0) {
		t.Fatalf("expected changed=0 on idempotent enable, got %v", res["report"])
	}

	// Deleting a disabled file removes .siu-disabled and sets status to missing.
	callOK(t, svc, "set_instruction_enabled", map[string]any{"id": codexID, "enabled": false})
	callOK(t, svc, "delete_instruction", map[string]any{"id": codexID})
	assertMissing(codexPath)
	assertMissing(disabledInstructionPath(codexPath))
	if entries = snapshotEntries(svc); entries[codexID]["status"] != "missing" {
		t.Fatalf("expected missing status after deleting disabled file, got %v", entries[codexID])
	}

	// Missing and unavailable entries reject set_instruction_enabled.
	callErr(t, svc, "set_instruction_enabled", map[string]any{"id": claudeID, "enabled": false})
	callErr(t, svc, "set_instruction_enabled", map[string]any{"id": claudeID, "enabled": true})
	callErr(t, svc, "set_instruction_enabled", map[string]any{"id": grokID, "enabled": false})
	callErr(t, svc, "set_instruction_enabled", map[string]any{"id": grokID, "enabled": true})

	// Compatibility aliases remain listed when disabled and restore cleanly.
	for _, tc := range []struct {
		id   string
		path string
		want string
	}{
		{overrideID, overridePath, "# Override rules\n"},
		{piID, piPath, "# Pi alias\n"},
		{sharedID, sharedPath, "# Shared droid alias\n"},
	} {
		callOK(t, svc, "set_instruction_enabled", map[string]any{"id": tc.id, "enabled": false})
		assertMissing(tc.path)
		check(disabledInstructionPath(tc.path), tc.want)
		entries = snapshotEntries(svc)
		if entries[tc.id]["status"] != "disabled" {
			t.Fatalf("expected %s disabled, got %v", tc.id, entries[tc.id])
		}
		// Ensure Pi alias didn't also expose a duplicate primary AGENTS.md entry.
		if tc.id == piID {
			if _, dup := entries["pi:"+filepath.Join(piDir, "AGENTS.md")]; dup {
				t.Fatalf("unexpected duplicate primary entry alongside disabled %s", tc.id)
			}
		}
		callOK(t, svc, "set_instruction_enabled", map[string]any{"id": tc.id, "enabled": true})
		check(tc.path, tc.want)
		assertMissing(disabledInstructionPath(tc.path))
	}

	// Conflict when both active and .siu-disabled files exist on disk.
	write(codexPath, "# Active conflict\n")
	write(disabledInstructionPath(codexPath), "# Disabled conflict\n")
	entries = snapshotEntries(svc)
	if entries[codexID]["status"] != "unavailable" || !strings.Contains(entries[codexID]["issue"].(string), "冲突") {
		t.Fatalf("expected conflict unavailable status, got %v", entries[codexID])
	}
	callErr(t, svc, "set_instruction_enabled", map[string]any{"id": codexID, "enabled": true})
	callErr(t, svc, "set_instruction_enabled", map[string]any{"id": codexID, "enabled": false})
	callErr(t, svc, "instruction_content", map[string]any{"id": codexID})
	callErr(t, svc, "save_instruction", map[string]any{"id": codexID, "content": "# overwrite"})
	callErr(t, svc, "delete_instruction", map[string]any{"id": codexID})
	check(codexPath, "# Active conflict\n")
	check(disabledInstructionPath(codexPath), "# Disabled conflict\n")

	// Symlink disable/enable preserves symlink target.
	targetFile := filepath.Join(home, "shared-claude.md")
	write(targetFile, "# Symlinked rules\n")
	if err := os.Symlink(targetFile, claudePath); err == nil {
		callOK(t, svc, "set_instruction_enabled", map[string]any{"id": claudeID, "enabled": false})
		assertMissing(claudePath)
		if dst, err := os.Readlink(disabledInstructionPath(claudePath)); err != nil || dst != targetFile {
			t.Fatalf("disabled symlink target = %q, %v, want %q", dst, err, targetFile)
		}
		callOK(t, svc, "set_instruction_enabled", map[string]any{"id": claudeID, "enabled": true})
		if dst, err := os.Readlink(claudePath); err != nil || dst != targetFile {
			t.Fatalf("restored symlink target = %q, %v, want %q", dst, err, targetFile)
		}
		check(targetFile, "# Symlinked rules\n")
	} else {
		t.Logf("skipping symlink disable/enable check: %v", err)
	}
}
