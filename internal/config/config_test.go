package config

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	value, source, err := EffectiveValue(ctx, cfg, map[string]bool{}, RepoDoc{}, db, projectID, "card.ls_limit")
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
	value, source, err = EffectiveValue(ctx, cfg, map[string]bool{}, RepoDoc{}, db, projectID, "card.ls_limit")
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

func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestRepoSafeKeys(t *testing.T) {
	safe := []string{
		"card.ls_limit", "card.duplicate_check", "card.duplicate_threshold",
		"lease.ttl", "board.default_columns",
		"labels.preset", "labels.require_on_card", "tags.require_on_card",
		"search.limit", "search.method",
	}
	for _, k := range safe {
		if !RepoSafe(k) {
			t.Errorf("RepoSafe(%q) = false, want true", k)
		}
	}
	refused := []string{"ui.port", "ui.bind", "ui.enabled", "db.busy_timeout_ms", "git.timeout",
		"search.vector.enabled", "search.vector.embed_command", "search.vector.endpoint"}
	for _, k := range refused {
		if RepoSafe(k) {
			t.Errorf("RepoSafe(%q) = true, want false", k)
		}
	}
}

func TestRepoConfigPathBothPresentIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  lease.ttl: 45m\n")
	writeRepoFile(t, dir, ".trellis.yml", "config:\n  lease.ttl: 45m\n")

	if _, err := RepoConfigPath(dir); err == nil {
		t.Fatal("want an error naming both files")
	}
}

func TestRepoConfigPathAcceptsEitherExtension(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yml", "config:\n  lease.ttl: 45m\n")
	path, err := RepoConfigPath(dir)
	if err != nil {
		t.Fatalf("RepoConfigPath: %v", err)
	}
	if filepath.Base(path) != ".trellis.yml" {
		t.Errorf("path = %q, want .trellis.yml", path)
	}
}

func TestRepoConfigPathWithNeitherFileIsNotAnError(t *testing.T) {
	path, err := RepoConfigPath(t.TempDir())
	if err != nil || path != "" {
		t.Fatalf("path=%q err=%v, want (\"\", nil)", path, err)
	}
}

func TestLoadRepoWithNoFileReturnsNotOK(t *testing.T) {
	doc, path, ok, err := LoadRepo(t.TempDir())
	if err != nil || ok || path != "" || len(doc.Present) != 0 {
		t.Fatalf("doc=%+v path=%q ok=%v err=%v, want a not-ok zero result", doc, path, ok, err)
	}
}

