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
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// marker names the temporary home a test binary created. A test that
// re-executes its own binary passes its environment on, so the child finds
// the marker and shares the parent's home instead of making (and, when it is
// killed, leaking) one of its own.
const marker = "TRELLIS_TEST_HOME"

// prefix starts every temporary home's name, followed by the creating
// process's pid: the sweep below needs to tell a home whose test is still
// running from one whose process is gone.
const prefix = "trellis-test-home-"

// staleAfter is how old a leftover home must be before the sweep removes it
// without being able to prove its process has exited. It is far longer than
// any package's tests take -- go test's own default timeout is 10 minutes --
// and only matters where a pid cannot be checked.
const staleAfter = 2 * time.Hour

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
	sweepStale()
	dir, err := os.MkdirTemp("", fmt.Sprintf("%s%d-", prefix, os.Getpid()))
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
//
// An interrupt -- go test's own Ctrl-C, or a harness killing a slow run --
// still removes the home: only SIGKILL escapes, and the next run's sweep
// collects what that leaves. A re-executed binary sharing its parent's home
// keeps the default signal behaviour instead, because a test that
// re-executes itself as a fixture may be testing exactly what the process
// does when it is asked to stop.
func Main(m *testing.M) {
	owns := os.Getenv(marker) == ""
	cleanup := Setup()
	stop := func() {}
	if owns {
		stop = onInterrupt(cleanup)
	}
	code := m.Run()
	stop()
	cleanup()
	os.Exit(code)
}

// onInterrupt runs cleanup and exits when the process is interrupted or
// asked to terminate. The returned function stops watching.
func onInterrupt(cleanup func()) (stop func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-signals:
			cleanup()
			os.Exit(1)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
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

// sweepStale removes temporary homes left behind by test binaries that are
// no longer running. A process killed outright never runs its cleanup, so
// without this the leftovers accumulate until the temp filesystem fills --
// which is exactly what happened on 2026-09-17.
func sweepStale() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		dir := filepath.Join(os.TempDir(), e.Name())
		if !stale(e, dir) {
			continue
		}
		removeAll(dir)
	}
}

// stale reports whether a leftover home's owner is gone. The pid in the
// name answers it directly where a process can be checked; otherwise the
// directory has to be old enough that no run could still be using it.
func stale(e fs.DirEntry, dir string) bool {
	pid, ok := pidOf(e.Name())
	if ok && pid == os.Getpid() {
		return false
	}
	if ok && !processAlive(pid) {
		return true
	}
	info, err := e.Info()
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) > staleAfter
}

// pidOf reads the creating process's pid out of a temporary home's name,
// which is prefix + pid + "-" + the random suffix MkdirTemp adds.
func pidOf(name string) (int, bool) {
	rest := strings.TrimPrefix(name, prefix)
	digits, _, found := strings.Cut(rest, "-")
	if !found {
		return 0, false
	}
	pid, err := strconv.Atoi(digits)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
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
