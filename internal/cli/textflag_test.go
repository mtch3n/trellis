package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTextValueLiteral(t *testing.T) {
	var v TextValue
	if err := v.Set("hello world"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v.String() != "hello world" {
		t.Errorf("String() = %q, want %q", v.String(), "hello world")
	}
}

func TestTextValueFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.md")
	body := "# Heading\n\nA body with `backticks` and $VARS and \"quotes\".\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var v TextValue
	if err := v.Set("@" + path); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v.String() != body {
		t.Errorf("String() = %q, want the file contents", v.String())
	}
}

func TestTextValueMissingFile(t *testing.T) {
	var v TextValue
	if err := v.Set("@/nonexistent/nope.md"); err == nil {
		t.Fatal("expected an error for a missing @file")
	}
}
