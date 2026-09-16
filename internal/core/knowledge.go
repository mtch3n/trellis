package core

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/home"
)

//go:embed templates/*.md
var templateFS embed.FS

// GlobalKey is reserved: init refuses it as a project key so [[GLOBAL/x]]
// cannot collide with a real project (§10.6).
const GlobalKey = "GLOBAL"

// Knowledge is the cached row for one markdown file. The file always wins: every
// read compares mtime and size and re-reads when they moved (§5).
type Knowledge struct {
	ID         string  `db:"id" json:"id"`
	ProjectID  string  `db:"project_id" json:"-"`
	BoardID    *string `db:"board_id" json:"-"`
	Slug       string  `db:"slug" json:"slug"`
	Title      string  `db:"title" json:"title"`
	Path       string  `db:"path" json:"path"`
	DocType    string  `db:"doc_type" json:"type"`
	Summary    string  `db:"summary" json:"summary,omitempty"`
	Provenance string  `db:"provenance" json:"provenance,omitempty"`
	Recap      *string `db:"recap" json:"recap,omitempty"`
	RecapHash  *string `db:"recap_hash" json:"-"`
	// BodyMD is loaded from Path and is deliberately not persisted in SQLite.
	BodyMD      string `db:"-" json:"body,omitempty"`
	ContentHash string `db:"content_hash" json:"-"`
	MTime       int64  `db:"mtime" json:"-"`
	Size        int64  `db:"size" json:"-"`
	Global      bool   `db:"global" json:"global,omitempty"`
	Private     bool   `db:"private" json:"private,omitempty"`
	ReviewBy    *int64 `db:"review_by" json:"review_by,omitempty"`
	ReviewedAt  *int64 `db:"reviewed_at" json:"reviewed_at,omitempty"`
	Version     int64  `db:"version" json:"version"`
	CreatedAt   int64  `db:"created_at" json:"created_at"`
	UpdatedAt   int64  `db:"updated_at" json:"updated_at"`

	// Computed for display.
	Ref       string        `db:"-" json:"ref"`             // KEY/slug
	BoardName string        `db:"-" json:"board,omitempty"` // association only
	Tags      []string      `db:"-" json:"tags,omitempty"`
	Labels    []string      `db:"-" json:"labels,omitempty"`
	Artifacts []ArtifactRef `db:"-" json:"artifacts,omitempty"`
}

// ArtifactRef is an artifact as an entry names it. A name that does not resolve
// to exactly one artifact of the entry's project is Missing and carries nothing
// but its name. Missing is always serialised, so a caller can test it without
// guessing what an absent field means.
type ArtifactRef struct {
	Name    string `db:"name" json:"name"`
	Kind    string `db:"kind" json:"kind,omitempty"`
	MIME    string `db:"mime" json:"mime,omitempty"`
	Size    int64  `db:"size" json:"size,omitempty"`
	Missing bool   `db:"missing" json:"missing"`
}

// NewKnowledge is what `knowledge new` supplies.
type NewKnowledge struct {
	Title      string
	Provenance string // authored | prompted | extracted; defaults to authored
	Private    bool
	Body       string // empty means the template
	Template   string
	Summary    string
	Board      string // board name, association only
	Tags       []string
	Labels     []string
}

// KnowledgeEdit is a whole-document replacement. Nil fields retain their
// current values, allowing callers to update frontmatter without losing the
// body (or update the body without losing title and summary).
type KnowledgeEdit struct {
	Title   *string
	Summary *string
	Body    *string
	// Artifacts, when non-nil, replaces the entry's artifact list.
	Artifacts *[]string
	IfVersion *int64
}

// Templates lists the shipped template names.
func Templates() []string {
	entries, _ := templateFS.ReadDir("templates")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	slices.Sort(names)
	return names
}

