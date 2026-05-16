package scripts_test

import (
	"os"
	"strings"
	"testing"
)

func TestTermuxDeployUsesCloudflareOnly(t *testing.T) {
	data, err := os.ReadFile("termux-deploy.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	for _, forbidden := range []string{
		"https://tailscale.com/install.sh",
		"tailscaled",
		"TS_SOCKET",
		"TAILSCALE=",
		"tailscale funnel",
		"Tailscale Funnel",
		"clang",
		"install -m 0755 ./coggo",
		"install -m 0755 ./coggo-oauth-gateway",
		"ENV_FILE=\"$REPO_ROOT/.env\"",
		"ENV_FILE=\"$HOME_DIR/coggo/.env\"",
		"cloudflared tunnel route dns \"$TUNNEL_NAME\" \"$CF_HOSTNAME\" || true",
		"start_if_down",
		"setsid nohup",
		"RUN_DIR=\"$HOME_DIR/.coggo/run\"",
		"for f in ~/.coggo/run/*.pid",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("termux-deploy.sh must not depend on %q", forbidden)
		}
	}
	for _, required := range []string{
		"pkg install -y cloudflared",
		"Restore an existing Coggo DB from R2 before first boot",
		"litestream restore -config scripts/litestream.yml -o \"\\$COGGO_DB_PATH\" \"\\$COGGO_DB_PATH\"",
		"make install-all",
		"APP_BIN_DIR=",
		"ENV_FILE=\"$HOME/.coggo/env\"",
		"ENV_FILE=\"$HOME_DIR/.coggo/env\"",
		"SERVICE_DIR=\"$PREFIX/var/service\"",
		"install_runit_service \"coggo\" 'exec \"$APP_BIN_DIR/coggo\" serve'",
		"install_runit_service \"coggo-gateway\" 'exec \"$APP_BIN_DIR/coggo-oauth-gateway\"'",
		"install_runit_service \"coggo-litestream\" 'exec \"$APP_BIN_DIR/litestream\" replicate",
		"install_runit_service \"coggo-cloudflared\" 'exec \"$PREFIX/bin/cloudflared\" tunnel run \"$CLOUDFLARE_TUNNEL_NAME\"'",
		". \"$PREFIX/etc/profile.d/start-services.sh\"",
		"sv-enable \"$name\"",
		"sv-disable \"$name\"",
		"sv restart coggo",
		"$PREFIX/var/log/sv/<service>/current",
		"write_cloudflared_config()",
		"cp \"$CF_CONFIG\" \"$CF_CONFIG.bak.",
		"route_output=\"$(cloudflared tunnel route dns \"$TUNNEL_NAME\" \"$CF_HOSTNAME\" 2>&1)\"",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("termux-deploy.sh must include %q", required)
		}
	}
}

func TestTermuxUpdateUsesRunitServices(t *testing.T) {
	data, err := os.ReadFile("termux-update.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	for _, forbidden := range []string{
		"RUN_DIR=",
		"kill -TERM",
		"kill -KILL",
		"*.pid",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("termux-update.sh must not depend on %q", forbidden)
		}
	}

	for _, required := range []string{
		"SERVICE_NAMES=\"coggo coggo-gateway coggo-litestream coggo-cloudflared\"",
		". \"${PREFIX:-/data/data/com.termux/files/usr}/etc/profile.d/start-services.sh\"",
		"sv restart \"$svc\"",
		"sv up \"$svc\"",
		"sv status \"$svc\"",
		"$SERVICE_LOG_DIR/$svc/current",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("termux-update.sh must include %q", required)
		}
	}
}
