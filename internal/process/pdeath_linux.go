//go:build linux

// Package process centralises OS-specific wiring that ties children to
// the wrapper's lifetime (PDEATHSIG on Linux, Job Objects on Windows).
package process

import (
	"os/exec"
	"syscall"
)

// ApplyPdeathsig configures cmd so the kernel sends SIGTERM to the child
// when the current (wrapper) process dies for any reason. Idempotent.
func ApplyPdeathsig(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}
