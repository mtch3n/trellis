//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// detach puts the daemon in its own session so it survives the terminal that
// started it closing, and so a Ctrl-C aimed at the CLI is not delivered to it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// processAlive reports whether a PID names a live process. Signal 0 performs
// the permission and existence checks without delivering anything.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// terminate asks the daemon to shut down cleanly; it handles SIGTERM.
func terminate(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
