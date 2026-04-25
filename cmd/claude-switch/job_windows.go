//go:build windows

package main

import (
	"os/exec"

	"github.com/jleal52/claude-switch/internal/process"
)

func createJob() (jobCloser, func(cmd *exec.Cmd) error, error) {
	j, err := process.NewKillOnCloseJob()
	if err != nil {
		return nil, nil, err
	}
	return j, j.Assign, nil
}

type jobCloser interface{ Close() error }
