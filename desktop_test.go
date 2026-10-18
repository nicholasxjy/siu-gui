package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRequestTimeout(t *testing.T) {
	cases := []struct {
		method string
		want   time.Duration
	}{
		{"snapshot", defaultRequestTimeout},
		{"save_server", defaultRequestTimeout},
		{"set_agents", defaultRequestTimeout},
		{"set_enabled", defaultRequestTimeout},
		{"delete", defaultRequestTimeout},
		{"import", defaultRequestTimeout},
		{"skill_content", defaultRequestTimeout},
		{"release_scan", defaultRequestTimeout},
		{"", defaultRequestTimeout},
		{"unknown_method", defaultRequestTimeout},
		{"discover", discoverRequestTimeout},
		{"scan_skills", downloadRequestTimeout},
		{"install_skills", downloadRequestTimeout},
		{"install_server", downloadRequestTimeout},
		{"update", updateRequestTimeout},
	}
	for _, c := range cases {
		if got := requestTimeout(c.method); got != c.want {
			t.Errorf("requestTimeout(%q) = %v, want %v", c.method, got, c.want)
		}
	}

	// The timeout tiers must stay ordered the way docs/protocol.md's
	// network behavior demands: plain library writes are fastest, then
	// discovery (a market search), then downloads (clone/install), then
	// updates (potentially several downloads in a row).
	if !(defaultRequestTimeout < discoverRequestTimeout &&
		discoverRequestTimeout < downloadRequestTimeout &&
		downloadRequestTimeout <= updateRequestTimeout) {
		t.Errorf("timeout tiers not ordered: default=%v discover=%v download=%v update=%v",
			defaultRequestTimeout, discoverRequestTimeout, downloadRequestTimeout, updateRequestTimeout)
	}
	if discoverRequestTimeout < 90*time.Second {
		t.Errorf("discoverRequestTimeout = %v, want >= 90s", discoverRequestTimeout)
	}
	if downloadRequestTimeout < 3*time.Minute {
		t.Errorf("downloadRequestTimeout = %v, want >= 3m", downloadRequestTimeout)
	}
	if updateRequestTimeout < 5*time.Minute {
		t.Errorf("updateRequestTimeout = %v, want >= 5m", updateRequestTimeout)
	}
}

func TestResolveCoreExecutablePathEmpty(t *testing.T) {
	if got := resolveCoreExecutablePath(""); got != "" {
		t.Errorf("resolveCoreExecutablePath(\"\") = %q, want \"\"", got)
	}
}

func TestResolveCoreExecutablePathMissing(t *testing.T) {
	dir := t.TempDir()
	if got := resolveCoreExecutablePath(dir); got != "" {
		t.Errorf("resolveCoreExecutablePath(%q) = %q, want \"\" (nothing staged)", dir, got)
	}
}

func TestResolveCoreExecutablePathFlattened(t *testing.T) {
	// The layout `mygo build`/`mygo dev` actually ship: resources/bin/.
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := coreExecutableName()
	path := filepath.Join(binDir, name)
	if err := os.WriteFile(path, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveCoreExecutablePath(dir); got != path {
		t.Errorf("resolveCoreExecutablePath(%q) = %q, want %q", dir, got, path)
	}
}

func TestResolveCoreExecutablePathPlatformSubdir(t *testing.T) {
	// The layout `go run`/`go test` see: resources/<os>-<arch>/bin/, left
	// exactly as the project has it under resources/.
	dir := t.TempDir()
	platformBinDir := filepath.Join(dir, runtime.GOOS+"-"+runtime.GOARCH, "bin")
	if err := os.MkdirAll(platformBinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := coreExecutableName()
	path := filepath.Join(platformBinDir, name)
	if err := os.WriteFile(path, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveCoreExecutablePath(dir); got != path {
		t.Errorf("resolveCoreExecutablePath(%q) = %q, want %q", dir, got, path)
	}
}

func TestResolveCoreExecutablePathPrefersFlattened(t *testing.T) {
	dir := t.TempDir()
	name := coreExecutableName()

	flatBinDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(flatBinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	flatPath := filepath.Join(flatBinDir, name)
	if err := os.WriteFile(flatPath, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	platformBinDir := filepath.Join(dir, runtime.GOOS+"-"+runtime.GOARCH, "bin")
	if err := os.MkdirAll(platformBinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(platformBinDir, name), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := resolveCoreExecutablePath(dir); got != flatPath {
		t.Errorf("resolveCoreExecutablePath(%q) = %q, want flattened path %q", dir, got, flatPath)
	}
}

func TestResolveCoreExecutablePathIgnoresDirectory(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	name := coreExecutableName()
	// A directory that happens to be named like the binary must not be
	// mistaken for it.
	if err := os.MkdirAll(filepath.Join(binDir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveCoreExecutablePath(dir); got != "" {
		t.Errorf("resolveCoreExecutablePath(%q) = %q, want \"\" (it is a directory)", dir, got)
	}
}

func TestCoreExecutableName(t *testing.T) {
	got := coreExecutableName()
	if runtime.GOOS == "windows" {
		if got != "siu-core.exe" {
			t.Errorf("coreExecutableName() = %q, want siu-core.exe on windows", got)
		}
		return
	}
	if got != "siu-core" {
		t.Errorf("coreExecutableName() = %q, want siu-core", got)
	}
}

func TestDesktopRequestWithoutClient(t *testing.T) {
	d := &Desktop{}
	_, err := d.Request(context.Background(), `{"method":"snapshot","params":{}}`)
	if err == nil {
		t.Fatal("Request with no backend client: want error, got nil")
	}
}

func TestDesktopCloseClientWithoutClient(t *testing.T) {
	// Must not panic when the app quits before WhenReady ever ran (e.g. a
	// very early startup failure).
	d := &Desktop{}
	d.closeClient()
}

func TestDesktopOpenPathRejectsEmpty(t *testing.T) {
	d := &Desktop{}
	if err := d.OpenPath(""); err == nil {
		t.Fatal("OpenPath(\"\"): want error, got nil")
	}
}

func TestDesktopOpenPathRejectsMissing(t *testing.T) {
	d := &Desktop{}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := d.OpenPath(missing); err == nil {
		t.Fatal("OpenPath(missing path): want error, got nil")
	}
}

func TestDesktopOpenURLRejectsNonHTTP(t *testing.T) {
	d := &Desktop{}
	for _, u := range []string{
		"javascript:alert(1)",
		"file:///etc/passwd",
		"ftp://example.com/file",
		"siu-gui://open",
		"not a url",
		"",
	} {
		if err := d.OpenURL(u); err == nil {
			t.Errorf("OpenURL(%q): want error, got nil", u)
		}
	}
}

func TestDesktopSetThemeRejectsUnknown(t *testing.T) {
	d := &Desktop{}
	for _, theme := range []string{"", "Dark", "SYSTEM", "blue", "auto"} {
		if err := d.SetTheme(theme); err == nil {
			t.Errorf("SetTheme(%q): want error, got nil", theme)
		}
	}
}

func TestDesktopInfo(t *testing.T) {
	d := &Desktop{}
	info := d.Info()
	if info.OS != runtime.GOOS {
		t.Errorf("Info().OS = %q, want %q", info.OS, runtime.GOOS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("Info().Arch = %q, want %q", info.Arch, runtime.GOARCH)
	}
}
