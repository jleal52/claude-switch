package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSupervisorEmitsJSONLUUIDAfterDiscovery(t *testing.T) {
	// Build a fake "claude home" with projects/<slug>/ and simulate a new
	// jsonl appearing shortly after open.
	claudeHome := t.TempDir()
	cwd := t.TempDir()
	projects := filepath.Join(claudeHome, "projects")
	require.NoError(t, os.MkdirAll(projects, 0o755))

	events := make(chan Event, 32)
	sup := NewSupervisor(Config{
		Start:      fakeStartFn,
		ClaudeBin:  "/bin/true",
		ClaudeHome: claudeHome,
	}, events)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go sup.Run(ctx)

	require.NoError(t, sup.Open(ctx, "s", cwd, "default", nil))

	// Compute the slug the way Claude does.
	slug := slugForTest(cwd)
	projDir := filepath.Join(projects, slug)
	require.NoError(t, os.MkdirAll(projDir, 0o755))

	// Simulate claude creating its jsonl 100 ms later.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(projDir, "abc123.jsonl"), []byte("{}\n"), 0o644)
	}()

	// We should see a SessionStartedEvent first (empty JSONLUUID), then a
	// follow-up event with the UUID populated.
	gotUUID := false
	deadline := time.After(3 * time.Second)
	for !gotUUID {
		select {
		case e := <-events:
			if ss, ok := e.(SessionStartedEvent); ok && ss.JSONLUUID != "" {
				require.Equal(t, "abc123", ss.JSONLUUID)
				gotUUID = true
			}
		case <-deadline:
			t.Fatal("never saw SessionStartedEvent with JSONLUUID")
		}
	}
}

func slugForTest(cwd string) string {
	// Mirror internal/tail/discover.go's slugifyCwd (kept in the test so
	// that refactoring the real one does not silently break this test).
	out := cwd
	for _, c := range []string{":", "/", "\\"} {
		out = replaceAll(out, c, "-")
	}
	return out
}

func replaceAll(s, old, new string) string {
	for {
		idx := indexOf(s, old)
		if idx < 0 {
			return s
		}
		s = s[:idx] + new + s[idx+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
