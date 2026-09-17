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
