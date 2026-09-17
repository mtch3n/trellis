package core

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestEntryMetadataEdit(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateLabel(t.Context(), p.ID, "reviewed", ""); err != nil {
		t.Fatal(err)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Metadata", Body: "original\n", Tags: []string{"old"}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err = c.EditEntry(t.Context(), p.ID, entry.Slug, "disclosed\n", &entry.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.PinEntry(t.Context(), p.ID, entry.Slug, "disclosed recap", ""); err != nil {
		t.Fatal(err)
	}
	entry, err = c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatal(err)
	}
	entry, err = c.EditEntryFields(t.Context(), p.ID, entry.Slug, EntryEdit{Template: new("research"), Private: new(true), Tags: new([]string{"new"}), Labels: new([]string{"reviewed"}), IfVersion: &entry.Version})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Template != "research" || !entry.Private || !slices.Equal(entry.Tags, []string{"new"}) || !slices.Equal(entry.Labels, []string{"reviewed"}) || entry.Recap != nil {
		t.Fatalf("edited: %+v", entry)
	}
	raw, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Template != "research" || !fm.Private || !slices.Equal(fm.Tags, entry.Tags) || !slices.Equal(fm.Labels, entry.Labels) {
		t.Fatalf("frontmatter: %+v", fm)
	}
	rev, err := os.ReadFile(revisionFilePath(entry.Path, entry.Version))
	if err != nil || !bytes.Equal(raw, rev) {
		t.Fatalf("revision: %v", err)
	}
	var leaked int
	if err = c.db.Get(&leaked, `SELECT count(*) FROM event WHERE entity_id = ? AND action IN ('edited','pinned') AND (COALESCE(new_value,'') != '' OR COALESCE(old_value,'') != '')`, entry.ID); err != nil || leaked != 0 {
		t.Fatalf("leaked=%d err=%v", leaked, err)
	}
	entry, err = c.EditEntryFields(t.Context(), p.ID, entry.Slug, EntryEdit{Body: new("still private\n"), IfVersion: &entry.Version})
	if err != nil || !entry.Private || entry.Template != "research" || len(entry.Tags) != 1 || len(entry.Labels) != 1 {
		t.Fatalf("omitted fields: %+v %v", entry, err)
	}
	if err = c.db.Get(&leaked, `SELECT count(*) FROM event WHERE entity_id = ? AND action = 'edited' AND COALESCE(new_value,'') != ''`, entry.ID); err != nil || leaked != 0 {
		t.Fatalf("private edit leaked=%d err=%v", leaked, err)
	}
	entry, err = c.EditEntryFields(t.Context(), p.ID, entry.Slug, EntryEdit{Private: new(false), Tags: new([]string{}), Labels: new([]string{}), IfVersion: &entry.Version})
	if err != nil || entry.Private || len(entry.Tags) != 0 || len(entry.Labels) != 0 || entry.Recap != nil {
		t.Fatalf("clear: %+v %v", entry, err)
	}
}

func TestEntryMetadataEditFailureIsAtomic(t *testing.T) {
	for _, kind := range []string{"type", "label", "version", "stale", "purge"} {
		t.Run(kind, func(t *testing.T) {
			c, p, _ := vaultCore(t)
			entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Atomic", Body: "body\n"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.PinEntry(t.Context(), p.ID, entry.Slug, "keep recap", ""); err != nil {
				t.Fatal(err)
			}
			entry, err = c.LoadEntry(t.Context(), p.ID, entry.Slug)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(entry.Path)
			if err != nil {
				t.Fatal(err)
			}
			edit := EntryEdit{Private: new(true), IfVersion: &entry.Version}
			code := ""
			switch kind {
			case "type":
				edit.Template = new("not-a-template")
				code = "unknown_template"
			case "label":
				edit.Labels = new([]string{"missing"})
				code = "label_not_found"
			case "version":
				edit.IfVersion = nil
				code = "version_required"
			case "stale":
				edit.IfVersion = new(int64(0))
				code = "conflict"
			case "purge":
				_, err = c.db.Exec(`CREATE TRIGGER fail_purge BEFORE UPDATE OF new_value ON event BEGIN SELECT RAISE(ABORT, 'purge failed'); END`)
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err = c.EditEntryFields(t.Context(), p.ID, entry.Slug, edit)
			if err == nil {
				t.Fatal("edit succeeded")
			}
			if code != "" {
				e, ok := errors.AsType[*Error](err)
				if !ok || e.Code != code {
					t.Fatalf("error=%v want %s", err, code)
				}
			}
			after, err := os.ReadFile(entry.Path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("file changed: %v", err)
			}
			got, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
			if err != nil || got.Private || got.Version != entry.Version || got.Recap == nil || *got.Recap != "keep recap" {
				t.Fatalf("rollback: %+v %v", got, err)
			}
		})
	}
}

// A file whose bytes are unchanged but whose mtime moved — a touch, or a
// write that was undone — is the same version.
func TestATouchedFileKeepsItsVersion(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Touched", Body: "body\n"})
	if err != nil {
		t.Fatal(err)
	}
	later := time.UnixMilli(entry.MTime).Add(5 * time.Second)
	if err := os.Chtimes(entry.Path, later, later); err != nil {
		t.Fatal(err)
	}
	got, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != entry.Version {
		t.Fatalf("version = %d after a touch, want %d", got.Version, entry.Version)
	}
	var mtime int64
	if err := c.db.Get(&mtime, `SELECT mtime FROM entry WHERE id = ?`, entry.ID); err != nil || mtime != later.UnixMilli() {
		t.Fatalf("stored mtime = %d (%v), want the new stat %d", mtime, err, later.UnixMilli())
	}
}

// An entry's board association can move after creation, and clearing it is
// the same edit with no name. The association lives in the frontmatter and in
// the row, so both have to follow.
func TestEditEntryBoardAssociation(t *testing.T) {
	c, p, b := vaultCore(t)
	ctx := t.Context()
	second, err := c.CreateBoard(ctx, p.ID, "second board", true)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Rollout notes", Board: b.Name})
	if err != nil {
		t.Fatal(err)
	}
	if entry.BoardName != b.Name {
		t.Fatalf("created with board %q, want %q", entry.BoardName, b.Name)
	}

	moved, err := c.EditEntryFields(ctx, p.ID, entry.Slug, EntryEdit{
		Board: &second.Name, IfVersion: &entry.Version,
	})
	if err != nil {
		t.Fatalf("move the association: %v", err)
	}
	reread, err := c.ReadEntry(ctx, p.ID, entry.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if reread.BoardName != second.Name {
		t.Errorf("board = %q after the move, want %q", reread.BoardName, second.Name)
	}
	raw, err := os.ReadFile(reread.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "board: second board") {
		t.Errorf("the file does not carry the new association:\n%s", raw)
	}

	none := ""
	if _, err := c.EditEntryFields(ctx, p.ID, entry.Slug, EntryEdit{
		Board: &none, IfVersion: &moved.Version,
	}); err != nil {
		t.Fatalf("clear the association: %v", err)
	}
	cleared, err := c.ReadEntry(ctx, p.ID, entry.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.BoardName != "" || cleared.BoardID != nil {
		t.Errorf("board = %q / %v after clearing", cleared.BoardName, cleared.BoardID)
	}

	// A board that does not exist is a refusal, not a silent association.
	absent := "no such board"
	if _, err := c.EditEntryFields(ctx, p.ID, entry.Slug, EntryEdit{
		Board: &absent, IfVersion: &cleared.Version,
	}); !isCode(err, "board_not_found") {
		t.Errorf("an unknown board = %v, want board_not_found", err)
	}
}
