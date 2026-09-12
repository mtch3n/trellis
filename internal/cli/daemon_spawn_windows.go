package cli

import (
	"os"
	"os/exec"
	"syscall"
)

// detach gives the daemon its own process group and no console, so closing the
// window that started it does not take it down.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x08000000, // DETACHED_PROCESS
	}
}

// processAlive reports whether a PID names a live process. On Windows
// FindProcess only succeeds for a process that exists.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	defer proc.Release()
	var code uint32
	if err := syscall.GetExitCodeProcess(syscall.Handle(proc.Pid), &code); err != nil {
		// The handle from FindProcess is enough to prove existence.
		return true
	}
	const stillActive = 259
	return code == stillActive
}

// terminate stops the daemon. Windows has no SIGTERM for another process, so
// this is not the clean shutdown the unix path gets: the daemon's deferred
// socket and lock cleanup does not run, and the next start clears both.
func terminate(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	defer proc.Release()
	return proc.Kill()
}
