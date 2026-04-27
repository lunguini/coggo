package cli

import (
	"context"
	"fmt"
	"os"

	cli "github.com/urfave/cli/v3"

	"github.com/lunguini/coggo/internal/peer"
	"github.com/lunguini/coggo/internal/types"
)

func cmdPeer() *cli.Command {
	return &cli.Command{
		Name:  "peer",
		Usage: "Manage peers (identities hosted by this binary)",
		Commands: []*cli.Command{
			{
				Name:      "add",
				Usage:     "Add a new peer",
				ArgsUsage: "<name>",
				Action:    actionPeerAdd,
			},
			{
				Name:   "list",
				Usage:  "List all peers",
				Action: actionPeerList,
			},
			{
				Name:      "info",
				Usage:     "Show details about a peer",
				ArgsUsage: "<name>",
				Action:    actionPeerInfo,
			},
			{
				Name:      "rename",
				Usage:     "Rename a peer",
				ArgsUsage: "<old> <new>",
				Action:    actionPeerRename,
			},
		},
	}
}

func actionPeerAdd(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return fmt.Errorf("peer add: name required")
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

	p := newPrompter(os.Stdin, os.Stdout)
	desc := p.ask("Description (optional)", "")

	np, err := peer.NewPeer(name, desc)
	if err != nil {
		return fmt.Errorf("peer add: %w", err)
	}
	if err := rt.Registry.Add(np); err != nil {
		return fmt.Errorf("peer add: %w", err)
	}
	if err := AddPeerToRuntime(ctx, rt, np, "cli"); err != nil {
		return fmt.Errorf("peer add: wire: %w", err)
	}
	fmt.Printf("Created peer %q\n  DID: %s\n", np.Name, np.DID)
	return nil
}

func actionPeerList(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	rt, err := Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	peers := rt.Registry.List()
	if len(peers) == 0 {
		fmt.Println("(no peers — run `coggo init`)")
		return nil
	}
	fmt.Printf("%-20s\t%-60s\t%s\n", "NAME", "DID", "CREATED")
	for _, p := range peers {
		fmt.Printf("%-20s\t%-60s\t%s\n", p.Name, p.DID, p.CreatedAt.Format("2006-01-02"))
	}
	return nil
}

func actionPeerInfo(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return fmt.Errorf("peer info: name required")
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

	p, err := rt.Registry.Resolve(name)
	if err != nil {
		return err
	}
	fmt.Printf("Name:        %s\n", p.Name)
	fmt.Printf("DID:         %s\n", p.DID)
	fmt.Printf("Description: %s\n", p.Description)
	fmt.Printf("Created:     %s\n", p.CreatedAt.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Settings:    briefing_time=%q frequency=%q capture=%q\n",
		p.Settings.BriefingTime, p.Settings.BriefingFrequency, p.Settings.CaptureConfirmation)

	// Counts
	allTypes := []string{"Project", "Goal", "Decision", "Domain", "Observation", "Setting", "EntityTypeDefinition", "RelationshipTypeDefinition"}
	totalEnt := 0
	for _, t := range allTypes {
		es, err := rt.Store.QueryEntities(ctx, p.DID, types.EntityQuery{Type: t})
		if err != nil {
			continue
		}
		totalEnt += len(es)
	}
	fmt.Printf("Entities:    %d\n", totalEnt)

	if evs, err := rt.Store.ListEvents(ctx, p.DID, p.CreatedAt.Add(-1), p.CreatedAt.AddDate(100, 0, 0)); err == nil {
		fmt.Printf("Events:      %d\n", len(evs))
	}
	return nil
}

func actionPeerRename(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() < 2 {
		return fmt.Errorf("peer rename: <old> <new> required")
	}
	old := cmd.Args().Get(0)
	newName := cmd.Args().Get(1)
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	rt, err := Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()
	if err := rt.Registry.Rename(old, newName); err != nil {
		return err
	}
	fmt.Printf("Renamed %s -> %s\n", old, newName)
	return nil
}
