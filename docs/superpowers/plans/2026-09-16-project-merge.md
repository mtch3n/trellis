# Project Merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ] `) syntax for tracking.

**Goal:** `trellis project merge SRC --into DST` moves everything SRC owns into DST. Card refs and every reference to SRC keep working.

**Architecture:**
- A card's ref becomes a stored, globally unique column.
- A retired key is kept in `merged_project`.
- The merge runs as one transaction whose file operations are staged and undone on failure.
- Plan mode runs the same code in a transaction that is always rolled back, with file operations skipped.
- Wikilinks are rewritten through one function that sits beside the parser.
- Pins under the scan root are rewritten after the commit.

**Tech Stack:** Go 1.27, SQLite (`modernc.org/sqlite`, `sqlx`), goose v3.28, cobra, `encoding/json/v2`, sqlite-vec (`internal/vector`).

**Spec:** `docs/superpowers/specs/2026-09-16-project-merge-design.md`

## Prerequisite

Both earlier plans must already have been executed:
- `docs/superpowers/plans/2026-09-16-pin-only-projects.md`
- `docs/superpowers/plans/2026-09-16-virtual-paths.md`

This plan uses what they added:
- `internal/vpath`, including `Parse`, `SplitAnchor`, `CardPath`, `BoardPath` and `ValidKey`
- `internal/resolve/pin.go`
- `core.CreateProject`, `core.InitProject` and `core.DocAddress`
- `core.ParseReference`, with its absolute-address semantics
- `checkCardProject`, `crossProjectBlock` and `ResolveArtifact`
- `internal/cli/resolve.go` and `internal/cli/target.go` (`withTarget`, `targetContext`, `argProject`, `normalizeProjectArg`)
- migrations 0013 and 0014
- the CLI test helpers `pinEnv`, `seedProject`, `writePin`, `coreErr`, `showBoard`, `runCmd` and `execCmd`, plus the store test helpers `openAtVersion` and `mustExec`

Run `git log --oneline | head -30` and confirm both plans' commits are present. If they are not, stop.

## Global Constraints

- **No backward-compatibility shims.** Pre-1.0.
- **Markdown files are the source of truth for knowledge.** Every file move and rewrite is staged, and is undone if the transaction fails.
- **Store writes go through `writeAtomic`** or the staging helpers in `internal/core/file_store.go`. Pins are written with `writeAtomic`.
- **A dangling wikilink is a stub, not an error.** A merge re-resolves stubs; it never fails because of one.
- **Card refs are never renumbered.** `seq` is only a per-project allocator.
- **Vault entries are never renamed automatically.**
- **Every command stays listed in help and has a `Short`.**
- **CI runs Linux, macOS and Windows.**
- **Gates before any task is done:**
  - `go build ./...`
  - `go test ./...`
  - `go vet ./...`
  - `gofmt -l .` prints nothing
  - `staticcheck ./...`
  - `GOOS=windows go build ./...`
- **Commits** follow the repository's style and end with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`

## File Structure

| File | Responsibility |
|---|---|
| `internal/store/migrations/0015_card_refs_and_merges.sql` (create) | `card.ref` with its unique index; the `merged_project` table |
| `internal/store/migrate_0015_test.go` (create) | Migration tests |
| `internal/core/card.go` (modify) | Stored `Ref`; `createCard` writes it; `loadCard` looks it up; `cardElsewhere`; `CardHolder` |
| `internal/core/{blocker,graph,search,recall,doc_relations}.go`, `internal/cli/board_show.go`, `internal/ui/server.go` (modify) | Read `card.ref` instead of building the ref from key and seq |
| `internal/core/project.go` (modify) | `mergedTarget`, `errProjectMerged`; `ProjectByKey` and `createProject` honor the reservation; `DeleteProject` releases derived state |
| `internal/core/file_store.go` (modify) | `stageMove`, `stageRewrite`, `fileStage`, `copyUnder` |
| `internal/core/markdown.go` (modify) | `RewriteWikilinks` |
| `internal/core/core.go` (modify) | `SetDropDerived`, `dropDerived` |
| `internal/home/home.go` (modify) | `VectorDBFile` |
| `internal/vector/index.go` (modify) | `DropTables` |
| `internal/retrieval/service.go` (modify) | `DropProject` |
| `internal/core/merge.go` (create) | Types, entry point, refusals, boards, labels, tags, cards, config, pins plan, retirement, backup, after-commit steps |
| `internal/core/merge_docs.go` (create) | Knowledge moves, collapses, conflicts, renames, reference rewriting, stub resolution |
| `internal/core/merge_artifacts.go` (create) | Artifact moves, collapses, conflicts, renames |
| `internal/core/{merge,merge_docs,merge_artifacts}_test.go` (create) | Merge tests |
| `internal/resolve/pin.go` (modify) | `ScanRoot`, `PinsUnder` |
| `internal/cli/target.go`, `internal/cli/resolve.go` (modify) | Card-holder routing; `project_merged` fixes |
| `internal/cli/project.go` (modify) | `project merge` |
| `internal/cli/doctor.go` (modify) | Merge-aware `project` and `project keys` checks |
| `internal/cli/{root,daemon}.go`, `internal/ui/server.go` (modify) | Wire `SetDropDerived` |
| `README.md`, `plugin/skills/trellis/SKILL.md` (modify) | Document `project merge` |

---

### Task 1: A card's ref is stored

**Files:**
- Create: `internal/store/migrations/0015_card_refs_and_merges.sql`
- Test: `internal/store/migrate_0015_test.go`
- Modify: `internal/core/card.go`
- Modify: `internal/core/blocker.go`
- Modify: `internal/core/graph.go`
- Modify: `internal/core/search.go`
- Modify: `internal/core/recall.go`
- Modify: `internal/core/doc_relations.go`
- Modify: `internal/cli/board_show.go`
- Modify: `internal/ui/server.go`
- Modify: `internal/cli/target.go`
- Test: `internal/core/card_ref_test.go` (append)
- Test: `internal/cli/card_holder_test.go` (create)

**Interfaces:**
- Consumes:
  - `vpath.CardPath` (layer 2)
  - `twoProjectsWithCards`, `CardRef.Project`, `CardRef.qualified`, `projectKeyOf` (layer 2)
  - `seededProject`, `seededBoard`, `testCore` (existing)
  - `targetContext(a refArg, key, ref string, relative bool)`, `argProject`, `normalizeProjectArg`, `projectConflict`, `namedBoard`, `resolveProject`, `selectBoard` (earlier plans)
- Produces:
  - Goose version 15. It adds `card.ref TEXT NOT NULL`, the global unique index `card_ref`, and the table `merged_project(key, into_id, merged_at)`.
  - `Card.Ref` is now `db:"ref"`.
  - `func cardElsewhere(tx *sqlx.Tx, ref string) error`, which returns `card_not_found` (exit 3) or `wrong_project` (exit 2).
  - `func (c *Core) CardHolder(ctx context.Context, ref string) (Project, bool, error)`
  - `checkCardProject` is deleted.

- [ ] **Step 1: Write the failing migration test**

`internal/store/migrate_0015_test.go`:

```go
package store

import (
	"slices"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestCardRefsAreBackfilledAndUnique(t *testing.T) {
	db := openAtVersion(t, 14)
	for _, q := range []string{
		`INSERT INTO project (id, key, name, created_at) VALUES ('p1', 'ALPHA', 'ALPHA', 1), ('p2', 'MY-APP', 'MY-APP', 1)`,
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES ('b1', 'p1', 'alpha', 'alpha', 1, 1), ('b2', 'p2', 'app', 'app', 1, 1)`,
		`INSERT INTO column_ (id, board_id, name, position) VALUES ('c1', 'b1', 'backlog', 0), ('c2', 'b2', 'backlog', 0)`,
		`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, created_at, updated_at) VALUES
		   ('k1', 'p1', 'b1', 1, 'c1', 'a', 'one', 1, 1),
		   ('k2', 'p1', 'b1', 12, 'c1', 'b', 'twelve', 1, 1),
		   ('k3', 'p2', 'b2', 1, 'c2', 'a', 'other', 1, 1)`,
	} {
		mustExec(t, db, q)
	}
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	var refs []string
	if err := db.Select(&refs, `SELECT ref FROM card ORDER BY ref`); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ALPHA-1", "ALPHA-12", "MY-APP-1"}; !slices.Equal(refs, want) {
		t.Errorf("refs = %v, want %v", refs, want)
	}
	if _, err := db.Exec(`UPDATE card SET ref = 'ALPHA-1' WHERE id = 'k3'`); err == nil {
		t.Error("two cards can share a ref")
	}

	mustExec(t, db, `INSERT INTO merged_project (key, into_id, merged_at) VALUES ('OLD', 'p1', 1)`)
	mustExec(t, db, `DELETE FROM project WHERE id = 'p1'`)
	var reserved int
	if err := db.Get(&reserved, `SELECT count(*) FROM merged_project`); err != nil || reserved != 0 {
		t.Errorf("merged_project rows = %d, %v; deleting the survivor must free the key", reserved, err)
	}
}
```

Run: `go test ./internal/store/ -run CardRefsAreBackfilled`
Expected: FAIL with `no such column: ref`.

- [ ] **Step 2: Write the migration**

`internal/store/migrations/0015_card_refs_and_merges.sql`:

```sql
-- +goose Up
-- A card's ref is data, not a rendering of its project's key: after a project
-- merge, API-12 lives in MONO and is still called API-12. The index is global
-- because a ref's prefix is a key, keys are unique, and a merged key stays
-- reserved in merged_project below.
ALTER TABLE card ADD COLUMN ref TEXT NOT NULL DEFAULT '';
UPDATE card SET ref = (SELECT key FROM project WHERE project.id = card.project_id) || '-' || seq;
CREATE UNIQUE INDEX card_ref ON card(ref);

