//go:build windows

package backend

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
)

// configurePlatform hides the console window (no flashing terminal for a
// GUI app) and starts the core in its own process group so it is not
// signaled together with siu-gui itself.
func configurePlatform(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// killProcessTree shells out to taskkill /T to reap the whole process tree;
// Process.Kill alone only signals the immediate process on Windows.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := kill.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill: %w: %s", err, out)
	}
	return nil
}
