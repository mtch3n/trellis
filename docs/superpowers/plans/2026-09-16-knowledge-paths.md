# Knowledge Paths Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **WHERE TO WORK — read before anything else.** Every path in this plan is
> relative to the worktree **`/home/mtchen/Personal/trellis-worktrees/knowledge-artifacts`**
> on branch `feat/knowledge-artifacts`. Run every command from there, e.g.
> `cd /home/mtchen/Personal/trellis-worktrees/knowledge-artifacts && go test ./...`.
> **Never** run a command in, or edit a file under, `/home/mtchen/Personal/trellis`.
> That is a shared checkout where other sessions hold uncommitted work.

> **LAYER DEPENDENCY — read before starting Task 9.** This plan has two parts.
> **Tasks 1–8** touch only `internal/core` and `internal/cli` as they exist
> today and can be built, tested and committed right now, in order, on top of
> `feat/knowledge-artifacts`. **Task 9** requires
> `docs/superpowers/specs/2026-09-16-virtual-paths-design.md` to have already
> shipped (that spec itself ships after
> `docs/superpowers/specs/2026-09-16-pin-only-projects-design.md`; see that
> spec's "Layer 1 of 3" ordering). Virtual paths rewrites `ParseWikilinks`,
> `ParseReference` and `resolveDocRef` to remove the `KEY/slug` and
> `GLOBAL/slug` qualified forms from relative wikilink parsing. Task 9 assumes
> that rewrite has already happened and adapts *behavior*, not a line-numbered
> diff, on top of it — see the note at the top of Task 9 before touching any
> code. Do not implement Task 9, and do not reintroduce the old
> `[[GLOBAL:slug]]` syntax anywhere, until that dependency is confirmed
> merged.

**Goal:** Let a knowledge document live at a path, not just a flat slug —
`deployment/rollback-runbook` as well as `recall-ranking` — with directory
creation guarded against near-duplicate names, a bare filename resolving
unambiguously across the whole vault, and `knowledge mv` for when a document's
address needs to change.

**Architecture:** A path is still one opaque string in the existing
`knowledge.slug` column; `/` inside it is meaningful only to Trellis, never to
SQLite. Every place that builds one path segment from user text now uses one
of three functions — `SlugifyPath` (explicit input: rejects), `truncateSegment`
(a generated leaf: truncates) or `normalizeSlugPath` (a lookup: never errors,
just may not match) — so the three call sites in the design doc that used to
flatten a path with a single whole-string `Slugify` stop doing that. A new
directory is refused when an existing one resembles it, unless the caller
says otherwise. A slug with no `/` still resolves even when the document
lives in a directory, by matching the leaf; more than one match lists both
and opens neither. `knowledge mv` moves the file, the row and — if the
revision-history feature has landed by the time this runs — the entry's
hidden revision directory, and never replaces anything.

**Tech Stack:** Go 1.27, SQLite via `sqlx` (modernc driver), cobra.

**Spec:** `docs/superpowers/specs/2026-09-16-knowledge-paths-design.md`, with
its "Reference syntax" section superseded by
`docs/superpowers/specs/2026-09-16-virtual-paths-design.md`'s "Addresses",
"Wikilinks" and "Relation to knowledge paths" sections (see the note above).
Cross-checked against
`docs/superpowers/specs/2026-09-16-revision-history-design.md` (the hidden
`.<filename>/` revision directory that must move with an entry) and
`docs/superpowers/specs/2026-09-16-knowledge-artifacts-design.md` (already
implemented on this branch; this plan does not touch artifacts).

## Global Constraints

- **Layering.** Tasks 1–8 do not depend on `internal/vpath`, on virtual paths,
  or on pin-only projects, and must not import or anticipate any of them.
  Task 9 depends on virtual paths having shipped first; see the note above.
  Do not add the old `[[GLOBAL:slug]]` / `[[KEY/slug]]` wikilink syntax
  anywhere in this plan — the amendment removes it, and it is superseded, not
  ported.
- **No schema migration in this plan.** `knowledge.slug` is already `TEXT`
  with `UNIQUE (project_id, slug)` (`internal/store/migrations/0007_knowledge.sql`)
  and SQLite does not care that the string now contains `/`. `KnowledgeFilter.Tags`
  reuses the existing `tag` / `knowledge_tag` tables
  (`internal/store/migrations/0004_labels.sql`, `0007_knowledge.sql`) — no new
  table or index. The highest migration on this branch is
  `internal/store/migrations/0012_private.sql`; the parallel revision-history
  and event-feed plans each add one, and whichever of those lands after this
  plan renumbers around it, but this plan itself adds none.
- **Stored paths are absolute today** (card TRELLIS-36: `knowledge.path` holds
  an absolute filesystem path, computed from `TRELLIS_HOME`/`c.kbRoot` at
  write time, never persisted relative). This plan does not change that. Every
  function in this plan that moves a file computes the new absolute path from
  the current storage root and the new slug; nothing stores or interprets a
  relative path.
- **Slug construction never uses `filepath.Join` for the slug itself.**
  `filepath.Join` uses the OS separator; a slug is a stored, portable string
  and must always use `/`, regardless of platform. `filepath.Join` and
  `filepath.FromSlash` are used only once, at the point a slug becomes an
  on-disk path.
- **Every knowledge file write still goes through `writeAtomic` /
  `replaceIfUnchanged` and every delete through `stageRemoval`**
  (`internal/core/file_store.go`), inside one `Core.Tx`, with a `done` flag
  gating undo on failure or panic and durable-state resolution of an
  ambiguous `tx.Commit`, exactly as `CreateKnowledge`, `EditKnowledgeFields`,
  `DeleteKnowledge`, `EscalateKnowledge` and `DemoteKnowledge` already do. New
  functions in this plan (`MoveKnowledge`) follow the same shape.
- **`knowledge mv` never replaces anything.** A destination slug already in
  use is a conflict, never an overwrite; `moveFileTo`
  (`internal/core/knowledge_mv.go`, Task 4) refuses with `path_taken` the same
  way `moveFile` already does.
- **Windows reserved device names** (`con`, `prn`, `aux`, `nul`, `com1`–`com9`,
  `lpt1`–`lpt9`) are rejected in every explicit path segment and treated as
  permanently taken (never silently produced) in every generated one.
- **Length ceilings are enforced at write, not linted:** one path segment
  ≤ 96 characters, a full relative slug ≤ 180 characters.
- **Cross-platform.** CI runs Linux, macOS and Windows; all three must pass.
  A directory rename is `os.Rename` with a same-content-copy fallback (never
  a hard assumption that rename works across filesystems); revision
  directories are hidden only by convention on Unix and nothing depends on
  that. Every test in this plan that plants a filesystem fixture uses
  `t.TempDir()`, never a hard-coded path.
- **Gates before any task is called done:**
  `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` (must print
  nothing), `go test -race ./internal/core`, and
  `GOOS=windows go build ./...`.
- **Known flake:** `TestListKnowledgeFiltersByTypeAndProvenance/both_dimensions`
  fails about one run in five (TRELLIS-26, pre-existing —
  `KnowledgeFilter.where()` ranges over a Go map). If that exact test fails,
  say so and move on. **Investigate any other failure; never attribute it to
  this flake.**
- Stage by explicit path only. Before every commit run
  `git diff --cached --name-only` and confirm each path is one this task
  changed.
- Every commit message ends with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/core/knowledge_path.go` | Create: path validation (`SlugifyPath`, `truncateSegment`, `normalizeSlugPath`), Windows reserved names, resemblance (`resembles`, `boundedEditDistance`), directory enumeration and refusal (`projectDirectories`, `refuseResemblingDir`), bare-leaf resolution (`resolveSlug`, `notFoundSlug`) |
| `internal/core/knowledge_path_test.go` | Create: pure unit tests for every function above |
| `internal/core/knowledge_mv.go` | Create: revision-directory move (`revisionDirFor`, `moveRevisionDirIfExists`, `copyDirFlat`), `MoveKnowledge`; Task 9 adds inbound-wikilink rewriting here |
| `internal/core/knowledge_dir_test.go` | Create: every DB-backed behavior added by Tasks 2–4, 6, 7, 9 |
| `internal/core/knowledge.go` | Modify: `NewKnowledge.Dir`/`.NewDir`, `CreateKnowledge`, `uniqueSlug`, `loadDoc`, `DeleteKnowledge`, `KnowledgeFilter.Tags`/`.Dir`, `where()` |
| `internal/core/pin.go` | Modify: `moveFile` becomes a thin wrapper over new `moveFileTo`; `EscalateKnowledge`/`DemoteKnowledge` preserve the subpath and carry the revision directory; `DemoteKnowledge`/`VerifyKnowledge` resolve a directory-shaped slug |
| `internal/core/lint.go` | Modify: `deep_directory`, `long_directory_name`, `similar_directory` findings (Task 7); `ambiguous_link` and the anchor check's per-segment slug (Task 9) |
| `internal/core/markdown.go` | Modify (Task 9 only): `ParseWikilinks`/`ParseReference` use `normalizeSlugPath` for a relative target |
| `internal/core/doc_relations.go` | Modify (Task 9 only): `resolveDocRef` bare-leaf fallback |
| `internal/cli/knowledge.go` | Modify: `--in`/`--new-dir` on `new`; new `mv`; `--tag` and a `[dir]` argument on `ls`; `renderKnowledgeList` groups by directory |
| `internal/cli/knowledge_dir_cmd_test.go` | Create: CLI-level tests for the above |
| `plugin/hooks/trellis_hook.py` | Modify: the session-start command list advertises `recall` |
| `scripts/tests/test_plugin_hooks.py` | Modify: one new test for the line above |

---

### Task 1: Path validation and slug construction primitives

**Needs virtual paths:** no.

**Files:**
- Create: `internal/core/knowledge_path.go`
- Create: `internal/core/knowledge_path_test.go`

**Interfaces:**
- Consumes: `Slugify` (`internal/core/markdown.go`), `Error`/`ErrUsage` (`internal/core/errors.go`).
- Produces (all pure, no `sqlx.Tx`, used by later tasks):
  - `const maxPathSegmentLen = 96`, `const maxRelSlugLen = 180`
  - `func SlugifyPath(raw string) (string, error)`
  - `func truncateSegment(seg string) string`
  - `func normalizeSlugPath(raw string) string`
  - `func resembles(a, b string) bool`
  - `func boundedEditDistance(a, b string, max int) int`
  - `var reservedDeviceNames map[string]bool`
  - `func reservedLeafTaken(slug string) bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/core/knowledge_path_test.go`:

```go
package core

import (
	"errors"
	"testing"
)

func TestSlugifyPathAcceptsAPlainDirectory(t *testing.T) {
	got, err := SlugifyPath("Deployment")
	if err != nil || got != "deployment" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestSlugifyPathSlugifiesEachSegment(t *testing.T) {
	got, err := SlugifyPath("Deployment/AWS Runbooks")
	if err != nil || got != "deployment/aws-runbooks" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestSlugifyPathRejectsAnAbsoluteInput(t *testing.T) {
	if _, err := SlugifyPath("/etc/passwd"); errCode(err) != "bad_path" {
		t.Fatalf("err = %v, want bad_path", err)
	}
}

func TestSlugifyPathRejectsBackslash(t *testing.T) {
	if _, err := SlugifyPath(`deployment\aws`); errCode(err) != "bad_path" {
		t.Fatalf("err = %v, want bad_path", err)
	}
}

func TestSlugifyPathRejectsTraversal(t *testing.T) {
	for _, in := range []string{"..", "../etc", "deployment/..", "deployment/../etc"} {
		if _, err := SlugifyPath(in); errCode(err) != "bad_path" {
			t.Errorf("SlugifyPath(%q) err = %v, want bad_path", in, err)
		}
	}
}

func TestSlugifyPathRejectsAnEmptySegment(t *testing.T) {
	if _, err := SlugifyPath("deployment//rollback"); errCode(err) != "bad_path" {
		t.Fatalf("err = %v, want bad_path", err)
	}
}

func TestSlugifyPathRejectsEveryReservedDeviceName(t *testing.T) {
	names := []string{"con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9"}
	for _, n := range names {
		if _, err := SlugifyPath(n); errCode(err) != "reserved_name" {
			t.Errorf("SlugifyPath(%q) err = %v, want reserved_name", n, err)
		}
		if _, err := SlugifyPath("docs/" + n); errCode(err) != "reserved_name" {
			t.Errorf("SlugifyPath(%q) err = %v, want reserved_name", "docs/"+n, err)
		}
	}
}

func TestSlugifyPathRejectsAnOverLongSegment(t *testing.T) {
	seg := ""
	for len(seg) < 97 {
		seg += "a"
	}
	if _, err := SlugifyPath(seg); errCode(err) != "path_too_long" {
		t.Fatalf("a 97-character segment: err = %v, want path_too_long", err)
	}
	seg96 := seg[:96]
	if got, err := SlugifyPath(seg96); err != nil || got != seg96 {
		t.Fatalf("a 96-character segment must be accepted: got %q, %v", got, err)
	}
}

func TestSlugifyPathRejectsAnOverLongFullPath(t *testing.T) {
	seg := ""
	for len(seg) < 90 {
		seg += "a"
	}
	// Two 90-character segments plus one "/" is 181 characters.
	if _, err := SlugifyPath(seg + "/" + seg); errCode(err) != "path_too_long" {
		t.Fatalf("a 181-character path: err = %v, want path_too_long", err)
	}
}

func TestSlugifyPathEmptyInputIsEmptyOutput(t *testing.T) {
	got, err := SlugifyPath("")
	if err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestTruncateSegmentLeavesAShortSegmentAlone(t *testing.T) {
	if got := truncateSegment("concurrency-model"); got != "concurrency-model" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateSegmentCutsAtTheCeiling(t *testing.T) {
	long := ""
	for len(long) < 120 {
		long += "a"
	}
	got := truncateSegment(long)
	if len(got) > maxPathSegmentLen {
		t.Fatalf("len(%q) = %d, want <= %d", got, len(got), maxPathSegmentLen)
	}
}

// The longest slug in the live vault today is 65 characters; the ceiling
// must not touch it.
func TestTruncateSegmentRoundTripsTheLongestKnownSlug(t *testing.T) {
	slug := "a-sixty-five-character-slug-that-already-exists-in-the-live-vault"
	if len(slug) != 65 {
		t.Fatalf("test fixture is %d characters, want 65", len(slug))
	}
	if got := truncateSegment(slug); got != slug {
		t.Fatalf("got %q, want it unchanged", got)
	}
}

func TestNormalizeSlugPathNeverErrors(t *testing.T) {
	cases := map[string]string{
		"Rollback":             "rollback",
		"deployment/rollback":  "deployment/rollback",
		"":                     "",
		"../etc":               "etc",
		"deployment//rollback": "deployment/rollback",
		"  Spaced Title  ":     "spaced-title",
	}
	for in, want := range cases {
		if got := normalizeSlugPath(in); got != want {
			t.Errorf("normalizeSlugPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResemblesPrefixRule(t *testing.T) {
	if !resembles("deploy", "deployment") {
		t.Error("deploy should resemble deployment")
	}
	if !resembles("runbook", "runbooks") {
		t.Error("runbook should resemble runbooks")
	}
}

func TestResemblesEditDistanceRule(t *testing.T) {
	if !resembles("deployment", "deplyoment") {
		t.Error("deployment should resemble deplyoment (transposition, edit distance 2)")
	}
}

func TestResemblesFloorsKeepObviousNamesApart(t *testing.T) {
	if resembles("api", "apis-legacy") {
		t.Error("api must not resemble apis-legacy: the shorter name is under the 4-character floor")
	}
	if resembles("docs", "dogs") {
		t.Error("docs must not resemble dogs: both are under the 5-character edit-distance floor")
	}
}

func TestResemblesIsFalseForIdenticalNames(t *testing.T) {
	if resembles("deployment", "deployment") {
		t.Error("a name never resembles itself; that is an exact match, not a finding")
	}
}

func TestReservedLeafTakenChecksOnlyTheLastSegment(t *testing.T) {
	if !reservedLeafTaken("con") {
		t.Error("con")
	}
	if !reservedLeafTaken("deployment/con") {
		t.Error("deployment/con")
	}
	if reservedLeafTaken("con-2") {
		t.Error("con-2 is not itself reserved")
	}
	if reservedLeafTaken("consulting") {
		t.Error("consulting must not match on a prefix")
	}
}

func errCode(err error) string {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return ""
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestSlugifyPath|TestTruncateSegment|TestNormalizeSlugPath|TestResembles|TestReservedLeafTaken' -v`
Expected: the package does not compile — `SlugifyPath undefined`, `truncateSegment undefined`, and so on.

- [ ] **Step 3: Implement the primitives**

Create `internal/core/knowledge_path.go`:

```go
package core

import (
	"fmt"
	"strings"
)

// maxPathSegmentLen and maxRelSlugLen are the two length ceilings from the
// knowledge-paths design. NAME_MAX (255) is not the binding constraint on any
// of the three platforms Trellis supports; Windows MAX_PATH (260) is. The
// storage root plus username reserves roughly 80 of it, leaving 180 for the
// slug. 96 per segment is chosen against the live vault: its longest slug is
// 65 characters, so a lower ceiling would reject a document that already
// exists.
const (
	maxPathSegmentLen = 96
	maxRelSlugLen      = 180
)

// reservedDeviceNames are the Windows device names that stay reserved no
// matter what extension follows them: "con.md" cannot be opened on Windows.
// Slugify lower-cases, so these are checked in lower-case form; the input to
// this map is always something Slugify has already produced.
var reservedDeviceNames = buildReservedDeviceNames()

func buildReservedDeviceNames() map[string]bool {
	m := map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}
	for i := 1; i <= 9; i++ {
		m[fmt.Sprintf("com%d", i)] = true
		m[fmt.Sprintf("lpt%d", i)] = true
	}
	return m
}

// reservedLeafTaken reports whether slug's final path segment is a Windows
// reserved device name. "deployment/con" is exactly as unwritable on Windows
// as "con" alone; the directory it sits in does not matter.
func reservedLeafTaken(slug string) bool {
	leaf := slug
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		leaf = slug[i+1:]
	}
	return reservedDeviceNames[leaf]
}

// SlugifyPath validates and slugifies caller-supplied path input: `--in` on
// `knowledge new`, and the destination of `knowledge mv`. Every segment is
// slugified the way a flat slug already is, then rejoined with "/". Unlike a
// title-derived slug, this is text the caller typed on purpose, so every
// violation is rejected rather than silently fixed — see "A generated slug is
// truncated; an explicit path is rejected" in the design.
func SlugifyPath(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "/") || strings.Contains(raw, `\`) {
		return "", ErrUsage("bad_path",
			fmt.Sprintf("%q must be a relative path using / to separate directories", raw), "")
	}
	parts := strings.Split(raw, "/")
	segs := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "." || trimmed == ".." {
			return "", ErrUsage("bad_path", fmt.Sprintf("%q may not contain \".\" or \"..\"", raw), "")
		}
		seg := Slugify(trimmed)
		if seg == "" {
			return "", ErrUsage("bad_path", fmt.Sprintf("%q has an empty path segment", raw), "")
		}
		if reservedDeviceNames[seg] {
			return "", ErrUsage("reserved_name",
				seg+" is a reserved device name on Windows and cannot be a directory or a slug", "")
		}
		if len(seg) > maxPathSegmentLen {
			return "", ErrUsage("path_too_long",
				fmt.Sprintf("%q is %d characters; one path segment is limited to %d", seg, len(seg), maxPathSegmentLen), "")
		}
		segs = append(segs, seg)
	}
	joined := strings.Join(segs, "/")
	if len(joined) > maxRelSlugLen {
		return "", ErrUsage("path_too_long",
			fmt.Sprintf("%q is %d characters; a knowledge path is limited to %d", joined, len(joined), maxRelSlugLen), "")
	}
	return joined, nil
}

// truncateSegment enforces the per-segment ceiling on a slug derived from a
// title rather than typed explicitly. A long title must still be writable —
// the full title survives in frontmatter and the slug is only an address —
// so it is truncated instead of rejected.
func truncateSegment(seg string) string {
	if len(seg) <= maxPathSegmentLen {
		return seg
	}
	return strings.TrimRight(seg[:maxPathSegmentLen], "-")
}

// normalizeSlugPath turns lookup input — a CLI argument, or (from Task 9
// onward) a wikilink target — into the path-shaped form stored in the slug
// column, without rejecting anything: a lookup that cannot possibly match
// should report "not found", not a validation error. Empty, "." and ".."
// segments are dropped rather than rejected, which is why a traversal
// attempt like "../etc" simply fails to match anything rather than escaping
// anywhere: it normalizes to "etc".
func normalizeSlugPath(raw string) string {
	parts := strings.Split(raw, "/")
	segs := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := Slugify(p); s != "" {
			segs = append(segs, s)
		}
	}
	return strings.Join(segs, "/")
}

// resembles reports whether two directory names are close enough to be the
// vocabulary drift the design guards against: "deploy" beside "deployment",
// or "deployment" beside "deplyoment". It leans toward false positives on
// purpose. Two names are never said to resemble themselves.
func resembles(a, b string) bool {
	if a == b {
		return false
	}
	shorter, longer := a, b
	if len(a) > len(b) {
		shorter, longer = b, a
	}
	if len(shorter) >= 4 && strings.HasPrefix(longer, shorter) {
		return true
	}
	if len(a) >= 5 && len(b) >= 5 && boundedEditDistance(a, b, 2) <= 2 {
		return true
	}
	return false
}

// boundedEditDistance computes the Levenshtein distance between a and b, or
// returns max+1 as soon as it can prove the true distance exceeds max. The
// resemblance check only ever needs to know "is it at most 2", so a length
// mismatch beyond max short-circuits immediately, and every row of the table
// bails out the moment its own minimum already exceeds max.
func boundedEditDistance(a, b string, max int) int {
	if d := len(a) - len(b); d > max || -d > max {
		return max + 1
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if cur[j] < rowMin {
				rowMin = cur[j]
			}
		}
		if rowMin > max {
			return max + 1
		}
		prev = cur
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestSlugifyPath|TestTruncateSegment|TestNormalizeSlugPath|TestResembles|TestReservedLeafTaken' -v`
Expected: PASS, all of them.

- [ ] **Step 5: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass; `gofmt -l .` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/core/knowledge_path.go internal/core/knowledge_path_test.go
git commit -m "feat(core): path validation and slug construction primitives

SlugifyPath validates explicit path input (--in, knowledge mv) and rejects
every violation; truncateSegment fits a title-derived slug into the ceiling
instead of rejecting it; normalizeSlugPath is the lenient form a lookup
uses. resembles() is the shared vocabulary-drift check a later task wires
into directory creation and lint.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `knowledge new` supports directories

**Needs virtual paths:** no.

**Files:**
- Modify: `internal/core/knowledge.go` (`NewKnowledge`, `CreateKnowledge`, `uniqueSlug`)
- Modify: `internal/core/knowledge_path.go` (`projectDirectories`, `refuseResemblingDir`)
- Modify: `internal/cli/knowledge.go` (`newKnowledgeNewCmd`)
- Create: `internal/core/knowledge_dir_test.go`
- Create: `internal/cli/knowledge_dir_cmd_test.go`

**Interfaces:**
- Consumes: Task 1's `SlugifyPath`, `truncateSegment`, `reservedLeafTaken`, `resembles`; existing `uniqueSlug`, `checkWrite`, `writeAtomic`, `Slugify`.
- Produces:
  - `NewKnowledge.Dir string` — a relative directory, validated by `SlugifyPath`
  - `NewKnowledge.NewDir bool` — create `Dir` even if it resembles an existing directory
  - `func (c *Core) projectDirectories(tx *sqlx.Tx, projectID string) ([]string, error)`
  - `func (c *Core) refuseResemblingDir(tx *sqlx.Tx, projectID, dir string, allowNew bool) error`
  - `uniqueSlug` treats a reserved Windows device name as always taken, even on the first attempt

- [ ] **Step 1: Write the failing tests**

Create `internal/core/knowledge_dir_test.go`:

```go
package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateKnowledgeWithDirNestsTheSlugAndPath(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback runbook", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Slug != "deployment/rollback-runbook" {
		t.Fatalf("slug = %q, want deployment/rollback-runbook", doc.Slug)
	}
	wantSuffix := filepath.Join("deployment", "rollback-runbook.md")
	if !strings.HasSuffix(doc.Path, wantSuffix) {
		t.Fatalf("path = %q, want it to end with %q", doc.Path, wantSuffix)
	}
	if _, err := os.Stat(doc.Path); err != nil {
		t.Fatalf("the file must exist at the nested path: %v", err)
	}
}

func TestCreateKnowledgeWithNoDirStaysAtTheRoot(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Recall ranking"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Slug != "recall-ranking" {
		t.Fatalf("slug = %q, want recall-ranking", doc.Slug)
	}
}

func TestUniqueSlugOperatesOnTheFullPathNotJustTheLeaf(t *testing.T) {
	c, p, _ := kbCore(t)
	a, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	b, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "docs"})
	if err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	if a.Slug != "deployment/rollback" || b.Slug != "docs/rollback" {
		t.Fatalf("slugs = %q, %q — two directories must not force a numeric suffix", a.Slug, b.Slug)
	}
	// Same title, same directory: this pair does collide, and today's suffix
	// behavior is unchanged.
	c2, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge c2: %v", err)
	}
	if c2.Slug != "deployment/rollback-2" {
		t.Fatalf("slug = %q, want deployment/rollback-2", c2.Slug)
	}
}

func TestCreateKnowledgeRejectsABadInDirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Dir: "../etc"}); errCode(err) != "bad_path" {
		t.Fatalf("err = %v, want bad_path", err)
	}
}

func TestCreateKnowledgeTitledLikeAReservedNameGetsASuffix(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "CON"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Slug != "con-2" {
		t.Fatalf("slug = %q, want con-2: \"con\" alone cannot be opened on Windows", doc.Slug)
	}
	if _, err := os.Stat(doc.Path); err != nil {
		t.Fatalf("the file must exist: %v", err)
	}
}

func TestCreateKnowledgeTruncatesAnOverLongTitleSlugAndKeepsTheFullTitle(t *testing.T) {
	c, p, _ := kbCore(t)
	title := "A title so long that its slugified form has to be truncated to fit the per-segment ceiling of ninety six characters"
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: title})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if len(doc.Slug) > maxPathSegmentLen {
		t.Fatalf("slug %q is %d characters, want <= %d", doc.Slug, len(doc.Slug), maxPathSegmentLen)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != title {
		t.Errorf("frontmatter title = %q, want the untruncated %q", fm.Title, title)
	}
}

func TestCreatingADirectoryThatResemblesAnExistingOneIsRefused(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Notes", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Runbook", Dir: "deployment"})
	if errCode(err) != "similar_directory" {
		t.Fatalf("err = %v, want similar_directory", err)
	}
	if !strings.Contains(err.Error(), "deploy") {
		t.Errorf("error %q does not name the existing directory", err)
	}
}

func TestNewDirCreatesItAnyway(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Notes", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Runbook", Dir: "deployment", NewDir: true})
	if err != nil {
		t.Fatalf("CreateKnowledge with NewDir: %v", err)
	}
	if doc.Slug != "deployment/runbook" {
		t.Fatalf("slug = %q, want deployment/runbook", doc.Slug)
	}
}

func TestWritingIntoAnExistingDirectoryIsNeverRefused(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "First", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge first: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Second", Dir: "deployment", NewDir: true}); err != nil {
		t.Fatalf("CreateKnowledge second: %v", err)
	}
	// "deploy" now resembles "deployment", which also exists, but "deploy"
	// itself is an existing directory and writing into it is never refused.
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Third", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge third into the existing deploy/: %v", err)
	}
}

func TestApiDoesNotMatchApisLegacyAsADirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Dir: "apis-legacy"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Y", Dir: "api"}); err != nil {
		t.Fatalf("api must not be refused as resembling apis-legacy: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestCreateKnowledgeWithDir|TestCreateKnowledgeWithNoDir|TestUniqueSlugOperatesOnTheFullPath|TestCreateKnowledgeRejectsABadInDirectory|TestCreateKnowledgeTitledLikeAReservedName|TestCreateKnowledgeTruncatesAnOverLongTitleSlug|TestCreatingADirectoryThatResembles|TestNewDirCreatesItAnyway|TestWritingIntoAnExistingDirectory|TestApiDoesNotMatchApisLegacy' -v`
Expected: `NewKnowledge.Dir undefined` and `NewKnowledge.NewDir undefined` — the package does not compile.

- [ ] **Step 3: Add directory enumeration and the resemblance refusal**

Append to `internal/core/knowledge_path.go`:

```go
// projectDirectories lists every distinct directory prefix used by the
// project's own entries, at every depth: "deployment/aws/runbooks/rollback"
// contributes "deployment", "deployment/aws" and "deployment/aws/runbooks".
// Derived from the slug column, not the filesystem — a revision directory
// (".<filename>/", see the revision-history design) has no row and so never
// appears here, and an empty leftover directory from a deleted entry is
// correctly forgotten.
func (c *Core) projectDirectories(tx *sqlx.Tx, projectID string) ([]string, error) {
	var slugs []string
	if err := tx.Select(&slugs, `SELECT slug FROM knowledge WHERE project_id = ?`, projectID); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var dirs []string
	for _, slug := range slugs {
		parts := strings.Split(slug, "/")
		for i := 1; i < len(parts); i++ {
			dir := strings.Join(parts[:i], "/")
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs, nil
}

// refuseResemblingDir is "look before you write", made mechanical: creating a
// directory that resembles one already in the project is refused unless
// allowNew is set. Writing into a directory that already exists, however it
// is spelled, is never refused — the exact-match check runs first.
func (c *Core) refuseResemblingDir(tx *sqlx.Tx, projectID, dir string, allowNew bool) error {
	if dir == "" || allowNew {
		return nil
	}
	existing, err := c.projectDirectories(tx, projectID)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e == dir {
			return nil
		}
	}
	var similar []string
	for _, e := range existing {
		if resembles(dir, e) {
			similar = append(similar, e)
		}
	}
	if len(similar) == 0 {
		return nil
	}
	slices.Sort(similar)
	return ErrUsage("similar_directory",
		dir+" is close to existing "+strings.Join(similar, ", ")+"; that may be the same idea spelled two ways",
		"trellis knowledge new --title \"...\" --in "+dir+" --new-dir")
}
```

Add `"slices"` and `"github.com/jmoiron/sqlx"` to `internal/core/knowledge_path.go`'s imports:

```go
import (
	"fmt"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
)
```

- [ ] **Step 4: Add the fields and wire them into `CreateKnowledge`**

In `internal/core/knowledge.go`, add to `type NewKnowledge struct`, directly after `Board string // board name, association only`:

```go
	// Dir places the entry in a directory instead of the vault root; empty
	// means the root. NewDir creates Dir even if it resembles an existing
	// directory.
	Dir    string
	NewDir bool
```

In `CreateKnowledge`, replace:

```go
	body := in.Body
	if body == "" {
		if body, err = templateBody(in.Template, in.Title); err != nil {
			return Knowledge{}, err
		}
	}
```

with:

```go
	body := in.Body
	if body == "" {
		if body, err = templateBody(in.Template, in.Title); err != nil {
			return Knowledge{}, err
		}
	}
	dirSlug, err := SlugifyPath(in.Dir)
	if err != nil {
		return Knowledge{}, err
	}
```

Inside the `c.Tx` closure, replace:

```go
		slug, err := uniqueSlug(tx, projectID, Slugify(in.Title))
		if err != nil {
			return err
		}
```

with:

```go
		if err := c.refuseResemblingDir(tx, projectID, dirSlug, in.NewDir); err != nil {
			return err
		}
		base := truncateSegment(Slugify(in.Title))
		if dirSlug != "" {
			base = dirSlug + "/" + base
			if room := maxRelSlugLen - len(dirSlug) - 1; len(base) > maxRelSlugLen {
				if room < 1 {
					return ErrUsage("path_too_long",
						dirSlug+" leaves no room for a title-derived slug",
						"trellis knowledge new --title \"...\" --in <a shorter directory>")
				}
				leaf := strings.TrimRight(Slugify(in.Title)[:room], "-")
				base = dirSlug + "/" + leaf
			}
		}
		slug, err := uniqueSlug(tx, projectID, base)
		if err != nil {
			return err
		}
```

Replace the line:

```go
		path := filepath.Join(dir, slug+".md")
```

with:

```go
		path := filepath.Join(dir, filepath.FromSlash(slug)+".md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
```

- [ ] **Step 5: Make `uniqueSlug` treat a reserved leaf as always taken**

Replace `uniqueSlug` in `internal/core/knowledge.go`:

```go
// uniqueSlug resolves collisions the way board slugs do: design, then
// design-2. A reserved Windows device name is treated as permanently taken
// even on the first attempt, so a title like "CON" becomes "con-2" — a name
// Windows can open — rather than a file it cannot.
func uniqueSlug(tx *sqlx.Tx, projectID, base string) (string, error) {
	if base == "" {
		base = "untitled"
	}
	for n := 1; ; n++ {
		slug := base
		if n > 1 {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		if reservedLeafTaken(slug) {
			continue
		}
		var exists int
		if err := tx.Get(&exists,
			`SELECT COUNT(*) FROM knowledge WHERE project_id = ? AND slug = ?`, projectID, slug); err != nil {
			return "", err
		}
		if exists == 0 {
			return slug, nil
		}
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestCreateKnowledgeWithDir|TestCreateKnowledgeWithNoDir|TestUniqueSlugOperatesOnTheFullPath|TestCreateKnowledgeRejectsABadInDirectory|TestCreateKnowledgeTitledLikeAReservedName|TestCreateKnowledgeTruncatesAnOverLongTitleSlug|TestCreatingADirectoryThatResembles|TestNewDirCreatesItAnyway|TestWritingIntoAnExistingDirectory|TestApiDoesNotMatchApisLegacy' -v`
Expected: PASS.

- [ ] **Step 7: Add `--in` and `--new-dir` to the CLI**

In `internal/cli/knowledge.go`, in `newKnowledgeNewCmd`, change:

```go
	var title, body, summary TextValue
	var template, board, provenance string
	var tags, labels []string
	var private bool
```

to:

```go
	var title, body, summary TextValue
	var template, board, provenance, dir string
	var tags, labels []string
	var private, newDir bool
```

Change the `core.NewKnowledge{...}` literal:

```go
				doc, err := app.Core.CreateKnowledge(cmd.Context(), app.Project.ID, core.NewKnowledge{
					Title: title.String(), Body: body.String(), Template: template,
					Provenance: provenance,
					Summary:    summary.String(), Board: board, Tags: tags, Labels: labels,
					Private: private,
				})
```

to:

```go
				doc, err := app.Core.CreateKnowledge(cmd.Context(), app.Project.ID, core.NewKnowledge{
					Title: title.String(), Body: body.String(), Template: template,
					Provenance: provenance,
					Summary:    summary.String(), Board: board, Tags: tags, Labels: labels,
					Private: private, Dir: dir, NewDir: newDir,
				})
```

Add two flags after `cmd.Flags().BoolVar(&private, ...)`:

```go
	cmd.Flags().StringVar(&dir, "in", "", "place the entry in this directory instead of the vault root")
	cmd.Flags().BoolVar(&newDir, "new-dir", false, "create --in even if it resembles an existing directory")
```

- [ ] **Step 8: Write a CLI test**

Create `internal/cli/knowledge_dir_cmd_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestKnowledgeNewInFlagNestsTheSlug(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "knowledge", "new", "--title", "Rollback runbook", "--in", "deployment", "--json")
	if !strings.Contains(out, `"slug":"deployment/rollback-runbook"`) {
		t.Errorf("output does not carry the nested slug:\n%s", out)
	}
}

func TestKnowledgeNewInFlagRefusesAResemblingDirectory(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "First", "--in", "deploy")
	_, err := execCmd("knowledge", "new", "--title", "Second", "--in", "deployment")
	if err == nil {
		t.Fatal("want an error: deployment resembles the existing deploy")
	}
	out := runCmd(t, "knowledge", "new", "--title", "Second", "--in", "deployment", "--new-dir", "--json")
	if !strings.Contains(out, `"slug":"deployment/second"`) {
		t.Errorf("output does not carry the nested slug:\n%s", out)
	}
}
```

- [ ] **Step 9: Run the CLI tests**

Run: `go test ./internal/cli -run 'TestKnowledgeNewInFlag' -v`
Expected: PASS.

- [ ] **Step 10: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 11: Commit**

```bash
git add internal/core/knowledge.go internal/core/knowledge_path.go internal/core/knowledge_dir_test.go \
        internal/cli/knowledge.go internal/cli/knowledge_dir_cmd_test.go
git commit -m "feat(core,cli): knowledge new places an entry in a directory

--in <dir> nests the slug and the file; --new-dir overrides the refusal
that fires when the requested directory resembles one that already exists.
uniqueSlug now treats a Windows-reserved leaf name as always taken, so a
title like \"CON\" gets a suffix instead of an unopenable file.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Bare-leaf resolution

**Needs virtual paths:** no.

**Files:**
- Modify: `internal/core/knowledge_path.go` (`resolveSlug`, `notFoundSlug`)
- Modify: `internal/core/knowledge.go` (`loadDoc`, `DeleteKnowledge`)
- Test: `internal/core/knowledge_dir_test.go`

**Interfaces:**
- Consumes: Task 1's `normalizeSlugPath`.
- Produces: `func (c *Core) resolveSlug(tx *sqlx.Tx, projectID, input string, includeGlobal bool) (string, error)`, error code `ambiguous_slug` with `Detail []string` carrying the candidates. `loadDoc` (and therefore `LoadKnowledge`, `ReadKnowledge`, `PinKnowledge`, `NominateKnowledge`, `EscalateKnowledge`, `EditKnowledgeFields`, everything that calls it) and `DeleteKnowledge` both resolve a bare leaf; `DeleteKnowledge` never resolves into the global vault.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/knowledge_dir_test.go`:

```go
func TestBareLeafOpensTheUniqueMatch(t *testing.T) {
	c, p, _ := kbCore(t)
	created, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	got, err := c.LoadKnowledge(t.Context(), p.ID, "rollback")
	if err != nil {
		t.Fatalf("LoadKnowledge by bare leaf: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("got %q, want %q", got.Slug, created.Slug)
	}
}

func TestBareLeafAmbiguityListsCandidatesAndOpensNeither(t *testing.T) {
	c, p, _ := kbCore(t)
	a, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	b, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "docs"})
	if err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	_, err = c.LoadKnowledge(t.Context(), p.ID, "rollback")
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "ambiguous_slug" {
		t.Fatalf("err = %v, want ambiguous_slug", err)
	}
	detail, ok := e.Detail.([]string)
	if !ok || !slices.Contains(detail, a.Slug) || !slices.Contains(detail, b.Slug) {
		t.Errorf("Detail = %v, want both %q and %q", e.Detail, a.Slug, b.Slug)
	}
}

func TestFullPathStillResolvesExactlyEvenWhenALeafIsAmbiguous(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"}); err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "docs"}); err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	got, err := c.LoadKnowledge(t.Context(), p.ID, "deployment/rollback")
	if err != nil {
		t.Fatalf("LoadKnowledge by full path: %v", err)
	}
	if got.Slug != "deployment/rollback" {
		t.Fatalf("slug = %q", got.Slug)
	}
}

func TestBareLeafNotFoundIsTheOrdinaryNotFoundError(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.LoadKnowledge(t.Context(), p.ID, "nope")
	if errCode(err) != "knowledge_not_found" {
		t.Fatalf("err = %v, want knowledge_not_found", err)
	}
}

func TestDeleteKnowledgeByBareLeaf(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if err := c.DeleteKnowledge(t.Context(), p.ID, "rollback"); err != nil {
		t.Fatalf("DeleteKnowledge by bare leaf: %v", err)
	}
	if _, err := os.Stat(doc.Path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}

// rm never bare-leaf-resolves into the global vault: a project's rm is
// scoped to its own vault, even though show/edit/pin already look there.
func TestDeleteKnowledgeDoesNotBareLeafIntoTheGlobalVault(t *testing.T) {
	c, p, _ := kbCore(t)
	other := seededProject2(t, c)
	doc, err := c.CreateKnowledge(t.Context(), other.ID, NewKnowledge{Title: "Shared", Dir: "docs"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), other.ID, doc.Slug, "cross-project"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	// LoadKnowledge from an unrelated project finds it in the vault...
	if _, err := c.LoadKnowledge(t.Context(), p.ID, "shared"); err != nil {
		t.Fatalf("LoadKnowledge should find the global entry: %v", err)
	}
	// ...but DeleteKnowledge from that same unrelated project must not.
	if err := c.DeleteKnowledge(t.Context(), p.ID, "shared"); errCode(err) != "knowledge_not_found" {
		t.Fatalf("err = %v, want knowledge_not_found: rm must not reach into another project's escalated entry", err)
	}
}
```

Add `"errors"` and `"slices"` to the test file's imports (alongside the existing `"os"`, `"path/filepath"`, `"strings"`, `"testing"`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestBareLeaf|TestFullPathStillResolves|TestDeleteKnowledgeByBareLeaf|TestDeleteKnowledgeDoesNotBareLeaf' -v`
Expected: FAIL — `LoadKnowledge`/`DeleteKnowledge` still resolve only exact, whole-string slugs.

- [ ] **Step 3: Add `resolveSlug`**

Append to `internal/core/knowledge_path.go`:

```go
// resolveSlug turns CLI or wikilink input into exactly one entry's stored
// slug. input may be the full path ("deployment/rollback") or a bare leaf
// ("rollback"): a bare leaf matches any directory and is a permanent
// addressing mode, not a compatibility shim. includeGlobal widens matching
// to the vault, which loadDoc's callers want (show, edit, pin, escalate...)
// and DeleteKnowledge does not: rm only ever removes this project's own row.
func (c *Core) resolveSlug(tx *sqlx.Tx, projectID, input string, includeGlobal bool) (string, error) {
	norm := normalizeSlugPath(input)
	scope := "project_id = ?"
	if includeGlobal {
		scope = "(project_id = ? OR global = 1)"
	}
	var exact []string
	if err := tx.Select(&exact, `SELECT slug FROM knowledge WHERE slug = ? AND `+scope, norm, projectID); err != nil {
		return "", err
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if norm == "" || strings.Contains(norm, "/") {
		return "", notFoundSlug(input)
	}
	var matches []string
	if err := tx.Select(&matches,
		`SELECT slug FROM knowledge WHERE (slug = ? OR slug LIKE '%/' || ?) AND `+scope+` ORDER BY slug`,
		norm, norm, projectID); err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", notFoundSlug(input)
	case 1:
		return matches[0], nil
	default:
		return "", &Error{
			Code: "ambiguous_slug", Exit: 2,
			Msg: fmt.Sprintf("%q matches more than one entry: %s", input, strings.Join(matches, ", ")),
			Fix: "trellis knowledge show <full path>", Detail: matches,
		}
	}
}

func notFoundSlug(input string) error {
	return ErrNotFound("knowledge_not_found", "no knowledge entry "+input, "trellis knowledge ls")
}
```

- [ ] **Step 4: Wire it into `loadDoc`**

In `internal/core/knowledge.go`, replace `loadDoc`:

```go
func (c *Core) loadDoc(tx *sqlx.Tx, projectID, slug string, out *Knowledge) error {
	resolved, err := c.resolveSlug(tx, projectID, slug, true)
	if err != nil {
		return err
	}
	err = tx.Get(out,
		`SELECT * FROM knowledge WHERE slug = ? AND (project_id = ? OR global = 1) ORDER BY global LIMIT 1`,
		resolved, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundSlug(slug)
	}
	if err != nil {
		return err
	}
	if err := c.refreshFromFile(tx, out); err != nil {
		return err
	}
	return c.docView(tx, out)
}
```

- [ ] **Step 5: Wire it into `DeleteKnowledge`**

In `internal/core/knowledge.go`, inside `DeleteKnowledge`, replace:

```go
		if err := tx.Get(&doc,
			`SELECT * FROM knowledge WHERE project_id = ? AND slug = ?`, projectID, Slugify(slug)); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("knowledge_not_found", "no knowledge entry "+slug, "trellis knowledge ls")
			}
			return err
		}
```

with:

```go
		resolved, rerr := c.resolveSlug(tx, projectID, slug, false)
		if rerr != nil {
			return rerr
		}
		if err := tx.Get(&doc,
			`SELECT * FROM knowledge WHERE project_id = ? AND slug = ?`, projectID, resolved); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return notFoundSlug(slug)
			}
			return err
		}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestBareLeaf|TestFullPathStillResolves|TestDeleteKnowledgeByBareLeaf|TestDeleteKnowledgeDoesNotBareLeaf' -v`
Expected: PASS.

- [ ] **Step 7: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && go test -race ./internal/core`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/core/knowledge_path.go internal/core/knowledge.go internal/core/knowledge_dir_test.go
git commit -m "feat(core): bare-leaf resolution for show, edit, pin and rm

A slug with no / matches any directory. A unique match opens; more than
one lists every candidate's full path and opens neither, rather than
guessing. rm is scoped to the calling project's own rows and never
bare-leaf-resolves into another project's escalated global entry, even
though show/edit/pin already look at the vault.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `knowledge mv`

**Needs virtual paths:** no (inbound-wikilink rewriting is deferred to Task 9 — see the note in Step 3).

**Files:**
- Create: `internal/core/knowledge_mv.go`
- Modify: `internal/core/pin.go` (`moveFile` becomes a wrapper over new `moveFileTo`)
- Modify: `internal/cli/knowledge.go` (new `newKnowledgeMvCmd`)
- Test: `internal/core/knowledge_dir_test.go`
- Test: `internal/cli/knowledge_dir_cmd_test.go`

**Interfaces:**
- Consumes: Task 1 (`SlugifyPath`, `refuseResemblingDir`), Task 3 (`resolveSlug`), existing `moveFile`, `copyAtomic`, `syncDirectory`, `writeLanded`.
- Produces:
  - `func moveFileTo(src, dest string) (string, error)` — `moveFile(src, destDir)` is now `moveFileTo(src, filepath.Join(destDir, baseName(src)))`
  - `func revisionDirFor(docPath string) string`
  - `func moveRevisionDirIfExists(oldDocPath, newDocPath string) (moved bool, err error)`
  - `func (c *Core) MoveKnowledge(ctx context.Context, projectID, ref, newPath string, newDir bool) (Knowledge, error)` — errors `global_entry`, `same_path`, `slug_taken`, plus whatever `resolveSlug`/`refuseResemblingDir`/`SlugifyPath` return

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/knowledge_dir_test.go`:

```go
func TestMoveKnowledgeUpdatesSlugPathAndFile(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	oldPath := doc.Path
	moved, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "deployment/rollback-runbook", false)
	if err != nil {
		t.Fatalf("MoveKnowledge: %v", err)
	}
	if moved.Slug != "deployment/rollback-runbook" {
		t.Fatalf("slug = %q", moved.Slug)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("the old file must be gone: %v", err)
	}
	if _, err := os.Stat(moved.Path); err != nil {
		t.Fatalf("the new file must exist: %v", err)
	}
	if _, err := c.LoadKnowledge(t.Context(), p.ID, "deployment/rollback-runbook"); err != nil {
		t.Fatalf("the row must resolve at the new path: %v", err)
	}
}

func TestMoveKnowledgeRefusesToReplaceAnExistingSlug(t *testing.T) {
	c, p, _ := kbCore(t)
	a, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "A"})
	if err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "B"}); err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	_, err = c.MoveKnowledge(t.Context(), p.ID, a.Slug, "b", false)
	if errCode(err) != "slug_taken" {
		t.Fatalf("err = %v, want slug_taken", err)
	}
	if _, err := os.Stat(a.Path); err != nil {
		t.Fatalf("a's file must be untouched: %v", err)
	}
}

func TestMoveKnowledgeChecksTheDestinationDirectoryForResemblance(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Notes", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Runbook"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "deployment/runbook", false); errCode(err) != "similar_directory" {
		t.Fatalf("err = %v, want similar_directory", err)
	}
	moved, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "deployment/runbook", true)
	if err != nil {
		t.Fatalf("MoveKnowledge with newDir: %v", err)
	}
	if moved.Slug != "deployment/runbook" {
		t.Fatalf("slug = %q", moved.Slug)
	}
}

func TestMoveKnowledgeRefusesAGlobalEntry(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Shared"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if _, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "renamed", false); errCode(err) != "global_entry" {
		t.Fatalf("err = %v, want global_entry", err)
	}
}

func TestMoveKnowledgeMovesTheRevisionDirectoryIfPresent(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Standup"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	// Simulate the revision-history feature having already captured a
	// version: a hidden directory named ".<filename>" beside the entry.
	oldRevDir := revisionDirFor(doc.Path)
	if err := os.MkdirAll(oldRevDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRevDir, "1.md"), []byte("version one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	moved, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "deployment/standup", false)
	if err != nil {
		t.Fatalf("MoveKnowledge: %v", err)
	}
	if _, err := os.Stat(oldRevDir); !os.IsNotExist(err) {
		t.Fatalf("old revision directory must be gone: %v", err)
	}
	newRevDir := revisionDirFor(moved.Path)
	got, err := os.ReadFile(filepath.Join(newRevDir, "1.md"))
	if err != nil {
		t.Fatalf("revision file must have moved with the entry: %v", err)
	}
	if string(got) != "version one\n" {
		t.Errorf("revision content = %q", got)
	}
}

func TestMoveKnowledgeIsFineWithNoRevisionDirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Plain"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "elsewhere", false); err != nil {
		t.Fatalf("MoveKnowledge: %v", err)
	}
}

func TestMoveKnowledgeByBareLeaf(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	moved, err := c.MoveKnowledge(t.Context(), p.ID, "rollback", "docs/rollback", false)
	if err != nil {
		t.Fatalf("MoveKnowledge by bare leaf: %v", err)
	}
	if moved.ID != doc.ID || moved.Slug != "docs/rollback" {
		t.Fatalf("moved = %+v", moved)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestMoveKnowledge' -v`
Expected: the package does not compile — `MoveKnowledge undefined`, `revisionDirFor undefined`.

- [ ] **Step 3: Generalize `moveFile`**

In `internal/core/pin.go`, replace `moveFile`:

```go
// moveFile moves src into destDir, refusing to replace anything already
// there. It is moveFileTo with the destination computed as "same basename,
// new directory" -- escalate and demote never rename the leaf, only relocate
// it between the project vault and the global one.
func moveFile(src, destDir string) (string, error) {
	return moveFileTo(src, filepath.Join(destDir, baseName(src)))
}

// moveFileTo moves src to the exact destination path dest, refusing to
// replace anything there. A hard link is nearly free and needs no fallback
// for the common case (same filesystem); os.Link's O_EXCL-like semantics are
// what make "refuses to replace" true without a TOCTOU gap. A link across
// filesystems, or on a filesystem without hard links, falls back to
// copyAtomic, which refuses the same way. Either way src is only removed once
// dest is safely in place. knowledge mv calls this directly because, unlike
// escalate and demote, it can rename the leaf as well as relocate it.
func moveFileTo(src, dest string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	if err := os.Link(src, dest); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", ErrConflict("path_taken", dest+" already exists", "")
		}
		if err := copyAtomic(dest, src); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return "", ErrConflict("path_taken", dest+" already exists", "")
			}
			return "", err
		}
	}
	if removeErr := os.Remove(src); removeErr != nil {
		if cleanupErr := os.Remove(dest); cleanupErr != nil {
			return "", errors.Join(removeErr, cleanupErr)
		}
		return "", removeErr
	}
	if err := syncDirectory(filepath.Dir(dest)); err != nil {
		return "", err
	}
	if err := syncDirectory(filepath.Dir(src)); err != nil {
		return "", err
	}
	return dest, nil
}
```

- [ ] **Step 4: Add revision-directory carry-along and `MoveKnowledge`**

Create `internal/core/knowledge_mv.go`:

```go
package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

// revisionDirFor is where the revision-history design keeps an entry's old
// versions: a hidden directory beside the file, named after the file
// including its extension ("standup.md" -> ".standup.md/"). It has no
// database row of its own, so it is never mistaken for an entry -- Slugify
// never produces a leading dot, and projectDirectories is derived from the
// slug column rather than the filesystem, so it never appears there either.
func revisionDirFor(docPath string) string {
	return filepath.Join(filepath.Dir(docPath), "."+filepath.Base(docPath))
}

// moveRevisionDirIfExists moves an entry's revision directory alongside it.
// It is a no-op, not an error, when the directory does not exist: revision
// history may not have shipped yet, or the entry may be new. A same-content
// copy is the fallback when os.Rename cannot move a directory in one step
// (crossing a filesystem boundary), since a revision directory can hold many
// files.
func moveRevisionDirIfExists(oldDocPath, newDocPath string) (moved bool, err error) {
	oldDir, newDir := revisionDirFor(oldDocPath), revisionDirFor(newDocPath)
	if _, err := os.Stat(oldDir); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if _, err := os.Stat(newDir); err == nil {
		return false, ErrConflict("path_taken", newDir+" already exists", "")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o700); err != nil {
		return false, err
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		if cerr := copyDirFlat(oldDir, newDir); cerr != nil {
			return false, errors.Join(err, cerr)
		}
		if rerr := os.RemoveAll(oldDir); rerr != nil {
			return false, rerr
		}
	}
	if err := syncDirectory(filepath.Dir(newDir)); err != nil {
		return false, err
	}
	if err := syncDirectory(filepath.Dir(oldDir)); err != nil {
		return false, err
	}
	return true, nil
}

// copyDirFlat copies every regular file directly inside src into dst, which
// is all a revision directory ever holds: one file per retained version, no
// subdirectories.
func copyDirFlat(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := copyAtomic(filepath.Join(dst, e.Name()), filepath.Join(src, e.Name())); err != nil {
			return err
		}
	}
	return syncDirectory(dst)
}

// MoveKnowledge renames or relocates an entry within its own project's
// vault, never replacing anything at the destination. It refuses a global
// entry (demote first) and a destination directory that resembles an
// existing one (see refuseResemblingDir), and it moves the entry's revision
// directory if one exists.
//
// It does not yet rewrite inbound wikilinks that name the old path -- that
// needs the directory-aware wikilink syntax Task 9 of this plan adds, which
// itself needs the virtual-paths layer. Until Task 9 lands, an existing
// inbound link keeps resolving correctly (link rows point at the entry's id,
// not its slug), but the referring document's stored text still names the
// old path; the next Trellis edit that re-syncs that document's wikilinks
// will find the old path gone and turn the link into a stub, which
// `knowledge lint` already reports -- the same way a deleted entry's inbound
// links do today.
func (c *Core) MoveKnowledge(ctx context.Context, projectID, ref, newPath string, newDir bool) (Knowledge, error) {
	newSlug, err := SlugifyPath(newPath)
	if err != nil {
		return Knowledge{}, err
	}
	if newSlug == "" {
		return Knowledge{}, ErrUsage("missing_path", "knowledge mv needs a destination path",
			"trellis knowledge mv "+ref+" new/path")
	}
	var doc Knowledge
	var src, dest string
	var revMoved bool
	var done bool
	err = c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure, or a panic, after the move undoes it before this closure
		// returns, while Core.Tx still holds SQLite's write lock. done, not
		// err, is what the undo is keyed on: a panic unwinds through this
		// defer without ever reaching the closure's own return statement, so
		// a named result would still read nil and the undo would be skipped.
		defer func() {
			if !done && dest != "" {
				if _, merr := moveFileTo(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
				if revMoved {
					if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
						err = errors.Join(err, merr)
					}
				}
				dest = ""
			}
		}()

		resolved, rerr := c.resolveSlug(tx, projectID, ref, false)
		if rerr != nil {
			return rerr
		}
		if err := tx.Get(&doc, `SELECT * FROM knowledge WHERE project_id = ? AND slug = ?`, projectID, resolved); err != nil {
			return err
		}
		if doc.Global {
			return ErrUsage("global_entry", doc.Slug+" is in the global vault; demote it first",
				"trellis knowledge demote "+doc.Slug)
		}
		if newSlug == doc.Slug {
			return ErrUsage("same_path", doc.Slug+" is already there", "")
		}
		var taken int
		if err := tx.Get(&taken, `SELECT COUNT(*) FROM knowledge WHERE project_id = ? AND slug = ?`,
			projectID, newSlug); err != nil {
			return err
		}
		if taken > 0 {
			return ErrConflict("slug_taken", newSlug+" already exists in this project",
				"trellis knowledge show "+newSlug)
		}
		destDir := ""
		if i := strings.LastIndex(newSlug, "/"); i >= 0 {
			destDir = newSlug[:i]
		}
		if err := c.refuseResemblingDir(tx, projectID, destDir, newDir); err != nil {
			return err
		}
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID); err != nil {
			return err
		}
		vault, err := c.kbDir(key, false)
		if err != nil {
			return err
		}
		src = doc.Path
		dest, err = moveFileTo(doc.Path, filepath.Join(vault, filepath.FromSlash(newSlug)+".md"))
		if err != nil {
			return err
		}
		revMoved, err = moveRevisionDirIfExists(src, dest)
		if err != nil {
			return err
		}
		now := c.clock.NowMS()
		oldSlug := doc.Slug
		if _, err := tx.Exec(`UPDATE knowledge SET slug = ?, path = ?, updated_at = ? WHERE id = ?`,
			newSlug, dest, now, doc.ID); err != nil {
			return err
		}
		doc.Slug, doc.Path, doc.UpdatedAt = newSlug, dest, now
		if err := c.recordEvent(tx, "knowledge", doc.ID, "moved", "", oldSlug, newSlug); err != nil {
			return err
		}
		if err := c.docView(tx, &doc); err != nil {
			return err
		}
		done = true
		return nil
	})
	if err != nil && done {
		// done means the closure completed and it was tx.Commit that failed:
		// durable state, not a guess, decides which side of the move the
		// file belongs on, exactly as EscalateKnowledge already resolves this.
		var landed string
		qerr := c.db.Get(&landed, `SELECT path FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(landed == dest, qerr) {
			err = nil
		} else {
			if _, merr := moveFileTo(dest, src); merr != nil {
				err = errors.Join(err, merr)
			}
			if revMoved {
				if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
			}
		}
	}
	if err == nil {
		c.notifyKnowledgeChanged(ctx, projectID)
	}
	return doc, err
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestMoveKnowledge' -v`
Expected: PASS, all of them.

- [ ] **Step 6: Add the CLI command**

In `internal/cli/knowledge.go`, add `newKnowledgeMvCmd()` to the `cmd.AddCommand(...)` list in `newKnowledgeCmd`, and define it:

```go
func newKnowledgeMvCmd() *cobra.Command {
	var newDir bool
	cmd := &cobra.Command{
		Use:   "mv <ref> <new-path>",
		Short: "Move or rename an entry within its project's vault",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				doc, err := app.Core.MoveKnowledge(cmd.Context(), app.Project.ID, args[0], args[1], newDir)
				if err != nil {
					return err
				}
				return Emit(cmd, doc, func() string { return "moved to " + doc.Slug })
			})
		},
	}
	cmd.Flags().BoolVar(&newDir, "new-dir", false, "create the destination directory even if it resembles an existing one")
	return cmd
}
```

- [ ] **Step 7: Write a CLI test**

Append to `internal/cli/knowledge_dir_cmd_test.go`:

```go
func TestKnowledgeMvCommand(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Rollback")
	out := runCmd(t, "knowledge", "mv", "rollback", "deployment/rollback-runbook", "--json")
	if !strings.Contains(out, `"slug":"deployment/rollback-runbook"`) {
		t.Errorf("output does not carry the new slug:\n%s", out)
	}
	show := runCmd(t, "knowledge", "show", "deployment/rollback-runbook")
	if !strings.Contains(show, "deployment/rollback-runbook") {
		t.Errorf("show does not find the moved entry:\n%s", show)
	}
}
```

- [ ] **Step 8: Run the CLI tests**

Run: `go test ./internal/cli -run 'TestKnowledgeMvCommand' -v`
Expected: PASS.

- [ ] **Step 9: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && go test -race ./internal/core`
Expected: all pass.

- [ ] **Step 10: Run the Windows build check**

Run: `GOOS=windows go build ./...`
Expected: succeeds.

- [ ] **Step 11: Commit**

```bash
git add internal/core/knowledge_mv.go internal/core/pin.go internal/core/knowledge_dir_test.go \
        internal/cli/knowledge.go internal/cli/knowledge_dir_cmd_test.go
git commit -m "feat(core,cli): knowledge mv moves an entry and its revision directory

MoveKnowledge never replaces anything at the destination, refuses a
directory that resembles an existing one, and refuses to act on a global
entry. It moves the hidden .<filename>/ revision directory alongside the
entry when one exists, and is a no-op about it when one does not.
Rewriting inbound wikilinks on the referring documents is deferred to a
later task that needs the virtual-paths layer; see the comment on
MoveKnowledge.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `promote`/`demote` preserve the subpath

**Needs virtual paths:** no.

**Files:**
- Modify: `internal/core/pin.go` (`EscalateKnowledge`, `DemoteKnowledge`, `VerifyKnowledge`)
- Test: `internal/core/knowledge_dir_test.go`

**Interfaces:**
- Consumes: Task 1's `normalizeSlugPath`, Task 4's `moveFileTo`, `moveRevisionDirIfExists`.
- Produces: no new exported functions; `EscalateKnowledge`/`DemoteKnowledge` keep their existing signatures and now place the file at `<vault>/<slug-with-subpath>.md` in the destination vault instead of flattening to the basename, and carry the revision directory. `DemoteKnowledge` and `VerifyKnowledge` resolve a directory-shaped slug instead of flattening it with a whole-string `Slugify`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/knowledge_dir_test.go`:

```go
func TestEscalateKnowledgePreservesTheSubpath(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	global, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason")
	if err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if global.Slug != "deployment/rollback" {
		t.Fatalf("slug = %q, want the subpath preserved", global.Slug)
	}
	if filepath.Base(filepath.Dir(global.Path)) != "deployment" {
		t.Fatalf("path = %q, want the subpath preserved on disk too", global.Path)
	}
}

func TestDemoteKnowledgePreservesTheSubpath(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	back, err := c.DemoteKnowledge(t.Context(), doc.Slug, "reason")
	if err != nil {
		t.Fatalf("DemoteKnowledge: %v", err)
	}
	if back.Slug != "deployment/rollback" {
		t.Fatalf("slug = %q, want the subpath preserved", back.Slug)
	}
	if filepath.Base(filepath.Dir(back.Path)) != "deployment" {
		t.Fatalf("path = %q, want the subpath preserved on disk too", back.Path)
	}
}

func TestEscalateKnowledgeMovesTheRevisionDirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	oldRevDir := revisionDirFor(doc.Path)
	if err := os.MkdirAll(oldRevDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRevDir, "1.md"), []byte("v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	global, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason")
	if err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if _, err := os.Stat(revisionDirFor(global.Path)); err != nil {
		t.Fatalf("revision directory must have moved: %v", err)
	}
}

// DemoteKnowledge and VerifyKnowledge look a global entry up by slug with a
// direct query, not through loadDoc/resolveSlug, so they need their own fix
// for a directory-shaped slug: today they flatten the input with a
// whole-string Slugify, which would turn "deployment/rollback" into
// "deployment-rollback" and never find the row.
func TestDemoteKnowledgeResolvesADirectoryShapedSlug(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if _, err := c.DemoteKnowledge(t.Context(), "deployment/rollback", "reason"); err != nil {
		t.Fatalf("DemoteKnowledge by full path: %v", err)
	}
}

func TestVerifyKnowledgeResolvesADirectoryShapedSlug(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if err := c.VerifyKnowledge(t.Context(), "deployment/rollback"); err != nil {
		t.Fatalf("VerifyKnowledge by full path: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestEscalateKnowledgePreserves|TestDemoteKnowledgePreserves|TestEscalateKnowledgeMovesTheRevisionDirectory|TestDemoteKnowledgeResolvesADirectoryShapedSlug|TestVerifyKnowledgeResolvesADirectoryShapedSlug' -v`
Expected: FAIL — today's `moveFile` flattens to the basename, so the slug's directory is lost, and the direct `Slugify(slug)` lookups in `DemoteKnowledge`/`VerifyKnowledge` cannot find a directory-shaped slug.

- [ ] **Step 3: Fix `EscalateKnowledge`**

In `internal/core/pin.go`, inside `EscalateKnowledge`, replace:

```go
	var doc Knowledge
	var src, dest string
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure, or a panic, after the move undoes it before this
		// closure returns, while Core.Tx still holds SQLite's write lock.
		// done, not err, is what the undo is keyed on: a panic unwinds
		// through this defer without ever reaching the closure's own return
		// statement, so a named result would still read nil and the undo
		// would be skipped.
		defer func() {
			if !done && dest != "" {
				if merr := moveBack(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
				dest = ""
			}
		}()
```

with:

```go
	var doc Knowledge
	var src, dest string
	var revMoved bool
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure, or a panic, after the move undoes it before this
		// closure returns, while Core.Tx still holds SQLite's write lock.
		// done, not err, is what the undo is keyed on: a panic unwinds
		// through this defer without ever reaching the closure's own return
		// statement, so a named result would still read nil and the undo
		// would be skipped.
		defer func() {
			if !done && dest != "" {
				if merr := moveBack(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
				if revMoved {
					if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
						err = errors.Join(err, merr)
					}
				}
				dest = ""
			}
		}()
```

Replace:

```go
		dir, err := c.kbDir(GlobalKey, true)
		if err != nil {
			return err
		}
		src = doc.Path
		moved, err := moveFile(doc.Path, dir)
		if err != nil {
			return err
		}
		dest = moved
```

with:

```go
		dir, err := c.kbDir(GlobalKey, true)
		if err != nil {
			return err
		}
		src = doc.Path
		dest, err = moveFileTo(doc.Path, filepath.Join(dir, filepath.FromSlash(doc.Slug)+".md"))
		if err != nil {
			return err
		}
		revMoved, err = moveRevisionDirIfExists(src, dest)
		if err != nil {
			return err
		}
```

And in the post-commit ambiguous-outcome block, replace:

```go
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

// DemoteKnowledge
```

with:

```go
		var landed string
		qerr := c.db.Get(&landed, `SELECT path FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(landed == dest, qerr) {
			err = nil
		} else {
			if merr := moveBack(dest, src); merr != nil {
				err = errors.Join(err, merr)
			}
			if revMoved {
				if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
			}
		}
	}
	return doc, err
}

// DemoteKnowledge
```

(That last replacement matches the exact boundary between `EscalateKnowledge` and the `DemoteKnowledge` doc comment, so the edit tool can locate it uniquely.)

- [ ] **Step 4: Apply the identical shape to `DemoteKnowledge`**

In `internal/core/pin.go`, inside `DemoteKnowledge`, replace:

```go
	var doc Knowledge
	var src, dest string
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure, or a panic, after the move undoes it before this
		// closure returns, while Core.Tx still holds SQLite's write lock.
		// done, not err, is what the undo is keyed on: a panic unwinds
		// through this defer without ever reaching the closure's own return
		// statement, so a named result would still read nil and the undo
		// would be skipped.
		defer func() {
			if !done && dest != "" {
				if merr := moveBack(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
				dest = ""
			}
		}()

		gerr := tx.Get(&doc, `SELECT * FROM knowledge WHERE slug = ? AND global = 1`, Slugify(slug))
		if errors.Is(gerr, sql.ErrNoRows) {
			return ErrNotFound("not_global", "no global entry "+slug, "trellis knowledge ls --global")
		}
		if gerr != nil {
			return gerr
		}
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, doc.ProjectID); err != nil {
			return err
		}
		dir, err := c.kbDir(key, false)
		if err != nil {
			return err
		}
		src = doc.Path
		moved, err := moveFile(doc.Path, dir)
		if err != nil {
			return err
		}
		dest = moved
		if _, err := tx.Exec(
			`UPDATE knowledge SET global = 0, path = ?, review_by = NULL, updated_at = ? WHERE id = ?`,
			dest, c.clock.NowMS(), doc.ID); err != nil {
			return err
		}
		doc.Global, doc.Path, doc.ReviewBy = false, dest, nil
		if err := c.recordEvent(tx, "knowledge", doc.ID, "demoted", "", "", reason); err != nil {
			return err
		}
		if err := c.docView(tx, &doc); err != nil {
			return err
		}
		done = true
		return nil
	})
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

with:

```go
	var doc Knowledge
	var src, dest string
	var revMoved bool
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure, or a panic, after the move undoes it before this
		// closure returns, while Core.Tx still holds SQLite's write lock.
		// done, not err, is what the undo is keyed on: a panic unwinds
		// through this defer without ever reaching the closure's own return
		// statement, so a named result would still read nil and the undo
		// would be skipped.
		defer func() {
			if !done && dest != "" {
				if merr := moveBack(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
				if revMoved {
					if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
						err = errors.Join(err, merr)
					}
				}
				dest = ""
			}
		}()

		gerr := tx.Get(&doc, `SELECT * FROM knowledge WHERE slug = ? AND global = 1`, normalizeSlugPath(slug))
		if errors.Is(gerr, sql.ErrNoRows) {
			return ErrNotFound("not_global", "no global entry "+slug, "trellis knowledge ls --global")
		}
		if gerr != nil {
			return gerr
		}
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, doc.ProjectID); err != nil {
			return err
		}
		dir, err := c.kbDir(key, false)
		if err != nil {
			return err
		}
		src = doc.Path
		dest, err = moveFileTo(doc.Path, filepath.Join(dir, filepath.FromSlash(doc.Slug)+".md"))
		if err != nil {
			return err
		}
		revMoved, err = moveRevisionDirIfExists(src, dest)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`UPDATE knowledge SET global = 0, path = ?, review_by = NULL, updated_at = ? WHERE id = ?`,
			dest, c.clock.NowMS(), doc.ID); err != nil {
			return err
		}
		doc.Global, doc.Path, doc.ReviewBy = false, dest, nil
		if err := c.recordEvent(tx, "knowledge", doc.ID, "demoted", "", "", reason); err != nil {
			return err
		}
		if err := c.docView(tx, &doc); err != nil {
			return err
		}
		done = true
		return nil
	})
	if err != nil && done {
		var landed string
		qerr := c.db.Get(&landed, `SELECT path FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(landed == dest, qerr) {
			err = nil
		} else {
			if merr := moveBack(dest, src); merr != nil {
				err = errors.Join(err, merr)
			}
			if revMoved {
				if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
			}
		}
	}
	return doc, err
}
```

- [ ] **Step 5: Fix `VerifyKnowledge`'s same flattening bug**

`VerifyKnowledge` has the identical `Slugify(slug)` lookup and the same gap:
a directory-shaped global slug would never be found. In
`internal/core/pin.go`, inside `VerifyKnowledge`, replace:

```go
		err := tx.Get(&doc, `SELECT * FROM knowledge WHERE slug = ? AND global = 1`, Slugify(slug))
