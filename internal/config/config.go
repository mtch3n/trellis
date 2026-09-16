// Package config loads and manages trellis settings: global config from YAML
// and per-project overrides from the database.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/home"
	"gopkg.in/yaml.v3"
)

// Config represents the full settings tree. All fields must have sensible
// zero-value defaults since YAML parsing leaves unset fields as zero values.
type Config struct {
	UI      UIConfig      `yaml:"ui"`
	DB      DBConfig      `yaml:"db"`
	Git     GitConfig     `yaml:"git"`
	Lease   LeaseConfig   `yaml:"lease"`
	Board   BoardConfig   `yaml:"board"`
	Labels  LabelsConfig  `yaml:"labels"`
	Tags    TagsConfig    `yaml:"tags"`
	Card    CardConfig    `yaml:"card"`
	Search  SearchConfig  `yaml:"search"`
	History HistoryConfig `yaml:"history"`
}

type UIConfig struct {
	Port int    `yaml:"port"`
	Bind string `yaml:"bind"`
	// Enabled is a pointer because it is the one setting whose default is
	// true: a plain bool cannot tell "absent from the file" from "set to
	// false", and every other field here relies on the zero value meaning
	// unset. Read it through UIEnabled rather than dereferencing.
	Enabled *bool `yaml:"enabled"`
}

// UIEnabled reports whether the daemon should serve the web UI. An unset key
// means yes.
func (u UIConfig) UIEnabled() bool { return u.Enabled == nil || *u.Enabled }

type DBConfig struct {
	BusyTimeoutMs int `yaml:"busy_timeout_ms"`
}

type GitConfig struct {
	Timeout string `yaml:"timeout"` // e.g., "5s"
}

type LeaseConfig struct {
	TTL string `yaml:"ttl"` // e.g., "30m"
}

type BoardConfig struct {
	DefaultColumns []string `yaml:"default_columns"`
}

type LabelsConfig struct {
	Preset        string `yaml:"preset"` // "default" or "none"
	RequireOnCard bool   `yaml:"require_on_card"`
}

type TagsConfig struct {
	RequireOnCard bool `yaml:"require_on_card"`
}

type CardConfig struct {
	LsLimit            int     `yaml:"ls_limit"`
	DuplicateCheck     bool    `yaml:"duplicate_check"`
	DuplicateThreshold float64 `yaml:"duplicate_threshold"`
}

type SearchConfig struct {
	Limit  int                `yaml:"limit"`
	Method string             `yaml:"method"` // fts, vector, or hybrid
	Vector VectorSearchConfig `yaml:"vector"`
}

// HistoryConfig controls revision retention for knowledge entries and cards.
type HistoryConfig struct {
	// Keep is a pointer because zero is a real, meaningful value -- it turns
	// capture off -- and a plain int cannot tell that apart from "absent from
	// the file", the same reason UIConfig.Enabled is a pointer. Read it
	// through EffectiveKeep rather than dereferencing.
	Keep *int `yaml:"keep"`
}

// EffectiveKeep reports how many revisions each entry and card retains. An
// unset key means the default, 100.
func (h HistoryConfig) EffectiveKeep() int {
	if h.Keep == nil {
		return 100
	}
	return *h.Keep
}

// VectorSearchConfig controls the optional semantic document index. The
// embedding executable receives UTF-8 text on stdin and must print either a
// JSON float array or {"embedding":[...]} on stdout.
type VectorSearchConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Provider     string `yaml:"provider"` // command, http, or local
	EmbedCommand string `yaml:"embed_command"`
	Endpoint     string `yaml:"endpoint"`
	Model        string `yaml:"model"`
	Dimension    int    `yaml:"dimension"`
	Limit        int    `yaml:"limit"`
	ChunkSize    int    `yaml:"chunk_size"`
	ChunkOverlap int    `yaml:"chunk_overlap"`
}

// Defaults returns a Config with all default values applied.
func Defaults() Config {
	return Config{
		UI: UIConfig{
			Port:    7788,
			Bind:    "127.0.0.1",
			Enabled: ptr(true),
		},
		DB: DBConfig{
			BusyTimeoutMs: 10000,
		},
		Git: GitConfig{
			Timeout: "5s",
		},
		Lease: LeaseConfig{
			TTL: "30m",
		},
		Board: BoardConfig{
			DefaultColumns: []string{"backlog", "in-progress", "review", "done"},
		},
		Labels: LabelsConfig{
			Preset:        "default",
			RequireOnCard: false,
		},
		Tags: TagsConfig{
			RequireOnCard: false,
		},
		Card: CardConfig{
			LsLimit:            50,
			DuplicateCheck:     true,
			DuplicateThreshold: 0.75,
		},
		Search: SearchConfig{
			Limit:  50,
			Method: "fts",
			Vector: VectorSearchConfig{Limit: 10, ChunkSize: 1200, ChunkOverlap: 200},
		},
		History: HistoryConfig{Keep: ptr(100)},
	}
}

