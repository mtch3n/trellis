package platform

import (
	"strings"
	"testing"
)

func TestCheckKnowledgeQuality(t *testing.T) {
	t.Run("accepts clean finding", func(t *testing.T) {
		body := "# Vector index\n\n## Fact\nRebuilds on every write above 10k rows.\n\n## Evidence\nMeasured in bench.go.\n\n## Scope\nPostgres 17.\n"
		if got := checkKnowledgeQuality("finding", "Vector index rebuilds on every write above 10k rows", body); len(got) != 0 {
			t.Fatalf("unexpected problems: %v", got)
		}
	})

	t.Run("ignores non-knowledge templates", func(t *testing.T) {
		bigCode := "```bash\n" + strings.Repeat("echo line\n", 20) + "```\n"
		if got := checkKnowledgeQuality("runbook", "Notes on deploy", bigCode); len(got) != 0 {
			t.Fatalf("runbook should not be rejected: %v", got)
		}
		if got := checkKnowledgeQuality("", "Notes on x", bigCode); len(got) != 0 {
			t.Fatalf("untemplated entry should not be rejected: %v", got)
		}
	})

	t.Run("rejects large code block dump", func(t *testing.T) {
		body := "## Fact\nBad.\n\n```go\n" + strings.Repeat("x := 1\n", 15) + "```\n\n## Evidence\nLog.\n\n## Scope\nAll.\n"
		got := checkKnowledgeQuality("finding", "Concrete summary that is long enough", body)
		if len(got) == 0 || !strings.Contains(got[0], "fenced code block") {
			t.Fatalf("expected code block violation, got: %v", got)
		}
	})

	t.Run("rejects vague or short summary", func(t *testing.T) {
		got := checkKnowledgeQuality("decision", "Notes on caching", "## Context\nX\n")
		if len(got) == 0 || !strings.Contains(got[0], "too vague") {
			t.Fatalf("expected vague summary violation, got: %v", got)
		}
		gotShort := checkKnowledgeQuality("finding", "Too short", "## Fact\nX\n")
		if len(gotShort) == 0 || !strings.Contains(gotShort[0], "too short") {
			t.Fatalf("expected short summary violation, got: %v", gotShort)
		}
	})

	t.Run("rejects copied open-questions table", func(t *testing.T) {
		rows := strings.Repeat("| Q | Status |\n", 10)
		body := "## Fact\nCopied.\n\n## Open Questions\n| Q | Status |\n|---|---|\n" + rows
		got := checkKnowledgeQuality("finding", "Concrete summary that is long enough", body)
		if len(got) < 2 {
			t.Fatalf("expected doc copy + table violations, got: %v", got)
		}
	})
}
