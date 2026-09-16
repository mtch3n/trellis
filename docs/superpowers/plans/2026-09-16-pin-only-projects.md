# Pin-only Project Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a project a virtual namespace that a directory reaches only through a committed `.trellis` pin, and remove git-based identity entirely.

**Architecture:**
- A new `internal/vpath` package owns the grammar: `/KEY` and `/KEY/boards/<slug>`.
- `internal/resolve` shrinks to a pin walk that never runs git and stops at `.git`.
- Core gains `CreateProject`, `BoardBySlug` and a transactional `InitProject` that publishes the pin with the existing no-clobber `writeAtomic`.
- The CLI resolves every command through one helper.
- A Go migration drops the three identity columns. Before it commits, it checks that no foreign-key reference is broken, and it rolls back if one is.

**Tech Stack:** Go 1.27 (`errors.AsType`, `t.Chdir`, `wg.Go`), SQLite through `modernc.org/sqlite` and `sqlx`, goose v3.28 migrations, cobra, `encoding/json/v2`.

**Spec:** `docs/superpowers/specs/2026-09-16-pin-only-projects-design.md`

## Prerequisite

When this plan was written, the working tree held other uncommitted work:

- project deletion in `internal/core/project.go` and `internal/ui/server.go`
- `internal/core/project_delete_test.go`
- the web redesign
- the split `plugin/skills/*`
- `PRODUCT.md`

Task 6 edits `DeleteProject`'s doc comment and Task 8 edits `PRODUCT.md`, so that work must be committed first. Run `git status --short`. If anything outside `docs/` is modified or untracked, stop and ask the author to commit it. Do not stash it or commit it yourself.

## Global Constraints

- **No backward compatibility (pre-1.0):**
  - A bare-key pin is rejected.
  - `init --pin` is removed.
  - No binding is migrated.
  - No shim is left behind.
- **No git subprocess anywhere in resolution.** `.git` is checked with `os.Lstat` only, as a stop sign.
- **The pin walk never inspects `$HOME` or the filesystem root.** It stops after a directory containing `.git`, and any error other than not-exist is an error, never absence.
- Compare paths through `normalizeDir`, or through `filepath.EvalSymlinks` for the walk's start. macOS resolves `/var` to `/private/var`, and Windows hands back 8.3 names.
- File writes go through `writeAtomic` in `internal/core/file_store.go`.
- **Project keys:** new keys match `^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`, and `GLOBAL` is reserved.
- **Board slugs in pins** follow `slugify` in `internal/core/board.go`: lower-case letters and digits (Unicode) joined by single hyphens.
- **The pin's content** is one line, `/KEY` or `/KEY/boards/<slug>`, plus a trailing newline.
- **Help:** every command stays listed in help and has a `Short` (`TestHelpListsEveryCommand`).
- **Platforms:** CI runs Linux, macOS and Windows, and all three must pass.
- **Gates before any task is done:**
  - `go build ./...`
  - `go test ./...`
  - `go vet ./...`
  - `gofmt -l .` must print nothing
  - `staticcheck ./...`
  - `GOOS=windows go build ./...`
- **Commit messages** follow the repository's style, `feat(core): <what now happens>`, and end with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`

## File Structure

| File | Responsibility |
|---|---|
| `internal/vpath/vpath.go` (create) | Grammar: `Path`, `ProjectPath`, `BoardPath`, `ParsePin`, `ValidKey`, `ValidSlug`, `KeyFromName`, `GlobalKey` |
| `internal/vpath/vpath_test.go` (create) | Grammar tests |
| `internal/resolve/pin.go` (create) | `PinFile`, `Pin`, `PinError`, `ErrNotRegular`, `ReadPin`, `FindPin`, `Unpinnable` |
| `internal/resolve/pin_test.go` (create) | Walk tests |
| `internal/resolve/resolve.go` (modify → shrink) | Keeps only `normalizeDir` after Task 6 |
| `internal/resolve/git.go`, `giturl.go`, `git_test.go`, `giturl_test.go`, `resolve_test.go` (delete in Task 6) | Git identity |
| `internal/core/project.go` (modify) | `Project`, `CreateProject`, `createProject`, `checkNewKey`, `InitRequest`, `InitResult`, `InitProject`; `EnsureProject` removed in Task 6 |
| `internal/core/project_init_test.go` (create) | `CreateProject` / `InitProject` tests |
| `internal/core/project_test.go` (delete in Task 6) | `EnsureProject` tests |
| `internal/core/board.go` (modify) | `boardBySlug`, `BoardBySlug` |
| `internal/core/label.go` (modify) | `seedLabelsIfNone`; `EnsureDefaultLabels` removed in Task 4 |
| `internal/core/knowledge.go` (modify) | `GlobalKey = vpath.GlobalKey` |
| `internal/store/db.go` (modify) | Split `connect` out of `Open` |
| `internal/store/migrate_0013.go` (create) | Go migration `0013_drop_project_identity` |
| `internal/store/migrate_0013_test.go` (create) | Migration tests |
| `internal/cli/resolve.go` (create) | `resolvedProject`, `resolveProject`, `selectBoard`, `pinFailure` |
| `internal/cli/pin_helpers_test.go` (create) | `pinEnv`, `seedProject`, `writePin`, `coreErr` |
| `internal/cli/resolve_test.go` (create) | Resolution tests through commands |
| `internal/cli/root.go`, `config.go`, `board_show.go` (modify) | Use the helper |
| `internal/cli/init.go` (rewrite) | New `init` |
| `internal/cli/init_test.go` (create) | `init` tests |
| `internal/cli/project.go` (create) | `project new`, `project ls` |
| `internal/cli/project_cmd_test.go` (create) | Project command tests |
| `internal/cli/doctor.go`, `doctor_test.go` (modify) | Project checks |
| `internal/cli/knowledge_cmd_test.go` (modify) | `projectEnv` seeds through `seedProject` |
| Test seeds in `internal/core/{column,note,lease,artifact}_test.go`, `internal/config/config_test.go`, `internal/cli/tui_test.go`, `internal/ui/server_test.go` (modify in Task 6) | No identity columns |
| `CLAUDE.md`, `README.md`, `PRODUCT.md`, `plugin/skills/trellis/SKILL.md`, `scripts/tests/test_plugin_cli.py`, `.gitignore`, `.trellis` (Task 8) | Docs and this repository's own pin |

Task order keeps every commit green. Tasks 3–5 write the transitional `identity_kind = 'pin'` because the column is `NOT NULL` until Task 6 drops it; Task 6 removes that value in the same commit as the column.

---

### Task 1: The virtual path grammar

**Files:**
- Create: `internal/vpath/vpath.go`
- Test: `internal/vpath/vpath_test.go`

`Path` is general from the start (`Project`, `Collection`, `Name`), so the later path-addressing work extends the parser without reshaping the type. This task accepts only the two shapes a pin may hold.

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `const GlobalKey = "GLOBAL"`
  - `const CollectionBoards = "boards"`
  - `const PinShapes = "/KEY or /KEY/boards/<slug>"`
  - `type Path struct{ Project, Collection, Name string }`
  - `func ProjectPath(key string) Path`
  - `func BoardPath(key, slug string) Path`
  - `func (Path) Board() string`, which returns `Name` when `Collection` is `boards` and `""` otherwise
  - `func (Path) String() string`
  - `func ParsePin(s string) (Path, error)`
  - `func ValidKey(key string) bool`
  - `func ValidSlug(s string) bool`
  - `func KeyFromName(name string) string`

- [ ] **Step 1: Write the failing tests**

`internal/vpath/vpath_test.go`:

```go
package vpath

import (
	"strings"
	"testing"
)

func TestParsePinAcceptsBothShapes(t *testing.T) {
	cases := map[string]Path{
		"/TRELLIS":              ProjectPath("TRELLIS"),
		"  /trellis\n":          ProjectPath("TRELLIS"),
		"/KIOSK-ANALYSE":        ProjectPath("KIOSK-ANALYSE"),
		"/MONO/boards/api":      BoardPath("MONO", "api"),
		"/MONO/boards/api-work": BoardPath("MONO", "api-work"),
		"/MONO/boards/重構":       BoardPath("MONO", "重構"),
		"/P2024/boards/2024-q1": BoardPath("P2024", "2024-q1"),
	}
	for in, want := range cases {
		got, err := ParsePin(in)
		if err != nil {
			t.Errorf("ParsePin(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParsePin(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParsePinRejects(t *testing.T) {
	cases := map[string]string{
		"":                       "empty",
		"   \n":                  "empty",
		"/A\n/B":                 "more than one line",
		"TRELLIS":                "old bare-key format",
		"trellis":                "old bare-key format",
		"/":                      "not a project key",
		"/1ABC":                  "not a project key",
		"/MY_APP":                "not a project key",
		"/A--B":                  "not a project key",
		"/GLOBAL":                "global knowledge vault",
		"/MONO/boards":           "not a pin",
		"/MONO/cards/MONO-1":     "not a pin",
		"/MONO/boards/api/extra": "not a pin",
		"/MONO/boards/API":       "not a board slug",
		"/MONO/boards/-api":      "not a board slug",
		"/MONO/boards/a--b":      "not a board slug",
		"MONO/boards/api":        "not a pin",
	}
	for in, fragment := range cases {
		_, err := ParsePin(in)
		if err == nil {
			t.Errorf("ParsePin(%q) succeeded, want an error mentioning %q", in, fragment)
			continue
		}
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("ParsePin(%q) error = %q, want it to mention %q", in, err, fragment)
		}
	}
}

func TestPathStringAndBoard(t *testing.T) {
	for _, p := range []Path{ProjectPath("A"), BoardPath("A-B", "api")} {
		got, err := ParsePin(p.String())
		if err != nil || got != p {
			t.Errorf("ParsePin(%q) = %+v, %v; want %+v", p.String(), got, err, p)
		}
	}
	if got := BoardPath("MONO", "api").String(); got != "/MONO/boards/api" {
		t.Errorf("String() = %q", got)
	}
	if got := BoardPath("MONO", "api").Board(); got != "api" {
		t.Errorf("Board() = %q", got)
	}
	if got := ProjectPath("MONO").Board(); got != "" {
		t.Errorf("Board() of a project path = %q", got)
	}
}

func TestKeyFromName(t *testing.T) {
	cases := map[string]string{
		"trellis":         "TRELLIS",
		"kiosk-analyse":   "KIOSK-ANALYSE",
		"my app":          "MY-APP",
		"my__app..v2":     "MY-APP-V2",
		"2024-migrations": "P2024-MIGRATIONS",
		"-leading":        "LEADING",
		"trailing-":       "TRAILING",
		"...":             "",
		"重構":              "",
	}
	for in, want := range cases {
		got := KeyFromName(in)
		if got != want {
			t.Errorf("KeyFromName(%q) = %q, want %q", in, got, want)
		}
		if got != "" && !ValidKey(got) {
			t.Errorf("KeyFromName(%q) = %q, which ValidKey rejects", in, got)
		}
	}
}

func TestValidSlugMatchesBoardSlugs(t *testing.T) {
	for _, s := range []string{"api", "api-work", "board-2", "重構", "é"} {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "-api", "api-", "a--b", "API", "a b", "a_b", "É"} {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true, want false", s)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vpath/`
Expected: FAIL. The build fails because `ParsePin`, `Path`, `ProjectPath`, `BoardPath`, `KeyFromName`, `ValidKey` and `ValidSlug` are undefined.

- [ ] **Step 3: Write the implementation**

`internal/vpath/vpath.go`:

```go
// Package vpath parses Trellis virtual paths: the addresses that name a
// project, and the things inside it, independently of any local directory.
//
// A .trellis pin holds one of two shapes:
//
//	/KEY
//	/KEY/boards/<slug>
package vpath

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// GlobalKey names the global knowledge vault. It is never a project.
const GlobalKey = "GLOBAL"

// CollectionBoards is the collection a pin may name inside a project.
const CollectionBoards = "boards"

// PinShapes lists the forms a pin accepts, for error messages.
const PinShapes = "/KEY or /KEY/boards/<slug>"

var keyRE = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`)

// Path is a parsed virtual path: a project, and optionally one named item in
// one of its collections.
type Path struct {
	Project    string // upper-case project key
	Collection string // "" when the path names the project itself
	Name       string // the item within Collection
}

// ProjectPath names a project.
func ProjectPath(key string) Path { return Path{Project: key} }

// BoardPath names a board by slug.
func BoardPath(key, slug string) Path {
	return Path{Project: key, Collection: CollectionBoards, Name: slug}
}

// Board is the board slug the path names, or "".
func (p Path) Board() string {
	if p.Collection == CollectionBoards {
		return p.Name
	}
	return ""
}

// String renders the canonical form.
func (p Path) String() string {
	if p.Collection == "" {
		return "/" + p.Project
	}
	return "/" + p.Project + "/" + p.Collection + "/" + p.Name
}

