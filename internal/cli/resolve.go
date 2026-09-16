package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
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
	if key := projectKey(); key != "" {
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
func selectBoard(ctx context.Context, c *core.Core, r resolvedProject) (core.Board, error) {
	requested := cmp.Or(boardFlag, os.Getenv("TRELLIS_BOARD"))
	if requested != "" || r.Pin == nil || r.Pin.Target.Board() == "" {
		return c.SelectBoard(ctx, r.Project.ID, requested)
	}
	b, err := c.BoardBySlug(ctx, r.Project.ID, r.Pin.Target.Board())
	if ce, ok := errors.AsType[*core.Error](err); ok {
		ce.Msg = r.Pin.Path + ": " + ce.Msg
	}
	return b, err
}
