package cli

import (
	"context"
	"fmt"

	cli "github.com/urfave/cli/v3"
)

// alias bundles a top-level convenience command (e.g. `coggo decision new`)
// that delegates to `coggo entity new <Type>` with a default peer.
func aliasCmd(name, typeName, defaultPeer string) *cli.Command {
	return &cli.Command{
		Name:  name,
		Usage: fmt.Sprintf("Convenience alias for `coggo entity new %s`", typeName),
		Commands: []*cli.Command{
			{
				Name:  "new",
				Usage: fmt.Sprintf("Create a new %s entity (defaults to peer %q)", typeName, defaultPeer),
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "peer", Usage: "Override the default peer"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runEntityNew(ctx, cmd, typeName, defaultPeer)
				},
			},
		},
	}
}

func cmdAliasDecision() *cli.Command    { return aliasCmd("decision", "Decision", "business") }
func cmdAliasGoal() *cli.Command        { return aliasCmd("goal", "Goal", "personal") }
func cmdAliasObservation() *cli.Command { return aliasCmd("observation", "Observation", "personal") }
func cmdAliasProject() *cli.Command     { return aliasCmd("project", "Project", "business") }
func cmdAliasDomain() *cli.Command      { return aliasCmd("domain", "Domain", "personal") }
