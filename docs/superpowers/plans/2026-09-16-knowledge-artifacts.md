# Knowledge Artifacts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **WHERE TO WORK — read before anything else.** Every path in this plan is
> relative to the worktree **`/home/mtchen/Personal/trellis-worktrees/knowledge-artifacts`**
> on branch `feat/knowledge-artifacts`. Run every command from there, e.g.
> `cd /home/mtchen/Personal/trellis-worktrees/knowledge-artifacts && go test ./...`.
> **Never** run a command in, or edit a file under, `/home/mtchen/Personal/trellis`.
> That is a shared checkout where other sessions hold uncommitted work.

**Goal:** Let a knowledge entry attach files — a meeting note and its recording, a research note and its PDF — and let the web UI list and preview them safely.

**Architecture:** An entry names its artifacts in an `artifacts:` frontmatter list. Link rows are derived from that list, the same way wikilinks are, and a name that resolves to nothing is a stub. Creating an artifact resolves earlier stubs; deleting one turns entry links back into stubs. The web API returns each entry's artifacts with a URL. A new endpoint serves the bytes behind the UI's existing host, origin and token checks, and sets per-type headers so uploaded HTML or SVG can never run as the UI.

**Tech Stack:** Go 1.27, SQLite via `sqlx` (modernc driver), `encoding/json/v2`, `net/http` `ServeContent`, cobra.

**Spec:** `docs/superpowers/specs/2026-09-16-knowledge-artifacts-design.md`

## Global Constraints

- Markdown files are the source of truth. An entry's artifact list lives in its frontmatter; link rows are derived from it and are rebuilt whenever the file changes.
- Artifact bytes are never stored in SQLite.
- Artifact names are resolved **by exact, case-sensitive match** within the entry's project. `dedupe` in `internal/core/doc_relations.go` lower-cases and must **not** be used for names — an artifact's extension keeps its case (`photo.PNG`).
- A name matching no artifact, or more than one, resolves to nothing. Never pick one of several.
- **No unique index on `artifact(project_id, name)`.** A migration adding one would fail on a database that already holds a duplicate, and a failed migration stops Trellis from starting.
- Core does not know UI URLs. `url` is added in `internal/ui`.
- The serving endpoint lives under `/api/`, so `protectedHandler` (`internal/ui/security.go`) applies to it. Nothing here may bypass or weaken that.
- Every artifact response sets `X-Content-Type-Options: nosniff`, and `Content-Security-Policy: sandbox` on every type **except `application/pdf`**. `text/*` is served as `text/plain; charset=utf-8`. Archives get `Content-Disposition: attachment`; everything else previewable gets `inline`. The disposition is built with `mime.FormatMediaType`.
- An artifact is served only when it is registered in the database **and** its stored path, after resolving symlinks, lies inside `<root>/projects/<KEY>/artifacts`.
- Never filter or disclose on the `private` mirror column in SQL. This work adds no such query.
- No backward-compatibility shims. Pre-1.0.
- No content inspection of any kind.
- Writes to entry files go through `EditKnowledgeFields`, which uses `writeAtomic`.
- CI runs Linux, macOS and Windows; all three must pass.
- Gates before any task is done: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` printing nothing.
- **Known flake:** `TestListKnowledgeFiltersByTypeAndProvenance/both_dimensions` fails about one run in five (TRELLIS-26, pre-existing — `KnowledgeFilter.where()` ranges over a Go map). If that exact test fails, say so and move on. **Investigate any other failure; never attribute it to this flake.**
- Stage by explicit path only. Before every commit run `git diff --cached --name-only` and confirm each path is one this task changed.
- Every commit message ends with `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/core/markdown.go` | Modify: `Frontmatter.Artifacts` |
| `internal/core/knowledge.go` | Modify: `ArtifactRef`, `Knowledge.Artifacts`, `docView`, `KnowledgeEdit.Artifacts`, `EditKnowledgeFields` |
| `internal/core/doc_relations.go` | Modify: derive artifact links in `syncDocRelations`; `resolveArtifactName`; `dedupeNames` |
| `internal/core/artifact.go` | Modify: name check and stub backfill on create, stubs on delete, resolve / link / unlink, `ListArtifacts` by entry, `ArtifactFile` |
| `internal/core/lint.go` | Modify: `missing_artifact`; orphans ignore artifact links |
| `internal/core/artifact_link_test.go` | Create: every core behaviour above |
| `internal/core/artifact_test.go` | Modify: one call site of `ListArtifacts` |
| `internal/cli/artifact.go` | Modify: `--doc` on add, link, ls; new `unlink`; id-or-name everywhere |
| `internal/cli/artifact_cmd_test.go` | Create: CLI behaviour |
| `internal/ui/artifacts.go` | Create: list-item shape with URLs; the serving handler and its headers |
| `internal/ui/server.go` | Modify: two list handlers, one route |
| `internal/ui/artifacts_test.go` | Create: list shape, headers per type, Range, 404s, token, header injection |

---

### Task 1: An entry names its artifacts, and the links are derived

**Files:**
- Modify: `internal/core/markdown.go` (`Frontmatter`)
- Modify: `internal/core/knowledge.go` (new `ArtifactRef`; `Knowledge`; `docView`)
- Modify: `internal/core/doc_relations.go` (`syncDocRelations`; new `resolveArtifactName`, `dedupeNames`)
- Create: `internal/core/artifact_link_test.go`

**Interfaces:**
- Consumes: existing `CreateArtifact`, `CreateKnowledge`, `LoadKnowledge`, `EditKnowledge`, `SplitFrontmatter`, `RenderDoc`, `kbCore`, `NewCardID`, `c.db`.
- Produces:
  - `Frontmatter.Artifacts []string` (`yaml:"artifacts,omitempty"`)
  - `type ArtifactRef struct { Name, Kind, MIME string; Size int64; Missing bool }` with db tags `name, kind, mime, size, missing` and json tags `name`, `kind,omitempty`, `mime,omitempty`, `size,omitempty`, `missing`
  - `Knowledge.Artifacts []ArtifactRef` (`db:"-" json:"artifacts,omitempty"`)
  - `func (c *Core) resolveArtifactName(tx *sqlx.Tx, projectID, name string) (any, error)`
  - `func dedupeNames(in []string) []string`
  - test helpers in `artifact_link_test.go`: `addArtifact`, `setArtifactsInFile`, `artifactsInFile`, `insertDuplicateArtifact`

- [ ] **Step 1: Write the failing tests**

Create `internal/core/artifact_link_test.go`:

```go
package core

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// addArtifact stores a file as an artifact of the project, the way
// `trellis artifact add` does.
func addArtifact(t *testing.T, c *Core, projectID, filename, content string) Artifact {
	t.Helper()
	src := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := c.CreateArtifact(t.Context(), projectID, src)
	if err != nil {
		t.Fatalf("CreateArtifact %s: %v", filename, err)
	}
	return a
}

