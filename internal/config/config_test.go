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
	// An empty root has no config file, so Load returns the defaults.
	cfg, err := Load(t.TempDir())
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
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("ui:\n  enabled: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(root)
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
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("history:\n  keep: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(root)
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
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("history:\n  keep: -1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(root); err == nil {
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
		t.Errorf("ValidateValue(ui.port, -1): %v, want nil: ui.port has no semantic constraint", err)
	}
}

func TestValidateValueRejectsBadLeaseTTLAndSearchMethod(t *testing.T) {
	if err := ValidateValue("claim.ttl", "banana"); err == nil {
		t.Error("ValidateValue(claim.ttl, banana), want an error")
	}
	if err := ValidateValue("claim.ttl", "-5m"); err == nil {
		t.Error("ValidateValue(claim.ttl, -5m), want an error: not positive")
	}
	if err := ValidateValue("claim.ttl", "0s"); err == nil {
		t.Error("ValidateValue(claim.ttl, 0s), want an error: not positive")
	}
	if err := ValidateValue("claim.ttl", "30m"); err != nil {
		t.Errorf("ValidateValue(claim.ttl, 30m): %v, want nil", err)
	}
	if err := ValidateValue("search.method", "bogus"); err == nil {
		t.Error("ValidateValue(search.method, bogus), want an error")
	}
	if err := ValidateValue("search.method", "hybrid"); err != nil {
		t.Errorf("ValidateValue(search.method, hybrid): %v, want nil", err)
	}
}

func TestDescribeCoversAllKeysExactly(t *testing.T) {
	described := map[string]bool{}
	for _, info := range Describe() {
		if described[info.Key] {
			t.Errorf("Describe() lists %q twice", info.Key)
		}
		described[info.Key] = true
	}
	for _, k := range AllKeys() {
		if !described[k] {
			t.Errorf("Describe() is missing %q", k)
		}
	}
	if len(described) != len(AllKeys()) {
		t.Errorf("Describe() has %d keys, AllKeys() has %d", len(described), len(AllKeys()))
	}
}

func TestDescribeMarksUIAndVectorKeysNotEditable(t *testing.T) {
	for _, info := range Describe() {
		wantEditable := !strings.HasPrefix(info.Key, "ui.") && !strings.HasPrefix(info.Key, "search.vector.")
		if info.Editable != wantEditable {
			t.Errorf("Describe()[%q].Editable = %v, want %v", info.Key, info.Editable, wantEditable)
		}
		if info.Description == "" {
			t.Errorf("Describe()[%q].Description is empty", info.Key)
		}
	}
}

func TestDescribeSearchMethodIsAnEnumWithChoices(t *testing.T) {
	for _, info := range Describe() {
		if info.Key != "search.method" {
			continue
		}
		if info.Type != TypeEnum {
			t.Errorf("search.method type = %q, want enum", info.Type)
		}
		if !slices.Contains(info.Choices, "fts") || !slices.Contains(info.Choices, "vector") || !slices.Contains(info.Choices, "hybrid") {
			t.Errorf("search.method choices = %v, want fts/vector/hybrid", info.Choices)
		}
		return
	}
	t.Fatal("Describe() does not list search.method")
}

func TestSetGlobalValuesRoundTripsCommentsAndExtensions(t *testing.T) {
	root := t.TempDir()
	original := "# a comment\nclaim:\n  ttl: 20m\nextensions:\n  actions: [a, b]\n"
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := SetGlobalValues(root, map[string]any{"history.keep": 50}, nil); err != nil {
		t.Fatalf("SetGlobalValues: %v", err)
	}
	text := readFile(t, filepath.Join(root, "config.yaml"))
	if !strings.Contains(text, "# a comment") {
		t.Errorf("comment lost:\n%s", text)
	}
	if !strings.Contains(text, "extensions:") || !strings.Contains(text, "actions:") {
		t.Errorf("extensions section lost:\n%s", text)
	}
	if !strings.Contains(text, "keep: 50") {
		t.Errorf("history.keep not written:\n%s", text)
	}

	if _, err := SetGlobalValues(root, nil, []string{"history.keep"}); err != nil {
		t.Fatalf("SetGlobalValues unset: %v", err)
	}
	text = readFile(t, filepath.Join(root, "config.yaml"))
	if strings.Contains(text, "keep:") {
		t.Errorf("history.keep survived unset:\n%s", text)
	}
	if !strings.Contains(text, "# a comment") || !strings.Contains(text, "extensions:") {
		t.Errorf("comment or extensions lost after unset:\n%s", text)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSetGlobalValuesRejectsWholeBatchOnOneBadValue(t *testing.T) {
	root := t.TempDir()
	_, err := SetGlobalValues(root, map[string]any{
		"claim.ttl":              "45m",
		"history.keep":           -1,
		"labels.require_on_card": true,
	}, nil)
	if err == nil {
		t.Fatal("want an error: history.keep is negative")
	}
	if _, statErr := os.Stat(filepath.Join(root, "config.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("config.yaml was written despite one invalid value in the batch: stat err = %v", statErr)
	}
}

func TestSetGlobalValuesThenLoadReadsTypedValuesBack(t *testing.T) {
	root := t.TempDir()
	cfg, err := SetGlobalValues(root, map[string]any{
		"claim.ttl":             "45m",
		"history.keep":          50,
		"board.default_columns": []any{"todo", "done"},
	}, nil)
	if err != nil {
		t.Fatalf("SetGlobalValues: %v", err)
	}
	if cfg.Lease.TTL != "45m" {
		t.Errorf("Lease.TTL = %q, want 45m", cfg.Lease.TTL)
	}
	if cfg.History.EffectiveKeep() != 50 {
		t.Errorf("History.EffectiveKeep() = %d, want 50", cfg.History.EffectiveKeep())
	}
	if len(cfg.Board.DefaultColumns) != 2 || cfg.Board.DefaultColumns[0] != "todo" || cfg.Board.DefaultColumns[1] != "done" {
		t.Errorf("Board.DefaultColumns = %v", cfg.Board.DefaultColumns)
	}

	reloaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Lease.TTL != "45m" || reloaded.History.EffectiveKeep() != 50 {
		t.Errorf("reloaded = %+v", reloaded)
	}
}

// board.default_columns is a list of strings, but nothing stops an item
// looking exactly like a bool or an int -- "true", "123". Written as a plain
// YAML scalar those would resolve to the bool true and the int 123 on the
// next parse, by Load or by anything else that reads config.yaml generically
// (a []any decode, say). settingNode's explicit double-quoted style must
// keep them strings all the way through.
func TestSetGlobalValuesQuotesStringLookingListItems(t *testing.T) {
	root := t.TempDir()
	if _, err := SetGlobalValues(root, map[string]any{
		"board.default_columns": []any{"true", "123"},
	}, nil); err != nil {
		t.Fatalf("SetGlobalValues: %v", err)
	}

	text := readFile(t, filepath.Join(root, "config.yaml"))
	if !strings.Contains(text, `"true"`) || !strings.Contains(text, `"123"`) {
		t.Errorf("config.yaml does not quote the string-looking items:\n%s", text)
	}

	// A generic decode -- what a different, less careful YAML reader would
	// do -- must still see strings, not a bool and an int.
	var generic map[string]any
	if err := yaml.Unmarshal([]byte(text), &generic); err != nil {
		t.Fatalf("generic unmarshal: %v", err)
	}
	board, _ := generic["board"].(map[string]any)
	items, _ := board["default_columns"].([]any)
	if len(items) != 2 || items[0] != "true" || items[1] != "123" {
		t.Fatalf("generic decode = %#v, want the strings [\"true\" \"123\"]", items)
	}

	reloaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.Board.DefaultColumns) != 2 || reloaded.Board.DefaultColumns[0] != "true" || reloaded.Board.DefaultColumns[1] != "123" {
		t.Errorf("Load() Board.DefaultColumns = %v, want [true 123] as strings", reloaded.Board.DefaultColumns)
	}
}

func TestSetGlobalValuesRefusesANonEditableKey(t *testing.T) {
	root := t.TempDir()
	_, err := SetGlobalValues(root, map[string]any{"ui.port": 9999}, nil)
	if err == nil {
		t.Fatal("want an error: ui.port is not editable")
	}
	ise, ok := err.(*InvalidSettingsError)
	if !ok {
		t.Fatalf("err type = %T, want *InvalidSettingsError", err)
	}
	if len(ise.Problems) != 1 || !strings.Contains(ise.Problems[0], "ui.port") {
		t.Errorf("Problems = %v", ise.Problems)
	}
	if _, statErr := os.Stat(filepath.Join(root, "config.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("config.yaml was written despite refusing a non-editable key: stat err = %v", statErr)
	}
}

func TestSetGlobalValuesRefusesAnUnknownKey(t *testing.T) {
	root := t.TempDir()
	if _, err := SetGlobalValues(root, map[string]any{"no.such.key": "x"}, nil); err == nil {
		t.Fatal("want an error for an unknown key")
	}
	if _, err := SetGlobalValues(root, nil, []string{"no.such.key"}); err == nil {
		t.Fatal("want an error unsetting an unknown key")
	}
}

func TestSetGlobalValuesRefusesUnsettingANonEditableKey(t *testing.T) {
	root := t.TempDir()
	if _, err := SetGlobalValues(root, nil, []string{"ui.port"}); err == nil {
		t.Fatal("want an error: ui.port is not editable")
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
		"card.ls_limit",
		"claim.ttl", "board.default_columns",
		"labels.require_on_card", "tags.require_on_card",
		"search.method",
	}
	for _, k := range safe {
		if !RepoSafe(k) {
			t.Errorf("RepoSafe(%q) = false, want true", k)
		}
	}
	refused := []string{"ui.port", "ui.bind", "ui.enabled",
		"search.vector.enabled", "search.vector.provider", "search.vector.model",
		"search.vector.embed_command", "search.vector.endpoint"}
	for _, k := range refused {
		if RepoSafe(k) {
			t.Errorf("RepoSafe(%q) = true, want false", k)
		}
	}
}

func TestRepoConfigPathBothPresentIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  claim.ttl: 45m\n")
	writeRepoFile(t, dir, ".trellis.yml", "config:\n  claim.ttl: 45m\n")

	if _, err := RepoConfigPath(dir); err == nil {
		t.Fatal("want an error naming both files")
	}
}

func TestRepoConfigPathAcceptsEitherExtension(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yml", "config:\n  claim.ttl: 45m\n")
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
  claim.ttl: 45m
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
	for _, k := range []string{"card.ls_limit", "claim.ttl", "labels.require_on_card", "board.default_columns"} {
		if !doc.Present[k] {
			t.Errorf("Present[%q] = false, want true", k)
		}
	}
	if doc.Present["search.method"] {
		t.Error("Present[\"search.method\"] = true, but the file never set it")
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

// review-cli #7: claim.ttl and search.method are plain strings, so a YAML
// type check alone never catches a value that does not parse for its key.
func TestLoadRepoRejectsAnUnparseableLeaseTTL(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  claim.ttl: banana\n")
	_, _, _, err := LoadRepo(dir)
	if err == nil || !strings.Contains(err.Error(), "claim.ttl") {
		t.Fatalf("err = %v, want an error naming claim.ttl", err)
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
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  card.ls_limit: 0\n")
	_, _, _, err := LoadRepo(dir)
	if err == nil || !strings.Contains(err.Error(), "card.ls_limit") {
		t.Errorf("err = %v, want an error naming card.ls_limit", err)
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
  claim.ttl: 45m

extensions:
  actions:
    - on: entry.created
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
	if !slices.Contains(AllKeys(), "claim.ttl") {
		t.Error("AllKeys is missing claim.ttl")
	}
}

func TestLoadWithPresenceDistinguishesFileFromDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("claim:\n  ttl: 10m\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, present, err := LoadWithPresence(root)
	if err != nil {
		t.Fatalf("LoadWithPresence: %v", err)
	}
	if cfg.Lease.TTL != "10m" {
		t.Fatalf("Lease.TTL = %q, want 10m", cfg.Lease.TTL)
	}
	if !present["claim.ttl"] {
		t.Error(`present["claim.ttl"] = false, want true: the file set it`)
	}
	if present["card.ls_limit"] {
		t.Error(`present["card.ls_limit"] = true, but the file never mentioned it`)
	}
}

func TestLoadWithPresenceOnAnEmptyHomeMarksNothingPresent(t *testing.T) {
	_, present, err := LoadWithPresence(t.TempDir())
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
	value, source, err := EffectiveValue(ctx, Defaults(), map[string]bool{}, RepoDoc{}, db, "p1", "claim.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "default" || value != "30m" {
		t.Fatalf("value=%q source=%q, want 30m/default", value, source)
	}

	// 2. The global file set it: config.
	globalCfg := Defaults()
	globalCfg.Lease.TTL = "20m"
	value, source, err = EffectiveValue(ctx, globalCfg, map[string]bool{"claim.ttl": true}, RepoDoc{}, db, "p1", "claim.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "config" || value != "20m" {
		t.Fatalf("value=%q source=%q, want 20m/config", value, source)
	}

	// 3. The repository file also set it: repo wins over the global file.
	repo := RepoDoc{Config: Config{Lease: LeaseConfig{TTL: "45m"}}, Present: map[string]bool{"claim.ttl": true}}
	value, source, err = EffectiveValue(ctx, globalCfg, map[string]bool{"claim.ttl": true}, repo, db, "p1", "claim.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "repo" || value != "45m" {
		t.Fatalf("value=%q source=%q, want 45m/repo", value, source)
	}

	// 4. A project override wins over everything.
	if err := SetProjectConfig(ctx, db, "p1", "claim.ttl", "5m"); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}
	value, source, err = EffectiveValue(ctx, globalCfg, map[string]bool{"claim.ttl": true}, repo, db, "p1", "claim.ttl")
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
			Search: SearchConfig{Method: "vector"},
			Tags:   TagsConfig{RequireOnCard: true},
		},
		Present: map[string]bool{"card.ls_limit": true, "claim.ttl": true, "search.method": true, "tags.require_on_card": true},
	}
	merged := ApplyRepoOverrides(cfg, repo)
	if merged.Card.LsLimit != 5 {
		t.Errorf("Card.LsLimit = %d, want 5", merged.Card.LsLimit)
	}
	if merged.Lease.TTL != "5m" {
		t.Errorf("Lease.TTL = %q, want 5m", merged.Lease.TTL)
	}
	if merged.Search.Method != "vector" {
		t.Errorf("Search.Method = %q, want vector", merged.Search.Method)
	}
	if !merged.Tags.RequireOnCard {
		t.Error("Tags.RequireOnCard = false, want true")
	}
	// board.default_columns was never present: the default must survive.
	if len(merged.Board.DefaultColumns) != len(Defaults().Board.DefaultColumns) {
		t.Errorf("Board.DefaultColumns = %v, want the untouched default", merged.Board.DefaultColumns)
	}
}

// The settings page matches each problem to its field by the "key: " prefix.
func TestSetGlobalValuesProblemsStartWithTheirKey(t *testing.T) {
	set := map[string]any{
		"claim.ttl":     "soon",
		"search.method": "grep",
		"history.keep":  -1,
		"card.ls_limit": "many",
		"no.such.key":   1,
		"ui.port":       9999,
	}
	_, err := SetGlobalValues(t.TempDir(), set, nil)
	ise, ok := err.(*InvalidSettingsError)
	if !ok {
		t.Fatalf("err = %v, want *InvalidSettingsError", err)
	}
	if len(ise.Problems) != len(set) {
		t.Errorf("Problems = %v, want one per key", ise.Problems)
	}
	for _, p := range ise.Problems {
		key, _, found := strings.Cut(p, ": ")
		if _, known := set[key]; !found || !known {
			t.Errorf("problem %q does not start with a key and \": \"", p)
		}
	}
}
