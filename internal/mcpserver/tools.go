package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/platform"
)

type tools struct {
	p *platform.Platform
}

type ProjectListIn struct{}

type ProjectListOut struct {
	Projects []platform.Project `json:"projects"`
}

type ProjectIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
}

type BoardListOut struct {
	Boards []platform.Board `json:"boards"`
}

type BoardWriteIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Action  string `json:"action" jsonschema:"new, rename, share or unshare"`
	Board   string `json:"board,omitempty" jsonschema:"the board to change (rename, share, unshare)"`
	Name    string `json:"name,omitempty" jsonschema:"the board name (new) or the new name (rename)"`
}

type BoardOut struct {
	Board platform.Board `json:"board"`
}

type CardListIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Board   string `json:"board,omitempty" jsonschema:"the board name or id; defaults to your personal board"`
}

type CardListOut struct {
	Cards []platform.Card `json:"cards"`
}

type CardShowIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Ref     string `json:"ref" jsonschema:"the card ref, such as APP-3"`
}

type CardWriteIn struct {
	Project   string  `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Action    string  `json:"action" jsonschema:"new, edit, move, comment or block"`
	Board     string  `json:"board,omitempty" jsonschema:"the board for new; defaults to your personal board"`
	Ref       string  `json:"ref,omitempty" jsonschema:"the card ref (edit, move, comment, block)"`
	Title     *string `json:"title,omitempty" jsonschema:"the title (new, edit)"`
	Body      *string `json:"body,omitempty" jsonschema:"the body (new, edit) or the comment text (comment)"`
	Column    string  `json:"column,omitempty" jsonschema:"the column for new, or the target column for move"`
	Blocker   string  `json:"blocker,omitempty" jsonschema:"the ref of the card that blocks this one (block)"`
	IfVersion int64   `json:"if_version,omitempty" jsonschema:"the version you read (edit)"`
}

type CardWriteOut struct {
	Card    platform.Card     `json:"card"`
	Comment *platform.Comment `json:"comment,omitempty"`
}

type CardClaimIn struct {
	Project    string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Action     string `json:"action" jsonschema:"claim, next, renew or release"`
	Board      string `json:"board,omitempty" jsonschema:"the board for next; defaults to your personal board"`
	Ref        string `json:"ref,omitempty" jsonschema:"the card ref (claim, renew, release)"`
	Steal      bool   `json:"steal,omitempty" jsonschema:"take a live claim from another actor (claim)"`
	Reason     string `json:"reason,omitempty" jsonschema:"why you steal (claim with steal)"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty" jsonschema:"the claim length in seconds; default 1800"`
}

type CardClaimOut struct {
	Card *platform.Card `json:"card,omitempty"`
}

var (
	boardActions = []string{"new", "rename", "share", "unshare"}
	cardActions  = []string{"new", "edit", "move", "comment", "block"}
	claimActions = []string{"claim", "next", "renew", "release"}
	readOnly     = &mcp.ToolAnnotations{ReadOnlyHint: true}
)

func (t *tools) register(s *mcp.Server) {
	t.add(s, &mcp.Tool{Name: "project_list", Description: "List the projects you are a member of", Annotations: readOnly}, nil, t.projectList)
	t.add(s, &mcp.Tool{Name: "board_list", Description: "List the boards you see in a project", Annotations: readOnly}, nil, t.boardList)
	t.add(s, &mcp.Tool{Name: "board_write", Description: "Create, rename, share or unshare a board"}, boardActions, t.boardWrite)
	t.add(s, &mcp.Tool{Name: "board_brief", Description: "Card counts per column, your claimed cards and your next card", Annotations: readOnly}, nil, t.boardBrief)
	t.add(s, &mcp.Tool{Name: "card_list", Description: "List the cards on a board", Annotations: readOnly}, nil, t.cardList)
	t.add(s, &mcp.Tool{Name: "card_show", Description: "Show one card with its comments and events", Annotations: readOnly}, nil, t.cardShow)
	t.add(s, &mcp.Tool{Name: "card_write", Description: "Create, edit, move, comment on or block a card"}, cardActions, t.cardWrite)
	t.add(s, &mcp.Tool{Name: "card_claim", Description: "Claim a card, claim the next one, renew or release a claim"}, claimActions, t.cardClaim)
	t.registerVault(s)
	t.registerRemind(s)
	t.registerCoord(s)
}

func (t *tools) projectList(ctx context.Context, c call, _ ProjectListIn) (ProjectListOut, error) {
	projects, err := t.p.ListProjects(ctx, c.pr)
	return ProjectListOut{Projects: projects}, err
}

func (t *tools) boardList(ctx context.Context, c call, in ProjectIn) (BoardListOut, error) {
	project, err := c.project(in.Project)
	if err != nil {
		return BoardListOut{}, err
	}
	boards, err := t.p.ListBoards(ctx, c.pr, project)
	return BoardListOut{Boards: boards}, err
}

