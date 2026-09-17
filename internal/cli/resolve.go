package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
)

// resolvedProject is the project a command acts on and, when a marker chose
// it, that marker.
type resolvedProject struct {
	Project core.Project
	Marker  *resolve.Marker
}

// resolveProject picks the project: --project, then TRELLIS_PROJECT, then the
// nearest .trellis marker. Nothing else -- no git lookup, and no project is
// ever created here.
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
	marker, found, err := resolve.FindMarker(dir)
	if err != nil {
		return resolvedProject{}, markerFailure(err)
	}
	if !found {
		return resolvedProject{}, core.ErrUsage("unresolved",
			"no .trellis marker in this directory or any parent", "trellis init --key <KEY>")
	}
	p, err := c.ProjectByKey(ctx, marker.Target.Project)
	if ce, ok := errors.AsType[*core.Error](err); ok && ce.Code == "project_not_found" {
		return resolvedProject{}, core.ErrNotFound("project_not_found",
			fmt.Sprintf("%s names project %s, which this Trellis database does not have", marker.Path, marker.Target.Project),
			"trellis init   # in "+filepath.Dir(marker.Path))
	}
	if err != nil {
		return resolvedProject{}, err
	}
	return resolvedProject{Project: p, Marker: &marker}, nil
}

// markerFailure reports a malformed marker as a usage error. Anything else is
// an I/O failure and passes through unchanged.
func markerFailure(err error) error {
	if me, ok := errors.AsType[*resolve.MarkerError](err); ok {
		return core.ErrUsage("bad_pin", me.Error(),
			"trellis init --key <KEY>   # after removing "+me.Path)
	}
	return err
}

// selectBoard picks the board: --board, then TRELLIS_BOARD, then the marker's
// board when the marker also chose the project, then core.SelectBoard's rules.
// Either selector may be a board address.
func selectBoard(ctx context.Context, c *core.Core, r resolvedProject) (core.Board, error) {
	if requested := cmp.Or(boardFlag, os.Getenv("TRELLIS_BOARD")); requested != "" {
		return boardNamed(ctx, c, r.Project, requested)
	}
	if r.Marker == nil || r.Marker.Target.Board() == "" {
		return c.SelectBoard(ctx, r.Project.ID, "")
	}
	b, err := c.BoardBySlug(ctx, r.Project.ID, r.Marker.Target.Board())
	if ce, ok := errors.AsType[*core.Error](err); ok {
		ce.Msg = r.Marker.Path + ": " + ce.Msg
	}
	return b, err
}

// namedProjectKey is the project named without the marker's help: --project,
// then the project a --board address names, then TRELLIS_PROJECT. --project
// and a --board address that disagree are a conflict: the caller stated two
// targets.
func namedProjectKey() (string, error) {
	flag := normalizeProjectArg(projectFlagKey)
	fromBoard := ""
	if v := strings.TrimSpace(boardFlag); strings.HasPrefix(v, "/") {
		p, err := core.ParseAddress(v, address.CollectionBoards)
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
	addr, err := core.ParseAddress(v, address.CollectionBoards)
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
	if p, err := address.Parse(v); err == nil && p.Collection == "" {
		return p.Project
	}
	return strings.ToUpper(v)
}
