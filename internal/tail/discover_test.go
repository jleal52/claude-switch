package tail

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForJSONLPicksNewestAfterStart(t *testing.T) {
	dir := t.TempDir()
	// Pre-existing file — must NOT be picked.
	older := filepath.Join(dir, "old.jsonl")
	require.NoError(t, os.WriteFile(older, []byte("{}\n"), 0o644))
	// Back-date the old file so its ModTime is clearly before notBefore,
	// even on filesystems with coarse (≥1 s) timestamp resolution.
	past := time.Now().Add(-2 * time.Second)
	require.NoError(t, os.Chtimes(older, past, past))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Drop a new file 100 ms in the future.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "new.jsonl"), []byte("{}\n"), 0o644)
	}()

	got, err := WaitForNewJSONL(ctx, dir, time.Now())
	require.NoError(t, err)
	require.Equal(t, "new.jsonl", filepath.Base(got))
}

func TestWaitForJSONLTimesOut(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := WaitForNewJSONL(ctx, dir, time.Now())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
