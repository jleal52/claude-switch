// Command claude-switch-server is the multi-tenant relay between browsers
// and wrappers. See docs/superpowers/specs/2026-04-25-server-design.md.
package main

import (
	"flag"
	"fmt"
	"os"
)

const serverVersion = "0.1.0-dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(serverVersion)
		return
	}
	fmt.Fprintln(os.Stderr, "claude-switch-server: not implemented yet (Task 1 bootstrap)")
	os.Exit(0)
}
