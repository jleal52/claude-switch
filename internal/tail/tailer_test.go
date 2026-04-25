package tail

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTailEmitsExistingAndNewLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\n"), 0o644))

	out := make(chan string, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = Tail(ctx, path, func(line string) { out <- line })
	}()

	require.Equal(t, "a", <-out)
	require.Equal(t, "b", <-out)

	// Append more lines.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, _ = f.WriteString("c\n")
	_, _ = f.WriteString("d\n")
	_ = f.Close()

	select {
	case line := <-out:
		require.Equal(t, "c", line)
	case <-time.After(time.Second):
		t.Fatal("did not see c")
	}
	select {
	case line := <-out:
		require.Equal(t, "d", line)
	case <-time.After(time.Second):
		t.Fatal("did not see d")
	}
}
