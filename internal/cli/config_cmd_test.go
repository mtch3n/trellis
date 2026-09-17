package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The value needs a -- separator: without it pflag reads -1 as an unknown
// shorthand flag rather than as config set's second positional argument.
// Interspersed flag parsing stays on (see TestConfigSetStillAcceptsFlagsAfterArgs)
// so a common agent-calling shape -- flags after the key and value -- keeps
// working; the separator is the cost of that instead of history.keep alone.
func TestConfigSetRejectsNegativeHistoryKeep(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "config", "set", "--", "history.keep", "-1"); cliErrCode(err) != "invalid_value" {
		t.Errorf("err = %v, want invalid_value", err)
	}
}

func TestConfigSetHistoryKeep(t *testing.T) {
	projectEnv(t)
	runCmd(t, "config", "set", "history.keep", "10")
	out := runCmd(t, "config", "get", "history.keep", "--json")
	if !strings.Contains(out, `"value":"10"`) {
		t.Errorf("config get history.keep = %s, want value 10", out)
	}
}

// A flag after the key and value must still parse: config set must not turn
// on SetInterspersed(false), which would make a trailing --json (or
// --project) a third positional argument instead of a flag.
func TestConfigSetStillAcceptsFlagsAfterArgs(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "config", "set", "card.ls_limit", "50", "--json")
	if !strings.Contains(out, `"value":"50"`) {
		t.Errorf("config set card.ls_limit 50 --json = %s, want value 50", out)
	}
}

// The global config.yaml file is what actually drives Core's capture
// behaviour (SetHistoryKeep is wired from it in openCore, mirroring
// claim.ttl and labels.require_on_card), not a project override in the
// database. This proves that wiring end to end.
func TestGlobalConfigHistoryKeepZeroDisablesCapture(t *testing.T) {
	projectEnv(t)
	root := os.Getenv("TRELLIS_HOME")
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("history:\n  keep: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runCmd(t, "vault", "new", "--title", "Off")
	out := runCmd(t, "vault", "show", "off", "--json")
	if !strings.Contains(out, `"slug":"off"`) {
		t.Fatalf("entry was not created:\n%s", out)
	}
	revDir := filepath.Join(root, "projects", "TEST", "vault", ".off.md")
	if _, err := os.Stat(revDir); !os.IsNotExist(err) {
		t.Errorf("a revision directory exists despite history.keep: 0 in config.yaml")
	}
}
