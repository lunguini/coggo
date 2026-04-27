package cli

import (
	"context"
	"fmt"
	"os"

	cli "github.com/urfave/cli/v3"

	"github.com/lunguini/coggo/internal/types"
)

func cmdType() *cli.Command {
	return &cli.Command{
		Name:  "type",
		Usage: "Manage entity and relationship type definitions",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List types (optionally filtered by --peer)",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "peer", Usage: "Limit to one peer"},
				},
				Action: actionTypeList,
			},
			{
				Name:   "add",
				Usage:  "Define a new type interactively",
				Action: actionTypeAdd,
			},
			{
				Name:      "show",
				Usage:     "Describe a type",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "peer", Usage: "Peer the type belongs to"},
					&cli.BoolFlag{Name: "relationship", Usage: "Look up a relationship type"},
				},
				Action: actionTypeShow,
			},
		},
	}
}

func actionTypeList(ctx context.Context, cmd *cli.Command) error {
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
	peers := rt.Registry.List()
	if peerName != "" {
		p, err := rt.Registry.Resolve(peerName)
		if err != nil {
			return err
		}
		peers = []*types.Peer{p}
	}
	for _, p := range peers {
		fmt.Printf("\n[%s] %s\n", p.Name, p.DID)
		ets := rt.Resolver.EntityTypes(p.DID)
		fmt.Printf("  Entity types (%d):\n", len(ets))
		for _, t := range ets {
			fmt.Printf("    - %s — %s\n", t.Name, t.Description)
		}
		rts := rt.Resolver.RelationTypes(p.DID)
		fmt.Printf("  Relationship types (%d):\n", len(rts))
		for _, t := range rts {
			fmt.Printf("    - %s — %s\n", t.Name, t.Description)
		}
	}
	return nil
}

func actionTypeAdd(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	rt, err := Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	if len(rt.Registry.List()) == 0 {
		return fmt.Errorf("type add: no peers; run `coggo init` first")
	}

	p := newPrompter(os.Stdin, os.Stdout)
	defaultPeer := cmd.Root().String("peer")
	if defaultPeer == "" {
		defaultPeer = rt.Registry.List()[0].Name
	}
	peerName := p.ask("Peer", defaultPeer)
	peerObj, err := rt.Registry.Resolve(peerName)
	if err != nil {
		return err
	}

	name := p.ask("Type name", "")
	if name == "" {
		return fmt.Errorf("type add: name required")
	}
	desc := p.ask("Description", "")
	isRel := p.askYN("Is this a relationship type?", false)
	directional := false
	if isRel {
		directional = p.askYN("Directional?", true)
	}

	var fields []types.FieldDef
	for {
		if !p.askYN("Add a field?", true) {
			break
		}
		fname := p.ask("  field name", "")
		if fname == "" {
			break
		}
		ftype := p.ask("  field type [string|number|boolean|timestamp|reference|list_of]", "string")
		required := p.askYN("  required?", false)
		fdesc := p.ask("  description (optional)", "")
		fd := types.FieldDef{
			Name: fname, Type: types.FieldType(ftype),
			Required: required, Description: fdesc,
		}
		if fd.Type == types.FieldListOf {
			el := p.ask("  list element type [string|number|boolean|timestamp|reference]", "string")
			fd.ElementType = types.FieldType(el)
		}
		fields = append(fields, fd)
	}

	if err := DefineType(ctx, rt, peerObj.DID, name, desc, fields, isRel, directional, "cli"); err != nil {
		return fmt.Errorf("type add: %w", err)
	}
	fmt.Printf("Defined type %q in peer %s\n", name, peerObj.Name)
	return nil
}

func actionTypeShow(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return fmt.Errorf("type show: name required")
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

	peerName := cmd.String("peer")
	if peerName == "" {
		peerName = cmd.Root().String("peer")
	}
	peerObj, err := resolvePeer(rt, peerName, "")
	if err != nil {
		return err
	}

	if cmd.Bool("relationship") {
		def, err := rt.Resolver.RelationType(peerObj.DID, name)
		if err != nil {
			return err
		}
		printRelType(def)
		return nil
	}
	def, err := rt.Resolver.EntityType(peerObj.DID, name)
	if err != nil {
		return err
	}
	printEntityType(def)
	return nil
}

func printEntityType(def *types.EntityTypeDefinition) {
	fmt.Printf("Entity type: %s\n", def.Name)
	fmt.Printf("  Peer: %s\n", def.PeerDID)
	fmt.Printf("  Description: %s\n", def.Description)
	fmt.Printf("  Fields:\n")
	for _, f := range def.Fields {
		req := ""
		if f.Required {
			req = " (required)"
		}
		fmt.Printf("    - %s [%s]%s — %s\n", f.Name, f.Type, req, f.Description)
	}
}

func printRelType(def *types.RelationshipTypeDefinition) {
	fmt.Printf("Relationship type: %s\n", def.Name)
	fmt.Printf("  Peer: %s\n", def.PeerDID)
	fmt.Printf("  Directional: %v\n", def.Directional)
	fmt.Printf("  Description: %s\n", def.Description)
	for _, f := range def.Fields {
		req := ""
		if f.Required {
			req = " (required)"
		}
		fmt.Printf("    - %s [%s]%s — %s\n", f.Name, f.Type, req, f.Description)
	}
}
