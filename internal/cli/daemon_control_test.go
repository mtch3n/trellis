package cli

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/daemon"
)

func TestReadDaemonPIDRejectsStaleFile(t *testing.T) {
	root := t.TempDir()
	if _, alive := readDaemonPID(root); alive {
		t.Error("a missing pidfile must not report a live daemon")
	}

	// A PID that has certainly exited: re-run this test binary with a filter
	// that matches no test, so it starts and exits immediately on every OS.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	dead := cmd.Process.Pid
	_ = cmd.Wait()
	write(t, daemonPIDPath(root), strconv.Itoa(dead))
	if pid, alive := readDaemonPID(root); alive {
		t.Errorf("pid %d has exited but reported alive", pid)
	}

	write(t, daemonPIDPath(root), strconv.Itoa(os.Getpid()))
	pid, alive := readDaemonPID(root)
	if !alive || pid != os.Getpid() {
		t.Errorf("want this process reported alive, got %d %v", pid, alive)
	}
}

func TestReadDaemonPIDRejectsGarbage(t *testing.T) {
	for _, content := range []string{"", "   ", "not-a-number", "0", "-1"} {
		root := t.TempDir()
		write(t, daemonPIDPath(root), content)
		if pid, alive := readDaemonPID(root); alive {
			t.Errorf("content %q must not report a live daemon, got %d", content, pid)
		}
	}
}

func TestReadDaemonPIDTolerantOfTrailingNewline(t *testing.T) {
	root := t.TempDir()
	write(t, daemonPIDPath(root), strconv.Itoa(os.Getpid())+"\n")
	if _, alive := readDaemonPID(root); !alive {
		t.Error("a trailing newline must not break pidfile parsing")
	}
}

func TestDaemonDefaultsPreferExplicitFlags(t *testing.T) {
	withConfig(t, "ui:\n  port: 9001\n  bind: 127.0.0.2\n")

	if bind, port := daemonDefaults("", 0); bind != "127.0.0.2" || port != 9001 {
		t.Errorf("config should supply defaults, got %s:%d", bind, port)
	}
	if bind, port := daemonDefaults("127.0.0.9", 4242); bind != "127.0.0.9" || port != 4242 {
		t.Errorf("explicit flags must win, got %s:%d", bind, port)
	}
}

func TestDaemonDefaultsFallBackWhenConfigIsEmpty(t *testing.T) {
	withConfig(t, "board:\n  default_columns: [backlog]\n")
	defaults := config.Defaults()
	if bind, port := daemonDefaults("", 0); bind != defaults.UI.Bind || port != defaults.UI.Port {
		t.Errorf("want built-in defaults %s:%d, got %s:%d", defaults.UI.Bind, defaults.UI.Port, bind, port)
	}
}

func TestDaemonHealthIsFalseWithoutADaemon(t *testing.T) {
	if _, ok := daemonHealth(context.Background(), t.TempDir()); ok {
		t.Error("an empty storage root must not report a healthy daemon")
	}
}

func TestStopSelfManagedWithoutPIDFile(t *testing.T) {
	root := t.TempDir()
	if err := stopSelfManaged(context.Background(), root); err == nil {
		t.Error("stopping with no pidfile must report an error")
	}
}

