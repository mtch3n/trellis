package mcpserver

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/platform"
)

const (
	HeaderProject = "X-Trellis-Project"
	HeaderSession = "X-Trellis-Session"
	identityKey   = "trellis.identity"
)

func TokenInfo(id platform.TokenIdentity) *auth.TokenInfo {
	return &auth.TokenInfo{UserID: id.MemberID.String(), Extra: map[string]any{identityKey: id}}
}

func New(p *platform.Platform) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "trellis", Version: "v0.1.0"}, nil)
	t := &tools{p: p}
	t.register(s)
	return s
}

func Handler(p *platform.Platform) http.Handler {
	s := New(p)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s },
		&mcp.StreamableHTTPOptions{Stateless: true})
}
