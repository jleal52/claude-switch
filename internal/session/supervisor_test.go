package session

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/pty"
)

// fakePTY is an in-memory PTY used by supervisor tests. Bytes written via
// Write are echoed back through Read after a short delay.
type fakePTY struct {
	mu     sync.Mutex
	buf    []byte
	cond   *sync.Cond
	closed bool
	cmd    *exec.Cmd
}

func newFakePTY() *fakePTY {
	f := &fakePTY{cmd: exec.Command("/bin/true")}
	f.cond = sync.NewCond(&f.mu)
	f.cmd.Process = &os.Process{Pid: 4242}
	return f
}

func (f *fakePTY) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for len(f.buf) == 0 && !f.closed {
		f.cond.Wait()
	}
	if f.closed && len(f.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.buf)
	f.buf = f.buf[n:]
	return n, nil
}

func (f *fakePTY) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buf = append(f.buf, p...) // loopback
	f.cond.Broadcast()
	return len(p), nil
}

func (f *fakePTY) Resize(pty.Size) error { return nil }
func (f *fakePTY) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	f.cond.Broadcast()
	return nil
}
func (f *fakePTY) Cmd() *exec.Cmd { return f.cmd }

// The supervisor calls sup.Spawn(cwd, account) which uses this factory.
func fakeStartFn(*exec.Cmd, pty.Size) (pty.PTY, error) { return newFakePTY(), nil }

func TestSupervisorOpenWriteData(t *testing.T) {
	events := make(chan Event, 16)
	sup := NewSupervisor(Config{Start: fakeStartFn, ClaudeBin: "/bin/true"}, events)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go sup.Run(ctx)

	sid := "sess-1"
	require.NoError(t, sup.Open(ctx, sid, "/tmp", "default", nil))

	// Wait for SessionStarted.
	started := waitFor(t, events, func(e Event) bool {
		ss, ok := e.(SessionStartedEvent)
		return ok && ss.Session == sid
	})
	require.Equal(t, 4242, started.(SessionStartedEvent).PID)

	// Write input — fake PTY echoes it back as output.
	require.NoError(t, sup.Input(sid, []byte("ping")))

	// Expect a PTYDataEvent with "ping".
	data := waitFor(t, events, func(e Event) bool {
		pd, ok := e.(PTYDataEvent)
		return ok && pd.Session == sid
	}).(PTYDataEvent)
	require.Equal(t, []byte("ping"), data.Bytes)

	// Close.
	require.NoError(t, sup.Close(sid))
	exited := waitFor(t, events, func(e Event) bool {
		_, ok := e.(SessionExitedEvent)
		return ok
	}).(SessionExitedEvent)
	require.Equal(t, "wrapper_close", exited.Reason)
}

func TestSupervisorDoubleOpenIsError(t *testing.T) {
	events := make(chan Event, 16)
	sup := NewSupervisor(Config{Start: fakeStartFn, ClaudeBin: "/bin/true"}, events)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go sup.Run(ctx)

	require.NoError(t, sup.Open(ctx, "s", "/tmp", "default", nil))
	err := sup.Open(ctx, "s", "/tmp", "default", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSessionExists))
}

func waitFor(t *testing.T, ch <-chan Event, pred func(Event) bool) Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-ch:
			if pred(e) {
				return e
			}
		case <-deadline:
			t.Fatalf("timed out waiting for expected event")
			return nil
		}
	}
}
