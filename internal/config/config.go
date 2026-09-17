// Package config loads and manages trellis settings: global config from YAML
// and per-project overrides from the database.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"gopkg.in/yaml.v3"
)

// Config represents the full settings tree. All fields must have sensible
// zero-value defaults since YAML parsing leaves unset fields as zero values.
type Config struct {
	UI      UIConfig      `yaml:"ui"`
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

type LeaseConfig struct {
	TTL string `yaml:"ttl"` // e.g., "30m"
}

type BoardConfig struct {
	DefaultColumns []string `yaml:"default_columns"`
}

type LabelsConfig struct {
	RequireOnCard bool `yaml:"require_on_card"`
}

type TagsConfig struct {
	RequireOnCard bool `yaml:"require_on_card"`
}

type CardConfig struct {
	LsLimit int `yaml:"ls_limit"`
}

type SearchConfig struct {
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
		Lease: LeaseConfig{
			TTL: "30m",
		},
		Board: BoardConfig{
			DefaultColumns: []string{"backlog", "in-progress", "review", "done"},
		},
		Labels: LabelsConfig{
			RequireOnCard: false,
		},
		Tags: TagsConfig{
			RequireOnCard: false,
		},
		Card: CardConfig{
			LsLimit: 50,
		},
		Search: SearchConfig{
			Method: "fts",
			Vector: VectorSearchConfig{Limit: 10, ChunkSize: 1200, ChunkOverlap: 200},
		},
		History: HistoryConfig{Keep: ptr(100)},
	}
}

// configPath returns config.yaml inside root. The caller resolves root
// (TRELLIS_HOME or the platform default) and passes it in, so a pinned root
// whose config still came from a different home never serves the wrong port
// for the daemon installed against it.
func configPath(root string) string {
	return filepath.Join(root, "config.yaml")
}

// Path returns config.yaml's path inside root, for a caller -- the settings
// API -- that reports where the file lives without loading it.
func Path(root string) string { return configPath(root) }

func ptr[T any](v T) *T { return &v }

// Load reads and parses the global config file inside root. Returns
// Defaults() if the file does not exist. Returns an error if the file exists
// but is malformed.
func Load(root string) (Config, error) {
	cfg := Defaults()
	path := configPath(root)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// File not present; return defaults with no error.
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	return parseConfigBytes(data)
}

// parseConfigBytes parses config.yaml's bytes and fills in defaults for any
// field the file left unset. It is the exact path both Load and
// SetGlobalValues use: the latter parses its freshly written bytes through
// this before writing anything, so it can never write a file Load would
// later reject.
func parseConfigBytes(data []byte) (Config, error) {
	cfg := Defaults()
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
	if cfg.Lease.TTL == "" {
		cfg.Lease.TTL = defaults.Lease.TTL
	}
	if len(cfg.Board.DefaultColumns) == 0 {
		cfg.Board.DefaultColumns = defaults.Board.DefaultColumns
	}
	if cfg.Card.LsLimit == 0 {
		cfg.Card.LsLimit = defaults.Card.LsLimit
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
	case "lease.ttl":
		return cfg.Lease.TTL, true
	case "board.default_columns":
		// For arrays, return comma-separated values.
		return fmt.Sprintf("[%s]", fmt.Sprint(cfg.Board.DefaultColumns)), true
	case "labels.require_on_card":
		return fmt.Sprintf("%v", cfg.Labels.RequireOnCard), true
	case "tags.require_on_card":
		return fmt.Sprintf("%v", cfg.Tags.RequireOnCard), true
	case "card.ls_limit":
		return fmt.Sprintf("%d", cfg.Card.LsLimit), true
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
// being a known key: history.keep's zero disables capture but its negative
// values are nonsensical, not a synonym for "unlimited"; lease.ttl is a
// wait, so a zero or negative duration means "immediately"; search.method
// must name a method the retrieval service actually implements.
func ValidateValue(key, value string) error {
	switch key {
	case "history.keep":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("history.keep: must be a whole number, got %q", value)
		}
		if n < 0 {
			return fmt.Errorf("history.keep: must not be negative, got %d", n)
		}
		return nil
	case "lease.ttl":
		return validatePositiveDuration(key, value)
	case "search.method":
		return validateChoice(key, value, searchMethods)
	default:
		return nil
	}
}

// validatePositiveDuration rejects a value for key that does not parse as a
// duration greater than zero.
func validatePositiveDuration(key, raw string) error {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("%s: must be a duration, got %q", key, raw)
	}
	if d <= 0 {
		return fmt.Errorf("%s: must be a positive duration, got %q", key, raw)
	}
	return nil
}

