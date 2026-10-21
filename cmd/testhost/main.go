// Command testhost is a development/test-only loopback HTTP server that
// lets browser-driven E2E tests exercise the real siu-core backend
// without a mygo native window (see docs/protocol.md's closing note).
// vite.config.ts proxies /__test/* here when SIU_GUI_BROWSER_TEST=1, for
// the Vite dev server at http://127.0.0.1:5173 only.
//
// It is never built into the production app: `mygo build` only compiles
// the root package (main.go), never cmd/testhost, so this binary exists
// solely for `go run ./cmd/testhost` during local/CI browser testing.
//
// Safety is load-bearing, not incidental: it refuses to start unless
// SIU_GUI_TEST_HOME is an explicit absolute path distinct from the real
// home directory (so a stray run can never read or write the operator's
// real agent configuration), it binds 127.0.0.1 only, it rejects anything
// but the Vite dev server's Origin, and OpenPath/OpenURL/PickDirectory
// never touch the OS or show native UI -- they just report that they are
// unsupported here.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/nicholasxjy/siu-gui/internal/backend"
)

// listenAddr is fixed, not configurable: docs/protocol.md and
// vite.config.ts's SIU_GUI_BROWSER_TEST proxy both hardcode it, and nothing
// in this program should make it easy to accidentally widen that.
const listenAddr = "127.0.0.1:47832"

// maxRequestBodyBytes bounds a single POST /__test/request body: generous
// enough for any real payload (e.g. install_skills with many paths) while
// still rejecting abuse or accidental huge uploads.
const maxRequestBodyBytes = 4 << 20 // 4 MiB

// allowedOrigins is the Vite dev server this harness exists for
// (vite.config.ts: host 127.0.0.1, port 5173). Nothing else may call it.
var allowedOrigins = map[string]bool{
	"http://127.0.0.1:5173": true,
	"http://localhost:5173": true,
}

// Timeout tiers mirror desktop.go's requestTimeout exactly (see its
// comment for why each exists): keep the two in sync if either changes.
const (
	defaultRequestTimeout  = 30 * time.Second
	discoverRequestTimeout = 150 * time.Second
	downloadRequestTimeout = 3 * time.Minute
	updateRequestTimeout   = 5 * time.Minute
)

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

func main() {
	testHome, err := validatedTestHome()
	if err != nil {
		log.Fatalf("testhost: %v", err)
	}
	log.Printf("testhost: SIU_GUI_TEST_HOME=%s", testHome)

	client := backend.New()
	defer client.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/__test/request", withOrigin(requireMethod(http.MethodPost, handleRequest(client))))
	mux.HandleFunc("/__test/info", withOrigin(requireMethod(http.MethodGet, handleInfo)))
	mux.HandleFunc("/__test/pick-directory", withOrigin(handleUnsupported("PickDirectory: testhost has no native UI to show a directory picker")))
	mux.HandleFunc("/__test/open-path", withOrigin(requireMethod(http.MethodPost, handleUnsupported("OpenPath: testhost never touches the OS"))))
	mux.HandleFunc("/__test/open-url", withOrigin(requireMethod(http.MethodPost, handleUnsupported("OpenURL: testhost never touches the OS"))))

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("testhost: listen %s: %v", listenAddr, err)
	}
	srv := &http.Server{Handler: mux}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	log.Printf("testhost: listening on http://%s", listenAddr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("testhost: serve: %v", err)
		}
	case <-sigCh:
		log.Print("testhost: shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

// validatedTestHome reads and validates SIU_GUI_TEST_HOME: it must be
// set, absolute, and must not resolve to the real home directory. siu-core
// itself (core/src/env.rs) also reads this variable directly from the
// process environment and roots everything under it when set; testhost
// only has to make sure it is actually set, correct, and passed through
// (which backend.Client does automatically via os.Environ()).
func validatedTestHome() (string, error) {
	raw := os.Getenv("SIU_GUI_TEST_HOME")
	if raw == "" {
		return "", errors.New("SIU_GUI_TEST_HOME must be set to an absolute path before starting testhost; it must differ from the real home directory")
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("SIU_GUI_TEST_HOME must be an absolute path, got %q", raw)
	}
	testHome := filepath.Clean(raw)

	realHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve the real home directory to compare against: %w", err)
	}
	realHome = filepath.Clean(realHome)

	if samePath(testHome, realHome) {
		return "", fmt.Errorf("SIU_GUI_TEST_HOME (%s) must not be the real home directory", testHome)
	}
	return testHome, nil
}

// samePath compares two paths for pointing at the same directory,
// resolving symlinks when possible so an aliased path to the real home
// cannot slip past validatedTestHome's check.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// withOrigin rejects any request whose Origin is not the Vite dev server
// this harness exists for. A GET may omit Origin (browsers do not always
// send it for simple same-origin GETs, and the only GET route here,
// /__test/info, is read-only); any other method without an Origin header
// is refused, since real browsers always send Origin for non-GET fetches
// -- its absence means the caller is not going through the Vite proxy.
func withOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && !allowedOrigins[origin] {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if origin == "" && r.Method != http.MethodGet {
			http.Error(w, "origin header required", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func requireMethod(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

// requestHead is only used to pick a timeout tier; the full payload is
// forwarded to siu-core unparsed, exactly as Desktop.Request does.
type requestHead struct {
	Method string `json:"method"`
}

func handleRequest(client *backend.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "request body too large or unreadable", http.StatusRequestEntityTooLarge)
			return
		}

		var head requestHead
		_ = json.Unmarshal(body, &head) // best-effort; the backend itself rejects a malformed payload

		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout(head.Method))
		defer cancel()

		data, err := client.Request(ctx, string(body))
		if err != nil {
			// Log only the method name: the payload and the core's
			// response can carry MCP server secrets (env values,
			// headers, tokens), which must never land in a log file.
			log.Printf("testhost: request method=%q failed: %v", head.Method, err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, data)
	}
}

type infoResponse struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Version string `json:"version"`
}

func handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(infoResponse{OS: runtime.GOOS, Arch: runtime.GOARCH, Version: "dev"})
}

func handleUnsupported(reason string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, reason, http.StatusNotImplemented)
	}
}
