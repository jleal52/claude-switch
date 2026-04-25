//go:build !windows

package main

import "os/exec"

func createJob() (jobCloser, func(cmd *exec.Cmd) error, error) {
	return noopCloser{}, func(*exec.Cmd) error { return nil }, nil
}

type jobCloser interface{ Close() error }

type noopCloser struct{}

func (noopCloser) Close() error { return nil }