```

with:

```go
		err := tx.Get(&doc, `SELECT * FROM knowledge WHERE slug = ? AND global = 1`, normalizeSlugPath(slug))
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestEscalateKnowledgePreserves|TestDemoteKnowledgePreserves|TestEscalateKnowledgeMovesTheRevisionDirectory|TestDemoteKnowledgeResolvesADirectoryShapedSlug|TestVerifyKnowledgeResolvesADirectoryShapedSlug' -v`
Expected: PASS.

- [ ] **Step 7: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && go test -race ./internal/core`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/core/pin.go internal/core/knowledge_dir_test.go
git commit -m "fix(core): escalate, demote and verify all handle a directory-shaped slug

Escalate and demote used to flatten to the basename when moving between a
project's vault and the global one, silently discarding any directory;
they now move to <vault>/<slug>.md, which is the slug's own subpath, and
carry the revision directory (if one exists) the same way knowledge mv
does. Demote and verify separately looked a global entry up with a
whole-string Slugify that would never match a nested slug; both now use
normalizeSlugPath.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Tags on `KnowledgeFilter`, and `knowledge ls` renders a tree

**Needs virtual paths:** no.

**Files:**
- Modify: `internal/core/knowledge.go` (`KnowledgeFilter`, `where`)
- Modify: `internal/cli/knowledge.go` (`newKnowledgeLsCmd`, `renderKnowledgeList`)
- Test: `internal/core/knowledge_dir_test.go`
- Test: `internal/cli/knowledge_dir_cmd_test.go`

**Interfaces:**
- Consumes: existing `knowledge_tag`/`tag` tables, `ListKnowledge`.
- Produces: `KnowledgeFilter.Tags []string` (must have every listed tag), `KnowledgeFilter.Dir string` (scope to a directory and its subtree), `--tag` and a `[dir]` positional argument on `knowledge ls`, and `renderKnowledgeList` grouping the text output by directory.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/knowledge_dir_test.go`:

