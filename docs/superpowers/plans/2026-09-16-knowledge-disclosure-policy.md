# Knowledge Disclosure Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give a knowledge document's author control over whether its body is transmitted automatically, without Trellis inspecting or judging what the body contains.

**Architecture:** One author-declared `private` boolean in YAML frontmatter, mirrored to a SQLite column the way `provenance` already is. The file always wins over the mirror, so every disclosure-bearing selection refreshes from disk before filtering. Private documents stay in local FTS5 and stay returned by search and recall; they are excluded from the vector corpus, never take a body or summary fallback for their recap, never record body text into the event log, and reach session injection as a pointer. The two surfaces that reach a model unasked — recall and the pin list — read the files before they disclose, rather than a column that is one read stale. Flipping the flag purges the four local copies that already exist.

**Tech Stack:** Go 1.x, SQLite via `sqlx`, goose migrations, `gopkg.in/yaml.v3`, cobra CLI.

**Spec:** `docs/superpowers/specs/2026-09-16-knowledge-disclosure-policy-design.md`

**Revision note:** this plan was rewritten twice after review, and the corrections are kept where they apply rather than tidied away, because each marks a place the obvious implementation is wrong. Round one: filtering the vector corpus in SQL reads a stale mirror, and the purge missed the largest copy (`event.new_value` for `edited`). Round two: a sixth exposure route was asserted from a grep hit without reading the query's `WHERE` clause and does not exist; recall and the pin list still read the mirror on the two paths where a lag matters most; and the reindex mechanism added in round one was unnecessary, because `LoadKnowledge` already notifies unconditionally and exclusion from the corpus is eviction.

## Global Constraints

- Markdown files are the source of truth. `private` lives in frontmatter; the column is a mirror, derived on read.
- **Never filter on the mirror.** Any selection made for a disclosure-bearing purpose refreshes from the file first and filters on the refreshed value. A `WHERE private = 0` clause is wrong in both directions.
- No backward-compatibility shims. Pre-1.0; remove obsolete paths outright.
- No content inspection of any kind. Trellis does not judge what a body contains.
- No restriction on what may be stored. Any markdown content is valid.
- Writes go through `writeAtomic` / `stageRemoval` in `internal/core/file_store.go`.
- **Never disclose from the mirror either.** Recall and the pin list refresh the documents they are about to return and decide afterwards. Both are bounded, and both reach a model without being asked.
- CI runs Linux, macOS and Windows; all three must pass.
- Gates before any task is done: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` clean.

## File Structure

| File | Responsibility |
|---|---|
| `internal/store/migrations/0012_private.sql` | Create: the mirrored column |
| `internal/core/markdown.go` | Modify: `Frontmatter.Private` |
| `internal/core/knowledge.go` | Modify: `Knowledge.Private`, create path, `refreshFromFile` sync and transition, edit-event gating |
| `internal/core/search.go` | Modify: `ListSearchKnowledge` refreshes then filters |
| `internal/cli/vector.go`, `internal/retrieval/service.go` | Modify: every vector corpus read moves to one list |
| `internal/core/pin.go` | Modify: no recap fallback for private; `Pins` refreshes before disclosing |
| `internal/core/recall.go` | Modify: redact after refreshing, not in SQL |
| `internal/core/disclose.go` | Create: the shared refresh-before-disclose helper |
| `internal/core/purge.go` | Create: the four-copy purge |
| `internal/cli/knowledge.go` | Modify: `--private`, extracted list renderer |
| `internal/cli/knowledge_cmd_test.go` | Create: the first command-level test harness in this package |
| `internal/core/private_test.go` | Create: every exposure route, both directions |

---

### Task 1: The flag exists, round-trips, and the file beats the mirror

**Files:**
- Create: `internal/store/migrations/0012_private.sql`
- Modify: `internal/core/markdown.go` (`Frontmatter`), `internal/core/knowledge.go` (`Knowledge`, `NewKnowledge`, `CreateKnowledge`, `insertKnowledge`, `refreshFromFile`)
- Test: `internal/core/private_test.go`

**Interfaces:**
- Consumes: nothing; first task.
- Produces: `Frontmatter.Private bool`, `Knowledge.Private bool`, `NewKnowledge.Private bool`.

The migration and the struct field must land in the same commit: `sqlx` uses `SELECT *`, so a column without a matching field fails every read with `missing destination name private`.

- [ ] **Step 1: Write the failing tests**

Create `internal/core/private_test.go`:

```go
package core

