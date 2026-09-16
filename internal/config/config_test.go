package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

func TestDefaultsLoadWithNoFile(t *testing.T) {
	// Temporarily override the config path to a non-existent file.
	// Since Load() checks os.ErrNotExist, this should return defaults.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with missing file: %v", err)
	}

	// Verify defaults are present.
	if cfg.UI.Port != 7788 {
		t.Errorf("UI.Port = %d, want 7788", cfg.UI.Port)
	}
	if cfg.UI.Bind != "127.0.0.1" {
		t.Errorf("UI.Bind = %q, want 127.0.0.1", cfg.UI.Bind)
	}
	if cfg.DB.BusyTimeoutMs != 10000 {
		t.Errorf("DB.BusyTimeoutMs = %d, want 10000", cfg.DB.BusyTimeoutMs)
	}
	if cfg.Lease.TTL != "30m" {
		t.Errorf("Lease.TTL = %q, want 30m", cfg.Lease.TTL)
	}
	if len(cfg.Board.DefaultColumns) != 4 {
		t.Errorf("Board.DefaultColumns len = %d, want 4", len(cfg.Board.DefaultColumns))
	}
	if cfg.Card.LsLimit != 50 {
		t.Errorf("Card.LsLimit = %d, want 50", cfg.Card.LsLimit)
	}
}

func TestYAMLFileOverridesDefaults(t *testing.T) {
	// Create a temporary config file.
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	content := `ui:
  port: 8888
  bind: 0.0.0.0
db:
  busy_timeout_ms: 5000
labels:
  require_on_card: true
`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	// Parse it manually (Load() uses os.UserHomeDir, so we parse directly).
	var cfg Config
	data, _ := os.ReadFile(configFile)
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	applyDefaults(&cfg)

	// Verify overrides.
	if cfg.UI.Port != 8888 {
		t.Errorf("UI.Port = %d, want 8888", cfg.UI.Port)
	}
	if cfg.UI.Bind != "0.0.0.0" {
		t.Errorf("UI.Bind = %q, want 0.0.0.0", cfg.UI.Bind)
	}
	if cfg.DB.BusyTimeoutMs != 5000 {
		t.Errorf("DB.BusyTimeoutMs = %d, want 5000", cfg.DB.BusyTimeoutMs)
	}
	if !cfg.Labels.RequireOnCard {
		t.Errorf("Labels.RequireOnCard = %v, want true", cfg.Labels.RequireOnCard)
	}

	// Verify defaults for unset fields.
	if cfg.Lease.TTL != "30m" {
		t.Errorf("Lease.TTL = %q, want 30m (default)", cfg.Lease.TTL)
	}
}

func TestMalformedYAMLReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write malformed YAML.
	content := `ui:
  port: not_a_number
  bind: [invalid`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	// Try to parse it.
	var cfg Config
	data, _ := os.ReadFile(configFile)
	err := yaml.Unmarshal(data, &cfg)
	if err == nil {
		t.Error("unmarshal malformed YAML: expected error, got nil")
	}
}

func TestProjectConfigGetSetRoundTrip(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	projectID := "TEST-123"

	// Create a test project.
	_, err := db.ExecContext(ctx,
		`INSERT INTO project (id, key, name, created_at) VALUES (?, ?, ?, ?)`,
		projectID, "TEST", "Test Project", 0)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	// Set a config value.
	err = SetProjectConfig(ctx, db, projectID, "labels.require_on_card", "true")
	if err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	// Get it back.
	value, ok, err := GetProjectConfig(ctx, db, projectID, "labels.require_on_card")
	if err != nil {
		t.Fatalf("GetProjectConfig: %v", err)
	}
	if !ok {
		t.Error("GetProjectConfig: value not found")
	}
	if value != "true" {
		t.Errorf("GetProjectConfig = %q, want true", value)
	}

	// Get a non-existent key.
	_, ok, err = GetProjectConfig(ctx, db, projectID, "nonexistent.key")
	if err != nil {
		t.Fatalf("GetProjectConfig nonexistent: %v", err)
	}
	if ok {
		t.Error("GetProjectConfig: expected not found for nonexistent key")
	}
}

func TestProjectOverrideTakesPrecedence(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	projectID := "TEST-456"

	// Create a test project.
	_, err := db.ExecContext(ctx,
		`INSERT INTO project (id, key, name, created_at) VALUES (?, ?, ?, ?)`,
		projectID, "TEST", "Test Project", 0)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	cfg := Defaults()

	// Without override, should get default.
	value, source, err := EffectiveValue(ctx, cfg, db, projectID, "card.ls_limit")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "default" {
		t.Errorf("source = %q, want default", source)
	}
	if value != "50" {
		t.Errorf("value = %q, want 50", value)
	}

	// Set a project override.
	err = SetProjectConfig(ctx, db, projectID, "card.ls_limit", "100")
	if err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	// Now should get the override.
	value, source, err = EffectiveValue(ctx, cfg, db, projectID, "card.ls_limit")
	if err != nil {
		t.Fatalf("EffectiveValue after override: %v", err)
	}
	if source != "project" {
		t.Errorf("source = %q, want project", source)
	}
	if value != "100" {
		t.Errorf("value = %q, want 100", value)
	}
}

