//go:build !linux && !darwin

package service

// New returns a manager whose every method reports ErrUnsupported. Windows has
// no per-user service manager Trellis drives; `daemon start` there falls back
// to a self-managed child process.
func New() Manager { return unsupported{} }
