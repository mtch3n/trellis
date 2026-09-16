package cli

import (
	"fmt"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newArtifactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifact",
		Short: "Store files and attach them to cards or knowledge entries",
		Long: "An <artifact> argument is an artifact id or its name. Attaching one to a\n" +
			"knowledge entry adds its name to the entry's `artifacts` frontmatter.",
	}
	cmd.AddCommand(newArtifactAddCmd(), newArtifactLsCmd(), newArtifactLinkCmd(),
		newArtifactUnlinkCmd(), newArtifactRmCmd())
	return cmd
}

// oneTarget enforces the --card / --doc choice. required means exactly one;
// otherwise at most one.
func oneTarget(card, doc string, required bool, usage string) error {
	switch {
	case card != "" && doc != "":
		return core.ErrUsage("target_conflict", "pass --card or --doc, not both", usage)
	case required && card == "" && doc == "":
		return core.ErrUsage("missing_target", "pass --card <ref> or --doc <slug>", usage)
	}
	return nil
}

func addTargetFlags(cmd *cobra.Command, card, doc *string, verb string) {
	cmd.Flags().StringVar(card, "card", "", verb+" a card reference")
	cmd.Flags().StringVar(doc, "doc", "", verb+" a knowledge entry slug")
}

func newArtifactAddCmd() *cobra.Command {
	var card, doc string
	cmd := &cobra.Command{
		Use:   "add <file>",
		Short: "Copy an image, recording, PDF or other permitted file into Trellis",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneTarget(card, doc, false, "trellis artifact add <file> [--card <ref> | --doc <slug>]"); err != nil {
				return err
			}
			return withBoard(func(app *appCtx) error {
				artifact, err := app.Core.CreateArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				switch {
				case card != "":
					id, err := cardID(cmd, app, card)
					if err != nil {
						return err
					}
					if err := app.Core.LinkArtifactToCard(cmd.Context(), app.Project.ID, id, artifact.ID); err != nil {
						return err
					}
				case doc != "":
					if _, err := app.Core.LinkArtifactToDoc(cmd.Context(), app.Project.ID, doc, artifact.ID); err != nil {
						return err
					}
				}
				return Emit(cmd, artifact, func() string { return artifact.ID + "  " + artifact.Name })
			})
		},
	}
	addTargetFlags(cmd, &card, &doc, "attach to")
	return cmd
}

func newArtifactLinkCmd() *cobra.Command {
	var card, doc string
	cmd := &cobra.Command{
		Use:   "link <artifact>",
		Short: "Attach an existing artifact to a card or a knowledge entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			usage := "trellis artifact link <artifact> --card <ref> | --doc <slug>"
			if err := oneTarget(card, doc, true, usage); err != nil {
				return err
			}
			return withBoard(func(app *appCtx) error {
				a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				if doc != "" {
					entry, err := app.Core.LinkArtifactToDoc(cmd.Context(), app.Project.ID, doc, a.ID)
					if err != nil {
						return err
					}
					return Emit(cmd, map[string]any{"artifact": a.ID, "name": a.Name, "doc": entry.Slug},
						func() string { return entry.Slug + " -> " + a.Name })
				}
				id, err := cardID(cmd, app, card)
				if err != nil {
					return err
				}
				if err := app.Core.LinkArtifactToCard(cmd.Context(), app.Project.ID, id, a.ID); err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"artifact": a.ID, "name": a.Name, "card": card},
					func() string { return card + " -> " + a.Name })
			})
		},
	}
	addTargetFlags(cmd, &card, &doc, "attach to")
	return cmd
}

func newArtifactUnlinkCmd() *cobra.Command {
	var card, doc string
	cmd := &cobra.Command{
		Use:   "unlink <artifact>",
		Short: "Detach an artifact from a card or a knowledge entry",
		Long: "Detaching from an entry removes the name from its `artifacts` frontmatter.\n" +
			"A name whose artifact no longer exists can still be removed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			usage := "trellis artifact unlink <artifact> --card <ref> | --doc <slug>"
			if err := oneTarget(card, doc, true, usage); err != nil {
				return err
			}
			return withBoard(func(app *appCtx) error {
				if doc != "" {
					entry, err := app.Core.UnlinkArtifactFromDoc(cmd.Context(), app.Project.ID, doc, args[0])
					if err != nil {
						return err
					}
					return Emit(cmd, map[string]any{"artifact": args[0], "doc": entry.Slug},
						func() string { return entry.Slug + " -x- " + args[0] })
				}
				a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				id, err := cardID(cmd, app, card)
				if err != nil {
					return err
				}
				if err := app.Core.UnlinkArtifactFromCard(cmd.Context(), app.Project.ID, id, a.ID); err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"artifact": a.ID, "name": a.Name, "card": card},
					func() string { return card + " -x- " + a.Name })
			})
		},
	}
	addTargetFlags(cmd, &card, &doc, "detach from")
	return cmd
}

func newArtifactLsCmd() *cobra.Command {
	var card, doc string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List stored artifacts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := oneTarget(card, doc, false, "trellis artifact ls [--card <ref> | --doc <slug>]"); err != nil {
				return err
			}
			return withBoard(func(app *appCtx) error {
				var cardIDValue, docIDValue string
				switch {
				case card != "":
					id, err := cardID(cmd, app, card)
					if err != nil {
						return err
					}
					cardIDValue = id
				case doc != "":
					entry, err := app.Core.LoadKnowledge(cmd.Context(), app.Project.ID, doc)
					if err != nil {
						return err
					}
					docIDValue = entry.ID
				}
				items, err := app.Core.ListArtifacts(cmd.Context(), app.Project.ID, cardIDValue, docIDValue)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"artifacts": items}, func() string {
					var b strings.Builder
					for _, item := range items {
						fmt.Fprintf(&b, "%s  %s  %s\n", item.ID, item.Kind, item.Path)
					}
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
	addTargetFlags(cmd, &card, &doc, "only artifacts attached to")
	return cmd
}

func newArtifactRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <artifact>",
		Short: "Delete an artifact; entries that name it keep the name as a stub",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				if err := app.Core.DeleteArtifact(cmd.Context(), app.Project.ID, a.ID); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"deleted": a.ID, "name": a.Name},
					func() string { return "deleted " + a.Name })
			})
		},
	}
}
