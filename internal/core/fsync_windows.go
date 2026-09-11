//go:build windows

package core

// syncDirectory is a no-op on Windows. Opening a directory handle to flush it
// is denied by the OS, so the Unix trick of fsyncing the parent reports
// "Access is denied" rather than adding durability. Windows orders metadata
// updates itself, so the rename or link is already the unit of durability.
func syncDirectory(string) error { return nil }
