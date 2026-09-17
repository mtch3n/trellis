package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/vpath"
)

// refArg is one reference a command takes, positional or from a flag, and
// the collection it names.
type refArg struct {
	Collection string // vpath.CollectionCards, CollectionKnowledge, CollectionBoards or CollectionArtifacts
	Value      string // "" when an optional flag was not given
	// NoProject lets a /GLOBAL/knowledge address run with no project at all:
	// reading, editing or walking from a vault entry needs none. fn then
	// receives an appCtx whose Project and Board are zero.
	NoProject bool
}

// argProject reads the project a reference names by itself -- an absolute
// address or, for a card, a qualified ref such as OTHER-12 -- and returns ""
// when the reference is relative or is a vault address. ref is the reference
// in the form core takes: a board address becomes its slug. Everything else
// passes through whole, because core reads card, knowledge and artifact
// addresses itself, and a card address must reach core with its project.
func argProject(a refArg) (key, ref string, err error) {
	v := strings.TrimSpace(a.Value)
	if !strings.HasPrefix(v, "/") {
		if a.Collection == vpath.CollectionCards {
			if r := core.ParseCardRef(v); r.ProjectKey != "" {
				return r.ProjectKey, v, nil
			}
		}
		return "", v, nil
	}
	target := v
	if a.Collection == vpath.CollectionKnowledge {
		target, _ = vpath.SplitAnchor(v) // only an entry has headings
	}
	p, err := core.ParseAddress(strings.TrimSpace(target), a.Collection)
	if err != nil {
		return "", "", err
	}
	switch {
	case p.Project == vpath.GlobalKey:
		return "", v, nil // the vault belongs to no project
	case a.Collection == vpath.CollectionBoards:
		return p.Project, p.Name, nil
	default:
		return p.Project, v, nil
	}
}

// withTarget runs fn in the context one reference names. See withTargets.
func withTarget(a refArg, fn func(app *appCtx, ref string) error) error {
	return withTargets([]refArg{a}, func(app *appCtx, refs []string) error { return fn(app, refs[0]) })
}

// withTargets runs fn in the context a command's references name, and closes
// the database afterwards. args holds the positional reference first, then
// every flag that takes one (--by, --card, a command's own --board). refs[i]
// is args[i] in the form core takes, and "" for a flag that was not given.
//
// A reference that names its own project -- an address, or a qualified card
// ref -- decides where the command runs, so it needs no pin. It beats the pin
// and TRELLIS_PROJECT, which are ambient.
//
// Two references naming different projects are a conflict: the caller
// stated two targets. A relative reference names the current project, so it
// conflicts with a reference naming another one whenever a current project
// resolves. In a directory where none does, the named project is the only
// candidate, and the relative reference is read there. With no reference
// naming a project, the command runs where withBoard would.
func withTargets(args []refArg, fn func(app *appCtx, refs []string) error) error {
	refs := make([]string, len(args))
	keys := make([]string, len(args))
	named, primary, relative := "", 0, false
	for i, a := range args {
		if strings.TrimSpace(a.Value) == "" {
			continue
		}
		key, ref, err := argProject(a)
		if err != nil {
			return err
		}
		refs[i], keys[i] = ref, key
		switch {
		case key == "" && !isVaultAddress(a.Value):
			relative = true
		case key == "":
		case named == "":
			named, primary = key, i
		case key != named:
			return projectConflict(named, a.Value, key)
		}
	}
	app, err := targetContext(args[primary], named, refs[primary], relative)
	if err != nil {
		return err
	}
	defer app.db.Close()
	for i, a := range args {
		if a.Collection != vpath.CollectionBoards || keys[i] == "" {
			continue
		}
		// A board address works on that board; core takes board names.
		b, err := app.Core.BoardBySlug(context.Background(), app.Project.ID, refs[i])
		if err != nil {
			return err
		}
		refs[i] = b.Name
		if i == primary {
			app.Board = b
		}
	}
	return fn(app, refs)
}

