package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/nicholasxjy/siu-project/internal/core"
)

type tuiHarness struct {
	t       *testing.T
	home    string
	service *core.Service
	ui      *tui
	sim     tcell.SimulationScreen
	cancel  context.CancelFunc
	runErr  chan error

	mu         sync.Mutex
	cond       *sync.Cond
	drawSeq    uint64
	screenText string
}

func newTUIHarness(t *testing.T, setupHome func(home string)) *tuiHarness {
	t.Helper()
	home := t.TempDir()
	t.Setenv(core.TestHomeVar, home)
	if setupHome != nil {
		setupHome(home)
	}

	service := core.NewTerminalService("test")
	ctx, cancel := context.WithCancel(context.Background())

	sim := tcell.NewSimulationScreen("UTF-8")
	ui := newTUI(ctx, service)
	ui.app.SetScreen(sim)
	sim.SetSize(120, 40)

	h := &tuiHarness{
		t:       t,
		home:    home,
		service: service,
		ui:      ui,
		sim:     sim,
		cancel:  cancel,
		runErr:  make(chan error, 1),
	}
	h.cond = sync.NewCond(&h.mu)

	ui.app.SetAfterDrawFunc(func(screen tcell.Screen) {
		text := captureScreenText(screen)
		h.mu.Lock()
		h.screenText = text
		h.drawSeq++
		h.cond.Broadcast()
		h.mu.Unlock()
	})

	ui.refresh()
	go func() {
		h.runErr <- ui.app.Run()
	}()

	t.Cleanup(func() {
		cancel()
		ui.app.Stop()
		select {
		case err := <-h.runErr:
			if err != nil {
				t.Errorf("ui.app.Run() returned error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("timed out waiting for tui app to stop")
		}
		_ = service.Close()
	})

	return h
}

func captureScreenText(screen tcell.Screen) string {
	w, h := screen.Size()
	var b strings.Builder
	for y := 0; y < h; y++ {
		var line strings.Builder
		for x := 0; x < w; x++ {
			r, _, _, width := screen.GetContent(x, y)
			if r == 0 {
				r = ' '
			}
			line.WriteRune(r)
			if width > 1 {
				x += width - 1
			}
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		if y < h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (h *tuiHarness) currentDraw() (uint64, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.drawSeq, h.screenText
}

func (h *tuiHarness) waitDrawAfter(seq uint64, timeout time.Duration) string {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()

	deadline := time.Now().Add(timeout)
	timer := time.AfterFunc(timeout, func() {
		h.mu.Lock()
		h.cond.Broadcast()
		h.mu.Unlock()
	})
	defer timer.Stop()

	for h.drawSeq <= seq {
		if time.Now().After(deadline) {
			return h.screenText
		}
		h.cond.Wait()
	}
	return h.screenText
}

func (h *tuiHarness) isIdle() bool {
	idle := false
	h.ui.app.QueueUpdate(func() {
		idle = h.ui.cancel == nil
	})
	return idle
}

func (h *tuiHarness) waitScreen(substrs ...string) string {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		seq, text := h.currentDraw()
		matched := seq > 0
		for _, sub := range substrs {
			if !strings.Contains(text, sub) {
				matched = false
				break
			}
		}
		if matched && h.isIdle() {
			// Re-read after QueueUpdate barrier to ensure no intervening draw changed state.
			_, latest := h.currentDraw()
			stillMatched := true
			for _, sub := range substrs {
				if !strings.Contains(latest, sub) {
					stillMatched = false
					break
				}
			}
			if stillMatched {
				return latest
			}
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for screen containing %q (idle=%v); last screen:\n%s", substrs, h.isIdle(), text)
		}
		h.waitDrawAfter(seq, time.Until(deadline))
	}
}

func (h *tuiHarness) sendKey(key tcell.Key, r rune, mod tcell.ModMask) {
	h.t.Helper()
	seq, _ := h.currentDraw()
	h.sim.InjectKey(key, r, mod)
	h.waitDrawAfter(seq, 5*time.Second)
	if after, text := h.currentDraw(); after <= seq {
		h.t.Fatalf("timed out waiting for draw after key (%v, %q) at seq %d; last screen:\n%s", key, r, seq, text)
	}
}

func (h *tuiHarness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		h.sendKey(tcell.KeyRune, r, tcell.ModNone)
	}
}

func (h *tuiHarness) stopWithKey(key tcell.Key, r rune) {
	h.t.Helper()
	h.sim.InjectKey(key, r, tcell.ModNone)
	select {
	case err := <-h.runErr:
		if err != nil {
			h.t.Fatalf("ui.app.Run() returned error: %v", err)
		}
		// Refill channel with nil so t.Cleanup's receive succeeds.
		h.runErr <- nil
	case <-time.After(5 * time.Second):
		_, text := h.currentDraw()
		h.t.Fatalf("timed out waiting for app exit after key (%v, %q); last screen:\n%s", key, r, text)
	}
}

func coreSnapshot(t *testing.T, home string) core.SnapshotDTO {
	t.Helper()
	t.Setenv(core.TestHomeVar, home)
	svc := core.NewTerminalService("test")
	defer svc.Close()
	snap, err := coreRequest[core.SnapshotDTO](context.Background(), svc, "snapshot", nil)
	if err != nil {
		t.Fatalf("core snapshot failed: %v", err)
	}
	return snap
}

func TestTUIFullWorkflowAndConfirmationSafety(t *testing.T) {
	workDir := t.TempDir()
	localSkills := filepath.Join(workDir, "local-skills")
	for name, desc := range map[string]string{
		"skill-alpha": "Alpha skill description",
		"skill-beta":  "Beta skill description",
	} {
		dir := filepath.Join(localSkills, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	h := newTUIHarness(t, func(home string) {
		for _, dir := range []string{".claude", ".codex"} {
			if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	})

	// 1. Startup on MCP tab.
	h.waitScreen("[ MCP ]", "MCP   0 已纳管 · 0 未纳管", "+ 添加 MCP 服务")

	// Open "+ 添加 MCP 服务" -> choose "stdio".
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("添加 MCP 服务 · 连接方式", "stdio", "http", "sse")
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("添加 MCP 服务 · stdio", "服务名称", "选择 Agent")

	// Edge case: submitting empty server form surfaces validation error and stays on form.
	for i := 0; i < 5; i++ {
		h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	}
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("添加 MCP 服务 · stdio", "操作失败")

	// Move back to 服务名称 and fill valid stdio MCP with an env secret.
	for i := 0; i < 5; i++ {
		h.sendKey(tcell.KeyBacktab, 0, tcell.ModNone)
	}
	h.typeText("alpha-mcp")
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.typeText("npx")
	// Skip args and type directly in the empty environment field.
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.typeText(`{"API_KEY":"env-secret-456"}`)
	// Advance past 显示敏感字段 to "选择 Agent".
	for i := 0; i < 2; i++ {
		h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	}
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)

	// Target selection: both Claude Code and Codex are preselected; deselect Codex.
	screen := h.waitScreen("分配给 Agent", "[x] Claude Code", "[x] Codex")
	if strings.Contains(screen, "~/.agents") {
		t.Fatalf("MCP target list unexpectedly showed ~/.agents:\n%s", screen)
	}
	h.sendKey(tcell.KeyDown, 0, tcell.ModNone)
	h.sendKey(tcell.KeyRune, ' ', tcell.ModNone)
	h.waitScreen("[x] Claude Code", "[ ] Codex")
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)

	// Verify MCP persisted in UI, agent config, and a fresh TerminalService snapshot.
	h.waitScreen("MCP   1 已纳管 · 0 未纳管", "alpha-mcp", "1 个 Agent  |  STDIO")
	// Verify MCP preview masks env secret values.
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("alpha-mcp", "查看配置")
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	preview := h.waitScreen("alpha-mcp · MCP 配置", "API_KEY", "••••••••")
	if strings.Contains(preview, "env-secret-456") {
		t.Fatalf("MCP preview leaked env secret:\n%s", preview)
	}
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("alpha-mcp", "查看配置")
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("[ MCP ]", "alpha-mcp")
	claudeCfg, err := os.ReadFile(filepath.Join(h.home, ".claude.json"))
	if err != nil {
		t.Fatalf("expected .claude.json to be written: %v", err)
	}
	if !strings.Contains(string(claudeCfg), "alpha-mcp") {
		t.Fatalf(".claude.json missing alpha-mcp: %s", claudeCfg)
	}
	if _, err := os.Stat(filepath.Join(h.home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("deselected Codex config should not exist, stat err=%v", err)
	}
	snap := coreSnapshot(t, h.home)
	if len(snap.MCP) != 1 || snap.MCP[0].Name != "alpha-mcp" || len(snap.MCP[0].Agents) != 1 || snap.MCP[0].Agents[0] != "claude" {
		t.Fatalf("unexpected persisted MCP snapshot: %+v", snap.MCP)
	}

	// 2. Navigate tabs (Tab / Shift+Tab / Tab) to Skills and install a local Skill.
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.waitScreen("[ Skills ]", "Skills   0 已纳管 · 0 未纳管", "+ 添加 Skills")
	h.sendKey(tcell.KeyBacktab, 0, tcell.ModNone)
	h.waitScreen("[ MCP ]", "alpha-mcp")
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.waitScreen("[ Skills ]", "+ 添加 Skills")

	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("添加 Skills", "GitHub 或本地路径", "扫描来源")
	h.typeText(localSkills)
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)

	// Edge case: uncheck both scanned skills and verify empty selection is rejected.
	h.waitScreen("选择要安装的 Skills · 0 已安装", "[x] skill-alpha", "[x] skill-beta")
	h.sendKey(tcell.KeyRune, ' ', tcell.ModNone)
	h.sendKey(tcell.KeyDown, 0, tcell.ModNone)
	h.sendKey(tcell.KeyRune, ' ', tcell.ModNone)
	h.waitScreen("[ ] skill-alpha", "[ ] skill-beta")
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("操作失败 · 请至少选择一个尚未安装的 Skill")

	// Select only skill-alpha (currently on row 1 skill-beta, move up to row 0).
	h.sendKey(tcell.KeyUp, 0, tcell.ModNone)
	h.sendKey(tcell.KeyRune, ' ', tcell.ModNone)
	h.waitScreen("[x] skill-alpha", "[ ] skill-beta")
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)

	// Target selection for Skills includes ~/.agents (shared) unselected by default.
	h.waitScreen("分配给 Agent", "[x] Claude Code", "[x] Codex", "[ ] ~/.agents (shared)")
	// Deselect Codex so only Claude Code receives skill-alpha.
	h.sendKey(tcell.KeyDown, 0, tcell.ModNone)
	h.sendKey(tcell.KeyRune, ' ', tcell.ModNone)
	h.waitScreen("[x] Claude Code", "[ ] Codex", "[ ] ~/.agents (shared)")
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)

	h.waitScreen("Skills   1 已纳管 · 0 未纳管", "skill-alpha", "1 个 Agent  |  Alpha skill description")
	if _, err := os.Stat(filepath.Join(h.home, ".claude", "skills", "skill-alpha", "SKILL.md")); err != nil {
		t.Fatalf("expected skill-alpha in .claude/skills: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.home, ".codex", "skills", "skill-alpha")); !os.IsNotExist(err) {
		t.Fatalf("deselected Codex should not have skill-alpha, stat err=%v", err)
	}

	// Open skill-alpha -> 查看 SKILL.md -> Esc back to home.
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("skill-alpha", "查看 SKILL.md")
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("skill-alpha · SKILL.md", "Alpha skill description")
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("skill-alpha", "查看 SKILL.md")
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("[ Skills ]", "skill-alpha")

	// 3. Navigate to Settings -> Backup Export + Confirmation Safety + Preview/Cancel/Restore.
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.waitScreen("[ 设置 ]", "重试写入配置", "导出备份", "预览与导入备份", "清空 MCP", "清空 Skills")

	// Move down to "导出备份" (index 2).
	h.sendKey(tcell.KeyDown, 0, tcell.ModNone)
	h.sendKey(tcell.KeyDown, 0, tcell.ModNone)
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("导出备份", "JSON 文件路径", "继续")

	backupPath := filepath.Join(workDir, "siu-export.json")
	h.typeText(backupPath)
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)

	// Confirmation safety: Esc must cancel export without writing file.
	h.waitScreen("导出备份", "备份包含明文凭据", "Y 确认", "Esc 取消")
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("导出备份", "JSON 文件路径", "继续")
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("cancelled export must not write %s, err=%v", backupPath, err)
	}

	// Confirm export with 'Y'.
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("导出备份", "备份包含明文凭据", "Y 确认")
	h.sendKey(tcell.KeyRune, 'y', tcell.ModNone)
	h.waitScreen("已导出备份", "MCP：1", "Skills：1")

	rawBackup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("expected backup file at %s: %v", backupPath, err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(rawBackup, &parsed); err != nil {
		t.Fatalf("invalid backup JSON: %v", err)
	}
	if _, hasInstr := parsed["instructions"]; hasInstr {
		t.Fatalf("terminal backup must omit instructions: %s", rawBackup)
	}

	// Return from "已导出备份" to Settings tab.
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("[ 设置 ]", "导出备份", "清空 MCP")

	// Confirmation safety on "清空 MCP" (row index 4): Esc cancels, then Y clears.
	for i := 0; i < 4; i++ {
		h.sendKey(tcell.KeyDown, 0, tcell.ModNone)
	}
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("清空 MCP", "将删除 1 个已纳管的 MCP", "Y 确认")
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("[ 设置 ]", "清空 MCP")
	if snap := coreSnapshot(t, h.home); len(snap.MCP) != 1 {
		t.Fatalf("cancelled clear must preserve MCP, got %+v", snap.MCP)
	}

	// Now confirm "清空 MCP".
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("清空 MCP", "将删除 1 个已纳管的 MCP", "Y 确认")
	h.sendKey(tcell.KeyRune, 'y', tcell.ModNone)
	screen = h.waitScreen("[ 设置 ]", "清空 Skills")
	if strings.Contains(screen, "清空 MCP") {
		t.Fatalf("清空 MCP row should disappear when 0 MCPs remain:\n%s", screen)
	}
	if snap := coreSnapshot(t, h.home); len(snap.MCP) != 0 {
		t.Fatalf("expected 0 MCPs after clear, got %+v", snap.MCP)
	}

	// Preview & Cancel, then Restore backup.
	// Move down to "预览与导入备份" (index 3).
	for i := 0; i < 3; i++ {
		h.sendKey(tcell.KeyDown, 0, tcell.ModNone)
	}
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("预览与导入备份", "JSON 文件路径", "继续")
	h.typeText(backupPath)
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)

	// Cancel restore confirmation with Esc.
	h.waitScreen("恢复备份", "MCP：1", "Skills：1", "恢复前会自动备份当前配置", "Y 确认")
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("预览与导入备份", "JSON 文件路径")
	if snap := coreSnapshot(t, h.home); len(snap.MCP) != 0 {
		t.Fatalf("cancelled restore must not restore MCP, got %+v", snap.MCP)
	}

	// Re-submit and confirm restore with 'Y'; import_config shows "操作结果" with safety backup log.
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("恢复备份", "MCP：1", "Skills：1", "Y 确认")
	h.sendKey(tcell.KeyRune, 'y', tcell.ModNone)
	h.waitScreen("操作结果", "配置已恢复", "恢复前的配置已备份到")
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("[ 设置 ]", "清空 MCP", "清空 Skills")

	// Verify restored state in fresh service, agent config, and safety backup.
	restored := coreSnapshot(t, h.home)
	if len(restored.MCP) != 1 || restored.MCP[0].Name != "alpha-mcp" || len(restored.Skills) != 1 || restored.Skills[0].Name != "skill-alpha" {
		t.Fatalf("unexpected restored snapshot: mcp=%+v skills=%+v", restored.MCP, restored.Skills)
	}
	claudeCfg, err = os.ReadFile(filepath.Join(h.home, ".claude.json"))
	if err != nil || !strings.Contains(string(claudeCfg), "alpha-mcp") {
		t.Fatalf("restored .claude.json missing alpha-mcp: %q, err=%v", claudeCfg, err)
	}
	if backups, err := filepath.Glob(filepath.Join(h.home, ".siu", "backups", "config-*.json")); err != nil || len(backups) == 0 {
		t.Fatalf("expected safety backup before restore, got %v (err=%v)", backups, err)
	}

	// Quit via 'q'.
	h.stopWithKey(tcell.KeyRune, 'q')
}

func TestTUITargetEdgeCases(t *testing.T) {
	h := newTUIHarness(t, func(home string) {
		for _, dir := range []string{".claude", ".codex"} {
			if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	})

	h.waitScreen("[ MCP ]", "+ 添加 MCP 服务")

	// Add an SSE MCP via 'A' shortcut: Codex does not support SSE, so only Claude Code should appear in targets.
	h.sendKey(tcell.KeyRune, 'a', tcell.ModNone)
	h.waitScreen("添加 MCP 服务 · 连接方式", "stdio", "http", "sse")
	h.sendKey(tcell.KeyDown, 0, tcell.ModNone)
	h.sendKey(tcell.KeyDown, 0, tcell.ModNone)
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("添加 MCP 服务 · sse", "服务名称", "服务地址", "选择 Agent")

	h.typeText("sse-mcp")
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.typeText("https://user:pass@example.com/sse?token=url-secret-789")
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.typeText(`{"Authorization":"Bearer head-secret-123"}`)
	for i := 0; i < 2; i++ {
		h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	}
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)

	screen := h.waitScreen("分配给 Agent", "[x] Claude Code")
	if strings.Contains(screen, "Codex") {
		t.Fatalf("Codex should be excluded for SSE transport:\n%s", screen)
	}

	// Deselect Claude Code too -> save in library with 0 agents.
	h.sendKey(tcell.KeyRune, ' ', tcell.ModNone)
	h.waitScreen("[ ] Claude Code")
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("MCP   1 已纳管 · 0 未纳管", "sse-mcp", "0 个 Agent  |  SSE")

	// Verify preview hides URL userinfo/query token and Authorization header value.
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("sse-mcp", "查看配置")
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	preview := h.waitScreen("sse-mcp · MCP 配置", "https://example.com/sse", "Authorization", "••••••••")
	if strings.Contains(preview, "url-secret-789") || strings.Contains(preview, "head-secret-123") || strings.Contains(preview, "pass@") {
		t.Fatalf("SSE MCP preview leaked sensitive credentials:\n%s", preview)
	}
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("sse-mcp", "查看配置")
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("[ MCP ]", "sse-mcp")

	// Verify '/' filter shortcut filters rows and can be cleared.
	h.sendKey(tcell.KeyRune, '/', tcell.ModNone)
	h.waitScreen("筛选 MCP", "筛选词", "应用筛选", "清除筛选")
	h.typeText("nonexistent")
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	filtered := h.waitScreen("MCP   1 已纳管 · 0 未纳管 · 匹配 0 项：nonexistent")
	if strings.Contains(filtered, "sse-mcp") {
		t.Fatalf("filter should hide sse-mcp:\n%s", filtered)
	}
	h.sendKey(tcell.KeyRune, '/', tcell.ModNone)
	h.waitScreen("筛选 MCP", "清除筛选")
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.sendKey(tcell.KeyTab, 0, tcell.ModNone)
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("MCP   1 已纳管 · 0 未纳管", "sse-mcp")

	// Simulate an existing assignment to a now-missing agent: assign claude first, then remove .claude.
	if _, err := coreRequest[core.MutationResultDTO](context.Background(), h.service, "set_agents", params{
		"kind":   "mcp",
		"name":   "sse-mcp",
		"agents": []string{"claude"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(h.home, ".claude")); err != nil {
		t.Fatal(err)
	}
	h.sendKey(tcell.KeyRune, 'r', tcell.ModNone)
	h.waitScreen("sse-mcp", "1 个 Agent  |  SSE")
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("sse-mcp", "分配给 Agent")
	for i := 0; i < 3; i++ {
		h.sendKey(tcell.KeyDown, 0, tcell.ModNone)
	}
	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("分配给 Agent", "没有可选项", "当前有未检测到或不兼容的分配，继续保存将移除这些分配。")

	h.sendKey(tcell.KeyEnter, 0, tcell.ModNone)
	h.waitScreen("操作结果", "Updated agents")
	h.sendKey(tcell.KeyEsc, 0, tcell.ModNone)
	h.waitScreen("MCP   1 已纳管 · 0 未纳管", "0 个 Agent  |  SSE")
	if snap := coreSnapshot(t, h.home); len(snap.MCP) != 1 || len(snap.MCP[0].Agents) != 0 {
		t.Fatalf("expected incompatible assignment to be removed on save, got %+v", snap.MCP)
	}

	h.stopWithKey(tcell.KeyRune, 'q')
}