func templateBody(name, title string) (string, error) {
	if name == "" {
		name = "note"
	}
	raw, err := templateFS.ReadFile("templates/" + name + ".md")
	if err != nil {
		return "", ErrUsage("unknown_template", "no template "+name,
			"trellis knowledge new --template "+strings.Join(Templates(), "|"))
	}
	return strings.ReplaceAll(string(raw), "{{TITLE}}", title), nil
}

// WithKBRoot overrides where knowledge files live. Tests use it; the CLI does
// not, because the storage root is resolved once from TRELLIS_HOME (§5.1).
func (c *Core) WithKBRoot(dir string) *Core {
	c.kbRoot = dir
	return c
}

func (c *Core) root() (string, error) {
	if c.kbRoot != "" {
		return c.kbRoot, nil
	}
	return home.Root()
}

// kbDir is where a project's vault lives: one directory per project, plus the
// reserved global one.
func (c *Core) kbDir(projectKey string, global bool) (string, error) {
	root, err := c.root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "projects", projectKey, "knowledge")
	if global {
		dir = filepath.Join(root, "global", "knowledge")
	}
	return dir, os.MkdirAll(dir, 0o700)
}

// CreateKnowledge writes the file first and the row second: the file is the
// record, and a row pointing at a file that was never written would be a lie.
func (c *Core) CreateKnowledge(ctx context.Context, projectID string, in NewKnowledge) (Knowledge, error) {
	if strings.TrimSpace(in.Title) == "" {
		return Knowledge{}, ErrUsage("missing_title", "a knowledge entry needs a title",
			`trellis knowledge new --title "Concurrency model"`)
	}
	provenance, err := checkProvenance(in.Provenance)
	if err != nil {
		return Knowledge{}, err
	}
	body := in.Body
	if body == "" {
		if body, err = templateBody(in.Template, in.Title); err != nil {
			return Knowledge{}, err
		}
	}
	if err := c.checkWrite(ctx, ProposedWrite{
		Op: "doc.write", EntityType: "knowledge", ProjectID: projectID,
		Fields: map[string]string{"title": in.Title, "body": body},
	}); err != nil {
		return Knowledge{}, err
	}

	var doc Knowledge
	var writtenPath string
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID); err != nil {
			return err
		}
		dir, err := c.kbDir(key, false)
		if err != nil {
			return err
		}
		slug, err := uniqueSlug(tx, projectID, Slugify(in.Title))
		if err != nil {
			return err
		}

		var boardID *string
		boardName := ""
		if in.Board != "" {
			b, err := c.boardByName(tx, projectID, in.Board)
			if err != nil {
				return err
			}
			boardID, boardName = &b.ID, b.Name
		}

		now := c.clock.NowMS()
		fm := Frontmatter{
			Title: in.Title, Type: cmpOr(in.Template, "note"), Summary: in.Summary,
			Provenance: provenance,
			Private:    in.Private,
			Board:      boardName, Tags: in.Tags, Labels: in.Labels,
			Created: msToRFC3339(now), Updated: msToRFC3339(now),
		}
		raw := RenderDoc(fm, body)
		path := filepath.Join(dir, slug+".md")
		if err := writeAtomic(path, []byte(raw), false); err != nil {
			return err
		}
		writtenPath = path
		st, err := os.Stat(path)
		if err != nil {
			return err
		}

		doc = Knowledge{
			ID: NewCardID(), ProjectID: projectID, BoardID: boardID, Slug: slug,
			Title: in.Title, Path: path, DocType: fm.Type, Summary: in.Summary,
			Provenance: provenance,
			Private:    in.Private,
			BodyMD:     body, ContentHash: ContentHash(raw), MTime: st.ModTime().UnixMilli(),
			Size: st.Size(), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertKnowledge(tx, doc); err != nil {
			return err
		}
		if err := c.syncDocRelations(tx, &doc, fm, body); err != nil {
			return err
		}
		if err := c.resolveDocStubs(tx, &doc); err != nil {
			return err
		}
		if err := c.rebuildKnowledgeFTS(tx); err != nil {
			return err
		}
		if err := c.recordEvent(tx, "knowledge", doc.ID, "created", "", "", doc.Title); err != nil {
			return err
		}
		return c.docView(tx, &doc)
	})
	if err != nil && writtenPath != "" {
		// A failed transaction must not leave a database-less knowledge file.
		// Keep a committed file intact if SQLite reports an ambiguous commit by
		// only removing the path when it still has the exact bytes we wrote.
		if raw, readErr := os.ReadFile(writtenPath); readErr == nil && ContentHash(string(raw)) == doc.ContentHash {
			_ = os.Remove(writtenPath)
			_ = syncDirectory(filepath.Dir(writtenPath))
		}
	}
	if err == nil {
		c.notifyKnowledgeChanged(ctx, projectID)
	}
	return doc, err
}