// configPath returns config.yaml inside the storage root. It goes through
// home.Root so TRELLIS_HOME moves the settings along with the database; a
// pinned root whose config still came from ~/.trellis would serve the wrong
// port for the daemon installed against it.
func configPath() (string, error) {
	root, err := home.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "config.yaml"), nil
}

func ptr[T any](v T) *T { return &v }

// Load reads and parses the global config file. Returns Defaults() if the file
// does not exist. Returns an error if the file exists but is malformed.
func Load() (Config, error) {
	cfg := Defaults()
	path, err := configPath()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// File not present; return defaults with no error.
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	// Apply defaults to any field that was not set in the file.
	// Unmarshal leaves unset fields as zero values, so we need to restore them.
	applyDefaults(&cfg)

	if *cfg.History.Keep < 0 {
		return cfg, fmt.Errorf("history.keep must not be negative, got %d", *cfg.History.Keep)
	}

	return cfg, nil
}

// applyDefaults fills in any zero-valued fields with defaults.
// Since all our defaults are non-zero, this is safe.
func applyDefaults(cfg *Config) {
	defaults := Defaults()

	if cfg.UI.Port == 0 {
		cfg.UI.Port = defaults.UI.Port
	}
	if cfg.UI.Bind == "" {
		cfg.UI.Bind = defaults.UI.Bind
	}
	if cfg.UI.Enabled == nil {
		cfg.UI.Enabled = defaults.UI.Enabled
	}
	if cfg.DB.BusyTimeoutMs == 0 {
		cfg.DB.BusyTimeoutMs = defaults.DB.BusyTimeoutMs
	}
	if cfg.Git.Timeout == "" {
		cfg.Git.Timeout = defaults.Git.Timeout
	}
	if cfg.Lease.TTL == "" {
		cfg.Lease.TTL = defaults.Lease.TTL
	}
	if len(cfg.Board.DefaultColumns) == 0 {
		cfg.Board.DefaultColumns = defaults.Board.DefaultColumns
	}
	if cfg.Labels.Preset == "" {
		cfg.Labels.Preset = defaults.Labels.Preset
	}
	if cfg.Card.LsLimit == 0 {
		cfg.Card.LsLimit = defaults.Card.LsLimit
	}
	if cfg.Card.DuplicateThreshold == 0 {
		cfg.Card.DuplicateThreshold = defaults.Card.DuplicateThreshold
	}
	if cfg.Search.Limit == 0 {
		cfg.Search.Limit = defaults.Search.Limit
	}
	if cfg.Search.Method == "" {
		cfg.Search.Method = defaults.Search.Method
	}
	if cfg.Search.Vector.Limit == 0 {
		cfg.Search.Vector.Limit = defaults.Search.Vector.Limit
	}
	if cfg.Search.Vector.ChunkSize == 0 {
		cfg.Search.Vector.ChunkSize = defaults.Search.Vector.ChunkSize
	}
	if cfg.Search.Vector.ChunkOverlap == 0 {
		cfg.Search.Vector.ChunkOverlap = defaults.Search.Vector.ChunkOverlap
	}
	if cfg.History.Keep == nil {
		cfg.History.Keep = defaults.History.Keep
	}
}

