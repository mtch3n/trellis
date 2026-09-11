package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// TextValue accepts a literal string, "-" for stdin, or "@path" for a file.
// Long markdown through shell quoting — backticks, $, nested quotes in
// heredocs — is the most likely way an agent gets this CLI wrong, so every
// text flag takes one of these.
type TextValue struct {
	value string
	set   bool
}

func (t *TextValue) String() string { return t.value }
func (t *TextValue) Type() string   { return "text|-|@file" }
func (t *TextValue) Set(raw string) error {
	switch {
	case raw == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		t.value = string(b)
	case strings.HasPrefix(raw, "@"):
		b, err := os.ReadFile(raw[1:])
		if err != nil {
			return fmt.Errorf("reading %s: %w", raw[1:], err)
		}
		t.value = string(b)
	default:
		t.value = raw
	}
	t.set = true
	return nil
}

// Changed reports whether the flag was supplied at all, so a partial update can
// distinguish "not given" from "set to empty".
func (t *TextValue) Changed() bool { return t.set }
