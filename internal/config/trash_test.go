package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrash_TRASH_C18_the_default_is_added_and_nothing_else_moves(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	before := "# my settings\nclaim:\n  ttl: 45m # longer claims\nextensions:\n  standup:\n    on: true\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureTrashRetention(root); err != nil {
		t.Fatalf("EnsureTrashRetention: %v", err)
	}

	after, _ := os.ReadFile(path)
	for _, line := range strings.Split(strings.TrimRight(before, "\n"), "\n") {
		if !strings.Contains(string(after), strings.TrimSpace(line)) {
			t.Errorf("lost %q:\n%s", line, after)
		}
	}
	cfg, err := Load(root)
	if err != nil || cfg.Trash.Retention != "30d" || cfg.Claim.TTL != "45m" {
		t.Errorf("after: retention %q, claim.ttl %q, %v", cfg.Trash.Retention, cfg.Claim.TTL, err)
	}

	set := "trash:\n  retention: 7d\n"
	os.WriteFile(path, []byte(set), 0o600)
	if err := EnsureTrashRetention(root); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != set {
		t.Errorf("an explicit 7d was rewritten:\n%s", got)
	}
}

func TestTrash_TRASH_C19_a_malformed_config_is_not_rewritten(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	bad := "claim: [unclosed\n"
	os.WriteFile(path, []byte(bad), 0o600)
	_, loadErr := Load(root)

	err := EnsureTrashRetention(root)

	if got, _ := os.ReadFile(path); string(got) != bad {
		t.Errorf("a malformed file was rewritten:\n%s", got)
	}
	if err != nil {
		t.Errorf("EnsureTrashRetention = %v; the load error (%v) is reported where it is today", err, loadErr)
	}
}

func TestTrash_TRASH_C26_retention_must_be_a_day_or_more(t *testing.T) {
	for _, bad := range []string{"0", "12h", "-1d", "abc", "0d"} {
		if err := ValidateValue("trash.retention", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, good := range []string{"30d", "12w", "720h", "1d", "24h"} {
		if err := ValidateValue("trash.retention", good); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
	root := t.TempDir()
	if _, err := SetGlobalValues(root, map[string]any{"trash.retention": "0"}, nil); err == nil {
		t.Errorf("the settings writer accepted 0")
	}
	os.WriteFile(filepath.Join(root, "config.yaml"), []byte("trash:\n  retention: 2h\n"), 0o600)
	if _, err := Load(root); err == nil {
		t.Errorf("Load accepted retention 2h")
	}
}
