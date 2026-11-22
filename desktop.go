// Desktop is the mygo-bound service the frontend calls through
// mygo-runtime as "Desktop.*" (see docs/protocol.md): Request handles
// the core JSON protocol in-process via internal/core, and the other
// methods are native desktop helpers (dialogs, opening paths/URLs, the
// app's own identity and appearance).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/nicholasxjy/siu-project/internal/core"
)

// Desktop is bound under the name "Desktop" (see mygo.BindAs in main.go).
// The zero value has no core service yet; main installs one with setCore
// from App.WhenReady, once the app (and so dialogs and the window controls
// CallerWindow resolves) is actually usable.
type Desktop struct {
	mu   sync.RWMutex
	core *core.Service
}

// setCore installs the in-process core service. Called once, from main's
// App.WhenReady callback.
func (d *Desktop) setCore(s *core.Service) {
	d.mu.Lock()
	d.core = s
	d.mu.Unlock()
}

func (d *Desktop) coreService() (*core.Service, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.core == nil {
		return nil, errors.New("siu-project: the desktop backend is not ready yet")
	}
	return d.core, nil
}

// closeCore cleans up any retained scan temp directories. Called once,
// from main's App.OnQuit hook.
func (d *Desktop) closeCore() {
	d.mu.RLock()
	s := d.core
	d.mu.RUnlock()
	if s != nil {
		_ = s.Close()
	}
}

// defaultRequestTimeout bounds how long local library reads and writes wait.
const defaultRequestTimeout = 30 * time.Second

// discoverRequestTimeout covers "discover": market/registry searches over
// the network, with cache fallback when upstream is slow or unreachable.
const discoverRequestTimeout = 150 * time.Second

// downloadRequestTimeout covers methods that download or install content
// ("scan_skills", "install_skills", "install_server") and configuration
// transfers / snapshot (which may restore config.json with large Skill trees).
const downloadRequestTimeout = 3 * time.Minute

// updateRequestTimeout covers "update": fetching and installing updates for
// MCP servers or Skills across multiple network calls.
const updateRequestTimeout = 5 * time.Minute

// requestTimeout picks the timeout budget for a core method name. Keep this
// in sync with cmd/testhost, which forwards the same payloads over HTTP
// during browser E2E tests.
func requestTimeout(method string) time.Duration {
	switch method {
	case "update":
		return updateRequestTimeout
	case "snapshot", "scan_skills", "install_skills", "install_server", "export_config", "preview_config", "import_config":
		return downloadRequestTimeout
	case "discover":
		return discoverRequestTimeout
	default:
		return defaultRequestTimeout
	}
}

type corePayloadHead struct {
	Method string `json:"method"`
}

// Request dispatches payload -- the core protocol's {method,params}
// envelope -- to the in-process Go core service and returns its raw JSON
// response; the frontend parses the {ok,data|error} shape itself (see
// docs/protocol.md).
func (d *Desktop) Request(ctx context.Context, payload string) (string, error) {
	svc, err := d.coreService()
	if err != nil {
		return "", err
	}
	var head corePayloadHead
	_ = json.Unmarshal([]byte(payload), &head)
	ctx, cancel := context.WithTimeout(ctx, requestTimeout(head.Method))
	defer cancel()
	return svc.Request(ctx, payload)
}

// PickDirectory shows a native directory picker anchored to the calling
// window (ctx carries it; see mygo.CallerWindow) and returns the chosen
// path, or "" if the user canceled.
func (d *Desktop) PickDirectory(ctx context.Context) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:    mygo.CallerWindow(ctx),
		Directory: true,
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	return paths[0], nil
}

// PickConfigFile selects a portable siu JSON backup for preview.
func (d *Desktop) PickConfigFile(ctx context.Context) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  mygo.CallerWindow(ctx),
		Title:   "导入 siu 配置",
		Filters: []mygo.FileFilter{{Name: "siu JSON 配置", Extensions: []string{"json"}}},
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// PickConfigDestination leaves overwrite confirmation to the native save dialog.
func (d *Desktop) PickConfigDestination(ctx context.Context) (string, error) {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:            mygo.CallerWindow(ctx),
		Title:             "导出 siu 配置",
		DefaultPath:       "siu-config-" + time.Now().Format("2006-01-02") + ".json",
		Filters:           []mygo.FileFilter{{Name: "siu JSON 配置", Extensions: []string{"json"}}},
		CreateDirectories: true,
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, nil
}

// OpenPath opens an existing file or directory with the OS's default
// application. It never invokes a shell, and rejects paths that do not
// exist instead of letting the OS decide what an unknown path means.
func (d *Desktop) OpenPath(path string) error {
	if path == "" {
		return errors.New("siu-project: OpenPath: path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("siu-project: OpenPath: %w", err)
	}
	return mygo.Shell.OpenPath(path)
}

// OpenURL opens an http(s) URL with the default browser. Other schemes
// (file:, javascript:, custom app schemes, ...) are rejected so the
// frontend cannot use it to launch arbitrary local resources or programs.
func (d *Desktop) OpenURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("siu-project: OpenURL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("siu-project: OpenURL: unsupported scheme %q, only http and https are allowed", u.Scheme)
	}
	return mygo.Shell.OpenExternal(rawURL)
}

// Info describes the running desktop app, for display and bug reports.
type Info struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Version string `json:"version"`
}

// Info returns the host OS, architecture and app version.
func (d *Desktop) Info() Info {
	return Info{OS: runtime.GOOS, Arch: runtime.GOARCH, Version: mygo.App.Version()}
}

// SetTheme overrides the app's light/dark appearance; "system" follows
// the OS. The frontend is responsible for persisting the choice itself
// (localStorage) and calling SetTheme again on the next launch.
func (d *Desktop) SetTheme(theme string) error {
	switch theme {
	case "system":
		mygo.Theme.SetSource(mygo.ThemeSystem)
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		return fmt.Errorf("siu-project: SetTheme: unknown theme %q, want system, light or dark", theme)
	}
	return nil
}
