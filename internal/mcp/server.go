// Package mcp implements Coggo's MCP server (HTTP transport, streamable).
//
// The server exposes 12 tools (entity/relation/type/search/time-travel ops plus
// cross-peer query) backed by the federation router. Bearer-token auth is
// enforced per tool call against the target peer named in the tool arguments.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/mark3labs/mcp-go/server"

	"github.com/lunguini/coggo/internal/peer"
	"github.com/lunguini/coggo/internal/types"
)

// Server is the Coggo MCP HTTP server.
type Server struct {
	Router       types.Router
	Registry     *peer.Registry
	Auth         types.Authority
	Addr         string
	EndpointPath string

	mcpServer  *server.MCPServer
	httpServer *server.StreamableHTTPServer
}

// New constructs a Server but does not start it. Call Start to bind the
// listener.
func New(router types.Router, registry *peer.Registry, authStore types.Authority, addr string) *Server {
	s := &Server{
		Router:       router,
		Registry:     registry,
		Auth:         authStore,
		Addr:         addr,
		EndpointPath: "/mcp",
	}
	mcpSrv := server.NewMCPServer(
		"coggo", "0.1.0",
		server.WithToolCapabilities(false),
		server.WithToolHandlerMiddleware(loggingMiddleware),
	)
	s.mcpServer = mcpSrv
	s.registerTools()
	s.httpServer = server.NewStreamableHTTPServer(
		mcpSrv,
		server.WithEndpointPath(s.EndpointPath),
		server.WithStateLess(true),
		server.WithHTTPContextFunc(s.injectAuth),
	)
	return s
}

// Start binds the HTTP listener and blocks until ctx is cancelled or the
// server fails. On ctx cancellation it issues a graceful shutdown.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("mcp: listening", "addr", s.Addr, "endpoint", s.EndpointPath)
		if err := s.httpServer.Start(s.Addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("mcp: http listen: %w", err)
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = s.httpServer.Shutdown(shutCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("mcp: shutdown: %w", err)
	}
	return nil
}