// ParsePin reads the content of a .trellis file. Surrounding whitespace is
// ignored, and the key is case-insensitive and returned upper-case. A bare key
// -- the pin format before virtual paths -- is rejected with a message that
// says how to fix it.
func ParsePin(s string) (Path, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Path{}, errors.New("empty; expected " + PinShapes)
	}
	if strings.ContainsAny(s, "\r\n") {
		return Path{}, errors.New("more than one line; expected " + PinShapes)
	}
	rest, ok := strings.CutPrefix(s, "/")
	if !ok {
		if ValidKey(strings.ToUpper(s)) {
			return Path{}, fmt.Errorf("%q is the old bare-key format; write /%s instead", s, strings.ToUpper(s))
		}
		return Path{}, fmt.Errorf("%q is not a pin; expected %s", s, PinShapes)
	}
	segs := strings.Split(rest, "/")
	key, err := projectKey(segs[0])
	if err != nil {
		return Path{}, err
	}
	switch {
	case len(segs) == 1:
		return ProjectPath(key), nil
	case len(segs) == 3 && segs[1] == CollectionBoards:
		if !ValidSlug(segs[2]) {
			return Path{}, fmt.Errorf("%q is not a board slug: use lower-case letters and digits joined by single hyphens", segs[2])
		}
		return BoardPath(key, segs[2]), nil
	default:
		return Path{}, fmt.Errorf("%q is not a pin; expected %s", s, PinShapes)
	}
}

// projectKey validates the first segment of a path as a project key.
func projectKey(seg string) (string, error) {
	key := strings.ToUpper(seg)
	if !ValidKey(key) {
		return "", fmt.Errorf("%q is not a project key: use letters, digits and single hyphens, starting with a letter", seg)
	}
	if key == GlobalKey {
		return "", errors.New("GLOBAL is the global knowledge vault, not a project")
	}
	return key, nil
}

// ValidKey reports whether key is a well-formed project key. It does not
// check reservation: GLOBAL is well-formed and reserved.
func ValidKey(key string) bool { return keyRE.MatchString(key) }

// ValidSlug reports whether s has the shape createBoard gives a slug:
// lower-case letters and digits, in runs joined by single hyphens. Letters
// are Unicode letters, matching slugify in internal/core/board.go.
func ValidSlug(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") || strings.Contains(s, "--") {
		return false
	}
	for _, r := range s {
		switch {
		case r == '-', unicode.IsDigit(r):
		case unicode.IsLetter(r) && !unicode.IsUpper(r) && !unicode.IsTitle(r):
		default:
			return false
		}
	}
	return true
}

