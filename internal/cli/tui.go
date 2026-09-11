package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const tuiHelp = `/board [name]           Show cards, or switch board
/boards                 List boards
/show <card>            Read a card before editing
/new <title>            Create a card
/title <card> <text>     Rename a card you have read
/body <card> <text>      Replace its body (use \n for line breaks)
/move <card> <column>    Move a card
/note <card> <text>      Append a note
/search <query>          Search project cards and knowledge (up to 50 hits)
/help                   Show commands
/quit                   Exit

Tab completes commands · ↑/↓ history · Ctrl-D or Ctrl-C exits
Text without a slash searches the project. No AI provider is required.`

var tuiCommands = []string{"/board", "/boards", "/show", "/new", "/title", "/body", "/move", "/note", "/search", "/help", "/quit"}

func newTUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "tui", Short: "Open the interactive terminal workspace", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if forceJSON || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
				return core.ErrUsage("terminal_required", "tui requires an interactive terminal and does not support --json", "trellis tui")
			}
			return withBoard(func(app *appCtx) error { return runTUI(cmd.Context(), app) })
		},
	}
	addActorFlag(cmd)
	return cmd
}

func runTUI(ctx context.Context, app *appCtx) error {
	workspace := newTerminalWorkspace(ctx, app)
	if err := workspace.reload(); err != nil {
		return err
	}
	return workspace.run()
}

func completeTUI(line string, pos int, key rune) (string, int, bool) {
	if key != '\t' || pos != len(line) || strings.ContainsAny(line, " \t") {
		return "", 0, false
	}
	var matches []string
	for _, command := range tuiCommands {
		if strings.HasPrefix(command, line) {
			matches = append(matches, command)
		}
	}
	if len(matches) == 1 {
		result := matches[0] + " "
		return result, len(result), true
	}
	return "", 0, false
}

// Stored content must never be interpreted as terminal control sequences.
func safeTerminalText(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, text)
}

type tuiSession struct {
	app  *appCtx
	seen map[string]int64
}

func tuiSplit(text string) (string, string) {
	text = strings.TrimSpace(text)
	i := strings.IndexFunc(text, unicode.IsSpace)
	if i < 0 {
		return text, ""
	}
	return text[:i], strings.TrimSpace(text[i:])
}

func (s *tuiSession) execute(ctx context.Context, line string) (string, error) {
	command, arg := tuiSplit(line)
	app := s.app
	switch command {
	case "/help":
		return tuiHelp, nil
	case "/boards":
		boards, err := app.Core.ListBoards(ctx, app.Project.ID)
		if err != nil {
			return "", err
		}
		var out strings.Builder
		for _, b := range boards {
			mark := "  "
			if b.ID == app.Board.ID {
				mark = "› "
			}
			fmt.Fprintf(&out, "%s%s (%s)\n", mark, b.Name, b.Slug)
		}
		return out.String(), nil
	case "/board":
		if arg != "" {
			board, err := app.Core.SelectBoard(ctx, app.Project.ID, arg)
			if err != nil {
				return "", err
			}
			app.Board = board
		}
		columns, err := app.Core.ListColumns(ctx, app.Board.ID)
		if err != nil {
			return "", err
		}
		page, err := app.Core.ListCardsPage(ctx, core.CardScope{BoardID: app.Board.ID}, core.CardFilter{Limit: -1})
		if err != nil {
			return "", err
		}
		var out strings.Builder
		fmt.Fprintf(&out, "%s / %s · %d cards\n", app.Project.Key, app.Board.Name, page.Total)
		for _, column := range columns {
			fmt.Fprintf(&out, "\n%s\n", strings.ToUpper(column.Name))
			count := 0
			for _, card := range page.Cards {
				if card.ColumnID != column.ID {
					continue
				}
				fmt.Fprintf(&out, "  %s  [%s] %s\n", card.Ref, card.PriorityName, card.Title)
				count++
			}
			if count == 0 {
				out.WriteString("  No cards\n")
			}
		}
		if len(columns) == 0 {
			out.WriteString("\nNo columns. Add one with trellis column new --help.\n")
		}
		return out.String(), nil
	case "/new":
		if arg == "" {
			return "", fmt.Errorf("usage: /new <title>")
		}
		card, err := app.Core.CreateCard(ctx, app.Project.ID, app.Board.ID, core.NewCard{Title: arg})
		if err != nil {
			return "", err
		}
		s.seen[card.ID] = card.Version
		return "Created " + card.Ref + " · " + card.Title, nil
	case "/show", "/title", "/body", "/move", "/note":
		ref, text := tuiSplit(arg)
		if ref == "" || (command != "/show" && text == "") || (command == "/show" && text != "") {
			return "", fmt.Errorf("invalid arguments for %s; see /help", command)
		}
		card, err := app.Core.GetCard(ctx, app.Project.ID, core.ParseCardRef(ref))
		if err != nil {
			return "", err
		}
		switch command {
		case "/show":
			s.seen[card.ID] = card.Version
			return fmt.Sprintf("%s · %s\n%s / %s · version %d\nLabels: %s\n\n%s", card.Ref, card.Title, card.ColumnName, card.PriorityName, card.Version, strings.Join(card.Labels, ", "), card.BodyMD), nil
		case "/title", "/body":
			version, ok := s.seen[card.ID]
			if !ok {
				return "", fmt.Errorf("read this card with /show %s before editing", ref)
			}
			edit := core.CardEdit{IfVersion: new(version)}
			if command == "/title" {
				edit.Title = new(text)
			} else {
				edit.Body = new(strings.ReplaceAll(text, `\n`, "\n"))
			}
			card, err = app.Core.EditCard(ctx, app.Project.ID, core.ParseCardRef(ref), edit)
			if err != nil {
				return "", err
			}
			s.seen[card.ID] = card.Version
		case "/move":
			card, err = app.Core.MoveCard(ctx, app.Project.ID, app.Board.ID, core.ParseCardRef(ref), text)
			if err != nil {
				return "", err
			}
		case "/note":
			if _, err = app.Core.CreateNote(ctx, card.ID, text); err != nil {
				return "", err
			}
		}
		return "Updated " + card.Ref + " · " + card.Title, nil
	default:
		query := line
		if command == "/search" {
			query = arg
		} else if strings.HasPrefix(command, "/") {
			return "", fmt.Errorf("unknown command %s; use /help", command)
		}
		if strings.TrimSpace(query) == "" {
			return "", fmt.Errorf("usage: /search <query>")
		}
		hits, err := app.Core.Search(ctx, app.Project.ID, query, core.SearchOpts{Limit: 50})
		if err != nil {
			return "", err
		}
		if len(hits) == 0 {
			return "No matches. Try another search.", nil
		}
		var out strings.Builder
		fmt.Fprintf(&out, "%d search results (maximum 50)\n", len(hits))
		for _, hit := range hits {
			fmt.Fprintf(&out, "  %s  [%s] %s\n", hit.Ref, hit.Kind, hit.Title)
		}
		return out.String(), nil
	}
}