// targetContext opens the context a command runs in. key is the project its
// references name, "" when none does. a is the reference that named it, or
// the positional one when none did. relative says whether some other
// reference relies on the current project.
func targetContext(a refArg, key, ref string, relative bool) (*appCtx, error) {
	if key == "" && a.NoProject && isVaultAddress(a.Value) {
		// A vault entry belongs to no project, so nothing ambient is
		// consulted: a malformed or stale pin, or a TRELLIS_PROJECT naming
		// nothing, must not stand between a reader and the vault.
		c, db, err := openCore()
		if err != nil {
			return nil, err
		}
		return &appCtx{Core: c, db: db}, nil
	}
	if key == "" {
		return currentBoard()
	}

	if flag := normalizeProjectArg(projectFlagKey); flag != "" && flag != key {
		return nil, projectConflict(flag, a.Value, key)
	}
	c, db, err := openCore()
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*appCtx, error) {
		db.Close()
		return nil, err
	}
	ctx := context.Background()
	r, rerr := resolveProject(ctx, c)
	switch {
	case rerr == nil && r.Project.Key == key:
		b, err := selectBoard(ctx, c, r)
		if err != nil {
			return fail(err)
		}
		return &appCtx{Core: c, Project: r.Project, Board: b, db: db}, nil
	case relative && rerr == nil:
		// The relative reference means this project, not the named one.
		return fail(projectConflict(r.Project.Key, a.Value, key))
	case relative && !isUnresolved(rerr):
		// A relative reference needs the current project, and a broken pin
		// is not the same as no pin.
		return fail(rerr)
	}
	p, err := c.ProjectByKey(ctx, key)
	if err != nil {
		return fail(err)
	}
	b, err := namedBoard(ctx, c, p, a.Collection, ref)
	if err != nil {
		return fail(err)
	}
	return &appCtx{Core: c, Project: p, Board: b, db: db}, nil
}

// namedBoard picks the board in a project a reference named. Only --board
// applies there: TRELLIS_BOARD and a pin's board describe the current
// project. A card is worked on its own board, so that moving OTHER-12 never
// drags it onto OTHER's default board.
func namedBoard(ctx context.Context, c *core.Core, p core.Project, collection, ref string) (core.Board, error) {
	switch {
	case collection == vpath.CollectionBoards:
		return core.Board{}, nil // withTargets sets the addressed board
	case boardFlag != "":
		return boardNamed(ctx, c, p, boardFlag)
	case collection == vpath.CollectionCards:
		card, err := c.GetCard(ctx, p.ID, core.ParseCardRef(ref))
		if err != nil {
			// The command looks the card up itself and reports this in its
			// own words; card block, for one, says cross_project_block.
			return core.Board{}, nil
		}
		boards, err := c.ListBoards(ctx, p.ID)
		if err != nil {
			return core.Board{}, err
		}
		i := slices.IndexFunc(boards, func(b core.Board) bool { return b.ID == card.BoardID })
		if i < 0 {
			return core.Board{}, fmt.Errorf("card %s is on a board that no longer exists", card.Ref)
		}
		return boards[i], nil
	default:
		return c.SelectBoard(ctx, p.ID, "")
	}
}

// isUnresolved reports whether err says no pin applies here.
func isUnresolved(err error) bool {
	ce, ok := errors.AsType[*core.Error](err)
	return ok && ce.Code == "unresolved"
}

// isVaultAddress reports whether v is a /GLOBAL address.
func isVaultAddress(v string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(v)), "/"+vpath.GlobalKey+"/")
}

// tuiCardRef reads a card argument inside the workspace, which is bound to
// one project. An address must name that project. A qualified ref's prefix
// is not compared here: core's checkCardProject judges it, and the merge
// layer, where MONO legitimately holds API-1, replaces that check.
func tuiCardRef(app *appCtx, arg string) (core.CardRef, error) {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "/") {
		p, err := core.ParseAddress(arg, vpath.CollectionCards)
		if err != nil {
			return core.CardRef{}, err
		}
		if p.Project != app.Project.Key {
			return core.CardRef{}, core.ErrUsage("wrong_project",
				fmt.Sprintf("%s is in project %s; this workspace is %s", arg, p.Project, app.Project.Key),
				"trellis tui --project "+p.Project)
		}
	}
	return core.ParseCardRef(arg), nil
}