// KeyFromName derives a default project key from a directory name:
// upper-case, every run of characters outside A-Z and 0-9 becomes one
// hyphen, and a leading digit gets a P prefix. It returns "" when nothing
// usable remains; the caller must then ask for an explicit key.
func KeyFromName(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToUpper(name) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	key := strings.TrimSuffix(b.String(), "-")
	if key != "" && key[0] >= '0' && key[0] <= '9' {
		key = "P" + key
	}
	return key
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/vpath/ && go vet ./internal/vpath/ && gofmt -l internal/vpath`
Expected: `ok`, and nothing printed by `gofmt`.

- [ ] **Step 5: Commit**

```bash
git add internal/vpath
git commit -m "feat(vpath): parse the virtual paths a pin can hold

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: The pin walk

**Files:**
- Create: `internal/resolve/pin.go`
- Test: `internal/resolve/pin_test.go`

`resolve.go` keeps `Identify` untouched until Task 6; the new walk lives beside it and does not replace it yet.

**Interfaces:**
- Consumes: `vpath.ParsePin`, `vpath.Path` (Task 1); `normalizeDir` (existing, `internal/resolve/resolve.go`).
- Produces:
  - `const PinFile = ".trellis"`
  - `var ErrNotRegular error`
  - `type Pin struct{ Path string; Target vpath.Path }`
  - `type PinError struct{ Path string; Err error }`, with `Error()` and `Unwrap()`
  - `func ReadPin(path string) (Pin, error)`
  - `func FindPin(dir string) (pin Pin, found bool, err error)`
  - `func Unpinnable(dir string) (reason string, err error)`

- [ ] **Step 1: Write the failing tests**

`internal/resolve/pin_test.go`:

```go
package resolve

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/vpath"
)

// isolateHome points $HOME (and Windows' USERPROFILE) at a fresh directory so
// the developer's real home never takes part in a walk.
func isolateHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	return h
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func pinAt(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, PinFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustFind(t *testing.T, dir string) Pin {
	t.Helper()
	pin, found, err := FindPin(dir)
	if err != nil {
		t.Fatalf("FindPin(%s): %v", dir, err)
	}
	if !found {
		t.Fatalf("FindPin(%s) found nothing", dir)
	}
	return pin
}

func mustNotFind(t *testing.T, dir string) {
	t.Helper()
	pin, found, err := FindPin(dir)
	if err != nil {
		t.Fatalf("FindPin(%s): %v", dir, err)
	}
	if found {
		t.Fatalf("FindPin(%s) = %+v, want nothing", dir, pin)
	}
}

func TestFindPinNearestWins(t *testing.T) {
	isolateHome(t)
	repo := mkdir(t, t.TempDir(), "mono")
	mkdir(t, repo, ".git")
	pinAt(t, repo, "/MONO\n")
	api := mkdir(t, repo, "api")
	pinAt(t, api, "/API/boards/api\n")
	src := mkdir(t, api, "src", "deep")
	web := mkdir(t, repo, "web")

	if got := mustFind(t, src); got.Target != vpath.BoardPath("API", "api") {
		t.Errorf("from api/src/deep: %+v, want /API/boards/api", got.Target)
	}
	got := mustFind(t, web)
	if got.Target.Project != "MONO" {
		t.Errorf("from web: %+v, want /MONO", got.Target)
	}
	if want := filepath.Join(normalizeDir(repo), PinFile); got.Path != want {
		t.Errorf("pin path = %s, want %s", got.Path, want)
	}
}

func TestFindPinStopsAtGitDirectory(t *testing.T) {
	isolateHome(t)
	parent := t.TempDir()
	pinAt(t, parent, "/STRAY\n")
	repo := mkdir(t, parent, "repo")
	mkdir(t, repo, ".git")
	mustNotFind(t, mkdir(t, repo, "sub"))
}

// A worktree's .git is a file, and must stop the walk just like a directory.
func TestFindPinStopsAtGitFile(t *testing.T) {
	isolateHome(t)
	parent := t.TempDir()
	pinAt(t, parent, "/STRAY\n")
	tree := mkdir(t, parent, "worktree")
	if err := os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustNotFind(t, tree)
}

// A pin at a repository root is found: the pin check comes before the stop.
func TestFindPinReadsTheRepositoryRoot(t *testing.T) {
	isolateHome(t)
	repo := t.TempDir()
	mkdir(t, repo, ".git")
	pinAt(t, repo, "/ROOT\n")
	if got := mustFind(t, repo); got.Target.Project != "ROOT" {
		t.Errorf("got %+v", got.Target)
	}
}

// The home directory is never inspected. Using a TempDir as $HOME also
// exercises normalization on macOS, where it is spelled /var/... and resolves
// to /private/var/....
func TestFindPinNeverReadsHome(t *testing.T) {
	h := isolateHome(t)
	pinAt(t, h, "/HOMEPIN\n")
	mustNotFind(t, mkdir(t, h, "project"))
}

// The regression test for the storage root: $HOME/.trellis is a directory.
// Any .trellis that is not a regular file is skipped, not read and not fatal.
func TestFindPinSkipsATrellisDirectory(t *testing.T) {
	isolateHome(t)
	top := t.TempDir()
	mkdir(t, top, ".git")
	pinAt(t, top, "/TOP\n")
	mid := mkdir(t, top, "mid")
	mkdir(t, mid, PinFile)
	if got := mustFind(t, mid); got.Target.Project != "TOP" {
		t.Errorf("got %+v, want the pin above the .trellis directory", got.Target)
	}
}

func TestFindPinFollowsASymlinkedPin(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "shared-pin")
	if err := os.WriteFile(target, []byte("/LINKED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, PinFile)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	mkdir(t, dir, ".git")
	if got := mustFind(t, dir); got.Target.Project != "LINKED" {
		t.Errorf("got %+v", got.Target)
	}
}

func TestFindPinDanglingSymlinkIsAnError(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, PinFile)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, found, err := FindPin(dir)
	if err == nil || found {
		t.Fatalf("FindPin = found %v, err %v; want an error", found, err)
	}
}

func TestFindPinMalformedIsAPinError(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	pinAt(t, dir, "TRELLIS\n")
	_, _, err := FindPin(dir)
	pe, ok := errors.AsType[*PinError](err)
	if !ok {
		t.Fatalf("error = %v, want *PinError", err)
	}
	if !strings.Contains(pe.Error(), "old bare-key format") || !strings.HasSuffix(pe.Path, PinFile) {
		t.Errorf("PinError = %q (path %s)", pe.Error(), pe.Path)
	}
}

// An unreadable pin must never let the walk continue to an ancestor's pin.
func TestFindPinUnreadablePinIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not deny reads here")
	}
	isolateHome(t)
	parent := t.TempDir()
	pinAt(t, parent, "/PARENT\n")
	child := mkdir(t, parent, "child")
	pinAt(t, child, "/CHILD\n")
	if err := os.Chmod(filepath.Join(child, PinFile), 0o000); err != nil {
		t.Fatal(err)
	}
	_, found, err := FindPin(child)
	if err == nil || found || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("FindPin = found %v, err %v; want a permission error", found, err)
	}
}

func TestFindPinUnsearchableDirectoryIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not deny access here")
	}
	isolateHome(t)
	parent := t.TempDir()
	pinAt(t, parent, "/PARENT\n")
	locked := mkdir(t, parent, "locked")
	inner := mkdir(t, locked, "inner")
	if err := os.Chmod(locked, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if _, found, err := FindPin(inner); err == nil || found {
		t.Fatalf("FindPin = found %v, err %v; want an error", found, err)
	}
}

func TestFindPinWithoutAHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	dir := t.TempDir()
	pinAt(t, dir, "/NOHOME\n")
	if got := mustFind(t, dir); got.Target.Project != "NOHOME" {
		t.Errorf("got %+v", got.Target)
	}
}

func TestUnpinnable(t *testing.T) {
	h := isolateHome(t)
	if reason, err := Unpinnable(h); err != nil || reason == "" {
		t.Errorf("Unpinnable(home) = %q, %v; want a reason", reason, err)
	}
	root := filepath.VolumeName(h) + string(filepath.Separator)
	if reason, err := Unpinnable(root); err != nil || reason == "" {
		t.Errorf("Unpinnable(%s) = %q, %v; want a reason", root, reason, err)
	}
	if reason, err := Unpinnable(mkdir(t, h, "project")); err != nil || reason != "" {
		t.Errorf("Unpinnable(project) = %q, %v; want none", reason, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/resolve/ -run 'FindPin|Unpinnable'`
Expected: FAIL. The build fails because `FindPin`, `PinFile`, `Pin`, `PinError` and `Unpinnable` are undefined.

- [ ] **Step 3: Write the implementation**

`internal/resolve/pin.go`:

```go
package resolve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mtch3n/trellis/internal/vpath"
)

// PinFile is the file that links a directory to a project.
const PinFile = ".trellis"

// ErrNotRegular marks a .trellis entry that is not a regular file, such as
// the storage root $HOME/.trellis. The walk skips it; init refuses it.
var ErrNotRegular = errors.New("not a regular file")

// Pin is a .trellis file and the virtual path it names.
type Pin struct {
	Path   string // absolute path of the file
	Target vpath.Path
}

// PinError is a .trellis entry that cannot serve as a pin: malformed content,
// or not a regular file.
type PinError struct {
	Path string
	Err  error
}

func (e *PinError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e *PinError) Unwrap() error { return e.Err }

// ReadPin parses the pin at path. A missing file yields an error wrapping
// fs.ErrNotExist; a symlink whose target is missing does not, because that is
// a broken pin rather than an absent one.
func ReadPin(path string) (Pin, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if _, lerr := os.Lstat(path); lerr == nil {
			return Pin{}, fmt.Errorf("%s is a symlink to a missing file", path)
		}
	}
	if err != nil {
		return Pin{}, err
	}
	if !info.Mode().IsRegular() {
		return Pin{}, &PinError{Path: path, Err: ErrNotRegular}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Pin{}, err
	}
	target, err := vpath.ParsePin(string(b))
	if err != nil {
		return Pin{}, &PinError{Path: path, Err: err}
	}
	return Pin{Path: path, Target: target}, nil
}

// FindPin walks up from dir to the nearest pin. found is false when no pin
// applies to dir.
//
// The walk never inspects $HOME or the filesystem root: a pin there would
// capture every directory beneath it, and $HOME/.trellis is the default
// storage root. It stops after a directory that contains .git, so a pin above
// a repository never applies to it. Pins are committed, and one outside the
// repository would make the same repository resolve differently depending on
// where it was cloned. .git is only a stop sign: nothing is read from it, and
// git is never run.
//
// Any failure other than absence is an error. Treating an unreadable .trellis
// or .git as missing would let the walk continue to an ancestor's pin and
// send work to the wrong board.
func FindPin(dir string) (pin Pin, found bool, err error) {
	start, err := canonicalDir(dir)
	if err != nil {
		return Pin{}, false, err
	}
	home := homeDir()
	for d := start; ; {
		parent := filepath.Dir(d)
		if d == home || parent == d {
			return Pin{}, false, nil
		}
		p, err := ReadPin(filepath.Join(d, PinFile))
		switch {
		case err == nil:
			return p, true, nil
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, ErrNotRegular):
		default:
			return Pin{}, false, err
		}
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return Pin{}, false, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Pin{}, false, err
		}
		d = parent
	}
}

// Unpinnable says why a pin written in dir would never be read, or returns
// "" when it would be.
func Unpinnable(dir string) (reason string, err error) {
	d, err := canonicalDir(dir)
	if err != nil {
		return "", err
	}
	switch {
	case filepath.Dir(d) == d:
		return "the filesystem root is never searched for a pin", nil
	case d == homeDir():
		return "the home directory is never searched for a pin", nil
	}
	return "", nil
}

// canonicalDir resolves dir to an absolute path with symlinks evaluated.
// Unlike normalizeDir it does not fall back to lexical cleaning: the walk
// starts from a directory that must exist.
func canonicalDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", dir, err)
	}
	return filepath.Clean(resolved), nil
}

// homeDir is the normalized home directory, or "" when there is none; then
// only the filesystem root bounds the walk.
func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return ""
	}
	return normalizeDir(h)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/resolve/ && go vet ./internal/resolve/ && gofmt -l internal/resolve`
Expected: `ok` (the existing `Identify` tests still pass), and nothing printed by `gofmt`.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/pin.go internal/resolve/pin_test.go
git commit -m "feat(resolve): find the nearest pin without running git

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Core creates and pins projects

**Files:**
- Modify: `internal/core/project.go`: add `CreateProject`, `createProject`, `checkNewKey`, `InitRequest`, `InitResult` and `InitProject`. Keep `EnsureProject` for now.
- Modify: `internal/core/board.go`: add `boardBySlug` and `BoardBySlug`.
- Modify: `internal/core/label.go`: add `seedLabelsIfNone`, and have `EnsureDefaultLabels` delegate to it.
- Modify: `internal/core/knowledge.go:24`: `const GlobalKey = vpath.GlobalKey`.
- Test: `internal/core/project_init_test.go`

**Interfaces:**
- Consumes: `vpath.ValidKey`, `vpath.Path`, `vpath.ProjectPath`, `vpath.BoardPath`, `vpath.GlobalKey` (Task 1); `resolve.Pin`, `resolve.PinFile`, `resolve.ReadPin` (Task 2); `writeAtomic` (`internal/core/file_store.go`); `createBoard`, `boardByName` (`internal/core/board.go`); `SeedDefaultLabels` (`internal/core/label.go`).
- Produces:
  - `func (c *Core) CreateProject(ctx context.Context, key string, preset bool) (Project, error)`
  - `func (c *Core) BoardBySlug(ctx context.Context, projectID, slug string) (Board, error)`
  - `type InitRequest struct{ Dir, Key string; Join bool; BoardName string; Existing *resolve.Pin; Preset bool }`
  - `type InitResult struct{ Project Project; Board *Board; PinPath string; Created, Wrote bool }`
  - `func (c *Core) InitProject(ctx context.Context, req InitRequest) (InitResult, error)`
  - Error codes:
    - `bad_key` (exit 2)
    - `reserved_key` (exit 2)
    - `key_collision` (exit 4)
    - `pin_exists` (exit 4)
    - `unknown_board` (exit 3, from `BoardBySlug`)
    - `board_not_found` (exit 3, existing, from `boardByName`)

- [ ] **Step 1: Write the failing tests**

`internal/core/project_init_test.go`:

```go
package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/resolve"
)

func errCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	te, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("error = %v, want *core.Error", err)
	}
	return te.Code
}

func readPinFile(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, resolve.PinFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func projectCount(t *testing.T, c *Core) int {
	t.Helper()
	var n int
	if err := c.db.Get(&n, `SELECT count(*) FROM project`); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateProjectMakesTheDefaultBoard(t *testing.T) {
	c := testCore(t)
	p, err := c.CreateProject(t.Context(), " xpsctl ", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "XPSCTL" || p.Name != "XPSCTL" {
		t.Errorf("project = %+v, want key and name XPSCTL", p)
	}
	boards, err := c.ListBoards(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 1 || boards[0].Name != "xpsctl" || !boards[0].IsDefault {
		t.Errorf("boards = %+v, want one default board named xpsctl", boards)
	}
}

func TestCreateProjectRefusesBadReservedAndTakenKeys(t *testing.T) {
	c := testCore(t)
	for key, want := range map[string]string{
		"":        "bad_key",
		"1ABC":    "bad_key",
		"MY_APP":  "bad_key",
		"GLOBAL":  "reserved_key",
		"global":  "reserved_key",
	} {
		_, err := c.CreateProject(t.Context(), key, false)
		if got := errCode(t, err); got != want {
			t.Errorf("CreateProject(%q) code = %s, want %s", key, got, want)
		}
	}
	if _, err := c.CreateProject(t.Context(), "TAKEN", false); err != nil {
		t.Fatal(err)
	}
	_, err := c.CreateProject(t.Context(), "taken", false)
	if got := errCode(t, err); got != "key_collision" {
		t.Errorf("code = %s, want key_collision", got)
	}
}

func TestCreateProjectSeedsLabelsOnlyWithPreset(t *testing.T) {
	c := testCore(t)
	with, err := c.CreateProject(t.Context(), "WITH", true)
	if err != nil {
		t.Fatal(err)
	}
	without, err := c.CreateProject(t.Context(), "WITHOUT", false)
	if err != nil {
		t.Fatal(err)
	}
	for id, wantAny := range map[string]bool{with.ID: true, without.ID: false} {
		var n int
		if err := c.db.Get(&n, `SELECT count(*) FROM label WHERE project_id = ?`, id); err != nil {
			t.Fatal(err)
		}
		if (n > 0) != wantAny {
			t.Errorf("project %s has %d labels, want any=%v", id, n, wantAny)
		}
	}
}

func TestBoardBySlug(t *testing.T) {
	c := testCore(t)
	p, err := c.CreateProject(t.Context(), "ALPHA", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(t.Context(), p.ID, "API Work", true); err != nil {
		t.Fatal(err)
	}
	b, err := c.BoardBySlug(t.Context(), p.ID, "api-work")
	if err != nil || b.Name != "API Work" {
		t.Fatalf("BoardBySlug = %+v, %v", b, err)
	}
	_, err = c.BoardBySlug(t.Context(), p.ID, "web")
	if got := errCode(t, err); got != "unknown_board" {
		t.Errorf("code = %s, want unknown_board", got)
	}
	if !strings.Contains(err.Error(), "alpha, api-work") {
		t.Errorf("error %q should list the slugs that exist", err)
	}
}

func TestInitProjectCreatesAndPins(t *testing.T) {
	c := testCore(t)
	dir := t.TempDir()
	res, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Key: "alpha", Preset: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || !res.Wrote || res.Project.Key != "ALPHA" || res.Board != nil {
		t.Errorf("result = %+v", res)
	}
	if res.PinPath != filepath.Join(dir, resolve.PinFile) {
		t.Errorf("pin path = %s", res.PinPath)
	}
	if got := readPinFile(t, dir); got != "/ALPHA\n" {
		t.Errorf("pin = %q, want /ALPHA", got)
	}
}

func TestInitProjectJoinsOnlyWhenAsked(t *testing.T) {
	c := testCore(t)
	if _, err := c.CreateProject(t.Context(), "ALPHA", false); err != nil {
		t.Fatal(err)
	}

	inferred := t.TempDir()
	_, err := c.InitProject(t.Context(), InitRequest{Dir: inferred, Key: "ALPHA"})
	if got := errCode(t, err); got != "key_collision" {
		t.Fatalf("code = %s, want key_collision", got)
	}
	if _, statErr := os.Stat(filepath.Join(inferred, resolve.PinFile)); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a refused init wrote a pin: %v", statErr)
	}
	if !strings.Contains(err.Error(), "trellis init --key ALPHA") {
		t.Errorf("error %q should say how to join deliberately", err)
	}

	explicit := t.TempDir()
	res, err := c.InitProject(t.Context(), InitRequest{Dir: explicit, Key: "ALPHA", Join: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || !res.Wrote {
		t.Errorf("result = %+v, want joined and written", res)
	}
	if projectCount(t, c) != 1 {
		t.Errorf("joining created a project")
	}
}

func TestInitProjectPinsANamedBoard(t *testing.T) {
	c := testCore(t)
	p, err := c.CreateProject(t.Context(), "ALPHA", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(t.Context(), p.ID, "API Work", true); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Key: "ALPHA", Join: true, BoardName: "API Work"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Board == nil || res.Board.Slug != "api-work" {
		t.Errorf("board = %+v", res.Board)
	}
	if got := readPinFile(t, dir); got != "/ALPHA/boards/api-work\n" {
		t.Errorf("pin = %q", got)
	}
}

// A failure after the project row is inserted must leave nothing behind.
func TestInitProjectIsOneTransaction(t *testing.T) {
	c := testCore(t)
	dir := t.TempDir()
	_, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Key: "NEW", BoardName: "missing", Preset: true})
	if got := errCode(t, err); got != "board_not_found" {
		t.Fatalf("code = %s, want board_not_found", got)
	}
	if projectCount(t, c) != 0 {
		t.Error("the project survived a failed init")
	}
	var boards, labels int
	if err := c.db.Get(&boards, `SELECT count(*) FROM board`); err != nil {
		t.Fatal(err)
	}
	if err := c.db.Get(&labels, `SELECT count(*) FROM label`); err != nil {
		t.Fatal(err)
	}
	if boards != 0 || labels != 0 {
		t.Errorf("left %d boards and %d labels behind", boards, labels)
	}
}

func existingPin(t *testing.T, dir, content string) *resolve.Pin {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, resolve.PinFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	pin, err := resolve.ReadPin(filepath.Join(dir, resolve.PinFile))
	if err != nil {
		t.Fatal(err)
	}
	return &pin
}

// A fresh clone on a new machine: the pin is committed, the database is empty.
func TestInitProjectMaterializesAnExistingPin(t *testing.T) {
	c := testCore(t)
	dir := t.TempDir()
	res, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Existing: existingPin(t, dir, "/BETA\n")})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Wrote || res.Project.Key != "BETA" {
		t.Errorf("result = %+v", res)
	}
	if got := readPinFile(t, dir); got != "/BETA\n" {
		t.Errorf("pin was rewritten: %q", got)
	}
}

func TestInitProjectDoesNotCreateAPinnedBoard(t *testing.T) {
	c := testCore(t)
	dir := t.TempDir()
	_, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Existing: existingPin(t, dir, "/BETA/boards/api\n")})
	if got := errCode(t, err); got != "unknown_board" {
		t.Fatalf("code = %s, want unknown_board", got)
	}
	if !strings.Contains(err.Error(), "trellis board new") {
		t.Errorf("error %q should point at board new", err)
	}
	if projectCount(t, c) != 0 {
		t.Error("the project survived a failed init")
	}
}

func TestInitProjectRefusesFlagsThatContradictThePin(t *testing.T) {
	c := testCore(t)
	dir := t.TempDir()
	pin := existingPin(t, dir, "/BETA\n")

	_, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Key: "GAMMA", Join: true, Existing: pin})
	if got := errCode(t, err); got != "pin_exists" {
		t.Errorf("--key GAMMA: code = %s, want pin_exists", got)
	}

	if _, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Existing: pin}); err != nil {
		t.Fatal(err)
	}
	_, err = c.InitProject(t.Context(), InitRequest{Dir: dir, BoardName: "beta", Existing: pin})
	if got := errCode(t, err); got != "pin_exists" {
		t.Errorf("--board beta against /BETA: code = %s, want pin_exists", got)
	}
	if got := readPinFile(t, dir); got != "/BETA\n" {
		t.Errorf("pin changed: %q", got)
	}
}

// Another init can publish the pin between this init's commit and its write.
func TestInitProjectNeverOverwritesAPinWrittenMeanwhile(t *testing.T) {
	c := testCore(t)

	same := t.TempDir()
	existingPin(t, same, "/ALPHA\n")
	res, err := c.InitProject(t.Context(), InitRequest{Dir: same, Key: "ALPHA", Join: true})
	if err != nil {
		t.Fatalf("identical pin: %v", err)
	}
	if res.Wrote {
		t.Error("reported writing a pin that was already there")
	}

	other := t.TempDir()
	existingPin(t, other, "/OTHER\n")
	_, err = c.InitProject(t.Context(), InitRequest{Dir: other, Key: "FRESH", Join: true})
	if got := errCode(t, err); got != "pin_exists" {
		t.Fatalf("code = %s, want pin_exists", got)
	}
	if !strings.Contains(err.Error(), "FRESH was created") {
		t.Errorf("error %q should say the project it created remains", err)
	}
	if got := readPinFile(t, other); got != "/OTHER\n" {
		t.Errorf("pin was overwritten: %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core/ -run 'CreateProject|BoardBySlug|InitProject'`
Expected: FAIL. The build fails because `CreateProject`, `BoardBySlug`, `InitProject` and `InitRequest` are undefined.

- [ ] **Step 3: Add `seedLabelsIfNone` to `internal/core/label.go`**

Replace the body of `EnsureDefaultLabels`, and add the helper directly above it:

```go
// seedLabelsIfNone seeds the default labels when the project has none, in the
// caller's transaction, and reports whether it did.
func (c *Core) seedLabelsIfNone(tx *sqlx.Tx, projectID string) (bool, error) {
	var count int
	if err := tx.Get(&count, `SELECT COUNT(*) FROM label WHERE project_id = ?`, projectID); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	return true, c.SeedDefaultLabels(tx, projectID)
}

// EnsureDefaultLabels idempotently creates default labels if they don't exist yet.
// Returns true if labels were created, false if they already existed.
func (c *Core) EnsureDefaultLabels(ctx context.Context, projectID string) (bool, error) {
	var created bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		created, err = c.seedLabelsIfNone(tx, projectID)
		return err
	})
	return created, err
}
```

- [ ] **Step 4: Add `BoardBySlug` to `internal/core/board.go`**

Add it after `boardByName`:

```go
// BoardBySlug finds a board by slug, which is how a pin names one.
func (c *Core) BoardBySlug(ctx context.Context, projectID, slug string) (Board, error) {
	return boardBySlug(ctx, c.db, projectID, slug)
}

// boardBySlug works on the database or inside a caller's transaction. A miss
// lists the slugs that do exist, because a pin can only be fixed by naming one
// of them or by creating the board.
func boardBySlug(ctx context.Context, q sqlx.QueryerContext, projectID, slug string) (Board, error) {
	var b Board
	err := sqlx.GetContext(ctx, q, &b, `SELECT * FROM board WHERE project_id = ? AND slug = ?`, projectID, slug)
	if !errors.Is(err, sql.ErrNoRows) {
		return b, err
	}
	var slugs []string
	if err := sqlx.SelectContext(ctx, q, &slugs,
		`SELECT slug FROM board WHERE project_id = ? ORDER BY created_at`, projectID); err != nil {
		return b, err
	}
	return b, ErrNotFound("unknown_board",
		fmt.Sprintf("no board with slug %q (have: %s)", slug, strings.Join(slugs, ", ")),
		"trellis board new --name <name>")
}
```

- [ ] **Step 5: Point `GlobalKey` at `vpath`**

In `internal/core/knowledge.go`, replace the declaration `const GlobalKey = "GLOBAL"` with:

```go
// GlobalKey names the global vault; vpath owns the reservation.
const GlobalKey = vpath.GlobalKey
```

Add `"github.com/mtch3n/trellis/internal/vpath"` to that file's imports.

- [ ] **Step 6: Add project creation and init to `internal/core/project.go`**

Add these imports: `"io/fs"`, `"github.com/mtch3n/trellis/internal/vpath"`. `resolve` is already imported.

Then append:

```go
// CreateProject makes a project and its default board. No directory is
// involved: this is `trellis project new`, for a project nothing pins yet.
func (c *Core) CreateProject(ctx context.Context, key string, preset bool) (Project, error) {
	var p Project
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		if p, err = c.createProject(tx, normalizeKey(key)); err != nil {
			return err
		}
		if preset {
			_, err = c.seedLabelsIfNone(tx, p.ID)
		}
		return err
	})
	return p, err
}

func normalizeKey(key string) string { return strings.ToUpper(strings.TrimSpace(key)) }

// checkNewKey enforces the key grammar on keys being created. Existing rows
// may predate it; they stay reachable by --project.
func checkNewKey(key string) error {
	if !vpath.ValidKey(key) {
		return ErrUsage("bad_key",
			fmt.Sprintf("%q is not a project key: use upper-case letters, digits and single hyphens, starting with a letter", key),
			"trellis init --key <KEY>")
	}
	if key == GlobalKey {
		return ErrUsage("reserved_key", "GLOBAL is the global knowledge vault and cannot name a project",
			"trellis init --key <OTHER-KEY>")
	}
	return nil
}

// createProject inserts a project and its default board in the caller's
// transaction.
func (c *Core) createProject(tx *sqlx.Tx, key string) (Project, error) {
	if err := checkNewKey(key); err != nil {
		return Project{}, err
	}
	var taken int
	if err := tx.Get(&taken, `SELECT count(*) FROM project WHERE key = ?`, key); err != nil {
		return Project{}, err
	}
	if taken > 0 {
		return Project{}, ErrConflict("key_collision",
			fmt.Sprintf("project %s already exists", key), "trellis project ls")
	}
	p := Project{ID: NewCardID(), Key: key, Name: key, CreatedAt: c.clock.NowMS()}
	// identity_kind stays NOT NULL until migration 0013 drops it.
	if _, err := tx.Exec(
		`INSERT INTO project (id, key, identity_kind, name, created_at) VALUES (?, ?, 'pin', ?, ?)`,
		p.ID, p.Key, p.Name, p.CreatedAt); err != nil {
		return Project{}, err
	}
	if err := c.recordEvent(tx, "project", p.ID, "created", "", "", p.Key); err != nil {
		return Project{}, err
	}
	if _, err := c.createBoard(tx, p.ID, strings.ToLower(p.Key), true, true); err != nil {
		return Project{}, err
	}
	return p, nil
}

// InitRequest describes one `trellis init`.
type InitRequest struct {
	// Dir is the directory that receives the pin.
	Dir string
	// Key is the project key, any case. With Existing set it may be empty; if
	// not, it must agree with the pin.
	Key string
	// Join allows pinning a project that already exists. Set it only when the
	// key was named explicitly: a key merely derived from a directory name
	// must never join an unrelated project that happens to share it.
	Join bool
	// BoardName is --board: the board, by name, that the pin should name.
	BoardName string
	// Existing is the pin already in Dir. Its target is honored, and the file
	// is never rewritten.
	Existing *resolve.Pin
	// Preset seeds the default labels into a project that has none.
	Preset bool
}

// InitResult reports what InitProject did.
type InitResult struct {
	Project Project
	Board   *Board // the board the pin names, or nil
	PinPath string
	Created bool // false: an existing project was joined
	Wrote   bool // false: the pin was already there
}

// InitProject makes Dir resolvable. Every database change happens in one
// transaction; the pin is published afterwards with a no-clobber link, so
// two concurrent inits can never overwrite each other's pin. A pin that
// appears in between is accepted if it says the same thing and refused
// otherwise, and a project created by the losing call stays behind unpinned.
//
// InitProject never creates a board other than a new project's default:
// slugs are derived from names with collision suffixes, so a board created
// to match a pinned slug could not be promised to receive that slug.
func (c *Core) InitProject(ctx context.Context, req InitRequest) (InitResult, error) {
	key := normalizeKey(req.Key)
	join := req.Join
	pinBoard := ""
	if e := req.Existing; e != nil {
		if key != "" && key != e.Target.Project {
			return InitResult{}, pinExists(e.Path, e.Target, "--key "+key)
		}
		key, pinBoard, join = e.Target.Project, e.Target.Board(), true
	}

	res := InitResult{PinPath: filepath.Join(req.Dir, resolve.PinFile)}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		err := tx.Get(&res.Project, `SELECT * FROM project WHERE key = ?`, key)
		switch {
		case err == nil && !join:
			return ErrConflict("key_collision",
				fmt.Sprintf("project %s already exists, and a key taken from the directory name never joins one", key),
				"trellis init --key "+key+"   # join it deliberately, or pick --key <OTHER-KEY>")
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			if res.Project, err = c.createProject(tx, key); err != nil {
				return err
			}
			res.Created = true
		default:
			return err
		}

		if req.Preset {
			if _, err := c.seedLabelsIfNone(tx, res.Project.ID); err != nil {
				return err
			}
		}

		switch {
		case req.BoardName != "":
			b, err := c.boardByName(tx, res.Project.ID, req.BoardName)
			if err != nil {
				return err
			}
			if req.Existing != nil && b.Slug != pinBoard {
				return pinExists(req.Existing.Path, req.Existing.Target, "--board "+req.BoardName)
			}
			res.Board = &b
		case pinBoard != "":
			b, err := boardBySlug(ctx, tx, res.Project.ID, pinBoard)
			if err != nil {
				return err
			}
			res.Board = &b
		}
		return nil
	})
	if err != nil {
		return InitResult{}, err
	}
	if req.Existing != nil {
		return res, nil
	}

	target := vpath.ProjectPath(res.Project.Key)
	if res.Board != nil {
		target = vpath.BoardPath(res.Project.Key, res.Board.Slug)
	}
	err = writeAtomic(res.PinPath, []byte(target.String()+"\n"), false)
	switch {
	case err == nil:
		res.Wrote = true
		return res, nil
	case errors.Is(err, fs.ErrExist):
		if other, readErr := resolve.ReadPin(res.PinPath); readErr == nil && other.Target == target {
			return res, nil
		}
		msg := res.PinPath + " was written by another init while this one ran"
		if res.Created {
			msg += fmt.Sprintf("; project %s was created and is not pinned", res.Project.Key)
		}
		return InitResult{}, ErrConflict("pin_exists", msg, "trellis project ls")
	default:
		return InitResult{}, fmt.Errorf("writing %s: %w; project %s is ready, rerun trellis init --key %s",
			res.PinPath, err, res.Project.Key, res.Project.Key)
	}
}

func pinExists(path string, target vpath.Path, flag string) error {
	return ErrConflict("pin_exists",
		fmt.Sprintf("%s already names %s, which %s contradicts", path, target, flag),
		"edit or delete "+path+", then rerun trellis init")
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/core/ && go vet ./internal/core/ && gofmt -l internal/core`
Expected: `ok`, with the existing `EnsureProject` tests still passing, and nothing printed by `gofmt`.

- [ ] **Step 8: Commit**

```bash
git add internal/core/project.go internal/core/project_init_test.go internal/core/board.go internal/core/label.go internal/core/knowledge.go
git commit -m "feat(core): create a project and pin a directory to it in one transaction

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Commands resolve through pins, and init writes them

**Files:**
- Create: `internal/cli/resolve.go`
- Create: `internal/cli/pin_helpers_test.go`
- Create: `internal/cli/resolve_test.go`
- Create: `internal/cli/init_test.go`
- Rewrite: `internal/cli/init.go`
- Modify:
  - `internal/cli/root.go`: `currentBoard`
  - `internal/cli/config.go`: `currentProject` and `resolveConfigProject`
  - `internal/cli/board_show.go`: the silence check
  - `internal/cli/knowledge_cmd_test.go`: `projectEnv`
  - `internal/core/label.go`: delete `EnsureDefaultLabels`, which no longer has a caller

**Interfaces:**
- Consumes: `resolve.FindPin`, `resolve.ReadPin`, `resolve.PinError`, `resolve.PinFile`, `resolve.Unpinnable` (Task 2); `vpath.KeyFromName` (Task 1); `core.InitProject`, `core.InitRequest`, `core.CreateProject`, `core.BoardBySlug` (Task 3).
- Produces:
  - `type resolvedProject struct{ Project core.Project; Pin *resolve.Pin }`
  - `func resolveProject(ctx context.Context, c *core.Core) (resolvedProject, error)`
  - `func selectBoard(ctx context.Context, c *core.Core, r resolvedProject) (core.Board, error)`
  - `func pinFailure(err error) error`
  - Test helpers: `pinEnv(t, name) string`, `seedProject(t, key, boards...) core.Project`, `writePin(t, dir, content)`, `coreErr(t, err) *core.Error`
  - Error codes:
    - `unresolved` (exit 2)
    - `bad_pin` (exit 2)
    - `project_not_found` (exit 3)
    - `init_project_flag` (exit 2)
    - `init_location` (exit 2)
    - `missing_key` (exit 2)

- [ ] **Step 1: Write the test helpers**

`internal/cli/pin_helpers_test.go`:

```go
package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
)

