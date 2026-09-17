package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/vpath"
)

// resolvedProject is the project a command acts on and, when a pin chose it,
// that pin.
type resolvedProject struct {
	Project core.Project
	Pin     *resolve.Pin
}

// resolveProject picks the project: --project, then TRELLIS_PROJECT, then the
// nearest .trellis pin. Nothing else -- no git lookup, and no project is ever
// created here.
func resolveProject(ctx context.Context, c *core.Core) (resolvedProject, error) {
	key, err := namedProjectKey()
	if err != nil {
		return resolvedProject{}, err
	}
	if key != "" {
		p, err := c.ProjectByKey(ctx, key)
		return resolvedProject{Project: p}, err
	}
	dir, err := os.Getwd()
	if err != nil {
		return resolvedProject{}, err
	}
	pin, found, err := resolve.FindPin(dir)
	if err != nil {
		return resolvedProject{}, pinFailure(err)
	}
	if !found {
		return resolvedProject{}, core.ErrUsage("unresolved",
			"no .trellis pin in this directory or any parent", "trellis init --key <KEY>")
	}
	p, err := c.ProjectByKey(ctx, pin.Target.Project)
	if ce, ok := errors.AsType[*core.Error](err); ok && ce.Code == "project_not_found" {
		return resolvedProject{}, core.ErrNotFound("project_not_found",
			fmt.Sprintf("%s names project %s, which this Trellis database does not have", pin.Path, pin.Target.Project),
			"trellis init   # in "+filepath.Dir(pin.Path))
	}
	if err != nil {
		return resolvedProject{}, err
	}
	return resolvedProject{Project: p, Pin: &pin}, nil
}

// pinFailure reports a malformed pin as a usage error. Anything else is an I/O
// failure and passes through unchanged.
func pinFailure(err error) error {
	if pe, ok := errors.AsType[*resolve.PinError](err); ok {
		return core.ErrUsage("bad_pin", pe.Error(),
			"trellis init --key <KEY>   # after removing "+pe.Path)
	}
	return err
}

// selectBoard picks the board: --board, then TRELLIS_BOARD, then the pin's
// board when the pin also chose the project, then core.SelectBoard's rules.
// Either selector may be a board address.
func selectBoard(ctx context.Context, c *core.Core, r resolvedProject) (core.Board, error) {
	if requested := cmp.Or(boardFlag, os.Getenv("TRELLIS_BOARD")); requested != "" {
		return boardNamed(ctx, c, r.Project, requested)
	}
	if r.Pin == nil || r.Pin.Target.Board() == "" {
		return c.SelectBoard(ctx, r.Project.ID, "")
	}
	b, err := c.BoardBySlug(ctx, r.Project.ID, r.Pin.Target.Board())
	if ce, ok := errors.AsType[*core.Error](err); ok {
		ce.Msg = r.Pin.Path + ": " + ce.Msg
	}
	return b, err
}

// namedProjectKey is the project named without the pin's help: --project,
// then the project a --board address names, then TRELLIS_PROJECT. --project
// and a --board address that disagree are a conflict: the caller stated two
// targets.
func namedProjectKey() (string, error) {
	flag := normalizeProjectArg(projectFlagKey)
	fromBoard := ""
	if v := strings.TrimSpace(boardFlag); strings.HasPrefix(v, "/") {
		p, err := core.ParseAddress(v, vpath.CollectionBoards)
		if err != nil {
			return "", err
		}
		fromBoard = p.Project
	}
	if flag != "" && fromBoard != "" && flag != fromBoard {
		return "", projectConflict(flag, boardFlag, fromBoard)
	}
	return cmp.Or(flag, fromBoard, normalizeProjectArg(os.Getenv("TRELLIS_PROJECT"))), nil
}

// boardNamed resolves a board given by name or by address in project p. An
// address naming another project is a conflict, never a silent switch.
func boardNamed(ctx context.Context, c *core.Core, p core.Project, v string) (core.Board, error) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "/") {
		return c.SelectBoard(ctx, p.ID, v)
	}
	addr, err := core.ParseAddress(v, vpath.CollectionBoards)
	if err != nil {
		return core.Board{}, err
	}
	if addr.Project != p.Key {
		return core.Board{}, projectConflict(p.Key, v, addr.Project)
	}
	return c.BoardBySlug(ctx, p.ID, addr.Name)
}

// projectConflict reports two explicit targets that disagree.
func projectConflict(have, arg, named string) error {
	return core.ErrUsage("project_conflict",
		fmt.Sprintf("this command acts in %s, but %s names %s; give only one of them", have, arg, named), "")
}

// normalizeProjectArg reads a project given as KEY or /KEY. Anything else is
// upper-cased and left for the lookup to report.
func normalizeProjectArg(v string) string {
	v = strings.TrimSpace(v)
	if p, err := vpath.Parse(v); err == nil && p.Collection == "" {
		return p.Project
	}
	return strings.ToUpper(v)
}
