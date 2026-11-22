package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/nicholasxjy/siu-project/internal/core"
)

type resource struct {
	name, description string
	server            *core.ServerDTO
	skill             *core.SkillEntryDTO
	agents            []string
	states            map[string]string
	locations         []core.FoundSkillAtDTO
	managed, enabled  bool
}

func (u *tui) kind() string {
	if u.tab == 0 {
		return "mcp"
	}
	return "skills"
}

func (u *tui) resources() []resource {
	var rows []resource
	if u.kind() == "mcp" {
		for _, item := range u.snap.MCP {
			server := core.ServerDTO{
				Name: item.Name, Transport: item.Transport, Command: item.Command, Args: item.Args,
				Env: item.Env, URL: item.URL, Headers: item.Headers,
			}
			desc := strings.ToUpper(item.Transport)
			if item.Origin != nil {
				desc += " · " + *item.Origin
			}
			rows = append(rows, resource{name: item.Name, description: desc, server: &server,
				agents: item.Agents, states: item.States, managed: true, enabled: item.Enabled})
		}
		for _, item := range u.snap.FoundMCP {
			rows = append(rows, resource{name: item.Server.Name, description: strings.ToUpper(item.Server.Transport),
				server: &item.Server, agents: item.Agents})
		}
	} else {
		for _, item := range u.snap.Skills {
			rows = append(rows, resource{name: item.Name, description: item.Description, skill: &item,
				agents: item.Agents, states: item.States, managed: true, enabled: item.Enabled})
		}
		for _, item := range u.snap.FoundSkills {
			rows = append(rows, resource{name: item.Name, description: item.Description, locations: item.At})
		}
	}
	return rows
}

func (u *tui) home(selected string) {
	u.leave(0)
	list := newList()
	title := tabs[u.tab]
	hint := "Tab / Shift+Tab 切换页面   ↑↓ 移动   Enter 打开   R 刷新   Q 退出"
	if u.tab == 2 {
		u.settings(list)
	} else {
		rows := u.resources()
		filter := u.filters[u.tab]
		managed := 0
		for _, item := range rows {
			status := "未纳管"
			if item.managed {
				managed++
				status = fmt.Sprintf("%d 个 Agent", len(item.agents))
				if !item.enabled {
					status = "已暂停 · " + status
				} else if slices.ContainsFunc(item.agents, func(id string) bool { return item.states[id] == "error" }) {
					status = "写入未完成 · " + status
				}
			}
			if !item.matches(filter) {
				continue
			}
			if item.description != "" {
				status += "  |  " + item.description
			}
			addRow(list, item.name, status, func() { u.resourcePage(item) })
			if item.name == selected {
				list.SetCurrentItem(list.GetItemCount() - 1)
			}
		}
		title = fmt.Sprintf("%s   %d 已纳管 · %d 未纳管", title, managed, len(rows)-managed)
		if filter != "" {
			title += fmt.Sprintf(" · 匹配 %d 项：%s", list.GetItemCount(), filter)
		}
		hint += "\n A 添加   D 发现   / 筛选"
		if u.tab == 0 {
			addRow(list, "+ 添加 MCP 服务", "配置 stdio、HTTP 或 SSE 服务", u.addResource)
		} else {
			addRow(list, "+ 添加 Skills", "扫描 GitHub 仓库或本地目录", u.addResource)
		}
		addRow(list, "发现资源", "搜索和安装目录中的资源", u.discoveryForm)
		if managed > 0 {
			addRow(list, "更新全部", "检查这一类资源的更新", func() {
				u.mutate("正在更新…", "update", params{"kind": u.kind()}, "")
			})
		}
	}
	u.pages = []page{{title: title, hint: hint, body: list, keys: u.homeKey}}
	u.show()
}