// pinEnv isolates a test from the developer's Trellis state: a private
// storage root and home, no project or board override, and a fresh working
// directory called name, which it returns.
func pinEnv(t *testing.T, name string) string {
	t.Helper()
	t.Setenv("TRELLIS_HOME", t.TempDir())
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("TRELLIS_PROJECT", "")
	t.Setenv("TRELLIS_BOARD", "")
	dir := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir
}

// seedProject creates key, plus any extra boards, the way `project new` does.
// The handle is closed before any command opens the same file: an open handle
// would hold the database, and on Windows would block TempDir cleanup.
func seedProject(t *testing.T, key string, boards ...string) core.Project {
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
	p, err := c.CreateProject(t.Context(), key, false)
	if err != nil {
		t.Fatalf("CreateProject(%s): %v", key, err)
	}
	for _, name := range boards {
		if _, err := c.CreateBoard(t.Context(), p.ID, name, true); err != nil {
			t.Fatalf("CreateBoard(%s): %v", name, err)
		}
	}
	return p
}

func writePin(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".trellis"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func coreErr(t *testing.T, err error) *core.Error {
	t.Helper()
	ce, ok := errors.AsType[*core.Error](err)
	if !ok {
		t.Fatalf("error = %v, want a *core.Error", err)
	}
	return ce
}
```

In `internal/cli/knowledge_cmd_test.go`, replace the whole `projectEnv` function and its comment. Also remove the now-unused imports `context`, `home`, `resolve` and `store`, but only if nothing else in that file uses them. Check with `go vet`.

```go
// projectEnv names the project through TRELLIS_PROJECT, so a command needs no
// pin. A named project is looked up, never created, so it is seeded first.
func projectEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TRELLIS_HOME", t.TempDir())
	t.Setenv("TRELLIS_PROJECT", "TEST")
	seedProject(t, "TEST")
}
```

- [ ] **Step 2: Write the failing resolution tests**

`internal/cli/resolve_test.go`:

```go
package cli

import (
	"encoding/json/v2"
	"strings"
	"testing"
)

type shownBoard struct {
	Project string `json:"project"`
	Slug    string `json:"slug"`
}

