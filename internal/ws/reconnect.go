package ws

import (
	"context"
	"math/rand"
	"time"
)

// Backoff produces reconnect delays: base * 2^attempts with ±50% jitter,
// capped at max.
type Backoff struct {
	base, max time.Duration
	attempts  int
	rng       *rand.Rand
}

func NewBackoff(base, max time.Duration) *Backoff {
	return &Backoff{base: base, max: max, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

func (b *Backoff) Next() time.Duration {
	d := b.base << b.attempts
	if d > b.max || d <= 0 {
		d = b.max
	}
	b.attempts++
	jitter := time.Duration(b.rng.Float64() * float64(d) * 0.5)
	return d + jitter
}

func (b *Backoff) Reset() { b.attempts = 0 }

// Run reconnects forever, calling runOnce between waits. Returns when ctx
// is cancelled.
func (c *Client) Run(ctx context.Context) error {
	bo := NewBackoff(time.Second, 60*time.Second)
	for {
		err := c.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = err // logged via slog in main; for now, retry.
		wait := bo.Next()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
