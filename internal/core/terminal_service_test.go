package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTerminalTestHome(t *testing.T) (home, codexDoc string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv(TestHomeVar, home)
	for _, dir := range []string{".claude", ".codex", ".siu"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	codexDoc = filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.WriteFile(codexDoc, []byte("# keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, codexDoc
}

func writeFixtureWith(t *testing.T, path string, mutate func(map[string]any)) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "siu-backup-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(doc)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(out)
	return hex.EncodeToString(sum[:])
}

func assertCodexDocIntact(t *testing.T, codexDoc string) {
	t.Helper()
	if got, err := os.ReadFile(codexDoc); err != nil || string(got) != "# keep me\n" {
		t.Fatalf("AGENTS.md changed: %q, %v", got, err)
	}
}

func TestTerminalServiceBackupLifecycleAndInstructionsIsolation(t *testing.T) {
	home, codexDoc := setupTerminalTestHome(t)
	guiSvc, termSvc := NewService("1.0.0"), NewTerminalService("1.0.0")
	defer guiSvc.Close()
	defer termSvc.Close()

	// GUI snapshot includes instructions and dispatches instruction_content; terminal omits/rejects them.
	guiSnap := callOK(t, guiSvc, "snapshot", nil)
	var instrID string
	for _, raw := range guiSnap["instructions"].([]any) {
		if m := raw.(map[string]any); m["path"] == codexDoc {
			instrID = m["id"].(string)
		}
	}
	if instrID == "" {
		t.Fatalf("GUI snapshot missing %s: %v", codexDoc, guiSnap["instructions"])
	}
	callOK(t, guiSvc, "instruction_content", map[string]any{"id": instrID})

	if _, ok := callOK(t, termSvc, "snapshot", nil)["instructions"]; ok {
		t.Fatal("terminal snapshot must omit instructions")
	}
	for _, m := range []string{"instruction_content", "save_instruction", "delete_instruction", "set_instruction_enabled"} {
		if errMsg := callErr(t, termSvc, m, map[string]any{"id": instrID, "content": "# x\n"}); !strings.Contains(errMsg, "unknown method") {
			t.Fatalf("%s error = %q, want unknown method", m, errMsg)
		}
	}

	// Any valid JSON shape for top-level instructions is ignored by terminal preview with original SHA-256.
	for _, tc := range []struct {
		name string
		val  any
	}{
		{"object", map[string]any{"codex": "# rule"}},
		{"array", []any{"a", map[string]any{"k": 1}}},
		{"string", "raw"},
		{"number", 42},
		{"bool", true},
		{"null", nil},
	} {
		p := filepath.Join(home, "shape-"+tc.name+".json")
		wantDigest := writeFixtureWith(t, p, func(d map[string]any) { d["instructions"] = tc.val })
		prev := callOK(t, termSvc, "preview_config", map[string]any{"path": p})
		if prev["digest"] != wantDigest || prev["mcpCount"] != float64(2) || prev["skillCount"] != float64(1) {
			t.Fatalf("%s preview = %v, want digest %s", tc.name, prev, wantDigest)
		}
		callErr(t, guiSvc, "preview_config", map[string]any{"path": p})
	}

	// Instruction-only byte change invalidates digest; fresh preview + import restores resources & safety backup.
	backupPath := filepath.Join(home, "ext-backup.json")
	digest1 := writeFixtureWith(t, backupPath, func(d map[string]any) { d["instructions"] = map[string]any{"codex": "v1"} })
	if prev := callOK(t, termSvc, "preview_config", map[string]any{"path": backupPath}); prev["digest"] != digest1 {
		t.Fatalf("preview1 digest = %v, want %s", prev["digest"], digest1)
	}
	digest2 := writeFixtureWith(t, backupPath, func(d map[string]any) { d["instructions"] = map[string]any{"codex": "v2"} })
	if errMsg := callErr(t, termSvc, "import_config", map[string]any{"path": backupPath, "digest": digest1}); !strings.Contains(errMsg, "changed") {
		t.Fatalf("expected digest mismatch error, got %q", errMsg)
	}
	callErr(t, guiSvc, "import_config", map[string]any{"path": backupPath, "digest": digest2})

	snap := callOK(t, termSvc, "import_config", map[string]any{"path": backupPath, "digest": digest2})["snapshot"].(map[string]any)
	if _, ok := snap["instructions"]; ok {
		t.Fatal("import_config snapshot must omit instructions")
	}
	if len(snap["mcp"].([]any)) != 2 || len(snap["skills"].([]any)) != 1 {
		t.Fatalf("unexpected restored counts: mcp=%d skills=%d", len(snap["mcp"].([]any)), len(snap["skills"].([]any)))
	}
	if proxy := callOK(t, termSvc, "proxy_settings", nil); proxy["mode"] != "custom" || proxy["url"] != "socks5h://127.0.0.1:1080" {
		t.Fatalf("restored proxy = %v", proxy)
	}
	if backups, err := filepath.Glob(filepath.Join(home, ".siu", "backups", "config-*.json")); err != nil || len(backups) != 1 {
		t.Fatalf("expected 1 safety backup, got %v (%v)", backups, err)
	}

	// Terminal v1 export omits instructions.
	exportPath := filepath.Join(home, "terminal-export.json")
	callOK(t, termSvc, "export_config", map[string]any{"path": exportPath})
	exportedRaw, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var exported map[string]any
	if err := json.Unmarshal(exportedRaw, &exported); err != nil {
		t.Fatal(err)
	}
	if exported["format"] != backupFormat || exported["version"] != float64(1) {
		t.Fatalf("unexpected export header: %v %v", exported["format"], exported["version"])
	}
	if _, ok := exported["instructions"]; ok {
		t.Fatal("terminal v1 export must omit instructions")
	}
	assertCodexDocIntact(t, codexDoc)
}

func TestTerminalServiceAutoRestoreReceiptOnceAndStrictGUI(t *testing.T) {
	home, codexDoc := setupTerminalTestHome(t)
	wantDigest := writeFixtureWith(t, filepath.Join(home, ".siu", "config.json"), func(d map[string]any) {
		d["instructions"] = map[string]any{"codex": "# ignored"}
	})

	// Strict GUI refuses default config.json with instructions and writes no receipt.
	guiSvc := NewService("1.0.0")
	defer guiSvc.Close()
	guiSnap := callOK(t, guiSvc, "snapshot", nil)
	if guiSnap["autoConfig"] != nil || len(guiSnap["issues"].([]any)) == 0 {
		t.Fatalf("expected GUI auto-restore rejection, got autoConfig=%v issues=%v", guiSnap["autoConfig"], guiSnap["issues"])
	}
	if _, err := os.Stat(filepath.Join(home, ".siu", "config.applied.json")); !os.IsNotExist(err) {
		t.Fatalf("GUI failure must not write config.applied.json, err=%v", err)
	}

	// Terminal auto-restores once with digest receipt, safety backup, and preserved AGENTS.md.
	termSvc := NewTerminalService("1.0.0")
	defer termSvc.Close()
	termSnap := callOK(t, termSvc, "snapshot", nil)
	if _, ok := termSnap["instructions"]; ok {
		t.Fatal("terminal snapshot must omit instructions")
	}
	if autoCfg := termSnap["autoConfig"].(map[string]any); autoCfg["digest"] != wantDigest {
		t.Fatalf("autoConfig = %v, want digest %s", autoCfg, wantDigest)
	}
	if len(termSnap["mcp"].([]any)) != 2 || len(termSnap["skills"].([]any)) != 1 {
		t.Fatalf("unexpected auto-restored counts")
	}
	if applied, err := ReadAppliedConfig(filepath.Join(home, ".siu")); err != nil || applied == nil || applied.Digest != wantDigest {
		t.Fatalf("config.applied.json = (%+v, %v), want %s", applied, err, wantDigest)
	}
	if backups, err := filepath.Glob(filepath.Join(home, ".siu", "backups", "config-*.json")); err != nil || len(backups) != 1 {
		t.Fatalf("expected 1 safety backup, got %v (%v)", backups, err)
	}
	assertCodexDocIntact(t, codexDoc)

	// Identical config.json does not re-apply over user edits.
	callOK(t, termSvc, "delete", map[string]any{"kind": "mcp", "name": "portable"})
	for _, raw := range callOK(t, termSvc, "snapshot", nil)["mcp"].([]any) {
		if raw.(map[string]any)["name"] == "portable" {
			t.Fatal("identical config.json re-applied and overwrote user edit")
		}
	}
}

func TestTerminalServiceStrictValidationAndSkillRollback(t *testing.T) {
	home, _ := setupTerminalTestHome(t)
	termSvc := NewTerminalService("1.0.0")
	defer termSvc.Close()

	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"top-level-extra", func(d map[string]any) { d["instructions"] = map[string]any{}; d["extra"] = 1 }},
		{"nested-proxy-instr", func(d map[string]any) { d["proxy"].(map[string]any)["instructions"] = map[string]any{} }},
		{"invalid-mcp", func(d map[string]any) { d["mcp"].([]any)[0].(map[string]any)["command"] = "" }},
		{"invalid-skill", func(d map[string]any) { d["skills"].([]any)[0].(map[string]any)["files"] = []any{} }},
		{"invalid-proxy", func(d map[string]any) { d["proxy"].(map[string]any)["url"] = "ftp://127.0.0.1:21" }},
	} {
		p := filepath.Join(home, "bad-"+tc.name+".json")
		digest := writeFixtureWith(t, p, tc.mutate)
		callErr(t, termSvc, "preview_config", map[string]any{"path": p})
		callErr(t, termSvc, "import_config", map[string]any{"path": p, "digest": digest})
	}

	rawValid, err := os.ReadFile(filepath.Join("testdata", "siu-backup-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	badJSONPath := filepath.Join(home, "bad-json.json")
	if err := os.WriteFile(badJSONPath, []byte(strings.Replace(string(rawValid), `"format": "siu-gui-backup",`, `"format": "siu-gui-backup", "instructions": {bad},`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	callErr(t, termSvc, "preview_config", map[string]any{"path": badJSONPath})

	// Seed modified "portable" skill, then swap it and hit a file blocker on a second skill to trigger rollback.
	srcDir := filepath.Join(home, "local-skills", "portable")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: portable\ndescription: Prior\n---\nprevious-portable-content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tok := callOK(t, termSvc, "scan_skills", map[string]any{"source": srcDir})["token"].(string)
	callOK(t, termSvc, "install_skills", map[string]any{"token": tok, "paths": []string{""}, "agents": []string{"claude"}})
	callOK(t, termSvc, "release_scan", map[string]any{"token": tok})

	rollbackPath := filepath.Join(home, "rollback.json")
	digest := writeFixtureWith(t, rollbackPath, func(d map[string]any) {
		d["instructions"] = map[string]any{"codex": "# ignored"}
		first := d["skills"].([]any)[0].(map[string]any)
		secondEntry := map[string]any{}
		for k, v := range first["entry"].(map[string]any) {
			secondEntry[k] = v
		}
		secondEntry["name"] = "blocked-skill"
		d["skills"] = []any{first, map[string]any{"entry": secondEntry, "files": first["files"]}}
	})
	if err := os.WriteFile(filepath.Join(home, ".siu", "skills", "blocked-skill"), []byte("blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if errMsg := callErr(t, termSvc, "import_config", map[string]any{"path": rollbackPath, "digest": digest}); !strings.Contains(errMsg, "undone") {
		t.Fatalf("expected rollback message, got %q", errMsg)
	}
	if content := callOK(t, termSvc, "skill_content", map[string]any{"name": "portable"})["content"].(string); !strings.Contains(content, "previous-portable-content") {
		t.Fatalf("prior portable skill not restored: %q", content)
	}
	if skills := callOK(t, termSvc, "snapshot", nil)["skills"].([]any); len(skills) != 1 || skills[0].(map[string]any)["name"] != "portable" {
		t.Fatalf("unexpected skills after rollback: %v", skills)
	}
}
