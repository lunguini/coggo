package cli

import (
	"context"
	"fmt"
	"os"

	cli "github.com/urfave/cli/v3"

	"github.com/lunguini/coggo/internal/briefing"
	"github.com/lunguini/coggo/internal/config"
	"github.com/lunguini/coggo/internal/peer"
	"github.com/lunguini/coggo/internal/types"
)

func cmdInit() *cli.Command {
	return &cli.Command{
		Name:   "init",
		Usage:  "First-run interactive setup",
		Action: actionInit,
	}
}

func actionInit(ctx context.Context, cmd *cli.Command) error {
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

	fmt.Println("Welcome to Coggo.")
	fmt.Println("")
	fmt.Println("Coggo can help you set up faster by examining existing tools you use.")
	fmt.Println("Each step asks separate permission.")
	fmt.Println("")
	if p.askYN("Should I look at your AI tool config files to find existing projects?", false) {
		fmt.Println("Import wizard is a v0.2 feature. Continuing with manual setup.")
	}

	fmt.Println("")
	fmt.Println("Discovery wizard ships in v0.2; you'll set up peers manually here.")
	fmt.Println("")

	// Step 3: define peers — personal, business, coggo
	fmt.Println("I'll create three peers by default. Press Enter to accept each, n to skip.")
	for _, spec := range []struct {
		name, desc string
	}{
		{"personal", "Life domains (health, finance, music, social)"},
		{"business", "Work and projects"},
		{briefing.CoggoPeerName, "Coggo's own state and self-knowledge"},
	} {
		if rt.Registry.ByName(spec.name) != nil {
			fmt.Printf("  peer %q already exists; skipping.\n", spec.name)
			continue
		}
		if !p.askYN(fmt.Sprintf("Create peer %q (%s)?", spec.name, spec.desc), true) {
			continue
		}
		np, err := peer.NewPeer(spec.name, spec.desc)
		if err != nil {
			return fmt.Errorf("init: new peer %s: %w", spec.name, err)
		}
		if err := rt.Registry.Add(np); err != nil {
			return fmt.Errorf("init: register %s: %w", spec.name, err)
		}
		if err := AddPeerToRuntime(ctx, rt, np, "cli-init"); err != nil {
			return fmt.Errorf("init: wire %s: %w", spec.name, err)
		}
		fmt.Printf("  created %s (%s)\n", np.Name, np.DID)
	}

	// Step 4: correlate items
	fmt.Println("")
	fmt.Println("Skipping correlation for now (v0.2 feature). You can add entities later via `coggo entity new`.")

	// Step 5: settings.
	fmt.Println("")
	briefingTime := p.ask("Daily briefing time? [morning|evening|on-demand]", "on-demand")
	defaultPeer := p.ask("Default peer for ambiguous commands? [personal|business|coggo]", "business")
	captureConf := p.ask("Capture confirmation? [always_confirm|log_and_tell|log_silently]", "log_and_tell")

	// Persist as Setting entities under the coggo peer (best effort).
	coggoPeer := rt.Registry.ByName(briefing.CoggoPeerName)
	if coggoPeer != nil {
		for _, kv := range []struct{ k, v string }{
			{"briefing_time", briefingTime},
			{"default_peer", defaultPeer},
			{"capture_confirmation", captureConf},
		} {
			_, _ = CreateEntity(ctx, rt, coggoPeer.DID, "Setting", map[string]any{
				"key": kv.k, "value": kv.v, "scope": "global",
			}, "cli-init")
		}
	}

	// Update per-peer settings on each peer.
	for _, peerName := range []string{"personal", "business", briefing.CoggoPeerName} {
		if rt.Registry.ByName(peerName) == nil {
			continue
		}
		_ = rt.Registry.UpdateSettings(peerName, types.PeerSettings{
			BriefingTime:        briefingTime,
			BriefingFrequency:   "on_demand",
			CaptureConfirmation: captureConf,
		})
	}

	// Step 6: exposure guidance.
	fmt.Println("")
	fmt.Println("Coggo is local-only by default. For public remote access, put")
	fmt.Println("the OAuth gateway behind Cloudflare Tunnel:")
	fmt.Println("  see docs/cloudflare-tunnel.md and docs/claude-ai-setup.md")

	// Save config to disk if missing.
	cfgPath := configPath(cmd)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := config.Save(cfgPath, cfg); err != nil {
			return fmt.Errorf("init: write config: %w", err)
		}
		fmt.Printf("Wrote default config to %s\n", cfgPath)
	}

	// Step 7: completion.
	fmt.Println("")
	fmt.Println("Setup complete.")
	fmt.Printf("  Local: http://%s\n", cfg.Server.ListenAddress)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  1. Run `coggo today` to see your first daily briefing.")
	fmt.Println("  2. Try `coggo decision new` to log your first decision.")
	fmt.Println("  3. Configure claude.ai with the Coggo skill: see docs/claude-ai-setup.md")
	fmt.Println("  4. Add Coggo to Claude Code: see docs/claude-code-setup.md")
	fmt.Println("")
	fmt.Println("Run `coggo help` for the full command list.")
	return nil
}
