package platform

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"uuid"

	"github.com/jmoiron/sqlx"
)

const (
	recallDefaultLimit = 5
	recallDefaultTerms = 4
	recallOverFetch    = 4
	recallFetchFloor   = 60
)

type RecallOpts struct {
	Limit       int
	Terms       int
	Exclude     []string
	Templates   []string
	Provenances []string
	MaxBytes    int
}

type RecallHit struct {
	EntryHit
	Recap string `db:"recap" json:"recap,omitempty"`
	Line  string `db:"-" json:"line"`
}

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

func recallTerms(text string, limit int) []string {
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
	terms := slices.SortedFunc(maps.Keys(best), func(a, b string) int {
		return cmp.Or(cmp.Compare(best[b], best[a]), cmp.Compare(a, b))
	})
	return terms[:min(limit, len(terms))]
}

func recallLine(h RecallHit) string {
	text := strings.Join(strings.Fields(cmp.Or(h.Recap, h.Title)), " ")
	line := fmt.Sprintf("%-9.9s  %-34s  %.96s", "entry", h.Slug, text)
	return strings.TrimRightFunc(line, unicode.IsSpace)
}

func (p *Platform) Recall(ctx context.Context, pr Principal, project, text string, o RecallOpts) ([]RecallHit, error) {
	limit := cmp.Or(o.Limit, recallDefaultLimit)
	terms := recallTerms(text, cmp.Or(o.Terms, recallDefaultTerms))
	hits := []RecallHit{}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil || len(terms) == 0 {
			return err
		}
		args := append(visibleEntryArgs(pr, proj), o.Templates, o.Provenances, max(limit*recallOverFetch, recallFetchFloor))
		parts := make([]string, 0, len(terms))
		for _, t := range terms {
			args = append(args, t)
			parts = append(parts, "plainto_tsquery('english', $"+strconv.Itoa(len(args))+")")
		}
		match := strings.Join(parts, " || ")
		var candidates []RecallHit
		if err := tx.Select(&candidates, `SELECT `+entryHitColumns+`, COALESCE(NULLIF(e.recap, ''), e.summary) AS recap
			FROM entry e JOIN member m ON m.id = e.owner_id
			WHERE `+visibleEntrySQL+`
			  AND NOT e.private
			  AND NOT (e.scope = 'global' AND e.verify_by < now())
			  AND (COALESCE(cardinality($5::text[]), 0) = 0 OR e.template = ANY ($5))
			  AND (COALESCE(cardinality($6::text[]), 0) = 0 OR e.provenance = ANY ($6))
			  AND e.search @@ (`+match+`)
			ORDER BY ts_rank(e.search, `+match+`) DESC, e.updated_at DESC
			LIMIT $7`, args...); err != nil {
			return err
		}
		boost, err := recallLinkBoosts(tx, candidates)
		if err != nil {
			return err
		}
		rankRecall(candidates, boost)
		used := 0
		for _, h := range candidates {
			if len(hits) == limit {
				break
			}
			if slices.Contains(o.Exclude, h.Slug) {
				continue
			}
			h.Line = recallLine(h)
			size := len(h.Line) + 1
			if o.MaxBytes > 0 && size > o.MaxBytes {
				continue
			}
			if o.MaxBytes > 0 && used+size > o.MaxBytes {
				break
			}
			used += size
			hits = append(hits, h)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func rankRecall(hits []RecallHit, boost map[uuid.UUID]float64) {
	score := make(map[uuid.UUID]float64, len(hits))
	for position, h := range hits {
		score[h.ID] = 1/(rrfK+float64(position)+1) + boost[h.ID]
	}
	slices.SortStableFunc(hits, func(a, b RecallHit) int {
		return cmp.Compare(score[b.ID], score[a.ID])
	})
}

func recallLinkBoosts(tx *sqlx.Tx, hits []RecallHit) (map[uuid.UUID]float64, error) {
	boost := map[uuid.UUID]float64{}
	if len(hits) < 2 {
		return boost, nil
	}
	ids := make([]uuid.UUID, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	var edges []struct {
		From   uuid.UUID `db:"from_entry_id"`
		To     uuid.UUID `db:"to_entry_id"`
		Degree int       `db:"degree"`
	}
	if err := tx.Select(&edges, `SELECT l.from_entry_id, l.to_entry_id,
			(SELECT count(*) FROM entry_link d WHERE d.rel = 'wikilink' AND d.to_entry_id = l.to_entry_id) AS degree
		FROM entry_link l
		WHERE l.rel = 'wikilink' AND l.from_entry_id = ANY ($1) AND l.to_entry_id = ANY ($1)`, ids); err != nil {
		return nil, err
	}
	for _, e := range edges {
		w := 1 / (1 + float64(e.Degree))
		boost[e.From] += w
		boost[e.To] += w
	}
	for id, b := range boost {
		boost[id] = min(b, 1) / rrfK
	}
	return boost, nil
}
