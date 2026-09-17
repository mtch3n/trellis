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
// when the work started. It is the only place the number lives.
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

	// The storage root is the directory holding the database in every real
	// install. This transaction only reads that path, and ends before the file
	// work so the schema step can have the connection.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	file, err := mainDatabasePath(ctx, tx)
	if err := errors.Join(err, tx.Rollback()); err != nil {
		return err
	}
	// An in-memory database has no files to move.
	root := ""
	if file != "" {
		root = filepath.Dir(file)
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

// vaultParents lists every directory that may hold a vault: each directory
// under root/projects, then root/global. It lists rather than globs, since a
// root may contain a pattern character such as '[' or '*'.
func vaultParents(root string) ([]string, error) {
	projects := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projects)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var parents []string
	for _, e := range entries {
		dir := filepath.Join(projects, e.Name())
		st, err := os.Stat(dir) // follows a symlinked project, as a glob would
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		if st.IsDir() {
			parents = append(parents, dir)
		}
	}
	return append(parents, filepath.Join(root, "global")), nil
}

func (fw *fileWork) moveVaults(root string) error {
	parents, err := vaultParents(root)
	if err != nil {
		return err
	}
	for _, parent := range parents {
		from, to := filepath.Join(parent, "knowledge"), filepath.Join(parent, "vault")
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
		if err := atomicfile.SyncDir(parent); err != nil {
			return err
		}
	}
	return nil
}

func (fw *fileWork) rewrite(root string) error {
	parents, err := vaultParents(root)
	if err != nil {
		return err
	}
	for _, parent := range parents {
		vault := filepath.Join(parent, "vault")
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
			return fw.edit(path, rewriteEntry)
		})
		if err != nil {
			return err
		}
	}
	templates := filepath.Join(root, "templates")
	entries, err := os.ReadDir(templates)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(templates, e.Name())
		if err := fw.edit(path, func(s string) string { return renameTopLevelKey(s, "verify", "resolve", true) }); err != nil {
			return err
		}
	}
	config := filepath.Join(root, "config.yaml")
	if _, err := os.Stat(config); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return fw.edit(config, func(s string) string { return renameTopLevelKey(s, "lease", "claim", false) })
}

// edit rewrites path through change. The change is recorded as soon as the
// new bytes are in place, so undo restores it even if a later step fails.
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
	fw.rewritten = append(fw.rewritten, fileChange{path: path, old: raw, oldHash: hash(string(raw)), newHash: hash(next)})
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	last := &fw.rewritten[len(fw.rewritten)-1]
	last.newSize, last.newMTime = st.Size(), st.ModTime().UnixMilli()
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
		if lines[0] != "---" {
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

// keyPattern is a project key as vpath spells it: upper-case segments joined
// by hyphens (MY-APP).
const keyPattern = `[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*`

var (
	// oldAddress is an address written before the rename, at the start of a
	// token. Its key is matched upper-case only, as Trellis prints it, so a
	// filesystem path such as /home/me/knowledge/ is never touched.
	oldAddress = regexp.MustCompile("(?m)(^|[\\s\\[(\"'`,:])(/" + keyPattern + ")/knowledge/")
	// oldWikilinkAddress is an old address as a [[wikilink]] target. A
	// target is always a reference, and core reads its key in any case.
	oldWikilinkAddress = regexp.MustCompile(`(\[\[\s*)(/(?i:` + keyPattern + `))/knowledge/`)
	// oldReference is an old address as a whole link target, in any case.
	oldReference = regexp.MustCompile(`^(/(?i:` + keyPattern + `))/knowledge/`)
)

// rewriteAddresses rewrites old addresses in prose: a card, a comment.
func rewriteAddresses(s string) string {
	return oldAddress.ReplaceAllString(s, "${1}${2}/vault/")
}

// rewriteEntry rewrites old addresses in an entry file, where a wikilink
// target also counts with a lower-case key.
func rewriteEntry(s string) string {
	return rewriteAddresses(oldWikilinkAddress.ReplaceAllString(s, "${1}${2}/vault/"))
}

// rewriteReference rewrites link.to_raw, which is always one reference.
func rewriteReference(s string) string {
	return oldReference.ReplaceAllString(s, "${1}/vault/")
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
		// A card's linked event names the relation it wrote.
		`UPDATE event SET field = 'cites' WHERE field = 'documents'`,
		`UPDATE project_config SET key = 'claim.ttl' WHERE key = 'lease.ttl'`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("%s: %w", firstLine(q), err)
		}
	}

	if err := carryHashes(ctx, tx, root, rewritten); err != nil {
		return err
	}
	for _, c := range []struct {
		table, col string
		change     func(string) string
	}{
		{"card", "body_md", rewriteAddresses},
		{"comment", "body_md", rewriteAddresses},
		{"link", "to_raw", rewriteReference},
	} {
		if err := rewriteColumn(ctx, tx, c.table, c.col, c.change); err != nil {
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
// Only values containing "knowledge" can change, so only those are read.
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
