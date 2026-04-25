//go:build !windows

package pty

import (
	"bufio"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPOSIXSpawnEchoRoundTrip(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "read line; echo got:$line")
	p, err := Start(cmd, Size{Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.Write([]byte("hello\n"))
	require.NoError(t, err)

	r := bufio.NewReader(p)
	deadline := time.Now().Add(3 * time.Second)
	var line string
	for time.Now().Before(deadline) {
		s, err := r.ReadString('\n')
		if err == nil && len(s) > 0 {
			line = s
			if containsGotHello(line) {
				break
			}
		}
	}
	require.Contains(t, line, "got:hello")
}

func containsGotHello(s string) bool {
	for i := 0; i+7 <= len(s); i++ {
		if s[i:i+7] == "got:hel" {
			return true
		}
	}
	return false
}
