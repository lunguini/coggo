package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// loggingMiddleware logs every MCP tool invocation. Args are emitted at debug
// (potentially noisy + may include user content); the call summary is at info
// so `--log-level info` is enough to see "what tools are being called" without
// drowning in payloads.
func loggingMiddleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		toolName := req.Params.Name

		// Args at debug only — they can be large and may include user data.
		if slog.Default().Enabled(ctx, slog.LevelDebug) {
			if argsJSON, err := json.Marshal(req.Params.Arguments); err == nil {
				slog.Debug("mcp.tool.call", "tool", toolName, "args", string(argsJSON))
			}
		}

		result, err := next(ctx, req)

		dur := time.Since(start)
		switch {
		case err != nil:
			slog.Info("mcp.tool.done", "tool", toolName, "ok", false, "err", err.Error(), "dur_ms", dur.Milliseconds())
		case result != nil && result.IsError:
			// Tool-level errors (auth failures, validation, etc.) come back as
			// IsError=true result, not a Go error. Surface them at info too.
			msg := errorTextOf(result)
			slog.Info("mcp.tool.done", "tool", toolName, "ok", false, "err", msg, "dur_ms", dur.Milliseconds())
		default:
			slog.Info("mcp.tool.done", "tool", toolName, "ok", true, "dur_ms", dur.Milliseconds())
		}

		return result, err
	}
}

func errorTextOf(r *mcp.CallToolResult) string {
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return "(error)"
}
