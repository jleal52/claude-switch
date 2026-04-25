package session

import (
	"context"
	"io"
	"time"
)

// Coalesce reads from r and calls flush with buffered bytes, enforcing
// two rules (whichever fires first):
//
//  1. size: buffered bytes reach maxBytes.
//  2. time: maxWait elapsed since the first byte of the current buffer.
//
// On r error (including io.EOF), flushes any remaining bytes and returns
// the error. On ctx cancel, returns ctx.Err() without flushing the tail.
func Coalesce(ctx context.Context, r io.Reader, maxBytes int, maxWait time.Duration, flush func([]byte)) error {
	type readResult struct {
		data []byte
		err  error
	}
	pending := make([]byte, 0, maxBytes)
	reads := make(chan readResult, 1)

	go func() {
		for {
			buf := make([]byte, 4096)
			n, err := r.Read(buf)
			select {
			case reads <- readResult{data: buf[:n], err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	var deadline <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			if len(pending) > 0 {
				flush(pending)
				pending = pending[:0]
			}
			deadline = nil
		case rr := <-reads:
			if len(rr.data) > 0 {
				// Accumulate, flushing in maxBytes chunks if this read alone exceeds.
				remaining := rr.data
				for len(pending)+len(remaining) >= maxBytes {
					space := maxBytes - len(pending)
					pending = append(pending, remaining[:space]...)
					flush(pending)
					pending = pending[:0]
					remaining = remaining[space:]
					deadline = nil
				}
				if len(remaining) > 0 {
					if len(pending) == 0 {
						deadline = time.After(maxWait)
					}
					pending = append(pending, remaining...)
				}
			}
			if rr.err != nil {
				if len(pending) > 0 {
					flush(pending)
				}
				return rr.err
			}
		}
	}
}
