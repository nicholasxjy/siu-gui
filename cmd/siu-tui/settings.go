package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nicholasxjy/siu-gui/internal/core"
	"github.com/rivo/tview"
)

func (u *tui) settings(list *tview.List) {
	addRow(list, "重试写入配置", "把当前资源与分配重新同步到 Agent", func() {
		u.mutate("正在同步…", "sync", nil, "")
	})
	addRow(list, "网络代理", "系统代理、自定义代理或直连", func() {
		request[core.ProxySettings](u, "正在读取代理设置…", "proxy_settings", nil, func(proxy core.ProxySettings) {
			u.form("网络代理", proxyForm(proxy, func(next core.ProxySettings) {
				request[core.ProxySettings](u, "正在保存代理设置…", "set_proxy_settings", params{"proxy": next}, func(core.ProxySettings) {
					u.home("")
					u.notice("已保存代理设置")
				})
			}))
		})
	})
	addRow(list, "导出备份", "将 MCP、Skills、分配与代理导出为 JSON", func() {
		u.backupPath("导出备份", func(path string) {
			u.confirm("导出备份", "路径："+path+"\n\n备份包含明文凭据。此路径如已存在，将被覆盖。", func() {
				request[backupResult](u, "正在导出…", "export_config", params{"path": path}, func(result backupResult) {
					u.home("")
					u.read("已导出备份", result.summary())
				})
			})
		})
	})
	addRow(list, "预览与导入备份", "从 JSON 恢复资源与代理设置", func() { u.backupPath("预览与导入备份", u.previewBackup) })
	for _, item := range []struct {
		kind, label string
		count       int
	}{
		{"mcp", "MCP", len(u.snap.MCP)}, {"skills", "Skills", len(u.snap.Skills)},
	} {
		if item.count == 0 {
			continue
		}
		addRow(list, "清空 "+item.label, fmt.Sprintf("删除 %d 个已纳管资源", item.count), func() {
			u.confirm("清空 "+item.label, fmt.Sprintf("将删除 %d 个已纳管的 %s，并撤回对应的 Agent 配置。", item.count, item.label), func() {
				u.mutate("正在清空…", "clear", params{"kind": item.kind}, "")
			})
		})
	}
	addRow(list, "本机 Agent 与资源库", u.snap.Root, u.localDetails)
	if len(u.snap.Issues) > 0 {
		addRow(list, "配置提示", fmt.Sprintf("%d 条提示", len(u.snap.Issues)), func() {
			u.read("配置提示", strings.Join(u.snap.Issues, "\n\n"))
		})
	}
}

func (u *tui) localDetails() {
	lines := []string{"位置：" + u.snap.Root, "版本：" + u.snap.Version, "平台：" + u.snap.Platform}
	for _, agent := range u.snap.Agents {
		state := "未检测到"
		if agent.Detected {
			state = "已检测到"
		}
		lines = append(lines, "", agent.Name+" · "+state)
		if agent.MCPPath != nil {
			lines = append(lines, "  MCP     "+*agent.MCPPath)
		}
		if agent.SkillsPath != nil {
			lines = append(lines, "  Skills  "+*agent.SkillsPath)
		}
	}
	u.read("本机 Agent 与资源库", strings.Join(lines, "\n"))
}

func (u *tui) backupPath(title string, submit func(string)) {
	form := newForm()
	field := textField(form, "JSON 文件路径", "", "./siu-backup.json", false)
	form.AddButton("继续", func() {
		path := strings.TrimSpace(field.GetText())
		if path == "" {
			u.fail(errors.New("请输入备份文件路径"))
			return
		}
		if len(path) >= 2 && path[0] == '~' && (path[1] == '/' || path[1] == filepath.Separator) {
			path = filepath.Join(u.snap.Home, path[2:])
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			u.fail(err)
			return
		}
		submit(absolute)
	})
	u.form(title, form)
}

type backupResult struct {
	Path       string   `json:"path"`
	Digest     string   `json:"digest"`
	MCPCount   int      `json:"mcpCount"`
	SkillCount int      `json:"skillCount"`
	Issues     []string `json:"issues"`
}

func (b backupResult) summary() string {
	return fmt.Sprintf("路径：%s\nMCP：%d\nSkills：%d", b.Path, b.MCPCount, b.SkillCount)
}

func (u *tui) previewBackup(path string) {
	request[backupResult](u, "正在读取备份…", "preview_config", params{"path": path}, func(result backupResult) {
		body := result.summary() + "\n\n恢复将替换当前库中已纳管的 MCP、Skills、分配和代理设置。恢复前会自动备份当前配置。"
		if len(result.Issues) > 0 {
			body += "\n\n提示\n" + strings.Join(result.Issues, "\n")
		}
		u.confirm("恢复备份", body, func() {
			u.mutate("正在恢复备份…", "import_config", params{"path": path, "digest": result.Digest}, "")
		})
	})
}
