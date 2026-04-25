package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/jleal52/claude-switch/internal/auth"
)

func runPair(ctx context.Context, serverBase string) int {
	host, _ := os.Hostname()
	creds, err := auth.Pair(ctx, auth.PairConfig{
		ServerBase:  serverBase,
		WrapperName: host,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Version:     wrapperVersion,
		Announce: func(code string) {
			fmt.Printf("Pair at:  %s/pair\nCode:     %s\nWaiting...\n", serverBase, code)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pairing failed:", err)
		return 1
	}
	path, err := auth.DefaultCredentialsPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := auth.Save(path, creds); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("Paired. Credentials saved to", path)
	return 0
}