```go
func TestListKnowledgeFiltersByTagsRequiringAll(t *testing.T) {
	c, p, _ := kbCore(t)
	both, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Both", Tags: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("CreateKnowledge both: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "OnlyA", Tags: []string{"a"}}); err != nil {
		t.Fatalf("CreateKnowledge onlyA: %v", err)
	}
	docs, err := c.ListKnowledge(t.Context(), p.ID, KnowledgeFilter{Tags: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("ListKnowledge: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != both.ID {
		t.Fatalf("docs = %+v, want only %q", docs, both.Slug)
	}
}

func TestListKnowledgeScopesToADirectoryAndItsSubtree(t *testing.T) {
	c, p, _ := kbCore(t)
	root, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge root: %v", err)
	}
	nested, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Setup", Dir: "deployment/aws"})
	if err != nil {
		t.Fatalf("CreateKnowledge nested: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Elsewhere", Dir: "docs"}); err != nil {
		t.Fatalf("CreateKnowledge elsewhere: %v", err)
	}
	docs, err := c.ListKnowledge(t.Context(), p.ID, KnowledgeFilter{Dir: "deployment"})
	if err != nil {
		t.Fatalf("ListKnowledge: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("docs = %+v, want the 2 entries under deployment/", docs)
	}
	ids := map[string]bool{docs[0].ID: true, docs[1].ID: true}
	if !ids[root.ID] || !ids[nested.ID] {
		t.Errorf("docs = %+v, want %q and %q", docs, root.Slug, nested.Slug)
	}
}
```