// setArtifactsInFile rewrites an entry's `artifacts` list the way someone
// editing the file directly would.
func setArtifactsInFile(t *testing.T, path string, names ...string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fm, body, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	fm.Artifacts = names
	if err := os.WriteFile(path, []byte(RenderDoc(fm, body)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// artifactsInFile reads the list straight from the file, not from the database.
func artifactsInFile(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return fm.Artifacts
}

// insertDuplicateArtifact adds a second row with a's name and a different path,
// which is what a storage root that moved between two `artifact add` calls
// leaves behind.
func insertDuplicateArtifact(t *testing.T, c *Core, projectID string, a Artifact) {
	t.Helper()
	if _, err := c.db.Exec(
		`INSERT INTO artifact (id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		NewCardID(), projectID, a.Name, filepath.Join(t.TempDir(), a.Name),
		a.Kind, a.MIME, a.Size, a.ContentHash, 1, 1); err != nil {
		t.Fatalf("insert duplicate: %v", err)
	}
}

func artifactNamesOf(doc Knowledge) []string {
	names := []string{}
	for _, r := range doc.Artifacts {
		names = append(names, r.Name)
	}
	return names
}

func TestEntryArtifactResolves(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "standup.mp3", "ID3 recording")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Standup"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, a.Name)

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("Artifacts = %+v, want one", got.Artifacts)
	}
	ref := got.Artifacts[0]
	if ref.Name != a.Name || ref.Missing || ref.Kind != a.Kind || ref.MIME != a.MIME || ref.Size != a.Size {
		t.Errorf("ref = %+v, want it to describe the stored artifact %+v", ref, a)
	}
}

func TestEntryArtifactThatDoesNotExistIsAStub(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Research"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, "not-yet.pdf")

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 1 || !got.Artifacts[0].Missing || got.Artifacts[0].Name != "not-yet.pdf" {
		t.Fatalf("Artifacts = %+v, want one missing entry named not-yet.pdf", got.Artifacts)
	}
	if r := got.Artifacts[0]; r.Kind != "" || r.MIME != "" || r.Size != 0 {
		t.Errorf("stub = %+v, want it to carry nothing but its name", r)
	}
}

func TestRemovingANameFromTheFileRemovesTheLink(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "clip.mp3", "ID3 clip")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Clip"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, a.Name)
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}

	setArtifactsInFile(t, doc.Path)
	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 0 {
		t.Errorf("Artifacts = %+v after the name was removed from the file", got.Artifacts)
	}
	var rows int
	if err := c.db.Get(&rows,
		`SELECT COUNT(*) FROM link WHERE from_id = ? AND rel = 'artifact'`, doc.ID); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("%d artifact link rows remain, want 0", rows)
	}
}

func TestANameSharedByTwoArtifactsIsAStub(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "diagram.png", "\x89PNG\r\n\x1a\nfirst")
	insertDuplicateArtifact(t, c, p.ID, a)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Design"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, a.Name)

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 1 || !got.Artifacts[0].Missing {
		t.Errorf("Artifacts = %+v, want an ambiguous name left unresolved", got.Artifacts)
	}
}

// An artifact's name keeps its extension's case, so a lower-cased lookup would
// never find "photo.PNG". The list also keeps the file's order, and a name
// listed twice links once.
func TestArtifactNamesKeepCaseAndOrder(t *testing.T) {
	c, p, _ := kbCore(t)
	upper := addArtifact(t, c, p.ID, "Photo.PNG", "\x89PNG\r\n\x1a\nx")
	notes := addArtifact(t, c, p.ID, "notes.txt", "plain")
	if upper.Name != "photo.PNG" {
		t.Fatalf("setup: name = %q; this test relies on the extension keeping its case", upper.Name)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Album"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, notes.Name, upper.Name, notes.Name)

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if names := artifactNamesOf(got); !slices.Equal(names, []string{notes.Name, upper.Name}) {
		t.Errorf("names = %v, want %v", names, []string{notes.Name, upper.Name})
	}
	for _, r := range got.Artifacts {
		if r.Missing {
			t.Errorf("%s did not resolve", r.Name)
		}
	}
}

func TestEditingTheBodyKeepsTheArtifactList(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "report.pdf", "%PDF-1.7\n")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Report"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, a.Name)
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}

	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "a new body\n", nil); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	if got := artifactsInFile(t, doc.Path); !slices.Equal(got, []string{a.Name}) {
		t.Errorf("file lists %v after a body edit, want [%s]", got, a.Name)
	}
	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].Missing {
		t.Errorf("Artifacts = %+v after a body edit", got.Artifacts)
	}
}
```

The content strings are chosen so Go's content sniffing gives real types: `ID3…` is `audio/mpeg`, `\x89PNG…` is `image/png`, `%PDF-` is `application/pdf`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestEntryArtifact|TestRemovingAName|TestANameShared|TestArtifactNamesKeep|TestEditingTheBodyKeeps' -v`
Expected: the package does not compile — `fm.Artifacts undefined` and `got.Artifacts undefined`.

- [ ] **Step 3: Add the frontmatter field**

In `internal/core/markdown.go`, inside `Frontmatter`, directly after `Labels`:

```go
	// Artifacts names the files attached to this entry, by stored artifact
	// name. The list is the record; link rows are derived from it.
	Artifacts []string `yaml:"artifacts,omitempty"`
```

- [ ] **Step 4: Add the reference type and the computed field**

In `internal/core/knowledge.go`, directly after the closing brace of `type Knowledge struct`, add:

```go
// ArtifactRef is an artifact as an entry names it. A name that does not resolve
// to exactly one artifact of the entry's project is Missing and carries nothing
// but its name. Missing is always serialised, so a caller can test it without
// guessing what an absent field means.
type ArtifactRef struct {
	Name    string `db:"name" json:"name"`
	Kind    string `db:"kind" json:"kind,omitempty"`
	MIME    string `db:"mime" json:"mime,omitempty"`
	Size    int64  `db:"size" json:"size,omitempty"`
	Missing bool   `db:"missing" json:"missing"`
}
```

Inside `type Knowledge struct`, in the `// Computed for display.` block, after `Labels`:

```go
	Artifacts []ArtifactRef `db:"-" json:"artifacts,omitempty"`
```

- [ ] **Step 5: Fill it in `docView`**

`docView` in `internal/core/knowledge.go` currently ends with:

```go
	doc.Labels = []string{}
	return tx.Select(&doc.Labels,
		`SELECT l.name FROM label l JOIN knowledge_label kl ON kl.label_id = l.id WHERE kl.doc_id = ? ORDER BY l.name`,
		doc.ID)
}
```

Replace those lines with:

```go
	doc.Labels = []string{}
	if err := tx.Select(&doc.Labels,
		`SELECT l.name FROM label l JOIN knowledge_label kl ON kl.label_id = l.id WHERE kl.doc_id = ? ORDER BY l.name`,
		doc.ID); err != nil {
		return err
	}
	// rowid order is the order syncDocRelations inserted the rows, which is
	// the order the file lists the names.
	doc.Artifacts = nil
	return tx.Select(&doc.Artifacts,
		`SELECT l.to_raw AS name,
		        COALESCE(a.kind, '') AS kind,
		        COALESCE(a.mime, '') AS mime,
		        COALESCE(a.size, 0)  AS size,
		        (l.to_id IS NULL)    AS missing
		 FROM link l LEFT JOIN artifact a ON a.id = l.to_id
		 WHERE l.from_type = 'doc' AND l.from_id = ? AND l.rel = 'artifact'
		 ORDER BY l.rowid`, doc.ID)
}
```

A comparison scanned into a `bool` is already how `Pins` reads `stale`, so `(l.to_id IS NULL) AS missing` works with this driver.

- [ ] **Step 6: Derive the links**

In `internal/core/doc_relations.go`, inside `syncDocRelations`, directly after the wikilink loop (the `for _, ref := range ParseWikilinks(body)` block) and before the `// Tags come from both...` comment, insert:

```go
	// Artifacts are named in the frontmatter and resolved by name within the
	// entry's project. Replaced wholesale, like wikilinks, because the file is
	// the record. The names are deduplicated here rather than by the UNIQUE
	// constraint, which cannot collapse rows whose anchor is NULL.
	if _, err := tx.Exec(
		`DELETE FROM link WHERE from_type = 'doc' AND from_id = ? AND rel = 'artifact'`, doc.ID); err != nil {
		return err
	}
	for _, name := range dedupeNames(fm.Artifacts) {
		toID, err := c.resolveArtifactName(tx, doc.ProjectID, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel)
			 VALUES ('doc', ?, 'artifact', ?, ?, 'artifact')`,
			doc.ID, toID, name); err != nil {
			return err
		}
	}
```

At the end of the file, add:

```go
// resolveArtifactName returns the id of the one artifact in the project with
// this name, or nil — a stub — when there is none or more than one. Picking one
// of several would attach the wrong file without anyone noticing.
func (c *Core) resolveArtifactName(tx *sqlx.Tx, projectID, name string) (any, error) {
	var ids []string
	if err := tx.Select(&ids,
		`SELECT id FROM artifact WHERE project_id = ? AND name = ?`, projectID, name); err != nil {
		return nil, err
	}
	if len(ids) != 1 {
		return nil, nil
	}
	return ids[0], nil
}

// dedupeNames trims and deduplicates while keeping case and order. It must not
// lower-case, unlike dedupe: an artifact name keeps its extension's case
// ("photo.PNG"), and a lower-cased name would never resolve.
func dedupeNames(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestEntryArtifact|TestRemovingAName|TestANameShared|TestArtifactNamesKeep|TestEditingTheBodyKeeps' -v`
Expected: PASS, all six.

- [ ] **Step 8: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass; `gofmt -l .` prints nothing.

- [ ] **Step 9: Commit**

```bash
git add internal/core/markdown.go internal/core/knowledge.go internal/core/doc_relations.go \
        internal/core/artifact_link_test.go
git commit -m "feat(core): an entry names its artifacts, and the links are derived

The list lives in the entry's frontmatter. Link rows are rebuilt from it
whenever the file changes, and a name that matches no artifact, or several,
is a stub. Names keep their case: an extension like .PNG is not lower-cased.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Creating and deleting an artifact keeps entry links honest

**Files:**
- Modify: `internal/core/artifact.go` (`CreateArtifact`, `DeleteArtifact`; new `artifactNameTaken`)
- Test: `internal/core/artifact_link_test.go`

**Interfaces:**
- Consumes: everything Task 1 produced.
- Produces: `func artifactNameTaken(tx *sqlx.Tx, projectID, path string) (bool, error)`. `CreateArtifact` never returns a name already present in the project's rows, and fills earlier stubs. `DeleteArtifact` leaves entry links as stubs and removes card links.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/artifact_link_test.go`:

```go
// A storage root that moved leaves rows whose files live elsewhere. Their
// names still resolve entries, so a new artifact must not take one.
func TestANameTakenInTheDatabaseIsNotReused(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.db.Exec(
		`INSERT INTO artifact (id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at)
		 VALUES (?, ?, 'x.png', ?, 'image', 'image/png', 3, 'h', 1, 1)`,
		NewCardID(), p.ID, filepath.Join(t.TempDir(), "x.png")); err != nil {
		t.Fatalf("insert: %v", err)
	}
	a := addArtifact(t, c, p.ID, "x.png", "\x89PNG\r\n\x1a\nx")
	if a.Name != "x-2.png" {
		t.Errorf("name = %q, want x-2.png: x.png is already a name in this project", a.Name)
	}
}

func TestCreatingAnArtifactResolvesAnEarlierStub(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Later"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, "later.pdf")
	// This read writes the stub row. Without it there would be nothing to
	// backfill, and the resync on the next read would hide a missing backfill.
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}

	a := addArtifact(t, c, p.ID, "later.pdf", "%PDF-1.7\n")

	// The file has not changed, so this read does not resync: the resolution
	// can only have come from the backfill.
	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].Missing || got.Artifacts[0].Kind != a.Kind {
		t.Errorf("Artifacts = %+v, want later.pdf resolved by the backfill", got.Artifacts)
	}
}

func TestDeletingAnArtifactLeavesEntryLinksAsStubs(t *testing.T) {
	c, p, b := kbCore(t)
	a := addArtifact(t, c, p.ID, "evidence.png", "\x89PNG\r\n\x1a\nx")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Evidence"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, a.Name)
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "attach"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if err := c.LinkArtifactToCard(t.Context(), p.ID, card.ID, a.ID); err != nil {
		t.Fatalf("LinkArtifactToCard: %v", err)
	}

	if err := c.DeleteArtifact(t.Context(), p.ID, a.ID); err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 1 || !got.Artifacts[0].Missing || got.Artifacts[0].Name != a.Name {
		t.Errorf("Artifacts = %+v, want the entry's link kept as a stub", got.Artifacts)
	}
	var cardLinks int
	if err := c.db.Get(&cardLinks,
		`SELECT COUNT(*) FROM link WHERE from_type = 'card' AND from_id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}
	if cardLinks != 0 {
		t.Errorf("%d card links remain, want 0: a card's link lives only in the database", cardLinks)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestANameTaken|TestCreatingAnArtifactResolves|TestDeletingAnArtifactLeaves' -v`
Expected: all three FAIL — the name is reused, the stub stays missing, and the entry's link row is deleted.

- [ ] **Step 3: Check the database when choosing a name**

In `internal/core/artifact.go`, inside `CreateArtifact`, the collision loop currently reads:

```go
		path := filepath.Join(dir, name)
		for n := 2; ; n++ {
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				break
			}
			path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, filepath.Ext(name)), n, filepath.Ext(name)))
		}
```

Replace it with:

```go
		path := filepath.Join(dir, name)
		for n := 2; ; n++ {
			taken, err := artifactNameTaken(tx, projectID, path)
			if err != nil {
				return err
			}
			if !taken {
				break
			}
			path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, filepath.Ext(name)), n, filepath.Ext(name)))
		}
```

Add this function anywhere in the file:

```go
// artifactNameTaken reports whether a candidate path cannot be used: a file is
// already there, or the project already has an artifact with that name. The
// database check matters because names resolve entries' references, and a
// storage root that has moved leaves rows whose files are elsewhere. An error
// other than "no such file" stops the search rather than looping forever.
func artifactNameTaken(tx *sqlx.Tx, projectID, path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	var n int
	if err := tx.Get(&n,
		`SELECT COUNT(*) FROM artifact WHERE project_id = ? AND name = ?`,
		projectID, filepath.Base(path)); err != nil {
		return false, err
	}
	return n > 0, nil
}
```

- [ ] **Step 4: Backfill stubs after the insert**

The transaction in `CreateArtifact` currently ends with:

```go
		_, err = tx.Exec(`INSERT INTO artifact (id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, out.ID, out.ProjectID, out.Name, out.Path, out.Kind, out.MIME, out.Size, out.ContentHash, out.CreatedAt, out.UpdatedAt)
		return err
```

Replace those two lines with:

```go
		if _, err := tx.Exec(`INSERT INTO artifact (id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, out.ID, out.ProjectID, out.Name, out.Path, out.Kind, out.MIME, out.Size, out.ContentHash, out.CreatedAt, out.UpdatedAt); err != nil {
			return err
		}
		// An entry may already name this artifact, written before it existed.
		// The name is new to the project (artifactNameTaken made sure), so no
		// stub it fills was ambiguous.
		_, err = tx.Exec(
			`UPDATE link SET to_id = ?
			 WHERE to_type = 'artifact' AND rel = 'artifact' AND to_id IS NULL AND to_raw = ?
			   AND from_type = 'doc'
			   AND from_id IN (SELECT id FROM knowledge WHERE project_id = ?)`,
			out.ID, out.Name, projectID)
		return err
```

- [ ] **Step 5: Keep entry links as stubs on delete**

In `DeleteArtifact`, directly after the `tx.Get(&path, ...)` block and **before** the existing `DELETE FROM link ...` statement, insert:

```go
		// An entry names its artifacts in its own file, so its link survives as
		// a stub, the same as a wikilink to a deleted entry. Clearing to_id
		// first keeps the DELETE below, and the artifact_links_ad trigger, from
		// matching it. A card's link lives only in the database and goes.
		if _, err := tx.Exec(
			`UPDATE link SET to_id = NULL
			 WHERE to_type = 'artifact' AND to_id = ? AND from_type = 'doc'`, artifactID); err != nil {
			return err
		}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestANameTaken|TestCreatingAnArtifactResolves|TestDeletingAnArtifactLeaves|TestArtifact' -v`
Expected: PASS, including the existing `TestArtifactStoresBytesOnDiskAndLinksToCard`.

- [ ] **Step 7: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/core/artifact.go internal/core/artifact_link_test.go
git commit -m "feat(core): creating and deleting an artifact keeps entry links honest

A new artifact never takes a name the project's rows already hold, and it
resolves entries that named it first. Deleting one leaves entry links as
stubs, because the entry's file still names it; card links go with it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Resolve, link and unlink

**Files:**
- Modify: `internal/core/knowledge.go` (`KnowledgeEdit`, `EditKnowledgeFields`)
- Modify: `internal/core/artifact.go` (new `ResolveArtifact`, `LinkArtifactToDoc`, `UnlinkArtifactFromDoc`, `UnlinkArtifactFromCard`, `editDocArtifacts`, `namesAdded`; `ListArtifacts` gains `docID`)
- Modify: `internal/core/artifact_test.go` (one call site)
- Modify: `internal/cli/artifact.go` (one call site only — the rest is Task 5)
- Test: `internal/core/artifact_link_test.go`

**Interfaces:**
- Consumes: Tasks 1 and 2.
- Produces:
  - `KnowledgeEdit.Artifacts *[]string` — when non-nil, replaces the entry's list; `EditKnowledgeFields` records `artifact_linked` / `artifact_unlinked` events per name
  - `func (c *Core) ResolveArtifact(ctx context.Context, projectID, ref string) (Artifact, error)` — errors with codes `artifact_not_found` or `artifact_ambiguous`
  - `func (c *Core) LinkArtifactToDoc(ctx context.Context, projectID, slug, artifactRef string) (Knowledge, error)`
  - `func (c *Core) UnlinkArtifactFromDoc(ctx context.Context, projectID, slug, artifactRef string) (Knowledge, error)`
  - `func (c *Core) UnlinkArtifactFromCard(ctx context.Context, projectID, cardID, artifactID string) error`
  - `func (c *Core) ListArtifacts(ctx context.Context, projectID, cardID, docID string) ([]Artifact, error)` — **signature change**

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/artifact_link_test.go`, and add `"errors"` and `"strings"` to its imports:

```go
func artifactErrCode(err error) string {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return ""
}

func TestResolveArtifactByIDOrName(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "map.png", "\x89PNG\r\n\x1a\nx")

	byID, err := c.ResolveArtifact(t.Context(), p.ID, a.ID)
	if err != nil || byID.ID != a.ID {
		t.Errorf("by id = %+v, %v", byID, err)
	}
	byName, err := c.ResolveArtifact(t.Context(), p.ID, a.Name)
	if err != nil || byName.ID != a.ID {
		t.Errorf("by name = %+v, %v", byName, err)
	}
	if _, err := c.ResolveArtifact(t.Context(), p.ID, "nope.png"); artifactErrCode(err) != "artifact_not_found" {
		t.Errorf("unknown: err = %v, want artifact_not_found", err)
	}

	insertDuplicateArtifact(t, c, p.ID, a)
	_, err = c.ResolveArtifact(t.Context(), p.ID, a.Name)
	if artifactErrCode(err) != "artifact_ambiguous" {
		t.Fatalf("shared name: err = %v, want artifact_ambiguous", err)
	}
	if !strings.Contains(err.Error(), a.ID) {
		t.Errorf("error %q does not name the matching ids", err)
	}
}

func TestLinkArtifactToEntry(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "meeting.mp3", "ID3 meeting")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Meeting"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	got, err := c.LinkArtifactToDoc(t.Context(), p.ID, doc.Slug, a.Name)
	if err != nil {
		t.Fatalf("LinkArtifactToDoc: %v", err)
	}
	if names := artifactNamesOf(got); !slices.Equal(names, []string{a.Name}) || got.Artifacts[0].Missing {
		t.Errorf("Artifacts = %+v", got.Artifacts)
	}
	if file := artifactsInFile(t, doc.Path); !slices.Equal(file, []string{a.Name}) {
		t.Errorf("file lists %v, want [%s]", file, a.Name)
	}
	var logged int
	if err := c.db.Get(&logged,
		`SELECT COUNT(*) FROM event WHERE entity_id = ? AND action = 'artifact_linked' AND new_value = ?`,
		doc.ID, a.Name); err != nil {
		t.Fatal(err)
	}
	if logged != 1 {
		t.Errorf("%d artifact_linked events, want 1", logged)
	}

	again, err := c.LinkArtifactToDoc(t.Context(), p.ID, doc.Slug, a.ID)
	if err != nil {
		t.Fatalf("LinkArtifactToDoc again: %v", err)
	}
	if again.Version != got.Version {
		t.Errorf("version %d -> %d: linking an already-listed artifact must not rewrite the file",
			got.Version, again.Version)
	}
}

// A database restored from an older backup can lack an entry's link rows while
// the file still lists the names. Linking must read the list from the file;
// rewriting it from the lagging rows would drop names.
func TestLinkKeepsNamesTheDatabaseHasLost(t *testing.T) {
	c, p, _ := kbCore(t)
	first := addArtifact(t, c, p.ID, "first.png", "\x89PNG\r\n\x1a\n1")
	second := addArtifact(t, c, p.ID, "second.png", "\x89PNG\r\n\x1a\n2")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Pair"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, first.Name)
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if _, err := c.db.Exec(`DELETE FROM link WHERE from_id = ? AND rel = 'artifact'`, doc.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := c.LinkArtifactToDoc(t.Context(), p.ID, doc.Slug, second.Name); err != nil {
		t.Fatalf("LinkArtifactToDoc: %v", err)
	}
	if file := artifactsInFile(t, doc.Path); !slices.Equal(file, []string{first.Name, second.Name}) {
		t.Errorf("file lists %v, want [%s %s]", file, first.Name, second.Name)
	}
}

func TestUnlinkArtifactFromEntry(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "a.png", "\x89PNG\r\n\x1a\na")
	b := addArtifact(t, c, p.ID, "b.png", "\x89PNG\r\n\x1a\nb")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Two"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, a.Name, b.Name)
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}

	got, err := c.UnlinkArtifactFromDoc(t.Context(), p.ID, doc.Slug, a.Name)
	if err != nil {
		t.Fatalf("UnlinkArtifactFromDoc: %v", err)
	}
	if names := artifactNamesOf(got); !slices.Equal(names, []string{b.Name}) {
		t.Errorf("names = %v, want [%s]", names, b.Name)
	}
	if file := artifactsInFile(t, doc.Path); !slices.Equal(file, []string{b.Name}) {
		t.Errorf("file lists %v, want [%s]", file, b.Name)
	}
	var logged int
	if err := c.db.Get(&logged,
		`SELECT COUNT(*) FROM event WHERE entity_id = ? AND action = 'artifact_unlinked' AND new_value = ?`,
		doc.ID, a.Name); err != nil {
		t.Fatal(err)
	}
	if logged != 1 {
		t.Errorf("%d artifact_unlinked events, want 1", logged)
	}

	again, err := c.UnlinkArtifactFromDoc(t.Context(), p.ID, doc.Slug, a.Name)
	if err != nil {
		t.Fatalf("UnlinkArtifactFromDoc again: %v", err)
	}
	if again.Version != got.Version {
		t.Errorf("unlinking a name that is not listed rewrote the file")
	}
}

// A stub must be clearable: its artifact no longer exists, so the name cannot
// be resolved, and unlinking must still remove it from the file.
func TestUnlinkClearsANameWhoseArtifactIsGone(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Gone"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, "deleted.pdf")
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}

	got, err := c.UnlinkArtifactFromDoc(t.Context(), p.ID, doc.Slug, "deleted.pdf")
	if err != nil {
		t.Fatalf("UnlinkArtifactFromDoc: %v", err)
	}
	if len(got.Artifacts) != 0 || len(artifactsInFile(t, doc.Path)) != 0 {
		t.Errorf("stub not cleared: Artifacts = %+v, file = %v", got.Artifacts, artifactsInFile(t, doc.Path))
	}
}

func TestUnlinkArtifactFromCard(t *testing.T) {
	c, p, b := kbCore(t)
	a := addArtifact(t, c, p.ID, "shot.png", "\x89PNG\r\n\x1a\nx")
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "attach"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if err := c.LinkArtifactToCard(t.Context(), p.ID, card.ID, a.ID); err != nil {
		t.Fatalf("LinkArtifactToCard: %v", err)
	}

	if err := c.UnlinkArtifactFromCard(t.Context(), p.ID, card.ID, a.ID); err != nil {
		t.Fatalf("UnlinkArtifactFromCard: %v", err)
	}
	items, err := c.ListArtifacts(t.Context(), p.ID, card.ID, "")
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("card still lists %d artifacts", len(items))
	}
	if err := c.UnlinkArtifactFromCard(t.Context(), p.ID, card.ID, a.ID); err != nil {
		t.Errorf("unlinking again: %v, want no error", err)
	}
}

