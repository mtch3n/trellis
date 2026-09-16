package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

// boardBrief holds the query results for the brief.
type boardBrief struct {
	yours  []cardInfo
	others []cardInfo
	counts map[string]int // column name -> count
	pins   []core.Pin
}

type cardInfo struct {
	Ref   string
	Title string
	Owner string
	Note  string
}

// newBoardShowCmd creates the board show subcommand.
func newBoardShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Display board state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			brief, _ := cmd.Flags().GetBool("brief")

			app, err := currentBoard()
			if err != nil {
				// No pin applies here, so Trellis is not in use in this
				// directory, and the SessionStart hook must stay silent.
				if ce, ok := errors.AsType[*core.Error](err); ok && ce.Code == "unresolved" {
					return nil
				}
				return err
			}
			defer app.db.Close()

			if brief {
				briefData, err := queryBrief(cmd.Context(), app)
				if err != nil {
					return err
				}
				text := formatBrief(briefData)
				fmt.Fprint(cmd.OutOrStdout(), text)
			} else {
				view, err := queryBoardView(cmd.Context(), app)
				if err != nil {
					return err
				}
				return Emit(cmd, view, func() string { return formatBoardView(view) })
			}
			return nil
		},
	}
	cmd.Flags().Bool("brief", false, "show as injection brief")
	return cmd
}

type boardView struct {
	Project string       `json:"project"`
	Board   string       `json:"board"`
	Slug    string       `json:"slug"`
	Columns []columnView `json:"columns"`
}

type columnView struct {
	Name  string      `json:"name"`
	Done  bool        `json:"done"`
	Cards []core.Card `json:"cards"`
}

func queryBoardView(ctx context.Context, app *appCtx) (boardView, error) {
	columns, err := app.Core.ListColumns(ctx, app.Board.ID)
	if err != nil {
		return boardView{}, err
	}

	view := boardView{
		Project: app.Project.Key,
		Board:   app.Board.Name,
		Slug:    app.Board.Slug,
		Columns: make([]columnView, 0, len(columns)),
	}
	for _, column := range columns {
		page, err := app.Core.ListCardsPage(ctx, core.CardScope{BoardID: app.Board.ID}, core.CardFilter{
			Column: column.Name,
			Limit:  -1,
		})
		if err != nil {
			return boardView{}, err
		}
		view.Columns = append(view.Columns, columnView{
			Name:  column.Name,
			Done:  column.IsDone,
			Cards: page.Cards,
		})
	}
	return view, nil
}

