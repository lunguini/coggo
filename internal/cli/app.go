package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	cli "github.com/urfave/cli/v3"

	"github.com/lunguini/coggo/internal/config"
)

// Version reported by `coggo version`. Overridable at build time via
// `-ldflags "-X github.com/lunguini/coggo/internal/cli.Version=<v>"`.
var Version = "0.1.0-dev"

// App returns the root cli.Command. Wired with global flags (--config, --peer)
// and all subcommands. Running with no subcommand is `serve`.
func App() *cli.Command {
	root := &cli.Command{
		Name:    "coggo",
		Usage:   "Personal multi-peer knowledge substrate",
		Version: Version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Usage:   "Path to coggo config TOML",
				Value:   config.DefaultPath(),
				Sources: cli.EnvVars("COGGO_CONFIG"),
			},
			&cli.StringFlag{
				Name:  "peer",
				Usage: "Default peer for ambiguous commands",
			},
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "debug | info | warn | error (overrides config)",
				Sources: cli.EnvVars("COGGO_LOG_LEVEL"),
			},
		},
		Before: beforeApp,
		Action: actionRoot,
		ExitErrHandler: func(context.Context, *cli.Command, error) {
			// main owns user-facing error printing and process exit.
		},
		Commands: []*cli.Command{
			cmdInit(),
			cmdServe(),
			cmdStatus(),
			cmdToday(),
			cmdPeer(),
			cmdType(),
			cmdEntity(),
			cmdRelation(),
			cmdToken(),
			cmdBackup(),
			cmdThink(),
			cmdAliasDecision(),
			cmdAliasGoal(),
			cmdAliasObservation(),
			cmdAliasProject(),
			cmdAliasDomain(),
			cmdVersion(),
		},
	}
	return root
}

func actionRoot(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Present() {
		return cli.Exit(fmt.Sprintf("Error: unknown command %q\nRun `coggo --help` to see available commands.", cmd.Args().First()), 2)
	}
	return actionServe(ctx, cmd)
}

// beforeApp configures slog from the loaded config's logging level.
func beforeApp(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return ctx, err
	}
	// Flag/env overrides config.
	configured := cfg.Logging.Level
	if v := cmd.String("log-level"); v != "" {
		configured = v
	}
	level := slog.LevelInfo
	switch strings.ToLower(configured) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
	return ctx, nil
}

func cmdVersion() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print version",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Println("coggo", Version)
			return nil
		},
	}
}
