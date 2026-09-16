package core

import (
	"context"
	"strings"

	"github.com/jmoiron/sqlx"
)

// SearchCards searches for cards in a project by full-text query.
// Results are ordered by FTS5 BM25 rank, with only non-archived cards included.
func (c *Core) SearchCards(ctx context.Context, projectID string, query string, limit int) ([]Card, error) {
	if limit <= 0 {
		limit = 50
	}
	var cards []Card
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Escape single quotes in the query for FTS5 MATCH
		escapedQuery := strings.ReplaceAll(query, "'", "''")

		rows, err := tx.Queryx(`
			SELECT c.* FROM card c
			JOIN card_fts ON card_fts.rowid = c.rowid
			WHERE c.project_id = ?
				AND c.archived_at IS NULL
				AND card_fts MATCH ?
			ORDER BY card_fts.rank
			LIMIT ?
		`, projectID, escapedQuery, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var card Card
			if err := rows.StructScan(&card); err != nil {
				return err
			}
			if err := c.cardView(tx, &card); err != nil {
				return err
			}
			cards = append(cards, card)
		}
		return rows.Err()
	})
	return cards, err
}

// FindSimilarOpenCards searches for open (non-archived, not in a done column)
// cards with titles matching the given query. Used for duplicate detection.
func (c *Core) FindSimilarOpenCards(ctx context.Context, projectID string, title string, limit int) ([]Card, error) {
	if limit <= 0 {
		limit = 10
	}
	var cards []Card
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Escape single quotes in the query for FTS5 MATCH
		escapedTitle := strings.ReplaceAll(title, "'", "''")

		rows, err := tx.Queryx(`
			SELECT c.* FROM card c
			JOIN card_fts ON card_fts.rowid = c.rowid
			JOIN column_ col ON col.id = c.column_id
			WHERE c.project_id = ?
				AND c.archived_at IS NULL
				AND col.is_done = 0
				AND card_fts MATCH ?
			ORDER BY card_fts.rank
			LIMIT ?
		`, projectID, escapedTitle, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var card Card
			if err := rows.StructScan(&card); err != nil {
				return err
			}
			if err := c.cardView(tx, &card); err != nil {
				return err
			}
			cards = append(cards, card)
		}
		return rows.Err()
	})
	return cards, err
}

// SearchHit is one result. Cross-project hits are pointers, never text:
// searching across projects is discovery, and reading the entry still requires
// being in that project or for it to have been escalated (§10.6.1).
type SearchHit struct {
	Kind       string `db:"kind" json:"kind"` // card or knowledge
	Ref        string `db:"ref" json:"ref"`
	Title      string `db:"title" json:"title"`
	Project    string `db:"project" json:"project"`
	Detail     string `db:"detail" json:"detail,omitempty"` // column for cards, type for entries
	Unreviewed bool   `db:"unreviewed" json:"unreviewed,omitempty"`
}

// SearchOpts narrows or widens a search.
type SearchOpts struct {
	Limit       int
	AllProjects bool // discovery across projects; returns pointers
	Label       string
	Method      string // optional per-request override: fts, vector, or hybrid
}

