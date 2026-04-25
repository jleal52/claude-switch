//go:build windows

package pty

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWindowsSpawnEchoRoundTrip(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo got:hello")
	p, err := Start(cmd, Size{Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	var total []byte
	for time.Now().Before(deadline) {
		n, err := p.Read(buf)
		if n > 0 {
			total = append(total, buf[:n]...)
			if strings.Contains(string(total), "got:hello") {
				return
			}
		}
		if err != nil {
			break
		}
	}
	t.Fatalf("did not see got:hello in PTY output: %q", string(total))
}
