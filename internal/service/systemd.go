package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const systemdUnitName = "trellis.service"

// renderSystemdUnit builds the user unit. Values reaching a systemd command
// line are double-quoted: an exec path or storage root containing a space is
// otherwise split into separate arguments.
func renderSystemdUnit(spec Spec) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Trellis local kanban and knowledge daemon\n")
	b.WriteString("Documentation=https://github.com/mtch3n/trellis\n")
	b.WriteString("After=default.target\n\n")

	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s daemon --bind %s --port %d\n",
		systemdQuote(spec.Exec), systemdQuote(spec.Bind), spec.Port)
	if spec.Home != "" {
		fmt.Fprintf(&b, "Environment=TRELLIS_HOME=%s\n", systemdQuote(spec.Home))
	}
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=5\n")
	// The daemon exits cleanly on SIGTERM, releasing the SQLite lock and
	// removing its socket. Killing the group instead would leave both behind.
	b.WriteString("KillSignal=SIGTERM\n")
	b.WriteString("TimeoutStopSec=20\n\n")

	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

// systemdQuote wraps a value in systemd's double-quote syntax, which accepts
// C-style escapes for a literal backslash or quote.
func systemdQuote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(v) + `"`
}

// parseSystemctlShow turns `systemctl show --property=...` output into a map.
// Values may contain '=', so only the first one separates key from value.
func parseSystemctlShow(out string) map[string]string {
	props := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		props[key] = value
	}
	return props
}

// execFromUnit recovers the program path from a rendered ExecStart line so
// doctor can tell that an installed unit still points at the running binary.
func execFromUnit(unit string) string {
	for line := range strings.SplitSeq(unit, "\n") {
		rest, found := strings.CutPrefix(strings.TrimSpace(line), "ExecStart=")
		if !found {
			continue
		}
		return systemdUnquoteFirst(rest)
	}
	return ""
}

// systemdUnquoteFirst returns the first argument of a command line, undoing
// the quoting systemdQuote applied.
func systemdUnquoteFirst(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, `"`) {
		first, _, _ := strings.Cut(line, " ")
		return first
	}
	var out strings.Builder
	escaped := false
	for _, r := range line[1:] {
		switch {
		case escaped:
			out.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			return out.String()
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

type systemdManager struct{}

func (systemdManager) Name() string { return "systemd" }

func (systemdManager) UnitPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "systemd", "user", systemdUnitName), nil
}

func (m systemdManager) Install(spec Spec) error {
	path, err := m.UnitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(renderSystemdUnit(spec)), 0o600); err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if spec.Linger {
		// Best effort: lingering may be denied by polkit, and the service
		// still works for the length of a login session without it.
		_ = exec.Command("loginctl", "enable-linger").Run()
	}
	return systemctl("enable", "--now", systemdUnitName)
}

func (m systemdManager) Uninstall() error {
	path, err := m.UnitPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return ErrNotInstalled
	}
	// Ignore failures here: a unit that is already stopped or was never
	// enabled must not block removing the file.
	_ = systemctl("disable", "--now", systemdUnitName)
	if err := os.Remove(path); err != nil {
		return err
	}
	return systemctl("daemon-reload")
}

func (m systemdManager) Start() error   { return m.run("start") }
func (m systemdManager) Stop() error    { return m.run("stop") }
func (m systemdManager) Restart() error { return m.run("restart") }

func (m systemdManager) run(verb string) error {
	installed, err := m.installed()
	if err != nil {
		return err
	}
	if !installed {
		return ErrNotInstalled
	}
	return systemctl(verb, systemdUnitName)
}

func (m systemdManager) installed() (bool, error) {
	path, err := m.UnitPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (m systemdManager) Status() (State, error) {
	path, err := m.UnitPath()
	if err != nil {
		return State{}, err
	}
	state := State{ManagedBy: m.Name(), UnitPath: path}
	unit, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.Installed = true
	state.Exec = execFromUnit(string(unit))

	out, err := exec.Command("systemctl", "--user", "show", systemdUnitName,
		"--property=ActiveState", "--property=UnitFileState", "--property=MainPID").Output()
	if err != nil {
		// A unit file with no reachable systemd (a container, or no user bus)
		// is a real condition doctor reports; it is not a status failure.
		return state, nil
	}
	props := parseSystemctlShow(string(out))
	state.Running = props["ActiveState"] == "active"
	state.Enabled = props["UnitFileState"] == "enabled"
	if pid, convErr := strconv.Atoi(props["MainPID"]); convErr == nil && pid > 0 {
		state.PID = pid
	}
	return state, nil
}

func systemctl(args ...string) error {
	full := append([]string{"--user"}, args...)
	out, err := exec.Command("systemctl", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
