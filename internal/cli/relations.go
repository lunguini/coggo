package cli

import (
	"context"
	"fmt"
	"os"

	cli "github.com/urfave/cli/v3"
)

func cmdRelation() *cli.Command {
	return &cli.Command{
		Name:  "relation",
		Usage: "Manage entity relationships",
		Commands: []*cli.Command{
			{
				Name:  "new",
				Usage: "Create a relationship interactively",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "peer", Usage: "Peer to create the relation in"},
				},
				Action: actionRelationNew,
			},
		},
	}
}

func actionRelationNew(ctx context.Context, cmd *cli.Command) error {
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
	if peerName == "" {
		peerName = cmd.Root().String("peer")
	}
	peerObj, err := resolvePeer(rt, peerName, "")
	if err != nil {
		return err
	}
	p := newPrompter(os.Stdin, os.Stdout)
	from := p.ask("from (entity id)", "")
	to := p.ask("to (entity id)", "")
	relType := p.ask("relationship type", "depends_on")
	if from == "" || to == "" || relType == "" {
		return fmt.Errorf("relation new: from/to/type required")
	}
	def, err := rt.Resolver.RelationType(peerObj.DID, relType)
	if err != nil {
		return err
	}
	data := map[string]any{}
	for _, f := range def.Fields {
		raw := p.ask(promptLabel(f), defaultStr(f.Default))
		if raw == "" {
			continue
		}
		v, err := coerceField(raw, f)
		if err != nil {
			return err
		}
		data[f.Name] = v
	}
	r, err := CreateRelation(ctx, rt, peerObj.DID, from, to, relType, data, "cli")
	if err != nil {
		return err
	}
	fmt.Printf("Created relation %s: %s -[%s]-> %s\n", r.ID, r.From, r.Type, r.To)
	return nil
}