// GetValue retrieves a configuration value by dotted key (e.g., "labels.require_on_card").
// Returns the value, whether it was found, and any error. If not found in the config,
// returns ("", false, nil).
func GetValue(cfg Config, key string) (string, bool) {
	switch key {
	case "ui.port":
		return fmt.Sprintf("%d", cfg.UI.Port), true
	case "ui.bind":
		return cfg.UI.Bind, true
	case "ui.enabled":
		return fmt.Sprintf("%v", cfg.UI.UIEnabled()), true
	case "db.busy_timeout_ms":
		return fmt.Sprintf("%d", cfg.DB.BusyTimeoutMs), true
	case "git.timeout":
		return cfg.Git.Timeout, true
	case "lease.ttl":
		return cfg.Lease.TTL, true
	case "board.default_columns":
		// For arrays, return comma-separated values.
		return fmt.Sprintf("[%s]", fmt.Sprint(cfg.Board.DefaultColumns)), true
	case "labels.preset":
		return cfg.Labels.Preset, true
	case "labels.require_on_card":
		return fmt.Sprintf("%v", cfg.Labels.RequireOnCard), true
	case "tags.require_on_card":
		return fmt.Sprintf("%v", cfg.Tags.RequireOnCard), true
	case "card.ls_limit":
		return fmt.Sprintf("%d", cfg.Card.LsLimit), true
	case "card.duplicate_check":
		return fmt.Sprintf("%v", cfg.Card.DuplicateCheck), true
	case "card.duplicate_threshold":
		return fmt.Sprintf("%v", cfg.Card.DuplicateThreshold), true
	case "search.limit":
		return fmt.Sprintf("%d", cfg.Search.Limit), true
	case "search.method":
		return cfg.Search.Method, true
	case "search.vector.enabled":
		return fmt.Sprintf("%v", cfg.Search.Vector.Enabled), true
	case "search.vector.embed_command":
		return cfg.Search.Vector.EmbedCommand, true
	case "search.vector.provider":
		return cfg.Search.Vector.Provider, true
	case "search.vector.endpoint":
		return cfg.Search.Vector.Endpoint, true
	case "search.vector.model":
		return cfg.Search.Vector.Model, true
	case "search.vector.dimension":
		return fmt.Sprintf("%d", cfg.Search.Vector.Dimension), true
	case "search.vector.limit":
		return fmt.Sprintf("%d", cfg.Search.Vector.Limit), true
	case "search.vector.chunk_size":
		return fmt.Sprintf("%d", cfg.Search.Vector.ChunkSize), true
	case "search.vector.chunk_overlap":
		return fmt.Sprintf("%d", cfg.Search.Vector.ChunkOverlap), true
	case "history.keep":
		return fmt.Sprintf("%d", cfg.History.EffectiveKeep()), true
	default:
		return "", false
	}
}

// ValidateValue rejects a value for a key with semantic constraints beyond
// being a known key -- so far only history.keep, whose zero disables capture
// but whose negative values are nonsensical, not a synonym for "unlimited".
func ValidateValue(key, value string) error {
	if key != "history.keep" {
		return nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("history.keep must be a whole number, got %q", value)
	}
	if n < 0 {
		return fmt.Errorf("history.keep must not be negative, got %d", n)
	}
	return nil
}

// EffectiveValue resolves key through every precedence layer, lowest to
// highest: built-in defaults, the global config file, a repository's
// .trellis.yaml, then a project override in the database. source names
// whichever layer answered: "default", "config", "repo" or "project".
func EffectiveValue(ctx context.Context, cfg Config, present map[string]bool, repo RepoDoc, db *sqlx.DB, projectID, key string) (value, source string, err error) {
	value, found := GetValue(cfg, key)
	if !found {
		return "", "", fmt.Errorf("unknown config key: %q", key)
	}
	source = "default"
	if present[key] {
		source = "config"
	}
	if repo.Present[key] {
		if v, ok := GetValue(repo.Config, key); ok {
			value, source = v, "repo"
		}
	}

	override, ok, err := GetProjectConfig(ctx, db, projectID, key)
	if err != nil {
		return "", "", err
	}
	if ok {
		return override, "project", nil
	}
	return value, source, nil
}

// GetProjectConfig retrieves a project-scoped config value from the database.
// Returns (value, true, nil) if found, (_, false, nil) if not found, or an error.
func GetProjectConfig(ctx context.Context, db *sqlx.DB, projectID, key string) (string, bool, error) {
	var value string
	err := db.GetContext(ctx, &value,
		"SELECT value FROM project_config WHERE project_id = ? AND key = ?",
		projectID, key)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query project config: %w", err)
	}
	return value, true, nil
}

// SetProjectConfig stores a project-scoped config value in the database.
func SetProjectConfig(ctx context.Context, db *sqlx.DB, projectID, key, value string) error {
	now := time.Now().UnixMilli()
	_, err := db.ExecContext(ctx,
		`INSERT INTO project_config (project_id, key, value, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(project_id, key) DO UPDATE SET value = ?, updated_at = ?`,
		projectID, key, value, now, value, now)
	if err != nil {
		return fmt.Errorf("set project config: %w", err)
	}
	return nil
}