func TestListArtifactsForAnEntry(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "one.png", "\x89PNG\r\n\x1a\n1")
	b := addArtifact(t, c, p.ID, "two.png", "\x89PNG\r\n\x1a\n2")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Listed"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, b.Name, a.Name)
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}

	items, err := c.ListArtifacts(t.Context(), p.ID, "", doc.ID)
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	if !slices.Equal(names, []string{b.Name, a.Name}) {
		t.Errorf("names = %v, want [%s %s]", names, b.Name, a.Name)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestResolveArtifact|TestLinkArtifactToEntry|TestLinkKeepsNames|TestUnlink|TestListArtifactsForAnEntry' -v`
Expected: the package does not compile — `c.ResolveArtifact undefined`, and `ListArtifacts` is called with too many arguments.

- [ ] **Step 3: Let an edit replace the list**

In `internal/core/knowledge.go`, add to `type KnowledgeEdit struct`:

```go
	// Artifacts, when non-nil, replaces the entry's artifact list.
	Artifacts *[]string
```

In `EditKnowledgeFields`, make four insertions.

(a) Directly after `if in.Summary != nil { fields["summary"] = *in.Summary }`:

```go
		if in.Artifacts != nil {
			fields["artifacts"] = strings.Join(*in.Artifacts, "\n")
		}
```

(b) Directly after the `fm, _, err := splitDocFile(doc.Path, raw)` statement and its `if err != nil { return err }`:

```go
		before := slices.Clone(fm.Artifacts)
```

(c) Directly after `if in.Summary != nil { fm.Summary = *in.Summary }`:

```go
		if in.Artifacts != nil {
			fm.Artifacts = dedupeNames(*in.Artifacts)
		}
```

(d) Directly after the `for _, field := range []string{"body", "title", "summary"}` loop and before `return c.docView(tx, &doc)`:

```go
		// An artifact name is metadata, not content, so it is recorded for a
		// private entry too; presence is not what the disclosure design
		// protects.
		if in.Artifacts != nil {
			for _, name := range namesAdded(before, fm.Artifacts) {
				if err := c.recordEvent(tx, "knowledge", doc.ID, "artifact_linked", "", "", name); err != nil {
					return err
				}
			}
			for _, name := range namesAdded(fm.Artifacts, before) {
				if err := c.recordEvent(tx, "knowledge", doc.ID, "artifact_unlinked", "", "", name); err != nil {
					return err
				}
			}
		}
```

The `edited` event loop iterates only body, title and summary, so the `artifacts` key in `fields` never produces an `edited` event; it exists so `checkWrite` sees the change.

- [ ] **Step 4: Add the core functions**

In `internal/core/artifact.go`, add `"database/sql"` and `"slices"` to the imports, then add:

```go
// ResolveArtifact finds an artifact by id or by name within a project. Ids are
// UUIDs and names are filenames, so the two cannot be confused. A name shared by
// several artifacts is an error that names them, never a guess.
func (c *Core) ResolveArtifact(ctx context.Context, projectID, ref string) (Artifact, error) {
	var out Artifact
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		err := tx.Get(&out, `SELECT * FROM artifact WHERE project_id = ? AND id = ?`, projectID, ref)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var matches []Artifact
		if err := tx.Select(&matches,
			`SELECT * FROM artifact WHERE project_id = ? AND name = ? ORDER BY created_at`,
			projectID, ref); err != nil {
			return err
		}
		switch len(matches) {
		case 0:
			return ErrNotFound("artifact_not_found", "no artifact "+ref, "trellis artifact ls")
		case 1:
			out = matches[0]
			return nil
		default:
			ids := make([]string, len(matches))
			for i, m := range matches {
				ids[i] = m.ID
			}
			return ErrUsage("artifact_ambiguous",
				"more than one artifact is named "+ref+": "+strings.Join(ids, ", "),
				"trellis artifact rm <id>   # remove the extra ones, then refer to it by name")
		}
	})
	return out, err
}

// LinkArtifactToDoc adds an artifact to an entry's `artifacts` list. The list
// lives in the entry's file, so this is an edit of that file, and the link row
// follows from it. Linking a name already listed changes nothing.
func (c *Core) LinkArtifactToDoc(ctx context.Context, projectID, slug, artifactRef string) (Knowledge, error) {
	a, err := c.ResolveArtifact(ctx, projectID, artifactRef)
	if err != nil {
		return Knowledge{}, err
	}
	return c.editDocArtifacts(ctx, projectID, slug, func(names []string) ([]string, bool) {
		if slices.Contains(names, a.Name) {
			return names, false
		}
		return append(names, a.Name), true
	})
}

// UnlinkArtifactFromDoc removes an artifact from an entry's list. The reference
// is resolved when it can be; when it cannot — the artifact is gone, or its name
// is shared — it is taken as written, so a stub can still be cleared. Removing a
// name that is not listed changes nothing.
func (c *Core) UnlinkArtifactFromDoc(ctx context.Context, projectID, slug, artifactRef string) (Knowledge, error) {
	name := artifactRef
	a, err := c.ResolveArtifact(ctx, projectID, artifactRef)
	switch e, ok := errors.AsType[*Error](err); {
	case err == nil:
		name = a.Name
	case ok && (e.Code == "artifact_not_found" || e.Code == "artifact_ambiguous"):
		// Keep the reference as written.
	default:
		return Knowledge{}, err
	}
	return c.editDocArtifacts(ctx, projectID, slug, func(names []string) ([]string, bool) {
		if !slices.Contains(names, name) {
			return names, false
		}
		return slices.DeleteFunc(names, func(n string) bool { return n == name }), true
	})
}

// editDocArtifacts reads an entry's artifact list from its file, lets change
// produce the next one, and writes it through EditKnowledgeFields. The list is
// read from the file rather than from derived link rows, which can lag the file
// after a database restore; rewriting the file from a lagging copy would drop
// names. IfVersion makes a concurrent edit between the read and the write a
// conflict instead of a lost update.
func (c *Core) editDocArtifacts(ctx context.Context, projectID, slug string,
	change func(names []string) ([]string, bool)) (Knowledge, error) {
	doc, err := c.LoadKnowledge(ctx, projectID, slug)
	if err != nil {
		return Knowledge{}, err
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		return Knowledge{}, err
	}
	fm, _, err := splitDocFile(doc.Path, raw)
	if err != nil {
		return Knowledge{}, err
	}
	next, changed := change(dedupeNames(fm.Artifacts))
	if !changed {
		return doc, nil
	}
	return c.EditKnowledgeFields(ctx, projectID, slug,
		KnowledgeEdit{Artifacts: &next, IfVersion: &doc.Version})
}

// UnlinkArtifactFromCard removes a card's link to an artifact. Removing a link
// that does not exist is not an error.
func (c *Core) UnlinkArtifactFromCard(ctx context.Context, projectID, cardID, artifactID string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.Exec(
			`DELETE FROM link
			 WHERE from_type = 'card' AND from_id = ? AND to_type = 'artifact'
			   AND to_id = ? AND rel = 'artifact'`, cardID, artifactID)
		return err
	})
}

// namesAdded returns the names in after that before lacks, in after's order.
func namesAdded(before, after []string) []string {
	var out []string
	for _, n := range after {
		if !slices.Contains(before, n) {
			out = append(out, n)
		}
	}
	return out
}
```

- [ ] **Step 5: List an entry's artifacts**

Replace `ListArtifacts` in `internal/core/artifact.go` entirely:

```go
// ListArtifacts lists a project's artifacts, or only those linked to one card
// or one entry. Unresolved names are not artifacts and are not listed here; an
// entry's stubs appear in its computed Artifacts.
func (c *Core) ListArtifacts(ctx context.Context, projectID, cardID, docID string) ([]Artifact, error) {
	out := []Artifact{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		switch {
		case cardID != "":
			return tx.Select(&out,
				`SELECT a.* FROM artifact a JOIN link l ON l.to_type = 'artifact' AND l.to_id = a.id
				 WHERE a.project_id = ? AND l.from_type = 'card' AND l.from_id = ?
				 ORDER BY a.updated_at DESC`, projectID, cardID)
		case docID != "":
			return tx.Select(&out,
				`SELECT a.* FROM artifact a JOIN link l ON l.to_type = 'artifact' AND l.to_id = a.id
				 WHERE a.project_id = ? AND l.from_type = 'doc' AND l.from_id = ? AND l.rel = 'artifact'
				 ORDER BY l.rowid`, projectID, docID)
		default:
			return tx.Select(&out,
				`SELECT * FROM artifact WHERE project_id = ? ORDER BY updated_at DESC`, projectID)
		}
	})
	return out, err
}
```

- [ ] **Step 6: Update the two existing call sites**

In `internal/core/artifact_test.go`, change `c.ListArtifacts(t.Context(), project.ID, card.ID)` to `c.ListArtifacts(t.Context(), project.ID, card.ID, "")`.

In `internal/cli/artifact.go`, change `app.Core.ListArtifacts(cmd.Context(), app.Project.ID, cardIDValue)` to `app.Core.ListArtifacts(cmd.Context(), app.Project.ID, cardIDValue, "")`. Change nothing else in that file in this task.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestResolveArtifact|TestLinkArtifactToEntry|TestLinkKeepsNames|TestUnlink|TestListArtifactsForAnEntry|TestArtifact|TestEntryArtifact|TestEditingTheBody' -v`
Expected: PASS.

- [ ] **Step 8: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/core/knowledge.go internal/core/artifact.go internal/core/artifact_test.go \
        internal/cli/artifact.go internal/core/artifact_link_test.go
git commit -m "feat(core): resolve, link and unlink artifacts on entries

An artifact is found by id or by name, and a shared name is an error, never
a guess. Linking and unlinking edit the entry's file, reading the list from
the file rather than from link rows that can lag it. A stub can be unlinked
by name even though its artifact is gone.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Lint reports missing artifacts

**Files:**
- Modify: `internal/core/lint.go`
- Test: `internal/core/artifact_link_test.go`

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces: `LintFinding` kind `missing_artifact`, with `Ref` set to the name. Orphan detection ignores artifact links.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/artifact_link_test.go`:

```go
func findingsFor(t *testing.T, c *Core, projectID, slug string) []LintFinding {
	t.Helper()
	all, err := c.Lint(t.Context(), projectID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	var out []LintFinding
	for _, f := range all {
		if f.Doc == slug {
			out = append(out, f)
		}
	}
	return out
}

func entryNaming(t *testing.T, c *Core, projectID, title string, names ...string) Knowledge {
	t.Helper()
	doc, err := c.CreateKnowledge(t.Context(), projectID, NewKnowledge{Title: title})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, names...)
	if _, err := c.LoadKnowledge(t.Context(), projectID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	return doc
}

func TestLintReportsAMissingArtifact(t *testing.T) {
	c, p, _ := kbCore(t)
	doc := entryNaming(t, c, p.ID, "Absent", "absent.pdf")

	var found bool
	for _, f := range findingsFor(t, c, p.ID, doc.Slug) {
		if f.Kind == "missing_artifact" && f.Ref == "absent.pdf" {
			found = true
			if !strings.Contains(f.Fix, "artifact add") {
				t.Errorf("fix = %q, want it to point at `artifact add`", f.Fix)
			}
		}
		if f.Kind == "stub" && f.Ref == "absent.pdf" {
			t.Errorf("an artifact name leaked into the wikilink stub finding")
		}
	}
	if !found {
		t.Error("no missing_artifact finding for absent.pdf")
	}
}

func TestLintReportsAnAmbiguousArtifact(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "twin.png", "\x89PNG\r\n\x1a\nx")
	insertDuplicateArtifact(t, c, p.ID, a)
	doc := entryNaming(t, c, p.ID, "Twin", a.Name)

	var found bool
	for _, f := range findingsFor(t, c, p.ID, doc.Slug) {
		if f.Kind == "missing_artifact" && f.Ref == a.Name {
			found = true
			if !strings.Contains(f.Fix, "artifact ls") {
				t.Errorf("fix = %q, want it to point at `artifact ls` for a shared name", f.Fix)
			}
		}
	}
	if !found {
		t.Error("no missing_artifact finding for a shared name")
	}
}

func TestLintIsQuietAboutAResolvedArtifact(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "fine.png", "\x89PNG\r\n\x1a\nx")
	doc := entryNaming(t, c, p.ID, "Fine", a.Name)
	for _, f := range findingsFor(t, c, p.ID, doc.Slug) {
		if f.Kind == "missing_artifact" {
			t.Errorf("unexpected finding %+v", f)
		}
	}
}

