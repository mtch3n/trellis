package platform

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"uuid"

	"github.com/jmoiron/sqlx"
)

const (
	MethodFTS    = "fts"
	MethodVector = "vector"
	MethodHybrid = "hybrid"
)

const (
	searchDefaultLimit = 50
	rrfK               = 60.0
)

type SearchOpts struct {
	Limit  int
	Method string
	Label  string
}

type EntryHit struct {
	ID       uuid.UUID `db:"id" json:"-"`
	Slug     string    `db:"slug" json:"slug"`
	Title    string    `db:"title" json:"title"`
	Scope    string    `db:"scope" json:"scope"`
	Owner    string    `db:"owner" json:"owner"`
	Template string    `db:"template" json:"template,omitempty"`
	Summary  string    `db:"summary" json:"summary,omitempty"`
	Private  bool      `db:"private" json:"private,omitzero"`
}

const entryHitColumns = `e.id, e.slug, e.title, e.scope, m.handle AS owner, e.template, e.summary, e.private`

const visibleEntrySQL = `e.deleted_at IS NULL AND (
	(e.scope = 'global' AND e.org_id = $2)
	OR (e.project_id = $1 AND (e.scope = 'project' OR e.owner_id = $3 OR $4)))`

func visibleEntryArgs(pr Principal, proj projectAccess) []any {
	return []any{proj.ID, proj.OrgID, pr.MemberID, pr.IsSystem()}
}

func (p *Platform) Search(ctx context.Context, pr Principal, project, query string, o SearchOpts) ([]EntryHit, error) {
	method := cmp.Or(o.Method, MethodHybrid)
	if !slices.Contains([]string{MethodFTS, MethodVector, MethodHybrid}, method) {
		return nil, ErrUsage("bad_method", "search method must be fts, vector or hybrid", "omit method for hybrid")
	}
	if p.embedder == nil {
		method = MethodFTS
	}
	limit := cmp.Or(o.Limit, searchDefaultLimit)
	var hits []EntryHit
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		var fts, vec []EntryHit
		if method != MethodVector {
			if fts, err = ftsEntries(tx, pr, proj, query, o.Label, limit); err != nil {
				return err
			}
		}
		if method != MethodFTS {
			vec, err = p.vectorEntries(ctx, tx, pr, proj, query, o.Label, limit)
			if err != nil && method == MethodVector {
				return err
			}
			if err != nil {
				slog.Warn("vector search failed; full-text hits only", "err", err)
			}
		}
		switch method {
		case MethodFTS:
			hits = fts
		case MethodVector:
			hits = vec
		default:
			hits = fuseHits(limit, fts, vec)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if hits == nil {
		hits = []EntryHit{}
	}
	return hits, nil
}

func ftsEntries(tx *sqlx.Tx, pr Principal, proj projectAccess, query, label string, limit int) ([]EntryHit, error) {
	var hits []EntryHit
	args := append(visibleEntryArgs(pr, proj), query, label, limit)
	err := tx.Select(&hits, `SELECT `+entryHitColumns+`
		FROM entry e JOIN member m ON m.id = e.owner_id
		WHERE `+visibleEntrySQL+`
		  AND e.search @@ websearch_to_tsquery('english', $5)
		  AND ($6 = '' OR $6 = ANY (e.labels))
		ORDER BY ts_rank(e.search, websearch_to_tsquery('english', $5)) DESC, e.updated_at DESC
		LIMIT $7`, args...)
	return hits, err
}

func (p *Platform) vectorEntries(ctx context.Context, tx *sqlx.Tx, pr Principal, proj projectAccess, query, label string, limit int) ([]EntryHit, error) {
	vec, err := p.embedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	var hits []EntryHit
	args := append(visibleEntryArgs(pr, proj), vec, p.embedder.Model(), label, limit)
	err = tx.Select(&hits, `SELECT `+entryHitColumns+`
		FROM (SELECT c.entry_id, min(c.embedding <=> $5::vector) AS dist
			FROM entry_chunk c JOIN entry e ON e.id = c.entry_id
			WHERE `+visibleEntrySQL+`
			  AND c.version = e.version AND c.model = $6
			  AND vector_dims(c.embedding) = vector_dims($5::vector)
			  AND ($7 = '' OR $7 = ANY (e.labels))
			GROUP BY c.entry_id) d
		JOIN entry e ON e.id = d.entry_id
		JOIN member m ON m.id = e.owner_id
		WHERE d.dist < 1
		ORDER BY d.dist, e.updated_at DESC
		LIMIT $8`, args...)
	return hits, err
}

func fuseHits(limit int, lists ...[]EntryHit) []EntryHit {
	score := map[uuid.UUID]float64{}
	byID := map[uuid.UUID]EntryHit{}
	var order []uuid.UUID
	for _, list := range lists {
		for rank, h := range list {
			if _, seen := byID[h.ID]; !seen {
				byID[h.ID] = h
				order = append(order, h.ID)
			}
			score[h.ID] += 1 / (rrfK + float64(rank+1))
		}
	}
	slices.SortStableFunc(order, func(a, b uuid.UUID) int { return cmp.Compare(score[b], score[a]) })
	out := make([]EntryHit, 0, min(limit, len(order)))
	for _, id := range order[:min(limit, len(order))] {
		out = append(out, byID[id])
	}
	return out
}
