// Package testhome makes a test binary's home hermetic, so a test that
// forgets to isolate itself (a missing root argument to core.New) cannot
// reach the developer's real Trellis storage.
//
// go test runs one process per package, so each package's TestMain gets its
// own temporary home; the isolation is per binary, not per test.
package testhome

import (
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// marker names the temporary home a test binary created. A test that
// re-executes its own binary passes its environment on, so the child finds
// the marker and shares the parent's home instead of making (and, when it is
// killed, leaking) one of its own.
const marker = "TRELLIS_TEST_HOME"

// goLocations are the go env values that default to somewhere under HOME. A
// test that runs `go build` would otherwise download a fresh module cache
// into the temporary home, and miss the user's go env file.
var goLocations = []string{"GOPATH", "GOMODCACHE", "GOCACHE", "GOENV"}

// Setup creates one unique temporary directory and points HOME (and, for
// Windows, USERPROFILE and LOCALAPPDATA) at it, and unsets TRELLIS_HOME, so
// the storage root's default-root logic resolves inside the temporary
// directory instead of the caller's real home. The Go toolchain's own
// locations are pinned to their real values first. It returns a cleanup
// function that removes the directory.
//
// A package with its own additional TestMain setup calls Setup directly and
// folds the returned cleanup into its own; a package with nothing else to do
// calls Main instead.
func Setup() (cleanup func()) {
	if os.Getenv(marker) != "" {
		return func() {}
	}
	pinGoLocations()
	dir, err := os.MkdirTemp("", "trellis-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Unsetenv("TRELLIS_HOME")
	os.Setenv(marker, dir)
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)
	os.Setenv("LOCALAPPDATA", dir)
	return func() { removeAll(dir) }
}

// Main runs m under the hermetic home Setup establishes and exits the
// process with its result. It is the whole of most packages' TestMain; a
// package with its own additional setup calls Setup instead, so it can fold
// testhome's cleanup into its own before exiting.
func Main(m *testing.M) {
	cleanup := Setup()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// pinGoLocations exports the go env values that live under HOME, read
// before HOME moves. Without go on PATH nothing can build, so nothing needs
// pinning.
func pinGoLocations() {
	out, err := exec.Command("go", append([]string{"env", "-json"}, goLocations...)...).Output()
	if err != nil {
		return
	}
	var env map[string]string
	if err := json.Unmarshal(out, &env); err != nil {
		return
	}
	for _, name := range goLocations {
		if env[name] != "" {
			os.Setenv(name, env[name])
		}
	}
}

// removeAll removes dir even when it holds read-only files, which the Go
// module cache writes.
func removeAll(dir string) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}
