package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/platform"
)

func (t *tools) registerRemind(s *mcp.Server) {
	t.add(s, &mcp.Tool{Name: "remind", Description: "The cards you claim with no comment by you since the claim, and your entries that are not private, global or nominated", Annotations: readOnly}, nil, t.remind)
}

func (t *tools) remind(ctx context.Context, c call, in ProjectIn) (platform.Reminder, error) {
	project, err := c.project(in.Project)
	if err != nil {
		return platform.Reminder{}, err
	}
	return t.p.Remind(ctx, c.pr, project)
}
