package core

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"testing"
	"time"
)

func TestKnowledgeMetadataEdit(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateLabel(t.Context(), p.ID, "reviewed", ""); err != nil {
		t.Fatal(err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Metadata", Body: "original\n", Tags: []string{"old"}})
	if err != nil {
		t.Fatal(err)
	}
	doc, err = c.EditKnowledge(t.Context(), p.ID, doc.Slug, "disclosed\n", &doc.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.PinKnowledge(t.Context(), p.ID, doc.Slug, "disclosed recap", ""); err != nil {
		t.Fatal(err)
	}
	doc, err = c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{Template: new("research"), Private: new(true), Tags: new([]string{"new"}), Labels: new([]string{"reviewed"}), IfVersion: &doc.Version})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Template != "research" || !doc.Private || !slices.Equal(doc.Tags, []string{"new"}) || !slices.Equal(doc.Labels, []string{"reviewed"}) || doc.Recap != nil {
		t.Fatalf("edited: %+v", doc)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Template != "research" || !fm.Private || !slices.Equal(fm.Tags, doc.Tags) || !slices.Equal(fm.Labels, doc.Labels) {
		t.Fatalf("frontmatter: %+v", fm)
	}
	rev, err := os.ReadFile(revisionFilePath(doc.Path, doc.Version))
	if err != nil || !bytes.Equal(raw, rev) {
		t.Fatalf("revision: %v", err)
	}
	var leaked int
	if err = c.db.Get(&leaked, `SELECT count(*) FROM event WHERE entity_id = ? AND action IN ('edited','pinned') AND (COALESCE(new_value,'') != '' OR COALESCE(old_value,'') != '')`, doc.ID); err != nil || leaked != 0 {
		t.Fatalf("leaked=%d err=%v", leaked, err)
	}
	doc, err = c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{Body: new("still private\n"), IfVersion: &doc.Version})
	if err != nil || !doc.Private || doc.Template != "research" || len(doc.Tags) != 1 || len(doc.Labels) != 1 {
		t.Fatalf("omitted fields: %+v %v", doc, err)
	}
	if err = c.db.Get(&leaked, `SELECT count(*) FROM event WHERE entity_id = ? AND action = 'edited' AND COALESCE(new_value,'') != ''`, doc.ID); err != nil || leaked != 0 {
		t.Fatalf("private edit leaked=%d err=%v", leaked, err)
	}
	doc, err = c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{Private: new(false), Tags: new([]string{}), Labels: new([]string{}), IfVersion: &doc.Version})
	if err != nil || doc.Private || len(doc.Tags) != 0 || len(doc.Labels) != 0 || doc.Recap != nil {
		t.Fatalf("clear: %+v %v", doc, err)
	}
}

func TestKnowledgeMetadataEditFailureIsAtomic(t *testing.T) {
	for _, kind := range []string{"type", "label", "version", "stale", "purge"} {
		t.Run(kind, func(t *testing.T) {
			c, p, _ := kbCore(t)
			doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Atomic", Body: "body\n"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.PinKnowledge(t.Context(), p.ID, doc.Slug, "keep recap", ""); err != nil {
				t.Fatal(err)
			}
			doc, err = c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(doc.Path)
			if err != nil {
				t.Fatal(err)
			}
			edit := KnowledgeEdit{Private: new(true), IfVersion: &doc.Version}
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
			_, err = c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, edit)
			if err == nil {
				t.Fatal("edit succeeded")
			}
			if code != "" {
				e, ok := errors.AsType[*Error](err)
				if !ok || e.Code != code {
					t.Fatalf("error=%v want %s", err, code)
				}
			}
			after, err := os.ReadFile(doc.Path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("file changed: %v", err)
			}
			got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
			if err != nil || got.Private || got.Version != doc.Version || got.Recap == nil || *got.Recap != "keep recap" {
				t.Fatalf("rollback: %+v %v", got, err)
			}
		})
	}
}

// A file whose bytes are unchanged but whose mtime moved — a touch, or a
// write that was undone — is the same version.
func TestATouchedFileKeepsItsVersion(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Touched", Body: "body\n"})
	if err != nil {
		t.Fatal(err)
	}
	later := time.UnixMilli(doc.MTime).Add(5 * time.Second)
	if err := os.Chtimes(doc.Path, later, later); err != nil {
		t.Fatal(err)
	}
	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != doc.Version {
		t.Fatalf("version = %d after a touch, want %d", got.Version, doc.Version)
	}
	var mtime int64
	if err := c.db.Get(&mtime, `SELECT mtime FROM entry WHERE id = ?`, doc.ID); err != nil || mtime != later.UnixMilli() {
		t.Fatalf("stored mtime = %d (%v), want the new stat %d", mtime, err, later.UnixMilli())
	}
}
