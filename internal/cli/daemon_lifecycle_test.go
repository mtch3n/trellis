package cli

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestDaemonLifecycle drives spawn, detect and stop against a real daemon
// process. The unit tests cover the parsing; only this proves the detached
// child actually starts, answers on its socket, and dies on request.
func TestDaemonLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real daemon process")
	}
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	// spawnDaemon re-executes os.Executable, which under `go test` is the test
	// binary; this drives the same code paths against a real CLI build instead.
	binary := buildTrellis(t)

	// Use port 0 to let the OS assign an available port, eliminating race
	// conditions where another process grabs the port between picking and binding
	cmd := exec.Command(binary, "daemon", "--bind", "127.0.0.1", "--port", "0")
	cmd.Env = append(os.Environ(), "TRELLIS_HOME="+root)
	logFile, err := os.Create(filepath.Join(root, "daemon.log"))
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer logFile.Close()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	// Reap the child the moment it exits, in a goroutine rather than in
	// Cleanup: on Unix, a process that has exited but not yet been reaped is
	// a zombie, and a zombie's PID still answers kill(pid, 0) as alive. Since
	// this test process is the child's parent, stopSelfManaged's new
	// exit-polling (readDaemonPID -> processAlive) would see that zombie as
	// still running until something calls Wait, and Cleanup does not run
	// until after the test body — including every stopSelfManaged call in
	// it — has already finished.
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

	ctx := context.Background()
	if err := waitForDaemon(ctx, root, true); err != nil {
		body, _ := os.ReadFile(filepath.Join(root, "daemon.log"))
		t.Fatalf("daemon never answered: %v\n%s", err, body)
	}

	url, alive := daemonPing(ctx, root)
	if !alive || url == "" {
		t.Fatalf("ping returned %q %v", url, alive)
	}
	got := servingAddress(url)
	host, port, err := net.SplitHostPort(got)
	if err != nil {
		t.Fatalf("invalid address %q: %v", got, err)
	}
	if host != "127.0.0.1" {
		t.Errorf("daemon is serving on %q, want 127.0.0.1", host)
	}
	if port == "0" {
		t.Errorf("daemon is serving on port 0, should have been assigned an actual port")
	}

	status, err := resolveDaemonStatus(ctx)
	if err != nil {
		t.Fatalf("resolveDaemonStatus: %v", err)
	}
	// The service manager may genuinely have a unit installed on a developer
	// machine; only the self-managed case is this test's business.
	if !status.Service.Installed {
		if status.ManagedBy != managedBySelf || status.PID != cmd.Process.Pid {
			t.Errorf("want self-managed pid %d, got %s pid %d", cmd.Process.Pid, status.ManagedBy, status.PID)
		}
	}
	if !status.Running {
		t.Error("status must report a responding daemon as running")
	}

	if err := stopSelfManaged(ctx, root); err != nil {
		t.Fatalf("stopSelfManaged: %v", err)
	}
	if _, alive := daemonPing(ctx, root); alive {
		t.Error("daemon still answering after stop")
	}
	if _, err := os.Stat(daemonPIDPath(root)); !os.IsNotExist(err) {
		t.Error("stop must remove the pidfile")
	}
	if err := stopSelfManaged(ctx, root); err == nil {
		t.Error("stopping an already-stopped daemon must report an error")
	}
}

func buildTrellis(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "trellis")
	if os.PathSeparator == '\\' {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "github.com/mtch3n/trellis/cmd/trellis")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build trellis: %v\n%s", err, out)
	}
	return binary
}
