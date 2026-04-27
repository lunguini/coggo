package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	cli "github.com/urfave/cli/v3"

	"github.com/lunguini/coggo/internal/types"
)

func cmdEntity() *cli.Command {
	return &cli.Command{
		Name:  "entity",
		Usage: "Create, list, view, and update entities",
		Commands: []*cli.Command{
			{
				Name:      "new",
				Usage:     "Create an entity interactively",
				ArgsUsage: "<type>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "peer", Usage: "Peer to create the entity in"},
				},
				Action: actionEntityNew,
			},
			{
				Name:      "list",
				Usage:     "List entities of a type",
				ArgsUsage: "<type>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "peer", Usage: "Peer to list from"},
				},
				Action: actionEntityList,
			},
			{
				Name:      "show",
				Usage:     "Show one entity",
				ArgsUsage: "<id>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "peer", Usage: "Peer the entity belongs to"},
				},
				Action: actionEntityShow,
			},
			{
				Name:      "update",
				Usage:     "Update an entity interactively",
				ArgsUsage: "<id>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "peer", Usage: "Peer the entity belongs to"},
				},
				Action: actionEntityUpdate,
			},
		},
	}
}

// runEntityNew is the shared implementation used by `entity new` and the
// alias commands (decision new, goal new, etc.).
func runEntityNew(ctx context.Context, cmd *cli.Command, typ, fallbackPeer string) error {
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
	peerObj, err := resolvePeer(rt, peerName, fallbackPeer)
	if err != nil {
		return err
	}

	def, err := rt.Resolver.EntityType(peerObj.DID, typ)
	if err != nil {
		return fmt.Errorf("entity new: type %q not defined in peer %s; run `coggo type add` or `coggo type list`", typ, peerObj.Name)
	}

	fmt.Printf("Creating entity of type %s in peer %s.\n\n", typ, peerObj.Name)
	p := newPrompter(os.Stdin, os.Stdout)

	fields := map[string]any{}
	for _, f := range def.Fields {
		label := promptLabel(f)
		raw := p.ask(label, defaultStr(f.Default))
		if raw == "" {
			if f.Default != nil {
				fields[f.Name] = f.Default
			}
			continue
		}
		v, err := coerceField(raw, f)
		if err != nil {
			return fmt.Errorf("entity new: %s: %w", f.Name, err)
		}
		fields[f.Name] = v
	}

	e, err := CreateEntity(ctx, rt, peerObj.DID, typ, fields, "cli")
	if err != nil {
		return fmt.Errorf("entity new: %w", err)
	}
	fmt.Printf("\nCreated %s: %s in peer %s\n", strings.ToLower(typ), e.ID, peerObj.Name)
	if title, ok := e.Data["title"].(string); ok && title != "" {
		fmt.Printf("  Title: %s\n", title)
	}
	fmt.Printf("  Created at: %s\n", e.CreatedAt.Format("2006-01-02 15:04:05"))

	if p.askYN("\nAdd a relationship?", false) {
		toID := p.ask("  to (entity id)", "")
		relType := p.ask("  relationship type", "depends_on")
		if toID != "" && relType != "" {
			if _, err := CreateRelation(ctx, rt, peerObj.DID, e.ID, toID, relType, nil, "cli"); err != nil {
				fmt.Printf("  (relation failed: %v)\n", err)
			} else {
				fmt.Println("  relation created.")
			}
		}
	}
	return nil
}

func actionEntityNew(ctx context.Context, cmd *cli.Command) error {
	typ := cmd.Args().First()
	if typ == "" {
		return fmt.Errorf("entity new: type required")
	}
	return runEntityNew(ctx, cmd, typ, "")
}

func actionEntityList(ctx context.Context, cmd *cli.Command) error {
	typ := cmd.Args().First()
	if typ == "" {
		return fmt.Errorf("entity list: type required")
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

	var peers []*types.Peer
	if peerName == "" {
		peers = rt.Registry.List()
	} else {
		p, err := rt.Registry.Resolve(peerName)
		if err != nil {
			return err
		}
		peers = []*types.Peer{p}
	}
	for _, p := range peers {
		es, err := QueryEntities(ctx, rt, p.DID, types.EntityQuery{Type: typ})
		if err != nil {
			return err
		}
		fmt.Printf("[%s] %d %s entities:\n", p.Name, len(es), typ)
		for _, e := range es {
			title := summary(e)
			fmt.Printf("  %s  %s  %s\n", e.ID, e.CreatedAt.Format("2006-01-02"), title)
		}
	}
	return nil
}

func actionEntityShow(ctx context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return fmt.Errorf("entity show: id required")
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
	if peerName == "" {
		return fmt.Errorf("entity show: --peer required (v0.1)")
	}
	peerObj, err := rt.Registry.Resolve(peerName)
	if err != nil {
		return err
	}
	e, err := GetEntity(ctx, rt, peerObj.DID, id)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(e, "", "  ")
	fmt.Println(string(b))
	return nil
}

func actionEntityUpdate(ctx context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return fmt.Errorf("entity update: id required")
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
	if peerName == "" {
		return fmt.Errorf("entity update: --peer required")
	}
	peerObj, err := rt.Registry.Resolve(peerName)
	if err != nil {
		return err
	}
	cur, err := GetEntity(ctx, rt, peerObj.DID, id)
	if err != nil {
		return err
	}
	def, err := rt.Resolver.EntityType(peerObj.DID, cur.Type)
	if err != nil {
		return err
	}
	p := newPrompter(os.Stdin, os.Stdout)
	updates := map[string]any{}
	for _, f := range def.Fields {
		curVal := ""
		if v, ok := cur.Data[f.Name]; ok {
			curVal = fmt.Sprintf("%v", v)
		}
		raw := p.ask(promptLabel(f), curVal)
		if raw == "" || raw == curVal {
			continue
		}
		v, err := coerceField(raw, f)
		if err != nil {
			return err
		}
		updates[f.Name] = v
	}
	if len(updates) == 0 {
		fmt.Println("(no changes)")
		return nil
	}
	e, err := UpdateEntity(ctx, rt, peerObj.DID, id, updates, "cli")
	if err != nil {
		return err
	}
	fmt.Printf("Updated %s\n", e.ID)
	return nil
}

// ---- helpers ----

func promptLabel(f types.FieldDef) string {
	req := "optional"
	if f.Required {
		req = "required"
	}
	if f.Description != "" {
		return fmt.Sprintf("%s (%s — %s)", f.Name, req, f.Description)
	}
	return fmt.Sprintf("%s (%s)", f.Name, req)
}

func defaultStr(d any) string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%v", d)
}

// coerceField parses a textual prompt value into the field's declared type.
func coerceField(raw string, f types.FieldDef) (any, error) {
	switch f.Type {
	case types.FieldString, types.FieldReference, types.FieldTimestamp:
		return raw, nil
	case types.FieldBoolean:
		v := strings.ToLower(raw)
		return v == "y" || v == "yes" || v == "true" || v == "1", nil
	case types.FieldNumber:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("not a number: %s", raw)
		}
		return n, nil
	case types.FieldListOf:
		parts := strings.Split(raw, ",")
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			out = append(out, strings.TrimSpace(p))
		}
		return out, nil
	}
	return raw, nil
}

func summary(e *types.Entity) string {
	for _, key := range []string{"title", "name", "text"} {
		if v, ok := e.Data[key].(string); ok && v != "" {
			if len(v) > 80 {
				return v[:77] + "..."
			}
			return v
		}
	}
	return ""
}