Append to `internal/cli/knowledge_dir_cmd_test.go`:

```go
func TestKnowledgeLsRendersATreeGroupedByDirectory(t *testing.T) {
	docs := []core.Knowledge{
		{Slug: "recall-ranking", DocType: "note", Title: "Recall ranking"},
		{Slug: "deployment/rollback", DocType: "runbook", Title: "Rollback"},
		{Slug: "docs/rollback", DocType: "note", Title: "Rollback (docs)"},
	}
	out := renderKnowledgeList(docs)
	if !strings.Contains(out, "deployment/\n") || !strings.Contains(out, "docs/\n") {
		t.Fatalf("output does not group by directory:\n%s", out)
	}
	if strings.Index(out, "deployment/") > strings.Index(out, "recall-ranking") {
		t.Fatalf("directories must sort alongside their leaf names:\n%s", out)
	}
}

func TestKnowledgeLsDirArgumentScopesToASubtree(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Rollback", "--in", "deployment")
	runCmd(t, "knowledge", "new", "--title", "Elsewhere", "--in", "docs")
	out := runCmd(t, "knowledge", "ls", "deployment", "--json")
	if !strings.Contains(out, "deployment/rollback") || strings.Contains(out, "docs/elsewhere") {
		t.Errorf("ls deployment did not scope correctly:\n%s", out)
	}
}

func TestKnowledgeLsTagFlagRequiresAllTags(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Both", "--tag", "a", "--tag", "b")
	runCmd(t, "knowledge", "new", "--title", "OnlyA", "--tag", "a")
	out := runCmd(t, "knowledge", "ls", "--tag", "a", "--tag", "b", "--json")
	if !strings.Contains(out, "\"slug\":\"both\"") || strings.Contains(out, "\"slug\":\"onlya\"") {
		t.Errorf("--tag a --tag b did not narrow to the entry with both:\n%s", out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestListKnowledgeFiltersByTagsRequiringAll|TestListKnowledgeScopesToADirectory' -v`
