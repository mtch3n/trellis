package cli

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newEventsCmd() *cobra.Command {
	var after int64
	var limit int
	var kinds, actions, templates []string
	var notActor, consumer string
	var allProjects, follow bool

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Read the event feed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEventsList(cmd, after, limit, kinds, actions, templates, notActor, consumer, allProjects, follow)
		},
	}
	cmd.Flags().Int64Var(&after, "after", 0, "only events after this seq")
	cmd.Flags().IntVar(&limit, "limit", 0, "row cap (default 1000, max 5000)")
	cmd.Flags().StringSliceVar(&kinds, "kind", nil, "card|entry|board|label|comment (repeatable)")
	cmd.Flags().StringSliceVar(&actions, "action", nil, "created, edited, moved, ... (repeatable; default: everything but read)")
	cmd.Flags().StringSliceVar(&templates, "template", nil, "knowledge templates (repeatable)")
	cmd.Flags().StringVar(&notActor, "not-actor", "", "skip events written by this actor")
	cmd.Flags().StringVar(&consumer, "consumer", "", "resume after this named consumer's cursor; creates it on first use")
	cmd.Flags().BoolVar(&allProjects, "all-projects", false, "every project, not just this one")
	cmd.Flags().BoolVar(&follow, "follow", false, "keep polling for new events, once a second")
	cmd.AddCommand(newEventsAckCmd(), newEventsConsumersCmd())
	return cmd
}

// runEventsList resolves the project (unless --all-projects), starts from
// --consumer's cursor when one is given (--after is ignored in that case: a
// consumer resumes from where it left off, unconditionally), reports a gap
// as its own JSON line before any event, and either prints one page or
// follows.
func runEventsList(cmd *cobra.Command, after int64, limit int, kinds, actions, templates []string,
	notActor, consumer string, allProjects, follow bool) error {
	var c *core.Core
	var db interface{ Close() error }
	var projectID string
	if allProjects {
		cc, dd, err := openCore()
		if err != nil {
			return err
		}
		c, db = cc, dd
	} else {
		pctx, err := currentProject()
		if err != nil {
			return err
		}
		c, db, projectID = pctx.Core, pctx.db, pctx.Project.ID
	}
	defer db.Close()

	if consumer != "" {
		ec, err := c.EnsureEventConsumer(cmd.Context(), consumer)
		if err != nil {
			return err
		}
		after = ec.Cursor
		gap, oldest, err := c.EventGapAfter(cmd.Context(), after)
		if err != nil {
			return err
		}
		if gap {
			if err := writeJSONLine(cmd, map[string]any{"gap": true, "oldest": oldest}); err != nil {
				return err
			}
		}
	}

	q := core.EventQuery{
		ProjectID: projectID, Limit: limit,
		Kinds: kinds, Actions: actions, Templates: templates, NotActor: notActor,
	}
	fetch := func(a int64) ([]core.FeedEvent, *int64, error) {
		q.After = a
		return c.EventFeed(cmd.Context(), q)
	}
	emit := func(ev core.FeedEvent) error { return writeJSONLine(cmd, ev) }

	if !follow {
		events, _, err := fetch(after)
		if err != nil {
			return err
		}
		for _, ev := range events {
			if err := emit(ev); err != nil {
				return err
			}
		}
		return nil
	}
	return runEventsFollow(cmd.Context(), time.Second, after, fetch, emit)
}

// runEventsFollow polls fetch every interval, starting after `after`, and
// calls emit for each event in seq order, advancing its local position from
// next each time. It stops silently when ctx is done. interval is a
// parameter, not a constant, so a test can drive many iterations without
// waiting on a real clock; the CLI passes a real time.Second.
func runEventsFollow(ctx context.Context, interval time.Duration, after int64,
	fetch func(after int64) ([]core.FeedEvent, *int64, error), emit func(core.FeedEvent) error) error {
	for {
		events, next, err := fetch(after)
		if err != nil {
			return err
		}
		for _, ev := range events {
			if err := emit(ev); err != nil {
				return err
			}
		}
		if next != nil {
			after = *next
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

func writeJSONLine(cmd *cobra.Command, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(append(b, '\n'))
	return err
}

func newEventsAckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ack NAME SEQ",
		Short: "Advance a consumer's cursor",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			seq, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return core.ErrUsage("invalid_seq", "seq must be an integer", "trellis events ack NAME 42")
			}
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			ec, err := c.AckEventConsumer(cmd.Context(), args[0], seq)
			if err != nil {
				return err
			}
			return Emit(cmd, ec, func() string { return fmt.Sprintf("%s -> %d", ec.Name, ec.Cursor) })
		},
	}
}

func newEventsConsumersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "consumers",
		Short: "List event consumers: name, cursor, lag, gap",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			list, err := c.ListEventConsumers(cmd.Context())
			if err != nil {
				return err
			}
			return Emit(cmd, list, func() string { return formatConsumerTable(list) })
		},
	}
	cmd.AddCommand(newEventsConsumersRmCmd())
	return cmd
}

func newEventsConsumersRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm NAME",
		Short: "Delete a named consumer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := c.DeleteEventConsumer(cmd.Context(), args[0]); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"removed": args[0]}, func() string { return "removed " + args[0] })
		},
	}
}

func formatConsumerTable(list []core.ConsumerStatus) string {
	if len(list) == 0 {
		return "(no consumers)"
	}
	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCURSOR\tLAG\tGAP")
	for _, s := range list {
		fmt.Fprintf(w, "%s\t%d\t%d\t%v\n", s.Name, s.Cursor, s.Lag, s.Gap)
	}
	w.Flush()
	return buf.String()
}
