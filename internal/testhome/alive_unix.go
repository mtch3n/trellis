//go:build !windows

package testhome

import "syscall"

// processAlive reports whether pid names a running process. Signal 0 checks
// for one without delivering anything; a process owned by someone else
// answers EPERM, which still means it exists.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