// Search runs one FTS query over cards and knowledge entries at once, so one
// described vocabulary covers both (§10).
func (c *Core) Search(ctx context.Context, projectID, query string, o SearchOpts) ([]SearchHit, error) {
	if o.Limit <= 0 {
		o.Limit = 50
	}
	if err := c.SyncKnowledgeSearch(ctx); err != nil {
		return nil, err
	}
	hits := []SearchHit{}
	// MATCH receives a parameter, so SQL quoting is unnecessary. FTS5 syntax
	// still treats punctuation such as '-' and words like OR as operators;
	// quote the user's ordinary text as one phrase and escape embedded quotes.
	match := ftsPhrase(query)
	now := c.clock.NowMS()

	scope, args := "c.project_id = ?", []any{projectID}
	docScope, docArgs := "(k.project_id = ? OR k.global = 1)", []any{projectID}
	if o.AllProjects {
		scope, args = "1 = 1", nil
		docScope, docArgs = "1 = 1", nil
	}

	labelJoin, docLabelJoin := "", ""
	if o.Label != "" {
		labelJoin = ` JOIN card_label cl ON cl.card_id = c.id
		              JOIN label lb ON lb.id = cl.label_id AND lb.name = ?`
		docLabelJoin = ` JOIN knowledge_label kl ON kl.doc_id = k.id
		                 JOIN label lb2 ON lb2.id = kl.label_id AND lb2.name = ?`
	}

	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Placeholder order follows the statement, not the struct: the label
		// JOIN is written before the WHERE clause.
		cardArgs := []any{}
		if o.Label != "" {
			cardArgs = append(cardArgs, o.Label)
		}
		cardArgs = append(cardArgs, args...)
		cardArgs = append(cardArgs, match, o.Limit)
		if err := tx.Select(&hits, `
			SELECT 'card' AS kind, p.key || '-' || c.seq AS ref, c.title, p.key AS project,
			       col.name AS detail, 0 AS unreviewed
			FROM card c
			JOIN card_fts ON card_fts.rowid = c.rowid
			JOIN project p ON p.id = c.project_id
			JOIN column_ col ON col.id = c.column_id`+labelJoin+`
			WHERE `+scope+` AND c.archived_at IS NULL AND card_fts MATCH ?
			ORDER BY card_fts.rank LIMIT ?`, cardArgs...); err != nil {
			return err
		}

		docQueryArgs := []any{now} // the unreviewed test in the SELECT clause
		if o.Label != "" {
			docQueryArgs = append(docQueryArgs, o.Label)
		}
		docQueryArgs = append(docQueryArgs, docArgs...)
		docQueryArgs = append(docQueryArgs, match, o.Limit)
		var docs []SearchHit
		if err := tx.Select(&docs, `
			SELECT 'knowledge' AS kind,
			       CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END || '/' || k.slug AS ref,
			       k.title,
			       CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END AS project,
			       k.doc_type AS detail,
			       (k.global = 1 AND k.review_by IS NOT NULL AND k.review_by < ?) AS unreviewed
			FROM knowledge k
			JOIN knowledge_fts ON knowledge_fts.rowid = k.rowid
			JOIN project p ON p.id = k.project_id`+docLabelJoin+`
			WHERE `+docScope+` AND knowledge_fts MATCH ?
			ORDER BY knowledge_fts.rank LIMIT ?`, docQueryArgs...); err != nil {
			return err
		}
		hits = append(hits, docs...)
		return nil
	})
	if len(hits) > o.Limit {
		hits = hits[:o.Limit]
	}
	return hits, err
}

// ftsPhrase quotes text so FTS5 reads it as literal words. Punctuation such as
// '-' and bare words like OR are operators to FTS5; a searcher typing them
// means them literally.
func ftsPhrase(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// matchKnowledge runs one FTS5 expression over the entries a project can see.
// The expression reaches MATCH untouched, so callers inside core may compose
// operators; Search quotes its caller's text into a phrase before calling in.
func (c *Core) matchKnowledge(ctx context.Context, projectID, match string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	if err := c.SyncKnowledgeSearch(ctx); err != nil {
		return nil, err
	}
	hits := []SearchHit{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&hits, `
			SELECT 'knowledge' AS kind,
			       CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END || '/' || k.slug AS ref,
			       k.title,
			       CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END AS project,
			       k.doc_type AS detail,
			       (k.global = 1 AND k.review_by IS NOT NULL AND k.review_by < ?) AS unreviewed
			FROM knowledge k
			JOIN knowledge_fts ON knowledge_fts.rowid = k.rowid
			JOIN project p ON p.id = k.project_id
			WHERE (k.project_id = ? OR k.global = 1) AND knowledge_fts MATCH ?
			ORDER BY knowledge_fts.rank LIMIT ?`,
			c.clock.NowMS(), projectID, match, limit)
	})
	return hits, err
}

// ListSearchKnowledge is the corpus every vector index build, count and prune
// reads. Private entries are dropped here rather than at each call site. Nothing
// enforces that a vector path reads its corpus from here: a new builder that
// queried the knowledge table directly would reintroduce them. Routing every
// one through this function is a convention, and a new one must keep it.
//
// The filter is applied after refreshFromFile and never in the SELECT. The
// private column is a mirror of the file and is stale for exactly one read
// after the file changes, which is the read that matters: filtering in SQL
// would still select a document just marked private, and would never again
// select one just un-marked.
func (c *Core) ListSearchKnowledge(ctx context.Context, projectID string) ([]Knowledge, error) {
	docs := []Knowledge{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var all []Knowledge
		if err := tx.Select(&all,
			`SELECT * FROM knowledge WHERE project_id = ? OR global = 1 ORDER BY updated_at DESC`,
			projectID); err != nil {
			return err
		}
		for i := range all {
			if err := c.refreshFromFile(tx, &all[i]); err != nil {
				return err
			}
			if err := c.docView(tx, &all[i]); err != nil {
				return err
			}
			if !all[i].Private {
				docs = append(docs, all[i])
			}
		}
		return nil
	})
	return docs, err
}
