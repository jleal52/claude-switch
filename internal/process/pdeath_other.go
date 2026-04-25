//go:build !linux

package process

import "os/exec"

// ApplyPdeathsig is a no-op on non-Linux platforms. macOS has no direct
// equivalent; Windows uses the Job Object mechanism (see job_windows.go).
func ApplyPdeathsig(_ *exec.Cmd) {}
