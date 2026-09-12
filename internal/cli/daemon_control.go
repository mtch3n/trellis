package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/daemon"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/service"
)

// How the running daemon is supervised. The CLI decides this before acting so
// `stop` never kills a process systemd will immediately restart.
const (
	managedByService = "service" // a systemd unit or launchd agent owns it
	managedBySelf    = "self"    // a detached child this CLI spawned
	managedByForeign = "foreign" // responding, but nothing here explains it
	managedByNone    = "none"    // not running
)

// daemonSpawnWait bounds how long start waits for a freshly spawned daemon to
// answer on its IPC socket. The first start of a large knowledge base rebuilds
// the search index before it listens.
const daemonSpawnWait = 30 * time.Second

func daemonPIDPath(root string) string { return filepath.Join(root, "daemon.pid") }
func daemonLogPath(root string) string { return filepath.Join(root, "daemon.log") }

// daemonHealth asks the running daemon over local IPC. This is the only
// authoritative answer to "is it up": a unit can be active while the process
// is still starting, and a pidfile can outlive the process it names.
func daemonHealth(ctx context.Context, root string) (url string, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := daemon.Call(ctx, daemon.Endpoint(root), daemon.Request{Method: "health"})
	if err != nil || !resp.OK {
		return "", false
	}
	if u, isString := resp.Data["url"].(string); isString {
		return u, true
	}
	return "", true
}

// readDaemonPID returns the PID this CLI last spawned, if that process is
// still alive. A stale file from a crashed daemon reports not-running.
func readDaemonPID(root string) (int, bool) {
	raw, err := os.ReadFile(daemonPIDPath(root))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if !processAlive(pid) {
		return pid, false
	}
	return pid, true
}

// daemonStatus is the merged view the control commands and doctor both act on.
type daemonStatus struct {
	Running   bool          `json:"running"`
	URL       string        `json:"url,omitempty"`
	PID       int           `json:"pid,omitempty"`
	ManagedBy string        `json:"managed_by"`
	Service   service.State `json:"service"`
	Root      string        `json:"-"`
}

// resolveDaemonStatus decides who owns the daemon. Health comes first because
// it is the only fact; the service manager and pidfile only explain it.
func resolveDaemonStatus(ctx context.Context) (daemonStatus, error) {
	root, err := home.Root()
	if err != nil {
		return daemonStatus{}, err
	}
	status := daemonStatus{Root: root, ManagedBy: managedByNone}
	status.URL, status.Running = daemonHealth(ctx, root)

	// A Status error means the service manager itself is broken, which doctor
	// reports; the control commands still work through the self-managed path.
	if state, statusErr := service.New().Status(); statusErr == nil {
		status.Service = state
	}

	switch {
	case status.Service.Installed:
		status.ManagedBy = managedByService
		status.PID = status.Service.PID
		// An installed unit that is not running leaves ManagedBy pointing at
		// the service so `start` delegates rather than spawning a rival child.
	case status.Running:
		if pid, alive := readDaemonPID(root); alive {
			status.ManagedBy, status.PID = managedBySelf, pid
		} else {
			status.ManagedBy = managedByForeign
		}
	}
	return status, nil
}

// daemonDefaults resolves the bind address and port for a command that was not
// given explicit flags, falling back to config and then to built-in defaults.
func daemonDefaults(bind string, port int) (string, int) {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Defaults()
	}
	if bind == "" {
		bind = cfg.UI.Bind
	}
	if port == 0 {
		port = cfg.UI.Port
	}
	defaults := config.Defaults()
	if bind == "" {
		bind = defaults.UI.Bind
	}
	if port == 0 {
		port = defaults.UI.Port
	}
	return bind, port
}

// daemonSpec builds the unit contents for `daemon install`. TRELLIS_HOME is
// pinned only when it was set explicitly: a service started at login inherits
// almost nothing from the shell, so an unpinned custom root would silently
// become the default one.
func daemonSpec(bind string, port int, linger bool) (service.Spec, error) {
	exe, err := os.Executable()
	if err != nil {
		return service.Spec{}, fmt.Errorf("locate the trellis binary: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		exe = resolved
	}
	return service.Spec{
		Exec:   exe,
		Bind:   bind,
		Port:   port,
		Home:   os.Getenv("TRELLIS_HOME"),
		Linger: linger,
	}, nil
}

// spawnDaemon starts a detached `trellis daemon` and waits for it to answer.
// The child outlives this CLI process, so its output goes to a log file rather
// than to a terminal that is about to disappear.
func spawnDaemon(ctx context.Context, root, bind string, port int) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("locate the trellis binary: %w", err)
	}
	logFile, err := os.OpenFile(daemonLogPath(root), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "daemon", "--bind", bind, "--port", strconv.Itoa(port))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	// Release the child so it is not left a zombie when this CLI exits.
	_ = cmd.Process.Release()
	if err := os.WriteFile(daemonPIDPath(root), []byte(strconv.Itoa(pid)), 0o600); err != nil {
		return pid, err
	}
	if err := waitForDaemon(ctx, root, true); err != nil {
		return pid, fmt.Errorf("%w (see %s)", err, daemonLogPath(root))
	}
	return pid, nil
}

// waitForDaemon polls the health endpoint until it matches want or the budget
// runs out. Polling beats a fixed sleep: a warm start answers immediately.
func waitForDaemon(ctx context.Context, root string, want bool) error {
	deadline := time.Now().Add(daemonSpawnWait)
	for {
		if _, ok := daemonHealth(ctx, root); ok == want {
			return nil
		}
		if time.Now().After(deadline) {
			if want {
				return errors.New("timed out waiting for the daemon to start")
			}
			return errors.New("timed out waiting for the daemon to stop")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// stopSelfManaged terminates the detached child and waits for the socket to go
// quiet. It asks politely first so the daemon releases its SQLite lock and
// removes its socket on the way out.
func stopSelfManaged(ctx context.Context, root string) error {
	pid, alive := readDaemonPID(root)
	if !alive {
		_ = os.Remove(daemonPIDPath(root))
		return errors.New("no self-managed trellis daemon is running")
	}
	if err := terminate(pid); err != nil {
		return fmt.Errorf("stop daemon %d: %w", pid, err)
	}
	if err := waitForDaemon(ctx, root, false); err != nil {
		return err
	}
	_ = os.Remove(daemonPIDPath(root))
	return nil
}
