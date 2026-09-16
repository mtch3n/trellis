# Virtual Path Addressing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every object one canonical address (`/KEY/boards/<slug>`, `/KEY/cards/<KEY-N>`, `/KEY/knowledge/<slug>`, `/GLOBAL/knowledge/<slug>`, `/KEY/artifacts/<name>`). Every reference argument accepts it, and every output carries it.

**Architecture:**
- **Grammar.** `internal/vpath` gains a general `Parse` beside the pin parser.
- **Core output.** Core builds document addresses in one Go helper and one SQL fragment.
- **Core input.** Core reads absolute knowledge and artifact arguments itself, and refuses an address naming a project other than the one it acts in (`wrong_project`).
- **Wikilinks** resolve across projects.
- **CLI.** One helper, `withTargets`, reads a command's references, positional and flags alike. When a reference names a project, the helper routes the command there, so the command needs no pin.
- **Web.** One small parser.

**Tech Stack:** Go 1.27 (`errors.AsType`, stdlib `uuid`, `t.Chdir`), SQLite via `sqlx`, goose SQL migrations, cobra, React + TypeScript built with pnpm and Vite, Node 26 `node --test`.

**Spec:** `docs/superpowers/specs/2026-09-16-virtual-paths-design.md`

## Prerequisite

Before starting, all three must hold:

1. **The pin-only plan is fully executed.** That plan is `docs/superpowers/plans/2026-09-16-pin-only-projects.md`. This plan relies on what it adds:
   - `internal/vpath` with `Path{Project, Collection, Name}`, `ParsePin`, `ProjectPath`, `BoardPath`, `Board()`, `ValidKey`, `ValidSlug`, `KeyFromName`, `GlobalKey` and `CollectionBoards`;
   - `internal/resolve/pin.go`;
   - `internal/cli/resolve.go`, with `resolvedProject`, `resolveProject`, `selectBoard` and `pinFailure`;
   - `core.CreateProject`, `core.BoardBySlug` and `core.InitProject`;
   - migration 0013;
   - the `store` test helpers `openAtVersion` and `mustExec`;
   - the `cli` test helpers `pinEnv`, `seedProject`, `writePin`, `coreErr` and `showBoard`;
   - the `core` test helper `errCode`.
2. **Other in-progress work is committed.** During planning, `web/` was being edited by someone else: the redesign, new pages, `OverviewPage.tsx` and `RootRedirect.tsx`. `plugin/skills/*` and `PRODUCT.md` were also uncommitted.
3. **Check the tree.** Run `git status --short`. If anything outside `docs/` is modified or untracked, stop and ask the author. Do not stash it and do not commit it for them.

## Global Constraints

- **The noun commands stay.** Do not add generic `ls`, `cat` or `mv` verbs.
- **A card's `ref` in output stays `KEY-N`.** Document refs become `/KEY/knowledge/<slug>` or `/GLOBAL/knowledge/<slug>`. Artifacts gain `ref`, of the form `/KEY/artifacts/<name>`.
- **Every argument or flag that takes a reference also takes an absolute address.** A relative argument keeps today's meaning.
- **An absolute argument, or a qualified card ref such as `OTHER-12`, names its own project.**
  - It beats the pin and `TRELLIS_PROJECT`.
  - A different `--project` is `project_conflict`, and so are two references naming different projects.
  - A relative reference means the current project; with one resolved, a reference naming another project is `project_conflict`.
  - `KEY-N` is never re-scoped to the current project, and a card address reaches core with its project (`CardRef.Project`).
- **A `/GLOBAL/knowledge` argument to a read or edit consults nothing ambient.**
- **Core refuses an absolute reference to another project** with `wrong_project`. `/GLOBAL` is allowed where the operation allows it.
- **The old reference forms are removed outright.** `[[KEY/slug]]` and `[[GLOBAL/slug]]` have no compatibility path, and raw link text is not migrated.
- **A dangling wikilink is a stub, not an error.** `resolveDocStubs` backfills it.
- **Recall stays scoped to the current project plus the vault.**
- **User text never reaches FTS5 `MATCH` unquoted.** This plan touches none of those paths.
- **Writes go through `writeAtomic` / `stageRemoval`.**
- **CI runs Linux, macOS and Windows; all three must pass.**
- **Web conventions:**
  - Use pnpm, and add no new dependency.
  - `web/dist` is rebuilt and committed whenever `web/src` changes.
- **Gates before any task is done:**
  - `go build ./...`
  - `go test ./...`
  - `go vet ./...`
  - `gofmt -l .` prints nothing
  - `staticcheck ./...`
  - `GOOS=windows go build ./...`
- **Formatting.** Code blocks here are not guaranteed to be gofmt-aligned. Run `gofmt -w` on every Go file you touch before the gates.
- **Commit messages** follow the repository style, `feat(core): <what now happens>`, and end with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`

## File Structure

| File | Responsibility |
|---|---|
| `internal/vpath/vpath.go` (modify) | `Parse`, `SplitAnchor`, collection constants, constructors, validators |
| `internal/vpath/vpath_test.go` (modify) | Address grammar tests |
| `internal/core/address.go` (create) | `DocAddress`, `docAddressSQL`, `ArtifactAddress`, `ParseAddress`, `wrongProject`, `projectKeyOf`, `readDocArg`, `vaultSlug`, `isUUID` |
| `internal/core/address_test.go` (create) | Output-address tests |
| `internal/core/knowledge_address_test.go` (create) | Knowledge-argument tests |
| `internal/core/wikilink_address_test.go` (create) | Cross-project wikilink and lint tests |
| `internal/core/card_ref_test.go` (create) | `wrong_project` and `cross_project_block` tests |
| `internal/core/artifact_address_test.go` (create) | Vault slug, artifact name and graph tests |
| `internal/core/knowledge.go` | `docView`, `loadDoc`, `DeleteKnowledge`, `GlobalKey` comment |
| `internal/core/search.go` | Addresses in `Search` and `matchKnowledge`; new `KnowledgeHit` |
| `internal/core/recall.go` | Address in the document query |
| `internal/core/doc_relations.go` | `Backlinks`, `resolveDocRef`, `LinkCardToDoc` |
| `internal/core/markdown.go` | `Reference`, `ParseWikilinks`, `ParseReference` |
| `internal/core/lint.go` | Anchors by target id, new finding kinds, `Doc` as an address |
| `internal/core/graph.go` | Document and artifact node refs |
| `internal/core/dupes.go` | Slug from an address |
| `internal/core/ids.go` | `ParseCardRef` accepts a card address |
| `internal/core/card.go` | `checkCardProject` in `loadCard` |
| `internal/core/blocker.go`, `internal/core/import.go` | `cross_project_block` |
| `internal/core/pin.go` | Vault slug refusal, stub backfill, `vaultSlug` in demote and verify |
| `internal/core/artifact.go` | `Ref`, unique names, `ResolveArtifact` |
| `internal/store/migrations/0014_unique_addresses.sql` (create) | Two unique indexes |
| `internal/store/migrate_0014_test.go` (create) | Migration tests |
| `internal/retrieval/service.go` | Vector hits through `core.KnowledgeHit` |
| `internal/ui/server.go` | Event refs, vault list refs |
| `internal/cli/target.go` (create) | `refArg`, `argProject`, `withTargets`, `withTarget`, `targetContext`, `namedBoard`, `tuiCardRef`, `isUnresolved`, `isVaultAddress` |
| `internal/cli/resolve.go` | `namedProjectKey`, `boardNamed`, `projectConflict`, `normalizeProjectArg` |
| `internal/cli/root.go` | `projectKey` removed |
| `internal/cli/target_test.go`, `internal/cli/target_knowledge_test.go` (create) | CLI routing tests |
| `internal/cli/card.go`, `card_block.go`, `card_archive.go`, `link.go`, `knowledge.go`, `board.go`, `artifact.go`, `tui.go`, `search.go`, `doctor.go`, `init.go` | Use the helpers; remove dead search code |
| `web/src/lib/vpath.ts` (create), `web/scripts/vpath.test.mjs` (create) | Web address parser |
| `web/src/lib/knowledge-graph.ts`, `web/src/pages/OverviewPage.tsx`, `web/src/components/wrappers/ProjectTimeline.tsx`, `web/src/lib/timeline-marks.ts`, `web/package.json`, `web/dist` | Web call sites and build |
| `plugin/hooks/trellis_hook.py`, `plugin/integrations/claude_code/recall.py`, `plugin/skills/trellis/SKILL.md`, `plugin/skills/writing-knowledge/SKILL.md`, `scripts/tests/test_integrations.py`, `README.md`, `PRODUCT.md`, `CLAUDE.md`, `docs/superpowers/specs/2026-09-16-knowledge-paths-design.md` | Teaching text and the knowledge-paths amendment |

---

### Task 1: The address grammar

**Files:**
- Modify: `internal/vpath/vpath.go`
- Test: `internal/vpath/vpath_test.go`

**Interfaces:**
- Consumes: `Path`, `ProjectPath`, `ValidKey`, `ValidSlug`, `GlobalKey`, `CollectionBoards` (pin-only plan).
- Produces:
  - `const CollectionCards = "cards"`, `CollectionKnowledge = "knowledge"`, `CollectionArtifacts = "artifacts"`
  - `func KnowledgePath(key, slug string) Path`
  - `func GlobalKnowledgePath(slug string) Path`
  - `func CardPath(key, ref string) Path`
  - `func ArtifactPath(key, name string) Path`
  - `func SplitAnchor(s string) (target, anchor string)`
  - `func Parse(s string) (Path, error)`
  - `func ValidCardRef(s string) bool`
  - `func ValidDocSlug(s string) bool`
  - `func ValidArtifactName(s string) bool`

- [ ] **Step 1: Write the failing tests**

Append to `internal/vpath/vpath_test.go`:

```go
func TestParseReadsEveryCollection(t *testing.T) {
	cases := map[string]Path{
		"/TRELLIS":                       ProjectPath("TRELLIS"),
		"/trellis":                       ProjectPath("TRELLIS"),
		"/TRELLIS/boards/api":            BoardPath("TRELLIS", "api"),
		"/mono/cards/mono-12":            CardPath("MONO", "MONO-12"),
		"/MONO/cards/my_app-1":           CardPath("MONO", "MY_APP-1"),
		"/KIOSK-ANALYSE/cards/KIOSK-ANALYSE-3": CardPath("KIOSK-ANALYSE", "KIOSK-ANALYSE-3"),
		"/MONO/knowledge/concurrency-model":    KnowledgePath("MONO", "concurrency-model"),
		"/GLOBAL/knowledge/conventions":        GlobalKnowledgePath("conventions"),
		"/global/knowledge/conventions":        GlobalKnowledgePath("conventions"),
		"/MONO/artifacts/Screen Shot.PNG":      ArtifactPath("MONO", "Screen Shot.PNG"),
		"  /MONO/knowledge/x \n":               KnowledgePath("MONO", "x"),
	}
	for in, want := range cases {
		got, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"TRELLIS":                  "starts with /",
		"MONO/knowledge/x":         "starts with /",
		"/":                        "not a project key",
		"/1ABC":                    "not a project key",
		"/MY_APP/cards/MY_APP-1":   "not a project key",
		"/GLOBAL":                  "GLOBAL holds only knowledge",
		"/GLOBAL/cards/X-1":        "GLOBAL holds only knowledge",
		"/MONO/boards":             "expected /KEY/<collection>/<name>",
		"/MONO/knowledge/a/b":      "expected /KEY/<collection>/<name>",
		"/MONO/boards/API":         "not a board slug",
		"/MONO/cards/12":           "not a card ref",
		"/MONO/cards/":             "not a card ref",
		"/MONO/knowledge/Bad Slug": "not a knowledge slug",
		"/MONO/artifacts/..":       "not an artifact name",
		"/MONO/notes/x":            "not a collection",
		"/MONO/knowledge/x#why":    "anchor",
	}
	for in, fragment := range cases {
		_, err := Parse(in)
		if err == nil {
			t.Errorf("Parse(%q) succeeded, want an error mentioning %q", in, fragment)
			continue
		}
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("Parse(%q) error = %q, want it to mention %q", in, err, fragment)
		}
	}
}

func TestAddressesRoundTrip(t *testing.T) {
	for _, p := range []Path{
		ProjectPath("A"), BoardPath("A", "api"), CardPath("A", "A-1"),
		KnowledgePath("A", "design"), GlobalKnowledgePath("design"), ArtifactPath("A", "shot.png"),
	} {
		got, err := Parse(p.String())
		if err != nil || got != p {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", p.String(), got, err, p)
		}
	}
	if got := GlobalKnowledgePath("x").String(); got != "/GLOBAL/knowledge/x" {
		t.Errorf("GlobalKnowledgePath = %q", got)
	}
}

func TestSplitAnchor(t *testing.T) {
	cases := map[string][2]string{
		"design":                      {"design", ""},
		"design#why":                  {"design", "why"},
		"/MONO/knowledge/design#a#b":  {"/MONO/knowledge/design", "a#b"},
	}
	for in, want := range cases {
		target, anchor := SplitAnchor(in)
		if target != want[0] || anchor != want[1] {
			t.Errorf("SplitAnchor(%q) = %q, %q; want %q, %q", in, target, anchor, want[0], want[1])
		}
	}
}

