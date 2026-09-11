//go:build !windows

package daemon

import (
	"net"
	"os"
	"path/filepath"
)

func Endpoint(root string) string { return filepath.Join(root, "daemon.sock") }

func Listen(root string) (net.Listener, func(), error) {
	path := Endpoint(root)
	// The daemon lock guarantees that an old endpoint cannot belong to a live
	// Trellis daemon in this storage root.
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = l.Close()
		_ = os.Remove(path)
		return nil, nil, err
	}
	return l, func() { _ = l.Close(); _ = os.Remove(path) }, nil
}
