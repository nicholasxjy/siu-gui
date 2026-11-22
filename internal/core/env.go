// Package core implements the siu MCP & Skills management engine in pure Go.
package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const TestHomeVar = "SIU_GUI_TEST_HOME"

// Env describes where user files and agent overrides live on this machine.
type Env struct {
	Home        string
	Config      string
	ClaudeDir   string
	CodexHome   string
	PiDir       string
	GrokHome    string
	GeminiHome  string
	OpenCodeDir string
	XDGConfig   string
	CopilotHome string
}

// Resolved holds the active environment and siu library root.
type Resolved struct {
	Env  Env
	Root string
}

// EnvFromSystem builds Env from the current user's system directories and
// per-agent environment overrides.
func EnvFromSystem() Env {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	config, err := os.UserConfigDir()
	if err != nil || config == "" {
		config = filepath.Join(home, ".config")
	}
	piDir := os.Getenv("PI_CODING_AGENT_DIR")
	if piDir != "" {
		if piDir == "~" {
			piDir = home
		} else if rest, ok := strings.CutPrefix(piDir, "~/"); ok {
			piDir = filepath.Join(home, rest)
		} else if rest, ok := strings.CutPrefix(piDir, `~\`); ok {
			piDir = filepath.Join(home, rest)
		}
	}
	return Env{
		Home:        home,
		Config:      config,
		ClaudeDir:   os.Getenv("CLAUDE_CONFIG_DIR"),
		CodexHome:   os.Getenv("CODEX_HOME"),
		PiDir:       piDir,
		GrokHome:    os.Getenv("GROK_HOME"),
		GeminiHome:  os.Getenv("GEMINI_CLI_HOME"),
		OpenCodeDir: os.Getenv("OPENCODE_CONFIG_DIR"),
		XDGConfig:   os.Getenv("XDG_CONFIG_HOME"),
		CopilotHome: os.Getenv("COPILOT_HOME"),
	}
}

// EnvRooted builds an isolated Env rooted completely under home, ignoring all
// system per-agent directory overrides.
func EnvRooted(home string) Env {
	return Env{
		Home:   home,
		Config: filepath.Join(home, ".config"),
	}
}

// Resolve computes the active Env and library root. When SIU_GUI_TEST_HOME is
// set, every real per-agent override and SIU_HOME are ignored.
func Resolve() Resolved {
	if testHome := os.Getenv(TestHomeVar); testHome != "" {
		_ = os.MkdirAll(testHome, 0o700)
		env := EnvRooted(testHome)
		root := filepath.Join(testHome, ".siu")
		hardenDir(testHome)
		hardenDir(root)
		return Resolved{Env: env, Root: root}
	}
	env := EnvFromSystem()
	root := os.Getenv("SIU_HOME")
	if root == "" {
		root = filepath.Join(env.Home, ".siu")
	}
	hardenDir(root)
	return Resolved{Env: env, Root: root}
}

func hardenDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o700)
}

func platformName() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}
