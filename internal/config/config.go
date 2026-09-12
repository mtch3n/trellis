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