func TestListProjectConfigs(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	projectID := "TEST-789"

	// Create a test project.
	_, err := db.ExecContext(ctx,
		`INSERT INTO project (id, key, name, created_at) VALUES (?, ?, ?, ?)`,
		projectID, "TEST", "Test Project", 0)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	// Set some config values.
	for key, value := range map[string]string{
		"labels.require_on_card": "true",
		"card.ls_limit":          "75",
		"tags.require_on_card":   "false",
	} {
		err = SetProjectConfig(ctx, db, projectID, key, value)
		if err != nil {
			t.Fatalf("SetProjectConfig %q: %v", key, err)
		}
	}

	// List them.
	configs, err := ListProjectConfigs(ctx, db, projectID)
	if err != nil {
		t.Fatalf("ListProjectConfigs: %v", err)
	}

	if len(configs) != 3 {
		t.Errorf("ListProjectConfigs returned %d configs, want 3", len(configs))
	}
	if configs["labels.require_on_card"] != "true" {
		t.Errorf("labels.require_on_card = %q, want true", configs["labels.require_on_card"])
	}
	if configs["card.ls_limit"] != "75" {
		t.Errorf("card.ls_limit = %q, want 75", configs["card.ls_limit"])
	}
}

// openTestDB opens an in-memory SQLite database for testing, with the schema set up.
func openTestDB(t *testing.T) *sqlx.DB {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	// Create the minimal schema needed for tests.
	schema := `
	CREATE TABLE project (
		id         TEXT PRIMARY KEY,
		key        TEXT NOT NULL UNIQUE,
		name       TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);

	CREATE TABLE project_config (
		project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
		key        TEXT NOT NULL,
		value      TEXT NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (project_id, key)
	);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}

	return db
}

func TestUIEnabledDefaultsToTrue(t *testing.T) {
	if !Defaults().UI.UIEnabled() {
		t.Error("the web UI must be on unless the config turns it off")
	}
	// An unset key is not the same as false: a config file that says nothing
	// about the UI must still serve it.
	var unset UIConfig
	if !unset.UIEnabled() {
		t.Error("an absent ui.enabled must mean enabled")
	}
	off := false
	if (UIConfig{Enabled: &off}).UIEnabled() {
		t.Error("ui.enabled: false must disable the UI")
	}
}

func TestLoadKeepsUIDisabled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("ui:\n  enabled: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// applyDefaults must not resurrect the default here, which is the whole
	// reason Enabled is a pointer.
	if cfg.UI.UIEnabled() {
		t.Error("applyDefaults overwrote an explicit ui.enabled: false")
	}
	if value, found := GetValue(cfg, "ui.enabled"); !found || value != "false" {
		t.Errorf("config ls should report false, got %q found=%v", value, found)
	}
}

func TestHistoryKeepDefaultsTo100(t *testing.T) {
	if keep := Defaults().History.EffectiveKeep(); keep != 100 {
		t.Fatalf("History.EffectiveKeep() = %d, want 100", keep)
	}
}

func TestHistoryKeepZeroSurvivesApplyDefaults(t *testing.T) {
	zero := 0
	cfg := Config{History: HistoryConfig{Keep: &zero}}
	applyDefaults(&cfg)
	if got := cfg.History.EffectiveKeep(); got != 0 {
		t.Errorf("History.EffectiveKeep() = %d, want 0: applyDefaults must not resurrect the default", got)
	}
}

func TestLoadKeepsHistoryKeepZero(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("history:\n  keep: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.History.EffectiveKeep(); got != 0 {
		t.Errorf("History.EffectiveKeep() = %d, want 0", got)
	}
	if value, found := GetValue(cfg, "history.keep"); !found || value != "0" {
		t.Errorf("config ls should report 0, got %q found=%v", value, found)
	}
}

func TestLoadRejectsNegativeHistoryKeep(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("history:\n  keep: -1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(); err == nil {
		t.Error("Load with history.keep: -1, want an error")
	}
}

func TestValidateValueRejectsNegativeHistoryKeep(t *testing.T) {
	if err := ValidateValue("history.keep", "-1"); err == nil {
		t.Error("ValidateValue(history.keep, -1), want an error")
	}
	if err := ValidateValue("history.keep", "0"); err != nil {
		t.Errorf("ValidateValue(history.keep, 0): %v, want nil", err)
	}
	if err := ValidateValue("history.keep", "not-a-number"); err == nil {
		t.Error("ValidateValue(history.keep, not-a-number), want an error")
	}
	if err := ValidateValue("ui.port", "-1"); err != nil {
		t.Errorf("ValidateValue(ui.port, -1): %v, want nil: only history.keep is constrained so far", err)
	}
}
