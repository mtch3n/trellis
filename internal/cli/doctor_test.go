package cli

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/service"
)

func TestCheckWebUI(t *testing.T) {
	enabled := config.Defaults()
	disabled := config.Defaults()
	off := false
	disabled.UI.Enabled = &off

	running := daemonStatus{Running: true, URL: "http://127.0.0.1:7788/?token=abc"}
	ipcOnly := daemonStatus{Running: true}

	if got := checkWebUI(running, enabled); got.Status != checkOK {
		t.Errorf("enabled and served should pass, got %+v", got)
	}
	if got := checkWebUI(daemonStatus{}, enabled); got.Status != checkOK {
		t.Errorf("enabled with no daemon should pass, got %+v", got)
	}
	if got := checkWebUI(ipcOnly, disabled); got.Status != checkOK {
		t.Errorf("disabled and not served should pass, got %+v", got)
	}
	// The two states a user cannot explain from either setting alone.
	if got := checkWebUI(ipcOnly, enabled); got.Status != checkWarn || got.Fix != "trellis daemon restart" {
		t.Errorf("enabled but not served should warn, got %+v", got)
	}
	if got := checkWebUI(running, disabled); got.Status != checkWarn || got.Fix != "trellis daemon restart" {
		t.Errorf("disabled but still served should warn, got %+v", got)
	}
}

func TestCheckPortIgnoredWhenUIIsOff(t *testing.T) {
	cfg := config.Defaults()
	off := false
	cfg.UI.Enabled = &off
	if got := checkPort(daemonStatus{}, cfg); got.Status != checkOK {
		t.Errorf("no port is bound with the UI off, got %+v", got)
	}
}

func TestCheckPortDetectsAConflict(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	host, port, _ := net.SplitHostPort(listener.Addr().String())

	cfg := config.Defaults()
	cfg.UI.Bind = host
	cfg.UI.Port = listener.Addr().(*net.TCPAddr).Port
	got := checkPort(daemonStatus{Running: false}, cfg)
	if got.Status != checkFail || !strings.Contains(got.Detail, port) {
		t.Errorf("an occupied port should fail, got %+v", got)
	}

	listener.Close()
	if got := checkPort(daemonStatus{Running: false}, cfg); got.Status != checkOK {
		t.Errorf("a free port should pass, got %+v", got)
	}
}

func TestCheckPortWarnsOnConfigDrift(t *testing.T) {
	cfg := config.Defaults()
	status := daemonStatus{Running: true, URL: "http://127.0.0.1:9999/?token=abc"}
	got := checkPort(status, cfg)
	if got.Status != checkWarn || !strings.Contains(got.Detail, "9999") {
		t.Errorf("a daemon on a different port than config should warn, got %+v", got)
	}

	status.URL = "http://127.0.0.1:7788/?token=abc"
	if got := checkPort(status, cfg); got.Status != checkOK {
		t.Errorf("a daemon on the configured port should pass, got %+v", got)
	}
}

func TestServingAddress(t *testing.T) {
	if got := servingAddress("http://127.0.0.1:7788/?token=abc"); got != "127.0.0.1:7788" {
		t.Errorf("want 127.0.0.1:7788, got %q", got)
	}
	if got := servingAddress("://nonsense"); got != "" {
		t.Errorf("an unparseable URL should yield empty, got %q", got)
	}
}

func TestCheckServiceReportsAMissingBinary(t *testing.T) {
	status := daemonStatus{Service: service.State{
		Installed: true, Enabled: true, ManagedBy: "systemd",
		UnitPath: "/tmp/trellis.service", Exec: filepath.Join(t.TempDir(), "gone"),
	}}
	got := checkService(status)
	if got.Status != checkFail || got.Fix != "trellis daemon install" {
		t.Errorf("a unit pointing at a deleted binary must fail, got %+v", got)
	}
}

func TestCheckServiceReportsExecDrift(t *testing.T) {
	other := filepath.Join(t.TempDir(), "trellis")
	write(t, other, "")
	status := daemonStatus{Service: service.State{
		Installed: true, Enabled: true, ManagedBy: "systemd",
		UnitPath: "/tmp/trellis.service", Exec: other,
	}}
	got := checkService(status)
	if got.Status != checkWarn || !strings.Contains(got.Detail, other) {
		t.Errorf("a unit running a different binary should warn, got %+v", got)
	}
}

func TestCheckServiceNotInstalled(t *testing.T) {
	got := checkService(daemonStatus{})
	if service.New().Name() == "" {
		if got.Status != checkOK {
			t.Errorf("an unsupported platform should not warn, got %+v", got)
		}
		return
	}
	if got.Status != checkWarn || got.Fix != "trellis daemon install" {
		t.Errorf("want a warn pointing at install, got %+v", got)
	}
}

