// Package service installs and controls the Trellis daemon as a per-user OS
// service. It owns every piece of knowledge about systemd and launchd so the
// CLI never shells out to a service manager directly.
package service

import "errors"

// ErrUnsupported is returned by every method on platforms with no per-user
// service manager Trellis knows how to drive. Callers report it verbatim
// rather than falling back to a half-working install.
var ErrUnsupported = errors.New("installing the daemon as a service is only supported on Linux (systemd) and macOS (launchd)")

// ErrNotInstalled distinguishes "no unit file" from a failed service manager
// call, so the CLI can fall back to a self-managed child process.
var ErrNotInstalled = errors.New("daemon service is not installed")

// Spec is everything the rendered unit needs. Exec is an absolute path: a
// relative one resolves against the service manager's working directory, not
// the one the user ran `daemon install` from.
type Spec struct {
	Exec   string
	Bind   string
	Port   int
	Home   string // TRELLIS_HOME to pin into the unit; empty means inherit.
	Linger bool   // Linux only: keep running when no session is open.
}

// State is a snapshot of what the service manager believes. Running is the
// manager's opinion; the CLI cross-checks it against the daemon's own IPC
// health endpoint, and `daemon doctor` reports when the two disagree.
type State struct {
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid,omitempty"`
	ManagedBy string `json:"managed_by"`
	UnitPath  string `json:"unit_path,omitempty"`
	// Exec is the program path recorded in the installed unit. It drifts from
	// the running binary after `trellis update` moves or replaces it.
	Exec string `json:"exec,omitempty"`
}

// Manager drives one platform's per-user service manager.
type Manager interface {
	// Name is the service manager's name, used in CLI output: "systemd" or "launchd".
	Name() string
	// UnitPath is where Install writes, whether or not the file exists yet.
	UnitPath() (string, error)
	Install(Spec) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (State, error)
}

// unsupported satisfies Manager on platforms without a supported service
// manager. Every method fails identically so no caller has to special-case.
type unsupported struct{}

func (unsupported) Name() string              { return "" }
func (unsupported) UnitPath() (string, error) { return "", ErrUnsupported }
func (unsupported) Install(Spec) error        { return ErrUnsupported }
func (unsupported) Uninstall() error          { return ErrUnsupported }
func (unsupported) Start() error              { return ErrUnsupported }
func (unsupported) Stop() error               { return ErrUnsupported }
func (unsupported) Restart() error            { return ErrUnsupported }
func (unsupported) Status() (State, error)    { return State{}, ErrUnsupported }
