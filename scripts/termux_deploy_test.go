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
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("termux-deploy.sh must not depend on %q", forbidden)
		}
	}
	for _, required := range []string{
		"pkg install -y cloudflared",
		"if [ -z \"${CLOUDFLARE_TUNNEL_NAME:-}\" ]; then",
		"start_if_down cloudflared \"$PREFIX/bin/cloudflared\" tunnel run \"$CLOUDFLARE_TUNNEL_NAME\"",
		"Restore an existing Coggo DB from R2 before first boot",
		"litestream restore -o \"\\$COGGO_DB_PATH\" -config scripts/litestream.yml",
		"make install-all",
		"APP_BIN_DIR=",
		"start_if_down coggo \"$APP_BIN_DIR/coggo\" serve",
		"start_if_down gateway \"$APP_BIN_DIR/coggo-oauth-gateway\"",
		"start_if_down litestream \"$APP_BIN_DIR/litestream\" replicate",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("termux-deploy.sh must include %q", required)
		}
	}
}
