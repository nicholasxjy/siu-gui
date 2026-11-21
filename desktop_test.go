package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nicholasxjy/siu-gui/internal/core"
)

func TestRequestTimeout(t *testing.T) {
	cases := []struct {
		method string
		want   time.Duration
	}{
		{"snapshot", downloadRequestTimeout},
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

func TestDesktopRequestWithoutCore(t *testing.T) {
	d := &Desktop{}
	_, err := d.Request(context.Background(), `{"method":"snapshot","params":{}}`)
	if err == nil {
		t.Fatal("Request with no core service: want error, got nil")
	}
}

func TestDesktopRequestWithInProcessCore(t *testing.T) {
	t.Setenv(core.TestHomeVar, t.TempDir())
	d := &Desktop{}
	d.setCore(core.NewService("0.0.6-test"))
	defer d.closeCore()

	raw, err := d.Request(context.Background(), `{"method":"snapshot","params":{}}`)
	if err != nil {
		t.Fatalf("Request(snapshot): %v", err)
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.OK || resp.Data.Version != "0.0.6-test" {
		t.Fatalf("unexpected response: %s", raw)
	}
}

func TestDesktopCloseCoreWithoutCore(t *testing.T) {
	d := &Desktop{}
	d.closeCore()
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
