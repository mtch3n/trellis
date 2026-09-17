package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/resolve"
)

// MergeOptions tunes `trellis project merge`.
type MergeOptions struct {
	// Apply performs the merge; without it the merge only reports its plan.
	Apply bool
	// RenameConflicts renames SRC's side of a name collision instead of
	// stopping. Vault entries are never renamed.
	RenameConflicts bool
	// ScanRoot, Markers and UnreadableMarkers describe the markers the caller
	// found. Core never walks the filesystem for them.
	ScanRoot          string
	Markers           []resolve.Marker
	UnreadableMarkers []string
}

// MergePlan is what a merge would do, or did.
type MergePlan struct {
	Src              string         `json:"src"`
	Dst              string         `json:"dst"`
	Ready            bool           `json:"ready"`
	Refused          string         `json:"refused"`
	Boards           []BoardMove    `json:"boards"`
	Cards            CardMoves      `json:"cards"`
	Entries          ItemMoves      `json:"knowledge"`
	Artifacts        ItemMoves      `json:"artifacts"`
	Labels           NameMoves      `json:"labels"`
	Tags             NameMoves      `json:"tags"`
	ConfigDropped    []ConfigDrop   `json:"config_dropped"`
	EntriesRewritten []string       `json:"documents_rewritten"`
	Markers          MarkerRewrites `json:"pins"`
	Backup           string         `json:"backup,omitempty"`
	Warnings         []string       `json:"warnings,omitempty"`

	dstID string
	files []string // every file the merge moves or rewrites, as it was before
}

type BoardMove struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	NewName string `json:"new_name"`
	NewSlug string `json:"new_slug"`
}

type CardMoves struct {
	Moved    int   `json:"moved"`
	FirstSeq int64 `json:"first_seq"`
}

type ItemMoves struct {
	Moved     int             `json:"moved"`
	Collapsed []string        `json:"collapsed"`
	Renamed   []Rename        `json:"renamed"`
	Conflicts []MergeConflict `json:"conflicts"`
}

type Rename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type MergeConflict struct {
	Name    string `json:"name"`
	SrcHash string `json:"src_hash,omitempty"`
	DstHash string `json:"dst_hash,omitempty"`
	Vault   bool   `json:"vault"`            // SRC's side is a vault entry, which is never renamed
	Reason  string `json:"reason,omitempty"` // set when the conflict is not a name clash
}

type NameMoves struct {
	Moved  []string `json:"moved"`
	Folded []string `json:"folded"`
}

type ConfigDrop struct {
	Key string `db:"key" json:"key"`
	Src string `db:"src" json:"src"`
	Dst string `db:"dst" json:"dst"`
}

type MarkerRewrites struct {
	ScanRoot string          `json:"scan_root"`
	Rewrite  []MarkerRewrite `json:"rewrite"`
	Left     []string        `json:"left"`
}

type MarkerRewrite struct {
	Path string `json:"path"`
	From string `json:"from"`
	To   string `json:"to"`
}

// errPlanOnly rolls back a plan-mode transaction; it never reaches a caller.
var errPlanOnly = errors.New("merge plan: rolled back by design")

// MergeProjects moves everything SRC owns into DST, and retires SRC's key.
//
// The plan and the apply are one code path: a plan runs the merge in a
// transaction that is always rolled back, with file operations skipped, so a
// plan is never a separate estimate. Apply re-runs it for real after a backup.
func (c *Core) MergeProjects(ctx context.Context, srcKey, dstKey string, opts MergeOptions) (MergePlan, error) {
	srcKey, dstKey = normalizeKey(srcKey), normalizeKey(dstKey)
	plan, err := c.runMerge(ctx, srcKey, dstKey, opts, nil)
	if err != nil || !opts.Apply {
		return plan, err
	}
	if !plan.Ready {
		return plan, mergeNotReady(plan)
	}
	backup, backedUp, err := c.backupForMerge(ctx, plan)
	if err != nil {
		return plan, fmt.Errorf("backing up before the merge: %w", err)
	}
	var warnings []string
	if err := c.dropDerived(ctx, srcKey); err != nil {
		warnings = append(warnings, "dropping "+srcKey+"'s vector tables: "+err.Error())
	}
	applied, err := c.runMerge(ctx, srcKey, dstKey, opts, backedUp)
	applied.Backup = backup
	applied.Warnings = append(warnings, applied.Warnings...)
	if err != nil {
		return applied, err
	}
	c.afterMerge(ctx, &applied)
	return applied, nil
}

