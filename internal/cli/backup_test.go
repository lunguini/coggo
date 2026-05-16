package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportIdentityBackupCopiesPeersWithRestrictiveMode(t *testing.T) {
	dataDir := t.TempDir()
	src := filepath.Join(dataDir, "peers.json")
	if err := os.WriteFile(src, []byte(`[{"name":"personal"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "identity", "peers.json")

	result, err := exportIdentityBackup(dataDir, dest, false)
	if err != nil {
		t.Fatalf("exportIdentityBackup returned error: %v", err)
	}
	if result.Source != src {
		t.Fatalf("Source = %q, want %q", result.Source, src)
	}
	if result.Destination != dest {
		t.Fatalf("Destination = %q, want %q", result.Destination, dest)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(got) != `[{"name":"personal"}]` {
		t.Fatalf("destination contents = %q", got)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("destination mode = %o, want 0600", gotMode)
	}
}

func TestExportIdentityBackupRefusesOverwriteWithoutForce(t *testing.T) {
	dataDir := t.TempDir()
	src := filepath.Join(dataDir, "peers.json")
	if err := os.WriteFile(src, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "peers.json")
	if err := os.WriteFile(dest, []byte(`existing`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := exportIdentityBackup(dataDir, dest, false)
	if err == nil {
		t.Fatal("exportIdentityBackup succeeded, want overwrite error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %q, want already exists", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing" {
		t.Fatalf("destination was overwritten: %q", got)
	}
}

func TestExportIdentityBackupErrorsWhenPeersMissing(t *testing.T) {
	_, err := exportIdentityBackup(t.TempDir(), filepath.Join(t.TempDir(), "peers.json"), false)
	if err == nil {
		t.Fatal("exportIdentityBackup succeeded, want missing peers error")
	}
	if !strings.Contains(err.Error(), "peers.json") {
		t.Fatalf("error = %q, want peers.json context", err)
	}
}

func TestImportIdentityBackupCopiesPeersWithRestrictiveMode(t *testing.T) {
	src := filepath.Join(t.TempDir(), "coggo-peers.json")
	if err := os.WriteFile(src, []byte(`[{"name":"business"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(t.TempDir(), "data")
	dest := filepath.Join(dataDir, "peers.json")

	result, err := importIdentityBackup(dataDir, src, false)
	if err != nil {
		t.Fatalf("importIdentityBackup returned error: %v", err)
	}
	if result.Source != src {
		t.Fatalf("Source = %q, want %q", result.Source, src)
	}
	if result.Destination != dest {
		t.Fatalf("Destination = %q, want %q", result.Destination, dest)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(got) != `[{"name":"business"}]` {
		t.Fatalf("destination contents = %q", got)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("destination mode = %o, want 0600", gotMode)
	}
}

func TestImportIdentityBackupRefusesOverwriteWithoutForce(t *testing.T) {
	src := filepath.Join(t.TempDir(), "coggo-peers.json")
	if err := os.WriteFile(src, []byte(`[{"name":"business"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	dest := filepath.Join(dataDir, "peers.json")
	if err := os.WriteFile(dest, []byte(`existing`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := importIdentityBackup(dataDir, src, false)
	if err == nil {
		t.Fatal("importIdentityBackup succeeded, want overwrite error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %q, want already exists", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing" {
		t.Fatalf("destination was overwritten: %q", got)
	}
}

func TestImportIdentityBackupErrorsWhenSourceMissing(t *testing.T) {
	_, err := importIdentityBackup(t.TempDir(), filepath.Join(t.TempDir(), "coggo-peers.json"), false)
	if err == nil {
		t.Fatal("importIdentityBackup succeeded, want missing source error")
	}
	if !strings.Contains(err.Error(), "coggo-peers.json") {
		t.Fatalf("error = %q, want source path context", err)
	}
}
