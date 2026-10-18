//go:build unix

package backend

import (
	"os/exec"
	"syscall"
)

// configurePlatform puts the core process in its own process group so
// killProcessTree can terminate it and any children it spawns together.
func configurePlatform(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}