// runMerge runs the merge in one transaction. backedUp is nil for a plan;
// for an apply it holds the hash of every file the backup copied, and the
// merge touches no file outside it.
func (c *Core) runMerge(ctx context.Context, srcKey, dstKey string, opts MergeOptions, backedUp map[string]string) (MergePlan, error) {
	plan := MergePlan{Src: srcKey, Dst: dstKey}
	stage := &fileStage{}
	apply := backedUp != nil
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		m := &merger{
			c: c, tx: tx, plan: &plan, opts: opts, apply: apply, stage: stage, backedUp: backedUp,
			entryPath: map[string]string{}, fromSrc: map[string]bool{}, addr: map[string]string{},
			renamed: map[string]string{}, boardSlug: map[string]string{}, origPath: map[string]string{},
			artRenamed: map[string]string{},
		}
		if err := m.run(srcKey, dstKey); err != nil {
			return err
		}
		if !apply {
			return errPlanOnly
		}
		return nil
	})
	switch {
	case err == nil:
		if apply {
			if ferr := stage.finalize(); ferr != nil {
				return plan, fmt.Errorf("removing the files a moved entry left behind: %w", ferr)
			}
		}
		return plan, nil
	case errors.Is(err, errPlanOnly):
		return plan, nil // plan mode stages no file operations
	}
	if rbErr := stage.rollback(); rbErr != nil {
		err = errors.Join(err, fmt.Errorf("restoring files after the failed merge: %w", rbErr))
	}
	return plan, err
}

// mergeNotReady is the error an apply gets when its plan is not ready.
func mergeNotReady(p MergePlan) error {
	if p.Refused != "" {
		return ErrConflict("merge_refused", p.Refused, "")
	}
	var names []string
	vault := false
	for _, group := range [][]MergeConflict{p.Entries.Conflicts, p.Artifacts.Conflicts} {
		for _, conflict := range group {
			names = append(names, conflict.Name)
			vault = vault || conflict.Vault
		}
	}
	fix := "trellis project merge " + p.Src + " --into " + p.Dst + " --rename-conflicts --apply"
	if vault {
		fix = "trellis vault demote <slug>   # vault entries are never renamed: demote or edit one side"
	}
	return ErrConflict("merge_conflicts",
		fmt.Sprintf("%s and %s both hold %s: %s", p.Src, p.Dst,
			plural(len(names), "an item with this name", "items with these names"), strings.Join(names, ", ")),
		fix)
}

// backupForMerge writes the database and a copy of every file the merge will
// touch, and returns the hash of each copy. VACUUM cannot run inside a
// transaction, so this precedes the merge; the apply then refuses to touch a
// file the backup does not hold as it is, so a change made in between stops
// the merge instead of escaping the backup.
func (c *Core) backupForMerge(ctx context.Context, plan MergePlan) (string, map[string]string, error) {
	stamp := time.UnixMilli(c.clock.NowMS()).UTC().Format("20060102T150405Z")
	dir := filepath.Join(c.root, "backups", fmt.Sprintf("merge-%s-into-%s-%s", plan.Src, plan.Dst, stamp))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	if err := c.Backup(ctx, filepath.Join(dir, "trellis.db")); err != nil {
		return "", nil, err
	}
	hashes, err := copyUnder(c.root, filepath.Join(dir, "files"), plan.files)
	return dir, hashes, err
}

// touch records a file the merge moves or rewrites. A plan collects them for
// the backup. An apply accepts only a file the backup copied and that has not
// changed since.
func (m *merger) touch(path string) error {
	if slices.Contains(m.plan.files, path) {
		return nil
	}
	m.plan.files = append(m.plan.files, path)
	if !m.apply {
		return nil
	}
	want, ok := m.backedUp[path]
	if !ok {
		return errMergeChanged(path, "appeared after the backup")
	}
	got, err := fileHash(path)
	if err != nil {
		return err
	}
	if got != want {
		return errMergeChanged(path, "changed after the backup")
	}
	return nil
}