func showBoard(t *testing.T, args ...string) shownBoard {
	t.Helper()
	var v shownBoard
	out := runCmd(t, append([]string{"board", "show", "--json"}, args...)...)
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("board show output: %v\n%s", err, out)
	}
	return v
}

func TestCommandsRefuseWithoutAPin(t *testing.T) {
	pinEnv(t, "loose")
	_, err := execCmd("card", "ls")
	ce := coreErr(t, err)
	if ce.Code != "unresolved" || !strings.Contains(ce.Fix, "trellis init --key") {
		t.Errorf("error = %+v", ce)
	}
}

// The SessionStart hook runs this everywhere; with no pin it must say nothing.
func TestBriefIsSilentWithoutAPin(t *testing.T) {
	pinEnv(t, "loose")
	out, err := execCmd("board", "show", "--brief")
	if err != nil || out != "" {
		t.Errorf("brief = %q, %v; want nothing", out, err)
	}
}

func TestBriefFailsForAPinnedProjectThatIsMissing(t *testing.T) {
	dir := pinEnv(t, "clone")
	writePin(t, dir, "/GHOST\n")
	_, err := execCmd("board", "show", "--brief")
	ce := coreErr(t, err)
	if ce.Code != "project_not_found" || !strings.Contains(ce.Msg, ".trellis") || ce.Exit == 0 {
		t.Errorf("error = %+v", ce)
	}
}

func TestMalformedPinIsAUsageError(t *testing.T) {
	dir := pinEnv(t, "old")
	writePin(t, dir, "TRELLIS\n")
	_, err := execCmd("card", "ls")
	ce := coreErr(t, err)
	if ce.Code != "bad_pin" || !strings.Contains(ce.Msg, "old bare-key format") {
		t.Errorf("error = %+v", ce)
	}
}

func TestProjectPrecedence(t *testing.T) {
	dir := pinEnv(t, "app")
	seedProject(t, "PINNED")
	seedProject(t, "ENVIRON")
	seedProject(t, "FLAGGED")
	writePin(t, dir, "/PINNED\n")

	if got := showBoard(t).Project; got != "PINNED" {
		t.Errorf("pin: project = %s", got)
	}
	t.Setenv("TRELLIS_PROJECT", "environ")
	if got := showBoard(t).Project; got != "ENVIRON" {
		t.Errorf("env over pin: project = %s", got)
	}
	if got := showBoard(t, "--project", "flagged").Project; got != "FLAGGED" {
		t.Errorf("flag over env: project = %s", got)
	}
}

func TestBoardPrecedence(t *testing.T) {
	dir := pinEnv(t, "mono")
	seedProject(t, "MONO", "API", "Web")
	writePin(t, dir, "/MONO/boards/api\n")

	if got := showBoard(t).Slug; got != "api" {
		t.Errorf("pin board: slug = %s", got)
	}
	t.Setenv("TRELLIS_BOARD", "Web")
	if got := showBoard(t).Slug; got != "web" {
		t.Errorf("env over pin: slug = %s", got)
	}
	if got := showBoard(t, "--board", "mono").Slug; got != "mono" {
		t.Errorf("flag over env: slug = %s", got)
	}
	t.Setenv("TRELLIS_BOARD", "")
	if got := showBoard(t, "--project", "MONO").Slug; got != "mono" {
		t.Errorf("--project must ignore the pin's board: slug = %s", got)
	}
}

func TestPinnedBoardThatIsMissingNamesThePin(t *testing.T) {
	dir := pinEnv(t, "mono")
	seedProject(t, "MONO")
	writePin(t, dir, "/MONO/boards/api\n")
	_, err := execCmd("card", "ls")
	ce := coreErr(t, err)
	if ce.Code != "unknown_board" || !strings.Contains(ce.Msg, ".trellis") {
		t.Errorf("error = %+v", ce)
	}
}
```

- [ ] **Step 3: Write the failing init tests**

`internal/cli/init_test.go`:

```go
package cli

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type initOutput struct {
	Project struct {
		Key string `json:"key"`
	} `json:"project"`
	Board *struct {
		Slug string `json:"slug"`
	} `json:"board"`
	PinPath    string   `json:"pin_path"`
	Created    bool     `json:"created"`
	PinWritten bool     `json:"pin_written"`
	Notes      []string `json:"notes"`
}

func runInit(t *testing.T, args ...string) initOutput {
	t.Helper()
	out := runCmd(t, append([]string{"init", "--json"}, args...)...)
	var v initOutput
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("init output: %v\n%s", err, out)
	}
	return v
}

func pinContent(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".trellis"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInitCreatesAProjectFromTheDirectoryName(t *testing.T) {
	dir := pinEnv(t, "my app")
	got := runInit(t)
	if got.Project.Key != "MY-APP" || !got.Created || !got.PinWritten {
		t.Errorf("init = %+v", got)
	}
	if pinContent(t, dir) != "/MY-APP\n" {
		t.Errorf("pin = %q", pinContent(t, dir))
	}
	if !strings.Contains(strings.Join(got.Notes, "\n"), "commit .trellis") {
		t.Errorf("notes = %v, want the commit reminder", got.Notes)
	}
	// The pin it wrote is what later commands resolve through.
	if showBoard(t).Project != "MY-APP" {
		t.Error("a command after init did not resolve to the new project")
	}
}

func TestInitNeverJoinsOnAnInferredKey(t *testing.T) {
	dir := pinEnv(t, "alpha")
	seedProject(t, "ALPHA")
	_, err := execCmd("init")
	if ce := coreErr(t, err); ce.Code != "key_collision" {
		t.Fatalf("error = %+v", ce)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".trellis")); statErr == nil {
		t.Error("a refused init wrote a pin")
	}
}

func TestInitJoinsANamedProject(t *testing.T) {
	dir := pinEnv(t, "checkout-2")
	seedProject(t, "ALPHA")
	got := runInit(t, "--key", "alpha")
	if got.Created || got.Project.Key != "ALPHA" {
		t.Errorf("init = %+v", got)
	}
	if pinContent(t, dir) != "/ALPHA\n" {
		t.Errorf("pin = %q", pinContent(t, dir))
	}
}

func TestInitReadsACommittedPin(t *testing.T) {
	dir := pinEnv(t, "fresh-clone")
	writePin(t, dir, "/BETA\n")
	got := runInit(t)
	if !got.Created || got.PinWritten || got.Project.Key != "BETA" {
		t.Errorf("init = %+v", got)
	}
	if pinContent(t, dir) != "/BETA\n" {
		t.Error("init rewrote an existing pin")
	}
}

func TestInitRefusesAKeyThatContradictsThePin(t *testing.T) {
	dir := pinEnv(t, "app")
	writePin(t, dir, "/BETA\n")
	_, err := execCmd("init", "--key", "GAMMA")
	if ce := coreErr(t, err); ce.Code != "pin_exists" {
		t.Errorf("error = %+v", ce)
	}
}

func TestInitBoardFlagPinsTheSlug(t *testing.T) {
	dir := pinEnv(t, "api")
	seedProject(t, "MONO", "API Work")
	got := runInit(t, "--key", "MONO", "--board", "API Work")
	if got.Board == nil || got.Board.Slug != "api-work" {
		t.Errorf("init = %+v", got)
	}
	if pinContent(t, dir) != "/MONO/boards/api-work\n" {
		t.Errorf("pin = %q", pinContent(t, dir))
	}
}

func TestInitRefusesTheProjectFlag(t *testing.T) {
	pinEnv(t, "app")
	_, err := execCmd("init", "--project", "ALPHA")
	if ce := coreErr(t, err); ce.Code != "init_project_flag" || !strings.Contains(ce.Fix, "--key ALPHA") {
		t.Errorf("error = %+v", ce)
	}
}

func TestInitRefusesTheHomeDirectory(t *testing.T) {
	dir := pinEnv(t, "home")
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	_, err := execCmd("init", "--key", "HOME")
	if ce := coreErr(t, err); ce.Code != "init_location" {
		t.Errorf("error = %+v", ce)
	}
}

