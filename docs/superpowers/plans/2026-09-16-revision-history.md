# Revision History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **WHERE TO WORK — read before anything else.** Every path in this plan is
> relative to the worktree **`/home/mtchen/Personal/trellis-worktrees/knowledge-artifacts`**
> on branch `feat/knowledge-artifacts`. Run every command from there, e.g.
> `cd /home/mtchen/Personal/trellis-worktrees/knowledge-artifacts && go test ./...`.
> **Never** run a command in, or edit a file under, `/home/mtchen/Personal/trellis`.
> That is a shared checkout where other sessions hold uncommitted work.

**Goal:** Let Trellis answer "what changed?" for a knowledge entry or a card: retain old versions without git, diff two retained versions with a maintained library, keep a bounded number by default, and expose all of it through the CLI, the web API and housekeeping.

**Architecture:** A knowledge entry's revisions are whole copies of its file, one per retained version, in a hidden directory beside it (`standup.md` → `.standup.md/7.md`). A version enters history by copy at the moment Trellis sees it — on create, before a write replaces the file, after a write, and when `refreshFromFile` notices an external edit — never by moving the previous file aside, which would lose a version to a race with a direct edit. A card's revisions are rows in a new `card_revision` table, since cards live in the database already. Retention is one setting, `history.keep` (default 100), read once into a `Core` field the same way `lease.ttl` already is. Diffing uses `github.com/aymanbagabas/go-udiff`, a maintained, dependency-free unified-diff library. Everything — capture, trim, move, and remove — runs inside the same `Core.Tx` the write it rides on already uses, and is undone by the same failure path.

**Tech Stack:** Go 1.27, SQLite via `sqlx` (modernc driver), goose migrations, `encoding/json/v2`, cobra, `github.com/aymanbagabas/go-udiff` v0.4.1 (new).

**Spec:** `docs/superpowers/specs/2026-09-16-revision-history-design.md`

## Global Constraints

- Markdown files are the source of truth. A knowledge revision is a file, written with `writeAtomic` like every other file; nothing needs to rebuild it.
- Every Trellis write runs inside one `Core.Tx`, which holds SQLite's write lock from `BEGIN` across processes (`internal/core/core.go`). Revision capture, trim, move and removal all run inside that same transaction and are undone by the same `done`-flag failure path `EditKnowledgeFields`, `DeleteKnowledge`, `EscalateKnowledge` and `DemoteKnowledge` already use.
- A version enters history by **copy**, never by moving the previous version aside — see "How a version enters history" in the spec. This is what keeps a version captured even when a direct edit lands between two Trellis writes.
- Two rules keep history free of repeats: a version already retained is not written again, and a revision byte-identical to the newest retained one is not written. Both live inside `captureKnowledgeRevision`/`captureCardRevision`, not at each call site.
- `history.keep` (default 100): 0 disables capture; a negative value is a configuration error. Lowering it does not delete anything by itself — `maintenance prune --revisions` does.
- No git. No restore-a-revision feature (rewriting a whole file, frontmatter included, could silently clear the `private` flag). No revisions of artifacts, pins, recaps or links. No age-based retention.
- The disclosure design's purge on reclassification does not touch revision files: they are local files with the same standing as the entry, disclosed on request the same as `knowledge show`.
- The diff endpoints and CLI sit under `/api/`, so `internal/ui/security.go`'s `protectedHandler` already checks host, origin and token; nothing here may bypass it.
- CI runs Linux, macOS and Windows; all three must pass. `syncDirectory` is already split per-platform (`internal/core/fsync_unix.go` / `fsync_windows.go`) and every new helper reuses it rather than adding a new platform file. A leading dot does not hide a directory on Windows, and nothing here depends on it doing so.
- No backward-compatibility shims. Pre-1.0.
- **A parallel plan (knowledge templates) is changing `CreateKnowledge`, `Frontmatter` and `RenderDoc` at the same time.** Task 1's edit to `CreateKnowledge` is a small, self-contained insertion (one `captureKnowledgeRevision` call plus one cleanup line) placed at a clearly-anchored point, specifically to minimize collision with that other plan's edits to the same function.
- Gates before any task is done: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` printing nothing. Add `go test -race ./internal/core` for any task touching `internal/core`, and `GOOS=windows go build ./...` for every task.
- Stage by explicit path only. Before every commit run `git diff --cached --name-only` and confirm each path is one this task changed.
- Every commit message ends with `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

## Diff library

The spec calls for a maintained unified-diff library and explicitly rejects `github.com/pmezard/go-difflib` (archived). Checked before choosing:

- `go mod why -m github.com/pmezard/go-difflib` in this repo prints "main module does not need module github.com/pmezard/go-difflib" — it sits in the extended module graph (something's `go.mod` mentions it) but nothing here imports it and it never reached `go.sum`. Adding it fresh would be adding a dead library back on purpose.
- `github.com/aymanbagabas/go-udiff` (checked via `pkg.go.dev` and the module's `go.mod` on GitHub): latest `v0.4.1`, published March 2026 — actively maintained. License MIT and BSD-3-Clause (permissive). `go 1.24` in its `go.mod`, comfortably under this repo's `go 1.27`. Zero dependencies (it is a fork of Go's own `internal/diff` with exported symbols). Exact API used here: `func Unified(oldLabel, newLabel, old, new string) string`, which takes two whole strings and default 3 lines of context and returns a complete unified diff — no separate edit-computation step needed for this plan's use.

Chosen: `github.com/aymanbagabas/go-udiff@v0.4.1`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/store/migrations/0013_card_revisions.sql` | Create: `card_revision` table |
| `internal/core/core.go` | Modify: `historyKeep` field, default, `SetHistoryKeep` |
| `internal/core/revision.go` | Create: knowledge revision file paths, capture, trim, move, list, diff |
| `internal/core/card_revision.go` | Create: card revision capture, trim, list, diff |
| `internal/core/knowledge.go` | Modify: `CreateKnowledge`, `EditKnowledgeFields`, `refreshFromFile`, `DeleteKnowledge` |
| `internal/core/pin.go` | Modify: `EscalateKnowledge`, `DemoteKnowledge` |
| `internal/core/card.go` | Modify: `createCard`, `EditCard` |
| `internal/core/maintenance.go` | Modify: `PruneRevisions`, `PruneOrphanHistory` |
| `internal/core/health.go` | Modify: `RevisionHealth`, `Health` |
| `internal/core/revision_test.go` | Create: knowledge revision behaviour |
| `internal/core/card_revision_test.go` | Create: card revision behaviour |
| `internal/core/maintenance_test.go` | Create: prune/health behaviour |
| `internal/config/config.go` | Modify: `HistoryConfig`, `Defaults`, `applyDefaults`, `GetValue`, `ValidateValue` |
| `internal/config/config_test.go` | Modify: `history.keep` coverage |
| `internal/cli/config.go` | Modify: validate on `set`, list `history.keep` |
| `internal/cli/config_cmd_test.go` | Create: `config set history.keep` behaviour |
| `internal/cli/root.go` | Modify: wire `SetHistoryKeep` in `openCore` |
| `internal/cli/daemon.go` | Modify: wire `SetHistoryKeep` for the daemon's `Core` |
| `internal/cli/knowledge.go` | Modify: `history`, `diff` subcommands |
| `internal/cli/card.go` | Modify: `history`, `diff` subcommands |
| `internal/cli/maintenance.go` | Modify: `--revisions`, `--orphan-history` |
| `internal/cli/history_cmd_test.go` | Create: CLI history/diff behaviour |
| `internal/cli/maintenance_cmd_test.go` | Create: CLI prune-flag behaviour |
| `internal/ui/server.go` | Modify: four routes |
| `internal/ui/history.go` | Create: history/diff handlers |
| `internal/ui/history_test.go` | Create: route behaviour |
| `go.mod`, `go.sum` | Modify: add `github.com/aymanbagabas/go-udiff` |

---

### Task 1: Knowledge revisions are captured on create and edit

**Files:**
- Create: `internal/core/revision.go`
- Modify: `internal/core/core.go` (`Core`, `New`)
- Modify: `internal/core/knowledge.go` (`CreateKnowledge`, `EditKnowledgeFields`, `refreshFromFile`)
- Create: `internal/core/revision_test.go`

**Interfaces:**
- Consumes: `writeAtomic` (`internal/core/file_store.go`), `syncDirectory` (`internal/core/fsync_unix.go` / `fsync_windows.go`), `Knowledge`, `NewKnowledge`, `KnowledgeEdit`, `CreateKnowledge`, `EditKnowledge`, `EditKnowledgeFields`, `kbCore` (test helper, `internal/core/knowledge_test.go`).
- Produces:
  - `func revisionDir(entryPath string) string`
  - `func revisionFilePath(entryPath string, version int64) string`
  - `func parseRevisionVersion(name string) (int64, bool)`
  - `func sortedRevisionVersions(dir string) ([]int64, error)`
  - `func trimRevisions(entryPath string, keep int) (int, error)`
  - `func removeRevisionDirIfEmpty(entryPath string) error`
  - `func (c *Core) captureKnowledgeRevision(entryPath string, version int64, raw []byte) error`
  - `Core.historyKeep int` (unexported; default 100 from `New`); no public setter yet (Task 4 adds `SetHistoryKeep`) — tests in this package set the field directly.

- [ ] **Step 1: Write the failing tests**

Create `internal/core/revision_test.go`:

```go
package core

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCreateKnowledgeCapturesVersionOne(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Standup", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	rev, err := os.ReadFile(revisionFilePath(doc.Path, 1))
	if err != nil {
		t.Fatalf("version 1 was not captured: %v", err)
	}
	entry, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(rev) != string(entry) {
		t.Errorf("revision 1 = %q, want the entry's own bytes %q", rev, entry)
	}
}

func TestEditKeepsBothVersions(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	v1, err := os.ReadFile(revisionFilePath(doc.Path, 1))
	if err != nil {
		t.Fatalf("version 1 missing before the edit: %v", err)
	}

	edited, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", &doc.Version)
	if err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	if edited.Version != 2 {
		t.Fatalf("version = %d, want 2", edited.Version)
	}
	v1After, err := os.ReadFile(revisionFilePath(doc.Path, 1))
	if err != nil || string(v1After) != string(v1) {
		t.Errorf("version 1 changed or vanished after the edit: %v", err)
	}
	v2, err := os.ReadFile(revisionFilePath(doc.Path, 2))
	if err != nil {
		t.Fatalf("version 2 was not captured: %v", err)
	}
	entry, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(v2) != string(entry) {
		t.Errorf("revision 2 = %q, want the entry's current bytes %q", v2, entry)
	}
}

// An entry that existed before this feature has no revision directory. Its
// first Trellis write must still capture the version it is about to replace.
func TestFirstEditCapturesAPredatingVersion(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Old", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if err := os.RemoveAll(revisionDir(doc.Path)); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}

	edited, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", &doc.Version)
	if err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	pre, err := os.ReadFile(revisionFilePath(doc.Path, 1))
	if err != nil || string(pre) != string(original) {
		t.Errorf("version 1 = %q, %v; want the pre-edit bytes %q", pre, err, original)
	}
	if _, err := os.Stat(revisionFilePath(doc.Path, edited.Version)); err != nil {
		t.Errorf("version %d was not captured: %v", edited.Version, err)
	}
}

// This is the case that motivates copying a version in rather than moving the
// previous one out: a direct edit lands after Trellis has already written a
// version, and that version must not be lost.
func TestADirectEditAfterATrellisWriteKeepsBothVersions(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Race", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	trellisEdit, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2 via trellis\n", &doc.Version)
	if err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}

	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	direct := RenderDoc(fm, "v3 direct edit\n")
	if err := os.WriteFile(doc.Path, []byte(direct), 0o600); err != nil {
		t.Fatal(err)
	}

	reloaded, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if reloaded.Version != trellisEdit.Version+1 {
		t.Fatalf("version = %d, want %d after the direct edit", reloaded.Version, trellisEdit.Version+1)
	}

	trellisRev, err := os.ReadFile(revisionFilePath(doc.Path, trellisEdit.Version))
	if err != nil {
		t.Fatalf("the trellis-written version was lost: %v", err)
	}
	if !strings.Contains(string(trellisRev), "v2 via trellis") {
		t.Errorf("revision %d = %q, want the trellis-written body", trellisEdit.Version, trellisRev)
	}
	directRev, err := os.ReadFile(revisionFilePath(doc.Path, reloaded.Version))
	if err != nil {
		t.Fatalf("the direct edit was not captured: %v", err)
	}
	if !strings.Contains(string(directRev), "v3 direct edit") {
		t.Errorf("revision %d = %q, want the direct edit's body", reloaded.Version, directRev)
	}
}

func TestRepeatedReadsWriteNoNewRevision(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Stable", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
			t.Fatalf("LoadKnowledge: %v", err)
		}
	}
	entries, err := os.ReadDir(revisionDir(doc.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d revision files after repeated reads, want 1", len(entries))
	}
}

// The private mirror can drift from the file without the file's bytes
// changing (§ refreshFromFile: a database restored from an older backup, or a
// file that already carried the key when the column was added). That bump
// must not write a revision.
func TestPrivateMirrorDriftWritesNoRevision(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Quiet", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.db.Exec(`UPDATE knowledge SET private = 1 WHERE id = ?`, doc.ID); err != nil {
		t.Fatal(err)
	}

	reloaded, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if reloaded.Version != doc.Version+1 {
		t.Fatalf("version = %d, want %d: the drift must still bump the version", reloaded.Version, doc.Version+1)
	}
	entries, err := os.ReadDir(revisionDir(doc.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d revision files after a drift-only bump, want 1 (version 1 only)", len(entries))
	}
	if _, err := os.Stat(revisionFilePath(doc.Path, reloaded.Version)); err == nil {
		t.Errorf("a revision was captured for the drift-only version %d", reloaded.Version)
	}
}

// c.historyKeep is unexported: this package's own tests set it directly
// rather than through a setter. Task 4 adds the public SetHistoryKeep, wired
// from config; the field and its enforcement already work without it.
func TestEditTrimsToHistoryKeep(t *testing.T) {
	c, p, _ := kbCore(t)
	c.historyKeep = 3
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Busy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	version := doc.Version
	for i := 2; i <= 4; i++ {
		edited, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, fmt.Sprintf("v%d\n", i), &version)
		if err != nil {
			t.Fatalf("EditKnowledge v%d: %v", i, err)
		}
		version = edited.Version
	}
	entries, err := os.ReadDir(revisionDir(doc.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("%d revision files, want 3 (keep=3)", len(entries))
	}
	if _, err := os.Stat(revisionFilePath(doc.Path, 1)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("version 1 should have been trimmed")
	}
	for v := int64(2); v <= 4; v++ {
		if _, err := os.Stat(revisionFilePath(doc.Path, v)); err != nil {
			t.Errorf("version %d should be retained: %v", v, err)
		}
	}
}

