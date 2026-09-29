package core

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/atomicfile"
)

// The kinds of thing rm puts in the trash.
const (
	TrashProject  = "project"
	TrashBoard    = "board"
	TrashCard     = "card"
	TrashEntry    = "entry"
	TrashArtifact = "artifact"
)

// TrashItem is one trashed thing: the snapshot of its rows, and where its
// files went.
type TrashItem struct {
	ID         string `db:"id" json:"id"`
	ProjectID  string `db:"project_id" json:"-"`
	ProjectKey string `db:"project_key" json:"project"`
	Kind       string `db:"kind" json:"kind"`
	ItemID     string `db:"item_id" json:"item_id"`
	Name       string `db:"name" json:"name"`
	Title      string `db:"title" json:"title,omitempty"`
	Seq        *int64 `db:"seq" json:"-"`
	TrashedAt  int64  `db:"trashed_at" json:"trashed_at"`
	TrashedBy  string `db:"trashed_by" json:"trashed_by"`
	Rows       string `db:"rows" json:"-"`
	Files      string `db:"files" json:"-"`
}

// snapshot is everything a restore puts back: rows in the order they were
// found, the rows whose reference the delete set to NULL, and links, which
// carry no foreign key.
type snapshot struct {
	Rows    []snapRow        `json:"rows"`
	Relinks []relink         `json:"relinks,omitempty"`
	Links   []map[string]any `json:"links,omitempty"`
}

type snapRow struct {
	Table string         `json:"table"`
	Row   map[string]any `json:"row"`
}

// relink is a reference ON DELETE SET NULL cleared: entries that were on a
// trashed board.
type relink struct {
	Table  string `json:"table"`
	Column string `json:"column"`
	Value  any    `json:"value"`
	IDs    []any  `json:"ids"`
}

// trashFile is one file or directory moved into the trash, both paths
// relative to the storage root.
type trashFile struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type fkEdge struct {
	child, col, parentCol, onDelete string
}

// schema is the foreign-key graph: which tables point at each table, and the
// order to insert into so every parent comes first.
type schema struct {
	children map[string][]fkEdge
	parents  map[string][]fkEdge // keyed by child table; child and parent swapped
	rank     map[string]int
}

func loadSchema(tx *sqlx.Tx) (*schema, error) {
	var tables []string
	if err := tx.Select(&tables, `SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND sql NOT LIKE 'CREATE VIRTUAL%'
		ORDER BY name`); err != nil {
		return nil, err
	}
	s := &schema{children: map[string][]fkEdge{}, parents: map[string][]fkEdge{}, rank: map[string]int{}}
	for _, t := range tables {
		var fks []struct {
			Parent   string `db:"table"`
			From     string `db:"from"`
			To       string `db:"to"`
			OnDelete string `db:"on_delete"`
		}
		if err := tx.Select(&fks, `SELECT "table", "from", "to", on_delete FROM pragma_foreign_key_list(?)`, t); err != nil {
			return nil, err
		}
		for _, fk := range fks {
			parent := strings.Trim(fk.Parent, `"`)
			to := cmp.Or(fk.To, "id")
			s.children[parent] = append(s.children[parent], fkEdge{child: t, col: fk.From, parentCol: to, onDelete: fk.OnDelete})
			s.parents[t] = append(s.parents[t], fkEdge{child: parent, col: fk.From, parentCol: to, onDelete: fk.OnDelete})
		}
	}
	var visit func(t string, depth int)
	visit = func(t string, depth int) {
		if depth > len(tables) {
			return
		}
		for _, p := range s.parents[t] {
			if p.child != t {
				visit(p.child, depth+1)
			}
		}
		if _, ok := s.rank[t]; !ok {
			s.rank[t] = len(s.rank)
		}
	}
	for _, t := range tables {
		visit(t, 0)
	}
	return s, nil
}

