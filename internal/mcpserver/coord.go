package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/platform"
)

type CoordinationIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Action  string `json:"action" jsonschema:"list or dismiss"`
	ID      string `json:"id,omitempty" jsonschema:"the notice id (dismiss)"`
}

type CoordinationOut struct {
	Notices []platform.Notice `json:"notices"`
}

var coordActions = []string{"list", "dismiss"}

func (t *tools) registerCoord(s *mcp.Server) {
	t.add(s, &mcp.Tool{Name: "coordination", Description: "List your notices about work that overlaps another member's, or dismiss one"}, coordActions, t.coordination)
}

func (t *tools) coordination(ctx context.Context, c call, in CoordinationIn) (CoordinationOut, error) {
	const tool = "coordination"
	var err error
	switch in.Action {
	case "list":
	case "dismiss":
		err = needs(tool, in.Action, "id", in.ID)
	default:
		err = badAction(tool, in.Action, coordActions)
	}
	if err != nil {
		return CoordinationOut{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return CoordinationOut{}, err
	}
	if in.Action == "dismiss" {
		if err := t.p.DismissNotice(ctx, c.pr, project, in.ID); err != nil {
			return CoordinationOut{}, err
		}
	}
	notices, err := t.p.Notices(ctx, c.pr, project)
	return CoordinationOut{Notices: notices}, err
}
