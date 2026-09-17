package cli

import (
	"testing"
)

// review-cli #5: a knowledge slug that merely looks like a qualified card ref
// (PREFIX-N) must not be routed as a card of a project named PREFIX when no
// such project exists.
func TestGraphSlugEndingInDigitsIsAnEntryNotACard(t *testing.T) {
	targetEnv(t)
	if got := refOf(t, "knowledge", "new", "--title", "Release 2026"); got == "" {
		t.Fatal("seed entry")
	}
	// The slug core.SlugifyPath derives from "Release 2026" is "release-2026",
	// which vpath.ValidCardRef also accepts as a card ref of project RELEASE.
	if got := refOf(t, "knowledge", "show", "release-2026"); got != "/ALPHA/vault/release-2026" {
		t.Fatalf("knowledge show release-2026 = %s", got)
	}
	nodes := refsIn(t, runCmd(t, "graph", "release-2026", "--json"), "nodes")
	if len(nodes) == 0 || nodes[0] != "/ALPHA/vault/release-2026" {
		t.Errorf("graph release-2026 = %v, want the entry itself", nodes)
	}
}

// A genuine qualified card ref, including one under a merged key, still
// routes to the card path.
func TestGraphStillRoutesAQualifiedCardRef(t *testing.T) {
	targetEnv(t)
	if got := refsIn(t, runCmd(t, "graph", "BETA-1", "--json"), "nodes"); len(got) == 0 || got[0] != "BETA-1" {
		t.Errorf("graph BETA-1 = %v", got)
	}
}

// review-cli #10: a relative document argument must mean the current
// project, not wherever the card happens to live, and must say so loudly
// rather than silently reading the wrong one.
func TestLinkRelativeDocMeansTheCurrentProject(t *testing.T) {
	targetEnv(t)
	refOf(t, "knowledge", "new", "--title", "Design", "--project", "ALPHA")
	refOf(t, "knowledge", "new", "--title", "Design", "--project", "BETA")

	_, err := execCmd("link", "BETA-1", "design")
	ce := coreErr(t, err)
	if ce.Code != "project_conflict" {
		t.Fatalf("link BETA-1 design: error = %+v, want project_conflict", ce)
	}

	// ALPHA's design must be untouched: no backlink was quietly created
	// against BETA's entry of the same name.
	nodes := refsIn(t, runCmd(t, "graph", "/ALPHA/vault/design", "--json"), "nodes")
	if len(nodes) != 1 {
		t.Errorf("ALPHA's design backlinks = %v, want none", nodes)
	}
}

// An address doc still crosses projects on purpose: link's documented
// exception (docs/superpowers/specs/2026-09-16-virtual-paths-design.md,
// "Cross-project operations").
func TestLinkStillCrossesProjectsWithAnAddressedDoc(t *testing.T) {
	targetEnv(t)
	refOf(t, "knowledge", "new", "--title", "Runbook", "--project", "BETA")
	runCmd(t, "link", "1", "/BETA/vault/runbook")
	nodes := refsIn(t, runCmd(t, "graph", "1", "--json"), "nodes")
	found := false
	for _, ref := range nodes {
		found = found || ref == "/BETA/vault/runbook"
	}
	if !found {
		t.Errorf("graph nodes = %v, want /BETA/vault/runbook", nodes)
	}
}