// UnsetProjectConfig removes a project override so resolution falls back to
// the global file or built-in default.
func UnsetProjectConfig(ctx context.Context, db *sqlx.DB, projectID, key string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM project_config WHERE project_id = ? AND key = ?`, projectID, key)
	return err
}

// ListProjectConfigs returns all project-scoped config entries for a project.
func ListProjectConfigs(ctx context.Context, db *sqlx.DB, projectID string) (map[string]string, error) {
	rows := make(map[string]string)
	var results []struct {
		Key   string
		Value string
	}
	err := db.SelectContext(ctx, &results,
		"SELECT key, value FROM project_config WHERE project_id = ? ORDER BY key",
		projectID)
	if err != nil {
		return nil, fmt.Errorf("list project configs: %w", err)
	}
	for _, r := range results {
		rows[r.Key] = r.Value
	}
	return rows, nil
}

// RepoSafe reports whether a repository's .trellis.yaml may set key. Every
// key is refused until listed here: the daemon, the web UI, storage and
// anything else that belongs to the machine are never listed, because a
// repository file is committed and arrives with every clone — it must not be
// able to redirect storage, open a port, or run a program.
func RepoSafe(key string) bool {
	switch key {
	case "card.ls_limit", "card.duplicate_check", "card.duplicate_threshold",
		"lease.ttl",
		"board.default_columns",
		"labels.preset", "labels.require_on_card",
		"tags.require_on_card",
		"search.limit", "search.method":
		return true
	default:
		return false
	}
}

// RepoConfigPath returns the repository config file under dir: ".trellis.yaml"
// or ".trellis.yml". Both present is an error naming both. Neither present
// returns ("", nil): dir simply has no repository config.
func RepoConfigPath(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	yamlPath := filepath.Join(dir, ".trellis.yaml")
	ymlPath := filepath.Join(dir, ".trellis.yml")
	_, err1 := os.Stat(yamlPath)
	_, err2 := os.Stat(ymlPath)
	if err1 != nil && !errors.Is(err1, os.ErrNotExist) {
		return "", err1
	}
	if err2 != nil && !errors.Is(err2, os.ErrNotExist) {
		return "", err2
	}
	has1, has2 := err1 == nil, err2 == nil
	switch {
	case has1 && has2:
		return "", fmt.Errorf("%s and %s are both present; keep only one", yamlPath, ymlPath)
	case has1:
		return yamlPath, nil
	case has2:
		return ymlPath, nil
	default:
		return "", nil
	}
}

// RepoDoc is a parsed, validated .trellis.yaml. Config holds only the keys
// RepoSafe allows, decoded onto a zero Config so GetValue can read them back
// with its existing per-key formatting. Present marks exactly which dotted
// keys the file set, distinguishing an explicit value from one that happens
// to share Config's zero value. Extensions is the "extensions" subtree
// exactly as written, decoded to a generic value: core parses it as YAML and
// never interprets it.
type RepoDoc struct {
	Config     Config
	Present    map[string]bool
	Extensions any
}

// LoadRepo reads and validates the repository config file in dir. ok is
// false with a zero RepoDoc when dir has no ".trellis.yaml"/".trellis.yml"
// (including dir == ""); err is non-nil when one exists but is invalid —
// both files present, an unknown top-level key, a key a repository may not
// set, or a value that does not parse for its key — and always names the
// file and, where applicable, the key.
func LoadRepo(dir string) (doc RepoDoc, path string, ok bool, err error) {
	path, err = RepoConfigPath(dir)
	if err != nil || path == "" {
		return RepoDoc{}, path, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RepoDoc{}, path, false, fmt.Errorf("read %s: %w", path, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return RepoDoc{}, path, false, fmt.Errorf("parse %s: %w", path, err)
	}
	doc = RepoDoc{Config: Config{}, Present: map[string]bool{}}
	if len(root.Content) == 0 {
		// An empty file: a valid, empty document.
		return doc, path, true, nil
	}
	body := root.Content[0]
	if body.Kind != yaml.MappingNode {
		return RepoDoc{}, path, false, fmt.Errorf("%s: the document must be a mapping", path)
	}

	for i := 0; i+1 < len(body.Content); i += 2 {
		topKey := body.Content[i].Value
		topVal := body.Content[i+1]
		switch topKey {
		case "config":
			if topVal.Kind != yaml.MappingNode {
				return RepoDoc{}, path, false, fmt.Errorf("%s: config must be a mapping of dotted keys to values", path)
			}
			for j := 0; j+1 < len(topVal.Content); j += 2 {
				key := topVal.Content[j].Value
				valueNode := topVal.Content[j+1]
				if !RepoSafe(key) {
					return RepoDoc{}, path, false, fmt.Errorf("%s: %q may not be set by a repository", path, key)
				}
				if err := setConfigField(&doc.Config, key, valueNode); err != nil {
					return RepoDoc{}, path, false, fmt.Errorf("%s: %q: %w", path, key, err)
				}
				doc.Present[key] = true
			}
		case "extensions":
			var ext any
			if err := topVal.Decode(&ext); err != nil {
				return RepoDoc{}, path, false, fmt.Errorf("%s: extensions: %w", path, err)
			}
			doc.Extensions = ext
		default:
			return RepoDoc{}, path, false, fmt.Errorf("%s: unknown top-level key %q", path, topKey)
		}
	}
	return doc, path, true, nil
}

// setConfigField decodes one repository-safe dotted key's YAML value into the
// matching field of cfg. Every key RepoSafe allows is handled here.
func setConfigField(cfg *Config, key string, node *yaml.Node) error {
	switch key {
	case "card.ls_limit":
		return node.Decode(&cfg.Card.LsLimit)
	case "card.duplicate_check":
		return node.Decode(&cfg.Card.DuplicateCheck)
	case "card.duplicate_threshold":
		return node.Decode(&cfg.Card.DuplicateThreshold)
	case "lease.ttl":
		return node.Decode(&cfg.Lease.TTL)
	case "board.default_columns":
		return node.Decode(&cfg.Board.DefaultColumns)
	case "labels.preset":
		return node.Decode(&cfg.Labels.Preset)
	case "labels.require_on_card":
		return node.Decode(&cfg.Labels.RequireOnCard)
	case "tags.require_on_card":
		return node.Decode(&cfg.Tags.RequireOnCard)
	case "search.limit":
		return node.Decode(&cfg.Search.Limit)
	case "search.method":
		return node.Decode(&cfg.Search.Method)
	default:
		return fmt.Errorf("not a repository-safe key")
	}
}

// AllKeys lists every dotted config key GetValue understands, in the order
// `config ls` displays them. internal/cli/config.go's newConfigLsCmd uses
// this instead of keeping its own copy of the list.
func AllKeys() []string {
	return []string{
		"ui.port", "ui.bind", "ui.enabled",
		"db.busy_timeout_ms",
		"git.timeout",
		"lease.ttl",
		"board.default_columns",
		"labels.preset", "labels.require_on_card",
		"tags.require_on_card",
		"card.ls_limit", "card.duplicate_check", "card.duplicate_threshold",
		"search.limit",
		"search.method",
		"search.vector.enabled", "search.vector.provider", "search.vector.embed_command", "search.vector.endpoint",
		"search.vector.model", "search.vector.dimension", "search.vector.limit",
		"history.keep",
	}
}

// LoadWithPresence is Load, plus which dotted keys the global file itself
// set. This is why it exists: EffectiveValue must report "config", not
// "default", for a value that came from the file, and by the time Load
// applies its defaults onto an unset field the two are indistinguishable.
func LoadWithPresence() (Config, map[string]bool, error) {
	cfg := Defaults()
	path, err := configPath()
	if err != nil {
		return cfg, map[string]bool{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, map[string]bool{}, nil
		}
		return cfg, map[string]bool{}, fmt.Errorf("read config: %w", err)
	}
	var raw Config
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, map[string]bool{}, fmt.Errorf("parse config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, map[string]bool{}, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)
	return cfg, presentKeys(raw), nil
}

// presentKeys reports which of AllKeys raw actually set, by comparing
// GetValue's string form of raw against the same key read from an entirely
// zero Config. This shares GetValue's one known blind spot: an explicit
// value equal to the zero value (search.limit: 0, ui.enabled: true) is
// indistinguishable from absence — the same limitation applyDefaults already
// has no way around.
func presentKeys(raw Config) map[string]bool {
	present := map[string]bool{}
	var zero Config
	for _, key := range AllKeys() {
		rawStr, _ := GetValue(raw, key)
		zeroStr, _ := GetValue(zero, key)
		if rawStr != zeroStr {
			present[key] = true
		}
	}
	return present
}

// ApplyRepoOverrides returns cfg with every repository-safe key repo.Present
// sets copied on top, field by field. EffectiveValue answers one key at a
// time against a database; this is for a caller — currentBoard, priming the
// Core's lease TTL, default columns and label/tag requirements — that needs
// a whole Config to seed something with, before any per-key project override
// is even in the picture.
func ApplyRepoOverrides(cfg Config, repo RepoDoc) Config {
	for key := range repo.Present {
		switch key {
		case "card.ls_limit":
			cfg.Card.LsLimit = repo.Config.Card.LsLimit
		case "card.duplicate_check":
			cfg.Card.DuplicateCheck = repo.Config.Card.DuplicateCheck
		case "card.duplicate_threshold":
			cfg.Card.DuplicateThreshold = repo.Config.Card.DuplicateThreshold
		case "lease.ttl":
			cfg.Lease.TTL = repo.Config.Lease.TTL
		case "board.default_columns":
			cfg.Board.DefaultColumns = repo.Config.Board.DefaultColumns
		case "labels.preset":
			cfg.Labels.Preset = repo.Config.Labels.Preset
		case "labels.require_on_card":
			cfg.Labels.RequireOnCard = repo.Config.Labels.RequireOnCard
		case "tags.require_on_card":
			cfg.Tags.RequireOnCard = repo.Config.Tags.RequireOnCard
		case "search.limit":
			cfg.Search.Limit = repo.Config.Search.Limit
		case "search.method":
			cfg.Search.Method = repo.Config.Search.Method
		}
	}
	return cfg
}

// SetRepoValue writes key = value into dir's repository config file under
// "config:", creating .trellis.yaml if neither file exists yet, and
// preserving every other key and the "extensions" section untouched. key
// must be RepoSafe.
func SetRepoValue(dir, key, value string) (string, error) {
	if !RepoSafe(key) {
		return "", fmt.Errorf("%q may not be set by a repository", key)
	}
	path, err := RepoConfigPath(dir)
	if err != nil {
		return "", err
	}
	root, err := readOrNewRepoRoot(path)
	if err != nil {
		return "", err
	}
	if path == "" {
		path = filepath.Join(dir, ".trellis.yaml")
	}

	body := root.Content[0]
	configNode := mapValue(body, "config")
	if configNode == nil {
		configNode = &yaml.Node{Kind: yaml.MappingNode}
		body.Content = append(body.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "config"}, configNode)
	}

	var valueNode *yaml.Node
	if key == "board.default_columns" {
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range strings.Split(value, ",") {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: strings.TrimSpace(item)})
		}
		valueNode = seq
	} else {
		valueNode = &yaml.Node{Kind: yaml.ScalarNode, Value: value}
	}
	setMapValueNode(configNode, key, valueNode)

	return path, writeRepoRoot(path, root)
}

// UnsetRepoValue removes key from dir's repository config file, leaving
// every other key and "extensions" untouched. Removing a key that is not
// present, or from a file that does not exist, is not an error.
func UnsetRepoValue(dir, key string) (string, error) {
	path, err := RepoConfigPath(dir)
	if err != nil || path == "" {
		return path, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return path, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return path, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(root.Content) == 0 {
		return path, nil
	}
	body := root.Content[0]
	if configNode := mapValue(body, "config"); configNode != nil {
		deleteMapValue(configNode, key)
	}
	return path, writeRepoRoot(path, &root)
}

// readOrNewRepoRoot reads path's YAML document tree, or builds an empty one
// when path is "" (neither .trellis.yaml nor .trellis.yml exists yet).
func readOrNewRepoRoot(path string) (*yaml.Node, error) {
	if path == "" {
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(root.Content) == 0 {
		root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	return &root, nil
}

// mapValue returns the value node for key in a mapping node, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setMapValueNode sets key's value node in a mapping node, adding the pair
// if key is not already present.
func setMapValueNode(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
}

// deleteMapValue removes key's pair from a mapping node, if present.
func deleteMapValue(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func writeRepoRoot(path string, root *yaml.Node) error {
	out, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, out)
}

// writeFileAtomic writes data to path via a temp file and rename, so a
// process killed mid-write never leaves a torn .trellis.yaml. This is
// separate from internal/core's writeAtomic: a repository config file is not
// knowledge content, has no database row to keep in sync with, and is
// deliberately overwritten on every set/unset rather than written
// no-clobber-once.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-trellis-config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