func TestHistoryKeepZeroCapturesNothing(t *testing.T) {
	c, p, _ := kbCore(t)
	c.historyKeep = 0
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Off", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := os.Stat(revisionDir(doc.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a revision directory was created despite historyKeep = 0")
	}
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", &doc.Version); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	if _, err := os.Stat(revisionDir(doc.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a revision directory was created by an edit despite historyKeep = 0")
	}
}

// A revision write that cannot land must fail the edit and leave the entry
// file exactly as it was: revision capture rides the same undo path as every
// other failure in EditKnowledgeFields.
func TestAFailedRevisionWriteFailsTheEdit(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Blocked", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	original, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Version 1's revision file cannot be (re-)written: a plain file occupies
	// where its directory needs to be.
	if err := os.RemoveAll(revisionDir(doc.Path)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(revisionDir(doc.Path), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", &doc.Version)
	if err == nil {
		t.Fatal("EditKnowledge succeeded despite a blocked revision directory")
	}

	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(original) {
		t.Errorf("file = %q, want the original bytes after the failed edit", raw)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestCreateKnowledgeCapturesVersionOne|TestEditKeepsBothVersions|TestFirstEditCapturesAPredatingVersion|TestADirectEditAfterATrellisWriteKeepsBothVersions|TestRepeatedReadsWriteNoNewRevision|TestPrivateMirrorDriftWritesNoRevision|TestEditTrimsToHistoryKeep|TestHistoryKeepZeroCapturesNothing|TestAFailedRevisionWriteFailsTheEdit' -v`
Expected: the package does not compile — `revisionFilePath undefined`, `revisionDir undefined`, `c.historyKeep undefined`.

- [ ] **Step 3: Add the `historyKeep` field**

In `internal/core/core.go`, add `historyKeep int` to `type Core struct` directly after `requireTags bool`:

```go
	requireLabels  bool
	requireTags    bool
	// historyKeep is how many revisions each entry and card retains. Default
	// 100; zero disables capture. Negative values are rejected before they
	// ever reach here — see config.ValidateValue and config.Load.
	historyKeep int
```

Change `New` to set the default:

```go
func New(db *sqlx.DB, clock Clock, actor string) *Core {
	return &Core{db: db, clock: clock, actor: actor, leaseTTL: 30 * 60 * 1000, historyKeep: 100}
}
```

- [ ] **Step 4: Create the revision file-path helpers**

Create `internal/core/revision.go`:

```go
package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// revisionDir is the hidden directory beside an entry's file that holds its
// retained versions: "standup.md" -> ".standup.md". Slugify turns every
// character outside a-z0-9 into "-" and trims "-", so no entry or directory
// name this package creates can start with a dot; a revision directory can
// therefore never be mistaken for one (revision-history design, "Never
// mistaken for an entry").
func revisionDir(entryPath string) string {
	return filepath.Join(filepath.Dir(entryPath), "."+filepath.Base(entryPath))
}

// revisionFilePath is where one version of an entry is stored. A revision
// keeps the entry's own extension, so version 7 of "standup.md" is
// ".standup.md/7.md".
func revisionFilePath(entryPath string, version int64) string {
	return filepath.Join(revisionDir(entryPath), strconv.FormatInt(version, 10)+filepath.Ext(entryPath))
}

// parseRevisionVersion extracts a revision file's version number from its
// name, ignoring anything that is not "<positive integer><anything>".
func parseRevisionVersion(name string) (int64, bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	v, err := strconv.ParseInt(base, 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// sortedRevisionVersions returns a revision directory's retained version
// numbers, ascending. A missing directory is not an error: nothing has ever
// been captured there.
func sortedRevisionVersions(dir string) ([]int64, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var versions []int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if v, ok := parseRevisionVersion(e.Name()); ok {
			versions = append(versions, v)
		}
	}
	slices.Sort(versions)
	return versions, nil
}

// trimRevisions removes an entry's oldest revisions until at most keep
// remain, ordered by version. It returns the number removed.
func trimRevisions(entryPath string, keep int) (int, error) {
	versions, err := sortedRevisionVersions(revisionDir(entryPath))
	if err != nil {
		return 0, err
	}
	removed := 0
	for len(versions) > keep {
		if err := os.Remove(revisionFilePath(entryPath, versions[0])); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		versions = versions[1:]
		removed++
	}
	if removed > 0 {
		if err := syncDirectory(revisionDir(entryPath)); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// removeRevisionDirIfEmpty removes an entry's revision directory when it
// holds no files, so a create that failed after writing version 1 leaves
// nothing behind for orphan detection to later report.
func removeRevisionDirIfEmpty(entryPath string) error {
	dir := revisionDir(entryPath)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	if err := os.Remove(dir); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(dir))
}

// captureKnowledgeRevision retains raw as version's copy of entryPath, unless
// capture is disabled (historyKeep == 0), that version is already retained,
// or raw is byte-identical to the newest retained revision — the two rules
// that keep history free of repeats (revision-history design). It then trims
// down to historyKeep.
func (c *Core) captureKnowledgeRevision(entryPath string, version int64, raw []byte) error {
	if c.historyKeep == 0 {
		return nil
	}
	dest := revisionFilePath(entryPath, version)
	if _, err := os.Stat(dest); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := revisionDir(entryPath)
	versions, err := sortedRevisionVersions(dir)
	if err != nil {
		return err
	}
	if len(versions) > 0 {
		prior, err := os.ReadFile(revisionFilePath(entryPath, versions[len(versions)-1]))
		if err != nil {
			return err
		}
		if string(prior) == string(raw) {
			return nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := writeAtomic(dest, raw, false); err != nil {
		return err
	}
	_, err = trimRevisions(entryPath, c.historyKeep)
	return err
}
```

- [ ] **Step 5: Capture version 1 in `CreateKnowledge`**

In `internal/core/knowledge.go`, `CreateKnowledge` currently reads (inside the `c.Tx` closure):

```go
		doc = Knowledge{
			ID: NewCardID(), ProjectID: projectID, BoardID: boardID, Slug: slug,
			Title: in.Title, Path: path, DocType: fm.Type, Summary: in.Summary,
			Provenance: provenance,
			Private:    in.Private,
			BodyMD:     body, ContentHash: ContentHash(raw), MTime: st.ModTime().UnixMilli(),
			Size: st.Size(), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertKnowledge(tx, doc); err != nil {
			return err
		}
		if err := c.syncDocRelations(tx, &doc, fm, body); err != nil {
			return err
		}
```

Insert the capture call between the two, immediately after `insertKnowledge` succeeds — after `insertKnowledge` so `doc.ContentHash` is already set, which the failure cleanup below depends on:

```go
		doc = Knowledge{
			ID: NewCardID(), ProjectID: projectID, BoardID: boardID, Slug: slug,
			Title: in.Title, Path: path, DocType: fm.Type, Summary: in.Summary,
			Provenance: provenance,
			Private:    in.Private,
			BodyMD:     body, ContentHash: ContentHash(raw), MTime: st.ModTime().UnixMilli(),
			Size: st.Size(), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertKnowledge(tx, doc); err != nil {
			return err
		}
		if err := c.captureKnowledgeRevision(doc.Path, doc.Version, []byte(raw)); err != nil {
			return err
		}
		if err := c.syncDocRelations(tx, &doc, fm, body); err != nil {
			return err
		}
```

`CreateKnowledge`'s failure cleanup, just below the `c.Tx` call, currently reads:

```go
	if err != nil && writtenPath != "" {
		// A failed transaction must not leave a database-less knowledge file.
		// Keep a committed file intact if SQLite reports an ambiguous commit by
		// only removing the path when it still has the exact bytes we wrote.
		if raw, readErr := os.ReadFile(writtenPath); readErr == nil && ContentHash(string(raw)) == doc.ContentHash {
			_ = os.Remove(writtenPath)
			_ = syncDirectory(filepath.Dir(writtenPath))
		}
	}
```

Extend the matched branch to also remove version 1's revision and, if that empties the directory, the directory itself:

```go
	if err != nil && writtenPath != "" {
		// A failed transaction must not leave a database-less knowledge file.
		// Keep a committed file intact if SQLite reports an ambiguous commit by
		// only removing the path when it still has the exact bytes we wrote.
		if raw, readErr := os.ReadFile(writtenPath); readErr == nil && ContentHash(string(raw)) == doc.ContentHash {
			_ = os.Remove(writtenPath)
			_ = syncDirectory(filepath.Dir(writtenPath))
			_ = os.Remove(revisionFilePath(writtenPath, 1))
			_ = removeRevisionDirIfEmpty(writtenPath)
		}
	}
```

- [ ] **Step 6: Capture before and after in `EditKnowledgeFields`**

In `internal/core/knowledge.go`, `EditKnowledgeFields` currently has:

```go
		base := doc.ContentHash // the hash this write is based on
		oldRaw = raw
		fm, body, err := splitDocFile(doc.Path, raw)
```

Insert the "before" capture between the two, using the pre-edit version and the file's current bytes:

```go
		base := doc.ContentHash // the hash this write is based on
		oldRaw = raw
		if err := c.captureKnowledgeRevision(doc.Path, doc.Version, oldRaw); err != nil {
			return err
		}
		fm, body, err := splitDocFile(doc.Path, raw)
```

Further down, the closure currently ends with:

```go
			for _, name := range namesAdded(fm.Artifacts, before) {
				value := name
				if doc.Private {
					value = ""
				}
				if err := c.recordEvent(tx, "knowledge", doc.ID, "artifact_unlinked", "", "", value); err != nil {
					return err
				}
			}
		}
		if err := c.docView(tx, &doc); err != nil {
			return err
		}
		done = true
		return nil
	})
```

Insert the "after" capture as the very last file-touching step, right before `docView` — this is deliberate: everything that can fail because of the *edit's own content* (the `recordEvent` calls above) has already succeeded by this point, so a later failure here cannot leave a revision file for a version the edit never actually committed:

```go
			for _, name := range namesAdded(fm.Artifacts, before) {
				value := name
				if doc.Private {
					value = ""
				}
				if err := c.recordEvent(tx, "knowledge", doc.ID, "artifact_unlinked", "", "", value); err != nil {
					return err
				}
			}
		}
		if err := c.captureKnowledgeRevision(doc.Path, doc.Version, []byte(out)); err != nil {
			return err
		}
		if err := c.docView(tx, &doc); err != nil {
			return err
		}
		done = true
		return nil
	})
```

`doc.Version` here is already the post-increment version (`doc.Version++` runs earlier in the function, right after `replaceIfUnchanged` succeeds), and `out` is still in scope from `out := RenderDoc(fm, body)`. If this call fails, `written` is already non-empty, so the function's existing `defer` calls `undoWrite(doc.Path, oldRaw, written)` and restores the entry file exactly as every other failure in this function already does.

- [ ] **Step 7: Capture on an externally-detected change in `refreshFromFile`**

In `internal/core/knowledge.go`, `refreshFromFile` currently has:

```go
	oldHash := doc.ContentHash
	doc.ContentHash = ContentHash(string(raw))
	changed := st.ModTime().UnixMilli() != doc.MTime || st.Size() != doc.Size ||
		oldHash != doc.ContentHash || privateDrifted
	doc.MTime = st.ModTime().UnixMilli()
```

Replace those lines to separate "the file's bytes changed" from "the row changed for some reason" — a private-mirror-only drift must bump the version without capturing a revision:

```go
	oldHash := doc.ContentHash
	doc.ContentHash = ContentHash(string(raw))
	contentChanged := st.ModTime().UnixMilli() != doc.MTime || st.Size() != doc.Size || oldHash != doc.ContentHash
	changed := contentChanged || privateDrifted
	doc.MTime = st.ModTime().UnixMilli()
```

Further down, the function currently has:

```go
	if _, err := tx.Exec(
		`UPDATE knowledge SET title = ?, doc_type = ?, summary = ?, provenance = ?, private = ?,
		                      content_hash = ?, mtime = ?, size = ?, version = ?,
		                      updated_at = ? WHERE id = ?`,
		doc.Title, doc.DocType, doc.Summary, doc.Provenance, doc.Private, doc.ContentHash,
		doc.MTime, doc.Size, doc.Version, doc.UpdatedAt, doc.ID); err != nil {
		return err
	}
	if becamePrivate {
```

Insert the capture call, gated on `contentChanged`, right after the `UPDATE`:

```go
	if _, err := tx.Exec(
		`UPDATE knowledge SET title = ?, doc_type = ?, summary = ?, provenance = ?, private = ?,
		                      content_hash = ?, mtime = ?, size = ?, version = ?,
		                      updated_at = ? WHERE id = ?`,
		doc.Title, doc.DocType, doc.Summary, doc.Provenance, doc.Private, doc.ContentHash,
		doc.MTime, doc.Size, doc.Version, doc.UpdatedAt, doc.ID); err != nil {
		return err
	}
	if contentChanged {
		if err := c.captureKnowledgeRevision(doc.Path, doc.Version, raw); err != nil {
			return err
		}
	}
	if becamePrivate {
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestCreateKnowledgeCapturesVersionOne|TestEditKeepsBothVersions|TestFirstEditCapturesAPredatingVersion|TestADirectEditAfterATrellisWriteKeepsBothVersions|TestRepeatedReadsWriteNoNewRevision|TestPrivateMirrorDriftWritesNoRevision|TestEditTrimsToHistoryKeep|TestHistoryKeepZeroCapturesNothing|TestAFailedRevisionWriteFailsTheEdit' -v`
Expected: PASS, all nine.

- [ ] **Step 9: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && go test -race ./internal/core && GOOS=windows go build ./...`
Expected: all pass; `gofmt -l .` prints nothing.

- [ ] **Step 10: Commit**

```bash
git add internal/core/core.go internal/core/revision.go internal/core/knowledge.go \
        internal/core/revision_test.go
git commit -m "feat(core): capture knowledge revisions on create and edit

Each entry's versions are copied, one file per version, into a hidden
directory beside it. A version is copied in the moment Trellis sees it --
on create, before a write replaces the file, and after -- never moved,
which is what keeps a version from being lost to a direct edit landing
between two Trellis writes. Capture and trim run inside the same
transaction the write already uses, and are undone by its existing
failure path.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: A knowledge entry's revisions move and delete with it

**Files:**
- Modify: `internal/core/revision.go` (`moveDir`, `moveDirBack`, `copyDirAtomic`)
- Modify: `internal/core/pin.go` (`EscalateKnowledge`, `DemoteKnowledge`)
- Modify: `internal/core/knowledge.go` (`DeleteKnowledge`)
- Modify: `internal/core/revision_test.go`

**Interfaces:**
- Consumes: Task 1's `revisionDir`; `moveFile`, `moveBack`, `baseName`, `copyAtomic`, `stageRemoval` (`internal/core/file_store.go`, `internal/core/pin.go`).
- Produces:
  - `func moveDir(src, destDir string) (string, error)` — moves a directory, refusing to replace anything at the destination; returns `("", nil)` when `src` does not exist.
  - `func moveDirBack(dest, src string) error` — undoes a successful `moveDir`; a no-op when `dest == ""`.
  - `func copyDirAtomic(dest, src string) error`

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/revision_test.go`:

```go
func TestEscalateMovesTheRevisionDirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Shared", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", &doc.Version); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	oldDir := revisionDir(doc.Path)

	escalated, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "shared across projects")
	if err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if _, err := os.Stat(oldDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the old revision directory still exists")
	}
	newDir := revisionDir(escalated.Path)
	for v := int64(1); v <= 2; v++ {
		if _, err := os.Stat(revisionFilePath(escalated.Path, v)); err != nil {
			t.Errorf("version %d missing after escalate: %v", v, err)
		}
	}
	_ = newDir

	back, err := c.DemoteKnowledge(t.Context(), escalated.Slug, "back to project")
	if err != nil {
		t.Fatalf("DemoteKnowledge: %v", err)
	}
	if _, err := os.Stat(newDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the global revision directory still exists after demote")
	}
	for v := int64(1); v <= 2; v++ {
		if _, err := os.Stat(revisionFilePath(back.Path, v)); err != nil {
			t.Errorf("version %d missing after demote: %v", v, err)
		}
	}
}

func TestAFailedEscalateMovesTheRevisionDirectoryBack(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	oldDir := revisionDir(doc.Path)

	if _, err := c.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON event BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	defer func() {
		if _, err := c.db.Exec(`DROP TRIGGER boom`); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	}()

	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason"); err == nil {
		t.Fatal("EscalateKnowledge succeeded despite the trigger")
	}
	if _, err := os.Stat(revisionFilePath(doc.Path, 1)); err != nil {
		t.Errorf("revision directory not restored at %s: %v", oldDir, err)
	}
	globalDir := filepath.Join(c.kbRoot, "global", "knowledge")
	if _, err := os.Stat(filepath.Join(globalDir, "."+filepath.Base(doc.Path))); !os.IsNotExist(err) {
		t.Errorf("revision directory should not remain in the global directory")
	}
}

