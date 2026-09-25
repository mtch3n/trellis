package config

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// AIConfig is the "ai" section of config.yaml: the providers the web UI's
// chat can talk to, and which one it opens with. It is global only. No
// project or repository file can set it, because a provider carries a
// credential and a local agent names a program to run.
type AIConfig struct {
	DefaultProvider string     `yaml:"default_provider,omitempty"`
	Providers       []Provider `yaml:"providers,omitempty"`
}

// Provider is one configured source of model output: an API reached through
// the daemon's credential proxy, or an agent CLI the daemon runs.
type Provider struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name,omitempty"`
	Kind string `yaml:"kind"`
	// BaseURL is the upstream API root. Kinds with a public endpoint fall
	// back to it when this is empty; azure and openai-compatible require it.
	BaseURL string `yaml:"base_url,omitempty"`
	Model   string `yaml:"model,omitempty"`
	// Effort is how hard the model reasons by default: low, medium, high or
	// xhigh. Empty leaves it to the model. A chat can override it per turn.
	Effort string `yaml:"effort,omitempty"`
	// APIKey is set in the file; APIKeyEnv names an environment variable
	// that overrides it when the variable is set in the daemon's environment.
	APIKey    string `yaml:"api_key,omitempty"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
	// Command and Dir are for local agents: the executable (a name on PATH or
	// a path) and the working directory it starts in.
	Command string `yaml:"command,omitempty"`
	Dir     string `yaml:"dir,omitempty"`
}

// ProviderKind describes one kind of provider: how the UI labels it, whether
// the daemon runs it as a local process, and where its API lives.
type ProviderKind struct {
	Kind           string `json:"kind"`
	Label          string `json:"label"`
	Local          bool   `json:"local"`
	DefaultBaseURL string `json:"default_base_url,omitempty"`
	DefaultCommand string `json:"default_command,omitempty"`
	NeedsBaseURL   bool   `json:"needs_base_url"`
	NeedsKey       bool   `json:"needs_key"`
}

// ProviderKinds lists every kind a provider may have, in the order the UI
// offers them.
func ProviderKinds() []ProviderKind {
	return []ProviderKind{
		{Kind: "openai", Label: "OpenAI", DefaultBaseURL: "https://api.openai.com/v1", NeedsKey: true},
		{Kind: "azure", Label: "Azure OpenAI", NeedsBaseURL: true, NeedsKey: true},
		{Kind: "anthropic", Label: "Anthropic", DefaultBaseURL: "https://api.anthropic.com/v1", NeedsKey: true},
		{Kind: "google", Label: "Google Gemini", DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta", NeedsKey: true},
		{Kind: "openai-compatible", Label: "OpenAI-compatible", NeedsBaseURL: true},
		{Kind: "claude-code", Label: "Claude Code", Local: true, DefaultCommand: "claude"},
		{Kind: "codex", Label: "Codex", Local: true, DefaultCommand: "codex"},
	}
}

// LookupProviderKind returns the kind named kind.
func LookupProviderKind(kind string) (ProviderKind, bool) {
	i := slices.IndexFunc(ProviderKinds(), func(k ProviderKind) bool { return k.Kind == kind })
	if i < 0 {
		return ProviderKind{}, false
	}
	return ProviderKinds()[i], true
}

// Key returns the credential the daemon sends upstream: the environment
// variable APIKeyEnv names when it is set, else APIKey.
func (p Provider) Key() string {
	if p.APIKeyEnv != "" {
		if v := os.Getenv(p.APIKeyEnv); v != "" {
			return v
		}
	}
	return p.APIKey
}

// Upstream returns the API root requests are forwarded to.
func (p Provider) Upstream() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	kind, _ := LookupProviderKind(p.Kind)
	return kind.DefaultBaseURL
}

// Executable returns the program a local agent runs.
func (p Provider) Executable() string {
	if p.Command != "" {
		return p.Command
	}
	kind, _ := LookupProviderKind(p.Kind)
	return kind.DefaultCommand
}

// Provider returns the provider with id.
func (a AIConfig) Provider(id string) (Provider, bool) {
	i := slices.IndexFunc(a.Providers, func(p Provider) bool { return p.ID == id })
	if i < 0 {
		return Provider{}, false
	}
	return a.Providers[i], true
}

var providerID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Validate reports every problem with the section, so a caller can show them
// together.
func (a AIConfig) Validate() []string {
	var problems []string
	seen := map[string]bool{}
	for _, p := range a.Providers {
		if !providerID.MatchString(p.ID) {
			problems = append(problems, fmt.Sprintf("provider id %q: use lowercase letters, digits and hyphens, at most 40", p.ID))
		}
		if seen[p.ID] {
			problems = append(problems, fmt.Sprintf("provider id %q is used twice", p.ID))
		}
		seen[p.ID] = true
		kind, ok := LookupProviderKind(p.Kind)
		if !ok {
			problems = append(problems, fmt.Sprintf("provider %s: unknown kind %q", p.ID, p.Kind))
			continue
		}
		if kind.NeedsBaseURL && p.BaseURL == "" {
			problems = append(problems, fmt.Sprintf("provider %s: %s needs a base URL", p.ID, kind.Label))
		}
		if !ValidEffort(p.Effort) {
			problems = append(problems, fmt.Sprintf("provider %s: effort must be one of %s, got %q", p.ID, strings.Join(Efforts, ", "), p.Effort))
		}
		if !kind.Local && p.Model == "" {
			problems = append(problems, fmt.Sprintf("provider %s: a model is required", p.ID))
		}
	}
	if a.DefaultProvider != "" && !seen[a.DefaultProvider] {
		problems = append(problems, fmt.Sprintf("default provider %q does not exist", a.DefaultProvider))
	}
	return problems
}

// SetAI replaces the "ai" section of config.yaml inside root, keeping every
// other section and its comments. Nothing is written when the section is
// invalid. It returns the config Load now reads back.
func SetAI(root string, ai AIConfig) (Config, error) {
	if problems := ai.Validate(); len(problems) > 0 {
		return Config{}, &InvalidSettingsError{Problems: problems}
	}
	path := configPath(root)
	yamlRoot, err := readOrNewConfigRoot(path)
	if err != nil {
		return Config{}, err
	}
	body := yamlRoot.Content[0]
	if len(ai.Providers) == 0 && ai.DefaultProvider == "" {
		deleteMapValue(body, "ai")
	} else {
		var node yaml.Node
		if err := node.Encode(ai); err != nil {
			return Config{}, err
		}
		setMapValueNode(body, "ai", &node)
	}
	out, err := yaml.Marshal(yamlRoot)
	if err != nil {
		return Config{}, err
	}
	if _, err := parseConfigBytes(out); err != nil {
		return Config{}, &InvalidSettingsError{Problems: []string{err.Error()}}
	}
	if err := writeConfigAtomic(path, out); err != nil {
		return Config{}, err
	}
	return Load(root)
}

// Efforts lists every effort a provider or a chat may ask for, lowest first.
// Every provider kind takes these words as they are.
var Efforts = []string{"low", "medium", "high", "xhigh"}

// ValidEffort reports whether effort is one of Efforts, or empty for the
// model's own default.
func ValidEffort(effort string) bool { return effort == "" || slices.Contains(Efforts, effort) }

// ValidProviderID reports whether id can name a provider.
func ValidProviderID(id string) bool { return providerID.MatchString(id) }