// TestStopSelfManagedWaitsForProcessExit is a regression test for TRELLIS-37.
// stopSelfManaged used to declare the daemon stopped the moment a single
// daemonHealth probe failed, and daemonHealth carries its own 2-second
// timeout — so a daemon that is merely slow to answer while it shuts down
// under load (the "busy machine" case that made TestDaemonLifecycle flaky
// under parallel load) looked identical to an already-stopped one.
//
// This drives that exact shape deterministically with a real subprocess
// whose health handler always takes longer than the 2-second probe timeout,
// and that only exits a known, fixed interval after receiving the stop
// signal. It confirms the race precondition directly (health already says
// "not running" while the process is provably still alive), then asserts
// stopSelfManaged does not report success until the process actually exits.
//
// The scenario is Unix-only: it needs the daemon to survive briefly past the
// stop request so a health probe can race it, and only the Unix terminate()
// (SIGTERM) is interceptable. On Windows, terminate() is an unconditional
// TerminateProcess (see daemon_spawn_windows.go) with no graceful window to
// race against, so the fixture cannot be built there.
func TestStopSelfManagedWaitsForProcessExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows terminate() is an unconditional TerminateProcess with no interceptable graceful window")
	}
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}

	root := t.TempDir()
	const shutdownDelay = 3 * time.Second

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSlowDaemon$")
	cmd.Env = append(os.Environ(),
		"TRELLIS_SLOW_DAEMON_ROOT="+root,
		"TRELLIS_SLOW_DAEMON_DELAY="+shutdownDelay.String(),
	)
	logFile, err := os.Create(filepath.Join(root, "slow-daemon.log"))
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer logFile.Close()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// Reap the moment it exits, in a goroutine rather than in Cleanup: see
	// the matching comment in TestDaemonLifecycle. A zombie still answers
	// kill(pid, 0) as alive, which would otherwise make stopSelfManaged's
	// exit-poll spin for the full daemonSpawnWait deadline, since Cleanup
	// only runs after this test body (including the stopSelfManaged call
	// below) has already finished.
	waitDone := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(waitDone)
	}()
	t.Cleanup(func() {
		_ = terminate(cmd.Process.Pid)
		<-waitDone
	})
	write(t, daemonPIDPath(root), strconv.Itoa(cmd.Process.Pid))
	waitForEndpoint(t, daemon.Endpoint(root))

	ctx := context.Background()
	if err := terminate(cmd.Process.Pid); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if _, healthy := daemonHealth(ctx, root); healthy {
		t.Fatal("expected the slow handler to time out the health probe")
	}
	if !processAlive(cmd.Process.Pid) {
		t.Fatal("process must still be alive right after its health probe timed out; the fixture is not reproducing the race")
	}

	start := time.Now()
	if err := stopSelfManaged(ctx, root); err != nil {
		t.Fatalf("stopSelfManaged: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("stopSelfManaged returned after %s, too fast to have waited for the daemon's %s shutdown delay", elapsed, shutdownDelay)
	}
	if processAlive(cmd.Process.Pid) {
		t.Error("stopSelfManaged returned success but the process is still alive")
	}
	if _, err := os.Stat(daemonPIDPath(root)); !os.IsNotExist(err) {
		t.Error("stop must remove the pidfile")
	}
}

// TestHelperSlowDaemon is not a real test. TestStopSelfManagedWaitsForProcessExit
// re-executes the test binary with -test.run matching only this name to get a
// real, separate process that behaves like a daemon which answers health
// checks slower than daemonHealth's 2-second probe timeout and takes a
// fixed, known interval to exit once it receives the stop signal. Without
// TRELLIS_SLOW_DAEMON_ROOT set, a normal `go test` run just skips it.
func TestHelperSlowDaemon(t *testing.T) {
	root := os.Getenv("TRELLIS_SLOW_DAEMON_ROOT")
	if root == "" {
		t.Skip("only runs as a helper subprocess")
	}
	delay, err := time.ParseDuration(os.Getenv("TRELLIS_SLOW_DAEMON_DELAY"))
	if err != nil {
		t.Fatalf("parse delay: %v", err)
	}

	listener, cleanup, err := daemon.Listen(root)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer cleanup()
	go func() {
		_ = daemon.Serve(listener, func(context.Context, daemon.Request) (daemon.Response, error) {
			time.Sleep(3 * time.Second) // longer than daemonHealth's 2s probe timeout
			return daemon.Response{OK: true}, nil
		})
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	<-sig
	// Simulate a bounded, slow graceful shutdown: still alive, and still
	// eventually answering, for a while after the stop signal arrives.
	time.Sleep(delay)
}

// waitForEndpoint polls until the daemon's IPC endpoint file exists, so the
// caller does not race the helper process's own startup.
func waitForEndpoint(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper daemon never started listening at %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// withConfig points config.Load at a scratch storage root holding the given
// YAML, so a developer's real settings cannot change the result.
func withConfig(t *testing.T, yaml string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	write(t, filepath.Join(root, "config.yaml"), yaml)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