Run: `go test ./internal/cli -run 'TestKnowledgeLs' -v`
Expected: `KnowledgeFilter.Tags`/`.Dir` undefined; the CLI has no `--tag` flag on `ls` and no `[dir]` argument.

- [ ] **Step 3: Extend `KnowledgeFilter`**

In `internal/core/knowledge.go`, change `KnowledgeFilter`:

```go
type KnowledgeFilter struct {
	BoardID     string   // association only; entries with no board always match
	DocTypes    []string // doc_type values to keep; empty keeps all
	Provenances []string // ingestion paths to keep; empty keeps all
	Tags        []string // every listed tag must be present; empty keeps all
	Dir         string   // scope to this directory and its subtree; empty keeps everything
}
```

In `where()`, insert directly before `return strings.Join(clauses, " AND "), args`:

```go
	if len(f.Tags) > 0 {
		clauses = append(clauses,
			`id IN (SELECT kt.doc_id FROM knowledge_tag kt JOIN tag t ON t.id = kt.tag_id
			        WHERE t.name IN (?`+strings.Repeat(", ?", len(f.Tags)-1)+`)
			        GROUP BY kt.doc_id HAVING COUNT(DISTINCT t.name) = ?)`)
		for _, v := range f.Tags {
			args = append(args, v)
		}
		args = append(args, len(f.Tags))
	}
	if f.Dir != "" {
		clauses = append(clauses, "(slug = ? OR slug LIKE ? || '/%')")
		args = append(args, f.Dir, f.Dir)
	}
```

