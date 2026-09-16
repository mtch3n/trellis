package cli

import (
	"os"
	"strings"
	"testing"
)

func TestKnowledgeNewSourceIsRepeatable(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "knowledge", "new", "--title", "Cited note",
		"--source", "https://example.com/a", "--source", "https://example.com/b", "--json")
	if !strings.Contains(out, "example.com/a") || !strings.Contains(out, "example.com/b") {
		t.Errorf("both sources were not recorded:\n%s", out)
	}
}

func TestKnowledgeEditSourceReplacesTheList(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Backfill me")

	runCmd(t, "knowledge", "edit", "backfill-me", "--source", "https://example.com/a", "--if-version", "1")

	out := runCmd(t, "knowledge", "show", "backfill-me", "--json")
	if !strings.Contains(out, "example.com/a") {
		t.Errorf("source was not backfilled:\n%s", out)
	}
}

func TestKnowledgeEditRequiresBodyOrSource(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Untouched")
	if _, err := runCmdErr(t, "knowledge", "edit", "untouched", "--if-version", "1"); cliErrCode(err) != "missing_body" {
		t.Errorf("err = %v, want missing_body", err)
	}
}

func TestKnowledgeNewSetRefusesSourcesByName(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "knowledge", "new", "--title", "X", "--set", "sources=https://example.com"); cliErrCode(err) != "reserved_field" {
		t.Errorf("err = %v, want reserved_field", err)
	}
}

// StringSliceVar splits on commas, so --source "RFC 2616, section 5" would
// land as two entries instead of one source whose text contains a comma.
// --source uses StringArrayVar to keep the value intact.
func TestKnowledgeNewSourcePreservesCommas(t *testing.T) {
	projectEnv(t)
	path := newEntry(t, "--title", "X", "--source", "RFC 2616, section 5")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "RFC 2616, section 5") {
		t.Errorf("file = %q, want the source's comma intact", string(raw))
	}
}
