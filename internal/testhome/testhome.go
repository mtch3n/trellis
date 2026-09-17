// Package testhome makes a test binary's home hermetic, so a test that
// forgets to isolate itself (a missing root argument to core.New) cannot
// reach the developer's real Trellis storage.
//
// go test runs one process per package, so each package's TestMain gets its
// own temporary home; the isolation is per binary, not per test.
package testhome

import (
	"fmt"
	"os"
	"testing"
)

// Setup creates one unique temporary directory and points HOME (and, for
// Windows, USERPROFILE and LOCALAPPDATA) at it, and unsets TRELLIS_HOME, so
// the storage root's default-root logic resolves inside the temporary
// directory instead of the caller's real home. It returns a cleanup function
// that removes the directory.
//
// A package with its own additional TestMain setup calls Setup directly and
// folds the returned cleanup into its own; a package with nothing else to do
// calls Main instead.
func Setup() (cleanup func()) {
	dir, err := os.MkdirTemp("", "trellis-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Unsetenv("TRELLIS_HOME")
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)
	os.Setenv("LOCALAPPDATA", dir)
	return func() { os.RemoveAll(dir) }
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