-- A key retired by a merge. A pin, --project or address naming it fails with a
-- pointer to where its contents went, and the key is never reused while cards
-- still carry it. Deleting the survivor frees it; those cards are gone too.
CREATE TABLE merged_project (
    key       TEXT PRIMARY KEY,
    into_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    merged_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE merged_project;
DROP INDEX card_ref;
ALTER TABLE card DROP COLUMN ref;
```

Run: `go test ./internal/store/`
Expected: `ok`.

- [ ] **Step 3: Write the failing core tests**

Append to `internal/core/card_ref_test.go`:

```go
// moveCardTo relocates a card by hand, the way a project merge does.
func moveCardTo(t *testing.T, c *Core, cardID string, p Project, seq int64) Board {
	t.Helper()
	b, err := c.SelectBoard(t.Context(), p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var col string
	if err := c.db.Get(&col, `SELECT id FROM column_ WHERE board_id = ? ORDER BY position LIMIT 1`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(`UPDATE card SET project_id = ?, board_id = ?, column_id = ?, seq = ? WHERE id = ?`,
		p.ID, b.ID, col, seq, cardID); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestACardKeepsItsRefInAnotherProject(t *testing.T) {
	c, p, _ := twoProjectsWithCards(t)
	ctx := t.Context()
	other, err := c.ProjectByKey(ctx, "OTHERPROJ")
	if err != nil {
		t.Fatal(err)
	}
	local, err := c.GetCard(ctx, p.ID, CardRef{Seq: 1})
	if err != nil {
		t.Fatal(err)
	}
	ob := moveCardTo(t, c, local.ID, other, 2)

	got, err := c.GetCard(ctx, other.ID, ParseCardRef("xpsctl-1"))
	if err != nil || got.ID != local.ID || got.Ref != "XPSCTL-1" {
		t.Fatalf("XPSCTL-1 in OTHERPROJ = %+v, %v", got, err)
	}
	if bare, err := c.GetCard(ctx, other.ID, ParseCardRef("1")); err != nil || bare.Ref != "OTHERPROJ-1" {
		t.Errorf("bare 1 in OTHERPROJ = %s, %v", bare.Ref, err)
	}
	_, err = c.GetCard(ctx, other.ID, ParseCardRef("OTHERPROJ-2"))
	if got := errCode(t, err); got != "card_not_found" {
		t.Errorf("seq 2 is an allocator value, not a ref: code = %s", got)
	}
	_, err = c.GetCard(ctx, p.ID, ParseCardRef("XPSCTL-1"))
	if got := errCode(t, err); got != "wrong_project" {
		t.Fatalf("XPSCTL-1 looked up in XPSCTL after the move: code = %s", got)
	}
	if te, _ := errors.AsType[*Error](err); !strings.Contains(te.Fix, "/OTHERPROJ/cards/XPSCTL-1") {
		t.Errorf("fix = %q", te.Fix)
	}

	_, err = c.GetCard(ctx, other.ID, ParseCardRef("/XPSCTL/cards/XPSCTL-1"))
	if got := errCode(t, err); got != "wrong_project" {
		t.Errorf("an address naming XPSCTL, looked up in OTHERPROJ: code = %s", got)
	}
	if got, err := c.GetCard(ctx, other.ID, ParseCardRef("/OTHERPROJ/cards/XPSCTL-1")); err != nil || got.ID != local.ID {
		t.Errorf("/OTHERPROJ/cards/XPSCTL-1 = %+v, %v", got, err)
	}

	holder, found, err := c.CardHolder(ctx, "xpsctl-1")
	if err != nil || !found || holder.Key != "OTHERPROJ" {
		t.Errorf("CardHolder = %+v, %v, %v", holder, found, err)
	}
	if _, found, _ := c.CardHolder(ctx, "12"); found {
		t.Error("a bare number has no holder")
	}

	next, err := c.CreateCard(ctx, other.ID, ob.ID, NewCard{Title: "next"})
	if err != nil || next.Ref != "OTHERPROJ-3" {
		t.Errorf("next card = %s, %v; want OTHERPROJ-3", next.Ref, err)
	}
}

func TestListingsShowTheStoredRef(t *testing.T) {
	c, p, pb := twoProjectsWithCards(t)
	ctx := t.Context()
	c.WithKBRoot(t.TempDir())
	blocked, err := c.CreateCard(ctx, p.ID, pb.ID, NewCard{Title: "waits on moved"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.BlockCard(ctx, p.ID, ParseCardRef(blocked.Ref), CardRef{Seq: 1}); err != nil {
		t.Fatal(err)
	}
	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Moved notes", Body: "about the sentinel"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LinkCardToDoc(ctx, p.ID, CardRef{Seq: 1}, doc.Slug); err != nil {
		t.Fatal(err)
	}
	// Rewrite the stored ref so every listing must read the column: nothing
	// can reach "RENAMED-9" by joining key and seq.
	if _, err := c.db.Exec(`UPDATE card SET ref = 'RENAMED-9' WHERE project_id = ? AND seq = 1`, p.ID); err != nil {
		t.Fatal(err)
	}

	blockers, err := c.Blockers(ctx, blocked.ID)
	if err != nil || len(blockers) != 1 || blockers[0].Ref != "RENAMED-9" {
		t.Errorf("Blockers = %+v, %v", blockers, err)
	}
	links, err := c.Backlinks(ctx, doc.ID)
	if err != nil || len(links) != 1 || links[0].Ref != "RENAMED-9" {
		t.Errorf("Backlinks = %+v, %v", links, err)
	}
	hits, err := c.Search(ctx, p.ID, "local", SearchOpts{})
	if err != nil || len(hits) == 0 || hits[0].Ref != "RENAMED-9" {
		t.Errorf("Search = %+v, %v", hits, err)
	}
	var id string
	if err := c.db.Get(&id, `SELECT id FROM card WHERE ref = 'RENAMED-9'`); err != nil {
		t.Fatal(err)
	}
	g, err := c.Traverse(ctx, id, 1, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range g.Nodes {
		found = found || (n.ID == id && n.Ref == "RENAMED-9")
	}
	if !found {
		t.Errorf("Traverse nodes = %+v, want the start card as RENAMED-9", g.Nodes)
	}
}
```

Add `"errors"` and `"strings"` to that file's imports if they are missing.

Run: `go test ./internal/core/ -run 'KeepsItsRef|StoredRef'`
Expected: FAIL. The build fails because `CardHolder` is undefined.

- [ ] **Step 4: Store and read the ref in core**

In `internal/core/card.go`:

1. In `Card`, delete the line `Ref string \`db:"-" json:"ref"\``, which is under "Computed for display". Add this line after `ArchivedAt`:

```go
	Ref        string   `db:"ref" json:"ref"` // stored: a merged card keeps its original prefix
```

2. In `cardView`, delete the `var key` declaration, the `SELECT key FROM project` query and the line `card.Ref = key + "-" + itoa(card.Seq)`. Declare `var colName string` on its own.

3. Replace `loadCard` together with the whole `checkCardProject` function and its comment:

```go
// loadCard fetches a card inside an existing transaction and fills computed fields.
// A qualified ref is matched against the stored ref, so a card that arrived
// through a merge is found under the prefix it was born with; a bare number
// means this project's own prefix.
func (c *Core) loadCard(tx *sqlx.Tx, projectID string, ref CardRef, out *Card) error {
	key, err := projectKeyOf(tx, projectID)
	if err != nil {
		return err
	}
	// An address names its project outright; the ref's prefix may differ
	// from it after a merge, the address's project may not.
	if ref.Project != "" && ref.Project != key {
		addr := ref.String() // an address renders as the address
		var exists int
		if err := tx.Get(&exists, `SELECT COUNT(*) FROM project WHERE key = ?`, ref.Project); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound("card_not_found",
				fmt.Sprintf("no card %s: there is no project %s", addr, ref.Project), "trellis project ls")
		}
		return ErrUsage("wrong_project", fmt.Sprintf("%s names project %s, not %s", addr, ref.Project, key),
			"trellis card show "+addr)
	}
	switch {
	case ref.UUID != "":
		err = tx.Get(out, `SELECT * FROM card WHERE id = ? AND project_id = ?`, ref.UUID, projectID)
	case ref.Seq > 0:
		want := ref.qualified()
		if ref.ProjectKey == "" {
			want = key + "-" + itoa(ref.Seq)
		}
		err = tx.Get(out, `SELECT * FROM card WHERE ref = ? AND project_id = ?`, want, projectID)
		if errors.Is(err, sql.ErrNoRows) && ref.ProjectKey != "" {
			return cardElsewhere(tx, want)
		}
	default:
		return ErrUsage("bad_card_ref", "card reference is empty", "trellis card ls")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound("card_not_found", "no card "+ref.String()+" in this project", "trellis card ls")
	}
	if err != nil {
		return err
	}
	return c.cardView(tx, out)
}

// cardElsewhere explains a qualified ref missing from the project it was
// looked up in: it is either a card in another project, or no card at all.
// OTHER-12 typed while working in KEY once opened KEY-12; a ref names one card.
func cardElsewhere(tx *sqlx.Tx, ref string) error {
	var holder string
	err := tx.Get(&holder,
		`SELECT p.key FROM card c JOIN project p ON p.id = c.project_id WHERE c.ref = ?`, ref)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound("card_not_found", "no card "+ref, "trellis card ls")
	}
	if err != nil {
		return err
	}
	return ErrUsage("wrong_project", fmt.Sprintf("%s is a card in project %s", ref, holder),
		"trellis card show "+vpath.CardPath(holder, ref).String())
}

// CardHolder finds the project that holds the card a qualified ref names,
// wherever its prefix points: after a merge, API-12 lives in MONO. A bare
// number or a UUID names no project by itself, so found is false.
func (c *Core) CardHolder(ctx context.Context, ref string) (Project, bool, error) {
	r := ParseCardRef(ref)
	if r.ProjectKey == "" {
		return Project{}, false, nil
	}
	var p Project
	err := c.db.GetContext(ctx, &p,
		`SELECT p.* FROM project p JOIN card c ON c.project_id = p.id WHERE c.ref = ?`, r.qualified())
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, nil
	}
	return p, err == nil, err
}
```

4. In `createCard`, read the key directly after the `MAX(seq)` query, set the ref, and add the column to the insert:

```go
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID); err != nil {
			return err
		}
```

```go
		card = Card{
			ID: NewCardID(), ProjectID: projectID, BoardID: boardID, Seq: seq, Ref: key + "-" + itoa(seq),
			ColumnID: col.ID, Title: in.Title, BodyMD: in.Body, Priority: prio,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
```

```go
		if _, err := tx.Exec(
			`INSERT INTO card (id, project_id, board_id, seq, ref, column_id, rank, title, body_md,
			                   priority, version, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			card.ID, card.ProjectID, card.BoardID, card.Seq, card.Ref, card.ColumnID, card.Rank, card.Title,
			card.BodyMD, int(card.Priority), card.Version, card.CreatedAt, card.UpdatedAt); err != nil {
			return err
		}
```

In `internal/core/blocker.go`:
- In `Blockers`, replace `p.key || '-' || b.seq AS ref` with `b.ref AS ref`, and delete the now-unused line `JOIN project p ON p.id = b.project_id`.
- In `crossProjectBlock`, replace the message with `fmt.Sprintf("%s is in another project; a card can only be blocked by a card in its own project", blocker)`.

In `internal/core/graph.go`, in the card branch of `Traverse`:
- delete the `var key string` declaration and its `SELECT key FROM project` query;
- replace `key+"-"+itoa(card.Seq)` with `card.Ref`.

In `internal/core/search.go` and `internal/core/recall.go`, replace `p.key || '-' || c.seq AS ref` with `c.ref AS ref`.

In `internal/core/doc_relations.go`, in `Backlinks`, replace `SELECT 'card', p.key || '-' || c.seq, c.title` with `SELECT 'card', c.ref, c.title`.

- [ ] **Step 5: Read the ref in the brief and the web API**

In `internal/cli/board_show.go`, `queryBrief`:
- In the `owned` struct, replace `Seq int64 \`db:"seq"\`` with `Ref string \`db:"ref"\``. In its query, replace `SELECT c.id, c.seq, c.title,` with `SELECT c.id, c.ref, c.title,`. Then use `Ref: o.Ref,`.
- In the `otherCards` struct, replace `Seq` in the same way. In its query, replace `SELECT c.seq, c.title, c.owner` with `SELECT c.ref, c.title, c.owner`. Then use `Ref: o.Ref,`.

In `internal/ui/server.go`, `handleProjectEvents`:
- Replace the row field `CardSeq *int64 \`db:"card_seq"\`` with `CardRef *string \`db:"card_ref"\``.
- In the query, replace `c.seq AS card_seq` with `c.ref AS card_ref`.
- Replace the switch case with:

```go
		case row.CardRef != nil:
			event.Ref = *row.CardRef
```

In `handleBoardCards`, in the `cards` struct, replace `Seq int64 \`db:"seq"\`` with `Ref string \`db:"ref"\``. In the query, replace `SELECT id, seq, title,` with `SELECT id, ref, title,`. Then use `Ref: c.Ref,`.

Run: `go build ./... && grep -rn "|| '-' ||\|\"%s-%d\", .*Seq\|+ \"-\" + fmt\|key+\"-\"+itoa" --include='*.go' internal`
Expected: the build succeeds and grep prints nothing.

- [ ] **Step 6: Route a qualified card ref to the project that holds it**

`internal/cli/card_holder_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
)

// moveFirstCard creates API-1 and moves it into MONO the way a merge does.
func moveFirstCard(t *testing.T) {
	t.Helper()
	api := seedProject(t, "API")
	mono := seedProject(t, "MONO")
	path, err := home.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.RealClock{}, "test")
	ab, err := c.SelectBoard(t.Context(), api.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(t.Context(), api.ID, ab.ID, core.NewCard{Title: "moved"})
	if err != nil {
		t.Fatal(err)
	}
	mb, err := c.SelectBoard(t.Context(), mono.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`UPDATE card SET project_id = ?, board_id = ?, seq = 7,
		   column_id = (SELECT id FROM column_ WHERE board_id = ? ORDER BY position LIMIT 1)
		 WHERE id = ?`, mono.ID, mb.ID, mb.ID, card.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAMovedCardIsFoundByItsRef(t *testing.T) {
	pinEnv(t, "anywhere")
	moveFirstCard(t)

	if out := runCmd(t, "card", "show", "api-1", "--json"); !strings.Contains(out, `"ref":"API-1"`) {
		t.Errorf("card show api-1 = %s", out)
	}
	if out := runCmd(t, "card", "show", "/MONO/cards/API-1", "--json"); !strings.Contains(out, `"ref":"API-1"`) {
		t.Errorf("card show by address = %s", out)
	}
	// --project MONO agrees with where the card is, whatever its prefix says.
	runCmd(t, "--project", "MONO", "card", "show", "API-1")

	_, err := execCmd("card", "show", "/API/cards/API-1")
	if ce := coreErr(t, err); ce.Code != "wrong_project" || !strings.Contains(ce.Fix, "/MONO/cards/API-1") {
		t.Errorf("stale address: %+v", ce)
	}
	_, err = execCmd("--project", "API", "card", "show", "API-1")
	if ce := coreErr(t, err); ce.Code != "project_conflict" {
		t.Errorf("--project naming the old prefix: %+v", ce)
	}
}
```

Run: `go test ./internal/cli/ -run MovedCard`
Expected: FAIL. `card show api-1` routes to project API and gets `wrong_project`.

In `internal/cli/target.go`, `targetContext`, change only the branch that runs when the argument names a project, below the `if key == "" { ... }` block:

1. Delete the `--project` conflict check that precedes `openCore()`:

```go
	if flag := normalizeProjectArg(projectFlagKey); flag != "" && flag != key {
		return nil, projectConflict(flag, a.Value, key)
	}
```

2. Directly after `ctx := context.Background()`, and before `r, rerr := resolveProject(ctx, c)`, insert the holder lookup, followed by the same check, which now compares against the project that actually holds the card:

```go
	// A card ref's prefix is where the card was born. After a merge it lives
	// elsewhere, so a qualified ref (not an address, which names its project
	// outright) is routed to the project that holds it.
	if a.Collection == vpath.CollectionCards && !strings.HasPrefix(strings.TrimSpace(a.Value), "/") {
		holder, found, err := c.CardHolder(ctx, ref)
		if err != nil {
			return fail(err)
		}
		if found {
			key = holder.Key
		}
	}
	if flag := normalizeProjectArg(projectFlagKey); flag != "" && flag != key {
		return fail(projectConflict(flag, a.Value, key))
	}
```

Leave the rest of `targetContext` as the virtual-paths plan left it.

- [ ] **Step 7: Run everything**

Run: `gofmt -l . ; go test ./... && go vet ./...`
Expected: `gofmt` prints nothing, and every package reports `ok`. The layer-2 tests `TestAQualifiedRefIsNeverRescoped` and `TestACardAddressKeepsItsProject` still pass:
- `OTHERPROJ-1` is found in OTHERPROJ and reported as `wrong_project`.
- An address naming a missing project is `card_not_found`.
- `/XPSCTL/cards/OTHERPROJ-1` misses in XPSCTL, and `cardElsewhere` reports `wrong_project`.

- [ ] **Step 8: Commit**

```bash
git add internal/store internal/core internal/cli internal/ui
git commit -m "feat(core): a card's ref is stored, so it survives a move between projects

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: A merged key stays reserved

**Files:**
- Modify: `internal/core/project.go`: add `mergedTarget` and `errProjectMerged`, and change `ProjectByKey` and `createProject`
- Modify: `internal/cli/resolve.go`: `resolveProject`
- Modify: `internal/cli/target.go`: new `mergedAddress`, used in `targetContext`
- Modify: `internal/cli/doctor.go`: `checkProject`
- Test: `internal/core/merged_key_test.go` (create)
- Test: `internal/cli/merged_key_test.go` (create)

**Interfaces:**
- Consumes: the `merged_project` table (Task 1); `vpath.Parse`, `vpath.SplitAnchor` (layer 2).
- Produces:
  - `func mergedTarget(tx *sqlx.Tx, key string) (string, error)`, which returns the survivor's key or `""`
  - `func errProjectMerged(key, into string) error`: code `project_merged`, exit 3, `Detail` of type `map[string]string{"into": into}`
  - Error code `key_reserved` (exit 4)
  - `func mergedAddress(err error, value string) error` in `cli`

- [ ] **Step 1: Write the failing core test**

`internal/core/merged_key_test.go`:

```go
package core

import (
	"strings"
	"testing"
)

func retire(t *testing.T, c *Core, key string, into Project) {
	t.Helper()
	if _, err := c.db.Exec(`INSERT INTO merged_project (key, into_id, merged_at) VALUES (?, ?, 1)`, key, into.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAMergedKeyIsReserved(t *testing.T) {
	c := testCore(t).WithKBRoot(t.TempDir())
	ctx := t.Context()
	mono, err := c.CreateProject(ctx, "MONO", false)
	if err != nil {
		t.Fatal(err)
	}
	retire(t, c, "API", mono)

	_, err = c.ProjectByKey(ctx, "api")
	if got := errCode(t, err); got != "project_merged" {
		t.Fatalf("ProjectByKey: code = %s", got)
	}
	te := err.(*Error)
	if te.Exit != 3 || !strings.Contains(te.Msg, "MONO") || te.Detail.(map[string]string)["into"] != "MONO" {
		t.Errorf("error = %+v", te)
	}

	_, err = c.CreateProject(ctx, "API", false)
	if got := errCode(t, err); got != "key_reserved" {
		t.Errorf("CreateProject: code = %s", got)
	}
	_, err = c.InitProject(ctx, InitRequest{Dir: t.TempDir(), Key: "API", Join: true})
	if got := errCode(t, err); got != "key_reserved" {
		t.Errorf("InitProject: code = %s", got)
	}

	if err := c.DeleteProject(ctx, "MONO"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateProject(ctx, "API", false); err != nil {
		t.Errorf("deleting the survivor must free the key: %v", err)
	}
}
```

Run: `go test ./internal/core/ -run MergedKeyIsReserved`
Expected: FAIL. The code is `project_not_found`.

- [ ] **Step 2: Implement the reservation in core**

In `internal/core/project.go`, add:

```go
// mergedTarget is the key of the project a retired key was merged into, or
// "" when key was never merged away.
func mergedTarget(tx *sqlx.Tx, key string) (string, error) {
	var into string
	err := tx.Get(&into,
		`SELECT p.key FROM merged_project m JOIN project p ON p.id = m.into_id WHERE m.key = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return into, err
}

// errProjectMerged is what naming a retired key gets. Detail carries the
// survivor's key for callers that can build a better fix, such as the same
// address in the survivor.
func errProjectMerged(key, into string) error {
	return &Error{
		Code: "project_merged", Exit: 3,
		Msg:    fmt.Sprintf("project %s was merged into %s", key, into),
		Fix:    "trellis --project " + into + " <command>",
		Detail: map[string]string{"into": into},
	}
}
```

In `ProjectByKey`, make the `errors.Is(err, sql.ErrNoRows)` branch check the reservation first:

```go
		if errors.Is(err, sql.ErrNoRows) {
			into, merr := mergedTarget(tx, strings.ToUpper(key))
			if merr != nil {
				return merr
			}
			if into != "" {
				return errProjectMerged(strings.ToUpper(key), into)
			}
			var keys []string
```

Leave the rest of that branch unchanged.

In `createProject`, directly after `checkNewKey`, add:

```go
	into, err := mergedTarget(tx, key)
	if err != nil {
		return Project{}, err
	}
	if into != "" {
		return Project{}, ErrConflict("key_reserved",
			fmt.Sprintf("%s was merged into %s, and its key stays reserved while cards still carry it", key, into),
			"trellis init --key "+into)
	}
```

Run: `go test ./internal/core/`
Expected: `ok`.

- [ ] **Step 3: Write the failing CLI test**

`internal/cli/merged_key_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
)

// retireKey records key as merged into into, as an applied merge does.
func retireKey(t *testing.T, key, into string) {
	t.Helper()
	path, err := home.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`INSERT INTO merged_project (key, into_id, merged_at) SELECT ?, id, 1 FROM project WHERE key = ?`,
		key, into); err != nil {
		t.Fatal(err)
	}
}

func TestAMergedKeyPointsAtItsSurvivor(t *testing.T) {
	dir := pinEnv(t, "api")
	seedProject(t, "MONO")
	retireKey(t, "API", "MONO")

	writePin(t, dir, "/API\n")
	_, err := execCmd("card", "ls")
	ce := coreErr(t, err)
	if ce.Code != "project_merged" || !strings.Contains(ce.Msg, ".trellis") || !strings.Contains(ce.Msg, "MONO") {
		t.Errorf("pin: %+v", ce)
	}

	_, err = execCmd("--project", "api", "card", "ls")
	if ce := coreErr(t, err); ce.Code != "project_merged" {
		t.Errorf("--project: %+v", ce)
	}

	_, err = execCmd("--project", "MONO", "knowledge", "show", "/API/knowledge/runbook#steps")
	if ce := coreErr(t, err); ce.Code != "project_conflict" {
		t.Errorf("an address naming another project than --project: %+v", ce)
	}
	_, err = execCmd("knowledge", "show", "/API/knowledge/runbook#steps")
	ce = coreErr(t, err)
	if ce.Code != "project_merged" || ce.Fix != "trellis knowledge show /MONO/knowledge/runbook#steps" {
		t.Errorf("address: %+v", ce)
	}

	_, err = execCmd("project", "new", "API")
	if ce := coreErr(t, err); ce.Code != "key_reserved" {
		t.Errorf("project new: %+v", ce)
	}
}

func TestDoctorNamesTheSurvivorOfAPinnedKey(t *testing.T) {
	dir := pinEnv(t, "api")
	seedProject(t, "MONO")
	retireKey(t, "API", "MONO")
	writePin(t, dir, "/API\n")
	got := checkProject()
	if got.Status != checkWarn || !strings.Contains(got.Detail, "merged into MONO") {
		t.Errorf("check = %+v", got)
	}
}
```

Run: `go test ./internal/cli/ -run 'MergedKey|SurvivorOfAPinnedKey'`
Expected: FAIL. The pin error lacks the pin path, the address fix is generic, and doctor reports "no such project".

- [ ] **Step 4: Better fixes in the CLI**

In `internal/cli/resolve.go`, `resolveProject`, directly after `p, err := c.ProjectByKey(ctx, pin.Target.Project)`, add:

```go
	if ce, ok := errors.AsType[*core.Error](err); ok && ce.Code == "project_merged" {
		into := mergedInto(ce)
		return resolvedProject{}, core.ErrNotFound("project_merged",
			fmt.Sprintf("%s names %s, which was merged into %s", pin.Path, pin.Target.Project, into),
			"edit "+pin.Path+" to name /"+into+"/boards/<board>, then commit it")
	}
```

Append to `internal/cli/resolve.go`:

```go
// mergedInto reads the survivor's key off a project_merged error.
func mergedInto(ce *core.Error) string {
	detail, _ := ce.Detail.(map[string]string)
	return detail["into"]
}
```

In `internal/cli/target.go`, in `targetContext`, replace the `p, err := c.ProjectByKey(ctx, key)` error return with:

```go
	p, err := c.ProjectByKey(ctx, key)
	if err != nil {
		return fail(mergedAddress(err, a.Value))
	}
```

Append to `internal/cli/target.go`:

```go
// showCommand is the command that opens an address in each collection.
var showCommand = map[string]string{
	vpath.CollectionCards:     "trellis card show ",
	vpath.CollectionKnowledge: "trellis knowledge show ",
	vpath.CollectionBoards:    "trellis board show --board ",
}

// mergedAddress points a project_merged error at the same address in the
// project the key went to. A document renamed during that merge has another
// slug there; the merge listed every rename when it ran.
func mergedAddress(err error, value string) error {
	ce, ok := errors.AsType[*core.Error](err)
	if !ok || ce.Code != "project_merged" {
		return err
	}
	into := mergedInto(ce)
	target, anchor := vpath.SplitAnchor(strings.TrimSpace(value))
	p, perr := vpath.Parse(strings.TrimSpace(target))
	if perr != nil || into == "" {
		return err
	}
	p.Project = into
	addr := p.String()
	if anchor != "" {
		addr += "#" + anchor
	}
	fix := "trellis artifact ls --project " + into
	if cmd, ok := showCommand[p.Collection]; ok {
		fix = cmd + addr
	}
	return &core.Error{Code: ce.Code, Msg: ce.Msg, Fix: fix, Exit: ce.Exit, Detail: ce.Detail}
}
```

In `internal/cli/doctor.go`, `checkProject`, replace the `if n == 0 { ... }` block with:

```go
	if n == 0 {
		var into string
		if err := db.Get(&into,
			`SELECT p.key FROM merged_project m JOIN project p ON p.id = m.into_id WHERE m.key = ?`,
			pin.Target.Project); err == nil {
			return warn("project", fmt.Sprintf("%s, but %s was merged into %s", detail, pin.Target.Project, into),
				"edit "+pin.Path+" to name /"+into+"/boards/<board>")
		}
		return warn("project", detail+", but this database has no such project", "trellis init")
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l . ; go test ./internal/cli/ ./internal/core/ && go vet ./...`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/core internal/cli
git commit -m "feat(core): a merged key stays reserved and points at its survivor

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: File operations that a failed transaction can undo

**Files:**
- Modify: `internal/core/file_store.go`
- Test: `internal/core/file_stage_test.go` (create)

**Interfaces:**
- Consumes: `writeAtomic`, `copyAtomic`, `syncDirectory`, `fileHash` (existing).
- Produces:
  - `type fileStage struct`, with the methods:
    - `move(from, to string) error`
    - `rewrite(path string, data []byte) error`
    - `rollback() error`
  - `func copyUnder(root, dir string, files []string) (map[string]string, error)`, which returns the hash of each copy by source path

- [ ] **Step 1: Write the failing tests**

`internal/core/file_stage_test.go`:

```go
package core

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestFileStageRollsBackInReverseOrder(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "projects", "API", "knowledge", "a.md")
	dst := filepath.Join(root, "projects", "MONO", "knowledge", "a.md")
	other := filepath.Join(root, "projects", "CORE", "knowledge", "b.md")
	writeFile(t, src, "moved")
	writeFile(t, other, "before")

	s := &fileStage{}
	if err := s.move(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := s.rewrite(other, []byte("after")); err != nil {
		t.Fatal(err)
	}
	if exists(src) || readFile(t, dst) != "moved" || readFile(t, other) != "after" {
		t.Fatal("the staged operations did not happen")
	}

	if err := s.rollback(); err != nil {
		t.Fatal(err)
	}
	if exists(dst) || readFile(t, src) != "moved" || readFile(t, other) != "before" {
		t.Error("rollback did not restore the files")
	}
}

func TestFileStageNeverOverwritesOnMove(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "a.md"), filepath.Join(root, "b.md")
	writeFile(t, src, "src")
	writeFile(t, dst, "dst")
	s := &fileStage{}
	if err := s.move(src, dst); err == nil {
		t.Fatal("a move onto an existing file succeeded")
	}
	if readFile(t, src) != "src" || readFile(t, dst) != "dst" {
		t.Error("a refused move changed a file")
	}
}

func TestCopyUnderKeepsThePathsBelowTheRoot(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "projects", "API", "knowledge", "a.md")
	b := filepath.Join(root, "global", "knowledge", "b.md")
	writeFile(t, a, "A")
	writeFile(t, b, "B")
	out := filepath.Join(t.TempDir(), "files")
	hashes, err := copyUnder(root, out, []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := fileHash(a); hashes[a] != want || len(hashes) != 2 {
		t.Errorf("hashes = %v", hashes)
	}
	if readFile(t, filepath.Join(out, "projects", "API", "knowledge", "a.md")) != "A" ||
		readFile(t, filepath.Join(out, "global", "knowledge", "b.md")) != "B" {
		t.Error("the copies are not laid out as under the root")
	}
	if _, err := copyUnder(root, out, []string{filepath.Join(t.TempDir(), "x.md")}); err == nil {
		t.Error("a file outside the root was accepted")
	}
}
```

Run: `go test ./internal/core/ -run 'FileStage|CopyUnder'`
Expected: FAIL. The build fails because `fileStage` and `copyUnder` are undefined.

- [ ] **Step 2: Write the implementation**

Append to `internal/core/file_store.go`, and add `"fmt"` and `"strings"` to its imports:

```go
// fileStage records file operations made inside a database transaction, so
// that a failed transaction can undo them, newest first. Every path is under
// the storage root, so a move never crosses filesystems.
type fileStage struct {
	undo []func() error
}

// move publishes from at to with a hard link, which fails if anything is at
// to -- even a file that appears after any check -- and then removes from.
// writeAtomic publishes the same way. The undo is registered as soon as the
// link exists, because every later step can fail.
func (s *fileStage) move(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if err := os.Link(from, to); err != nil {
		return fmt.Errorf("cannot move %s to %s: %w", from, to, err)
	}
	s.undo = append(s.undo, func() error {
		if _, err := os.Lstat(from); errors.Is(err, os.ErrNotExist) {
			if err := os.Link(to, from); err != nil {
				return err
			}
		}
		return os.Remove(to)
	})
	if err := os.Remove(from); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(to)); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(from))
}

// rewrite replaces a file's content, keeping the old bytes for undo. The undo
// is registered before the write: writeAtomic can replace the file and then
// fail to sync its directory, and that file must still be restored. Undoing a
// write that never happened rewrites the same bytes.
func (s *fileStage) rewrite(path string, data []byte) error {
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s.undo = append(s.undo, func() error { return writeAtomic(path, old, true) })
	return writeAtomic(path, data, true)
}

// rollback undoes every recorded operation, newest first, and reports every
// failure rather than stopping at the first.
func (s *fileStage) rollback() error {
	var errs []error
	for i := len(s.undo) - 1; i >= 0; i-- {
		errs = append(errs, s.undo[i]())
	}
	s.undo = nil
	return errors.Join(errs...)
}

// copyUnder copies each file into dir at its path relative to root, and
// returns the hash of every copy by source path. A file outside root is
// refused: the copy is a backup of the storage root.
func copyUnder(root, dir string, files []string) (map[string]string, error) {
	hashes := make(map[string]string, len(files))
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%s is outside the storage root %s", f, root)
		}
		dest := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return nil, err
		}
		if err := copyAtomic(dest, f); err != nil {
			return nil, err
		}
		if hashes[f], err = fileHash(dest); err != nil {
			return nil, err
		}
	}
	return hashes, nil
}
```

Run: `go test ./internal/core/ -run 'FileStage|CopyUnder' && go vet ./internal/core/`
Expected: `ok`.

- [ ] **Step 3: Commit**

```bash
git add internal/core/file_store.go internal/core/file_stage_test.go
git commit -m "feat(core): stage file moves and rewrites so a failed transaction undoes them

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Rewriting wikilinks in place

**Files:**
- Modify: `internal/core/markdown.go`
- Test: `internal/core/markdown_test.go` (append)

**Interfaces:**
- Consumes: `ParseReference`, `wikiLinkRE`, `fenceRE` (existing, as changed by layer 2).
- Produces: `func RewriteWikilinks(text string, fn func(Reference) (string, bool)) string`

- [ ] **Step 1: Write the failing test**

Append to `internal/core/markdown_test.go`:

```go
func TestRewriteWikilinks(t *testing.T) {
	text := "See [[/API/knowledge/runbook#Roll back|the runbook]] and [[design]].\n" +
		"Inline `[[/API/knowledge/runbook]]` stays.\n" +
		"```\n[[/API/knowledge/runbook]]\n```\n" +
		"Also [[/api/knowledge/runbook]] and [[/OTHER/knowledge/runbook]].\n"
	got := RewriteWikilinks(text, func(ref Reference) (string, bool) {
		if ref.ProjectKey != "API" || ref.Slug != "runbook" {
			return "", false
		}
		target := "/MONO/knowledge/runbook-api"
		if i := strings.Index(ref.Raw, "#"); i >= 0 {
			target += ref.Raw[i:]
		}
		return target, true
	})
	want := "See [[/MONO/knowledge/runbook-api#Roll back|the runbook]] and [[design]].\n" +
		"Inline `[[/API/knowledge/runbook]]` stays.\n" +
		"```\n[[/API/knowledge/runbook]]\n```\n" +
		"Also [[/MONO/knowledge/runbook-api]] and [[/OTHER/knowledge/runbook]].\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if same := RewriteWikilinks(text, func(Reference) (string, bool) { return "", false }); same != text {
		t.Error("a rewrite that changes nothing altered the text")
	}
}
```

Add `"strings"` to that file's imports if it is missing.

Run: `go test ./internal/core/ -run RewriteWikilinks`
Expected: FAIL. `RewriteWikilinks` is undefined.

- [ ] **Step 2: Write the implementation**

Append to `internal/core/markdown.go`:

```go
// RewriteWikilinks replaces link targets in text. fn sees every wikilink that
// ParseWikilinks would see -- code spans and fenced blocks are skipped the
// same way -- and returns the new target, anchor included, or false to leave
// the link alone. An alias after | is kept as written.
func RewriteWikilinks(text string, fn func(Reference) (string, bool)) string {
	code := fenceRE.FindAllStringIndex(text, -1)
	inCode := func(start, end int) bool {
		for _, span := range code {
			if start < span[1] && span[0] < end {
				return true
			}
		}
		return false
	}
	var b strings.Builder
	last := 0
	for _, m := range wikiLinkRE.FindAllStringSubmatchIndex(text, -1) {
		if inCode(m[0], m[1]) {
			continue
		}
		raw := strings.TrimSpace(text[m[2]:m[3]])
		if m[4] >= 0 {
			raw += text[m[4]:m[5]]
		}
		next, ok := fn(ParseReference(raw))
		if !ok {
			continue
		}
		alias := ""
		if m[6] >= 0 {
			alias = text[m[6]:m[7]]
		}
		b.WriteString(text[last:m[0]])
		b.WriteString("[[" + next + alias + "]]")
		last = m[1]
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}
```

Run: `go test ./internal/core/ -run 'RewriteWikilinks|Wikilink' && go vet ./internal/core/`
Expected: `ok`.

- [ ] **Step 3: Commit**

```bash
git add internal/core/markdown.go internal/core/markdown_test.go
git commit -m "feat(core): rewrite wikilink targets without touching code

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: A removed project's vector tables are dropped

`vec_knowledge_<hash>` and `vec_admin_<hash>` are virtual tables in `trellis.db`, and their names derive from the project's vector-file path (`internal/vector/index.go`, `tableName`). Nothing drops them today, including `DeleteProject`.

Dropping them is safe at any time:
- sqlite-vec's `Destroy` only closes a handle, and the vectors themselves live in `vectors.db`;
- `vector.New` recreates both tables with `CREATE VIRTUAL TABLE IF NOT EXISTS` the next time the project is indexed.

Callers therefore drop them **before** a removal's transaction, while the vector file is still at the path the tables were created with. If the removal then fails, nothing is lost.

**Files:**
- Modify: `internal/home/home.go`: new `VectorDBFile`
- Modify: `internal/vector/index.go`: new `DropTables`
- Test: `internal/vector/index_test.go` (append)
- Modify: `internal/retrieval/service.go`: new `DropProject`
- Modify: `internal/core/core.go`: new `SetDropDerived` and `dropDerived`
- Modify: `internal/core/project.go`: `DeleteProject`
- Test: `internal/core/project_delete_test.go` (append)
- Modify: `internal/cli/root.go`, `internal/cli/daemon.go`, `internal/ui/server.go`: wiring

**Interfaces:**
- Produces:
  - `func home.VectorDBFile(projectKey string) (string, error)`, which creates nothing
  - `func vector.DropTables(ctx context.Context, db *sql.DB, dbPath string) error`
  - `func (s *retrieval.Service) DropProject(ctx context.Context, projectKey string) error`
  - `func (c *Core) SetDropDerived(fn func(ctx context.Context, projectKey string) error)`
  - `func (c *Core) dropDerived(ctx context.Context, projectKey string) error`

- [ ] **Step 1: Write the failing vector test**

Append to `internal/vector/index_test.go`:

```go
func TestDropTablesRemovesAProjectsVirtualTables(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.VectorSearchConfig{Enabled: true, EmbedCommand: fakeEmbed, Dimension: 2, Limit: 5}
	gone := filepath.Join(dir, "projects", "API", "vectors.db")
	kept := filepath.Join(dir, "projects", "MONO", "vectors.db")
	for _, p := range []string{gone, kept} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		idx, err := New(db, p, cfg)
		if err != nil {
			t.Fatal(err)
		}
		idx.Close()
	}
	count := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name LIKE 'vec_%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()

	if err := DropTables(t.Context(), db, gone); err != nil {
		t.Fatalf("DropTables: %v", err)
	}
	if got := count(); got != before-2 {
		t.Errorf("vec tables = %d, want %d", got, before-2)
	}
	if err := DropTables(t.Context(), db, gone); err != nil {
		t.Errorf("dropping twice: %v", err)
	}
	// The survivor's tables still work, and the dropped ones come back on demand.
	for _, p := range []string{kept, gone} {
		idx, err := New(db, p, cfg)
		if err != nil {
			t.Fatalf("New(%s) after the drop: %v", p, err)
		}
		idx.Close()
	}
}
```

Run: `go test ./internal/vector/ -run DropTables`
Expected: FAIL. `DropTables` is undefined.

If the drop fails once `DropTables` exists, with an error saying the module cannot be destroyed, stop and report it. Do not work around it.

- [ ] **Step 2: Write `DropTables` and `VectorDBFile`**

Append to `internal/vector/index.go`:

```go
// DropTables removes the virtual tables New created in db for the index at
// dbPath. Their names derive from that path, so a project that is deleted or
// merged away leaves them behind otherwise. Dropping is safe at any time: the
// vectors live in dbPath, and New recreates both tables on demand.
func DropTables(ctx context.Context, db *sql.DB, dbPath string) error {
	virtual := tableName(dbPath)
	admin := "vec_admin_" + strings.TrimPrefix(virtual, "vec_knowledge_")
	for _, table := range []string{admin, virtual} {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			return fmt.Errorf("drop %s: %w", table, err)
		}
	}
	return nil
}
```

In `internal/home/home.go`, replace `VectorDBPath` with:

```go
// VectorDBFile is where a project's vector index lives. It creates nothing:
// removing a project needs the path only to name that index's tables.
func VectorDBFile(projectKey string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "projects", projectKey, "vectors.db"), nil
}

// VectorDBPath returns the disposable per-project vector index path, creating
// the project's directory.
func VectorDBPath(projectKey string) (string, error) {
	if _, err := ProjectRoot(projectKey); err != nil {
		return "", err
	}
	return VectorDBFile(projectKey)
}
```

Append to `internal/retrieval/service.go`:

```go
// DropProject forgets the vector tables of a project that is about to be
// removed. Its files go with the project's directory.
func (s *Service) DropProject(ctx context.Context, projectKey string) error {
	path, err := home.VectorDBFile(projectKey)
	if err != nil {
		return err
	}
	return vector.DropTables(ctx, s.db.DB, path)
}
```

Run: `go test ./internal/vector/ ./internal/home/ ./internal/retrieval/`
Expected: `ok`.

- [ ] **Step 3: Write the failing core test**

Append to `internal/core/project_delete_test.go`:

```go
func TestDeleteProjectDropsDerivedStateFirst(t *testing.T) {
	c := testCore(t).WithKBRoot(t.TempDir())
	p := seededProject(t, c)
	seededBoard(t, c, p)
	var dropped []string
	c.SetDropDerived(func(_ context.Context, key string) error {
		var n int
		if err := c.db.Get(&n, `SELECT count(*) FROM project WHERE key = ?`, key); err != nil {
			return err
		}
		if n != 1 {
			t.Errorf("derived state dropped after %s was already gone", key)
		}
		dropped = append(dropped, key)
		return errors.New("a derived-state failure must not block the delete")
	})
	if err := c.DeleteProject(t.Context(), p.Key); err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 1 || dropped[0] != p.Key {
		t.Errorf("dropped = %v", dropped)
	}
}
```

Add `"context"` and `"errors"` to that file's imports if they are missing.

Run: `go test ./internal/core/ -run DropsDerived`
Expected: FAIL. `SetDropDerived` is undefined.

- [ ] **Step 4: Add the hook, and use it in `DeleteProject`**

In `internal/core/core.go`, add a field to `Core` after `knowledgeChanged`:

```go
	// dropDerivedFn releases derived state that is keyed by a project's key --
	// the vector tables -- before the project is removed or merged away.
	dropDerivedFn func(context.Context, string) error
```

Append:

```go
func (c *Core) SetDropDerived(fn func(context.Context, string) error) { c.dropDerivedFn = fn }

// dropDerived is best effort: the state it drops is rebuilt on demand, so a
// failure is reported by the caller and never blocks the removal.
func (c *Core) dropDerived(ctx context.Context, projectKey string) error {
	if c.dropDerivedFn == nil {
		return nil
	}
	return c.dropDerivedFn(ctx, projectKey)
}
```

In `internal/core/project.go`, `DeleteProject`, insert this directly after `key = strings.ToUpper(strings.TrimSpace(key))`:

```go
	// Dropped first, while the vector file is still where the tables point.
	// A refused or failed delete loses nothing: the tables come back on use.
	_ = c.dropDerived(ctx, key)
```

Wire the hook next to each existing `SetKnowledgeChanged` call:
- `internal/cli/root.go`, `openCore`: `c.SetDropDerived(search.DropProject)`
- `internal/cli/daemon.go`: `c.SetDropDerived(search.DropProject)`
- `internal/ui/server.go`, `NewServerWithSearch`: `c.SetDropDerived(search.DropProject)`

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l . ; go test ./... && go vet ./...`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/home internal/vector internal/retrieval internal/core internal/cli internal/ui
git commit -m "fix(core): drop a removed project's vector tables instead of leaking them

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: The merge engine: boards, cards, vocabulary, config, pins, retirement

This task builds `MergeProjects` with plan and apply modes, and a backup before apply. Knowledge and artifacts arrive in Tasks 7 and 8. Until then, `load` refuses a project that owns either, so nothing can be half-merged.

**Files:**
- Create: `internal/core/merge.go`
- Test: `internal/core/merge_test.go`

**Interfaces:**
- Consumes:
  - `fileStage`, `copyUnder` (Task 3)
  - `dropDerived` (Task 5)
  - `mergedTarget`, `errProjectMerged` (Task 2)
  - `normalizeKey`, `slugify`, `plural`, `recordEvent`, `rebuildKnowledgeFTS`, `Core.knowledgeChanged`, `fileHash`, `Backup` (existing)
  - `resolve.Pin`, `vpath.BoardPath`, `vpath.ValidKey`
- Produces:
  - `type MergeOptions struct{ Apply, RenameConflicts bool; ScanRoot string; Pins []resolve.Pin; UnreadablePins []string }`
  - `type MergePlan struct`, with the fields in the code below
  - `BoardMove`, `CardMoves`, `ItemMoves`, `Rename`, `MergeConflict`, `NameMoves`, `ConfigDrop`, `PinRewrites` and `PinRewrite`
  - `func (c *Core) MergeProjects(ctx context.Context, srcKey, dstKey string, opts MergeOptions) (MergePlan, error)`
  - Error codes `merge_refused`, `merge_conflicts` and `merge_changed` (exit 4)
  - Unexported, for Tasks 7–9:
    - `merger`, with the fields `docPath`, `fromSrc`, `addr`, `renamed`, `boardSlug`, `srcDefaultSlug`, `root`, `stage`, `apply` and `backedUp`
    - `(*merger).run`, `(*merger).touch`
    - `MergePlan.files`, `MergePlan.dstID`
    - `(*Core).afterMerge`
    - `mergeNotReady`

- [ ] **Step 1: Write the failing tests**

`internal/core/merge_test.go`:

```go
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/vpath"
)

type mergeFixture struct {
	t                   *testing.T
	c                   *Core
	root                string
	api, mono           Project
	apiBoard, monoBoard Board
}

func newMergeFixture(t *testing.T) *mergeFixture {
	t.Helper()
	root := t.TempDir()
	f := &mergeFixture{t: t, c: testCore(t).WithKBRoot(root), root: root}
	f.api, f.apiBoard = f.project("API")
	f.mono, f.monoBoard = f.project("MONO")
	return f
}

func (f *mergeFixture) project(key string) (Project, Board) {
	f.t.Helper()
	p, err := f.c.CreateProject(f.t.Context(), key, true)
	if err != nil {
		f.t.Fatal(err)
	}
	b, err := f.c.SelectBoard(f.t.Context(), p.ID, "")
	if err != nil {
		f.t.Fatal(err)
	}
	return p, b
}

func (f *mergeFixture) board(p Project, name string) Board {
	f.t.Helper()
	b, err := f.c.CreateBoard(f.t.Context(), p.ID, name, true)
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func (f *mergeFixture) card(p Project, b Board, title string, labels, tags []string) Card {
	f.t.Helper()
	card, err := f.c.CreateCard(f.t.Context(), p.ID, b.ID, NewCard{Title: title, Labels: labels, Tags: tags})
	if err != nil {
		f.t.Fatal(err)
	}
	return card
}

func (f *mergeFixture) merge(opts MergeOptions) MergePlan {
	f.t.Helper()
	plan, err := f.c.MergeProjects(f.t.Context(), "api", "mono", opts)
	if err != nil {
		f.t.Fatalf("MergeProjects: %v", err)
	}
	return plan
}

func (f *mergeFixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.c.db.Get(&n, q, args...); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *mergeFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.c.db.Exec(q, args...); err != nil {
		f.t.Fatal(err)
	}
}

// snapshot is every row count a merge could change, and the files under root.
func (f *mergeFixture) snapshot() string {
	f.t.Helper()
	var b strings.Builder
	for _, table := range []string{"project", "board", "column_", "card", "label", "tag", "card_label",
		"card_tag", "knowledge", "knowledge_label", "artifact", "link", "event", "merged_project", "project_config"} {
		fmt.Fprintf(&b, "%s=%d ", table, f.count("SELECT count(*) FROM "+table))
	}
	filepath.WalkDir(f.root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == "backups" {
			return filepath.SkipDir // an applied merge writes one; it is not state
		}
		if err == nil && !d.IsDir() {
			raw, _ := os.ReadFile(path)
			fmt.Fprintf(&b, "\n%s %d", path, len(raw))
		}
		return nil
	})
	return b.String()
}

func TestMergeMovesBoardsCardsLabelsAndTags(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	f.board(f.api, "Web")
	f.board(f.mono, "Web")
	if _, err := f.c.CreateLabel(ctx, f.api.ID, "infra", "Infrastructure work"); err != nil {
		t.Fatal(err)
	}
	first := f.card(f.api, f.apiBoard, "first", []string{"bug", "infra"}, []string{"x"})
	f.card(f.api, f.apiBoard, "second", nil, nil)
	f.card(f.mono, f.monoBoard, "mono one", nil, nil)

	plan := f.merge(MergeOptions{Apply: true})

	wantBoards := []BoardMove{
		{Name: "api", Slug: "api", NewName: "api", NewSlug: "api"},
		{Name: "Web", Slug: "web", NewName: "Web (API)", NewSlug: "api-web"},
	}
	if !slices.Equal(plan.Boards, wantBoards) {
		t.Errorf("boards = %+v", plan.Boards)
	}
	if plan.Cards != (CardMoves{Moved: 2, FirstSeq: 2}) {
		t.Errorf("cards = %+v", plan.Cards)
	}
	if !slices.Contains(plan.Labels.Moved, "infra") || !slices.Contains(plan.Labels.Folded, "bug") {
		t.Errorf("labels = %+v", plan.Labels)
	}
	if !slices.Equal(plan.Tags.Moved, []string{"x"}) {
		t.Errorf("tags = %+v", plan.Tags)
	}

	got, err := f.c.GetCard(ctx, f.mono.ID, ParseCardRef("API-1"))
	if err != nil || got.ID != first.ID {
		t.Fatalf("API-1 in MONO = %+v, %v", got, err)
	}
	if !slices.Equal(got.Labels, []string{"bug", "infra"}) || !slices.Equal(got.Tags, []string{"x"}) {
		t.Errorf("labels %v, tags %v", got.Labels, got.Tags)
	}
	if next := f.card(f.mono, f.monoBoard, "after", nil, nil); next.Ref != "MONO-4" {
		t.Errorf("next MONO card = %s, want MONO-4", next.Ref)
	}
	if _, err := f.c.ProjectByKey(ctx, "API"); errCode(t, err) != "project_merged" {
		t.Errorf("API after the merge: %v", err)
	}
	boards, err := f.c.ListBoards(ctx, f.mono.ID)
	if err != nil || len(boards) != 4 {
		t.Fatalf("MONO boards = %+v, %v", boards, err)
	}
	for _, b := range boards {
		if b.IsDefault != (b.Name == "mono") {
			t.Errorf("board %s default = %v", b.Name, b.IsDefault)
		}
	}
	if n := f.count(`SELECT count(*) FROM label WHERE project_id = ? AND name = 'bug'`, f.mono.ID); n != 1 {
		t.Errorf("MONO has %d bug labels", n)
	}
	if n := f.count(`SELECT count(*) FROM label WHERE project_id = ?`, f.api.ID); n != 0 {
		t.Errorf("%d labels still belong to API", n)
	}
}

func TestMergePlanChangesNothingAndMatchesApply(t *testing.T) {
	f := newMergeFixture(t)
	f.board(f.mono, "api") // forces a board rename
	f.card(f.api, f.apiBoard, "first", []string{"bug"}, nil)
	before := f.snapshot()

	plan := f.merge(MergeOptions{})
	if !plan.Ready || plan.Backup != "" || plan.Refused != "" {
		t.Errorf("plan = %+v", plan)
	}
	if after := f.snapshot(); after != before {
		t.Errorf("plan mode changed something:\nbefore %s\nafter  %s", before, after)
	}

	applied := f.merge(MergeOptions{Apply: true})
	if applied.Backup == "" {
		t.Error("apply reported no backup")
	}
	applied.Backup, applied.Warnings = "", nil
	if !reflect.DeepEqual(plan, applied) {
		t.Errorf("plan and apply differ:\nplan    %+v\napplied %+v", plan, applied)
	}
}

func TestMergeRefusals(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()

	plan, err := f.c.MergeProjects(ctx, "API", "api", MergeOptions{})
	if err != nil || plan.Ready || plan.Refused == "" {
		t.Errorf("into itself: %+v, %v", plan, err)
	}
	_, err = f.c.MergeProjects(ctx, "API", "API", MergeOptions{Apply: true})
	if got := errCode(t, err); got != "merge_refused" {
		t.Errorf("into itself, apply: %s", got)
	}
	_, err = f.c.MergeProjects(ctx, "API", "NOPE", MergeOptions{})
	if got := errCode(t, err); got != "project_not_found" {
		t.Errorf("missing DST: %s", got)
	}

	held := f.card(f.api, f.apiBoard, "held", nil, nil)
	f.exec(`UPDATE card SET owner = 'agent:x', lease_until = ? WHERE id = ?`, f.c.clock.NowMS()+60_000, held.ID)
	if plan := f.merge(MergeOptions{}); plan.Ready || !strings.Contains(plan.Refused, "held") {
		t.Errorf("held lease: %+v", plan)
	}
	_, err = f.c.MergeProjects(ctx, "API", "MONO", MergeOptions{Apply: true})
	if got := errCode(t, err); got != "merge_refused" {
		t.Errorf("held lease, apply: %s", got)
	}
	if f.count(`SELECT count(*) FROM project WHERE key = 'API'`) != 1 {
		t.Error("a refused merge removed API")
	}
	f.exec(`UPDATE card SET owner = NULL, lease_until = NULL WHERE id = ?`, held.ID)

	// A key from before the key grammar, with a card whose ref carries it.
	f.exec(`INSERT INTO project (id, key, name, created_at) VALUES ('odd', 'MY_APP', 'MY_APP', 1)`)
	oddBoard := f.board(Project{ID: "odd", Key: "MY_APP"}, "odd")
	legacy := f.card(Project{ID: "odd", Key: "MY_APP"}, oddBoard, "legacy", nil, nil)
	if legacy.Ref != "MY_APP-1" {
		t.Fatalf("legacy ref = %s", legacy.Ref)
	}
	plan, err = f.c.MergeProjects(ctx, "API", "MY_APP", MergeOptions{})
	if err != nil || !strings.Contains(plan.Refused, "MY_APP") {
		t.Errorf("DST without a pinnable key: %+v, %v", plan, err)
	}
	if _, err := f.c.MergeProjects(ctx, "MY_APP", "MONO", MergeOptions{Apply: true}); err != nil {
		t.Fatalf("a SRC without a pinnable key must merge: %v", err)
	}
	for _, ref := range []string{"MY_APP-1", "/MONO/cards/MY_APP-1"} {
		if got, err := f.c.GetCard(ctx, f.mono.ID, ParseCardRef(ref)); err != nil || got.ID != legacy.ID {
			t.Errorf("%s after the merge = %+v, %v", ref, got, err)
		}
	}
}

func TestMergeListsDroppedConfigAndBacksUp(t *testing.T) {
	f := newMergeFixture(t)
	f.exec(`INSERT INTO project_config (project_id, key, value, updated_at) VALUES
	          (?, 'lease.ttl', '10m', 1), (?, 'card.ls_limit', '20', 1), (?, 'search.method', 'fts', 1),
	          (?, 'lease.ttl', '10m', 1), (?, 'card.ls_limit', '50', 1)`,
		f.api.ID, f.api.ID, f.api.ID, f.mono.ID, f.mono.ID)

	plan := f.merge(MergeOptions{Apply: true})

	want := []ConfigDrop{{Key: "card.ls_limit", Src: "20", Dst: "50"}, {Key: "search.method", Src: "fts", Dst: ""}}
	if !slices.Equal(plan.ConfigDropped, want) {
		t.Errorf("config dropped = %+v", plan.ConfigDropped)
	}
	if n := f.count(`SELECT count(*) FROM project_config WHERE project_id = ?`, f.mono.ID); n != 2 {
		t.Errorf("MONO has %d overrides, want its own 2", n)
	}
	if !strings.HasPrefix(plan.Backup, filepath.Join(f.root, "backups", "merge-API-into-MONO-")) {
		t.Errorf("backup = %s", plan.Backup)
	}
	if _, err := os.Stat(filepath.Join(plan.Backup, "trellis.db")); err != nil {
		t.Errorf("backup database: %v", err)
	}
}

func TestMergeRepointsEarlierMerges(t *testing.T) {
	f := newMergeFixture(t)
	f.project("CORE")
	f.merge(MergeOptions{Apply: true})
	if _, err := f.c.MergeProjects(t.Context(), "MONO", "CORE", MergeOptions{Apply: true}); err != nil {
		t.Fatal(err)
	}
	_, err := f.c.ProjectByKey(t.Context(), "API")
	if got := errCode(t, err); got != "project_merged" || !strings.Contains(err.Error(), "CORE") {
		t.Errorf("API after two merges: %v", err)
	}
}

func TestMergePlansPinRewrites(t *testing.T) {
	f := newMergeFixture(t)
	f.board(f.api, "Web")
	pins := []resolve.Pin{
		{Path: "/r/api/.trellis", Target: vpath.ProjectPath("API")},
		{Path: "/r/web/.trellis", Target: vpath.BoardPath("API", "web")},
		{Path: "/r/gone/.trellis", Target: vpath.BoardPath("API", "gone")},
		{Path: "/r/.trellis", Target: vpath.ProjectPath("MONO")},
	}
	plan := f.merge(MergeOptions{ScanRoot: "/r", Pins: pins, UnreadablePins: []string{"/r/bad/.trellis"}})
	want := []PinRewrite{
		{Path: "/r/api/.trellis", From: "/API", To: "/MONO/boards/api"},
		{Path: "/r/web/.trellis", From: "/API/boards/web", To: "/MONO/boards/web"},
	}
	if plan.Pins.ScanRoot != "/r" || !slices.Equal(plan.Pins.Rewrite, want) ||
		!slices.Equal(plan.Pins.Left, []string{"/r/gone/.trellis", "/r/bad/.trellis"}) {
		t.Errorf("pins = %+v", plan.Pins)
	}
}
```

Run: `go test ./internal/core/ -run Merge`
Expected: FAIL. The build fails because `MergeProjects` and `MergeOptions` are undefined.

- [ ] **Step 2: Write `internal/core/merge.go`**

```go
package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/vpath"
)

// MergeOptions tunes `trellis project merge`.
type MergeOptions struct {
	// Apply performs the merge; without it the merge only reports its plan.
	Apply bool
	// RenameConflicts renames SRC's side of a name collision instead of
	// stopping. Vault entries are never renamed.
	RenameConflicts bool
	// ScanRoot, Pins and UnreadablePins describe the pins the caller found.
	// Core never walks the filesystem for them.
	ScanRoot       string
	Pins           []resolve.Pin
	UnreadablePins []string
}

// MergePlan is what a merge would do, or did.
type MergePlan struct {
	Src                string          `json:"src"`
	Dst                string          `json:"dst"`
	Ready              bool            `json:"ready"`
	Refused            string          `json:"refused"`
	Boards             []BoardMove     `json:"boards"`
	Cards              CardMoves       `json:"cards"`
	Knowledge          ItemMoves       `json:"knowledge"`
	Artifacts          ItemMoves       `json:"artifacts"`
	Labels             NameMoves       `json:"labels"`
	Tags               NameMoves       `json:"tags"`
	ConfigDropped      []ConfigDrop    `json:"config_dropped"`
	DocumentsRewritten []string        `json:"documents_rewritten"`
	Pins               PinRewrites     `json:"pins"`
	Backup             string          `json:"backup,omitempty"`
	Warnings           []string        `json:"warnings,omitempty"`

	dstID string
	files []string // every file the merge moves or rewrites, as it was before
}

type BoardMove struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	NewName string `json:"new_name"`
	NewSlug string `json:"new_slug"`
}

type CardMoves struct {
	Moved    int   `json:"moved"`
	FirstSeq int64 `json:"first_seq"`
}

type ItemMoves struct {
	Moved     int             `json:"moved"`
	Collapsed []string        `json:"collapsed"`
	Renamed   []Rename        `json:"renamed"`
	Conflicts []MergeConflict `json:"conflicts"`
}

type Rename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type MergeConflict struct {
	Name    string `json:"name"`
	SrcHash string `json:"src_hash,omitempty"`
	DstHash string `json:"dst_hash,omitempty"`
	Vault   bool   `json:"vault"`            // SRC's side is a vault entry, which is never renamed
	Reason  string `json:"reason,omitempty"` // set when the conflict is not a name clash
}

type NameMoves struct {
	Moved  []string `json:"moved"`
	Folded []string `json:"folded"`
}

type ConfigDrop struct {
	Key string `db:"key" json:"key"`
	Src string `db:"src" json:"src"`
	Dst string `db:"dst" json:"dst"`
}

type PinRewrites struct {
	ScanRoot string       `json:"scan_root"`
	Rewrite  []PinRewrite `json:"rewrite"`
	Left     []string     `json:"left"`
}

type PinRewrite struct {
	Path string `json:"path"`
	From string `json:"from"`
	To   string `json:"to"`
}

// errPlanOnly rolls back a plan-mode transaction; it never reaches a caller.
var errPlanOnly = errors.New("merge plan: rolled back by design")

// MergeProjects moves everything SRC owns into DST, and retires SRC's key.
//
// The plan and the apply are one code path: a plan runs the merge in a
// transaction that is always rolled back, with file operations skipped, so a
// plan is never a separate estimate. Apply re-runs it for real after a backup.
func (c *Core) MergeProjects(ctx context.Context, srcKey, dstKey string, opts MergeOptions) (MergePlan, error) {
	srcKey, dstKey = normalizeKey(srcKey), normalizeKey(dstKey)
	plan, err := c.runMerge(ctx, srcKey, dstKey, opts, nil)
	if err != nil || !opts.Apply {
		return plan, err
	}
	if !plan.Ready {
		return plan, mergeNotReady(plan)
	}
	backup, backedUp, err := c.backupForMerge(ctx, plan)
	if err != nil {
		return plan, fmt.Errorf("backing up before the merge: %w", err)
	}
	var warnings []string
	if err := c.dropDerived(ctx, srcKey); err != nil {
		warnings = append(warnings, "dropping "+srcKey+"'s vector tables: "+err.Error())
	}
	applied, err := c.runMerge(ctx, srcKey, dstKey, opts, backedUp)
	applied.Backup = backup
	applied.Warnings = append(warnings, applied.Warnings...)
	if err != nil {
		return applied, err
	}
	c.afterMerge(ctx, &applied)
	return applied, nil
}

// runMerge runs the merge in one transaction. backedUp is nil for a plan;
// for an apply it holds the hash of every file the backup copied, and the
// merge touches no file outside it.
func (c *Core) runMerge(ctx context.Context, srcKey, dstKey string, opts MergeOptions, backedUp map[string]string) (MergePlan, error) {
	plan := MergePlan{Src: srcKey, Dst: dstKey}
	stage := &fileStage{}
	apply := backedUp != nil
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		m := &merger{
			c: c, tx: tx, plan: &plan, opts: opts, apply: apply, stage: stage, backedUp: backedUp,
			docPath: map[string]string{}, fromSrc: map[string]bool{}, addr: map[string]string{},
			renamed: map[string]string{}, boardSlug: map[string]string{},
		}
		if err := m.run(srcKey, dstKey); err != nil {
			return err
		}
		if !apply {
			return errPlanOnly
		}
		return nil
	})
	switch {
	case err == nil:
		return plan, nil
	case errors.Is(err, errPlanOnly):
		return plan, nil // plan mode stages no file operations
	}
	if rbErr := stage.rollback(); rbErr != nil {
		err = errors.Join(err, fmt.Errorf("restoring files after the failed merge: %w", rbErr))
	}
	return plan, err
}

// mergeNotReady is the error an apply gets when its plan is not ready.
func mergeNotReady(p MergePlan) error {
	if p.Refused != "" {
		return ErrConflict("merge_refused", p.Refused, "")
	}
	var names []string
	vault := false
	for _, group := range [][]MergeConflict{p.Knowledge.Conflicts, p.Artifacts.Conflicts} {
		for _, conflict := range group {
			names = append(names, conflict.Name)
			vault = vault || conflict.Vault
		}
	}
	fix := "trellis project merge " + p.Src + " --into " + p.Dst + " --rename-conflicts --apply"
	if vault {
		fix = "trellis knowledge demote <slug>   # vault entries are never renamed: demote or edit one side"
	}
	return ErrConflict("merge_conflicts",
		fmt.Sprintf("%s and %s both hold %s: %s", p.Src, p.Dst,
			plural(len(names), "an item with this name", "items with these names"), strings.Join(names, ", ")),
		fix)
}

// backupForMerge writes the database and a copy of every file the merge will
// touch, and returns the hash of each copy. VACUUM cannot run inside a
// transaction, so this precedes the merge; the apply then refuses to touch a
// file the backup does not hold as it is, so a change made in between stops
// the merge instead of escaping the backup.
func (c *Core) backupForMerge(ctx context.Context, plan MergePlan) (string, map[string]string, error) {
	root, err := c.root()
	if err != nil {
		return "", nil, err
	}
	stamp := time.UnixMilli(c.clock.NowMS()).UTC().Format("20060102T150405Z")
	dir := filepath.Join(root, "backups", fmt.Sprintf("merge-%s-into-%s-%s", plan.Src, plan.Dst, stamp))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	if err := c.Backup(ctx, filepath.Join(dir, "trellis.db")); err != nil {
		return "", nil, err
	}
	hashes, err := copyUnder(root, filepath.Join(dir, "files"), plan.files)
	return dir, hashes, err
}

// touch records a file the merge moves or rewrites. A plan collects them for
// the backup. An apply accepts only a file the backup copied and that has not
// changed since.
func (m *merger) touch(path string) error {
	if slices.Contains(m.plan.files, path) {
		return nil
	}
	m.plan.files = append(m.plan.files, path)
	if !m.apply {
		return nil
	}
	want, ok := m.backedUp[path]
	if !ok {
		return errMergeChanged(path, "appeared after the backup")
	}
	got, err := fileHash(path)
	if err != nil {
		return err
	}
	if got != want {
		return errMergeChanged(path, "changed after the backup")
	}
	return nil
}

func errMergeChanged(path, what string) error {
	return ErrConflict("merge_changed",
		fmt.Sprintf("%s %s; the merge changed nothing", path, what),
		"trellis project merge <SRC> --into <DST> --apply   # run it again")
}

// afterMerge runs what cannot be part of the transaction. Each step is best
// effort and reports into Warnings.
func (c *Core) afterMerge(ctx context.Context, plan *MergePlan) {
	if c.knowledgeChanged != nil {
		if err := c.knowledgeChanged(ctx, plan.dstID); err != nil {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("refreshing %s's derived search state: %v", plan.Dst, err))
		}
	}
}

// merger is one run of a merge inside its transaction.
type merger struct {
	c        *Core
	tx       *sqlx.Tx
	plan     *MergePlan
	opts     MergeOptions
	apply    bool
	backedUp map[string]string // path -> hash of its backup copy; nil for a plan
	stage    *fileStage
	root     string

	src, dst Project

	boardSlug      map[string]string // SRC board slug -> its slug in DST
	srcDefaultSlug string            // DST slug of SRC's default board, or ""
	docPath        map[string]string // document id -> where its file is now
	fromSrc        map[string]bool   // documents that came from SRC and still exist
	addr           map[string]string // SRC document address -> its address now
	renamed        map[string]string // SRC slug -> its slug in DST, for renamed entries
}

func (m *merger) run(srcKey, dstKey string) error {
	refused, err := m.load(srcKey, dstKey)
	if err != nil {
		return err
	}
	if refused {
		if m.apply {
			return mergeNotReady(*m.plan)
		}
		return nil
	}
	m.plan.Ready = true
	for _, step := range []func() error{m.boards, m.labels, m.tags, m.cards, m.config, m.pins, m.retire} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// load reads both projects and decides whether the merge may run at all.
func (m *merger) load(srcKey, dstKey string) (refused bool, err error) {
	if m.root, err = m.c.root(); err != nil {
		return false, err
	}
	if srcKey == dstKey {
		m.plan.Refused = "a project cannot be merged into itself"
		return true, nil
	}
	if m.src, err = m.project(srcKey); err != nil {
		return false, err
	}
	if m.dst, err = m.project(dstKey); err != nil {
		return false, err
	}
	m.plan.dstID = m.dst.ID

	held, err := m.count(`SELECT COUNT(*) FROM card WHERE project_id = ? AND owner IS NOT NULL AND lease_until > ?`,
		m.src.ID, m.c.clock.NowMS())
	if err != nil {
		return false, err
	}
	// Until documents and artifacts can move, a project that owns any is
	// refused rather than half-merged.
	owned, err := m.count(`SELECT (SELECT COUNT(*) FROM knowledge WHERE project_id = ?) +
	                              (SELECT COUNT(*) FROM artifact WHERE project_id = ?)`, m.src.ID, m.src.ID)
	if err != nil {
		return false, err
	}
	switch {
	case !vpath.ValidKey(m.dst.Key):
		m.plan.Refused = fmt.Sprintf("%s cannot be named by a pin; merge into a project whose key can", m.dst.Key)
	case held > 0:
		m.plan.Refused = fmt.Sprintf("%s has %d %s held by an agent right now", m.src.Key, held, plural(held, "card", "cards"))
	case owned > 0:
		m.plan.Refused = "merging knowledge and artifacts is not implemented yet"
	}
	return m.plan.Refused != "", nil
}

func (m *merger) project(key string) (Project, error) {
	var p Project
	err := m.tx.Get(&p, `SELECT * FROM project WHERE key = ?`, key)
	if !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	into, err := mergedTarget(m.tx, key)
	if err != nil {
		return p, err
	}
	if into != "" {
		return p, errProjectMerged(key, into)
	}
	return p, ErrNotFound("project_not_found", "no project "+key, "trellis project ls")
}

func (m *merger) count(q string, args ...any) (int, error) {
	var n int
	err := m.tx.Get(&n, q, args...)
	return n, err
}

// boards moves SRC's boards into DST as separate boards. A clashing slug gets
// SRC's key as a prefix and a clashing name gets it as a suffix, so the moved
// board is recognizable; DST's default stays the default.
func (m *merger) boards() error {
	var dst, src []Board
	if err := m.tx.Select(&dst, `SELECT * FROM board WHERE project_id = ?`, m.dst.ID); err != nil {
		return err
	}
	if err := m.tx.Select(&src, `SELECT * FROM board WHERE project_id = ? ORDER BY created_at`, m.src.ID); err != nil {
		return err
	}
	slugs, names := map[string]bool{}, map[string]bool{}
	for _, b := range dst {
		slugs[b.Slug], names[b.Name] = true, true
	}
	prefix := slugify(m.src.Key)
	for _, b := range src {
		slug, name := b.Slug, b.Name
		if slugs[slug] {
			slug = freeName(prefix+"-"+b.Slug, slugs)
		}
		if names[name] {
			name = freeName(fmt.Sprintf("%s (%s)", b.Name, m.src.Key), names)
		}
		slugs[slug], names[name] = true, true
		m.boardSlug[b.Slug] = slug
		if b.IsDefault {
			m.srcDefaultSlug = slug
		}
		m.plan.Boards = append(m.plan.Boards, BoardMove{Name: b.Name, Slug: b.Slug, NewName: name, NewSlug: slug})
		if _, err := m.tx.Exec(`UPDATE board SET project_id = ?, name = ?, slug = ?, is_default = 0 WHERE id = ?`,
			m.dst.ID, name, slug, b.ID); err != nil {
			return err
		}
	}
	return nil
}

// freeName is base, or base-2, base-3, ... whichever is not taken.
func freeName(base string, taken map[string]bool) string {
	name := base
	for n := 2; taken[name]; n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	return name
}

func (m *merger) labels() error {
	return m.foldNames("label", "label_id", &m.plan.Labels,
		[][2]string{{"card_label", "card_id"}, {"knowledge_label", "doc_id"}})
}

func (m *merger) tags() error {
	return m.foldNames("tag", "tag_id", &m.plan.Tags,
		[][2]string{{"card_tag", "card_id"}, {"knowledge_tag", "doc_id"}})
}

// foldNames moves SRC's rows of a per-project vocabulary into DST. A name DST
// already has is folded into DST's row: every junction row is re-pointed, and
// SRC's row is deleted, which cascades its old junction rows away.
func (m *merger) foldNames(table, column string, out *NameMoves, junctions [][2]string) error {
	var rows []struct {
		ID   string  `db:"id"`
		Name string  `db:"name"`
		Into *string `db:"into_id"`
	}
	if err := m.tx.Select(&rows,
		`SELECT s.id, s.name, d.id AS into_id FROM `+table+` s
		 LEFT JOIN `+table+` d ON d.project_id = ? AND d.name = s.name
		 WHERE s.project_id = ? ORDER BY s.name`, m.dst.ID, m.src.ID); err != nil {
		return err
	}
	for _, r := range rows {
		if r.Into == nil {
			if _, err := m.tx.Exec(`UPDATE `+table+` SET project_id = ? WHERE id = ?`, m.dst.ID, r.ID); err != nil {
				return err
			}
			out.Moved = append(out.Moved, r.Name)
			continue
		}
		for _, j := range junctions {
			if _, err := m.tx.Exec(
				`INSERT OR IGNORE INTO `+j[0]+` (`+j[1]+`, `+column+`)
				 SELECT `+j[1]+`, ? FROM `+j[0]+` WHERE `+column+` = ?`, *r.Into, r.ID); err != nil {
				return err
			}
		}
		if _, err := m.tx.Exec(`DELETE FROM `+table+` WHERE id = ?`, r.ID); err != nil {
			return err
		}
		out.Folded = append(out.Folded, r.Name)
	}
	return nil
}

// cards moves SRC's cards after DST's highest seq. Their refs are stored and
// do not change; seq is only DST's allocator.
func (m *merger) cards() error {
	if err := m.tx.Get(&m.plan.Cards.FirstSeq,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM card WHERE project_id = ?`, m.dst.ID); err != nil {
		return err
	}
	var ids []string
	if err := m.tx.Select(&ids, `SELECT id FROM card WHERE project_id = ? ORDER BY seq`, m.src.ID); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := m.tx.Exec(`UPDATE card SET project_id = ?, seq = ? WHERE id = ?`,
			m.dst.ID, m.plan.Cards.FirstSeq+int64(i), id); err != nil {
			return err
		}
	}
	m.plan.Cards.Moved = len(ids)
	return nil
}

// config lists SRC's overrides that DST does not share. DST's stay; SRC's go
// with SRC.
func (m *merger) config() error {
	return m.tx.Select(&m.plan.ConfigDropped,
		`SELECT s.key, s.value AS src, COALESCE(d.value, '') AS dst
		 FROM project_config s LEFT JOIN project_config d ON d.project_id = ? AND d.key = s.key
		 WHERE s.project_id = ? AND (d.value IS NULL OR d.value <> s.value)
		 ORDER BY s.key`, m.dst.ID, m.src.ID)
}

// pins plans the rewrite of every pin naming SRC, so that a directory keeps
// opening the board it opened before.
func (m *merger) pins() error {
	m.plan.Pins.ScanRoot = m.opts.ScanRoot
	for _, p := range m.opts.Pins {
		if p.Target.Project != m.src.Key {
			continue
		}
		slug := m.srcDefaultSlug
		if b := p.Target.Board(); b != "" {
			slug = m.boardSlug[b]
		}
		if slug == "" {
			m.plan.Pins.Left = append(m.plan.Pins.Left, p.Path)
			continue
		}
		m.plan.Pins.Rewrite = append(m.plan.Pins.Rewrite, PinRewrite{
			Path: p.Path, From: p.Target.String(), To: vpath.BoardPath(m.dst.Key, slug).String(),
		})
	}
	m.plan.Pins.Left = append(m.plan.Pins.Left, m.opts.UnreadablePins...)
	return nil
}

// retire removes SRC and reserves its key, re-pointing the reservations of
// earlier merges into SRC so every chain ends at the survivor.
func (m *merger) retire() error {
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE merged_project SET into_id = ? WHERE into_id = ?`, []any{m.dst.ID, m.src.ID}},
		{`INSERT INTO merged_project (key, into_id, merged_at) VALUES (?, ?, ?)`,
			[]any{m.src.Key, m.dst.ID, m.c.clock.NowMS()}},
		{`DELETE FROM project WHERE id = ?`, []any{m.src.ID}},
	} {
		if _, err := m.tx.Exec(q.sql, q.args...); err != nil {
			return err
		}
	}
	if err := m.c.rebuildKnowledgeFTS(m.tx); err != nil {
		return err
	}
	if err := m.c.recordEvent(m.tx, "project", m.dst.ID, "merged", "project", m.src.Key, m.dst.Key); err != nil {
		return err
	}
	return m.c.recordEvent(m.tx, "project", m.src.ID, "merged_into", "project", m.src.Key, m.dst.Key)
}
```

- [ ] **Step 3: Run the tests to verify they pass**

Run: `gofmt -l . ; go test ./internal/core/ && go vet ./internal/core/`
Expected: `ok`. `gofmt` may realign the `MergePlan` tags; run `gofmt -w internal/core/merge.go` if it lists the file.

Run `staticcheck ./internal/core/` too. The fields `docPath`, `fromSrc`, `addr` and `renamed` are only initialized in this task, and Task 7 reads them. If staticcheck reports them anyway, note that in the task report and continue; Task 7 clears it.

- [ ] **Step 4: Commit**

```bash
git add internal/core/merge.go internal/core/merge_test.go
git commit -m "feat(core): merge one project's boards, cards and vocabulary into another

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Documents move, collapse, conflict, or are renamed, and references follow

**Files:**
- Create: `internal/core/merge_docs.go`
- Modify: `internal/core/merge.go`: the `merger` fields, the constructor in `runMerge`, `run`, and the guard in `load`
- Test: `internal/core/merge_docs_test.go`

**Interfaces:**
- Consumes:
  - `fileStage.move`, `fileStage.rewrite` (Task 3)
  - `RewriteWikilinks` (Task 4)
  - `merger` (Task 6)
  - `fileHash`, `freeName`, `Slugify`, `DocAddress`, `ParseReference`, `resolveDocRef`, `refreshFromFile`, `recordEvent` (existing and layer 2)
  - `vpath.SplitAnchor`
- Produces:
  - `type docRow`, `type docMove`
  - `(*merger).planDocs`, `moveDocs`, `collapseDoc`, `references`, `rewriteDoc`, `rewriteCardTargets` and `resolveStubs`

- [ ] **Step 1: Write the failing tests**

`internal/core/merge_docs_test.go`:

```go
package core

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func (f *mergeFixture) doc(p Project, title, body string) Knowledge {
	f.t.Helper()
	d, err := f.c.CreateKnowledge(f.t.Context(), p.ID, NewKnowledge{Title: title, Body: body})
	if err != nil {
		f.t.Fatal(err)
	}
	return d
}

func backlinkRefs(t *testing.T, c *Core, docID string) []string {
	t.Helper()
	links, err := c.Backlinks(t.Context(), docID)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, l := range links {
		refs = append(refs, l.Ref)
	}
	return refs
}

func TestMergeMovesDocumentsAndRewritesAddresses(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	core, _ := f.project("CORE")
	runbook := f.doc(f.api, "Runbook", "Roll back with care.\n\n## Steps\n\nDo it.\n")
	f.doc(f.api, "Index", "See [[runbook#steps]] and [[/API/knowledge/runbook]].\n")
	cite := f.doc(core, "Citations", "Read [[/API/knowledge/runbook#steps|the runbook]].\n\n`[[/API/knowledge/runbook]]`\n")
	f.doc(f.mono, "Overview", "Waits for [[runbook]].\n")
	card := f.card(f.api, f.apiBoard, "linked", nil, nil)
	if err := f.c.LinkCardToDoc(ctx, f.api.ID, ParseCardRef(card.Ref), "/API/knowledge/runbook#steps"); err != nil {
		t.Fatal(err)
	}

	plan := f.merge(MergeOptions{Apply: true})

	if plan.Knowledge.Moved != 2 {
		t.Errorf("moved = %d", plan.Knowledge.Moved)
	}
	rewritten := slices.Clone(plan.DocumentsRewritten)
	slices.Sort(rewritten)
	if !slices.Equal(rewritten, []string{"/CORE/knowledge/citations", "/MONO/knowledge/index"}) {
		t.Errorf("rewritten = %v", plan.DocumentsRewritten)
	}

	moved := filepath.Join(f.root, "projects", "MONO", "knowledge", "runbook.md")
	doc, err := f.c.ReadKnowledge(ctx, f.mono.ID, "runbook")
	if err != nil || doc.Path != moved || doc.Ref != "/MONO/knowledge/runbook" {
		t.Fatalf("runbook = %+v, %v", doc, err)
	}
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("moved file: %v", err)
	}

	text := readFile(t, cite.Path)
	if !strings.Contains(text, "[[/MONO/knowledge/runbook#steps|the runbook]]") ||
		!strings.Contains(text, "`[[/API/knowledge/runbook]]`") {
		t.Errorf("citations file:\n%s", text)
	}
	index, err := f.c.ReadKnowledge(ctx, f.mono.ID, "index")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(index.BodyMD, "[[runbook#steps]]") || !strings.Contains(index.BodyMD, "[[/MONO/knowledge/runbook]]") {
		t.Errorf("index body:\n%s", index.BodyMD)
	}

	refs := backlinkRefs(t, f.c, runbook.ID)
	for _, want := range []string{"/CORE/knowledge/citations", "/MONO/knowledge/index", "/MONO/knowledge/overview", "API-1"} {
		if !slices.Contains(refs, want) {
			t.Errorf("backlinks %v lack %s", refs, want)
		}
	}
	if n := f.count(`SELECT count(*) FROM link WHERE from_type = 'card' AND to_raw = '/MONO/knowledge/runbook#steps'`); n != 1 {
		t.Errorf("card link targets rewritten: %d", n)
	}
}

func TestMergeCollapsesAnIdenticalDocument(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	a := f.doc(f.api, "Shared", "Identical body.\n")
	m := f.doc(f.mono, "Shared", "Different for now.\n")
	if err := os.WriteFile(m.Path, []byte(readFile(t, a.Path)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.ReadKnowledge(ctx, f.mono.ID, "shared"); err != nil {
		t.Fatal(err)
	}
	f.doc(f.api, "Citer", "See [[shared]].\n")

	plan := f.merge(MergeOptions{Apply: true})

	if !slices.Equal(plan.Knowledge.Collapsed, []string{"shared"}) || plan.Knowledge.Moved != 1 {
		t.Errorf("knowledge = %+v", plan.Knowledge)
	}
	if n := f.count(`SELECT count(*) FROM knowledge WHERE slug = 'shared'`); n != 1 {
		t.Errorf("%d shared entries remain", n)
	}
	if refs := backlinkRefs(t, f.c, m.ID); !slices.Contains(refs, "/MONO/knowledge/citer") {
		t.Errorf("backlinks of the survivor = %v", refs)
	}
}

func TestMergeStopsOnADifferingDocument(t *testing.T) {
	f := newMergeFixture(t)
	f.doc(f.api, "Runbook", "API's way.\n")
	f.doc(f.mono, "Runbook", "MONO's way.\n")
	before := f.snapshot()

	plan := f.merge(MergeOptions{})
	if plan.Ready || len(plan.Knowledge.Conflicts) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	got := plan.Knowledge.Conflicts[0]
	if got.Name != "runbook" || got.SrcHash == got.DstHash || got.Vault {
		t.Errorf("conflict = %+v", got)
	}
	_, err := f.c.MergeProjects(t.Context(), "API", "MONO", MergeOptions{Apply: true})
	if code := errCode(t, err); code != "merge_conflicts" || !strings.Contains(err.Error(), "--rename-conflicts") {
		t.Errorf("apply: %v", err)
	}
	if after := f.snapshot(); after != before {
		t.Errorf("a refused merge changed something:\n%s\n%s", before, after)
	}
}

func TestMergeRenamesConflictsOnRequest(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	core, _ := f.project("CORE")
	f.doc(f.api, "Runbook", "API's way.\n")
	f.doc(f.mono, "Runbook", "MONO's way.\n")
	f.doc(f.api, "Index", "[[runbook]] and [[/API/knowledge/runbook#x]]\n")
	home := f.doc(f.mono, "Home", "[[runbook]]\n")
	elsewhere := f.doc(core, "Elsewhere", "[[/API/knowledge/runbook]]\n")

	plan := f.merge(MergeOptions{Apply: true, RenameConflicts: true})

	if !slices.Equal(plan.Knowledge.Renamed, []Rename{{From: "runbook", To: "runbook-api"}}) {
		t.Errorf("renamed = %+v", plan.Knowledge.Renamed)
	}
	index := readFile(t, filepath.Join(f.root, "projects", "MONO", "knowledge", "index.md"))
	if !strings.Contains(index, "[[runbook-api]] and [[/MONO/knowledge/runbook-api#x]]") {
		t.Errorf("index:\n%s", index)
	}
	if got := readFile(t, home.Path); !strings.Contains(got, "[[runbook]]") || strings.Contains(got, "runbook-api") {
		t.Errorf("MONO's own link changed:\n%s", got)
	}
	if got := readFile(t, elsewhere.Path); !strings.Contains(got, "[[/MONO/knowledge/runbook-api]]") {
		t.Errorf("elsewhere:\n%s", got)
	}
	renamed, err := f.c.ReadKnowledge(ctx, f.mono.ID, "runbook-api")
	if err != nil || !strings.Contains(renamed.BodyMD, "API's way.") {
		t.Errorf("runbook-api = %+v, %v", renamed, err)
	}
}

func TestMergeNeverRenamesAVaultEntry(t *testing.T) {
	f := newMergeFixture(t)
	f.doc(f.api, "Conventions", "API's.\n")
	if _, err := f.c.EscalateKnowledge(t.Context(), f.api.ID, "conventions", "shared"); err != nil {
		t.Fatal(err)
	}
	f.doc(f.mono, "Conventions", "MONO's.\n")

	plan := f.merge(MergeOptions{RenameConflicts: true})
	if plan.Ready || len(plan.Knowledge.Conflicts) != 1 || !plan.Knowledge.Conflicts[0].Vault {
		t.Fatalf("plan = %+v", plan.Knowledge)
	}
	_, err := f.c.MergeProjects(t.Context(), "API", "MONO", MergeOptions{Apply: true, RenameConflicts: true})
	if code := errCode(t, err); code != "merge_conflicts" || !strings.Contains(err.Error(), "demote") {
		t.Errorf("apply: %v", err)
	}
}

func TestMergeMovesAVaultEntryItOwns(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	f.doc(f.api, "Conventions", "Shared.\n")
	escalated, err := f.c.EscalateKnowledge(ctx, f.api.ID, "conventions", "shared")
	if err != nil {
		t.Fatal(err)
	}
	var vaultPath string
	if err := f.c.db.Get(&vaultPath, `SELECT path FROM knowledge WHERE id = ?`, escalated.ID); err != nil {
		t.Fatal(err)
	}

	f.merge(MergeOptions{Apply: true})

	var row struct {
		ProjectID string `db:"project_id"`
		Global    bool   `db:"global"`
		Path      string `db:"path"`
	}
	if err := f.c.db.Get(&row, `SELECT project_id, global, path FROM knowledge WHERE id = ?`, escalated.ID); err != nil {
		t.Fatal(err)
	}
	if row.ProjectID != f.mono.ID || !row.Global || row.Path != vaultPath {
		t.Errorf("vault row = %+v", row)
	}
	got, err := f.c.ReadKnowledge(ctx, "", "/GLOBAL/knowledge/conventions")
	if err != nil || got.Ref != "/GLOBAL/knowledge/conventions" {
		t.Errorf("vault entry = %+v, %v", got, err)
	}
}

func TestMergeDocumentPlanMatchesApply(t *testing.T) {
	f := newMergeFixture(t)
	f.doc(f.api, "Runbook", "x\n")
	f.doc(f.api, "Index", "[[/API/knowledge/runbook]]\n")
	plan := f.merge(MergeOptions{})
	applied := f.merge(MergeOptions{Apply: true})
	applied.Backup, applied.Warnings = "", nil
	if !reflect.DeepEqual(plan, applied) {
		t.Errorf("plan and apply differ:\nplan    %+v\napplied %+v", plan, applied)
	}
	backup := filepath.Join(f.root, "backups")
	entries, err := os.ReadDir(backup)
	if err != nil || len(entries) != 1 {
		t.Fatalf("backups: %v, %v", entries, err)
	}
	copied := filepath.Join(backup, entries[0].Name(), "files", "projects", "API", "knowledge", "runbook.md")
	if _, err := os.Stat(copied); err != nil {
		t.Errorf("the backup lacks the moved file: %v", err)
	}
}

// A failure after files have moved puts every file back and changes nothing.
func TestMergeFailureRestoresFiles(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not deny writes here")
	}
	f := newMergeFixture(t)
	core, _ := f.project("CORE")
	f.doc(f.api, "Runbook", "x\n")
	cite := f.doc(core, "Citations", "[[/API/knowledge/runbook]]\n")
	before := f.snapshot()

	// The rewrite of CORE's document happens after API's files moved; a
	// directory that refuses new files makes it fail there.
	dir := filepath.Dir(cite.Path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	_, err := f.c.MergeProjects(t.Context(), "API", "MONO", MergeOptions{Apply: true})
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err == nil {
		t.Fatal("the merge succeeded")
	}
	if after := f.snapshot(); after != before {
		t.Errorf("a failed merge left changes:\nbefore %s\nafter  %s", before, after)
	}
}

// An edit made on disk and not yet read back still has its links rewritten:
// the files, not the link rows, say who cites SRC.
func TestMergeRewritesLinksTheDatabaseHasNotSeen(t *testing.T) {
	f := newMergeFixture(t)
	core, _ := f.project("CORE")
	f.doc(f.api, "Runbook", "x\n")
	notes := f.doc(core, "Notes", "Nothing yet.\n")
	edited := readFile(t, notes.Path) + "\nSee [[/API/knowledge/runbook]].\n"
	if err := os.WriteFile(notes.Path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := f.merge(MergeOptions{Apply: true})

	if !slices.Contains(plan.DocumentsRewritten, "/CORE/knowledge/notes") {
		t.Errorf("rewritten = %v", plan.DocumentsRewritten)
	}
	if got := readFile(t, notes.Path); !strings.Contains(got, "[[/MONO/knowledge/runbook]]") {
		t.Errorf("notes:\n%s", got)
	}
}

func TestMergeKeepsBothReasonsOfOneActorsNominations(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	a := f.doc(f.api, "Shared", "Identical body.\n")
	m := f.doc(f.mono, "Shared", "Different for now.\n")
	if err := os.WriteFile(m.Path, []byte(readFile(t, a.Path)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.ReadKnowledge(ctx, f.mono.ID, "shared"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.NominateKnowledge(ctx, f.api.ID, "shared", "api reason"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.NominateKnowledge(ctx, f.mono.ID, "shared", "mono reason"); err != nil {
		t.Fatal(err)
	}

	f.merge(MergeOptions{Apply: true})

	var reasons []string
	if err := f.c.db.Select(&reasons, `SELECT reason FROM nomination WHERE knowledge_id = ?`, m.ID); err != nil {
		t.Fatal(err)
	}
	if len(reasons) != 1 || !strings.Contains(reasons[0], "mono reason") || !strings.Contains(reasons[0], "api reason") {
		t.Errorf("nominations = %q", reasons)
	}
}

func TestMergePlanSeesAnUntrackedFileInTheWay(t *testing.T) {
	f := newMergeFixture(t)
	f.doc(f.api, "Runbook", "x\n")
	writeFile(t, filepath.Join(f.root, "projects", "MONO", "knowledge", "runbook.md"), "not tracked")

	plan := f.merge(MergeOptions{})

	if plan.Ready || len(plan.Knowledge.Conflicts) != 1 ||
		!strings.Contains(plan.Knowledge.Conflicts[0].Reason, "no entry") {
		t.Errorf("plan = %+v", plan.Knowledge)
	}
}

// The backup is taken before the apply's transaction. A file changed in
// between stops the apply; it never escapes the backup.
func TestMergeStopsWhenAFileChangesAfterTheBackup(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	doc := f.doc(f.api, "Runbook", "x\n")
	f.c.SetDropDerived(func(context.Context, string) error {
		return os.WriteFile(doc.Path, []byte("edited meanwhile\n"), 0o600)
	})

	_, err := f.c.MergeProjects(ctx, "API", "MONO", MergeOptions{Apply: true})

	if got := errCode(t, err); got != "merge_changed" {
		t.Fatalf("code = %s", got)
	}
	if _, err := f.c.ProjectByKey(ctx, "API"); err != nil {
		t.Errorf("API was merged anyway: %v", err)
	}
	if got := readFile(t, doc.Path); got != "edited meanwhile\n" {
		t.Errorf("the edited file = %q", got)
	}
}
```

Run: `go test ./internal/core/ -run 'MergeMoves|MergeCollapses|MergeStops|MergeRenames|MergeNever|MergeDocument|MergeFailure'`
Expected: FAIL. Every test gets `merge_refused`, "merging knowledge and artifacts is not implemented yet", or a build error for `planDocs`.

- [ ] **Step 2: Wire documents into the engine**

In `internal/core/merge.go`:

1. Add these fields to `merger`, after `renamed`:

```go
	origPath map[string]string // SRC document id -> its file path before the merge
	docMoves []docMove
```

2. In `runMerge`'s `merger` literal, add `origPath: map[string]string{},`.

3. Replace `run` with:

```go
func (m *merger) run(srcKey, dstKey string) error {
	refused, err := m.load(srcKey, dstKey)
	if err != nil {
		return err
	}
	if refused {
		if m.apply {
			return mergeNotReady(*m.plan)
		}
		return nil
	}
	// Every conflict is known before anything changes.
	if err := m.planDocs(); err != nil {
		return err
	}
	m.plan.Ready = len(m.plan.Knowledge.Conflicts) == 0
	if m.apply && !m.plan.Ready {
		return mergeNotReady(*m.plan)
	}
	for _, step := range []func() error{
		m.boards, m.labels, m.tags, m.cards, m.moveDocs, m.references, m.config, m.pins, m.retire,
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}
```

4. In `load`, the guard now covers artifacts only:

```go
	// Until artifacts can move, a project that owns any is refused rather
	// than half-merged.
	owned, err := m.count(`SELECT COUNT(*) FROM artifact WHERE project_id = ?`, m.src.ID)
```

and the matching case message becomes `"merging artifacts is not implemented yet"`.

- [ ] **Step 3: Write `internal/core/merge_docs.go`**

```go
package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"unicode/utf8"

	"github.com/mtch3n/trellis/internal/vpath"
)

type docRow struct {
	ID     string `db:"id"`
	Slug   string `db:"slug"`
	Path   string `db:"path"`
	Global bool   `db:"global"`
}

// docMove is what happens to one SRC document: it moves under slug, or, when
// into is set, it collapses into DST's identical entry.
type docMove struct {
	doc        docRow
	slug       string
	into       string
	intoGlobal bool
}

// planDocs decides every SRC document's fate before anything changes.
// Identical bytes under one slug collapse; different content is a conflict,
// renamed only on request and never for a vault entry.
func (m *merger) planDocs() error {
	var src, dst []docRow
	if err := m.tx.Select(&src,
		`SELECT id, slug, path, global FROM knowledge WHERE project_id = ? ORDER BY slug`, m.src.ID); err != nil {
		return err
	}
	if err := m.tx.Select(&dst,
		`SELECT id, slug, path, global FROM knowledge WHERE project_id = ?`, m.dst.ID); err != nil {
		return err
	}
	bySlug, taken := map[string]docRow{}, map[string]bool{}
	for _, d := range dst {
		bySlug[d.Slug], taken[d.Slug] = d, true
	}
	for _, s := range src {
		taken[s.Slug] = true
	}
	out := &m.plan.Knowledge
	dir := filepath.Join(m.root, "projects", m.dst.Key, "knowledge")
	// movable reports whether s can move under slug, recording a conflict
	// when its file is gone or an untracked file already holds the
	// destination. The plan must see what the apply would trip over.
	movable := func(s docRow, slug string) bool {
		if _, err := os.Lstat(s.Path); err != nil {
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Slug, Reason: "its file is missing: " + s.Path})
			return false
		}
		if s.Global {
			return true
		}
		dest := filepath.Join(dir, slug+".md")
		if _, err := os.Lstat(dest); err == nil {
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Slug, Reason: "a file with no entry is already at " + dest})
			return false
		}
		return true
	}
	for _, s := range src {
		m.docPath[s.ID], m.origPath[s.ID], m.fromSrc[s.ID] = s.Path, s.Path, true
		d, clash := bySlug[s.Slug]
		if !clash {
			if movable(s, s.Slug) {
				m.docMoves = append(m.docMoves, docMove{doc: s, slug: s.Slug})
			}
			continue
		}
		srcHash, err := fileHash(s.Path)
		if err != nil {
			return err
		}
		dstHash, err := fileHash(d.Path)
		if err != nil {
			return err
		}
		switch {
		case srcHash == dstHash && !s.Global:
			m.docMoves = append(m.docMoves, docMove{doc: s, slug: s.Slug, into: d.ID, intoGlobal: d.Global})
			out.Collapsed = append(out.Collapsed, s.Slug)
		case s.Global || !m.opts.RenameConflicts:
			out.Conflicts = append(out.Conflicts,
				MergeConflict{Name: s.Slug, SrcHash: srcHash, DstHash: dstHash, Vault: s.Global})
		default:
			slug := freeName(s.Slug+"-"+Slugify(m.src.Key), taken)
			taken[slug] = true
			if movable(s, slug) {
				m.docMoves = append(m.docMoves, docMove{doc: s, slug: slug})
				out.Renamed = append(out.Renamed, Rename{From: s.Slug, To: slug})
			}
		}
	}
	return nil
}

// moveDocs carries out planDocs. A project entry's file moves into DST's
// vault directory; a vault entry stays where it is and only changes origin.
func (m *merger) moveDocs() error {
	dir := filepath.Join(m.root, "projects", m.dst.Key, "knowledge")
	for _, mv := range m.docMoves {
		d := mv.doc
		old := DocAddress(m.src.Key, d.Global, d.Slug)
		if mv.into != "" {
			if err := m.collapseDoc(d, mv.into); err != nil {
				return err
			}
			m.addr[old] = DocAddress(m.dst.Key, mv.intoGlobal, mv.slug)
			continue
		}
		path := d.Path
		if !d.Global {
			path = filepath.Join(dir, mv.slug+".md")
		}
		if _, err := m.tx.Exec(`UPDATE knowledge SET project_id = ?, slug = ?, path = ? WHERE id = ?`,
			m.dst.ID, mv.slug, path, d.ID); err != nil {
			return err
		}
		if path != d.Path {
			if err := m.touch(d.Path); err != nil {
				return err
			}
			if m.apply {
				if err := m.stage.move(d.Path, path); err != nil {
					return err
				}
				m.docPath[d.ID] = path
			}
		}
		if !d.Global {
			m.addr[old] = DocAddress(m.dst.Key, false, mv.slug)
		}
		if mv.slug != d.Slug {
			m.renamed[d.Slug] = mv.slug
		}
		m.plan.Knowledge.Moved++
	}
	return nil
}

// collapseDoc folds a SRC entry into DST's identical one: whatever pointed at
// SRC's row now points at DST's. SRC's file stays in SRC's directory, which
// is kept with the backup after the commit.
func (m *merger) collapseDoc(d docRow, into string) error {
	// One actor's two nominations cannot both survive the unique key: keep
	// DST's row and append SRC's reason to it, so no evidence is lost.
	if _, err := m.tx.Exec(
		`UPDATE nomination AS dn
		 SET reason = dn.reason || char(10) || sn.reason, created_at = min(dn.created_at, sn.created_at)
		 FROM nomination AS sn
		 WHERE sn.knowledge_id = ? AND dn.knowledge_id = ? AND dn.actor = sn.actor AND dn.reason <> sn.reason`,
		d.ID, into); err != nil {
		return err
	}
	for _, q := range []string{
		`UPDATE link SET to_id = ? WHERE to_type = 'doc' AND to_id = ?`,
		`UPDATE OR IGNORE pin SET knowledge_id = ? WHERE knowledge_id = ?`,
		`UPDATE OR IGNORE nomination SET knowledge_id = ? WHERE knowledge_id = ?`,
	} {
		if _, err := m.tx.Exec(q, into, d.ID); err != nil {
			return err
		}
	}
	if _, err := m.tx.Exec(`DELETE FROM link WHERE from_type = 'doc' AND from_id = ?`, d.ID); err != nil {
		return err
	}
	if _, err := m.tx.Exec(`DELETE FROM knowledge WHERE id = ?`, d.ID); err != nil {
		return err
	}
	delete(m.fromSrc, d.ID)
	return m.c.recordEvent(m.tx, "knowledge", d.ID, "collapsed", "into", "", into)
}

// references rewrites every link that named a SRC document by address, in
// any project, and every relative link from a SRC document to one that was
// renamed. Then it resolves the stubs the merge satisfied.
func (m *merger) references() error {
	if len(m.addr) > 0 {
		prefix := "/" + m.src.Key + "/"
		ids, err := m.docsCiting(m.src.Key)
		if err != nil {
			return err
		}
		if len(m.renamed) > 0 {
			for id := range m.fromSrc {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range slices.Compact(ids) {
			if err := m.rewriteDoc(id); err != nil {
				return err
			}
		}
		if err := m.rewriteCardTargets(prefix); err != nil {
			return err
		}
	}
	return m.resolveStubs()
}

// docsCiting lists every document whose file, as it is on disk now, holds a
// wikilink into project key. The files are the source of truth; link rows lag
// behind an edit made outside Trellis until that document is next read.
func (m *merger) docsCiting(key string) ([]string, error) {
	var docs []struct {
		ID   string `db:"id"`
		Path string `db:"path"`
	}
	if err := m.tx.Select(&docs, `SELECT id, path FROM knowledge ORDER BY id`); err != nil {
		return nil, err
	}
	var ids []string
	for _, d := range docs {
		path := d.Path
		if p, ok := m.docPath[d.ID]; ok {
			path = p
		}
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			m.plan.Warnings = append(m.plan.Warnings, "not searched for links: "+path+" is missing")
			continue
		}
		if err != nil {
			return nil, err
		}
		_, body, _ := SplitFrontmatter(string(raw))
		if slices.ContainsFunc(ParseWikilinks(body), func(r Reference) bool { return r.ProjectKey == key }) {
			ids = append(ids, d.ID)
		}
	}
	return ids, nil
}

// rewriteDoc rewrites one document's links through RewriteWikilinks, then
// reloads its row so the link table follows the new text.
func (m *merger) rewriteDoc(id string) error {
	var d struct {
		Key    string `db:"key"`
		Slug   string `db:"slug"`
		Path   string `db:"path"`
		Global bool   `db:"global"`
	}
	if err := m.tx.Get(&d,
		`SELECT p.key, k.slug, k.path, k.global FROM knowledge k JOIN project p ON p.id = k.project_id
		 WHERE k.id = ?`, id); err != nil {
		return err
	}
	current, original := d.Path, d.Path
	if p, ok := m.docPath[id]; ok {
		current, original = p, m.origPath[id]
	}
	raw, err := os.ReadFile(current)
	if err != nil {
		return err
	}
	fromSrc := m.fromSrc[id]
	text := RewriteWikilinks(string(raw), func(ref Reference) (string, bool) {
		_, anchor := vpath.SplitAnchor(ref.Raw)
		if anchor != "" {
			anchor = "#" + anchor
		}
		switch {
		case ref.ProjectKey == m.src.Key:
			to, ok := m.addr[DocAddress(m.src.Key, false, ref.Slug)]
			return to + anchor, ok
		case ref.ProjectKey == "" && fromSrc:
			to, ok := m.renamed[ref.Slug]
			return to + anchor, ok
		}
		return "", false
	})
	if text == string(raw) {
		return nil
	}
	m.plan.DocumentsRewritten = append(m.plan.DocumentsRewritten, DocAddress(d.Key, d.Global, d.Slug))
	if err := m.touch(original); err != nil {
		return err
	}
	if !m.apply {
		return nil
	}
	if err := m.stage.rewrite(current, []byte(text)); err != nil {
		return err
	}
	var doc Knowledge
	if err := m.tx.Get(&doc, `SELECT * FROM knowledge WHERE id = ?`, id); err != nil {
		return err
	}
	return m.c.refreshFromFile(m.tx, &doc)
}

// rewriteCardTargets updates `trellis link` targets that named a SRC document
// by address. Their text is all the link keeps of what was typed.
func (m *merger) rewriteCardTargets(prefix string) error {
	var links []struct {
		FromID string `db:"from_id"`
		ToRaw  string `db:"to_raw"`
	}
	if err := m.tx.Select(&links,
		`SELECT from_id, to_raw FROM link
		 WHERE from_type = 'card' AND rel = 'documents' AND upper(substr(to_raw, 1, ?)) = ?`,
		utf8.RuneCountInString(prefix), prefix); err != nil {
		return err
	}
	for _, l := range links {
		ref := ParseReference(l.ToRaw)
		to, ok := m.addr[DocAddress(m.src.Key, false, ref.Slug)]
		if ref.ProjectKey != m.src.Key || !ok {
			continue
		}
		if _, anchor := vpath.SplitAnchor(l.ToRaw); anchor != "" {
			to += "#" + anchor
		}
		if _, err := m.tx.Exec(
			`UPDATE OR IGNORE link SET to_raw = ?
			 WHERE from_type = 'card' AND from_id = ? AND rel = 'documents' AND to_raw = ?`,
			to, l.FromID, l.ToRaw); err != nil {
			return err
		}
	}
	return nil
}

// resolveStubs points dangling links at whatever they name now. SRC's entries
// arrived under DST, so a link from DST that waited for one of them resolves,
// and so does an address to DST written anywhere before its entry arrived.
func (m *merger) resolveStubs() error {
	prefix := "/" + m.dst.Key + "/"
	n := utf8.RuneCountInString(prefix)
	var stubs []struct {
		FromType  string `db:"from_type"`
		FromID    string `db:"from_id"`
		ToRaw     string `db:"to_raw"`
		ProjectID string `db:"project_id"`
	}
	if err := m.tx.Select(&stubs,
		`SELECT l.from_type, l.from_id, l.to_raw, k.project_id
		 FROM link l JOIN knowledge k ON k.id = l.from_id
		 WHERE l.from_type = 'doc' AND l.to_type = 'doc' AND l.to_id IS NULL
		   AND (k.project_id = ? OR upper(substr(l.to_raw, 1, ?)) = ?)
		 UNION ALL
		 SELECT l.from_type, l.from_id, l.to_raw, cd.project_id
		 FROM link l JOIN card cd ON cd.id = l.from_id
		 WHERE l.from_type = 'card' AND l.to_type = 'doc' AND l.to_id IS NULL
		   AND (cd.project_id = ? OR upper(substr(l.to_raw, 1, ?)) = ?)`,
		m.dst.ID, n, prefix, m.dst.ID, n, prefix); err != nil {
		return err
	}
	for _, s := range stubs {
		toID, err := m.c.resolveDocRef(m.tx, s.ProjectID, ParseReference(s.ToRaw))
		if err != nil {
			return err
		}
		id, ok := toID.(string)
		if !ok {
			continue
		}
		if _, err := m.tx.Exec(
			`UPDATE link SET to_id = ?
			 WHERE from_type = ? AND from_id = ? AND to_type = 'doc' AND to_raw = ? AND to_id IS NULL`,
			id, s.FromType, s.FromID, s.ToRaw); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l . ; go test ./internal/core/ && go vet ./internal/core/ && staticcheck ./internal/core/`
Expected: `ok`, with nothing from `gofmt` or `staticcheck`.

If `TestMergeCollapsesAnIdenticalDocument` finds `Moved` = 2, the refresh in the test did not copy the bytes. Check that `ReadKnowledge` re-read MONO's file before debugging the merge.

- [ ] **Step 5: Commit**

```bash
git add internal/core/merge.go internal/core/merge_docs.go internal/core/merge_docs_test.go
git commit -m "feat(core): merge documents, and rewrite the addresses that named them

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Artifacts move, collapse, conflict, or are renamed

**Files:**
- Create: `internal/core/merge_artifacts.go`
- Modify: `internal/core/merge.go`: the `merger` field, `run`, and removing the guard from `load`
- Test: `internal/core/merge_artifacts_test.go`

**Interfaces:**
- Consumes:
  - `merger`, `fileStage.move`, `freeName`, `fileHash`, `Slugify`
  - `ResolveArtifact`, `ListArtifacts`, `LinkArtifactToCard`, `CreateArtifact` (existing and layer 2)
  - `writeFile`, `readFile` (Task 3 tests)
- Produces:
  - `type artifactRow`, `type artifactMove`
  - `func freeArtifactName(name, suffix string, taken map[string]bool) string`
  - `(*merger).planArtifacts`, `moveArtifacts`

- [ ] **Step 1: Write the failing tests**

`internal/core/merge_artifacts_test.go`:

```go
package core

import (
	"path/filepath"
	"slices"
	"testing"
)

func (f *mergeFixture) artifact(p Project, name, content string) Artifact {
	f.t.Helper()
	src := filepath.Join(f.t.TempDir(), name)
	writeFile(f.t, src, content)
	a, err := f.c.CreateArtifact(f.t.Context(), p.ID, src)
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}

func TestMergeMovesAndCollapsesArtifacts(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	card := f.card(f.api, f.apiBoard, "has files", nil, nil)
	shot := f.artifact(f.api, "shot.png", "api pixels")
	logo := f.artifact(f.api, "logo.png", "same logo")
	monoLogo := f.artifact(f.mono, "logo.png", "same logo")
	for _, a := range []Artifact{shot, logo} {
		if err := f.c.LinkArtifactToCard(ctx, f.api.ID, card.ID, a.ID); err != nil {
			t.Fatal(err)
		}
	}

	plan := f.merge(MergeOptions{Apply: true})

	if plan.Artifacts.Moved != 1 || !slices.Equal(plan.Artifacts.Collapsed, []string{"logo.png"}) {
		t.Errorf("artifacts = %+v", plan.Artifacts)
	}
	got, err := f.c.ResolveArtifact(ctx, f.mono.ID, "shot.png")
	if err != nil || got.ID != shot.ID || got.Ref != "/MONO/artifacts/shot.png" ||
		got.Path != filepath.Join(f.root, "projects", "MONO", "artifacts", "shot.png") {
		t.Fatalf("shot.png = %+v, %v", got, err)
	}
	if readFile(t, got.Path) != "api pixels" {
		t.Error("the moved file lost its content")
	}
	items, err := f.c.ListArtifacts(ctx, f.mono.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range items {
		ids = append(ids, a.ID)
	}
	slices.Sort(ids)
	want := []string{shot.ID, monoLogo.ID}
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Errorf("the card's artifacts = %v, want %v", ids, want)
	}
	if n := f.count(`SELECT count(*) FROM artifact WHERE id = ?`, logo.ID); n != 0 {
		t.Error("the collapsed artifact's row survived")
	}
}

func TestMergeArtifactConflicts(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	f.artifact(f.api, "shot.png", "api")
	f.artifact(f.mono, "shot.png", "mono")

	plan := f.merge(MergeOptions{})
	if plan.Ready || len(plan.Artifacts.Conflicts) != 1 || plan.Artifacts.Conflicts[0].Name != "shot.png" ||
		plan.Artifacts.Conflicts[0].Vault {
		t.Fatalf("plan = %+v", plan.Artifacts)
	}

	plan = f.merge(MergeOptions{Apply: true, RenameConflicts: true})
	if !slices.Equal(plan.Artifacts.Renamed, []Rename{{From: "shot.png", To: "shot-api.png"}}) {
		t.Errorf("renamed = %+v", plan.Artifacts.Renamed)
	}
	for name, content := range map[string]string{"shot-api.png": "api", "shot.png": "mono"} {
		a, err := f.c.ResolveArtifact(ctx, f.mono.ID, name)
		if err != nil || readFile(t, a.Path) != content {
			t.Errorf("%s = %+v, %v", name, a, err)
		}
	}
}

func TestFreeArtifactNameKeepsTheExtension(t *testing.T) {
	taken := map[string]bool{"shot-api.png": true}
	if got := freeArtifactName("shot.png", "api", taken); got != "shot-api-2.png" {
		t.Errorf("got %s", got)
	}
	if got := freeArtifactName("README", "api", nil); got != "README-api" {
		t.Errorf("got %s", got)
	}
}
```

Run: `go test ./internal/core/ -run 'Artifacts|ArtifactConflicts|FreeArtifactName'`
Expected: FAIL. The build fails because `freeArtifactName` is undefined.

- [ ] **Step 2: Write `internal/core/merge_artifacts.go`**

```go
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type artifactRow struct {
	ID   string `db:"id"`
	Name string `db:"name"`
	Path string `db:"path"`
}

// artifactMove is what happens to one SRC artifact: it moves under name, or,
// when into is set, collapses into DST's artifact with the same bytes.
type artifactMove struct {
	row  artifactRow
	name string
	into string
}

// planArtifacts applies the document rules to artifacts, by file name.
// There is no vault for artifacts, so every conflict may be renamed.
func (m *merger) planArtifacts() error {
	var src, dst []artifactRow
	if err := m.tx.Select(&src,
		`SELECT id, name, path FROM artifact WHERE project_id = ? ORDER BY name`, m.src.ID); err != nil {
		return err
	}
	if err := m.tx.Select(&dst, `SELECT id, name, path FROM artifact WHERE project_id = ?`, m.dst.ID); err != nil {
		return err
	}
	byName, taken := map[string]artifactRow{}, map[string]bool{}
	for _, d := range dst {
		byName[d.Name], taken[d.Name] = d, true
	}
	for _, s := range src {
		taken[s.Name] = true
	}
	out := &m.plan.Artifacts
	dir := filepath.Join(m.root, "projects", m.dst.Key, "artifacts")
	movable := func(s artifactRow, name string) bool {
		if _, err := os.Lstat(s.Path); err != nil {
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Name, Reason: "its file is missing: " + s.Path})
			return false
		}
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			out.Conflicts = append(out.Conflicts,
				MergeConflict{Name: s.Name, Reason: "a file with no artifact is already at " + filepath.Join(dir, name)})
			return false
		}
		return true
	}
	for _, s := range src {
		d, clash := byName[s.Name]
		if !clash {
			if movable(s, s.Name) {
				m.artMoves = append(m.artMoves, artifactMove{row: s, name: s.Name})
			}
			continue
		}
		srcHash, err := fileHash(s.Path)
		if err != nil {
			return err
		}
		dstHash, err := fileHash(d.Path)
		if err != nil {
			return err
		}
		switch {
		case srcHash == dstHash:
			m.artMoves = append(m.artMoves, artifactMove{row: s, name: s.Name, into: d.ID})
			out.Collapsed = append(out.Collapsed, s.Name)
		case !m.opts.RenameConflicts:
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Name, SrcHash: srcHash, DstHash: dstHash})
		default:
			name := freeArtifactName(s.Name, Slugify(m.src.Key), taken)
			taken[name] = true
			if movable(s, name) {
				m.artMoves = append(m.artMoves, artifactMove{row: s, name: name})
				out.Renamed = append(out.Renamed, Rename{From: s.Name, To: name})
			}
		}
	}
	return nil
}

