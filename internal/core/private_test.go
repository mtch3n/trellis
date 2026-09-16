package core

import (
	"os"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
)

// setPrivateInFile edits the file the way a human with an editor would, which
// is the ordinary way this flag gets set.
func setPrivateInFile(t *testing.T, path string, on bool) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(raw)
	s = strings.ReplaceAll(s, "private: true\n", "")
	if on {
		s = strings.Replace(s, "title:", "private: true\ntitle:", 1)
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestPrivateRoundTripsThroughTheFile(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Staging credentials", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if !doc.Private {
		t.Fatal("Private = false on the returned doc, want true")
	}

	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "private: true") {
		t.Errorf("frontmatter missing the flag:\n%s", raw)
	}

	reread, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if !reread.Private {
		t.Error("Private = false after reload, want true")
	}
}

// Both directions. The un-setting direction is the one a SQL-side filter would
// break permanently, so it is asserted explicitly.
func TestPrivateFollowsTheFileInBothDirections(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy log"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Private {
		t.Fatal("Private = true by default, want false")
	}

	setPrivateInFile(t, doc.Path, true)
	on, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge after setting: %v", err)
	}
	if !on.Private {
		t.Fatal("Private = false after the file set it, want true")
	}

	setPrivateInFile(t, doc.Path, false)
	off, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge after clearing: %v", err)
	}
	if off.Private {
		t.Error("Private = true after the file cleared it, want false")
	}
}

// The mirror can disagree with the file — a database restored from an older
// backup, or a file that already carried the key when the column was added. The
// file wins, and the content hash alone does not notice, so the refresh must
// compare the flag too.
func TestMirrorDriftIsCorrectedFromTheFile(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Staging credentials", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.db.Exec(`UPDATE knowledge SET private = 0 WHERE id = ?`, doc.ID); err != nil {
		t.Fatalf("drift the mirror: %v", err)
	}

	reread, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if !reread.Private {
		t.Fatal("the stale mirror won over the file")
	}

	var stored int
	if err := c.db.Get(&stored, `SELECT private FROM knowledge WHERE id = ?`, doc.ID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored != 1 {
		t.Errorf("private column = %d after the read, want 1: the correction was not persisted", stored)
	}
}

func TestPrivateWithANonBooleanValueFailsTheParse(t *testing.T) {
	_, _, err := SplitFrontmatter("---\ntitle: X\nprivate: maybe\n---\n\nbody\n")
	if err == nil {
		t.Fatal("SplitFrontmatter accepted a non-boolean private value")
	}
	if !strings.Contains(err.Error(), "frontmatter") {
		t.Errorf("error = %v, want it to name the frontmatter", err)
	}
}

// The corpus that feeds the vector index is the one place a body is shipped to
// something that may not be on this machine.
func TestVectorCorpusExcludesPrivate(t *testing.T) {
	c, p, _ := kbCore(t)

	open, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Recall ranking"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	secret, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Staging credentials", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	docs, err := c.ListSearchKnowledge(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("ListSearchKnowledge: %v", err)
	}
	var sawOpen, sawSecret bool
	for _, d := range docs {
		switch d.Slug {
		case open.Slug:
			sawOpen = true
		case secret.Slug:
			sawSecret = true
		}
	}
	if !sawOpen {
		t.Errorf("the open entry %q is missing from the corpus", open.Slug)
	}
	if sawSecret {
		t.Errorf("the private entry %q reached the corpus", secret.Slug)
	}
}

// A hand-edit is the ordinary way the flag changes, and the corpus must follow
// it on the very next read — in both directions. Filtering in SQL passes the
// first half of this test and fails the second permanently.
func TestVectorCorpusFollowsTheFileOnTheNextRead(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Env staging"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	inCorpus := func() bool {
		t.Helper()
		docs, err := c.ListSearchKnowledge(t.Context(), p.ID)
		if err != nil {
			t.Fatalf("ListSearchKnowledge: %v", err)
		}
		for _, d := range docs {
			if d.Slug == doc.Slug {
				return true
			}
		}
		return false
	}

	if !inCorpus() {
		t.Fatal("setup is wrong: the entry is not in the corpus to begin with")
	}

	setPrivateInFile(t, doc.Path, true)
	if inCorpus() {
		t.Error("still in the corpus after the file was marked private; the filter read a stale mirror")
	}

	setPrivateInFile(t, doc.Path, false)
	if !inCorpus() {
		t.Error("never returned to the corpus after the file was un-marked; the filter read a stale mirror")
	}
}