func TestInitRefusesATrellisDirectory(t *testing.T) {
	dir := pinEnv(t, "app")
	if err := os.Mkdir(filepath.Join(dir, ".trellis"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := execCmd("init", "--key", "APP")
	if ce := coreErr(t, err); ce.Code != "bad_pin" {
		t.Errorf("error = %+v", ce)
	}
}

func TestInitRefusesAnUnusableDirectoryName(t *testing.T) {
	pinEnv(t, "___")
	_, err := execCmd("init")
	if ce := coreErr(t, err); ce.Code != "missing_key" {
		t.Errorf("error = %+v", ce)
	}
}

func TestInitWarnsWhenTrellisProjectIsSet(t *testing.T) {
	pinEnv(t, "app")
	seedProject(t, "OTHER")
	t.Setenv("TRELLIS_PROJECT", "OTHER")
	got := runInit(t, "--key", "APP")
	if !strings.Contains(strings.Join(got.Notes, "\n"), "TRELLIS_PROJECT=OTHER") {
		t.Errorf("notes = %v", got.Notes)
	}
}

func TestInitNotesTheParentPinItOverrides(t *testing.T) {
	parent := pinEnv(t, "mono")
	writePin(t, parent, "/MONO\n")
	api := filepath.Join(parent, "api")
	if err := os.Mkdir(api, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(api)
	got := runInit(t, "--key", "API")
	if !strings.Contains(strings.Join(got.Notes, "\n"), "overrides /MONO") {
		t.Errorf("notes = %v", got.Notes)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'Refuse|Brief|Malformed|Precedence|Pinned|Init'`
Expected: FAIL. The tests compile, then fail: `card ls` still auto-creates through git or errors with `trellis init --pin`, and `init` has no `--key` join semantics or `notes`.

- [ ] **Step 5: Write the resolution helper**

`internal/cli/resolve.go`:

```go
package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
)

// resolvedProject is the project a command acts on and, when a pin chose it,
// that pin.
type resolvedProject struct {
	Project core.Project
	Pin     *resolve.Pin
}

// resolveProject picks the project: --project, then TRELLIS_PROJECT, then the
// nearest .trellis pin. Nothing else -- no git lookup, and no project is ever
// created here.
func resolveProject(ctx context.Context, c *core.Core) (resolvedProject, error) {
	if key := projectKey(); key != "" {
		p, err := c.ProjectByKey(ctx, key)
		return resolvedProject{Project: p}, err
	}
	dir, err := os.Getwd()
	if err != nil {
		return resolvedProject{}, err
	}
	pin, found, err := resolve.FindPin(dir)
	if err != nil {
		return resolvedProject{}, pinFailure(err)
	}
	if !found {
		return resolvedProject{}, core.ErrUsage("unresolved",
			"no .trellis pin in this directory or any parent", "trellis init --key <KEY>")
	}
	p, err := c.ProjectByKey(ctx, pin.Target.Project)
	if ce, ok := errors.AsType[*core.Error](err); ok && ce.Code == "project_not_found" {
		return resolvedProject{}, core.ErrNotFound("project_not_found",
			fmt.Sprintf("%s names project %s, which this Trellis database does not have", pin.Path, pin.Target.Project),
			"trellis init   # in "+filepath.Dir(pin.Path))
	}
	if err != nil {
		return resolvedProject{}, err
	}
	return resolvedProject{Project: p, Pin: &pin}, nil
}

// pinFailure reports a malformed pin as a usage error. Anything else is an I/O
// failure and passes through unchanged.
func pinFailure(err error) error {
	if pe, ok := errors.AsType[*resolve.PinError](err); ok {
		return core.ErrUsage("bad_pin", pe.Error(),
			"trellis init --key <KEY>   # after removing "+pe.Path)
	}
	return err
}

// selectBoard picks the board: --board, then TRELLIS_BOARD, then the pin's
// board when the pin also chose the project, then core.SelectBoard's rules.
func selectBoard(ctx context.Context, c *core.Core, r resolvedProject) (core.Board, error) {
	requested := cmp.Or(boardFlag, os.Getenv("TRELLIS_BOARD"))
	if requested != "" || r.Pin == nil || r.Pin.Target.Board() == "" {
		return c.SelectBoard(ctx, r.Project.ID, requested)
	}
	b, err := c.BoardBySlug(ctx, r.Project.ID, r.Pin.Target.Board())
	if ce, ok := errors.AsType[*core.Error](err); ok {
		ce.Msg = r.Pin.Path + ": " + ce.Msg
	}
	return b, err
}
```

- [ ] **Step 6: Use the helper in `root.go`, `config.go` and `board_show.go`**

In `internal/cli/root.go`, replace `currentBoard` (its comment and body):

```go
// currentBoard resolves the project and then the board. Standing where no pin
// applies exits 2 rather than creating anything.
func currentBoard() (*appCtx, error) {
	c, db, err := openCore()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	r, err := resolveProject(ctx, c)
	if err != nil {
		db.Close()
		return nil, err
	}
	b, err := selectBoard(ctx, c, r)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &appCtx{Core: c, Project: r.Project, Board: b, db: db}, nil
}
```

Remove the `"github.com/mtch3n/trellis/internal/resolve"` import from `root.go`. Also update the comment on `projectFlagKey` to read `empty means "resolve from the nearest .trellis pin"`.

In `internal/cli/config.go`, replace the block in `currentProject` from `var p core.Project` down to the end of the `else` branch with:

```go
	r, err := resolveProject(context.Background(), c)
	if err != nil {
		db.Close()
		return nil, err
	}
	p := r.Project
```

Replace `resolveConfigProject` with:

```go
// resolveConfigProject returns the project whose overrides apply, or nil when
// no pin applies here: the global defaults are still a real answer. A bad
// --project, a malformed pin, or a pin naming a missing project is an error.
func resolveConfigProject() (*projectContext, error) {
	pctx, err := currentProject()
	if ce, ok := errors.AsType[*core.Error](err); ok && ce.Code == "unresolved" {
		return nil, nil
	}
	return pctx, err
}
```

In `config.go`, add `"errors"` to the imports and remove `"github.com/mtch3n/trellis/internal/resolve"`.

In `internal/cli/board_show.go`, replace the silence check:

```go
			app, err := currentBoard()
			if err != nil {
				// No pin applies here, so Trellis is not in use in this
				// directory, and the SessionStart hook must stay silent.
				if ce, ok := errors.AsType[*core.Error](err); ok && ce.Code == "unresolved" {
					return nil
				}
				return err
			}
```

Add `"errors"` to `board_show.go`'s imports. In all three files, remove any import the compiler now reports as unused (`go build ./internal/cli/` names them).

- [ ] **Step 7: Rewrite `internal/cli/init.go`**

```go
package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/vpath"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var key string
	var noPreset bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Pin this directory to a project, creating the project if needed",
		Long: "Write .trellis here, naming the project -- and, with --board, the board --\n" +
			"that commands run in this directory act on. Commit the file: every clone\n" +
			"and worktree then resolves to the same project.\n\n" +
			"Without --key the key comes from the directory name, and init only ever\n" +
			"creates: an existing project is joined only when named with --key.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if projectFlagKey != "" {
				return core.ErrUsage("init_project_flag", "init names its project with --key, not --project",
					"trellis init --key "+strings.ToUpper(projectFlagKey))
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			reason, err := resolve.Unpinnable(dir)
			if err != nil {
				return err
			}
			if reason != "" {
				return core.ErrUsage("init_location", reason, "cd <project directory>, then trellis init")
			}

			req := core.InitRequest{Dir: dir, Key: key, Join: key != "", BoardName: boardFlag, Preset: !noPreset}
			existing, err := resolve.ReadPin(filepath.Join(dir, resolve.PinFile))
			switch {
			case err == nil:
				req.Existing = &existing
			case errors.Is(err, fs.ErrNotExist):
				if req.Key == "" {
					if req.Key = vpath.KeyFromName(filepath.Base(dir)); req.Key == "" {
						return core.ErrUsage("missing_key",
							fmt.Sprintf("cannot derive a project key from %q", filepath.Base(dir)),
							"trellis init --key <KEY>")
					}
				}
			default:
				return pinFailure(err)
			}
			overridden := parentPin(dir, req.Existing)

			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := cmd.Context()

			res, err := c.InitProject(ctx, req)
			if err != nil {
				return err
			}
			board := res.Board
			if board == nil {
				if b, err := c.SelectBoard(ctx, res.Project.ID, ""); err == nil {
					board = &b
				}
			}
			var cols []core.Column
			if board != nil {
				if cols, err = c.ListColumns(ctx, board.ID); err != nil {
					return err
				}
			}
			notes := initNotes(res, overridden)

			return Emit(cmd, map[string]any{
				"project": res.Project, "board": board, "columns": cols,
				"pin_path": res.PinPath, "created": res.Created, "pin_written": res.Wrote,
				"notes": notes,
			}, func() string { return initTable(res, board, cols, notes) })
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "project key; naming an existing project joins it")
	cmd.Flags().BoolVar(&noPreset, "no-preset", false, "skip seeding default labels")
	return cmd
}

// parentPin is the pin a new pin in dir would override, or nil. A directory
// that holds .git already stops the walk, so nothing above it applies.
func parentPin(dir string, existing *resolve.Pin) *resolve.Pin {
	if existing != nil {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	pin, found, err := resolve.FindPin(filepath.Dir(dir))
	if err != nil || !found {
		return nil
	}
	return &pin
}

func initNotes(res core.InitResult, overridden *resolve.Pin) []string {
	notes := []string{}
	if res.Wrote {
		notes = append(notes, "commit .trellis so every clone and worktree resolves to "+res.Project.Key)
	}
	if overridden != nil {
		notes = append(notes, fmt.Sprintf("this pin overrides %s from %s", overridden.Target, overridden.Path))
	}
	if env := strings.ToUpper(os.Getenv("TRELLIS_PROJECT")); env != "" && env != res.Project.Key {
		notes = append(notes, fmt.Sprintf("TRELLIS_PROJECT=%s is set; commands in this environment act on %s, not on this pin", env, env))
	}
	return notes
}

func initTable(res core.InitResult, board *core.Board, cols []core.Column, notes []string) string {
	var b strings.Builder
	verb := "joined project"
	if res.Created {
		verb = "created project"
	}
	fmt.Fprintf(&b, "%s %s · pin %s", verb, res.Project.Key, res.PinPath)
	if board != nil {
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = c.Name
		}
		fmt.Fprintf(&b, "\nboard %s · columns: %v", board.Name, names)
	}
	for _, n := range notes {
		b.WriteString("\nnote: " + n)
	}
	return b.String()
}
```

Delete `EnsureDefaultLabels` and its comment from `internal/core/label.go`. Its only caller was the old `init`. Also delete the sentence `Use EnsureDefaultLabels for an idempotent version.` from the comment on `SeedDefaultLabels`.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/cli/ ./internal/core/ && go vet ./... && gofmt -l .`
Expected: `ok` for both packages, and nothing printed by `gofmt`.

- [ ] **Step 9: Commit**

```bash
git add internal/cli internal/core/label.go
git commit -m "feat(cli): resolve every command through the nearest pin, and init writes it

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `trellis project new` and `project ls`

**Files:**
- Create: `internal/cli/project.go`
- Modify: `internal/cli/root.go`, to register `newProjectCmd()` in `root.AddCommand`
- Test: `internal/cli/project_cmd_test.go`

**Interfaces:**
- Consumes: `core.CreateProject` (Task 3); `core.ListProjects`, `core.ListBoards` (existing); `openCore`, `Emit` (existing); `pinEnv`, `coreErr` (Task 4).
- Produces: `func newProjectCmd() *cobra.Command`. The JSON output of `project ls` is `{"projects":[{"key","name","boards","created_at"}]}`.

- [ ] **Step 1: Write the failing tests**

`internal/cli/project_cmd_test.go`:

```go
package cli

import (
	"encoding/json/v2"
	"testing"
)

func TestProjectNewCreatesAnUnpinnedProject(t *testing.T) {
	pinEnv(t, "anywhere")
	out := runCmd(t, "project", "new", "research", "--json")
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil || p.Key != "RESEARCH" {
		t.Fatalf("project new = %q, %v", out, err)
	}
	// Reachable by name, although no pin names it.
	if got := showBoard(t, "--project", "RESEARCH").Slug; got != "research" {
		t.Errorf("default board slug = %s", got)
	}
}

func TestProjectNewRefusesReservedAndTakenKeys(t *testing.T) {
	pinEnv(t, "anywhere")
	runCmd(t, "project", "new", "TAKEN")
	for key, want := range map[string]string{"GLOBAL": "reserved_key", "taken": "key_collision", "my_app": "bad_key"} {
		_, err := execCmd("project", "new", key)
		if ce := coreErr(t, err); ce.Code != want {
			t.Errorf("project new %s: code = %s, want %s", key, ce.Code, want)
		}
	}
}

func TestProjectLsListsKeysAndBoardCounts(t *testing.T) {
	pinEnv(t, "anywhere")
	seedProject(t, "ALPHA", "Web")
	seedProject(t, "BETA")
	out := runCmd(t, "project", "ls", "--json")
	var v struct {
		Projects []struct {
			Key    string `json:"key"`
			Boards int    `json:"boards"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, p := range v.Projects {
		got[p.Key] = p.Boards
	}
	if got["ALPHA"] != 2 || got["BETA"] != 1 || len(got) != 2 {
		t.Errorf("projects = %v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run ProjectNew\|ProjectLs`
Expected: FAIL with `unknown command "project"`.

- [ ] **Step 3: Write the command**

`internal/cli/project.go`:

```go
package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Create and list projects"}
	cmd.AddCommand(newProjectNewCmd(), newProjectLsCmd())
	return cmd
}

func newProjectNewCmd() *cobra.Command {
	var noPreset bool
	cmd := &cobra.Command{
		Use:   "new <KEY>",
		Short: "Create a project without pinning any directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			p, err := c.CreateProject(cmd.Context(), args[0], !noPreset)
			if err != nil {
				return err
			}
			return Emit(cmd, p, func() string {
				return fmt.Sprintf("created project %s · pin a directory to it with: trellis init --key %s", p.Key, p.Key)
			})
		},
	}
	cmd.Flags().BoolVar(&noPreset, "no-preset", false, "skip seeding default labels")
	return cmd
}

func newProjectLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List every project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := cmd.Context()
			projects, err := c.ListProjects(ctx)
			if err != nil {
				return err
			}
			type row struct {
				Key       string `json:"key"`
				Name      string `json:"name"`
				Boards    int    `json:"boards"`
				CreatedAt int64  `json:"created_at"`
			}
			rows := make([]row, len(projects))
			for i, p := range projects {
				boards, err := c.ListBoards(ctx, p.ID)
				if err != nil {
					return err
				}
				rows[i] = row{Key: p.Key, Name: p.Name, Boards: len(boards), CreatedAt: p.CreatedAt}
			}
			return Emit(cmd, map[string]any{"projects": rows}, func() string {
				var b strings.Builder
				w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
				for _, r := range rows {
					fmt.Fprintf(w, "%s\t%d boards\t%s\n", r.Key, r.Boards,
						time.UnixMilli(r.CreatedAt).UTC().Format(time.DateOnly))
				}
				w.Flush()
				return strings.TrimRight(b.String(), "\n")
			})
		},
	}
}
```

In `internal/cli/root.go`, add `newProjectCmd()` to the `root.AddCommand(...)` list, directly after `newInitCmd()`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ && gofmt -l internal/cli`
Expected: `ok`, including `TestHelpListsEveryCommand`, and nothing printed by `gofmt`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/project.go internal/cli/project_cmd_test.go internal/cli/root.go
git commit -m "feat(cli): create a project without a directory, and list projects

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Drop project identity

This task removes git identity in one commit: the columns, the struct fields, `EnsureProject`, `Identify`, the git code and every test seed that wrote those columns. Splitting it would leave a commit whose inserts fail against the schema.

**Files:**
- Modify: `internal/store/db.go` (split out `connect`)
- Create: `internal/store/migrate_0013.go`
- Test: `internal/store/migrate_0013_test.go`
- Modify: `internal/core/project.go`:
  - remove `EnsureProject` and its comment
  - remove the three fields from `Project`
  - remove `identity_kind` from `createProject`'s insert
  - update `DeleteProject`'s comment
- Delete:
  - `internal/core/project_test.go`
  - `internal/resolve/git.go`, `internal/resolve/git_test.go`
  - `internal/resolve/giturl.go`, `internal/resolve/giturl_test.go`
  - `internal/resolve/resolve_test.go`
- Shrink: `internal/resolve/resolve.go`, to its package comment and `normalizeDir`
- Modify test seeds:
  - `internal/core/column_test.go` (`seededProject`, `seededProject2`)
  - `internal/core/note_test.go`
  - `internal/core/lease_test.go` (four sites)
  - `internal/core/artifact_test.go`
  - `internal/config/config_test.go` (three inserts)
  - `internal/cli/tui_test.go`
  - `internal/ui/server_test.go` (five sites)

**Interfaces:**
- Consumes: `core.CreateProject` (Task 3).
- Produces:
  - `store.connect(path string) (*sqlx.DB, error)` (unexported)
  - goose version 13, `0013_drop_project_identity.go`
  - `core.Project` becomes `{ID, Key, Name, CreatedAt}`

- [ ] **Step 1: Split `connect` out of `store.Open`**

In `internal/store/db.go`, move everything in `Open` after `defer lock.Unlock()` and before `goose.Up` into a new function. `Open` becomes:

```go
func Open(path string) (*sqlx.DB, error) {
	// (the existing long comment about the migration lock stays here)
	lock := flock.New(path + ".migrate.lock")
	if err := lock.Lock(); err != nil {
		return nil, fmt.Errorf("acquiring migration lock: %w", err)
	}
	defer lock.Unlock()

	db, err := connect(path)
	if err != nil {
		return nil, err
	}
	if err := goose.Up(db.DB, "migrations"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// connect opens path with the mandatory pragmas and prepares goose, without
// migrating. Open wraps it in the migration lock; tests use it to stop at an
// earlier schema version.
func connect(path string) (*sqlx.DB, error) {
	dsn := fmt.Sprintf("file:%s?_txlock=immediate&_time_integer_format=unix_milli&_pragma=%s&_pragma=%s&_pragma=%s&_pragma=%s",
		path,
		url.QueryEscape("journal_mode(WAL)"),
		url.QueryEscape("busy_timeout(10000)"),
		url.QueryEscape("synchronous(NORMAL)"),
		url.QueryEscape("foreign_keys(ON)"),
	)
	db, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One connection: SQLite writes serialize anyway, and a pool would apply
	// pragmas per connection.
	db.SetMaxOpenConns(1)

	var setupErr error
	gooseSetup.Do(func() {
		goose.SetBaseFS(migrationFS)
		goose.SetLogger(goose.NopLogger())
		setupErr = goose.SetDialect("sqlite3")
	})
	if setupErr != nil {
		db.Close()
		return nil, setupErr
	}
	return db, nil
}
```

Run: `go test ./internal/store/`
Expected: `ok`. This is a pure refactor.

- [ ] **Step 2: Write the failing migration tests**

`internal/store/migrate_0013_test.go`:

```go
package store

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
)

func openAtVersion(t *testing.T, version int64) *sqlx.DB {
	t.Helper()
	db, err := connect(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := goose.UpTo(db.DB, "migrations", version); err != nil {
		t.Fatalf("UpTo(%d): %v", version, err)
	}
	return db
}

func mustExec(t *testing.T, db *sqlx.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// seedV12 fills every table a project owns, with the schema as it was before
// 0013, so a cascade anywhere would show up as a missing row.
func seedV12(t *testing.T, db *sqlx.DB) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO project (id, key, identity_kind, identity_value, root_path, name, created_at) VALUES
		   ('p1', 'ALPHA', 'path', '/src/alpha', '/src/alpha', 'ALPHA', 1),
		   ('p2', 'BETA', 'remote', 'github.com/x/beta', '/src/beta', 'BETA', 1)`,
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES ('b1', 'p1', 'alpha', 'alpha', 1, 1)`,
		`INSERT INTO column_ (id, board_id, name, position) VALUES ('c1', 'b1', 'backlog', 0)`,
		`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, created_at, updated_at)
		   VALUES ('k1', 'p1', 'b1', 1, 'c1', 'a', 'First', 1, 1)`,
		`INSERT INTO label (id, project_id, name, description, created_at) VALUES ('l1', 'p1', 'bug', 'A defect', 1)`,
		`INSERT INTO card_label (card_id, label_id) VALUES ('k1', 'l1')`,
		`INSERT INTO knowledge (id, project_id, slug, title, path, content_hash, mtime, size, created_at, updated_at)
		   VALUES ('n1', 'p1', 'design', 'Design', '/kb/design.md', 'h', 1, 1, 1, 1)`,
		`INSERT INTO project_config (project_id, key, value, updated_at) VALUES ('p1', 'lease.ttl', '10m', 1)`,
	} {
		mustExec(t, db, q)
	}
}

func projectColumns(t *testing.T, db *sqlx.DB) []string {
	t.Helper()
	var cols []string
	if err := db.Select(&cols, `SELECT name FROM pragma_table_info('project') ORDER BY cid`); err != nil {
		t.Fatal(err)
	}
	return cols
}

func TestDropProjectIdentityKeepsEveryRow(t *testing.T) {
	db := openAtVersion(t, 12)
	seedV12(t, db)
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}
	for table, want := range map[string]int{
		"project": 2, "board": 1, "column_": 1, "card": 1, "label": 1,
		"card_label": 1, "knowledge": 1, "project_config": 1,
	} {
		var n int
		if err := db.Get(&n, "SELECT count(*) FROM "+table); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%s has %d rows, want %d", table, n, want)
		}
	}
	if got := projectColumns(t, db); !slices.Equal(got, []string{"id", "key", "name", "created_at"}) {
		t.Errorf("project columns = %v", got)
	}
	var fk int
	if err := db.Get(&fk, `PRAGMA foreign_keys`); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v; want enforcement back on", fk, err)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Error("foreign_key_check reports a violation after the migration")
	}
}

func TestDropProjectIdentityRecordsTheOldBindings(t *testing.T) {
	db := openAtVersion(t, 12)
	seedV12(t, db)
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := db.Select(&got,
		`SELECT entity_id || ' ' || field || ' ' || old_value FROM event
		 WHERE entity_type = 'project' AND action = 'unbound' AND actor = 'migration'
		 ORDER BY entity_id, field`); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"p1 identity path:/src/alpha",
		"p1 root_path /src/alpha",
		"p2 identity remote:github.com/x/beta",
		"p2 root_path /src/beta",
	}
	if !slices.Equal(got, want) {
		t.Errorf("unbound events = %v, want %v", got, want)
	}
}

func TestDropProjectIdentityRollsBackOnAForeignKeyViolation(t *testing.T) {
	db := openAtVersion(t, 12)
	seedV12(t, db)
	mustExec(t, db, `PRAGMA foreign_keys = OFF`)
	mustExec(t, db, `INSERT INTO board (id, project_id, name, slug, created_at) VALUES ('orphan', 'missing', 'x', 'x', 1)`)
	mustExec(t, db, `PRAGMA foreign_keys = ON`)

	err := goose.Up(db.DB, "migrations")
	if err == nil || !strings.Contains(err.Error(), "foreign key check failed") {
		t.Fatalf("Up = %v, want the foreign key check to fail it", err)
	}
	if got := projectColumns(t, db); !slices.Contains(got, "root_path") {
		t.Errorf("project columns = %v; the rebuild was not rolled back", got)
	}
	version, err := goose.GetDBVersion(db.DB)
	if err != nil || version != 12 {
		t.Errorf("version = %d, %v; want 12", version, err)
	}
	var events int
	if err := db.Get(&events, `SELECT count(*) FROM event WHERE action = 'unbound'`); err != nil || events != 0 {
		t.Errorf("unbound events = %d, %v; want none after a rollback", events, err)
	}
	var fk int
	if err := db.Get(&fk, `PRAGMA foreign_keys`); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v; want enforcement back on", fk, err)
	}
}
```

Run: `go test ./internal/store/ -run DropProjectIdentity`
Expected: FAIL. The migration does not exist, so the project columns still include `identity_kind`.

- [ ] **Step 3: Write the migration**

`internal/store/migrate_0013.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationNoTxContext("0013_drop_project_identity.go", dropProjectIdentity, restoreProjectIdentity)
}

