package service

// New returns the systemd user-service manager. Trellis never installs a
// system-wide unit: the database lives in one user's home directory.
func New() Manager { return systemdManager{} }
