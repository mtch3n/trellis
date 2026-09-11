package cli

import "testing"

func TestRedactArgvKeepsShapeNotContent(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{[]string{"card", "new", "--title", "sk-live-abc123"}, "card new --title"},
		{[]string{"card", "note", "12", "-"}, "card note"},
		{[]string{"card", "ls", "--json", "--limit=5"}, "card ls --json --limit"},
		{[]string{"agent", "ls"}, "agent ls"},
	} {
		if got := redactArgv(tc.in); got != tc.want {
			t.Errorf("redactArgv(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
