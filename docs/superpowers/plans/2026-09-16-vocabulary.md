# One Word per Concept — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename Trellis so each concept has one word at every layer — storage, schema, Go, CLI, help text, flags, JSON, web, docs — and add a test that keeps it that way.

**Architecture:** A vocabulary test lands first and allowlists every current use of a retired word; each later task deletes lines from that allowlist. One Go migration moves the vault directories, rewrites file contents and renames the schema in a single step. Go identifiers are renamed with `gopls rename`, one concept per commit, so every commit builds and passes.

**Tech Stack:** Go 1.27, SQLite (modernc), goose v3 Go migrations, cobra, gopls; React/TypeScript for the web layer; Python for the plugin hook.

**Spec:** `docs/superpowers/specs/2026-09-16-vocabulary-design.md`. Read it first. §2 is the glossary, §5 the rename map, §6 the help-text table. Tasks below cite those sections instead of repeating them.

## Global Constraints

- **Starts last.** Do not begin until every other branch has merged into `feat/memory-groundwork`, including `wip/template` and `wip/comments`; the integrating session (trellis-2f) confirms this. Since 3564b86, this release's schema changes live in one Go migration, `internal/store/migrate_0014.go`.
- **Template and comment are already done.** `wip/template` made `template` the only classification (frontmatter `template:`, column `template`, `--template`, JSON `template`, no `note` template). `wip/comments` turned card notes into comments. This plan treats both as correct and touches neither.
- **The glossary skills plan has merged** (`2026-09-16-glossary-skills.md`). The vocabulary test will flag the old command names in its skills; Task 13 fixes them.
- **Trellis's glossary is a vault entry**, not a file in the repository. Task 14 creates it after the switch-over.
- **Branch binaries never touch the real home.** Any binary built from this repository — `go run`, `go build -o`, even for `--help` — runs only with `TRELLIS_HOME` set to a scratch directory. A branch binary once migrated the user's live `~/.trellis`.
- **Root-inject has merged** (TRELLIS-48, TRELLIS-36). `core.New` takes the storage root, `WithKBRoot` is gone, and `knowledge` and `artifact` store no `path`: a file's place is derived from root, project key and slug. Task 5 is written for that schema. If `internal/store/migrate_0014.go` still creates `knowledge.path`, stop and ask the integrator.
- **`CLAUDE.md` is local.** It is excluded in `.git/info/exclude`: never commit it. Its edits are made once, in Task 14, in the main checkout.
- **Web work belongs to the UI session.** Task 12's web half is handed to trellis-8d through the integrator; this plan's executor does the TUI half only.
- **One concept, one word, at every layer** (spec §1). No abbreviations of a glossary word.
- **No backward compatibility.** No aliases for old commands, flags, JSON keys, routes or config keys. Old spellings fail with the ordinary unknown-command or unknown-flag error.
- **Behaviour does not change.** Bugs listed in spec §10 stay out of this work.
- **Files are the source of truth.** Every file write goes through the atomic writer; anything derived must be rebuildable from the files.
- **CI runs Linux, macOS and Windows.** Before every commit: `go build ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`. `gofmt -l .` must print nothing. Before Task 14 is done, `staticcheck ./...` must be clean too.
- **Modern Go.** Use the `modern-go-guidelines:use-modern-go` skill before writing Go. The toolchain is go1.27.
- **Commit messages** end with `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.
- Read-oriented shell commands use absolute paths; `cd` is only for `go`, `git` and `pnpm`.

## File Structure

| Path | Responsibility |
|---|---|
| `internal/vocabulary/rules.go` | The retired words, as regexes, with what to write instead |
| `internal/vocabulary/rules_test.go` | Each rule matches what it should and nothing it should not |
| `internal/vocabulary/vocabulary_test.go` | Scans the tree; fails on any use not in the allowlist |
| `internal/vocabulary/allowlist.txt` | Current permitted uses; shrinks with each task |
| `internal/atomicfile/atomicfile.go` | `WriteTemp`, `Write` — moved from `internal/core/file_store.go` |
| `internal/atomicfile/syncdir_unix.go`, `syncdir_windows.go` | `SyncDir` — moved from `internal/core/fsync_*.go` |
| `internal/store/migrate_vocabulary.go` | The vocabulary migration: files, then schema |
| `internal/store/migrate_vocabulary_test.go` | Migration test, including both failure paths |
| `internal/address/` | Was `internal/vpath/` |
| `internal/core/entry.go` | Was `knowledge.go` |
| `internal/core/nomination.go`, `promote.go` | Split out of `pin.go` |
| `internal/core/duplicates.go` | Was `dupes.go` |
| `internal/core/template_resolve.go` | Was `template_verify.go` |
| `internal/cli/vault.go`, `vault_template.go` | Were `knowledge.go`, `knowledge_template.go` |

---

### Task 1: Start point and re-audit

**Files:**
- Modify: `docs/superpowers/specs/2026-09-16-vocabulary-design.md` (only if the audit finds new rows)

**Interfaces:**
- Produces: a branch `wip/vocabulary` in `/home/mtchen/Personal/trellis-worktrees/vocabulary`, cut from the tip of `feat/memory-groundwork`.

- [ ] **Step 1: Confirm the preconditions**

Ask the integrating session (trellis-2f) to confirm that every other branch has merged. Ask the integrator whether any decision in the spec's Decisions table has changed since the plan was written. If one has, edit spec §2 and §5 before going further.

- [ ] **Step 2: Create the worktree**

```bash
cd /home/mtchen/Personal/trellis
git fetch --all --prune
git worktree add -b wip/vocabulary /home/mtchen/Personal/trellis-worktrees/vocabulary feat/memory-groundwork
cd /home/mtchen/Personal/trellis-worktrees/vocabulary
go build ./... && go test ./...
```

Expected: build succeeds, all tests pass. If anything fails, stop and report it to the integrator; do not start the rename on a red tree.

- [ ] **Step 3: Re-run the help and flag audit**

```bash
cd /home/mtchen/Personal/trellis-worktrees/vocabulary
git grep -nE '(Use|Short|Long):|Flags\(\)\.|PersistentFlags\(\)\.' -- 'internal/cli/*.go' ':!internal/cli/*_test.go' > /tmp/vocab-cli-now.txt
git -C /home/mtchen/Personal/trellis-worktrees/vocabulary diff --stat 9bcd869 -- internal/cli | tail -1
```

Read `/tmp/vocab-cli-now.txt`. For every command or flag string that is not in spec §6 and uses a word the spec's §2 "Not" columns list, add a row to spec §6.

- [ ] **Step 4: Pick the migration number**

```bash
ls /home/mtchen/Personal/trellis-worktrees/vocabulary/internal/store/migrations /home/mtchen/Personal/trellis-worktrees/vocabulary/internal/store/
```

`vocabularyVersion` in Task 5 is the next number after the highest migration listed. It was 15 when this plan was written: 0001–0013 are SQL files, and 0014 is `migrate_0014.go`. Nothing else carries the number: the files are named `migrate_vocabulary*.go`, and the test starts at `vocabularyVersion-1`.

- [ ] **Step 5: Commit any spec change**

```bash
git add docs/superpowers/specs/2026-09-16-vocabulary-design.md
git commit -m "docs: bring the vocabulary audit up to the merged tree"
```

---

### Task 2: (moved to Task 14)

Trellis's own glossary entry is created after the switch-over, in Task 14 Step 3. Two reasons:

- Branch binaries never touch the real home.
- The installed binary has no `glossary` template until the switch-over reinstalls it.

Spec §2 is the reference while the rename runs.

---

### Task 3: The vocabulary test

**Files:**
- Create: `internal/vocabulary/rules.go`
- Create: `internal/vocabulary/rules_test.go`
- Create: `internal/vocabulary/vocabulary_test.go`
- Create: `internal/vocabulary/allowlist.txt` (generated)

**Interfaces:**
- Produces: `vocabulary.Retired []Rule`, `vocabulary.Find(text string) []string` (rule names the text breaks). Task 10 uses `Find`.
- Produces: `go test ./internal/vocabulary -update`, which rewrites `allowlist.txt` from the tree and keeps each line's `# why` comment.

- [ ] **Step 1: Write the rule test first**

`internal/vocabulary/rules_test.go`:

```go
package vocabulary

import (
	"slices"
	"testing"
)

func TestRules(t *testing.T) {
	cases := []struct {
		rule   string
		hits   []string
		misses []string
	}{
		{"kb", []string{"kbDir", "WithKBRoot", "the kb", "KBDocType"}, []string{"kbps"}},
		{"knowledge-base", []string{"knowledge base", "Knowledge-base", "knowledgebase"}, nil},
		{"knowledge-cli", []string{"trellis knowledge show x"}, []string{"trellis vault show x"}},
		{"knowledge-ident", []string{"ListKnowledge", "knowledge_fts", `"knowledge"`, "/p/:key/knowledge", "KnowledgePage"},
			[]string{"writing-knowledge", "what knowledge to keep"}},
		{"doc", []string{"loadDoc(", "docView", "docsByID", "DocType", "doc_id", "'doc'", "a doc.", "the docs are"},
			[]string{"docs/superpowers/x.md", "godoc", "Docker"}},
		{"document", []string{"a document", "Documents"}, []string{"documentation"}},
		{"type-field", []string{"doc types", "doc_type", `yaml:"type,omitempty"`}, []string{`yaml:"template"`}},
		{"escalate", []string{"escalate", "EscalateKnowledge", "escalation queue"}, []string{"promote"}},
		{"review", []string{"unreviewed", "review_by", "reviewed_at", "ReviewBy", "review clock"}, []string{"Review column"}},
		{"lease", []string{"lease_until", "RenewLease", "a lease", "Lease held"}, []string{"release", "Released", "please"}},
		{"owner", []string{"owner", "ownership", "holder", "held by"}, []string{"household"}},
		{"finding-umbrella", []string{"LintFinding", "no findings"}, []string{"template finding"}},
		{"dangling", []string{"a dangling link"}, nil},
		{"dupe", []string{"--dupes", "DupeCluster", "dupe"}, []string{"duplicate"}},
		{"orphan-file", []string{"--orphan-history", "orphanHistory", "stale orphan", "orphaned revision"}, []string{"orphan entry"}},
		{"stale-other", []string{"stale_leases", "Stale leases", "stale_documents"}, []string{"stale recap"}},
		{"ingestion-path", []string{"ingestion path", "ingestion paths"}, []string{"provenance"}},
		{"unarchive", []string{"UnarchiveCard", "unarchived"}, []string{"archive"}},
		{"feed", []string{"event feed", "EventFeed", "FeedEvent"}, []string{"feedback"}},
		{"noms", []string{`json:"noms"`}, []string{"nominations"}},
		{"pin-file", []string{".trellis pin", "pin walk", "PinFile", "FindPin", "ReadPin", "pin_path", "bad_pin",
			"resolve.Pin", "Pin this directory", "without pinning any directory"},
			[]string{"pin an entry", "PinEntry", "vault pins"}},
		{"vpath", []string{"vpath.Parse", "virtual path", "virtual-paths"}, []string{"address"}},
		{"attach", []string{"attach", "Attachments", "attached", "Detach"}, []string{"artifact"}},
		{"new-card-id", []string{"NewCardID()"}, []string{"NewID()"}},
		{"corpus", []string{"the corpus"}, nil},
		{"card-note", []string{"card note X-1", "CreateNote", `"note"`, "'note'"}, []string{"a note on style", "notes/", "Note:"}},
		{"verify-rule", []string{"verify: [sources]", `yaml:"verify`}, []string{"vault verify"}},
		{"old-address", []string{"/KEY/knowledge/slug", "/GLOBAL/knowledge/x"}, []string{"/KEY/vault/slug"}},
	}
	for _, c := range cases {
		for _, s := range c.hits {
			if !slices.Contains(Find(s), c.rule) {
				t.Errorf("rule %s should match %q", c.rule, s)
			}
		}
		for _, s := range c.misses {
			if slices.Contains(Find(s), c.rule) {
				t.Errorf("rule %s should not match %q", c.rule, s)
			}
		}
	}
}
```

- [ ] **Step 2: Run it to see it fail**

```bash
cd /home/mtchen/Personal/trellis-worktrees/vocabulary
go test ./internal/vocabulary -run TestRules
```

Expected: FAIL, `undefined: Find`.

- [ ] **Step 3: Write the rules**

`internal/vocabulary/rules.go`:

```go
// Package vocabulary holds the words Trellis has retired and the test that
// keeps them retired. The project's glossary entry says what each concept is
// called; this package says what it must no longer be called.
package vocabulary

import "regexp"

// Rule is one retired word or word family.
type Rule struct {
	Name    string // the id allowlist.txt uses
	Pattern *regexp.Regexp
	Use     string // what to write instead
}

