package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/store"
)

// TestKnowledgeEditRequiresAVersion is R1: a whole-field replacement without
// the version the caller read must be refused, and must not touch the file.
func TestKnowledgeEditRequiresAVersion(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	before, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != "version_required" || e.Exit != 2 {
		t.Fatalf("err = %v, want version_required with exit 2", err)
	}

	after, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("file changed despite the missing version:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestConcurrentKnowledgeEditsFirstToFinishWins races eight independent
// *Core connections, each standing in for a separate writer process, against
// the same database file and KB root. The first to finish must win; every
// other writer must see a conflict, not a silently discarded edit.
func TestConcurrentKnowledgeEditsFirstToFinishWins(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.db")
	root := filepath.Join(dir, "kb")

	const n = 8
	cores := make([]*Core, n)
	for i := range cores {
		db, err := store.Open(dbPath)
		if err != nil {
			t.Fatalf("store.Open %d: %v", i, err)
		}
		t.Cleanup(func() { db.Close() })
		cores[i] = New(db, FixedClock{MS: 1_757_000_000_000}, fmt.Sprintf("writer:%d", i)).WithKBRoot(root)
	}

	p := seededProject(t, cores[0])
	doc, err := cores[0].CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy", Body: "v0\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	start := make(chan struct{})
	results := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := cores[i].EditKnowledge(t.Context(), p.ID, doc.Slug,
				fmt.Sprintf("writer %d\n", i), ptr(int64(1)))
			results[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	winner := -1
	for i, rerr := range results {
		if rerr == nil {
			if winner != -1 {
				t.Fatalf("more than one writer succeeded: %d and %d", winner, i)
			}
			winner = i
			continue
		}
		var e *Error
		if !errors.As(rerr, &e) || e.Code != "conflict" || e.Exit != 4 {
			t.Fatalf("writer %d: err = %v, want a conflict with exit 4", i, rerr)
		}
	}
	if winner == -1 {
		t.Fatal("no writer succeeded")
	}

	reloaded, err := cores[0].LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if reloaded.Version != 2 {
		t.Errorf("Version = %d, want 2", reloaded.Version)
	}
	raw, err := os.ReadFile(reloaded.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("writer %d\n", winner)
	if !strings.Contains(string(raw), want) {
		t.Errorf("file = %q, want it to contain the winner's body %q", raw, want)
	}
}

// externalWriter simulates a program outside Trellis -- an editor -- writing
// straight to a knowledge file while a Trellis write is in flight. Attaching
// it as a Policy runs it from inside checkWrite, which is exactly where the
// real defect let an outside write get silently discarded.
type externalWriter struct{ path string }

func (externalWriter) Name() string { return "external-writer" }

func (e externalWriter) Check(context.Context, ProposedWrite) error {
	f, err := os.OpenFile(e.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("EXTERNAL\n")
	return err
}

// TestADirectEditDuringAWriteIsNotOverwritten is defect #2: a write to the
// file by another program between the refresh and the rename must not be
// silently discarded.
func TestADirectEditDuringAWriteIsNotOverwritten(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	c.WithPolicies(externalWriter{path: doc.Path})

	newTitle := "Renamed"
	_, err = c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{
		Title: &newTitle, IfVersion: &doc.Version,
	})
	var e *Error
	if !errors.As(err, &e) || e.Code != "conflict" || e.Exit != 4 {
		t.Fatalf("err = %v, want a conflict with exit 4", err)
	}

	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(raw, []byte("EXTERNAL\n")) {
		t.Errorf("file = %q, want it to still end with the external write", raw)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Deploy" {
		t.Errorf("title in file = %q, want the edit not to have landed", fm.Title)
	}
}

// TestReplaceIfUnchanged is R3's direct unit test.
func TestReplaceIfUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := ContentHash("original\n")

	if err := replaceIfUnchanged(path, []byte("should not land\n"), "not-the-real-hash"); !errors.Is(err, errFileChanged) {
		t.Fatalf("err = %v, want errFileChanged", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "original\n" {
		t.Errorf("file = %q, want unchanged after a wrong base", raw)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if strings.Contains(ent.Name(), ".tmp-") {
			t.Errorf("leftover temp file %s after a failed replace", ent.Name())
		}
	}

	if err := replaceIfUnchanged(path, []byte("replaced\n"), base); err != nil {
		t.Fatalf("replaceIfUnchanged with the right base: %v", err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "replaced\n" {
		t.Errorf("file = %q, want replaced", raw)
	}
}

// TestAFailedEditPutsTheFileBack is defect #4: the undo must happen while the
// transaction (and so the write lock) is still held, and must be provable
// from the row read directly, without a refresh papering over a bad restore.
func TestAFailedEditPutsTheFileBack(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	original, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON event BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	defer func() {
		if _, err := c.db.Exec(`DROP TRIGGER boom`); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	}()

	newBody := "v2\n"
	_, err = c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{Body: &newBody, IfVersion: &doc.Version})
	if err == nil {
		t.Fatal("edit succeeded despite the trigger, want it to fail")
	}

	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, original) {
		t.Errorf("file = %q, want the original bytes restored", raw)
	}

	var version int64
	if err := c.db.Get(&version, `SELECT version FROM knowledge WHERE id = ?`, doc.ID); err != nil {
		t.Fatal(err)
	}
	if version != doc.Version {
		t.Errorf("row version = %d, want unchanged at %d", version, doc.Version)
	}
}

// TestEscalateRefusesASlugTheGlobalVaultHas is R7.
func TestEscalateRefusesASlugTheGlobalVaultHas(t *testing.T) {
	c, p, _ := kbCore(t)
	other, err := c.EnsureProject(t.Context(), resolve.Identity{Kind: "test", Value: "other", SuggestedKey: "OTHER"})
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	first, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy"})
	if err != nil {
		t.Fatalf("CreateKnowledge (first): %v", err)
	}
	second, err := c.CreateKnowledge(t.Context(), other.ID, NewKnowledge{Title: "Deploy"})
	if err != nil {
		t.Fatalf("CreateKnowledge (second): %v", err)
	}

	escalated, err := c.EscalateKnowledge(t.Context(), p.ID, first.Slug, "shared across projects")
	if err != nil {
		t.Fatalf("EscalateKnowledge (first): %v", err)
	}
	globalRaw, err := os.ReadFile(escalated.Path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.EscalateKnowledge(t.Context(), other.ID, second.Slug, "also shared")
	var e *Error
	if !errors.As(err, &e) || e.Code != "global_slug_taken" || e.Exit != 4 {
		t.Fatalf("err = %v, want global_slug_taken with exit 4", err)
	}

	after, err := os.ReadFile(escalated.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(globalRaw, after) {
		t.Errorf("the global file changed after the refused escalation")
	}

	reloaded, err := c.LoadKnowledge(t.Context(), other.ID, second.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge for the second project's entry: %v", err)
	}
	if reloaded.Global {
		t.Error("second entry became global despite the refusal")
	}
	if _, err := os.Stat(second.Path); err != nil {
		t.Errorf("second entry's file should remain at %s: %v", second.Path, err)
	}
}

// TestAFailedEscalateMovesTheFileBack is R8 for EscalateKnowledge.
func TestAFailedEscalateMovesTheFileBack(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	original := doc.Path

	if _, err := c.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON event BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason"); err == nil {
		t.Fatal("EscalateKnowledge succeeded despite the trigger")
	}

	if _, err := os.Stat(original); err != nil {
		t.Errorf("file should be back at %s: %v", original, err)
	}
	globalDir := filepath.Join(c.kbRoot, "global", "knowledge")
	if _, err := os.Stat(filepath.Join(globalDir, filepath.Base(original))); !os.IsNotExist(err) {
		t.Errorf("file should not remain in the global directory")
	}

	if _, err := c.db.Exec(`DROP TRIGGER boom`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}

	reloaded, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge after dropping the trigger: %v", err)
	}
	if reloaded.Global {
		t.Error("entry became global despite the failed escalate")
	}
}

// TestAFailedDemoteMovesTheFileBack is R8 for DemoteKnowledge.
func TestAFailedDemoteMovesTheFileBack(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	escalated, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason")
	if err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	original := escalated.Path

	if _, err := c.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON event BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, err := c.DemoteKnowledge(t.Context(), doc.Slug, "wrong call"); err == nil {
		t.Fatal("DemoteKnowledge succeeded despite the trigger")
	}

	if _, err := os.Stat(original); err != nil {
		t.Errorf("file should be back at %s: %v", original, err)
	}

	if _, err := c.db.Exec(`DROP TRIGGER boom`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}

	reloaded, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge after dropping the trigger: %v", err)
	}
	if !reloaded.Global {
		t.Error("entry demoted despite the failed call")
	}
}

// TestMoveFileNeverReplaces is R6 and defect #1: escalating two projects'
// same-named entry must not let the second replace the first's file.
func TestMoveFileNeverReplaces(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()
	src := filepath.Join(srcDir, "deploy.md")
	if err := os.WriteFile(src, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(destDir, "deploy.md")
	if err := os.WriteFile(dest, []byte("theirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := moveFile(src, destDir)
	var e *Error
	if !errors.As(err, &e) || e.Code != "path_taken" || e.Exit != 4 {
		t.Fatalf("err = %v, want path_taken with exit 4", err)
	}

	srcRaw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(srcRaw) != "mine\n" {
		t.Errorf("src = %q, want unchanged", srcRaw)
	}
	destRaw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(destRaw) != "theirs\n" {
		t.Errorf("dest = %q, want unchanged", destRaw)
	}
}

// TestAFailedDeletePutsTheFileBack is R5.
func TestAFailedDeletePutsTheFileBack(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	original, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}

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

	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatalf("file should still exist: %v", err)
	}
	if !bytes.Equal(raw, original) {
		t.Errorf("file = %q, want the original bytes", raw)
	}

	var count int
	if err := c.db.Get(&count, `SELECT COUNT(*) FROM knowledge WHERE id = ?`, doc.ID); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1 (still present)", count)
	}
}