func TestDeletingAnEntryRemovesItsRevisionDirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Gone", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	dir := revisionDir(doc.Path)

	if err := c.DeleteKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("DeleteKnowledge: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("revision directory still exists after delete")
	}
}

func TestAFailedDeleteRestoresTheRevisionDirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	dir := revisionDir(doc.Path)

	if _, err := c.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON event BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	defer func() {
		if _, err := c.db.Exec(`DROP TRIGGER boom`); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	}()

	if err := c.DeleteKnowledge(t.Context(), p.ID, doc.Slug); err == nil {
		t.Fatal("DeleteKnowledge succeeded despite the trigger")
	}
	if _, err := os.Stat(revisionFilePath(doc.Path, 1)); err != nil {
		t.Errorf("revision directory not restored at %s: %v", dir, err)
	}
}
```

Add `"path/filepath"` to the file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestEscalateMovesTheRevisionDirectory|TestAFailedEscalateMovesTheRevisionDirectoryBack|TestDeletingAnEntryRemovesItsRevisionDirectory|TestAFailedDeleteRestoresTheRevisionDirectory' -v`
Expected: FAIL — the revision directory is left behind by escalate/demote, and delete removes the entry but not its revisions.

- [ ] **Step 3: Add `moveDir`, `moveDirBack` and `copyDirAtomic`**

Append to `internal/core/revision.go`:

```go
// moveDir moves the directory at src into destDir, refusing to replace
// anything already there. Directories cannot be hard-linked the way moveFile
// links a file to get that refusal for free, so the destination is checked
// first — os.Rename silently replaces an empty directory it is given no
// chance to refuse. Rename is atomic and cheap on the common case (same
// filesystem); when it fails for any other reason, this falls back to a
// recursive copy, removing the source only once the copy is synced. src not
// existing is not an error: an entry created before this feature, or with
// capture disabled, may have no revision directory to move.
func moveDir(src, destDir string) (string, error) {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, baseName(src))
	if _, err := os.Stat(dest); err == nil {
		return "", ErrConflict("path_taken", dest+" already exists", "")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(src, dest); err != nil {
		if cerr := copyDirAtomic(dest, src); cerr != nil {
			return "", cerr
		}
		if err := os.RemoveAll(src); err != nil {
			return "", err
		}
	}
	if err := syncDirectory(destDir); err != nil {
		return "", err
	}
	return dest, syncDirectory(filepath.Dir(src))
}

// moveDirBack undoes a successful moveDir: dest moves back beside src. A
// moveDir that found nothing to move returns "", so dest may be empty here;
// that is a no-op, not an error.
func moveDirBack(dest, src string) error {
	if dest == "" {
		return nil
	}
	_, err := moveDir(dest, filepath.Dir(src))
	return err
}

// copyDirAtomic copies a flat directory of revision files. Revision
// directories never nest — each holds only "<version><ext>" files — so this
// need not recurse.
func copyDirAtomic(dest, src string) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyAtomic(filepath.Join(dest, e.Name()), filepath.Join(src, e.Name())); err != nil {
			return err
		}
	}
	return syncDirectory(dest)
}
```

- [ ] **Step 4: Move the revision directory in `EscalateKnowledge`**

In `internal/core/pin.go`, `EscalateKnowledge` currently reads:

```go
func (c *Core) EscalateKnowledge(ctx context.Context, projectID, slug, reason string) (Knowledge, error) {
	var doc Knowledge
	var src, dest string
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		defer func() {
			if !done && dest != "" {
				if merr := moveBack(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
				dest = ""
			}
		}()
```

Replace with:

```go
func (c *Core) EscalateKnowledge(ctx context.Context, projectID, slug, reason string) (Knowledge, error) {
	var doc Knowledge
	var src, dest, revSrc, revDest string
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		defer func() {
			if !done {
				if dest != "" {
					if merr := moveBack(dest, src); merr != nil {
						err = errors.Join(err, merr)
					}
					dest = ""
				}
				if merr := moveDirBack(revDest, revSrc); merr != nil {
					err = errors.Join(err, merr)
				}
				revDest = ""
			}
		}()
```

Further down, `EscalateKnowledge` currently reads:

```go
		src = doc.Path
		moved, err := moveFile(doc.Path, dir)
		if err != nil {
			return err
		}
		dest = moved
		now := c.clock.NowMS()
```

Insert the revision-directory move between the two:

```go
		src = doc.Path
		moved, err := moveFile(doc.Path, dir)
		if err != nil {
			return err
		}
		dest = moved
		revSrc = revisionDir(src)
		revMoved, err := moveDir(revSrc, dir)
		if err != nil {
			return err
		}
		revDest = revMoved
		now := c.clock.NowMS()
```

Finally, the post-`Tx` ambiguous-commit resolution currently reads:

```go
	if err != nil && done {
		var landed string
		qerr := c.db.Get(&landed, `SELECT path FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(landed == dest, qerr) {
			err = nil
		} else if merr := moveBack(dest, src); merr != nil {
			err = errors.Join(err, merr)
		}
	}
	return doc, err
}
```

Replace with:

```go
	if err != nil && done {
		var landed string
		qerr := c.db.Get(&landed, `SELECT path FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(landed == dest, qerr) {
			err = nil
		} else {
			if merr := moveBack(dest, src); merr != nil {
				err = errors.Join(err, merr)
			}
			if merr := moveDirBack(revDest, revSrc); merr != nil {
				err = errors.Join(err, merr)
			}
		}
	}
	return doc, err
}
```

- [ ] **Step 5: Move the revision directory in `DemoteKnowledge`**

Apply the identical shape of change to `DemoteKnowledge` in `internal/core/pin.go`. It currently reads:

```go
func (c *Core) DemoteKnowledge(ctx context.Context, slug, reason string) (Knowledge, error) {
	var doc Knowledge
	var src, dest string
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		defer func() {
			if !done && dest != "" {
				if merr := moveBack(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
				dest = ""
			}
		}()
```

Replace with:

```go
func (c *Core) DemoteKnowledge(ctx context.Context, slug, reason string) (Knowledge, error) {
	var doc Knowledge
	var src, dest, revSrc, revDest string
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		defer func() {
			if !done {
				if dest != "" {
					if merr := moveBack(dest, src); merr != nil {
						err = errors.Join(err, merr)
					}
					dest = ""
				}
				if merr := moveDirBack(revDest, revSrc); merr != nil {
					err = errors.Join(err, merr)
				}
				revDest = ""
			}
		}()
```

Further down, `DemoteKnowledge` currently reads:

```go
		src = doc.Path
		moved, err := moveFile(doc.Path, dir)
		if err != nil {
			return err
		}
		dest = moved
		if _, err := tx.Exec(
			`UPDATE knowledge SET global = 0, path = ?, review_by = NULL, updated_at = ? WHERE id = ?`,
```

Insert the revision-directory move between the two:

```go
		src = doc.Path
		moved, err := moveFile(doc.Path, dir)
		if err != nil {
			return err
		}
		dest = moved
		revSrc = revisionDir(src)
		revMoved, err := moveDir(revSrc, dir)
		if err != nil {
			return err
		}
		revDest = revMoved
		if _, err := tx.Exec(
			`UPDATE knowledge SET global = 0, path = ?, review_by = NULL, updated_at = ? WHERE id = ?`,
```

Its post-`Tx` block currently reads:

```go
	if err != nil && done {
		var landed string
		qerr := c.db.Get(&landed, `SELECT path FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(landed == dest, qerr) {
			err = nil
		} else if merr := moveBack(dest, src); merr != nil {
			err = errors.Join(err, merr)
		}
	}
	return doc, err
}

// VerifyKnowledge resets the review clock on a global entry.
```

Replace the block above `// VerifyKnowledge` with:

```go
	if err != nil && done {
		var landed string
		qerr := c.db.Get(&landed, `SELECT path FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(landed == dest, qerr) {
			err = nil
		} else {
			if merr := moveBack(dest, src); merr != nil {
				err = errors.Join(err, merr)
			}
			if merr := moveDirBack(revDest, revSrc); merr != nil {
				err = errors.Join(err, merr)
			}
		}
	}
	return doc, err
}

// VerifyKnowledge resets the review clock on a global entry.
```

- [ ] **Step 6: Stage the revision directory's removal in `DeleteKnowledge`**

In `internal/core/knowledge.go`, `DeleteKnowledge` currently reads:

```go
func (c *Core) DeleteKnowledge(ctx context.Context, projectID, slug string) error {
	var staged *stagedRemoval
	var doc Knowledge
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		if err := tx.Get(&doc,
			`SELECT * FROM knowledge WHERE project_id = ? AND slug = ?`, projectID, Slugify(slug)); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("knowledge_not_found", "no knowledge entry "+slug, "trellis knowledge ls")
			}
			return err
		}
		var serr error
		staged, serr = stageRemoval(doc.Path)
		if serr != nil {
			return serr
		}
		defer func() {
			if !done {
				if rerr := staged.restore(); rerr != nil {
					err = errors.Join(err, rerr)
				}
			}
		}()
```

Replace with:

```go
func (c *Core) DeleteKnowledge(ctx context.Context, projectID, slug string) error {
	var staged, revStaged *stagedRemoval
	var doc Knowledge
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		if err := tx.Get(&doc,
			`SELECT * FROM knowledge WHERE project_id = ? AND slug = ?`, projectID, Slugify(slug)); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("knowledge_not_found", "no knowledge entry "+slug, "trellis knowledge ls")
			}
			return err
		}
		var serr error
		staged, serr = stageRemoval(doc.Path)
		if serr != nil {
			return serr
		}
		revStaged, serr = stageRemoval(revisionDir(doc.Path))
		if serr != nil {
			if rerr := staged.restore(); rerr != nil {
				return errors.Join(serr, rerr)
			}
			return serr
		}
		defer func() {
			if !done {
				if rerr := staged.restore(); rerr != nil {
					err = errors.Join(err, rerr)
				}
				if rerr := revStaged.restore(); rerr != nil {
					err = errors.Join(err, rerr)
				}
			}
		}()
```

Its post-`Tx` block currently reads:

```go
	if err != nil && done {
		var gone int
		qerr := c.db.Get(&gone, `SELECT COUNT(*) FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(gone == 0, qerr) {
			err = nil
		} else if rerr := staged.restore(); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}
	if err != nil {
		return err
	}
	if err := staged.finalize(); err != nil {
		return err
	}
	c.notifyKnowledgeChanged(ctx, projectID)
	return nil
}
```

Replace with:

```go
	if err != nil && done {
		var gone int
		qerr := c.db.Get(&gone, `SELECT COUNT(*) FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(gone == 0, qerr) {
			err = nil
		} else {
			if rerr := staged.restore(); rerr != nil {
				err = errors.Join(err, rerr)
			}
			if rerr := revStaged.restore(); rerr != nil {
				err = errors.Join(err, rerr)
			}
		}
	}
	if err != nil {
		return err
	}
	if err := staged.finalize(); err != nil {
		return err
	}
	if err := revStaged.finalize(); err != nil {
		return err
	}
	c.notifyKnowledgeChanged(ctx, projectID)
	return nil
}
```

`stageRemoval` already handles a path that does not exist by returning a `stagedRemoval` whose `restore`/`finalize` are no-ops (`internal/core/file_store.go`), so this works whether or not an entry's revision directory exists — a fresh entry with `historyKeep: 0` in effect has none, and nothing above treats that as an error.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestEscalateMovesTheRevisionDirectory|TestAFailedEscalateMovesTheRevisionDirectoryBack|TestDeletingAnEntryRemovesItsRevisionDirectory|TestAFailedDeleteRestoresTheRevisionDirectory' -v`
Expected: PASS.

- [ ] **Step 8: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && go test -race ./internal/core && GOOS=windows go build ./...`
Expected: all pass. Also confirm the pre-existing race-condition regression suite still passes with these edits: `go test ./internal/core -run 'TestAFailedEditPutsTheFileBack|TestAFailedEscalateMovesTheFileBack|TestAFailedDemoteMovesTheFileBack|TestAFailedDeletePutsTheFileBack|TestAPanicAfterTheEditWritePutsTheFileBack|TestAPanicAfterTheDeleteWritePutsTheFileBack|TestAPanicAfterTheEscalateMoveMovesTheFileBack|TestAPanicAfterTheDemoteMoveMovesTheFileBack' -v` — expected PASS, unchanged: none of this task's additions call `c.clock`, so the `panicClock{calls: N}` call-count assumptions those tests depend on are untouched.

- [ ] **Step 9: Commit**

```bash
git add internal/core/revision.go internal/core/pin.go internal/core/knowledge.go \
        internal/core/revision_test.go
git commit -m "feat(core): move and remove a knowledge entry's revisions with it

Escalate and demote move the hidden revision directory alongside the
entry's file, undoing that move the same way they already undo the
file's own move on failure. Delete stages the revision directory's
removal alongside the entry's, so a failed delete restores both.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Cards capture a revision on create and on a title or body edit

**Files:**
- Create: `internal/store/migrations/0013_card_revisions.sql`
- Create: `internal/core/card_revision.go`
- Modify: `internal/core/card.go` (`createCard`, `EditCard`)
- Create: `internal/core/card_revision_test.go`