func (t *tools) boardWrite(ctx context.Context, c call, in BoardWriteIn) (BoardOut, error) {
	const tool = "board_write"
	var err error
	switch in.Action {
	case "new":
		err = needs(tool, in.Action, "name", in.Name)
	case "rename":
		err = cmpErr(needs(tool, in.Action, "board", in.Board), needs(tool, in.Action, "name", in.Name))
	case "share", "unshare":
		err = needs(tool, in.Action, "board", in.Board)
	default:
		err = badAction(tool, in.Action, boardActions)
	}
	if err != nil {
		return BoardOut{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return BoardOut{}, err
	}
	var b platform.Board
	switch in.Action {
	case "new":
		b, err = t.p.NewBoard(ctx, c.pr, project, in.Name)
	case "rename":
		b, err = t.p.RenameBoard(ctx, c.pr, project, in.Board, in.Name)
	case "share":
		b, err = t.p.ShareBoard(ctx, c.pr, project, in.Board)
	case "unshare":
		b, err = t.p.UnshareBoard(ctx, c.pr, project, in.Board)
	}
	return BoardOut{Board: b}, err
}

func (t *tools) boardBrief(ctx context.Context, c call, in ProjectIn) (platform.Brief, error) {
	project, err := c.project(in.Project)
	if err != nil {
		return platform.Brief{}, err
	}
	return t.p.Brief(ctx, c.pr, project)
}

func (t *tools) cardList(ctx context.Context, c call, in CardListIn) (CardListOut, error) {
	project, err := c.project(in.Project)
	if err != nil {
		return CardListOut{}, err
	}
	cards, err := t.p.ListCards(ctx, c.pr, project, in.Board)
	return CardListOut{Cards: cards}, err
}

func (t *tools) cardShow(ctx context.Context, c call, in CardShowIn) (platform.Card, error) {
	if err := needs("card_show", "show", "ref", in.Ref); err != nil {
		return platform.Card{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return platform.Card{}, err
	}
	return t.p.ShowCard(ctx, c.pr, project, in.Ref)
}

func (t *tools) cardWrite(ctx context.Context, c call, in CardWriteIn) (CardWriteOut, error) {
	const tool = "card_write"
	var err error
	switch in.Action {
	case "new":
		err = needs(tool, in.Action, "title", deref(in.Title))
	case "edit":
		err = needs(tool, in.Action, "ref", in.Ref)
	case "move":
		err = cmpErr(needs(tool, in.Action, "ref", in.Ref), needs(tool, in.Action, "column", in.Column))
	case "comment":
		err = cmpErr(needs(tool, in.Action, "ref", in.Ref), needs(tool, in.Action, "body", deref(in.Body)))
	case "block":
		err = cmpErr(needs(tool, in.Action, "ref", in.Ref), needs(tool, in.Action, "blocker", in.Blocker))
	default:
		err = badAction(tool, in.Action, cardActions)
	}
	if err != nil {
		return CardWriteOut{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return CardWriteOut{}, err
	}
	var out CardWriteOut
	switch in.Action {
	case "new":
		out.Card, err = t.p.CreateCard(ctx, c.pr, project, platform.NewCard{Board: in.Board, Title: deref(in.Title), Body: deref(in.Body), Column: in.Column})
	case "edit":
		out.Card, err = t.p.EditCard(ctx, c.pr, project, in.Ref, platform.CardEdit{Title: in.Title, Body: in.Body, IfVersion: in.IfVersion})
	case "move":
		out.Card, err = t.p.MoveCard(ctx, c.pr, project, in.Ref, in.Column)
	case "block":
		out.Card, err = t.p.BlockCard(ctx, c.pr, project, in.Ref, in.Blocker)
	case "comment":
		var cm platform.Comment
		if cm, err = t.p.CommentCard(ctx, c.pr, project, in.Ref, deref(in.Body)); err != nil {
			return out, err
		}
		out.Comment = &cm
		out.Card, err = t.p.ShowCard(ctx, c.pr, project, in.Ref)
	}
	return out, err
}

func (t *tools) cardClaim(ctx context.Context, c call, in CardClaimIn) (CardClaimOut, error) {
	const tool = "card_claim"
	var err error
	switch in.Action {
	case "claim", "renew", "release":
		err = needs(tool, in.Action, "ref", in.Ref)
	case "next":
	default:
		err = badAction(tool, in.Action, claimActions)
	}
	if err != nil {
		return CardClaimOut{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return CardClaimOut{}, err
	}
	ttl := time.Duration(in.TTLSeconds) * time.Second
	var card platform.Card
	switch in.Action {
	case "claim":
		card, err = t.p.ClaimCard(ctx, c.pr, project, in.Ref, platform.ClaimOpts{TTL: ttl, Steal: in.Steal, Reason: in.Reason})
	case "renew":
		card, err = t.p.RenewClaim(ctx, c.pr, project, in.Ref, ttl)
	case "release":
		card, err = t.p.ReleaseClaim(ctx, c.pr, project, in.Ref)
	case "next":
		next, err := t.p.ClaimNext(ctx, c.pr, project, in.Board, ttl)
		return CardClaimOut{Card: next}, err
	}
	if err != nil {
		return CardClaimOut{}, err
	}
	return CardClaimOut{Card: &card}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func cmpErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
