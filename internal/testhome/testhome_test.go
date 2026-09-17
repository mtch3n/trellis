package testhome

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { Main(m) }

func TestGoLocationsStayOutsideTheTemporaryHome(t *testing.T) {
	home := os.Getenv("HOME")
	for _, name := range goLocations {
		if v := os.Getenv(name); v != "" && strings.HasPrefix(v, home) {
			t.Errorf("%s = %s is inside the temporary home %s", name, v, home)
		}
	}
	if os.Getenv("GOMODCACHE") == "" {
		t.Error("GOMODCACHE was not pinned")
	}
}

func TestAReexecutedBinarySharesTheHome(t *testing.T) {
	home := os.Getenv("HOME")
	cleanup := Setup()
	cleanup()
	if got := os.Getenv("HOME"); got != home {
		t.Errorf("a nested Setup moved HOME from %s to %s", home, got)
	}
	if _, err := os.Stat(home); err != nil {
		t.Errorf("a nested cleanup removed the shared home: %v", err)
	}
}

func TestRemoveAllRemovesReadOnlyTrees(t *testing.T) {
	dir, err := os.MkdirTemp("", "testhome-readonly-")
	if err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(dir, "pkg", "mod")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "go.mod"), []byte("module x\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{inner, filepath.Dir(inner)} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	removeAll(dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("%s survived: %v", dir, err)
	}
}

// A test binary that is killed never runs its cleanup. The next run has to
// collect what it left, or the temp filesystem fills up over a day's work.
func TestSweepRemovesAHomeWhoseProcessIsGone(t *testing.T) {
	dead := deadPID(t)
	gone, err := os.MkdirTemp("", prefix+strconv.Itoa(dead)+"-")
	if err != nil {
		t.Fatal(err)
	}
	mine, err := os.MkdirTemp("", prefix+strconv.Itoa(os.Getpid())+"-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mine)
	fresh, err := os.MkdirTemp("", "trellis-unrelated-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(fresh)

	sweepStale()

	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		os.RemoveAll(gone)
		t.Errorf("a home whose process exited survived: %v", err)
	}
	for _, keep := range []string{mine, fresh, os.Getenv("HOME")} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s was swept: %v", keep, err)
		}
	}
}

// deadPID returns the pid of a process that has certainly exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()
	if processAlive(pid) {
		t.Skip("the pid was reused immediately")
	}
	return pid
}

func TestPidOfReadsTheCreatingProcess(t *testing.T) {
	if pid, ok := pidOf(prefix + "4321-XYZ"); !ok || pid != 4321 {
		t.Errorf("pidOf = %d, %v; want 4321, true", pid, ok)
	}
	if _, ok := pidOf("trellis-test-home-nodigits"); ok {
		t.Error("a name without a pid must not parse")
	}
}
