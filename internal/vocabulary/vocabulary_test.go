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
	"os/exec"
	"path"
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
	"docs/superpowers/", "CLAUDE.md", "internal/vocabulary/",
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
	for _, rel := range repositoryFiles(t, root) {
		if isSkipped(rel) || !scanned[path.Ext(rel)] {
			continue
		}
		if err := scanFile(filepath.Join(root, filepath.FromSlash(rel)), rel, hits); err != nil {
			t.Fatal(err)
		}
	}
	return hits
}

// repositoryFiles lists the files git knows about -- tracked, plus new files
// not yet committed and not ignored -- relative to root with forward slashes.
// A walk of the directory would also read what each machine ignores (.serena/,
// .claude/, build output), so the counts, and the allowlist, would differ
// between a developer's checkout and CI.
func repositoryFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for name := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	slices.Sort(files)
	return slices.Compact(files)
}

// scanFile records every retired-word hit in the file at abs under rel. A
// tracked file deleted in the working tree has nothing to scan.
func scanFile(abs, rel string, hits map[key][]int) error {
	f, err := os.Open(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
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
		line = strings.TrimSuffix(line, "\r") // a Windows checkout may convert line endings
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

// A machine's ignored files must not change the counts, but a new file must
// be checked before anyone remembers to commit it.
func TestRepositoryFilesSkipsIgnoredButKeepsNewFiles(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	for name, body := range map[string]string{
		"tracked.go":       "package x\n",
		".gitignore":       "local/\n",
		"new.md":           "not committed yet\n",
		"local/editor.yml": "machine-local\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "tracked.go", ".gitignore")

	got := repositoryFiles(t, root)
	if want := []string{".gitignore", "new.md", "tracked.go"}; !slices.Equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}
