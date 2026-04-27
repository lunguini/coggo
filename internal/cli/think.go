package cli

import (
	"context"
	"fmt"

	cli "github.com/urfave/cli/v3"
)

func cmdThink() *cli.Command {
	return &cli.Command{
		Name:      "think",
		Usage:     "STUB: reasoning is a v0.2 feature",
		ArgsUsage: "<query>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Println("Reasoning is a v0.2 feature.")
			return nil
		},
	}
}
