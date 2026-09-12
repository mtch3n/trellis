package service

// New returns the launchd per-user agent manager.
func New() Manager { return launchdManager{} }