import (
	"os"
	"strings"
	"testing"
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run TestPrivate -v`
Expected: FAIL — the package does not compile, `unknown field Private in struct literal`.

- [ ] **Step 3: Add the migration**

Create `internal/store/migrations/0012_private.sql`:

```sql
-- +goose Up
-- Whether the author has declared this body not-for-automatic-distribution.
-- Derived from the file's frontmatter like every other knowledge column. It is
-- a mirror and never the authority: code that selects documents for a
-- disclosure-bearing purpose refreshes from the file first, because this value
-- is stale for exactly one read after the file changes.
--
-- Deliberately no index. Nothing may filter on this column in SQL — see the
-- Global Constraints — so an index on it could never be used.
ALTER TABLE knowledge ADD COLUMN private INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE knowledge DROP COLUMN private;
```

- [ ] **Step 4: Add the frontmatter field**

In `internal/core/markdown.go`, inside `Frontmatter`, after `Provenance`:

```go
	// Private is the author's declaration that this body must not be
	// transmitted automatically. Egress, not access: see
	// docs/superpowers/specs/2026-09-16-knowledge-disclosure-policy-design.md.
	// Typed bool on purpose — a non-boolean value is a parse failure rather
	// than a silent false, because failing open here cannot be undone.
	Private bool `yaml:"private,omitempty"`
```

- [ ] **Step 5: Add the domain field and the write paths**

In `internal/core/knowledge.go`, in `Knowledge`, beside `Global`:

```go
	Private bool `db:"private" json:"private,omitempty"`
```

In `NewKnowledge`, add `Private bool`.

In `CreateKnowledge`, add `Private: in.Private,` to both the `Frontmatter` literal and the `Knowledge` literal.

Replace `insertKnowledge` entirely. The column list, the placeholder list and the argument list must all grow together — the existing statement has 16 of each, and adding a column and an argument without a seventeenth `?` produces a silent arity mismatch at runtime:

```go
func insertKnowledge(tx *sqlx.Tx, d Knowledge) error {
	_, err := tx.Exec(
		`INSERT INTO knowledge (id, project_id, board_id, slug, title, path, doc_type, summary,
		                        provenance, private, content_hash, mtime, size, global, version,
		                        created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.ProjectID, d.BoardID, d.Slug, d.Title, d.Path, d.DocType, d.Summary,
		d.Provenance, d.Private, d.ContentHash, d.MTime, d.Size, d.Global, d.Version,
		d.CreatedAt, d.UpdatedAt)
	return err
}
```

- [ ] **Step 6: Make the refresh notice a flag change**

In `refreshFromFile`, replace `doc.Provenance = fm.Provenance` and the `changed` computation:

```go
	doc.Provenance = fm.Provenance
	// The flag is compared separately because the content hash cannot see it:
	// a database restored from an older backup, or a file that already carried
	// the key when the column was added, has an unchanged file and a wrong row.
	privateDrifted := doc.Private != fm.Private
	doc.Private = fm.Private
	doc.BodyMD = body
	oldHash := doc.ContentHash
	doc.ContentHash = ContentHash(string(raw))
	changed := st.ModTime().UnixMilli() != doc.MTime || st.Size() != doc.Size ||
		oldHash != doc.ContentHash || privateDrifted
```

Add `private = ?` to the `UPDATE knowledge SET ...` statement with `doc.Private` in the matching argument position.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/core -run TestPrivate -v && go test ./internal/core -run TestMirrorDrift -v`
Expected: PASS.

- [ ] **Step 8: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass, `gofmt -l .` prints nothing.

- [ ] **Step 9: Commit**

```bash
git add internal/store/migrations/0012_private.sql internal/core/markdown.go \
        internal/core/knowledge.go internal/core/private_test.go
git commit -m "feat(core): let an author declare a body not-for-automatic-distribution

The column is a mirror. The file decides, including when the two disagree,
which the content hash alone cannot detect.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: One vector corpus, filtered after the refresh

**Files:**
- Modify: `internal/core/search.go` (`ListSearchKnowledge`)
- Modify: `internal/cli/vector.go:96` (status), `:131` (rebuild), `:160` (prune)
- Modify: `internal/retrieval/service.go:247` (`VectorRebuild`), `:267` (`VectorPrune`)
- Test: `internal/core/private_test.go`

**Interfaces:**
- Consumes: `Knowledge.Private` from Task 1.
- Produces: `ListSearchKnowledge` is the only corpus any vector code reads. Nothing else may query the knowledge table to build, count or prune vectors.

Two defects from the first draft are fixed here.

**The filter cannot go in the SQL.** `ListSearchKnowledge` selects rows and *then* calls `refreshFromFile` per row (`search.go:236-243`), so a `WHERE private = 0` clause reads the mirror before the file has been consulted: a document just marked private is still selected and still embedded, and a document just un-marked can never be selected again.

**Five other sites read a different corpus.** `cli/vector.go:96,131,160` and `retrieval/service.go:247,267` all call `ListKnowledge(ctx, projectID, core.KnowledgeFilter{})`, whose `where()` is `project_id = ?` while `ListSearchKnowledge` is `project_id = ? OR global = 1`. They already disagree today: `vector prune` builds its keep-list without global entries and therefore deletes vectors `Reconcile` just wrote. Repointing all five fixes that and makes the single-corpus claim true.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/private_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestVectorCorpus|TestListKnowledgeStillReturnsPrivate' -v`
Expected: both `TestVectorCorpus*` FAIL; `TestListKnowledgeStillReturnsPrivate` PASSES already.

- [ ] **Step 3: Refresh, then filter**

In `internal/core/search.go`, replace `ListSearchKnowledge` entirely:

```go
// ListSearchKnowledge is the corpus every vector index build, count and prune
// reads. Private entries are dropped here rather than at each call site, so a
// second builder cannot reintroduce them by querying the table directly.
//
// The filter is applied after refreshFromFile and never in the SELECT. The
// private column is a mirror of the file and is stale for exactly one read
// after the file changes, which is the read that matters: filtering in SQL
// would still select a document just marked private, and would never again
// select one just un-marked.
func (c *Core) ListSearchKnowledge(ctx context.Context, projectID string) ([]Knowledge, error) {
	docs := []Knowledge{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var all []Knowledge
		if err := tx.Select(&all,
			`SELECT * FROM knowledge WHERE project_id = ? OR global = 1 ORDER BY updated_at DESC`,
			projectID); err != nil {
			return err
		}
		for i := range all {
			if err := c.refreshFromFile(tx, &all[i]); err != nil {
				return err
			}
			if err := c.docView(tx, &all[i]); err != nil {
				return err
			}
			if !all[i].Private {
				docs = append(docs, all[i])
			}
		}
		return nil
	})
	return docs, err
}
```

- [ ] **Step 4: Repoint the five remaining vector call sites**

In `internal/cli/vector.go`, at lines 96, 131 and 160, and in `internal/retrieval/service.go`, at lines 247 and 267, replace each

```go
docs, err := <receiver>.ListKnowledge(<ctx>, <projectID>, core.KnowledgeFilter{})
```

with

```go
docs, err := <receiver>.ListSearchKnowledge(<ctx>, <projectID>)
```

Keep each surrounding variable name as it is. Remove the `core` import from `internal/cli/vector.go` only if nothing else in that file uses it, then run `gofmt`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core ./internal/cli ./internal/retrieval -v -run 'Private|Vector|Corpus'`
Expected: PASS.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/core/search.go internal/cli/vector.go internal/retrieval/service.go \
        internal/core/private_test.go
git commit -m "feat(core): keep private entries out of the vector corpus

Six call sites read the corpus and two of them disagreed on scope, so prune
was already deleting vectors reconcile had just written. They now read one
list, and that list filters after refreshing from disk rather than in SQL,
because the column is a mirror and is stale on the read that matters.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: A private entry's recap is never lifted from its body

**Files:**
- Modify: `internal/core/pin.go` (`PinKnowledge`)
- Test: `internal/core/private_test.go`

**Interfaces:**
- Consumes: `Knowledge.Private` from Task 1.
- Produces: `PinKnowledge` returns `ErrUsage("recap_required", ...)` for a private entry with no explicit recap.

`PinKnowledge` falls back `explicit recap -> doc.Summary -> FirstParagraph(doc.BodyMD)` (`pin.go:41`). The recap is injected at session start, so the last hop would put the first paragraph of a private body into model context.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/private_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestPinOnPrivate|TestPinOnNormal' -v`
Expected: `TestPinOnPrivateRefusesTheBodyFallback` FAILS; `TestPinOnNormalStillFallsBack` PASSES.

- [ ] **Step 3: Gate the fallback**

In `internal/core/pin.go`, inside `PinKnowledge`, replace the fallback block:

```go
		text := strings.TrimSpace(recap)
		if text == "" && doc.Private {
			return ErrUsage("recap_required",
				"a private entry needs a recap written on purpose; trellis will not lift one from the body",
				`trellis knowledge pin `+doc.Slug+` --recap "one line an agent can act on"`)
		}
		if text == "" {
			text = cmpOr(doc.Summary, FirstParagraph(doc.BodyMD))
		}
```

The existing `no_recap` error below stays for the ordinary case where the fallback finds nothing.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestPinOnPrivate|TestPinOnNormal' -v`
Expected: PASS.

- [ ] **Step 5: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/core/pin.go internal/core/private_test.go
git commit -m "feat(core): refuse to lift a private entry's recap from its body

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Recall refreshes before it discloses

**Files:**
- Create: `internal/core/disclose.go`
- Modify: `internal/core/recall.go` (the knowledge branch of `Recall`)
- Test: `internal/core/private_test.go`

**Interfaces:**
- Consumes: `Knowledge.Private` from Task 1.
- Produces: `func (c *Core) privateAfterRefresh(tx *sqlx.Tx, ids []string) (map[string]bool, error)`. Task 7 uses it for pins.

Recall has its own recap fallback, `COALESCE(NULLIF(k.recap, ''), k.summary)` at `recall.go:169`, independent of `PinKnowledge`. Task 3 does not close it.

Gating it in SQL with a `CASE WHEN k.private` would read the mirror, which the Global Constraints forbid — and here the forbidden thing is worst. `Recall` reaches FTS through `SyncKnowledgeSearch`, which refreshes the search index without updating knowledge rows, so after a hand-edit sets the flag the mirror is still `0` and recall would hand a model the old summary. Recall and the session brief are the two paths that put text in front of a model unasked; a one-read lag is not acceptable on either.

The refresh is cheap because the set is bounded: `RecallOpts.Limit` defaults to 5.

- [ ] **Step 1: Write the failing test**

Append to `internal/core/private_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestRecallRedacts|TestRecallStillCarries' -v`
Expected: `TestRecallRedactsAPrivateEntryWithNoPriorRead` FAILS with the summary present; `TestRecallStillCarries...` PASSES.

- [ ] **Step 3: Write the shared refresh helper**

Create `internal/core/disclose.go`:

```go
package core

import "github.com/jmoiron/sqlx"

// privateAfterRefresh re-reads the named entries from disk and reports which of
// them the file says are private.
//
// It exists because the private column is a mirror and is one read stale after
// a file changes, and because the two callers — recall and the pin list — are
// the paths that put text in front of a model without being asked. Deciding
// from the mirror there means a document can be disclosed after its author
// marked it private and before anything happened to refresh the row.
//
// Reading the files is affordable because both callers are bounded: a recall
// returns RecallOpts.Limit hits, five by default, and pins are curated by hand.
func (c *Core) privateAfterRefresh(tx *sqlx.Tx, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args, err := sqlx.In(`SELECT * FROM knowledge WHERE id IN (?)`, ids)
	if err != nil {
		return nil, err
	}
	var docs []Knowledge
	if err := tx.Select(&docs, tx.Rebind(q), args...); err != nil {
		return nil, err
	}
	for i := range docs {
		if err := c.refreshFromFile(tx, &docs[i]); err != nil {
			return nil, err
		}
		if docs[i].Private {
			out[docs[i].ID] = true
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Redact in the recall transaction**

In `internal/core/recall.go`, leave the `SELECT` at line 169 exactly as it is — the fallback stays for ordinary entries and the gate is applied afterwards, on refreshed state.

Immediately after the knowledge `tx.Select(&docs, ...)` call succeeds and before `recallLinkBoosts`, insert:

```go
		ids := make([]string, 0, len(docs))
		for _, d := range docs {
			ids = append(ids, d.ID)
		}
		private, err := c.privateAfterRefresh(tx, ids)
		if err != nil {
			return err
		}
		for i := range docs {
			if private[docs[i].ID] {
				// The identifier and the title still travel. Only an
				// explicitly written recap would have survived, and the
				// purge has already cleared any that existed.
				docs[i].Recap = ""
			}
		}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestRecall' -v`
Expected: PASS.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/core/disclose.go internal/core/recall.go internal/core/private_test.go
git commit -m "feat(core): recall refreshes an entry before disclosing its recap

Recall reaches FTS through SyncKnowledgeSearch, which never updates the
knowledge row, so gating on the private column would have handed a model
the summary of a document its author had just marked private.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: A private entry records no content in the event log

**Files:**
- Modify: `internal/core/knowledge.go:547-553` (`EditKnowledgeFields`)
- Test: `internal/core/private_test.go`

**Interfaces:**
- Consumes: `Knowledge.Private` from Task 1.
- Produces: for a private document, `recordEvent` is called with an empty `newV` for the `edited` action. The event still exists; only its content is omitted.

This is the largest route and the first draft of the spec missed it. `EditKnowledgeFields` writes the **entire edited body** into `event.new_value` on every edit, so a document that was never pinned and never embedded still has its full body in the event log.

The second draft justified this by claiming `internal/ui/server.go:535` serves `new_value` over HTTP. **That is false** — that query is the card-detail handler and is scoped `WHERE entity_type = 'card'`, and the global activity feed (`server.go:249-259`) selects `field` but neither value column. No HTTP surface serves a knowledge event's value. The task stands on its own: the event log is a real local copy that reclassification must be able to clear, and a copy nothing publishes today is still a copy.

- [ ] **Step 1: Write the failing test**

Append to `internal/core/private_test.go`:

```go
// Every edit copies the whole body into event.new_value. For a private entry
// the audit trail keeps the fact of the edit and drops its content.
func TestEditOnPrivateRecordsNoContent(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Staging credentials", Private: true, Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "hunter2 is the password\n", nil); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}

	var leaked int
	if err := c.db.Get(&leaked,
		`SELECT COUNT(*) FROM event WHERE entity_id = ? AND COALESCE(new_value, '') LIKE '%hunter2%'`,
		doc.ID); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if leaked != 0 {
		t.Errorf("%d event rows carry the private body, want 0", leaked)
	}

	var edits int
	if err := c.db.Get(&edits,
		`SELECT COUNT(*) FROM event WHERE entity_id = ? AND action = 'edited'`, doc.ID); err != nil {
		t.Fatalf("count edits: %v", err)
	}
	if edits == 0 {
		t.Error("the edit was not recorded at all; the fact of the edit must survive")
	}
}

// Ordinary documents keep the audit fidelity they have today.
func TestEditOnNormalStillRecordsContent(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Recall ranking", Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "ranks are fused\n", nil); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}

	var recorded int
	if err := c.db.Get(&recorded,
		`SELECT COUNT(*) FROM event WHERE entity_id = ? AND COALESCE(new_value, '') LIKE '%fused%'`,
		doc.ID); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if recorded == 0 {
		t.Error("an ordinary edit stopped recording its content")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestEditOnPrivate|TestEditOnNormal' -v`
Expected: `TestEditOnPrivateRecordsNoContent` FAILS; `TestEditOnNormalStillRecordsContent` PASSES.

- [ ] **Step 3: Omit the value for a private entry**

In `internal/core/knowledge.go`, replace the event loop in `EditKnowledgeFields`:

```go
		for _, field := range []string{"body", "title", "summary"} {
			if _, ok := fields[field]; !ok {
				continue
			}
			// The value is the content. A private entry records that it was
			// edited and nothing more, because an audit log holding whole
			// bodies is a copy of them.
			value := fields[field]
			if doc.Private {
				value = ""
			}
			if err := c.recordEvent(tx, "knowledge", doc.ID, "edited", field, "", value); err != nil {
				return err
			}
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestEditOnPrivate|TestEditOnNormal' -v`
Expected: PASS.

- [ ] **Step 5: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/core/knowledge.go internal/core/private_test.go
git commit -m "feat(core): keep a private entry's content out of the event log

Every edit copied the whole body into event.new_value, so a document that
was never pinned and never embedded still had its body in the audit log.
A private entry now records the fact of the edit without its content.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Flipping the flag purges what already escaped

**Files:**
- Create: `internal/core/purge.go`
- Modify: `internal/core/knowledge.go` (`refreshFromFile`)
- Test: `internal/core/private_test.go`

**Interfaces:**
- Consumes: `Knowledge.Private` from Task 1, the private-aware event recording from Task 5.
- Produces: `func (c *Core) purgeDisclosedCopies(tx *sqlx.Tx, doc *Knowledge) error`, called from `refreshFromFile` on a `false -> true` transition.

Four local copies exist: `knowledge.recap` with `recap_hash`, the pins that would inject it, `event.new_value` for `pinned`/`unpinned`, and `event.new_value` for `edited` — the largest, and the one missed in the first draft.

**The vector index needs no step here.** A second draft added a mechanism to push a reindex after the transition, on the belief that `refreshFromFile` never triggers one. That belief was wrong twice over: `knowledge.go:280` is `LoadKnowledge`, which already calls `notifyKnowledgeChanged` unconditionally after every successful load, and `Reconcile` builds both its upsert set and its prune keep-list from `ListSearchKnowledge` — so a document Task 2 dropped from the corpus is pruned by the same pass that would have re-embedded it. Exclusion is eviction. The mechanism is dropped; the remaining lag is recorded in the spec's accepted limitations.

- [ ] **Step 1: Write the failing test**

Append to `internal/core/private_test.go`:

```go
// Marking an existing entry private has to clean up behind itself. The event
// log is the copy that gets forgotten: PinKnowledge writes the recap into
// new_value and EditKnowledgeFields writes the whole body there.
func TestMarkingPrivatePurgesEveryLocalCopy(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Staging cluster access", Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "the password is hunter2\n", nil); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	if _, err := c.PinKnowledge(t.Context(), p.ID, doc.Slug, "hunter2 opens staging", ""); err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}

	countLeaks := func() int {
		t.Helper()
		var n int
		if err := c.db.Get(&n,
			`SELECT COUNT(*) FROM event WHERE entity_id = ? AND COALESCE(new_value, '') LIKE '%hunter2%'`,
			doc.ID); err != nil {
			t.Fatalf("count events: %v", err)
		}
		return n
	}
	if countLeaks() == 0 {
		t.Fatal("setup is wrong: nothing reached the event log")
	}

	setPrivateInFile(t, doc.Path, true)
	reread, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if !reread.Private {
		t.Fatal("the external edit was not picked up")
	}

	var recap string
	if err := c.db.Get(&recap, `SELECT COALESCE(recap, '') FROM knowledge WHERE id = ?`, doc.ID); err != nil {
		t.Fatalf("read recap: %v", err)
	}
	if recap != "" {
		t.Errorf("knowledge.recap = %q, want it cleared", recap)
	}
	if n := countLeaks(); n != 0 {
		t.Errorf("%d event rows still carry content, want 0", n)
	}

	pins, err := c.Pins(t.Context(), p.ID, "")
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	for _, pin := range pins {
		if pin.Slug == doc.Slug {
			t.Error("still pinned; a pin injects a recap that no longer exists")
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/core -run TestMarkingPrivatePurgesEveryLocalCopy -v`
Expected: FAIL — `knowledge.recap` still holds the recap.

- [ ] **Step 3: Write the purge**

Create `internal/core/purge.go`:

```go
package core

import "github.com/jmoiron/sqlx"

// purgeDisclosedCopies removes the copies of a body that trellis made before
// its author declared it private. Four exist locally:
//
//	knowledge.recap / recap_hash   the stored recap
//	pin                            the rows that would inject it
//	event.new_value  pinned        PinKnowledge writes the recap text there
//	event.new_value  edited        EditKnowledgeFields writes the whole body
//	                               there, which is the largest copy and the one
//	                               nobody thinks to look for
//
// The vector index needs nothing here. Private entries are absent from the
// corpus ListSearchKnowledge returns, and Reconcile builds both its upsert set
// and its prune keep-list from that one list, so the next reconcile evicts the
// chunks as a side effect of not re-embedding them.
//
// Anything already sent to a remote embedder is gone. This cleans up locally
// and makes no wider claim.
func (c *Core) purgeDisclosedCopies(tx *sqlx.Tx, doc *Knowledge) error {
	if _, err := tx.Exec(
		`UPDATE knowledge SET recap = NULL, recap_hash = NULL WHERE id = ?`, doc.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM pin WHERE knowledge_id = ?`, doc.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE event SET new_value = NULL, old_value = NULL
		 WHERE entity_type = 'knowledge' AND entity_id = ?
		   AND action IN ('pinned', 'unpinned', 'edited')`,
		doc.ID); err != nil {
		return err
	}
	doc.Recap, doc.RecapHash = nil, nil
	return c.recordEvent(tx, "knowledge", doc.ID, "privatised", "", "", "")
}
```

`Recap` and `RecapHash` are `*string` on `Knowledge` (`knowledge.go:38-39`), so they are set to `nil`, not `""`.

- [ ] **Step 4: Call it on the transition**

In `internal/core/knowledge.go`, in `refreshFromFile`, insert one line above the `privateDrifted` line added in Task 1 so the block reads:

```go
	becamePrivate := fm.Private && !doc.Private
	privateDrifted := doc.Private != fm.Private
	doc.Private = fm.Private
```

and after the `UPDATE knowledge SET ...` statement succeeds, before `syncDocRelations`:

```go
	if becamePrivate {
		if err := c.purgeDisclosedCopies(tx, doc); err != nil {
			return err
		}
	}
```

`becamePrivate` also fires when a restored database disagrees with its file. That is the safe direction and is intended: the restore purges and records `privatised`.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/core -run TestMarkingPrivatePurgesEveryLocalCopy -v`
Expected: PASS.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/core/purge.go internal/core/knowledge.go internal/core/private_test.go
git commit -m "feat(core): purge local copies when an entry becomes private

Clears the recap, drops its pins, and blanks the recap and body text that
pin and edit events had written into the audit log.

Content already sent to a remote embedder cannot be recalled and is not
claimed to be.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: The pin list refreshes before it discloses

**Files:**
- Modify: `internal/core/pin.go` (`Pin` struct, `Pins`)
- Test: `internal/core/private_test.go`

**Interfaces:**
- Consumes: `privateAfterRefresh` from Task 4, `Knowledge.Private` from Task 1.
- Produces: `Pin.ID string` with `json:"-"`, and a `Pins` result whose recaps reflect what the files say right now.

`Pins` (`pin.go:109`) is pure SQL over `k.recap`. It is what the session-start hook injects, so it is the single most direct path from a file to a model's context, and it currently cannot see a flag set since the row was last refreshed. Task 6 purges on transition, but the transition only happens when something refreshes that row — and a session brief that reads pins first is exactly the case where nothing has.

- [ ] **Step 1: Write the failing test**

Append to `internal/core/private_test.go`:

```go
// The session brief reads pins. Making Pins the very first call after the file
// changed is the whole point: any test that loads the document first refreshes
// the row as a side effect and proves nothing.
func TestPinsRedactAPrivateEntryWithNoPriorRead(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Staging cluster access", Body: "initial\n",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.PinKnowledge(t.Context(), p.ID, doc.Slug, "hunter2 opens staging", ""); err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}

	setPrivateInFile(t, doc.Path, true)

	pins, err := c.Pins(t.Context(), p.ID, "")
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	for _, pin := range pins {
		if pin.Slug != doc.Slug {
			continue
		}
		if strings.Contains(pin.Recap, "hunter2") {
			t.Fatalf("recap = %q reached the brief after the file said private", pin.Recap)
		}
	}
}

// An ordinary pin still carries its recap into the brief.
func TestPinsStillCarryAnOrdinaryRecap(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Recall ranking"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.PinKnowledge(t.Context(), p.ID, doc.Slug, "ranks are fused", ""); err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}

	pins, err := c.Pins(t.Context(), p.ID, "")
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	for _, pin := range pins {
		if pin.Slug == doc.Slug && pin.Recap != "ranks are fused" {
			t.Errorf("recap = %q, want the authored one", pin.Recap)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestPinsRedact|TestPinsStillCarry' -v`
Expected: `TestPinsRedactAPrivateEntryWithNoPriorRead` FAILS with the recap present; `TestPinsStillCarry...` PASSES.

- [ ] **Step 3: Carry the id out of the query**

In `internal/core/pin.go`, add to the `Pin` struct, above `Slug`:

```go
	// ID is not part of the payload — callers address a pin by slug — but the
	// disclosure refresh needs it. See privateAfterRefresh.
	ID string `db:"id" json:"-"`
```

and add `k.id,` to the front of the `SELECT` list in `Pins`.

- [ ] **Step 4: Refresh, then redact**

In `Pins`, replace the final `return tx.Select(&pins, q, args...)` with:

```go
		if err := tx.Select(&pins, q, args...); err != nil {
			return err
		}
		ids := make([]string, 0, len(pins))
		for _, pin := range pins {
			ids = append(ids, pin.ID)
		}
		// The brief is injected without anyone asking for it, so the flag is
		// read from the files rather than from the mirror, which is one read
		// stale after a hand edit. Pins are curated, so this is a handful of
		// stats and reads.
		private, err := c.privateAfterRefresh(tx, ids)
		if err != nil {
			return err
		}
		for i := range pins {
			if private[pins[i].ID] {
				pins[i].Recap = ""
			}
		}
		return nil
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestPins' -v`
Expected: PASS.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/core/pin.go internal/core/private_test.go
git commit -m "feat(core): the pin list reads the files before it fills the brief

Pins are injected at session start without being asked for, so the flag
cannot be read from a column that is one read behind the file.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: The CLI can set and show the flag

**Files:**
- Modify: `internal/cli/knowledge.go` (`new` command, `ls` renderer)
- Create: `internal/cli/knowledge_cmd_test.go`

**Interfaces:**
- Consumes: `NewKnowledge.Private` from Task 1, `Knowledge.Private` from Task 1.
- Produces: `trellis knowledge new --private`; `renderKnowledgeList(docs []core.Knowledge) string` extracted as a pure function; a `private` marker in the `ls` text output. JSON output already carries the field from its struct tag.

Two things the second draft got wrong here. `trellis init` has no `--name` flag — only `--pin`, `--key` and `--no-preset` (`internal/cli/init.go:61-63`) — and `init` without `--pin` in a bare temp directory fails in `resolve.Identify` with "not inside a git repository and no .trellis pin found". The harness below sidesteps both by setting `TRELLIS_PROJECT`, which short-circuits the whole resolution chain at `resolve.go:32-34`.

The `ls` renderer is extracted to a pure function because `Emit` (`internal/cli/output.go:16`) selects JSON when stdout is captured, which would bypass the marker and make a command-level assertion on it meaningless.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/knowledge_cmd_test.go`:

```go
package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

// TRELLIS_PROJECT is the first branch of resolve.Identify, so it needs neither
// a git repository nor a .trellis pin. TRELLIS_HOME keeps the vault and the
// database inside the test's temp directory.
func projectEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TRELLIS_HOME", t.TempDir())
	t.Setenv("TRELLIS_PROJECT", "TEST")
}

func runCmd(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestKnowledgeNewPrivateFlag(t *testing.T) {
	projectEnv(t)

	out := runCmd(t, "knowledge", "new", "--title", "Staging credentials", "--private", "--json")
	if !strings.Contains(out, `"private":true`) {
		t.Errorf("output did not report the flag:\n%s", out)
	}
}

// The renderer is tested directly: Emit picks JSON when stdout is captured, so
// asserting the marker through the command would assert nothing.
func TestRenderKnowledgeListMarksPrivate(t *testing.T) {
	got := renderKnowledgeList([]core.Knowledge{
		{Slug: "staging-credentials", DocType: "reference", Title: "Staging credentials", Private: true},
		{Slug: "recall-ranking", DocType: "decision", Title: "Recall ranking"},
	})

	var secret, open string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "staging-credentials") {
			secret = line
		}
		if strings.Contains(line, "recall-ranking") {
			open = line
		}
	}
	if secret == "" || open == "" {
		t.Fatalf("both rows must render:\n%s", got)
	}
	if !strings.Contains(secret, "private") {
		t.Errorf("private entry is not marked:\n%s", secret)
	}
	if strings.Contains(open, "private") {
		t.Errorf("ordinary entry is marked private:\n%s", open)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli -run 'TestKnowledgeNewPrivateFlag|TestRenderKnowledgeListMarksPrivate' -v`
Expected: FAIL — the package does not compile, `undefined: renderKnowledgeList`.

- [ ] **Step 3: Add the flag**

In `internal/cli/knowledge.go`, in the `new` command, declare `var private bool` beside the other flag variables, pass `Private: private` in the `core.NewKnowledge` literal, and register:

```go
	cmd.Flags().BoolVar(&private, "private", false,
		"do not transmit this body automatically: no vector index, no recap fallback, no content in the event log, pointer-only injection")
```

- [ ] **Step 4: Extract the renderer and add the marker**

In `internal/cli/knowledge.go`, add above the `ls` command. `fmt`, `strings` and `text/tabwriter` are already imported in this file:

```go
// renderKnowledgeList is the text form of `knowledge ls`. It is a function
// rather than a closure so it can be tested directly: Emit selects JSON
// whenever stdout is captured.
func renderKnowledgeList(docs []core.Knowledge) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, d := range docs {
		mark := ""
		if d.Private {
			mark = "private"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", d.Slug, d.DocType, d.Provenance, mark, d.Title)
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}
```

and replace the `ls` command's `Emit` closure body with `return renderKnowledgeList(docs)`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli -run 'TestKnowledgeNewPrivateFlag|TestRenderKnowledgeListMarksPrivate' -v`
Expected: PASS.

- [ ] **Step 6: Run the full gates, including the other platforms**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./... && GOOS=darwin go build ./...`
Expected: all pass. Note that a Windows cross-build is not a Windows test run; the file-editing tests in `internal/core` only prove themselves on the Windows CI job.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/knowledge.go internal/cli/knowledge_cmd_test.go
git commit -m "feat(cli): set and show the private flag

Adds the first command-level test harness in internal/cli, and extracts the
ls renderer so the marker can be asserted without Emit switching to JSON.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Self-review against the spec

| Spec requirement | Task |
|---|---|
| One frontmatter key, mirrored column, no index | 1 |
| Frontmatter is authoritative; the mirror is never trusted over the file | 1 (drift test), 2 (corpus), 4 (recall), 7 (pins) |
| Non-boolean value is a parse error | 1 |
| Route 1 + 2, vector index, closed | 2 |
| Route 3, pin recap fallback, closed | 3 |
| Route 4, recall recap fallback, closed | 4 |
| Route 5, event log, closed | 5 |
| Every vector call site reads one corpus | 2 (all six) |
| Exclusion is eviction; no separate vector step | 6 — stated in `purge.go`, no code added |
| Local FTS5 keeps indexing private entries | 2 — `rebuildKnowledgeFTS` reads the knowledge table unfiltered; asserted by `TestListKnowledgeStillReturnsPrivate` |
| `search`/`recall` still return private entries | 4 |
| Session injection is pointer-shaped (ref + title) | 4 and 7 — an empty recap is what makes a hit a pointer |
| Reclassification with no intervening read | 4 (recall first) and 7 (pins first) |
| `knowledge show` still returns the body | untouched by design |
| Reclassification purges recap column and pins | 6 |
| Reclassification purges pinned and edited events | 6 |
| Mirror drift is treated as a transition | 6 |
| Already-embedded content unrecoverable | 6 — stated in `purge.go`; no code claim made |
| No content inspection | no task adds any |

No spec requirement is left without a task.

**Deliberate omissions, both recorded in the spec's accepted limitations:**
- Old vectors are evicted at the next reconcile rather than immediately. A second draft engineered this away with an unexported `reindex` field and three notify call sites; the mechanism rested on a misreading of which functions notify, and `LoadKnowledge` already notifies unconditionally, so it is gone.
- No user-facing message says remote copies cannot be recalled, because the transition is triggered by a file edit rather than a command and there is no command output to attach one to.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-16-knowledge-disclosure-policy.md`. Two execution options:

1. **Subagent-Driven (recommended)** — a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.
