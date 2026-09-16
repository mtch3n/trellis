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
	"github.com/mtch3n/trellis/internal/vpath"
)

//go:embed templates/*.md
var templateFS embed.FS

// GlobalKey names the global vault. vpath owns the reservation: no project can
// take the key, so /GLOBAL/knowledge/<slug> never collides with a project.
const GlobalKey = vpath.GlobalKey

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
	Ref       string        `db:"-" json:"ref"`             // /KEY/knowledge/<slug> or /GLOBAL/knowledge/<slug>
	BoardName string        `db:"-" json:"board,omitempty"` // association only
	Tags      []string      `db:"-" json:"tags,omitempty"`
	Labels    []string      `db:"-" json:"labels,omitempty"`
	Artifacts []ArtifactRef `db:"-" json:"artifacts,omitempty"`
	Sources   []string      `db:"-" json:"sources,omitempty"`
	// Warnings is set only by CreateKnowledge, when creating from a
	// template under enforce: warn found a problem: a missing required
	// field, a value outside its choices, or a missing section. It is
	// never persisted or reloaded — the render-once model checks a
	// document against its template once, at creation.
	Warnings []string `db:"-" json:"warnings,omitempty"`
}

// ArtifactRef is an artifact as an entry names it. A name that does not resolve
// to exactly one artifact of the entry's project is Missing and carries nothing
// but its name. Missing is always serialised, so a caller can test it without
// guessing what an absent field means.
type ArtifactRef struct {
	Name    string `db:"name" json:"name"`
	Kind    string `db:"kind" json:"kind,omitempty"`
	MIME    string `db:"mime" json:"mime,omitempty"`
	Size    int64  `db:"size" json:"size,omitzero"`
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
	// Dir places the entry in a directory instead of the vault root; empty
	// means the root. NewDir creates Dir even if it resembles an existing
	// directory.
	Dir    string
	NewDir bool
	// Set supplies values for fields a template asks for (required or
	// choices), and any other field the caller wants recorded. Every entry
	// is written into the new document's frontmatter.
	Set map[string]string
	// Sources cites what this entry's claims are based on. See
	// Frontmatter.Sources.
	Sources []string
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
	// Sources, when non-nil, replaces the entry's source list.
	Sources   *[]string
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
	for name := range in.Set {
		if flag, reserved := reservedFrontmatterFields[name]; reserved {
			return Knowledge{}, ErrUsage("reserved_field",
				`"`+name+`" is a built-in frontmatter field and cannot be set with --set`,
				"use "+flag+" instead")
		}
	}
	templatesDirPath, err := c.templatesDir()
	if err != nil {
		return Knowledge{}, err
	}
	tmpl, err := loadTemplate(templatesDirPath, cmpOr(in.Template, "note"))
	if err != nil {
		return Knowledge{}, err
	}
	fields := map[string][]string{"sources": cleanSources(in.Sources)}
	for k, v := range in.Set {
		fields[k] = []string{v}
	}
	body := in.Body
	checkSections := body != ""
	if body == "" {
		body = stripOptionalMarkers(renderTemplateBody(tmpl.Body, in.Title, in.Set))
	}
	violations := templateViolations(tmpl, fields, body, checkSections)
	verifyProblems, err := c.templateVerifyViolations(ctx, projectID, tmpl, fields, body)
	if err != nil {
		return Knowledge{}, err
	}
	violations = append(violations, verifyProblems...)
	if len(violations) > 0 && tmpl.Enforce == "reject" {
		return Knowledge{}, ErrUsage("template_violation",
			tmpl.Name+" does not meet its template:\n  - "+strings.Join(violations, "\n  - "),
			templateViolationFix(tmpl.Name, violations))
	}
	if err := c.checkWrite(ctx, ProposedWrite{
		Op: "doc.write", EntityType: "knowledge", ProjectID: projectID,
		Fields: map[string]string{"title": in.Title, "body": body},
	}); err != nil {
		return Knowledge{}, err
	}

	dirSlug, err := SlugifyPath(in.Dir)
	if err != nil {
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
		if err := c.refuseResemblingDir(tx, projectID, dirSlug, in.NewDir); err != nil {
			return err
		}
		base := truncateSegment(Slugify(in.Title))
		if dirSlug != "" {
			base = dirSlug + "/" + base
			if room := maxRelSlugLen - len(dirSlug) - 1; len(base) > maxRelSlugLen {
				if room < 1 {
					return ErrUsage("path_too_long",
						dirSlug+" leaves no room for a title-derived slug",
						"trellis knowledge new --title \"...\" --in <a shorter directory>")
				}
				leaf := strings.TrimRight(Slugify(in.Title)[:room], "-")
				base = dirSlug + "/" + leaf
			}
		}
		slug, err := uniqueSlug(tx, projectID, base)
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
			Sources: cleanSources(in.Sources),
			Created: msToRFC3339(now), Updated: msToRFC3339(now),
		}
		if len(in.Set) > 0 {
			fm.Extra = make(map[string]any, len(in.Set))
			for k, v := range in.Set {
				fm.Extra[k] = v
			}
		}
		raw := RenderDoc(fm, body)
		path := filepath.Join(dir, filepath.FromSlash(slug)+".md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
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
			Sources:    cleanSources(in.Sources),
			BodyMD:     body, ContentHash: ContentHash(raw), MTime: st.ModTime().UnixMilli(),
			Size: st.Size(), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertKnowledge(tx, doc); err != nil {
			return err
		}
		if err := c.captureKnowledgeRevision(doc.Path, doc.Version, []byte(raw)); err != nil {
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
			_ = os.Remove(revisionFilePath(writtenPath, 1))
			_ = removeRevisionDirIfEmpty(writtenPath)
		}
	}
	if err == nil {
		c.notifyKnowledgeChanged(ctx, projectID)
		if len(violations) > 0 {
			doc.Warnings = violations
		}
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

// uniqueSlug resolves collisions the way board slugs do: design, then
// design-2. A reserved Windows device name is treated as permanently taken
// even on the first attempt, so a title like "CON" becomes "con-2" — a name
// Windows can open — rather than a file it cannot.
func uniqueSlug(tx *sqlx.Tx, projectID, base string) (string, error) {
	if base == "" {
		base = "untitled"
	}
	for n := 1; ; n++ {
		slug := base
		if n > 1 {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		if reservedLeafTaken(slug) {
			continue
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
	key, err := projectKeyOf(tx, projectID)
	if err != nil {
		return err
	}
	d, err := readDocArg(slug, key)
	if err != nil {
		return err
	}
	if projectID == "" || d.scope == docVault {
		err = tx.Get(out, `SELECT * FROM knowledge WHERE slug = ? AND global = 1`, d.slug)
	} else if d.scope == docOwn {
		err = tx.Get(out, `SELECT * FROM knowledge WHERE slug = ? AND project_id = ? AND global = 0`, d.slug, projectID)
	} else {
		err = tx.Get(out, `SELECT * FROM knowledge WHERE slug = ? AND (project_id = ? OR global = 1) ORDER BY global LIMIT 1`, d.slug, projectID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundSlug(slug)
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
	doc.Sources = fm.Sources
	// The flag is compared separately because the content hash cannot see it:
	// a database restored from an older backup, or a file that already carried
	// the key when the column was added, has an unchanged file and a wrong row.
	becamePrivate := fm.Private && !doc.Private
	privateDrifted := doc.Private != fm.Private
	doc.Private = fm.Private
	doc.BodyMD = body
	oldHash := doc.ContentHash
	doc.ContentHash = ContentHash(string(raw))
	contentChanged := st.ModTime().UnixMilli() != doc.MTime || st.Size() != doc.Size || oldHash != doc.ContentHash
	changed := contentChanged || privateDrifted
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
	if contentChanged {
		if err := c.captureKnowledgeRevision(doc.Path, doc.Version, raw); err != nil {
			return err
		}
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
	doc.Ref = DocAddress(key, doc.Global, doc.Slug)
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
	Tags        []string // every listed tag must be present; empty keeps all
	Dir         string   // scope to this directory and its subtree; empty keeps everything
}

func (f KnowledgeFilter) where() (string, []any) {
	clauses, args := []string{"project_id = ?"}, []any{}
	if f.BoardID != "" {
		clauses = append(clauses, "(board_id IS NULL OR board_id = ?)")
		args = append(args, f.BoardID)
	}
	// An IN list is built from a closed vocabulary, never from user text, so
	// the placeholders are generated here rather than interpolated.
	for _, col := range []string{"doc_type", "provenance"} {
		var values []string
		switch col {
		case "doc_type":
			values = f.DocTypes
		case "provenance":
			values = f.Provenances
		}
		if len(values) == 0 {
			continue
		}
		clauses = append(clauses, col+" IN (?"+strings.Repeat(", ?", len(values)-1)+")")
		for _, v := range values {
			args = append(args, v)
		}
	}
	if len(f.Tags) > 0 {
		clauses = append(clauses,
			`id IN (SELECT kt.doc_id FROM knowledge_tag kt JOIN tag t ON t.id = kt.tag_id
			        WHERE t.name IN (?`+strings.Repeat(", ?", len(f.Tags)-1)+`)
			        GROUP BY kt.doc_id HAVING COUNT(DISTINCT t.name) = ?)`)
		for _, v := range f.Tags {
			args = append(args, v)
		}
		args = append(args, len(f.Tags))
	}
	if f.Dir != "" {
		clauses = append(clauses, "(slug = ? OR slug LIKE ? || '/%')")
		args = append(args, f.Dir, f.Dir)
	}
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

// changedOnDisk reports that a knowledge file no longer holds the bytes a
// write inside this transaction was based on: something outside Trellis, an
// editor most likely, won the race.
func changedOnDisk(slug string) error {
	return ErrConflict("conflict", slug+" changed on disk while it was being edited",
		"trellis knowledge show "+slug)
}

// writeLanded resolves the one question a failed tx.Commit leaves open: did
// the write the closure already made actually land? matches is whatever
// durable state says should be true if it did (a hash, a path, a row being
// gone); queryErr is the error from reading that durable state. A durable
// read that itself failed cannot answer the question, so it answers no:
// undo rather than guess.
func writeLanded(matches bool, queryErr error) bool {
	return queryErr == nil && matches
}

// EditKnowledgeFields atomically replaces the selected Markdown fields and
// updates the cached row from the same rendered file.
func (c *Core) EditKnowledgeFields(ctx context.Context, projectID, slug string, in KnowledgeEdit) (Knowledge, error) {
	var doc Knowledge
	var oldRaw []byte
	var written string
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure, or a panic, from here on undoes the write before this
		// closure returns, while Core.Tx still holds SQLite's write lock --
		// not after it rolls back and frees the lock for another process to
		// act in. done, not err, is what the undo is keyed on: a panic
		// unwinds through this defer without ever reaching the closure's own
		// return statement, so a named result would still read nil and the
		// undo would be skipped.
		defer func() {
			if !done && written != "" {
				// errors.Join always wraps, even when the second argument is
				// nil, which would turn this *Error into one coreError's
				// direct type assertion no longer recognises. Join only when
				// the undo itself failed; otherwise the original error, with
				// its code and exit intact, passes through unchanged.
				if uerr := undoWrite(doc.Path, oldRaw, written); uerr != nil {
					err = errors.Join(err, uerr)
				}
				written = ""
			}
		}()

		if err := c.loadDoc(tx, projectID, slug, &doc); err != nil {
			return err
		}
		if in.IfVersion == nil {
			return ErrUsage("version_required",
				"knowledge edit replaces whole fields and needs the version you read",
				fmt.Sprintf("trellis knowledge show %s --json   # then pass --if-version %d", doc.Slug, doc.Version))
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
		if in.Sources != nil {
			fields["sources"] = strings.Join(*in.Sources, "\n")
		}
		if err := c.checkWrite(ctx, ProposedWrite{
			Op: "doc.write", EntityType: "knowledge", EntityID: doc.ID, ProjectID: projectID,
			Fields: fields,
		}); err != nil {
			return err
		}

		// The one read this write is based on. loadDoc's own read, above, may
		// be stale by now: checkWrite just ran arbitrary policy code, and
		// nothing here holds a lock against a program outside Trellis.
		raw, err := os.ReadFile(doc.Path)
		if err != nil {
			return err
		}
		if ContentHash(string(raw)) != doc.ContentHash {
			return changedOnDisk(doc.Slug)
		}
		base := doc.ContentHash // the hash this write is based on
		oldRaw = raw
		if err := c.captureKnowledgeRevision(doc.Path, doc.Version, oldRaw); err != nil {
			return err
		}
		fm, body, err := splitDocFile(doc.Path, raw)
		if err != nil {
			return err
		}
		before := slices.Clone(fm.Artifacts)
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
		if in.Sources != nil {
			fm.Sources = cleanSources(*in.Sources)
		}
		now := c.clock.NowMS()
		fm.Updated = msToRFC3339(now)
		out := RenderDoc(fm, body)
		if err := replaceIfUnchanged(doc.Path, []byte(out), base); err != nil {
			if errors.Is(err, errFileChanged) {
				return changedOnDisk(doc.Slug)
			}
			return err
		}
		written = ContentHash(out)
		st, err := os.Stat(doc.Path)
		if err != nil {
			return err
		}

		doc.Title = cmpOr(fm.Title, doc.Title)
		doc.Summary = fm.Summary
		doc.BodyMD = body
		doc.ContentHash = written
		doc.Sources = fm.Sources
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
		// An artifact name is content the same way a title or body is: a
		// private entry's event records that a link changed and nothing
		// more, the same rule the loop above applies.
		if in.Artifacts != nil {
			for _, name := range namesAdded(before, fm.Artifacts) {
				value := name
				if doc.Private {
					value = ""
				}
				if err := c.recordEvent(tx, "knowledge", doc.ID, "artifact_linked", "", "", value); err != nil {
					return err
				}
			}
			for _, name := range namesAdded(fm.Artifacts, before) {
				value := name
				if doc.Private {
					value = ""
				}
				if err := c.recordEvent(tx, "knowledge", doc.ID, "artifact_unlinked", "", "", value); err != nil {
					return err
				}
			}
		}
		if err := c.captureKnowledgeRevision(doc.Path, doc.Version, []byte(out)); err != nil {
			return err
		}
		if err := c.docView(tx, &doc); err != nil {
			return err
		}
		done = true
		return nil
	})
	if err != nil && done {
		// done means the closure completed and it was tx.Commit that
		// failed: an ambiguous outcome the driver does not resolve for us.
		// Durable state is the only honest answer, and when it shows the
		// write landed, the edit is a success no matter what Commit
		// reported.
		var landed string
		qerr := c.db.Get(&landed, `SELECT content_hash FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(landed == written, qerr) {
			err = nil
		} else if uerr := undoWrite(doc.Path, oldRaw, written); uerr != nil {
			err = errors.Join(err, uerr)
		}
	}
	if err == nil {
		c.notifyKnowledgeChanged(ctx, projectID)
	}
	return doc, err
}

// DeleteKnowledge removes the row and the file.
func (c *Core) DeleteKnowledge(ctx context.Context, projectID, slug string) error {
	var staged, revStaged *stagedRemoval
	var doc Knowledge
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		resolved, rerr := c.resolveSlug(tx, projectID, slug, false)
		if rerr != nil {
			return rerr
		}
		if err := tx.Get(&doc,
			`SELECT * FROM knowledge WHERE project_id = ? AND slug = ?`, projectID, resolved); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return notFoundSlug(slug)
			}
			return err
		}
		var serr error
		staged, serr = stageRemoval(doc.Path)
		if serr != nil {
			return serr
		}
		revStaged, serr = stageRemoval(revisionDir(doc.Path))
		if serr != nil {
			if rerr := staged.restore(); rerr != nil {
				return errors.Join(serr, rerr)
			}
			return serr
		}
		// A failure, or a panic, below undoes the stage before this closure
		// returns, while Core.Tx still holds SQLite's write lock. done, not
		// err, is what the restore is keyed on: a panic unwinds through this
		// defer without ever reaching the closure's own return statement, so
		// a named result would still read nil and the restore would be
		// skipped.
		defer func() {
			if !done {
				if rerr := staged.restore(); rerr != nil {
					err = errors.Join(err, rerr)
				}
				if rerr := revStaged.restore(); rerr != nil {
					err = errors.Join(err, rerr)
				}
			}
		}()
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
		if err := c.recordEvent(tx, "knowledge", doc.ID, "deleted", "", doc.Title, ""); err != nil {
			return err
		}
		done = true
		return nil
	})
	if err != nil && done {
		// done means the closure completed and it was tx.Commit that
		// failed: an ambiguous outcome only durable state can resolve, and
		// when it shows the delete landed, the delete is a success no
		// matter what Commit reported. When done is false, the closure's
		// own defer already restored -- there is nothing here to resolve,
		// including the "not found" case where doc.ID is not a real row.
		var gone int
		qerr := c.db.Get(&gone, `SELECT COUNT(*) FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(gone == 0, qerr) {
			err = nil
		} else {
			if rerr := staged.restore(); rerr != nil {
				err = errors.Join(err, rerr)
			}
			if rerr := revStaged.restore(); rerr != nil {
				err = errors.Join(err, rerr)
			}
		}
	}
	if err != nil {
		return err
	}
	if err := staged.finalize(); err != nil {
		return err
	}
	if err := revStaged.finalize(); err != nil {
		return err
	}
	c.notifyKnowledgeChanged(ctx, projectID)
	return nil
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

// cleanSources trims each source and drops blank ones, but keeps
// duplicates: citing the same source twice is redundant, not wrong, and
// unlike an artifact name a source is never resolved by identity.
func cleanSources(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
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
