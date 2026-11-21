//go:build !windows

package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// Link points link at target using a symlink on Unix.
func Link(target, link string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("%s: %w", link, err)
	}
	return nil
}

// Unlink removes a siu-created symlink.
func Unlink(p string) error {
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	return nil
}