**Interfaces:**
- Consumes: `Card`, `CardEdit`, `CardRef`, `CreateCard`, `EditCard`, `MoveCard`, `DeleteCard`, `kbCore` (test helper).
- Produces:
  - `func (c *Core) captureCardRevision(tx *sqlx.Tx, cardID string, version int64, title, body string) error`
  - `func trimCardRevisions(tx *sqlx.Tx, cardID string, keep int) error`

- [ ] **Step 1: Write the failing tests**

Create `internal/core/card_revision_test.go`:

```go
package core

import (
	"fmt"
	"testing"
)

func cardRevisionCount(t *testing.T, c *Core, cardID string) int {
	t.Helper()
	var n int
	if err := c.db.Get(&n, `SELECT COUNT(*) FROM card_revision WHERE card_id = ?`, cardID); err != nil {
		t.Fatal(err)
	}
	return n
}

func cardRevisionVersions(t *testing.T, c *Core, cardID string) []int64 {
	t.Helper()
	var versions []int64
	if err := c.db.Select(&versions, `SELECT version FROM card_revision WHERE card_id = ? ORDER BY version`, cardID); err != nil {
		t.Fatal(err)
	}
	return versions
}

func TestCreateCardCapturesVersionOne(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship", Body: "draft"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 1 {
		t.Fatalf("%d card_revision rows after create, want 1", n)
	}
	var title, body string
	if err := c.db.QueryRow(`SELECT title, body_md FROM card_revision WHERE card_id = ? AND version = 1`, card.ID).
		Scan(&title, &body); err != nil {
		t.Fatal(err)
	}
	if title != "Ship" || body != "draft" {
		t.Errorf("revision 1 = %q/%q, want Ship/draft", title, body)
	}
}

func TestEditingTitleOrBodyCapturesBothVersions(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship", Body: "v1"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	newBody := "v2"
	edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &newBody, IfVersion: &card.Version})
	if err != nil {
		t.Fatalf("EditCard: %v", err)
	}
	versions := cardRevisionVersions(t, c, card.ID)
	if len(versions) != 2 || versions[0] != card.Version || versions[1] != edited.Version {
		t.Fatalf("versions = %v, want [%d %d]", versions, card.Version, edited.Version)
	}
}

// Moving a card bumps its version without touching title or body, so it must
// not write a revision. The card's next body edit does, numbered with the
// card's version at that time -- which is why versions have gaps.
func TestMovingACardWritesNoRevisionButTheNextEditDoes(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	moved, err := c.MoveCard(t.Context(), p.ID, b.ID, CardRef{UUID: card.ID}, "review")
	if err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	if moved.Version != card.Version+1 {
		t.Fatalf("version = %d, want %d after the move", moved.Version, card.Version+1)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 1 {
		t.Fatalf("%d card_revision rows after a move, want 1 (version 1 only)", n)
	}

	newBody := "after the move"
	edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &newBody, IfVersion: &moved.Version})
	if err != nil {
		t.Fatalf("EditCard: %v", err)
	}
	versions := cardRevisionVersions(t, c, card.ID)
	if len(versions) != 2 || versions[1] != edited.Version {
		t.Fatalf("versions = %v, want the move's version absent and %d present", versions, edited.Version)
	}
}

func TestFirstEditOfAPreexistingCardCapturesItsPriorState(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Old", Body: "v1"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	// Simulate a card that predates this feature: no rows for it yet.
	if _, err := c.db.Exec(`DELETE FROM card_revision WHERE card_id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}

	newBody := "v2"
	edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &newBody, IfVersion: &card.Version})
	if err != nil {
		t.Fatalf("EditCard: %v", err)
	}
	versions := cardRevisionVersions(t, c, card.ID)
	if len(versions) != 2 || versions[0] != card.Version || versions[1] != edited.Version {
		t.Fatalf("versions = %v, want [%d %d]", versions, card.Version, edited.Version)
	}
}

func TestEditCardTrimsToHistoryKeep(t *testing.T) {
	c, p, b := kbCore(t)
	c.historyKeep = 3
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Busy", Body: "v1"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	version := card.Version
	for i := 2; i <= 5; i++ {
		body := fmt.Sprintf("v%d", i)
		edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &body, IfVersion: &version})
		if err != nil {
			t.Fatalf("EditCard v%d: %v", i, err)
		}
		version = edited.Version
	}
	if n := cardRevisionCount(t, c, card.ID); n != 3 {
		t.Fatalf("%d card_revision rows, want 3 (keep=3)", n)
	}
}

func TestHistoryKeepZeroCapturesNoCardRevisions(t *testing.T) {
	c, p, b := kbCore(t)
	c.historyKeep = 0
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Off"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 0 {
		t.Fatalf("%d card_revision rows despite historyKeep = 0, want 0", n)
	}
	newBody := "v2"
	if _, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &newBody, IfVersion: &card.Version}); err != nil {
		t.Fatalf("EditCard: %v", err)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 0 {
		t.Fatalf("%d card_revision rows despite historyKeep = 0, want 0", n)
	}
}

