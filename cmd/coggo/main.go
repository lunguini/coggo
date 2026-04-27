// Command coggo is the entry point for the Coggo personal multi-peer
// knowledge substrate. The actual command tree lives in internal/cli.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lunguini/coggo/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.App().Run(ctx, os.Args); err != nil {
		slog.Error("coggo failed", "err", err)
		os.Exit(1)
	}
}
