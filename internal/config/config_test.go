package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
server_url = "wss://example.com/ws"
log_level = "debug"
pty_data_flush_ms = 32
pty_data_flush_bytes = 8192
default_cols = 100
default_rows = 40
`), 0o644))

	cfg, err := LoadFromPath(path)
	require.NoError(t, err)
	require.Equal(t, "wss://example.com/ws", cfg.ServerURL)
	require.Equal(t, "debug", cfg.LogLevel)
	require.Equal(t, 32, cfg.PTYDataFlushMs)
	require.Equal(t, 8192, cfg.PTYDataFlushBytes)
	require.Equal(t, uint16(100), cfg.DefaultCols)
	require.Equal(t, uint16(40), cfg.DefaultRows)
}

func TestDefaultsApplied(t *testing.T) {
	cfg, err := LoadFromPath("") // empty path -> all defaults
	require.NoError(t, err)
	require.Equal(t, "info", cfg.LogLevel)
	require.Equal(t, 16, cfg.PTYDataFlushMs)
	require.Equal(t, 16384, cfg.PTYDataFlushBytes)
	require.Equal(t, uint16(120), cfg.DefaultCols)
	require.Equal(t, uint16(32), cfg.DefaultRows)
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	require.NoError(t, os.WriteFile(path, []byte(`server_url = "wss://file"`), 0o644))
	t.Setenv("CLAUDE_SWITCH_SERVER_URL", "wss://env")

	cfg, err := LoadFromPath(path)
	require.NoError(t, err)
	require.Equal(t, "wss://env", cfg.ServerURL)
}
