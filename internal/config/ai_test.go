package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetAIKeepsOtherSectionsAndComments(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, []byte("# mine\nclaim:\n  ttl: 1h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ai := AIConfig{
		DefaultProvider: "work",
		Providers: []Provider{{
			ID: "work", Kind: "azure", BaseURL: "https://res.openai.azure.com/openai", Model: "gpt-5", APIKey: "secret",
		}},
	}
	cfg, err := SetAI(root, ai)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Claim.TTL != "1h" {
		t.Fatalf("claim.ttl = %q, want 1h", cfg.Claim.TTL)
	}
	got, ok := cfg.AI.Provider("work")
	if !ok || got.Key() != "secret" || got.Upstream() != "https://res.openai.azure.com/openai" {
		t.Fatalf("provider = %+v", got)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# mine") {
		t.Fatalf("comment lost:\n%s", data)
	}

	if _, err := SetAI(root, AIConfig{}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "ai:") {
		t.Fatalf("empty section still written:\n%s", data)
	}
}

func TestSetAIRejectsInvalidProviders(t *testing.T) {
	root := t.TempDir()
	_, err := SetAI(root, AIConfig{
		DefaultProvider: "missing",
		Providers: []Provider{
			{ID: "Bad ID", Kind: "openai", Model: "gpt-5"},
			{ID: "a", Kind: "nope"},
			{ID: "b", Kind: "azure", Model: "gpt-5"},
			{ID: "c", Kind: "anthropic", Effort: "extreme"},
		},
	})
	ise, ok := errors.AsType[*InvalidSettingsError](err)
	if !ok {
		t.Fatalf("err = %v, want InvalidSettingsError", err)
	}
	if len(ise.Problems) != 6 {
		t.Fatalf("problems = %q, want 6", ise.Problems)
	}
	if _, err := os.Stat(filepath.Join(root, "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("an invalid section was written")
	}
}

func TestProviderKeyPrefersItsEnvironmentVariable(t *testing.T) {
	t.Setenv("TRELLIS_TEST_KEY", "from-env")
	p := Provider{APIKey: "from-file", APIKeyEnv: "TRELLIS_TEST_KEY"}
	if p.Key() != "from-env" {
		t.Fatalf("Key() = %q", p.Key())
	}
	t.Setenv("TRELLIS_TEST_KEY", "")
	if p.Key() != "from-file" {
		t.Fatalf("Key() = %q with the variable empty", p.Key())
	}
}

func TestLocalProvidersNeedNoModel(t *testing.T) {
	ai := AIConfig{Providers: []Provider{{ID: "claude", Kind: "claude-code"}}}
	if problems := ai.Validate(); len(problems) != 0 {
		t.Fatalf("problems = %q", problems)
	}
	if got := ai.Providers[0].Executable(); got != "claude" {
		t.Fatalf("Executable() = %q", got)
	}
}