// freeArtifactName suffixes the stem and keeps the extension last:
// shot-api.png, then shot-api-2.png.
func freeArtifactName(name, suffix string, taken map[string]bool) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext) + "-" + suffix
	candidate := stem + ext
	for n := 2; taken[candidate]; n++ {
		candidate = fmt.Sprintf("%s-%d%s", stem, n, ext)
	}
	return candidate
}

// moveArtifacts carries out planArtifacts. A collapsed artifact's card links
// move to DST's copy; deleting its row fires artifact_links_ad, which removes
// any link that already pointed at both. Its file stays in SRC's directory,
// which is kept with the backup.
func (m *merger) moveArtifacts() error {
	dir := filepath.Join(m.root, "projects", m.dst.Key, "artifacts")
	for _, mv := range m.artMoves {
		a := mv.row
		if mv.into != "" {
			if _, err := m.tx.Exec(
				`UPDATE OR IGNORE link SET to_id = ?, to_raw = ? WHERE to_type = 'artifact' AND to_id = ?`,
				mv.into, mv.into, a.ID); err != nil {
				return err
			}
			if _, err := m.tx.Exec(`DELETE FROM artifact WHERE id = ?`, a.ID); err != nil {
				return err
			}
			continue
		}
		path := filepath.Join(dir, mv.name)
		if _, err := m.tx.Exec(`UPDATE artifact SET project_id = ?, name = ?, path = ? WHERE id = ?`,
			m.dst.ID, mv.name, path, a.ID); err != nil {
			return err
		}
		if err := m.touch(a.Path); err != nil {
			return err
		}
		if m.apply {
			if err := m.stage.move(a.Path, path); err != nil {
				return err
			}
		}
		m.plan.Artifacts.Moved++
	}
	return nil
}
```

- [ ] **Step 3: Wire artifacts into the engine**

In `internal/core/merge.go`:

1. Add the field `artMoves []artifactMove` to `merger`, after `docMoves`.

2. In `run`, replace the planning block and the step list:

```go
	// Every conflict is known before anything changes.
	for _, detect := range []func() error{m.planDocs, m.planArtifacts} {
		if err := detect(); err != nil {
			return err
		}
	}
	m.plan.Ready = len(m.plan.Knowledge.Conflicts) == 0 && len(m.plan.Artifacts.Conflicts) == 0
	if m.apply && !m.plan.Ready {
		return mergeNotReady(*m.plan)
	}
	for _, step := range []func() error{
		m.boards, m.labels, m.tags, m.cards, m.moveDocs, m.moveArtifacts,
		m.references, m.config, m.pins, m.retire,
	} {
```

3. In `load`, delete the guard entirely:
   - the comment starting "Until artifacts can move";
   - the `owned` query and its error check;
   - the `case owned > 0:` branch.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l . ; go test ./internal/core/ && go vet ./internal/core/ && staticcheck ./internal/core/`
Expected: `ok`, with nothing from `gofmt` or `staticcheck`.

- [ ] **Step 5: Commit**

```bash
git add internal/core/merge.go internal/core/merge_artifacts.go internal/core/merge_artifacts_test.go
git commit -m "feat(core): merge artifacts, collapsing identical files

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: After the commit: leftovers, pins, derived state

**Files:**
- Modify: `internal/resolve/pin.go`: new `ScanRoot` and `PinsUnder`
- Test: `internal/resolve/pin_test.go` (append)
- Modify: `internal/core/merge.go`: `afterMerge`
- Test: `internal/core/merge_test.go` (append)

**Interfaces:**
- Consumes:
  - `canonicalDir`, `homeDir`, `ReadPin`, `PinFile` (pin-only plan)
  - `isolateHome`, `mkdir`, `pinAt`, `normalizeDir` (existing resolve tests)
  - `writeAtomic`, `Core.knowledgeChanged`, `SetKnowledgeChanged`, `SetDropDerived`
- Produces:
  - `func resolve.ScanRoot(dir string) (string, error)`
  - `func resolve.PinsUnder(root string) (pins []Pin, skipped []string, err error)`

- [ ] **Step 1: Write the failing resolve tests**

Append to `internal/resolve/pin_test.go`, and add `"slices"` to its imports:

```go
func TestScanRootIsTheEnclosingRepository(t *testing.T) {
	isolateHome(t)
	repo := t.TempDir()
	mkdir(t, repo, ".git")
	if got, err := ScanRoot(mkdir(t, repo, "a", "b")); err != nil || got != normalizeDir(repo) {
		t.Errorf("inside a repository: %s, %v", got, err)
	}
	loose := t.TempDir()
	if got, err := ScanRoot(loose); err != nil || got != normalizeDir(loose) {
		t.Errorf("outside any repository: %s, %v", got, err)
	}
}

func TestPinsUnderSkipsGitAndNestedRepositories(t *testing.T) {
	isolateHome(t)
	repo := normalizeDir(t.TempDir())
	mkdir(t, repo, ".git")
	pinAt(t, repo, "/MONO\n")
	pinAt(t, mkdir(t, repo, "api"), "/API\n")
	pinAt(t, mkdir(t, repo, ".git", "hooks"), "/HIDDEN\n")
	nested := mkdir(t, repo, "vendor", "lib")
	mkdir(t, nested, ".git")
	pinAt(t, nested, "/LIB\n")
	pinAt(t, mkdir(t, repo, "old"), "OLD\n")

	pins, skipped, err := PinsUnder(repo)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, p := range pins {
		keys = append(keys, p.Target.Project)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"API", "MONO"}) {
		t.Errorf("pins = %v", keys)
	}
	if !slices.Equal(skipped, []string{filepath.Join(repo, "old", PinFile)}) {
		t.Errorf("skipped = %v", skipped)
	}
}
```

Run: `go test ./internal/resolve/ -run 'ScanRoot|PinsUnder'`
Expected: FAIL. `ScanRoot` is undefined.

- [ ] **Step 2: Write `ScanRoot` and `PinsUnder`**

Append to `internal/resolve/pin.go`:

```go
// ScanRoot is where a merge looks for pins to rewrite: the nearest ancestor
// of dir, dir included, that contains .git, within the walk's usual
// boundaries; dir itself when there is none.
func ScanRoot(dir string) (string, error) {
	start, err := canonicalDir(dir)
	if err != nil {
		return "", err
	}
	home := homeDir()
	for d := start; ; {
		parent := filepath.Dir(d)
		if d == home || parent == d {
			return start, nil
		}
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		d = parent
	}
}

