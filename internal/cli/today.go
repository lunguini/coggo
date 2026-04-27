package cli

import (
	"context"
	"fmt"
	"time"

	cli "github.com/urfave/cli/v3"

	"github.com/lunguini/coggo/internal/briefing"
)

func cmdToday() *cli.Command {
	return &cli.Command{
		Name:  "today",
		Usage: "Print the daily briefing across all peers",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "peer", Usage: "Limit briefing to one peer"},
		},
		Action: actionToday,
	}
}

func actionToday(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	rt, err := Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	peerName := cmd.String("peer")
	b, err := briefing.Generate(ctx, rt.Registry, rt.Store, peerName, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("today: %w", err)
	}
	fmt.Print(briefing.Render(b))
	return nil
}