func TestNameValidators(t *testing.T) {
	// A card ref's prefix is looser than a project key: a card keeps its ref
	// when its project is merged, and keys that predate the key grammar, such
	// as MY_APP, are still somebody's prefix.
	for s, want := range map[string]bool{
		"MONO-12": true, "KIOSK-ANALYSE-3": true, "MY_APP-1": true, "V1.2-3": true,
		"mono-12": false, "12": false, "MONO-": false, "MONO-1a": false, "-12": false,
		"A/B-1": false, "A#B-1": false, "A B-1": false,
	} {
		if ValidCardRef(s) != want {
			t.Errorf("ValidCardRef(%q) = %v", s, !want)
		}
	}
	for s, want := range map[string]bool{"design": true, "design-2": true, "Design": false, "a--b": false, "": false, "重構": false} {
		if ValidDocSlug(s) != want {
			t.Errorf("ValidDocSlug(%q) = %v", s, !want)
		}
	}
	for s, want := range map[string]bool{"shot.png": true, "Screen Shot.PNG": true, "": false, ".": false, "..": false, "a/b": false, `a\b`: false, "a\x00b": false} {
		if ValidArtifactName(s) != want {
			t.Errorf("ValidArtifactName(%q) = %v", s, !want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vpath/`
Expected: FAIL. The build fails because `Parse`, `CardPath`, `KnowledgePath`, `GlobalKnowledgePath`, `ArtifactPath`, `SplitAnchor` and the validators are undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/vpath/vpath.go`:

```go
// The collections an address may name inside a project, besides boards.
const (
	CollectionCards     = "cards"
	CollectionKnowledge = "knowledge"
	CollectionArtifacts = "artifacts"
)

var (
	cardRefRE = regexp.MustCompile(`^[^/#\s-][^/#\s]*-[0-9]+$`)
	docSlugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// KnowledgePath names a project's knowledge entry.
func KnowledgePath(key, slug string) Path {
	return Path{Project: key, Collection: CollectionKnowledge, Name: slug}
}

// GlobalKnowledgePath names a vault entry.
func GlobalKnowledgePath(slug string) Path { return KnowledgePath(GlobalKey, slug) }

// CardPath names a card by its full ref. The ref's prefix is not required to
// be key: a project that absorbed another holds cards with a foreign prefix.
func CardPath(key, ref string) Path {
	return Path{Project: key, Collection: CollectionCards, Name: ref}
}

// ArtifactPath names an artifact by file name.
func ArtifactPath(key, name string) Path {
	return Path{Project: key, Collection: CollectionArtifacts, Name: name}
}

// SplitAnchor cuts an address or a link target at its first '#'.
func SplitAnchor(s string) (target, anchor string) {
	target, anchor, _ = strings.Cut(s, "#")
	return target, anchor
}

// Parse reads an absolute address:
//
//	/KEY
//	/KEY/boards/<slug>
//	/KEY/cards/<KEY-N>
//	/KEY/knowledge/<slug>
//	/GLOBAL/knowledge/<slug>
//	/KEY/artifacts/<name>
//
// The key and a card ref are case-insensitive and returned upper-case. An
// anchor is not part of an address; split it off first with SplitAnchor.
func Parse(s string) (Path, error) {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, "/")
	if !ok {
		return Path{}, fmt.Errorf("%q is not an address: an address starts with /", s)
	}
	if strings.Contains(s, "#") {
		return Path{}, fmt.Errorf("%q carries an anchor; an address names the entry, not a heading", s)
	}
	segs := strings.Split(rest, "/")
	key := strings.ToUpper(segs[0])
	if !ValidKey(key) {
		return Path{}, fmt.Errorf("%q is not a project key: use letters, digits and single hyphens, starting with a letter", segs[0])
	}
	if len(segs) == 1 {
		if key == GlobalKey {
			return Path{}, errors.New("GLOBAL holds only knowledge: /GLOBAL/knowledge/<slug>")
		}
		return ProjectPath(key), nil
	}
	if len(segs) != 3 {
		return Path{}, fmt.Errorf("%q is not an address; expected /KEY/<collection>/<name>", s)
	}
	collection, name := segs[1], segs[2]
	if key == GlobalKey && collection != CollectionKnowledge {
		return Path{}, errors.New("GLOBAL holds only knowledge: /GLOBAL/knowledge/<slug>")
	}
	switch collection {
	case CollectionBoards:
		if !ValidSlug(name) {
			return Path{}, fmt.Errorf("%q is not a board slug: use lower-case letters and digits joined by single hyphens", name)
		}
	case CollectionCards:
		name = strings.ToUpper(name)
		if !ValidCardRef(name) {
			return Path{}, fmt.Errorf("%q is not a card ref: expected KEY-N, such as %s-12", segs[2], key)
		}
	case CollectionKnowledge:
		if !ValidDocSlug(name) {
			return Path{}, fmt.Errorf("%q is not a knowledge slug: use lower-case letters and digits joined by single hyphens", name)
		}
	case CollectionArtifacts:
		if !ValidArtifactName(name) {
			return Path{}, fmt.Errorf("%q is not an artifact name", name)
		}
	default:
		return Path{}, fmt.Errorf("%q is not a collection: use boards, cards, knowledge or artifacts", collection)
	}
	return Path{Project: key, Collection: collection, Name: name}, nil
}

// ValidCardRef reports whether s is an upper-case card ref, PREFIX-N. The
// prefix is deliberately looser than a project key: a card keeps its ref when
// its project is merged into another, and keys created before the key
// grammar existed (MY_APP) are still prefixes. It only has to be one address
// segment, so it holds no /, # or whitespace, and does not start with -.
func ValidCardRef(s string) bool { return s == strings.ToUpper(s) && cardRefRE.MatchString(s) }

// ValidDocSlug reports whether s has the shape core.Slugify produces.
func ValidDocSlug(s string) bool { return docSlugRE.MatchString(s) }

// ValidArtifactName reports whether s can be one artifact file name.
func ValidArtifactName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/vpath/ && go vet ./internal/vpath/ && gofmt -l internal/vpath`
Expected: `ok`, including the pin tests, and nothing printed by `gofmt`.

- [ ] **Step 5: Commit**

```bash
git add internal/vpath
git commit -m "feat(vpath): parse an address in any collection

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---
### Task 2: Document addresses in every output

**Files:**
- Create: `internal/core/address.go`
- Test: `internal/core/address_test.go`
- Modify:
  - `internal/core/knowledge.go`: the `GlobalKey` comment, the `Knowledge.Ref` comment, `docView`
  - `internal/core/search.go`: the document queries in `Search` and `matchKnowledge`; new `KnowledgeHit`
  - `internal/core/recall.go`: the document query
  - `internal/core/doc_relations.go`: `Backlinks`
  - `internal/core/graph.go`: the document node
  - `internal/core/dupes.go`
  - `internal/retrieval/service.go`
  - `internal/cli/search.go`: delete the dead `vectorSearchHits`, `mergeSearchHits` and `min`
  - `internal/ui/server.go`: `handleProjectEvents` document refs, `handleGlobalKnowledgeList`
- Modify existing tests:
  - `internal/core/knowledge_test.go`
  - `internal/core/recall_test.go`
  - `internal/core/pin_test.go`
  - `internal/ui/server_test.go`

**Interfaces:**
- Consumes: `vpath.KnowledgePath`, `vpath.GlobalKnowledgePath`, `vpath.GlobalKey`, `vpath.Parse` (Task 1).
- Produces:
  - `func DocAddress(key string, global bool, slug string) string`
  - `const docAddressSQL`, which assumes the aliases `k` = knowledge and `p` = project
  - `func (c *Core) KnowledgeHit(ctx context.Context, docID, projectID string, allProjects bool, label string) (SearchHit, error)`
  - `Knowledge.Ref`, `SearchHit.Ref`, `RecallHit.Ref`, `Backlink.Ref` (for a document source), `GraphNode.Ref` (for a document) and project event refs are now addresses.

- [ ] **Step 1: Write the failing tests**

`internal/core/address_test.go`:

```go
package core

import "testing"

func TestDocAddress(t *testing.T) {
	if got := DocAddress("XPSCTL", false, "design"); got != "/XPSCTL/knowledge/design" {
		t.Errorf("project entry = %q", got)
	}
	if got := DocAddress("XPSCTL", true, "design"); got != "/GLOBAL/knowledge/design" {
		t.Errorf("vault entry = %q", got)
	}
}

// The SQL fragment and the Go helper must never disagree: search results are
// compared with and passed back to commands that use the Go form.
func TestDocAddressSQLMatchesGo(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Local entry"}); err != nil {
		t.Fatal(err)
	}
	shared, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Vault entry"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, p.ID, shared.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Slug   string `db:"slug"`
		Global bool   `db:"global"`
		Ref    string `db:"ref"`
	}
	if err := c.db.Select(&rows, `SELECT k.slug, k.global, `+docAddressSQL+` AS ref
		FROM knowledge k JOIN project p ON p.id = k.project_id ORDER BY k.slug`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	for _, r := range rows {
		if want := DocAddress(p.Key, r.Global, r.Slug); r.Ref != want {
			t.Errorf("SQL address %q, Go address %q", r.Ref, want)
		}
	}
}

func TestSearchRecallAndVectorHitsCarryAddresses(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Lease renewal", Body: "A claim starts the lease.\n"})
	if err != nil {
		t.Fatal(err)
	}
	const want = "/XPSCTL/knowledge/lease-renewal"
	if doc.Ref != want {
		t.Errorf("created ref = %q, want %q", doc.Ref, want)
	}

	hits, err := c.Search(ctx, p.ID, "lease", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	recalled, err := c.Recall(ctx, p.ID, "why did the lease expire", RecallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, h := range hits {
		if h.Kind == "knowledge" {
			refs = append(refs, h.Ref)
		}
	}
	for _, h := range recalled {
		if h.Kind == "knowledge" {
			refs = append(refs, h.Ref)
		}
	}
	hit, err := c.KnowledgeHit(ctx, doc.ID, p.ID, false, "")
	if err != nil {
		t.Fatal(err)
	}
	refs = append(refs, hit.Ref)
	if len(refs) != 3 {
		t.Fatalf("refs = %v, want one from search, recall and the vector lookup", refs)
	}
	for _, ref := range refs {
		if ref != want {
			t.Errorf("ref = %q, want %q", ref, want)
		}
	}
}

// A vault entry that links somewhere is named by its vault address in that
// target's backlinks, not by the key of the project it came from.
func TestBacklinksNameAVaultSourceByItsVaultAddress(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	target, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Source", Body: "See [[target]].\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, p.ID, source.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	back, err := c.Backlinks(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Ref != "/GLOBAL/knowledge/source" {
		t.Errorf("backlinks = %+v, want the vault address", back)
	}
}

func TestGraphNamesEntriesByAddress(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Target"}); err != nil {
		t.Fatal(err)
	}
	source, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Source", Body: "See [[target]].\n"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := c.Traverse(ctx, source.ID, 1, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		seen[n.Ref] = true
	}
	if !seen["/XPSCTL/knowledge/source"] || !seen["/XPSCTL/knowledge/target"] {
		t.Errorf("nodes = %+v", g.Nodes)
	}
}
```

Update the existing assertions to the new form:

```bash
sed -i 's#"XPSCTL/#"/XPSCTL/knowledge/#g; s#want XPSCTL/#want /XPSCTL/knowledge/#g' \
  internal/core/knowledge_test.go internal/core/recall_test.go
sed -i 's#"GLOBAL/postgres-conventions"#"/GLOBAL/knowledge/postgres-conventions"#' internal/core/pin_test.go
sed -i 's#event.Ref == "EVT/"+doc.Slug#event.Ref == "/EVT/knowledge/"+doc.Slug#' internal/ui/server_test.go
grep -n 'XPSCTL/\|GLOBAL/\|EVT/' internal/core/knowledge_test.go internal/core/recall_test.go internal/core/pin_test.go internal/ui/server_test.go
```

Expected output of the `grep`: every match reads `/XPSCTL/knowledge/`, `/GLOBAL/knowledge/` or `/EVT/knowledge/`. `server_test.go` may also show a `"/tmp/"` path; that is not a ref.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core/ ./internal/ui/`
Expected: FAIL. The build fails because `DocAddress`, `docAddressSQL` and `KnowledgeHit` are undefined.

- [ ] **Step 3: Write `internal/core/address.go`**

```go
package core

import "github.com/mtch3n/trellis/internal/vpath"

// DocAddress is a knowledge entry's canonical address: /KEY/knowledge/<slug>,
// or /GLOBAL/knowledge/<slug> once it is in the vault. key is ignored for a
// vault entry.
func DocAddress(key string, global bool, slug string) string {
	if global {
		return vpath.GlobalKnowledgePath(slug).String()
	}
	return vpath.KnowledgePath(key, slug).String()
}

// docAddressSQL is DocAddress in SQL, for queries that alias knowledge as k
// and project as p. TestDocAddressSQLMatchesGo holds the two together.
const docAddressSQL = `'/' || CASE WHEN k.global = 1 THEN '` + vpath.GlobalKey +
	`' ELSE p.key END || '/knowledge/' || k.slug`
```

- [ ] **Step 4: Use it everywhere a document ref is built**

In `internal/core/knowledge.go`:

1. Replace the whole comment block above `const GlobalKey`, including any paragraph the pin-only plan left there, with:

```go
// GlobalKey names the global vault. vpath owns the reservation: no project can
// take the key, so /GLOBAL/knowledge/<slug> never collides with a project.
```

2. In `Knowledge`, change the comment on `Ref` from `// KEY/slug` to `// /KEY/knowledge/<slug> or /GLOBAL/knowledge/<slug>`.
3. In `docView`, replace `doc.Ref = key + "/" + doc.Slug` with:

```go
	doc.Ref = DocAddress(key, doc.Global, doc.Slug)
```

In `internal/core/search.go` (`Search` and `matchKnowledge`) and in `internal/core/recall.go` (`Recall`, the `docs` query), each document query has this line:

```
			       CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END || '/' || k.slug AS ref,
```

Replace it in all three queries with:

```
			       `+docAddressSQL+` AS ref,
```

The raw string closes before the fragment and reopens after it. Leave the separate `... END AS project` line alone.

Append `KnowledgeHit` to `internal/core/search.go`:

```go
// KnowledgeHit is the search hit for one entry, found by id: how a vector
// match becomes a result. Unless allProjects is set, the entry must belong to
// projectID or the vault. A non-empty label must be on the entry. A miss is
// sql.ErrNoRows.
func (c *Core) KnowledgeHit(ctx context.Context, docID, projectID string, allProjects bool, label string) (SearchHit, error) {
	q := `SELECT 'knowledge' AS kind, ` + docAddressSQL + ` AS ref, k.title,
	             CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END AS project,
	             k.doc_type AS detail, 0 AS unreviewed
	      FROM knowledge k JOIN project p ON p.id = k.project_id
	      WHERE k.id = ?`
	args := []any{docID}
	if !allProjects {
		q += ` AND (k.project_id = ? OR k.global = 1)`
		args = append(args, projectID)
	}
	if label != "" {
		q += ` AND EXISTS (SELECT 1 FROM knowledge_label kl JOIN label l ON l.id = kl.label_id
		                   WHERE kl.doc_id = k.id AND l.name = ?)`
		args = append(args, label)
	}
	var hit SearchHit
	err := c.db.GetContext(ctx, &hit, q, args...)
	return hit, err
}
```

In `internal/core/doc_relations.go`, `Backlinks`, replace the first `SELECT` line of the query:

```go
			`SELECT 'doc' AS from_type, `+docAddressSQL+` AS ref, k.title,
```

The card half of the `UNION` is unchanged.

In `internal/core/graph.go`, in the document branch of `Traverse`, replace `node.Type, node.Ref, node.Title = "doc", key+"/"+doc.Slug, doc.Title` with:

```go
					node.Type, node.Ref, node.Title = "doc", DocAddress(key, doc.Global, doc.Slug), doc.Title
```

In `internal/core/dupes.go`, replace `slug := h.Ref[strings.Index(h.Ref, "/")+1:]` with:

```go
			addr, err := vpath.Parse(h.Ref)
			if err != nil {
				return nil, err
			}
			slug := addr.Name
```

Also add `"github.com/mtch3n/trellis/internal/vpath"` to `dupes.go`'s imports.

In `internal/retrieval/service.go`, the loop `for _, match := range matches {` builds a query by hand. Replace the whole loop body with:

```go
		for _, match := range matches {
			hit, err := s.core.KnowledgeHit(ctx, match.ID, projectID, opts.AllProjects, opts.Label)
			if err != nil {
				continue
			}
			if !seen[hit.Ref] {
				out = append(out, hit)
				seen[hit.Ref] = true
			}
		}
```

In `internal/cli/search.go`:
- Delete `vectorSearchHits`, `mergeSearchHits` and the package-level `min`. None of them has a caller (`grep -rn 'vectorSearchHits\|mergeSearchHits' internal` shows only their definitions), and the built-in `min` covers any other use.
- Remove the imports this leaves unused: `context` and the `vecsearch` alias. `go build ./internal/cli/` names them.

In `internal/ui/server.go`, `handleProjectEvents`, replace the `case row.Slug != nil:` branch with:

```go
		case row.Slug != nil:
			event.Ref = core.DocAddress(p.Key, row.Global != nil && *row.Global, *row.Slug)
```

In `handleGlobalKnowledgeList`, fill the refs after the `SelectContext` and before `writeJSON`:

```go
	for i := range docs {
		docs[i].Ref = core.DocAddress("", true, docs[i].Slug)
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w internal && go build ./... && go test ./internal/core/ ./internal/ui/ ./internal/retrieval/ ./internal/cli/ && go vet ./...`
Expected: `ok` for all four packages.

- [ ] **Step 6: Commit**

```bash
git add internal/core internal/retrieval internal/cli/search.go internal/ui
git commit -m "feat(core): name knowledge entries by address in every output

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---
### Task 3: Knowledge arguments take addresses

**Files:**
- Modify: `internal/core/address.go`
- Modify: `internal/core/knowledge.go`: `loadDoc`, `DeleteKnowledge`
- Modify: `internal/core/pin.go`: `DemoteKnowledge`, `VerifyKnowledge`
- Test: `internal/core/knowledge_address_test.go`

**Interfaces:**
- Consumes: `vpath.Parse`, `vpath.SplitAnchor`, `vpath.GlobalKey`, the collection constants (Task 1).
- Produces, all in `address.go`:
  - `func ParseAddress(arg, collection string) (vpath.Path, error)`, which returns `bad_path` (exit 2) or `wrong_collection` (exit 2)
  - `func wrongProject(arg string, p vpath.Path, current string) error`, returning `wrong_project` (exit 2)
  - `func commandFor(p vpath.Path, arg string) string`
  - `func projectKeyOf(tx *sqlx.Tx, projectID string) (string, error)`, which returns `""` for an empty id
  - `type docScope int`, with the values `docRelative`, `docOwn` and `docVault`
  - `type docArg struct{ slug string; scope docScope }`
  - `func readDocArg(arg, projectKey string) (docArg, error)`
  - `func vaultSlug(arg string) (string, error)`, whose error is `not_global` (exit 2)
- Behavior:
  - `LoadKnowledge`, `ReadKnowledge`, `EditKnowledge(Fields)`, `PinKnowledge`, `UnpinKnowledge`, `NominateKnowledge`, `EscalateKnowledge` and `DeleteKnowledge` accept a relative slug, `/<their project>/knowledge/<slug>` or `/GLOBAL/knowledge/<slug>`.
  - An empty `projectID` reads the vault only.
  - `DemoteKnowledge` and `VerifyKnowledge` accept a slug or a vault address.

- [ ] **Step 1: Write the failing tests**

`internal/core/knowledge_address_test.go`:

```go
package core

import (
	"errors"
	"strings"
	"testing"
)

func TestKnowledgeTakesItsOwnAddress(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Concurrency model", Body: "## Leases\n"})
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{doc.Ref, doc.Ref + "#leases", doc.Slug} {
		got, err := c.ReadKnowledge(ctx, p.ID, arg)
		if err != nil || got.ID != doc.ID {
			t.Errorf("ReadKnowledge(%q) = %s, %v", arg, got.ID, err)
		}
	}
	edited, err := c.EditKnowledge(ctx, p.ID, doc.Ref, "Rewritten.\n", nil)
	if err != nil || edited.ID != doc.ID {
		t.Fatalf("EditKnowledge by address = %s, %v", edited.ID, err)
	}
	if err := c.DeleteKnowledge(ctx, p.ID, doc.Ref); err != nil {
		t.Fatalf("DeleteKnowledge by address: %v", err)
	}
	_, err = c.ReadKnowledge(ctx, p.ID, doc.Slug)
	if got := errCode(t, err); got != "knowledge_not_found" {
		t.Errorf("after delete: code = %s", got)
	}
}

// The round trip the spec calls broken: every ref search and recall print must
// open with the command that shows it.
func TestSearchAndRecallRefsOpen(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Lease renewal"}); err != nil {
		t.Fatal(err)
	}
	shared, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Lease expiry"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, p.ID, shared.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	hits, err := c.Search(ctx, p.ID, "lease", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	recalled, err := c.Recall(ctx, p.ID, "lease", RecallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, h := range hits {
		refs = append(refs, h.Ref)
	}
	for _, h := range recalled {
		refs = append(refs, h.Ref)
	}
	if len(refs) != 4 {
		t.Fatalf("refs = %v, want both entries from search and from recall", refs)
	}
	for _, ref := range refs {
		if _, err := c.ReadKnowledge(ctx, p.ID, ref); err != nil {
			t.Errorf("%s does not open: %v", ref, err)
		}
	}
}

func TestAnotherProjectsAddressIsWrongProject(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	doc, err := c.CreateKnowledge(ctx, other.ID, NewKnowledge{Title: "Runbook"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ReadKnowledge(ctx, p.ID, doc.Ref)
	if got := errCode(t, err); got != "wrong_project" {
		t.Errorf("code = %s, want wrong_project", got)
	}
	if _, err := c.ReadKnowledge(ctx, other.ID, doc.Ref); err != nil {
		t.Errorf("in its own project: %v", err)
	}
}

func TestMisdirectedAndMalformedAddresses(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	_, err := c.ReadKnowledge(ctx, p.ID, "/XPSCTL/cards/XPSCTL-1")
	if got := errCode(t, err); got != "wrong_collection" {
		t.Fatalf("code = %s, want wrong_collection", got)
	}
	if te, _ := errors.AsType[*Error](err); !strings.Contains(te.Fix, "trellis card show /XPSCTL/cards/XPSCTL-1") {
		t.Errorf("fix = %q, want the command that takes a card", te.Fix)
	}
	_, err = c.ReadKnowledge(ctx, p.ID, "/xps_ctl/knowledge/x")
	if got := errCode(t, err); got != "bad_path" {
		t.Errorf("code = %s, want bad_path", got)
	}
}

// /KEY/knowledge/<slug> names KEY's own entry, which an escalated one no
// longer is. A relative slug still finds it, through the vault.
func TestOwnAddressSkipsTheVault(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, p.ID, doc.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	_, err = c.ReadKnowledge(ctx, p.ID, "/XPSCTL/knowledge/conventions")
	if got := errCode(t, err); got != "knowledge_not_found" {
		t.Errorf("own address of an escalated entry: code = %s", got)
	}
	for _, arg := range []string{"conventions", "/GLOBAL/knowledge/conventions"} {
		if _, err := c.ReadKnowledge(ctx, p.ID, arg); err != nil {
			t.Errorf("ReadKnowledge(%q): %v", arg, err)
		}
	}
}

func TestVaultEntriesNeedNoProject(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, p.ID, doc.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Local only"}); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"conventions", "/GLOBAL/knowledge/conventions"} {
		got, err := c.ReadKnowledge(ctx, "", arg)
		if err != nil || got.ID != doc.ID {
			t.Errorf("ReadKnowledge(no project, %q) = %s, %v", arg, got.ID, err)
		}
	}
	_, err = c.ReadKnowledge(ctx, "", "local-only")
	if got := errCode(t, err); got != "knowledge_not_found" {
		t.Errorf("a project entry with no project: code = %s", got)
	}

	if err := c.VerifyKnowledge(ctx, "/GLOBAL/knowledge/conventions"); err != nil {
		t.Errorf("VerifyKnowledge by address: %v", err)
	}
	err = c.VerifyKnowledge(ctx, "/XPSCTL/knowledge/local-only")
	if got := errCode(t, err); got != "not_global" {
		t.Errorf("verify a project address: code = %s", got)
	}
	moved, err := c.DemoteKnowledge(ctx, "/GLOBAL/knowledge/conventions", "back")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Ref != "/XPSCTL/knowledge/conventions" {
		t.Errorf("demoted ref = %q", moved.Ref)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core/ -run 'TakesItsOwnAddress|RefsOpen|WrongProject|Misdirected|SkipsTheVault|NeedNoProject'`
Expected: FAIL. `ReadKnowledge(doc.Ref)` returns `knowledge_not_found`, because `loadDoc` slugifies the address.

- [ ] **Step 3: Add the argument readers to `internal/core/address.go`**

Change the import block to:

```go
import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/vpath"
)
```

Append:

```go
// ParseAddress reads an absolute address that must name something in
// collection. A malformed address is bad_path. A well-formed one in another
// collection is wrong_collection, and its fix names the command that takes it.
func ParseAddress(arg, collection string) (vpath.Path, error) {
	p, err := vpath.Parse(arg)
	if err != nil {
		return vpath.Path{}, ErrUsage("bad_path", err.Error(),
			"trellis search <words>   # results carry valid addresses")
	}
	if p.Collection != collection {
		return vpath.Path{}, ErrUsage("wrong_collection",
			fmt.Sprintf("%s names %s, not %s", arg, collectionNoun(p.Collection), collectionNoun(collection)),
			commandFor(p, arg))
	}
	return p, nil
}

func collectionNoun(collection string) string {
	switch collection {
	case vpath.CollectionBoards:
		return "a board"
	case vpath.CollectionCards:
		return "a card"
	case vpath.CollectionKnowledge:
		return "a knowledge entry"
	case vpath.CollectionArtifacts:
		return "an artifact"
	}
	return "a project"
}

// commandFor is the command that takes an address of p's kind.
func commandFor(p vpath.Path, arg string) string {
	switch p.Collection {
	case vpath.CollectionBoards:
		return "trellis board show --board " + arg
	case vpath.CollectionCards:
		return "trellis card show " + arg
	case vpath.CollectionKnowledge:
		return "trellis knowledge show " + arg
	case vpath.CollectionArtifacts:
		return "trellis artifact ls --project " + p.Project
	}
	return "trellis card ls --project " + arg
}

// wrongProject refuses an address naming a project other than the one a
// call acts in. The CLI routes an address to its own project before core sees
// it, so this fires for a caller that fixed the project first: --project, or
// a project-scoped HTTP route.
func wrongProject(arg string, p vpath.Path, current string) error {
	if current == "" {
		current = "no project"
	}
	return ErrUsage("wrong_project",
		fmt.Sprintf("%s is in project %s, and this call acts in %s", arg, p.Project, current),
		commandFor(p, arg))
}

// projectKeyOf is projectID's key, or "" when projectID is "".
func projectKeyOf(tx *sqlx.Tx, projectID string) (string, error) {
	if projectID == "" {
		return "", nil
	}
	var key string
	err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID)
	return key, err
}

// docScope is where a knowledge argument looks.
type docScope int

const (
	docRelative docScope = iota // the project's own entries first, then the vault
	docOwn                      // the project's own entry, not escalated
	docVault                    // the vault
)

// docArg is a knowledge argument read against the project it is used in.
type docArg struct {
	slug  string
	scope docScope
}

// readDocArg reads a knowledge argument. A relative one is slugified, as it
// always was; an anchor is dropped, because a lookup names the entry. An
// absolute one must name projectKey's entries or the vault.
func readDocArg(arg, projectKey string) (docArg, error) {
	target, _ := vpath.SplitAnchor(strings.TrimSpace(arg))
	target = strings.TrimSpace(target)
	if !strings.HasPrefix(target, "/") {
		return docArg{slug: Slugify(target), scope: docRelative}, nil
	}
	p, err := ParseAddress(target, vpath.CollectionKnowledge)
	if err != nil {
		return docArg{}, err
	}
	switch p.Project {
	case vpath.GlobalKey:
		return docArg{slug: p.Name, scope: docVault}, nil
	case projectKey:
		return docArg{slug: p.Name, scope: docOwn}, nil
	}
	return docArg{}, wrongProject(arg, p, projectKey)
}

// vaultSlug reads the argument of a command that acts only on the vault: a
// bare slug, or a /GLOBAL/knowledge address.
func vaultSlug(arg string) (string, error) {
	target, _ := vpath.SplitAnchor(strings.TrimSpace(arg))
	target = strings.TrimSpace(target)
	if !strings.HasPrefix(target, "/") {
		return Slugify(target), nil
	}
	p, err := ParseAddress(target, vpath.CollectionKnowledge)
	if err != nil {
		return "", err
	}
	if p.Project != vpath.GlobalKey {
		return "", ErrUsage("not_global", arg+" is a project entry, not a vault entry",
			"trellis knowledge show "+arg)
	}
	return p.Name, nil
}
```

- [ ] **Step 4: Route lookups through `readDocArg`**

In `internal/core/knowledge.go`, replace `loadDoc` with:

```go
// loadDoc resolves a knowledge argument within a project: a relative slug
// finds the project's entry and then the vault's, an address finds exactly
// what it names. With no project, only the vault is searched.
func (c *Core) loadDoc(tx *sqlx.Tx, projectID, arg string, out *Knowledge) error {
	key, err := projectKeyOf(tx, projectID)
	if err != nil {
		return err
	}
	d, err := readDocArg(arg, key)
	if err != nil {
		return err
	}
	vaultOnly := d.scope == docVault || projectID == ""
	switch {
	case d.scope == docOwn:
		err = tx.Get(out, `SELECT * FROM knowledge WHERE slug = ? AND project_id = ? AND global = 0`,
			d.slug, projectID)
	case vaultOnly:
		err = tx.Get(out, `SELECT * FROM knowledge WHERE slug = ? AND global = 1`, d.slug)
	default:
		err = tx.Get(out,
			`SELECT * FROM knowledge WHERE slug = ? AND (project_id = ? OR global = 1) ORDER BY global LIMIT 1`,
			d.slug, projectID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		where := "in this project"
		if vaultOnly {
			where = "in the vault"
		}
		return ErrNotFound("knowledge_not_found", "no knowledge entry "+arg+" "+where,
			"trellis knowledge ls")
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

In `DeleteKnowledge`, replace the `tx.Get(&doc, ...)` block at the top of the transaction with:

```go
		var doc Knowledge
		key, err := projectKeyOf(tx, projectID)
		if err != nil {
			return err
		}
		d, err := readDocArg(slug, key)
		if err != nil {
			return err
		}
		// Only an entry this project owns is deleted, escalated ones included.
		q := `SELECT * FROM knowledge WHERE project_id = ? AND slug = ?`
		switch d.scope {
		case docOwn:
			q += ` AND global = 0`
		case docVault:
			q += ` AND global = 1`
		}
		if err := tx.Get(&doc, q, projectID, d.slug); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("knowledge_not_found", "no knowledge entry "+slug+" owned by this project",
					"trellis knowledge ls")
			}
			return err
		}
		staged, err = stageRemoval(doc.Path)
		if err != nil {
			return err
		}
```

This replaces the old `var err error` line and the `staged, err = stageRemoval(doc.Path)` block that followed it; the rest of the function is unchanged.

In `internal/core/pin.go`:
- In `DemoteKnowledge`, the transaction starts with `err := tx.Get(&doc, ..., Slugify(slug))`. Replace that line with:

```go
		name, err := vaultSlug(slug)
		if err != nil {
			return err
		}
		err = tx.Get(&doc, `SELECT * FROM knowledge WHERE slug = ? AND global = 1`, name)
```

- Make the same change in `VerifyKnowledge`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w internal/core && go test ./internal/core/ && go vet ./internal/core/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/core
git commit -m "feat(core): read a knowledge entry by its address, and refuse another project's

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---
### Task 4: Wikilinks and card links cross projects

**Files:**
- Modify: `internal/core/markdown.go`: `Reference`, `ParseWikilinks`, `ParseReference`
- Modify: `internal/core/doc_relations.go`: `resolveDocRef`, `LinkCardToDoc`
- Modify: `internal/core/lint.go`
- Modify test: `internal/core/markdown_test.go`
- Test: `internal/core/wikilink_address_test.go`

**Interfaces:**
- Consumes: `vpath.Parse`, `vpath.SplitAnchor` (Task 1); `ParseAddress`, `commandFor` (Task 3).
- Produces:
  - **`Reference`:** the fields are unchanged. `ProjectKey` is `""` for a relative target, `"GLOBAL"` for the vault, and otherwise the key an address names. An absolute target that names no knowledge entry keeps its text as `Slug`, so it can never match a row.
  - **`resolveDocRef`**: a relative target resolves in the source's project first, then in the vault, the same order `loadDoc` uses.
  - **`LintFinding.Kind`** gains `wrong_collection` and `bad_path`. `LintFinding.Doc` is now the holding entry's address.
  - **Lint** checks an anchor against the entry the link resolved to, wherever it lives. `type linkTarget struct{ ref string; anchors map[string]bool }`, `type linkTargets map[string]linkTarget` with `func (t linkTargets) get(tx *sqlx.Tx, id string) (linkTarget, bool, error)`, and `func anchorSet(body string) map[string]bool`. `linkFinding` now takes the transaction and the cache and returns an error.
  - **`LinkCardToDoc`** accepts an address in another project. A non-knowledge address is `wrong_collection`.

- [ ] **Step 1: Write the failing tests**

In `internal/core/markdown_test.go`, replace `TestParseWikilinksFormsAndSkips`, and add `TestParseReference`:

```go
func TestParseWikilinksFormsAndSkips(t *testing.T) {
	body := "See [[design]] and [[/XPSCTL/knowledge/concurrency-model#parallel safety]].\n" +
		"Alias [[design|the design]] is the same target.\n" +
		"```\nnot a [[link]] in code\n```\n" +
		"Nor `[[inline]]`.\n" +
		"A card is no entry: [[/XPSCTL/cards/XPSCTL-1]]. The old form: [[XPSCTL/design]].\n"
	got := ParseWikilinks(body)
	want := []Reference{
		{Raw: "design", Slug: "design"},
		{Raw: "/XPSCTL/knowledge/concurrency-model#parallel safety", ProjectKey: "XPSCTL",
			Slug: "concurrency-model", Anchor: "parallel-safety"},
		{Raw: "/XPSCTL/cards/XPSCTL-1", Slug: "/XPSCTL/cards/XPSCTL-1"},
		{Raw: "XPSCTL/design", Slug: "xpsctl-design"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseWikilinks =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseReference(t *testing.T) {
	cases := map[string]Reference{
		"design#Why Not":                  {Raw: "design#Why Not", Slug: "design", Anchor: "why-not"},
		"a/b":                             {Raw: "a/b", Slug: "a-b"},
		"/other/knowledge/runbook":        {Raw: "/other/knowledge/runbook", ProjectKey: "OTHER", Slug: "runbook"},
		"/GLOBAL/knowledge/conventions#x": {Raw: "/GLOBAL/knowledge/conventions#x", ProjectKey: "GLOBAL", Slug: "conventions", Anchor: "x"},
		"/bad_key/knowledge/x":            {Raw: "/bad_key/knowledge/x", Slug: "/bad_key/knowledge/x"},
	}
	for in, want := range cases {
		if got := ParseReference(in); got != want {
			t.Errorf("ParseReference(%q) = %+v, want %+v", in, got, want)
		}
	}
}
```

`internal/core/wikilink_address_test.go`:

```go
package core

import (
	"strings"
	"testing"
)

func lintKinds(t *testing.T, c *Core, projectID string) (map[string]int, []LintFinding) {
	t.Helper()
	findings, err := c.Lint(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, f := range findings {
		kinds[f.Kind]++
	}
	return kinds, findings
}

func TestWikilinkResolvesInAnotherProject(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	target, err := c.CreateKnowledge(ctx, other.ID, NewKnowledge{Title: "Runbook"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{
		Title: "Setup", Body: "Follow [[/OTHERPROJ/knowledge/runbook]].\n"}); err != nil {
		t.Fatal(err)
	}
	back, err := c.Backlinks(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Ref != "/XPSCTL/knowledge/setup" {
		t.Errorf("backlinks = %+v", back)
	}
	if _, findings := lintKinds(t, c, p.ID); len(findings) != 0 {
		t.Errorf("findings = %+v, want none", findings)
	}
}

func TestCrossProjectStubIsBackfilled(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{
		Title: "Setup", Body: "Follow [[/OTHERPROJ/knowledge/later]].\n"}); err != nil {
		t.Fatal(err)
	}
	_, findings := lintKinds(t, c, p.ID)
	if len(findings) != 1 || findings[0].Kind != "stub" ||
		!strings.Contains(findings[0].Fix, "trellis --project OTHERPROJ knowledge new") {
		t.Fatalf("findings = %+v, want one stub pointing at OTHERPROJ", findings)
	}
	later, err := c.CreateKnowledge(ctx, other.ID, NewKnowledge{Title: "Later"})
	if err != nil {
		t.Fatal(err)
	}
	if back, _ := c.Backlinks(ctx, later.ID); len(back) != 1 {
		t.Errorf("backlinks after the target was written = %+v", back)
	}
	if kinds, _ := lintKinds(t, c, p.ID); kinds["stub"] != 0 {
		t.Errorf("the stub was not backfilled: %v", kinds)
	}
}

func TestLinkToAProjectThatDoesNotExistIsAStub(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Setup", Body: "See [[/NOPE/knowledge/x]].\n"}); err != nil {
		t.Fatalf("writing a link to a missing project must not fail: %v", err)
	}
	_, findings := lintKinds(t, c, p.ID)
	if len(findings) != 1 || findings[0].Kind != "stub" || findings[0].Ref != "/NOPE/knowledge/x" {
		t.Errorf("findings = %+v", findings)
	}
	if findings[0].Doc != "/XPSCTL/knowledge/setup" {
		t.Errorf("finding names its entry as %q, want its address", findings[0].Doc)
	}
}

// A relative link means this project, then the vault, as a relative
// argument does.
func TestARelativeLinkFallsBackToTheVault(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	shared, err := c.CreateKnowledge(ctx, other.ID, NewKnowledge{Title: "Conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, other.ID, shared.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{
		Title: "Setup", Body: "Follow [[conventions]].\n"}); err != nil {
		t.Fatal(err)
	}
	back, err := c.Backlinks(ctx, shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Ref != "/XPSCTL/knowledge/setup" {
		t.Errorf("vault backlinks = %+v", back)
	}
	if kinds, findings := lintKinds(t, c, p.ID); kinds["stub"] != 0 {
		t.Errorf("findings = %+v, want the link resolved", findings)
	}

	// The project's own entry still wins over the vault's.
	own, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{
		Title: "Onboarding", Body: "Read [[conventions]].\n"}); err != nil {
		t.Fatal(err)
	}
	back, err = c.Backlinks(ctx, own.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Ref != "/XPSCTL/knowledge/onboarding" {
		t.Errorf("own backlinks = %+v, want the project entry to win", back)
	}
}

func TestTheOldQualifiedFormIsARelativeStub(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Design"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{
		Title: "Setup", Body: "See [[XPSCTL/design]].\n"}); err != nil {
		t.Fatal(err)
	}
	_, findings := lintKinds(t, c, p.ID)
	var stubs []string
	for _, f := range findings {
		if f.Kind == "stub" {
			stubs = append(stubs, f.Ref)
		}
	}
	if len(stubs) != 1 || stubs[0] != "XPSCTL/design" {
		t.Errorf("stubs = %v, want the old form reported", stubs)
	}
}

func TestLintNamesAddressesThatNameNoEntry(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Setup", Body: "A card: [[/XPSCTL/cards/XPSCTL-1]]. Broken: [[/xps_ctl/knowledge/x]].\n"}); err != nil {
		t.Fatal(err)
	}
	kinds, findings := lintKinds(t, c, p.ID)
	if kinds["wrong_collection"] != 1 || kinds["bad_path"] != 1 || kinds["stub"] != 0 {
		t.Errorf("findings = %+v", findings)
	}
}

// An anchor is checked on the entry the link resolved to. Checking by slug
// compared another project's entry with this project's namesake.
func TestAnchorsAreCheckedOnTheLinkedEntry(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Runbook", Body: "## Steps\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(ctx, other.ID, NewKnowledge{Title: "Runbook", Body: "## Rollback\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Setup",
		Body: "Local [[runbook#steps]], remote [[/OTHERPROJ/knowledge/runbook#rollback]].\n"}); err != nil {
		t.Fatal(err)
	}
	if kinds, findings := lintKinds(t, c, p.ID); kinds["broken_anchor"] != 0 {
		t.Errorf("findings = %+v", findings)
	}
}

// A missing heading is found in a target this project does not own: lint
// reads that entry's file rather than trusting its own listing.
func TestMissingHeadingsInForeignAndVaultTargets(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	if _, err := c.CreateKnowledge(ctx, other.ID, NewKnowledge{Title: "Runbook", Body: "## Rollback\n"}); err != nil {
		t.Fatal(err)
	}
	shared, err := c.CreateKnowledge(ctx, other.ID, NewKnowledge{Title: "Conventions", Body: "## Naming\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, other.ID, shared.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Setup", Body: "" +
		"Foreign [[/OTHERPROJ/knowledge/runbook#rollback]] and [[/OTHERPROJ/knowledge/runbook#nope]].\n" +
		"Vault [[/GLOBAL/knowledge/conventions#naming]] and [[conventions#missing]].\n"}); err != nil {
		t.Fatal(err)
	}
	_, findings := lintKinds(t, c, p.ID)
	broken := map[string]string{}
	for _, f := range findings {
		if f.Kind == "broken_anchor" {
			broken[f.Ref] = f.Fix
		}
	}
	if len(broken) != 2 {
		t.Fatalf("broken anchors = %v, want two; findings: %+v", broken, findings)
	}
	if fix := broken["/OTHERPROJ/knowledge/runbook#nope"]; !strings.Contains(fix, "trellis knowledge show /OTHERPROJ/knowledge/runbook") {
		t.Errorf("foreign fix = %q", fix)
	}
	if fix := broken["conventions#missing"]; !strings.Contains(fix, "trellis knowledge show /GLOBAL/knowledge/conventions") {
		t.Errorf("vault fix = %q", fix)
	}
}

func TestLinkCardToDocAcrossProjects(t *testing.T) {
	c, p, b := kbCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Roll back"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := c.CreateKnowledge(ctx, other.ID, NewKnowledge{Title: "Runbook", Body: "## Rollback\n"})
	if err != nil {
		t.Fatal(err)
	}
	ref := CardRef{Seq: card.Seq}
	if err := c.LinkCardToDoc(ctx, p.ID, ref, "/OTHERPROJ/knowledge/runbook#rollback"); err != nil {
		t.Fatalf("LinkCardToDoc: %v", err)
	}
	back, err := c.Backlinks(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].FromType != "card" || back[0].Ref != card.Ref || back[0].Anchor != "rollback" {
		t.Errorf("backlinks = %+v", back)
	}
	err = c.LinkCardToDoc(ctx, p.ID, ref, "/XPSCTL/cards/XPSCTL-1")
	if got := errCode(t, err); got != "wrong_collection" {
		t.Errorf("card address: code = %s", got)
	}
	err = c.LinkCardToDoc(ctx, p.ID, ref, "/OTHERPROJ/knowledge/missing")
	if got := errCode(t, err); got != "knowledge_not_found" {
		t.Errorf("missing entry: code = %s", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core/ -run 'Wikilink|ParseReference|Stub|FallsBackToTheVault|OldQualified|NameNoEntry|Anchors|MissingHeadings|LinkCardToDocAcross'`
Expected: FAIL. `ParseWikilinks` still splits on the first `/`, cross-project links stay stubs, and a relative link never reaches the vault.

- [ ] **Step 3: Rewrite the reference parser**

In `internal/core/markdown.go`, add `"github.com/mtch3n/trellis/internal/vpath"` to the imports. Then replace the `Reference` type, `ParseWikilinks` and `ParseReference` with:

```go
// Reference is one [[wikilink]] target, or a link target typed on the command
// line.
type Reference struct {
	Raw        string // the target as written, anchor included: "design", "/XPSCTL/knowledge/design#why"
	ProjectKey string // "" for a relative target, "GLOBAL" for the vault, else the key an address names
	Slug       string
	Anchor     string // heading slug, without the #
}

// ParseWikilinks finds every [[link]], [[link#anchor]] and
// [[/KEY/knowledge/link]] in a body. Code spans and fenced blocks are skipped:
// an agent pasting a snippet that happens to contain brackets is not making a
// reference.
func ParseWikilinks(body string) []Reference {
	clean := fenceRE.ReplaceAllString(body, "")
	seen := map[string]bool{}
	var refs []Reference
	for _, m := range wikiLinkRE.FindAllStringSubmatch(clean, -1) {
		ref := ParseReference(strings.TrimSpace(m[1]) + m[2])
		if ref.Slug == "" || seen[ref.Raw] {
			continue
		}
		seen[ref.Raw] = true
		refs = append(refs, ref)
	}
	return refs
}

// ParseReference reads one link target -- "slug", "slug#anchor",
// "/KEY/knowledge/slug#anchor" or "/GLOBAL/knowledge/slug" -- into a
// Reference. It is the inverse of Raw, so a link recovered from the database
// resolves exactly as it did when the body was parsed.
//
// A relative target is slugified whole: [[a/b]] is the slug a-b until
// knowledge paths give it a directory. An absolute target that names no
// knowledge entry -- a card address, or a malformed one -- keeps its text as
// the slug. No row can match that, so the link stays a stub and lint says why.
func ParseReference(raw string) Reference {
	raw = strings.TrimSpace(raw)
	target, anchor := vpath.SplitAnchor(raw)
	target = strings.TrimSpace(target)
	ref := Reference{Raw: raw, Anchor: Slugify(anchor)}
	if !strings.HasPrefix(target, "/") {
		ref.Slug = Slugify(target)
		return ref
	}
	p, err := vpath.Parse(target)
	if err != nil || p.Collection != vpath.CollectionKnowledge {
		ref.Slug = target
		return ref
	}
	ref.ProjectKey, ref.Slug = p.Project, p.Name
	return ref
}
```

- [ ] **Step 4: Resolve across projects**

In `internal/core/doc_relations.go`, add `"github.com/mtch3n/trellis/internal/vpath"` to the imports. Then replace `resolveDocRef` and its comment with:

```go
// resolveDocRef turns a reference into a doc id, or NULL for a stub. An
// unresolved link is listed by `knowledge lint`, never an error: writing a link
// to something not yet written is how a vault gets built.
//
// A relative reference resolves in the source's project first and in the
// vault second, the order loadDoc uses for a relative argument. An address
// resolves wherever it points, another project included: the link names its
// target exactly, and reading that project by name is already allowed. A
// project or entry that does not exist yet leaves a stub, which
// resolveDocStubs fills in when the entry is created or escalated.
func (c *Core) resolveDocRef(tx *sqlx.Tx, projectID string, ref Reference) (any, error) {
	var q string
	var args []any
	switch ref.ProjectKey {
	case "":
		q = `SELECT id FROM knowledge WHERE slug = ? AND (project_id = ? OR global = 1)
		     ORDER BY global LIMIT 1`
		args = []any{ref.Slug, projectID}
	case GlobalKey:
		q, args = `SELECT id FROM knowledge WHERE slug = ? AND global = 1`, []any{ref.Slug}
	default:
		q = `SELECT k.id FROM knowledge k JOIN project p ON p.id = k.project_id
		     WHERE k.slug = ? AND p.key = ? AND k.global = 0`
		args = []any{ref.Slug, ref.ProjectKey}
	}
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

Replace `LinkCardToDoc` with:

```go
// LinkCardToDoc is the structured card-to-doc relationship (§10.2):
//
//	trellis link XPSCTL-12 design#concurrency
//	trellis link XPSCTL-12 /OTHER/knowledge/runbook#rollback
//
// The target may be in another project; link rows carry no foreign key, and
// wikilinks cross projects too.
func (c *Core) LinkCardToDoc(ctx context.Context, projectID string, cardRef CardRef, target string) error {
	if t, _ := vpath.SplitAnchor(strings.TrimSpace(target)); strings.HasPrefix(strings.TrimSpace(t), "/") {
		if _, err := ParseAddress(strings.TrimSpace(t), vpath.CollectionKnowledge); err != nil {
			return err
		}
	}
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var card Card
		if err := c.loadCard(tx, projectID, cardRef, &card); err != nil {
			return err
		}
		ref := ParseReference(target)
		toID, err := c.resolveDocRef(tx, projectID, ref)
		if err != nil {
			return err
		}
		if toID == nil {
			return ErrNotFound("knowledge_not_found", "no knowledge entry "+ref.Raw,
				`trellis knowledge new --title "..."`)
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
			 VALUES ('card', ?, 'doc', ?, ?, ?, 'documents')`,
			card.ID, toID, ref.Raw, nullIfEmpty(ref.Anchor)); err != nil {
			return err
		}
		return c.recordEvent(tx, "card", card.ID, "linked", "documents", "", ref.Raw)
	})
}
```

- [ ] **Step 5: Rewrite lint's link checks**

Replace the whole of `internal/core/lint.go` with:

```go
package core

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/vpath"
)

// LintFinding is one problem with the vault. Lint reports; it never repairs.
type LintFinding struct {
	Kind string `json:"kind"` // stub, broken_anchor, orphan, wrong_collection, bad_path
	Doc  string `json:"doc"`  // the address of the entry that holds the problem
	Ref  string `json:"ref,omitempty"`
	Fix  string `json:"fix"`
}

// Lint reports stubs, broken anchors, orphans and link targets that name no
// entry (§10). Each finding names what to do about it; none of them is an
// error, because a vault under construction is full of them.
func (c *Core) Lint(ctx context.Context, projectID string) ([]LintFinding, error) {
	out := []LintFinding{}
	docs, err := c.ListKnowledge(ctx, projectID, KnowledgeFilter{})
	if err != nil {
		return nil, err
	}
	// Anchors are checked on the entry a link resolved to, by id, wherever it
	// lives. The project's own entries are in hand already; a target in
	// another project or in the vault is read from its file the first time a
	// link needs it.
	targets := linkTargets{}
	for _, d := range docs {
		targets[d.ID] = linkTarget{ref: d.Ref, anchors: anchorSet(d.BodyMD)}
	}

	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		for _, d := range docs {
			var rows []struct {
				ToRaw  string         `db:"to_raw"`
				ToID   sql.NullString `db:"to_id"`
				Anchor sql.NullString `db:"anchor"`
			}
			if err := tx.Select(&rows,
				`SELECT to_raw, to_id, anchor FROM link
				 WHERE from_type = 'doc' AND from_id = ? AND rel = 'wikilink' ORDER BY to_raw`, d.ID); err != nil {
				return err
			}
			for _, r := range rows {
				f, ok, err := linkFinding(tx, targets, d, r.ToRaw, r.ToID, r.Anchor)
				if err != nil {
					return err
				}
				if ok {
					out = append(out, f)
				}
			}

			var inbound int
			if err := tx.Get(&inbound,
				`SELECT COUNT(*) FROM link WHERE to_type = 'doc' AND to_id = ?`, d.ID); err != nil {
				return err
			}
			// Stubs count as outbound: an entry whose only link is broken is
			// reported as a stub, and reporting it as an orphan too would be
			// two findings for one fix.
			var outbound int
			if err := tx.Get(&outbound,
				`SELECT COUNT(*) FROM link WHERE from_type = 'doc' AND from_id = ?`,
				d.ID); err != nil {
				return err
			}
			if inbound == 0 && outbound == 0 {
				out = append(out, LintFinding{Kind: "orphan", Doc: d.Ref,
					Fix: "link it from a card or another entry, or remove it"})
			}
		}
		return nil
	})
	slices.SortStableFunc(out, func(a, b LintFinding) int { return cmp.Compare(a.Kind, b.Kind) })
	return out, err
}

// linkFinding judges one wikilink held by d.
func linkFinding(tx *sqlx.Tx, targets linkTargets, d Knowledge, raw string,
	toID, anchor sql.NullString) (LintFinding, bool, error) {
	ref := ParseReference(raw)
	if !toID.Valid {
		target, _ := vpath.SplitAnchor(raw)
		target = strings.TrimSpace(target)
		if strings.HasPrefix(target, "/") && ref.ProjectKey == "" {
			return addressFinding(d, raw, target), true, nil
		}
		return LintFinding{Kind: "stub", Doc: d.Ref, Ref: raw, Fix: stubFix(ref)}, true, nil
	}
	if !anchor.Valid || anchor.String == "" {
		return LintFinding{}, false, nil
	}
	target, found, err := targets.get(tx, toID.String)
	if err != nil || !found || target.anchors[anchor.String] {
		return LintFinding{}, false, err
	}
	return LintFinding{Kind: "broken_anchor", Doc: d.Ref, Ref: raw,
		Fix: "trellis knowledge show " + target.ref + "   # check its headings"}, true, nil
}

// linkTarget is what an anchor check needs from the entry a link resolved to.
type linkTarget struct {
	ref     string
	anchors map[string]bool
}

// linkTargets caches link targets by entry id.
type linkTargets map[string]linkTarget

// get returns the target with id, reading its file the first time. found is
// false when the row or its file has gone, which is not an anchor problem.
func (t linkTargets) get(tx *sqlx.Tx, id string) (linkTarget, bool, error) {
	if lt, ok := t[id]; ok {
		return lt, true, nil
	}
	var row struct {
		Path string `db:"path"`
		Ref  string `db:"ref"`
	}
	err := tx.Get(&row, `SELECT k.path, `+docAddressSQL+` AS ref
		FROM knowledge k JOIN project p ON p.id = k.project_id WHERE k.id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return linkTarget{}, false, nil
	}
	if err != nil {
		return linkTarget{}, false, err
	}
	raw, err := os.ReadFile(row.Path)
	if errors.Is(err, os.ErrNotExist) {
		return linkTarget{}, false, nil
	}
	if err != nil {
		return linkTarget{}, false, err
	}
	_, body, err := splitDocFile(row.Path, raw)
	if err != nil {
		return linkTarget{}, false, err
	}
	lt := linkTarget{ref: row.Ref, anchors: anchorSet(body)}
	t[id] = lt
	return lt, true, nil
}

func anchorSet(body string) map[string]bool {
	set := map[string]bool{}
	for _, a := range HeadingAnchors(body) {
		set[a] = true
	}
	return set
}

// addressFinding explains an absolute link target that names no knowledge
// entry: a malformed address, or one in another collection.
func addressFinding(d Knowledge, raw, target string) LintFinding {
	f := LintFinding{Kind: "wrong_collection", Doc: d.Ref, Ref: raw,
		Fix: "trellis knowledge edit " + d.Ref + " --body @file   # a wikilink names /KEY/knowledge/<slug>"}
	if _, err := vpath.Parse(target); err != nil {
		f.Kind = "bad_path"
	}
	return f
}

// stubFix says how to write the entry a stub is waiting for.
func stubFix(ref Reference) string {
	switch ref.ProjectKey {
	case "":
		return `trellis knowledge new --title "` + ref.Raw + `"`
	case GlobalKey:
		return `trellis knowledge new --title "` + ref.Slug + `"   # then a human escalates it`
	}
	return "trellis --project " + ref.ProjectKey + ` knowledge new --title "` + ref.Slug + `"`
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w internal/core && go test ./internal/core/ && go vet ./internal/core/`
Expected: `ok`. That includes the existing stub, anchor and orphan tests in `knowledge_test.go` and the backfill tests.

- [ ] **Step 7: Commit**

```bash
git add internal/core
git commit -m "feat(core): resolve wikilinks and card links across projects by address

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---
### Task 5: A qualified card ref is never re-scoped

**Files:**
- Modify: `internal/core/ids.go`: `CardRef.Project`, `ParseCardRef`, `CardRef.String`, new `CardRef.qualified`
- Modify: `internal/core/card.go`: `loadCard`, new `checkCardProject`
- Modify: `internal/core/blocker.go`: `BlockCard`, `UnblockCard`, new `crossProjectBlock`
- Modify: `internal/core/import.go`
- Test: `internal/core/card_ref_test.go`
- Modify test: `internal/core/ids_test.go`

**Interfaces:**
- Consumes: `vpath.Parse`, `vpath.CardPath`, `vpath.CollectionCards` (Task 1).
- Produces:
  - **`CardRef.Project string`**: the project an address names; `""` for shorthand (`12`, `KEY-12`, a UUID).
  - **`ParseCardRef`** also accepts `/KEY/cards/<REF>`. It sets `Project` to `KEY` and `Seq`/`ProjectKey` from the ref, so the address keeps its project all the way into core.
  - **`CardRef.String()`** renders an address as the address; `func (r CardRef) qualified() string` is the `PREFIX-N` part.
  - **`func (c *Core) checkCardProject(tx *sqlx.Tx, projectID string, ref CardRef) error`** is the single place a ref is held to its project. The merge layer replaces it.
    - An address whose `Project` differs from the current key is refused, and so is a `ProjectKey` prefix that differs.
    - When the project it names does not exist, the result is `card_not_found` (exit 3); otherwise `wrong_project` (exit 2).
  - **`func crossProjectBlock(err error, blocker CardRef) error`** turns `wrong_project` into `cross_project_block` (exit 2).

- [ ] **Step 1: Write the failing tests**

In `internal/core/ids_test.go`, add:

```go
func TestParseCardRefReadsACardAddress(t *testing.T) {
	cases := map[string]CardRef{
		"/xpsctl/cards/xpsctl-12": {Seq: 12, ProjectKey: "XPSCTL", Project: "XPSCTL"},
		// The address's project and the ref's prefix are kept apart: after a
		// merge they legitimately differ, and core decides what that means.
		"/MONO/cards/MY_APP-3": {Seq: 3, ProjectKey: "MY_APP", Project: "MONO"},
		"XPSCTL-12":            {Seq: 12, ProjectKey: "XPSCTL"},
	}
	for in, want := range cases {
		if got := ParseCardRef(in); got != want {
			t.Errorf("ParseCardRef(%q) = %+v, want %+v", in, got, want)
		}
	}
	for _, s := range []string{"/XPSCTL/knowledge/design", "/XPSCTL/cards/12", "/XPSCTL", "/XPSCTL/cards/XPSCTL-99999999999999999999"} {
		if got := ParseCardRef(s); got != (CardRef{}) {
			t.Errorf("ParseCardRef(%q) = %+v, want the empty ref", s, got)
		}
	}
	if got := ParseCardRef("/MONO/cards/MY_APP-3").String(); got != "/MONO/cards/MY_APP-3" {
		t.Errorf("String() of an address = %q", got)
	}
}
```

`internal/core/card_ref_test.go`:

```go
package core

import (
	"errors"
	"strings"
	"testing"
)

// twoProjectsWithCards gives XPSCTL and OTHERPROJ one card each, both seq 1.
func twoProjectsWithCards(t *testing.T) (*Core, Project, Board) {
	t.Helper()
	c := testCore(t)
	p := seededProject(t, c)
	pb := seededBoard(t, c, p)
	o := seededProject2(t, c)
	ob := seededBoard(t, c, o)
	if _, err := c.CreateCard(t.Context(), p.ID, pb.ID, NewCard{Title: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateCard(t.Context(), o.ID, ob.ID, NewCard{Title: "remote"}); err != nil {
		t.Fatal(err)
	}
	return c, p, pb
}

func TestAQualifiedRefIsNeverRescoped(t *testing.T) {
	c, p, _ := twoProjectsWithCards(t)
	ctx := t.Context()
	for _, ref := range []string{"1", "XPSCTL-1", "xpsctl-1", "/XPSCTL/cards/XPSCTL-1"} {
		card, err := c.GetCard(ctx, p.ID, ParseCardRef(ref))
		if err != nil || card.Ref != "XPSCTL-1" {
			t.Errorf("GetCard(%q) = %s, %v", ref, card.Ref, err)
		}
	}

	_, err := c.GetCard(ctx, p.ID, ParseCardRef("OTHERPROJ-1"))
	if got := errCode(t, err); got != "wrong_project" {
		t.Fatalf("OTHERPROJ-1 in XPSCTL: code = %s, want wrong_project", got)
	}
	if te, _ := errors.AsType[*Error](err); !strings.Contains(te.Fix, "/OTHERPROJ/cards/OTHERPROJ-1") {
		t.Errorf("fix = %q, want the card's address", te.Fix)
	}

	_, err = c.GetCard(ctx, p.ID, ParseCardRef("NOPE-1"))
	if got := errCode(t, err); got != "card_not_found" {
		t.Errorf("a project that does not exist: code = %s", got)
	}
}

// An address names its project even when its ref's prefix is the current
// key: /OTHERPROJ/cards/XPSCTL-1 is not XPSCTL's card 1.
func TestACardAddressKeepsItsProject(t *testing.T) {
	c, p, _ := twoProjectsWithCards(t)
	ctx := t.Context()
	_, err := c.GetCard(ctx, p.ID, ParseCardRef("/OTHERPROJ/cards/XPSCTL-1"))
	if got := errCode(t, err); got != "wrong_project" {
		t.Fatalf("code = %s, want wrong_project", got)
	}
	if te, _ := errors.AsType[*Error](err); !strings.Contains(te.Msg, "OTHERPROJ") {
		t.Errorf("message = %q, want it to name OTHERPROJ", te.Msg)
	}
	_, err = c.GetCard(ctx, p.ID, ParseCardRef("/NOPE/cards/XPSCTL-1"))
	if got := errCode(t, err); got != "card_not_found" {
		t.Errorf("an address to a missing project: code = %s", got)
	}
	// And the prefix check still holds inside the right project.
	_, err = c.GetCard(ctx, p.ID, ParseCardRef("/XPSCTL/cards/OTHERPROJ-1"))
	if got := errCode(t, err); got != "wrong_project" {
		t.Errorf("a foreign prefix under the right project: code = %s", got)
	}
}

func TestABlockerMustBeInTheSameProject(t *testing.T) {
	c, p, _ := twoProjectsWithCards(t)
	ctx := t.Context()
	err := c.BlockCard(ctx, p.ID, CardRef{Seq: 1}, ParseCardRef("OTHERPROJ-1"))
	if got := errCode(t, err); got != "cross_project_block" {
		t.Errorf("BlockCard: code = %s", got)
	}
	err = c.UnblockCard(ctx, p.ID, CardRef{Seq: 1}, ParseCardRef("OTHERPROJ-1"))
	if got := errCode(t, err); got != "cross_project_block" {
		t.Errorf("UnblockCard: code = %s", got)
	}
	// The address's project differs from the prefix, which is this project's.
	err = c.BlockCard(ctx, p.ID, CardRef{Seq: 1}, ParseCardRef("/OTHERPROJ/cards/XPSCTL-1"))
	if got := errCode(t, err); got != "cross_project_block" {
		t.Errorf("BlockCard by a foreign address: code = %s", got)
	}
}

func TestImportRefusesABlockerInAnotherProject(t *testing.T) {
	c, p, b := twoProjectsWithCards(t)
	ctx := t.Context()
	_, err := c.ImportCards(ctx, p.ID, b.ID, []ImportCard{{Title: "waits", BlockedBy: []string{"OTHERPROJ-1"}}})
	if got := errCode(t, err); got != "cross_project_block" {
		t.Errorf("foreign blocker: code = %s", got)
	}
	_, err = c.ImportCards(ctx, p.ID, b.ID, []ImportCard{{Title: "waits", BlockedBy: []string{"typo-1"}}})
	if got := errCode(t, err); got != "unknown_blocker" {
		t.Errorf("a handle that is no card: code = %s", got)
	}
	cards, err := c.ImportCards(ctx, p.ID, b.ID, []ImportCard{{Title: "waits", BlockedBy: []string{"/XPSCTL/cards/XPSCTL-1"}}})
	if err != nil || len(cards) != 1 {
		t.Errorf("a blocker given as an address: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core/ -run 'CardAddress|NeverRescoped|SameProject|BlockerInAnotherProject'`
Expected: FAIL. The build fails because `CardRef.Project` is undefined.

- [ ] **Step 3: Implement**

In `internal/core/ids.go`, add `"github.com/mtch3n/trellis/internal/vpath"` to the imports, and replace `CardRef`, `ParseCardRef` and `String` with:

```go
// CardRef is a parsed card reference. Exactly one of UUID or Seq is set.
type CardRef struct {
	UUID       string
	Seq        int64
	ProjectKey string // the ref's prefix, set when the reference was qualified
	Project    string // the project an address names; "" for shorthand
}

// ParseCardRef accepts "12", "XPSCTL-12", "/XPSCTL/cards/XPSCTL-12", or a
// uuid. An address keeps the project it names in Project, apart from the
// ref's prefix: the two differ once projects are merged, and deciding what
// that means is checkCardProject's job, not the parser's.
func ParseCardRef(s string) CardRef {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "/") {
		p, err := vpath.Parse(s)
		if err != nil || p.Collection != vpath.CollectionCards {
			return CardRef{}
		}
		// vpath has checked the PREFIX-N shape; only the number can still
		// fail, by overflowing.
		key, num, _ := strings.CutLast(p.Name, "-")
		n, err := strconv.ParseInt(num, 10, 64)
		if err != nil {
			return CardRef{}
		}
		return CardRef{Seq: n, ProjectKey: key, Project: p.Project}
	}
	if _, err := uuid.Parse(s); err == nil {
		return CardRef{UUID: s}
	}
	if key, num, ok := strings.CutLast(s, "-"); ok {
		if n, err := strconv.ParseInt(num, 10, 64); err == nil {
			return CardRef{Seq: n, ProjectKey: strings.ToUpper(key)}
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return CardRef{Seq: n}
	}
	return CardRef{}
}

// qualified is the PREFIX-N form of a qualified ref.
func (r CardRef) qualified() string { return r.ProjectKey + "-" + itoa(r.Seq) }

// String renders a CardRef for error messages. An address is shown as the
// address the caller typed.
func (r CardRef) String() string {
	switch {
	case r.UUID != "":
		return r.UUID
	case r.Project != "":
		return vpath.CardPath(r.Project, r.qualified()).String()
	case r.ProjectKey != "":
		return r.qualified()
	case r.Seq > 0:
		return itoa(r.Seq)
	}
	return "<none>"
}
```

In `internal/core/card.go`, add `"github.com/mtch3n/trellis/internal/vpath"` to the imports. Make `checkCardProject` the first statement of `loadCard`:

```go
	if err := c.checkCardProject(tx, projectID, ref); err != nil {
		return err
	}
```

Add the function after `loadCard`:

```go
// checkCardProject is the one place a card ref is held to the project it is
// looked up in. OTHER-12 typed while working in KEY used to open KEY-12, and
// /OTHER/cards/KEY-12 would have too. An address names its project, and so
// does a ref's prefix; either one disagreeing with the current key is another
// project. The project-merge layer replaces this check when refs become
// stored data and a prefix no longer has to match its project.
func (c *Core) checkCardProject(tx *sqlx.Tx, projectID string, ref CardRef) error {
	if ref.Project == "" && ref.ProjectKey == "" {
		return nil
	}
	key, err := projectKeyOf(tx, projectID)
	if err != nil {
		return err
	}
	named := ref.Project
	if named == "" || named == key {
		named = ref.ProjectKey
	}
	if named == key {
		return nil
	}
	var exists int
	if err := tx.Get(&exists, `SELECT COUNT(*) FROM project WHERE key = ?`, named); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound("card_not_found",
			fmt.Sprintf("no card %s: there is no project %s", ref, named), "trellis project ls")
	}
	addr := vpath.CardPath(named, ref.qualified()).String()
	return ErrUsage("wrong_project",
		fmt.Sprintf("%s is a card in project %s, not in %s", ref, named, key),
		"trellis card show "+addr)
}
```

In `internal/core/blocker.go`, change the imports to `"context"`, `"errors"` and `sqlx`. In both `BlockCard` and `UnblockCard`, wrap the blocker's `loadCard` error:

```go
		if err := c.loadCard(tx, projectID, blockerRef, &blocker); err != nil {
			return crossProjectBlock(err, blockerRef)
		}
```

Append:

```go
// crossProjectBlock says why a blocker from another project is refused: a card
// and the cards blocking it live in one project, where claiming can see both.
func crossProjectBlock(err error, blocker CardRef) error {
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != "wrong_project" {
		return err
	}
	return ErrUsage("cross_project_block",
		blocker.String()+" is not in this project; a card can only be blocked by a card in its own project",
		`trellis card note <card> --body "waiting on `+blocker.String()+`"`)
}
```

In `internal/core/import.go`, add `"errors"` to the imports. Replace the blocker lookup inside the `for _, dep := range item.BlockedBy` loop:

```go
				if !ok {
					var existing Card
					ref := ParseCardRef(dep)
					if err := c.loadCard(tx, projectID, ref, &existing); err != nil {
						if e, isCore := errors.AsType[*Error](err); isCore && e.Code == "wrong_project" {
							return crossProjectBlock(err, ref)
						}
						return ErrUsage("unknown_blocker",
							"card "+strconv.Itoa(i+1)+" is blocked by "+dep+", which is neither an id in this import nor an existing card",
							`give the blocking entry an "id" and reference it, or pass an existing ref like XPSCTL-12`)
					}
					blocker = &existing
				}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/core && go test ./internal/core/ ./internal/ui/ && go vet ./internal/core/`
Expected: `ok`. The UI tests still pass: their URLs use the project's own refs.

- [ ] **Step 5: Commit**

```bash
git add internal/core
git commit -m "fix(core): a card ref names its project instead of silently opening the local one

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---
### Task 6: Unique vault slugs and artifact names, and artifact addresses

**Files:**
- Create: `internal/store/migrations/0014_unique_addresses.sql`
- Test: `internal/store/migrate_0014_test.go`
- Modify: `internal/core/address.go`: add `ArtifactAddress`, `isUUID`
- Modify: `internal/core/pin.go`: `EscalateKnowledge`, `DemoteKnowledge`
- Modify: `internal/core/artifact.go`: the `Ref` field, `CreateArtifact`, `ListArtifacts`, new `ResolveArtifact`
- Modify: `internal/core/graph.go`: the artifact node
- Test: `internal/core/artifact_address_test.go`

**Interfaces:**
- Consumes: `openAtVersion`, `mustExec` (pin-only plan, package `store` tests); `ParseAddress`, `wrongProject`, `projectKeyOf` (Task 3); `resolveDocStubs` (existing).
- Produces:
  - Goose version 14.
  - `func ArtifactAddress(key, name string) string`
  - `func isUUID(s string) bool`
  - `Artifact.Ref string` (`json:"ref"`)
  - `func (c *Core) ResolveArtifact(ctx context.Context, projectID, arg string) (Artifact, error)`: takes a UUID, a name, or an address naming `projectID`'s project. It returns `artifact_not_found` (exit 3), `wrong_project` or `wrong_collection`.
  - `EscalateKnowledge` returns `global_slug_taken` (exit 4). Escalation and demotion both backfill stubs that name the entry's new address.

- [ ] **Step 1: Write the failing migration tests**

`internal/store/migrate_0014_test.go`:

```go
package store

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
)

func seedV13(t *testing.T, db *sqlx.DB, rows ...string) {
	t.Helper()
	mustExec(t, db, `INSERT INTO project (id, key, name, created_at) VALUES
		('p1', 'ALPHA', 'ALPHA', 1), ('p2', 'BETA', 'BETA', 1)`)
	for _, q := range rows {
		mustExec(t, db, q)
	}
}

func knowledgeRow(id, project, slug string, global int) string {
	return fmt.Sprintf(`INSERT INTO knowledge
		(id, project_id, slug, title, path, content_hash, mtime, size, global, created_at, updated_at)
		VALUES ('%s', '%s', '%s', 't', '/kb/%s.md', 'h', 1, 1, %d, 1, 1)`, id, project, slug, id, global)
}

func artifactRow(id, project, name string) string {
	return fmt.Sprintf(`INSERT INTO artifact
		(id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at)
		VALUES ('%s', '%s', '%s', '/art/%s/%s', 'image', 'image/png', 1, 'h', 1, 1)`,
		id, project, name, id, name)
}

func requireFailedAt13(t *testing.T, db *sqlx.DB) {
	t.Helper()
	err := goose.Up(db.DB, "migrations")
	if err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("Up = %v, want a unique constraint failure", err)
	}
	version, err := goose.GetDBVersion(db.DB)
	if err != nil || version != 13 {
		t.Errorf("version = %d, %v; want 13", version, err)
	}
}

