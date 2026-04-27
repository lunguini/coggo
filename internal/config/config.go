// Package config implements load/save of Coggo's TOML configuration file.
//
// The config file is optional: Load returns Default() when the file is missing.
// Defaults are filled in for any zero-valued fields after parse, so partial
// config files behave the same as empty ones for the unset fields.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Config is the on-disk configuration. See spec §9.2.
type Config struct {
	Server    ServerConfig    `toml:"server"`
	Storage   StorageConfig   `toml:"storage"`
	Embedding EmbeddingConfig `toml:"embedding"`
	Tailscale TailscaleConfig `toml:"tailscale"`
	Logging   LoggingConfig   `toml:"logging"`
}

// ServerConfig holds the listen address and data directory.
type ServerConfig struct {
	ListenAddress string `toml:"listen_address"`
	DataDir       string `toml:"data_dir"`
}

// StorageConfig holds the storage backend selection and embedding dimension.
type StorageConfig struct {
	Backend            string `toml:"backend"`
	EmbeddingDimension int    `toml:"embedding_dimension"`
}

// EmbeddingConfig holds the embedding provider settings. The API key is
// sourced from the COGGO_EMBEDDING_API_KEY environment variable, never the
// config file.
type EmbeddingConfig struct {
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
}

// TailscaleConfig holds the Tailscale integration toggles.
type TailscaleConfig struct {
	Enabled       bool `toml:"enabled"`
	FunnelEnabled bool `toml:"funnel_enabled"`
}

// LoggingConfig holds the slog level.
type LoggingConfig struct {
	Level string `toml:"level"`
}

// Default returns a Config populated with the v0.1 defaults.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddress: "localhost:6177",
			DataDir:       "~/.local/share/coggo",
		},
		Storage: StorageConfig{
			Backend:            "sqlite",
			EmbeddingDimension: 1024,
		},
		Embedding: EmbeddingConfig{
			Provider: "none",
			Model:    "",
		},
		Tailscale: TailscaleConfig{
			Enabled:       false,
			FunnelEnabled: false,
		},
		Logging: LoggingConfig{
			Level: "info",
		},
	}
}

// DefaultPath returns the preferred config-file location. XDG-compliant
// (~/.config/coggo/config.toml) with a ~/.coggo/config.toml fallback if XDG
// can't be resolved.
func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "coggo", "config.toml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "coggo", "config.toml")
	}
	return ".coggo/config.toml"
}

// Load reads a Config from disk. If the file is missing, Default() is
// returned with no error. Defaults are applied to any zero-valued fields.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	c := &Config{}
	if err := toml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	applyDefaults(c)
	return c, nil
}

// Save writes c to path atomically (tmp + rename) with mode 0644. Parent
// directories are created if missing.
func Save(path string, c *Config) error {
	if c == nil {
		return fmt.Errorf("config: nil config")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	b, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}

// ExpandPath expands a leading "~" in p to the current user's home directory.
func ExpandPath(p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// DataDir returns the expanded data directory.
func DataDir(c *Config) string {
	return ExpandPath(c.Server.DataDir)
}

// ResolvedDBPath returns the SQLite database path inside the data dir.
func ResolvedDBPath(c *Config) string {
	return filepath.Join(DataDir(c), "coggo.db")
}

// applyDefaults fills in any zero-valued fields with defaults from Default().
func applyDefaults(c *Config) {
	d := Default()
	if c.Server.ListenAddress == "" {
		c.Server.ListenAddress = d.Server.ListenAddress
	}
	if c.Server.DataDir == "" {
		c.Server.DataDir = d.Server.DataDir
	}
	if c.Storage.Backend == "" {
		c.Storage.Backend = d.Storage.Backend
	}
	if c.Storage.EmbeddingDimension == 0 {
		c.Storage.EmbeddingDimension = d.Storage.EmbeddingDimension
	}
	if c.Embedding.Provider == "" {
		c.Embedding.Provider = d.Embedding.Provider
	}
	if c.Logging.Level == "" {
		c.Logging.Level = d.Logging.Level
	}
}