- [ ] **Step 4: Run the core tests to verify they pass**

Run: `go test ./internal/core -run 'TestListKnowledgeFiltersByTagsRequiringAll|TestListKnowledgeScopesToADirectory' -v`
Expected: PASS.

- [ ] **Step 5: Group the CLI's text rendering by directory**

In `internal/cli/knowledge.go`, replace `renderKnowledgeList`:

```go
// renderKnowledgeList is the text form of `knowledge ls`: a tree grouped by
// directory, since a knowledge slug may now be path-shaped. A root-level
// entry — the majority of any small vault — renders exactly as it always
// has; an entry under a directory gets a header line for that directory the
// first time it appears. JSON output (Emit's other branch) stays a flat
// array; a client can group it the same way from the slug.
func renderKnowledgeList(docs []core.Knowledge) string {
	sorted := slices.Clone(docs)
	slices.SortFunc(sorted, func(a, b core.Knowledge) int { return cmp.Compare(a.Slug, b.Slug) })
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	lastDir := ""
	for _, d := range sorted {
		dir, leaf := "", d.Slug
		if i := strings.LastIndex(d.Slug, "/"); i >= 0 {
			dir, leaf = d.Slug[:i], d.Slug[i+1:]
		}
		if dir != lastDir {
			if dir != "" {
				fmt.Fprintf(w, "%s/\n", dir)
			}
			lastDir = dir
		}
		indent := ""
		if dir != "" {
			indent = "  "
		}
		mark := ""
		if d.Private {
			mark = "private"
		}
		fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\t%s\n", indent, leaf, d.DocType, d.Provenance, mark, d.Title)
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}
```

Add `"slices"` to the file's imports (alongside the existing `"cmp"`, `"fmt"`, `"os"`, `"strings"`, `"text/tabwriter"`, `"time"`).

- [ ] **Step 6: Add `--tag` and the `[dir]` argument to `knowledge ls`**

