package session

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeReader emits a sequence of byte chunks with controlled delays, then EOF.
type fakeReader struct {
	chunks []fakeChunk
	i      int
}

type fakeChunk struct {
	data  []byte
	delay time.Duration
}

func (r *fakeReader) Read(p []byte) (int, error) {
	if r.i >= len(r.chunks) {
		return 0, io.EOF
	}
	c := r.chunks[r.i]
	time.Sleep(c.delay)
	n := copy(p, c.data)
	r.i++
	return n, nil
}

func TestCoalesceFlushesByTime(t *testing.T) {
	r := &fakeReader{chunks: []fakeChunk{
		{[]byte("ab"), 0},
		{[]byte("cd"), 0},
	}}
	var out [][]byte
	err := Coalesce(context.Background(), r, 1024, 5*time.Millisecond, func(b []byte) {
		cp := make([]byte, len(b))
		copy(cp, b)
		out = append(out, cp)
	})
	require.ErrorIs(t, err, io.EOF)
	// Two tiny writes within <5 ms should coalesce into one flush at most
	// (plus possibly a final flush for the tail). Allow 1-2 flushes total.
	require.GreaterOrEqual(t, len(out), 1)
	require.LessOrEqual(t, len(out), 2)
	joined := bytes.Join(out, nil)
	require.Equal(t, []byte("abcd"), joined)
}

func TestCoalesceFlushesBySize(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 32) // bigger than threshold=16
	r := &fakeReader{chunks: []fakeChunk{{big, 0}}}

	var flushes [][]byte
	err := Coalesce(context.Background(), r, 16, time.Second, func(b []byte) {
		cp := make([]byte, len(b))
		copy(cp, b)
		flushes = append(flushes, cp)
	})
	require.ErrorIs(t, err, io.EOF)
	require.GreaterOrEqual(t, len(flushes), 2) // size trigger split it
	require.Equal(t, big, bytes.Join(flushes, nil))
}

func TestCoalesceStopsOnContext(t *testing.T) {
	r := &fakeReader{chunks: []fakeChunk{
		{[]byte("ab"), 0},
		{[]byte("cd"), 500 * time.Millisecond}, // would block past cancel
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := Coalesce(ctx, r, 1024, 10*time.Millisecond, func(b []byte) {})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