func TestUniqueAddressesAcceptCleanData(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db,
		knowledgeRow("n1", "p1", "design", 1),
		knowledgeRow("n2", "p2", "design", 0),
		knowledgeRow("n3", "p2", "notes", 1),
		artifactRow("a1", "p1", "shot.png"),
		artifactRow("a2", "p2", "shot.png"),
	)
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}
	var n int
	if err := db.Get(&n, `SELECT count(*) FROM sqlite_master
		WHERE type = 'index' AND name IN ('knowledge_global_slug', 'artifact_name')`); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("found %d of the 2 indexes", n)
	}
}

func TestUniqueAddressesRefuseTwoVaultEntriesWithOneSlug(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db, knowledgeRow("n1", "p1", "design", 1), knowledgeRow("n2", "p2", "design", 1))
	requireFailedAt13(t, db)
}

func TestUniqueAddressesRefuseTwoArtifactsWithOneName(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db, artifactRow("a1", "p1", "shot.png"), artifactRow("a2", "p1", "shot.png"))
	requireFailedAt13(t, db)
}
```

`artifactRow` builds two different paths for the same name, because `path` is already unique per project.

Run: `go test ./internal/store/ -run UniqueAddresses`
Expected: FAIL. With no migration 14 the clean case finds 0 indexes, and the duplicate cases migrate without error.

- [ ] **Step 2: Write the migration**

`internal/store/migrations/0014_unique_addresses.sql`:

```sql
-- +goose Up
-- An address names exactly one object (docs/superpowers/specs/2026-09-16-virtual-paths-design.md).
-- Creating a unique index fails when rows already collide, and that is the
-- intended outcome: two vault entries with one slug already share a file path,
-- and choosing between them is a human decision.
CREATE UNIQUE INDEX knowledge_global_slug ON knowledge(slug) WHERE global = 1;
CREATE UNIQUE INDEX artifact_name ON artifact(project_id, name);

