package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mtch3n/trellis/internal/config"
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
