package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newArtifactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifact",
		Short: "Store files and link them to cards or entries",
		Long: "An <artifact> argument is an artifact id or its name. Linking one to an\n" +
			"entry adds its name to the entry's `artifacts` frontmatter.",
	}
	cmd.AddCommand(newArtifactAddCmd(), newArtifactLsCmd(), newArtifactLinkCmd(),
		newArtifactUnlinkCmd(), newArtifactRmCmd())
	return cmd
}

// oneTarget enforces the --card / --entry choice. required means exactly one;
// otherwise at most one.
func oneTarget(card, entry string, required bool, usage string) error {
	switch {
	case card != "" && entry != "":
		return core.ErrUsage("target_conflict", "pass --card or --entry, not both", usage)
	case required && card == "" && entry == "":
		return core.ErrUsage("missing_target", "pass --card <card> or --entry <entry>", usage)
	}
	return nil
}

func addTargetFlags(cmd *cobra.Command, card, entry *string, verb string) {
	cmd.Flags().StringVar(card, "card", "", verb+" a card reference")
	cmd.Flags().StringVar(entry, "entry", "", verb+" an entry")
}

func newArtifactAddCmd() *cobra.Command {
	var card, entry string
	cmd := &cobra.Command{
		Use:   "add <file>",
		Short: "Copy an image, recording, PDF or other permitted file into Trellis",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneTarget(card, entry, false, "trellis artifact add <file> [--card <card> | --entry <entry>]"); err != nil {
				return err
			}
			return withTargets([]refArg{
				{Collection: address.CollectionCards, Value: card},
				{Collection: address.CollectionVault, Value: entry},
			}, func(app *appCtx, refs []string) error {
				// Resolve card ID before creating artifact so we fail early if the ref is bad
				var resolvedCardID string
				if refs[0] != "" {
					var err error
					resolvedCardID, err = cardID(cmd, app, refs[0])
					if err != nil {
						return err
					}
				}

				artifact, err := app.Core.CreateArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}

				if resolvedCardID != "" {
					if err := app.Core.LinkArtifactToCard(cmd.Context(), app.Project.ID, resolvedCardID, artifact.ID); err != nil {
						deleteErr := app.Core.DeleteArtifact(cmd.Context(), app.Project.ID, artifact.ID)
						return errors.Join(err, deleteErr)
					}
				} else if refs[1] != "" {
					if _, err := app.Core.LinkArtifactToEntry(cmd.Context(), app.Project.ID, refs[1], artifact.ID); err != nil {
						deleteErr := app.Core.DeleteArtifact(cmd.Context(), app.Project.ID, artifact.ID)
						return errors.Join(err, deleteErr)
					}
				}
				return Emit(cmd, artifact, func() string { return artifact.Ref + "  " + artifact.Path })
			})
		},
	}
	addTargetFlags(cmd, &card, &entry, "link to")
	return cmd
}

func newArtifactLinkCmd() *cobra.Command {
	var card, entry string
	cmd := &cobra.Command{
		Use:   "link <artifact>",
		Short: "Link an existing artifact to a card or an entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			usage := "trellis artifact link <artifact> --card <card> | --entry <entry>"
			if err := oneTarget(card, entry, true, usage); err != nil {
				return err
			}
			if entry != "" {
				return withTarget(refArg{Collection: address.CollectionVault, Value: entry}, func(app *appCtx, ref string) error {
					a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, args[0])
					if err != nil {
						return err
					}
					linked, err := app.Core.LinkArtifactToEntry(cmd.Context(), app.Project.ID, ref, a.ID)
					if err != nil {
						return err
					}
					return Emit(cmd, map[string]any{"artifact": a.Ref, "entry": linked.Slug},
						func() string { return linked.Slug + " -> " + a.Ref })
				})
			}
			return withTargets([]refArg{
				{Collection: address.CollectionArtifacts, Value: args[0]},
				{Collection: address.CollectionCards, Value: card},
			}, func(app *appCtx, refs []string) error {
				a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, refs[0])
				if err != nil {
					return err
				}
				id, err := cardID(cmd, app, refs[1])
				if err != nil {
					return err
				}
				if err := app.Core.LinkArtifactToCard(cmd.Context(), app.Project.ID, id, a.ID); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"artifact": a.Ref, "card": card}, func() string { return card + " -> " + a.Ref })
			})
		},
	}
	addTargetFlags(cmd, &card, &entry, "link to")
	return cmd
}

func newArtifactUnlinkCmd() *cobra.Command {
	var card, entry string
	cmd := &cobra.Command{
		Use:   "unlink <artifact>",
		Short: "Unlink an artifact from a card or an entry",
		Long: "Unlinking from an entry removes the name from its `artifacts` frontmatter.\n" +
			"A name whose artifact no longer exists can still be removed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			usage := "trellis artifact unlink <artifact> --card <card> | --entry <entry>"
			if err := oneTarget(card, entry, true, usage); err != nil {
				return err
			}
			return withTargets([]refArg{
				{Collection: address.CollectionCards, Value: card},
				{Collection: address.CollectionVault, Value: entry},
			}, func(app *appCtx, refs []string) error {
				if refs[1] != "" {
					unlinked, err := app.Core.UnlinkArtifactFromEntry(cmd.Context(), app.Project.ID, refs[1], args[0])
					if err != nil {
						return err
					}
					return Emit(cmd, map[string]any{"artifact": args[0], "entry": unlinked.Slug},
						func() string { return unlinked.Slug + " -x- " + args[0] })
				}
				a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				id, err := cardID(cmd, app, refs[0])
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
	addTargetFlags(cmd, &card, &entry, "unlink from")
	return cmd
}

func newArtifactLsCmd() *cobra.Command {
	var card, entry string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List stored artifacts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := oneTarget(card, entry, false, "trellis artifact ls [--card <card> | --entry <entry>]"); err != nil {
				return err
			}
			return withTargets([]refArg{
				{Collection: address.CollectionCards, Value: card},
				{Collection: address.CollectionVault, Value: entry},
			}, func(app *appCtx, refs []string) error {
				var cardIDValue, entryIDValue string
				if refs[0] != "" {
					var err error
					if cardIDValue, err = cardID(cmd, app, refs[0]); err != nil {
						return err
					}
				}
				if refs[1] != "" {
					loaded, err := app.Core.LoadEntry(cmd.Context(), app.Project.ID, refs[1])
					if err != nil {
						return err
					}
					entryIDValue = loaded.ID
				}
				items, err := app.Core.ListArtifacts(cmd.Context(), app.Project.ID, cardIDValue, entryIDValue)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"artifacts": items}, func() string {
					var b strings.Builder
					for _, item := range items {
						fmt.Fprintf(&b, "%s  %s  %s\n", item.Ref, item.Kind, item.Path)
					}
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
	addTargetFlags(cmd, &card, &entry, "only artifacts linked to")
	return cmd
}

func newArtifactRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <artifact>",
		Short: "Delete an artifact; entries that name it keep the name, and lint reports it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionArtifacts, Value: args[0]}, func(app *appCtx, ref string) error {
				a, err := app.Core.ResolveArtifact(cmd.Context(), app.Project.ID, ref)
				if err != nil {
					return err
				}
				if err := app.Core.DeleteArtifact(cmd.Context(), app.Project.ID, a.ID); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"deleted": a.Ref}, func() string { return "deleted " + a.Ref })
			})
		},
	}
}