-- +goose Down
DROP INDEX artifact_name;
DROP INDEX knowledge_global_slug;
```

Run: `go test ./internal/store/`
Expected: `ok`, including the 0013 tests.

- [ ] **Step 3: Write the failing core tests**

`internal/core/artifact_address_test.go`:

```go
package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEscalateRefusesASlugTheVaultHolds(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	mine, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Conventions", Body: "mine\n"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := c.CreateKnowledge(ctx, other.ID, NewKnowledge{Title: "Conventions", Body: "theirs\n"})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := c.EscalateKnowledge(ctx, p.ID, mine.Slug, "shared")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.EscalateKnowledge(ctx, other.ID, theirs.Slug, "also shared")
	if got := errCode(t, err); got != "global_slug_taken" {
		t.Fatalf("code = %s, want global_slug_taken", got)
	}
	if _, err := os.Stat(theirs.Path); err != nil {
		t.Errorf("the refused entry's file moved: %v", err)
	}
	raw, err := os.ReadFile(moved.Path)
	if err != nil || !strings.Contains(string(raw), "mine") {
		t.Errorf("the vault file was overwritten: %q, %v", raw, err)
	}
}

func TestEscalationBackfillsVaultStubs(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	if _, err := c.CreateKnowledge(ctx, other.ID, NewKnowledge{
		Title: "Notes", Body: "See [[/GLOBAL/knowledge/conventions]].\n"}); err != nil {
		t.Fatal(err)
	}
	if kinds, _ := lintKinds(t, c, other.ID); kinds["stub"] != 1 {
		t.Fatalf("before escalation: %v, want one stub", kinds)
	}
	target, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, p.ID, target.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	if kinds, _ := lintKinds(t, c, other.ID); kinds["stub"] != 0 {
		t.Errorf("after escalation: %v, want the stub resolved", kinds)
	}
}