func TestDeletingACardRemovesItsRevisions(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 1 {
		t.Fatalf("%d card_revision rows before delete, want 1", n)
	}
	if err := c.DeleteCard(t.Context(), p.ID, CardRef{UUID: card.ID}); err != nil {
		t.Fatalf("DeleteCard: %v", err)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 0 {
		t.Fatalf("%d card_revision rows after delete, want 0 (ON DELETE CASCADE)", n)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestCreateCardCapturesVersionOne|TestEditingTitleOrBodyCapturesBothVersions|TestMovingACardWritesNoRevisionButTheNextEditDoes|TestFirstEditOfAPreexistingCardCapturesItsPriorState|TestEditCardTrimsToHistoryKeep|TestHistoryKeepZeroCapturesNoCardRevisions|TestDeletingACardRemovesItsRevisions' -v`
Expected: the package does not compile, or `no such table: card_revision` — the table and the capture calls do not exist yet.

- [ ] **Step 3: Add the migration**

Create `internal/store/migrations/0013_card_revisions.sql`:

```sql
-- +goose Up
-- A card's title and body at a point in time. A card's version also goes up
-- on moves, lease changes and archiving, none of which touch title or body,
-- so revision numbers have gaps -- they are the card's version at capture
-- time, not a dense count. Deletion of the card cascades here: a revision
-- has no life of its own once the card is gone.
CREATE TABLE card_revision (
    card_id    TEXT    NOT NULL REFERENCES card(id) ON DELETE CASCADE,
    version    INTEGER NOT NULL,
    title      TEXT    NOT NULL,
    body_md    TEXT    NOT NULL,
    actor      TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (card_id, version)
);

-- +goose Down
DROP TABLE card_revision;
```

- [ ] **Step 4: Add `captureCardRevision` and `trimCardRevisions`**

Create `internal/core/card_revision.go`:

```go
package core

import (
	"github.com/jmoiron/sqlx"
)

// captureCardRevision retains a card's title and body at version, unless
// capture is disabled. The insert is idempotent (INSERT OR IGNORE) so
// callers may capture a version that might already have a row -- the one a
// card is about to leave, which an earlier edit's own "after" capture may
// already have written -- without checking first.
func (c *Core) captureCardRevision(tx *sqlx.Tx, cardID string, version int64, title, body string) error {
	if c.historyKeep == 0 {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO card_revision (card_id, version, title, body_md, actor, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		cardID, version, title, body, c.actor, c.clock.NowMS()); err != nil {
		return err
	}
	return trimCardRevisions(tx, cardID, c.historyKeep)
}

// trimCardRevisions removes a card's oldest revisions until at most keep
// remain, ordered by version.
func trimCardRevisions(tx *sqlx.Tx, cardID string, keep int) error {
	var versions []int64
	if err := tx.Select(&versions,
		`SELECT version FROM card_revision WHERE card_id = ? ORDER BY version DESC`, cardID); err != nil {
		return err
	}
	if len(versions) <= keep {
		return nil
	}
	_, err := tx.Exec(`DELETE FROM card_revision WHERE card_id = ? AND version <= ?`, cardID, versions[keep])
	return err
}
```

- [ ] **Step 5: Capture version 1 in `createCard`**

In `internal/core/card.go`, `createCard` currently reads:

```go
		if _, err := tx.Exec(
			`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, body_md,
			                   priority, version, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			card.ID, card.ProjectID, card.BoardID, card.Seq, card.ColumnID, card.Rank, card.Title,
			card.BodyMD, int(card.Priority), card.Version, card.CreatedAt, card.UpdatedAt); err != nil {
			return err
		}

		// Add labels (with validation - hard reject if label doesn't exist)
```

Insert the capture call between the two:

```go
		if _, err := tx.Exec(
			`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, body_md,
			                   priority, version, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			card.ID, card.ProjectID, card.BoardID, card.Seq, card.ColumnID, card.Rank, card.Title,
			card.BodyMD, int(card.Priority), card.Version, card.CreatedAt, card.UpdatedAt); err != nil {
			return err
		}
		if err := c.captureCardRevision(tx, card.ID, card.Version, card.Title, card.BodyMD); err != nil {
			return err
		}

		// Add labels (with validation - hard reject if label doesn't exist)
```

This is inside `createCard`, the shared transaction-level helper `CreateCard` and card import both call, so an imported card gets its version 1 captured too, the same as one created interactively.

- [ ] **Step 6: Capture before and after in `EditCard`**

In `internal/core/card.go`, `EditCard` currently reads:

```go
		if e.IfVersion != nil && *e.IfVersion != card.Version {
			return &Error{
				Code: "conflict", Exit: 4,
				Msg: fmt.Sprintf("%s changed since you read it (you: v%d, now: v%d)",
					refOrID(card), *e.IfVersion, card.Version),
				Fix: "trellis card show " + refOrID(card) + " --json",
			}
		}

		w := ProposedWrite{
```

Insert the "before" capture between the two, unconditionally whenever a title or body replacement was requested (`replaces`, already computed above this point) — `captureCardRevision`'s `INSERT OR IGNORE` makes this a no-op when that version is already retained:

```go
		if e.IfVersion != nil && *e.IfVersion != card.Version {
			return &Error{
				Code: "conflict", Exit: 4,
				Msg: fmt.Sprintf("%s changed since you read it (you: v%d, now: v%d)",
					refOrID(card), *e.IfVersion, card.Version),
				Fix: "trellis card show " + refOrID(card) + " --json",
			}
		}
		if replaces {
			if err := c.captureCardRevision(tx, card.ID, card.Version, card.Title, card.BodyMD); err != nil {
				return err
			}
		}

		w := ProposedWrite{
```

Further down, `EditCard` currently reads:

```go
		if len(pending) > 0 {
			args = append(args, card.ID)
			if _, err := tx.Exec(
				"UPDATE card SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
				return err
			}
			card.Version++
			for _, fn := range pending {
				if err := fn(); err != nil {
					return err
				}
			}
		}
		return c.cardView(tx, &card)
```

Insert the "after" capture once the version has been bumped and `card.Title`/`card.BodyMD` already hold the new values (both are mutated earlier in the function, before `pending` runs):

```go
		if len(pending) > 0 {
			args = append(args, card.ID)
			if _, err := tx.Exec(
				"UPDATE card SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
				return err
			}
			card.Version++
			for _, fn := range pending {
				if err := fn(); err != nil {
					return err
				}
			}
			if replaces {
				if err := c.captureCardRevision(tx, card.ID, card.Version, card.Title, card.BodyMD); err != nil {
					return err
				}
			}
		}
		return c.cardView(tx, &card)
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestCreateCardCapturesVersionOne|TestEditingTitleOrBodyCapturesBothVersions|TestMovingACardWritesNoRevisionButTheNextEditDoes|TestFirstEditOfAPreexistingCardCapturesItsPriorState|TestEditCardTrimsToHistoryKeep|TestHistoryKeepZeroCapturesNoCardRevisions|TestDeletingACardRemovesItsRevisions' -v`
Expected: PASS, all seven.

- [ ] **Step 8: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && go test -race ./internal/core && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/store/migrations/0013_card_revisions.sql internal/core/card_revision.go \
        internal/core/card.go internal/core/card_revision_test.go
git commit -m "feat(core): capture card revisions on create and on title/body edits

A card's revisions are rows, since a card already lives in the
database. Every write goes through Trellis, so unlike knowledge there
is no direct-edit gap: a card that predates this feature gets its
prior state captured by its first edit, the same way a pre-existing
file does. A move bumps the version without writing a revision.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `history.keep` is configurable

**Files:**
- Modify: `internal/config/config.go` (`HistoryConfig`, `Defaults`, `applyDefaults`, `GetValue`, `ValidateValue`)
- Modify: `internal/config/config_test.go`
- Modify: `internal/core/core.go` (`SetHistoryKeep`)
- Modify: `internal/cli/config.go` (`newConfigSetCmd`, `newConfigLsCmd`)
- Modify: `internal/cli/root.go` (`openCore`)
- Modify: `internal/cli/daemon.go` (daemon's `Core` construction)
- Create: `internal/cli/config_cmd_test.go`

**Interfaces:**
- Consumes: `Config`, `Defaults`, `applyDefaults`, `GetValue`, `EffectiveValue`, `SetProjectConfig` (`internal/config/config.go`); `Core.historyKeep` (Task 1).
- Produces:
  - `type HistoryConfig struct { Keep *int }` with `func (h HistoryConfig) EffectiveKeep() int`
  - `func ValidateValue(key, value string) error`
  - `func (c *Core) SetHistoryKeep(n int)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestHistoryKeepDefaultsTo100(t *testing.T) {
	if keep := Defaults().History.EffectiveKeep(); keep != 100 {
		t.Fatalf("History.EffectiveKeep() = %d, want 100", keep)
	}
}

func TestHistoryKeepZeroSurvivesApplyDefaults(t *testing.T) {
	zero := 0
	cfg := Config{History: HistoryConfig{Keep: &zero}}
	applyDefaults(&cfg)
	if got := cfg.History.EffectiveKeep(); got != 0 {
		t.Errorf("History.EffectiveKeep() = %d, want 0: applyDefaults must not resurrect the default", got)
	}
}

func TestLoadKeepsHistoryKeepZero(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("history:\n  keep: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.History.EffectiveKeep(); got != 0 {
		t.Errorf("History.EffectiveKeep() = %d, want 0", got)
	}
	if value, found := GetValue(cfg, "history.keep"); !found || value != "0" {
		t.Errorf("config ls should report 0, got %q found=%v", value, found)
	}
}

func TestLoadRejectsNegativeHistoryKeep(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("history:\n  keep: -1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(); err == nil {
		t.Error("Load with history.keep: -1, want an error")
	}
}

func TestValidateValueRejectsNegativeHistoryKeep(t *testing.T) {
	if err := ValidateValue("history.keep", "-1"); err == nil {
		t.Error("ValidateValue(history.keep, -1), want an error")
	}
	if err := ValidateValue("history.keep", "0"); err != nil {
		t.Errorf("ValidateValue(history.keep, 0): %v, want nil", err)
	}
	if err := ValidateValue("history.keep", "not-a-number"); err == nil {
		t.Error("ValidateValue(history.keep, not-a-number), want an error")
	}
	if err := ValidateValue("ui.port", "-1"); err != nil {
		t.Errorf("ValidateValue(ui.port, -1): %v, want nil: only history.keep is constrained so far", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config -run 'TestHistoryKeep|TestLoadKeepsHistoryKeepZero|TestLoadRejectsNegativeHistoryKeep|TestValidateValueRejectsNegativeHistoryKeep' -v`
Expected: the package does not compile — `HistoryConfig undefined`, `ValidateValue undefined`.

- [ ] **Step 3: Add `HistoryConfig`**

In `internal/config/config.go`, add to `type Config struct`, directly after `Search SearchConfig`:

```go
	Search  SearchConfig  `yaml:"search"`
	History HistoryConfig `yaml:"history"`
}
```

Directly after `type SearchConfig struct { ... }` and before `VectorSearchConfig`, add:

```go
// HistoryConfig controls revision retention for knowledge entries and cards.
type HistoryConfig struct {
	// Keep is a pointer because zero is a real, meaningful value -- it turns
	// capture off -- and a plain int cannot tell that apart from "absent from
	// the file", the same reason UIConfig.Enabled is a pointer. Read it
	// through EffectiveKeep rather than dereferencing.
	Keep *int `yaml:"keep"`
}

// EffectiveKeep reports how many revisions each entry and card retains. An
// unset key means the default, 100.
func (h HistoryConfig) EffectiveKeep() int {
	if h.Keep == nil {
		return 100
	}
	return *h.Keep
}
```

- [ ] **Step 4: Wire the default and `applyDefaults`**

In `Defaults()`, add to the returned `Config` literal, directly after the `Search` field:

```go
		Search: SearchConfig{
			Limit:  50,
			Method: "fts",
			Vector: VectorSearchConfig{Limit: 10, ChunkSize: 1200, ChunkOverlap: 200},
		},
		History: HistoryConfig{Keep: ptr(100)},
	}
}
```

In `applyDefaults`, add directly after the `Search.Vector.ChunkOverlap` check:

```go
	if cfg.Search.Vector.ChunkOverlap == 0 {
		cfg.Search.Vector.ChunkOverlap = defaults.Search.Vector.ChunkOverlap
	}
	if cfg.History.Keep == nil {
		cfg.History.Keep = defaults.History.Keep
	}
}
```

- [ ] **Step 5: Reject a negative value on load, and via `ValidateValue`**

In `Load()`, directly after `applyDefaults(&cfg)` and before `return cfg, nil`:

```go
	applyDefaults(&cfg)

	if *cfg.History.Keep < 0 {
		return cfg, fmt.Errorf("history.keep must not be negative, got %d", *cfg.History.Keep)
	}

	return cfg, nil
}
```

Add `"strconv"` to the file's imports, then add, directly after `GetValue`'s closing brace:

```go
// ValidateValue rejects a value for a key with semantic constraints beyond
// being a known key -- so far only history.keep, whose zero disables capture
// but whose negative values are nonsensical, not a synonym for "unlimited".
func ValidateValue(key, value string) error {
	if key != "history.keep" {
		return nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("history.keep must be a whole number, got %q", value)
	}
	if n < 0 {
		return fmt.Errorf("history.keep must not be negative, got %d", n)
	}
	return nil
}
```

- [ ] **Step 6: Add the `GetValue` case**

In `GetValue`, add directly after the `"search.vector.chunk_overlap"` case:

```go
	case "search.vector.chunk_overlap":
		return fmt.Sprintf("%d", cfg.Search.Vector.ChunkOverlap), true
	case "history.keep":
		return fmt.Sprintf("%d", cfg.History.EffectiveKeep()), true
	default:
```

- [ ] **Step 7: Run the config tests to verify they pass**

Run: `go test ./internal/config -run 'TestHistoryKeep|TestLoadKeepsHistoryKeepZero|TestLoadRejectsNegativeHistoryKeep|TestValidateValueRejectsNegativeHistoryKeep' -v`
Expected: PASS, all five.

- [ ] **Step 8: Add `SetHistoryKeep` and wire it into both `Core` construction sites**

In `internal/core/core.go`, add directly after `SetCardRequirements`:

```go
func (c *Core) SetCardRequirements(labels, tags bool) { c.requireLabels, c.requireTags = labels, tags }

// SetHistoryKeep configures how many revisions each entry and card retains.
// A negative value is rejected by config.ValidateValue and config.Load
// before it can reach here; this treats one defensively by leaving the
// current value in place.
func (c *Core) SetHistoryKeep(n int) {
	if n >= 0 {
		c.historyKeep = n
	}
}
```

In `internal/cli/root.go`, `openCore` currently ends with:

```go
	c.SetDefaultColumns(cfg.Board.DefaultColumns)
	c.SetCardRequirements(cfg.Labels.RequireOnCard, cfg.Tags.RequireOnCard)
	search := retrieval.NewService(c, db, path, cfg)
```

Insert the new call between the two:

```go
	c.SetDefaultColumns(cfg.Board.DefaultColumns)
	c.SetCardRequirements(cfg.Labels.RequireOnCard, cfg.Tags.RequireOnCard)
	c.SetHistoryKeep(cfg.History.EffectiveKeep())
	search := retrieval.NewService(c, db, path, cfg)
```

In `internal/cli/daemon.go`, the daemon's own `Core` currently has no such wiring at all (it never calls `SetLeaseTTL`, `SetDefaultColumns` or `SetCardRequirements` either — an existing gap outside this plan's scope). It reads:

```go
	c := core.New(db, core.RealClock{}, actor)
	if err := c.SyncKnowledgeSearch(parent); err != nil {
		return err
	}
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		cfg = config.Defaults()
	}
	search := retrieval.NewService(c, db, dbPath, cfg)
```

Insert the call after `cfg` is resolved, so the daemon's `Core` — which backs every web-UI write — enforces the same retention as the CLI:

```go
	c := core.New(db, core.RealClock{}, actor)
	if err := c.SyncKnowledgeSearch(parent); err != nil {
		return err
	}
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		cfg = config.Defaults()
	}
	c.SetHistoryKeep(cfg.History.EffectiveKeep())
	search := retrieval.NewService(c, db, dbPath, cfg)
```

- [ ] **Step 9: Validate on `config set`, and list `history.keep`**

In `internal/cli/config.go`, `newConfigSetCmd` currently reads:

```go
			_, found := config.GetValue(globalCfg, key)
			if !found {
				return core.ErrUsage("unknown_key",
					fmt.Sprintf("unknown config key: %q", key),
					"trellis config ls")
			}

			pctx, err := currentProject()
```

Insert the validation call between the two:

```go
			_, found := config.GetValue(globalCfg, key)
			if !found {
				return core.ErrUsage("unknown_key",
					fmt.Sprintf("unknown config key: %q", key),
					"trellis config ls")
			}
			if err := config.ValidateValue(key, value); err != nil {
				return core.ErrUsage("invalid_value", err.Error(), "trellis config set history.keep 100")
			}

			pctx, err := currentProject()
```

In `newConfigLsCmd`, `allKeys` currently ends with:

```go
				"search.vector.enabled", "search.vector.provider", "search.vector.embed_command", "search.vector.endpoint",
				"search.vector.model", "search.vector.dimension", "search.vector.limit",
			}
```

Add the new key:

```go
				"search.vector.enabled", "search.vector.provider", "search.vector.embed_command", "search.vector.endpoint",
				"search.vector.model", "search.vector.dimension", "search.vector.limit",
				"history.keep",
			}
```

- [ ] **Step 10: Write the CLI test**

Create `internal/cli/config_cmd_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigSetRejectsNegativeHistoryKeep(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "config", "set", "history.keep", "-1"); cliErrCode(err) != "invalid_value" {
		t.Errorf("err = %v, want invalid_value", err)
	}
}

func TestConfigSetHistoryKeep(t *testing.T) {
	projectEnv(t)
	runCmd(t, "config", "set", "history.keep", "10")
	out := runCmd(t, "config", "get", "history.keep", "--json")
	if !strings.Contains(out, `"value":"10"`) {
		t.Errorf("config get history.keep = %s, want value 10", out)
	}
}

// The global config.yaml file is what actually drives Core's capture
// behaviour (SetHistoryKeep is wired from it in openCore, mirroring
// lease.ttl and labels.require_on_card), not a project override in the
// database. This proves that wiring end to end.
func TestGlobalConfigHistoryKeepZeroDisablesCapture(t *testing.T) {
	projectEnv(t)
	root := os.Getenv("TRELLIS_HOME")
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("history:\n  keep: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runCmd(t, "knowledge", "new", "--title", "Off")
	out := runCmd(t, "knowledge", "show", "off", "--json")
	if !strings.Contains(out, `"slug":"off"`) {
		t.Fatalf("entry was not created:\n%s", out)
	}
	revDir := filepath.Join(root, "projects", "TEST", "knowledge", ".off.md")
	if _, err := os.Stat(revDir); !os.IsNotExist(err) {
		t.Errorf("a revision directory exists despite history.keep: 0 in config.yaml")
	}
}
```

- [ ] **Step 11: Run the tests to verify they pass**

Run: `go test ./internal/config ./internal/cli -run 'TestHistoryKeep|TestLoadKeepsHistoryKeepZero|TestLoadRejectsNegativeHistoryKeep|TestValidateValueRejectsNegativeHistoryKeep|TestConfigSetRejectsNegativeHistoryKeep|TestConfigSetHistoryKeep|TestGlobalConfigHistoryKeepZeroDisablesCapture' -v`
Expected: PASS.

- [ ] **Step 12: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 13: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/core/core.go \
        internal/cli/config.go internal/cli/root.go internal/cli/daemon.go \
        internal/cli/config_cmd_test.go
git commit -m "feat(config): history.keep controls revision retention

Zero disables capture; a negative value is rejected both when the
global file is hand-edited and, more usefully, at 'config set'. Like
lease.ttl and labels.require_on_card, it is wired into Core once from
the global file, in both the CLI's and the daemon's Core construction,
so a value set through 'config set' (a project override) is visible
through 'config get/ls' but does not yet change runtime behaviour --
the same limitation those two settings already have.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Diffing and listing retained revisions

**Files:**
- Modify: `go.mod`, `go.sum` (add `github.com/aymanbagabas/go-udiff`)
- Modify: `internal/core/revision.go` (`RevisionInfo`, `RevisionDiff`, `resolveDiffRange`, `ListKnowledgeRevisions`, `DiffKnowledge`)
- Modify: `internal/core/card_revision.go` (`CardRevisionInfo`, `ListCardRevisions`, `DiffCard`)
- Modify: `internal/core/revision_test.go`, `internal/core/card_revision_test.go`

**Interfaces:**
- Consumes: Tasks 1–3; `github.com/aymanbagabas/go-udiff`'s `func Unified(oldLabel, newLabel, old, new string) string`.
- Produces:
  - `type RevisionInfo struct { Version int64; Timestamp int64 }` (`json:"version"`, `json:"timestamp"`)
  - `type RevisionDiff struct { From, To int64; Diff string }` (`json:"from"`, `json:"to"`, `json:"diff"`)
  - `type CardRevisionInfo struct { Version, Timestamp int64; Actor string }` (`db:"version" json:"version"`, `db:"created_at" json:"timestamp"`, `db:"actor" json:"actor"`)
  - `func (c *Core) ListKnowledgeRevisions(ctx context.Context, projectID, slug string) ([]RevisionInfo, error)`
  - `func (c *Core) DiffKnowledge(ctx context.Context, projectID, slug string, from, to int64) (RevisionDiff, error)`
  - `func (c *Core) ListCardRevisions(ctx context.Context, projectID string, ref CardRef) ([]CardRevisionInfo, error)`
  - `func (c *Core) DiffCard(ctx context.Context, projectID string, ref CardRef, from, to int64) (RevisionDiff, error)`
  - `func resolveDiffRange(versions []int64, from, to int64, historyCmd string) (int64, int64, error)` — errors `revision_not_retained` (named version not retained) and `no_revisions` (nothing retained yet). Ambiguity ruled here: with neither `from` nor `to` given, `to` defaults to the latest retained version and `from` to the version immediately before it; given only one of the two, the other defaults the same way **relative to it**, not to the absolute latest — e.g. `--to 8` alone diffs the version before 8 against 8, and `--from 5` alone diffs 5 against the current latest.

- [ ] **Step 1: Add the dependency**

Per `toolchain-conventions`: this is a regular runtime dependency (imported Go code, not a build-time tool), so it is added with plain `go get`, pinned to an exact version, followed by `go mod tidy` so `go.sum` and the indirect-requires block stay consistent.

Run:
```bash
go get github.com/aymanbagabas/go-udiff@v0.4.1
go mod tidy
```
Expected: `go.mod` gains a `require github.com/aymanbagabas/go-udiff v0.4.1` line; `go.sum` gains matching entries.

- [ ] **Step 2: Write the failing tests**

Append to `internal/core/revision_test.go`:

```go
func TestListKnowledgeRevisionsNewestFirst(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Log", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	version := doc.Version
	for i := 2; i <= 3; i++ {
		edited, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, fmt.Sprintf("v%d\n", i), &version)
		if err != nil {
			t.Fatalf("EditKnowledge v%d: %v", i, err)
		}
		version = edited.Version
	}
	revs, err := c.ListKnowledgeRevisions(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("ListKnowledgeRevisions: %v", err)
	}
	if len(revs) != 3 {
		t.Fatalf("%d revisions, want 3", len(revs))
	}
	for i, want := range []int64{3, 2, 1} {
		if revs[i].Version != want {
			t.Errorf("revs[%d].Version = %d, want %d (newest first)", i, revs[i].Version, want)
		}
	}
}

func TestDiffKnowledgeDefaultsToPreviousAgainstLatest(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Notes", Body: "line one\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "line one\nline two\n", &doc.Version); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	diff, err := c.DiffKnowledge(t.Context(), p.ID, doc.Slug, 0, 0)
	if err != nil {
		t.Fatalf("DiffKnowledge: %v", err)
	}
	if diff.From != 1 || diff.To != 2 {
		t.Errorf("from/to = %d/%d, want 1/2", diff.From, diff.To)
	}
	if !strings.Contains(diff.Diff, "+line two") {
		t.Errorf("diff = %q, want it to add line two", diff.Diff)
	}
}

func TestDiffKnowledgeRejectsAnUnretainedVersion(t *testing.T) {
	c, p, _ := kbCore(t)
	c.historyKeep = 1
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Tight", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", &doc.Version); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	_, err = c.DiffKnowledge(t.Context(), p.ID, doc.Slug, 1, 2)
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "revision_not_retained" {
		t.Fatalf("err = %v, want revision_not_retained", err)
	}
	if !strings.Contains(err.Error(), "2-2") {
		t.Errorf("error %q does not name the retained range", err.Error())
	}
}
```

Add `"fmt"` to the file's imports (if not already present from Task 1).

Append to `internal/core/card_revision_test.go`:

```go
func TestListCardRevisionsCarriesTheActor(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	revs, err := c.ListCardRevisions(t.Context(), p.ID, CardRef{UUID: card.ID})
	if err != nil {
		t.Fatalf("ListCardRevisions: %v", err)
	}
	if len(revs) != 1 || revs[0].Actor != c.actor {
		t.Fatalf("revs = %+v, want one revision by %s", revs, c.actor)
	}
}

func TestDiffCardRendersTitleAndBody(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship", Body: "draft"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	newBody := "final"
	edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &newBody, IfVersion: &card.Version})
	if err != nil {
		t.Fatalf("EditCard: %v", err)
	}
	diff, err := c.DiffCard(t.Context(), p.ID, CardRef{UUID: card.ID}, 0, 0)
	if err != nil {
		t.Fatalf("DiffCard: %v", err)
	}
	if diff.From != card.Version || diff.To != edited.Version {
		t.Errorf("from/to = %d/%d, want %d/%d", diff.From, diff.To, card.Version, edited.Version)
	}
	if !strings.Contains(diff.Diff, "-draft") || !strings.Contains(diff.Diff, "+final") {
		t.Errorf("diff = %q, want draft removed and final added", diff.Diff)
	}
	if !strings.Contains(diff.Diff, "# Ship") {
		t.Errorf("diff = %q, want the rendering to include the title", diff.Diff)
	}
}
```

Add `"errors"` and `"strings"` to `internal/core/card_revision_test.go`'s imports.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestListKnowledgeRevisionsNewestFirst|TestDiffKnowledgeDefaultsToPreviousAgainstLatest|TestDiffKnowledgeRejectsAnUnretainedVersion|TestListCardRevisionsCarriesTheActor|TestDiffCardRendersTitleAndBody' -v`
Expected: the package does not compile — `ListKnowledgeRevisions undefined`, `DiffCard undefined`.

- [ ] **Step 4: Add `RevisionInfo`, `RevisionDiff`, `resolveDiffRange` and the knowledge functions**

Append to `internal/core/revision.go`. Add `"context"` and `"fmt"` to its imports, and add the module dependency's import path:

```go
	"github.com/aymanbagabas/go-udiff"
```

```go
// RevisionInfo is one retained version of a knowledge entry, newest first.
type RevisionInfo struct {
	Version   int64 `json:"version"`
	Timestamp int64 `json:"timestamp"`
}

// RevisionDiff is a unified diff between two retained versions.
type RevisionDiff struct {
	From int64  `json:"from"`
	To   int64  `json:"to"`
	Diff string `json:"diff"`
}

// resolveDiffRange picks the two versions a diff compares, from a sorted
// (ascending) list of retained versions. With neither from nor to given, to
// is the latest retained version and from is the one immediately before it.
// Given only one, the other is resolved the same way relative to it: --to 8
// alone diffs the version before 8 against 8; --from 5 alone diffs 5 against
// the current latest.
func resolveDiffRange(versions []int64, from, to int64, historyCmd string) (int64, int64, error) {
	retained := func(v int64) bool {
		_, ok := slices.BinarySearch(versions, v)
		return ok
	}
	rangeMsg := func() string {
		if len(versions) == 0 {
			return "no revisions are retained"
		}
		return fmt.Sprintf("the retained range is %d-%d", versions[0], versions[len(versions)-1])
	}
	if to != 0 {
		if !retained(to) {
			return 0, 0, ErrUsage("revision_not_retained",
				fmt.Sprintf("version %d is not retained; %s", to, rangeMsg()), historyCmd)
		}
	} else {
		if len(versions) == 0 {
			return 0, 0, ErrUsage("no_revisions", "no revisions are retained", historyCmd)
		}
		to = versions[len(versions)-1]
	}
	if from != 0 {
		if !retained(from) {
			return 0, 0, ErrUsage("revision_not_retained",
				fmt.Sprintf("version %d is not retained; %s", from, rangeMsg()), historyCmd)
		}
	} else {
		idx, _ := slices.BinarySearch(versions, to)
		if idx == 0 {
			return 0, 0, ErrUsage("no_earlier_revision",
				fmt.Sprintf("version %d has no earlier retained revision; %s", to, rangeMsg()), historyCmd)
		}
		from = versions[idx-1]
	}
	return from, to, nil
}

// ListKnowledgeRevisions lists an entry's retained versions, newest first.
// Knowledge revisions carry no actor: the entry file has no author field, and
// a direct edit has no Trellis actor at all.
func (c *Core) ListKnowledgeRevisions(ctx context.Context, projectID, slug string) ([]RevisionInfo, error) {
	doc, err := c.LoadKnowledge(ctx, projectID, slug)
	if err != nil {
		return nil, err
	}
	versions, err := sortedRevisionVersions(revisionDir(doc.Path))
	if err != nil {
		return nil, err
	}
	out := make([]RevisionInfo, len(versions))
	for i, v := range versions {
		st, err := os.Stat(revisionFilePath(doc.Path, v))
		if err != nil {
			return nil, err
		}
		out[len(versions)-1-i] = RevisionInfo{Version: v, Timestamp: st.ModTime().UnixMilli()}
	}
	return out, nil
}

// DiffKnowledge returns a unified diff between two retained versions of an
// entry's whole file, frontmatter included.
func (c *Core) DiffKnowledge(ctx context.Context, projectID, slug string, from, to int64) (RevisionDiff, error) {
	doc, err := c.LoadKnowledge(ctx, projectID, slug)
	if err != nil {
		return RevisionDiff{}, err
	}
	versions, err := sortedRevisionVersions(revisionDir(doc.Path))
	if err != nil {
		return RevisionDiff{}, err
	}
	from, to, err = resolveDiffRange(versions, from, to, "trellis knowledge history "+doc.Slug)
	if err != nil {
		return RevisionDiff{}, err
	}
	fromRaw, err := os.ReadFile(revisionFilePath(doc.Path, from))
	if err != nil {
		return RevisionDiff{}, err
	}
	toRaw, err := os.ReadFile(revisionFilePath(doc.Path, to))
	if err != nil {
		return RevisionDiff{}, err
	}
	diff := udiff.Unified(
		fmt.Sprintf("%s@v%d", doc.Slug, from), fmt.Sprintf("%s@v%d", doc.Slug, to),
		string(fromRaw), string(toRaw))
	return RevisionDiff{From: from, To: to, Diff: diff}, nil
}
```

- [ ] **Step 5: Add `CardRevisionInfo`, `ListCardRevisions` and `DiffCard`**

Append to `internal/core/card_revision.go`. Add `"context"` and `"fmt"` to its imports and the same `udiff` import:

```go
// CardRevisionInfo is one retained version of a card, newest first.
type CardRevisionInfo struct {
	Version   int64  `db:"version" json:"version"`
	Timestamp int64  `db:"created_at" json:"timestamp"`
	Actor     string `db:"actor" json:"actor"`
}

// ListCardRevisions lists a card's retained versions, newest first.
func (c *Core) ListCardRevisions(ctx context.Context, projectID string, ref CardRef) ([]CardRevisionInfo, error) {
	var card Card
	out := []CardRevisionInfo{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		return tx.Select(&out,
			`SELECT version, created_at, actor FROM card_revision WHERE card_id = ? ORDER BY version DESC`, card.ID)
	})
	return out, err
}

// DiffCard returns a unified diff between two retained versions of a card,
// each rendered as "# <title>\n\n<body>".
func (c *Core) DiffCard(ctx context.Context, projectID string, ref CardRef, from, to int64) (RevisionDiff, error) {
	var card Card
	var rows []struct {
		Version int64  `db:"version"`
		Title   string `db:"title"`
		Body    string `db:"body_md"`
	}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		return tx.Select(&rows,
			`SELECT version, title, body_md FROM card_revision WHERE card_id = ? ORDER BY version`, card.ID)
	})
	if err != nil {
		return RevisionDiff{}, err
	}
	versions := make([]int64, len(rows))
	rendered := map[int64]string{}
	for i, r := range rows {
		versions[i] = r.Version
		rendered[r.Version] = "# " + r.Title + "\n\n" + r.Body
	}
	from, to, err = resolveDiffRange(versions, from, to, "trellis card history "+card.Ref)
	if err != nil {
		return RevisionDiff{}, err
	}
	diff := udiff.Unified(
		fmt.Sprintf("%s@v%d", card.Ref, from), fmt.Sprintf("%s@v%d", card.Ref, to),
		rendered[from], rendered[to])
	return RevisionDiff{From: from, To: to, Diff: diff}, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestListKnowledgeRevisionsNewestFirst|TestDiffKnowledgeDefaultsToPreviousAgainstLatest|TestDiffKnowledgeRejectsAnUnretainedVersion|TestListCardRevisionsCarriesTheActor|TestDiffCardRendersTitleAndBody' -v`
Expected: PASS, all five.

- [ ] **Step 7: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && go test -race ./internal/core && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/core/revision.go internal/core/card_revision.go \
        internal/core/revision_test.go internal/core/card_revision_test.go
git commit -m "feat(core): list and diff retained revisions

Diffing uses github.com/aymanbagabas/go-udiff, a maintained,
dependency-free unified-diff library -- github.com/pmezard/go-difflib
is archived and was already unused dead weight in the module graph.
A diff defaults to the previous retained version against the latest;
naming an unretained version is an error that states the retained
range.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `knowledge history`, `knowledge diff`, `card history`, `card diff`

**Files:**
- Modify: `internal/cli/knowledge.go` (`newKnowledgeCmd`, `newKnowledgeHistoryCmd`, `newKnowledgeDiffCmd`)
- Modify: `internal/cli/card.go` (`newCardCmd`, `newCardHistoryCmd`, `newCardDiffCmd`)
- Create: `internal/cli/history_cmd_test.go`

**Interfaces:**
- Consumes: `ListKnowledgeRevisions`, `DiffKnowledge`, `ListCardRevisions`, `DiffCard`, `RevisionInfo`, `CardRevisionInfo`, `RevisionDiff` (Task 5); `withBoard`, `Emit`, `msDate` (`internal/cli/root.go`, `internal/cli/card.go`, `internal/cli/knowledge.go`).
- Produces: `trellis knowledge history <slug>`, `trellis knowledge diff <slug> [--from N] [--to M]`, `trellis card history <card>`, `trellis card diff <card> [--from N] [--to M]`.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/history_cmd_test.go`:

```go
package cli

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

func TestKnowledgeHistoryAndDiff(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Notes", "--body", "line one")
	runCmd(t, "knowledge", "edit", "notes", "--body", "line one\nline two", "--if-version", "1")

	history := runCmd(t, "knowledge", "history", "notes", "--json")
	if !strings.Contains(history, `"version":2`) || !strings.Contains(history, `"version":1`) {
		t.Fatalf("history = %s, want both versions", history)
	}

	diff := runCmd(t, "knowledge", "diff", "notes", "--json")
	if !strings.Contains(diff, `"from":1`) || !strings.Contains(diff, `"to":2`) {
		t.Fatalf("diff = %s, want from 1 to 2", diff)
	}
	if !strings.Contains(diff, "line two") {
		t.Fatalf("diff = %s, want it to mention the added line", diff)
	}
}

func TestKnowledgeDiffRejectsAnUnretainedVersion(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Solo")

	if _, err := runCmdErr(t, "knowledge", "diff", "solo", "--from", "9", "--to", "9"); cliErrCode(err) != "revision_not_retained" {
		t.Errorf("err = %v, want revision_not_retained", err)
	}
}

func TestCardHistoryAndDiff(t *testing.T) {
	projectEnv(t)
	created := runCmd(t, "card", "new", "--title", "Ship", "--body", "draft", "--json")
	var card core.Card
	if err := json.Unmarshal([]byte(created), &card); err != nil {
		t.Fatalf("decode created card: %v", err)
	}
	runCmd(t, "card", "edit", card.Ref, "--body", "final", "--if-version", "1")

	history := runCmd(t, "card", "history", card.Ref, "--json")
	if !strings.Contains(history, `"actor"`) {
		t.Fatalf("history = %s, want an actor field", history)
	}

	diff := runCmd(t, "card", "diff", card.Ref, "--json")
	if !strings.Contains(diff, "-draft") || !strings.Contains(diff, "+final") {
		t.Fatalf("diff = %s, want draft removed and final added", diff)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli -run 'TestKnowledgeHistoryAndDiff|TestKnowledgeDiffRejectsAnUnretainedVersion|TestCardHistoryAndDiff' -v`
Expected: `unknown command "history" for "trellis knowledge"` (and the same for `diff`, and for `card`).

- [ ] **Step 3: Add the knowledge subcommands**

In `internal/cli/knowledge.go`, `newKnowledgeCmd` currently reads:

```go
	cmd.AddCommand(
		newKnowledgeNewCmd(), newKnowledgeShowCmd(), newKnowledgeLsCmd(), newKnowledgeEditCmd(),
		newKnowledgeRmCmd(), newKnowledgePinCmd(), newKnowledgePinsCmd(), newKnowledgeLintCmd(),
		newKnowledgeNominateCmd(), newKnowledgeNominationsCmd(), newKnowledgeEscalateCmd(),
		newKnowledgeDemoteCmd(), newKnowledgeVerifyCmd(), newKnowledgeHealthCmd(),
		newKnowledgeUptakeCmd())
	return cmd
}
```

Replace with:

```go
	cmd.AddCommand(
		newKnowledgeNewCmd(), newKnowledgeShowCmd(), newKnowledgeLsCmd(), newKnowledgeEditCmd(),
		newKnowledgeRmCmd(), newKnowledgePinCmd(), newKnowledgePinsCmd(), newKnowledgeLintCmd(),
		newKnowledgeNominateCmd(), newKnowledgeNominationsCmd(), newKnowledgeEscalateCmd(),
		newKnowledgeDemoteCmd(), newKnowledgeVerifyCmd(), newKnowledgeHealthCmd(),
		newKnowledgeUptakeCmd(), newKnowledgeHistoryCmd(), newKnowledgeDiffCmd())
	return cmd
}
```

Add, directly after `newKnowledgeHealthCmd`'s closing brace:

```go
func newKnowledgeHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <slug>",
		Short: "List an entry's retained revisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				revs, err := app.Core.ListKnowledgeRevisions(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"revisions": revs}, func() string {
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, r := range revs {
						fmt.Fprintf(w, "%d\t%s\n", r.Version, msDate(r.Timestamp))
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newKnowledgeDiffCmd() *cobra.Command {
	var from, to int64
	cmd := &cobra.Command{
		Use:   "diff <slug>",
		Short: "Show a unified diff between two retained revisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				d, err := app.Core.DiffKnowledge(cmd.Context(), app.Project.ID, args[0], from, to)
				if err != nil {
					return err
				}
				return Emit(cmd, d, func() string { return d.Diff })
			})
		},
	}
	cmd.Flags().Int64Var(&from, "from", 0, "earlier version (default: the one before --to)")
	cmd.Flags().Int64Var(&to, "to", 0, "later version (default: the latest retained)")
	return cmd
}
```

- [ ] **Step 4: Add the card subcommands**

In `internal/cli/card.go`, `newCardCmd` currently reads:

```go
	cmd.AddCommand(
		newCardNewCmd(), newCardShowCmd(), newCardLsCmd(), newCardMoveCmd(), newCardEditCmd(), newCardRmCmd(),
		newCardClaimCmd(), newCardReleaseCmd(), newCardRenewCmd(), newCardNextCmd(), newCardNoteCmd(),
		newCardArchiveCmd(), newCardBlockCmd(), newCardImportCmd())
	return cmd
}
```

Replace with:

```go
	cmd.AddCommand(
		newCardNewCmd(), newCardShowCmd(), newCardLsCmd(), newCardMoveCmd(), newCardEditCmd(), newCardRmCmd(),
		newCardClaimCmd(), newCardReleaseCmd(), newCardRenewCmd(), newCardNextCmd(), newCardNoteCmd(),
		newCardArchiveCmd(), newCardBlockCmd(), newCardImportCmd(), newCardHistoryCmd(), newCardDiffCmd())
	return cmd
}
```

Add, directly after `newCardShowCmd`'s closing brace:

```go
func newCardHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <card>",
		Short: "List a card's retained revisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				revs, err := app.Core.ListCardRevisions(cmd.Context(), app.Project.ID, core.ParseCardRef(args[0]))
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"revisions": revs}, func() string {
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, r := range revs {
						fmt.Fprintf(w, "%d\t%s\t%s\n", r.Version, msDate(r.Timestamp), r.Actor)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newCardDiffCmd() *cobra.Command {
	var from, to int64
	cmd := &cobra.Command{
		Use:   "diff <card>",
		Short: "Show a unified diff between two retained revisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				d, err := app.Core.DiffCard(cmd.Context(), app.Project.ID, core.ParseCardRef(args[0]), from, to)
				if err != nil {
					return err
				}
				return Emit(cmd, d, func() string { return d.Diff })
			})
		},
	}
	cmd.Flags().Int64Var(&from, "from", 0, "earlier version (default: the one before --to)")
	cmd.Flags().Int64Var(&to, "to", 0, "later version (default: the latest retained)")
	return cmd
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli -run 'TestKnowledgeHistoryAndDiff|TestKnowledgeDiffRejectsAnUnretainedVersion|TestCardHistoryAndDiff' -v`
Expected: PASS, all three.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/knowledge.go internal/cli/card.go internal/cli/history_cmd_test.go
git commit -m "feat(cli): knowledge history/diff and card history/diff

Both entities get the same two verbs: history lists retained versions
newest first (cards also carry an actor), and diff renders a unified
diff, defaulting to the previous retained version against the latest.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Web API — the four GET routes

**Files:**
- Modify: `internal/ui/server.go` (`registerRoutes`)
- Create: `internal/ui/history.go`
- Create: `internal/ui/history_test.go`

**Interfaces:**
- Consumes: `ListKnowledgeRevisions`, `DiffKnowledge`, `ListCardRevisions`, `DiffCard` (Task 5); `writeJSON`, `s.error`, `s.coreError`, `protectedHandler` (`internal/ui/server.go`, `internal/ui/security.go`).
- Produces routes:
  - `GET /api/p/{key}/knowledge/{slug}/history`
  - `GET /api/p/{key}/knowledge/{slug}/diff?from=N&to=M`
  - `GET /api/p/{key}/cards/{card}/history`
  - `GET /api/p/{key}/cards/{card}/diff?from=N&to=M`

  Path shape chosen (ruled by the coordinator; matches what this task already derives from the code): cards already have an unscoped, board-agnostic route, `GET /api/p/{key}/cards/{card}` → `handleCardDetail` — a card's identity (its `seq`, unique per project) needs no board to resolve. `history`/`diff` extend that exact same unscoped path. Knowledge has no board-scoped equivalent to extend (`LoadKnowledge` already takes only `projectID` and `slug`, no board), so `history`/`diff` extend the existing unscoped project listing path, `GET /api/p/{key}/knowledge` → `handleProjectKnowledgeList`, with `/{slug}/...`. Neither new route needs a `/b/{board}/...` variant: nothing about a revision or a diff is board-scoped, and the spec's own path list (`GET /api/p/{key}/knowledge/{slug}/history`, etc.) already reflects exactly this shape.

  `{slug}` resolution: `ListKnowledgeRevisions`/`DiffKnowledge` (Task 5) both start with `c.LoadKnowledge(ctx, projectID, slug)`, which delegates to `loadDoc`'s existing query — `SELECT * FROM knowledge WHERE slug = ? AND (project_id = ? OR global = 1) ORDER BY global LIMIT 1` — so a name already resolves the calling project's own entry first and only falls back to the global vault when the project has none by that slug. No new resolution logic is needed in the handlers for this; they get it by construction from reusing `LoadKnowledge`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/history_test.go`:

