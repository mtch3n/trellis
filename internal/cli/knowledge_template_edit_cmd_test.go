package cli

import (
	"strings"
	"testing"
)

// knowledge edit prints a warn template's problems, as knowledge new does.
func TestKnowledgeEditPrintsTemplateWarnings(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Latency", "--template", "research")
	out := runCmd(t, "knowledge", "edit", "latency", "--body", "# Latency\n", "--if-version", "1")
	if !strings.Contains(out, `"warnings":["missing section Question"`) {
		t.Errorf("edit output:\n%s", out)
	}
}