func screenShot(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "screen shot.png")
	if err := os.WriteFile(source, []byte("not really an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestArtifactsAreFoundByIdNameAndAddress(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	a, err := c.CreateArtifact(ctx, p.ID, screenShot(t))
	if err != nil {
		t.Fatal(err)
	}
	const want = "/XPSCTL/artifacts/screen-shot.png"
	if a.Name != "screen-shot.png" || a.Ref != want {
		t.Fatalf("artifact = %+v", a)
	}
	items, err := c.ListArtifacts(ctx, p.ID, "")
	if err != nil || len(items) != 1 || items[0].Ref != want {
		t.Fatalf("ListArtifacts = %+v, %v", items, err)
	}
	for _, arg := range []string{a.ID, a.Name, a.Ref} {
		got, err := c.ResolveArtifact(ctx, p.ID, arg)
		if err != nil || got.ID != a.ID || got.Ref != want {
			t.Errorf("ResolveArtifact(%q) = %+v, %v", arg, got, err)
		}
	}
	other := seededProject2(t, c)
	for arg, code := range map[string]string{
		"missing.png":            "artifact_not_found",
		"/XPSCTL/cards/XPSCTL-1": "wrong_collection",
	} {
		_, err := c.ResolveArtifact(ctx, p.ID, arg)
		if got := errCode(t, err); got != code {
			t.Errorf("ResolveArtifact(%q): code = %s, want %s", arg, got, code)
		}
	}
	_, err = c.ResolveArtifact(ctx, other.ID, a.Ref)
	if got := errCode(t, err); got != "wrong_project" {
		t.Errorf("another project's address: code = %s", got)
	}
}

// A row can outlive its file, and its name is still its address.
func TestArtifactNamesStayUniqueWhenTheFileIsGone(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	source := screenShot(t)
	first, err := c.CreateArtifact(ctx, p.ID, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first.Path); err != nil {
		t.Fatal(err)
	}
	second, err := c.CreateArtifact(ctx, p.ID, source)
	if err != nil {
		t.Fatalf("second artifact: %v", err)
	}
	if second.Name != "screen-shot-2.png" {
		t.Errorf("second name = %q, want screen-shot-2.png", second.Name)
	}
}

func TestGraphNamesArtifactsByAddress(t *testing.T) {
	c, p, b := kbCore(t)
	ctx := t.Context()
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "attach evidence"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.CreateArtifact(ctx, p.ID, screenShot(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LinkArtifactToCard(ctx, p.ID, card.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	g, err := c.Traverse(ctx, card.ID, 1, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.Type == "artifact" && n.Ref != a.Ref {
			t.Errorf("artifact node ref = %q, want %q", n.Ref, a.Ref)
		}
	}
}
```

`lintKinds` comes from Task 4's `wikilink_address_test.go`.

Run: `go test ./internal/core/ -run 'VaultHolds|BackfillsVault|IdNameAndAddress|FileIsGone|ArtifactsByAddress'`
Expected: FAIL. The build fails because `Artifact.Ref` and `ResolveArtifact` are undefined.

- [ ] **Step 4: Implement**

Append to `internal/core/address.go`, and add `"uuid"` (standard library) to its imports:

```go
// ArtifactAddress is an artifact's canonical address.
func ArtifactAddress(key, name string) string {
	return vpath.ArtifactPath(key, name).String()
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
```

In `internal/core/pin.go`, `EscalateKnowledge`, between `dir, err := c.kbDir(GlobalKey, true)` (with its error check) and `dest, err := moveFile(doc.Path, dir)`, insert:

```go
		// An address names one entry. Two projects escalating one slug would
		// both claim it, and the second move would overwrite the first file.
		vaultAddress := DocAddress("", true, doc.Slug)
		var taken int
		if err := tx.Get(&taken,
			`SELECT COUNT(*) FROM knowledge WHERE global = 1 AND slug = ?`, doc.Slug); err != nil {
			return err
		}
		_, statErr := os.Lstat(dir + string(os.PathSeparator) + baseName(doc.Path))
		if taken > 0 || statErr == nil {
			return ErrConflict("global_slug_taken", "the vault already holds "+vaultAddress,
				"trellis knowledge show "+vaultAddress+"   # compare the two, then rename or merge one")
		}
```

In the same function, after `doc.Global, doc.Path, doc.ReviewBy, doc.ReviewedAt = true, dest, &reviewBy, &now`, insert:

```go
		// Links written to the vault address before the entry got there are
		// stubs; escalating is what makes them resolvable.
		if err := c.resolveDocStubs(tx, &doc); err != nil {
			return err
		}
```

In `DemoteKnowledge`, after `doc.Global, doc.Path, doc.ReviewBy = false, dest, nil`, insert the same `resolveDocStubs` call, with the comment `// Links to the project address resolve once the entry is back.`

In `internal/core/artifact.go`:

1. Add `"database/sql"` to the imports.
2. Add a field to `Artifact`, after `UpdatedAt`:

```go
	Ref         string `db:"-" json:"ref"` // /KEY/artifacts/<name>
```

3. In `CreateArtifact`, replace the name-picking loop with:

```go
		path := filepath.Join(dir, name)
		for n := 2; ; n++ {
			// The table is checked as well as the disk: a row can outlive its
			// file, and its name is still its address.
			var taken int
			if err := tx.Get(&taken, `SELECT COUNT(*) FROM artifact WHERE project_id = ? AND name = ?`,
				projectID, filepath.Base(path)); err != nil {
				return err
			}
			if _, err := os.Stat(path); taken == 0 && errors.Is(err, os.ErrNotExist) {
				break
			}
			path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, filepath.Ext(name)), n, filepath.Ext(name)))
		}
```

4. Replace the end of the transaction (`_, err = tx.Exec(`INSERT INTO artifact ...`); return err`) with:

```go
		if _, err := tx.Exec(`INSERT INTO artifact (id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, out.ID, out.ProjectID, out.Name, out.Path, out.Kind, out.MIME, out.Size, out.ContentHash, out.CreatedAt, out.UpdatedAt); err != nil {
			return err
		}
		out.Ref = ArtifactAddress(key, out.Name)
		return nil
```

5. Replace `ListArtifacts` with:

```go
func (c *Core) ListArtifacts(ctx context.Context, projectID, cardID string) ([]Artifact, error) {
	var out []Artifact
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		if cardID == "" {
			err = tx.Select(&out, `SELECT * FROM artifact WHERE project_id = ? ORDER BY updated_at DESC`, projectID)
		} else {
			err = tx.Select(&out, `SELECT a.* FROM artifact a JOIN link l ON l.to_type = 'artifact' AND l.to_id = a.id WHERE a.project_id = ? AND l.from_type = 'card' AND l.from_id = ? ORDER BY a.updated_at DESC`, projectID, cardID)
		}
		if err != nil {
			return err
		}
		key, err := projectKeyOf(tx, projectID)
		if err != nil {
			return err
		}
		for i := range out {
			out[i].Ref = ArtifactAddress(key, out[i].Name)
		}
		return nil
	})
	return out, err
}
```

6. Append `ResolveArtifact`:

```go
// ResolveArtifact finds an artifact by id, by name, or by an address that
// names projectID's project.
func (c *Core) ResolveArtifact(ctx context.Context, projectID, arg string) (Artifact, error) {
	arg = strings.TrimSpace(arg)
	var out Artifact
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		key, err := projectKeyOf(tx, projectID)
		if err != nil {
			return err
		}
		column, value := "name", arg
		switch {
		case isUUID(arg):
			column = "id"
		case strings.HasPrefix(arg, "/"):
			p, err := ParseAddress(arg, vpath.CollectionArtifacts)
			if err != nil {
				return err
			}
			if p.Project != key {
				return wrongProject(arg, p, key)
			}
			value = p.Name
		}
		err = tx.Get(&out, `SELECT * FROM artifact WHERE project_id = ? AND `+column+` = ?`, projectID, value)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound("artifact_not_found", "no artifact "+arg+" in project "+key, "trellis artifact ls")
		}
		if err != nil {
			return err
		}
		out.Ref = ArtifactAddress(key, out.Name)
		return nil
	})
	return out, err
}
```

Also add `"github.com/mtch3n/trellis/internal/vpath"` to `artifact.go`'s imports. `column` is one of two literals, never user text.

In `internal/core/graph.go`, replace `node.Type, node.Ref, node.Title = "artifact", artifact.ID, artifact.Name` with:

```go
					akey, err := projectKeyOf(tx, artifact.ProjectID)
					if err != nil {
						return err
					}
					node.Type, node.Ref, node.Title = "artifact", ArtifactAddress(akey, artifact.Name), artifact.Name
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w internal && go test ./internal/core/ ./internal/store/ ./internal/cli/ && go vet ./...`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/store internal/core
git commit -m "feat(core): one vault entry per slug, one artifact per name, and artifacts by address

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---
### Task 7: Commands follow the project an argument names

**Files:**
- Create: `internal/cli/target.go`
- Test: `internal/cli/target_test.go`
- Modify: `internal/cli/resolve.go`: `resolveProject`, `selectBoard`; new `namedProjectKey`, `boardNamed`, `projectConflict`, `normalizeProjectArg`
- Modify: `internal/cli/root.go`: delete `projectKey`
- Modify: `internal/cli/doctor.go`: `checkProject`
- Modify: `internal/cli/init.go`: `--project` refusal, `initNotes`
- Modify: `internal/cli/card.go`: show, move, edit, rm, claim, release, renew, note
- Modify: `internal/cli/card_block.go`, `internal/cli/card_archive.go`
- Modify test: `internal/cli/doctor_test.go`

**Interfaces:**
- Consumes:
  - `vpath.Parse`, `vpath.SplitAnchor`, the collection constants (Task 1)
  - `core.ParseAddress` (Task 3), `core.ParseCardRef` (Task 5), `core.BoardBySlug`
  - `resolveProject`, `selectBoard`, `resolvedProject` (pin-only plan)
  - `pinEnv`, `seedProject`, `writePin`, `coreErr`, `showBoard`, `runCmd`, `execCmd` (existing test helpers)
- Produces, in `internal/cli/target.go`:
  - `type refArg struct{ Collection, Value string; NoProject bool }`
  - `func argProject(a refArg) (key, ref string, err error)`: a board address becomes its slug; card, knowledge and artifact references pass through whole
  - `func withTargets(args []refArg, fn func(app *appCtx, refs []string) error) error`: the positional reference first, then every reference-taking flag
  - `func withTarget(a refArg, fn func(app *appCtx, ref string) error) error`: `withTargets` with one reference
  - `func targetContext(a refArg, key, ref string, relative bool) (*appCtx, error)`
  - `func namedBoard(ctx context.Context, c *core.Core, p core.Project, collection, ref string) (core.Board, error)`
  - `func isUnresolved(err error) bool`
  - `func isVaultAddress(v string) bool`
- Produces, in `internal/cli/resolve.go`:
  - `func namedProjectKey() (string, error)`
  - `func boardNamed(ctx context.Context, c *core.Core, p core.Project, v string) (core.Board, error)`
  - `func projectConflict(have, arg, named string) error`, which returns `project_conflict` (exit 2)
  - `func normalizeProjectArg(v string) string`
- Behavior:
  - `--project` and `TRELLIS_PROJECT` accept `/KEY`.
  - `--board` and `TRELLIS_BOARD` accept `/KEY/boards/<slug>`. A `--board` address names its project.
  - `projectKey()` is deleted; `namedProjectKey` replaces its one caller.
  - A command whose references name two different projects fails with `project_conflict`, and so does one whose relative reference means the pinned project while another reference names a different one.
  - A `NoProject` vault address consults nothing ambient: no pin, no `TRELLIS_PROJECT`, no `--project`.

- [ ] **Step 1: Write the failing tests**

`internal/cli/target_test.go`:

```go
package cli

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"
)

// refOf runs a command with --json and returns the "ref" it prints.
func refOf(t *testing.T, args ...string) string {
	t.Helper()
	out := runCmd(t, append(args, "--json")...)
	var v struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return v.Ref
}

// targetEnv is a directory pinned to ALPHA, with BETA beside it and one card
// in each: ALPHA-1 and BETA-1. BETA also has a board named Side.
func targetEnv(t *testing.T) string {
	t.Helper()
	dir := pinEnv(t, "alpha")
	seedProject(t, "ALPHA")
	seedProject(t, "BETA", "Side")
	writePin(t, dir, "/ALPHA\n")
	if ref := refOf(t, "card", "new", "--title", "alpha one"); ref != "ALPHA-1" {
		t.Fatalf("seed card = %s", ref)
	}
	if ref := refOf(t, "card", "new", "--title", "beta one", "--project", "BETA"); ref != "BETA-1" {
		t.Fatalf("seed card = %s", ref)
	}
	return dir
}

func TestAQualifiedCardRefNamesItsProject(t *testing.T) {
	targetEnv(t)
	for arg, want := range map[string]string{
		"BETA-1": "BETA-1", "beta-1": "BETA-1", "1": "ALPHA-1", "ALPHA-1": "ALPHA-1",
		"/BETA/cards/BETA-1": "BETA-1", "/alpha/cards/alpha-1": "ALPHA-1",
	} {
		if got := refOf(t, "card", "show", arg); got != want {
			t.Errorf("card show %s = %s, want %s", arg, got, want)
		}
	}
}

func TestACardAddressNeedsNoPin(t *testing.T) {
	pinEnv(t, "loose")
	seedProject(t, "BETA")
	refOf(t, "card", "new", "--title", "beta one", "--project", "BETA")
	if got := refOf(t, "card", "show", "/BETA/cards/BETA-1"); got != "BETA-1" {
		t.Errorf("card show = %s", got)
	}
	if got := refOf(t, "card", "show", "BETA-1"); got != "BETA-1" {
		t.Errorf("card show BETA-1 = %s", got)
	}
}