func TestCheckProjectFromAMarker(t *testing.T) {
	dir := markerEnv(t, "app")
	seedProject(t, "APP")
	writeMarker(t, dir, "/APP\n")
	got := checkProject()
	if got.Status != checkOK || !strings.Contains(got.Detail, "/APP (marker ") {
		t.Errorf("check = %+v", got)
	}
}

func TestCheckProjectWithoutAMarkerWarns(t *testing.T) {
	markerEnv(t, "loose")
	got := checkProject()
	if got.Status != checkWarn || got.Fix != "trellis init --key <KEY>" {
		t.Errorf("check = %+v", got)
	}
}

func TestCheckProjectNamesItsSource(t *testing.T) {
	markerEnv(t, "loose")
	t.Setenv("TRELLIS_PROJECT", "envkey")
	if got := checkProject(); !strings.Contains(got.Detail, "ENVKEY (from TRELLIS_PROJECT)") {
		t.Errorf("env: %+v", got)
	}
	t.Setenv("TRELLIS_PROJECT", "/envkey")
	if got := checkProject(); !strings.Contains(got.Detail, "ENVKEY (from TRELLIS_PROJECT)") {
		t.Errorf("env address: %+v", got)
	}
	projectFlagKey = "flagkey"
	t.Cleanup(func() { projectFlagKey = "" })
	if got := checkProject(); !strings.Contains(got.Detail, "FLAGKEY (from --project)") {
		t.Errorf("flag: %+v", got)
	}
}

func TestCheckProjectWarnsWhenTheMarkersProjectIsMissing(t *testing.T) {
	dir := markerEnv(t, "clone")
	seedProject(t, "OTHER")
	writeMarker(t, dir, "/GHOST\n")
	got := checkProject()
	if got.Status != checkWarn || got.Fix != "trellis init" {
		t.Errorf("check = %+v", got)
	}
}

// A fresh clone: the marker is committed, and this machine has no database yet.
func TestCheckProjectWithoutADatabaseWarns(t *testing.T) {
	dir := markerEnv(t, "clone")
	writeMarker(t, dir, "/APP\n")
	got := checkProject()
	if got.Status != checkWarn || got.Fix != "trellis init" || !strings.Contains(got.Detail, "no database here yet") {
		t.Errorf("check = %+v", got)
	}
	path, err := home.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the check created a database: %v", err)
	}
}

func TestRunDoctorCoversEveryAreaAndNeverPanics(t *testing.T) {
	t.Setenv("TRELLIS_HOME", t.TempDir())
	checks := runDoctor(context.Background())
	seen := map[string]bool{}
	for _, c := range checks {
		seen[c.Name] = true
		switch c.Status {
		case checkOK, checkWarn, checkFail:
		default:
			t.Errorf("check %q has unknown status %q", c.Name, c.Status)
		}
	}
	for _, name := range []string{"binary", "storage root", "database", "config", "daemon", "auto-start", "web ui", "http port", "project", "project keys", "vector search"} {
		if !seen[name] {
			t.Errorf("doctor did not report a %q check", name)
		}
	}
}

func TestDoctorTableShowsFixesAndAligns(t *testing.T) {
	out := doctorTable([]Check{
		ok("binary", "/usr/bin/trellis"),
		warn("daemon", "not running", "trellis daemon start"),
		fail("http port", "in use", "trellis config set ui.port 7789"),
	})
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("want 3 checks plus 2 fix lines, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "ok  ") || !strings.HasPrefix(lines[3], "FAIL") {
		t.Errorf("status column is wrong:\n%s", out)
	}
	if !strings.Contains(out, "-> trellis daemon start") {
		t.Errorf("fix line missing:\n%s", out)
	}
	// Every detail must start at the same column regardless of name length.
	column := strings.Index(lines[0], "/usr/bin/trellis")
	if got := strings.Index(lines[3], "in use"); got != column {
		t.Errorf("details are not aligned: %d vs %d\n%s", column, got, out)
	}
}

func TestErrExitCarriesACodeAndPrintsNothing(t *testing.T) {
	// doctor signals its exit code through errExit; run() prints the report
	// itself, so the error must contribute no second line of output.
	var err error = errExit{code: 1}
	if err.Error() != "" {
		t.Errorf("errExit must print nothing, got %q", err.Error())
	}
	exit, isExit := errors.AsType[errExit](err)
	if !isExit || exit.code != 1 {
		t.Errorf("run() must recover the code, got %d %v", exit.code, isExit)
	}
}