// PinsUnder lists the pins beneath root. It skips .git directories and every
// nested directory holding its own .git: a nested repository has its own pins
// and its own commits. Symlinks are not followed. A pin that cannot be read
// or parsed, and a directory that cannot be listed, is reported in skipped
// rather than failing the scan.
func PinsUnder(root string) (pins []Pin, skipped []string, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			skipped = append(skipped, path+": "+walkErr.Error())
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			if path != root {
				if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if d.Name() != PinFile || !d.Type().IsRegular() {
			return nil
		}
		pin, err := ReadPin(path)
		if err != nil {
			skipped = append(skipped, path)
			return nil
		}
		pins = append(pins, pin)
		return nil
	})
	return pins, skipped, err
}
```

Run: `go test ./internal/resolve/`
Expected: `ok`.

- [ ] **Step 3: Write the failing core tests**

Append to `internal/core/merge_test.go`, and add `"context"` and `"errors"` to its imports:

```go
func TestMergeFinishesAfterTheCommit(t *testing.T) {
	f := newMergeFixture(t)
	f.doc(f.api, "Runbook", "x\n")
	repo := t.TempDir()
	pinPath := filepath.Join(repo, "api", ".trellis")
	writeFile(t, pinPath, "/API\n")
	var notified []string
	f.c.SetKnowledgeChanged(func(_ context.Context, id string) error {
		notified = append(notified, id)
		return nil
	})

	plan := f.merge(MergeOptions{Apply: true, ScanRoot: repo,
		Pins: []resolve.Pin{{Path: pinPath, Target: vpath.ProjectPath("API")}}})

	if got := readFile(t, pinPath); got != "/MONO/boards/api\n" {
		t.Errorf("pin = %q", got)
	}
	if _, err := os.Stat(filepath.Join(f.root, "projects", "API")); !os.IsNotExist(err) {
		t.Errorf("API's directory is still in the storage root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(plan.Backup, "leftover", "API")); err != nil {
		t.Errorf("API's leftovers were not kept with the backup: %v", err)
	}
	if !slices.Contains(notified, f.mono.ID) {
		t.Errorf("MONO's derived state was not refreshed: %v", notified)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("warnings = %v", plan.Warnings)
	}
}

func TestMergeReportsAFailedRefresh(t *testing.T) {
	f := newMergeFixture(t)
	f.c.SetKnowledgeChanged(func(context.Context, string) error { return errors.New("embedder down") })
	plan := f.merge(MergeOptions{Apply: true})
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "embedder down") {
		t.Errorf("warnings = %v", plan.Warnings)
	}
}

