// Package tail discovers and tails the .jsonl file backing a claude session.
// Claude Code writes one file per session under ~/.claude/projects/<slug>/.
// The wrapper correlates its freshly-spawned child with the jsonl that
// appeared in that directory after the child started.
package tail

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WaitForNewJSONL polls projectDir for a *.jsonl created at or after notBefore.
// Returns the first match or ctx.Err() on timeout.
func WaitForNewJSONL(ctx context.Context, projectDir string, notBefore time.Time) (string, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			if p := scanNewest(projectDir, notBefore); p != "" {
				return p, nil
			}
		}
	}
}

func scanNewest(dir string, notBefore time.Time) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(notBefore) {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best = filepath.Join(dir, e.Name())
			bestTime = info.ModTime()
		}
	}
	return best
}

// ProjectDirForCwd returns the Claude Code project directory for a given cwd.
// Claude slugifies cwd by replacing path separators with "-" and leading "/" with "".
func ProjectDirForCwd(claudeHome, cwd string) string {
	slug := slugifyCwd(cwd)
	return filepath.Join(claudeHome, "projects", slug)
}

func slugifyCwd(cwd string) string {
	// Claude Code's slug rule (observed empirically):
	//   /c/Users/Usuario         -> "C--Users-Usuario"
	//   /home/usuario            -> "-home-usuario"
	//   C:\Proyectos\jorge       -> "C--Proyectos-jorge"
	// We apply: replace ':' and path separators with '-'. First two chars
	// of an absolute Windows drive path ("C:") become "C-" (":" -> "-"),
	// then the "\" after produces a second "-" → "C--".
	s := strings.ReplaceAll(cwd, ":", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	// On POSIX the leading slash becomes a leading "-"; Claude keeps that.
	return s
}