// Retired is every rule, in the order the glossary introduces them.
var Retired = []Rule{
	{"kb", regexp.MustCompile(`(?i)\bkb\b|kb(dir|root|core|doctype)|withkbroot`), "vault"},
	{"knowledge-base", regexp.MustCompile(`(?i)knowledge[ -]?base`), "vault"},
	{"knowledge-cli", regexp.MustCompile(`trellis knowledge\b`), "trellis vault"},
	{"knowledge-ident", regexp.MustCompile(`\b\w*Knowledge\w*|\bknowledge_\w+|"knowledge"|/knowledge\b`), "entry or vault"},
	{"doc", regexp.MustCompile(`\bdocs?(?:$|[^/\w])|\bdocs?[A-Z]\w*|\b[a-z]+Docs?\b|\bDoc[A-Z]\w*|\bdoc_\w+|'doc'|"doc"`), "entry"},
	{"document", regexp.MustCompile(`(?i)\bdocuments?\b`), "entry"},
	{"type-field", regexp.MustCompile(`(?i)doc[ _]?types?\b|yaml:"type\b`), "template"},
	{"escalate", regexp.MustCompile(`(?i)escalat`), "promote"},
	{"review", regexp.MustCompile(`(?i)unreviewed|review_?by|reviewed_?at|review clock`), "verify, unverified, verify_by, verified_at"},
	{"lease", regexp.MustCompile(`(?i)(?:^|[^ep])lease`), "claim"},
	{"owner", regexp.MustCompile(`(?i)\bowner(ship)?\b|\bholder\b|\bheld\b`), "claimant, claimed_by"},
	{"finding-umbrella", regexp.MustCompile(`LintFinding|\bfindings\b`), "diagnostic"},
	{"dangling", regexp.MustCompile(`(?i)\bdangling\b`), "stub"},
	{"dupe", regexp.MustCompile(`(?i)\bdupes?\b|dupecluster`), "duplicate"},
	{"orphan-file", regexp.MustCompile(`(?i)orphan[-_ ]?history|stale orphan|orphaned revision`), "leftover"},
	{"stale-other", regexp.MustCompile(`(?i)stale[_ ]leases?|stale[_ ]documents?`), "expired claims, unindexed entries"},
	{"ingestion-path", regexp.MustCompile(`(?i)ingestion paths?`), "provenance"},
	{"unarchive", regexp.MustCompile(`(?i)unarchiv`), "restore"},
	{"feed", regexp.MustCompile(`(?i)\bfeed\b|feedevent|eventfeed`), "event log"},
	{"noms", regexp.MustCompile(`\bnoms\b`), "nominations"},
	{"pin-file", regexp.MustCompile(`(?i)\.trellis pin|pin walk|\bpin(file|error|path|boundary)\b|\b(find|read|parse|parent|existing|write)pin\b|pin_(path|written|exists)|bad_pin|resolve\.pin\b|pin this directory|pinning any directory`), "marker"},
	{"vpath", regexp.MustCompile(`(?i)\bvpath\b|virtual[ -]paths?`), "address"},
	{"attach", regexp.MustCompile(`(?i)\battach(ment|ments|ed|es|ing)?\b|\bdetach`), "link, artifact"},
	{"new-card-id", regexp.MustCompile(`\bNewCardID\b`), "NewID"},
	{"corpus", regexp.MustCompile(`(?i)\bcorpus\b`), "vector index"},
	{"card-note", regexp.MustCompile(`card note\b|\b(Create|List|Delete)Notes?\b|"note"|'note'`), "comment"},
	{"verify-rule", regexp.MustCompile(`(?m)^verify:|yaml:"verify`), "resolve"},
	{"old-address", regexp.MustCompile(`/[A-Z][A-Z0-9]*/knowledge/`), "/KEY/vault/"},
}

// Find returns the names of the rules text breaks.
func Find(text string) []string {
	var names []string
	for _, r := range Retired {
		if r.Pattern.MatchString(text) {
			names = append(names, r.Name)
		}
	}
	return names
}

func ruleNamed(name string) Rule {
	for _, r := range Retired {
		if r.Name == name {
			return r
		}
	}
	return Rule{Name: name, Use: "(unknown rule)"}
}
```

- [ ] **Step 4: Run the rule test**

```bash
go test ./internal/vocabulary -run TestRules -v
```

Expected: PASS. If a case fails, fix the regex, not the case: the cases are the contract.

- [ ] **Step 5: Write the tree scan**

`internal/vocabulary/vocabulary_test.go`:

```go
package vocabulary