func formatBoardView(view boardView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s / %s\n", view.Project, view.Board)
	for _, column := range view.Columns {
		marker := ""
		if column.Done {
			marker = " (done)"
		}
		fmt.Fprintf(&b, "\n%s%s [%d]\n", column.Name, marker, len(column.Cards))
		if len(column.Cards) == 0 {
			b.WriteString("  —\n")
			continue
		}
		for _, card := range column.Cards {
			owner := ""
			if card.Owner != nil {
				owner = " · " + *card.Owner
			}
			fmt.Fprintf(&b, "  %-14s %-8s %s%s\n", card.Ref, card.PriorityName, card.Title, owner)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func queryBrief(ctx context.Context, app *appCtx) (*boardBrief, error) {
	brief := &boardBrief{
		yours:  []cardInfo{},
		others: []cardInfo{},
		counts: make(map[string]int),
	}

	// Get the actor from the environment, as set by the hook.
	actor := os.Getenv("TRELLIS_AGENT")

	now := time.Now().UnixMilli()

	// YOURS: cards where owner = :me AND lease_until > :now
	if actor != "" {
		var owned []struct {
			ID    string `db:"id"`
			Seq   int64  `db:"seq"`
			Title string `db:"title"`
			Note  string `db:"body_md"`
		}
		err := app.db.SelectContext(ctx, &owned,
			`SELECT c.id, c.seq, c.title,
			        COALESCE((SELECT body_md FROM note WHERE card_id = c.id ORDER BY created_at DESC LIMIT 1), '') AS body_md
			 FROM card c
			 JOIN column_ col ON col.id = c.column_id
			 WHERE c.project_id = ? AND c.owner = ? AND c.lease_until > ? AND c.archived_at IS NULL
			 ORDER BY col.position DESC, c.priority, c.rank`,
			app.Project.ID, actor, now)
		if err != nil {
			return nil, err
		}
		for _, o := range owned {
			brief.yours = append(brief.yours, cardInfo{
				Ref:   fmt.Sprintf("%s-%d", app.Project.Key, o.Seq),
				Title: o.Title,
				Note:  o.Note,
			})
		}
	}

	// OTHERS: cards where owner IS NOT NULL AND owner != :me AND is_done = 0
	var otherCards []struct {
		Seq   int64  `db:"seq"`
		Title string `db:"title"`
		Owner string `db:"owner"`
	}
	err := app.db.SelectContext(ctx, &otherCards,
		`SELECT c.seq, c.title, c.owner
		 FROM card c
		 JOIN column_ col ON col.id = c.column_id
		 WHERE c.project_id = ? AND c.owner IS NOT NULL AND c.owner != ? AND c.archived_at IS NULL AND col.is_done = 0
		 ORDER BY c.updated_at DESC LIMIT 5`,
		app.Project.ID, actor)
	if err != nil {
		return nil, err
	}
	for _, o := range otherCards {
		brief.others = append(brief.others, cardInfo{
			Ref:   fmt.Sprintf("%s-%d", app.Project.Key, o.Seq),
			Title: o.Title,
			Owner: o.Owner,
		})
	}

	// COUNTS: grouped count over columns
	type countRow struct {
		ColumnName string `db:"name"`
		Count      int    `db:"count"`
	}
	var counts []countRow
	err = app.db.SelectContext(ctx, &counts,
		`SELECT col.name, COUNT(*) as count
		 FROM card c
		 JOIN column_ col ON col.id = c.column_id
		 WHERE c.project_id = ? AND c.archived_at IS NULL
		 GROUP BY col.id
		 ORDER BY col.position`,
		app.Project.ID)
	if err != nil {
		return nil, err
	}
	for _, r := range counts {
		brief.counts[r.ColumnName] = r.Count
	}

	// PINNED: pin joined to knowledge, comparing recap_hash against
	// content_hash. Ordered most recently pinned first.
	// Read MaxInjectedPins + 1 to know if there are more; the brief renders
	// only MaxInjectedPins in full, then counts the rest.
	pins, err := app.Core.Pins(ctx, app.Project.ID, app.Board.ID, core.MaxInjectedPins+1)
	if err != nil {
		return nil, err
	}
	brief.pins = pins

	return brief, nil
}

func formatBrief(brief *boardBrief) string {
	var result strings.Builder

	// YOURS section: never trimmed
	if len(brief.yours) > 0 {
		result.WriteString("### yours\n")
		for _, card := range brief.yours {
			fmt.Fprintf(&result, "  %s: %s\n", card.Ref, card.Title)
			if card.Note != "" {
				note := card.Note
				if len(note) > 100 {
					note = note[:100] + "..."
				}
				fmt.Fprintf(&result, "    %s\n", note)
			}
		}
		result.WriteString("\n")
	}

	// OTHERS section: can trim but show some
	if len(brief.others) > 0 {
		result.WriteString("### others\n")
		for i, card := range brief.others {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&result, "  %s (%s): %s\n", card.Ref, card.Owner, card.Title)
		}
		result.WriteString("\n")
	}

	// COUNTS section
	if len(brief.counts) > 0 {
		result.WriteString("### counts\n")
		for name, count := range brief.counts {
			fmt.Fprintf(&result, "  %s: %d\n", name, count)
		}
		result.WriteString("\n")
	}

	// PINNED section: knowledge the agent wrote down for its future self.
	// Trimmed before YOURS and DO THIS, and each line carries the command that
	// expands it, because an agent that cannot act on a line ignores it (§10.5).
	if len(brief.pins) > 0 {
		result.WriteString("### pinned — read fully with: trellis knowledge show <slug>\n")
		for i, p := range brief.pins {
			if i >= core.MaxInjectedPins {
				fmt.Fprintf(&result, "  +%d more: trellis knowledge pins\n", len(brief.pins)-i)
				break
			}
			stale := ""
			if p.Stale {
				// A confidently wrong recap of an entry that has since changed
				// is worse than no recap.
				stale = "(stale) "
			}
			// A pin with no recap is a pointer, which is what a private
			// entry injects: the slug and the title, nothing from the body.
			recap := cmp.Or(p.Recap, p.Title)
			if len(recap) > 120 {
				recap = recap[:120] + "..."
			}
			fmt.Fprintf(&result, "  %-22s %s%s\n", p.Slug, stale, recap)
		}
		result.WriteString("\n")
	}

	// DO THIS section (commands cheatsheet): never trimmed
	result.WriteString("### do this\n")
	result.WriteString("  `card new --title \"...\"`      create work\n")
	result.WriteString("  `card next --claim`           claim next unblocked card\n")
	result.WriteString("  `card note <id> \"...\"`      log progress (renews lease)\n")
	result.WriteString("  `card move <id> <column>`     move to column\n")
	result.WriteString("  `knowledge new --title ...`   write down what you learned\n")

	return result.String()
}
