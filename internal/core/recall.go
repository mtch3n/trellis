package core

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/jmoiron/sqlx"
)

// RecallHit is one recall result: an identifier plus the single line that
// decides whether opening it is worth a turn. The body stays on disk.
type RecallHit struct {
	Kind    string `db:"kind" json:"kind"` // card or knowledge
	Ref     string `db:"ref" json:"ref"`
	Title   string `db:"title" json:"title"`
	Project string `db:"project" json:"project"`
	Detail  string `db:"detail" json:"detail,omitempty"` // column for cards, type for entries
	Recap   string `db:"recap" json:"recap,omitempty"`

	// ID joins against the link graph. Callers get refs, not internal ids.
	ID string `db:"id" json:"-"`
}

// RecallOpts bounds a recall.
type RecallOpts struct {
	Limit   int      // hits returned; default 5
	Terms   int      // terms lifted from the text; default 4
	Exclude []string // refs the caller already holds, so they are not resent
}

const (
	// Rank positions are fused, not scores, matching ReciprocalRankFusion: a
	// BM25 score and a link count have no common unit, but their orderings do.
	recallRRFK = 60.0
	// Ranking has to see candidates the final limit would have cut, or it is
	// only reordering a list something else already decided.
	recallOverFetch  = 4
	recallFetchFloor = 60
)

// recallStop holds the prose function words free text is full of and a title is
// not. significantTerms carries a shorter list tuned for titles; widening that
// one would change duplicate detection, which is a different job on different
// input.
var recallStop = func() map[string]bool {
	stop := map[string]bool{}
	for w := range strings.FieldsSeq(`
		about after all also and any are because been before being but can
		could did does doing done for from get give had has have having her
		here him his how into its just like may might more most much must not
		now off once only other our out over own same she should some such
		than that the their them then there these they this those through too
		under until very was were what when where which while who why will
		with would you your
	`) {
		stop[w] = true
	}
	return stop
}()

// recallTerms lifts the words worth searching out of free text, strongest
// first. Identifiers and versioned names are rarer than prose, so each one
// narrows a search far more than an ordinary long word does.
func recallTerms(text string, limit int) []string {
	if limit <= 0 {
		limit = 4
	}
	best := map[string]int{}
	boundary := func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
			r != '_' && r != '.' && r != '-' && r != '/'
	}
	for field := range strings.FieldsFuncSeq(text, boundary) {
		key := strings.ToLower(strings.Trim(field, "._-/"))
		if len([]rune(key)) < 3 || recallStop[key] || !strings.ContainsFunc(key, unicode.IsLetter) {
			continue
		}
		score := len(key)
		if strings.ContainsAny(field, "_./-") || strings.ContainsFunc(field, unicode.IsDigit) {
			score += 10
		}
		if field == strings.ToUpper(field) {
			score += 3
		}
		best[key] = max(score, best[key])
	}
	terms := slices.Collect(maps.Keys(best))
	// Ties break alphabetically so the same text always yields the same query.
	slices.SortFunc(terms, func(a, b string) int {
		return cmp.Or(cmp.Compare(best[b], best[a]), cmp.Compare(a, b))
	})
	if len(terms) > limit {
		terms = terms[:limit]
	}
	return terms
}

// Recall finds knowledge entries and cards bearing on a passage of free text.
//
// Search quotes its caller's text into one phrase, which is right for someone
// who means the words they typed. A prompt is not that: matched as a phrase it
// finds nothing. Recall lifts terms out and ORs them into one expression, so
// what reaches MATCH is composed here rather than typed by a user (§10).
func (c *Core) Recall(ctx context.Context, projectID, text string, o RecallOpts) ([]RecallHit, error) {
	if o.Limit <= 0 {
		o.Limit = 5
	}
	terms := recallTerms(text, o.Terms)
	if len(terms) == 0 {
		return []RecallHit{}, nil
	}
	if err := c.SyncKnowledgeSearch(ctx); err != nil {
		return nil, err
	}
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		quoted = append(quoted, ftsPhrase(t))
	}
	match := strings.Join(quoted, " OR ")

	hits := []RecallHit{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		fetch := max(o.Limit*recallOverFetch, recallFetchFloor)

		// summary is the recap fallback: an entry that was never pinned still
		// has the line its author wrote to describe it.
		var docs []RecallHit
		if err := tx.Select(&docs, `
			SELECT 'knowledge' AS kind, k.id,
			       CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END || '/' || k.slug AS ref,
			       k.title,
			       CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END AS project,
			       k.doc_type AS detail,
			       COALESCE(NULLIF(k.recap, ''), k.summary) AS recap
			FROM knowledge k
			JOIN knowledge_fts ON knowledge_fts.rowid = k.rowid
			JOIN project p ON p.id = k.project_id
			WHERE (k.project_id = ? OR k.global = 1) AND knowledge_fts MATCH ?
			ORDER BY knowledge_fts.rank LIMIT ?`, projectID, match, fetch); err != nil {
			return err
		}
		var cards []RecallHit
		if err := tx.Select(&cards, `
			SELECT 'card' AS kind, c.id, p.key || '-' || c.seq AS ref, c.title, p.key AS project,
			       col.name AS detail, '' AS recap
			FROM card c
			JOIN card_fts ON card_fts.rowid = c.rowid
			JOIN project p ON p.id = c.project_id
			JOIN column_ col ON col.id = c.column_id
			WHERE c.project_id = ? AND c.archived_at IS NULL AND card_fts MATCH ?
			ORDER BY card_fts.rank LIMIT ?`, projectID, match, fetch); err != nil {
			return err
		}

		boost, err := recallLinkBoosts(tx, docs, cards)
		if err != nil {
			return err
		}
		rankRecall(docs, boost)
		rankRecall(cards, boost)

		// Knowledge outranks cards: recall exists to surface what was written
		// down, and open cards already reach the agent through the brief.
		for _, group := range [][]RecallHit{docs, cards} {
			for _, h := range group {
				if len(hits) == o.Limit {
					return nil
				}
				if !slices.Contains(o.Exclude, h.Ref) {
					hits = append(hits, h)
				}
			}
		}
		return nil
	})
	return hits, err
}

