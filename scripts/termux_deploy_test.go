package scripts_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestTermuxDeployInstallsTailscaleWithUpstreamInstaller(t *testing.T) {
	data, err := os.ReadFile("termux-deploy.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	pkgInstallBlock := regexp.MustCompile(`(?s)pkg install -y \\\n(.*?)\n\n`).FindStringSubmatch(script)
	if len(pkgInstallBlock) != 2 {
		t.Fatal("could not find primary pkg install block")
	}
	if strings.Contains(pkgInstallBlock[1], "tailscale") {
		t.Fatal("Termux pkg install block must not include tailscale; Termux apt cannot locate that package")
	}
	if !strings.Contains(script, "https://tailscale.com/install.sh") {
		t.Fatal("termux-deploy.sh must install Tailscale via https://tailscale.com/install.sh")
	}
}
