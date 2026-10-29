package importer

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

type localProject struct {
	ID   string `db:"id"`
	Key  string `db:"key"`
	Name string `db:"name"`
}

type localBoard struct {
	ID        string `db:"id"`
	Name      string `db:"name"`
	CreatedAt int64  `db:"created_at"`
}

type localColumn struct {
	ID       string `db:"id"`
	BoardID  string `db:"board_id"`
	Name     string `db:"name"`
	Position int    `db:"position"`
	Done     bool   `db:"is_done"`
}

type localCard struct {
	ID         string `db:"id"`
	BoardID    string `db:"board_id"`
	ColumnID   string `db:"column_id"`
	Seq        int64  `db:"seq"`
	Ref        string `db:"ref"`
	Title      string `db:"title"`
	Body       string `db:"body_md"`
	Version    int64  `db:"version"`
	CreatedAt  int64  `db:"created_at"`
	UpdatedAt  int64  `db:"updated_at"`
	ArchivedAt *int64 `db:"archived_at"`
	Position   float64
}

type localComment struct {
	CardID    string `db:"card_id"`
	Body      string `db:"body_md"`
	CreatedAt int64  `db:"created_at"`
}

type localEntry struct {
	ID         string  `db:"id"`
	Slug       string  `db:"slug"`
	Title      string  `db:"title"`
	Template   string  `db:"template"`
	Summary    string  `db:"summary"`
	Recap      *string `db:"recap"`
	Global     bool    `db:"global"`
	Private    bool    `db:"private"`
	VerifyBy   *int64  `db:"verify_by"`
	VerifiedAt *int64  `db:"verified_at"`
	Version    int64   `db:"version"`
	Provenance string  `db:"provenance"`
	CreatedAt  int64   `db:"created_at"`
	UpdatedAt  int64   `db:"updated_at"`
	Body       string
	Sources    []string
	Fields     map[string]any
	Revisions  []revision
}

type revision struct {
	Version int64
	Title   string
	Summary string
	Body    string
	At      time.Time
}

type localLink struct {
	FromType string  `db:"from_type"`
	FromID   string  `db:"from_id"`
	ToType   string  `db:"to_type"`
	ToID     *string `db:"to_id"`
	ToRaw    string  `db:"to_raw"`
	Rel      string  `db:"rel"`
}

type localPin struct {
	EntryID   string  `db:"entry_id"`
	BoardID   *string `db:"board_id"`
	CreatedAt int64   `db:"created_at"`
}

type localNomination struct {
	EntryID   string `db:"entry_id"`
	Reason    string `db:"reason"`
	CreatedAt int64  `db:"created_at"`
}

type localArtifact struct {
	ID          string `db:"id"`
	Name        string `db:"name"`
	Kind        string `db:"kind"`
	Mime        string `db:"mime"`
	ContentHash string `db:"content_hash"`
	CreatedAt   int64  `db:"created_at"`
	UpdatedAt   int64  `db:"updated_at"`
	Content     []byte
}

type snapshot struct {
	Project     localProject
	Boards      []localBoard
	Columns     []localColumn
	Cards       []localCard
	Comments    []localComment
	Entries     []localEntry
	Links       []localLink
	Pins        []localPin
	Nominations []localNomination
	Artifacts   []localArtifact
}

type frontmatter struct {
	Title   string         `yaml:"title"`
	Summary string         `yaml:"summary"`
	Sources []string       `yaml:"sources"`
	Extra   map[string]any `yaml:",inline"`
}

var frontmatterKeys = []string{"template", "status", "provenance", "private", "board", "tags", "labels", "artifacts", "created", "updated"}

type home struct {
	dir string
	db  *sqlx.DB
}

func openHome(dir string) (*home, error) {
	path := filepath.Join(dir, "trellis.db")
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sqlx.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &home{dir: dir, db: db}, nil
}

func (h *home) close() { h.db.Close() }