// dropProjectIdentity removes identity_kind, identity_value and root_path. A
// .trellis pin is now the only link from a directory to a project.
//
// root_path is UNIQUE, which SQLite's DROP COLUMN refuses, so the table is
// rebuilt -- and a rebuild is where this migration could destroy the
// database. Every connection runs with foreign_keys on, and dropping a parent
// table under enforcement is an implicit DELETE that cascades into board,
// card, knowledge and everything else a project owns. PRAGMA foreign_keys is
// a no-op inside a transaction, so this migration runs without goose's
// transaction: it turns enforcement off on one pinned connection, rebuilds
// inside its own transaction, and commits only once foreign_key_check is
// empty. goose's SQL runner cannot do that last step; it never reads the rows
// a PRAGMA returns.
//
// The old values go to the event log first. Migrations run on whatever
// command opens the database, so nobody gets to copy them down beforehand.
func dropProjectIdentity(ctx context.Context, db *sql.DB) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() {
		if _, onErr := conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`); onErr != nil {
			err = errors.Join(err, onErr)
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // a no-op once committed

	now := time.Now().UnixMilli()
	steps := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action, field, old_value)
		  SELECT ?, 'migration', 'project', id, 'unbound', 'root_path', root_path
		  FROM project WHERE root_path IS NOT NULL AND root_path <> ''`, []any{now}},
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action, field, old_value)
		  SELECT ?, 'migration', 'project', id, 'unbound', 'identity', identity_kind || ':' || identity_value
		  FROM project WHERE identity_value IS NOT NULL AND identity_value <> ''`, []any{now}},
		{`CREATE TABLE project_new (
		      id         TEXT PRIMARY KEY,
		      key        TEXT NOT NULL UNIQUE,
		      name       TEXT NOT NULL,
		      created_at INTEGER NOT NULL
		  )`, nil},
		{`INSERT INTO project_new (id, key, name, created_at) SELECT id, key, name, created_at FROM project`, nil},
		{`DROP TABLE project`, nil},
		{`ALTER TABLE project_new RENAME TO project`, nil},
	}
	for _, s := range steps {
		if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
			return err
		}
	}
	if err := foreignKeyCheck(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// foreignKeyCheck fails with every violating row named.
func foreignKeyCheck(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var broken []string
	for rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		broken = append(broken, fmt.Sprintf("%s row %d references a missing %s", table, rowid.Int64, parent))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(broken) > 0 {
		return fmt.Errorf("foreign key check failed after rebuilding project, rolled back: %s",
			strings.Join(broken, "; "))
	}
	return nil
}

// restoreProjectIdentity re-adds the three columns, empty. The values are in
// the event log, not restored.
func restoreProjectIdentity(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`ALTER TABLE project ADD COLUMN identity_kind TEXT NOT NULL DEFAULT 'pin'`,
		`ALTER TABLE project ADD COLUMN identity_value TEXT`,
		`ALTER TABLE project ADD COLUMN root_path TEXT`,
		`CREATE INDEX project_identity ON project(identity_value)`,
		`CREATE UNIQUE INDEX project_root ON project(root_path)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
```

Run: `go test ./internal/store/`
Expected: `ok`. The three new tests pass, and `TestConcurrentOpenAcrossProcesses` still passes.

- [ ] **Step 4: Remove identity from core**

In `internal/core/project.go`:

1. Replace the `Project` struct with:

```go
// Project is a virtual namespace, named by its key. No directory belongs to
// it; a .trellis pin is how a directory reaches it.
type Project struct {
	ID        string `db:"id" json:"id"`
	Key       string `db:"key" json:"key"`
	Name      string `db:"name" json:"name"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
}
```

2. Delete `EnsureProject` and its doc comment.
3. In `createProject`, delete the `identity_kind stays NOT NULL` comment and replace the insert with:

```go
	if _, err := tx.Exec(
		`INSERT INTO project (id, key, name, created_at) VALUES (?, ?, ?, ?)`,
		p.ID, p.Key, p.Name, p.CreatedAt); err != nil {
		return Project{}, err
	}
```

4. In `DeleteProject`'s doc comment, replace the last sentence, `Running trellis in the repository again creates a fresh, empty project.`, with: `A pin that still names the deleted key then fails with project_not_found; trellis init in that directory creates a fresh, empty project.`

Delete `internal/core/project_test.go`. It tested only `EnsureProject`, and `project_init_test.go` covers creation.

In `internal/core/board.go`, fix the comment near `SetDefaultBoard` that says the first board "is created by EnsureProject". It should say `is created by createProject`.

- [ ] **Step 5: Remove git identity from resolve**

```bash
git rm internal/resolve/git.go internal/resolve/git_test.go internal/resolve/giturl.go internal/resolve/giturl_test.go internal/resolve/resolve_test.go
```

Replace `internal/resolve/resolve.go` with:

```go
// Package resolve maps a working directory to the project it is pinned to.
// A .trellis file is the only link; see FindPin.
package resolve

import "path/filepath"

// normalizeDir resolves symlinks and canonicalizes a directory so that two
// spellings of the same place compare equal. A path that cannot be resolved --
// one that does not exist yet -- is only cleaned, which keeps this usable as a
// plain comparison helper.
func normalizeDir(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}
```

- [ ] **Step 6: Update every test seed**

`internal/core/column_test.go`, in both `seededProject` and `seededProject2`: drop the `IdentityKind`, `IdentityValue` and `RootPath` fields from the literal, and use this insert. Also change the comment above `seededProject` to say `rather than calling createProject` instead of `EnsureProject`, and make the same change where that comment mentions `EnsureProject` a second time.

```go
	_, err := c.db.Exec(
		`INSERT INTO project (id, key, name, created_at) VALUES (?, ?, ?, ?)`,
		p.ID, p.Key, p.Name, p.CreatedAt)
```

`internal/core/note_test.go` and the four sites in `internal/core/lease_test.go` share one pattern. Replace each `id := resolve.Identity{...}` block and the `EnsureProject` call after it. The keys are:

| File | Key |
|---|---|
| `note_test.go` | `NOTE` |
| `lease_test.go`, first site | `CLAIM` |
| `lease_test.go`, second site | `CLAIMSPEC` |
| `lease_test.go`, third site | `RELEASE` |
| `lease_test.go`, fourth site | `DONEREL` |

For example, the `CLAIM` site becomes:

```go
	proj, err := core.CreateProject(ctx, "CLAIM", false)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
```

`internal/core/artifact_test.go`:

```go
	project, err := c.CreateProject(t.Context(), "ARTIFACT", false)
```

`internal/ui/server_test.go` has five sites. Replace each `c.EnsureProject(ctx-or-context.Background(), resolve.Identity{... SuggestedKey: K ...})` with `c.CreateProject(<same ctx>, K, false)`. The keys are `UITEST`, `P5TEST`, the loop variable `key` (`GONE`/`KEPT`), `ORDER` and `EVT`.

`internal/config/config_test.go` has three inserts. Each becomes:

```go
	_, err := db.ExecContext(ctx,
		`INSERT INTO project (id, key, name, created_at) VALUES (?, ?, ?, ?)`,
		projectID, "TEST", "Test Project", 0)
```

Keep each site's own `projectID`, key and name values; only the identity columns and their arguments go.

`internal/cli/tui_test.go`:

```go
	_, err = db.Exec(`INSERT INTO project (id, key, name, created_at) VALUES ('p', 'TEST', 'test', 1)`)
```

Remove the `"github.com/mtch3n/trellis/internal/resolve"` import from every test file that no longer uses it.

- [ ] **Step 7: Verify that nothing references identity anymore**

Run: `grep -rn 'EnsureProject\|Identify(\|resolve\.Identity\|identity_kind\|identity_value\|root_path\|IdentityKind\|RootPath\|NormalizeRemote\|resolve\.Repo' --include='*.go' .`
Expected: the only matches are in `internal/store/migrate_0013.go`, `internal/store/migrate_0013_test.go`, and the `0001_init.sql` history, which is not a `.go` file.

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all `ok`, and nothing printed by `gofmt`.

- [ ] **Step 8: Commit**

```bash
git add -A internal/store internal/core internal/resolve internal/config internal/cli internal/ui
git commit -m "feat(store): drop git identity from projects, keeping the old bindings in the event log

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

Before committing, check `git status --short` to confirm that `git add -A` staged only files this task touched.

---

### Task 7: Doctor reports where the project came from

**Files:**
- Modify: `internal/cli/doctor.go`:
  - `checkProject`
  - a new `checkProjectKeys`
  - a new `openExistingDB`
  - `runDoctor`
- Test: `internal/cli/doctor_test.go`

**Interfaces:**
- Consumes: `resolve.FindPin` (Task 2); `vpath.ValidKey` (Task 1); `projectFlagKey` (existing); `pinEnv`, `writePin`, `seedProject` (Task 4).
- Produces: doctor check names `project` and `project keys`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/doctor_test.go`. Add `"os"` and `"github.com/mtch3n/trellis/internal/home"` and `"github.com/mtch3n/trellis/internal/store"` to its imports if they are missing.

```go
func TestCheckProjectFromAPin(t *testing.T) {
	dir := pinEnv(t, "app")
	seedProject(t, "APP")
	writePin(t, dir, "/APP\n")
	got := checkProject()
	if got.Status != checkOK || !strings.Contains(got.Detail, "/APP (pin ") {
		t.Errorf("check = %+v", got)
	}
}

func TestCheckProjectWithoutAPinWarns(t *testing.T) {
	pinEnv(t, "loose")
	got := checkProject()
	if got.Status != checkWarn || got.Fix != "trellis init --key <KEY>" {
		t.Errorf("check = %+v", got)
	}
}

func TestCheckProjectNamesItsSource(t *testing.T) {
	pinEnv(t, "loose")
	t.Setenv("TRELLIS_PROJECT", "envkey")
	if got := checkProject(); !strings.Contains(got.Detail, "ENVKEY (from TRELLIS_PROJECT)") {
		t.Errorf("env: %+v", got)
	}
	projectFlagKey = "flagkey"
	t.Cleanup(func() { projectFlagKey = "" })
	if got := checkProject(); !strings.Contains(got.Detail, "FLAGKEY (from --project)") {
		t.Errorf("flag: %+v", got)
	}
}

func TestCheckProjectWarnsWhenThePinnedProjectIsMissing(t *testing.T) {
	dir := pinEnv(t, "clone")
	seedProject(t, "OTHER")
	writePin(t, dir, "/GHOST\n")
	got := checkProject()
	if got.Status != checkWarn || got.Fix != "trellis init" {
		t.Errorf("check = %+v", got)
	}
}

func TestCheckProjectKeysFlagsKeysAPinCannotName(t *testing.T) {
	pinEnv(t, "anywhere")
	seedProject(t, "GOOD")
	path, err := home.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO project (id, key, name, created_at) VALUES ('x', 'MY_APP', 'MY_APP', 1)`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	got := checkProjectKeys()
	if got.Status != checkWarn || !strings.Contains(got.Detail, "MY_APP") || strings.Contains(got.Detail, "GOOD") {
		t.Errorf("check = %+v", got)
	}
}