// validateChoice rejects a value for key that is not one of choices.
func validateChoice(key, raw string, choices []string) error {
	if !slices.Contains(choices, raw) {
		return fmt.Errorf("%s: must be one of %s, got %q", key, strings.Join(choices, ", "), raw)
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
	case "card.ls_limit",
		"lease.ttl",
		"board.default_columns",
		"labels.require_on_card",
		"tags.require_on_card",
		"search.method":
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

// searchMethods lists every value search.method accepts: internal/retrieval's
// Service.vectorConfig and the CLI's own --method flag recognize exactly
// these three.
var searchMethods = []string{"fts", "vector", "hybrid"}

// setConfigField decodes one repository-safe dotted key's YAML value into the
// matching field of cfg, and rejects a value that would not parse for its
// key -- not merely one of the wrong YAML type. A repository file is
// committed and arrives with every clone, so a value only type-checked here
// (lease.ttl and search.method are both plain strings, so any string passes
// a type check) can silently disable the setting for everyone who clones it.
// Every key RepoSafe allows is handled here.
func setConfigField(cfg *Config, key string, node *yaml.Node) error {
	switch key {
	case "card.ls_limit":
		if err := node.Decode(&cfg.Card.LsLimit); err != nil {
			return err
		}
		return positiveNumber(cfg.Card.LsLimit)
	case "lease.ttl":
		var raw string
		if err := node.Decode(&raw); err != nil {
			return err
		}
		if _, err := time.ParseDuration(raw); err != nil {
			return fmt.Errorf("not a duration: %w", err)
		}
		cfg.Lease.TTL = raw
		return nil
	case "board.default_columns":
		return node.Decode(&cfg.Board.DefaultColumns)
	case "labels.require_on_card":
		return node.Decode(&cfg.Labels.RequireOnCard)
	case "tags.require_on_card":
		return node.Decode(&cfg.Tags.RequireOnCard)
	case "search.method":
		var raw string
		if err := node.Decode(&raw); err != nil {
			return err
		}
		if !slices.Contains(searchMethods, raw) {
			return fmt.Errorf("must be one of %s, got %q", strings.Join(searchMethods, ", "), raw)
		}
		cfg.Search.Method = raw
		return nil
	default:
		return fmt.Errorf("not a repository-safe key")
	}
}

// positiveNumber rejects zero and negative values for a key named
// "*_limit": it means "how many", and a repository file should not be able
// to turn that into "unlimited" or "none" by accident.
func positiveNumber(n int) error {
	if n <= 0 {
		return fmt.Errorf("must be a positive number, got %d", n)
	}
	return nil
}

// AllKeys lists every dotted config key GetValue understands, in the order
// `config ls` displays them. internal/cli/config.go's newConfigLsCmd uses
// this instead of keeping its own copy of the list.
func AllKeys() []string {
	return []string{
		"ui.port", "ui.bind", "ui.enabled",
		"lease.ttl",
		"board.default_columns",
		"labels.require_on_card",
		"tags.require_on_card",
		"card.ls_limit",
		"search.method",
		"search.vector.enabled", "search.vector.provider", "search.vector.embed_command", "search.vector.endpoint",
		"search.vector.model", "search.vector.dimension", "search.vector.limit",
		"history.keep",
	}
}

// ValueType is the JSON shape a setting's value and default take in the
// settings API: a number, a bool, a string, or a list of strings.
type ValueType string

const (
	TypeInt      ValueType = "int"
	TypeNumber   ValueType = "number"
	TypeBool     ValueType = "bool"
	TypeString   ValueType = "string"
	TypeDuration ValueType = "duration"
	TypeEnum     ValueType = "enum"
	TypeList     ValueType = "list"
)

// KeyInfo is one config key's metadata for the settings API: its JSON type,
// its closed choices or lower bound where either applies, whether the web
// settings page may change it, and whether the running daemon needs a
// restart before a new value takes effect.
type KeyInfo struct {
	Key         string    `json:"key"`
	Type        ValueType `json:"type"`
	Choices     []string  `json:"choices,omitempty"`
	Min         *int      `json:"min,omitempty"`
	Editable    bool      `json:"editable"`
	Restart     bool      `json:"restart"`
	Description string    `json:"description"`
}

// Describe returns every key AllKeys lists, in the same order, with the
// metadata the settings API needs. TestDescribeCoversAllKeysExactly enforces
// that the two lists match exactly, so a new config key fails the build
// until it is described here too.
//
// editable is false for every ui.* key and every search.vector.* key: they
// configure the daemon's listen address and port, or where vault text is
// sent for embedding, and the web settings page must not be able to change
// either from inside the browser it would then be talking to.
//
// restart is true for a key the running daemon only reads once, building
// something that lives for the process: the listener (ui.*) and the
// retrieval service's search method and vector settings, both captured at
// daemon startup and never re-read. It is false for the five keys the daemon
// re-applies to its Core on every settings change (Core.ApplyConfig) and for
// card.ls_limit, which the CLI alone reads fresh on each invocation.
func Describe() []KeyInfo {
	return []KeyInfo{
		{Key: "ui.port", Type: TypeInt, Editable: false, Restart: true,
			Description: "The port the daemon's web UI and API listen on."},
		{Key: "ui.bind", Type: TypeString, Editable: false, Restart: true,
			Description: "The loopback address the daemon's web UI and API bind to."},
		{Key: "ui.enabled", Type: TypeBool, Editable: false, Restart: true,
			Description: "Whether the daemon serves the web UI at all, or stays on local IPC only."},
		{Key: "lease.ttl", Type: TypeDuration, Editable: true, Restart: false,
			Description: "How long a claim lasts before it expires."},
		{Key: "board.default_columns", Type: TypeList, Editable: true, Restart: false,
			Description: "The columns a new board starts with."},
		{Key: "labels.require_on_card", Type: TypeBool, Editable: true, Restart: false,
			Description: "Whether a card must carry at least one label."},
		{Key: "tags.require_on_card", Type: TypeBool, Editable: true, Restart: false,
			Description: "Whether a card must carry at least one tag."},
		{Key: "card.ls_limit", Type: TypeInt, Editable: true, Restart: false,
			Description: "The default number of cards a listing returns."},
		{Key: "search.method", Type: TypeEnum, Choices: slices.Clone(searchMethods), Editable: true, Restart: true,
			Description: "Which method finds cards and knowledge entries: full-text, vector, or hybrid."},
		{Key: "search.vector.enabled", Type: TypeBool, Editable: false, Restart: true,
			Description: "Whether the optional semantic vector index is built and searched."},
		{Key: "search.vector.provider", Type: TypeString, Editable: false, Restart: true,
			Description: "How embeddings are produced: command, http, or local."},
		{Key: "search.vector.embed_command", Type: TypeString, Editable: false, Restart: true,
			Description: "The command that turns text into an embedding vector."},
		{Key: "search.vector.endpoint", Type: TypeString, Editable: false, Restart: true,
			Description: "The HTTP endpoint embeddings are requested from."},
		{Key: "search.vector.model", Type: TypeString, Editable: false, Restart: true,
			Description: "The embedding model name sent to the provider."},
		{Key: "search.vector.dimension", Type: TypeInt, Editable: false, Restart: true,
			Description: "The embedding vector's length."},
		{Key: "search.vector.limit", Type: TypeInt, Editable: false, Restart: true,
			Description: "The default number of vector search results."},
		{Key: "history.keep", Type: TypeInt, Min: ptr(0), Editable: true, Restart: false,
			Description: "How many revisions each knowledge entry and card retains."},
	}
}

// describeIndex is Describe() keyed by dotted key, for a caller that looks
// up one key's metadata rather than walking the whole list.
func describeIndex() map[string]KeyInfo {
	out := make(map[string]KeyInfo, len(AllKeys()))
	for _, info := range Describe() {
		out[info.Key] = info
	}
	return out
}

// TypedValue returns key's value from cfg as the typed JSON value the
// settings API reports -- a number, a bool, a string, or a list of strings
// -- rather than GetValue's text form. ok is false for a key Describe does
// not know.
func TypedValue(cfg Config, key string) (value any, ok bool) {
	switch key {
	case "ui.port":
		return cfg.UI.Port, true
	case "ui.bind":
		return cfg.UI.Bind, true
	case "ui.enabled":
		return cfg.UI.UIEnabled(), true
	case "lease.ttl":
		return cfg.Lease.TTL, true
	case "board.default_columns":
		return slices.Clone(cfg.Board.DefaultColumns), true
	case "labels.require_on_card":
		return cfg.Labels.RequireOnCard, true
	case "tags.require_on_card":
		return cfg.Tags.RequireOnCard, true
	case "card.ls_limit":
		return cfg.Card.LsLimit, true
	case "search.method":
		return cfg.Search.Method, true
	case "search.vector.enabled":
		return cfg.Search.Vector.Enabled, true
	case "search.vector.provider":
		return cfg.Search.Vector.Provider, true
	case "search.vector.embed_command":
		return cfg.Search.Vector.EmbedCommand, true
	case "search.vector.endpoint":
		return cfg.Search.Vector.Endpoint, true
	case "search.vector.model":
		return cfg.Search.Vector.Model, true
	case "search.vector.dimension":
		return cfg.Search.Vector.Dimension, true
	case "search.vector.limit":
		return cfg.Search.Vector.Limit, true
	case "history.keep":
		return cfg.History.EffectiveKeep(), true
	default:
		return nil, false
	}
}

// InvalidSettingsError reports every problem found while validating a batch
// of settings changes together, so a caller such as the settings API can
// show all of them at once instead of stopping at the first.
type InvalidSettingsError struct {
	Problems []string
}

func (e *InvalidSettingsError) Error() string {
	return "invalid settings: " + strings.Join(e.Problems, "; ")
}

// SetGlobalValues edits the global config.yaml inside root as a YAML node
// tree: every entry in set and unset is validated first, and nothing is
// written if any of them is invalid. unset removes the key so the built-in
// default applies again. Comments and unknown top-level sections such as
// "extensions:" survive, the same way SetRepoValue preserves them in a
// repository file. It returns the config Load would now read back.
func SetGlobalValues(root string, set map[string]any, unset []string) (Config, error) {
	infos := describeIndex()

	setKeys := make([]string, 0, len(set))
	for k := range set {
		setKeys = append(setKeys, k)
	}
	sort.Strings(setKeys)
	unsetKeys := slices.Clone(unset)
	sort.Strings(unsetKeys)

	var problems []string
	nodes := make(map[string]*yaml.Node, len(set))
	for _, key := range setKeys {
		info, ok := infos[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: unknown key", key))
			continue
		}
		if !info.Editable {
			problems = append(problems, fmt.Sprintf("%s: not editable", key))
			continue
		}
		node, problem := settingNode(info, set[key])
		if problem != "" {
			problems = append(problems, problem)
			continue
		}
		nodes[key] = node
	}
	for _, key := range unsetKeys {
		info, ok := infos[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: unknown key", key))
			continue
		}
		if !info.Editable {
			problems = append(problems, fmt.Sprintf("%s: not editable", key))
		}
	}
	if len(problems) > 0 {
		return Config{}, &InvalidSettingsError{Problems: problems}
	}

	path := configPath(root)
	yamlRoot, err := readOrNewConfigRoot(path)
	if err != nil {
		return Config{}, err
	}
	body := yamlRoot.Content[0]
	for key, node := range nodes {
		setNestedValue(body, strings.Split(key, "."), node)
	}
	for _, key := range unsetKeys {
		deleteNestedValue(body, strings.Split(key, "."))
	}

	out, err := yaml.Marshal(yamlRoot)
	if err != nil {
		return Config{}, err
	}
	// Never write a file the loader would reject: parse the exact bytes
	// through the exact path Load uses before they ever reach disk.
	if _, err := parseConfigBytes(out); err != nil {
		return Config{}, &InvalidSettingsError{Problems: []string{err.Error()}}
	}

	if err := writeConfigAtomic(path, out); err != nil {
		return Config{}, err
	}
	return Load(root)
}

// settingNode converts value -- a set entry's JSON-decoded value, or a Go
// value a direct caller such as a test passes -- into the YAML node
// SetGlobalValues writes for info's key, or a problem string naming what is
// wrong. It accepts both encoding/json/v2's decoded shapes (float64, []any)
// and native Go ones (int, []string), so a package-internal test can call
// SetGlobalValues without going through JSON first.
func settingNode(info KeyInfo, value any) (*yaml.Node, string) {
	switch info.Type {
	case TypeInt:
		n, ok := asWholeNumber(value)
		if !ok {
			return nil, fmt.Sprintf("%s: must be a whole number", info.Key)
		}
		if info.Min != nil && n < *info.Min {
			return nil, fmt.Sprintf("%s: must be at least %d, got %d", info.Key, *info.Min, n)
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(n)}, ""
	case TypeNumber:
		f, ok := asNumber(value)
		if !ok {
			return nil, fmt.Sprintf("%s: must be a number", info.Key)
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatFloat(f, 'g', -1, 64)}, ""
	case TypeBool:
		b, ok := value.(bool)
		if !ok {
			return nil, fmt.Sprintf("%s: must be true or false", info.Key)
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatBool(b)}, ""
	case TypeDuration:
		s, ok := value.(string)
		if !ok {
			return nil, fmt.Sprintf("%s: must be a duration string", info.Key)
		}
		if err := validatePositiveDuration(info.Key, s); err != nil {
			return nil, err.Error()
		}
		return quotedScalar(s), ""
	case TypeEnum:
		s, ok := value.(string)
		if !ok {
			return nil, fmt.Sprintf("%s: must be one of %s", info.Key, strings.Join(info.Choices, ", "))
		}
		if err := validateChoice(info.Key, s, info.Choices); err != nil {
			return nil, err.Error()
		}
		return quotedScalar(s), ""
	case TypeString:
		s, ok := value.(string)
		if !ok {
			return nil, fmt.Sprintf("%s: must be a string", info.Key)
		}
		return quotedScalar(s), ""
	case TypeList:
		items, ok := asStringList(value)
		if !ok {
			return nil, fmt.Sprintf("%s: must be a list of strings", info.Key)
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range items {
			seq.Content = append(seq.Content, quotedScalar(item))
		}
		return seq, ""
	default:
		return nil, fmt.Sprintf("%s: unsupported type", info.Key)
	}
}

// quotedScalar wraps a string-typed setting value in an explicitly
// double-quoted YAML scalar. board.default_columns ["true", "123"] must
// come back as those exact strings, not the bool and int a plain scalar
// would resolve to on the next parse -- by us or by anything else that
// reads config.yaml -- so every string, duration, enum and list-item value
// SetGlobalValues writes is quoted, never left to YAML's own type guessing.
func quotedScalar(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: s}
}

// asWholeNumber accepts an int, an int64, or a float64 with no fractional
// part -- encoding/json/v2 decodes every JSON number as float64, but a
// package-internal caller may pass a native Go int instead.
func asWholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int64(n)) {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

// asNumber accepts any of the numeric shapes asWholeNumber does, without
// requiring a whole number.
func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// asStringList accepts a []string directly, or encoding/json/v2's decoded
// []any of strings.
func asStringList(v any) ([]string, bool) {
	switch items := v.(type) {
	case []string:
		return items, true
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

// readOrNewConfigRoot reads path's YAML document tree for editing, or builds
// an empty mapping when the file does not exist yet: config.yaml is
// optional, and Load already treats a missing file as all-defaults.
func readOrNewConfigRoot(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}, nil
		}
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

// setNestedValue sets the value at path inside mapping node m, creating
// intermediate mappings as needed. Unlike setMapValueNode, which
// SetRepoValue uses for a repository file's flat dotted-string keys,
// config.yaml is a real nested tree -- "lease.ttl" lives at m["lease"]["ttl"]
// -- so this walks path one segment at a time.
func setNestedValue(m *yaml.Node, path []string, value *yaml.Node) {
	if len(path) == 1 {
		setMapValueNode(m, path[0], value)
		return
	}
	child := mapValue(m, path[0])
	if child == nil || child.Kind != yaml.MappingNode {
		child = &yaml.Node{Kind: yaml.MappingNode}
		setMapValueNode(m, path[0], child)
	}
	setNestedValue(child, path[1:], value)
}

// deleteNestedValue removes the value at path inside mapping node m, if
// present. A missing intermediate mapping means there is nothing to remove.
func deleteNestedValue(m *yaml.Node, path []string) {
	if len(path) == 1 {
		deleteMapValue(m, path[0])
		return
	}
	child := mapValue(m, path[0])
	if child == nil || child.Kind != yaml.MappingNode {
		return
	}
	deleteNestedValue(child, path[1:])
}

// writeConfigAtomic writes data to path -- config.yaml -- through a temp
// file in the same directory: create at mode 0600, write, fsync, close,
// rename. A process killed mid-write never leaves a torn config.yaml.
func writeConfigAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-trellis-config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// LoadWithPresence is Load, plus which dotted keys the global file itself
// set. This is why it exists: EffectiveValue must report "config", not
// "default", for a value that came from the file, and by the time Load
// applies its defaults onto an unset field the two are indistinguishable.
func LoadWithPresence(root string) (Config, map[string]bool, error) {
	cfg := Defaults()
	path := configPath(root)
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
// value equal to the zero value (ui.enabled: true) is indistinguishable from
// absence — the same limitation applyDefaults already has no way around.
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
		case "lease.ttl":
			cfg.Lease.TTL = repo.Config.Lease.TTL
		case "board.default_columns":
			cfg.Board.DefaultColumns = repo.Config.Board.DefaultColumns
		case "labels.require_on_card":
			cfg.Labels.RequireOnCard = repo.Config.Labels.RequireOnCard
		case "tags.require_on_card":
			cfg.Tags.RequireOnCard = repo.Config.Tags.RequireOnCard
		case "search.method":
			cfg.Search.Method = repo.Config.Search.Method
		}
	}
	return cfg
}

// repoValueNode builds the YAML node SetRepoValue would write for key/value:
// a sequence for board.default_columns, a scalar for everything else. It is
// shared with ValidateRepoValue, so both check the exact node that would be
// written.
func repoValueNode(key, value string) *yaml.Node {
	if key == "board.default_columns" {
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range strings.Split(value, ",") {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: strings.TrimSpace(item)})
		}
		return seq
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Value: value}
}

// ValidateRepoValue reports whether value would decode for key the way
// LoadRepo's setConfigField does, without writing anything: a caller such as
// `config set --repo` can refuse a bad value up front, before touching the
// file, with the same complaint the file's own loader would give on its next
// read.
func ValidateRepoValue(key, value string) error {
	if !RepoSafe(key) {
		return fmt.Errorf("%q may not be set by a repository", key)
	}
	var scratch Config
	return setConfigField(&scratch, key, repoValueNode(key, value))
}

// SetRepoValue writes key = value into dir's repository config file under
// "config:", creating .trellis.yaml if neither file exists yet, and
// preserving every other key and the "extensions" section untouched. key
// must be RepoSafe, and value must be one setConfigField accepts: a
// repository file is committed and arrives with every clone, so a value this
// loader would itself reject must never be written -- it would break every
// command that resolves a repository-scoped setting, in every clone, until
// someone notices and edits the file by hand.
func SetRepoValue(dir, key, value string) (string, error) {
	valueNode := repoValueNode(key, value)
	if err := ValidateRepoValue(key, value); err != nil {
		return "", err
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