func insertKnowledge(tx *sqlx.Tx, d Knowledge) error {
	_, err := tx.Exec(
		`INSERT INTO knowledge (id, project_id, board_id, slug, title, path, doc_type, summary,
		                        provenance, private, content_hash, mtime, size, global, version,
		                        created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.ProjectID, d.BoardID, d.Slug, d.Title, d.Path, d.DocType, d.Summary,
		d.Provenance, d.Private, d.ContentHash, d.MTime, d.Size, d.Global, d.Version,
		d.CreatedAt, d.UpdatedAt)
	return err
}

// uniqueSlug resolves collisions the way board slugs do: design, then design-2.
func uniqueSlug(tx *sqlx.Tx, projectID, base string) (string, error) {
	if base == "" {
		base = "untitled"
	}
	for n := 1; ; n++ {
		slug := base
		if n > 1 {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		var exists int
		if err := tx.Get(&exists,
			`SELECT COUNT(*) FROM knowledge WHERE project_id = ? AND slug = ?`, projectID, slug); err != nil {
			return "", err
		}
		if exists == 0 {
			return slug, nil
		}
	}
}

// LoadKnowledge resolves a slug within a project, re-reading the file when it
// changed underneath (§8.6: the file always wins).
func (c *Core) LoadKnowledge(ctx context.Context, projectID, slug string) (Knowledge, error) {
	var doc Knowledge
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return c.loadDoc(tx, projectID, slug, &doc)
	})
	if err == nil {
		c.notifyKnowledgeChanged(ctx, projectID)
	}
	return doc, err
}

// ReadKnowledge is LoadKnowledge plus the read counter that the escalation
// queue and `knowledge ls --cold` are computed from. Separate from Load so
// internal lookups — lint, the graph, resolving a link — do not inflate a
// number that is supposed to mean "a person or agent went and read this".
func (c *Core) ReadKnowledge(ctx context.Context, projectID, slug string) (Knowledge, error) {
	var doc Knowledge
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadDoc(tx, projectID, slug, &doc); err != nil {
			return err
		}
		return c.recordRead(tx, doc.ID)
	})
	return doc, err
}

func (c *Core) loadDoc(tx *sqlx.Tx, projectID, slug string, out *Knowledge) error {
	err := tx.Get(out,
		`SELECT * FROM knowledge WHERE slug = ? AND (project_id = ? OR global = 1) ORDER BY global LIMIT 1`,
		Slugify(slug), projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound("knowledge_not_found", "no knowledge entry "+slug+" in this project",
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

// refreshFromFile re-reads the file when mtime or size moved. This is the whole
// of the "stat sweep": a stat is microseconds, so it runs on every read rather
// than on a schedule, and an edit in Obsidian is visible to the next command.
func (c *Core) refreshFromFile(tx *sqlx.Tx, doc *Knowledge) error {
	st, err := os.Stat(doc.Path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound("file_missing", "the file for "+doc.Slug+" is gone: "+doc.Path,
			"trellis knowledge rm "+doc.Slug+"   # drop the row too")
	}
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		return err
	}
	fm, body, err := splitDocFile(doc.Path, raw)
	if err != nil {
		return err
	}
	doc.Title = cmpOr(fm.Title, doc.Title)
	doc.DocType = cmpOr(fm.Type, doc.DocType)
	doc.Summary = fm.Summary
	doc.Provenance = fm.Provenance
	// The flag is compared separately because the content hash cannot see it:
	// a database restored from an older backup, or a file that already carried
	// the key when the column was added, has an unchanged file and a wrong row.
	becamePrivate := fm.Private && !doc.Private
	privateDrifted := doc.Private != fm.Private
	doc.Private = fm.Private
	doc.BodyMD = body
	oldHash := doc.ContentHash
	doc.ContentHash = ContentHash(string(raw))
	changed := st.ModTime().UnixMilli() != doc.MTime || st.Size() != doc.Size ||
		oldHash != doc.ContentHash || privateDrifted
	doc.MTime = st.ModTime().UnixMilli()
	doc.Size = st.Size()
	doc.UpdatedAt = c.clock.NowMS()
	if !changed {
		doc.BodyMD = body
		return nil
	}
	doc.Version++

	if _, err := tx.Exec(
		`UPDATE knowledge SET title = ?, doc_type = ?, summary = ?, provenance = ?, private = ?,
		                      content_hash = ?, mtime = ?, size = ?, version = ?,
		                      updated_at = ? WHERE id = ?`,
		doc.Title, doc.DocType, doc.Summary, doc.Provenance, doc.Private, doc.ContentHash,
		doc.MTime, doc.Size, doc.Version, doc.UpdatedAt, doc.ID); err != nil {
		return err
	}
	if becamePrivate {
		if err := c.purgeDisclosedCopies(tx, doc); err != nil {
			return err
		}
	}
	if err := c.syncDocRelations(tx, doc, fm, body); err != nil {
		return err
	}
	if err := c.rebuildKnowledgeFTS(tx); err != nil {
		return err
	}
	return c.recordEvent(tx, "knowledge", doc.ID, "reloaded", "", "", "external edit")
}

// docView fills the computed fields.
func (c *Core) docView(tx *sqlx.Tx, doc *Knowledge) error {
	key := GlobalKey
	if !doc.Global {
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, doc.ProjectID); err != nil {
			return err
		}
	}
	doc.Ref = key + "/" + doc.Slug
	doc.BoardName = ""
	if doc.BoardID != nil {
		if err := tx.Get(&doc.BoardName, `SELECT name FROM board WHERE id = ?`, *doc.BoardID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	doc.Tags = []string{}
	if err := tx.Select(&doc.Tags,
		`SELECT t.name FROM tag t JOIN knowledge_tag kt ON kt.tag_id = t.id WHERE kt.doc_id = ? ORDER BY t.name`,
		doc.ID); err != nil {
		return err
	}
	doc.Labels = []string{}
	if err := tx.Select(&doc.Labels,
		`SELECT l.name FROM label l JOIN knowledge_label kl ON kl.label_id = l.id WHERE kl.doc_id = ? ORDER BY l.name`,
		doc.ID); err != nil {
		return err
	}
	// rowid order is the order syncDocRelations inserted the rows, which is
	// the order the file lists the names.
	doc.Artifacts = nil
	return tx.Select(&doc.Artifacts,
		`SELECT l.to_raw AS name,
		        COALESCE(a.kind, '') AS kind,
		        COALESCE(a.mime, '') AS mime,
		        COALESCE(a.size, 0)  AS size,
		        (l.to_id IS NULL)    AS missing
		 FROM link l LEFT JOIN artifact a ON a.id = l.to_id
		 WHERE l.from_type = 'doc' AND l.from_id = ? AND l.rel = 'artifact'
		 ORDER BY l.rowid`, doc.ID)
}

// ListKnowledge returns the selected board's entries plus the unscoped ones
// (§10.1): a board is a lens, so narrowing by one never hides project-wide
// knowledge. An empty boardID lists the whole project.
// KnowledgeFilter narrows a listing. A zero value lists everything the project
// can see, which is what almost every caller wants.
type KnowledgeFilter struct {
	BoardID     string   // association only; entries with no board always match
	DocTypes    []string // doc_type values to keep; empty keeps all
	Provenances []string // ingestion paths to keep; empty keeps all
}

func (f KnowledgeFilter) where() (string, []any) {
	clauses, args := []string{"project_id = ?"}, []any{}
	if f.BoardID != "" {
		clauses = append(clauses, "(board_id IS NULL OR board_id = ?)")
		args = append(args, f.BoardID)
	}
	// An IN list is built from a closed vocabulary, never from user text, so
	// the placeholders are generated here rather than interpolated.
	for column, values := range map[string][]string{"doc_type": f.DocTypes, "provenance": f.Provenances} {
		if len(values) == 0 {
			continue
		}
		clauses = append(clauses, column+" IN (?"+strings.Repeat(", ?", len(values)-1)+")")
		for _, v := range values {
			args = append(args, v)
		}
	}
	slices.Sort(clauses[1:]) // map iteration must not reach the query
	return strings.Join(clauses, " AND "), args
}

func (c *Core) ListKnowledge(ctx context.Context, projectID string, f KnowledgeFilter) ([]Knowledge, error) {
	docs := []Knowledge{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		where, args := f.where()
		if err := tx.Select(&docs, `SELECT * FROM knowledge WHERE `+where+
			` ORDER BY updated_at DESC`, append([]any{projectID}, args...)...); err != nil {
			return err
		}
		for i := range docs {
			if err := c.refreshFromFile(tx, &docs[i]); err != nil {
				return err
			}
			if err := c.docView(tx, &docs[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return docs, err
}

// EditKnowledge replaces the body. The file is rewritten and the row follows.
func (c *Core) EditKnowledge(ctx context.Context, projectID, slug, body string, ifVersion *int64) (Knowledge, error) {
	return c.EditKnowledgeFields(ctx, projectID, slug, KnowledgeEdit{Body: &body, IfVersion: ifVersion})
}

// EditKnowledgeFields atomically replaces the selected Markdown fields and
// updates the cached row from the same rendered file.
func (c *Core) EditKnowledgeFields(ctx context.Context, projectID, slug string, in KnowledgeEdit) (Knowledge, error) {
	var doc Knowledge
	var oldRaw []byte
	var wroteFile bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadDoc(tx, projectID, slug, &doc); err != nil {
			return err
		}
		// refreshFromFile has already folded in any external edit, so a version
		// mismatch here means exactly that: someone else changed the file.
		if in.IfVersion != nil && *in.IfVersion != doc.Version {
			return &Error{
				Code: "conflict", Exit: 4,
				Msg: fmt.Sprintf("%s changed on disk since you read it (you: v%d, now: v%d)",
					doc.Slug, *in.IfVersion, doc.Version),
				Fix: "trellis knowledge show " + doc.Slug,
			}
		}
		fields := map[string]string{}
		if in.Body != nil {
			fields["body"] = *in.Body
		}
		if in.Title != nil {
			fields["title"] = *in.Title
		}
		if in.Summary != nil {
			fields["summary"] = *in.Summary
		}
		if in.Artifacts != nil {
			fields["artifacts"] = strings.Join(*in.Artifacts, "\n")
		}
		if err := c.checkWrite(ctx, ProposedWrite{
			Op: "doc.write", EntityType: "knowledge", EntityID: doc.ID, ProjectID: projectID,
			Fields: fields,
		}); err != nil {
			return err
		}

		raw, err := os.ReadFile(doc.Path)
		if err != nil {
			return err
		}
		oldRaw = append(oldRaw[:0], raw...)
		fm, _, err := splitDocFile(doc.Path, raw)
		if err != nil {
			return err
		}
		before := slices.Clone(fm.Artifacts)
		body := doc.BodyMD
		if in.Body != nil {
			body = *in.Body
		}
		if in.Title != nil {
			if strings.TrimSpace(*in.Title) == "" {
				return ErrUsage("missing_title", "a knowledge entry needs a title", "trellis knowledge show "+doc.Slug)
			}
			fm.Title = *in.Title
		}
		if in.Summary != nil {
			fm.Summary = *in.Summary
		}
		if in.Artifacts != nil {
			fm.Artifacts = dedupeNames(*in.Artifacts)
		}
		now := c.clock.NowMS()
		fm.Updated = msToRFC3339(now)
		out := RenderDoc(fm, body)
		if err := writeAtomic(doc.Path, []byte(out), true); err != nil {
			return err
		}
		wroteFile = true
		st, err := os.Stat(doc.Path)
		if err != nil {
			return err
		}

		doc.Title = cmpOr(fm.Title, doc.Title)
		doc.Summary = fm.Summary
		doc.BodyMD = body
		doc.ContentHash = ContentHash(out)
		doc.MTime = st.ModTime().UnixMilli()
		doc.Size = st.Size()
		doc.Version++
		doc.UpdatedAt = now
		if _, err := tx.Exec(
			`UPDATE knowledge SET title = ?, summary = ?, content_hash = ?, mtime = ?, size = ?,
			                      version = ?, updated_at = ? WHERE id = ?`,
			doc.Title, doc.Summary, doc.ContentHash, doc.MTime, doc.Size, doc.Version, doc.UpdatedAt, doc.ID); err != nil {
			return err
		}
		if err := c.syncDocRelations(tx, &doc, fm, body); err != nil {
			return err
		}
		if err := c.rebuildKnowledgeFTS(tx); err != nil {
			return err
		}
		for _, field := range []string{"body", "title", "summary"} {
			if _, ok := fields[field]; !ok {
				continue
			}
			// The value is the content. A private entry records that it was
			// edited and nothing more, because an audit log holding whole
			// bodies is a copy of them.
			value := fields[field]
			if doc.Private {
				value = ""
			}
			if err := c.recordEvent(tx, "knowledge", doc.ID, "edited", field, "", value); err != nil {
				return err
			}
		}
		// An artifact name is metadata, not content, so it is recorded for a
		// private entry too; presence is not what the disclosure design
		// protects.
		if in.Artifacts != nil {
			for _, name := range namesAdded(before, fm.Artifacts) {
				if err := c.recordEvent(tx, "knowledge", doc.ID, "artifact_linked", "", "", name); err != nil {
					return err
				}
			}
			for _, name := range namesAdded(fm.Artifacts, before) {
				if err := c.recordEvent(tx, "knowledge", doc.ID, "artifact_unlinked", "", "", name); err != nil {
					return err
				}
			}
		}
		return c.docView(tx, &doc)
	})
	if err != nil && wroteFile {
		if restoreErr := writeAtomic(doc.Path, oldRaw, true); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
	}
	if err == nil {
		c.notifyKnowledgeChanged(ctx, projectID)
	}
	return doc, err
}

// DeleteKnowledge removes the row and the file.
func (c *Core) DeleteKnowledge(ctx context.Context, projectID, slug string) error {
	var staged *stagedRemoval
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var doc Knowledge
		if err := tx.Get(&doc,
			`SELECT * FROM knowledge WHERE project_id = ? AND slug = ?`, projectID, Slugify(slug)); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("knowledge_not_found", "no knowledge entry "+slug, "trellis knowledge ls")
			}
			return err
		}
		var err error
		staged, err = stageRemoval(doc.Path)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM knowledge WHERE id = ?`, doc.ID); err != nil {
			return err
		}
		if err := c.rebuildKnowledgeFTS(tx); err != nil {
			return err
		}
		// Inbound links survive as stubs rather than vanishing: a reference to
		// something deleted is a finding, not a silent no-op (§10.4).
		if _, err := tx.Exec(
			`UPDATE link SET to_id = NULL WHERE to_type = 'doc' AND to_id = ?`, doc.ID); err != nil {
			return err
		}
		return c.recordEvent(tx, "knowledge", doc.ID, "deleted", "", doc.Title, "")
	})
	if err != nil {
		if restoreErr := staged.restore(); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
		return err
	}
	if err := staged.finalize(); err != nil {
		return err
	}
	if err == nil {
		c.notifyKnowledgeChanged(ctx, projectID)
	}
	return err
}

