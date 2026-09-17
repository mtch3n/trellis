package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func count(t *testing.T, c *Core, query string, args ...any) int {
	t.Helper()
	var n int
	if err := c.db.Get(&n, query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestDeleteProjectRemovesEverythingItOwns(t *testing.T) {
	c, p, b := kbCore(t)
	ctx := t.Context()
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "doomed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateComment(ctx, card.ID, "a note"); err != nil {
		t.Fatal(err)
	}
	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Design", Body: "See [[elsewhere]].\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LinkCardToDoc(ctx, p.ID, CardRef{UUID: card.ID}, doc.Slug); err != nil {
		t.Fatal(err)
	}

	other := seededProject2(t, c)
	otherBoard := seededBoard(t, c, other)
	kept, err := c.CreateCard(ctx, other.ID, otherBoard.ID, NewCard{Title: "survives"})
	if err != nil {
		t.Fatal(err)
	}

	if err := c.DeleteProject(ctx, "xpsctl"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	for _, check := range []struct {
		query string
		args  []any
	}{
		{`SELECT COUNT(*) FROM project WHERE id = ?`, []any{p.ID}},
		{`SELECT COUNT(*) FROM board WHERE project_id = ?`, []any{p.ID}},
		{`SELECT COUNT(*) FROM card WHERE project_id = ?`, []any{p.ID}},
		{`SELECT COUNT(*) FROM comment WHERE card_id = ?`, []any{card.ID}},
		{`SELECT COUNT(*) FROM knowledge WHERE project_id = ?`, []any{p.ID}},
		{`SELECT COUNT(*) FROM link WHERE from_id IN (?, ?) OR to_id IN (?, ?)`, []any{card.ID, doc.ID, card.ID, doc.ID}},
	} {
		if n := count(t, c, check.query, check.args...); n != 0 {
			t.Errorf("%s = %d, want 0", check.query, n)
		}
	}
	if _, err := os.Stat(doc.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the entry's file should be gone, stat = %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.kbRoot, "projects", p.Key)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the project's directory should be gone, stat = %v", err)
	}

	if _, err := c.GetCard(ctx, other.ID, CardRef{UUID: kept.ID}); err != nil {
		t.Errorf("another project's card must survive: %v", err)
	}
	if n := count(t, c,
		`SELECT COUNT(*) FROM event WHERE entity_type = 'project' AND entity_id = ? AND action = 'deleted'`, p.ID); n != 1 {
		t.Errorf("deleted events = %d, want 1: the event log is the change feed", n)
	}
}

func TestDeleteProjectWithNoDirectory(t *testing.T) {
	c := testCore(t)
	c.WithKBRoot(t.TempDir())
	p := seededProject(t, c)
	seededBoard(t, c, p)

	if err := c.DeleteProject(t.Context(), p.Key); err != nil {
		t.Fatalf("an initialised, never used project must delete cleanly: %v", err)
	}
	if n := count(t, c, `SELECT COUNT(*) FROM project`); n != 0 {
		t.Errorf("projects = %d, want 0", n)
	}
}

func TestDeleteProjectRefusesWhileAnAgentHoldsACard(t *testing.T) {
	c, p, b := kbCore(t)
	ctx := t.Context()
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "in flight"})
	if err != nil {
		t.Fatal(err)
	}
	holder := New(c.db, c.clock, "sess:holder")
	if _, err := holder.RegisterAgent(ctx, "worker-1", "agent", "/tmp", "host", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ClaimCard(ctx, card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}

	err = c.DeleteProject(ctx, p.Key)
	var te *Error
	if !errors.As(err, &te) || te.Code != "project_leased" {
		t.Fatalf("DeleteProject = %v, want project_leased", err)
	}
	if n := count(t, c, `SELECT COUNT(*) FROM card WHERE id = ?`, card.ID); n != 1 {
		t.Error("a refused delete must change nothing")
	}
}

func TestDeleteProjectRefusesWhileItOwnsVaultEntries(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Postgres conventions"})
	if err != nil {
		t.Fatal(err)
	}
	escalated, err := c.EscalateKnowledge(ctx, p.ID, doc.Slug, "every repo re-derives this")
	if err != nil {
		t.Fatal(err)
	}

	err = c.DeleteProject(ctx, p.Key)
	var te *Error
	if !errors.As(err, &te) || te.Code != "project_has_vault_entries" {
		t.Fatalf("DeleteProject = %v, want project_has_vault_entries", err)
	}
	if _, err := os.Stat(escalated.Path); err != nil {
		t.Errorf("the vault entry's file must be untouched: %v", err)
	}
	if n := count(t, c, `SELECT COUNT(*) FROM knowledge WHERE id = ?`, doc.ID); n != 1 {
		t.Error("the vault entry's row must be untouched")
	}
}

func TestDeleteProjectUnknownKey(t *testing.T) {
	c := testCore(t)
	err := c.DeleteProject(t.Context(), "NOPE")
	var te *Error
	if !errors.As(err, &te) || te.Code != "project_not_found" {
		t.Fatalf("DeleteProject = %v, want project_not_found", err)
	}
}