func TestAnAddressBeatsTrellisProjectAndConflictsWithTheFlag(t *testing.T) {
	targetEnv(t)
	t.Setenv("TRELLIS_PROJECT", "ALPHA")
	if got := refOf(t, "card", "show", "/BETA/cards/BETA-1"); got != "BETA-1" {
		t.Errorf("address under TRELLIS_PROJECT = %s", got)
	}
	if got := refOf(t, "card", "show", "BETA-1", "--project", "BETA"); got != "BETA-1" {
		t.Errorf("agreeing --project = %s", got)
	}
	_, err := execCmd("card", "show", "/BETA/cards/BETA-1", "--project", "ALPHA")
	if ce := coreErr(t, err); ce.Code != "project_conflict" {
		t.Errorf("error = %+v", ce)
	}
}

func TestAMisdirectedOrMalformedAddress(t *testing.T) {
	targetEnv(t)
	_, err := execCmd("card", "show", "/BETA/knowledge/notes")
	ce := coreErr(t, err)
	if ce.Code != "wrong_collection" || !strings.Contains(ce.Fix, "trellis knowledge show /BETA/knowledge/notes") {
		t.Errorf("error = %+v", ce)
	}
	_, err = execCmd("card", "show", "/BETA/cards/12")
	if ce := coreErr(t, err); ce.Code != "bad_path" {
		t.Errorf("error = %+v", ce)
	}
}

// Moving another project's card must not drag it onto that project's default
// board: a card named by address is worked on its own board.
func TestMovingANamedCardKeepsItsBoard(t *testing.T) {
	targetEnv(t)
	if ref := refOf(t, "card", "new", "--title", "side card", "--project", "BETA", "--board", "Side"); ref != "BETA-2" {
		t.Fatalf("side card = %s", ref)
	}
	runCmd(t, "card", "move", "BETA-2", "done")
	out := runCmd(t, "board", "show", "--json", "--project", "BETA", "--board", "Side")
	var view struct {
		Columns []struct {
			Name  string `json:"name"`
			Cards []struct {
				Ref string `json:"ref"`
			} `json:"cards"`
		} `json:"columns"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	var done []string
	for _, col := range view.Columns {
		if col.Name != "done" {
			continue
		}
		for _, c := range col.Cards {
			done = append(done, c.Ref)
		}
	}
	if !slices.Contains(done, "BETA-2") {
		t.Errorf("done on Side = %v, want BETA-2 there; columns: %+v", done, view.Columns)
	}
}

// In a pinned directory a relative card means the pinned project, so a --by
// naming another one is a conflict. An address whose project differs from
// its ref's prefix gets through the CLI only when both references name that
// project, and core refuses it there.
func TestABlockerFromAnotherProjectIsRefused(t *testing.T) {
	targetEnv(t)
	for _, by := range []string{"BETA-1", "/BETA/cards/BETA-1", "/BETA/cards/ALPHA-1"} {
		_, err := execCmd("card", "block", "1", "--by", by)
		if ce := coreErr(t, err); ce.Code != "project_conflict" {
			t.Errorf("--by %s: error = %+v", by, ce)
		}
	}
	_, err := execCmd("card", "block", "/ALPHA/cards/ALPHA-1", "--by", "/BETA/cards/BETA-1")
	if ce := coreErr(t, err); ce.Code != "project_conflict" {
		t.Errorf("two addresses, two projects: error = %+v", ce)
	}
	_, err = execCmd("card", "block", "/BETA/cards/BETA-1", "--by", "/BETA/cards/ALPHA-1")
	if ce := coreErr(t, err); ce.Code != "cross_project_block" {
		t.Errorf("ALPHA's ref under BETA's address: error = %+v", ce)
	}
}

// With no pin, a --by address is what names the project.
func TestABlockerAddressNamesTheProject(t *testing.T) {
	pinEnv(t, "loose")
	seedProject(t, "BETA")
	refOf(t, "card", "new", "--title", "first", "--project", "BETA")
	refOf(t, "card", "new", "--title", "second", "--project", "BETA")
	out := runCmd(t, "card", "block", "2", "--by", "/BETA/cards/BETA-1", "--json")
	var v struct {
		Ref       string `json:"ref"`
		BlockedBy []struct {
			Ref string `json:"ref"`
		} `json:"blocked_by"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if v.Ref != "BETA-2" || len(v.BlockedBy) != 1 || v.BlockedBy[0].Ref != "BETA-1" {
		t.Errorf("block = %+v", v)
	}
}

func TestProjectAndBoardSelectorsTakeAddresses(t *testing.T) {
	targetEnv(t)
	if got := showBoard(t, "--project", "/beta").Project; got != "BETA" {
		t.Errorf("--project /beta = %s", got)
	}
	if got := showBoard(t, "--board", "/BETA/boards/side"); got.Project != "BETA" || got.Slug != "side" {
		t.Errorf("--board address = %+v", got)
	}
	_, err := execCmd("board", "show", "--json", "--board", "/BETA/boards/side", "--project", "ALPHA")
	if ce := coreErr(t, err); ce.Code != "project_conflict" {
		t.Errorf("error = %+v", ce)
	}
	t.Setenv("TRELLIS_PROJECT", "/BETA")
	if got := showBoard(t).Project; got != "BETA" {
		t.Errorf("TRELLIS_PROJECT=/BETA = %s", got)
	}
}
```

In `internal/cli/doctor_test.go`, extend `TestCheckProjectNamesItsSource`. After the existing `TRELLIS_PROJECT` assertion, add:

```go
	t.Setenv("TRELLIS_PROJECT", "/envkey")
	if got := checkProject(); !strings.Contains(got.Detail, "ENVKEY (from TRELLIS_PROJECT)") {
		t.Errorf("env address: %+v", got)
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'QualifiedCardRef|NeedsNoPin|BeatsTrellisProject|Misdirected|KeepsItsBoard|BlockerFromAnother|BlockerAddress|SelectorsTakeAddresses|NamesItsSource'`
Expected: FAIL. `card show BETA-1` returns `wrong_project` from core because the command still runs in ALPHA, and `--project /beta` is `project_not_found`.

- [ ] **Step 3: Teach resolution to read addresses**

In `internal/cli/resolve.go`, add `"fmt"`, `"strings"` and `"github.com/mtch3n/trellis/internal/vpath"` to the imports.

Replace the first block of `resolveProject`, `if key := projectKey(); key != "" {`, with:

```go
	key, err := namedProjectKey()
	if err != nil {
		return resolvedProject{}, err
	}
	if key != "" {
		p, err := c.ProjectByKey(ctx, key)
		return resolvedProject{Project: p}, err
	}
```


Replace `selectBoard` with:

```go
// selectBoard picks the board: --board, then TRELLIS_BOARD, then the pin's
// board when the pin also chose the project, then core.SelectBoard's rules.
// Either selector may be a board address.
func selectBoard(ctx context.Context, c *core.Core, r resolvedProject) (core.Board, error) {
	if requested := cmp.Or(boardFlag, os.Getenv("TRELLIS_BOARD")); requested != "" {
		return boardNamed(ctx, c, r.Project, requested)
	}
	if r.Pin == nil || r.Pin.Target.Board() == "" {
		return c.SelectBoard(ctx, r.Project.ID, "")
	}
	b, err := c.BoardBySlug(ctx, r.Project.ID, r.Pin.Target.Board())
	if ce, ok := errors.AsType[*core.Error](err); ok {
		ce.Msg = r.Pin.Path + ": " + ce.Msg
	}
	return b, err
}
```

Append:

```go
// namedProjectKey is the project named without the pin's help: --project,
// then the project a --board address names, then TRELLIS_PROJECT. --project
// and a --board address that disagree are a conflict: the caller stated two
// targets.
func namedProjectKey() (string, error) {
	flag := normalizeProjectArg(projectFlagKey)
	fromBoard := ""
	if v := strings.TrimSpace(boardFlag); strings.HasPrefix(v, "/") {
		p, err := core.ParseAddress(v, vpath.CollectionBoards)
		if err != nil {
			return "", err
		}
		fromBoard = p.Project
	}
	if flag != "" && fromBoard != "" && flag != fromBoard {
		return "", projectConflict(flag, boardFlag, fromBoard)
	}
	return cmp.Or(flag, fromBoard, normalizeProjectArg(os.Getenv("TRELLIS_PROJECT"))), nil
}

// boardNamed resolves a board given by name or by address in project p. An
// address naming another project is a conflict, never a silent switch.
func boardNamed(ctx context.Context, c *core.Core, p core.Project, v string) (core.Board, error) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "/") {
		return c.SelectBoard(ctx, p.ID, v)
	}
	addr, err := core.ParseAddress(v, vpath.CollectionBoards)
	if err != nil {
		return core.Board{}, err
	}
	if addr.Project != p.Key {
		return core.Board{}, projectConflict(p.Key, v, addr.Project)
	}
	return c.BoardBySlug(ctx, p.ID, addr.Name)
}

// projectConflict reports two explicit targets that disagree.
func projectConflict(have, arg, named string) error {
	return core.ErrUsage("project_conflict",
		fmt.Sprintf("this command acts in %s, but %s names %s; give only one of them", have, arg, named), "")
}

// normalizeProjectArg reads a project given as KEY or /KEY. Anything else is
// upper-cased and left for the lookup to report.
func normalizeProjectArg(v string) string {
	v = strings.TrimSpace(v)
	if p, err := vpath.Parse(v); err == nil && p.Collection == "" {
		return p.Project
	}
	return strings.ToUpper(v)
}
```

In `internal/cli/root.go`, delete `projectKey` and its comment. Its only caller was `resolveProject`.

In `internal/cli/doctor.go`, `checkProject`, replace `strings.ToUpper(projectFlagKey)` with `normalizeProjectArg(projectFlagKey)`, and `strings.ToUpper(os.Getenv("TRELLIS_PROJECT"))` with `normalizeProjectArg(os.Getenv("TRELLIS_PROJECT"))`.

In `internal/cli/init.go`, replace `strings.ToUpper(projectFlagKey)` in the `init_project_flag` fix with `normalizeProjectArg(projectFlagKey)`. In `initNotes`, replace `strings.ToUpper(os.Getenv("TRELLIS_PROJECT"))` with `normalizeProjectArg(os.Getenv("TRELLIS_PROJECT"))`.

- [ ] **Step 4: Write `internal/cli/target.go`**

```go
package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/vpath"
)

// refArg is one reference a command takes, positional or from a flag, and
// the collection it names.
type refArg struct {
	Collection string // vpath.CollectionCards, CollectionKnowledge, CollectionBoards or CollectionArtifacts
	Value      string // "" when an optional flag was not given
	// NoProject lets a /GLOBAL/knowledge address run with no project at all:
	// reading, editing or walking from a vault entry needs none. fn then
	// receives an appCtx whose Project and Board are zero.
	NoProject bool
}

// argProject reads the project a reference names by itself -- an absolute
// address or, for a card, a qualified ref such as OTHER-12 -- and returns ""
// when the reference is relative or is a vault address. ref is the reference
// in the form core takes: a board address becomes its slug. Everything else
// passes through whole, because core reads card, knowledge and artifact
// addresses itself, and a card address must reach core with its project.
func argProject(a refArg) (key, ref string, err error) {
	v := strings.TrimSpace(a.Value)
	if !strings.HasPrefix(v, "/") {
		if a.Collection == vpath.CollectionCards {
			if r := core.ParseCardRef(v); r.ProjectKey != "" {
				return r.ProjectKey, v, nil
			}
		}
		return "", v, nil
	}
	target := v
	if a.Collection == vpath.CollectionKnowledge {
		target, _ = vpath.SplitAnchor(v) // only an entry has headings
	}
	p, err := core.ParseAddress(strings.TrimSpace(target), a.Collection)
	if err != nil {
		return "", "", err
	}
	switch {
	case p.Project == vpath.GlobalKey:
		return "", v, nil // the vault belongs to no project
	case a.Collection == vpath.CollectionBoards:
		return p.Project, p.Name, nil
	default:
		return p.Project, v, nil
	}
}

// withTarget runs fn in the context one reference names. See withTargets.
func withTarget(a refArg, fn func(app *appCtx, ref string) error) error {
	return withTargets([]refArg{a}, func(app *appCtx, refs []string) error { return fn(app, refs[0]) })
}

// withTargets runs fn in the context a command's references name, and closes
// the database afterwards. args holds the positional reference first, then
// every flag that takes one (--by, --card, a command's own --board). refs[i]
// is args[i] in the form core takes, and "" for a flag that was not given.
//
// A reference that names its own project -- an address, or a qualified card
// ref -- decides where the command runs, so it needs no pin. It beats the pin
// and TRELLIS_PROJECT, which are ambient.
//
// Two references naming different projects are a conflict: the caller
// stated two targets. A relative reference names the current project, so it
// conflicts with a reference naming another one whenever a current project
// resolves. In a directory where none does, the named project is the only
// candidate, and the relative reference is read there. With no reference
// naming a project, the command runs where withBoard would.
func withTargets(args []refArg, fn func(app *appCtx, refs []string) error) error {
	refs := make([]string, len(args))
	keys := make([]string, len(args))
	named, primary, relative := "", 0, false
	for i, a := range args {
		if strings.TrimSpace(a.Value) == "" {
			continue
		}
		key, ref, err := argProject(a)
		if err != nil {
			return err
		}
		refs[i], keys[i] = ref, key
		switch {
		case key == "" && !isVaultAddress(a.Value):
			relative = true
		case key == "":
		case named == "":
			named, primary = key, i
		case key != named:
			return projectConflict(named, a.Value, key)
		}
	}
	app, err := targetContext(args[primary], named, refs[primary], relative)
	if err != nil {
		return err
	}
	defer app.db.Close()
	for i, a := range args {
		if a.Collection != vpath.CollectionBoards || keys[i] == "" {
			continue
		}
		// A board address works on that board; core takes board names.
		b, err := app.Core.BoardBySlug(context.Background(), app.Project.ID, refs[i])
		if err != nil {
			return err
		}
		refs[i] = b.Name
		if i == primary {
			app.Board = b
		}
	}
	return fn(app, refs)
}

// targetContext opens the context a command runs in. key is the project its
// references name, "" when none does. a is the reference that named it, or
// the positional one when none did. relative says whether some other
// reference relies on the current project.
func targetContext(a refArg, key, ref string, relative bool) (*appCtx, error) {
	if key == "" && a.NoProject && isVaultAddress(a.Value) {
		// A vault entry belongs to no project, so nothing ambient is
		// consulted: a malformed or stale pin, or a TRELLIS_PROJECT naming
		// nothing, must not stand between a reader and the vault.
		c, db, err := openCore()
		if err != nil {
			return nil, err
		}
		return &appCtx{Core: c, db: db}, nil
	}
	if key == "" {
		return currentBoard()
	}

	if flag := normalizeProjectArg(projectFlagKey); flag != "" && flag != key {
		return nil, projectConflict(flag, a.Value, key)
	}
	c, db, err := openCore()
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*appCtx, error) {
		db.Close()
		return nil, err
	}
	ctx := context.Background()
	r, rerr := resolveProject(ctx, c)
	switch {
	case rerr == nil && r.Project.Key == key:
		b, err := selectBoard(ctx, c, r)
		if err != nil {
			return fail(err)
		}
		return &appCtx{Core: c, Project: r.Project, Board: b, db: db}, nil
	case relative && rerr == nil:
		// The relative reference means this project, not the named one.
		return fail(projectConflict(r.Project.Key, a.Value, key))
	case relative && !isUnresolved(rerr):
		// A relative reference needs the current project, and a broken pin
		// is not the same as no pin.
		return fail(rerr)
	}
	p, err := c.ProjectByKey(ctx, key)
	if err != nil {
		return fail(err)
	}
	b, err := namedBoard(ctx, c, p, a.Collection, ref)
	if err != nil {
		return fail(err)
	}
	return &appCtx{Core: c, Project: p, Board: b, db: db}, nil
}

// namedBoard picks the board in a project a reference named. Only --board
// applies there: TRELLIS_BOARD and a pin's board describe the current
// project. A card is worked on its own board, so that moving OTHER-12 never
// drags it onto OTHER's default board.
func namedBoard(ctx context.Context, c *core.Core, p core.Project, collection, ref string) (core.Board, error) {
	switch {
	case collection == vpath.CollectionBoards:
		return core.Board{}, nil // withTargets sets the addressed board
	case boardFlag != "":
		return boardNamed(ctx, c, p, boardFlag)
	case collection == vpath.CollectionCards:
		card, err := c.GetCard(ctx, p.ID, core.ParseCardRef(ref))
		if err != nil {
			// The command looks the card up itself and reports this in its
			// own words; card block, for one, says cross_project_block.
			return core.Board{}, nil
		}
		boards, err := c.ListBoards(ctx, p.ID)
		if err != nil {
			return core.Board{}, err
		}
		i := slices.IndexFunc(boards, func(b core.Board) bool { return b.ID == card.BoardID })
		if i < 0 {
			return core.Board{}, fmt.Errorf("card %s is on a board that no longer exists", card.Ref)
		}
		return boards[i], nil
	default:
		return c.SelectBoard(ctx, p.ID, "")
	}
}

// isUnresolved reports whether err says no pin applies here.
func isUnresolved(err error) bool {
	ce, ok := errors.AsType[*core.Error](err)
	return ok && ce.Code == "unresolved"
}

// isVaultAddress reports whether v is a /GLOBAL address.
func isVaultAddress(v string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(v)), "/"+vpath.GlobalKey+"/")
}
```

- [ ] **Step 5: Route the card commands through `withTarget`**

In `internal/cli/card.go`, add `"github.com/mtch3n/trellis/internal/vpath"` to the imports. In each command below, replace `withBoard(func(app *appCtx) error {` with:

```go
withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
```

Inside the closure, replace each `core.ParseCardRef(args[0])` with `core.ParseCardRef(ref)` and each `cardID(cmd, app, args[0])` with `cardID(cmd, app, ref)`. The commands are:
- `newCardShowCmd`
- `newCardMoveCmd`, which has two `ParseCardRef(args[0])` calls; the error hints keep `args[0]`
- `newCardEditCmd`
- `newCardRmCmd`
- `newCardClaimCmd`
- `newCardReleaseCmd`
- `newCardRenewCmd`
- `newCardNoteCmd`

In `internal/cli/card_archive.go`, add the `vpath` import and replace the `RunE` with:

```go
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				action, fn := "archived", app.Core.ArchiveCard
				if restore {
					action, fn = "restored", app.Core.UnarchiveCard
				}
				card, err := fn(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
				if err != nil {
					return err
				}
				return Emit(cmd, card, func() string { return action + " " + card.Ref + "  " + card.Title })
			})
		},
```

Replace the `RunE` of `newCardBlockCmd` in `internal/cli/card_block.go` with:

```go
		RunE: func(cmd *cobra.Command, args []string) error {
			if by == "" {
				return core.ErrUsage("missing_blocker", "say which card blocks it",
					"trellis card block "+args[0]+" --by 12")
			}
			// --by takes a reference too, so it can name the project: a
			// blocker lives in its card's project.
			return withTargets([]refArg{
				{Collection: vpath.CollectionCards, Value: args[0]},
				{Collection: vpath.CollectionCards, Value: by},
			}, func(app *appCtx, refs []string) error {
				link := app.Core.BlockCard
				if remove {
					link = app.Core.UnblockCard
				}
				if err := link(cmd.Context(), app.Project.ID,
					core.ParseCardRef(refs[0]), core.ParseCardRef(refs[1])); err != nil {
					return err
				}
				return emitBlockers(cmd, app, refs[0])
			})
		},
```

Also add the `vpath` import to `card_block.go`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w internal/cli && go build ./... && go test ./internal/cli/ && go vet ./internal/cli/`
Expected: `ok`, including every pin-only resolution and `init` test.

- [ ] **Step 7: Commit**

```bash
git add internal/cli
git commit -m "feat(cli): a card address or qualified ref names its project, no pin needed

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---
### Task 8: Knowledge, link, graph, board, artifact and workspace commands follow addresses

**Files:**
- Modify: `internal/cli/target.go`: add `tuiCardRef`
- Modify:
  - `internal/cli/knowledge.go`: new, show, edit, rm, pin, nominate, escalate
  - `internal/cli/link.go`
  - `internal/cli/board.go`: default, rename, rm
  - `internal/cli/artifact.go`
  - `internal/cli/tui.go`
- Test: `internal/cli/target_knowledge_test.go`

**Interfaces:**
- Consumes:
  - `withTarget`, `withTargets`, `refArg`, `boardNamed`, `targetEnv`, `refOf` (Task 7)
  - `core.ParseAddress` (Task 3), `core.ParseCardRef` and `CardRef.Project` (Task 5)
  - `core.ResolveArtifact` (Task 6)
  - `core.EscalateKnowledge`
  - `home.DBPath`, `store.Open`
- Produces:
  - `func tuiCardRef(app *appCtx, arg string) (core.CardRef, error)`: refuses an address naming another project; a qualified ref's prefix is left to core's `checkCardProject`
- Behavior of reference-taking flags: `knowledge new --board`, `knowledge pin --board`, `artifact add --card`, `artifact link --card` and `artifact ls --card` go through `withTargets` with the positional reference, so an address in the flag names the command's project, even with no pin, and a disagreement is `project_conflict`.
  - `func resolveEntity(cmd *cobra.Command, app *appCtx, collection, ref string) (string, error)`: its signature changes
- Behavior of `graph <arg>`:
  - An address is routed by its collection.
  - An argument shaped like a card ref (`PREFIX-N`) is a card. An entry whose slug ends in `-N` is walked from by its address.
  - Any other relative argument tries an entry first, then a card, as before.

- [ ] **Step 1: Write the failing tests**

`internal/cli/target_knowledge_test.go`:

```go
package cli

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
)

