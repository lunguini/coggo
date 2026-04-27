package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Server.ListenAddress != "localhost:6177" {
		t.Errorf("listen_address = %q", c.Server.ListenAddress)
	}
	if c.Server.DataDir != "~/.local/share/coggo" {
		t.Errorf("data_dir = %q", c.Server.DataDir)
	}
	if c.Storage.Backend != "sqlite" {
		t.Errorf("backend = %q", c.Storage.Backend)
	}
	if c.Storage.EmbeddingDimension != 1024 {
		t.Errorf("embedding_dimension = %d", c.Storage.EmbeddingDimension)
	}
	if c.Embedding.Provider != "none" {
		t.Errorf("embedding provider = %q", c.Embedding.Provider)
	}
	if c.Logging.Level != "info" {
		t.Errorf("logging level = %q", c.Logging.Level)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	in := Default()
	in.Server.ListenAddress = "127.0.0.1:9999"
	in.Tailscale.Enabled = true
	in.Logging.Level = "debug"

	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Server.ListenAddress != in.Server.ListenAddress {
		t.Errorf("listen mismatch: %q vs %q", out.Server.ListenAddress, in.Server.ListenAddress)
	}
	if !out.Tailscale.Enabled {
		t.Errorf("tailscale enabled lost")
	}
	if out.Logging.Level != "debug" {
		t.Errorf("logging level lost: %q", out.Logging.Level)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if c.Server.ListenAddress != "localhost:6177" {
		t.Errorf("expected defaults; got %q", c.Server.ListenAddress)
	}
}

func TestLoadAppliesDefaultsForZeroFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[server]\nlisten_address = \"x:1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.ListenAddress != "x:1" {
		t.Errorf("listen = %q", c.Server.ListenAddress)
	}
	if c.Storage.Backend != "sqlite" {
		t.Errorf("expected default backend; got %q", c.Storage.Backend)
	}
	if c.Storage.EmbeddingDimension != 1024 {
		t.Errorf("expected default dim; got %d", c.Storage.EmbeddingDimension)
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no homedir")
	}
	cases := []struct{ in, want string }{
		{"~", home},
		{"~/foo", filepath.Join(home, "foo")},
		{"/abs/path", "/abs/path"},
		{"", ""},
	}
	for _, c := range cases {
		if got := ExpandPath(c.in); got != c.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolvedDBPath(t *testing.T) {
	c := Default()
	c.Server.DataDir = "/data"
	if got := ResolvedDBPath(c); got != "/data/coggo.db" {
		t.Errorf("got %q", got)
	}
}
