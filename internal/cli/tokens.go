package cli

import (
	"context"
	"fmt"

	cli "github.com/urfave/cli/v3"
)

func cmdToken() *cli.Command {
	return &cli.Command{
		Name:  "token",
		Usage: "Manage bearer tokens for MCP clients",
		Commands: []*cli.Command{
			{
				Name:  "create",
				Usage: "Issue a new bearer token (peer-scoped or all-peers)",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: "peer", Usage: "Peer name to authorize (repeatable)"},
					&cli.BoolFlag{Name: "all", Usage: "Authorize all peers (wildcard); supersedes --peer"},
					&cli.StringFlag{Name: "label", Usage: "Human-readable label"},
				},
				Action: actionTokenCreate,
			},
			{
				Name:   "list",
				Usage:  "List active tokens",
				Action: actionTokenList,
			},
			{
				Name:      "revoke",
				Usage:     "Revoke a token by id",
				ArgsUsage: "<id>",
				Action:    actionTokenRevoke,
			},
		},
	}
}

func actionTokenCreate(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	rt, err := Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	label := cmd.String("label")

	var peers []string
	scopeNote := ""
	switch {
	case cmd.Bool("all"):
		peers = []string{"*"}
		scopeNote = "all peers (wildcard)"
	case len(cmd.StringSlice("peer")) > 0:
		peers = cmd.StringSlice("peer")
		// Verify each peer exists so users get a clear error before they try to use the token.
		for _, name := range peers {
			if rt.Registry.ByName(name) == nil {
				return fmt.Errorf("token create: no peer named %q (use `coggo peer list` to see available)", name)
			}
		}
		scopeNote = fmt.Sprintf("peers %v", peers)
	default:
		return fmt.Errorf("token create: provide --peer <name> (repeatable) or --all")
	}

	id, secret, err := rt.Auth.Issue(ctx, peers, label)
	if err != nil {
		return err
	}
	fmt.Println("Save this secret now — it will not be shown again.")
	fmt.Printf("  secret: %s\n", secret)
	fmt.Printf("  id:     %s\n", id)
	fmt.Printf("  scope:  %s\n", scopeNote)
	return nil
}

func actionTokenList(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	rt, err := Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()
	toks, err := rt.Auth.List(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%-30s\t%-30s\t%-20s\t%-20s\t%s\n", "ID", "PEERS", "LABEL", "CREATED", "LAST USED")
	for _, t := range toks {
		last := "(never)"
		if !t.LastUsedAt.IsZero() {
			last = t.LastUsedAt.Format("2006-01-02 15:04")
		}
		fmt.Printf("%-30s\t%-30v\t%-20s\t%-20s\t%s\n",
			t.ID, t.Peers, t.Label, t.CreatedAt.Format("2006-01-02 15:04"), last)
	}
	return nil
}

func actionTokenRevoke(ctx context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return fmt.Errorf("token revoke: id required")
	}
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	rt, err := Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()
	if err := rt.Auth.Revoke(ctx, id); err != nil {
		return err
	}
	fmt.Printf("Revoked %s\n", id)
	return nil
}
