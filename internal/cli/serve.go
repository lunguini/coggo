package cli

import (
	"context"
	"errors"
	"log/slog"

	cli "github.com/urfave/cli/v3"

	"github.com/lunguini/coggo/internal/mcp"
)

func cmdServe() *cli.Command {
	return &cli.Command{
		Name:   "serve",
		Usage:  "Start the MCP server (default action)",
		Action: actionServe,
	}
}

func actionServe(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	rt, err := Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	srv := mcp.New(rt.Router, rt.Registry, rt.Auth, cfg.Server.ListenAddress)

	slog.Info("coggo serving", "addr", cfg.Server.ListenAddress)
	if err := srv.Start(ctx); err != nil {
		// Context cancellation is the normal shutdown path; don't surface as
		// a CLI failure.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			slog.Info("coggo shutdown")
			return nil
		}
		return err
	}
	return nil
}
