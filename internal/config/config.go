// Package config loads and manages trellis settings: global config from YAML
// and per-project overrides from the database.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/home"
	"gopkg.in/yaml.v3"
)

// Config represents the full settings tree. All fields must have sensible
// zero-value defaults since YAML parsing leaves unset fields as zero values.
type Config struct {
	UI     UIConfig     `yaml:"ui"`
	DB     DBConfig     `yaml:"db"`
	Git    GitConfig    `yaml:"git"`
	Lease  LeaseConfig  `yaml:"lease"`
	Board  BoardConfig  `yaml:"board"`
	Labels LabelsConfig `yaml:"labels"`
	Tags   TagsConfig   `yaml:"tags"`
	Card   CardConfig   `yaml:"card"`
	Search SearchConfig `yaml:"search"`
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
	default:
		return "", false
	}
}

// EffectiveValue returns the effective value for a key, resolving defaults then
// project overrides. source will be "default", "config", or "project".
func EffectiveValue(ctx context.Context, cfg Config, db *sqlx.DB, projectID, key string) (value string, source string, err error) {
	// Start with the default.
	value, found := GetValue(cfg, key)
	if !found {
		return "", "", fmt.Errorf("unknown config key: %q", key)
	}
	source = "default"

	// Check for project override.
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
