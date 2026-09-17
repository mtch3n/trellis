package cli

import (
	"strings"
	"testing"
)

// vault edit prints a warn template's warnings, as vault new does.
func TestVaultEditPrintsTemplateWarnings(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "Latency", "--template", "research")
	out := runCmd(t, "vault", "edit", "latency", "--body", "# Latency\n", "--if-version", "1")
	if !strings.Contains(out, `"warnings":["missing section Question"`) {
		t.Errorf("edit output:\n%s", out)
	}
}