// RebuildKnowledgeSearch refreshes the derived FTS index from the Markdown
// files. It is intentionally rebuildable: SQLite stores metadata and search
// terms, while the file remains the source of truth.
func (c *Core) RebuildKnowledgeSearch(ctx context.Context) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec("DELETE FROM knowledge_search_state"); err != nil {
			return err
		}
		return c.rebuildKnowledgeFTS(tx)
	})
}

// SyncKnowledgeSearch indexes only files whose metadata changed, or rows absent
// from the cache after a migration. Missing files lose their stale search terms
// but retain metadata so knowledge rm and diagnostics remain usable.
func (c *Core) SyncKnowledgeSearch(ctx context.Context) error {
	return c.Tx(ctx, c.rebuildKnowledgeFTS)
}

func (c *Core) rebuildKnowledgeFTS(tx *sqlx.Tx) error {
	var docs []struct {
		RowID   int64  `db:"rowid"`
		Path    string `db:"path"`
		Title   string `db:"title"`
		Summary string `db:"summary"`
		Stamp   int64  `db:"stamp"`
		Size    int64  `db:"size"`
	}
	if err := tx.Select(&docs, `SELECT k.rowid, k.path, k.title, k.summary,
 COALESCE(s.mtime, -1) AS stamp, COALESCE(s.size, -1) AS size
 FROM knowledge k LEFT JOIN knowledge_search_state s ON s.rowid = k.rowid`); err != nil {
		return err
	}
	for _, d := range docs {
		st, err := os.Stat(d.Path)
		if errors.Is(err, os.ErrNotExist) {
			if _, err := tx.Exec("DELETE FROM knowledge_fts WHERE rowid = ?", d.RowID); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM knowledge_search_state WHERE rowid = ?", d.RowID); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if st.ModTime().UnixNano() == d.Stamp && st.Size() == d.Size {
			continue
		}
		raw, err := os.ReadFile(d.Path)
		if err != nil {
			return err
		}
		fm, body, err := splitDocFile(d.Path, raw)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO knowledge_fts(rowid, title, summary, body_md) VALUES (?, ?, ?, ?)", d.RowID, cmpOr(fm.Title, d.Title), fm.Summary, body); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO knowledge_search_state(rowid, mtime, size) VALUES (?, ?, ?)", d.RowID, st.ModTime().UnixNano(), st.Size()); err != nil {
			return err
		}
	}
	return nil
}

func msToRFC3339(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Provenances are the ingestion paths an entry can arrive by. The set is closed
// for the same reason the label vocabulary is: a value invented mid-sentence is
// a value nothing can be compared against, and comparing them is the whole
// point of recording it.
//
//	authored   an agent wrote it under the writing-knowledge gate
//	prompted   a hook insisted; an agent still wrote it
//	extracted  a model produced it from conversation
func Provenances() []string { return []string{"authored", "prompted", "extracted"} }

// checkProvenance defaults to authored, which is what the ordinary path is.
// It does not reject an unrecognised value read back from a file: the file is
// the source of truth, and `knowledge lint` is where vault problems are
// reported rather than raised mid-write.
func checkProvenance(v string) (string, error) {
	if v == "" {
		return "authored", nil
	}
	if slices.Contains(Provenances(), v) {
		return v, nil
	}
	return "", ErrUsage("unknown_provenance",
		"provenance must be one of "+strings.Join(Provenances(), ", "),
		`trellis knowledge new --title "..." --provenance extracted`)
}