func TestMergeDropsDerivedStateBeforeApplying(t *testing.T) {
	f := newMergeFixture(t)
	var dropped []string
	f.c.SetDropDerived(func(_ context.Context, key string) error {
		if f.count(`SELECT count(*) FROM project WHERE key = ?`, key) != 1 {
			t.Errorf("%s was dropped after it was merged away", key)
		}
		dropped = append(dropped, key)
		return errors.New("vector tables busy")
	})

	plan := f.merge(MergeOptions{Apply: true})

	if !slices.Equal(dropped, []string{"API"}) {
		t.Errorf("dropped = %v", dropped)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "vector tables busy") {
		t.Errorf("warnings = %v", plan.Warnings)
	}
}
```

Run: `go test ./internal/core/ -run 'FinishesAfterTheCommit|DropsDerivedState'`
Expected: FAIL. The pin is unchanged, and API's directory is still in place.

- [ ] **Step 4: Finish the merge after the commit**

In `internal/core/merge.go`, replace `afterMerge` with:

```go
// afterMerge runs what no transaction reaches. Each step is best effort and
// reports into Warnings: the merge has already committed.
//
// SRC's directory now holds only what the merge left behind -- collapsed
// files and derived vector files -- so it is kept with the backup rather than
// deleted. Pins live outside the storage root and are rewritten last.
func (c *Core) afterMerge(ctx context.Context, plan *MergePlan) {
	warn := func(format string, args ...any) {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(format, args...))
	}
	if root, err := c.root(); err != nil {
		warn("locating the storage root: %v", err)
	} else if srcDir := filepath.Join(root, "projects", plan.Src); dirExists(srcDir) {
		leftover := filepath.Join(plan.Backup, "leftover")
		if err := os.MkdirAll(leftover, 0o700); err != nil {
			warn("keeping %s with the backup: %v", srcDir, err)
		} else if err := os.Rename(srcDir, filepath.Join(leftover, plan.Src)); err != nil {
			warn("keeping %s with the backup: %v", srcDir, err)
		}
	}
	for _, r := range plan.Pins.Rewrite {
		if err := writeAtomic(r.Path, []byte(r.To+"\n"), true); err != nil {
			warn("rewriting %s: %v; it still names %s", r.Path, err, r.From)
		}
	}
	if c.knowledgeChanged != nil {
		if err := c.knowledgeChanged(ctx, plan.dstID); err != nil {
			warn("refreshing %s's derived search state: %v", plan.Dst, err)
		}
	}
}

func dirExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l . ; go test ./internal/core/ ./internal/resolve/ && go vet ./... && staticcheck ./...`
Expected: `ok`, with nothing from `gofmt` or `staticcheck`.

- [ ] **Step 6: Commit**

```bash
git add internal/resolve internal/core
git commit -m "feat(core): after a merge, rewrite pins and keep the source's leftovers with the backup

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: `trellis project merge`, doctor, documentation and the gates

**Files:**
- Modify: `internal/cli/project.go`
- Test: `internal/cli/project_cmd_test.go` (append)
- Modify: `internal/cli/doctor.go`: the fix text of `checkProjectKeys`
- Modify: `README.md`
- Modify: `plugin/skills/trellis/SKILL.md`

**Interfaces:**
- Consumes:
  - `core.MergeProjects`, `core.MergeOptions`, `core.MergePlan` (Tasks 6–9)
  - `resolve.ScanRoot`, `resolve.PinsUnder` (Task 9)
  - `normalizeProjectArg`, `openCore`, `Emit`
  - `pinEnv`, `seedProject`, `writePin`, `showBoard`, `coreErr` (test helpers)
- Produces:
  - `func newProjectMergeCmd() *cobra.Command`
  - `func mergeTable(p core.MergePlan, applied bool) string`
  - Error code `missing_into` (exit 2)

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/project_cmd_test.go`, and add `"os"`, `"path/filepath"` and `"strings"` to its imports:

```go
func TestProjectMergeEndToEnd(t *testing.T) {
	repo := pinEnv(t, "mono")
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedProject(t, "MONO")
	seedProject(t, "API")
	writePin(t, repo, "/MONO\n")
	api := filepath.Join(repo, "api")
	if err := os.Mkdir(api, 0o755); err != nil {
		t.Fatal(err)
	}
	writePin(t, api, "/API\n")
	t.Chdir(api)
	if out := runCmd(t, "card", "new", "--title", "from api", "--json"); !strings.Contains(out, `"ref":"API-1"`) {
		t.Fatalf("card new = %s", out)
	}

	_, err := execCmd("project", "merge", "api")
	if ce := coreErr(t, err); ce.Code != "missing_into" {
		t.Errorf("no --into: %+v", ce)
	}

	var plan struct {
		Ready bool `json:"ready"`
		Pins  struct {
			Rewrite []struct {
				Path string `json:"path"`
				To   string `json:"to"`
			} `json:"rewrite"`
		} `json:"pins"`
	}
	out := runCmd(t, "project", "merge", "api", "--into", "/mono", "--json")
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !plan.Ready || len(plan.Pins.Rewrite) != 1 || plan.Pins.Rewrite[0].To != "/MONO/boards/api" {
		t.Errorf("plan = %+v", plan)
	}
	if got, _ := os.ReadFile(filepath.Join(api, ".trellis")); string(got) != "/API\n" {
		t.Errorf("the plan rewrote a pin: %q", got)
	}

	runCmd(t, "project", "merge", "api", "--into", "mono", "--apply")

	if got, _ := os.ReadFile(filepath.Join(api, ".trellis")); string(got) != "/MONO/boards/api\n" {
		t.Errorf("pin after the merge = %q", got)
	}
	if got := showBoard(t); got.Project != "MONO" || got.Slug != "api" {
		t.Errorf("this directory now opens %+v", got)
	}
	runCmd(t, "card", "show", "API-1")
	_, err = execCmd("--project", "API", "card", "ls")
	if ce := coreErr(t, err); ce.Code != "project_merged" {
		t.Errorf("--project API: %+v", ce)
	}
}
```

Run: `go test ./internal/cli/ -run ProjectMergeEndToEnd`
Expected: FAIL with `unknown command "merge"`, or an unknown-flag error.

- [ ] **Step 2: Write the command**

In `internal/cli/project.go`, add `newProjectMergeCmd()` to the `cmd.AddCommand(...)` call in `newProjectCmd`. Add the imports `"os"`, `"github.com/mtch3n/trellis/internal/core"` and `"github.com/mtch3n/trellis/internal/resolve"`. Then append:

```go
func newProjectMergeCmd() *cobra.Command {
	var into string
	var apply, rename bool
	cmd := &cobra.Command{
		Use:   "merge <SRC> --into <DST>",
		Short: "Merge one project into another; prints the plan unless --apply",
		Long: "Move every board, card, knowledge entry and artifact of SRC into DST, keep\n" +
			"card refs such as SRC-12 working, and retire SRC's key. Without --apply the\n" +
			"merge only reports what it would do; with --apply it backs up first.\n\n" +
			"A document or artifact that both projects name, with different content, stops\n" +
			"the merge; --rename-conflicts renames SRC's side instead. Pins that name SRC\n" +
			"under the enclosing repository are rewritten: commit them.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if into == "" {
				return core.ErrUsage("missing_into", "name the project to merge into",
					"trellis project merge "+args[0]+" --into <KEY>")
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := resolve.ScanRoot(dir)
			if err != nil {
				return err
			}
			pins, skipped, err := resolve.PinsUnder(root)
			if err != nil {
				return err
			}
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			plan, err := c.MergeProjects(cmd.Context(), normalizeProjectArg(args[0]), normalizeProjectArg(into),
				core.MergeOptions{Apply: apply, RenameConflicts: rename, ScanRoot: root, Pins: pins, UnreadablePins: skipped})
			if err != nil {
				return err
			}
			return Emit(cmd, plan, func() string { return mergeTable(plan, apply) })
		},
	}
	cmd.Flags().StringVar(&into, "into", "", "the project that receives everything (KEY or /KEY)")
	cmd.Flags().BoolVar(&apply, "apply", false, "perform the merge; without it only the plan is printed")
	cmd.Flags().BoolVar(&rename, "rename-conflicts", false, "rename SRC's side of a name conflict instead of stopping")
	return cmd
}

