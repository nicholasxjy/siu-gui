// Desktop is the mygo-bound service the frontend calls through
// mygo-runtime as "Desktop.*" (see docs/protocol.md): Request forwards the
// core JSON-RPC-like protocol to the siu-core sidecar, and the other
// methods are native desktop helpers (dialogs, opening paths/URLs, the
// app's own identity and appearance) that do not go through siu-core.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/nicholasxjy/siu-gui/internal/backend"
)

// Desktop is bound under the name "Desktop" (see mygo.BindAs in main.go).
// The zero value has no backend client yet; main installs one with
// setClient from App.WhenReady, once the app (and so dialogs and the
// window controls CallerWindow resolves) is actually usable. Calls that
// reach Desktop before that point reflect a frontend bug, not a user
// action, since the window itself cannot exist yet either.
type Desktop struct {
	mu     sync.RWMutex
	client *backend.Client
}

// setClient installs the backend client. Called once, from main's
// App.WhenReady callback.
func (d *Desktop) setClient(c *backend.Client) {
	d.mu.Lock()
	d.client = c
	d.mu.Unlock()
}

func (d *Desktop) coreClient() (*backend.Client, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.client == nil {
		return nil, errors.New("siu-gui: the desktop backend is not ready yet")
	}
	return d.client, nil
}

// closeClient shuts down the backend's siu-core process, if one was ever
// started. Called once, from main's App.OnQuit hook.
func (d *Desktop) closeClient() {
	d.mu.RLock()
	c := d.client
	d.mu.RUnlock()
	if c != nil {
		_ = c.Close()
	}
}

// defaultRequestTimeout bounds how long most core requests wait for a
// response: local library reads and writes (snapshot, save_server,
// set_agents, set_enabled, delete, import, skill_content, release_scan).
// Generous enough for file I/O, still gives the user feedback instead of
// hanging forever if siu-core becomes unresponsive.
const defaultRequestTimeout = 30 * time.Second

// discoverRequestTimeout covers "discover": a market/registry search over
// the network, which upstream may take close to a minute to answer before
// siu-core's own cache fallback kicks in.
const discoverRequestTimeout = 150 * time.Second

// downloadRequestTimeout covers methods that download or install content
// from the network ("scan_skills" clones/fetches a GitHub source,
// "install_skills" and "install_server" may still be finishing a download
// started by scan/discover). Several times upstream's own ~60s per-request
// budget, since siu-core may retry or chain more than one network call.
const downloadRequestTimeout = 3 * time.Minute

// updateRequestTimeout is the longest budget: "update" fetches and
// installs new versions of MCP servers or Skills, which can mean several
// slow network operations in a row.
const updateRequestTimeout = 5 * time.Minute

// requestTimeout picks the timeout budget for a core method name. An
// empty or unrecognized method still gets the default budget; siu-core
// itself rejects unknown method names, this is only about how long we
// wait for that rejection. Keep this in sync with cmd/testhost, which
// forwards the same payloads over HTTP during browser E2E tests and must
// not time out before siu-core's own network calls would.
func requestTimeout(method string) time.Duration {
	switch method {
	case "update":
		return updateRequestTimeout
	case "scan_skills", "install_skills", "install_server":
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

// Request forwards payload -- the core protocol's {method,params}
// envelope -- to siu-core and returns its raw JSON response verbatim; the
// frontend parses the {ok,data|error} shape itself (see
// docs/protocol.md). The only errors Request itself produces are
// transport-level: no backend yet, or the request timed out. A timeout
// here means the outcome is unknown, not that it failed or was rolled
// back: backend.Client's error message says so explicitly, and the
// frontend must refresh (snapshot) to find the actual state rather than
// assume the mutation did or did not apply.
func (d *Desktop) Request(ctx context.Context, payload string) (string, error) {
	client, err := d.coreClient()
	if err != nil {
		return "", err
	}
	var head corePayloadHead
	// Best-effort: an invalid payload is still forwarded and rejected with
	// a clear error by backend.Client.Request itself.
	_ = json.Unmarshal([]byte(payload), &head)
	ctx, cancel := context.WithTimeout(ctx, requestTimeout(head.Method))
	defer cancel()
	return client.Request(ctx, payload)
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

// OpenPath opens an existing file or directory with the OS's default
// application. It never invokes a shell, and rejects paths that do not
// exist instead of letting the OS decide what an unknown path means.
func (d *Desktop) OpenPath(path string) error {
	if path == "" {
		return errors.New("siu-gui: OpenPath: path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("siu-gui: OpenPath: %w", err)
	}
	return mygo.Shell.OpenPath(path)
}

// OpenURL opens an http(s) URL with the default browser. Other schemes
// (file:, javascript:, custom app schemes, ...) are rejected so the
// frontend cannot use it to launch arbitrary local resources or programs.
func (d *Desktop) OpenURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("siu-gui: OpenURL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("siu-gui: OpenURL: unsupported scheme %q, only http and https are allowed", u.Scheme)
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
		return fmt.Errorf("siu-gui: SetTheme: unknown theme %q, want system, light or dark", theme)
	}
	return nil
}

// coreExecutableName is the sidecar binary's file name on this platform.
func coreExecutableName() string {
	if runtime.GOOS == "windows" {
		return "siu-core.exe"
	}
	return "siu-core"
}

// resolveCoreExecutablePath looks for the packaged siu-core sidecar under
// resourcesDir (mygo.App.Path(mygo.PathResources)). `mygo build` and
// `mygo dev` flatten the platform's resources/<os>-<arch> directory
// straight into bin/ (see docs/distribution.md#platform-resources); `go
// run` and `go test` leave resources exactly as the project has it,
// platform subdirectories included. Both layouts are tried, flattened
// first since that is what a packaged app actually ships; "" means
// neither has a bundled sidecar here, so the caller should fall back to
// backend.New's embedded binary (or SIU_GUI_CORE, which backend.Client
// always prefers regardless of what path is passed to it).
func resolveCoreExecutablePath(resourcesDir string) string {
	if resourcesDir == "" {
		return ""
	}
	name := coreExecutableName()
	if p := filepath.Join(resourcesDir, "bin", name); isRegularFile(p) {
		return p
	}
	if p := filepath.Join(resourcesDir, runtime.GOOS+"-"+runtime.GOARCH, "bin", name); isRegularFile(p) {
		return p
	}
	return ""
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
