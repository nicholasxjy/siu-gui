package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func callService(t *testing.T, svc *Service, method string, params map[string]any) map[string]any {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	payload, err := json.Marshal(map[string]any{
		"id":     1,
		"method": method,
		"params": params,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := svc.Request(context.Background(), string(payload))
	if err != nil {
		t.Fatalf("%s transport error: %v", method, err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("%s invalid JSON %q: %v", method, raw, err)
	}
	return resp
}

func callOK(t *testing.T, svc *Service, method string, params map[string]any) map[string]any {
	t.Helper()
	resp := callService(t, svc, method, params)
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("%s failed: %v", method, resp["error"])
	}
	data, _ := resp["data"].(map[string]any)
	return data
}

func callErr(t *testing.T, svc *Service, method string, params map[string]any) string {
	t.Helper()
	resp := callService(t, svc, method, params)
	if ok, _ := resp["ok"].(bool); ok {
		t.Fatalf("%s unexpectedly succeeded: %v", method, resp["data"])
	}
	errMsg, _ := resp["error"].(string)
	return errMsg
}

func TestServiceFullMCPSkillsAndConfigLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv(TestHomeVar, home)
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	// Seed an unmanaged MCP server in Claude
	_ = os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers":{"mine":{"type":"stdio","command":"me","args":[],"env":{}}}}`), 0o644)

	svc := NewService("0.0.6")
	defer svc.Close()

	// 1. Fresh snapshot sees unmanaged server
	snap := callOK(t, svc, "snapshot", nil)
	if snap["version"] != "0.0.6" {
		t.Fatalf("version = %v", snap["version"])
	}
	foundMCP := snap["foundMcp"].([]any)
	if len(foundMCP) != 1 {
		t.Fatalf("foundMcp len = %d, want 1", len(foundMCP))
	}

	// 2. Collision guard refuses saving a server with the same name as an unmanaged one
	errMsg := callErr(t, svc, "save_server", map[string]any{
		"server": map[string]any{
			"name": "mine", "transport": "stdio", "command": "other",
			"args": []string{}, "env": map[string]string{}, "url": "", "headers": map[string]string{},
		},
		"agents": []string{"claude"},
	})
	if !strings.Contains(errMsg, "import it explicitly first") {
		t.Fatalf("expected collision error, got %q", errMsg)
	}

	// 3. Explicit import takes ownership
	imp := callOK(t, svc, "import", map[string]any{"kind": "mcp", "name": "mine"})
	mcpEntries := imp["snapshot"].(map[string]any)["mcp"].([]any)
	if len(mcpEntries) != 1 {
		t.Fatalf("expected 1 managed MCP after import")
	}

	// 4. Add, pause, resume, rename, and delete a server
	callOK(t, svc, "save_server", map[string]any{
		"server": map[string]any{
			"name": "ctx7", "transport": "http", "command": "",
			"args": []string{}, "env": map[string]string{},
			"url": "https://mcp.context7.com/mcp", "headers": map[string]string{},
		},
		"agents": []string{"claude", "codex"},
	})
	paused := callOK(t, svc, "set_enabled", map[string]any{"kind": "mcp", "name": "ctx7", "enabled": false})
	for _, raw := range paused["snapshot"].(map[string]any)["mcp"].([]any) {
		m := raw.(map[string]any)
		if m["name"] == "ctx7" {
			states := m["states"].(map[string]any)
			if states["claude"] != "paused" || states["codex"] != "paused" {
				t.Fatalf("expected paused states, got %v", states)
			}
		}
	}
	callOK(t, svc, "set_enabled", map[string]any{"kind": "mcp", "name": "ctx7", "enabled": true})
	callOK(t, svc, "save_server", map[string]any{
		"oldName": "ctx7",
		"server": map[string]any{
			"name": "ctx7-renamed", "transport": "http", "command": "",
			"args": []string{}, "env": map[string]string{},
			"url": "https://mcp.context7.com/mcp", "headers": map[string]string{},
		},
		"agents": []string{"claude"},
	})
	callOK(t, svc, "set_agents", map[string]any{"kind": "mcp", "name": "ctx7-renamed", "agents": []string{"claude", "codex"}})

	// 5. Discover featured MCP and install_server
	disc := callOK(t, svc, "discover", map[string]any{"kind": "mcp", "query": ""})
	if len(disc["servers"].([]any)) < 20 {
		t.Fatalf("expected featured servers in discover")
	}
	callOK(t, svc, "install_server", map[string]any{
		"id":     "deepwiki",
		"values": map[string]string{},
		"agents": []string{"claude"},
	})

	// 6. Scan, install, read content, update, and release a local skill
	srcDir := filepath.Join(home, "local-skills", "pdf")
	_ = os.MkdirAll(srcDir, 0o755)
	_ = os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: pdf\ndescription: PDF helper\n---\nv1\n"), 0o644)

	scan := callOK(t, svc, "scan_skills", map[string]any{"source": srcDir})
	tok := scan["token"].(string)
	callOK(t, svc, "install_skills", map[string]any{
		"token":  tok,
		"paths":  []string{""},
		"agents": []string{"claude", "codex"},
	})
	content := callOK(t, svc, "skill_content", map[string]any{"name": "pdf"})
	if !strings.Contains(content["content"].(string), "v1") {
		t.Fatalf("unexpected skill_content: %v", content)
	}
	_ = os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: pdf\ndescription: PDF helper\n---\nv2\n"), 0o644)
	callOK(t, svc, "update", map[string]any{"kind": "skills", "name": "pdf"})
	content2 := callOK(t, svc, "skill_content", map[string]any{"name": "pdf"})
	if !strings.Contains(content2["content"].(string), "v2") {
		t.Fatalf("expected updated content v2, got %v", content2)
	}
	callOK(t, svc, "release_scan", map[string]any{"token": tok})
	if errMsg := callErr(t, svc, "install_skills", map[string]any{"token": tok, "paths": []string{""}, "agents": []string{"claude"}}); !strings.Contains(errMsg, "expired") {
		t.Fatalf("expected expired scan error, got %q", errMsg)
	}

	// 7. Proxy settings round-trip without leaking secrets in errors
	customProxy := map[string]any{"mode": "custom", "url": "socks5h://127.0.0.1:1080", "noProxy": "localhost"}
	callOK(t, svc, "set_proxy_settings", map[string]any{"proxy": customProxy})
	badErr := callErr(t, svc, "set_proxy_settings", map[string]any{
		"proxy": map[string]any{"mode": "custom", "url": "ftp://user:supersecret@localhost/p", "noProxy": ""},
	})
	if strings.Contains(badErr, "supersecret") {
		t.Fatalf("error leaked secret: %q", badErr)
	}

	// 8. Export, clear, preview, digest mismatch rejection, and restore
	exportPath := filepath.Join(home, "backup.json")
	callOK(t, svc, "export_config", map[string]any{"path": exportPath, "theme": "dark"})
	callOK(t, svc, "clear", map[string]any{"kind": "mcp"})
	callOK(t, svc, "clear", map[string]any{"kind": "skills"})

	prev := callOK(t, svc, "preview_config", map[string]any{"path": exportPath})
	digest := prev["digest"].(string)
	if prev["theme"] != "dark" {
		t.Fatalf("preview theme = %v, want dark", prev["theme"])
	}
	if errMsg := callErr(t, svc, "import_config", map[string]any{"path": exportPath, "digest": "wrong"}); !strings.Contains(errMsg, "changed") {
		t.Fatalf("expected digest mismatch error, got %q", errMsg)
	}
	restored := callOK(t, svc, "import_config", map[string]any{"path": exportPath, "digest": digest})
	if restored["theme"] != "dark" {
		t.Fatalf("restored theme = %v, want dark", restored["theme"])
	}
	if len(restored["snapshot"].(map[string]any)["skills"].([]any)) != 1 {
		t.Fatalf("expected restored skill")
	}

	// 9. Auto-restore via root/config.json runs once per content digest
	defaultCfg := filepath.Join(home, ".siu", "config.json")
	rawBackup, _ := os.ReadFile(exportPath)
	_ = os.WriteFile(defaultCfg, rawBackup, 0o600)
	snapAuto := callOK(t, svc, "snapshot", nil)
	autoCfg := snapAuto["autoConfig"].(map[string]any)
	if autoCfg["digest"] != digest || autoCfg["theme"] != "dark" {
		t.Fatalf("autoConfig = %v", autoCfg)
	}
	// Delete a server and snapshot again: identical config.json must NOT re-apply
	callOK(t, svc, "delete", map[string]any{"kind": "mcp", "name": "deepwiki"})
	snapAfter := callOK(t, svc, "snapshot", nil)
	for _, raw := range snapAfter["mcp"].([]any) {
		if raw.(map[string]any)["name"] == "deepwiki" {
			t.Fatal("identical config.json re-applied and overwrote user edit")
		}
	}
}

func TestServiceConcurrentRequestsAndMalformedConfigIsolation(t *testing.T) {
	home := t.TempDir()
	t.Setenv(TestHomeVar, home)
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	// Malformed codex TOML containing a secret must surface an issue without leaking the secret
	_ = os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("API_KEY = \"secret-value-never-echo"), 0o644)

	svc := NewService("0.0.6")
	defer svc.Close()

	snap := callOK(t, svc, "snapshot", nil)
	issues := snap["issues"].([]any)
	if len(issues) == 0 {
		t.Fatal("expected issue for malformed codex config")
	}
	for _, iss := range issues {
		if strings.Contains(iss.(string), "secret-value-never-echo") {
			t.Fatalf("issue leaked config contents: %v", iss)
		}
	}

	// Concurrent writes must serialize cleanly under -race
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := "srv-" + string(rune('a'+idx))
			callOK(t, svc, "save_server", map[string]any{
				"server": map[string]any{
					"name": name, "transport": "stdio", "command": "node",
					"args": []string{"x.js"}, "env": map[string]string{},
					"url": "", "headers": map[string]string{},
				},
				"agents": []string{"claude"},
			})
		}(i)
	}
	wg.Wait()
	finalSnap := callOK(t, svc, "snapshot", nil)
	if len(finalSnap["mcp"].([]any)) != 6 {
		t.Fatalf("expected 6 servers after concurrent saves, got %d", len(finalSnap["mcp"].([]any)))
	}
}
