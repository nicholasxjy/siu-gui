package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var fakeCoreBinary string

func TestMain(m *testing.M) {
	bin, cleanup, err := buildFakeCore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "build fakecore helper:", err)
		os.Exit(1)
	}
	fakeCoreBinary = bin
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func buildFakeCore() (string, func(), error) {
	srcDir, err := filepath.Abs("testdata/fakecore")
	if err != nil {
		return "", nil, err
	}
	tmpDir, err := os.MkdirTemp("", "siu-gui-fakecore-")
	if err != nil {
		return "", nil, err
	}
	out := filepath.Join(tmpDir, "fakecore"+exeExt())

	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = srcDir
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("%w: %s", err, output)
	}
	return out, func() { os.RemoveAll(tmpDir) }, nil
}

func exeExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func TestInvalidPayloadRejectedWithoutStartingProcess(t *testing.T) {
	c := New()
	defer c.Close()

	if _, err := c.Request(context.Background(), `{"params":{}}`); err == nil {
		t.Fatal("expected error for missing method")
	}
	if _, err := c.Request(context.Background(), `not-json`); err == nil {
		t.Fatal("expected error for invalid JSON payload")
	}
}

func TestConcurrentRequestsDispatchByID(t *testing.T) {
	t.Setenv("SIU_GUI_CORE", fakeCoreBinary)
	t.Setenv("FAKE_CORE_MODE", "echo")

	c := New()
	defer c.Close()

	const n = 25
	var wg sync.WaitGroup
	errs := make([]error, n)
	results := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := fmt.Sprintf(`{"method":"ping","params":{"n":%d}}`, i)
			res, err := c.Request(context.Background(), payload)
			errs[i] = err
			results[i] = res
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("request %d failed: %v", i, errs[i])
		}
		var resp struct {
			OK   bool `json:"ok"`
			Data struct {
				Method string `json:"method"`
				Params struct {
					N int `json:"n"`
				} `json:"params"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(results[i]), &resp); err != nil {
			t.Fatalf("unmarshal result %d (%s): %v", i, results[i], err)
		}
		if !resp.OK || resp.Data.Method != "ping" || resp.Data.Params.N != i {
			t.Fatalf("request %d got mismatched response: %s", i, results[i])
		}
	}
}

func TestProcessCrashFailsPendingThenRestartSucceeds(t *testing.T) {
	t.Setenv("SIU_GUI_CORE", fakeCoreBinary)
	t.Setenv("FAKE_CORE_MODE", "crash-once")
	t.Setenv("FAKE_CORE_STATEFILE", filepath.Join(t.TempDir(), "state"))

	c := New()
	defer c.Close()

	_, err := c.Request(context.Background(), `{"method":"ping","params":{}}`)
	if err == nil {
		t.Fatal("expected error from a process that crashes without responding")
	}
	if !strings.Contains(err.Error(), "exited unexpectedly") {
		t.Fatalf("expected explicit unexpected-exit error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "refresh state") {
		t.Fatalf("expected no-auto-retry/refresh guidance in error, got: %v", err)
	}

	res, err := c.Request(context.Background(), `{"method":"ping","params":{"after":"restart"}}`)
	if err != nil {
		t.Fatalf("expected automatic restart on next request to succeed, got: %v", err)
	}
	if !strings.Contains(res, `"after":"restart"`) {
		t.Fatalf("unexpected response after restart: %s", res)
	}
}

func TestRequestCancellationDoesNotClaimRollback(t *testing.T) {
	t.Setenv("SIU_GUI_CORE", fakeCoreBinary)
	t.Setenv("FAKE_CORE_MODE", "hang")

	c := New()
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := c.Request(ctx, `{"method":"slow_mutation","params":{}}`)
	if err == nil {
		t.Fatal("expected a cancellation/timeout error")
	}
	if !strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("expected ambiguous-outcome guidance in error, got: %v", err)
	}
}

func TestAbandonedResponseAfterCancelIsDiscardedSafely(t *testing.T) {
	t.Setenv("SIU_GUI_CORE", fakeCoreBinary)
	t.Setenv("FAKE_CORE_MODE", "slow-echo")
	t.Setenv("FAKE_CORE_DELAY_MS", "150")

	c := New()
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Request(ctx, `{"method":"slow","params":{}}`); err == nil {
		t.Fatal("expected request to time out locally before the slow response arrives")
	}

	// Give the fake core time to flush its now-abandoned response; the
	// dispatcher must drop it instead of panicking or blocking.
	time.Sleep(250 * time.Millisecond)

	res, err := c.Request(context.Background(), `{"method":"after","params":{}}`)
	if err != nil {
		t.Fatalf("client should remain usable after an abandoned response: %v", err)
	}
	if !strings.Contains(res, `"method":"after"`) {
		t.Fatalf("unexpected response: %s", res)
	}
}

func TestCloseReapsHangingProcessWithinBoundedTime(t *testing.T) {
	t.Setenv("SIU_GUI_CORE", fakeCoreBinary)
	t.Setenv("FAKE_CORE_MODE", "stubborn")

	orig := shutdownGraceTimeout
	shutdownGraceTimeout = 50 * time.Millisecond
	defer func() { shutdownGraceTimeout = orig }()

	c := New()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, _ = c.Request(ctx, `{"method":"noop","params":{}}`) // starts the process; expected to time out locally

	done := make(chan error, 1)
	go func() { done <- c.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return in time; process tree may not have been reaped")
	}
}

func TestCloseIsIdempotentAndRejectsFurtherRequests(t *testing.T) {
	t.Setenv("SIU_GUI_CORE", fakeCoreBinary)
	t.Setenv("FAKE_CORE_MODE", "echo")

	c := New()
	if _, err := c.Request(context.Background(), `{"method":"ping","params":{}}`); err != nil {
		t.Fatalf("warm-up request failed: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close should be a no-op, got: %v", err)
	}
	if _, err := c.Request(context.Background(), `{"method":"ping","params":{}}`); err == nil {
		t.Fatal("expected Request after Close to fail")
	}
}

func TestCloseBeforeLazyStartDoesNotLeakProcess(t *testing.T) {
	t.Setenv("SIU_GUI_CORE", fakeCoreBinary)
	t.Setenv("FAKE_CORE_MODE", "echo")
	c := New()
	entered, resume := make(chan struct{}), make(chan struct{})
	c.ensureStartedHook = func() { close(entered); <-resume }
	done := make(chan error, 1)
	go func() {
		_, err := c.Request(context.Background(), `{"method":"ping"}`)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach lazy-start boundary")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	close(resume)
	select {
	case err := <-done:
		if err != errClientClosed {
			t.Fatalf("want closed error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request did not stop after Close")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc != nil {
		t.Fatal("process started after Close returned")
	}
}

func TestCloseReturnsWhenProcessTerminationFails(t *testing.T) {
	t.Setenv("SIU_GUI_CORE", fakeCoreBinary)
	t.Setenv("FAKE_CORE_MODE", "stubborn")
	origKill, origGrace := killProcessTreeFunc, shutdownGraceTimeout
	killProcessTreeFunc = func(*exec.Cmd) error { return fmt.Errorf("simulated termination failure") }
	shutdownGraceTimeout = 10 * time.Millisecond
	defer func() { killProcessTreeFunc = origKill; shutdownGraceTimeout = origGrace }()
	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, _ = c.Request(ctx, `{"method":"ping"}`)
	p := c.proc
	if p == nil {
		t.Fatal("expected a started process")
	}
	t.Cleanup(func() {
		_ = killProcessTree(p.cmd)
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			t.Error("process was not reaped after test cleanup")
		}
	})
	done := make(chan error, 1)
	go func() { done <- c.Close() }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "simulated termination failure") {
			t.Fatalf("expected termination error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close blocked after process termination failed")
	}
}

func TestResolveCoreExecutablePriorityOrder(t *testing.T) {
	dir := t.TempDir()
	resourcesPath := filepath.Join(dir, "resources-core")
	if err := os.WriteFile(resourcesPath, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := resolveCoreExecutable(resourcesPath)
	if err != nil || got != resourcesPath {
		t.Fatalf("expected explicit resources path %q, got %q err %v", resourcesPath, got, err)
	}

	envPath := filepath.Join(dir, "env-core")
	if err := os.WriteFile(envPath, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIU_GUI_CORE", envPath)
	got, err = resolveCoreExecutable(resourcesPath)
	if err != nil || got != envPath {
		t.Fatalf("expected SIU_GUI_CORE override %q to win, got %q err %v", envPath, got, err)
	}
}

func TestResolveCoreExecutableMissingExplicitPathErrors(t *testing.T) {
	_, err := resolveCoreExecutable(filepath.Join(t.TempDir(), "missing-core"))
	if err == nil {
		t.Fatal("expected error for a missing explicit resources binary")
	}
}

func TestEmbeddedCoreMissingGivesActionableInstruction(t *testing.T) {
	if _, err := embeddedBundle.ReadFile("bundle/" + embeddedBinaryName); err == nil {
		t.Skip("bundle/siu-core.bin has been staged; missing-binary guidance no longer exercised")
	}
	_, err := embeddedCoreBytes()
	if err == nil {
		t.Fatal("expected error because bundle/siu-core.bin has not been generated yet")
	}
	if !strings.Contains(err.Error(), "bun run prepare:core") {
		t.Fatalf("expected actionable instruction, got: %v", err)
	}
}

func TestExtractCoreBytesAtomicAndConcurrencySafe(t *testing.T) {
	setFakeUserCacheDir(t, t.TempDir())
	data := []byte("fake-core-binary-contents-for-testing")

	path1, err := extractCoreBytes(data)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	info, err := os.Stat(path1)
	if err != nil {
		t.Fatalf("stat extracted binary: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("expected 0700 perms, got %v", info.Mode().Perm())
	}

	var wg sync.WaitGroup
	paths := make([]string, 8)
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := extractCoreBytes(data)
			if err != nil {
				t.Errorf("concurrent extract %d: %v", i, err)
				return
			}
			paths[i] = p
		}(i)
	}
	wg.Wait()

	for i, p := range paths {
		if p != path1 {
			t.Fatalf("concurrent extract %d produced a different path: %s vs %s", i, p, path1)
		}
	}
	got, err := os.ReadFile(path1)
	if err != nil {
		t.Fatalf("read back extracted binary: %v", err)
	}
	if string(got) != string(data) {
		t.Fatal("content mismatch after concurrent extraction")
	}
}

func setFakeUserCacheDir(t *testing.T, dir string) {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LocalAppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CACHE_HOME", dir)
	}
}
