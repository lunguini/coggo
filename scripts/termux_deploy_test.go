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
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("termux-deploy.sh must not depend on %q", forbidden)
		}
	}
	for _, required := range []string{
		"pkg install -y cloudflared",
		"if [ -z \"${CLOUDFLARE_TUNNEL_NAME:-}\" ]; then",
		"start_if_down cloudflared \"$PREFIX/bin/cloudflared\" tunnel run \"$CLOUDFLARE_TUNNEL_NAME\"",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("termux-deploy.sh must include %q", required)
		}
	}
}
