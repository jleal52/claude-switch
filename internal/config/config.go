// Package config merges TOML file, environment variables, and defaults.
// CLI flags (handled in main) override config file values.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

// Config holds all runtime configuration for claude-switch.
type Config struct {
	ServerURL         string `toml:"server_url"           env:"CLAUDE_SWITCH_SERVER_URL"`
	LogLevel          string `toml:"log_level"            env:"CLAUDE_SWITCH_LOG_LEVEL"`
	LogFile           string `toml:"log_file"             env:"CLAUDE_SWITCH_LOG_FILE"`
	PTYDataFlushMs    int    `toml:"pty_data_flush_ms"    env:"CLAUDE_SWITCH_PTY_FLUSH_MS"`
	PTYDataFlushBytes int    `toml:"pty_data_flush_bytes" env:"CLAUDE_SWITCH_PTY_FLUSH_BYTES"`
	DefaultCols       uint16 `toml:"default_cols"         env:"CLAUDE_SWITCH_DEFAULT_COLS"`
	DefaultRows       uint16 `toml:"default_rows"         env:"CLAUDE_SWITCH_DEFAULT_ROWS"`
}

func defaults() Config {
	return Config{
		LogLevel:          "info",
		PTYDataFlushMs:    16,
		PTYDataFlushBytes: 16 * 1024,
		DefaultCols:       120,
		DefaultRows:       32,
	}
}

// LoadFromPath reads the TOML file at path (if non-empty), merges env vars
// on top, and fills defaults for anything still zero.
func LoadFromPath(path string) (*Config, error) {
	cfg := defaults()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("read %s: %w", path, err)
			}
		} else {
			var fromFile Config
			if err := toml.Unmarshal(b, &fromFile); err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
			mergeNonZero(&cfg, &fromFile)
		}
	}
	applyEnv(&cfg)
	return &cfg, nil
}

// DefaultPath returns the OS-specific config path.
func DefaultPath() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("AppData")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "claude-switch", "config.toml"), nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "claude-switch", "config.toml"), nil
}

func mergeNonZero(dst, src *Config) {
	if src.ServerURL != "" {
		dst.ServerURL = src.ServerURL
	}
	if src.LogLevel != "" {
		dst.LogLevel = src.LogLevel
	}
	if src.LogFile != "" {
		dst.LogFile = src.LogFile
	}
	if src.PTYDataFlushMs != 0 {
		dst.PTYDataFlushMs = src.PTYDataFlushMs
	}
	if src.PTYDataFlushBytes != 0 {
		dst.PTYDataFlushBytes = src.PTYDataFlushBytes
	}
	if src.DefaultCols != 0 {
		dst.DefaultCols = src.DefaultCols
	}
	if src.DefaultRows != 0 {
		dst.DefaultRows = src.DefaultRows
	}
}

func applyEnv(dst *Config) {
	if v := os.Getenv("CLAUDE_SWITCH_SERVER_URL"); v != "" {
		dst.ServerURL = v
	}
	if v := os.Getenv("CLAUDE_SWITCH_LOG_LEVEL"); v != "" {
		dst.LogLevel = v
	}
	if v := os.Getenv("CLAUDE_SWITCH_LOG_FILE"); v != "" {
		dst.LogFile = v
	}
	if v := os.Getenv("CLAUDE_SWITCH_PTY_FLUSH_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			dst.PTYDataFlushMs = n
		}
	}
	if v := os.Getenv("CLAUDE_SWITCH_PTY_FLUSH_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			dst.PTYDataFlushBytes = n
		}
	}
	if v := os.Getenv("CLAUDE_SWITCH_DEFAULT_COLS"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 16); err == nil {
			dst.DefaultCols = uint16(n)
		}
	}
	if v := os.Getenv("CLAUDE_SWITCH_DEFAULT_ROWS"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 16); err == nil {
			dst.DefaultRows = uint16(n)
		}
	}
}
