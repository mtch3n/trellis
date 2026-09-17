package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/doctor"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/service"
	"github.com/mtch3n/trellis/internal/store"
	"github.com/spf13/cobra"
)

// Check and its outcomes are internal/doctor's, so `trellis doctor` and the
// web UI's diagnostics report the same shape. The CLI adds only the checks
// that depend on it: the daemon, the service manager, and the project the
// working directory resolves to.
type Check = doctor.Check

const (
	checkOK   = doctor.StatusOK
	checkWarn = doctor.StatusWarn
	checkFail = doctor.StatusFail
)

func ok(name, detail string) Check        { return doctor.OK(name, detail) }
func warn(name, detail, fix string) Check { return doctor.Warn(name, detail, fix) }
func fail(name, detail, fix string) Check { return doctor.Fail(name, detail, fix) }

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check this Trellis installation",
		Long: "Check the binary, storage root, database, configuration, daemon and search\n" +
			"backend, and report what to run for anything that is wrong.\n" +
			"Exits 1 when a check fails; warnings alone exit 0.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := runDoctor(cmd.Context())
			failed := 0
			for _, c := range checks {
				if c.Status == checkFail {
					failed++
				}
			}
			if err := Emit(cmd, map[string]any{"checks": checks, "failed": failed},
				func() string { return doctorTable(checks) }); err != nil {
				return err
			}
			if failed > 0 {
				// Emit already reported the detail; a bare error here would
				// print a second, redundant line.
				cmd.SilenceErrors = true
				return errExit{code: 1}
			}
			return nil
		},
	}
}

// errExit carries an exit code with nothing left to print.
type errExit struct{ code int }

func (errExit) Error() string { return "" }

