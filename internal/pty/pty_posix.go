//go:build !windows

package pty

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type posixPTY struct {
	master *os.File
	cmd    *exec.Cmd
}

func start(cmd *exec.Cmd, size Size) (PTY, error) {
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: size.Cols, Rows: size.Rows})
	if err != nil {
		return nil, err
	}
	return &posixPTY{master: master, cmd: cmd}, nil
}

func (p *posixPTY) Read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *posixPTY) Write(b []byte) (int, error) { return p.master.Write(b) }

func (p *posixPTY) Resize(s Size) error {
	return pty.Setsize(p.master, &pty.Winsize{Cols: s.Cols, Rows: s.Rows})
}

func (p *posixPTY) Close() error {
	// Kill the child first, then close the master so Read() in the session
	// reader goroutine unblocks with EOF.
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return p.master.Close()
}

func (p *posixPTY) Cmd() *exec.Cmd { return p.cmd }
