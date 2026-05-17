//go:build windows

package cli

func probeDisk(string) (string, string) {
	return "unknown", "disk usage check is not supported on windows"
}