func doctorTable(checks []Check) string {
	var b strings.Builder
	width := 0
	for _, c := range checks {
		width = max(width, len(c.Name))
	}
	symbol := map[string]string{checkOK: "ok  ", checkWarn: "warn", checkFail: "FAIL"}
	for _, c := range checks {
		fmt.Fprintf(&b, "%s  %-*s  %s\n", symbol[c.Status], width, c.Name, c.Detail)
		if c.Fix != "" {
			fmt.Fprintf(&b, "      %-*s  -> %s\n", width, "", c.Fix)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// runDoctor executes every check in order, cheapest and most foundational
// first: a broken storage root explains most of what follows.
func runDoctor(ctx context.Context) []Check {
	checks := []Check{doctor.Binary()}

	root, err := home.Root()
	if err != nil {
		return append(checks, fail("storage root", "cannot resolve the storage root: "+err.Error(),
			"set TRELLIS_HOME to a writable directory"))
	}
	cfg, cfgErr := config.Load(root)
	if cfgErr != nil {
		cfg = config.Defaults()
	}
	checks = append(checks, doctor.StorageRoot(root), doctor.Database(), doctor.Config(cfg, cfgErr))

	status, statusErr := resolveDaemonStatus(ctx)
	if statusErr != nil {
		checks = append(checks, fail("daemon", statusErr.Error(), ""))
	} else {
		checks = append(checks, checkDaemon(status), checkService(status), checkWebUI(status, cfg), checkPort(status, cfg))
	}
	return append(checks, checkProject(), doctor.ProjectKeys(), doctor.VectorSearch(cfg))
}

func checkDaemon(status daemonStatus) Check {
	if status.Running {
		return ok("daemon", fmt.Sprintf("running %s, supervised by %s",
			servingDescription(status.URL), describeManager(status)))
	}
	if status.Service.Installed {
		return fail("daemon", "installed as a service but not responding on its socket",
			"trellis daemon restart, then check "+home.DaemonLogPath(status.Root))
	}
	// Not running is a legitimate state: the CLI works without a daemon.
	return warn("daemon", "not running; search and the web UI run in-process", "trellis daemon start")
}

func checkService(status daemonStatus) Check {
	state := status.Service
	if !state.Installed {
		if service.New().Name() == "" {
			return ok("auto-start", "not supported on "+runtime.GOOS)
		}
		return warn("auto-start", "not installed; the daemon will not start at login", "trellis daemon install")
	}
	// The most common real breakage: `trellis update` or a move replaces the
	// binary, and the unit keeps pointing at a path that no longer exists.
	if state.Exec != "" {
		if _, err := os.Stat(state.Exec); err != nil {
			return fail("auto-start", fmt.Sprintf("%s unit points at a missing binary: %s", state.ManagedBy, state.Exec),
				"trellis daemon install")
		}
		if current, err := currentExec(); err == nil && current != state.Exec {
			return warn("auto-start", fmt.Sprintf("%s unit runs %s, but you are running %s", state.ManagedBy, state.Exec, current),
				"trellis daemon install")
		}
	}
	if !state.Enabled {
		return warn("auto-start", state.ManagedBy+" unit is installed but not enabled", "trellis daemon start")
	}
	return ok("auto-start", fmt.Sprintf("%s, %s", state.ManagedBy, state.UnitPath))
}

func currentExec() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		return resolved, nil
	}
	return exe, nil
}

// checkWebUI reports whether the browser UI is reachable, and catches the one
// state a user cannot explain from either setting alone: a daemon still
// running from before ui.enabled was turned back on.
func checkWebUI(status daemonStatus, cfg config.Config) Check {
	enabled := cfg.UI.UIEnabled()
	switch {
	case !enabled && status.Running && status.URL != "":
		return warn("web ui", "ui.enabled is false but the running daemon is still serving "+status.URL,
			"trellis daemon restart")
	case !enabled:
		return ok("web ui", "disabled by ui.enabled; the daemon serves local IPC only")
	case status.Running && status.URL == "":
		return warn("web ui", "ui.enabled is true but the running daemon started without it",
			"trellis daemon restart")
	case status.Running:
		return ok("web ui", status.URL)
	default:
		return ok("web ui", "enabled; served once the daemon or `trellis ui` runs")
	}
}

// checkPort compares the configured address with reality. A running daemon
// keeps the port it was started with, so config drift only bites at the next
// restart; a stopped daemon cannot start at all if something else holds it.
func checkPort(status daemonStatus, cfg config.Config) Check {
	addr := net.JoinHostPort(cfg.UI.Bind, strconv.Itoa(cfg.UI.Port))
	if !cfg.UI.UIEnabled() {
		return ok("http port", "no port is bound while ui.enabled is false")
	}
	if status.Running && status.URL != "" {
		serving := servingAddress(status.URL)
		if serving != "" && serving != addr {
			return warn("http port", fmt.Sprintf("daemon is serving %s, but ui.bind/ui.port say %s", serving, addr),
				"trellis daemon restart to adopt the configured port")
		}
		return ok("http port", addr+" served by the daemon")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fail("http port", addr+" is already in use by another process",
			"trellis config set ui.port <other port>")
	}
	_ = listener.Close()
	return ok("http port", addr+" is free")
}

// servingAddress extracts host:port from the URL the daemon's ping reports,
// which carries a session token query string the caller does not want.
func servingAddress(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

// checkProject reports which project this directory acts on, and how that was
// decided.
func checkProject() Check {
	switch {
	case projectFlagKey != "":
		return ok("project", normalizeProjectArg(projectFlagKey)+" (from --project)")
	case os.Getenv("TRELLIS_PROJECT") != "":
		return ok("project", normalizeProjectArg(os.Getenv("TRELLIS_PROJECT"))+" (from TRELLIS_PROJECT)")
	}
	dir, err := os.Getwd()
	if err != nil {
		return warn("project", "cannot read the working directory: "+err.Error(), "")
	}
	marker, found, err := resolve.FindMarker(dir)
	if err != nil {
		return warn("project", err.Error(), "trellis init --key <KEY>")
	}
	if !found {
		return warn("project", "no .trellis marker in this directory or any parent", "trellis init --key <KEY>")
	}
	detail := fmt.Sprintf("%s (marker %s)", marker.Target, marker.Path)
	path, err := home.DBPath()
	if err != nil {
		return warn("project", detail+", but the database cannot be located: "+err.Error(), "")
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return warn("project", detail+", but there is no database here yet", "trellis init")
	} else if err != nil {
		return warn("project", detail+", but the database cannot be read: "+err.Error(), "")
	}
	db, err := store.Open(path)
	if err != nil {
		return warn("project", detail+", but the database cannot be opened: "+err.Error(), "")
	}
	defer db.Close()
	var n int
	if err := db.Get(&n, `SELECT count(*) FROM project WHERE key = ?`, marker.Target.Project); err != nil {
		return warn("project", detail+", but the database cannot be read: "+err.Error(), "")
	}
	if n == 0 {
		return warn("project", detail+", but this database has no such project", "trellis init")
	}
	return ok("project", detail)
}

// configFileHint names the config file so an error can point at it without
// every caller re-deriving the storage root.
func configFileHint() string {
	root, err := home.Root()
	if err != nil {
		return "the trellis config file"
	}
	return filepath.Join(root, "config.yaml")
}