func TestCheckProjectKeysWithoutADatabase(t *testing.T) {
	t.Setenv("TRELLIS_HOME", t.TempDir())
	if got := checkProjectKeys(); got.Status != checkOK {
		t.Errorf("check = %+v", got)
	}
}
```

In `TestRunDoctorCoversEveryAreaAndNeverPanics`, add `"project keys"` to the list of names.

Run: `go test ./internal/cli/ -run 'CheckProject|RunDoctor'`
Expected: FAIL. `checkProjectKeys` is undefined.

- [ ] **Step 2: Write the checks**

In `internal/cli/doctor.go`, replace `checkProject`, and add the two helpers:

```go
// checkProject reports which project this directory acts on, and how that was
// decided.
func checkProject() Check {
	switch {
	case projectFlagKey != "":
		return ok("project", strings.ToUpper(projectFlagKey)+" (from --project)")
	case os.Getenv("TRELLIS_PROJECT") != "":
		return ok("project", strings.ToUpper(os.Getenv("TRELLIS_PROJECT"))+" (from TRELLIS_PROJECT)")
	}
	dir, err := os.Getwd()
	if err != nil {
		return warn("project", "cannot read the working directory: "+err.Error(), "")
	}
	pin, found, err := resolve.FindPin(dir)
	if err != nil {
		return warn("project", err.Error(), "trellis init --key <KEY>")
	}
	if !found {
		return warn("project", "no .trellis pin in this directory or any parent", "trellis init --key <KEY>")
	}
	detail := fmt.Sprintf("%s (pin %s)", pin.Target, pin.Path)
	db, err := openExistingDB()
	if err != nil {
		return ok("project", detail)
	}
	defer db.Close()
	var n int
	if err := db.Get(&n, `SELECT count(*) FROM project WHERE key = ?`, pin.Target.Project); err == nil && n == 0 {
		return warn("project", detail+", but this database has no such project", "trellis init")
	}
	return ok("project", detail)
}

// checkProjectKeys lists projects whose key predates the key grammar. They
// stay reachable with --project, but no pin can name them.
func checkProjectKeys() Check {
	db, err := openExistingDB()
	if err != nil {
		return ok("project keys", "no database yet")
	}
	defer db.Close()
	var keys []string
	if err := db.Select(&keys, `SELECT key FROM project ORDER BY key`); err != nil {
		return warn("project keys", "cannot read project keys: "+err.Error(), "trellis maintenance")
	}
	bad := slices.DeleteFunc(keys, vpath.ValidKey)
	if len(bad) == 0 {
		return ok("project keys", "every key can be pinned")
	}
	return warn("project keys",
		fmt.Sprintf("no pin can name %s: %s", plural(len(bad), "this project", "these projects"), strings.Join(bad, ", ")),
		"trellis --project <KEY> ...   # still reachable by name")
}

// openExistingDB opens the database only when it already exists, so a check
// never creates one.
func openExistingDB() (*sqlx.DB, error) {
	path, err := home.DBPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return store.Open(path)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
```

In `runDoctor`, change the last line to:

```go
	return append(checks, checkProject(), checkProjectKeys(), checkVectorSearch(cfg))
```

Add the imports `"slices"`, `"github.com/jmoiron/sqlx"` and `"github.com/mtch3n/trellis/internal/vpath"`, and keep `resolve`. Package `cli` has no `plural` of its own (core's is unexported), so the one above is new.

- [ ] **Step 3: Run the tests to verify they pass**

Run: `go test ./internal/cli/ && go vet ./internal/cli/ && gofmt -l internal/cli`
Expected: `ok`, and nothing printed by `gofmt`.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "feat(cli): doctor names the pin a directory resolves through

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Documentation, this repository's pin, and the release gates

**Files:**
- Modify:
  - `CLAUDE.md`
  - `README.md`
  - `PRODUCT.md`
  - `plugin/skills/trellis/SKILL.md`
  - `scripts/tests/test_plugin_cli.py`
  - `.gitignore`
- Create: `.trellis`

- [ ] **Step 1: Update `CLAUDE.md`**

In the Architecture list, replace the `internal/resolve` bullet with:

```markdown
- `internal/resolve` — maps a working directory to a project through the
  nearest `.trellis` pin. `--project` and `TRELLIS_PROJECT` override it. There
  is no git lookup.
- `internal/vpath` — the virtual path grammar a pin holds: `/KEY` and
  `/KEY/boards/<slug>`.
```

Replace the invariant "The pin walk has a boundary" with:

```markdown
- **The pin walk has boundaries.**
  - It stops after a directory containing `.git`, so a stray `.trellis` above
    a repository cannot capture it. `.git` is checked with `Lstat`; git is
    never run.
  - It never checks `$HOME` or the filesystem root: `$HOME/.trellis` is the
    storage root.
  - Any error other than not-exist is an error, never absence.
  - Compare paths through `normalizeDir`: macOS and Windows hand back other
    spellings of the same directory.
- **Projects are virtual.** A project is its key. `.trellis` holds `/KEY` or
  `/KEY/boards/<slug>`, is meant to be committed, and is written only by
  `trellis init` through `writeAtomic`.
```

- [ ] **Step 2: Update `README.md` and `PRODUCT.md`**

`README.md`, line 18: replace `Projects are identified by Git repository URL; a repository with no remote gets a local pin file.` with `A project is a virtual namespace; a committed .trellis file pins a directory to it.`

`README.md`, line 54: replace the comment `# Enable trellis in the current git repository` with `# Pin this directory to a project (commit the .trellis it writes)`.

`PRODUCT.md`, line 47: read the surrounding sentence, then rewrite its resolution order (`pin, git remote, then git root`) as `--project, TRELLIS_PROJECT, then the nearest .trellis pin`. Keep the sentence's grammar intact.

- [ ] **Step 3: Update the core skill**

In `plugin/skills/trellis/SKILL.md`, find the command reference block. It is the fenced block that lists commands with `#` comments, near the `knowledge lint` line. Add these lines to it, matching its column alignment:

```
trellis init --key <KEY>                        # pin this directory; commit .trellis
trellis project new <KEY>                       # a project no directory pins yet
trellis project ls                              # every project
```

- [ ] **Step 4: Update the real-CLI integration test**

In `scripts/tests/test_plugin_cli.py`, replace `cli("init", "--pin", "--key", "HOOKTEST")` with `cli("init", "--key", "HOOKTEST")`.

At the end of `test_claim_resume_compact_and_handoff`, add:

```python
            unpinned = Path(directory) / "unpinned"
            unpinned.mkdir()
            silent = subprocess.run(
                [os.sys.executable, "-B", str(ROOT / "plugin/hooks/trellis_hook.py"), "session-start"],
                input=json.dumps(dict(session_id=session, cwd=str(unpinned), source="startup")),
                cwd=directory, env=env, text=True, capture_output=True, timeout=10,
            )
            self.assertEqual(silent.returncode, 0, silent.stderr)
            self.assertEqual(silent.stdout, "")

            ghost = Path(directory) / "ghost"
            ghost.mkdir()
            (ghost / ".trellis").write_text("/GHOST\n")
            missing = subprocess.run(
                [os.sys.executable, "-B", str(ROOT / "plugin/hooks/trellis_hook.py"), "session-start"],
                input=json.dumps(dict(session_id=session, cwd=str(ghost), source="startup")),
                cwd=directory, env=env, text=True, capture_output=True, timeout=10,
            )
            self.assertIn("unavailable", json.loads(missing.stdout)["hookSpecificOutput"]["additionalContext"])
```

Run: `go build -o /tmp/trellis-it ./cmd/trellis && TRELLIS_TEST_BINARY=/tmp/trellis-it python3 -B -m unittest discover -s scripts/tests -v`
Expected: every test passes, including `test_claim_resume_compact_and_handoff`.

- [ ] **Step 5: Pin this repository**

In `.gitignore`, delete these two lines:

```
# Per-checkout project pin written by `trellis init --pin`; it is local state.
/.trellis
```

Write the pin, then check that `init` reads it. Use a throwaway storage root. The new binary migrates any database it opens, and the author's real database is theirs to migrate, after taking a backup (Step 8).

```bash
printf '/TRELLIS\n' > .trellis
go build -o /tmp/trellis-it ./cmd/trellis
TRELLIS_HOME="$(mktemp -d)" /tmp/trellis-it init --json
```

Expected: the JSON shows `"key":"TRELLIS"`, `"created":true` and `"pin_written":false`, and `.trellis` is unchanged.

- [ ] **Step 6: Run every gate**

```bash
go build ./...
go test ./...
go test -race ./internal/store/ ./internal/core/
go vet ./...
gofmt -l .
staticcheck ./...
GOOS=windows go build ./...
GOOS=darwin go build ./...
python3 -B -m unittest discover -s scripts/tests
```

Expected: every command succeeds, and `gofmt -l .` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add CLAUDE.md README.md PRODUCT.md plugin/skills/trellis/SKILL.md scripts/tests/test_plugin_cli.py .gitignore .trellis
git commit -m "docs: projects are pinned, not detected; commit this repository's pin

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 8: Hand over the upgrade**

Do not run anything against the author's real database. Report three things.

1. **Back up first.** The first command the new binary runs against `~/.trellis/trellis.db` applies migration `0013`, and that includes the SessionStart hook. Back up before installing:
   ```bash
   trellis backup ~/trellis-before-0013.db
   ```
2. **Restart the daemon.** The running daemon still uses the old binary, and the old binary cannot insert projects into the new schema. After installing, run `trellis daemon restart`.
3. **Reconnect the other projects.** After the migration, this query lists the directories it recorded:

```bash
sqlite3 ~/.trellis/trellis.db "SELECT p.key, e.old_value FROM event e
  JOIN project p ON p.id = e.entity_id
  WHERE e.entity_type = 'project' AND e.action = 'unbound' AND e.field = 'root_path'"
```

Each listed directory needs `trellis init --key <KEY>` run once, and the resulting `.trellis` committed where the repository allows it.
