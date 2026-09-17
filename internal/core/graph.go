package core

import (
	"context"
	"strings"

	"github.com/jmoiron/sqlx"
)

// GraphNode is one entity in a neighbourhood.
type GraphNode struct {
	Type  string `db:"type" json:"type"` // card, entry, or artifact
	ID    string `db:"id" json:"id"`
	Ref   string `db:"ref" json:"ref"`
	Title string `db:"title" json:"title"`
	Depth int    `db:"depth" json:"depth"`
	Done  bool   `db:"done" json:"done,omitzero"` // cards only
}

// GraphEdge is one link traversed to reach a node.
type GraphEdge struct {
	FromID string `db:"from_id" json:"from"`
	ToID   string `db:"to_id" json:"to"`
	Rel    string `db:"rel" json:"rel"`
}

// Graph is the answer to "XPSCTL-12 is blocked — by what, and is that thing
// itself blocked?". Backlinks answer one hop; a chain is exactly what one hop
// cannot see (§10.12).
type Graph struct {
	Root  string      `json:"root"`
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// Traverse walks the link table from a starting entity. rels empty means every
// relation; reverse walks inbound edges, which is how "what breaks if I change
// this" gets answered.
//
// One recursive CTE, the same shape the cycle check uses. No precomputed
// closure: a few thousand edges cost microseconds, and a closure would have to
// be maintained on every write (§10.13).
func (c *Core) Traverse(ctx context.Context, startID string, depth int, rels []string, reverse bool) (Graph, error) {
	if depth <= 0 {
		depth = 2
	}
	g := Graph{Root: startID, Nodes: []GraphNode{}, Edges: []GraphEdge{}}

	near, far := "from_id", "to_id"
	if reverse {
		near, far = "to_id", "from_id"
	}
	relFilter := ""
	args := []any{startID, depth}
	if len(rels) > 0 {
		relFilter = " AND l.rel IN (?" + strings.Repeat(", ?", len(rels)-1) + ")"
		for _, r := range rels {
			args = append(args, r)
		}
	}

	q := `WITH RECURSIVE reach(id, depth) AS (
	        SELECT ?, 0
	        UNION
	        SELECT l.` + far + `, reach.depth + 1
	        FROM link l JOIN reach ON l.` + near + ` = reach.id
	        WHERE reach.depth < ? AND l.` + far + ` IS NOT NULL` + relFilter + `
	      )
	      SELECT id, MIN(depth) AS depth FROM reach GROUP BY id`

	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var reached []struct {
			ID    string `db:"id"`
			Depth int    `db:"depth"`
		}
		if err := tx.Select(&reached, q, args...); err != nil {
			return err
		}
		for _, r := range reached {
			node := GraphNode{ID: r.ID, Depth: r.Depth}
			var card Card
			if err := tx.Get(&card, `SELECT * FROM card WHERE id = ?`, r.ID); err == nil {
				var done bool
				if err := tx.Get(&done, `SELECT is_done FROM column_ WHERE id = ?`, card.ColumnID); err != nil {
					return err
				}
				node.Type, node.Ref, node.Title, node.Done = "card", card.Ref, card.Title, done
			} else {
				var entry Entry
				if err := tx.Get(&entry, `SELECT * FROM entry WHERE id = ?`, r.ID); err == nil {
					key := GlobalKey
					if !entry.Global {
						if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, entry.ProjectID); err != nil {
							return err
						}
					}
					node.Type, node.Ref, node.Title = "entry", EntryAddress(key, entry.Global, entry.Slug), entry.Title
				} else {
					var artifact Artifact
					if err := tx.Get(&artifact, `SELECT * FROM artifact WHERE id = ?`, r.ID); err != nil {
						continue // a stub target: an id that resolves to nothing
					}
					akey, err := projectKeyOf(tx, artifact.ProjectID)
					if err != nil {
						return err
					}
					node.Type, node.Ref, node.Title = "artifact", ArtifactAddress(akey, artifact.Name), artifact.Name
				}
			}
			g.Nodes = append(g.Nodes, node)
		}

		ids := make([]string, 0, len(g.Nodes))
		for _, n := range g.Nodes {
			ids = append(ids, n.ID)
		}
		if len(ids) == 0 {
			return nil
		}
		eq, eargs, err := sqlx.In(
			`SELECT from_id, to_id, rel FROM link
			 WHERE from_id IN (?) AND to_id IN (?) ORDER BY rel, from_id`, ids, ids)
		if err != nil {
			return err
		}
		return tx.Select(&g.Edges, eq, eargs...)
	})
	return g, err
}