// capture adds to snap every row of table whose col is one of vals, and,
// through ON DELETE CASCADE, every row that depends on them.
func capture(tx *sqlx.Tx, s *schema, snap *snapshot, seen map[string]bool, table, col string, vals []any) error {
	if len(vals) == 0 {
		return nil
	}
	q, args, err := sqlx.In(fmt.Sprintf(`SELECT rowid AS "__rowid", * FROM %q WHERE %q IN (?)`, table, col), vals)
	if err != nil {
		return err
	}
	rows, err := tx.Queryx(q, args...)
	if err != nil {
		return err
	}
	var found []map[string]any
	for rows.Next() {
		row := map[string]any{}
		if err := rows.MapScan(row); err != nil {
			rows.Close()
			return err
		}
		key := fmt.Sprint(table, "\x00", row["__rowid"])
		delete(row, "__rowid")
		if seen[key] {
			continue
		}
		seen[key] = true
		for k, v := range row {
			if b, ok := v.([]byte); ok {
				row[k] = string(b)
			}
		}
		found = append(found, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, row := range found {
		snap.Rows = append(snap.Rows, snapRow{Table: table, Row: row})
	}
	for _, e := range s.children[table] {
		var parentVals []any
		for _, row := range found {
			if v := row[e.parentCol]; v != nil {
				parentVals = append(parentVals, v)
			}
		}
		switch e.onDelete {
		case "CASCADE":
			if err := capture(tx, s, snap, seen, e.child, e.col, parentVals); err != nil {
				return err
			}
		case "SET NULL":
			for _, v := range parentVals {
				var ids []any
				if err := tx.Select(&ids, fmt.Sprintf(`SELECT id FROM %q WHERE %q = ?`, e.child, e.col), v); err != nil {
					return err
				}
				if len(ids) > 0 {
					snap.Relinks = append(snap.Relinks, relink{Table: e.child, Column: e.col, Value: v, IDs: ids})
				}
			}
		}
	}
	return nil
}

// captureLinks adds the links a restore must put back: every link out of a
// captured card, entry or artifact, and every link into a captured card, or
// into a captured artifact from anything but an entry. A link into an entry,
// or from an entry into an artifact, is the entry file's own and becomes a
// stub instead, resolved again when the target is restored. Captured links
// are deleted here: a card has no trigger that would.
func captureLinks(tx *sqlx.Tx, snap *snapshot) error {
	ids := map[string][]any{}
	for _, r := range snap.Rows {
		switch r.Table {
		case "card", "entry", "artifact":
			ids[r.Table] = append(ids[r.Table], r.Row["id"])
		}
	}
	var clauses []string
	var args []any
	for _, typ := range []string{"card", "entry", "artifact"} {
		if len(ids[typ]) == 0 {
			continue
		}
		q, a, err := sqlx.In(`(from_type = ? AND from_id IN (?))`, typ, ids[typ])
		if err != nil {
			return err
		}
		clauses, args = append(clauses, q), append(args, a...)
	}
	if len(ids["card"]) > 0 {
		q, a, err := sqlx.In(`(to_type = 'card' AND to_id IN (?))`, ids["card"])
		if err != nil {
			return err
		}
		clauses, args = append(clauses, q), append(args, a...)
	}
	if len(ids["artifact"]) > 0 {
		q, a, err := sqlx.In(`(to_type = 'artifact' AND from_type != 'entry' AND to_id IN (?))`, ids["artifact"])
		if err != nil {
			return err
		}
		clauses, args = append(clauses, q), append(args, a...)
	}
	if len(clauses) == 0 {
		return nil
	}
	where := strings.Join(clauses, " OR ")
	rows, err := tx.Queryx(`SELECT * FROM link WHERE `+where, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		row := map[string]any{}
		if err := rows.MapScan(row); err != nil {
			rows.Close()
			return err
		}
		for k, v := range row {
			if b, ok := v.([]byte); ok {
				row[k] = string(b)
			}
		}
		snap.Links = append(snap.Links, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM link WHERE `+where, args...)
	return err
}

// trashItem is what a delete tells trashRows about the thing it trashes.
type trashItem struct {
	id         string // the trash row's id, chosen before any file moves
	kind       string
	table      string
	itemID     string
	name       string
	title      string
	seq        *int64
	projectID  string
	projectKey string
	files      []trashFile
}

// trashRows snapshots item's rows into the trash table. The caller deletes the
// live rows afterwards, in the same transaction, exactly as before trash
// existed.
func (c *Core) trashRows(tx *sqlx.Tx, item trashItem) error {
	s, err := loadSchema(tx)
	if err != nil {
		return err
	}
	snap := &snapshot{}
	if err := capture(tx, s, snap, map[string]bool{}, item.table, "id", []any{item.itemID}); err != nil {
		return err
	}
	if err := captureLinks(tx, snap); err != nil {
		return err
	}
	if item.seq == nil {
		// A board or a project carries its cards: remember the highest number
		// among them, for nextCardSeq.
		for _, r := range snap.Rows {
			if n, ok := r.Row["seq"].(int64); ok && r.Table == "card" && (item.seq == nil || n > *item.seq) {
				item.seq = &n
			}
		}
	}
	rows, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	files, err := json.Marshal(cmpNil(item.files))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO trash (id, project_id, project_key, kind, item_id, name, title, seq,
		trashed_at, trashed_by, rows, files) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.id, item.projectID, item.projectKey, item.kind, item.itemID, item.name, item.title, item.seq,
		c.clock.NowMS(), c.actor, string(rows), string(files))
	return err
}

func cmpNil(f []trashFile) []trashFile {
	if f == nil {
		return []trashFile{}
	}
	return f
}

// trashDir is where a trashed item's files go: beside the vault they came
// from, so a vault walk never reaches them and a move never crosses a
// filesystem.
func (c *Core) trashDir(projectKey string, global bool, trashID string) string {
	return filepath.Join(filepath.Dir(c.vaultDir(projectKey, global)), ".trash", trashID)
}

// stagedMoves moves files into or out of the trash before a transaction, and
// moves them back if the transaction fails.
type stagedMoves struct {
	done []trashFile // absolute paths, in the order moved
}

func (c *Core) stageMoves(files []trashFile, reverse bool) (*stagedMoves, error) {
	st := &stagedMoves{}
	for _, f := range files {
		from, to := filepath.Join(c.root, f.From), filepath.Join(c.root, f.To)
		if reverse {
			from, to = to, from
		}
		if _, err := os.Lstat(from); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return st, withUndo(err, st.undo())
		}
		if _, err := os.Lstat(to); err == nil {
			return st, withUndo(fmt.Errorf("%s already exists", to), st.undo())
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return st, withUndo(err, st.undo())
		}
		if err := os.Rename(from, to); err != nil {
			return st, withUndo(err, st.undo())
		}
		st.done = append(st.done, trashFile{From: from, To: to})
		if err := syncDirs(from, to); err != nil {
			return st, withUndo(err, st.undo())
		}
	}
	return st, nil
}

func syncDirs(paths ...string) error {
	for _, p := range paths {
		if err := atomicfile.SyncDir(filepath.Dir(p)); err != nil {
			return err
		}
	}
	return nil
}

// withUndo returns err, joined with the undo's own failure only when there
// is one, so a caller still sees err's type.
func withUndo(err, undoErr error) error {
	if undoErr == nil {
		return err
	}
	return errors.Join(err, undoErr)
}

func (st *stagedMoves) undo() error {
	if st == nil {
		return nil
	}
	var errs []error
	for _, f := range slices.Backward(st.done) {
		if err := os.Rename(f.To, f.From); err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, syncDirs(f.From, f.To))
	}
	st.done = nil
	return errors.Join(errs...)
}

// withMoves runs fn in a transaction after moving files, and moves them back
// when fn or the commit fails. landed resolves a failed commit: it reports
// whether the write reached the database anyway.
func (c *Core) withMoves(ctx context.Context, files func(tx *sqlx.Tx) ([]trashFile, bool, error), fn func(tx *sqlx.Tx) error, landed func() (bool, error)) error {
	var st *stagedMoves
	done := false
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		fs, reverse, err := files(tx)
		if err != nil {
			return err
		}
		if st, err = c.stageMoves(fs, reverse); err != nil {
			return err
		}
		defer func() {
			if !done {
				err = withUndo(err, st.undo())
			}
		}()
		if err := fn(tx); err != nil {
			return err
		}
		done = true
		return nil
	})
	if err != nil && done {
		ok, qerr := landed()
		if writeLanded(ok, qerr) {
			return nil
		}
		return withUndo(err, st.undo())
	}
	return err
}

// Trash lists a project's trashed items of one kind, newest first. An empty
// kind lists every kind.
func (c *Core) Trash(ctx context.Context, projectID, kind string) ([]TrashItem, error) {
	var out []TrashItem
	q := `SELECT * FROM trash WHERE project_id = ?`
	args := []any{projectID}
	if kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	err := c.db.SelectContext(ctx, &out, q+` ORDER BY trashed_at DESC, id DESC`, args...)
	return out, err
}

// findTrash returns the newest trashed item of kind named name.
func findTrash(tx *sqlx.Tx, projectID, kind, name string) (TrashItem, error) {
	var it TrashItem
	q := `SELECT * FROM trash WHERE kind = ? AND name = ?`
	args := []any{kind, name}
	if projectID != "" {
		q += ` AND project_id = ?`
		args = append(args, projectID)
	}
	err := tx.Get(&it, q+` ORDER BY trashed_at DESC, id DESC LIMIT 1`, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound(kind+"_not_trashed", fmt.Sprintf("no trashed %s %s", kind, name),
			fmt.Sprintf("trellis %s ls --trashed", cliNoun(kind)))
	}
	return it, err
}

func cliNoun(kind string) string {
	if kind == TrashEntry {
		return "vault"
	}
	return kind
}

// decodeSnapshot reads a trash row's rows, keeping integers integers.
func decodeSnapshot(raw string) (snapshot, error) {
	var snap snapshot
	d := json.NewDecoder(bytes.NewReader([]byte(raw)))
	d.UseNumber()
	if err := d.Decode(&snap); err != nil {
		return snap, err
	}
	for _, r := range snap.Rows {
		fixNumbers(r.Row)
	}
	for _, l := range snap.Links {
		fixNumbers(l)
	}
	for i := range snap.Relinks {
		snap.Relinks[i].Value = fixNumber(snap.Relinks[i].Value)
		for j := range snap.Relinks[i].IDs {
			snap.Relinks[i].IDs[j] = fixNumber(snap.Relinks[i].IDs[j])
		}
	}
	return snap, nil
}

func fixNumbers(row map[string]any) {
	for k, v := range row {
		row[k] = fixNumber(v)
	}
}

func fixNumber(v any) any {
	n, ok := v.(json.Number)
	if !ok {
		return v
	}
	if i, err := n.Int64(); err == nil {
		return i
	}
	f, _ := n.Float64()
	return f
}

// rootRow returns the snapshot's own row.
func (it TrashItem) rootRow(snap snapshot) map[string]any {
	for _, r := range snap.Rows {
		if r.Row["id"] == it.ItemID {
			return r.Row
		}
	}
	return nil
}

// scanRow fills dest, a struct with db tags, from a snapshot row, through
// the same mapping every live read uses. q must be unsafe (tx.Unsafe()): a
// snapshot row may hold columns dest has no field for.
func scanRow(q sqlx.Queryer, dest any, row map[string]any) error {
	cols := slices.Sorted(func(yield func(string) bool) {
		for k := range row {
			if !yield(k) {
				return
			}
		}
	})
	parts := make([]string, len(cols))
	args := make([]any, len(cols))
	for i, col := range cols {
		parts[i] = fmt.Sprintf(`? AS %q`, col)
		args[i] = row[col]
	}
	return sqlx.Get(q, dest, `SELECT `+strings.Join(parts, ", "), args...)
}

// errRestoreParent reports that a restore cannot proceed because what the
// item lived in is gone: every missing parent, as table and id.
type errRestoreParent struct{ missing [][2]string }

func (e errRestoreParent) Error() string { return fmt.Sprint("missing ", e.missing) }

// insertSnapshot puts snap's rows back, parents first. A dependent row whose
// parent no longer exists -- a label deleted since -- is skipped, and so is
// everything under it. The item's own row missing a parent fails with
// errRestoreParent, except a card whose column is gone, which lands in its
// board's first column.
func insertSnapshot(tx *sqlx.Tx, s *schema, snap snapshot, rootTable string, rootID any) error {
	rows := slices.Clone(snap.Rows)
	slices.SortStableFunc(rows, func(a, b snapRow) int { return s.rank[a.Table] - s.rank[b.Table] })
	for _, r := range rows {
		isRoot := r.Table == rootTable && r.Row["id"] == rootID
		skip := false
		var missing [][2]string
		for _, p := range s.parents[r.Table] {
			v := r.Row[p.col]
			if v == nil {
				continue
			}
			var n int
			if err := tx.Get(&n, fmt.Sprintf(`SELECT count(*) FROM %q WHERE %q = ?`, p.child, p.parentCol), v); err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			if isRoot && r.Table == "card" && p.child == "column_" {
				var first string
				if err := tx.Get(&first, `SELECT id FROM column_ WHERE board_id = ? ORDER BY position LIMIT 1`, r.Row["board_id"]); err == nil {
					r.Row[p.col] = first
					continue
				}
			}
			if isRoot {
				missing = append(missing, [2]string{p.child, fmt.Sprint(v)})
				continue
			}
			skip = true
			break
		}
		if len(missing) > 0 {
			return errRestoreParent{missing: missing}
		}
		if skip {
			continue
		}
		cols := slices.Sorted(func(yield func(string) bool) {
			for k := range r.Row {
				if !yield(k) {
					return
				}
			}
		})
		live, err := tableColumns(tx, r.Table)
		if err != nil {
			return err
		}
		cols = slices.DeleteFunc(cols, func(c string) bool { return !live[c] })
		names := make([]string, len(cols))
		marks := make([]string, len(cols))
		args := make([]any, len(cols))
		for i, col := range cols {
			names[i], marks[i], args[i] = fmt.Sprintf("%q", col), "?", r.Row[col]
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %q (%s) VALUES (%s)`, r.Table,
			strings.Join(names, ", "), strings.Join(marks, ", ")), args...); err != nil {
			return fmt.Errorf("restore %s: %w", r.Table, err)
		}
	}
	for _, rl := range snap.Relinks {
		q, args, err := sqlx.In(fmt.Sprintf(`UPDATE %q SET %q = ? WHERE %q IS NULL AND id IN (?)`,
			rl.Table, rl.Column, rl.Column), rl.Value, rl.IDs)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(q, args...); err != nil {
			return err
		}
	}
	for _, l := range snap.Links {
		if !linkEndExists(tx, l["from_type"], l["from_id"]) || (l["to_id"] != nil && !linkEndExists(tx, l["to_type"], l["to_id"])) {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, l["from_type"], l["from_id"], l["to_type"], l["to_id"], l["to_raw"], l["anchor"], l["rel"]); err != nil {
			return err
		}
	}
	return nil
}

func tableColumns(tx *sqlx.Tx, table string) (map[string]bool, error) {
	var cols []string
	if err := tx.Select(&cols, `SELECT name FROM pragma_table_info(?)`, table); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(cols))
	for _, c := range cols {
		out[c] = true
	}
	return out, nil
}

