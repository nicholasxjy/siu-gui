package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/nicholasxjy/siu-project/internal/core"
	"github.com/rivo/tview"
)

func newForm() *tview.Form {
	form := tview.NewForm().
		SetFieldBackgroundColor(tcell.ColorDarkSlateGray).
		SetFieldTextColor(tcell.ColorWhite).
		SetButtonBackgroundColor(tcell.ColorLightSkyBlue).
		SetButtonTextColor(tcell.ColorBlack)
	form.SetBorderPadding(0, 0, 1, 1)
	return form
}

func textField(form *tview.Form, label, value, placeholder string, secret bool) *tview.InputField {
	field := tview.NewInputField().SetLabel(tview.Escape(visible(label, false))).SetText(value).
		SetPlaceholder(visible(placeholder, false))
	if secret {
		field.SetMaskCharacter('*')
	}
	form.AddFormItem(field)
	return field
}

func reveal(form *tview.Form, fields ...*tview.InputField) {
	form.AddCheckbox("显示敏感字段", false, func(show bool) {
		mask := '*'
		if show {
			mask = 0
		}
		for _, field := range fields {
			field.SetMaskCharacter(mask)
		}
	})
}

func (u *tui) editServer(item *resource) {
	list := newList()
	title := "添加 MCP 服务"
	if item != nil {
		title = "编辑 " + item.name
	}
	for i, transport := range []struct{ id, desc string }{
		{"stdio", "本地可执行命令"}, {"http", "远程 HTTP 服务"}, {"sse", "远程 SSE 服务"},
	} {
		addRow(list, transport.id, transport.desc, func() { u.serverForm(title, core.Transport(transport.id), item) })
		if item != nil && item.server.Transport == transport.id {
			list.SetCurrentItem(i)
		}
	}
	u.push(page{title: title + " · 连接方式", hint: "↑↓ 移动   Enter 继续   Esc 返回", body: list})
}

func jsonField(v any) string {
	data, _ := json.Marshal(v)
	switch text := string(data); text {
	case "null", "[]", "{}":
		return ""
	default:
		return text
	}
}

func decodeField(input *tview.InputField, dst any) error {
	value := strings.TrimSpace(input.GetText())
	if value != "" && json.Unmarshal([]byte(value), dst) != nil {
		return fmt.Errorf("%s格式有误，请检查 JSON", input.GetLabel())
	}
	return nil
}

func (u *tui) serverForm(title string, transport core.Transport, item *resource) {
	var current core.ServerDTO
	if item != nil {
		current = *item.server
	}
	form := newForm()
	name := textField(form, "服务名称", current.Name, "context7", false)
	var readConnection func(*core.Server) error
	if transport == core.TransportStdio {
		command := textField(form, "启动命令", current.Command, "npx", false)
		args := textField(form, "参数 JSON 数组", jsonField(current.Args), "[\"-y\",\"package\"]", true)
		env := textField(form, "环境变量 JSON 对象", jsonField(current.Env), "{\"API_KEY\":\"value\"}", true)
		reveal(form, args, env)
		readConnection = func(server *core.Server) error {
			server.Command = strings.TrimSpace(command.GetText())
			if err := decodeField(args, &server.Args); err != nil {
				return err
			}
			return decodeField(env, &server.Env)
		}
	} else {
		endpoint := textField(form, "服务地址", current.URL, "https://example.com/mcp", true)
		headers := textField(form, "请求头 JSON 对象", jsonField(current.Headers), "{\"Authorization\":\"value\"}", true)
		reveal(form, endpoint, headers)
		readConnection = func(server *core.Server) error {
			server.URL = strings.TrimSpace(endpoint.GetText())
			return decodeField(headers, &server.Headers)
		}
	}
	form.AddButton("选择 Agent", func() {
		server := core.Server{Name: strings.TrimSpace(name.GetText()), Transport: transport}
		if err := readConnection(&server); err != nil {
			u.fail(err)
			return
		}
		if err := server.Validate(); err != nil {
			u.fail(err)
			return
		}
		var assigned []string
		if item != nil {
			assigned = item.agents
		}
		u.targets("mcp", transport.Label(), assigned, item == nil, func(agents []string) {
			p := params{"server": server, "agents": agents}
			if item != nil {
				p["oldName"] = item.name
			}
			u.mutate("正在保存…", "save_server", p, server.Name)
		})
	})
	u.form(title+" · "+transport.Label(), form)
}

func (u *tui) skillSource(source, preferred string) {
	form := newForm()
	field := textField(form, "GitHub 或本地路径", source, "owner/repo 或 ./skills", false)
	form.AddButton("扫描来源", func() {
		value := strings.TrimSpace(field.GetText())
		if value == "" {
			u.fail(errors.New("请输入 GitHub 仓库或本地路径"))
			return
		}
		request[core.ScanResultDTO](u, "正在扫描 Skills…", "scan_skills", params{"source": value}, func(scan core.ScanResultDTO) {
			u.skillScan(scan, preferred)
		})
	})
	u.form("添加 Skills", form)
}