// escalateByHand does what a human does at a terminal: escalate is refused to
// agents and needs a TTY, so tests go through core.
func escalateByHand(t *testing.T, key, slug string) {
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
	c := core.New(db, core.RealClock{}, "test")
	p, err := c.ProjectByKey(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), p.ID, slug, "shared"); err != nil {
		t.Fatal(err)
	}
}

// refsIn returns the "ref" of every object in the array under field. The
// payload is read loosely because graph output mixes a string root with its
// arrays.
func refsIn(t *testing.T, out, field string) []string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	items, _ := v[field].([]any)
	var refs []string
	for _, item := range items {
		obj, _ := item.(map[string]any)
		ref, _ := obj["ref"].(string)
		refs = append(refs, ref)
	}
	return refs
}

func TestEveryPrintedEntryRefOpens(t *testing.T) {
	targetEnv(t)
	const want = "/ALPHA/knowledge/lease-renewal"
	if got := refOf(t, "knowledge", "new", "--title", "Lease renewal", "--body", "A claim starts the lease.\n"); got != want {
		t.Fatalf("created = %s", got)
	}
	refs := refsIn(t, runCmd(t, "search", "lease", "--json"), "results")
	refs = append(refs, refsIn(t, runCmd(t, "recall", "why did the lease expire", "--json"), "results")...)
	if len(refs) != 2 {
		t.Fatalf("refs = %v", refs)
	}
	for _, ref := range refs {
		if got := refOf(t, "knowledge", "show", ref); got != want {
			t.Errorf("knowledge show %s = %s", ref, got)
		}
	}
}

func TestAnEntryAddressNamesItsProject(t *testing.T) {
	targetEnv(t)
	const want = "/BETA/knowledge/runbook"
	if got := refOf(t, "knowledge", "new", "--title", "Runbook", "--project", "BETA"); got != want {
		t.Fatalf("created = %s", got)
	}
	if got := refOf(t, "knowledge", "show", want); got != want {
		t.Errorf("knowledge show = %s", got)
	}
	_, err := execCmd("knowledge", "show", "runbook")
	if ce := coreErr(t, err); ce.Code != "knowledge_not_found" {
		t.Errorf("a relative slug stays in ALPHA: %+v", ce)
	}
}

// A vault address consults nothing ambient: no pin, broken or stale, and no
// TRELLIS_PROJECT stands between a reader and the vault.
func TestAVaultAddressIgnoresAmbientState(t *testing.T) {
	dir := pinEnv(t, "loose")
	seedProject(t, "ALPHA")
	refOf(t, "knowledge", "new", "--title", "Conventions", "--project", "ALPHA")
	escalateByHand(t, "ALPHA", "conventions")
	const want = "/GLOBAL/knowledge/conventions"

	_, err := execCmd("knowledge", "show", "conventions")
	if ce := coreErr(t, err); ce.Code != "unresolved" {
		t.Errorf("a relative slug still needs a project: %+v", ce)
	}

	// Each state builds on the one before; the last has a stale pin and an
	// environment naming a project that does not exist.
	for _, state := range []struct {
		name  string
		setup func()
	}{
		{"no pin", func() {}},
		{"malformed pin", func() { writePin(t, dir, "ALPHA\n") }},
		{"stale pin", func() { writePin(t, dir, "/GHOST\n") }},
		{"bad env", func() { t.Setenv("TRELLIS_PROJECT", "NOPE") }},
	} {
		state.setup()
		name := state.name
		if got := refOf(t, "knowledge", "show", want); got != want {
			t.Errorf("%s: knowledge show = %s", name, got)
		}
		if got := refOf(t, "knowledge", "edit", want, "--body", "Edited under "+name+".\n"); got != want {
			t.Errorf("%s: knowledge edit = %s", name, got)
		}
		if nodes := refsIn(t, runCmd(t, "graph", want, "--json"), "nodes"); len(nodes) == 0 || nodes[0] != want {
			t.Errorf("%s: graph = %v", name, nodes)
		}
	}
}