// Orphan means disconnected from other entries and cards. A file attached to
// an entry does not connect it to anything, and the orphan fix says so.
func TestAnEntryLinkedOnlyToAnArtifactIsStillAnOrphan(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "alone.png", "\x89PNG\r\n\x1a\nx")
	doc := entryNaming(t, c, p.ID, "Alone", a.Name)
	for _, f := range findingsFor(t, c, p.ID, doc.Slug) {
		if f.Kind == "orphan" {
			return
		}
	}
	t.Error("an entry whose only link is an artifact was not reported as an orphan")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestLintReports|TestLintIsQuiet|TestAnEntryLinkedOnly' -v`
Expected: `TestLintReportsAMissingArtifact`, `TestLintReportsAnAmbiguousArtifact` and `TestAnEntryLinkedOnlyToAnArtifactIsStillAnOrphan` FAIL; `TestLintIsQuietAboutAResolvedArtifact` PASSES.

- [ ] **Step 3: Report missing artifacts**

In `internal/core/lint.go`, add `"strconv"` to the imports. Change the `Kind` comment on `LintFinding` to:

```go
	Kind string `json:"kind"` // stub, broken_anchor, orphan, missing_artifact
```

Inside `Lint`, in the per-entry loop, directly after the `for _, r := range rows { ... }` loop and before `var inbound int`, insert:

