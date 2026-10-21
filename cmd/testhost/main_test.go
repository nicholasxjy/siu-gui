package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withEnv(t *testing.T, key, value string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestValidatedTestHomeRejectsUnset(t *testing.T) {
	withEnv(t, "SIU_GUI_TEST_HOME", "")
	if _, err := validatedTestHome(); err == nil {
		t.Fatal("validatedTestHome with unset SIU_GUI_TEST_HOME: want error, got nil")
	}
}

func TestValidatedTestHomeRejectsRelative(t *testing.T) {
	withEnv(t, "SIU_GUI_TEST_HOME", "relative/path")
	if _, err := validatedTestHome(); err == nil {
		t.Fatal("validatedTestHome with relative path: want error, got nil")
	}
}

func TestValidatedTestHomeRejectsRealHome(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory to compare against on this system")
	}
	withEnv(t, "SIU_GUI_TEST_HOME", realHome)
	if _, err := validatedTestHome(); err == nil {
		t.Fatal("validatedTestHome with the real home directory: want error, got nil")
	}
}

func TestValidatedTestHomeRejectsRealHomeTrailingSlash(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory to compare against on this system")
	}
	withEnv(t, "SIU_GUI_TEST_HOME", realHome+string(filepath.Separator))
	if _, err := validatedTestHome(); err == nil {
		t.Fatal("validatedTestHome with the real home directory (trailing slash): want error, got nil")
	}
}

func TestValidatedTestHomeAcceptsDistinctAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "SIU_GUI_TEST_HOME", dir)
	got, err := validatedTestHome()
	if err != nil {
		t.Fatalf("validatedTestHome(%q): unexpected error: %v", dir, err)
	}
	if got != filepath.Clean(dir) {
		t.Errorf("validatedTestHome(%q) = %q, want %q", dir, got, filepath.Clean(dir))
	}
}

func TestRequestTimeout(t *testing.T) {
	cases := []struct {
		method string
		want   time.Duration
	}{
		{"snapshot", downloadRequestTimeout},
		{"", defaultRequestTimeout},
		{"discover", discoverRequestTimeout},
		{"scan_skills", downloadRequestTimeout},
		{"install_skills", downloadRequestTimeout},
		{"install_server", downloadRequestTimeout},
		{"export_config", downloadRequestTimeout},
		{"preview_config", downloadRequestTimeout},
		{"import_config", downloadRequestTimeout},
		{"update", updateRequestTimeout},
	}
	for _, c := range cases {
		if got := requestTimeout(c.method); got != c.want {
			t.Errorf("requestTimeout(%q) = %v, want %v", c.method, got, c.want)
		}
	}
}

func TestWithOriginAllowsKnownOrigins(t *testing.T) {
	handler := withOrigin(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, origin := range []string{"http://127.0.0.1:5173", "http://localhost:5173"} {
		req := httptest.NewRequest(http.MethodPost, "/__test/request", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("origin %q: status = %d, want 200", origin, rec.Code)
		}
	}
}

func TestWithOriginRejectsUnknownOrigin(t *testing.T) {
	handler := withOrigin(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, origin := range []string{
		"http://evil.example",
		"http://127.0.0.1:5174",
		"https://127.0.0.1:5173",
		"http://127.0.0.1:5173.evil.example",
		"null",
	} {
		req := httptest.NewRequest(http.MethodPost, "/__test/request", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("origin %q: status = %d, want 403", origin, rec.Code)
		}
	}
}

func TestWithOriginRejectsMissingOriginForPost(t *testing.T) {
	handler := withOrigin(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "/__test/request", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without Origin: status = %d, want 403", rec.Code)
	}
}

func TestWithOriginAllowsMissingOriginForGet(t *testing.T) {
	handler := withOrigin(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/__test/info", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET without Origin: status = %d, want 200", rec.Code)
	}
}

func TestRequireMethodRejectsWrongMethod(t *testing.T) {
	handler := requireMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/__test/request", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET on a POST-only route: status = %d, want 405", rec.Code)
	}
}

func TestHandleInfoReturnsOSArchVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/__test/info", nil)
	rec := httptest.NewRecorder()
	handleInfo(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleInfo: status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"os"`) || !strings.Contains(body, `"arch"`) || !strings.Contains(body, `"version"`) {
		t.Errorf("handleInfo body = %q, want os/arch/version fields", body)
	}
}

func TestHandleUnsupportedNeverSucceeds(t *testing.T) {
	handler := handleUnsupported("not supported here")
	req := httptest.NewRequest(http.MethodPost, "/__test/open-path", strings.NewReader("/etc/passwd"))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("handleUnsupported: status = %d, want 501", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not supported here") {
		t.Errorf("handleUnsupported body = %q, want it to explain why", rec.Body.String())
	}
}

func TestListenAddrIsLoopbackOnly(t *testing.T) {
	if !strings.HasPrefix(listenAddr, "127.0.0.1:") {
		t.Errorf("listenAddr = %q, want a 127.0.0.1 address, never a wildcard bind", listenAddr)
	}
}

func TestAllowedOriginsHaveNoWildcard(t *testing.T) {
	for origin := range allowedOrigins {
		if origin == "*" || strings.Contains(origin, "*") {
			t.Errorf("allowedOrigins contains a wildcard entry: %q", origin)
		}
	}
}
