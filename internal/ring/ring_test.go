package ring

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRingBelowCapacity(t *testing.T) {
	r := New(16)
	r.Write([]byte("hello"))
	require.Equal(t, []byte("hello"), r.Snapshot())
	require.Equal(t, 5, r.Len())
}

func TestRingEvictsOldestOverCapacity(t *testing.T) {
	r := New(8)
	r.Write([]byte("abcdefgh")) // fills exactly
	r.Write([]byte("IJK"))      // pushes out "abc"
	require.Equal(t, []byte("defghIJK"), r.Snapshot())
	require.Equal(t, 8, r.Len())
}

func TestRingSingleWriteLargerThanCapacity(t *testing.T) {
	r := New(4)
	r.Write([]byte("1234567890"))
	require.Equal(t, []byte("7890"), r.Snapshot())
}

func TestRingThreadSafe(t *testing.T) {
	// Multiple writers + a reader don't race or panic. We're not asserting
	// ordering — just that the data-race detector stays quiet.
	r := New(1024)
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				r.Write([]byte("abcdefgh"))
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		_ = r.Snapshot()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}