```go
package ui

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

func TestKnowledgeHistoryAndDiffRoutes(t *testing.T) {
	s, c, p := artifactTestServer(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, core.NewKnowledge{Title: "Notes", Body: "line one\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "line one\nline two\n", &doc.Version); err != nil {
		t.Fatal(err)
	}

	history := serve(s, http.MethodGet, "/api/p/"+p.Key+"/knowledge/"+doc.Slug+"/history", nil)
	if history.Code != http.StatusOK {
		t.Fatalf("history status = %d, body = %s", history.Code, history.Body)
	}
	var revs []core.RevisionInfo
	if err := json.Unmarshal(history.Body.Bytes(), &revs); err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Version != 2 || revs[1].Version != 1 {
		t.Fatalf("revisions = %+v, want [2 1]", revs)
	}

	diff := serve(s, http.MethodGet, "/api/p/"+p.Key+"/knowledge/"+doc.Slug+"/diff", nil)
	if diff.Code != http.StatusOK || !strings.Contains(diff.Body.String(), "line two") {
		t.Fatalf("diff status = %d, body = %s", diff.Code, diff.Body)
	}

	explicit := serve(s, http.MethodGet, "/api/p/"+p.Key+"/knowledge/"+doc.Slug+"/diff?from=1&to=2", nil)
	if explicit.Code != http.StatusOK || !strings.Contains(explicit.Body.String(), `"from":1`) {
		t.Fatalf("explicit diff status = %d, body = %s", explicit.Code, explicit.Body)
	}

	badRange := serve(s, http.MethodGet, "/api/p/"+p.Key+"/knowledge/"+doc.Slug+"/diff?from=9&to=9", nil)
	if badRange.Code != http.StatusBadRequest {
		t.Errorf("unretained version status = %d, want 400", badRange.Code)
	}
}

func TestCardHistoryAndDiffRoutes(t *testing.T) {
	s, c, p := artifactTestServer(t)
	board, err := c.SelectBoard(t.Context(), p.ID, "default")
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(t.Context(), p.ID, board.ID, core.NewCard{Title: "Ship", Body: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	newBody := "final"
	if _, err := c.EditCard(t.Context(), p.ID, core.CardRef{UUID: card.ID}, core.CardEdit{Body: &newBody, IfVersion: &card.Version}); err != nil {
		t.Fatal(err)
	}

	history := serve(s, http.MethodGet, "/api/p/"+p.Key+"/cards/"+card.Ref+"/history", nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"actor"`) {
		t.Fatalf("history status = %d, body = %s", history.Code, history.Body)
	}

	diff := serve(s, http.MethodGet, "/api/p/"+p.Key+"/cards/"+card.Ref+"/diff", nil)
	if diff.Code != http.StatusOK || !strings.Contains(diff.Body.String(), "-draft") || !strings.Contains(diff.Body.String(), "+final") {
		t.Fatalf("diff status = %d, body = %s", diff.Code, diff.Body)
	}
}

