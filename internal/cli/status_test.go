package cli

import (
	"strings"
	"testing"
)

func TestStatusEndpointURL(t *testing.T) {
	for _, tc := range []struct {
		addr string
		path string
		want string
	}{
		{addr: "localhost:6177", path: "/mcp", want: "http://127.0.0.1:6177/mcp"},
		{addr: ":6177", path: "/healthz", want: "http://127.0.0.1:6177/healthz"},
		{addr: "127.0.0.1:6177", path: "/mcp", want: "http://127.0.0.1:6177/mcp"},
	} {
		got := localStatusURL(tc.addr, tc.path)
		if got != tc.want {
			t.Fatalf("localStatusURL(%q, %q) = %q, want %q", tc.addr, tc.path, got, tc.want)
		}
	}
}

func TestPublicStatusURL(t *testing.T) {
	got := publicStatusURL("https://coggo.example.com/", "/healthz")
	if want := "https://coggo.example.com/healthz"; got != want {
		t.Fatalf("publicStatusURL = %q, want %q", got, want)
	}
}

func TestParseMemAvailable(t *testing.T) {
	meminfo := strings.NewReader("MemTotal:       8024000 kB\nMemAvailable:   524288 kB\n")
	got, ok := parseMemAvailable(meminfo)
	if !ok {
		t.Fatal("expected MemAvailable to be parsed")
	}
	if want := uint64(512 * 1024 * 1024); got != want {
		t.Fatalf("MemAvailable = %d, want %d", got, want)
	}
}

func TestProbeMemoryFromReader(t *testing.T) {
	status, detail := probeMemoryFromReader(strings.NewReader("MemAvailable:   524288 kB\n"))
	if status != "ok" {
		t.Fatalf("status = %q, want ok; detail=%q", status, detail)
	}
	if detail != "512.0 MiB available" {
		t.Fatalf("detail = %q, want 512.0 MiB available", detail)
	}
}

func TestFormatBytes(t *testing.T) {
	for _, tc := range []struct {
		bytes uint64
		want  string
	}{
		{bytes: 512, want: "512 B"},
		{bytes: 1536, want: "1.5 KiB"},
		{bytes: 5 * 1024 * 1024 * 1024, want: "5.0 GiB"},
	} {
		if got := formatBytes(tc.bytes); got != tc.want {
			t.Fatalf("formatBytes(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestNearestExistingPath(t *testing.T) {
	dir := t.TempDir()
	got := nearestExistingPath(dir + "/missing/nested/coggo.db")
	if got != dir {
		t.Fatalf("nearestExistingPath returned %q, want %q", got, dir)
	}
}

func TestAppIncludesStatusCommand(t *testing.T) {
	app := App()
	for _, cmd := range app.Commands {
		if cmd.Name == "status" {
			return
		}
	}
	t.Fatal("App() does not include status command")
}
