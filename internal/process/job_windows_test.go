//go:build windows

package process

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJobKillsChildWhenJobCloses(t *testing.T) {
	job, err := NewKillOnCloseJob()
	require.NoError(t, err)

	cmd := exec.Command("cmd.exe", "/c", "ping -n 60 127.0.0.1 >nul")
	require.NoError(t, cmd.Start())
	require.NoError(t, job.Assign(cmd))

	// Close the job: child must die.
	require.NoError(t, job.Close())

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		// Exited promptly — expected.
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("child survived job close")
	}
}
