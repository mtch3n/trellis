package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/service"
	"github.com/mtch3n/trellis/internal/store"
	"github.com/mtch3n/trellis/internal/version"
	"github.com/mtch3n/trellis/internal/vpath"
	"github.com/spf13/cobra"
)

// Check outcomes. warn means "works, but something will surprise you later";
// fail means a command is broken right now. Only fail sets a non-zero exit.
const (
	checkOK   = "ok"
	checkWarn = "warn"
	checkFail = "fail"
)

// Check is one diagnostic. Fix is the command that resolves it, so an agent
// reading --json output can act without parsing prose.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

func ok(name, detail string) Check { return Check{Name: name, Status: checkOK, Detail: detail} }
func fail(name, detail, fix string) Check {
	return Check{Name: name, Status: checkFail, Detail: detail, Fix: fix}
}
func warn(name, detail, fix string) Check {
	return Check{Name: name, Status: checkWarn, Detail: detail, Fix: fix}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose this Trellis installation",
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
	checks := []Check{checkBinary()}

	root, err := home.Root()
	if err != nil {
		return append(checks, fail("storage root", "cannot resolve the storage root: "+err.Error(),
			"set TRELLIS_HOME to a writable directory"))
	}
	checks = append(checks, checkStorageRoot(root), checkDatabase())

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		checks = append(checks, warn("config", "unreadable, using defaults: "+cfgErr.Error(), "trellis config ls"))
		cfg = config.Defaults()
	} else {
		checks = append(checks, ok("config", fmt.Sprintf("ui %s:%d, search %s", cfg.UI.Bind, cfg.UI.Port, cfg.Search.Method)))
	}

	status, statusErr := resolveDaemonStatus(ctx)
	if statusErr != nil {
		checks = append(checks, fail("daemon", statusErr.Error(), ""))
	} else {
		checks = append(checks, checkDaemon(status), checkService(status), checkWebUI(status, cfg), checkPort(status, cfg))
	}
	return append(checks, checkProject(), checkProjectKeys(), checkVectorSearch(cfg))
}

func checkBinary() Check {
	exe, err := os.Executable()
	if err != nil {
		return warn("binary", "cannot locate the running binary: "+err.Error(), "")
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		exe = resolved
	}
	return ok("binary", fmt.Sprintf("%s (%s, %s/%s)", exe, version.Version, runtime.GOOS, runtime.GOARCH))
}

func checkStorageRoot(root string) Check {
	info, err := os.Stat(root)
	if err != nil {
		return fail("storage root", root+": "+err.Error(), "set TRELLIS_HOME to a writable directory")
	}
	if !info.IsDir() {
		return fail("storage root", root+" is not a directory", "remove it or set TRELLIS_HOME elsewhere")
	}
	// Stat cannot tell us about write permission portably; a probe file can.
	probe := filepath.Join(root, ".doctor-write-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		return fail("storage root", root+" is not writable: "+err.Error(), "fix the directory permissions")
	}
	_ = os.Remove(probe)
	return ok("storage root", root)
}

func checkDatabase() Check {
	path, err := home.DBPath()
	if err != nil {
		return fail("database", err.Error(), "")
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return warn("database", "no database yet at "+path, "trellis init")
	}
	// Opening runs any pending migrations, so a clean open is also a clean
	// schema. Counting projects proves the file is readable, not just present.
	db, err := store.Open(path)
	if err != nil {
		return fail("database", "cannot open "+path+": "+err.Error(), "trellis backup, then restore or re-init")
	}
	defer db.Close()
	var projects int
	if err := db.Get(&projects, "SELECT count(*) FROM project"); err != nil {
		return fail("database", "cannot read "+path+": "+err.Error(), "trellis maintenance")
	}
	return ok("database", fmt.Sprintf("%s (%d projects)", path, projects))
}