func errMergeChanged(path, what string) error {
	return ErrConflict("merge_changed",
		fmt.Sprintf("%s %s; the merge changed nothing", path, what),
		"trellis project merge <SRC> --into <DST> --apply   # run it again")
}

// afterMerge runs what no transaction reaches. Each step is best effort and
// reports into Warnings: the merge has already committed.
//
// SRC's directory now holds only what the merge left behind -- collapsed
// files and derived vector files -- so it is kept with the backup rather than
// deleted. Markers live outside the storage root and are rewritten last.
func (c *Core) afterMerge(ctx context.Context, plan *MergePlan) {
	warn := func(format string, args ...any) {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(format, args...))
	}
	if srcDir := filepath.Join(c.root, "projects", plan.Src); dirExists(srcDir) {
		leftover := filepath.Join(plan.Backup, "leftover")
		if err := os.MkdirAll(leftover, 0o700); err != nil {
			warn("keeping %s with the backup: %v", srcDir, err)
		} else if err := os.Rename(srcDir, filepath.Join(leftover, plan.Src)); err != nil {
			warn("keeping %s with the backup: %v", srcDir, err)
		}
	}
	for _, r := range plan.Markers.Rewrite {
		if err := rewriteMarker(r); err != nil {
			warn("rewriting %s: %v; it still names %s", r.Path, err, r.From)
		}
	}
	if c.entryChanged != nil {
		if err := c.entryChanged(ctx, plan.dstID); err != nil {
			warn("refreshing %s's derived search state: %v", plan.Dst, err)
		}
	}
}

// rewriteMarker points a marker at the survivor, unless it no longer names
// what the plan found: a marker someone changed since is theirs.
func rewriteMarker(r MarkerRewrite) error {
	raw, err := os.ReadFile(r.Path)
	if err != nil {
		return err
	}
	current, err := address.ParseMarker(string(raw))
	if err != nil {
		return err
	}
	if current.String() != r.From {
		return fmt.Errorf("it now names %s", current)
	}
	return replaceIfUnchanged(r.Path, []byte(r.To+"\n"), ContentHash(string(raw)))
}

func dirExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

// merger is one run of a merge inside its transaction.
type merger struct {
	c        *Core
	tx       *sqlx.Tx
	plan     *MergePlan
	opts     MergeOptions
	apply    bool
	backedUp map[string]string // path -> hash of its backup copy; nil for a plan
	stage    *fileStage

	src, dst Project

	boardSlug      map[string]string // SRC board slug -> its slug in DST
	srcDefaultSlug string            // DST slug of SRC's default board, or ""
	entryPath      map[string]string // entry id -> where its file is now
	fromSrc        map[string]bool   // entries that came from SRC and still exist
	addr           map[string]string // SRC entry address -> its address now
	renamed        map[string]string // SRC slug -> its slug in DST, for renamed entries
	origPath       map[string]string // SRC entry id -> its file path before the merge
	artRenamed     map[string]string // SRC artifact name -> its name in DST, for renamed artifacts
	entryMoves     []entryMove
	artMoves       []artifactMove
}

