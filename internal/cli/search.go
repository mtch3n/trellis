package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/daemon"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/retrieval"
	"github.com/spf13/cobra"
)

func newSearchCmd() *cobra.Command {
	var limit int
	var allProjects bool
	var label string
	var method string
	var useDaemon bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search cards and knowledge entries",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := core.SearchOpts{Limit: limit, AllProjects: allProjects, Label: label}
			opts.Method = method

			// --all-projects is discovery: it needs no project resolution and
			// works outside a repository. It returns pointers, not text.
			if allProjects {
				if useDaemon {
					return searchViaDaemon(cmd, args[0], "", opts, true, method)
				}
				c, db, err := openCore()
				if err != nil {
					return err
				}
				defer db.Close()
				hits, err := c.Search(cmd.Context(), "", args[0], opts)
				if err != nil {
					return err
				}
				return emitHits(cmd, hits, true)
			}

			return withBoard(func(app *appCtx) error {
				if useDaemon {
					return searchViaDaemon(cmd, args[0], app.Project.ID, opts, false, method)
				}
				cfg := app.cfg
				if method != "" {
					cfg.Search.Method = method
				}
				root, err := home.Root()
				if err != nil {
					return err
				}
				path := filepath.Join(root, "trellis.db")
				service := retrieval.NewService(app.Core, app.db, path, cfg, root)
				hits, err := service.Search(cmd.Context(), app.Project.ID, args[0], opts)
				if err != nil {
					return err
				}
				return emitHits(cmd, hits, false)
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum number of results")
	cmd.Flags().BoolVar(&allProjects, "all-projects", false, "discover across projects; returns pointers")
	cmd.Flags().StringVar(&label, "label", "", "only results carrying this label")
	cmd.Flags().StringVar(&method, "method", "", "search method: fts, vector, or hybrid")
	cmd.Flags().BoolVar(&useDaemon, "daemon", false, "send this search through the running daemon")
	return cmd
}

func searchViaDaemon(cmd *cobra.Command, query, projectID string, opts core.SearchOpts, showProject bool, method string) error {
	root, err := home.Root()
	if err != nil {
		return err
	}
	resp, err := daemon.Call(cmd.Context(), daemon.Endpoint(root), daemon.Request{Method: "search", Query: query, ProjectID: projectID, AllProjects: opts.AllProjects, Label: opts.Label, Limit: opts.Limit, SearchMethod: method})
	if err != nil {
		return err
	}
	return emitHits(cmd, resp.Results, showProject)
}

func emitHits(cmd *cobra.Command, hits []core.SearchHit, showProject bool) error {
	return Emit(cmd, map[string]any{"results": hits}, func() string {
		if len(hits) == 0 {
			return "no results"
		}
		var b strings.Builder
		w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, h := range hits {
			title := h.Title
			if h.Unreviewed {
				title += "  (unreviewed)"
			}
			if showProject {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", h.Project, h.Ref, h.Detail, title)
				continue
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", h.Ref, h.Detail, title)
		}
		w.Flush()
		return strings.TrimRight(b.String(), "\n")
	})
}