// rankRecall reorders one FTS result list by its rank position fused with the
// link boost, so a well-connected hit can rise past a marginally better textual
// match. Positions are fused rather than scores: a BM25 score and a link count
// share no unit, but their orderings do. Sorting is stable, so FTS order breaks
// ties and an unlinked vault behaves exactly as it did before.
func rankRecall(hits []RecallHit, boost map[string]float64) {
	if len(hits) < 2 {
		return
	}
	score := make(map[string]float64, len(hits))
	for position, h := range hits {
		score[h.ID] = 1/(recallRRFK+float64(position)+1) + boost[h.ID]
	}
	slices.SortStableFunc(hits, func(a, b RecallHit) int {
		return cmp.Compare(score[b.ID], score[a.ID])
	})
}

// recallLinkBoosts scores each candidate by how much of the rest of the result
// set it is connected to. A hit whose links point at other hits is central to
// what was asked about; one that stands alone is not.
//
// Each edge is weighted inversely to how linked its target already is, so an
// index everything points at adds almost nothing while a rarely cited entry
// adds a lot. No model is involved: this is the graph the author wrote.
func recallLinkBoosts(tx *sqlx.Tx, groups ...[]RecallHit) (map[string]float64, error) {
	boost := map[string]float64{}
	var ids []string
	for _, group := range groups {
		for _, h := range group {
			if h.ID != "" {
				ids = append(ids, h.ID)
			}
		}
	}
	if len(ids) < 2 {
		return boost, nil
	}

	type edge struct {
		From string `db:"from_id"`
		To   string `db:"to_id"`
	}
	query, args, err := sqlx.In(`
		SELECT from_id, to_id FROM link
		WHERE rel IN ('wikilink', 'documents') AND to_id IS NOT NULL
		  AND from_id IN (?) AND to_id IN (?)`, ids, ids)
	if err != nil {
		return nil, err
	}
	var edges []edge
	if err := tx.Select(&edges, tx.Rebind(query), args...); err != nil {
		return nil, err
	}
	if len(edges) == 0 {
		return boost, nil
	}

	targets := make([]string, 0, len(edges))
	for _, e := range edges {
		targets = append(targets, e.To)
	}
	type degree struct {
		To string `db:"to_id"`
		N  int    `db:"n"`
	}
	query, args, err = sqlx.In(`
		SELECT to_id, COUNT(*) AS n FROM link
		WHERE rel IN ('wikilink', 'documents') AND to_id IN (?)
		GROUP BY to_id`, targets)
	if err != nil {
		return nil, err
	}
	var degrees []degree
	if err := tx.Select(&degrees, tx.Rebind(query), args...); err != nil {
		return nil, err
	}
	linked := make(map[string]int, len(degrees))
	for _, d := range degrees {
		linked[d.To] = d.N
	}

	for _, e := range edges {
		// Both ends gain. An edge inside the result set says the two belong to
		// one answer, whichever direction the author happened to write it.
		w := 1 / (1 + float64(linked[e.To]))
		boost[e.From] += w
		boost[e.To] += w
	}
	// A fully connected hit may climb about as far as being ranked first, and
	// no further: the link graph advises the ordering, it does not replace it.
	for id, b := range boost {
		boost[id] = min(b, 1) / recallRRFK
	}
	return boost, nil
}
