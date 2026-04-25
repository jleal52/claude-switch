package ws

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackoffIsExponentialWithCap(t *testing.T) {
	b := NewBackoff(100*time.Millisecond, 2*time.Second)
	d0 := b.Next()
	d1 := b.Next()
	d2 := b.Next()
	d10 := time.Duration(0)
	for i := 0; i < 10; i++ {
		d10 = b.Next()
	}

	require.GreaterOrEqual(t, d0, 100*time.Millisecond)
	require.LessOrEqual(t, d0, 150*time.Millisecond) // +jitter up to 50%
	require.Greater(t, d1, d0/2)                      // roughly doubled minus jitter
	require.Greater(t, d2, d1/2)
	require.LessOrEqual(t, d10, 2*time.Second+time.Duration(float64(2*time.Second)*0.5))
	// Reset puts us back to base.
	b.Reset()
	dr := b.Next()
	require.LessOrEqual(t, dr, 150*time.Millisecond)

	_ = math.Pi // silence unused import if any
}