```go
			var artifactStubs []string
			if err := tx.Select(&artifactStubs,
				`SELECT to_raw FROM link
				 WHERE from_type = 'doc' AND from_id = ? AND rel = 'artifact' AND to_id IS NULL
				 ORDER BY to_raw`, d.ID); err != nil {
				return err
			}
			for _, name := range artifactStubs {
				var matches int
				if err := tx.Get(&matches,
					`SELECT COUNT(*) FROM artifact WHERE project_id = ? AND name = ?`,
					d.ProjectID, name); err != nil {
					return err
				}
				f := LintFinding{Kind: "missing_artifact", Doc: d.Slug, Ref: name,
					Fix: "trellis artifact add <file>   # no artifact is named " + name}
				if matches > 1 {
					f.Fix = "trellis artifact ls   # " + strconv.Itoa(matches) +
						" artifacts are named " + name + "; remove the extra ones"
				}
				out = append(out, f)
			}
```

- [ ] **Step 4: Keep artifact links out of the orphan count**

In the same loop, the outbound count reads:

```go
			if err := tx.Get(&outbound,
				`SELECT COUNT(*) FROM link WHERE from_type = 'doc' AND from_id = ?`,
				d.ID); err != nil {
```

Change the query to:

```go
				`SELECT COUNT(*) FROM link WHERE from_type = 'doc' AND from_id = ? AND rel != 'artifact'`,
```

and extend the comment above it with: `An attached artifact is not a connection to another entry or card, so it does not count.`

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestLint|TestAnEntryLinkedOnly' -v`
Expected: PASS, including existing lint tests.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/core/lint.go internal/core/artifact_link_test.go
git commit -m "feat(core): lint reports artifacts an entry names but cannot resolve

A missing name points at artifact add; a shared name points at artifact ls.
An attached file does not stop an entry from being reported as an orphan.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: The CLI links artifacts to entries

**Files:**
- Modify: `internal/cli/artifact.go` (replace the whole file)
- Create: `internal/cli/artifact_cmd_test.go`

**Interfaces:**
- Consumes: `ResolveArtifact`, `LinkArtifactToDoc`, `UnlinkArtifactFromDoc`, `UnlinkArtifactFromCard`, `ListArtifacts(ctx, projectID, cardID, docID)`, `LoadKnowledge`; test helpers `projectEnv` and `runCmd` in `internal/cli/knowledge_cmd_test.go`.
- Produces: `trellis artifact add <file> [--card|--doc]`, `link <artifact> (--card|--doc)`, `unlink <artifact> (--card|--doc)`, `ls [--card|--doc]`, `rm <artifact>`; error codes `target_conflict`, `missing_target`.

`newRootCmd` sets `SilenceUsage` and `SilenceErrors`, so `Execute` returns the `*core.Error` rather than printing it; the error-case tests inspect it directly.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/artifact_cmd_test.go`:

```go
package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

// runCmdErr runs a command and returns its error instead of failing the test.
func runCmdErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

func cliErrCode(err error) string {
	if e, ok := errors.AsType[*core.Error](err); ok {
		return e.Code
	}
	return ""
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArtifactAddAttachesToAnEntry(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Standup")

	runCmd(t, "artifact", "add", writeFile(t, "standup.mp3", "ID3 audio"), "--doc", "standup")

	out := runCmd(t, "knowledge", "show", "standup", "--json")
	if !strings.Contains(out, `"standup.mp3"`) {
		t.Errorf("entry does not list the artifact:\n%s", out)
	}
}

func TestArtifactLinkAndUnlinkByName(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Research")
	runCmd(t, "artifact", "add", writeFile(t, "paper.pdf", "%PDF-1.7\n"))

	runCmd(t, "artifact", "link", "paper.pdf", "--doc", "research")
	listed := runCmd(t, "artifact", "ls", "--doc", "research", "--json")
	if !strings.Contains(listed, "paper.pdf") {
		t.Fatalf("ls --doc does not list the linked artifact:\n%s", listed)
	}

	runCmd(t, "artifact", "unlink", "paper.pdf", "--doc", "research")
	after := runCmd(t, "artifact", "ls", "--doc", "research", "--json")
	if strings.Contains(after, "paper.pdf") {
		t.Errorf("artifact still listed after unlink:\n%s", after)
	}
}

func TestArtifactLinkNeedsExactlyOneTarget(t *testing.T) {
	projectEnv(t)
	runCmd(t, "artifact", "add", writeFile(t, "x.png", "\x89PNG\r\n\x1a\nx"))

	if _, err := runCmdErr(t, "artifact", "link", "x.png"); cliErrCode(err) != "missing_target" {
		t.Errorf("no target: err = %v, want missing_target", err)
	}
	if _, err := runCmdErr(t, "artifact", "link", "x.png", "--card", "TEST-1", "--doc", "x"); cliErrCode(err) != "target_conflict" {
		t.Errorf("both targets: err = %v, want target_conflict", err)
	}
	if _, err := runCmdErr(t, "artifact", "unlink", "x.png"); cliErrCode(err) != "missing_target" {
		t.Errorf("unlink with no target: err = %v, want missing_target", err)
	}
}

func TestArtifactRmAcceptsAName(t *testing.T) {
	projectEnv(t)
	runCmd(t, "artifact", "add", writeFile(t, "old.png", "\x89PNG\r\n\x1a\nx"))

	runCmd(t, "artifact", "rm", "old.png")

	if strings.Contains(runCmd(t, "artifact", "ls", "--json"), "old.png") {
		t.Error("artifact still listed after rm by name")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli -run 'TestArtifact' -v`
Expected: FAIL — `unknown flag: --doc` and `unknown command "unlink"`.

- [ ] **Step 3: Replace the command file**

Replace `internal/cli/artifact.go` entirely:

```go
package cli

import (
	"fmt"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newArtifactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifact",
		Short: "Store files and attach them to cards or knowledge entries",
		Long: "An <artifact> argument is an artifact id or its name. Attaching one to a\n" +
			"knowledge entry adds its name to the entry's `artifacts` frontmatter.",
	}
	cmd.AddCommand(newArtifactAddCmd(), newArtifactLsCmd(), newArtifactLinkCmd(),
		newArtifactUnlinkCmd(), newArtifactRmCmd())
	return cmd
}

// oneTarget enforces the --card / --doc choice. required means exactly one;
// otherwise at most one.
func oneTarget(card, doc string, required bool, usage string) error {
	switch {
	case card != "" && doc != "":
		return core.ErrUsage("target_conflict", "pass --card or --doc, not both", usage)
	case required && card == "" && doc == "":
		return core.ErrUsage("missing_target", "pass --card <ref> or --doc <slug>", usage)
	}
	return nil
}

func addTargetFlags(cmd *cobra.Command, card, doc *string, verb string) {
	cmd.Flags().StringVar(card, "card", "", verb+" a card reference")
	cmd.Flags().StringVar(doc, "doc", "", verb+" a knowledge entry slug")
}

func newArtifactAddCmd() *cobra.Command {
	var card, doc string
	cmd := &cobra.Command{
		Use:   "add <file>",
		Short: "Copy an image, recording, PDF or other permitted file into Trellis",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneTarget(card, doc, false, "trellis artifact add <file> [--card <ref> | --doc <slug>]"); err != nil {
				return err
			}
			return withBoard(func(app *appCtx) error {
				artifact, err := app.Core.CreateArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				switch {
				case card != "":
					id, err := cardID(cmd, app, card)
					if err != nil {
						return err
					}
					if err := app.Core.LinkArtifactToCard(cmd.Context(), app.Project.ID, id, artifact.ID); err != nil {
						return err
					}
				case doc != "":
					if _, err := app.Core.LinkArtifactToDoc(cmd.Context(), app.Project.ID, doc, artifact.ID); err != nil {
						return err
					}
				}
				return Emit(cmd, artifact, func() string { return artifact.ID + "  " + artifact.Name })
			})
		},
	}
	addTargetFlags(cmd, &card, &doc, "attach to")
	return cmd
}

func newArtifactLinkCmd() *cobra.Command {
	var card, doc string
	cmd := &cobra.Command{
		Use:   "link <artifact>",
		Short: "Attach an existing artifact to a card or a knowledge entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			usage := "trellis artifact link <artifact> --card <ref> | --doc <slug>"
			if err := oneTarget(card, doc, true, usage); err != nil {
				return err
			}
			return withBoard(func(app *appCtx) error {
				a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				if doc != "" {
					entry, err := app.Core.LinkArtifactToDoc(cmd.Context(), app.Project.ID, doc, a.ID)
					if err != nil {
						return err
					}
					return Emit(cmd, map[string]any{"artifact": a.ID, "name": a.Name, "doc": entry.Slug},
						func() string { return entry.Slug + " -> " + a.Name })
				}
				id, err := cardID(cmd, app, card)
				if err != nil {
					return err
				}
				if err := app.Core.LinkArtifactToCard(cmd.Context(), app.Project.ID, id, a.ID); err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"artifact": a.ID, "name": a.Name, "card": card},
					func() string { return card + " -> " + a.Name })
			})
		},
	}
	addTargetFlags(cmd, &card, &doc, "attach to")
	return cmd
}

