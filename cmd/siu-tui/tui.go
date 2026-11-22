package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/nicholasxjy/siu-project/internal/core"
	"github.com/rivo/tview"
)

type params map[string]any

type page struct {
	title        string
	hint         string
	body         tview.Primitive
	keys         func(*tcell.EventKey) bool
	dropdownOpen func() bool
	close        func()
}

type tui struct {
	app     *tview.Application
	service *core.Service
	ctx     context.Context
	layout  *tview.Flex
	status  *tview.TextView
	pages   []page
	tab     int
	filters [2]string
	cancel  context.CancelFunc
	snap    core.SnapshotDTO
}

var tabs = [...]string{"MCP", "Skills", "设置"}

func newTUI(ctx context.Context, service *core.Service) *tui {
	u := &tui{
		app:     tview.NewApplication().EnablePaste(true),
		service: service,
		ctx:     ctx,
		layout:  tview.NewFlex().SetDirection(tview.FlexRow),
		status:  textView("").SetTextColor(tcell.ColorLightSkyBlue),
	}
	u.app.SetRoot(u.layout, true).SetInputCapture(u.key)
	u.home("")
	return u
}

func (u *tui) show() {
	p := u.pages[len(u.pages)-1]
	nav := tabs
	nav[u.tab] = "[ " + nav[u.tab] + " ]"
	banner := textView(" siu    " + strings.Join(nav[:], "   "))
	banner.SetBackgroundColor(tcell.ColorDarkSlateGray)
	banner.SetTextColor(tcell.ColorWhite)
	title := textView(" " + p.title).SetTextStyle(tcell.StyleDefault.Foreground(tcell.ColorLightSkyBlue).Bold(true))
	hint := textView(" " + p.hint).SetTextColor(tcell.ColorSilver)
	u.layout.Clear().
		AddItem(banner, 1, 0, false).
		AddItem(title, 1, 0, false).
		AddItem(p.body, 0, 1, true).
		AddItem(u.status, 2, 0, false).
		AddItem(hint, 2, 0, false)
	u.app.SetFocus(p.body)
}

func (u *tui) push(p page) {
	u.notice("")
	u.pages = append(u.pages, p)
	u.show()
}

func (u *tui) leave(n int) {
	for len(u.pages) > n {
		p := u.pages[len(u.pages)-1]
		u.pages = u.pages[:len(u.pages)-1]
		if p.close != nil {
			p.close()
		}
	}
}

func (u *tui) key(event *tcell.EventKey) *tcell.EventKey {
	if u.cancel != nil {
		if event.Key() == tcell.KeyCtrlC {
			u.cancel()
			u.notice("正在取消，请等待操作结束…")
		}
		return nil
	}
	p := u.pages[len(u.pages)-1]
	if p.keys != nil && p.keys(event) {
		return nil
	}
	switch event.Key() {
	case tcell.KeyEsc:
		if p.dropdownOpen != nil && p.dropdownOpen() {
			return event
		}
		if len(u.pages) > 1 {
			u.leave(len(u.pages) - 1)
			u.notice("")
			u.show()
		}
		return nil
	case tcell.KeyCtrlC:
		u.app.Stop()
		return nil
	}
	return event
}

func (u *tui) notice(message string) {
	u.status.SetText(" " + visible(message, false))
}

func (u *tui) fail(err error) {
	u.notice("操作失败 · " + err.Error())
}

func (u *tui) refresh() {
	request[core.SnapshotDTO](u, "正在读取资源库…", "snapshot", nil, func(snap core.SnapshotDTO) {
		selected := ""
		if len(u.pages) > 0 {
			if rows, ok := u.pages[0].body.(*tview.List); ok && rows.GetItemCount() > 0 {
				selected, _ = rows.GetItemText(rows.GetCurrentItem())
			}
		}
		u.snap = snap
		u.home(selected)
		if len(snap.Issues) > 0 {
			u.notice(fmt.Sprintf("有 %d 条配置提示，可在设置中查看", len(snap.Issues)))
		}
	})
}

func (u *tui) mutate(label, method string, p params, selected string) {
	request[core.MutationResultDTO](u, label, method, p, func(result core.MutationResultDTO) {
		u.snap = result.Snapshot
		u.home(selected)
		if len(result.Report.Issues) > 0 || (result.Log != nil && *result.Log != "") {
			lines := []string{result.Message, fmt.Sprintf("变更数：%d", result.Report.Changed)}
			lines = append(lines, result.Report.Issues...)
			if result.Log != nil {
				lines = append(lines, "", *result.Log)
			}
			u.read("操作结果", strings.Join(lines, "\n"))
			return
		}
		u.notice(result.Message)
	})
}

