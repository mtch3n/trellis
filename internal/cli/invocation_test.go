package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRedactArgvKeepsShapeNotContent(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{[]string{"card", "new", "--title", "sk-live-abc123"}, "card new --title"},
		{[]string{"card", "comment", "12", "-"}, "card comment"},
		{[]string{"card", "ls", "--json", "--limit=5"}, "card ls --json --limit"},
		{[]string{"agent", "ls"}, "agent ls"},
	} {
		if got := redactArgv(tc.in); got != tc.want {
			t.Errorf("redactArgv(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// `trellis --help` logs its invocation. That log must never be what creates
// or upgrades a database: a build from a newer branch run with --help once
// migrated a user's real database behind the installed binary's back.
func TestLogInvocationNeverCreatesTheDatabase(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	t.Setenv("TRELLIS_NO_LOG", "")
	logInvocation([]string{"--help"}, 0, time.Now())
	if _, err := os.Stat(filepath.Join(root, "trellis.db")); !os.IsNotExist(err) {
		t.Errorf("logging --help created the database: %v", err)
	}
}
