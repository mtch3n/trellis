package core

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/atomicfile"
)

//go:embed templates/*.md
var templateFS embed.FS

// GlobalKey names the global vault. package address owns the reservation: no project can
// take the key, so /GLOBAL/vault/<slug> never collides with a project.
const GlobalKey = address.GlobalKey

// Knowledge is the cached row for one markdown file. The file always wins: every
// read compares mtime and size and re-reads when they moved (§5).
type Knowledge struct {
	ID        string  `db:"id" json:"id"`
	ProjectID string  `db:"project_id" json:"-"`
	BoardID   *string `db:"board_id" json:"-"`
	Slug      string  `db:"slug" json:"slug"`
	Title     string  `db:"title" json:"title"`
	// Path is derived from the storage root, the entry's project key (or the
	// global vault), and its slug — see docPath. It is filled by docView and
	// refreshFromFile, never scanned from a column.
	Path       string  `db:"-" json:"path"`
	Template   string  `db:"template" json:"template"`
	Summary    string  `db:"summary" json:"summary,omitempty"`
	Provenance string  `db:"provenance" json:"provenance,omitempty"`
	Recap      *string `db:"recap" json:"recap,omitempty"`
	RecapHash  *string `db:"recap_hash" json:"-"`
	// BodyMD is loaded from Path and is deliberately not persisted in SQLite.
	BodyMD      string `db:"-" json:"body,omitempty"`
	ContentHash string `db:"content_hash" json:"-"`
	MTime       int64  `db:"mtime" json:"-"`
	Size        int64  `db:"size" json:"-"`
	Global      bool   `db:"global" json:"global,omitzero"`
	Private     bool   `db:"private" json:"private,omitzero"`
	ReviewBy    *int64 `db:"verify_by" json:"verify_by,omitempty"`
	ReviewedAt  *int64 `db:"verified_at" json:"verified_at,omitempty"`
	Version     int64  `db:"version" json:"version"`
	CreatedAt   int64  `db:"created_at" json:"created_at"`
	UpdatedAt   int64  `db:"updated_at" json:"updated_at"`

	// Computed for display.
	Ref       string         `db:"-" json:"ref"`             // /KEY/vault/<slug> or /GLOBAL/vault/<slug>
	BoardName string         `db:"-" json:"board,omitempty"` // association only
	Tags      []string       `db:"-" json:"tags,omitempty"`
	Labels    []string       `db:"-" json:"labels,omitempty"`
	Artifacts []ArtifactRef  `db:"-" json:"artifacts,omitempty"`
	Sources   []string       `db:"-" json:"sources,omitempty"`
	Fields    map[string]any `db:"-" json:"fields"`
	Missing   bool           `db:"-" json:"missing,omitzero"` // file is missing; content withheld
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
	Sources  *[]string
	Template *string
	Private  *bool
	Tags     *[]string
	Labels   *[]string
	// Set writes these frontmatter fields, the ones a template may ask for;
	// an empty value removes the field. Built-in fields have their own
	// options and are refused here.
	Set       map[string]string
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

// docDir is where a project's vault lives: one directory per project, plus
// the reserved global one. It does not create the directory; a write that
// needs it existing goes through kbDir.
func (c *Core) docDir(projectKey string, global bool) string {
	if global {
		return filepath.Join(c.root, "global", "vault")
	}
	return filepath.Join(c.root, "projects", projectKey, "vault")
}

// kbDir is docDir, creating the directory: only a write needs that.
func (c *Core) kbDir(projectKey string, global bool) (string, error) {
	dir := c.docDir(projectKey, global)
	return dir, os.MkdirAll(dir, 0o700)
}

// docPath is where one entry's file lives, derived from the storage root, its
// project key (GlobalKey for a global entry) and its slug (§ TRELLIS-36). It
// is never stored: a copied or moved storage root must not carry a stale
// absolute path along with it.
func (c *Core) docPath(projectKey string, global bool, slug string) string {
	return filepath.Join(c.docDir(projectKey, global), filepath.FromSlash(slug)+".md")
}

// keyOfDoc resolves the project key a document's file and address are built
// from: the global vault's reserved key for a global entry, or its owning
// project's key otherwise.
func (c *Core) keyOfDoc(tx *sqlx.Tx, doc *Knowledge) (string, error) {
	if doc.Global {
		return GlobalKey, nil
	}
	var key string
	err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, doc.ProjectID)
	return key, err
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
	// Handle empty template (no template enforced)
	body := in.Body
	var violations []string
	if in.Template == "" {
		// No template: just use provided body or create simple header
		if body == "" {
			body = "# " + in.Title + "\n"
		}
	} else {
		// Template provided: load and validate
		templatesDirPath, err := c.templatesDir()
		if err != nil {
			return Knowledge{}, err
		}
		tmpl, err := loadTemplate(templatesDirPath, in.Template)
		if err != nil {
			return Knowledge{}, err
		}
		// The same fields the edit path checks (template.go's templateProblems
		// via frontmatterFields), not just sources/summary/--set: a required
		// or choices rule on title, tags, labels, board or provenance must
		// hold at creation too, not only from the next edit onward. The board
		// field uses the name the caller typed; it is only resolved to a row
		// (and rejected if it does not exist) once creation itself proceeds.
		checkFM := Frontmatter{
			Title: in.Title, Summary: in.Summary, Provenance: provenance,
			Board: in.Board, Tags: in.Tags, Labels: in.Labels, Sources: cleanSources(in.Sources),
		}
		if len(in.Set) > 0 {
			checkFM.Extra = make(map[string]any, len(in.Set))
			for k, v := range in.Set {
				checkFM.Extra[k] = v
			}
		}
		fields := frontmatterFields(checkFM)
		checkSections := body != ""
		if body == "" {
			body = stripOptionalMarkers(renderTemplateBody(tmpl.Body, in.Title, in.Set))
		}
		// A read transaction of its own, so a reject template writes nothing.
		if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
			var err error
			violations, err = c.templateProblems(tx, projectID, tmpl, fields, body, checkSections)
			return err
		}); err != nil {
			return Knowledge{}, err
		}
		if err := enforceTemplate(tmpl, violations); err != nil {
			return Knowledge{}, err
		}
	}
	if err := c.checkWrite(ctx, ProposedWrite{
		Op: "doc.write", EntityType: "entry", ProjectID: projectID,
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
			Title: in.Title, Template: in.Template, Summary: in.Summary,
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
		path := c.docPath(key, false, slug)
		// A revision directory can outlive the entry it belonged to when the
		// file and row are removed outside Trellis -- exactly what
		// `maintenance prune --orphan-history` exists for. Adopting it here
		// would hand this new entry someone else's history, so refuse
		// instead: run the prune first.
		if _, err := os.Stat(revisionDir(path)); err == nil {
			return ErrConflict("stale_history",
				"a revision history for "+slug+" already exists with no entry using it",
				"trellis maintenance prune --orphan-history")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := atomicfile.Write(path, []byte(raw), false); err != nil {
			return err
		}
		writtenPath = path
		st, err := os.Stat(path)
		if err != nil {
			return err
		}

		doc = Knowledge{
			ID: NewCardID(), ProjectID: projectID, BoardID: boardID, Slug: slug,
			Title: in.Title, Path: path, Template: fm.Template, Summary: in.Summary,
			Provenance: provenance,
			Private:    in.Private,
			Sources:    cleanSources(in.Sources),
			BodyMD:     body, ContentHash: ContentHash(raw), MTime: st.ModTime().UnixMilli(),
			Size: st.Size(), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		doc.Fields = extraToFields(fm.Extra)
		if err := insertKnowledge(tx, doc); err != nil {
			return err
		}
		if _, err := c.captureKnowledgeRevision(doc.Path, doc.Version, []byte(raw)); err != nil {
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
		if err := c.recordEvent(tx, "entry", doc.ID, "created", "", "", doc.Title); err != nil {
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
			_ = atomicfile.SyncDir(filepath.Dir(writtenPath))
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
		`INSERT INTO entry (id, project_id, board_id, slug, title, template, summary,
		                        provenance, private, content_hash, mtime, size, global, version,
		                        created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.ProjectID, d.BoardID, d.Slug, d.Title, d.Template, d.Summary,
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
			`SELECT COUNT(*) FROM entry WHERE project_id = ? AND slug = ?`, projectID, slug); err != nil {
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
	var exactSlug string
	if projectID == "" || d.scope == docVault {
		exactSlug, err = c.resolveSlug(tx, "", d.slug, true)
	} else if d.scope == docOwn {
		exactSlug, err = c.resolveSlug(tx, projectID, d.slug, false)
	} else {
		exactSlug, err = c.resolveSlug(tx, projectID, d.slug, true)
	}
	if err != nil {
		return err
	}

	if projectID == "" || d.scope == docVault {
		err = tx.Get(out, `SELECT * FROM entry WHERE slug = ? AND global = 1`, exactSlug)
	} else if d.scope == docOwn {
		err = tx.Get(out, `SELECT * FROM entry WHERE slug = ? AND project_id = ? AND global = 0`, exactSlug, projectID)
	} else {
		err = tx.Get(out, `SELECT * FROM entry WHERE slug = ? AND (project_id = ? OR global = 1) ORDER BY global LIMIT 1`, exactSlug, projectID)
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

// extraToFields converts Frontmatter.Extra to Knowledge.Fields.
// Each Extra value becomes a string or []string (for slices).
// Nil values are dropped. An empty Extra becomes an empty but non-nil map.
func extraToFields(extra map[string]any) map[string]any {
	fields := make(map[string]any)
	for k, v := range extra {
		if v == nil {
			continue
		}
		// Handle slices by converting each item with fmt.Sprint
		switch val := v.(type) {
		case []any:
			items := make([]string, 0, len(val))
			for _, item := range val {
				if item != nil {
					items = append(items, fmt.Sprint(item))
				}
			}
			if len(items) > 0 {
				fields[k] = items
			}
		default:
			fields[k] = fmt.Sprint(v)
		}
	}
	return fields
}

// refreshFromFile re-reads the file when mtime or size moved. This is the whole
// of the "stat sweep": a stat is microseconds, so it runs on every read rather
// than on a schedule, and an edit in Obsidian is visible to the next command.
func (c *Core) refreshFromFile(tx *sqlx.Tx, doc *Knowledge) (err error) {
	// The capture below writes the retained copy of this version before the
	// row commits it. If a later step in this function fails, that copy must
	// not survive: kept, it would be skipped as "already retained" the next
	// time this version is really reached, and the content that actually
	// belongs at that version would never be captured.
	var revisionDest string
	defer func() {
		if err != nil && revisionDest != "" {
			if derr := discardCapturedRevision(revisionDest); derr != nil {
				err = errors.Join(err, derr)
			}
		}
	}()
	key, err := c.keyOfDoc(tx, doc)
	if err != nil {
		return err
	}
	doc.Path = c.docPath(key, doc.Global, doc.Slug)
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
	// Unlike Title, an empty template is meaningful: it means "no template",
	// not "keep whatever the row had". cmpOr here would make removing the
	// key by hand a no-op, so ls --template, the Templates recall filter and
	// Knowledge.Template would all keep reporting a template the file no
	// longer names.
	doc.Template = fm.Template
	doc.Summary = fm.Summary
	doc.Provenance = fm.Provenance
	doc.Sources = fm.Sources
	doc.Fields = extraToFields(fm.Extra)
	// The flag is compared separately because the content hash cannot see it:
	// a database restored from an older backup, or a file that already carried
	// the key when the column was added, has an unchanged file and a wrong row.
	becamePrivate := fm.Private && !doc.Private
	privateDrifted := doc.Private != fm.Private
	doc.Private = fm.Private
	doc.BodyMD = body
	oldHash := doc.ContentHash
	doc.ContentHash = ContentHash(string(raw))
	statMoved := st.ModTime().UnixMilli() != doc.MTime || st.Size() != doc.Size
	contentChanged := oldHash != doc.ContentHash
	changed := contentChanged || privateDrifted
	doc.MTime = st.ModTime().UnixMilli()
	doc.Size = st.Size()
	doc.UpdatedAt = c.clock.NowMS()
	if !changed {
		// Same bytes under a new mtime — a touch, or a write that was undone.
		// That is not a new version: bumping it would hand every holder of the
		// current version a false conflict. Record the stat and stop.
		if statMoved {
			if _, err := tx.Exec(`UPDATE entry SET mtime = ?, size = ? WHERE id = ?`,
				doc.MTime, doc.Size, doc.ID); err != nil {
				return err
			}
		}
		return nil
	}
	doc.Version++

	if _, err := tx.Exec(
		`UPDATE entry SET title = ?, template = ?, summary = ?, provenance = ?, private = ?,
		                      content_hash = ?, mtime = ?, size = ?, version = ?,
		                      updated_at = ? WHERE id = ?`,
		doc.Title, doc.Template, doc.Summary, doc.Provenance, doc.Private, doc.ContentHash,
		doc.MTime, doc.Size, doc.Version, doc.UpdatedAt, doc.ID); err != nil {
		return err
	}
	if contentChanged {
		revisionDest, err = c.captureKnowledgeRevision(doc.Path, doc.Version, raw)
		if err != nil {
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
	return c.recordEvent(tx, "entry", doc.ID, "reloaded", "", "", "external edit")
}

// docView fills the computed fields.
func (c *Core) docView(tx *sqlx.Tx, doc *Knowledge) error {
	key, err := c.keyOfDoc(tx, doc)
	if err != nil {
		return err
	}
	doc.Ref = DocAddress(key, doc.Global, doc.Slug)
	doc.Path = c.docPath(key, doc.Global, doc.Slug)
	doc.BoardName = ""
	if doc.BoardID != nil {
		if err := tx.Get(&doc.BoardName, `SELECT name FROM board WHERE id = ?`, *doc.BoardID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	doc.Tags = []string{}
	if err := tx.Select(&doc.Tags,
		`SELECT t.name FROM tag t JOIN entry_tag kt ON kt.tag_id = t.id WHERE kt.entry_id = ? ORDER BY t.name`,
		doc.ID); err != nil {
		return err
	}
	doc.Labels = []string{}
	if err := tx.Select(&doc.Labels,
		`SELECT l.name FROM label l JOIN entry_label kl ON kl.label_id = l.id WHERE kl.entry_id = ? ORDER BY l.name`,
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
		 WHERE l.from_type = 'entry' AND l.from_id = ? AND l.rel = 'artifact'
		 ORDER BY l.rowid`, doc.ID)
}

// ListKnowledge returns the selected board's entries plus the unscoped ones
// (§10.1): a board is a lens, so narrowing by one never hides project-wide
// knowledge. An empty boardID lists the whole project.
// KnowledgeFilter narrows a listing. A zero value lists everything the project
// can see, which is what almost every caller wants.
type KnowledgeFilter struct {
	BoardID     string   // association only; entries with no board always match
	Templates   []string // template values to keep; empty keeps all
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
	for _, col := range []string{"template", "provenance"} {
		var values []string
		switch col {
		case "template":
			values = f.Templates
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
			`id IN (SELECT kt.entry_id FROM entry_tag kt JOIN tag t ON t.id = kt.tag_id
			        WHERE t.name IN (?`+strings.Repeat(", ?", len(f.Tags)-1)+`)
			        GROUP BY kt.entry_id HAVING COUNT(DISTINCT t.name) = ?)`)
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
		if err := tx.Select(&docs, `SELECT * FROM entry WHERE `+where+
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

// ListGlobalKnowledge lists the global vault, each entry refreshed from its
// file first: a caller that withholds private content must read the file's
// flag, never a mirror that a hand edit has not reached yet.
func (c *Core) ListGlobalKnowledge(ctx context.Context) ([]Knowledge, error) {
	docs := []Knowledge{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Select(&docs, `SELECT * FROM entry WHERE global = 1 ORDER BY updated_at DESC`); err != nil {
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
	var revisionDest string
	var done bool
	var templateWarnings []string
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
			if !done && revisionDest != "" {
				// The new version was captured on the assumption this write
				// would land. It did not: that capture must go too, or the
				// real version reached later will find it "already retained"
				// and skip capturing it.
				if derr := discardCapturedRevision(revisionDest); derr != nil {
					err = errors.Join(err, derr)
				}
				revisionDest = ""
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
			Op: "doc.write", EntityType: "entry", EntityID: doc.ID, ProjectID: projectID,
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
		if _, err := c.captureKnowledgeRevision(doc.Path, doc.Version, oldRaw); err != nil {
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
		if in.Template != nil {
			fm.Template = *in.Template
		}
		if in.Private != nil {
			fm.Private = *in.Private
		}
		if in.Tags != nil {
			fm.Tags = *in.Tags
		}
		if in.Labels != nil {
			fm.Labels = *in.Labels
		}
		if in.Artifacts != nil {
			fm.Artifacts = dedupeNames(*in.Artifacts)
		}
		if in.Sources != nil {
			fm.Sources = cleanSources(*in.Sources)
		}
		for _, name := range slices.Sorted(maps.Keys(in.Set)) {
			if flag, reserved := reservedFrontmatterFields[name]; reserved {
				return ErrUsage("reserved_field",
					`"`+name+`" is a built-in frontmatter field and cannot be set with --set`,
					"use "+flag+" instead")
			}
			if in.Set[name] == "" {
				delete(fm.Extra, name)
				continue
			}
			if fm.Extra == nil {
				fm.Extra = map[string]any{}
			}
			fm.Extra[name] = in.Set[name]
		}
		// The result must satisfy its template, the one it keeps or the one
		// this edit switches to. A template that no longer exists is only
		// an error when this edit names it; otherwise the entry stays
		// editable and lint reports it.
		if fm.Template != "" {
			tmpl, err := c.templateNamed(fm.Template)
			switch {
			case err == nil:
				problems, err := c.templateProblems(tx, projectID, tmpl, frontmatterFields(fm), body, true)
				if err != nil {
					return err
				}
				if err := enforceTemplate(tmpl, problems); err != nil {
					return err
				}
				templateWarnings = problems
			case in.Template == nil && (isCode(err, "unknown_template") || isCode(err, "bad_template_name")):
				templateWarnings = []string{"no template " + fm.Template + "; its rules were not checked"}
			default:
				return err
			}
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
		doc.Fields = extraToFields(fm.Extra)
		doc.Template = fm.Template
		// Handle private false→true transition: purge disclosed copies in the same transaction
		oldPrivate := doc.Private
		doc.Private = fm.Private
		doc.MTime = st.ModTime().UnixMilli()
		doc.Size = st.Size()
		doc.Version++
		doc.UpdatedAt = now
		if _, err := tx.Exec(
			`UPDATE entry SET title = ?, summary = ?, content_hash = ?, mtime = ?, size = ?,
			                      version = ?, updated_at = ?, template = ?, private = ? WHERE id = ?`,
			doc.Title, doc.Summary, doc.ContentHash, doc.MTime, doc.Size, doc.Version, doc.UpdatedAt, doc.Template, doc.Private, doc.ID); err != nil {
			return err
		}
		// Purge disclosed copies if changing from public to private
		if !oldPrivate && doc.Private {
			if err := c.purgeDisclosedCopies(tx, &doc); err != nil {
				return err
			}
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
			// bodies is a copy of them. However, titles are disclosed by design
			// (like in created/deleted events), so keep them.
			value := fields[field]
			if doc.Private && field != "title" {
				value = ""
			}
			if err := c.recordEvent(tx, "entry", doc.ID, "edited", field, "", value); err != nil {
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
				if err := c.recordEvent(tx, "entry", doc.ID, "artifact_linked", "", "", value); err != nil {
					return err
				}
			}
			for _, name := range namesAdded(fm.Artifacts, before) {
				value := name
				if doc.Private {
					value = ""
				}
				if err := c.recordEvent(tx, "entry", doc.ID, "artifact_unlinked", "", "", value); err != nil {
					return err
				}
			}
		}
		revisionDest, err = c.captureKnowledgeRevision(doc.Path, doc.Version, []byte(out))
		if err != nil {
			return err
		}
		if err := c.docView(tx, &doc); err != nil {
			return err
		}
		if len(templateWarnings) > 0 {
			doc.Warnings = templateWarnings
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
		qerr := c.db.Get(&landed, `SELECT content_hash FROM entry WHERE id = ?`, doc.ID)
		if writeLanded(landed == written, qerr) {
			err = nil
		} else {
			if uerr := undoWrite(doc.Path, oldRaw, written); uerr != nil {
				err = errors.Join(err, uerr)
			}
			if derr := discardCapturedRevision(revisionDest); derr != nil {
				err = errors.Join(err, derr)
			}
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
		key, err := projectKeyOf(tx, projectID)
		if err != nil {
			return err
		}
		d, err := readDocArg(slug, key)
		if err != nil {
			return err
		}
		exactSlug, rerr := c.resolveSlug(tx, projectID, d.slug, d.scope == docVault)
		if rerr != nil {
			return rerr
		}
		q := `SELECT * FROM entry WHERE project_id = ? AND slug = ?`
		switch d.scope {
		case docOwn:
			q += ` AND global = 0`
		case docVault:
			q += ` AND global = 1`
		}
		if err := tx.Get(&doc, q, projectID, exactSlug); err != nil {
			if err.Error() == "sql: no rows in result set" {
				return ErrNotFound("knowledge_not_found", "no knowledge entry "+slug+" owned by this project", "trellis knowledge ls")
			}
			return err
		}
		doc.Path = c.docPath(key, doc.Global, doc.Slug)
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
		if err := c.recordEvent(tx, "entry", doc.ID, "deleted", "", doc.Title, ""); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM entry WHERE id = ?`, doc.ID); err != nil {
			return err
		}
		if err := c.rebuildKnowledgeFTS(tx); err != nil {
			return err
		}
		// Inbound links survive as stubs rather than vanishing: a reference to
		// something deleted is a finding, not a silent no-op (§10.4).
		if _, err := tx.Exec(
			`UPDATE link SET to_id = NULL WHERE to_type = 'entry' AND to_id = ?`, doc.ID); err != nil {
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
		qerr := c.db.Get(&gone, `SELECT COUNT(*) FROM entry WHERE id = ?`, doc.ID)
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
		if _, err := tx.Exec("DELETE FROM entry_search_state"); err != nil {
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
		Slug    string `db:"slug"`
		Global  bool   `db:"global"`
		Key     string `db:"pkey"`
		Title   string `db:"title"`
		Summary string `db:"summary"`
		Stamp   int64  `db:"stamp"`
		Size    int64  `db:"size"`
	}
	if err := tx.Select(&docs, `SELECT k.rowid, k.slug, k.global, p.key AS pkey, k.title, k.summary,
 COALESCE(s.mtime, -1) AS stamp, COALESCE(s.size, -1) AS size
 FROM entry k JOIN project p ON p.id = k.project_id
 LEFT JOIN entry_search_state s ON s.rowid = k.rowid`); err != nil {
		return err
	}
	for _, d := range docs {
		path := c.docPath(d.Key, d.Global, d.Slug)
		st, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			if _, err := tx.Exec("DELETE FROM entry_fts WHERE rowid = ?", d.RowID); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM entry_search_state WHERE rowid = ?", d.RowID); err != nil {
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
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fm, body, err := splitDocFile(path, raw)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO entry_fts(rowid, title, summary, body_md) VALUES (?, ?, ?, ?)", d.RowID, cmpOr(fm.Title, d.Title), fm.Summary, body); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO entry_search_state(rowid, mtime, size) VALUES (?, ?, ?)", d.RowID, st.ModTime().UnixNano(), st.Size()); err != nil {
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