import (
	"bufio"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite allowlist.txt from the current tree")

const allowlistFile = "allowlist.txt"

var scanned = map[string]bool{
	".go": true, ".sql": true, ".ts": true, ".tsx": true, ".js": true, ".py": true,
	".md": true, ".json": true, ".sh": true, ".yaml": true, ".yml": true, ".html": true,
}

// skipped are prefixes, relative to the module root with forward slashes, that
// must keep the old words: history, and the files that define the rules.
var skipped = []string{
	".git/", ".superpowers/", "bin/", "node_modules/", "web/node_modules/", "web/dist/",
	"docs/superpowers/", "internal/vocabulary/",
	"internal/store/migrations/", "internal/store/migrate_",
	"go.sum", "web/pnpm-lock.yaml",
}

type key struct{ path, rule string }

type allowed struct {
	count int
	why   string
}

func TestRetiredWords(t *testing.T) {
	hits := scan(t, moduleRoot(t))
	if *update {
		writeAllowlist(t, hits, readAllowlist(t))
		return
	}
	allow := readAllowlist(t)
	var problems []string
	for k, lines := range hits {
		if n := len(lines); n > allow[k].count {
			problems = append(problems, fmt.Sprintf("%s lines %v: %d use(s) of retired %q beyond the allowlist; write %s",
				k.path, lines, n-allow[k].count, k.rule, ruleNamed(k.rule).Use))
		}
	}
	for k, a := range allow {
		if n := len(hits[k]); n < a.count {
			problems = append(problems, fmt.Sprintf("%s: allowlist permits %d %q, the tree has %d; run go test ./internal/vocabulary -update",
				k.path, a.count, k.rule, n))
		}
	}
	slices.Sort(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}

func isSkipped(rel string) bool {
	for _, s := range skipped {
		if strings.HasPrefix(rel, s) {
			return true
		}
	}
	return false
}

func scan(t *testing.T, root string) map[key][]int {
	t.Helper()
	hits := map[key][]int{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if isSkipped(rel + "/") {
				return filepath.SkipDir
			}
			return nil
		}
		if isSkipped(rel) || !scanned[filepath.Ext(path)] {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for line := 1; sc.Scan(); line++ {
			for _, r := range Retired {
				for range r.Pattern.FindAllStringIndex(sc.Text(), -1) {
					k := key{rel, r.Name}
					hits[k] = append(hits[k], line)
				}
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

// allowlist.txt: one "path<TAB>rule<TAB>count[<TAB># why]" per line; lines
// starting with # are comments.
func readAllowlist(t *testing.T) map[key]allowed {
	t.Helper()
	out := map[key]allowed{}
	raw, err := os.ReadFile(allowlistFile)
	if errors.Is(err, fs.ErrNotExist) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	lineNo := 0
	for line := range strings.SplitSeq(string(raw), "\n") {
		lineNo++
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, "\t", 4)
		if len(f) < 3 {
			t.Fatalf("%s:%d: want path<TAB>rule<TAB>count", allowlistFile, lineNo)
		}
		n, err := strconv.Atoi(f[2])
		if err != nil {
			t.Fatalf("%s:%d: %v", allowlistFile, lineNo, err)
		}
		a := allowed{count: n}
		if len(f) == 4 {
			a.why = f[3]
		}
		out[key{f[0], f[1]}] = a
	}
	return out
}

func writeAllowlist(t *testing.T, hits map[key][]int, old map[key]allowed) {
	t.Helper()
	keys := slices.SortedFunc(maps.Keys(hits), func(a, b key) int {
		return cmp.Or(strings.Compare(a.path, b.path), strings.Compare(a.rule, b.rule))
	})
	var b strings.Builder
	b.WriteString("# Uses of retired words the tree still has. Each task of the vocabulary\n")
	b.WriteString("# plan deletes lines; a line that stays says why after a fourth tab.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s\t%s\t%d", k.path, k.rule, len(hits[k]))
		if why := old[k].why; why != "" {
			b.WriteString("\t" + why)
		}
		b.WriteString("\n")
	}
	if err := os.WriteFile(allowlistFile, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 6: Run it to see it fail, then generate the allowlist**

```bash
go test ./internal/vocabulary -run TestRetiredWords 2>&1 | tail -3
go test ./internal/vocabulary -run TestRetiredWords -update
go test ./internal/vocabulary
wc -l internal/vocabulary/allowlist.txt
```

Expected: first run FAILS with many "beyond the allowlist" lines; after `-update`, PASS. The line count is the size of the work.

- [ ] **Step 7: Prove the test catches a new use**

```bash
printf 'package core\n\n// a kb comment\n' > internal/core/zz_vocab_probe.go
go test ./internal/vocabulary 2>&1 | grep zz_vocab_probe
rm internal/core/zz_vocab_probe.go
```

Expected: one line naming `internal/core/zz_vocab_probe.go` and rule `kb`.

- [ ] **Step 8: Commit**

```bash
git add internal/vocabulary
git commit -m "test: fail the build on a retired word"
```

---

### Task 4: One atomic writer for core and store

The migration in Task 5 writes vault files from `internal/store`, which cannot import `internal/core`. Moving the writer to a leaf package keeps "writes go through the atomic writer" true with one implementation.

**Files:**
- Create: `internal/atomicfile/atomicfile.go`
- Create: `internal/atomicfile/atomicfile_test.go`
- Create: `internal/atomicfile/syncdir_unix.go`, `internal/atomicfile/syncdir_windows.go`
- Delete: `internal/core/fsync_unix.go`, `internal/core/fsync_windows.go`
- Modify: `internal/core/file_store.go` (drop `writeTemp`, `writeAtomic`)
- Modify: every caller of `writeAtomic`, `writeTemp`, `syncDirectory` in `internal/core`

**Interfaces:**
- Produces: `atomicfile.WriteTemp(dir, name string, data []byte) (string, error)`, `atomicfile.Write(path string, data []byte, replace bool) error`, `atomicfile.SyncDir(path string) error`.

- [ ] **Step 1: Write the test**

`internal/atomicfile/atomicfile_test.go`:

```go
package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReplaces(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, []byte("new"), true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "new" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteWithoutReplaceRefusesAnExistingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, []byte("new"), false); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("want ErrExist, got %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("a temp file was left behind: %v", entries)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

```bash
go test ./internal/atomicfile
```

Expected: FAIL, `undefined: Write`.

- [ ] **Step 3: Move the code**

Create `internal/atomicfile/atomicfile.go` with the package doc below, then move `writeTemp` and `writeAtomic` from `internal/core/file_store.go` into it unchanged except for their names (`WriteTemp`, `Write`) and the call to `syncDirectory`, which becomes `SyncDir`:

```go
// Package atomicfile writes files so a crash leaves either the old bytes or
// the new ones, never a mix. Both core and the store's migrations write vault
// files, and the temp-file dance must exist once.
package atomicfile
```

Move `internal/core/fsync_unix.go` to `internal/atomicfile/syncdir_unix.go` and `internal/core/fsync_windows.go` to `internal/atomicfile/syncdir_windows.go`, changing `package core` to `package atomicfile` and `syncDirectory` to `SyncDir`:

```bash
git mv internal/core/fsync_unix.go internal/atomicfile/syncdir_unix.go
git mv internal/core/fsync_windows.go internal/atomicfile/syncdir_windows.go
sed -i 's/^package core$/package atomicfile/; s/\bsyncDirectory\b/SyncDir/' internal/atomicfile/syncdir_*.go
```

Point core at it:

```bash
grep -rlE '\b(writeAtomic|writeTemp|syncDirectory)\(' internal/core --include='*.go' \
  | xargs sed -i -E 's/\bwriteAtomic\(/atomicfile.Write(/g; s/\bwriteTemp\(/atomicfile.WriteTemp(/g; s/\bsyncDirectory\(/atomicfile.SyncDir(/g'
go run golang.org/x/tools/cmd/goimports@latest -w internal/core
```

If `goimports` is not wanted, add `"github.com/mtch3n/trellis/internal/atomicfile"` to each edited file's imports by hand.

`CLAUDE.md` names `writeAtomic` and `fsync_*.go`; Task 14 updates it.

- [ ] **Step 4: Run everything**

```bash
go build ./... && go test ./internal/atomicfile ./internal/core/... && GOOS=windows go build ./...
go test ./internal/vocabulary -update && go test ./internal/vocabulary
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A internal/atomicfile internal/core internal/vocabulary/allowlist.txt
git commit -m "refactor: one atomic writer, shared by core and store"
```

---

### Task 5: The migration, and the data contract

Everything the migration rewrites and everything code writes into those same places change in this one commit, or the tree is red in between: table and column names in SQL strings, `db:` tags, the vault directory name, the address collection, the template rule key, and the stored words (event actions, entity names, link types and relations, the config key). **Go identifiers, command names, flags and JSON keys do not change here.**

**Files:**
- Create: `internal/store/migrate_vocabulary.go`
- Create: `internal/store/migrate_vocabulary_test.go`
- Modify: `internal/store/migrate_0014.go` (`foreignKeyCheck` takes the migration's name)
- Modify: `internal/store/migrate_0014_test.go` (`migrateUp` stops at 14)
- Modify: SQL strings and `db:` tags across `internal/core`, `internal/ui`, `internal/retrieval`, `internal/cli`
- Modify: the function that derives an entry's directory from the root (`kbDir` today): the vault directory name
- Modify: `internal/vpath/vpath.go` (the `CollectionKnowledge` value)
- Modify: `internal/core/template.go` (`yaml:"verify"` → `yaml:"resolve"`)
- Modify: `internal/core/templates/*.md` (any `verify:` rule)
- Modify: `internal/config/*.go` (`lease.ttl` → `claim.ttl`)
- Modify: tests that assert any of the above

**Interfaces:**
- Consumes: `atomicfile.Write`, `atomicfile.SyncDir` (Task 4).
- Produces: schema `entry(verify_by, verified_at, …)` (its `template` column is unchanged), `entry_label(entry_id)`, `entry_tag(entry_id)`, `entry_fts`, `entry_search_state`, `pin(entry_id)`, `nomination(entry_id)`, `card(claimed_by, claim_until)`; link types `'entry'`; relation `'cites'`; event entity `'entry'`; event actions `promoted`, `restored`, `privatized`, `set_default`; directories `projects/<KEY>/vault`, `global/vault`; addresses `/KEY/vault/<slug>`; template rule `resolve:`; config key `claim.ttl`.

- [ ] **Step 1: Write the migration test**

`internal/store/migrate_vocabulary_test.go`. It builds a storage root with the database inside it, as production does, at the schema version just before this migration:

```go
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
)

const entryBefore = "---\ntitle: A\ntemplate: runbook\nsources:\n  - /TR/knowledge/b\n---\nSee [[/TR/knowledge/b]], [[/TR/knowledge/ops/rollback]] and /home/me/knowledge/notes.\n"

func sha(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// rootBefore is a storage root holding trellis.db just before this migration:
// one project TR with one entry, one card and comment, one global entry, one
// template and one revision. The schema is as migration 0014 left it:
// knowledge.template, a comment table, project(id, key, name, created_at).
func rootBefore(t *testing.T) (string, *sqlx.DB) {
	t.Helper()
	root := t.TempDir()
	db, err := connect(filepath.Join(root, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := goose.UpTo(db.DB, "migrations", vocabularyVersion-1); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "projects", "TR", "knowledge", "a.md")
	writeFile(t, entry, entryBefore)
	writeFile(t, filepath.Join(root, "projects", "TR", "knowledge", ".a.md", "1.md"), "---\ntitle: A\ntemplate: runbook\n---\nsee /TR/knowledge/b\n")
	writeFile(t, filepath.Join(root, "global", "knowledge", "g.md"), "---\ntitle: G\ntemplate: decision\n---\nSee /TR/knowledge/a\n")
	writeFile(t, filepath.Join(root, "templates", "cited.md"), "---\nenforce: reject\nverify: [sources]\n---\n# {{title}}\n")
	writeFile(t, filepath.Join(root, "config.yaml"), "lease:\n  ttl: 30m\n")
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO project (id, key, name, created_at) VALUES ('p1', 'TR', 'TR', 1)`, nil},
		{`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES ('b1', 'p1', 'main', 'main', 1, 1)`, nil},
		{`INSERT INTO column_ (id, board_id, name, position) VALUES ('c1', 'b1', 'backlog', 0)`, nil},
		{`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, body_md, owner, lease_until, created_at, updated_at)
		  VALUES ('k1', 'p1', 'b1', 1, 'c1', 'a', 'First', 'see /TR/knowledge/a', 'agent:x', 99, 1, 1)`, nil},
		{`INSERT INTO comment (id, card_id, actor, body_md, created_at) VALUES ('m1', 'k1', 'agent:x', 'read /TR/knowledge/a', 1)`, nil},
		{`INSERT INTO knowledge (id, project_id, slug, title, template, recap, recap_hash, content_hash, mtime, size, created_at, updated_at)
		  VALUES ('e1', 'p1', 'a', 'A', 'runbook', 'do A', ?, ?, 1, 1, 1, 1)`, []any{sha(entryBefore), sha(entryBefore)}},
		{`INSERT INTO knowledge (id, project_id, slug, title, template, content_hash, mtime, size, global, review_by, reviewed_at, created_at, updated_at)
		  VALUES ('e2', 'p1', 'g', 'G', 'decision', 'x', 1, 1, 1, 5, 4, 1, 1)`, nil},
		{`INSERT INTO pin (id, knowledge_id, created_at) VALUES ('pn1', 'e1', 1)`, nil},
		{`INSERT INTO nomination (id, knowledge_id, actor, reason, created_at) VALUES ('nm1', 'e1', 'agent:x', 'r', 1)`, nil},
		{`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES ('card', 'k1', 'doc', 'e1', '/TR/knowledge/a', 'documents')`, nil},
		{`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES ('doc', 'e1', 'doc', NULL, '/TR/knowledge/b', 'wikilink')`, nil},
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action) VALUES (1, 'h', 'knowledge', 'e2', 'escalated')`, nil},
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action) VALUES (1, 'h', 'card', 'k1', 'unarchived')`, nil},
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action, field, old_value) VALUES (1, 'h', 'card', 'k1', 'stolen', 'owner', 'agent:y')`, nil},
		{`INSERT INTO project_config (project_id, key, value, updated_at) VALUES ('p1', 'lease.ttl', '10m', 1)`, nil},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	return root, db
}

var retiredSchema = regexp.MustCompile(`(?i)knowledge|\bdoc_(type|id)\b|\bowner\b|lease_until|review_by|reviewed_at`)

func TestVocabularyMigration(t *testing.T) {
	root, db := rootBefore(t)
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(root, "projects", "TR", "vault", "a.md")
	got := readFile(t, moved)
	want := strings.ReplaceAll(entryBefore, "/TR/knowledge/", "/TR/vault/")
	if got != want {
		t.Errorf("entry file:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(got, "/home/me/knowledge/notes") {
		t.Error("a filesystem path inside a body was rewritten")
	}
	if r := readFile(t, filepath.Join(root, "projects", "TR", "vault", ".a.md", "1.md")); !strings.Contains(r, "/TR/knowledge/b") {
		t.Errorf("a revision was rewritten: %q", r)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "TR", "knowledge")); !os.IsNotExist(err) {
		t.Errorf("old project vault still there: %v", err)
	}
	if g := readFile(t, filepath.Join(root, "global", "vault", "g.md")); !strings.Contains(g, "See /TR/vault/a") {
		t.Errorf("global entry: %q", g)
	}
	if tpl := readFile(t, filepath.Join(root, "templates", "cited.md")); !strings.Contains(tpl, "\nresolve: [sources]\n") {
		t.Errorf("template: %q", tpl)
	}
	if cfg := readFile(t, filepath.Join(root, "config.yaml")); cfg != "claim:\n  ttl: 30m\n" {
		t.Errorf("config.yaml: %q", cfg)
	}

	var objects []struct {
		Name string `db:"name"`
		SQL  string `db:"sql"`
	}
	if err := db.Select(&objects, `SELECT name, COALESCE(sql, '') AS sql FROM sqlite_master`); err != nil {
		t.Fatal(err)
	}
	for _, o := range objects {
		if retiredSchema.MatchString(o.Name) || retiredSchema.MatchString(o.SQL) {
			t.Errorf("schema object %s still uses a retired word:\n%s", o.Name, o.SQL)
		}
	}

	var row struct {
		Template    string `db:"template"`
		ContentHash string `db:"content_hash"`
		RecapHash   string `db:"recap_hash"`
	}
	if err := db.Get(&row, `SELECT template, content_hash, recap_hash FROM entry WHERE id = 'e1'`); err != nil {
		t.Fatal(err)
	}
	if row.Template != "runbook" || row.ContentHash != sha(want) || row.RecapHash != sha(want) {
		t.Errorf("entry row = %+v; want template runbook and both hashes %s", row, sha(want))
	}
	var verifyBy int
	if err := db.Get(&verifyBy, `SELECT verify_by FROM entry WHERE id = 'e2'`); err != nil || verifyBy != 5 {
		t.Errorf("verify_by = %d, %v", verifyBy, err)
	}

	checks := []struct{ q, want string }{
		{`SELECT claimed_by || '/' || claim_until FROM card WHERE id = 'k1'`, "agent:x/99"},
		{`SELECT body_md FROM card WHERE id = 'k1'`, "see /TR/vault/a"},
		{`SELECT body_md FROM comment WHERE id = 'm1'`, "read /TR/vault/a"},
		{`SELECT group_concat(x, ' ') FROM (SELECT from_type || '>' || to_type || ':' || rel || ':' || to_raw AS x
		   FROM link ORDER BY from_type)`,
			"card>entry:cites:/TR/vault/a entry>entry:wikilink:/TR/vault/b"},
		{`SELECT group_concat(x, ' ') FROM (SELECT entity_type || ':' || action || ':' || COALESCE(field, '') AS x
		   FROM event ORDER BY seq)`,
			"entry:promoted: card:restored: card:stolen:claimed_by"},
		{`SELECT entry_id FROM pin`, "e1"},
		{`SELECT entry_id FROM nomination`, "e1"},
		{`SELECT key FROM project_config`, "claim.ttl"},
	}
	for _, c := range checks {
		var got string
		if err := db.Get(&got, c.q); err != nil {
			t.Fatalf("%s: %v", c.q, err)
		}
		if got != c.want {
			t.Errorf("%s\n got %q\nwant %q", c.q, got, c.want)
		}
	}
}

func TestVocabularyMigrationRefusesTwoVaults(t *testing.T) {
	root, db := rootBefore(t)
	writeFile(t, filepath.Join(root, "projects", "TR", "vault", "stray.md"), "x")
	err := goose.Up(db.DB, "migrations")
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("want a 'both … exist' error, got %v", err)
	}
	if readFile(t, filepath.Join(root, "projects", "TR", "knowledge", "a.md")) != entryBefore {
		t.Error("the entry changed although the migration refused")
	}
}

func TestVocabularyMigrationPutsFilesBackWhenTheSchemaFails(t *testing.T) {
	root, db := rootBefore(t)
	mustExec(t, db, `CREATE TABLE entry (x INTEGER)`) // makes the table rename fail
	if err := goose.Up(db.DB, "migrations"); err == nil {
		t.Fatal("want the schema step to fail")
	}
	got := readFile(t, filepath.Join(root, "projects", "TR", "knowledge", "a.md"))
	if got != entryBefore {
		t.Errorf("entry not restored:\n%s", got)
	}
	if tpl := readFile(t, filepath.Join(root, "templates", "cited.md")); !strings.Contains(tpl, "\nverify: [sources]\n") {
		t.Errorf("template not restored: %q", tpl)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "TR", "vault")); !os.IsNotExist(err) {
		t.Errorf("new vault directory left behind: %v", err)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

```bash
go test ./internal/store -run TestVocabularyMigration
```

Expected: FAIL. Files are still under `knowledge/`, and `no such table: entry`.

`mustExec` comes from `migrate_0014_test.go`. `openAtVersion` there is not used, because this test needs the database inside a storage root.

- [ ] **Step 3: Write the migration**

`internal/store/migrate_vocabulary.go`:

```go
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mtch3n/trellis/internal/atomicfile"
	"github.com/pressly/goose/v3"
)

// vocabularyVersion is this migration's goose version: the next free number
// when the work starts (Task 1 Step 4). It is the only place the number lives.
const vocabularyVersion = 15

func init() {
	goose.AddNamedMigrationNoTxContext(fmt.Sprintf("%04d_vocabulary.go", vocabularyVersion),
		renameVocabulary, refuseVocabularyDown)
}

// renameVocabulary moves Trellis onto the glossary's words: the
// vault directories, what the files say, and the schema.
//
// Files go first and the schema last, in one transaction. A schema failure
// puts every file back, so the old binary still finds what it left. goose
// records the version after this returns; a process that dies in between
// leaves the schema renamed and the version unrecorded, so the next start
// finds no knowledge table and succeeds without doing anything.
func renameVocabulary(ctx context.Context, db *sql.DB) error {
	var pending int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'knowledge'`).Scan(&pending); err != nil {
		return err
	}
	if pending == 0 {
		return nil
	}
	root, err := storageRoot(ctx, db)
	if err != nil {
		return err
	}
	var fw fileWork
	if root != "" {
		if err := fw.moveVaults(root); err != nil {
			return errors.Join(err, fw.undo())
		}
		if err := fw.rewrite(root); err != nil {
			return errors.Join(err, fw.undo())
		}
	}
	if err := renameSchema(ctx, db, root, fw.rewritten); err != nil {
		return errors.Join(err, fw.undo())
	}
	return nil
}

func refuseVocabularyDown(context.Context, *sql.DB) error {
	return fmt.Errorf("migration %04d is not reversible; restore a backup taken with `trellis backup`", vocabularyVersion)
}

// storageRoot is the directory holding the database, which is the storage
// root in every real install. An in-memory database has no files to move.
func storageRoot(ctx context.Context, db *sql.DB) (string, error) {
	var file string
	if err := db.QueryRowContext(ctx,
		`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&file); err != nil {
		return "", err
	}
	if file == "" {
		return "", nil
	}
	return filepath.Dir(file), nil
}

type dirMove struct{ from, to string }

type fileChange struct {
	path     string
	old      []byte
	oldHash  string
	newHash  string
	newSize  int64
	newMTime int64
}

// fileWork remembers everything it did, so undo can reverse it.
type fileWork struct {
	moved     []dirMove
	rewritten []fileChange
}

func (fw *fileWork) moveVaults(root string) error {
	olds, err := filepath.Glob(filepath.Join(root, "projects", "*", "knowledge"))
	if err != nil {
		return err
	}
	olds = append(olds, filepath.Join(root, "global", "knowledge"))
	for _, from := range olds {
		to := filepath.Join(filepath.Dir(from), "vault")
		if _, err := os.Stat(from); errors.Is(err, fs.ErrNotExist) {
			continue // nothing there, or moved by an attempt that died
		} else if err != nil {
			return err
		}
		if _, err := os.Stat(to); err == nil {
			return fmt.Errorf("both %s and %s exist; merge them by hand, then start trellis again", from, to)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		fw.moved = append(fw.moved, dirMove{from, to})
		if err := atomicfile.SyncDir(filepath.Dir(from)); err != nil {
			return err
		}
	}
	return nil
}

func (fw *fileWork) rewrite(root string) error {
	vaults, err := filepath.Glob(filepath.Join(root, "projects", "*", "vault"))
	if err != nil {
		return err
	}
	vaults = append(vaults, filepath.Join(root, "global", "vault"))
	for _, vault := range vaults {
		err := filepath.WalkDir(vault, func(path string, d fs.DirEntry, err error) error {
			if path == vault && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipDir
			}
			if err != nil {
				return err
			}
			if strings.HasPrefix(d.Name(), ".") && path != vault {
				if d.IsDir() {
					return filepath.SkipDir // a revision directory keeps what it said
				}
				return nil // a temp file
			}
			if d.IsDir() || filepath.Ext(path) != ".md" {
				return nil
			}
			return fw.edit(path, rewriteAddresses)
		})
		if err != nil {
			return err
		}
	}
	templates, err := filepath.Glob(filepath.Join(root, "templates", "*.md"))
	if err != nil {
		return err
	}
	for _, path := range templates {
		if err := fw.edit(path, func(s string) string { return renameTopLevelKey(s, "verify", "resolve", true) }); err != nil {
			return err
		}
	}
	config := filepath.Join(root, "config.yaml")
	if _, err := os.Stat(config); err == nil {
		return fw.edit(config, func(s string) string { return renameTopLevelKey(s, "lease", "claim", false) })
	}
	return nil
}

func (fw *fileWork) edit(path string, change func(string) string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next := change(string(raw))
	if next == string(raw) {
		return nil
	}
	if err := atomicfile.Write(path, []byte(next), true); err != nil {
		return err
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	fw.rewritten = append(fw.rewritten, fileChange{
		path: path, old: raw, oldHash: hash(string(raw)), newHash: hash(next),
		newSize: st.Size(), newMTime: st.ModTime().UnixMilli(),
	})
	return nil
}

// undo reverses the file work, newest first. Moves are undone after the
// rewrites, so each rewrite is put back at the path it was made at.
func (fw *fileWork) undo() error {
	var errs []error
	for i := len(fw.rewritten) - 1; i >= 0; i-- {
		errs = append(errs, atomicfile.Write(fw.rewritten[i].path, fw.rewritten[i].old, true))
	}
	for i := len(fw.moved) - 1; i >= 0; i-- {
		errs = append(errs, os.Rename(fw.moved[i].to, fw.moved[i].from))
	}
	return errors.Join(errs...)
}

// hash is core.ContentHash: sha256 of the whole file, hex.
func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// renameTopLevelKey renames a column-zero "from:" key. With header set, only
// inside a leading "---" block, leaving the body alone. It does nothing when
// "to:" is already present, so a second run cannot produce a duplicate key.
func renameTopLevelKey(s, from, to string, header bool) string {
	nl := "\n"
	if strings.Contains(s, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(s, nl)
	start, end := 0, len(lines)
	if header {
		if len(lines) == 0 || lines[0] != "---" {
			return s
		}
		start = 1
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				end = i
				break
			}
		}
	}
	at, value := -1, ""
	for i := start; i < end; i++ {
		if strings.HasPrefix(lines[i], to+":") {
			return s
		}
		if rest, ok := strings.CutPrefix(lines[i], from+":"); ok && at < 0 {
			at, value = i, rest
		}
	}
	if at < 0 {
		return s
	}
	lines[at] = to + ":" + value
	return strings.Join(lines, nl)
}

// oldAddress is an address written before the rename, at the start of a
// token. Keys are matched upper-case only, as Trellis prints them, so a
// filesystem path such as /home/me/knowledge/ is never touched. A hand-written
// lower-case address is left alone; lint reports it as a stub.
var oldAddress = regexp.MustCompile("(?m)(^|[\\s\\[(\"'`,:])(/[A-Z][A-Z0-9]*)/knowledge/")

func rewriteAddresses(s string) string {
	return oldAddress.ReplaceAllString(s, "${1}${2}/vault/")
}

func renameSchema(ctx context.Context, db *sql.DB, root string, rewritten []fileChange) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // a no-op once committed

	for _, q := range []string{
		`DROP TRIGGER knowledge_search_ad`,
		`ALTER TABLE knowledge RENAME TO entry`,
		`ALTER TABLE entry RENAME COLUMN review_by TO verify_by`,
		`ALTER TABLE entry RENAME COLUMN reviewed_at TO verified_at`,
		`ALTER TABLE knowledge_label RENAME TO entry_label`,
		`ALTER TABLE entry_label RENAME COLUMN doc_id TO entry_id`,
		`ALTER TABLE knowledge_tag RENAME TO entry_tag`,
		`ALTER TABLE entry_tag RENAME COLUMN doc_id TO entry_id`,
		`ALTER TABLE knowledge_fts RENAME TO entry_fts`,
		`ALTER TABLE knowledge_search_state RENAME TO entry_search_state`,
		`ALTER TABLE pin RENAME COLUMN knowledge_id TO entry_id`,
		`ALTER TABLE nomination RENAME COLUMN knowledge_id TO entry_id`,
		`ALTER TABLE card RENAME COLUMN owner TO claimed_by`,
		`ALTER TABLE card RENAME COLUMN lease_until TO claim_until`,
		`DROP INDEX knowledge_project`,
		`CREATE INDEX entry_project ON entry(project_id, updated_at DESC)`,
		`DROP INDEX knowledge_board`,
		`CREATE INDEX entry_board ON entry(board_id)`,
		`DROP INDEX knowledge_global`,
		`CREATE INDEX entry_global ON entry(global) WHERE global = 1`,
		`DROP INDEX knowledge_label_label`,
		`CREATE INDEX entry_label_label ON entry_label(label_id)`,
		`DROP INDEX knowledge_tag_tag`,
		`CREATE INDEX entry_tag_tag ON entry_tag(tag_id)`,
		`DROP INDEX knowledge_provenance`,
		`CREATE INDEX entry_provenance ON entry(provenance)`,
		`CREATE TRIGGER entry_search_ad AFTER DELETE ON entry BEGIN
		     DELETE FROM entry_fts WHERE rowid = OLD.rowid;
		     DELETE FROM entry_search_state WHERE rowid = OLD.rowid;
		 END`,
		`UPDATE link SET from_type = 'entry' WHERE from_type = 'doc'`,
		`UPDATE link SET to_type = 'entry' WHERE to_type = 'doc'`,
		`UPDATE link SET rel = 'cites' WHERE rel = 'documents'`,
		`UPDATE event SET entity_type = 'entry' WHERE entity_type = 'knowledge'`,
		`UPDATE event SET action = 'promoted' WHERE action = 'escalated'`,
		`UPDATE event SET action = 'restored' WHERE action = 'unarchived'`,
		`UPDATE event SET action = 'privatized' WHERE action = 'privatised'`,
		`UPDATE event SET action = 'set_default' WHERE action = 'default'`,
		`UPDATE event SET field = 'claimed_by' WHERE field = 'owner'`,
		`UPDATE event SET field = 'claim_until' WHERE field = 'lease_until'`,
		`UPDATE event SET field = 'verify_by' WHERE field = 'review_by'`,
		`UPDATE project_config SET key = 'claim.ttl' WHERE key = 'lease.ttl'`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("%s: %w", firstLine(q), err)
		}
	}

	if err := carryHashes(ctx, tx, root, rewritten); err != nil {
		return err
	}
	for _, c := range [][2]string{{"card", "body_md"}, {"comment", "body_md"}, {"link", "to_raw"}} {
		if err := rewriteColumn(ctx, tx, c[0], c[1], rewriteAddresses); err != nil {
			return err
		}
	}

	if err := foreignKeyCheck(ctx, tx, fmt.Sprintf("%04d", vocabularyVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// carryHashes keeps a rewritten file from reading as an external edit. Rows
// store no path: an entry's file is <root>/projects/<KEY>/vault/<slug>.md, or
// <root>/global/vault/<slug>.md. When that file is one this migration
// rewrote, and the row was in step with it, the row's hash, size and mtime
// follow the new bytes, and so does a recap that was current. Paths are
// compared resolved: SQLite may report the database under /private/var, and
// Windows may hand back a short name.
func carryHashes(ctx context.Context, tx *sql.Tx, root string, rewritten []fileChange) error {
	if len(rewritten) == 0 {
		return nil
	}
	byFile := map[string]fileChange{}
	for _, f := range rewritten {
		if real, err := filepath.EvalSymlinks(f.path); err == nil {
			byFile[real] = f
		}
	}
	type row struct {
		id, slug, key, hash string
		global              bool
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT e.id, e.slug, p.key, e.content_hash, e.global FROM entry e JOIN project p ON p.id = e.project_id`)
	if err != nil {
		return err
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.slug, &r.key, &r.hash, &r.global); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, r := range all {
		dir := filepath.Join(root, "projects", r.key, "vault")
		if r.global {
			dir = filepath.Join(root, "global", "vault")
		}
		real, err := filepath.EvalSymlinks(filepath.Join(dir, filepath.FromSlash(r.slug)+".md"))
		if err != nil {
			continue // the file is gone; core reports it as it always has
		}
		f, ok := byFile[real]
		if !ok || f.oldHash != r.hash {
			continue // not rewritten, or already out of step with its file
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE entry SET content_hash = ?, size = ?, mtime = ?,
			        recap_hash = CASE WHEN recap_hash = ? THEN ? ELSE recap_hash END
			 WHERE id = ?`,
			f.newHash, f.newSize, f.newMTime, f.oldHash, f.newHash, r.id); err != nil {
			return err
		}
	}
	return nil
}

// rewriteColumn applies change to every value of table.col that it alters.
// Only values containing "/knowledge" can change, so only those are read.
func rewriteColumn(ctx context.Context, tx *sql.Tx, table, col string, change func(string) string) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT rowid, `+col+` FROM `+table+` WHERE instr(`+col+`, 'knowledge') > 0`)
	if err != nil {
		return err
	}
	type update struct {
		rowid int64
		value string
	}
	var updates []update
	for rows.Next() {
		var id int64
		var v string
		if err := rows.Scan(&id, &v); err != nil {
			rows.Close()
			return err
		}
		if next := change(v); next != v {
			updates = append(updates, update{id, next})
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, u := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET `+col+` = ? WHERE rowid = ?`, u.value, u.rowid); err != nil {
			return err
		}
	}
	return nil
}
```

Notes for the implementer:

- **Row matching.** The hash carry-over matches on the file derived from root, key and slug, plus the old hash, never on the hash alone. A row that does not match is simply re-read by core on first use, as any external edit is. If root-inject derives an entry's file differently from `<root>/projects/<KEY>/knowledge/<slug>.md`, match what it does.
- **Shared helpers.** `foreignKeyCheck` and `firstLine` already live in `migrate_0014.go`. `foreignKeyCheck` names migration 0014 in its error, so give it a `migration string` parameter. Its message then reads `"foreign key check failed during migration "+migration+", rolled back: …"`, and 0014's own call passes `"0014"`:

  ```go
  func foreignKeyCheck(ctx context.Context, tx *sql.Tx, migration string) error {
  ```
- **Leftover schema objects.** `TestVocabularyMigration` scans `sqlite_master` after migrating and names any table, index or trigger still using a retired word. A migration merged after this plan was written may add one; add its rename to the statement list.

- [ ] **Step 4: Run the migration test**

```bash
go test ./internal/store -run TestVocabularyMigration -v
```

Expected: the three tests PASS. The rest of the suite is red, because code still queries `knowledge`.

- [ ] **Step 5: Move the code onto the new schema and stored words**

Apply these substitutions to Go code outside `internal/store` and `internal/vocabulary`. They are ordered so a longer name is replaced before any prefix of it:

```bash
cd /home/mtchen/Personal/trellis-worktrees/vocabulary
files=$(git grep -lE 'knowledge|doc_id|knowledge_id|owner|lease_until|review_by|reviewed_at|escalated|unarchived|privatised|documents|lease\.ttl|CollectionKnowledge|yaml:"verify' -- '*.go' ':!internal/store/**' ':!internal/vocabulary/**')
sed -i -E \
  -e 's/\bknowledge_search_state\b/entry_search_state/g' \
  -e 's/\bknowledge_fts\b/entry_fts/g' \
  -e 's/\bknowledge_label\b/entry_label/g' \
  -e 's/\bknowledge_tag\b/entry_tag/g' \
  -e 's/\bknowledge_id\b/entry_id/g' \
  -e 's/\bdoc_id\b/entry_id/g' \
  -e 's/\breview_by\b/verify_by/g' \
  -e 's/\breviewed_at\b/verified_at/g' \
  -e 's/\blease_until\b/claim_until/g' \
  -e 's/"lease\.ttl"/"claim.ttl"/g' \
  -e 's/yaml:"verify,/yaml:"resolve,/g' \
  $files
```

Then fix, by hand, each of the following. They are too context-dependent for `sed`. Use `git grep -n` to find each one.

- **Table name `knowledge` in SQL.** `git grep -nE "\b(FROM|JOIN|INTO|UPDATE|TABLE)\s+knowledge\b"` finds them. Replace `knowledge` with `entry`, and a `k.` alias may stay.
- **`db:"owner"` and SQL `owner`** on the card: becomes `claimed_by`. `git grep -nE "\bowner\b" -- 'internal/**/*.go'` lists them. Only the card column and its `db:` tags change; JSON tags stay `owner` until Task 11.
- **`link` type literals `'doc'` / `"doc"`:** become `'entry'`. The relation literal `'documents'` becomes `'cites'`, including `recall.go`'s `rel IN ('wikilink', 'documents')`.
- **`recordEvent(tx, "knowledge", …)`:** becomes `"entry"`. So does any `entity_type = 'knowledge'` in SQL, and the `events --kind` value list in `internal/cli/events.go`.
- **The event action strings:** `"escalated"` → `"promoted"`, `"unarchived"` → `"restored"`, `"privatised"` → `"privatized"`, and the board `"default"` action → `"set_default"`. The event field name `"owner"` → `"claimed_by"`.
- **`kbDir`'s directory names:** `"knowledge"` → `"vault"`, in both `filepath.Join` calls.
- **`vpath.CollectionKnowledge`'s value:** `"knowledge"` → `"vault"`, and the error `GLOBAL holds only knowledge` → `GLOBAL holds only vault entries`.
- **Shipped templates:** any `verify:` line in `internal/core/templates/*.md` → `resolve:`.
- **The config key:** its YAML struct tag and default table in `internal/config`, `lease:` → `claim:`.
- **Test fixtures:** tests that write old addresses (`/KEY/knowledge/`), or assert any of the above, change to the new words.
- **Migration 0014's tests:** they must stop at version 14, because after this migration the names they query (`knowledge`, `note`) no longer exist. In `internal/store/migrate_0014_test.go`, `migrateUp` calls `goose.Up(db.DB, "migrations")`; make it `goose.UpTo(db.DB, "migrations", 14)`. `TestMigration0014RollsBackWhole` may keep its own `goose.Up`, because 0014 fails before 0015 runs. Verified: with this change, `go test ./internal/store` passes.

A `json:` tag spelled like a renamed column (for example `json:"review_by"`) is changed by the `sed` above along with its `db:` tag. That is the one place JSON moves early; Task 11 handles every other key.

- [ ] **Step 6: Run everything**

```bash
go build ./... && go vet ./... && go test ./... && GOOS=windows go build ./...
python3 -B -m unittest discover -s scripts/tests
gofmt -l .
```

Expected: PASS, and `gofmt -l .` prints nothing. For a failure, read the assertion: an expected value with an old stored word is a fixture to update; an SQL error is a missed table or column.

- [ ] **Step 7: Try it on a copy of real data**

This is safe only because the branch's migrations drop the stored paths, so a copied home is then a real copy; the probe below proves it before the copy is touched. Take the database with the installed binary's `backup`. That binary is at the live database's version, so it does not migrate; it only logs its own invocation. Copy the files beside the backup. Everything the branch binary will open must be under `/tmp/vocab-home` before its first command. Never run the branch binary without `TRELLIS_HOME`.

```bash
rm -rf /tmp/vocab-home && mkdir -p /tmp/vocab-home
trellis backup /tmp/vocab-home/trellis.db
cp -a ~/.trellis/projects ~/.trellis/global ~/.trellis/templates /tmp/vocab-home/ 2>/dev/null || true
[ -f ~/.trellis/config.yaml ] && cp ~/.trellis/config.yaml /tmp/vocab-home/
ls -la /tmp/vocab-home    # trellis.db, projects/, global/, templates/ (and config.yaml) are all here
go build -o /tmp/trellis-vocab ./cmd/trellis
probe=$(mktemp -d); TRELLIS_HOME=$probe /tmp/trellis-vocab project ls >/dev/null
python3 -c 'import sqlite3,sys; c=[r[1] for r in sqlite3.connect(sys.argv[1]).execute("pragma table_info(entry)")]; assert c and "path" not in c, "entry has a path column: STOP"' "$probe/trellis.db"
rm -rf "$probe"
TRELLIS_HOME=/tmp/vocab-home /tmp/trellis-vocab knowledge ls --json | head -c 600
TRELLIS_HOME=/tmp/vocab-home /tmp/trellis-vocab knowledge pins --json
ls /tmp/vocab-home/projects/*/
```

Then read every entry, and confirm none was treated as an external edit:

```bash
TRELLIS_HOME=/tmp/vocab-home /tmp/trellis-vocab project ls --json
# for each project KEY printed above:
for slug in $(TRELLIS_HOME=/tmp/vocab-home /tmp/trellis-vocab --project KEY knowledge ls --json | python3 -c 'import json,sys; [print(e["slug"]) for e in json.load(sys.stdin)["knowledge"]]'); do
  TRELLIS_HOME=/tmp/vocab-home /tmp/trellis-vocab --project KEY knowledge show "$slug" --json > /dev/null
done
TRELLIS_HOME=/tmp/vocab-home /tmp/trellis-vocab --project KEY events --action reloaded --json
```

Expected:
- entries are listed with `/KEY/vault/...` refs;
- pins show `"stale": false` wherever they were fresh before;
- every project directory has `vault/` and no `knowledge/`;
- `events --action reloaded` prints nothing.

The command is still `knowledge`, and the list key still `knowledge`, until Tasks 10 and 11.

- [ ] **Step 8: Commit**

```bash
go test ./internal/vocabulary -update && go test ./internal/vocabulary
git add -A
git commit -m "feat(store): move vaults, files and schema onto the glossary's words"
```

---

### Task 6: Go names — vault and entry

**Files:**
- Rename: `internal/vpath/` → `internal/address/`
- Rename: `internal/core/knowledge.go` → `entry.go`, and every `internal/core/knowledge_*.go` → `entry_*.go`
- Rename: `internal/core/doc_relations.go` → `entry_relations.go`
- Modify: every Go file that uses the renamed identifiers

**Interfaces:**
- Produces: `core.Entry` (was `Knowledge`), `core.EntryFilter`, `core.NewEntry`, `(*Core).CreateEntry`, `ReadEntry`, `EditEntry`, `DeleteEntry`, `ListEntries`, `ColdEntries`, `MoveEntry`, `PinEntry`, `UnpinEntry`, `NominateEntry`, `DemoteEntry`, `VerifyEntry`, `LinkCardToEntry`, `EntryAddress`; `(*Core).WithVaultRoot`; package `address` with `Address`, `Parse`, `CollectionVault`, `Entry`, `GlobalEntry`, `Card`, `Artifact`, `Project`, `ValidKey`, `GlobalKey`.

- [ ] **Step 1: Save the rename helper**

`/tmp/gorename.sh` is a throwaway and is not committed:

```bash
cat > /tmp/gorename.sh <<'EOF'
#!/usr/bin/env bash
# Rename a Go identifier and every use of it across the module.
# usage: gorename.sh <file> <OldName> <NewName>
set -euo pipefail
file=$1 old=$2 new=$3
# A top-level func/type/var/const wins over an indented struct field or
# const-block line; the first of each kind is used.
pos=$(awk -v id="$old" '
  !top && match($0, "^(func (\\([^)]*\\) )?|type |var |const )" id "[^A-Za-z0-9_]") {
    top = NR ":" (RSTART + RLENGTH - 1 - length(id))
  }
  !nested && match($0, "^\t+" id "[^A-Za-z0-9_]") {
    nested = NR ":" (RSTART + RLENGTH - 1 - length(id))
  }
  END { print (top != "" ? top : nested) }' "$file")
[ -n "$pos" ] || { echo "no declaration of $old in $file" >&2; exit 1; }
echo "renaming $old at $file:$pos"
gopls rename -w "$file:$pos" "$new"
EOF
chmod +x /tmp/gorename.sh
```

The helper prints the position it chose. When a field name is shared by several structs in one file (for example `Path`), check that position, and if it is the wrong struct, call `gopls rename -w <file>:<line>:<col> <New>` with the right one.

- [ ] **Step 2: Rename the address package**

```bash
cd /home/mtchen/Personal/trellis-worktrees/vocabulary
git mv internal/vpath internal/address
git mv internal/address/vpath.go internal/address/address.go
git mv internal/address/vpath_test.go internal/address/address_test.go
sed -i 's/^package vpath$/package address/' internal/address/*.go
git grep -l 'internal/vpath' | xargs sed -i 's#internal/vpath#internal/address#g'
git grep -lE '\bvpath\.' -- '*.go' | xargs sed -i -E 's/\bvpath\./address./g'
go build ./... 2>&1 | head -20
```

Wherever the build reports that a local variable named `address` shadows the package, rename that variable to `addr`. Then rename the identifiers:

```bash
f=internal/address/address.go
/tmp/gorename.sh $f Path Address
/tmp/gorename.sh $f CollectionKnowledge CollectionVault
/tmp/gorename.sh $f KnowledgePath Entry
/tmp/gorename.sh $f GlobalKnowledgePath GlobalEntry
/tmp/gorename.sh $f CardPath Card
/tmp/gorename.sh $f ArtifactPath Artifact
/tmp/gorename.sh $f ProjectPath Project
grep -nE '^func [A-Z]\w*Path\(' $f   # any other constructor: XPath → X, the same way
go build ./... && go test ./internal/address/... ./internal/core/...
```

- [ ] **Step 3: List the core names to rename**

```bash
git grep -hoE '\b[A-Za-z_]*(Knowledge|Doc|KB)[A-Za-z0-9_]*\b|\bkb[A-Z]\w*|\bdocs?\b' -- 'internal/**/*.go' 'cmd/**/*.go' | sort | uniq -c | sort -rn > /tmp/vocab-go-names.txt
cat /tmp/vocab-go-names.txt
```

Rename each with `/tmp/gorename.sh <declaring file> <Old> <New>`, using these rules:

| Old | New |
|---|---|
| `Knowledge` (the type) | `Entry` |
| `…Knowledge` returning one entry (`CreateKnowledge`, `ReadKnowledge`, `EditKnowledge`, `DeleteKnowledge`, `MoveKnowledge`, `PinKnowledge`, `UnpinKnowledge`, `NominateKnowledge`, `DemoteKnowledge`, `VerifyKnowledge`) | `…Entry` |
| `…Knowledge` returning a list (`ListKnowledge`, `ColdKnowledge`) | `…Entries` |
| `KnowledgeFilter`, `NewKnowledge`, `KnowledgeHit` | `EntryFilter`, `NewEntry`, `EntryHit` |
| `matchKnowledge` | `matchEntries` |
| `EscalateKnowledge` | leave for Task 8 |
| `feedRow`'s `KB*` fields and its `kb_*` SQL aliases (`KBKey`, `KBSlug`, `KBTitle`, and the template one if `wip/template` kept the prefix) | `Entry*` and `entry_*` (`EntryKey`, `EntrySlug`, `EntryTitle`, …) |
| `loadDoc`, `docView`, `resolveDocRef`, `resolveDocStubs`, `RenderDoc`, `LinkCardToDoc`, `DocAddress` | `loadEntry`, `entryView`, `resolveEntryRef`, `resolveEntryStubs`, `RenderEntry`, `LinkCardToEntry`, `EntryAddress` |
| any other `…Doc…` | `…Entry…` |
| `kbDir`, `kbCore`, and `kbRoot` / `WithKBRoot` if root-inject left any | `vaultDir`, `vaultCore`, `vaultRoot` |
| local variables `doc`, `docs`, `d` holding entries | `entry`, `entries`, `e` — `gopls rename` at each declaration |

A name that clashes after renaming — for example a method `Entry` on a type that already has an `Entry` field — gets the plural or a verb; the build says where.


- [ ] **Step 4: Rename the files**

```bash
cd /home/mtchen/Personal/trellis-worktrees/vocabulary/internal/core
for f in knowledge*.go; do git mv "$f" "entry${f#knowledge}"; done
git mv doc_relations.go entry_relations.go
ls /home/mtchen/Personal/trellis-worktrees/vocabulary/internal/core | grep -iE 'knowledge|doc'
```

Expected: the last command prints nothing.

- [ ] **Step 5: Fix comments and messages in core**

```bash
cd /home/mtchen/Personal/trellis-worktrees/vocabulary
go test ./internal/vocabulary 2>&1 | grep -E 'internal/(core|address|retrieval|vector|ui|cli)/' | grep -E '"(kb|knowledge-base|knowledge-ident|doc|document|type-field|vpath|old-address)"'
```

Rewrite each listed line in the glossary's words. Comments: "doc" → "entry", "knowledge base"/"KB" → "vault", "virtual path" → "address". Error messages from core, such as `a knowledge entry needs a title`, become `an entry needs a title`. Leave command names in error hints (`trellis knowledge …`) for Task 10.

- [ ] **Step 6: Run everything and commit**

```bash
go build ./... && go vet ./... && go test ./... && GOOS=windows go build ./...
gofmt -l .
go test ./internal/vocabulary -update && go test ./internal/vocabulary
git add -A
git commit -m "refactor: entries live in vaults, and the code says so"
```

---

### Task 7: Go names — claims

**Files:**
- Modify: `internal/core/lease.go` → rename to `internal/core/claim.go`, with its test
- Modify: `internal/core/card.go`, `internal/ui/server.go`, `internal/cli/card.go`, `internal/cli/agent.go`, and every user of the renamed fields

**Interfaces:**
- Produces: `Card.ClaimedBy` (was `Owner`), `Card.ClaimUntil` (was `LeaseUntil`), `(*Core).RenewClaim` (was `RenewLease`), and the claim functions `ClaimCard`, `ClaimNextCard`, `ReleaseCard`, `StealCard` (unchanged where already named so).

- [ ] **Step 1: List and rename**

```bash
cd /home/mtchen/Personal/trellis-worktrees/vocabulary
git mv internal/core/lease.go internal/core/claim.go
[ -f internal/core/lease_test.go ] && git mv internal/core/lease_test.go internal/core/claim_test.go
git grep -hoE '\b\w*([Ll]ease|[Oo]wner|[Hh]older)\w*\b' -- 'internal/**/*.go' | sort | uniq -c | sort -rn
```

Rename with `/tmp/gorename.sh`, using these rules:

| Old | New |
|---|---|
| `Owner` (field) | `ClaimedBy` |
| `LeaseUntil` | `ClaimUntil` |
| `RenewLease` | `RenewClaim` |
| `…Lease…` | `…Claim…` |
| `holder` (contention payload field) | `ClaimedBy` |
| `ttl` variables and `LeaseTTL`-style config fields | `claimTTL`, `ClaimTTL` |
| the `locked` variable in the web `steal` handler | `claimed` |

`json:` tags stay as they are until Task 11. Error codes stay until Task 11.

- [ ] **Step 2: Fix comments and messages**

```bash
go test ./internal/vocabulary 2>&1 | grep -E '"(lease|owner|stale-other)"' | grep -vE 'internal/cli/|web/'
```

Rewrite each hit in core and ui: "lease" → "claim", "owner"/"holder" → "claimant", "held" → "claimed". The principle comment "leases, not locks" becomes "claims expire; locks don't".

- [ ] **Step 3: Run everything and commit**

```bash
go build ./... && go vet ./... && go test -race ./internal/core/... ./internal/ui/... && go test ./... && GOOS=windows go build ./...
gofmt -l .
go test ./internal/vocabulary -update && go test ./internal/vocabulary
git add -A
git commit -m "refactor: one word, claim, for a card's expiring hold"
```

---

### Task 8: Go names — promotion, verification, nominations

**Files:**
- Split: `internal/core/pin.go` → `pin.go` (pins), `nomination.go` (nominations), `promote.go` (promote, demote, verify, unverified, `moveFile`)
- Split: `internal/core/pin_test.go` the same way
- Modify: `internal/cli/knowledge.go` (the `escalate` command's Go names only), `internal/ui/server.go`

**Interfaces:**
- Produces: `(*Core).PromoteEntry` (was `EscalateKnowledge`), `Entry.VerifyBy`, `Entry.VerifiedAt`, `Entry.Unverified(nowMS int64) bool`, `GlobalVerifyDays` (was `GlobalReviewDays`), `Nomination.Nominations` (was `Noms`).

- [ ] **Step 1: Split `pin.go`**

Move `Nomination`, `NominateEntry` and `Nominations` into a new `internal/core/nomination.go`. Move `GlobalReviewDays`, `EscalateKnowledge`, `DemoteEntry`, `VerifyEntry`, `Unreviewed`, `moveFile` and `baseName` into a new `internal/core/promote.go`. Each new file gets `package core` and the imports its code needs. Split the tests the same way, into `nomination_test.go` and `promote_test.go`. Run:

```bash
go build ./... && go test ./internal/core/...
```

Expected: PASS. The split changes no code.

- [ ] **Step 2: Rename**

```bash
f=internal/core/promote.go
/tmp/gorename.sh $f EscalateKnowledge PromoteEntry
/tmp/gorename.sh $f GlobalReviewDays GlobalVerifyDays
/tmp/gorename.sh internal/core/entry.go ReviewBy VerifyBy
/tmp/gorename.sh internal/core/entry.go ReviewedAt VerifiedAt
/tmp/gorename.sh $f Unreviewed Unverified
/tmp/gorename.sh internal/core/nomination.go Noms Nominations
git grep -nE '\bunreviewed\b|\bUnreviewed\w*|escalat' -- 'internal/**/*.go'
```

Rename what the last command lists, with the same rules. Any Go identifier or variable called `unreviewed` becomes `unverified`; `escalat…` becomes `promot…`. The `nominations` CLI command's own variable `noms` becomes `nominees`. The `json:"noms"` tag stays until Task 11.

- [ ] **Step 3: Fix comments and messages**

Comments in core say "escalation" → "promotion", "candidate for escalation" → "nominee", "review clock" → "verify date", "the queue" → "the nomination queue". Core error texts follow: `agents nominate; humans escalate` lives in the CLI and waits for Task 10.

- [ ] **Step 4: Run everything and commit**

```bash
go build ./... && go vet ./... && go test ./... && GOOS=windows go build ./...
gofmt -l .
go test ./internal/vocabulary -update && go test ./internal/vocabulary
git add -A
git commit -m "refactor: promote and verify, and pin.go split by concern"
```

---

### Task 9: Go names — marker, diagnostics, duplicates, event log, and the rest

**Files:**
- Modify: `internal/resolve/*.go`, `internal/cli/init.go`, `internal/cli/resolve.go`, `internal/cli/config.go`, `internal/cli/doctor.go`
- Modify: `internal/core/lint.go`, `internal/core/health.go`, `internal/core/maintenance.go`
- Rename: `internal/core/dupes.go` → `duplicates.go` (and its test)
- Rename: `internal/core/template_verify.go` → `template_resolve.go` (and its test)
- Modify: `internal/core/event_feed.go` → rename to `event_log.go` (and its test)
- Modify: `internal/core/ids.go`, `internal/core/archive.go`, `internal/cli/events.go`, `internal/cli/daemon.go`

**Interfaces:**
- Produces:
  - Markers: `resolve.Marker`, `resolve.MarkerFile`, `resolve.FindMarker`, `resolve.ReadMarker`, `resolve.ParseMarker`, `resolve.MarkerError`; `core.InitResult.MarkerPath`.
  - Diagnostics and duplicates: `core.Diagnostic` (was `LintFinding`); `core.DuplicateCluster`.
  - Templates and events: `Template.Resolve` (was `Verify`); `(*Core).EventLog`, `core.LogEvent`, `core.EventQuery.Entities` (was `Kinds`).
  - The rest: `core.NewID` (was `NewCardID`); `(*Core).RestoreCard` (was `UnarchiveCard`).

- [ ] **Step 1: The marker**

```bash
git grep -hoE '\b\w*[Pp]in\w*\b' -- 'internal/resolve/*.go' 'internal/address/*.go' 'internal/cli/init.go' 'internal/cli/resolve.go' 'internal/cli/config.go' 'internal/cli/doctor.go' 'internal/core/project*.go' | sort | uniq -c
```

In those files, every identifier about the `.trellis` file renames `Pin` → `Marker` and `pin` → `marker` (`PinFile` → `MarkerFile`, `address.ParsePin` → `address.ParseMarker`, `findPin` → `findMarker`, `pinBoundary` → `markerBoundary`, `parentPin` → `parentMarker`, `PinPath` → `MarkerPath`, `Unpinnable` → `Unmarkable`, and so on). Identifiers about pinned entries (`core.Pin`, `PinEntry`, `pinTable`, `pins`) keep their names. Use `/tmp/gorename.sh` for each. Comments and messages follow; for example `no .trellis pin found` → `no .trellis marker found`. The error code `bad_pin` and the JSON keys `pin_path`, `pin_written` wait for Task 11.

- [ ] **Step 2: Diagnostics and duplicates**

```bash
git mv internal/core/dupes.go internal/core/duplicates.go
[ -f internal/core/dupes_test.go ] && git mv internal/core/dupes_test.go internal/core/duplicates_test.go
/tmp/gorename.sh internal/core/lint.go LintFinding Diagnostic
/tmp/gorename.sh internal/core/duplicates.go DupeCluster DuplicateCluster
git grep -nE '\b\w*[Dd]upe\w*\b|orphanHistory|orphanedHistory|\bfindings?\b' -- 'internal/**/*.go'
```

Rename what that lists: `dupe…` → `duplicate…`, `orphanHistory` → `leftoverRevisions`, `findings` → `diagnostics`. Health's label `orphaned revision directories` becomes `leftover revision directories`. The CLI flag names wait for Task 10.

- [ ] **Step 3: The template rule and the event log**

```bash
git mv internal/core/template_verify.go internal/core/template_resolve.go
git mv internal/core/template_verify_test.go internal/core/template_resolve_test.go
/tmp/gorename.sh internal/core/template.go Verify Resolve
git mv internal/core/event_feed.go internal/core/event_log.go
[ -f internal/core/event_feed_test.go ] && git mv internal/core/event_feed_test.go internal/core/event_log_test.go
/tmp/gorename.sh internal/core/event_log.go EventFeed EventLog
/tmp/gorename.sh internal/core/event_log.go FeedEvent LogEvent
/tmp/gorename.sh internal/core/event_log.go feedRow logRow
/tmp/gorename.sh internal/core/event_log.go toFeedEvent toLogEvent
/tmp/gorename.sh internal/core/event_log.go Kinds Entities
git grep -nE '\bVerify\b|\bverify\w*\(' -- internal/core/template*.go
```

`template.go` declares `Verify` in two structs (lines near 36 and 47 today). Rename the second with `gopls rename -w internal/core/template.go:<line>:<col> Resolve`. The functions the rule calls follow, for example `verifySources` → `resolveSources`. `logRow.Kind` (SQL alias `kind`) and `LogEvent.Kind` become `Entity` (alias `entity`). The JSON tag waits for Task 11.

- [ ] **Step 4: Ids, restore, liveness**

```bash
/tmp/gorename.sh internal/core/ids.go NewCardID NewID
/tmp/gorename.sh internal/core/archive.go UnarchiveCard RestoreCard
git grep -nE 'Health' -- internal/daemon internal/cli/daemon*.go
```

The daemon's liveness method or IPC request named `health` becomes `ping`. The vault's `Health` functions keep their name.

- [ ] **Step 5: Run everything and commit**

```bash
go build ./... && go vet ./... && go test -race ./internal/daemon/... && go test ./... && GOOS=windows go build ./...
gofmt -l .
go test ./internal/vocabulary -update && go test ./internal/vocabulary
git add -A
git commit -m "refactor: marker, diagnostic, duplicate, event log, and the smaller renames"
```

---

### Task 10: The CLI — commands, flags, help text

**Files:**
- Rename: `internal/cli/knowledge.go` → `vault.go`, `knowledge_template.go` → `vault_template.go`, and every `internal/cli/knowledge_*` file → `vault_*`
- Modify: every file in `internal/cli` with a row in spec §6
- Modify: every error hint in `internal/**` that names `trellis knowledge …`
- Modify: `internal/cli/help_test.go`

**Interfaces:**
- Consumes: `vocabulary.Find` (Task 3).
- Produces: `trellis vault {new,show,ls,edit,rm,mv,history,diff,pin,pins,lint,health,nominate,nominations,promote,demote,verify,uptake,template}`; flags `--entity` (on `events`), `--entry` (on `artifact link/unlink`), `--duplicates` (on `vault health`), `--leftover-revisions` (on `maintenance prune`).

- [ ] **Step 1: Extend the help test first**

Add to `internal/cli/help_test.go`:

```go
// Help text is how an agent learns the vocabulary, so it must use the
// glossary's words.
func TestHelpUsesTheGlossary(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		texts := map[string]string{"use": cmd.Use, "short": cmd.Short, "long": cmd.Long}
		cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
			texts["--"+f.Name] = f.Name + " " + f.Usage
		})
		for where, text := range texts {
			if rules := vocabulary.Find(text); len(rules) > 0 {
				t.Errorf("%s %s breaks %v: %q", cmd.CommandPath(), where, rules, text)
			}
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
}
```

It needs the imports `"github.com/spf13/pflag"` and `"github.com/mtch3n/trellis/internal/vocabulary"`.

- [ ] **Step 2: Run it to see it fail**

```bash
go test ./internal/cli -run TestHelpUsesTheGlossary 2>&1 | head -40
```

Expected: FAIL, one line per spec §6 row.

- [ ] **Step 3: Rename the command and its files**

```bash
cd /home/mtchen/Personal/trellis-worktrees/vocabulary/internal/cli
for f in knowledge*.go; do git mv "$f" "vault${f#knowledge}"; done
cd /home/mtchen/Personal/trellis-worktrees/vocabulary
git grep -hoE '\bnewKnowledge\w*|\bnewTemplate\w*Cmd' -- internal/cli | sort -u
```

Rename each `newKnowledgeXCmd` to `newVaultXCmd` with `/tmp/gorename.sh`. `newKnowledgeEscalateCmd` becomes `newVaultPromoteCmd`. Then, in `vault.go`:

- `Use: "knowledge"` → `Use: "vault"`
- `Use: "escalate <slug>"` → `Use: "promote <entry>"`
- The `vault` command gets:

```go
Long: "An <entry> argument is a slug in this project's vault, or an address:\n" +
	"/KEY/vault/<slug> or /GLOBAL/vault/<slug>.",
```

- [ ] **Step 4: Apply spec §6**

Change every string in spec §6's "Now" column to its "Becomes" value, verbatim. For the flag renames, change the flag name and its variable together; for example, `doc` bound to `"doc"` becomes `entry` bound to `"entry"`. `vault lint`'s Short lists every diagnostic kind the code emits:

```bash
git grep -hoE 'Kind: *"[a-z_]+"' -- internal/core | sort -u
```

- [ ] **Step 5: Error hints and messages**

```bash
git grep -lE 'trellis knowledge\b' -- 'internal/**/*.go' | xargs sed -i -E 's/trellis knowledge escalate\b/trellis vault promote/g; s/trellis knowledge\b/trellis vault/g'
git grep -nE 'escalate|--doc\b|--dupes|--orphan-history|--kind\b' -- 'internal/**/*.go'
```

Fix what the grep prints by hand. For example, `requireHuman`'s texts become `agents nominate; humans promote`. The `no_tty` hint currently reads `trellis ui   # then escalate from the browser`. It becomes `trellis ui` with the comment removed; the UI has no promote action, per spec §10.

- [ ] **Step 6: Run everything and commit**

```bash
go build ./... && go vet ./... && go test ./... && GOOS=windows go build ./...
gofmt -l .
go build -o /tmp/trellis-vocab ./cmd/trellis
export TRELLIS_HOME=$(mktemp -d)
/tmp/trellis-vocab vault --help && /tmp/trellis-vocab knowledge 2>&1 | head -2
rm -rf "$TRELLIS_HOME"; unset TRELLIS_HOME
go test ./internal/vocabulary -update && go test ./internal/vocabulary
git add -A
git commit -m "feat(cli)!: trellis vault, promote, and help text in the glossary's words"
```

Expected from the smoke run: `vault --help` lists every subcommand; `trellis knowledge` fails as an unknown command.

---

### Task 11: JSON keys and error codes

**Files:**
- Modify: `json:` tags in `internal/core`, `internal/ui`, `internal/cli`
- Modify: error codes in `internal/core/errors.go` users and `internal/cli`
- Modify: CLI and UI tests that assert keys or codes
- Modify: `plugin/hooks/trellis_hook.py` and `scripts/tests/test_plugin_hooks.py`, where they read these keys

**Interfaces:**
- Produces: the JSON and error codes in spec §5 "JSON" and "Error codes".

- [ ] **Step 1: Change the expected values in the tests first**

```bash
git grep -nE '"(knowledge|owner|lease_until|holder|findings|unreviewed|pin_path|pin_written|stale_leases|stale_documents|clusters|noms|kind)"|not_owned|project_leased|project_has_vault_entries|unknown_board|unknown_column|bad_pin|knowledge_not_found' -- 'internal/**/*_test.go' 'scripts/tests/*.py'
```

Change each expected key and code to spec §5's value. Check the `"kind"` hits one by one: only the event log's `kind` becomes `entity`; a search hit's and a diagnostic's `kind` stay. Then run the suite to see it fail:

```bash
go test ./internal/... 2>&1 | grep -E '^(---|FAIL)' | head
```

- [ ] **Step 2: Change the code**

Change the `json:` tags and the literal keys:

- `json:"owner"` → `json:"claimed_by"`
- `json:"lease_until"` → `json:"claim_until"`
- the contention payload's `holder` → `claimed_by`
- lint's `findings` → `diagnostics`
- `unreviewed` → `unverified`
- `pin_path`, `pin_written` → `marker_path`, `marker_written`
- `stale_leases` → `expired_claims`
- `stale_documents` → `unindexed_entries`
- `clusters` → `duplicate_clusters`
- `noms` → `nominations`
- `{"knowledge": …}` → `{"entries": …}`
- search and recall `kind` `"knowledge"` → `"entry"`
- `trellis events` lines: `kind` → `entity`
- the web API's event rows: `entity_type` → `entity`
- `orphan_history` in `GET /api/maintenance` and `POST /api/maintenance/prune` → `leftover_revisions`
- the setting key `lease.ttl` in `GET`/`PATCH /api/settings` → `claim.ttl` (it follows the config key)
- a template's `verify` rule, wherever template JSON exposes it → `resolve`

Task 12 Step 3 holds the complete list of HTTP routes and JSON names, which is what trellis-8d receives.

The card detail's comment and history JSON belongs to `wip/comments`; leave it.

Then change the error codes:

- `knowledge_not_found` → `entry_not_found`
- `unknown_board` → `board_not_found`, where the code already exists
- `unknown_column` → `column_not_found`
- `project_has_vault_entries` → `project_has_global_entries`
- `project_leased` → `project_has_claims`
- `bad_pin` → `bad_marker`

`not_owned` splits: the code path where the caller does not hold the claim returns `not_yours`; the path where another actor holds it returns `contention`.

- [ ] **Step 3: The hook**

In `plugin/hooks/trellis_hook.py`, update every key it reads from Trellis JSON to match. Then run:

```bash
python3 -B -m unittest discover -s scripts/tests
```

- [ ] **Step 4: Run everything and commit**

```bash
go build ./... && go vet ./... && go test ./... && GOOS=windows go build ./...
python3 -B -m unittest discover -s scripts/tests
gofmt -l .
go test ./internal/vocabulary -update && go test ./internal/vocabulary
git add -A
git commit -m "feat!: JSON keys and error codes in the glossary's words"
```

---

### Task 12: Web and TUI

**Owner:** the web half goes to the UI session (trellis-8d) through the integrator, with this task's text. This plan's executor does the TUI half and the server routes, which the web half depends on.

**Files:**
- Modify: `internal/ui/server.go` (routes), `internal/ui/server_test.go`
- Modify: `internal/cli/tui_workspace.go`
- Modify (UI session): `web/src/**`, `scripts/ui-browser-smoke.sh`, `web/dist` (rebuilt)

**Interfaces:**
- Produces: routes `/api/global/vault`, `/api/events`, `/p/:key/vault`; the JSON from Task 11.

- [ ] **Step 1: Server routes**

In `internal/ui/server.go`, rename every route the table in Step 3 lists. That is:

- `/api/activity` → `/api/events`
- `/api/global/knowledge` → `/api/global/vault`
- `/api/p/{key}/knowledge…` → `/api/p/{key}/vault…`
- `/api/p/{key}/b/{board}/knowledge…` → `…/vault…`
- `/api/p/{key}/links/knowledge` → `/api/p/{key}/links/vault`

The `…/steal` and `…/release` routes keep their names. Find anything left with:

```bash
git grep -nE '"(GET|POST|PUT|PATCH|DELETE) [^"]*(knowledge|activity|orphan)' -- internal/ui
```

The SSE endpoint `/events` keeps its path if it does not collide; if the mux reports a conflict, move SSE to `/api/events/stream`. Update `server_test.go`. Run `go test ./internal/ui/...`.

- [ ] **Step 2: TUI labels**

In `internal/cli/tui_workspace.go`:

- tab "Knowledge" → "Vault"
- "Documents"/"Document" → "Entries"/"Entry"
- grouping by `Template` → labelled "Template"
- the "New item" menu → "New card" and "New entry" as two items, if it offers both

Run `go test ./internal/cli/...`.

- [ ] **Step 3: Hand the web half over**

`web/` belongs to trellis-8d. This task edits nothing under `web/`; it sends the renames below to trellis-8d through the integrator, and that session changes the web code.

**Wire names that change.** Everything else on the wire keeps its name.

| Kind | Now | Becomes |
|---|---|---|
| route | `/api/activity` | `/api/events` (SSE `/events` moves to `/api/events/stream` only if the mux conflicts) |
| route | `/api/global/knowledge` | `/api/global/vault` |
| route | `/api/p/{key}/knowledge`, `…/{slug}`, `…/{slug}/diff`, `…/{slug}/history` | `/api/p/{key}/vault…` |
| route | `/api/p/{key}/b/{board}/knowledge`, `…/{slug}` | `/api/p/{key}/b/{board}/vault…` |
| route | `/api/p/{key}/links/knowledge` | `/api/p/{key}/links/vault` |
| web route | `/p/:key/knowledge` | `/p/:key/vault` |
| JSON | `owner`, `lease_until` | `claimed_by`, `claim_until` |
| JSON | `stale_leases` | `expired_claims` |
| JSON | card detail `activity` | `events` (the timeline itself belongs to `wip/comments`) |
| JSON | event rows `entity_type` | `entity` |
| JSON | `{"knowledge": […]}` | `{"entries": […]}` |
| JSON | `unreviewed` | `unverified` |
| JSON | lint `findings` | `diagnostics` |
| JSON | nomination `noms` | `nominations` |
| JSON | `orphan_history` (`GET /api/maintenance`, `POST /api/maintenance/prune`) | `leftover_revisions` |
| JSON | template `verify` (if exposed) | `resolve` |
| JSON value | setting key `lease.ttl` (`/api/settings`) | `claim.ttl` |
| JSON value | event entity `knowledge` | `entry` |
| JSON value | search hit `kind: knowledge`, graph node `type: doc` | `entry` |
| JSON value | event actions `escalated`, `unarchived`, `privatised`, `default` | `promoted`, `restored`, `privatized`, `set_default` |
| JSON value | link relation `documents` | `cites` |
| error code | `knowledge_not_found`, `unknown_board`, `unknown_column`, `project_has_vault_entries`, `project_leased`, `bad_pin`, `not_owned` | per Task 11 |

`/api/templates/*`, `/api/settings`, `/api/logs`, `/api/maintenance/compact` and `nothing_to_prune` keep their names.

Also send the integrator this checklist for trellis-8d:

- Types follow Task 11's JSON: `KnowledgeEntry` → `Entry`, `owner` → `claimed_by`, `lease_until` → `claim_until`, `stale_leases` → `expired_claims`, `unreviewed` → `unverified`.
- Routes: `/p/:key/knowledge` → `/p/:key/vault`, and the API routes from Step 1.
- Labels, from spec §5 "Web and TUI":
  - "Status" → "Column", "Kind" → "Template", "Visibility" → "Private"
  - "Attachments" → "Artifacts", "Take the lease"/"Take it" → "Steal the claim"/"Steal it", "Held by" → "Claimed by"
  - "Stale leases" → "Expired claims", "Unlinked" → "Orphans", "Activity" → "Events" (the card's timeline belongs to `wip/comments`, labelled "Timeline")
  - "Could not take the lease" → "Could not steal the claim"
- Every raw `event.action` goes through a label map instead of `sentence(event.action)`: created, edited, moved, deleted, claimed, stolen, released, renewed, archived, restored, blocked, unblocked, linked, labeled, unlabeled, tagged, untagged, pinned, unpinned, nominated, promoted, demoted, verified, privatized, injected, read, reloaded, renamed, set_default, merged, rebound.
- Components and files named `Knowledge*`, `Vault*` for one vault only, or `*Doc*` follow the glossary.
- `scripts/ui-browser-smoke.sh` waits for text the UI renders.
- Rebuild `web/dist`, and run `go test ./internal/vocabulary` on the web changes.

- [ ] **Step 4: Commit the server and TUI half**

```bash
go build ./... && go test ./... && GOOS=windows go build ./...
go test ./internal/vocabulary -update && go test ./internal/vocabulary
git add -A
git commit -m "feat(ui,tui)!: vault routes and terminal labels in the glossary's words"
```

---

### Task 13: Hook, skills and docs

**Files:**
- Modify: `plugin/hooks/trellis_hook.py`, `scripts/tests/test_plugin_hooks.py`
- Modify: `plugin/skills/trellis/SKILL.md` and `plugin/skills/trellis/references/*`, `plugin/skills/writing-knowledge/SKILL.md`, `plugin/skills/when-to-use-trellis/SKILL.md`, `plugin/skills/coordinating/SKILL.md`
- Modify: `plugin/**/plugin.json`, marketplace and build manifests
- Modify: `README.md`, `PRODUCT.md`, `docs/ui-operation-parity.md`, `scripts/*`
- Modify: any spec or plan under `docs/superpowers/` that has not been executed (ask the integrator which)

- [ ] **Step 1: The hook first**

In `plugin/hooks/trellis_hook.py`, the session-start command list names the new commands exactly:

- `knowledge show <slug>` → `vault show <entry>`
- `knowledge new --title ...` → `vault new --title ...`
- `recall <text>` and `search <text>` unchanged

Any prose "knowledge base" becomes "vault". Update the test's expected strings, then run:

```bash
python3 -B -m unittest discover -s scripts/tests
```

- [ ] **Step 2: Skills**

Rewrite the four skills in the glossary's words.

- **Command changes:** `trellis knowledge` → `trellis vault`; `escalate` → `promote`.
- **Word changes:** "doc"/"document" → "entry"; "knowledge base" → "vault"; "lease"/"owner" → "claim"/"claimant".
- **Who may nominate:**
  - `plugin/skills/trellis/SKILL.md` stops calling `nominate` a human gate: agents nominate; humans promote, demote and verify.
  - `when-to-use-trellis` says the same.
- **Glossary skills:** if `using-glossary` and `keeping-glossary` landed before this plan, their `trellis knowledge` commands become `trellis vault`.
- **Lint:** `plugin/skills/trellis/SKILL.md` describes lint as reporting diagnostics (stubs, broken anchors, orphans, missing artifacts), not only unresolved wikilinks.
- **Templates:** `writing-knowledge` lists the shipped templates — the names `trellis vault template ls` prints — instead of Failed approach, Measurement, Trap and Convention. The skill's directory name, `writing-knowledge`, uses "knowledge" as a mass noun and stays.

- [ ] **Step 3: Top-level docs and manifests**

- **README, PRODUCT, `docs/ui-operation-parity.md`, the plugin manifests:** "knowledge base" → "vault"; command names follow; "lease-steal" → "claim steal"; "Knowledge documents" → "Vault entries".
- **scripts:** `scripts/*` follow the new commands.

- [ ] **Step 4: Skills match the final CLI**

The rename is the release's last code change. So this is where every skill is checked against the tool as it now is, for every change the release made, not only for words. It covers:

- **Comments** replaced notes: `trellis card comment`, many per card, shown with events as the timeline.
- **Card relations:** `trellis card relate <card> <relation> <other-card>`.
- **Templates:**
  - enforced on every Trellis write, with lint reporting `template_violation` and `unknown_template`;
  - `trellis vault edit --set name=value`, where an empty value removes a field;
  - the `resolve:` rule, formerly `verify:`.
- **Artifacts:** `trellis artifact add` rolls back when the link fails, so a failed add leaves nothing behind.
- **Maintenance:** `trellis maintenance prune --leftover-revisions`, formerly `--orphan-history`.
- **Events:** the entity names for `trellis events --entity`: card, entry, board, label, comment.
- **Addresses:** including entries in directories.
- **Claims**, and the two glossary skills.
- **The web settings page:** any skill sentence that tells a human to edit `config.yaml` also mentions the settings page, briefly.

List every command the skills name, and check each against the final binary's help in a scratch home:

```bash
cd /home/mtchen/Personal/trellis-worktrees/vocabulary
go build -o /tmp/trellis-vocab ./cmd/trellis
export TRELLIS_HOME=$(mktemp -d)
grep -rhoE 'trellis [a-z]+( [a-z-]+)?' plugin/skills | sort -u | while read -r _ noun verb; do
  /tmp/trellis-vocab $noun $verb --help >/dev/null 2>&1 || echo "no such command: $noun $verb"
done
grep -rhoE -- '--[a-z][a-z-]+' plugin/skills | sort -u > /tmp/skill-flags.txt
/tmp/trellis-vocab --help >/dev/null
rm -rf "$TRELLIS_HOME"; unset TRELLIS_HOME
```

Also check every config key the skills name against the keys the final binary knows. Removed keys (`db.busy_timeout_ms`, `git.timeout`, `labels.preset`, `card.duplicate_check`, `card.duplicate_threshold`, `search.limit`) must not appear:

```bash
export TRELLIS_HOME=$(mktemp -d)
/tmp/trellis-vocab config ls --json > /tmp/config-keys.json
grep -rhoE '\b[a-z_]+\.[a-z_]+(\.[a-z_]+)?\b' plugin/skills | sort -u | while read -r k; do
  grep -q "\"$k\"" /tmp/config-keys.json || echo "not a config key (check whether it is one): $k"
done
rm -rf "$TRELLIS_HOME"; unset TRELLIS_HOME
```

The loop also prints file names and other dotted words. Only the ones that are meant as config keys matter.

Fix every "no such command". For each flag in `/tmp/skill-flags.txt`, find the command it is used with and confirm that command's `--help` lists it. Then reread each skill against spec §2 and correct any sentence that no longer describes what the tool does.

- [ ] **Step 5: Unexecuted specs and plans**

For each spec or plan the integrator names as not yet executed, reword it into the glossary's words.

- [ ] **Step 6: Run everything and commit**

```bash
go test ./... && python3 -B -m unittest discover -s scripts/tests
go test ./internal/vocabulary -update && go test ./internal/vocabulary
cat internal/vocabulary/allowlist.txt
git add -A
git commit -m "docs: hook, skills and docs in the glossary's words"
```

Every line left in `allowlist.txt` must now be a genuine second meaning, such as `owner` as an example template field or "document" as a PDF artifact kind. Add the reason after a fourth tab on each. Any other line is unfinished work: go back and fix it.

---

### Task 14: Ship it

- [ ] **Step 1: Full verification**

```bash
cd /home/mtchen/Personal/trellis-worktrees/vocabulary
go build ./... && go vet ./... && gofmt -l . && staticcheck ./... && go test ./... && go test -race ./... && GOOS=windows go build ./... && GOOS=darwin go build ./...
python3 -B -m unittest discover -s scripts/tests
```

Expected: everything passes; `gofmt -l .` and `staticcheck` print nothing.

- [ ] **Step 2: Hand the branch to the integrator**

Report to trellis-2f: the branch, its commits, the web checklist from Task 12 Step 3, and the upgrade notes below. The integrator merges; do not merge yourself.

Upgrade notes:
- The first start of the new binary migrates `~/.trellis`, so take a backup first: `trellis backup ~/trellis-before-vocabulary.db`, plus a copy of `~/.trellis/projects` and `~/.trellis/global`.
- Restart the daemon after installing: `trellis daemon restart`. The daemon runs the migration on its own start.
- Rebuild each project's vector index: `trellis vector rebuild` in each project. The vector database is derived and still says `doc_type`.
- A repository `.trellis.yaml` that sets `lease.ttl` must now say `claim.ttl`.

- [ ] **Step 3: After the switch-over, Trellis's glossary and CLAUDE.md**

This step runs after the integrator has merged, tagged and switched the user's machine over, with the new binary installed. It uses the installed `trellis` on the real home, which is now correct.

```bash
cd /home/mtchen/Personal/trellis
trellis vault template ls | grep -q glossary || trellis vault template reinstall glossary
trellis vault ls --template glossary
```

If no glossary exists, write its body from spec §2. Use one `###` area per table (Vault, Promotion, Claims, Diagnostics, Board, Addressing), rows copied verbatim under `## Terms`, and a third cell for the two-column tables: the retired word the prose names, or empty. Then create it:

```bash
trellis vault new --template glossary --title "Glossary" \
  --summary "One word per concept for Trellis; look a word up here before naming anything" \
  --body - < /tmp/trellis-glossary.md
```

Ask the author whether to pin it. Pin only on a yes:

```bash
trellis vault pin glossary --recap "One word per concept: look words up in the glossary entry before naming anything"
```

`CLAUDE.md` is the user's file. Do not edit it directly. Write the proposed version to `/tmp/CLAUDE.md.proposed`, send the integrator the output of `diff -u /home/mtchen/Personal/trellis/CLAUDE.md /tmp/CLAUDE.md.proposed`, and apply it only after the user's OK. It stays untracked; never commit it. The proposed changes:

- **Architecture:** `internal/vpath` becomes `internal/address — the address grammar a marker holds`, and `internal/resolve`'s "nearest `.trellis` pin" becomes "nearest `.trellis` marker".
- **Invariants — atomic writes:** "Writes go through `writeAtomic` / `stageRemoval` in `internal/core/file_store.go`" becomes "Writes go through `atomicfile.Write` / `stageRemoval` (`internal/atomicfile`, `internal/core/file_store.go`)".
- **Invariants — renamed words:**
  - "The pin walk" → "The marker walk"
  - `knowledge lint` → `vault lint`
  - `resolveDocStubs` → `resolveEntryStubs`
  - `matchKnowledge` → `matchEntries`
- **Cross-platform:** `fsync_unix.go` / `fsync_windows.go` becomes `internal/atomicfile/syncdir_*.go`.
- **New section:** add, before `## Cross-platform`:

  ```markdown
  ## Vocabulary

  - **One concept, one word.** The TRELLIS vault's glossary entry names every
    concept: `trellis vault show glossary`. Look a word up before introducing
    it, and add a row when a concept is new.
  - `go test ./internal/vocabulary` fails on a retired word. Fix the word, or, when
    the hit is a genuinely different meaning, add an allowlist line that says why.
  ```

Expected:
- `trellis vault show glossary` prints the table.
- On a yes to pinning, `trellis board show --brief` includes the recap.
- `CLAUDE.md` changes only after the user has approved the diff.