// A flag that takes a reference names the command's project as a positional
// one does, so these all work with no pin at all.
func TestReferenceFlagsNameTheProject(t *testing.T) {
	pinEnv(t, "loose")
	seedProject(t, "BETA", "Side")
	refOf(t, "card", "new", "--title", "beta one", "--project", "BETA")
	refOf(t, "knowledge", "new", "--title", "Runbook", "--project", "BETA")

	out := runCmd(t, "knowledge", "new", "--title", "Side notes", "--board", "/BETA/boards/side", "--json")
	var doc struct {
		Ref   string `json:"ref"`
		Board string `json:"board"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Ref != "/BETA/knowledge/side-notes" || doc.Board != "Side" {
		t.Errorf("knowledge new = %+v", doc)
	}

	runCmd(t, "knowledge", "pin", "runbook", "--board", "/BETA/boards/side", "--recap", "roll back first")
	out = runCmd(t, "knowledge", "pins", "--project", "BETA", "--board", "Side", "--json")
	var pins struct {
		Pins []struct {
			Slug  string `json:"slug"`
			Board string `json:"board"`
		} `json:"pins"`
	}
	if err := json.Unmarshal([]byte(out), &pins); err != nil {
		t.Fatal(err)
	}
	if len(pins.Pins) != 1 || pins.Pins[0].Slug != "runbook" || pins.Pins[0].Board != "Side" {
		t.Errorf("pins = %+v", pins.Pins)
	}

	file := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(file, []byte("not really an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	const shot = "/BETA/artifacts/shot.png"
	if got := refOf(t, "artifact", "add", file, "--card", "/BETA/cards/BETA-1"); got != shot {
		t.Errorf("artifact add = %s", got)
	}
	second := filepath.Join(t.TempDir(), "other.png")
	if err := os.WriteFile(second, []byte("also not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCmd(t, "artifact", "add", second, "--project", "BETA")
	runCmd(t, "artifact", "link", "other.png", "--card", "/BETA/cards/BETA-1")
	got := refsIn(t, runCmd(t, "artifact", "ls", "--card", "/BETA/cards/BETA-1", "--json"), "artifacts")
	if len(got) != 2 {
		t.Errorf("linked to BETA-1 = %v, want both", got)
	}
}

func TestReferenceFlagsThatDisagreeConflict(t *testing.T) {
	targetEnv(t)
	for _, args := range [][]string{
		// a relative positional means the pinned ALPHA
		{"knowledge", "pin", "runbook", "--board", "/BETA/boards/side", "--recap", "x"},
		{"artifact", "link", "shot.png", "--card", "/BETA/cards/BETA-1"},
		// two references, two projects
		{"knowledge", "pin", "/ALPHA/knowledge/runbook", "--board", "/BETA/boards/side", "--recap", "x"},
		{"artifact", "link", "/ALPHA/artifacts/shot.png", "--card", "/BETA/cards/BETA-1"},
	} {
		_, err := execCmd(args...)
		if ce := coreErr(t, err); ce.Code != "project_conflict" {
			t.Errorf("%v: error = %+v", args, ce)
		}
	}
}

func TestLinkAndGraphCrossProjects(t *testing.T) {
	targetEnv(t)
	refOf(t, "knowledge", "new", "--title", "Runbook", "--project", "BETA")
	runCmd(t, "link", "1", "/BETA/knowledge/runbook")
	nodes := refsIn(t, runCmd(t, "graph", "1", "--json"), "nodes")
	found := false
	for _, ref := range nodes {
		found = found || ref == "/BETA/knowledge/runbook"
	}
	if !found {
		t.Errorf("graph nodes = %v", nodes)
	}
	if got := refsIn(t, runCmd(t, "graph", "BETA-1", "--json"), "nodes"); len(got) == 0 || got[0] != "BETA-1" {
		t.Errorf("graph BETA-1 = %v", got)
	}
	if got := refsIn(t, runCmd(t, "graph", "/BETA/knowledge/runbook", "--json"), "nodes"); len(got) == 0 {
		t.Errorf("graph from an address = %v", got)
	}
	_, err := execCmd("graph", "/BETA/boards/side")
	if ce := coreErr(t, err); ce.Code != "wrong_collection" {
		t.Errorf("graph from a board: %+v", ce)
	}
}

func TestBoardCommandsTakeAnAddress(t *testing.T) {
	pinEnv(t, "loose")
	seedProject(t, "BETA", "Side")
	runCmd(t, "board", "default", "/BETA/boards/side")
	if got := showBoard(t, "--project", "BETA").Slug; got != "side" {
		t.Errorf("default board = %s, want side", got)
	}
	runCmd(t, "board", "rename", "/BETA/boards/side", "Sidecar")
	if got := showBoard(t, "--project", "BETA"); got.Slug != "side" {
		t.Errorf("rename changed the slug: %+v", got)
	}
}

func TestArtifactsTakeNamesAndAddresses(t *testing.T) {
	targetEnv(t)
	file := filepath.Join(t.TempDir(), "screen shot.png")
	if err := os.WriteFile(file, []byte("not really an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	const want = "/ALPHA/artifacts/screen-shot.png"
	if got := refOf(t, "artifact", "add", file); got != want {
		t.Fatalf("added = %s", got)
	}
	runCmd(t, "artifact", "link", "screen-shot.png", "--card", "/ALPHA/cards/ALPHA-1")
	if got := refsIn(t, runCmd(t, "artifact", "ls", "--card", "ALPHA-1", "--json"), "artifacts"); len(got) != 1 || got[0] != want {
		t.Errorf("linked = %v", got)
	}
	runCmd(t, "artifact", "rm", want)
	if got := refsIn(t, runCmd(t, "artifact", "ls", "--json"), "artifacts"); len(got) != 0 {
		t.Errorf("after rm = %v", got)
	}
}

// The workspace is bound to one project. An address naming another project
// is refused here; a ref's prefix is core's to judge, because after a merge
// MONO holds cards named API-1.
func TestTheWorkspaceStaysInItsProject(t *testing.T) {
	app := &appCtx{Project: core.Project{Key: "ALPHA"}}
	for arg, want := range map[string]core.CardRef{
		"4":                   {Seq: 4},
		"ALPHA-3":             {Seq: 3, ProjectKey: "ALPHA"},
		"API-1":               {Seq: 1, ProjectKey: "API"},
		"/ALPHA/cards/API-1":  {Seq: 1, ProjectKey: "API", Project: "ALPHA"},
	} {
		got, err := tuiCardRef(app, arg)
		if err != nil || got != want {
			t.Errorf("tuiCardRef(%q) = %+v, %v", arg, got, err)
		}
	}
	_, err := tuiCardRef(app, "/BETA/cards/BETA-1")
	if ce := coreErr(t, err); ce.Code != "wrong_project" {
		t.Errorf("another project's address: %+v", ce)
	}
	_, err = tuiCardRef(app, "/ALPHA/cards/12")
	if ce := coreErr(t, err); ce.Code != "bad_path" {
		t.Errorf("a malformed address: %+v", ce)
	}
}

// Inside the workspace, core still refuses a prefix naming another project
// until the merge layer stores refs.
func TestTheWorkspaceLeavesPrefixesToCore(t *testing.T) {
	targetEnv(t)
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
	p, err := c.ProjectByKey(t.Context(), "ALPHA")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := tuiCardRef(&appCtx{Core: c, Project: p}, "BETA-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.GetCard(t.Context(), p.ID, ref)
	if ce := coreErr(t, err); ce.Code != "wrong_project" {
		t.Errorf("GetCard(BETA-1) in ALPHA: %+v", ce)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'EntryRefOpens|EntryAddressNames|VaultAddressIgnores|ReferenceFlags|LinkAndGraph|BoardCommandsTake|ArtifactsTake|Workspace'`
Expected: FAIL. The build fails because `tuiCardRef` is undefined.

- [ ] **Step 3: Add `tuiCardRef` to `internal/cli/target.go`**

```go
// tuiCardRef reads a card argument inside the workspace, which is bound to
// one project. An address must name that project. A qualified ref's prefix
// is not compared here: core's checkCardProject judges it, and the merge
// layer, where MONO legitimately holds API-1, replaces that check.
func tuiCardRef(app *appCtx, arg string) (core.CardRef, error) {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "/") {
		p, err := core.ParseAddress(arg, vpath.CollectionCards)
		if err != nil {
			return core.CardRef{}, err
		}
		if p.Project != app.Project.Key {
			return core.CardRef{}, core.ErrUsage("wrong_project",
				fmt.Sprintf("%s is in project %s; this workspace is %s", arg, p.Project, app.Project.Key),
				"trellis tui --project "+p.Project)
		}
	}
	return core.ParseCardRef(arg), nil
}
```

- [ ] **Step 4: Route the knowledge commands**

In `internal/cli/knowledge.go`, add `"github.com/mtch3n/trellis/internal/vpath"` to the imports.

1. **`newKnowledgeNewCmd`:** its `--board` takes a reference, so it can name the project. Replace `return withBoard(func(app *appCtx) error {` with the lines below, and pass `Board: refs[0]` instead of `Board: board`:

```go
			return withTargets([]refArg{{Collection: vpath.CollectionBoards, Value: board}}, func(app *appCtx, refs []string) error {
```

2. **`newKnowledgeShowCmd`:** replace `withBoard(func(app *appCtx) error {` with the line below, and use `ref` in place of `args[0]` inside the closure:

```go
			return withTarget(refArg{Collection: vpath.CollectionKnowledge, Value: args[0], NoProject: true}, func(app *appCtx, ref string) error {
```

3. **`newKnowledgeEditCmd`:** make the same change as `show`, including `NoProject: true`. Pass `ref` to `EditKnowledge`.

4. **`newKnowledgeRmCmd`, `newKnowledgeNominateCmd`, `newKnowledgeEscalateCmd`:** make the same change without `NoProject`. Pass `ref` to `DeleteKnowledge`, `NominateKnowledge` and `EscalateKnowledge`. Their JSON echoes keep `args[0]`. `requireHuman(args[0])` in escalate is unchanged.

5. **`newKnowledgePinCmd`:** both the entry and `--board` are references. Replace `return withBoard(func(app *appCtx) error {` with the lines below, and pass `refs[0]` and `refs[1]` to `UnpinKnowledge` and `PinKnowledge` in place of `args[0]` and `board`:

```go
			return withTargets([]refArg{
				{Collection: vpath.CollectionKnowledge, Value: args[0]},
				{Collection: vpath.CollectionBoards, Value: board},
			}, func(app *appCtx, refs []string) error {
```

`demote` and `verify` are unchanged: they open core without a project, and core now reads a vault address.

- [ ] **Step 5: Route link, graph, board, artifact and the workspace**

Replace `newLinkCmd`'s `RunE` in `internal/cli/link.go` with:

```go
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				if err := app.Core.LinkCardToDoc(cmd.Context(), app.Project.ID,
					core.ParseCardRef(ref), args[1]); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"from": args[0], "to": args[1]},
					func() string { return args[0] + " -> " + args[1] })
			})
		},
```

Change its `Use` to `"link <card> <entry[#anchor]>"`.

Replace `newGraphCmd`'s `Use` with `"graph <card|entry|artifact>"`, and its `RunE` with:

```go
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := strings.TrimSpace(args[0])
			run := func(collection string) func(*appCtx, string) error {
				return func(app *appCtx, ref string) error {
					startID, err := resolveEntity(cmd, app, collection, ref)
					if err != nil {
						return err
					}
					g, err := app.Core.Traverse(cmd.Context(), startID, depth, rels, reverse)
					if err != nil {
						return err
					}
					return Emit(cmd, g, func() string { return graphTree(g) })
				}
			}
			switch {
			case strings.HasPrefix(arg, "/"):
				target, _ := vpath.SplitAnchor(arg)
				p, err := vpath.Parse(strings.TrimSpace(target))
				if err != nil {
					return core.ErrUsage("bad_path", err.Error(), "trellis search <words>   # results carry valid addresses")
				}
				switch p.Collection {
				case vpath.CollectionCards, vpath.CollectionKnowledge, vpath.CollectionArtifacts:
				default:
					return core.ErrUsage("wrong_collection",
						arg+" names a project or a board; a graph starts from a card, an entry or an artifact",
						"trellis card ls")
				}
				return withTarget(refArg{Collection: p.Collection, Value: arg, NoProject: true}, run(p.Collection))
			case vpath.ValidCardRef(strings.ToUpper(arg)):
				// KEY-N is a card ref everywhere, and names its project.
				return withTarget(refArg{Collection: vpath.CollectionCards, Value: arg}, run(vpath.CollectionCards))
			}
			relative := run("")
			return withBoard(func(app *appCtx) error { return relative(app, arg) })
		},
```

Replace `resolveEntity` with:

```go
// resolveEntity finds where a walk starts. A collection says what the argument
// is; a relative argument of unknown kind tries an entry first, then a card.
func resolveEntity(cmd *cobra.Command, app *appCtx, collection, ref string) (string, error) {
	ctx := cmd.Context()
	switch collection {
	case vpath.CollectionKnowledge:
		doc, err := app.Core.LoadKnowledge(ctx, app.Project.ID, ref)
		return doc.ID, err
	case vpath.CollectionCards:
		card, err := app.Core.GetCard(ctx, app.Project.ID, core.ParseCardRef(ref))
		return card.ID, err
	case vpath.CollectionArtifacts:
		a, err := app.Core.ResolveArtifact(ctx, app.Project.ID, ref)
		return a.ID, err
	}
	if doc, err := app.Core.LoadKnowledge(ctx, app.Project.ID, ref); err == nil {
		return doc.ID, nil
	}
	card, err := app.Core.GetCard(ctx, app.Project.ID, core.ParseCardRef(ref))
	if err != nil {
		return "", core.ErrNotFound("not_found", "no card or knowledge entry "+ref,
			"trellis card ls   # or: trellis knowledge ls")
	}
	return card.ID, nil
}
```

Add `"github.com/mtch3n/trellis/internal/vpath"` to `link.go`'s imports.

In `internal/cli/board.go`, add the `vpath` import. Then:

- **`board default`:** replace the `RunE` body with:

```go
			return withTarget(refArg{Collection: vpath.CollectionBoards, Value: args[0]}, func(app *appCtx, name string) error {
				board, err := app.Core.SetDefaultBoard(cmd.Context(), app.Project.ID, name)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"board": board.Name}, func() string {
					return fmt.Sprintf("set default board to %q", board.Name)
				})
			})
```

- **`board rename`:** replace the body with the version below. `<from>` may be an address; `<to>` is a new name.

```go
		return withTarget(refArg{Collection: vpath.CollectionBoards, Value: args[0]}, func(app *appCtx, name string) error {
			b, err := app.Core.RenameBoard(cmd.Context(), app.Project.ID, name, args[1])
			if err != nil {
				return err
			}
			return Emit(cmd, b, func() string { return fmt.Sprintf("renamed board %q", b.Name) })
		})
```

- **`board rm`:** replace the body with:

```go
		return withTarget(refArg{Collection: vpath.CollectionBoards, Value: args[0]}, func(app *appCtx, name string) error {
			if err := app.Core.DeleteBoard(cmd.Context(), app.Project.ID, name, force); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"deleted": args[0]}, func() string { return "deleted " + args[0] })
		})
```

In `internal/cli/artifact.go`, add the `vpath` import, and change the four commands.

- **`artifact rm`:** replace the body with:

```go
		return withTarget(refArg{Collection: vpath.CollectionArtifacts, Value: args[0]}, func(app *appCtx, ref string) error {
			a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, ref)
			if err != nil {
				return err
			}
			if err := app.Core.DeleteArtifact(cmd.Context(), app.Project.ID, a.ID); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"deleted": a.Ref}, func() string { return "deleted " + a.Ref })
		})
```

- **`artifact add`:** the file is not a reference, but `--card` is. Replace the `RunE` body with:

```go
			return withTargets([]refArg{{Collection: vpath.CollectionCards, Value: card}}, func(app *appCtx, refs []string) error {
				artifact, err := app.Core.CreateArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				if refs[0] != "" {
					id, err := cardID(cmd, app, refs[0])
					if err != nil {
						return err
					}
					if err := app.Core.LinkArtifactToCard(cmd.Context(), app.Project.ID, id, artifact.ID); err != nil {
						return err
					}
				}
				return Emit(cmd, artifact, func() string { return artifact.Ref + "  " + artifact.Path })
			})
```

- **`artifact link`:** replace the `withBoard` call with:

```go
			return withTargets([]refArg{
				{Collection: vpath.CollectionArtifacts, Value: args[0]},
				{Collection: vpath.CollectionCards, Value: card},
			}, func(app *appCtx, refs []string) error {
				a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, refs[0])
				if err != nil {
					return err
				}
				id, err := cardID(cmd, app, refs[1])
				if err != nil {
					return err
				}
				if err := app.Core.LinkArtifactToCard(cmd.Context(), app.Project.ID, id, a.ID); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"artifact": a.Ref, "card": card}, func() string { return card + " -> " + a.Ref })
			})
```

  Its usage hint becomes `trellis artifact link <name> --card <card>`.

- **`artifact ls`:** replace the `RunE` body with the version below. A `--card` that names a project lists that project's artifacts.

```go
			return withTargets([]refArg{{Collection: vpath.CollectionCards, Value: card}}, func(app *appCtx, refs []string) error {
				cardIDValue := ""
				if refs[0] != "" {
					var err error
					if cardIDValue, err = cardID(cmd, app, refs[0]); err != nil {
						return err
					}
				}
				items, err := app.Core.ListArtifacts(cmd.Context(), app.Project.ID, cardIDValue)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"artifacts": items}, func() string {
					var b strings.Builder
					for _, item := range items {
						fmt.Fprintf(&b, "%s  %s  %s\n", item.Ref, item.Kind, item.Path)
					}
					return strings.TrimRight(b.String(), "\n")
				})
			})
```

In `internal/cli/tui.go`:

- **`/board`:** replace `app.Core.SelectBoard(ctx, app.Project.ID, arg)` with `boardNamed(ctx, app.Core, app.Project, arg)`.
- **`/show`, `/title`, `/body`, `/move`, `/note`:** replace

```go
		card, err := app.Core.GetCard(ctx, app.Project.ID, core.ParseCardRef(ref))
```

  with

```go
		cardRef, err := tuiCardRef(app, ref)
		if err != nil {
			return "", err
		}
		card, err := app.Core.GetCard(ctx, app.Project.ID, cardRef)
```

  Then replace the two later `core.ParseCardRef(ref)` calls, in `EditCard` and `MoveCard`, with `cardRef`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w internal/cli && go build ./... && go test ./internal/cli/ && go vet ./internal/cli/`
Expected: `ok`, including `TestHelpListsEveryCommand`.

- [ ] **Step 7: Commit**

```bash
git add internal/cli
git commit -m "feat(cli): every reference argument takes an address and follows its project

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---
### Task 9: The web reads addresses

**Files:**
- Create: `web/src/lib/vpath.ts`
- Test: `web/scripts/vpath.test.mjs`
- Modify:
  - `web/package.json`: add a `test` script
  - `web/src/lib/knowledge-graph.ts`
  - `web/src/pages/OverviewPage.tsx`
  - `web/src/components/wrappers/ProjectTimeline.tsx`
  - `web/src/lib/timeline-marks.ts`
  - `web/dist`: rebuilt

**Interfaces:**
- Consumes: the address grammar of Task 1, and the server's document refs from Task 2.
- Produces, in `web/src/lib/vpath.ts`:
  - `GLOBAL_KEY`
  - `type Collection`
  - `interface Address { project; collection; name }`
  - `parseAddress(text): Address | null`
  - `splitAnchor(text): [string, string]`
  - `docAddress(projectKey, global, slug): string`
- `knowledge-graph.ts` gains `parseReference(raw)`, and imports `GLOBAL_KEY` from `vpath.ts` instead of exporting its own.

Before starting, load the `toolchain-conventions` skill (pnpm). Run every command below from `web/`.

- [ ] **Step 1: Write the failing test**

`web/scripts/vpath.test.mjs`:

```js
// Node strips the types from the imported .ts module; no build step or test
// dependency is needed. The cases mirror internal/vpath/vpath_test.go.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { docAddress, parseAddress, splitAnchor } from '../src/lib/vpath.ts'

test('reads every collection as internal/vpath does', () => {
  const cases = {
    '/TRELLIS': { project: 'TRELLIS', collection: '', name: '' },
    '/trellis/boards/api': { project: 'TRELLIS', collection: 'boards', name: 'api' },
    '/mono/cards/mono-12': { project: 'MONO', collection: 'cards', name: 'MONO-12' },
    '/MONO/cards/my_app-1': { project: 'MONO', collection: 'cards', name: 'MY_APP-1' },
    '/MONO/knowledge/concurrency-model': { project: 'MONO', collection: 'knowledge', name: 'concurrency-model' },
    '/global/knowledge/conventions': { project: 'GLOBAL', collection: 'knowledge', name: 'conventions' },
    '/MONO/artifacts/Screen Shot.PNG': { project: 'MONO', collection: 'artifacts', name: 'Screen Shot.PNG' },
    '/MONO/boards/重構': { project: 'MONO', collection: 'boards', name: '重構' },
  }
  for (const [text, want] of Object.entries(cases)) {
    assert.deepEqual(parseAddress(text), want, text)
  }
})

test('rejects what internal/vpath rejects', () => {
  for (const text of [
    'TRELLIS', 'MONO/knowledge/x', '/', '/1ABC', '/MY_APP/cards/MY_APP-1', '/GLOBAL',
    '/GLOBAL/cards/X-1', '/MONO/boards', '/MONO/knowledge/a/b', '/MONO/boards/API',
    '/MONO/cards/12', '/MONO/cards/', '/MONO/knowledge/Bad Slug', '/MONO/artifacts/..',
    '/MONO/notes/x', '/MONO/knowledge/x#why',
  ]) {
    assert.equal(parseAddress(text), null, text)
  }
})

test('splits anchors and builds entry addresses', () => {
  assert.deepEqual(splitAnchor('/MONO/knowledge/design#a#b'), ['/MONO/knowledge/design', 'a#b'])
  assert.deepEqual(splitAnchor('design'), ['design', ''])
  assert.equal(docAddress('mono', false, 'design'), '/MONO/knowledge/design')
  assert.equal(docAddress('mono', true, 'design'), '/GLOBAL/knowledge/design')
})
```

In `web/package.json`, add this script after `"lint"`:

```json
    "test": "node --test scripts/vpath.test.mjs",
```

Run: `pnpm run test`
Expected: FAIL with `Cannot find module .../src/lib/vpath.ts`.

- [ ] **Step 2: Write `web/src/lib/vpath.ts`**

```ts
/**
 * Virtual addresses, mirroring internal/vpath: `/KEY`,
 * `/KEY/<collection>/<name>` and `/GLOBAL/knowledge/<slug>`. The web only
 * reads addresses the server wrote, but it validates them the same way, so a
 * malformed one never becomes a link.
 */

export const GLOBAL_KEY = 'GLOBAL'

export type Collection = 'boards' | 'cards' | 'knowledge' | 'artifacts'

export interface Address {
  project: string
  /** Empty when the address names the project itself. */
  collection: Collection | ''
  name: string
}

const COLLECTIONS: readonly string[] = ['boards', 'cards', 'knowledge', 'artifacts']
const KEY = /^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$/
const CARD_REF = /^[^/#\s-][^/#\s]*-[0-9]+$/
const DOC_SLUG = /^[a-z0-9]+(-[a-z0-9]+)*$/
const BOARD_SLUG = /^[\p{L}\p{N}]+(-[\p{L}\p{N}]+)*$/u

function validName(collection: Collection, name: string): string | null {
  switch (collection) {
    case 'boards':
      return BOARD_SLUG.test(name) && name === name.toLowerCase() ? name : null
    case 'cards': {
      const ref = name.toUpperCase()
      return CARD_REF.test(ref) ? ref : null
    }
    case 'knowledge':
      return DOC_SLUG.test(name) ? name : null
    case 'artifacts':
      return name !== '' && name !== '.' && name !== '..' && !/[\\\0]/.test(name) ? name : null
  }
}

/** Reads an absolute address, or returns null. Split an anchor off first. */
export function parseAddress(text: string): Address | null {
  const value = text.trim()
  if (!value.startsWith('/') || value.includes('#')) return null
  const parts = value.slice(1).split('/')
  const project = parts[0].toUpperCase()
  if (!KEY.test(project)) return null
  if (parts.length === 1) {
    return project === GLOBAL_KEY ? null : { project, collection: '', name: '' }
  }
  if (parts.length !== 3 || !COLLECTIONS.includes(parts[1])) return null
  const collection = parts[1] as Collection
  if (project === GLOBAL_KEY && collection !== 'knowledge') return null
  const name = validName(collection, parts[2])
  return name === null ? null : { project, collection, name }
}

/** Cuts a reference at its first '#'. */
export function splitAnchor(text: string): [target: string, anchor: string] {
  const at = text.indexOf('#')
  return at < 0 ? [text, ''] : [text.slice(0, at), text.slice(at + 1)]
}

/** A knowledge entry's address, exactly as the server writes it. */
export function docAddress(projectKey: string, global: boolean, slug: string): string {
  return global ? `/${GLOBAL_KEY}/knowledge/${slug}` : `/${projectKey.toUpperCase()}/knowledge/${slug}`
}
```

Run: `pnpm run test`
Expected: `# pass 3`, `# fail 0`.

- [ ] **Step 3: Use it at the four call sites**

In `web/src/lib/knowledge-graph.ts`:

1. Replace the header comment's sentence that begins "stored: code spans and fences are skipped" through "a stub on purpose." with:

```
 * stored: code spans and fences are skipped, a relative target is slugified
 * whole, `[[/KEY/knowledge/slug]]` names a project's entry, and
 * `[[/GLOBAL/knowledge/slug]]` reaches the vault. The backend resolves links
 * into other projects too, but this graph holds only this project and the
 * vault, so such a link is drawn as an outside stub labelled with its address.
```

2. Delete `export const GLOBAL_KEY = 'GLOBAL'`, and add near the top:

```ts
import { GLOBAL_KEY, docAddress, parseAddress, splitAnchor } from '@/lib/vpath'
```

3. Replace `parseWikilinks` with:

```ts
export function parseWikilinks(body: string): Reference[] {
  const seen = new Set<string>()
  const refs: Reference[] = []
  for (const match of body.replace(FENCE, '').matchAll(WIKILINK)) {
    const raw = match[1].trim() + (match[2] ?? '')
    const ref = parseReference(raw)
    if (!ref.slug || seen.has(raw)) continue
    seen.add(raw)
    refs.push(ref)
  }
  return refs
}

/**
 * One link target, read as `ParseReference` reads it: a relative target is
 * slugified whole, an address names its project, and an address that names no
 * entry keeps its text as the slug so it stays a stub.
 */
export function parseReference(raw: string): Reference {
  const target = splitAnchor(raw)[0].trim()
  if (!target.startsWith('/')) return { key: '', slug: slugify(target) }
  const address = parseAddress(target)
  if (address?.collection !== 'knowledge') return { key: '', slug: target }
  return { key: address.project, slug: address.name }
}
```

4. In `buildGraph`, a relative link falls back to the vault, as the backend's does. Replace the line `if (ref.key === '') target = nodeId(vault, ref.slug)` with:

```ts
      if (ref.key === '') {
        const own = nodeId(vault, ref.slug)
        target = nodes.has(own) ? own : nodeId(true, ref.slug)
      }
```

   Then replace the stub label line with:

```ts
        const label = ref.key ? docAddress(ref.key, ref.key === GLOBAL_KEY, ref.slug) : ref.slug
```

In `web/src/pages/OverviewPage.tsx`, add `import { docAddress } from '@/lib/vpath'`. Then replace the `titles` map's key expression, `` `${entry.global ? 'GLOBAL' : projectKey}/${entry.slug}` ``, with `docAddress(projectKey ?? '', Boolean(entry.global), entry.slug)`.

In `web/src/components/wrappers/ProjectTimeline.tsx`, add `import { parseAddress } from '@/lib/vpath'`. Then replace `encodeURIComponent(mark.ref.slice(mark.ref.indexOf('/') + 1))` with `encodeURIComponent(parseAddress(mark.ref)?.name ?? mark.ref)`.

In `web/src/lib/timeline-marks.ts`, change the comment on `Mark.ref` to `/** A card ref, or a knowledge address (`/KEY/knowledge/slug`). */`.

- [ ] **Step 4: Check and rebuild**

```bash
node scripts/ui-audit.js
pnpm install --frozen-lockfile
pnpm run test
pnpm run lint
pnpm run build
```

Expected:
- the audit passes;
- the tests pass;
- lint reports nothing new;
- `tsc -b` and `vite build` succeed, writing new hashed files into `web/dist`.

From the repository root, run: `go build ./... && go test ./internal/ui/`
Expected: `ok`. The server embeds the new `web/dist`.

- [ ] **Step 5: Commit**

```bash
git add web/src web/scripts/vpath.test.mjs web/package.json web/dist
git commit -m "feat(web): read knowledge addresses in the graph, the overview and the timeline

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

`git add web/dist` also stages the deletion of the old hashed assets; check `git status --short web/dist` before committing.

---
### Task 10: Teach the addresses, amend knowledge paths, and run the gates

**Files:**
- Modify:
  - `plugin/hooks/trellis_hook.py`
  - `plugin/integrations/claude_code/recall.py`
  - `scripts/tests/test_integrations.py`
  - `plugin/skills/trellis/SKILL.md`
  - `plugin/skills/writing-knowledge/SKILL.md`
  - `README.md`
  - `PRODUCT.md`
  - `CLAUDE.md`
  - `docs/superpowers/specs/2026-09-16-knowledge-paths-design.md`

**Interfaces:**
- Consumes: the behavior of Tasks 1–9.
- Produces: no code interfaces.

- [ ] **Step 1: Update the Python tests first**

In `scripts/tests/test_integrations.py`, change every mock document ref to the address form, and change the text assertion to match:

```bash
sed -i \
  -e 's#"TRELLIS/lease-renewal"#"/TRELLIS/knowledge/lease-renewal"#g' \
  -e 's#"T/x"#"/T/knowledge/x"#' \
  -e 's#"TRELLIS/pain-point-analysis-sept-2026-with-a-very-long-slug"#"/TRELLIS/knowledge/pain-point-analysis-sept-2026-with-a-very-long-slug"#' \
  -e 's#f"TRELLIS/entry-{n}"#f"/TRELLIS/knowledge/entry-{n}"#' \
  -e 's#"knowledge show <slug>"#"knowledge show <ref>"#' \
  scripts/tests/test_integrations.py
grep -n 'TRELLIS/\|T/x\|knowledge show' scripts/tests/test_integrations.py
```

Expected: every match now reads `/TRELLIS/knowledge/`, `/T/knowledge/x` or `knowledge show <ref>`.

Run: `python3 -B -m unittest discover -s scripts/tests`
Expected: FAIL in `test_injects_identifiers_and_recaps_never_bodies`, because `recall.py` still says `<slug>`.

- [ ] **Step 2: Update the plugin text**

In `plugin/integrations/claude_code/recall.py`, in `handle`, replace `` "`trellis knowledge show <slug>` or `trellis card show <ref>` only if it bears on the " `` with:

```python
            "`trellis knowledge show <ref>` or `trellis card show <ref>`, passing the ref as printed, only if it bears on the "
```

In `plugin/hooks/trellis_hook.py`, replace `search <text>; knowledge show <slug>.\n` with `search <text>; knowledge show <ref>.\n`.

Run: `python3 -B -m unittest discover -s scripts/tests`
Expected: `OK`.

- [ ] **Step 3: Update the skills**

In `plugin/skills/trellis/SKILL.md`, in the `# read` group of the command block, after `trellis knowledge show concurrency-model`, add:

```
trellis knowledge show /XPSCTL/knowledge/concurrency-model   # the ref search and recall print
trellis card show /OTHER/cards/OTHER-3        # an address names its own project; no --project
```

Replace the sentence `Use the slug the CLI returns rather than guessing one from the title.` with:

```markdown
Use the ref the CLI returns rather than guessing one from the title. Search and
recall print entries as addresses (`/KEY/knowledge/<slug>`, or
`/GLOBAL/knowledge/<slug>` for the vault); pass them back unchanged. `KEY-N`
and `/KEY/...` name their own project, so they work from any directory.
```

In `plugin/skills/writing-knowledge/SKILL.md`, append to the `## Linking` section:

```markdown
A bare `[[slug]]` means this project. To link another project's entry or a
vault entry, write its address: `[[/OTHER/knowledge/runbook]]`,
`[[/GLOBAL/knowledge/conventions]]`. The form `[[KEY/slug]]` is not a
cross-project link; lint reports it as a stub.
```

- [ ] **Step 4: Update the README, PRODUCT and CLAUDE**

In `README.md`, replace the `**Artifacts**` code block with:

```bash
trellis artifact add screenshot.png --card 12
trellis artifact link screenshot.png --card 12      # by name, or /KEY/artifacts/<name>
trellis artifact ls --card 12
```

Directly after that block, add:

```markdown
Every object has an address: `/KEY/boards/<slug>`, `/KEY/cards/KEY-12`,
`/KEY/knowledge/<slug>`, `/GLOBAL/knowledge/<slug>` and
`/KEY/artifacts/<name>`. Any command that takes a reference also takes an
address, and an address acts in its own project from any directory.
```

In `PRODUCT.md`, replace `` `TRELLIS/pain-point-analysis-sept-2026` `` with `` `/TRELLIS/knowledge/pain-point-analysis-sept-2026` ``.

In `CLAUDE.md`:

1. Replace the `internal/vpath` Architecture bullet with:

```markdown
- `internal/vpath` — the address grammar. A pin holds `/KEY` or
  `/KEY/boards/<slug>`; every object has `/KEY/<collection>/<name>`, and vault
  entries `/GLOBAL/knowledge/<slug>`.
```

2. Add this invariant after the "Projects are virtual" invariant:

```markdown
- **An address names its own project.** The CLI routes an absolute argument,
  or a qualified card ref, to the project it names. Core refuses an address
  naming any project but the one it acts in (`wrong_project`). A relative
  argument means what it always did. Card refs print as `KEY-N`; everything
  else prints as its address, built by `core.DocAddress` /
  `core.ArtifactAddress` and never by hand.
```

- [ ] **Step 5: Amend the knowledge-paths spec**

In `docs/superpowers/specs/2026-09-16-knowledge-paths-design.md`, replace everything from the heading `## Reference syntax: \`/\` is path, \`:\` is project` up to, but not including, `## Validation` with:

```markdown
## Reference syntax

Superseded by `2026-09-16-virtual-paths-design.md`, which ships first.
Cross-project and vault references are absolute addresses
(`[[/OTHER/knowledge/x]]`, `[[/GLOBAL/knowledge/x]]`), and the old `KEY/slug`
qualifier is already gone. A `/` inside a relative reference is therefore free
for directories: `[[deployment/rollback]]` means the path `deployment/rollback`
in the current project.

What this spec adds is that relative references and the name part of
`/KEY/knowledge/<name>` may contain directory segments. The knowledge-slug rule
in `internal/vpath` (`ValidDocSlug`) is relaxed to one slug per segment, and
`ParseReference` stops slugifying a relative target whole.
```

In the same file's Testing section, replace the bullet that begins `` - Reference syntax: `[[a/b]]` resolves as a path, `[[GLOBAL:x]]` `` with:

```markdown
- Reference syntax: `[[a/b]]` resolves as the path `a/b`, and
  `[[/KEY/knowledge/a/b]]` resolves the same entry by address.
```

- [ ] **Step 6: Run every gate**

```bash
go build ./...
go test ./...
go test -race ./internal/core/ ./internal/cli/
go vet ./...
gofmt -l .
staticcheck ./...
GOOS=windows go build ./...
GOOS=darwin go build ./...
python3 -B -m unittest discover -s scripts/tests
go build -o /tmp/trellis-it ./cmd/trellis && TRELLIS_TEST_BINARY=/tmp/trellis-it python3 -B -m unittest discover -s scripts/tests
(cd web && pnpm run test && pnpm run lint)
grep -rn "'/' || k.slug\|+ \"/\" + doc.Slug\|key+\"/\"+" internal
```

Expected:
- every command succeeds;
- `gofmt -l .` prints nothing;
- the final `grep` prints nothing, because no document ref is built by hand any more.

- [ ] **Step 7: Commit**

```bash
git add plugin scripts/tests README.md PRODUCT.md CLAUDE.md docs/superpowers/specs/2026-09-16-knowledge-paths-design.md
git commit -m "docs: teach addresses, and hand the reference syntax over from knowledge paths

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```