func newArtifactUnlinkCmd() *cobra.Command {
	var card, doc string
	cmd := &cobra.Command{
		Use:   "unlink <artifact>",
		Short: "Detach an artifact from a card or a knowledge entry",
		Long: "Detaching from an entry removes the name from its `artifacts` frontmatter.\n" +
			"A name whose artifact no longer exists can still be removed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			usage := "trellis artifact unlink <artifact> --card <ref> | --doc <slug>"
			if err := oneTarget(card, doc, true, usage); err != nil {
				return err
			}
			return withBoard(func(app *appCtx) error {
				if doc != "" {
					entry, err := app.Core.UnlinkArtifactFromDoc(cmd.Context(), app.Project.ID, doc, args[0])
					if err != nil {
						return err
					}
					return Emit(cmd, map[string]any{"artifact": args[0], "doc": entry.Slug},
						func() string { return entry.Slug + " -x- " + args[0] })
				}
				a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				id, err := cardID(cmd, app, card)
				if err != nil {
					return err
				}
				if err := app.Core.UnlinkArtifactFromCard(cmd.Context(), app.Project.ID, id, a.ID); err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"artifact": a.ID, "name": a.Name, "card": card},
					func() string { return card + " -x- " + a.Name })
			})
		},
	}
	addTargetFlags(cmd, &card, &doc, "detach from")
	return cmd
}

func newArtifactLsCmd() *cobra.Command {
	var card, doc string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List stored artifacts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := oneTarget(card, doc, false, "trellis artifact ls [--card <ref> | --doc <slug>]"); err != nil {
				return err
			}
			return withBoard(func(app *appCtx) error {
				var cardIDValue, docIDValue string
				switch {
				case card != "":
					id, err := cardID(cmd, app, card)
					if err != nil {
						return err
					}
					cardIDValue = id
				case doc != "":
					entry, err := app.Core.LoadKnowledge(cmd.Context(), app.Project.ID, doc)
					if err != nil {
						return err
					}
					docIDValue = entry.ID
				}
				items, err := app.Core.ListArtifacts(cmd.Context(), app.Project.ID, cardIDValue, docIDValue)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"artifacts": items}, func() string {
					var b strings.Builder
					for _, item := range items {
						fmt.Fprintf(&b, "%s  %s  %s\n", item.ID, item.Kind, item.Path)
					}
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
	addTargetFlags(cmd, &card, &doc, "only artifacts attached to")
	return cmd
}

func newArtifactRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <artifact>",
		Short: "Delete an artifact; entries that name it keep the name as a stub",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				if err := app.Core.DeleteArtifact(cmd.Context(), app.Project.ID, a.ID); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"deleted": a.ID, "name": a.Name},
					func() string { return "deleted " + a.Name })
			})
		},
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli -run 'TestArtifact' -v`
Expected: PASS.

- [ ] **Step 5: Run the full gates, including the other platforms**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./... && GOOS=darwin go build ./...`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/artifact.go internal/cli/artifact_cmd_test.go
git commit -m "feat(cli): attach artifacts to knowledge entries

add, link and ls take --doc beside --card; unlink is new for both; every
command takes an artifact id or its name. Passing both targets, or none
where one is required, is refused.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Core resolves an artifact for serving, and only inside its directory

**Files:**
- Modify: `internal/core/artifact.go` (split `artifactDir`; new `ArtifactFile`)
- Test: `internal/core/artifact_link_test.go`

**Interfaces:**
- Consumes: Task 1 test helpers.
- Produces:
  - `func (c *Core) artifactDirPath(projectKey string) (string, error)` — computes the directory without creating it
  - `func (c *Core) ArtifactFile(ctx context.Context, projectID, name string) (Artifact, string, error)` — returns the artifact and the resolved path to open, or `artifact_not_found`

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/artifact_link_test.go`:

```go
func TestArtifactFileResolvesARegisteredArtifact(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "serve.png", "\x89PNG\r\n\x1a\nx")

	got, path, err := c.ArtifactFile(t.Context(), p.ID, a.Name)
	if err != nil {
		t.Fatalf("ArtifactFile: %v", err)
	}
	want, err := filepath.EvalSymlinks(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || path != want {
		t.Errorf("got %s at %q, want %s at %q", got.ID, path, a.ID, want)
	}
}