// mergeTable is the plan for a person: what moves, what stops the merge, and
// what the caller has to do next.
func mergeTable(p core.MergePlan, applied bool) string {
	var b strings.Builder
	verb := "would merge"
	if applied {
		verb = "merged"
	}
	fmt.Fprintf(&b, "%s %s into %s\n", verb, p.Src, p.Dst)
	if p.Refused != "" {
		fmt.Fprintf(&b, "refused: %s\n", p.Refused)
		return strings.TrimRight(b.String(), "\n")
	}
	for _, bm := range p.Boards {
		fmt.Fprintf(&b, "board   %s -> %s (/%s/boards/%s)\n", bm.Name, bm.NewName, p.Dst, bm.NewSlug)
	}
	fmt.Fprintf(&b, "cards   %d, numbered in %s from %d; refs unchanged\n", p.Cards.Moved, p.Dst, p.Cards.FirstSeq)
	for _, group := range []struct {
		name  string
		moves core.ItemMoves
	}{{"knowledge", p.Knowledge}, {"artifacts", p.Artifacts}} {
		fmt.Fprintf(&b, "%-9s %d moved, %d collapsed, %d renamed, %d in conflict\n", group.name,
			group.moves.Moved, len(group.moves.Collapsed), len(group.moves.Renamed), len(group.moves.Conflicts))
		for _, r := range group.moves.Renamed {
			fmt.Fprintf(&b, "  renamed   %s -> %s\n", r.From, r.To)
		}
		for _, conflict := range group.moves.Conflicts {
			note := ""
			if conflict.Vault {
				note = " (vault entry: never renamed)"
			}
			fmt.Fprintf(&b, "  conflict  %s%s\n", conflict.Name, note)
		}
	}
	fmt.Fprintf(&b, "labels  %d moved, %d folded; tags %d moved, %d folded\n",
		len(p.Labels.Moved), len(p.Labels.Folded), len(p.Tags.Moved), len(p.Tags.Folded))
	for _, d := range p.ConfigDropped {
		fmt.Fprintf(&b, "config  %s=%s dropped (%s keeps %q)\n", d.Key, d.Src, p.Dst, d.Dst)
	}
	for _, addr := range p.DocumentsRewritten {
		fmt.Fprintf(&b, "rewrite %s\n", addr)
	}
	for _, r := range p.Pins.Rewrite {
		fmt.Fprintf(&b, "pin     %s: %s -> %s\n", r.Path, r.From, r.To)
	}
	for _, left := range p.Pins.Left {
		fmt.Fprintf(&b, "pin     left as is: %s\n", left)
	}
	fmt.Fprintf(&b, "pins    searched under %s only\n", p.Pins.ScanRoot)
	if p.Backup != "" {
		fmt.Fprintf(&b, "backup  %s\n", p.Backup)
	}
	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "warning %s\n", w)
	}
	switch {
	case !p.Ready:
		b.WriteString("not ready: resolve the conflicts, or rerun with --rename-conflicts\n")
	case applied && len(p.Pins.Rewrite) > 0:
		b.WriteString("commit the rewritten .trellis files\n")
	case !applied:
		b.WriteString("rerun with --apply to perform it\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
```

In `internal/cli/doctor.go`, `checkProjectKeys`, change the fix to:

```go
		"trellis project merge <KEY> --into <VALID-KEY>")
```

Run: `gofmt -l . ; go test ./internal/cli/`
Expected: `ok`, including `TestHelpListsEveryCommand`.

- [ ] **Step 3: Document it**

`README.md`: next to the existing `trellis init` lines, add:

```bash
trellis project merge API --into MONO            # print what a merge would do
trellis project merge API --into MONO --apply    # back up, merge, rewrite pins
```

`plugin/skills/trellis/SKILL.md`: add this line to the command reference block that already lists `project new`:

```
trellis project merge SRC --into DST            # plan; --apply merges, refs like SRC-12 keep working
```

- [ ] **Step 4: Run every gate**

```bash
go build ./...
go test ./...
go test -race ./internal/core/ ./internal/store/ ./internal/cli/
go vet ./...
gofmt -l .
staticcheck ./...
GOOS=windows go build ./...
GOOS=darwin go build ./...
python3 -B -m unittest discover -s scripts/tests
```

Expected: every command succeeds, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/cli README.md plugin/skills/trellis/SKILL.md
git commit -m "feat(cli): trellis project merge

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 6: Hand over**

Report to the author:
- The first command the new binary runs applies migration `0015`, which backfills `card.ref`. Back up first with `trellis backup <path>`.
- After installing, run `trellis daemon restart`: the old daemon cannot insert cards into the new schema.
- Any project whose key fails the grammar, as listed by `trellis doctor`, can now be folded with `trellis project merge <KEY> --into <VALID-KEY>`.