// Everything local keeps listing private entries.
func TestListKnowledgeStillReturnsPrivate(t *testing.T) {
	c, p, _ := kbCore(t)

	secret, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Staging credentials", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	docs, err := c.ListKnowledge(t.Context(), p.ID, KnowledgeFilter{})
	if err != nil {
		t.Fatalf("ListKnowledge: %v", err)
	}
	for _, d := range docs {
		if d.Slug == secret.Slug {
			return
		}
	}
	t.Errorf("ListKnowledge dropped the private entry %q; it is local and must stay listed", secret.Slug)
}

func TestPinOnPrivateRefusesTheBodyFallback(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Staging credentials", Private: true,
		Body: "hunter2 is the staging database password.\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	_, err = c.PinKnowledge(t.Context(), p.ID, doc.Slug, "", "")
	if err == nil {
		t.Fatal("PinKnowledge fell back to the body for a private entry")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("the error itself leaked the body: %v", err)
	}

	pin, err := c.PinKnowledge(t.Context(), p.ID, doc.Slug, "staging DB access, rotated quarterly", "")
	if err != nil {
		t.Fatalf("PinKnowledge with an explicit recap: %v", err)
	}
	if strings.Contains(pin.Recap, "hunter2") {
		t.Errorf("recap = %q, want only what the author wrote", pin.Recap)
	}
}

func TestPinOnNormalStillFallsBack(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Recall ranking", Body: "Ranks are fused, not scored.\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	pin, err := c.PinKnowledge(t.Context(), p.ID, doc.Slug, "", "")
	if err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}
	if !strings.Contains(pin.Recap, "fused") {
		t.Errorf("recap = %q, want the first paragraph", pin.Recap)
	}
}

// Recall keeps returning private entries — an agent that cannot see that an env
// document exists cannot ask for it — but returns the identifier, not content.
//
// The flag is set by editing the file and recall is the VERY NEXT call. Setting
// it through the API, or loading the document first, refreshes the row as a side
// effect and hides exactly the bug this guards.
func TestRecallRedactsAPrivateEntryWithNoPriorRead(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title:   "Staging cluster access",
		Summary: "hunter2 opens the staging cluster",
		Body:    "The staging cluster password is hunter2.\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	setPrivateInFile(t, doc.Path, true)

	hits, err := c.Recall(t.Context(), p.ID, "staging cluster access", RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}

	var found bool
	for _, h := range hits {
		if !strings.HasSuffix(h.Ref, doc.Slug) {
			continue
		}
		found = true
		if h.Title != "Staging cluster access" {
			t.Errorf("Title = %q, want the title; presence is not what is protected", h.Title)
		}
		if h.Recap != "" {
			t.Errorf("Recap = %q, want empty: the summary reached a model after the file said private", h.Recap)
		}
	}
	if !found {
		t.Fatalf("recall dropped the private entry %q; it must stay discoverable", doc.Slug)
	}
}

// An ordinary entry still gets its summary as a recap.
func TestRecallStillCarriesAnOrdinaryRecap(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Recall ranking", Summary: "ranks are fused, not scored",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	hits, err := c.Recall(t.Context(), p.ID, "recall ranking", RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	for _, h := range hits {
		if strings.HasSuffix(h.Ref, doc.Slug) && h.Recap == "" {
			t.Error("an ordinary entry lost its recap")
		}
	}
}

// privateAfterRefresh cannot confirm the disclosure status of a file that is no
// longer there, so it must redact rather than disclose or fail. This is the
// exact function Recall calls to decide what to redact; Task 7 hands it the pin
// list, which — unlike recall's FTS-matched candidates — has no earlier sweep
// that would have already dropped a vanished file from consideration, so this
// path is the one that actually meets a deleted file in practice.
func TestPrivateAfterRefreshTreatsAMissingFileAsPrivate(t *testing.T) {
	c, p, _ := kbCore(t)

	gone, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Deleted after the fact",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	present, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Still on disk",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	if err := os.Remove(gone.Path); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	var private map[string]bool
	err = c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		var txErr error
		private, txErr = c.privateAfterRefresh(tx, []string{gone.ID, present.ID})
		return txErr
	})
	if err != nil {
		t.Fatalf("privateAfterRefresh: %v, want no error for a missing file", err)
	}
	if !private[gone.ID] {
		t.Errorf("private[%q] = false, want true: a file that cannot be read must be treated as private", gone.ID)
	}
	if private[present.ID] {
		t.Errorf("private[%q] = true, want false: this file is still on disk and was never marked private", present.ID)
	}
}