func request[T any](u *tui, label, method string, p params, done func(T)) {
	ctx, cancel := context.WithTimeout(u.ctx, 3*time.Minute)
	u.cancel = cancel
	u.notice(label)
	go func() {
		defer cancel()
		value, err := coreRequest[T](ctx, u.service, method, p)
		if u.ctx.Err() != nil {
			return
		}
		u.app.QueueUpdateDraw(func() {
			u.cancel = nil
			u.notice("")
			if err != nil {
				u.fail(err)
				return
			}
			done(value)
		})
	}()
}

func coreRequest[T any](ctx context.Context, service *core.Service, method string, p params) (T, error) {
	var result T
	payload, err := json.Marshal(struct {
		Method string `json:"method"`
		Params params `json:"params"`
	}{method, p})
	if err != nil {
		return result, err
	}
	raw, err := service.Request(ctx, string(payload))
	if err != nil {
		return result, err
	}
	var response struct {
		OK    bool   `json:"ok"`
		Data  T      `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return result, err
	}
	if !response.OK {
		return result, errors.New(response.Error)
	}
	return response.Data, nil
}

func visible(text string, multiline bool) string {
	return strings.Map(func(r rune) rune {
		if !unicode.IsControl(r) || (multiline && (r == '\n' || r == '\t')) {
			return r
		}
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		return -1
	}, text)
}

func textView(text string) *tview.TextView {
	return tview.NewTextView().SetWordWrap(true).SetText(visible(text, true))
}

func newList() *tview.List {
	return tview.NewList().
		SetHighlightFullLine(true).
		SetMainTextColor(tcell.ColorWhite).
		SetSecondaryTextColor(tcell.ColorSilver).
		SetSelectedTextColor(tcell.ColorBlack).
		SetSelectedBackgroundColor(tcell.ColorLightSkyBlue)
}

func addRow(list *tview.List, name, detail string, selectRow func()) {
	list.AddItem(tview.Escape(visible(name, false)), tview.Escape(visible(detail, false)), 0, selectRow)
}

func (u *tui) read(title, body string) {
	u.push(page{title: title, hint: "↑↓/PgUp/PgDn 滚动   Esc 返回", body: textView(body)})
}

func (u *tui) confirm(title, body string, accept func()) {
	u.push(page{
		title: title, body: textView(body), hint: "Y 确认   ↑↓ 滚动   Esc 取消",
		keys: func(event *tcell.EventKey) bool {
			if event.Rune() == 'y' || event.Rune() == 'Y' {
				accept()
				return true
			}
			return false
		},
	})
}

type choice struct {
	id, label, detail string
	selected          bool
}

func (u *tui) choose(title string, choices []choice, accept func([]string)) {
	list := newList()
	if len(choices) == 0 {
		addRow(list, "没有可选项", "", nil)
	}
	row := func(item choice) (string, string) {
		mark := "[ ] "
		if item.selected {
			mark = "[x] "
		}
		return tview.Escape(visible(mark+item.label, false)), tview.Escape(visible(item.detail, false))
	}
	for _, item := range choices {
		name, detail := row(item)
		list.AddItem(name, detail, 0, nil)
	}
	u.push(page{
		title: title, body: list, hint: "↑↓ 移动   Space 切换选中   Enter 继续   Esc 返回",
		keys: func(event *tcell.EventKey) bool {
			switch {
			case event.Rune() == ' ':
				if index := list.GetCurrentItem(); index >= 0 && index < len(choices) {
					choices[index].selected = !choices[index].selected
					name, detail := row(choices[index])
					list.SetItemText(index, name, detail)
				}
			case event.Key() == tcell.KeyEnter:
				var picked []string
				for _, item := range choices {
					if item.selected {
						picked = append(picked, item.id)
					}
				}
				accept(picked)
			default:
				return false
			}
			return true
		},
	})
}

func (u *tui) form(title string, form *tview.Form) {
	u.push(page{
		title: title, body: form, hint: "Tab / Shift+Tab 移动   Enter 操作   Esc 返回",
		dropdownOpen: func() bool {
			for i := 0; i < form.GetFormItemCount(); i++ {
				if dropdown, ok := form.GetFormItem(i).(*tview.DropDown); ok && dropdown.IsOpen() {
					return true
				}
			}
			return false
		},
	})
}