func (m *merger) run(srcKey, dstKey string) error {
	refused, err := m.load(srcKey, dstKey)
	if err != nil {
		return err
	}
	if refused {
		if m.apply {
			return mergeNotReady(*m.plan)
		}
		return nil
	}
	// Every conflict is known before anything changes.
	for _, detect := range []func() error{m.planEntries, m.planArtifacts} {
		if err := detect(); err != nil {
			return err
		}
	}
	m.plan.Ready = len(m.plan.Entries.Conflicts) == 0 && len(m.plan.Artifacts.Conflicts) == 0
	if m.apply && !m.plan.Ready {
		return mergeNotReady(*m.plan)
	}
	for _, step := range []func() error{
		m.boards, m.labels, m.tags, m.cards, m.moveEntries, m.moveArtifacts,
		m.references, m.config, m.markers, m.retire,
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// load reads both projects and decides whether the merge may run at all.
func (m *merger) load(srcKey, dstKey string) (refused bool, err error) {
	if srcKey == dstKey {
		m.plan.Refused = "a project cannot be merged into itself"
		return true, nil
	}
	if m.src, err = m.project(srcKey); err != nil {
		return false, err
	}
	if m.dst, err = m.project(dstKey); err != nil {
		return false, err
	}
	m.plan.dstID = m.dst.ID

	claimed, err := m.count(`SELECT COUNT(*) FROM card WHERE project_id = ? AND claimed_by IS NOT NULL AND claim_until > ?`,
		m.src.ID, m.c.clock.NowMS())
	if err != nil {
		return false, err
	}
	switch {
	case !address.ValidKey(m.dst.Key):
		m.plan.Refused = fmt.Sprintf("%s cannot be named by a marker; merge into a project whose key can", m.dst.Key)
	case claimed > 0:
		m.plan.Refused = fmt.Sprintf("%s has %d %s claimed by an agent right now", m.src.Key, claimed, plural(claimed, "card", "cards"))
	}
	return m.plan.Refused != "", nil
}

func (m *merger) project(key string) (Project, error) {
	var p Project
	err := m.tx.Get(&p, `SELECT * FROM project WHERE key = ?`, key)
	if !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	into, err := mergedTarget(m.tx, key)
	if err != nil {
		return p, err
	}
	if into != "" {
		return p, errProjectMerged(key, into)
	}
	return p, ErrNotFound("project_not_found", "no project "+key, "trellis project ls")
}

func (m *merger) count(q string, args ...any) (int, error) {
	var n int
	err := m.tx.Get(&n, q, args...)
	return n, err
}

// boards moves SRC's boards into DST as separate boards. A clashing slug gets
// SRC's key as a prefix and a clashing name gets it as a suffix, so the moved
// board is recognizable; DST's default stays the default.
func (m *merger) boards() error {
	var dst, src []Board
	if err := m.tx.Select(&dst, `SELECT * FROM board WHERE project_id = ?`, m.dst.ID); err != nil {
		return err
	}
	if err := m.tx.Select(&src, `SELECT * FROM board WHERE project_id = ? ORDER BY created_at`, m.src.ID); err != nil {
		return err
	}
	slugs, names := map[string]bool{}, map[string]bool{}
	for _, b := range dst {
		slugs[b.Slug], names[b.Name] = true, true
	}
	prefix := slugify(m.src.Key)
	for _, b := range src {
		slug, name := b.Slug, b.Name
		if slugs[slug] {
			slug = freeName(prefix+"-"+b.Slug, slugs)
		}
		if names[name] {
			name = freeName(fmt.Sprintf("%s (%s)", b.Name, m.src.Key), names)
		}
		slugs[slug], names[name] = true, true
		m.boardSlug[b.Slug] = slug
		if b.IsDefault {
			m.srcDefaultSlug = slug
		}
		m.plan.Boards = append(m.plan.Boards, BoardMove{Name: b.Name, Slug: b.Slug, NewName: name, NewSlug: slug})
		if _, err := m.tx.Exec(`UPDATE board SET project_id = ?, name = ?, slug = ?, is_default = 0 WHERE id = ?`,
			m.dst.ID, name, slug, b.ID); err != nil {
			return err
		}
	}
	return nil
}

// freeName is base, or base-2, base-3, ... whichever is not taken.
func freeName(base string, taken map[string]bool) string {
	name := base
	for n := 2; taken[name]; n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	return name
}

func (m *merger) labels() error {
	return m.foldNames("label", "label_id", &m.plan.Labels,
		[][2]string{{"card_label", "card_id"}, {"entry_label", "entry_id"}})
}

func (m *merger) tags() error {
	return m.foldNames("tag", "tag_id", &m.plan.Tags,
		[][2]string{{"card_tag", "card_id"}, {"entry_tag", "entry_id"}})
}

// foldNames moves SRC's rows of a per-project vocabulary into DST. A name DST
// already has is folded into DST's row: every junction row is re-pointed, and
// SRC's row is deleted, which cascades its old junction rows away.
func (m *merger) foldNames(table, column string, out *NameMoves, junctions [][2]string) error {
	var rows []struct {
		ID   string  `db:"id"`
		Name string  `db:"name"`
		Into *string `db:"into_id"`
	}
	if err := m.tx.Select(&rows,
		`SELECT s.id, s.name, d.id AS into_id FROM `+table+` s
		 LEFT JOIN `+table+` d ON d.project_id = ? AND d.name = s.name
		 WHERE s.project_id = ? ORDER BY s.name`, m.dst.ID, m.src.ID); err != nil {
		return err
	}
	for _, r := range rows {
		if r.Into == nil {
			if _, err := m.tx.Exec(`UPDATE `+table+` SET project_id = ? WHERE id = ?`, m.dst.ID, r.ID); err != nil {
				return err
			}
			out.Moved = append(out.Moved, r.Name)
			continue
		}
		for _, j := range junctions {
			if _, err := m.tx.Exec(
				`INSERT OR IGNORE INTO `+j[0]+` (`+j[1]+`, `+column+`)
				 SELECT `+j[1]+`, ? FROM `+j[0]+` WHERE `+column+` = ?`, *r.Into, r.ID); err != nil {
				return err
			}
		}
		if _, err := m.tx.Exec(`DELETE FROM `+table+` WHERE id = ?`, r.ID); err != nil {
			return err
		}
		out.Folded = append(out.Folded, r.Name)
	}
	return nil
}

// cards moves SRC's cards after DST's highest seq. Their refs are stored and
// do not change; seq is only DST's allocator.
func (m *merger) cards() error {
	if err := m.tx.Get(&m.plan.Cards.FirstSeq,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM card WHERE project_id = ?`, m.dst.ID); err != nil {
		return err
	}
	var ids []string
	if err := m.tx.Select(&ids, `SELECT id FROM card WHERE project_id = ? ORDER BY seq`, m.src.ID); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := m.tx.Exec(`UPDATE card SET project_id = ?, seq = ? WHERE id = ?`,
			m.dst.ID, m.plan.Cards.FirstSeq+int64(i), id); err != nil {
			return err
		}
	}
	m.plan.Cards.Moved = len(ids)
	return nil
}

// config lists SRC's overrides that DST does not share. DST's stay; SRC's go
// with SRC.
func (m *merger) config() error {
	return m.tx.Select(&m.plan.ConfigDropped,
		`SELECT s.key, s.value AS src, COALESCE(d.value, '') AS dst
		 FROM project_config s LEFT JOIN project_config d ON d.project_id = ? AND d.key = s.key
		 WHERE s.project_id = ? AND (d.value IS NULL OR d.value <> s.value)
		 ORDER BY s.key`, m.dst.ID, m.src.ID)
}

// markers plans the rewrite of every marker naming SRC, so that a directory
// keeps opening the board it opened before.
func (m *merger) markers() error {
	m.plan.Markers.ScanRoot = m.opts.ScanRoot
	for _, p := range m.opts.Markers {
		if p.Target.Project != m.src.Key {
			continue
		}
		slug := m.srcDefaultSlug
		if b := p.Target.Board(); b != "" {
			slug = m.boardSlug[b]
		}
		if slug == "" {
			m.plan.Markers.Left = append(m.plan.Markers.Left, p.Path)
			continue
		}
		m.plan.Markers.Rewrite = append(m.plan.Markers.Rewrite, MarkerRewrite{
			Path: p.Path, From: p.Target.String(), To: address.Board(m.dst.Key, slug).String(),
		})
	}
	m.plan.Markers.Left = append(m.plan.Markers.Left, m.opts.UnreadableMarkers...)
	return nil
}

// retire removes SRC and reserves its key, re-pointing the reservations of
// earlier merges into SRC so every chain ends at the survivor.
func (m *merger) retire() error {
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE merged_project SET into_id = ? WHERE into_id = ?`, []any{m.dst.ID, m.src.ID}},
		// What SRC owned is DST's now, and so is its history.
		{`UPDATE event SET project_id = ? WHERE project_id = ?`, []any{m.dst.ID, m.src.ID}},
		{`INSERT INTO merged_project (key, into_id, merged_at) VALUES (?, ?, ?)`,
			[]any{m.src.Key, m.dst.ID, m.c.clock.NowMS()}},
		{`DELETE FROM project WHERE id = ?`, []any{m.src.ID}},
	} {
		if _, err := m.tx.Exec(q.sql, q.args...); err != nil {
			return err
		}
	}
	if err := m.c.rebuildEntryFTS(m.tx); err != nil {
		return err
	}
	if err := m.c.recordEvent(m.tx, "project", m.dst.ID, "merged", "project", m.src.Key, m.dst.Key); err != nil {
		return err
	}
	return m.c.recordEvent(m.tx, "project", m.src.ID, "merged_into", "project", m.src.Key, m.dst.Key)
}