In `internal/cli/knowledge.go`, replace `newKnowledgeLsCmd`:

```go
func newKnowledgeLsCmd() *cobra.Command {
	var thisBoard, cold bool
	var docTypes, provenances, tags []string
	cmd := &cobra.Command{
		Use:   "ls [dir]",
		Short: "List entries, as a tree grouped by directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				filter := core.KnowledgeFilter{DocTypes: docTypes, Provenances: provenances, Tags: tags}
				if thisBoard {
					filter.BoardID = app.Board.ID
				}
				if len(args) == 1 {
					dir, err := core.SlugifyPath(args[0])
					if err != nil {
						return err
					}
					filter.Dir = dir
				}
				var docs []core.Knowledge
				var err error
				if cold {
					docs, err = app.Core.ColdKnowledge(cmd.Context(), app.Project.ID)
				} else {
					docs, err = app.Core.ListKnowledge(cmd.Context(), app.Project.ID, filter)
				}
				if err != nil {
					return err
				}
				withholdContent(docs)
				return Emit(cmd, map[string]any{"knowledge": docs}, func() string {
					return renderKnowledgeList(docs)
				})
			})
		},
	}
	cmd.Flags().BoolVar(&thisBoard, "board-only", false, "this board's entries plus the unscoped ones")
	cmd.Flags().BoolVar(&cold, "cold", false, "entries nothing has read in 30 days")
	cmd.Flags().StringSliceVar(&docTypes, "type", nil, "only these doc types: "+strings.Join(core.Templates(), "|"))
	cmd.Flags().StringSliceVar(&provenances, "provenance", nil, "only these ingestion paths: "+strings.Join(core.Provenances(), "|"))
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "only entries with every one of these tags")
	return cmd
}
```

- [ ] **Step 7: Run the CLI tests to verify they pass**

Run: `go test ./internal/cli -run 'TestKnowledgeLs' -v`
Expected: PASS.

- [ ] **Step 8: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/core/knowledge.go internal/core/knowledge_dir_test.go \
        internal/cli/knowledge.go internal/cli/knowledge_dir_cmd_test.go
git commit -m "feat(core,cli): tags narrow knowledge ls, and ls renders a tree

Tags were write-only: KnowledgeFilter had no way to read them back.
--tag (AND semantics: every listed tag must be present) fixes that.
knowledge ls [dir] scopes to a directory and its subtree, and the text
rendering now groups entries under a directory header instead of a flat
table; JSON output is unchanged.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Lint reports directory depth, long names, and near-duplicate directories

**Needs virtual paths:** no.

**Files:**
- Modify: `internal/core/lint.go`
- Test: `internal/core/knowledge_dir_test.go`

**Interfaces:**
- Consumes: Task 1's `resembles`, `projectDirectories`.
- Produces: `LintFinding` kinds `deep_directory` (depth ≥ 3), `long_directory_name` (a directory segment over 30 characters), `similar_directory` (project-wide, using the same `resembles` rule directory creation refuses on).

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/knowledge_dir_test.go`:

```go
func findingsOfKind(findings []LintFinding, kind string) []LintFinding {
	var out []LintFinding
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestLintReportsDeepDirectories(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment/aws/runbooks"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if got := findingsOfKind(findings, "deep_directory"); len(got) != 1 {
		t.Fatalf("findings = %+v, want one deep_directory", findings)
	}
}

func TestLintDoesNotReportDeepDirectoryAtDepthTwo(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment/aws"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if got := findingsOfKind(findings, "deep_directory"); len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
}

func TestLintReportsALongDirectoryName(t *testing.T) {
	c, p, _ := kbCore(t)
	long := "a-directory-name-that-is-well-past-thirty-characters"
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Dir: long}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	got := findingsOfKind(findings, "long_directory_name")
	if len(got) != 1 || got[0].Ref != long {
		t.Fatalf("findings = %+v, want one naming %q", findings, long)
	}
}

func TestLintReportsSimilarDirectories(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "A", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "B", Dir: "deployment", NewDir: true}); err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if got := findingsOfKind(findings, "similar_directory"); len(got) == 0 {
		t.Fatalf("findings = %+v, want at least one similar_directory", findings)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestLintReportsDeepDirectories|TestLintDoesNotReportDeepDirectoryAtDepthTwo|TestLintReportsALongDirectoryName|TestLintReportsSimilarDirectories' -v`
Expected: FAIL — none of these findings exist yet.

- [ ] **Step 3: Add the findings**

In `internal/core/lint.go`, update the `LintFinding.Kind` doc comment:

```go
// LintFinding is one problem with the vault. Lint reports; it never repairs.
// Kind is one of: stub, broken_anchor, orphan, missing_artifact,
// deep_directory, long_directory_name, similar_directory.
type LintFinding struct {
	Kind string `json:"kind"`
	Doc  string `json:"doc"`
	Ref  string `json:"ref,omitempty"`
	Fix  string `json:"fix"`
}
```

Inside the per-doc `for _, d := range docs` loop in `Lint`, directly after the existing `outbound`/`orphan` block and before its closing `}`, insert:

```go
			if dirs := strings.Split(d.Slug, "/"); len(dirs) > 1 {
				dirs = dirs[:len(dirs)-1]
				if len(dirs) >= 3 {
					out = append(out, LintFinding{Kind: "deep_directory", Doc: d.Slug,
						Ref: strings.Join(dirs, "/"),
						Fix: "trellis knowledge mv " + d.Slug + " <a shallower path>   # depth is a design smell past two levels"})
				}
				for _, seg := range dirs {
					if len(seg) > 30 {
						out = append(out, LintFinding{Kind: "long_directory_name", Doc: d.Slug, Ref: seg,
							Fix: "trellis knowledge mv " + d.Slug + " <a shorter directory name>"})
					}
				}
			}
```

After the closing `}` of the `for _, d := range docs` loop, but still inside the `c.Tx` closure and before its `return nil`, insert the project-wide pairwise check:

```go
		dirs, derr := c.projectDirectories(tx, projectID)
		if derr != nil {
			return derr
		}
		slices.Sort(dirs)
		for i, a := range dirs {
			for _, b := range dirs[i+1:] {
				if resembles(a, b) {
					out = append(out, LintFinding{Kind: "similar_directory", Ref: a + ", " + b,
						Fix: "trellis knowledge mv <an entry under one> <the other>   # or leave both if they mean different things"})
				}
			}
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestLintReportsDeepDirectories|TestLintDoesNotReportDeepDirectoryAtDepthTwo|TestLintReportsALongDirectoryName|TestLintReportsSimilarDirectories' -v`
Expected: PASS.

- [ ] **Step 5: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/core/lint.go internal/core/knowledge_dir_test.go
git commit -m "feat(core): lint reports directory depth, length and near-duplicates

deep_directory fires past two levels, long_directory_name past 30
characters in one segment, and similar_directory reuses the same
resemblance rule directory creation already refuses on, so drift that
slips past --new-dir is still visible afterward.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: The session-start hook advertises `recall`

**Needs virtual paths:** no.

**Files:**
- Modify: `plugin/hooks/trellis_hook.py`
- Modify: `scripts/tests/test_plugin_hooks.py`

**Interfaces:**
- Consumes: nothing new.
- Produces: no code interface; the session-start `additionalContext` string now mentions `recall <text>`.

- [ ] **Step 1: Write the failing test**

Append to the `HookTests` class in `scripts/tests/test_plugin_hooks.py`:

```python
    def test_session_start_command_list_mentions_recall(self):
        result = self.invoke()
        context = result["hookSpecificOutput"]["additionalContext"]
        self.assertIn("recall <text>", context)
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `python3 -B -m unittest scripts.tests.test_plugin_hooks.HookTests.test_session_start_command_list_mentions_recall -v`
Expected: FAIL — `recall` is not in the command list yet.

- [ ] **Step 3: Update the command list**

In `plugin/hooks/trellis_hook.py`, change:

```python
            + "Commands: board show --brief; card ls; card show <ref>; card next --claim; "
            + "card new --title <text>; card move <ref> <column>; "
            + "card edit <ref> --body <text> --if-version <n>; "
            + "card note <ref> --body <text>; search <text>; knowledge show <slug>.\n"
```

to:

```python
            + "Commands: board show --brief; card ls; card show <ref>; card next --claim; "
            + "card new --title <text>; card move <ref> <column>; "
            + "card edit <ref> --body <text> --if-version <n>; "
            + "card note <ref> --body <text>; search <text>; recall <text>; "
            + "knowledge show <slug>.\n"
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `python3 -B -m unittest scripts.tests.test_plugin_hooks.HookTests.test_session_start_command_list_mentions_recall -v`
Expected: PASS.

- [ ] **Step 5: Run the full hook test suite**

Run: `python3 -B -m unittest discover -s scripts/tests -v`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add plugin/hooks/trellis_hook.py scripts/tests/test_plugin_hooks.py
git commit -m "docs(hook): session start advertises recall, not just search

recall fuses BM25, vector and link-graph ranking and is the better
retrieval; the hook is the one channel that reaches every session, so it
is worth the one string.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Directory-aware wikilinks

**Needs virtual paths: YES.** Do not start this task until
`docs/superpowers/specs/2026-09-16-virtual-paths-design.md` has shipped on
this codebase. Before writing any code, re-read the current
`ParseWikilinks`, `ParseReference` (`internal/core/markdown.go`) and
`resolveDocRef` (`internal/core/doc_relations.go`) — virtual paths will have
already rewritten them to remove the `KEY/slug`/`GLOBAL/slug` qualified forms
from relative parsing (per that spec's "Wikilinks" section: "A relative
target is slugified as a whole, so `[[a/b]]` becomes slug `a-b` until
knowledge paths give it a directory"). **This task's diff below is written
against today's pre-virtual-paths code, because that is the only code that
exists right now.** Its job is unaffected by exactly how virtual paths
restructured these functions: wherever a *relative* wikilink target is turned
into a slug with a single whole-string `Slugify(...)` call, replace that call
with `normalizeSlugPath(...)` (Task 1). Re-derive the exact line numbers
against the code as it stands; the behavior and the tests below do not
change.

**Files:**
- Modify: `internal/core/markdown.go` (`ParseWikilinks`, `ParseReference` — behavior only, see the note above)
- Modify: `internal/core/doc_relations.go` (`resolveDocRef` bare-leaf fallback)
- Modify: `internal/core/knowledge_mv.go` (`MoveKnowledge` calls a new `rewriteInboundWikilinks`)
- Modify: `internal/core/lint.go` (`ambiguous_link`; the `broken_anchor` anchor lookup uses a per-segment slug)
- Test: `internal/core/knowledge_dir_test.go`

**Interfaces:**
- Consumes: Task 1 (`normalizeSlugPath`), Task 4 (`MoveKnowledge`), and whatever `internal/vpath`/virtual paths left `Reference`, `ParseWikilinks` and `resolveDocRef` looking like — this task does not add to or change that surface, only the relative-target behavior inside it.
- Produces:
  - A relative wikilink target with a `/` resolves as a directory path within the current project, not a flattened slug.
  - `resolveDocRef` resolves a bare-leaf relative target (no `/`) when it matches exactly one entry in the project; more than one match stays a stub.
  - `func (c *Core) rewriteInboundWikilinks(tx *sqlx.Tx, doc *Knowledge, oldSlug string) error`
  - `func rewriteWikilinkTargets(body, oldSlug, newSlug string) string`
  - `LintFinding` kind `ambiguous_link` for a stub wikilink whose leaf matches more than one entry.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/knowledge_dir_test.go`:

```go
func TestWikilinkToADirectoryPathResolves(t *testing.T) {
	c, p, _ := kbCore(t)
	target, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge target: %v", err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Runbook index",
		Body: "See [[deployment/rollback]] for the steps.\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge doc: %v", err)
	}
	back, err := c.Backlinks(t.Context(), target.ID)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 1 || back[0].Title != doc.Title {
		t.Fatalf("Backlinks = %+v, want one from %q", back, doc.Title)
	}
}

func TestWikilinkBareLeafResolvesTheUniqueMatch(t *testing.T) {
	c, p, _ := kbCore(t)
	target, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge target: %v", err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Index", Body: "See [[rollback]].\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge doc: %v", err)
	}
	back, err := c.Backlinks(t.Context(), target.ID)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 1 || back[0].Title != doc.Title {
		t.Fatalf("Backlinks = %+v, want one from %q", back, doc.Title)
	}
}

func TestWikilinkToAnAmbiguousLeafStaysAStubAndLintReportsIt(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"}); err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "docs"}); err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Index", Body: "See [[rollback]].\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge doc: %v", err)
	}
	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Kind == "ambiguous_link" && f.Doc == doc.Slug {
			found = true
		}
	}
	if !found {
		t.Fatalf("findings = %+v, want an ambiguous_link for %s", findings, doc.Slug)
	}
}

