//go:build windows

package pty

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/UserExistsError/conpty"
)

type windowsPTY struct {
	cp  *conpty.ConPty
	cmd *exec.Cmd
}

func start(cmd *exec.Cmd, size Size) (PTY, error) {
	// conpty.Start takes a single command line string; reconstruct it from cmd.
	var sb strings.Builder
	sb.WriteString(quoteArg(cmd.Path))
	for _, a := range cmd.Args[1:] {
		sb.WriteByte(' ')
		sb.WriteString(quoteArg(a))
	}

	opts := []conpty.ConPtyOption{conpty.ConPtyDimensions(int(size.Cols), int(size.Rows))}
	if cmd.Dir != "" {
		opts = append(opts, conpty.ConPtyWorkDir(cmd.Dir))
	}
	if len(cmd.Env) > 0 {
		opts = append(opts, conpty.ConPtyEnv(cmd.Env))
	}

	cp, err := conpty.Start(sb.String(), opts...)
	if err != nil {
		return nil, fmt.Errorf("conpty start: %w", err)
	}

	// conpty manages its own process; expose Pid through the Cmd for callers
	// that expect *exec.Cmd. Real handle is owned by cp.
	cmd.Process = &os.Process{Pid: int(cp.Pid())}

	return &windowsPTY{cp: cp, cmd: cmd}, nil
}

func (p *windowsPTY) Read(b []byte) (int, error)  { return p.cp.Read(b) }
func (p *windowsPTY) Write(b []byte) (int, error) { return p.cp.Write(b) }

func (p *windowsPTY) Resize(s Size) error {
	return p.cp.Resize(int(s.Cols), int(s.Rows))
}

func (p *windowsPTY) Close() error {
	// Wait briefly for graceful exit then force close.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = p.cp.Wait(ctx)
	return p.cp.Close()
}

func (p *windowsPTY) Cmd() *exec.Cmd { return p.cmd }

func quoteArg(a string) string {
	if a == "" {
		return `""`
	}
	if !strings.ContainsAny(a, " \t\"") {
		return a
	}
	return `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
}
