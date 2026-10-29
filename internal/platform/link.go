package platform

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"strings"
	"uuid"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
)

type EntryLink struct {
	Rel     string     `json:"rel"`
	Target  string     `json:"target"`
	Stub    bool       `json:"stub"`
	EntryID *uuid.UUID `json:"entry_id,omitempty"`
	Slug    string     `json:"slug,omitempty"`
	Scope   string     `json:"scope,omitempty"`
	Owner   string     `json:"owner,omitempty"`
	Card    string     `json:"card,omitempty"`
}

const (
	relWikilink = "wikilink"
	relSource   = "source"
)

var wikiLinkRE = regexp.MustCompile(`\[\[([^\]|#]+)(#[^\]|]+)?(\|[^\]]+)?\]\]`)

func parseWikilinks(body string) []string {
	var out []string
	for _, m := range wikiLinkRE.FindAllStringSubmatch(fenceRE.ReplaceAllString(body, ""), -1) {
		target := strings.TrimSpace(m[1])
		if target != "" && !slices.Contains(out, target) {
			out = append(out, target)
		}
	}
	return out
}

func wikilinkTarget(s string) (string, bool) {
	s = strings.TrimSpace(s)
	inner, ok := strings.CutPrefix(s, "[[")
	if !ok {
		return "", false
	}
	inner, ok = strings.CutSuffix(inner, "]]")
	if !ok || inner == "" {
		return "", false
	}
	inner, _, _ = strings.Cut(inner, "|")
	inner, _, _ = strings.Cut(inner, "#")
	return strings.TrimSpace(inner), true
}

func cardRefTarget(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "/") {
		a, err := address.Parse(s)
		if err != nil || a.Collection != address.CollectionCards {
			return "", false
		}
		s = a.Name
	}
	return s, address.ValidCardRef(s)
}

type linkWriter struct {
	proj     projectAccess
	memberID uuid.UUID
}