func TestMoveKnowledgeRewritesInboundWikilinks(t *testing.T) {
	c, p, _ := kbCore(t)
	target, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback"})
	if err != nil {
		t.Fatalf("CreateKnowledge target: %v", err)
	}
	referrer, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Index",
		Body: "See [[rollback]] for the steps.\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge referrer: %v", err)
	}
	moved, err := c.MoveKnowledge(t.Context(), p.ID, target.Slug, "deployment/rollback", false)
	if err != nil {
		t.Fatalf("MoveKnowledge: %v", err)
	}
	raw, err := os.ReadFile(referrer.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "[[deployment/rollback]]") {
		t.Fatalf("referrer body = %q, want the wikilink rewritten to the new path", raw)
	}
	back, err := c.Backlinks(t.Context(), moved.ID)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 1 {
		t.Fatalf("Backlinks = %+v, want the link to survive the move", back)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestWikilink|TestMoveKnowledgeRewritesInboundWikilinks' -v`
Expected: FAIL, with the specific failure depending on what virtual paths left behind — a directory-shaped relative target does not resolve, or resolves to the wrong (flattened) slug.

- [ ] **Step 3: Make relative-target slugification path-aware**

Find every place virtual paths left a **relative** wikilink or reference target being turned into a slug with a single `Slugify(wholeTarget)` call (this was `ParseWikilinks` and `ParseReference` in `internal/core/markdown.go` before virtual paths, at the assignments to `ref.Slug`). Replace that call with `normalizeSlugPath(wholeTarget)`. Do **not** touch how an absolute address (leading `/`) is parsed — that is `internal/vpath`'s territory and out of scope here.

- [ ] **Step 4: Add the bare-leaf fallback to `resolveDocRef`**

`resolveDocRef` today (before virtual paths) ends with:

```go
	var id string
	err := tx.Get(&id, q, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return id, nil
}
```

Re-verify this shape against the code as virtual paths left it — the exact-match query (`q`/`args`) and the qualifier switch above it will look different, but the "try an exact match, and a miss is a stub, never an error" contract will not. Replace the block above with:

```go
	var id string
	err := tx.Get(&id, q, args...)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// Bare-leaf fallback: only for an unqualified reference that does not
	// already name a directory. A unique match resolves; more than one stays
	// a stub, which Lint reports as ambiguous_link rather than stub. If
	// virtual paths removed Reference.ProjectKey entirely for a relative
	// target (rather than leaving it always ""), drop that half of the
	// condition — every non-absolute reference reaching this point is
	// unqualified by construction.
	if ref.ProjectKey == "" && !strings.Contains(ref.Slug, "/") {
		var matches []string
		if err := tx.Select(&matches,
			`SELECT id FROM knowledge WHERE project_id = ? AND slug LIKE '%/' || ?`, projectID, ref.Slug); err != nil {
			return nil, err
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
	}
	return nil, nil
}
```

Do not add bare-leaf matching to the `GLOBAL` or cross-project branches (the code above the exact-match query) — those keep resolving by exact slug only, per the design's "Reference syntax"/virtual-paths "Wikilinks" sections.

- [ ] **Step 5: Report `ambiguous_link` in Lint**

In `internal/core/lint.go`, inside the per-doc wikilink loop, replace the stub branch:

```go
				if !r.ToID.Valid {
					out = append(out, LintFinding{Kind: "stub", Doc: d.Slug, Ref: r.ToRaw,
						Fix: `trellis knowledge new --title "` + r.ToRaw + `"`})
					continue
				}
```

with:

```go
				if !r.ToID.Valid {
					kind, fix := "stub", `trellis knowledge new --title "`+r.ToRaw+`"`
					leaf, _, _ := strings.Cut(r.ToRaw, "#")
					if leaf = normalizeSlugPath(leaf); !strings.Contains(leaf, "/") {
						var n int
						if err := tx.Get(&n,
							`SELECT COUNT(*) FROM knowledge WHERE project_id = ? AND slug LIKE '%/' || ?`,
							d.ProjectID, leaf); err != nil {
							return err
						}
						if n > 1 {
							kind, fix = "ambiguous_link", "trellis knowledge show <full path>   # "+r.ToRaw+" matches more than one entry"
						}
					}
					out = append(out, LintFinding{Kind: kind, Doc: d.Slug, Ref: r.ToRaw, Fix: fix})
					continue
				}
```

- [ ] **Step 6: Fix the `broken_anchor` check's slug lookup**

In the same loop, the existing anchor check flattens a qualified slug with `Slugify(slug)` after stripping a leading `key/` segment:

```go
				slug, _, _ := strings.Cut(r.ToRaw, "#")
				if _, rest, ok := strings.Cut(slug, "/"); ok {
					slug = rest
				}
				if set, known := anchorsBySlug[Slugify(slug)]; known && !set[r.Anchor.String] {
```

Once virtual paths has removed the qualified `key/rest` split from relative parsing, that middle `strings.Cut` is stale — a relative slug never has a project-key prefix to strip anymore, and `Slugify(slug)` must not flatten a genuine directory. Replace those three lines with:

```go
				slug, _, _ := strings.Cut(r.ToRaw, "#")
				if set, known := anchorsBySlug[normalizeSlugPath(slug)]; known && !set[r.Anchor.String] {
```

(`anchorsBySlug` is keyed by `d.Slug` already, which is the full path-shaped slug, so this now compares like with like.)

- [ ] **Step 7: Rewrite inbound wikilinks on `knowledge mv`**

Append to `internal/core/knowledge_mv.go`:

```go
// rewriteInboundWikilinks updates every doc whose body contains a wikilink to
// oldSlug so it names doc.Slug instead, and resyncs that doc's derived rows
// from the rewritten file. It runs inside knowledge mv's own transaction:
// leaving this for later would mean the move quietly erodes the link graph
// the moment the referring document is next edited and its wikilinks are
// re-resolved against a slug that no longer exists. Only from_type = 'doc'
// links are rewritten: a card's structured link (trellis link) stores its
// own to_raw text in the link table and is never parsed from the card's
// body, so there is no card file to rewrite.
//
// If the revision-history feature has landed by the time this is
// implemented, this write must capture a revision for each referring
// document exactly as EditKnowledgeFields does before it replaces a file --
// check that function's current shape before writing this one.
func (c *Core) rewriteInboundWikilinks(tx *sqlx.Tx, doc *Knowledge, oldSlug string) error {
	var fromIDs []string
	if err := tx.Select(&fromIDs,
		`SELECT DISTINCT from_id FROM link
		 WHERE to_type = 'doc' AND to_id = ? AND from_type = 'doc' AND rel = 'wikilink'`, doc.ID); err != nil {
		return err
	}
	for _, fromID := range fromIDs {
		var from Knowledge
		if err := tx.Get(&from, `SELECT * FROM knowledge WHERE id = ?`, fromID); err != nil {
			return err
		}
		raw, err := os.ReadFile(from.Path)
		if err != nil {
			return err
		}
		fm, body, err := splitDocFile(from.Path, raw)
		if err != nil {
			return err
		}
		newBody := rewriteWikilinkTargets(body, oldSlug, doc.Slug)
		if newBody == body {
			continue
		}
		out := RenderDoc(fm, newBody)
		if err := replaceIfUnchanged(from.Path, []byte(out), ContentHash(string(raw))); err != nil {
			return err
		}
		from.BodyMD = newBody
		if err := c.syncDocRelations(tx, &from, fm, newBody); err != nil {
			return err
		}
	}
	if len(fromIDs) > 0 {
		return c.rebuildKnowledgeFTS(tx)
	}
	return nil
}

// rewriteWikilinkTargets replaces the target of every [[target#anchor|alias]]
// wikilink whose slug is oldSlug with newSlug, leaving the anchor, the alias
// and everything outside a fenced code block exactly as written.
func rewriteWikilinkTargets(body, oldSlug, newSlug string) string {
	fences := fenceRE.FindAllStringIndex(body, -1)
	inFence := func(pos int) bool {
		for _, f := range fences {
			if pos >= f[0] && pos < f[1] {
				return true
			}
		}
		return false
	}
	matches := wikiLinkRE.FindAllStringSubmatchIndex(body, -1)
	var b strings.Builder
	last := 0
	for _, m := range matches {
		if inFence(m[0]) {
			continue
		}
		target := body[m[2]:m[3]]
		if normalizeSlugPath(strings.TrimSpace(target)) != oldSlug {
			continue
		}
		b.WriteString(body[last:m[2]])
		b.WriteString(newSlug)
		last = m[3]
	}
	b.WriteString(body[last:])
	return b.String()
}
```

In `MoveKnowledge`, directly after `if err := c.docView(tx, &doc); err != nil { return err }` and before `done = true`, insert:

```go
		if err := c.rewriteInboundWikilinks(tx, &doc, oldSlug); err != nil {
			return err
		}
```

(`oldSlug` is already in scope from the earlier `oldSlug := doc.Slug` line Task 4 added.)

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestWikilink|TestMoveKnowledgeRewritesInboundWikilinks' -v`
Expected: PASS.

- [ ] **Step 9: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && go test -race ./internal/core`
Expected: all pass.

Run: `GOOS=windows go build ./...`
Expected: succeeds.

- [ ] **Step 10: Commit**

```bash
git add internal/core/markdown.go internal/core/doc_relations.go internal/core/knowledge_mv.go \
        internal/core/lint.go internal/core/knowledge_dir_test.go
git commit -m "feat(core): wikilinks resolve directory paths and bare leaves

A relative wikilink target is now path-shaped (normalizeSlugPath) instead
of flattened by a single Slugify call, on top of the virtual-paths layer's
removal of the KEY/slug and GLOBAL/slug qualified forms. resolveDocRef
adds the same bare-leaf fallback knowledge show already has; an ambiguous
leaf stays a stub and lint reports ambiguous_link instead of stub.
knowledge mv now rewrites every inbound wikilink's stored target in the
same transaction as the move, so the link graph does not quietly erode the
next time a referring document is edited.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Self-review

**Spec coverage.**

- Path is optional; root-level documents unchanged — Task 2 (`Dir` empty is a no-op).
- `uniqueSlug` operates on the full path — Task 2 (verified unchanged in behavior, with a targeted test for the coexistence case).
- Reference syntax — superseded per the amendment; Task 9 implements the amendment's directory-relaxation of the vpath knowledge-slug grammar and explicitly does not implement `[[GLOBAL:slug]]`.
- Validation (segments, traversal, reserved names, length ceilings, generated-vs-explicit truncation/rejection) — Task 1, with the full test matrix from the spec's own Testing section.
- Depth lint findings and long-directory-name lint — Task 7.
- Directory resemblance refusal and `--new-dir` — Task 2 (creation) and Task 4 (`mv`'s destination), sharing `resembles`/`refuseResemblingDir` from Task 1.
- Bare-leaf resolution and its ambiguity table (`show`/`mv`/`rm` as errors, wikilinks as stubs) — Task 3 (`show`/`rm`, and `mv` reuses it in Task 4) and Task 9 (wikilinks).
- Moving (`knowledge mv`, inbound wikilink rewriting, `promote`/`demote` preserving subpath) — Tasks 4, 5 and 9.
- Retrieval stays flat — no `--path` filter was added to `search`/`recall`; `ls`'s `[dir]` argument is deliberately scoped to `ls`, matching the design's own carve-out for it, not to `search`.
- Carried in the same pass: `Tags` on `KnowledgeFilter` and `--tag` — Task 6; `ls` tree view and `ls <dir>` — Task 6; the hook's command list gaining `recall` — Task 8.
- Non-goals (no reserved `index.md`, no fixed vocabulary, no count cap, no directory-inherited disclosure) — nothing in this plan adds any of them.
- Testing section — every bullet maps to a task above; the "Windows CI" bullet is covered by the `GOOS=windows go build ./...` gate repeated in Tasks 4 and 9 and the Global Constraints gate list (this plan does not add a Windows-only test job; the existing CI matrix already runs the package's tests on Windows).

**Gap found and fixed during review:** the original spec's "Two call sites flatten a path" note names `loadDoc` and `ParseReference`. `loadDoc` is fixed in Task 3 via `resolveSlug`/`normalizeSlugPath`. `ParseReference` is bundled into Task 9 alongside `ParseWikilinks` since both are owned by the same post-virtual-paths file and both need the identical fix; an earlier draft of this plan only mentioned `ParseWikilinks` in Task 9's title and has been corrected to name both explicitly.

**Placeholder scan.** No "TBD"/"handle appropriately"/"similar to Task N" remain. The one place this plan is deliberately underspecified is Task 9's Step 3 (the exact line numbers of a file that does not exist yet on this branch) — that is a stated, reasoned exception per the "Layer dependency" note, not an oversight, and every other step in Task 9 gives complete code.

**Type consistency.** `NewKnowledge.Dir`/`.NewDir` (Task 2) flow unchanged into `KnowledgeFilter.Dir` (Task 6, a different field on a different struct — verified no name reuse causes confusion) and into `MoveKnowledge`'s `newDir bool` parameter (Task 4) and `refuseResemblingDir`'s `allowNew bool` parameter (Task 1) — same boolean meaning throughout, different names because one is a struct field mirroring a CLI flag and the other two are function parameters. `resolveSlug`'s `includeGlobal bool` is used identically in Tasks 3 and 4 (`false` for both `DeleteKnowledge` and `MoveKnowledge`, `true` for `loadDoc`). `Error.Detail` is `any` (existing type); Task 3 sets it to `[]string`, and its test asserts that concrete type — consistent with no other code path relying on a different `Detail` shape.

**Ruling on spec ambiguity — resolved in this plan:** the spec does not say whether "existing directories" for the resemblance check and for lint's `similar_directory` finding are enumerated from the filesystem or from the database. This plan enumerates from `knowledge.slug` (Task 1's `projectDirectories`), not from a filesystem walk: it is simpler, self-cleans when a directory's last entry is deleted, and — per the revision-history spec's own reasoning ("no entry or directory name can start with a dot") — a hidden revision directory can never appear in it, satisfying that spec's "skips dot-directories" requirement without any extra code.