func TestArtifactFileRefuses(t *testing.T) {
	cases := map[string]func(t *testing.T, c *Core, projectID string, a Artifact) string{
		"an unknown name": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			return "nope.png"
		},
		"a shared name": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			insertDuplicateArtifact(t, c, projectID, a)
			return a.Name
		},
		"a registered artifact whose file is gone": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			if err := os.Remove(a.Path); err != nil {
				t.Fatal(err)
			}
			return a.Name
		},
		"a row whose path lies outside the directory": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			outside := filepath.Join(t.TempDir(), "secret.txt")
			if err := os.WriteFile(outside, []byte("not yours"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := c.db.Exec(`UPDATE artifact SET path = ? WHERE id = ?`, outside, a.ID); err != nil {
				t.Fatal(err)
			}
			return a.Name
		},
		"an unregistered file placed in the directory": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			sneaky := filepath.Join(filepath.Dir(a.Path), "sneaky.png")
			if err := os.WriteFile(sneaky, []byte("\x89PNG\r\n\x1a\nx"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "sneaky.png"
		},
		"a symlink leading out of the directory": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			outside := filepath.Join(t.TempDir(), "secret.txt")
			if err := os.WriteFile(outside, []byte("not yours"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(filepath.Dir(a.Path), "link.png")
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlinks unavailable here: %v", err)
			}
			if _, err := c.db.Exec(`UPDATE artifact SET path = ?, name = 'link.png' WHERE id = ?`, link, a.ID); err != nil {
				t.Fatal(err)
			}
			return "link.png"
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			c, p, _ := kbCore(t)
			a := addArtifact(t, c, p.ID, "base.png", "\x89PNG\r\n\x1a\nx")
			ref := setup(t, c, p.ID, a)
			if _, _, err := c.ArtifactFile(t.Context(), p.ID, ref); artifactErrCode(err) != "artifact_not_found" {
				t.Errorf("err = %v, want artifact_not_found", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestArtifactFile' -v`
Expected: the package does not compile — `c.ArtifactFile undefined`.

- [ ] **Step 3: Compute the directory without creating it**

In `internal/core/artifact.go`, replace `artifactDir` with:

```go
// artifactDirPath is where a project's artifacts live. It does not create the
// directory, because serving must not write.
func (c *Core) artifactDirPath(projectKey string) (string, error) {
	root, err := c.root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "projects", projectKey, "artifacts"), nil
}

func (c *Core) artifactDir(projectKey string) (string, error) {
	dir, err := c.artifactDirPath(projectKey)
	if err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, 0o700)
}
```

- [ ] **Step 4: Add `ArtifactFile`**

Add to `internal/core/artifact.go`:

```go
// ArtifactFile resolves an artifact by name for serving and returns the path to
// open. Every refusal is the same not-found error, so a caller learns nothing
// about why.
//
// The stored path comes from the database, so it is checked against the
// project's artifact directory after resolving symlinks on both sides. A row
// edited or restored from elsewhere, or a symlink planted in the directory, must
// not become a way to read an arbitrary file. The name itself is only ever a
// lookup key and is never joined into a path.
func (c *Core) ArtifactFile(ctx context.Context, projectID, name string) (Artifact, string, error) {
	notFound := ErrNotFound("artifact_not_found", "no artifact "+name, "trellis artifact ls")
	var key string
	var matches []Artifact
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID); err != nil {
			return err
		}
		return tx.Select(&matches,
			`SELECT * FROM artifact WHERE project_id = ? AND name = ?`, projectID, name)
	})
	if err != nil {
		return Artifact{}, "", err
	}
	if len(matches) != 1 {
		return Artifact{}, "", notFound
	}
	a := matches[0]

	dirPath, err := c.artifactDirPath(key)
	if err != nil {
		return Artifact{}, "", err
	}
	dir, err := filepath.EvalSymlinks(dirPath)
	if err != nil {
		return Artifact{}, "", notFound
	}
	path, err := filepath.EvalSymlinks(a.Path)
	if err != nil {
		return Artifact{}, "", notFound
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == "." || filepath.IsAbs(rel) ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Artifact{}, "", notFound
	}
	return a, path, nil
}
```

`EvalSymlinks` is applied to both sides so the comparison is between canonical paths. That also covers macOS resolving `/var` to `/private/var` and Windows returning long names for 8.3 short ones. `filepath.Rel` fails across Windows volumes, and that failure is a refusal.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestArtifactFile|TestArtifact' -v`
Expected: PASS. The symlink subtest may report SKIP on Windows without developer mode, and that is acceptable.

- [ ] **Step 6: Run the full gates, including the other platforms**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/core/artifact.go internal/core/artifact_link_test.go
git commit -m "feat(core): resolve an artifact for serving, only inside its directory

Only a registered artifact whose stored path, with symlinks resolved, lies
inside the project's artifact directory is returned. Everything else is the
same not-found error.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: The web API returns each entry's artifacts

> **Precondition — for the controller, before this task is dispatched.** Another
> session is committing backend changes to `internal/ui/server.go` on
> `feat/memory-groundwork`. Confirm those commits have landed, then run
> `git merge --no-edit feat/memory-groundwork` in this worktree and run the full
> gates. Tasks 1–6 do not touch `internal/ui/`, so the merge should apply
> cleanly. **Do not start Task 7 or Task 8 until the merge is in.** Line numbers
> below are from 0e31a9c and will have moved: find the code by name.

**Files:**
- Create: `internal/ui/artifacts.go`
- Modify: `internal/ui/server.go` (`handleProjectKnowledgeList`, `handleKnowledgeList`)
- Create: `internal/ui/artifacts_test.go`

**Interfaces:**
- Consumes: `core.Knowledge.Artifacts`, `core.ArtifactRef`, `core.LinkArtifactToDoc`, `core.SplitFrontmatter`, `core.RenderDoc`.
- Produces:
  - `type artifactItem struct { core.ArtifactRef; URL string }`
  - `type knowledgeItem struct { core.Knowledge; Artifacts []artifactItem }`
  - `func knowledgeItems(projectKey string, docs []core.Knowledge) []knowledgeItem`
  - test helpers `artifactTestServer`, `storeArtifact`

The repository encodes with `encoding/json/v2`. It resolves an embedded struct's field that a shallower field shadows in favour of the shallower one, without error, so `knowledgeItem.Artifacts` replaces the embedded `core.Knowledge.Artifacts` in the output. This was verified against the repository's Go 1.27 toolchain.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/artifacts_test.go`:

```go
package ui

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/store"
)

func artifactTestServer(t *testing.T) (*Server, *core.Core, core.Project) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-artifact-test").WithKBRoot(t.TempDir())
	p, err := c.EnsureProject(t.Context(), resolve.Identity{Kind: "test", Value: "art", SuggestedKey: "ART"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(t.Context(), p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	return NewServer(c, db, "127.0.0.1:0"), c, p
}

func storeArtifact(t *testing.T, c *core.Core, projectID, filename string, content []byte) core.Artifact {
	t.Helper()
	src := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(src, content, 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := c.CreateArtifact(t.Context(), projectID, src)
	if err != nil {
		t.Fatalf("CreateArtifact %s: %v", filename, err)
	}
	return a
}

func getJSON(t *testing.T, s *Server, path string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body)
	}
	var items []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return items
}

func itemBySlug(t *testing.T, items []map[string]any, slug string) map[string]any {
	t.Helper()
	for _, it := range items {
		if it["slug"] == slug {
			return it
		}
	}
	t.Fatalf("no item with slug %q", slug)
	return nil
}

func TestKnowledgeListsCarryArtifacts(t *testing.T) {
	s, c, p := artifactTestServer(t)
	a := storeArtifact(t, c, p.ID, "clip.mp3", []byte("ID3 audio"))

	with, err := c.CreateKnowledge(t.Context(), p.ID, core.NewKnowledge{Title: "With"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.LinkArtifactToDoc(t.Context(), p.ID, with.Slug, a.Name); err != nil {
		t.Fatal(err)
	}

	stub, err := c.CreateKnowledge(t.Context(), p.ID, core.NewKnowledge{Title: "Stub"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(stub.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, body, err := core.SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	fm.Artifacts = []string{"absent.pdf"}
	if err := os.WriteFile(stub.Path, []byte(core.RenderDoc(fm, body)), 0o600); err != nil {
		t.Fatal(err)
	}

	plain, err := c.CreateKnowledge(t.Context(), p.ID, core.NewKnowledge{Title: "Plain"})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/p/ART/knowledge", "/api/p/ART/b/default/knowledge"} {
		t.Run(path, func(t *testing.T) {
			items := getJSON(t, s, path)

			arts, ok := itemBySlug(t, items, with.Slug)["artifacts"].([]any)
			if !ok || len(arts) != 1 {
				t.Fatalf("with: artifacts = %v", itemBySlug(t, items, with.Slug)["artifacts"])
			}
			got := arts[0].(map[string]any)
			if got["name"] != a.Name || got["kind"] != "audio" || got["missing"] != false ||
				got["url"] != "/api/p/ART/artifacts/"+a.Name {
				t.Errorf("resolved artifact = %v", got)
			}

			stubArts, ok := itemBySlug(t, items, stub.Slug)["artifacts"].([]any)
			if !ok || len(stubArts) != 1 {
				t.Fatalf("stub: artifacts = %v", itemBySlug(t, items, stub.Slug)["artifacts"])
			}
			missing := stubArts[0].(map[string]any)
			if missing["missing"] != true {
				t.Errorf("stub = %v, want missing: true", missing)
			}
			for _, k := range []string{"url", "kind", "mime", "size"} {
				if _, present := missing[k]; present {
					t.Errorf("stub carries %q: %v", k, missing)
				}
			}

			if _, present := itemBySlug(t, items, plain.Slug)["artifacts"]; present {
				t.Errorf("an entry with no artifacts carries the field")
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui -run TestKnowledgeListsCarryArtifacts -v`
Expected: FAIL — the resolved artifact has no `url`.

- [ ] **Step 3: Add the response shape**

Create `internal/ui/artifacts.go`:

```go
package ui

import (
	"net/url"

	"github.com/mtch3n/trellis/internal/core"
)

// artifactItem is an entry's artifact as the UI receives it: the core reference
// plus the URL to fetch it from. Core does not know the UI's routes, so the URL
// is added here. A missing artifact has no URL.
type artifactItem struct {
	core.ArtifactRef
	URL string `json:"url,omitempty"`
}

// knowledgeItem is a knowledge entry as the UI list endpoints return it. Its
// Artifacts field shadows the embedded one; encoding/json/v2 resolves that in
// favour of the shallower field.
type knowledgeItem struct {
	core.Knowledge
	Artifacts []artifactItem `json:"artifacts,omitempty"`
}

func knowledgeItems(projectKey string, docs []core.Knowledge) []knowledgeItem {
	out := make([]knowledgeItem, len(docs))
	for i, d := range docs {
		out[i].Knowledge = d
		for _, a := range d.Artifacts {
			item := artifactItem{ArtifactRef: a}
			if !a.Missing {
				item.URL = artifactURL(projectKey, a.Name)
			}
			out[i].Artifacts = append(out[i].Artifacts, item)
		}
	}
	return out
}

func artifactURL(projectKey, name string) string {
	return "/api/p/" + url.PathEscape(projectKey) + "/artifacts/" + url.PathEscape(name)
}
```

- [ ] **Step 4: Return that shape from both list handlers**

In `internal/ui/server.go`, `handleProjectKnowledgeList` ends with `writeJSON(w, http.StatusOK, docs)`. Change that line to:

```go
	writeJSON(w, http.StatusOK, knowledgeItems(p.Key, docs))
```

`handleKnowledgeList`, the handler registered for `GET /api/p/{key}/b/{board}/knowledge`, resolves `p, b, err := s.projectAndBoard(...)` and ends with `writeJSON(w, http.StatusOK, docs)`. Change that line to:

```go
	writeJSON(w, http.StatusOK, knowledgeItems(p.Key, docs))
```

Leave `handleGlobalKnowledgeList` unchanged: the spec says global entries never carry artifacts.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/ui -run TestKnowledgeListsCarryArtifacts -v`
Expected: PASS for both paths.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass, including the other session's merged UI tests.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/artifacts.go internal/ui/server.go internal/ui/artifacts_test.go
git commit -m "feat(ui): knowledge lists carry each entry's artifacts

A resolved artifact carries the URL the UI fetches it from; a missing one
carries only its name and missing: true.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: The web API serves an artifact safely

> Same precondition as Task 7: the merge from `feat/memory-groundwork` must be in.

**Files:**
- Modify: `internal/ui/artifacts.go` (add the handler and headers)
- Modify: `internal/ui/server.go` (one route)
- Test: `internal/ui/artifacts_test.go`

**Interfaces:**
- Consumes: `core.ArtifactFile`, `Server.protectedHandler`, `Server.token`, the Task 7 test helpers.
- Produces: route `GET /api/p/{key}/artifacts/{name}`; `func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request)`; `func setArtifactHeaders(h http.Header, a core.Artifact)`.

`http.ServeContent` answers Range, conditional and HEAD requests. It sniffs the body only when `Content-Type` is unset, so the headers must be set **before** it is called. The server sets no `WriteTimeout` (`internal/ui/security.go`), so long media streams are not cut off.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/artifacts_test.go`, and add `"mime"`, `"net/url"`, `"strconv"` and `"strings"` to its imports:

```go
var (
	pngBytes  = []byte("\x89PNG\r\n\x1a\nimage-data")
	mp3Bytes  = []byte("ID3\x03\x00audio-data")
	mp4Bytes  = []byte("\x00\x00\x00\x10ftypmp42\x00\x00\x00\x00video-data")
	pdfBytes  = []byte("%PDF-1.7\npdf-data")
	htmlBytes = []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")
	textBytes = []byte("plain notes, long enough to take a range from")
	zipBytes  = []byte("PK\x03\x04zip-data")
)

func serve(s *Server, method, path string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func TestArtifactHeadersByType(t *testing.T) {
	cases := []struct {
		file        string
		content     []byte
		contentType string
		sandbox     bool
		disposition string
	}{
		{"shot.png", pngBytes, "image/png", true, "inline"},
		{"clip.mp3", mp3Bytes, "audio/mpeg", true, "inline"},
		{"clip.mp4", mp4Bytes, "video/mp4", true, "inline"},
		{"paper.pdf", pdfBytes, "application/pdf", false, "inline"},
		{"page.html", htmlBytes, "text/plain; charset=utf-8", true, "inline"},
		{"notes.txt", textBytes, "text/plain; charset=utf-8", true, "inline"},
		{"bundle.zip", zipBytes, "application/zip", true, "attachment"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			s, c, p := artifactTestServer(t)
			a := storeArtifact(t, c, p.ID, tc.file, tc.content)

			rec := serve(s, http.MethodGet, artifactURL("ART", a.Name), nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			h := rec.Header()
			if got := h.Get("Content-Type"); got != tc.contentType {
				t.Errorf("Content-Type = %q, want %q (stored %q)", got, tc.contentType, a.MIME)
			}
			if h.Get("X-Content-Type-Options") != "nosniff" {
				t.Error("missing nosniff")
			}
			if got := h.Get("Content-Security-Policy") == "sandbox"; got != tc.sandbox {
				t.Errorf("sandbox = %v, want %v", got, tc.sandbox)
			}
			disposition, params, err := mime.ParseMediaType(h.Get("Content-Disposition"))
			if err != nil {
				t.Fatalf("Content-Disposition %q: %v", h.Get("Content-Disposition"), err)
			}
			if disposition != tc.disposition || params["filename"] != a.Name {
				t.Errorf("Content-Disposition = %s %v, want %s filename=%s", disposition, params, tc.disposition, a.Name)
			}
			if rec.Body.String() != string(tc.content) {
				t.Error("body differs from the stored file")
			}
		})
	}
}

// Go's content sniffing never produces image/svg+xml, so an SVG upload is
// stored as text. A row that does carry the type — edited, restored, or from a
// future detector — must still be sandboxed, because SVG can carry script.
func TestAnSVGRowIsSandboxed(t *testing.T) {
	s, c, p := artifactTestServer(t)
	base := storeArtifact(t, c, p.ID, "base.png", pngBytes)
	svgPath := filepath.Join(filepath.Dir(base.Path), "drawing.svg")
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if err := os.WriteFile(svgPath, svg, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO artifact (id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at)
		 VALUES (?, ?, 'drawing.svg', ?, 'image', 'image/svg+xml', ?, 'h', 1, 1)`,
		core.NewCardID(), p.ID, svgPath, len(svg)); err != nil {
		t.Fatal(err)
	}

	rec := serve(s, http.MethodGet, artifactURL("ART", "drawing.svg"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", got)
	}
	if rec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Error("an SVG was served without sandbox")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
}

// Media must seek, and the frontend caps text previews at 256 KB with a Range
// request, so both must answer 206 with the requested bytes.
func TestArtifactRange(t *testing.T) {
	for _, tc := range []struct {
		file        string
		content     []byte
		contentType string
	}{
		{"clip.mp3", mp3Bytes, "audio/mpeg"},
		{"notes.txt", textBytes, "text/plain; charset=utf-8"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			s, c, p := artifactTestServer(t)
			a := storeArtifact(t, c, p.ID, tc.file, tc.content)

			rec := serve(s, http.MethodGet, artifactURL("ART", a.Name), http.Header{"Range": {"bytes=0-4"}})
			if rec.Code != http.StatusPartialContent {
				t.Fatalf("status = %d, want 206", rec.Code)
			}
			if rec.Body.String() != string(tc.content[:5]) {
				t.Errorf("body = %q, want %q", rec.Body.String(), tc.content[:5])
			}
			if want := "bytes 0-4/" + strconv.Itoa(len(tc.content)); rec.Header().Get("Content-Range") != want {
				t.Errorf("Content-Range = %q, want %q", rec.Header().Get("Content-Range"), want)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Errorf("206 Content-Type = %q, want %q", got, tc.contentType)
			}
		})
	}
}

func TestArtifactHead(t *testing.T) {
	s, c, p := artifactTestServer(t)
	a := storeArtifact(t, c, p.ID, "shot.png", pngBytes)
	rec := serve(s, http.MethodHead, artifactURL("ART", a.Name), nil)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("HEAD = %d, %d bytes, %q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
	}
}

func TestArtifactNotFound(t *testing.T) {
	cases := map[string]func(t *testing.T, s *Server, c *core.Core, p core.Project, a core.Artifact) string{
		"unknown project": func(t *testing.T, s *Server, c *core.Core, p core.Project, a core.Artifact) string {
			return artifactURL("NOPE", a.Name)
		},
		"unknown name": func(t *testing.T, s *Server, c *core.Core, p core.Project, a core.Artifact) string {
			return artifactURL("ART", "nope.png")
		},
		"file gone": func(t *testing.T, s *Server, c *core.Core, p core.Project, a core.Artifact) string {
			if err := os.Remove(a.Path); err != nil {
				t.Fatal(err)
			}
			return artifactURL("ART", a.Name)
		},
		"path outside the directory": func(t *testing.T, s *Server, c *core.Core, p core.Project, a core.Artifact) string {
			outside := filepath.Join(t.TempDir(), "secret.txt")
			if err := os.WriteFile(outside, []byte("not yours"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE artifact SET path = ? WHERE id = ?`, outside, a.ID); err != nil {
				t.Fatal(err)
			}
			return artifactURL("ART", a.Name)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			s, c, p := artifactTestServer(t)
			a := storeArtifact(t, c, p.ID, "base.png", pngBytes)
			rec := serve(s, http.MethodGet, setup(t, s, c, p, a), nil)
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "not yours") {
				t.Error("served a file from outside the artifact directory")
			}
		})
	}
}

// The route sits behind protectedHandler like every other /api/ route.
func TestArtifactRouteIsProtected(t *testing.T) {
	s, c, p := artifactTestServer(t)
	a := storeArtifact(t, c, p.ID, "shot.png", pngBytes)
	h := s.protectedHandler("127.0.0.1:0")

	do := func(host, token string) int {
		req := httptest.NewRequest(http.MethodGet, artifactURL("ART", a.Name), nil)
		req.Host = host
		if token != "" {
			req.Header.Set("X-Trellis-Token", token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := do("127.0.0.1:0", ""); got != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", got)
	}
	if got := do("127.0.0.1:0", s.token); got != http.StatusOK {
		t.Errorf("with token: %d, want 200", got)
	}
	if got := do("evil.example:0", s.token); got != http.StatusForbidden {
		t.Errorf("foreign host: %d, want 403", got)
	}
}

// A name is only a lookup key, but it is echoed into Content-Disposition, so a
// hostile one must not break the header.
func TestAFilenameCannotBreakTheDispositionHeader(t *testing.T) {
	s, c, p := artifactTestServer(t)
	base := storeArtifact(t, c, p.ID, "base.png", pngBytes)
	copyPath := filepath.Join(filepath.Dir(base.Path), "copy.png")
	if err := os.WriteFile(copyPath, pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	evil := "evil\".png\r\nX-Injected: yes"
	if _, err := s.db.Exec(
		`INSERT INTO artifact (id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 1)`,
		core.NewCardID(), p.ID, evil, copyPath, base.Kind, base.MIME, base.Size, base.ContentHash); err != nil {
		t.Fatal(err)
	}

	rec := serve(s, http.MethodGet, "/api/p/ART/artifacts/"+url.PathEscape(evil), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	v := rec.Header().Get("Content-Disposition")
	if strings.ContainsAny(v, "\r\n") {
		t.Fatalf("Content-Disposition carries a raw line break: %q", v)
	}
	_, params, err := mime.ParseMediaType(v)
	if err != nil {
		t.Fatalf("Content-Disposition %q does not parse: %v", v, err)
	}
	if params["filename"] != evil {
		t.Errorf("filename = %q, want the name round-tripped", params["filename"])
	}
}
```

`Server` holds its database as `db *sqlx.DB` and its session token as `token string`, so the tests use `s.db` and `s.token` directly. `coreError` maps a `core.Error` with `Exit: 3` — which is what `ErrNotFound` sets — to HTTP 404, and `s.error` writes its message JSON-encoded, so a name echoed in an error body is inert.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui -run 'TestArtifact|TestAFilename|TestAnSVG' -v`
Expected: FAIL — every request answers 404, or the SPA fallback, because the route does not exist.

- [ ] **Step 3: Add the handler and the headers**

Append to `internal/ui/artifacts.go`, and add `"context"`, `"mime"`, `"net/http"`, `"os"` and `"strings"` to its imports:

```go
// handleArtifact serves an artifact's bytes for preview. It is registered under
// /api/, so protectedHandler has already checked the host, the origin and the
// session token before this runs.
func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, r.PathValue("key")); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}
	a, path, err := s.core.ArtifactFile(ctx, p.ID, r.PathValue("name"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		s.error(w, http.StatusNotFound, "artifact file not found")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Headers first: ServeContent sniffs the body only when Content-Type is
	// unset, and sniffing is exactly what must not happen here.
	setArtifactHeaders(w.Header(), a)
	http.ServeContent(w, r, a.Name, st.ModTime(), f)
}

// setArtifactHeaders chooses what a browser may do with an artifact. Artifacts
// are arbitrary user files served from the UI's own origin, and the accepted
// types include HTML and SVG, both of which can carry script:
//
//   - text of any kind, HTML included, is sent as plain text, so it never
//     renders as a page;
//   - every response except PDF is sandboxed, so anything that does render as
//     a document runs in an opaque origin with no reach into the UI. A
//     resource's CSP applies only when it renders as a document, so <img>,
//     <audio> and <video> previews are unaffected;
//   - PDF is not sandboxed, because browsers refuse to start their PDF viewer
//     in a sandboxed document, and they render PDF in an isolated viewer;
//   - archives, and anything unrecognised, are downloads.
func setArtifactHeaders(h http.Header, a core.Artifact) {
	media, _, err := mime.ParseMediaType(a.MIME)
	if err != nil {
		media = ""
	}
	contentType, disposition, sandbox := media, "inline", true
	switch {
	case media == "application/pdf":
		sandbox = false
	case strings.HasPrefix(media, "text/"):
		contentType = "text/plain; charset=utf-8"
	case strings.HasPrefix(media, "image/"),
		strings.HasPrefix(media, "audio/"),
		strings.HasPrefix(media, "video/"):
	case media == "application/zip", media == "application/gzip", media == "application/x-tar":
		disposition = "attachment"
	default:
		contentType, disposition = "application/octet-stream", "attachment"
	}
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	if sandbox {
		h.Set("Content-Security-Policy", "sandbox")
	}
	// FormatMediaType quotes the filename and falls back to RFC 2231 encoding
	// for anything unsafe, so a hostile name cannot break the header. It
	// returns "" only for an invalid disposition token, which ours never is.
	if v := mime.FormatMediaType(disposition, map[string]string{"filename": a.Name}); v != "" {
		h.Set("Content-Disposition", v)
	} else {
		h.Set("Content-Disposition", disposition)
	}
}
```

SVG needs no case of its own. Go's content sniffing never produces `image/svg+xml` (an SVG file sniffs as `text/plain` or `text/xml`), so SVG artifacts are stored as text and served as plain text. A row that did carry `image/svg+xml` would fall into the image case and still be sandboxed.

- [ ] **Step 4: Register the route**

In `internal/ui/server.go`, beside the other `s.mux.HandleFunc("GET /api/p/{key}/...` registrations and **before** the `"/"` SPA fallback, add:

```go
	s.mux.HandleFunc("GET /api/p/{key}/artifacts/{name}", s.handleArtifact)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ui -run 'TestArtifact|TestAFilename|TestAnSVG|TestKnowledgeListsCarry' -v`
Expected: PASS.

- [ ] **Step 6: Run the full gates, including the other platforms**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./... && GOOS=darwin go build ./...`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/artifacts.go internal/ui/server.go internal/ui/artifacts_test.go
git commit -m "feat(ui): serve an artifact's bytes for preview, safely

Text, HTML included, is sent as plain text; every type but PDF is
sandboxed; archives are downloads; nosniff everywhere. Range, HEAD and
conditional requests are answered. The route sits behind the same host,
origin and token checks as the rest of the API.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## After the last task — not automatable

**Check the PDF preview in a real browser, with the web UI session.** A browser's PDF viewer may issue its own range requests for the same URL. If those requests arrive without the session cookie, or marked `Sec-Fetch-Site: cross-site`, `protectedHandler` answers 401 or 403 and the preview breaks. No Go test can settle this. If it fails, fix it in how that endpoint is treated, **not** by removing the checks.

## Self-review against the spec

| Spec requirement | Task |
|---|---|
| `artifacts: []string` in frontmatter, file as source of truth | 1 |
| Links derived in `syncDocRelations`; stub when unresolved | 1 |
| Resolution within the entry's project | 1 |
| A shared name resolves to nothing | 1, 3, 4, 6 |
| `CreateArtifact` checks the database for the name | 2 |
| No unique index | Global Constraints; no migration in any task |
| `CreateArtifact` backfills stubs | 2 |
| `DeleteArtifact` keeps entry links as stubs, removes card links | 2 |
| CLI `add / link / unlink / ls` with `--card | --doc`; id or name | 5 |
| Link / unlink edit the frontmatter through the edit path; no-op when unchanged | 3 |
| `unlink` for cards | 3, 5 |
| `artifact_linked` / `artifact_unlinked` events | 3 |
| `knowledge show` includes artifacts | 1 (`docView`), asserted in 5 |
| Lint `missing_artifact`, missing vs ambiguous | 4 |
| `stub` finding stays wikilink-only | 4 |
| List items carry `artifacts` with `url`; stub is name + `missing` only; field omitted when empty | 7 |
| Global list carries none | 7 (handler left unchanged) |
| Core does not know URLs | 7 (`artifactItem` lives in `internal/ui`) |
| Serving: project, name, confinement, file present, `ServeContent` | 6, 8 |
| Only registered artifacts served | 6, 8 |
| Headers per type, nosniff, sandbox except PDF (SVG included), text as plain, archive attachment, `FormatMediaType` | 8 |
| Content-Type set before `ServeContent` | 8 |
| Range for media **and** text | 8 |
| Endpoint inherits `protectedHandler` | 8 (asserted) |
| Hostile filename cannot break the header | 8 |
| Disclosure: no new automatic route | no task adds one; the endpoint answers only explicit requests |
| PDF preview through the browser's viewer | after the last task, manually |

**Deliberate differences from the spec, found while reading the code:**

- **`DeleteArtifact` deletes link rows explicitly as well as through the trigger.** The spec mentions only the trigger. Clearing `to_id` first keeps entry links out of both, so the behaviour the spec describes still holds.
- **Artifact names must not go through `dedupe`, which lower-cases.** The spec does not mention case. Extensions keep theirs (`photo.PNG`), and a lower-cased lookup would never resolve, so `dedupeNames` preserves case.
- **Orphan detection now ignores artifact links.** The spec is silent on this. Otherwise, attaching a file would stop an entry from being reported as disconnected, which contradicts the orphan finding's own fix text.
- **SVG is served as plain text in practice**, because Go never sniffs `image/svg+xml`. This is safe, but an uploaded SVG diagram previews as XML source, not as an image. It is recorded as a known limitation rather than worked around.
- **`CreateArtifact` does not sanitise the file extension**, so a name can carry unusual characters. `FormatMediaType` protects the header. The name is never joined into a path, and `ArtifactFile` checks the stored path, not the name.