func (w linkWriter) entry(tx *sqlx.Tx, slug string) (*uuid.UUID, error) {
	var id uuid.UUID
	err := tx.Get(&id, `SELECT id FROM entry WHERE slug = $1 AND deleted_at IS NULL
		AND ((scope = 'personal' AND project_id = $2 AND owner_id = $3)
		  OR (scope = 'project' AND project_id = $2)
		  OR (scope = 'global' AND org_id = $4))
		ORDER BY CASE scope WHEN 'personal' THEN 0 WHEN 'project' THEN 1 ELSE 2 END LIMIT 1`,
		slug, w.proj.ID, w.memberID, w.proj.OrgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &id, err
}

func (w linkWriter) card(tx *sqlx.Tx, ref string) (*uuid.UUID, error) {
	var id uuid.UUID
	err := tx.Get(&id, `SELECT c.id FROM card c JOIN board b ON b.id = c.board_id
		WHERE c.project_id = $1 AND c.ref = $2 AND (b.scope = 'project' OR b.owner_id = $3)`, w.proj.ID, ref, w.memberID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &id, err
}

func (w linkWriter) resolves(tx *sqlx.Tx, v string) (ok, isRef bool, err error) {
	if slug, is := wikilinkTarget(v); is {
		id, err := w.entry(tx, slug)
		return id != nil, true, err
	}
	if ref, is := cardRefTarget(v); is {
		id, err := w.card(tx, ref)
		return id != nil, true, err
	}
	return false, false, nil
}

func syncLinks(tx *sqlx.Tx, w linkWriter, entryID uuid.UUID, body string, sources []string) error {
	if _, err := tx.Exec(`DELETE FROM entry_link WHERE from_entry_id = $1`, entryID); err != nil {
		return err
	}
	insert := func(rel, target string, toEntry, toCard *uuid.UUID) error {
		_, err := tx.Exec(`INSERT INTO entry_link (from_entry_id, writer_id, rel, target, to_entry_id, to_card_id)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`, entryID, w.memberID, rel, target, toEntry, toCard)
		return err
	}
	for _, slug := range parseWikilinks(body) {
		id, err := w.entry(tx, slug)
		if err != nil {
			return err
		}
		if err := insert(relWikilink, slug, id, nil); err != nil {
			return err
		}
	}
	for _, src := range sources {
		if slug, ok := wikilinkTarget(src); ok {
			id, err := w.entry(tx, slug)
			if err != nil {
				return err
			}
			if err := insert(relSource, slug, id, nil); err != nil {
				return err
			}
		} else if ref, ok := cardRefTarget(src); ok {
			id, err := w.card(tx, ref)
			if err != nil {
				return err
			}
			if err := insert(relSource, ref, nil, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func fillStubs(tx *sqlx.Tx, e Entry) error {
	_, err := tx.Exec(`UPDATE entry_link l SET to_entry_id = $1 FROM entry f
		WHERE f.id = l.from_entry_id AND l.target = $2 AND l.to_entry_id IS NULL AND l.to_card_id IS NULL
		  AND (($3 = 'personal' AND f.project_id = $4 AND l.writer_id = $5)
		    OR ($3 = 'project' AND f.project_id = $4)
		    OR ($3 = 'global' AND f.org_id = $6))`,
		e.ID, e.Slug, e.Scope, e.ProjectID, e.OwnerID, e.OrgID)
	return err
}

func visibleLinks(tx *sqlx.Tx, pr Principal, proj projectAccess, entryID uuid.UUID) ([]EntryLink, error) {
	var rows []struct {
		Rel        string     `db:"rel"`
		Target     string     `db:"target"`
		EntryID    *uuid.UUID `db:"entry_id"`
		ProjectID  *uuid.UUID `db:"project_id"`
		OwnerID    *uuid.UUID `db:"owner_id"`
		Scope      *string    `db:"scope"`
		Slug       *string    `db:"slug"`
		Owner      *string    `db:"owner"`
		Gone       bool       `db:"gone"`
		Card       *string    `db:"card"`
		CardOwner  *uuid.UUID `db:"card_owner"`
		BoardScope *string    `db:"board_scope"`
		CardProj   *uuid.UUID `db:"card_project"`
	}
	if err := tx.Select(&rows, `SELECT l.rel, l.target, t.id AS entry_id, t.project_id, t.owner_id, t.scope, t.slug,
		tm.handle AS owner, COALESCE(t.deleted_at IS NOT NULL, false) AS gone,
		c.ref AS card, b.owner_id AS card_owner, b.scope AS board_scope, c.project_id AS card_project
		FROM entry_link l
		LEFT JOIN entry t ON t.id = l.to_entry_id LEFT JOIN member tm ON tm.id = t.owner_id
		LEFT JOIN card c ON c.id = l.to_card_id LEFT JOIN board b ON b.id = c.board_id
		WHERE l.from_entry_id = $1 ORDER BY l.rel DESC, l.target`, entryID); err != nil {
		return nil, err
	}
	links := []EntryLink{}
	for _, r := range rows {
		l := EntryLink{Rel: r.Rel, Target: r.Target, Stub: true}
		switch {
		case r.EntryID != nil && !r.Gone:
			t := Entry{OwnerID: *r.OwnerID, Scope: *r.Scope, ProjectID: *r.ProjectID}
			if t.Scope == ScopeGlobal || t.ProjectID == proj.ID {
				if proj.verdict(pr, t.target(), OpRead) == Allow {
					l = EntryLink{Rel: r.Rel, Target: r.Target, EntryID: r.EntryID, Slug: *r.Slug, Scope: *r.Scope, Owner: *r.Owner}
				}
			}
		case r.Card != nil && *r.CardProj == proj.ID:
			if proj.verdict(pr, Target{OwnerID: *r.CardOwner, Scope: *r.BoardScope}, OpRead) == Allow {
				l = EntryLink{Rel: r.Rel, Target: r.Target, Card: *r.Card}
			}
		}
		links = append(links, l)
	}
	return links, nil
}

func (p *Platform) LinkEntry(ctx context.Context, pr Principal, project, slug, cardRef string) error {
	return p.changeCardEntry(ctx, pr, project, slug, cardRef, "entry.linked", func(tx *sqlx.Tx, cardID, entryID uuid.UUID) error {
		_, err := tx.Exec(`INSERT INTO card_entry (card_id, entry_id, member_id, actor) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
			cardID, entryID, pr.MemberID, pr.Actor())
		return err
	})
}

func (p *Platform) UnlinkEntry(ctx context.Context, pr Principal, project, slug, cardRef string) error {
	return p.changeCardEntry(ctx, pr, project, slug, cardRef, "entry.unlinked", func(tx *sqlx.Tx, cardID, entryID uuid.UUID) error {
		_, err := tx.Exec(`DELETE FROM card_entry WHERE card_id = $1 AND entry_id = $2`, cardID, entryID)
		return err
	})
}

func (p *Platform) changeCardEntry(ctx context.Context, pr Principal, project, slug, cardRef, kind string,
	change func(tx *sqlx.Tx, cardID, entryID uuid.UUID) error) error {
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		c, err := loadCard(tx, pr, proj, cardRef, OpWrite, true)
		if err != nil {
			return err
		}
		e, err := findEntry(tx, pr, proj, slug, OpRead, false, false)
		if err != nil {
			return err
		}
		if err := change(tx, c.ID, e.ID); err != nil {
			return err
		}
		return recordEntryEvent(tx, pr, proj, e.ID, kind, map[string]any{"card": c.Ref})
	})
}
