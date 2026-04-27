package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/lunguini/coggo/internal/peer"
	"github.com/lunguini/coggo/internal/types"
)

// ctxKey is unexported to keep middleware-injected values private to this
// package; tool handlers retrieve them via the helpers below.
type ctxKey int

const (
	ctxKeyToken ctxKey = iota
	ctxKeyAuth
	ctxKeyRegistry
	ctxKeyRouter
)

// injectAuth runs as the StreamableHTTP context func: it pulls the bearer
// secret out of the Authorization header and stows it (and the auth/registry/
// router pointers) on the per-request context. Verification happens per tool
// call in authorizeForPeer because the target peer is a tool argument, not a
// connection-level fact.
//
// Note: the MCP spec calls for HTTP 401/403 on unauth/forbidden, but the
// streamable-http transport here returns 200 with an in-band JSON-RPC
// response. For v0.1 we surface auth failures as MCP tool error results
// (IsError=true with a clear message) — protocol-correct, and any MCP
// client will surface the message to the user.
func (s *Server) injectAuth(ctx context.Context, r *http.Request) context.Context {
	authz := r.Header.Get("Authorization")
	token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	ctx = context.WithValue(ctx, ctxKeyToken, token)
	ctx = context.WithValue(ctx, ctxKeyAuth, s.Auth)
	ctx = context.WithValue(ctx, ctxKeyRegistry, s.Registry)
	ctx = context.WithValue(ctx, ctxKeyRouter, s.Router)
	return ctx
}

func tokenFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyToken).(string)
	return v
}

func authFromCtx(ctx context.Context) types.Authority {
	v, _ := ctx.Value(ctxKeyAuth).(types.Authority)
	return v
}

func registryFromCtx(ctx context.Context) *peer.Registry {
	v, _ := ctx.Value(ctxKeyRegistry).(*peer.Registry)
	return v
}

func routerFromCtx(ctx context.Context) types.Router {
	v, _ := ctx.Value(ctxKeyRouter).(types.Router)
	return v
}

// authorizeForPeer resolves a peer name-or-DID to its DID and verifies the
// bearer token has authority for that peer (by name). Returns the resolved
// DID, peer, and the verified token (so callers can read its label for
// provenance); returns an error suitable for surfacing as a tool error
// result.
func authorizeForPeer(ctx context.Context, peerNameOrDID string) (string, *types.Peer, *types.Token, error) {
	reg := registryFromCtx(ctx)
	auth := authFromCtx(ctx)
	if reg == nil || auth == nil {
		return "", nil, nil, fmt.Errorf("server misconfigured")
	}
	if peerNameOrDID == "" {
		return "", nil, nil, fmt.Errorf("peer is required")
	}
	p, err := reg.Resolve(peerNameOrDID)
	if err != nil {
		return "", nil, nil, fmt.Errorf("unknown peer %q", peerNameOrDID)
	}
	tok := tokenFromCtx(ctx)
	if tok == "" {
		return "", nil, nil, fmt.Errorf("unauthorized: missing bearer token")
	}
	verified, err := auth.Verify(ctx, tok, p.Name)
	if err != nil {
		return "", nil, nil, fmt.Errorf("unauthorized: %w", err)
	}
	return p.DID, p, verified, nil
}

// clientIDFromToken returns the label to record as created_by_client for
// writes authorized by tok. Falls back to "mcp" when no label was set.
func clientIDFromToken(tok *types.Token) string {
	if tok != nil && tok.Label != "" {
		return tok.Label
	}
	return "mcp"
}