func (u *tui) homeKey(event *tcell.EventKey) bool {
	switch {
	case event.Key() == tcell.KeyTab:
		u.tab = (u.tab + 1) % len(tabs)
	case event.Key() == tcell.KeyBacktab:
		u.tab = (u.tab + len(tabs) - 1) % len(tabs)
	case event.Rune() == 'r' || event.Rune() == 'R':
		u.refresh()
		return true
	case event.Rune() == 'q' || event.Rune() == 'Q':
		u.app.Stop()
		return true
	default:
		if u.tab == 2 {
			return false
		}
		switch event.Rune() {
		case 'a', 'A':
			u.addResource()
		case 'd', 'D':
			u.discoveryForm()
		case '/':
			u.filterForm()
		default:
			return false
		}
		return true
	}
	u.notice("")
	u.home("")
	return true
}

func (u *tui) addResource() {
	if u.tab == 0 {
		u.editServer(nil)
	} else {
		u.skillSource("", "")
	}
}

func (u *tui) filterForm() {
	form := newForm()
	field := textField(form, "筛选词", u.filters[u.tab], "名称、描述、来源或 Agent ID", false)
	form.AddButton("应用筛选", func() {
		u.filters[u.tab] = strings.TrimSpace(field.GetText())
		u.home("")
	})
	form.AddButton("清除筛选", func() {
		u.filters[u.tab] = ""
		u.home("")
	})
	u.form("筛选 "+tabs[u.tab], form)
}

func (item resource) matches(filter string) bool {
	if filter == "" {
		return true
	}
	text := item.name + " " + item.description + " " + strings.Join(item.agents, " ")
	if item.skill != nil && item.skill.Source != nil {
		text += " " + item.skill.Source.Repo + " " + item.skill.Source.Dir
	}
	for _, location := range item.locations {
		text += " " + location.Agent + " " + location.Path
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(filter))
}

func stateLabel(state string) string {
	switch state {
	case "on":
		return "已配置"
	case "paused":
		return "已暂停"
	case "error":
		return "写入未完成"
	}
	if why, ok := strings.CutPrefix(state, "unsupported:"); ok {
		return why
	}
	return "未配置"
}

func (u *tui) resourcePage(item resource) {
	list := newList()
	kind := u.kind()
	p := params{"kind": kind, "name": item.name}
	if item.server != nil {
		addRow(list, "查看配置", "凭据已遮蔽", func() { u.read(item.name+" · MCP 配置", serverPreview(*item.server)) })
	}
	if item.skill != nil {
		addRow(list, "查看 SKILL.md", "查看已纳管的本地副本", func() {
			request[struct{ Content, Path string }](u, "正在读取 SKILL.md…", "skill_content", params{"name": item.name}, func(result struct{ Content, Path string }) {
				u.read(item.name+" · SKILL.md", result.Path+"\n\n"+result.Content)
			})
		})
	}
	addRow(list, "详情与 Agent 状态", "查看来源、分配和配置写入状态", func() { u.resourceDetails(item) })
	if !item.managed {
		addRow(list, "导入资源库", "接管现有资源并保留分配", func() {
			u.confirm("导入 "+item.name, "将此资源及其现有 Agent 分配加入 Siu 资源库。", func() {
				u.mutate("正在导入…", "import", p, item.name)
			})
		})
	} else {
		if item.server != nil {
			addRow(list, "编辑 MCP 服务", "修改连接、参数或服务名称", func() { u.editServer(&item) })
		}
		addRow(list, "分配给 Agent", "选择目标 Agent", func() {
			transport := ""
			if item.server != nil {
				transport = item.server.Transport
			}
			u.targets(kind, transport, item.agents, false, func(agents []string) {
				u.mutate("正在写入分配…", "set_agents", params{"kind": kind, "name": item.name, "agents": agents}, item.name)
			})
		})
		toggle := "暂停"
		if !item.enabled {
			toggle = "启用"
		}
		addRow(list, toggle, "保留资源库配置与 Agent 分配", func() {
			u.mutate("正在"+toggle+"…", "set_enabled", params{"kind": kind, "name": item.name, "enabled": !item.enabled}, item.name)
		})
		addRow(list, "检查更新", "更新当前资源", func() { u.mutate("正在更新…", "update", p, item.name) })
		addRow(list, "删除", "移除资源及由 Siu 写入的配置", func() {
			u.confirm("删除 "+item.name, "将从资源库删除该资源，并撤回由 Siu 写入的 Agent 配置。", func() {
				u.mutate("正在删除…", "delete", p, "")
			})
		})
	}
	u.push(page{title: item.name, hint: "↑↓ 移动   Enter 打开   Esc 返回", body: list})
}

