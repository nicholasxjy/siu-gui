package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// coreBinaryFileName returns the platform-appropriate executable name used
// once the sidecar is extracted into the user cache directory.
func coreBinaryFileName() string {
	if runtime.GOOS == "windows" {
		return "siu-core.exe"
	}
	return "siu-core"
}

// extractEmbeddedCore stages the embedded siu-core binary into
// os.UserCacheDir()/siu-gui/core/<sha256>/ and returns its path.
func extractEmbeddedCore() (string, error) {
	data, err := embeddedCoreBytes()
	if err != nil {
		return "", err
	}
	return extractCoreBytes(data)
}

// extractCoreBytes performs the actual content-addressed, atomic,
// concurrency-safe extraction. Factored out from extractEmbeddedCore so
// tests can exercise the write path with synthetic data.
func extractCoreBytes(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}

	dir := filepath.Join(cacheRoot, "siu-gui", "core", hash)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create core cache directory %s: %w", dir, err)
	}
	_ = os.Chmod(dir, 0o700) // tighten perms even if the directory pre-existed

	dest := filepath.Join(dir, coreBinaryFileName())
	if info, statErr := os.Stat(dest); statErr == nil && !info.IsDir() && info.Size() == int64(len(data)) {
		return dest, nil
	}

	tmp, err := os.CreateTemp(dir, ".siu-core-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file for core extraction in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write core binary to temp file: %w", err)
	}
	if err := tmp.Chmod(0o700); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("chmod temp core binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp core binary: %w", err)
	}

	// os.Rename is atomic on POSIX and uses MoveFileEx with replace-existing
	// semantics on Windows, so concurrent extractions converge safely since
	// the destination path is content-addressed (same hash => same bytes).
	if err := os.Rename(tmpPath, dest); err != nil {
		return "", fmt.Errorf("install core binary to %s: %w", dest, err)
	}
	cleanupTmp = false
	return dest, nil
}
