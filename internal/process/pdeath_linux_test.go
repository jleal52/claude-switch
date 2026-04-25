//go:build linux

package process

import (
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyPdeathsigSetsSignal(t *testing.T) {
	cmd := exec.Command("/bin/true")
	ApplyPdeathsig(cmd)
	require.NotNil(t, cmd.SysProcAttr)
	require.Equal(t, syscall.SIGTERM, cmd.SysProcAttr.Pdeathsig)
}

func TestApplyPdeathsigPreservesExistingSysProcAttr(t *testing.T) {
	cmd := exec.Command("/bin/true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	ApplyPdeathsig(cmd)
	require.True(t, cmd.SysProcAttr.Setsid)
	require.Equal(t, syscall.SIGTERM, cmd.SysProcAttr.Pdeathsig)
}