// The four routes sit behind protectedHandler like every other /api/ route.
func TestHistoryRoutesAreProtected(t *testing.T) {
	s, c, p := artifactTestServer(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, core.NewKnowledge{Title: "Guarded"})
	if err != nil {
		t.Fatal(err)
	}
	h := s.protectedHandler("127.0.0.1:0")

	do := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/p/"+p.Key+"/knowledge/"+doc.Slug+"/history", nil)
		req.Host = "127.0.0.1:0"
		if token != "" {
			req.Header.Set("X-Trellis-Token", token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := do(""); got != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", got)
	}
	if got := do(s.token); got != http.StatusOK {
		t.Errorf("with token: %d, want 200", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui -run 'TestKnowledgeHistoryAndDiffRoutes|TestCardHistoryAndDiffRoutes|TestHistoryRoutesAreProtected' -v`
Expected: 404s — the routes do not exist yet.

- [ ] **Step 3: Add the handlers**

Create `internal/ui/history.go`:

```go
package ui

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/mtch3n/trellis/internal/core"
)

func (s *Server) projectByKey(ctx context.Context, key string) (core.Project, error) {
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, key); err != nil {
		return core.Project{}, core.ErrNotFound("project_not_found", "project not found", "")
	}
	return p, nil
}

// parseDiffRange reads ?from=&to=, both optional. A present-but-invalid value
// is rejected here rather than silently treated as absent.
func parseDiffRange(r *http.Request) (from, to int64, err error) {
	if v := r.URL.Query().Get("from"); v != "" {
		if from, err = strconv.ParseInt(v, 10, 64); err != nil {
			return 0, 0, fmt.Errorf("invalid from: %q", v)
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if to, err = strconv.ParseInt(v, 10, 64); err != nil {
			return 0, 0, fmt.Errorf("invalid to: %q", v)
		}
	}
	return from, to, nil
}

func (s *Server) handleKnowledgeHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	revs, err := s.core.ListKnowledgeRevisions(ctx, p.ID, r.PathValue("slug"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, revs)
}

func (s *Server) handleKnowledgeDiff(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	from, to, err := parseDiffRange(r)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := s.core.DiffKnowledge(ctx, p.ID, r.PathValue("slug"), from, to)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleCardHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	revs, err := s.core.ListCardRevisions(ctx, p.ID, core.ParseCardRef(r.PathValue("card")))
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, revs)
}

func (s *Server) handleCardDiff(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	from, to, err := parseDiffRange(r)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := s.core.DiffCard(ctx, p.ID, core.ParseCardRef(r.PathValue("card")), from, to)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
```

- [ ] **Step 4: Register the routes**

In `internal/ui/server.go`, `registerRoutes` currently has:

```go
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/knowledge", s.handleKnowledgeList)
	s.mux.HandleFunc("GET /api/p/{key}/knowledge", s.handleProjectKnowledgeList)
	s.mux.HandleFunc("GET /api/p/{key}/artifacts/{name}", s.handleArtifact)
```

Insert two routes between the last two:

```go
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/knowledge", s.handleKnowledgeList)
	s.mux.HandleFunc("GET /api/p/{key}/knowledge", s.handleProjectKnowledgeList)
	s.mux.HandleFunc("GET /api/p/{key}/knowledge/{slug}/history", s.handleKnowledgeHistory)
	s.mux.HandleFunc("GET /api/p/{key}/knowledge/{slug}/diff", s.handleKnowledgeDiff)
	s.mux.HandleFunc("GET /api/p/{key}/artifacts/{name}", s.handleArtifact)
```

And, directly after `GET /api/p/{key}/cards/{card}`:

```go
	s.mux.HandleFunc("GET /api/p/{key}/cards/{card}", s.handleCardDetail)
```

Insert:

```go
	s.mux.HandleFunc("GET /api/p/{key}/cards/{card}", s.handleCardDetail)
	s.mux.HandleFunc("GET /api/p/{key}/cards/{card}/history", s.handleCardHistory)
	s.mux.HandleFunc("GET /api/p/{key}/cards/{card}/diff", s.handleCardDiff)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ui -run 'TestKnowledgeHistoryAndDiffRoutes|TestCardHistoryAndDiffRoutes|TestHistoryRoutesAreProtected' -v`
Expected: PASS, all three.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/server.go internal/ui/history.go internal/ui/history_test.go
git commit -m "feat(ui): serve knowledge and card history and diff over HTTP

Four GET routes under /api/, behind the existing protectedHandler:
{slug}/history and {slug}/diff extend knowledge's existing unscoped
project route, and {card}/history and {card}/diff extend the card
route that already resolves a card without a board.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Housekeeping — `maintenance prune` and `knowledge health`

**Files:**
- Modify: `internal/core/maintenance.go` (`PruneRevisions`, `PruneOrphanHistory`)
- Modify: `internal/core/health.go` (`RevisionHealth`, `Health`)
- Create: `internal/core/maintenance_test.go`
- Modify: `internal/cli/maintenance.go` (`newMaintenancePruneCmd`)
- Create: `internal/cli/maintenance_cmd_test.go`

**Interfaces:**
- Consumes: `trimRevisions`, `revisionDir`, `sortedRevisionVersions` (`internal/core/revision.go`); `trimCardRevisions` (`internal/core/card_revision.go`); `Health`, `HealthLine` (`internal/core/health.go`).
- Produces:
  - `func (c *Core) PruneRevisions(ctx context.Context) (int64, error)`
  - `func (c *Core) PruneOrphanHistory(ctx context.Context) (int64, error)`
  - `func (c *Core) RevisionHealth(ctx context.Context, projectID string) (revisions, orphaned int, err error)`
  - Two new `HealthLine`s: `"revisions"` and `"orphaned revision directories"`.
  - `trellis maintenance prune --revisions` and `--orphan-history`, neither requiring `--before`.

- [ ] **Step 1: Write the failing tests**

Create `internal/core/maintenance_test.go`:

```go
package core

import (
	"fmt"
	"os"
	"testing"
)

func TestPruneRevisionsTrimsKnowledgeAndCards(t *testing.T) {
	c, p, b := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Log", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	version := doc.Version
	for i := 2; i <= 4; i++ {
		edited, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, fmt.Sprintf("v%d\n", i), &version)
		if err != nil {
			t.Fatalf("EditKnowledge v%d: %v", i, err)
		}
		version = edited.Version
	}
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	cardVersion := card.Version
	for i := 2; i <= 4; i++ {
		body := fmt.Sprintf("v%d", i)
		edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &body, IfVersion: &cardVersion})
		if err != nil {
			t.Fatalf("EditCard v%d: %v", i, err)
		}
		cardVersion = edited.Version
	}

	c.historyKeep = 2
	n, err := c.PruneRevisions(t.Context())
	if err != nil {
		t.Fatalf("PruneRevisions: %v", err)
	}
	if n == 0 {
		t.Fatal("PruneRevisions removed nothing")
	}
	entries, err := os.ReadDir(revisionDir(doc.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("%d knowledge revisions after pruning to keep=2, want 2", len(entries))
	}
	var cardRevs int
	if err := c.db.Get(&cardRevs, `SELECT COUNT(*) FROM card_revision WHERE card_id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}
	if cardRevs != 2 {
		t.Errorf("%d card revisions after pruning to keep=2, want 2", cardRevs)
	}
}

func TestPruneOrphanHistoryRemovesDirectoriesWithNoEntryFile(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Gone", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if err := os.Remove(doc.Path); err != nil {
		t.Fatal(err)
	}

	n, err := c.PruneOrphanHistory(t.Context())
	if err != nil {
		t.Fatalf("PruneOrphanHistory: %v", err)
	}
	if n != 1 {
		t.Fatalf("removed %d, want 1", n)
	}
	if _, err := os.Stat(revisionDir(doc.Path)); !os.IsNotExist(err) {
		t.Errorf("revision directory still exists after pruning")
	}
}

func TestHealthReportsRevisionsAndOrphans(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Watched", Body: "v1\n"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	orphan, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Orphan", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if err := os.Remove(orphan.Path); err != nil {
		t.Fatal(err)
	}

	lines, err := c.Health(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	var revisions, orphaned int
	for _, l := range lines {
		switch l.What {
		case "revisions":
			revisions = l.Count
		case "orphaned revision directories":
			orphaned = l.Count
		}
	}
	if revisions != 1 {
		t.Errorf("revisions = %d, want 1 (only the watched entry counts)", revisions)
	}
	if orphaned != 1 {
		t.Errorf("orphaned = %d, want 1", orphaned)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestPruneRevisionsTrimsKnowledgeAndCards|TestPruneOrphanHistoryRemovesDirectoriesWithNoEntryFile|TestHealthReportsRevisionsAndOrphans' -v`
Expected: the package does not compile — `PruneRevisions undefined`.

- [ ] **Step 3: Add `PruneRevisions` and `PruneOrphanHistory`**

In `internal/core/maintenance.go`, change the imports to:

```go
import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jmoiron/sqlx"
)
```

Append, after `Compact`:

```go
// PruneRevisions trims every knowledge entry's and every card's revisions
// down to history.keep. Capture already enforces the limit going forward;
// this is for after lowering it, when the excess would otherwise wait for
// the next write. It returns the number of revisions removed.
func (c *Core) PruneRevisions(ctx context.Context) (int64, error) {
	var docs []Knowledge
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&docs, `SELECT * FROM knowledge`)
	}); err != nil {
		return 0, err
	}
	var total int64
	for _, d := range docs {
		n, err := trimRevisions(d.Path, c.historyKeep)
		if err != nil {
			return total, err
		}
		total += int64(n)
	}

	var cardIDs []string
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&cardIDs, `SELECT DISTINCT card_id FROM card_revision`)
	}); err != nil {
		return total, err
	}
	for _, id := range cardIDs {
		if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
			var before, after int
			if err := tx.Get(&before, `SELECT COUNT(*) FROM card_revision WHERE card_id = ?`, id); err != nil {
				return err
			}
			if err := trimCardRevisions(tx, id, c.historyKeep); err != nil {
				return err
			}
			if err := tx.Get(&after, `SELECT COUNT(*) FROM card_revision WHERE card_id = ?`, id); err != nil {
				return err
			}
			total += int64(before - after)
			return nil
		}); err != nil {
			return total, err
		}
	}
	return total, nil
}

// PruneOrphanHistory removes revision directories whose entry file is gone --
// what deleting a file outside Trellis leaves behind, since nothing else
// notices. It returns the number of directories removed.
func (c *Core) PruneOrphanHistory(ctx context.Context) (int64, error) {
	var docs []Knowledge
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&docs, `SELECT * FROM knowledge`)
	}); err != nil {
		return 0, err
	}
	var removed int64
	for _, d := range docs {
		dir := revisionDir(d.Path)
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return removed, err
		}
		if _, err := os.Stat(d.Path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		if err := os.RemoveAll(dir); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
```

- [ ] **Step 4: Add `RevisionHealth` and extend `Health`**

In `internal/core/health.go`, change the imports to:

```go
import (
	"context"
	"errors"
	"os"

	"github.com/jmoiron/sqlx"
)
```

Append, after `readWindowMS`:

```go
// RevisionHealth counts a project's retained knowledge revisions and the
// revision directories a file deleted outside Trellis leaves behind: the row
// still names a path, but the path is gone.
func (c *Core) RevisionHealth(ctx context.Context, projectID string) (revisions, orphaned int, err error) {
	var docs []Knowledge
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&docs, `SELECT * FROM knowledge WHERE project_id = ?`, projectID)
	}); err != nil {
		return 0, 0, err
	}
	for _, d := range docs {
		entries, derr := os.ReadDir(revisionDir(d.Path))
		if errors.Is(derr, os.ErrNotExist) {
			continue
		}
		if derr != nil {
			return 0, 0, derr
		}
		if _, ferr := os.Stat(d.Path); errors.Is(ferr, os.ErrNotExist) {
			orphaned++
			continue
		} else if ferr != nil {
			return 0, 0, ferr
		}
		revisions += len(entries)
	}
	return revisions, orphaned, nil
}
```

`Health` currently reads:

```go
	return []HealthLine{
		{What: "entries", Count: total, Fix: "trellis knowledge ls"},
		{What: "never read in " + itoa(ReadWindowDays) + "d", Count: cold, Fix: "trellis knowledge ls --cold"},
		{What: "stale pinned recaps", Count: stale, Fix: "trellis knowledge pins --stale"},
		{What: "orphans (no links)", Count: byKind["orphan"], Fix: "trellis knowledge lint"},
		{What: "stubs", Count: byKind["stub"], Fix: "trellis knowledge lint"},
		{What: "broken anchors", Count: byKind["broken_anchor"], Fix: "trellis knowledge lint"},
	}, nil
}
```

Replace with:

```go
	revisions, orphanedHistory, err := c.RevisionHealth(ctx, projectID)
	if err != nil {
		return nil, err
	}

	return []HealthLine{
		{What: "entries", Count: total, Fix: "trellis knowledge ls"},
		{What: "never read in " + itoa(ReadWindowDays) + "d", Count: cold, Fix: "trellis knowledge ls --cold"},
		{What: "stale pinned recaps", Count: stale, Fix: "trellis knowledge pins --stale"},
		{What: "orphans (no links)", Count: byKind["orphan"], Fix: "trellis knowledge lint"},
		{What: "stubs", Count: byKind["stub"], Fix: "trellis knowledge lint"},
		{What: "broken anchors", Count: byKind["broken_anchor"], Fix: "trellis knowledge lint"},
		{What: "revisions", Count: revisions, Fix: "trellis maintenance prune --revisions"},
		{What: "orphaned revision directories", Count: orphanedHistory, Fix: "trellis maintenance prune --orphan-history"},
	}, nil
}
```

- [ ] **Step 5: Run the core tests to verify they pass**

Run: `go test ./internal/core -run 'TestPruneRevisionsTrimsKnowledgeAndCards|TestPruneOrphanHistoryRemovesDirectoriesWithNoEntryFile|TestHealthReportsRevisionsAndOrphans' -v`
Expected: PASS, all three.

- [ ] **Step 6: Extend `maintenance prune`**

In `internal/cli/maintenance.go`, replace `newMaintenancePruneCmd` entirely:

```go
func newMaintenancePruneCmd() *cobra.Command {
	var retention string
	var events, invocations, revisions, orphanHistory bool
	cmd := &cobra.Command{
		Use: "prune", Short: "Delete old event, invocation or revision history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !events && !invocations && !revisions && !orphanHistory {
				return core.ErrUsage("nothing_to_prune",
					"select --events, --invocations, --revisions and/or --orphan-history",
					"trellis maintenance prune --before 90d --events")
			}
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()

			var total int64
			var before int64
			if events || invocations {
				age, err := retentionDuration(retention)
				if err != nil {
					return core.ErrUsage("invalid_retention", err.Error(), "trellis maintenance prune --before 90d --events")
				}
				before = time.Now().Add(-age).UnixMilli()
				n, err := c.PruneHistory(cmd.Context(), before, events, invocations)
				if err != nil {
					return err
				}
				total += n
			}
			if revisions {
				n, err := c.PruneRevisions(cmd.Context())
				if err != nil {
					return err
				}
				total += n
			}
			if orphanHistory {
				n, err := c.PruneOrphanHistory(cmd.Context())
				if err != nil {
					return err
				}
				total += n
			}
			return Emit(cmd, map[string]any{"deleted": total, "before": before},
				func() string { return fmt.Sprintf("deleted %d historical rows", total) })
		},
	}
	cmd.Flags().StringVar(&retention, "before", "", "retention age, for example 90d or 12h (required with --events or --invocations)")
	cmd.Flags().BoolVar(&events, "events", false, "prune event history")
	cmd.Flags().BoolVar(&invocations, "invocations", false, "prune invocation history")
	cmd.Flags().BoolVar(&revisions, "revisions", false, "trim every entry's and card's revisions to history.keep")
	cmd.Flags().BoolVar(&orphanHistory, "orphan-history", false, "remove revision directories whose entry file is gone")
	return cmd
}
```

This drops the unconditional `cmd.MarkFlagRequired("before")` the old version had (`--before` is required only alongside `--events`/`--invocations`, enforced above inside `RunE`, not by cobra) and removes the import of nothing new — `core`, `fmt`, `time` and `cobra` are already imported by this file.

- [ ] **Step 7: Write the CLI test**

Create `internal/cli/maintenance_cmd_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestMaintenancePruneRequiresASelector(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "maintenance", "prune"); cliErrCode(err) != "nothing_to_prune" {
		t.Errorf("err = %v, want nothing_to_prune", err)
	}
}

func TestMaintenancePruneRevisionsNeedsNoBefore(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Entry")
	out := runCmd(t, "maintenance", "prune", "--revisions", "--json")
	if !strings.Contains(out, `"deleted"`) {
		t.Fatalf("out = %s, want a deleted count", out)
	}
}

func TestMaintenancePruneOrphanHistoryNeedsNoBefore(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "maintenance", "prune", "--orphan-history", "--json")
	if !strings.Contains(out, `"deleted":0`) {
		t.Fatalf("out = %s, want zero orphans in a fresh vault", out)
	}
}

func TestMaintenancePruneEventsStillRequiresBefore(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "maintenance", "prune", "--events"); cliErrCode(err) != "invalid_retention" {
		t.Errorf("err = %v, want invalid_retention", err)
	}
}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/cli -run 'TestMaintenancePruneRequiresASelector|TestMaintenancePruneRevisionsNeedsNoBefore|TestMaintenancePruneOrphanHistoryNeedsNoBefore|TestMaintenancePruneEventsStillRequiresBefore' -v`
Expected: PASS, all four.

- [ ] **Step 9: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && go test -race ./internal/core && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/core/maintenance.go internal/core/health.go internal/core/maintenance_test.go \
        internal/cli/maintenance.go internal/cli/maintenance_cmd_test.go
git commit -m "feat: prune revisions by count and by orphan, report both in health

'maintenance prune' gains --revisions (trim every entry and card to
history.keep) and --orphan-history (remove revision directories whose
entry file is gone), neither of which takes --before. 'knowledge
health' reports both counts, each naming the flag that acts on it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Self-review

**Spec coverage:**
- Knowledge revisions as files, one hidden dir per entry — Task 1 (`revisionDir`/`revisionFilePath`).
- Copy-in-not-move-out for create, before-write, after-write, and external-change — Task 1 Steps 5–7, with the direct-edit-after-a-Trellis-write test explicitly proving the motivating case.
- Two no-repeat rules (already-retained, byte-identical) — Task 1 `captureKnowledgeRevision`.
- Concurrent writers / `--if-version` / undo-inside-the-transaction — reused verbatim from the existing write path; Task 1/2 place every new call so existing failure and panic tests keep passing.
- Escalate/demote/delete move or remove the revision directory with the entry, undone the same way as the entry's own move/removal — Task 2.
- `knowledge mv` moving it too — explicitly **not** in this plan: `knowledge mv` does not exist in this codebase yet (verified by grep); it is future work for the knowledge-paths plan named in the spec's own "Order" section, which this plan's revision-directory helpers (`moveDir`) are built to be reused by.
- Cards as rows, capture points, `INSERT OR IGNORE`, gaps from moves/leases/archiving, cascade delete — Task 3.
- `history.keep`, 0 disables, negative is an error, exposed through `config get/set/unset` — Task 4.
- Diff library evaluation and choice — documented above and in Task 5, with the exact API called.
- CLI `history`/`diff` for both entities — Task 6.
- Web API, exact JSON shapes, behind `protectedHandler` — Task 7.
- Disclosure — no code path added; noted in Global Constraints, since the spec says nothing here changes.
- Housekeeping `--revisions`/`--orphan-history`, `knowledge health` reporting both — Task 8.
- Windows: hidden-dot directories created/written/moved/removed — every path helper uses `filepath` throughout; `moveDir`/`stageRemoval` are exercised by Task 2's tests on every CI platform via the gates, and rely only on `os.Rename`/`os.RemoveAll`, which behave the same for directories on Windows as on Unix.
- Testing checklist in the spec: every bullet maps to a named test above (cross-referenced while drafting each task); the one item deliberately not covered by a new test is the pre-existing `TestListKnowledgeFiltersByTypeAndProvenance/both_dimensions` flake, which this plan does not touch.

**Placeholders:** none found on review — every step has runnable code, and every "modify" step quotes the exact current text being replaced (verified against the worktree while drafting, not from memory).

**Type consistency:** `RevisionInfo`/`RevisionDiff`/`CardRevisionInfo` are defined once (Task 5) and referenced by identical name and field set in Tasks 6 and 7. `historyKeep` (Task 1) and `SetHistoryKeep` (Task 4) agree on sign handling (negative ignored defensively; validation lives in `config`). `resolveDiffRange` takes `[]int64` ascending everywhere it's called (Task 5's two callers both sort ascending via `sortedRevisionVersions`/manual slice before calling it).

**Ambiguities ruled on (stated inline above, repeated here for visibility):**
1. `history.keep` behaves like `lease.ttl` and `labels.require_on_card`: wired into `Core` once from the *global* config file (both the CLI's and the daemon's `openCore`-equivalent), not re-read per project from the `project_config` override table. `config get/set/unset` still work for it, matching existing precedent's own limitation.
2. Diff range defaults: with neither `--from` nor `--to`, `to` is the latest retained version and `from` is the one before it; with only one given, the other defaults **relative to the one given**, not to the absolute latest.
3. Orphan detection (`--orphan-history`, `knowledge health`) is driven by the database's own knowledge rows (does this row's file exist; if not, does its revision directory exist), never by walking the vault directory tree — consistent with the codebase's invariant that Trellis never discovers entries by scanning directories.
4. `card history`/`card diff` and `knowledge history`/`knowledge diff` web routes are unscoped by board (`/api/p/{key}/cards/{card}/...`, `/api/p/{key}/knowledge/{slug}/...`), extending the existing unscoped routes for each entity rather than introducing new `/b/{board}/...` variants that neither `LoadKnowledge` nor a card's identity need.
