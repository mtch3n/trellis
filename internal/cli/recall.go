package cli

import (
	"cmp"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newRecallCmd() *cobra.Command {
	var limit, terms int
	var exclude, docTypes, provenances []string
	var record bool

	cmd := &cobra.Command{
		Use:   "recall <text>",
		Short: "Surface knowledge and cards bearing on a passage of text",
		Long: `Surface knowledge and cards bearing on a passage of text.

search takes a query someone meant to type and matches it as one phrase. recall
takes free text — a prompt, an error, a paragraph — lifts the terms worth
searching out of it, and returns identifiers with the one line that decides
whether opening each is worth a turn. Bodies stay on disk.

Output is JSON whenever stdout is not a terminal, so a caller reads results and
stops when it is empty. Pass --exclude the refs it already holds.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				hits, err := app.Core.Recall(cmd.Context(), app.Project.ID, args[0], core.RecallOpts{
					Limit: limit, Terms: terms, Exclude: exclude,
					DocTypes: docTypes, Provenances: provenances, Record: record,
				})
				if err != nil {
					return err
				}
				return emitRecall(cmd, hits)
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 5, "maximum number of results")
	cmd.Flags().IntVar(&terms, "terms", 4, "maximum terms lifted from the text")
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "refs to omit, for a caller that already holds them")
	cmd.Flags().StringSliceVar(&docTypes, "type", nil, "only these doc types; drops cards from the result")
	cmd.Flags().StringSliceVar(&provenances, "provenance", nil, "only these ingestion paths; drops cards from the result")
	cmd.Flags().BoolVar(&record, "record", false, "note each hit as injected, so `knowledge uptake` can tell whether it was opened")
	return cmd
}

func emitRecall(cmd *cobra.Command, hits []core.RecallHit) error {
	return Emit(cmd, map[string]any{"results": hits}, func() string {
		if len(hits) == 0 {
			return "no results"
		}
		var b strings.Builder
		w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, h := range hits {
			fmt.Fprintf(w, "%s\t%s\t%s\n", h.Kind, h.Ref, cmp.Or(h.Recap, h.Title))
		}
		w.Flush()
		return strings.TrimRight(b.String(), "\n")
	})
}
