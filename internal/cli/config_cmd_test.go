package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigSetRejectsNegativeHistoryKeep(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "config", "set", "history.keep", "-1"); cliErrCode(err) != "invalid_value" {
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

// The global config.yaml file is what actually drives Core's capture
// behaviour (SetHistoryKeep is wired from it in openCore, mirroring
// lease.ttl and labels.require_on_card), not a project override in the
// database. This proves that wiring end to end.
func TestGlobalConfigHistoryKeepZeroDisablesCapture(t *testing.T) {
	projectEnv(t)
	root := os.Getenv("TRELLIS_HOME")
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("history:\n  keep: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runCmd(t, "knowledge", "new", "--title", "Off")
	out := runCmd(t, "knowledge", "show", "off", "--json")
	if !strings.Contains(out, `"slug":"off"`) {
		t.Fatalf("entry was not created:\n%s", out)
	}
	revDir := filepath.Join(root, "projects", "TEST", "knowledge", ".off.md")
	if _, err := os.Stat(revDir); !os.IsNotExist(err) {
		t.Errorf("a revision directory exists despite history.keep: 0 in config.yaml")
	}
}
