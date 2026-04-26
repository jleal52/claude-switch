package hub

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeWrapperConn satisfies WrapperConn for tests.
type fakeWrapperConn struct {
	mu     sync.Mutex
	sent   []OutboundFrame
	closed bool
}

func (f *fakeWrapperConn) Send(fr OutboundFrame) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ErrWrapperOffline
	}
	f.sent = append(f.sent, fr)
	return nil
}
func (f *fakeWrapperConn) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}

func TestRegisterAndDispatchOpen(t *testing.T) {
	h := New()
	conn := &fakeWrapperConn{}
	h.RegisterWrapper("w1", conn)

	require.NoError(t, h.OpenSession(context.Background(), OpenSessionRequest{
		WrapperID: "w1", SessionID: "s1", Cwd: "/", Account: "default",
	}))

	require.Len(t, conn.sent, 1)
	require.Equal(t, FrameTypeOpenSession, conn.sent[0].Type)
}

func TestOpenOnUnknownWrapperReturnsOffline(t *testing.T) {
	h := New()
	err := h.OpenSession(context.Background(), OpenSessionRequest{
		WrapperID: "missing", SessionID: "s1", Cwd: "/", Account: "default",
	})
	require.ErrorIs(t, err, ErrWrapperOffline)
}

func TestRingCacheReplay(t *testing.T) {
	h := New()
	h.UpdateRing("s1", []byte("hello"))
	h.UpdateRing("s1", []byte(" world"))
	require.Equal(t, []byte("hello world"), h.SnapshotRing("s1"))
}