func TestLoadRepoAppliesAllowedKeys(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", `config:
  card.ls_limit: 25
  lease.ttl: 45m
  labels.require_on_card: true
  board.default_columns: [todo, doing, done]
`)
	doc, path, ok, err := LoadRepo(dir)
	if err != nil {
		t.Fatalf("LoadRepo: %v", err)
	}
	if !ok || path == "" {
		t.Fatalf("ok=%v path=%q, want a loaded repo config", ok, path)
	}
	if doc.Config.Card.LsLimit != 25 {
		t.Errorf("Card.LsLimit = %d, want 25", doc.Config.Card.LsLimit)
	}
	if doc.Config.Lease.TTL != "45m" {
		t.Errorf("Lease.TTL = %q, want 45m", doc.Config.Lease.TTL)
	}
	if !doc.Config.Labels.RequireOnCard {
		t.Error("Labels.RequireOnCard = false, want true")
	}
	if len(doc.Config.Board.DefaultColumns) != 3 || doc.Config.Board.DefaultColumns[0] != "todo" {
		t.Errorf("Board.DefaultColumns = %v", doc.Config.Board.DefaultColumns)
	}
	for _, k := range []string{"card.ls_limit", "lease.ttl", "labels.require_on_card", "board.default_columns"} {
		if !doc.Present[k] {
			t.Errorf("Present[%q] = false, want true", k)
		}
	}
	if doc.Present["search.limit"] {
		t.Error("Present[\"search.limit\"] = true, but the file never set it")
	}
}

func TestLoadRepoRefusesADisallowedKey(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  ui.port: 9999\n")
	_, path, _, err := LoadRepo(dir)
	if err == nil {
		t.Fatal("want an error: ui.port is not repository-safe")
	}
	if !strings.Contains(err.Error(), "ui.port") {
		t.Errorf("error %q does not name the refused key", err)
	}
	_ = path
}

func TestLoadRepoRefusesAnUnknownKey(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  nonexistent.key: 1\n")
	_, _, _, err := LoadRepo(dir)
	if err == nil || !strings.Contains(err.Error(), "nonexistent.key") {
		t.Fatalf("err = %v, want an error naming nonexistent.key", err)
	}
}

func TestLoadRepoRejectsABadValue(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  card.ls_limit: not-a-number\n")
	_, _, _, err := LoadRepo(dir)
	if err == nil || !strings.Contains(err.Error(), "card.ls_limit") {
		t.Fatalf("err = %v, want an error naming card.ls_limit", err)
	}
}

// review-cli #7: lease.ttl and search.method are plain strings, so a YAML
// type check alone never catches a value that does not parse for its key.
func TestLoadRepoRejectsAnUnparseableLeaseTTL(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  lease.ttl: banana\n")
	_, _, _, err := LoadRepo(dir)
	if err == nil || !strings.Contains(err.Error(), "lease.ttl") {
		t.Fatalf("err = %v, want an error naming lease.ttl", err)
	}
}

func TestLoadRepoRejectsAnUnknownSearchMethod(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  search.method: bogus\n")
	_, _, _, err := LoadRepo(dir)
	if err == nil || !strings.Contains(err.Error(), "search.method") {
		t.Fatalf("err = %v, want an error naming search.method", err)
	}
}

func TestLoadRepoRejectsANonPositiveLimit(t *testing.T) {
	for _, tc := range []struct{ key, yaml string }{
		{"card.ls_limit", "config:\n  card.ls_limit: 0\n"},
		{"search.limit", "config:\n  search.limit: -5\n"},
	} {
		dir := t.TempDir()
		writeRepoFile(t, dir, ".trellis.yaml", tc.yaml)
		_, _, _, err := LoadRepo(dir)
		if err == nil || !strings.Contains(err.Error(), tc.key) {
			t.Errorf("%s: err = %v, want an error naming %s", tc.key, err, tc.key)
		}
	}
}

// review-cli #6: SetRepoValue must refuse a value its own loader would
// reject, instead of writing it and breaking every command in that
// repository the next time it reads the file.
func TestSetRepoValueRejectsABadValue(t *testing.T) {
	dir := t.TempDir()
	if _, err := SetRepoValue(dir, "card.ls_limit", "abc"); err == nil {
		t.Fatal("SetRepoValue accepted a non-numeric card.ls_limit")
	}
	if _, _, ok, _ := LoadRepo(dir); ok {
		t.Fatal("SetRepoValue wrote a file despite rejecting the value")
	}
}

func TestLoadRepoRejectsAnUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "storage:\n  path: /tmp\n")
	_, _, _, err := LoadRepo(dir)
	if err == nil || !strings.Contains(err.Error(), "storage") {
		t.Fatalf("err = %v, want an error naming the unknown top-level key storage", err)
	}
}

func TestLoadRepoPassesExtensionsThroughUntouched(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", `config:
  lease.ttl: 45m

extensions:
  actions:
    - on: knowledge.created
      type: finding
      run: ./scripts/review-finding.sh
`)
	doc, _, ok, err := LoadRepo(dir)
	if err != nil || !ok {
		t.Fatalf("LoadRepo: ok=%v err=%v", ok, err)
	}
	m, isMap := doc.Extensions.(map[string]any)
	if !isMap {
		t.Fatalf("Extensions = %#v (%T), want a map", doc.Extensions, doc.Extensions)
	}
	actions, isSlice := m["actions"].([]any)
	if !isSlice || len(actions) != 1 {
		t.Fatalf("Extensions[actions] = %#v, want a one-item list", m["actions"])
	}
}

func TestLoadRepoWithEmptyDirReadsNothing(t *testing.T) {
	doc, path, ok, err := LoadRepo("")
	if err != nil || ok || path != "" || doc.Extensions != nil {
		t.Fatalf("doc=%+v path=%q ok=%v err=%v, want a not-ok zero result for an empty dir", doc, path, ok, err)
	}
}

func TestAllKeysIncludesEveryKeyGetValueKnows(t *testing.T) {
	for _, k := range AllKeys() {
		if _, found := GetValue(Defaults(), k); !found {
			t.Errorf("AllKeys lists %q, but GetValue does not recognize it", k)
		}
	}
	if !slices.Contains(AllKeys(), "lease.ttl") {
		t.Error("AllKeys is missing lease.ttl")
	}
}

func TestLoadWithPresenceDistinguishesFileFromDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("lease:\n  ttl: 10m\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, present, err := LoadWithPresence()
	if err != nil {
		t.Fatalf("LoadWithPresence: %v", err)
	}
	if cfg.Lease.TTL != "10m" {
		t.Fatalf("Lease.TTL = %q, want 10m", cfg.Lease.TTL)
	}
	if !present["lease.ttl"] {
		t.Error(`present["lease.ttl"] = false, want true: the file set it`)
	}
	if present["card.ls_limit"] {
		t.Error(`present["card.ls_limit"] = true, but the file never mentioned it`)
	}
}

func TestLoadWithPresenceOnAnEmptyHomeMarksNothingPresent(t *testing.T) {
	t.Setenv("TRELLIS_HOME", t.TempDir())
	_, present, err := LoadWithPresence()
	if err != nil {
		t.Fatalf("LoadWithPresence: %v", err)
	}
	if len(present) != 0 {
		t.Errorf("present = %v, want empty: there is no config.yaml", present)
	}
}

func TestEffectiveValueReportsDefaultThenConfigThenRepoThenProject(t *testing.T) {
	// openTestDB (config_test.go:248) already exists and sets up both
	// `project` and `project_config`; reuse it rather than declaring a
	// second, duplicate in-memory schema helper.
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// 1. Nothing set anywhere: default.
	value, source, err := EffectiveValue(ctx, Defaults(), map[string]bool{}, RepoDoc{}, db, "p1", "lease.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "default" || value != "30m" {
		t.Fatalf("value=%q source=%q, want 30m/default", value, source)
	}

	// 2. The global file set it: config.
	globalCfg := Defaults()
	globalCfg.Lease.TTL = "20m"
	value, source, err = EffectiveValue(ctx, globalCfg, map[string]bool{"lease.ttl": true}, RepoDoc{}, db, "p1", "lease.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "config" || value != "20m" {
		t.Fatalf("value=%q source=%q, want 20m/config", value, source)
	}

	// 3. The repository file also set it: repo wins over the global file.
	repo := RepoDoc{Config: Config{Lease: LeaseConfig{TTL: "45m"}}, Present: map[string]bool{"lease.ttl": true}}
	value, source, err = EffectiveValue(ctx, globalCfg, map[string]bool{"lease.ttl": true}, repo, db, "p1", "lease.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "repo" || value != "45m" {
		t.Fatalf("value=%q source=%q, want 45m/repo", value, source)
	}

	// 4. A project override wins over everything.
	if err := SetProjectConfig(ctx, db, "p1", "lease.ttl", "5m"); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}
	value, source, err = EffectiveValue(ctx, globalCfg, map[string]bool{"lease.ttl": true}, repo, db, "p1", "lease.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "project" || value != "5m" {
		t.Fatalf("value=%q source=%q, want 5m/project", value, source)
	}
}

func TestEffectiveValueUnknownKey(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, _, err := EffectiveValue(context.Background(), Defaults(), map[string]bool{}, RepoDoc{}, db, "p1", "no.such.key"); err == nil {
		t.Fatal("want an error for an unknown key")
	}
}

func TestApplyRepoOverridesCopiesEveryPresentKey(t *testing.T) {
	cfg := Defaults()
	repo := RepoDoc{
		Config: Config{
			Card:   CardConfig{LsLimit: 5},
			Lease:  LeaseConfig{TTL: "5m"},
			Search: SearchConfig{Limit: 3, Method: "vector"},
		},
		Present: map[string]bool{"card.ls_limit": true, "lease.ttl": true, "search.limit": true, "search.method": true},
	}
	merged := ApplyRepoOverrides(cfg, repo)
	if merged.Card.LsLimit != 5 {
		t.Errorf("Card.LsLimit = %d, want 5", merged.Card.LsLimit)
	}
	if merged.Lease.TTL != "5m" {
		t.Errorf("Lease.TTL = %q, want 5m", merged.Lease.TTL)
	}
	if merged.Search.Limit != 3 || merged.Search.Method != "vector" {
		t.Errorf("Search = %+v, want Limit=3 Method=vector", merged.Search)
	}
	// board.default_columns was never present: the default must survive.
	if len(merged.Board.DefaultColumns) != len(Defaults().Board.DefaultColumns) {
		t.Errorf("Board.DefaultColumns = %v, want the untouched default", merged.Board.DefaultColumns)
	}
}
