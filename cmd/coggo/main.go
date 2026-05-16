// Command coggo is the entry point for the Coggo personal multi-peer
// knowledge substrate. The actual command tree lives in internal/cli.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	appcli "github.com/lunguini/coggo/internal/cli"
	urfavecli "github.com/urfave/cli/v3"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := appcli.App().Run(ctx, os.Args); err != nil {
		var exitErr urfavecli.ExitCoder
		if errors.As(err, &exitErr) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(exitErr.ExitCode())
		}
		slog.Error("coggo failed", "err", err)
		os.Exit(1)
	}
}