func (u *tui) resourceDetails(item resource) {
	lines := []string{item.description}
	if item.skill != nil && item.skill.Source != nil {
		source := item.skill.Source
		if source.Kind == "github" {
			lines = append(lines, fmt.Sprintf("来源：%s @ %s  %s", source.Repo, source.Ref, source.Path))
		} else {
			lines = append(lines, "来源："+source.Dir)
		}
	}
	lines = append(lines, "", "Agent")
	if item.managed {
		for _, agent := range u.snap.Agents {
			if state, ok := item.states[agent.ID]; ok {
				lines = append(lines, fmt.Sprintf("%s   %s", agent.Name, stateLabel(state)))
			}
		}
	} else {
		lines = append(lines, item.agents...)
		for _, loc := range item.locations {
			lines = append(lines, loc.Agent+"   "+loc.Path)
		}
	}
	u.read(item.name+" · 详情", strings.Join(lines, "\n"))
}

func (u *tui) targets(kind, transport string, current []string, useDefaults bool, done func([]string)) {
	var options []choice
	for _, agent := range u.snap.Agents {
		path := agent.SkillsPath
		usable := agent.Detected || agent.ID == "agents"
		if kind == "mcp" {
			path = agent.MCPPath
			usable = agent.Detected && slices.Contains(agent.Transports, transport)
		}
		if usable && path != nil {
			options = append(options, choice{id: agent.ID, label: agent.Name, detail: *path,
				selected: slices.Contains(current, agent.ID) || (useDefaults && agent.ID != "agents")})
		}
	}
	u.choose("分配给 Agent", options, done)
	if slices.ContainsFunc(current, func(id string) bool {
		return !slices.ContainsFunc(options, func(c choice) bool { return c.id == id })
	}) {
		u.notice("当前有未检测到或不兼容的分配，继续保存将移除这些分配。")
	}
}

var sensitiveField = regexp.MustCompile(`(?i)token|secret|password|(?:api[-_]?)?key|credential|authorization`)

func serverPreview(server core.ServerDTO) string {
	const hidden = "••••••••"
	server.Args = slices.Clone(server.Args)
	for i := 0; i < len(server.Args); i++ {
		if sensitiveField.MatchString(server.Args[i]) {
			if key, _, ok := strings.Cut(server.Args[i], "="); ok {
				server.Args[i] = key + "=" + hidden
			} else if strings.HasPrefix(server.Args[i], "-") && i+1 < len(server.Args) {
				i++
				server.Args[i] = hidden
			} else if !strings.HasPrefix(server.Args[i], "-") {
				server.Args[i] = hidden
			}
		}
	}
	if server.URL != "" {
		if endpoint, err := url.Parse(server.URL); err == nil && endpoint.Host != "" {
			endpoint.User = nil
			endpoint.RawQuery = ""
			endpoint.Fragment = ""
			server.URL = endpoint.String()
		} else {
			server.URL = hidden
		}
	}
	for _, field := range []*map[string]string{&server.Env, &server.Headers} {
		copy := make(map[string]string, len(*field))
		for key := range *field {
			copy[key] = hidden
		}
		*field = copy
	}
	body, _ := json.MarshalIndent(server, "", "  ")
	return string(body)
}