func (h *home) projects() ([]localProject, error) {
	var out []localProject
	err := h.db.Select(&out, `SELECT id, key, name FROM project ORDER BY key`)
	return out, err
}

func (h *home) leftOut() (map[string]int64, error) {
	counts := map[string]int64{}
	for kind, q := range map[string]string{
		"events":         `SELECT count(*) FROM event`,
		"labels":         `SELECT count(*) FROM label`,
		"tags":           `SELECT count(*) FROM tag`,
		"trash":          `SELECT count(*) FROM trash`,
		"card relations": `SELECT count(*) FROM link WHERE rel = 'relates_to'`,
		"priority":       `SELECT count(*) FROM card WHERE priority <> 2`,
	} {
		var n int64
		if err := h.db.Get(&n, q); err != nil {
			return nil, err
		}
		counts[kind] = n
	}
	vectors, err := h.vectors()
	if err != nil {
		return nil, err
	}
	counts["vectors"] = vectors
	return counts, nil
}

func (h *home) vectors() (int64, error) {
	paths, err := filepath.Glob(filepath.Join(h.dir, "projects", "*", "vectors.db"))
	if err != nil {
		return 0, err
	}
	var total int64
	for _, path := range paths {
		n, err := countVectors(path)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", path, err)
		}
		total += n
	}
	return total, nil
}

