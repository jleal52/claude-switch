// Package pty abstracts the platform-specific pseudo-terminal. A PTY wraps
// a running child process; callers read its output via io.Reader, write
// input via io.Writer, and request window-size changes via Resize.
package pty

import (
	"io"
	"os/exec"
)

// Size is the terminal viewport in character cells.
type Size struct {
	Cols uint16
	Rows uint16
}

// PTY is a running child attached to a pseudo-terminal master.
// Read() drains the child's combined stdout/stderr. Write() sends to stdin.
// Close() terminates the child and frees fds/handles.
type PTY interface {
	io.ReadWriteCloser
	Resize(Size) error
	// Cmd returns the underlying *exec.Cmd so the session layer can inspect
	// PID, wait for exit, and read ExitCode. Do not call Start() on it.
	Cmd() *exec.Cmd
}

// Start launches cmd attached to a new PTY of the requested size.
// Cmd must be configured (Path, Args, Env, Dir) but NOT yet started.
func Start(cmd *exec.Cmd, size Size) (PTY, error) {
	return start(cmd, size)
}