func linkEndExists(tx *sqlx.Tx, typ, id any) bool {
	table, ok := map[any]string{"card": "card", "entry": "entry", "artifact": "artifact"}[typ]
	if !ok {
		return true
	}
	var n int
	if err := tx.Get(&n, fmt.Sprintf(`SELECT count(*) FROM %q WHERE id = ?`, table), id); err != nil {
		return false
	}
	return n > 0
}

// kindTable is the table holding a kind's own row.
var kindTable = map[string]string{
	TrashProject: "project", TrashBoard: "board", TrashCard: "card", TrashEntry: "entry", TrashArtifact: "artifact",
}

// restoreTrash puts a trashed item back: its files first, then its rows,
// then the stubs that pointed at it. check refuses a restore that would
// collide with something live, inside the same transaction.
func (c *Core) restoreTrash(ctx context.Context, trashID string, check func(tx *sqlx.Tx, it TrashItem) error) (TrashItem, error) {
	var it TrashItem
	err := c.withMoves(ctx, func(tx *sqlx.Tx) ([]trashFile, bool, error) {
		if err := tx.Get(&it, `SELECT * FROM trash WHERE id = ?`, trashID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, false, ErrNotFound("not_trashed", "nothing in the trash with id "+trashID, "")
			}
			return nil, false, err
		}
		if err := check(tx, it); err != nil {
			return nil, false, err
		}
		var files []trashFile
		if err := json.Unmarshal([]byte(it.Files), &files); err != nil {
			return nil, false, err
		}
		for _, f := range files {
			if _, err := os.Lstat(filepath.Join(c.root, f.To)); errors.Is(err, os.ErrNotExist) {
				return nil, false, ErrConflict("trash_files_gone",
					fmt.Sprintf("%s %s is being purged: its files are already gone", it.Kind, it.Name), "")
			}
		}
		return files, true, nil
	}, func(tx *sqlx.Tx) error {
		snap, err := decodeSnapshot(it.Rows)
		if err != nil {
			return err
		}
		s, err := loadSchema(tx)
		if err != nil {
			return err
		}
		if err := insertSnapshot(tx, s, snap, kindTable[it.Kind], it.ItemID); err != nil {
			if pe, ok := errors.AsType[errRestoreParent](err); ok {
				return c.parentConflict(tx, it, pe)
			}
			return err
		}
		entries := false
		for _, r := range snap.Rows {
			switch r.Table {
			case "entry":
				entries = true
				if err := c.resolveEntryStubs(tx, &Entry{ID: fmt.Sprint(r.Row["id"])}); err != nil {
					return err
				}
			case "artifact":
				if err := backfillArtifactStubs(tx, fmt.Sprint(r.Row["project_id"]),
					fmt.Sprint(r.Row["name"]), fmt.Sprint(r.Row["id"])); err != nil {
					return err
				}
			}
		}
		if it.Kind == TrashBoard {
			// Deleting the default promoted another board; the restored one
			// comes back as an ordinary board.
			if _, err := tx.Exec(`UPDATE board SET is_default = 0 WHERE id = ? AND
				(SELECT count(*) FROM board WHERE project_id = ? AND is_default = 1) > 1`, it.ItemID, it.ProjectID); err != nil {
				return err
			}
		}
		if entries {
			if err := c.rebuildEntryFTS(tx); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM trash WHERE id = ?`, it.ID); err != nil {
			return err
		}
		return c.recordEvent(tx, it.Kind, it.ItemID, "restored", "", "", it.Name)
	}, func() (bool, error) {
		var n int
		err := c.db.Get(&n, `SELECT count(*) FROM trash WHERE id = ?`, trashID)
		return n == 0, err
	})
	if err == nil && it.Kind != TrashCard && it.Kind != TrashBoard {
		c.notifyEntryChanged(ctx, it.ProjectID)
	}
	return it, err
}

// parentConflict explains a restore whose container is gone: restore the
// container first when it is in the trash too.
func (c *Core) parentConflict(tx *sqlx.Tx, it TrashItem, pe errRestoreParent) error {
	for _, m := range pe.missing {
		var parent TrashItem
		err := tx.Get(&parent, `SELECT * FROM trash WHERE item_id = ? ORDER BY trashed_at DESC LIMIT 1`, m[1])
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		return ErrConflict(parent.Kind+"_trashed",
			fmt.Sprintf("%s %s was on %s %s, which is in the trash", it.Kind, it.Name, parent.Kind, parent.Name),
			fmt.Sprintf("trellis %s restore %s", cliNoun(parent.Kind), parent.Name))
	}
	return ErrConflict("restore_parent_gone",
		fmt.Sprintf("%s %s cannot come back: its %s no longer exists", it.Kind, it.Name,
			strings.TrimSuffix(pe.missing[0][0], "_")), "")
}

// purgeDir is the directory a trashed item's files live under.
func purgeDir(to, trashID string) string {
	marker := string(filepath.Separator) + trashID
	if i := strings.Index(to, marker); i >= 0 {
		return to[:i+len(marker)]
	}
	return to
}

// PurgeTrash removes for good every item trashed before cutoff. Files go
// first and the row last, so a purge cut short leaves rows the next one
// finishes, never files nothing points at.
func (c *Core) PurgeTrash(ctx context.Context, cutoff int64) (int, error) {
	var items []TrashItem
	if err := c.db.SelectContext(ctx, &items, `SELECT * FROM trash WHERE trashed_at < ? ORDER BY trashed_at`, cutoff); err != nil {
		return 0, err
	}
	purged := 0
	for _, it := range items {
		var files []trashFile
		if err := json.Unmarshal([]byte(it.Files), &files); err != nil {
			return purged, err
		}
		for _, f := range files {
			dir := filepath.Join(c.root, purgeDir(f.To, it.ID))
			if err := os.RemoveAll(dir); err != nil {
				return purged, err
			}
			if err := atomicfile.SyncDir(filepath.Dir(dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return purged, err
			}
		}
		if it.Kind == TrashProject {
			var live int
			if err := c.db.GetContext(ctx, &live, `SELECT count(*) FROM project WHERE key = ?`, it.Name); err != nil {
				return purged, err
			}
			if live == 0 {
				if err := os.RemoveAll(filepath.Join(c.root, "projects", it.Name)); err != nil {
					return purged, err
				}
			}
		}
		err := c.Tx(ctx, func(tx *sqlx.Tx) error {
			res, err := tx.Exec(`DELETE FROM trash WHERE id = ?`, it.ID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil
			}
			return c.recordEvent(tx, it.Kind, it.ItemID, "purged", "", it.Name, "")
		})
		if err != nil {
			return purged, err
		}
		purged++
	}
	return purged, nil
}

// sweepEvery is how often trash is purged automatically.
const sweepEvery = int64(60 * 60 * 1000)

// SweepTrash purges what is past retention, at most once per hour across
// every process: the conditional UPDATE lets exactly one of several racing
// starts through.
func (c *Core) SweepTrash(ctx context.Context, retention time.Duration) (bool, error) {
	now := c.clock.NowMS()
	var last int64
	if err := c.db.GetContext(ctx, &last, `SELECT last_at FROM trash_sweep WHERE id = 1`); err != nil {
		return false, err
	}
	if now-last < sweepEvery {
		return false, nil
	}
	res, err := c.db.ExecContext(ctx, `UPDATE trash_sweep SET last_at = ? WHERE id = 1 AND last_at = ?`, now, last)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	_, err = c.PurgeTrash(ctx, now-retention.Milliseconds())
	return true, err
}