func countVectors(path string) (int64, error) {
	db, err := sqlx.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var tables []string
	if err := db.Select(&tables, `SELECT m.name FROM sqlite_master m
		WHERE m.type = 'table' AND EXISTS (SELECT 1 FROM pragma_table_info(m.name) WHERE name = 'embedding')`); err != nil {
		return 0, err
	}
	var total int64
	for _, t := range tables {
		var n int64
		if err := db.Get(&n, `SELECT count(*) FROM "`+strings.ReplaceAll(t, `"`, `""`)+`"`); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func (h *home) snapshot(p localProject) (snapshot, error) {
	s := snapshot{Project: p}
	queries := []struct {
		dst any
		q   string
	}{
		{&s.Boards, `SELECT id, name, created_at FROM board WHERE project_id = ? ORDER BY name`},
		{&s.Columns, `SELECT c.id, c.board_id, c.name, c.position, c.is_done FROM column_ c JOIN board b ON b.id = c.board_id
			WHERE b.project_id = ? ORDER BY c.board_id, c.position`},
		{&s.Cards, `SELECT id, board_id, column_id, seq, ref, title, body_md, version, created_at, updated_at, archived_at FROM card
			WHERE project_id = ? ORDER BY column_id, priority, rank`},
		{&s.Comments, `SELECT n.card_id, n.body_md, n.created_at FROM comment n JOIN card c ON c.id = n.card_id
			WHERE c.project_id = ? ORDER BY n.created_at`},
		{&s.Entries, `SELECT id, slug, title, template, summary, recap, global, private, verify_by, verified_at, version,
			provenance, created_at, updated_at FROM entry WHERE project_id = ? ORDER BY slug`},
		{&s.Pins, `SELECT p.entry_id, p.board_id, p.created_at FROM pin p JOIN entry e ON e.id = p.entry_id WHERE e.project_id = ?`},
		{&s.Nominations, `SELECT n.entry_id, n.reason, n.created_at FROM nomination n JOIN entry e ON e.id = n.entry_id
			WHERE e.project_id = ? ORDER BY n.created_at`},
		{&s.Artifacts, `SELECT id, name, kind, mime, content_hash, created_at, updated_at FROM artifact WHERE project_id = ? ORDER BY name`},
		{&s.Links, `SELECT l.from_type, l.from_id, l.to_type, l.to_id, l.to_raw, l.rel FROM link l
			WHERE l.rel IN ('wikilink', 'cites', 'blocked_by', 'artifact') AND (
				(l.from_type = 'entry' AND l.from_id IN (SELECT id FROM entry WHERE project_id = ?)) OR
				(l.from_type = 'card' AND l.from_id IN (SELECT id FROM card WHERE project_id = ?)))`},
	}
	for _, q := range queries {
		args := []any{p.ID}
		if strings.Count(q.q, "?") == 2 {
			args = append(args, p.ID)
		}
		if err := h.db.Select(q.dst, q.q, args...); err != nil {
			return s, err
		}
	}
	position := map[string]float64{}
	for i := range s.Cards {
		c := &s.Cards[i]
		position[c.ColumnID]++
		c.Position = position[c.ColumnID]
		if c.Ref == "" {
			c.Ref = p.Key + "-" + strconv.FormatInt(c.Seq, 10)
		}
	}
	for i := range s.Entries {
		if err := h.readEntry(p.Key, &s.Entries[i]); err != nil {
			return s, err
		}
	}
	for i := range s.Artifacts {
		a := &s.Artifacts[i]
		path := filepath.Join(h.dir, "projects", p.Key, "artifacts", filepath.FromSlash(a.Name))
		content, err := os.ReadFile(path)
		if err != nil {
			return s, fmt.Errorf("artifact %s: %w", a.Name, err)
		}
		a.Content = content
	}
	return s, nil
}

func (h *home) entryPath(key string, e *localEntry) string {
	dir := filepath.Join(h.dir, "projects", key, "vault")
	if e.Global {
		dir = filepath.Join(h.dir, "global", "vault")
	}
	return filepath.Join(dir, filepath.FromSlash(e.Slug)+".md")
}

func (h *home) readEntry(key string, e *localEntry) error {
	path := h.entryPath(key, e)
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("entry %s: %w", e.Slug, err)
	}
	fm, body, err := splitEntry(raw)
	if err != nil {
		return fmt.Errorf("entry %s: %s: %w", e.Slug, path, err)
	}
	e.Body, e.Sources = body, fm.Sources
	e.Fields = maps.Clone(fm.Extra)
	if e.Fields == nil {
		e.Fields = map[string]any{}
	}
	maps.DeleteFunc(e.Fields, func(k string, _ any) bool { return slices.Contains(frontmatterKeys, k) })
	revs, err := readRevisions(path)
	if err != nil {
		return fmt.Errorf("entry %s: %w", e.Slug, err)
	}
	if !slices.ContainsFunc(revs, func(r revision) bool { return r.Version == e.Version }) {
		revs = append(revs, revision{Version: e.Version, Title: e.Title, Summary: e.Summary, Body: e.Body, At: time.UnixMilli(e.UpdatedAt)})
	}
	e.Revisions = revs
	return nil
}

func readRevisions(entryPath string) ([]revision, error) {
	dir := filepath.Join(filepath.Dir(entryPath), "."+filepath.Base(entryPath))
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []revision
	for _, f := range files {
		v, err := strconv.ParseInt(strings.TrimSuffix(f.Name(), filepath.Ext(f.Name())), 10, 64)
		if f.IsDir() || err != nil || v <= 0 {
			continue
		}
		path := filepath.Join(dir, f.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		info, err := f.Info()
		if err != nil {
			return nil, err
		}
		fm, body, err := splitEntry(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, revision{Version: v, Title: fm.Title, Summary: fm.Summary, Body: body, At: info.ModTime()})
	}
	slices.SortFunc(out, func(a, b revision) int { return int(a.Version - b.Version) })
	return out, nil
}

func splitEntry(raw []byte) (frontmatter, string, error) {
	var fm frontmatter
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return fm, s, nil
	}
	header, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return fm, s, nil
	}
	body = strings.TrimPrefix(strings.TrimPrefix(body, "\n"), "\n")
	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return fm, body, err
	}
	return fm, body, nil
}

func nullMilli(v *int64) sql.Null[time.Time] {
	if v == nil {
		return sql.Null[time.Time]{}
	}
	return sql.Null[time.Time]{V: time.UnixMilli(*v), Valid: true}
}
