package transcripts

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type captured struct {
	mu      sync.Mutex
	updates []Update
}

func (c *captured) onUpdate(u Update) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, u)
}

func (c *captured) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.updates)
}

func (c *captured) waitFor(t *testing.T, n int) []Update {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.len() >= n {
			c.mu.Lock()
			defer c.mu.Unlock()
			out := make([]Update, len(c.updates))
			copy(out, c.updates)
			return out
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor: only got %d updates after 2s, wanted %d", c.len(), n)
	return nil
}

func writeUserEntry(t *testing.T, path string, ts, content string) {
	t.Helper()
	line := `{"type":"user","timestamp":"` + ts + `","cwd":"/x","message":{"role":"user","content":"` + content + `"}}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(line), 0o644))
}

func TestWatcherEmitsFullFirstTick(t *testing.T) {
	root := t.TempDir()
	slug := "-x"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o755))
	writeUserEntry(t, filepath.Join(root, slug, "u1.jsonl"), "2026-05-09T10:00:00.000Z", "hi")

	cap := &captured{}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w := &Watcher{Root: root, Interval: 10 * time.Millisecond}
	go func() { _ = w.Run(ctx, cap.onUpdate) }()

	updates := cap.waitFor(t, 1)
	require.Equal(t, 1, len(updates[0].Snapshot.Transcripts))
	require.Equal(t, "u1", updates[0].Snapshot.Transcripts[0].JSONLUUID)
	require.True(t, updates[0].Full, "first emission should be marked full")
	require.Nil(t, updates[0].Diff, "first emission should not carry a diff")
}

func TestWatcherEmitsDiffOnNewTranscript(t *testing.T) {
	root := t.TempDir()
	slug := "-x"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o755))
	writeUserEntry(t, filepath.Join(root, slug, "u1.jsonl"), "2026-05-09T10:00:00.000Z", "hi")

	cap := &captured{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w := &Watcher{Root: root, Interval: 20 * time.Millisecond}
	go func() { _ = w.Run(ctx, cap.onUpdate) }()

	cap.waitFor(t, 1)
	writeUserEntry(t, filepath.Join(root, slug, "u2.jsonl"), "2026-05-09T11:00:00.000Z", "yo")

	updates := cap.waitFor(t, 2)
	require.False(t, updates[1].Full)
	require.NotNil(t, updates[1].Diff)
	require.Len(t, updates[1].Diff.UpsertTranscripts, 1)
	require.Equal(t, "u2", updates[1].Diff.UpsertTranscripts[0].JSONLUUID)
	require.Empty(t, updates[1].Diff.RemovedTranscripts)
}

func TestWatcherEmitsDiffOnRemoval(t *testing.T) {
	root := t.TempDir()
	slug := "-x"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o755))
	p1 := filepath.Join(root, slug, "u1.jsonl")
	writeUserEntry(t, p1, "2026-05-09T10:00:00.000Z", "hi")
	writeUserEntry(t, filepath.Join(root, slug, "u2.jsonl"), "2026-05-09T11:00:00.000Z", "yo")

	cap := &captured{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w := &Watcher{Root: root, Interval: 20 * time.Millisecond}
	go func() { _ = w.Run(ctx, cap.onUpdate) }()

	cap.waitFor(t, 1)
	require.NoError(t, os.Remove(p1))

	updates := cap.waitFor(t, 2)
	require.False(t, updates[1].Full)
	require.Equal(t, []string{"u1"}, updates[1].Diff.RemovedTranscripts)
}

func TestWatcherSuppressesTicksWithoutChanges(t *testing.T) {
	root := t.TempDir()
	slug := "-x"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o755))
	writeUserEntry(t, filepath.Join(root, slug, "u1.jsonl"), "2026-05-09T10:00:00.000Z", "hi")

	cap := &captured{}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	w := &Watcher{Root: root, Interval: 10 * time.Millisecond}
	go func() { _ = w.Run(ctx, cap.onUpdate) }()

	// Wait for first emission, then idle. Should not get a flood of identical
	// diffs even though we tick every 10 ms.
	cap.waitFor(t, 1)
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, 1, cap.len(), "watcher should not re-emit when nothing changed")
}
