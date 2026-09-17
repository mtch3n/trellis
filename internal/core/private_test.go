package core

import (
	"errors"
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
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Staging credentials", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if !entry.Private {
		t.Fatal("Private = false on the returned doc, want true")
	}

	raw, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "private: true") {
		t.Errorf("frontmatter missing the flag:\n%s", raw)
	}

	reread, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
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
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Deploy log"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if entry.Private {
		t.Fatal("Private = true by default, want false")
	}

	setPrivateInFile(t, entry.Path, true)
	on, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge after setting: %v", err)
	}
	if !on.Private {
		t.Fatal("Private = false after the file set it, want true")
	}

	setPrivateInFile(t, entry.Path, false)
	off, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
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
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Staging credentials", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.db.Exec(`UPDATE entry SET private = 0 WHERE id = ?`, entry.ID); err != nil {
		t.Fatalf("drift the mirror: %v", err)
	}

	reread, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if !reread.Private {
		t.Fatal("the stale mirror won over the file")
	}

	var stored int
	if err := c.db.Get(&stored, `SELECT private FROM entry WHERE id = ?`, entry.ID); err != nil {
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

// A non-boolean still fails the parse, and the error names the file. The
// failure surfaces from sweeps over the whole vault (the CLI runs one before
// every command), so without the path nobody can tell which file to fix.
// Msg is checked rather than Error() because Msg is what --json reports.
func TestABadPrivateValueNamesTheFile(t *testing.T) {
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Staging credentials"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	raw, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	bad := strings.Replace(string(raw), "title:", "private: maybe\ntitle:", 1)
	if err := os.WriteFile(entry.Path, []byte(bad), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	for name, call := range map[string]func() error{
		"search sync": func() error { return c.SyncEntrySearch(t.Context()) },
		"load": func() error {
			_, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
			return err
		},
	} {
		err := call()
		coreErr, ok := errors.AsType[*Error](err)
		if !ok || coreErr.Code != "bad_frontmatter" {
			t.Errorf("%s: err = %v, want bad_frontmatter", name, err)
			continue
		}
		if !strings.Contains(coreErr.Msg, entry.Path) {
			t.Errorf("%s: message %q does not name %s", name, coreErr.Msg, entry.Path)
		}
	}
}

// The corpus that feeds the vector index is the one place a body is shipped to
// something that may not be on this machine.
func TestVectorCorpusExcludesPrivate(t *testing.T) {
	c, p, _ := vaultCore(t)

	open, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Recall ranking"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	secret, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Staging credentials", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	entries, err := c.ListSearchEntries(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("ListSearchKnowledge: %v", err)
	}
	var sawOpen, sawSecret bool
	for _, e := range entries {
		switch e.Slug {
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
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Env staging"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	inCorpus := func() bool {
		t.Helper()
		entries, err := c.ListSearchEntries(t.Context(), p.ID)
		if err != nil {
			t.Fatalf("ListSearchKnowledge: %v", err)
		}
		for _, e := range entries {
			if e.Slug == entry.Slug {
				return true
			}
		}
		return false
	}

	if !inCorpus() {
		t.Fatal("setup is wrong: the entry is not in the corpus to begin with")
	}

	setPrivateInFile(t, entry.Path, true)
	if inCorpus() {
		t.Error("still in the corpus after the file was marked private; the filter read a stale mirror")
	}

	setPrivateInFile(t, entry.Path, false)
	if !inCorpus() {
		t.Error("never returned to the corpus after the file was un-marked; the filter read a stale mirror")
	}
}

// Everything local keeps listing private entries.
func TestListEntriesStillReturnsPrivate(t *testing.T) {
	c, p, _ := vaultCore(t)

	secret, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Staging credentials", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	entries, err := c.ListEntries(t.Context(), p.ID, EntryFilter{})
	if err != nil {
		t.Fatalf("ListKnowledge: %v", err)
	}
	for _, e := range entries {
		if e.Slug == secret.Slug {
			return
		}
	}
	t.Errorf("ListKnowledge dropped the private entry %q; it is local and must stay listed", secret.Slug)
}

// A private entry has no recap. Recall and the pin list blank one anyway, so a
// stored recap would never be shown; it would only sit in entry.recap and
// the event log, waiting to be injected the moment the entry is un-marked.
// Pinning succeeds with or without --recap and stores nothing either way.
func TestPinOnPrivateStoresNoRecap(t *testing.T) {
	c, p, _ := vaultCore(t)

	const title = "Staging credentials"
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: title, Private: true,
		Summary: "swordfish opens the staging database",
		Body:    "hunter2 is the staging database password.\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	storesNothing := func(step string) {
		t.Helper()
		var row struct {
			Recap *string `db:"recap"`
			Hash  *string `db:"recap_hash"`
		}
		if err := c.db.Get(&row, `SELECT recap, recap_hash FROM entry WHERE id = ?`, entry.ID); err != nil {
			t.Fatalf("%s: read recap: %v", step, err)
		}
		if row.Recap != nil || row.Hash != nil {
			t.Errorf("%s: recap = %v, recap_hash = %v, want both NULL", step, row.Recap, row.Hash)
		}

		var pinned struct {
			Total  int `db:"total"`
			Valued int `db:"valued"`
		}
		if err := c.db.Get(&pinned,
			`SELECT COUNT(*) AS total, COUNT(new_value) AS valued
			 FROM event WHERE entity_id = ? AND action = 'pinned'`, entry.ID); err != nil {
			t.Fatalf("%s: read pinned events: %v", step, err)
		}
		if pinned.Total == 0 {
			t.Errorf("%s: the pin was not recorded at all; the fact of it must survive", step)
		}
		if pinned.Valued != 0 {
			t.Errorf("%s: %d pinned events carry a value, want none", step, pinned.Valued)
		}

		var leaked int
		if err := c.db.Get(&leaked,
			`SELECT COUNT(*) FROM event WHERE entity_id = ?
			   AND (COALESCE(new_value, '') LIKE '%hunter2%'
			     OR COALESCE(new_value, '') LIKE '%swordfish%'
			     OR COALESCE(new_value, '') LIKE '%rotated quarterly%')`,
			entry.ID); err != nil {
			t.Fatalf("%s: count leaks: %v", step, err)
		}
		if leaked != 0 {
			t.Errorf("%s: %d event rows carry body, summary or recap text, want 0", step, leaked)
		}
	}

	pin, err := c.PinEntry(t.Context(), p.ID, entry.Slug, "", "")
	if err != nil {
		t.Fatalf("PinKnowledge with no recap: %v", err)
	}
	if pin.Recap != "" || pin.Title != title {
		t.Errorf("pin = %+v, want an empty recap and the title", pin)
	}
	storesNothing("no recap")

	pin, err = c.PinEntry(t.Context(), p.ID, entry.Slug, "staging DB access, rotated quarterly", "")
	if err != nil {
		t.Fatalf("PinKnowledge with an explicit recap: %v", err)
	}
	if pin.Recap != "" {
		t.Errorf("pin.Recap = %q, want the supplied recap discarded", pin.Recap)
	}
	storesNothing("explicit recap")

	pins, err := c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	var found bool
	for _, got := range pins {
		if got.Slug != entry.Slug {
			continue
		}
		found = true
		if got.Recap != "" || got.Title != title {
			t.Errorf("pin = %+v, want a pointer: the title and no recap", got)
		}
	}
	if !found {
		t.Fatal("the private entry is not in the pin list; it must stay pinned as a pointer")
	}
}

func TestPinOnNormalStillFallsBack(t *testing.T) {
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Recall ranking", Body: "Ranks are fused, not scored.\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	pin, err := c.PinEntry(t.Context(), p.ID, entry.Slug, "", "")
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
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title:   "Staging cluster access",
		Summary: "hunter2 opens the staging cluster",
		Body:    "The staging cluster password is hunter2.\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	setPrivateInFile(t, entry.Path, true)

	hits, err := c.Recall(t.Context(), p.ID, "staging cluster access", RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}

	var found bool
	for _, h := range hits {
		if !strings.HasSuffix(h.Ref, entry.Slug) {
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
		t.Fatalf("recall dropped the private entry %q; it must stay discoverable", entry.Slug)
	}
}

// An ordinary entry still gets its summary as a recap.
func TestRecallStillCarriesAnOrdinaryRecap(t *testing.T) {
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Recall ranking", Summary: "ranks are fused, not scored",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	hits, err := c.Recall(t.Context(), p.ID, "recall ranking", RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	var found bool
	for _, h := range hits {
		if !strings.HasSuffix(h.Ref, entry.Slug) {
			continue
		}
		found = true
		if h.Recap != "ranks are fused, not scored" {
			t.Errorf("Recap = %q, want the summary", h.Recap)
		}
	}
	if !found {
		t.Fatalf("recall did not return %q at all, so its recap was never checked", entry.Slug)
	}
}

// privateAfterRefresh cannot confirm the disclosure status of a file that is no
// longer there, so it must redact rather than disclose or fail. This is the
// exact function Recall calls to decide what to redact; Task 7 hands it the pin
// list, which — unlike recall's FTS-matched candidates — has no earlier sweep
// that would have already dropped a vanished file from consideration, so this
// path is the one that actually meets a deleted file in practice.
func TestPrivateAfterRefreshTreatsAMissingFileAsPrivate(t *testing.T) {
	c, p, _ := vaultCore(t)

	gone, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Deleted after the fact",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	present, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Still on disk",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	if err := os.Remove(gone.Path); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	var private, missing map[string]bool
	err = c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		var txErr error
		private, missing, txErr = c.privateAfterRefresh(tx, []string{gone.ID, present.ID})
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
	if !missing[gone.ID] || missing[present.ID] {
		t.Errorf("missing = %v, want only %s", missing, gone.ID)
	}

	// The cold listing withholds the gone entry's content like a private
	// one's, but says why.
	cold, err := c.ColdEntries(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("ColdKnowledge: %v", err)
	}
	for _, e := range cold {
		if e.Missing != (e.ID == gone.ID) {
			t.Errorf("cold %s: missing = %v", e.Slug, e.Missing)
		}
	}
}

// Every edit copies the whole body into event.new_value. For a private entry
// the audit trail keeps the fact of the edit and drops its content.
func TestEditOnPrivateRecordsNoContent(t *testing.T) {
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Staging credentials", Private: true, Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "hunter2 is the password\n", &entry.Version); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}

	var leaked int
	if err := c.db.Get(&leaked,
		`SELECT COUNT(*) FROM event WHERE entity_id = ? AND COALESCE(new_value, '') LIKE '%hunter2%'`,
		entry.ID); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if leaked != 0 {
		t.Errorf("%d event rows carry the private body, want 0", leaked)
	}

	var edits int
	if err := c.db.Get(&edits,
		`SELECT COUNT(*) FROM event WHERE entity_id = ? AND action = 'edited'`, entry.ID); err != nil {
		t.Fatalf("count edits: %v", err)
	}
	if edits == 0 {
		t.Error("the edit was not recorded at all; the fact of the edit must survive")
	}

	// Titles are disclosed on purpose, as the created and deleted events
	// show, so an edited event keeps the new one.
	if _, err := c.EditEntryFields(t.Context(), p.ID, entry.Slug, EntryEdit{Title: new("Staging access"), IfVersion: new(entry.Version + 1)}); err != nil {
		t.Fatalf("EditKnowledgeFields: %v", err)
	}
	var title string
	if err := c.db.Get(&title,
		`SELECT COALESCE(new_value, '') FROM event WHERE entity_id = ? AND action = 'edited' AND field = 'title'`,
		entry.ID); err != nil {
		t.Fatalf("read the title edit: %v", err)
	}
	if title != "Staging access" {
		t.Errorf("edited event title = %q, want the new title", title)
	}
}

// Marking an existing entry private has to clean up behind itself. The event
// log is the copy that gets forgotten: PinKnowledge writes the recap into
// new_value and EditKnowledgeFields writes the whole body there.
func TestMarkingPrivatePurgesEveryLocalCopy(t *testing.T) {
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Staging cluster access", Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "the password is hunter2\n", &entry.Version); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	if _, err := c.PinEntry(t.Context(), p.ID, entry.Slug, "hunter2 opens staging", ""); err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}

	countLeaks := func(action string) int {
		t.Helper()
		var n int
		if err := c.db.Get(&n,
			`SELECT COUNT(*) FROM event WHERE entity_id = ? AND action = ?
			   AND COALESCE(new_value, '') LIKE '%hunter2%'`,
			entry.ID, action); err != nil {
			t.Fatalf("count %s events: %v", action, err)
		}
		return n
	}
	// Each copy is confirmed on its own: an aggregate count would pass with
	// only one of them present, and then the purge of the other proves nothing.
	for _, action := range []string{"pinned", "edited"} {
		if countLeaks(action) == 0 {
			t.Fatalf("setup is wrong: no %s event carries the text", action)
		}
	}

	setPrivateInFile(t, entry.Path, true)
	reread, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if !reread.Private {
		t.Fatal("the external edit was not picked up")
	}

	var recap string
	if err := c.db.Get(&recap, `SELECT COALESCE(recap, '') FROM entry WHERE id = ?`, entry.ID); err != nil {
		t.Fatalf("read recap: %v", err)
	}
	if recap != "" {
		t.Errorf("entry.recap = %q, want it cleared", recap)
	}
	for _, action := range []string{"pinned", "edited"} {
		if n := countLeaks(action); n != 0 {
			t.Errorf("%d %s event rows still carry content, want 0", n, action)
		}
	}

	// The pin itself is not content — it is (id, entry_id, board_id,
	// created_at) — so the purge leaves it alone. Deleting it here would also
	// make UnpinKnowledge fail right after a privatise (see
	// TestUnpinningAJustPrivatisedEntrySucceeds). The pin must survive and
	// inject only a title/ref pointer, with an empty recap.
	pins, err := c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	var found bool
	for _, pin := range pins {
		if pin.Slug != entry.Slug {
			continue
		}
		found = true
		if pin.Recap != "" {
			t.Errorf("pin.Recap = %q, want empty: the recap column was purged", pin.Recap)
		}
	}
	if !found {
		t.Error("the pin was dropped; it should survive as a pointer with no recap")
	}
}

// The purge runs inside the caller's transaction. If it also deleted the pin,
// UnpinKnowledge's own DELETE (which runs right after loadDoc triggers the
// purge) would find zero rows, return not_pinned, and Core.Tx would roll back
// the whole transaction — including the purge. An author who marks a document
// private and immediately unpins it must not see that.
func TestUnpinningAJustPrivatisedEntrySucceeds(t *testing.T) {
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Staging cluster access", Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.PinEntry(t.Context(), p.ID, entry.Slug, "hunter2 opens staging", ""); err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}

	setPrivateInFile(t, entry.Path, true)

	if err := c.UnpinEntry(t.Context(), p.ID, entry.Slug, ""); err != nil {
		t.Fatalf("UnpinKnowledge right after privatising: %v", err)
	}

	pins, err := c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	for _, pin := range pins {
		if pin.Slug == entry.Slug {
			t.Error("still pinned after UnpinKnowledge succeeded")
		}
	}
}

// The session brief reads pins. Making Pins the very first call after the file
// changed is the whole point: any test that loads the document first refreshes
// the row as a side effect and proves nothing.
func TestPinsRedactAPrivateEntryWithNoPriorRead(t *testing.T) {
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Staging cluster access", Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.PinEntry(t.Context(), p.ID, entry.Slug, "hunter2 opens staging", ""); err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}

	setPrivateInFile(t, entry.Path, true)

	pins, err := c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	var found bool
	for _, pin := range pins {
		if pin.Slug != entry.Slug {
			continue
		}
		found = true
		if strings.Contains(pin.Recap, "hunter2") {
			t.Fatalf("recap = %q reached the brief after the file said private", pin.Recap)
		}
	}
	if !found {
		t.Fatalf("the pin list dropped %q, so its redaction was never checked", entry.Slug)
	}
}

// An ordinary pin still carries its recap into the brief.
func TestPinsStillCarryAnOrdinaryRecap(t *testing.T) {
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Recall ranking"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.PinEntry(t.Context(), p.ID, entry.Slug, "ranks are fused", ""); err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}

	pins, err := c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	var found bool
	for _, pin := range pins {
		if pin.Slug != entry.Slug {
			continue
		}
		found = true
		if pin.Recap != "ranks are fused" {
			t.Errorf("recap = %q, want the authored one", pin.Recap)
		}
	}
	if !found {
		t.Fatalf("the pin list dropped %q, so its recap was never checked", entry.Slug)
	}
}

// Reclassifying an entry as private must also purge the artifact names its
// link events recorded while the entry was still public: a name identifies a
// file, and once the entry is private that identification is exactly what
// must not linger in the log.
func TestMarkingPrivatePurgesArtifactLinkEvents(t *testing.T) {
	c, p, _ := vaultCore(t)

	a := addArtifact(t, c, p.ID, "evidence.png", "\x89PNG\r\n\x1a\nx")
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Staging cluster access"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.LinkArtifactToEntry(t.Context(), p.ID, entry.Slug, a.Name); err != nil {
		t.Fatalf("LinkArtifactToDoc: %v", err)
	}

	var before string
	if err := c.db.Get(&before,
		`SELECT COALESCE(new_value, '') FROM event WHERE entity_id = ? AND action = 'artifact_linked'`,
		entry.ID); err != nil {
		t.Fatalf("setup: read event: %v", err)
	}
	if before != a.Name {
		t.Fatalf("setup is wrong: artifact_linked new_value = %q, want %q", before, a.Name)
	}

	setPrivateInFile(t, entry.Path, true)
	reread, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if !reread.Private {
		t.Fatal("the external edit was not picked up")
	}

	var after string
	if err := c.db.Get(&after,
		`SELECT COALESCE(new_value, '') FROM event WHERE entity_id = ? AND action = 'artifact_linked'`,
		entry.ID); err != nil {
		t.Fatalf("read event after privatising: %v", err)
	}
	if after != "" {
		t.Errorf("artifact_linked new_value = %q after privatising, want empty", after)
	}
}

// The purge runs inside its caller's transaction, so any caller that fails
// after loadDoc rolls the purge back, and the private mirror with it.
// UnpinKnowledge naming a board that does not exist is one such caller: loadDoc
// purges, then boardByName fails. The old recap is back in the column and
// nothing has read the document successfully since. If Pins ever went back to
// reading entry.recap directly, this is the sequence that would leak it.
func TestPinsDoNotDiscloseAfterARolledBackPurge(t *testing.T) {
	c, p, _ := vaultCore(t)

	const recap = "hunter2 opens staging"
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Staging cluster access", Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.PinEntry(t.Context(), p.ID, entry.Slug, recap, ""); err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}

	setPrivateInFile(t, entry.Path, true)

	err = c.UnpinEntry(t.Context(), p.ID, entry.Slug, "no-such-board")
	if coreErr, ok := errors.AsType[*Error](err); !ok || coreErr.Code != "board_not_found" {
		t.Fatalf("UnpinKnowledge = %v, want board_not_found", err)
	}

	type stored struct {
		Private bool    `db:"private"`
		Recap   *string `db:"recap"`
	}
	read := func() stored {
		t.Helper()
		var row stored
		if err := c.db.Get(&row, `SELECT private, recap FROM entry WHERE id = ?`, entry.ID); err != nil {
			t.Fatalf("read row: %v", err)
		}
		return row
	}
	// Without a genuine rollback this test proves nothing, so confirm one: the
	// purge and the mirror update both have to be undone.
	if row := read(); row.Private || row.Recap == nil || *row.Recap != recap {
		t.Fatalf("setup is wrong: private = %v, recap = %v after the failed unpin, want false and %q",
			row.Private, row.Recap, recap)
	}

	pins, err := c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	var found bool
	for _, pin := range pins {
		if pin.Slug != entry.Slug {
			continue
		}
		found = true
		if strings.Contains(pin.Recap, "hunter2") {
			t.Fatalf("recap = %q reached the brief after a rolled-back purge restored the stale column", pin.Recap)
		}
	}
	if !found {
		t.Fatal("the entry is missing from the pin list; the redaction was never exercised")
	}

	// Pins' own refresh is the next successful read, so it redoes the purge.
	if row := read(); !row.Private || row.Recap != nil {
		t.Errorf("private = %v, recap = %v after Pins, want true and NULL: the purge did not heal",
			row.Private, row.Recap)
	}
}

// The cold listing is the other way `knowledge ls` reaches rows, and it must
// report the flag the file holds even when it is the first read after a hand
// edit. A file that is gone cannot be confirmed either way, so it reads as
// private rather than failing the listing.
func TestColdEntriesReadsTheFlagFromTheFile(t *testing.T) {
	c, p, _ := vaultCore(t)

	marked, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Staging cluster access"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	gone, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Deleted by hand"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	open, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Recall ranking"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setPrivateInFile(t, marked.Path, true)
	if err := os.Remove(gone.Path); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	cold, err := c.ColdEntries(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("ColdKnowledge: %v, want no error for a missing file", err)
	}
	want := map[string]bool{marked.Slug: true, gone.Slug: true, open.Slug: false}
	for _, e := range cold {
		private, ok := want[e.Slug]
		if !ok {
			continue
		}
		delete(want, e.Slug)
		if e.Private != private {
			t.Errorf("%s: Private = %v, want %v", e.Slug, e.Private, private)
		}
	}
	for slug := range want {
		t.Errorf("%s is missing from the cold listing", slug)
	}
}

// Staleness describes a recap, and a private pin has none. It must not read as
// stale on the first read after the flag is set by hand, when the row still
// carries the old recap_hash, nor on any later read, when the purge has cleared
// it. Health counts stale recaps and must agree with the pin list.
func TestAPrivatePinIsNeverStale(t *testing.T) {
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Staging cluster access", Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.PinEntry(t.Context(), p.ID, entry.Slug, "hunter2 opens staging", ""); err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}
	setPrivateInFile(t, entry.Path, true)

	for read := 1; read <= 2; read++ {
		pins, err := c.Pins(t.Context(), p.ID, "", 0)
		if err != nil {
			t.Fatalf("read %d: Pins: %v", read, err)
		}
		var found bool
		for _, pin := range pins {
			if pin.Slug != entry.Slug {
				continue
			}
			found = true
			if pin.Stale {
				t.Errorf("read %d: a private pin with recap %q is marked stale", read, pin.Recap)
			}
		}
		if !found {
			t.Fatalf("read %d: the pin list dropped %q", read, entry.Slug)
		}
	}

	lines, err := c.Health(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	for _, l := range lines {
		if l.What == "stale pinned recaps" && l.Count != 0 {
			t.Errorf("health counts %d stale recaps, want 0: a private pin has no recap", l.Count)
		}
	}
}

// Ordinary documents keep the audit fidelity they have today.
func TestEditOnNormalStillRecordsContent(t *testing.T) {
	c, p, _ := vaultCore(t)

	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Recall ranking", Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "ranks are fused\n", &entry.Version); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}

	var recorded int
	if err := c.db.Get(&recorded,
		`SELECT COUNT(*) FROM event WHERE entity_id = ? AND COALESCE(new_value, '') LIKE '%fused%'`,
		entry.ID); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if recorded == 0 {
		t.Error("an ordinary edit stopped recording its content")
	}
}