func (u *tui) skillScan(scan core.ScanResultDTO, preferred string) {
	var choices []choice
	installed := 0
	for _, skill := range scan.Skills {
		if skill.Installed {
			installed++
			continue
		}
		choices = append(choices, choice{id: skill.Path, label: skill.Name, detail: skill.Description,
			selected: preferred == "" || skill.Name == preferred || path.Base(skill.Path) == preferred})
	}
	u.choose(fmt.Sprintf("选择要安装的 Skills · %d 已安装", installed), choices, func(paths []string) {
		if len(paths) == 0 {
			u.fail(errors.New("请至少选择一个尚未安装的 Skill"))
			return
		}
		u.targets("skills", "", nil, true, func(agents []string) {
			u.mutate("正在安装 Skills…", "install_skills", params{"token": scan.Token, "paths": paths, "agents": agents}, "")
		})
	})
	u.pages[len(u.pages)-1].close = func() {
		_, _ = coreRequest[struct{}](u.ctx, u.service, "release_scan", params{"token": scan.Token})
	}
}

func (u *tui) discoveryForm() {
	kind := u.kind()
	catalog := "MCP Registry"
	if kind == "skills" {
		catalog = "skills.sh"
	}
	form := newForm()
	field := textField(form, "搜索词", "", "留空浏览推荐资源", false)
	search := func(refresh bool) {
		request[core.DiscoveryDTO](u, "正在查询 "+catalog+"…", "discover",
			params{"kind": kind, "query": strings.TrimSpace(field.GetText()), "refresh": refresh},
			func(result core.DiscoveryDTO) { u.discoveryResults(catalog, result) })
	}
	form.AddButton("搜索", func() { search(false) }).AddButton("强制刷新", func() { search(true) })
	u.form("发现 · "+catalog, form)
}

func (u *tui) discoveryResults(catalog string, result core.DiscoveryDTO) {
	list := newList()
	for _, entry := range result.Servers {
		addRow(list, entry.Title, entry.Description, func() { u.marketServer(entry) })
	}
	for _, entry := range result.Skills {
		addRow(list, entry.Name, fmt.Sprintf("%s · %d 次安装", entry.Source, entry.Installs), func() {
			u.skillSource(entry.Source, entry.SkillID)
		})
	}
	if list.GetItemCount() == 0 {
		addRow(list, "没有匹配结果", "返回后可更换搜索词", nil)
	}
	if len(result.Warnings) > 0 {
		addRow(list, fmt.Sprintf("查看 %d 条发现提示", len(result.Warnings)), "", func() {
			u.read("发现提示", strings.Join(result.Warnings, "\n"))
		})
	}
	if result.Cached {
		catalog += " · 使用了缓存"
	}
	u.push(page{title: catalog, hint: "↑↓ 移动   Enter 打开   Esc 返回", body: list})
}

func (u *tui) marketServer(entry core.MarketServerDTO) {
	list := newList()
	addRow(list, "安装 MCP 服务", "填写目录要求的参数并选择 Agent", func() { u.installMarketServer(entry) })
	addRow(list, "查看目录信息", entry.Publisher, func() {
		body := strings.Join([]string{entry.Description, entry.Homepage, "", serverPreview(entry.Server)}, "\n")
		u.read(entry.Title, body)
	})
	u.push(page{title: entry.Title, hint: "↑↓ 移动   Enter 打开   Esc 返回", body: list})
}

func (u *tui) installMarketServer(entry core.MarketServerDTO) {
	form := newForm()
	name := textField(form, "服务名称", entry.Server.Name, "", false)
	var fields []*tview.InputField
	for _, input := range entry.Inputs {
		label := input.Label
		if input.Required {
			label += " *"
		}
		fields = append(fields, textField(form, label, "", input.Hint, input.Secret))
	}
	form.AddButton("选择 Agent", func() {
		serverName := strings.TrimSpace(name.GetText())
		if err := core.ValidName(serverName); err != nil {
			u.fail(err)
			return
		}
		values := make(map[string]string, len(fields))
		for i, input := range entry.Inputs {
			value := strings.TrimSpace(fields[i].GetText())
			if input.Required && value == "" {
				u.fail(fmt.Errorf("请填写 %s", input.Label))
				return
			}
			values[input.Key] = value
		}
		u.targets("mcp", entry.Server.Transport, nil, true, func(agents []string) {
			u.mutate("正在安装 MCP…", "install_server", params{
				"id": entry.ID, "name": serverName, "values": values, "agents": agents,
			}, serverName)
		})
	})
	u.form("安装 · "+entry.Title, form)
}

func proxyForm(proxy core.ProxySettings, save func(core.ProxySettings)) *tview.Form {
	form := newForm()
	modes := []string{"system", "custom", "direct"}
	form.AddDropDown("代理模式", modes, slices.Index(modes, string(proxy.Mode)), func(mode string, _ int) {
		proxy.Mode = core.ProxyMode(mode)
	})
	endpoint := textField(form, "自定义代理地址", proxy.URL, "http://127.0.0.1:8080", true)
	bypass := textField(form, "自定义绕过列表", proxy.NoProxy, "localhost,.example.com", false)
	reveal(form, endpoint)
	form.AddButton("保存代理", func() {
		proxy.URL = strings.TrimSpace(endpoint.GetText())
		proxy.NoProxy = strings.TrimSpace(bypass.GetText())
		save(proxy)
	})
	return form
}
