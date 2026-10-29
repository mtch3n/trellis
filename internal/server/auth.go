package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/mtch3n/trellis/internal/mcpserver"
	"github.com/mtch3n/trellis/internal/platform"
)

func requireAgent(p *platform.Platform, next http.Handler) http.Handler {
	verify := func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		id, err := p.VerifyToken(ctx, token)
		if platform.IsCode(err, "bad_token") {
			return nil, fmt.Errorf("%w: unknown or revoked token", auth.ErrInvalidToken)
		}
		if err != nil {
			return nil, err
		}
		if id.Kind != platform.TokenAgent {
			return nil, fmt.Errorf("%w: /mcp takes an agent token", auth.ErrInvalidToken)
		}
		return mcpserver.TokenInfo(id), nil
	}
	guarded := auth.RequireBearerToken(verify, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guarded.ServeHTTP(bearerChallenge{ResponseWriter: w}, r)
	})
}

type bearerChallenge struct {
	http.ResponseWriter
}

func (b bearerChallenge) WriteHeader(code int) {
	if code == http.StatusUnauthorized && b.Header().Get("WWW-Authenticate") == "" {
		b.Header().Set("WWW-Authenticate", "Bearer")
	}
	b.ResponseWriter.WriteHeader(code)
}

func (b bearerChallenge) Unwrap() http.ResponseWriter {
	return b.ResponseWriter
}