func checkDaemon(status daemonStatus) Check {
	if status.Running {
		return ok("daemon", fmt.Sprintf("running %s, supervised by %s",
			servingDescription(status.URL), describeManager(status)))
	}
	if status.Service.Installed {
		return fail("daemon", "installed as a service but not responding on its socket",
			"trellis daemon restart, then check "+daemonLogPath(status.Root))
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
	address := net.JoinHostPort(cfg.UI.Bind, strconv.Itoa(cfg.UI.Port))
	if !cfg.UI.UIEnabled() {
		return ok("http port", "no port is bound while ui.enabled is false")
	}
	if status.Running && status.URL != "" {
		serving := servingAddress(status.URL)
		if serving != "" && serving != address {
			return warn("http port", fmt.Sprintf("daemon is serving %s, but ui.bind/ui.port say %s", serving, address),
				"trellis daemon restart to adopt the configured port")
		}
		return ok("http port", address+" served by the daemon")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fail("http port", address+" is already in use by another process",
			"trellis config set ui.port <other port>")
	}
	_ = listener.Close()
	return ok("http port", address+" is free")
}

// servingAddress extracts host:port from the daemon's health URL, which
// carries a session token query string the caller does not want.
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
		return ok("project", strings.ToUpper(projectFlagKey)+" (from --project)")
	case os.Getenv("TRELLIS_PROJECT") != "":
		return ok("project", strings.ToUpper(os.Getenv("TRELLIS_PROJECT"))+" (from TRELLIS_PROJECT)")
	}
	dir, err := os.Getwd()
	if err != nil {
		return warn("project", "cannot read the working directory: "+err.Error(), "")
	}
	pin, found, err := resolve.FindPin(dir)
	if err != nil {
		return warn("project", err.Error(), "trellis init --key <KEY>")
	}
	if !found {
		return warn("project", "no .trellis pin in this directory or any parent", "trellis init --key <KEY>")
	}
	detail := fmt.Sprintf("%s (pin %s)", pin.Target, pin.Path)
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
	if err := db.Get(&n, `SELECT count(*) FROM project WHERE key = ?`, pin.Target.Project); err != nil {
		return warn("project", detail+", but the database cannot be read: "+err.Error(), "")
	}
	if n == 0 {
		return warn("project", detail+", but this database has no such project", "trellis init")
	}
	return ok("project", detail)
}

// checkProjectKeys lists projects whose key predates the key grammar. They
// stay reachable with --project, but no pin can name them.
func checkProjectKeys() Check {
	db, err := openExistingDB()
	if err != nil {
		return ok("project keys", "no database yet")
	}
	defer db.Close()
	var keys []string
	if err := db.Select(&keys, `SELECT key FROM project ORDER BY key`); err != nil {
		return warn("project keys", "cannot read project keys: "+err.Error(), "trellis maintenance")
	}
	bad := slices.DeleteFunc(keys, vpath.ValidKey)
	if len(bad) == 0 {
		return ok("project keys", "every key can be pinned")
	}
	return warn("project keys",
		fmt.Sprintf("no pin can name %s: %s", plural(len(bad), "this project", "these projects"), strings.Join(bad, ", ")),
		"trellis --project <KEY> ...   # still reachable by name")
}

// openExistingDB opens the database only when it already exists, so a check
// never creates one.
func openExistingDB() (*sqlx.DB, error) {
	path, err := home.DBPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return store.Open(path)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// checkVectorSearch verifies the one part of search that depends on something
// outside the binary: a user-supplied embedding command or endpoint.
func checkVectorSearch(cfg config.Config) Check {
	vector := cfg.Search.Vector
	if !vector.Enabled {
		if cfg.Search.Method == "vector" || cfg.Search.Method == "hybrid" {
			return warn("vector search", "search.method is "+cfg.Search.Method+" but search.vector.enabled is false",
				"trellis config set search.vector.enabled true")
		}
		return ok("vector search", "disabled")
	}
	switch vector.Provider {
	case "command":
		if vector.EmbedCommand == "" {
			return fail("vector search", "provider is command but search.vector.embed_command is empty",
				"trellis config set search.vector.embed_command <path>")
		}
		program := strings.Fields(vector.EmbedCommand)[0]
		if _, err := exec.LookPath(program); err != nil {
			return fail("vector search", "embed command not executable: "+program, "install it or fix search.vector.embed_command")
		}
		return ok("vector search", "command "+vector.EmbedCommand)
	case "http":
		if vector.Endpoint == "" {
			return fail("vector search", "provider is http but search.vector.endpoint is empty",
				"trellis config set search.vector.endpoint <url>")
		}
		return ok("vector search", "http "+vector.Endpoint)
	case "":
		return fail("vector search", "enabled but search.vector.provider is unset",
			"trellis config set search.vector.provider command")
	default:
		return ok("vector search", vector.Provider)
	}
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
