package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/resolve"
)

func TestEnsureProjectIsIdempotent(t *testing.T) {
	c := testCore(t)
	id := resolve.Identity{Kind: "remote", Value: "github.com/mtch3n/xpsctl",
		RootPath: "/home/m/xpsctl", SuggestedKey: "XPSCTL"}

	a, err := c.EnsureProject(t.Context(), id)
	if err != nil {
		t.Fatalf("first EnsureProject: %v", err)
	}
	b, err := c.EnsureProject(t.Context(), id)
	if err != nil {
		t.Fatalf("second EnsureProject: %v", err)
	}
	if a.ID != b.ID {
		t.Errorf("EnsureProject created two projects: %q then %q", a.ID, b.ID)
	}
}

// The B1 case: a repo with no remote gets a path identity; adding a remote
// later must rebind that project rather than orphan it.
func TestEnsureProjectRebindsWhenRemoteAppears(t *testing.T) {
	c := testCore(t)
	root := "/home/m/trellis"

	before, err := c.EnsureProject(t.Context(),
		resolve.Identity{Kind: "path", Value: root, RootPath: root, SuggestedKey: "TRELLIS"})
	if err != nil {
		t.Fatalf("EnsureProject(path): %v", err)
	}

	after, err := c.EnsureProject(t.Context(),
		resolve.Identity{Kind: "remote", Value: "github.com/mtch3n/trellis",
			RootPath: root, SuggestedKey: "TRELLIS"})
	if err != nil {
		t.Fatalf("EnsureProject(remote): %v", err)
	}

	if after.ID != before.ID {
		t.Fatalf("rebinding created a new project: %q then %q", before.ID, after.ID)
	}
	if after.IdentityKind != "remote" || after.IdentityValue != "github.com/mtch3n/trellis" {
		t.Errorf("identity = %s/%s, want remote/github.com/mtch3n/trellis",
			after.IdentityKind, after.IdentityValue)
	}

	var n int
	if err := c.db.Get(&n, "SELECT count(*) FROM project"); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("project count = %d, want 1", n)
	}

	var action string
	if err := c.db.Get(&action,
		"SELECT action FROM event WHERE entity_type='project' ORDER BY seq DESC LIMIT 1"); err != nil {
		t.Fatal(err)
	}
	if action != "rebound" {
		t.Errorf("last project event = %q, want rebound", action)
	}
}

func TestEnsureProjectKeyCollision(t *testing.T) {
	c := testCore(t)
	_, err := c.EnsureProject(t.Context(), resolve.Identity{Kind: "remote",
		Value: "github.com/mtch3n/skills", RootPath: "/a", SuggestedKey: "SKILLS"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.EnsureProject(t.Context(), resolve.Identity{Kind: "remote",
		Value: "github.com/gojitech/skills", RootPath: "/b", SuggestedKey: "SKILLS"})
	if err == nil {
		t.Fatal("expected a collision error for a duplicate key")
	}
	te, ok := errors.AsType[*Error](err)
	if !ok || te.Code != "key_collision" {
		t.Errorf("error = %v, want code key_collision", err)
	}
	if !strings.Contains(te.Fix, "--key") {
		t.Errorf("fix = %q, should suggest --key", te.Fix)
	}
}
