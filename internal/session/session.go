package session

import (
	"errors"
	"os/exec"
	"sync"
	"time"

	"github.com/jleal52/claude-switch/internal/pty"
	"github.com/jleal52/claude-switch/internal/ring"
)

// RingBytes is the per-session PTY output ring size. 64 KiB per the spec.
const RingBytes = 64 * 1024

// Errors.
var (
	ErrSessionExists   = errors.New("session: already open")
	ErrSessionNotFound = errors.New("session: not found")
)

// Event is the supervisor's outbound event type (consumed by the ws layer).
// Exact frames live in internal/proto; session emits plain Go values and
// the ws layer converts them.
type Event interface{ isEvent() }

type SessionStartedEvent struct {
	Session   string
	PID       int
	JSONLUUID string
	Cwd       string
	Account   string
}

func (SessionStartedEvent) isEvent() {}

type SessionExitedEvent struct {
	Session  string
	ExitCode int
	Reason   string
	Detail   string
}

func (SessionExitedEvent) isEvent() {}

type PTYDataEvent struct {
	Session string
	Bytes   []byte
}

func (PTYDataEvent) isEvent() {}

type JSONLTailEvent struct {
	Session string
	Entry   string
}

func (JSONLTailEvent) isEvent() {}

// StartFn is the injectable PTY-start function (lets tests use a fake PTY
// without exec-ing a real shell).
type StartFn func(*exec.Cmd, pty.Size) (pty.PTY, error)

// Session is a live PTY bound to a running `claude` (or test stand-in).
type Session struct {
	ID        string
	Cwd       string
	Account   string
	JSONLUUID string
	Created   time.Time

	pty      pty.PTY
	inbox    chan []byte
	stop     chan struct{}
	ring     *ring.Buffer
	closeFn  sync.Once
	closeErr error
}

func (s *Session) PID() int {
	if s.pty == nil || s.pty.Cmd() == nil || s.pty.Cmd().Process == nil {
		return 0
	}
	return s.pty.Cmd().Process.Pid
}

// Write enqueues bytes to be written to the PTY stdin. Non-blocking up to
// the inbox capacity; drops further bytes (the server re-sends at most
// the ring's worth, so brief back-pressure is fine).
func (s *Session) Write(b []byte) {
	select {
	case s.inbox <- append([]byte(nil), b...):
	default:
	}
}

// CloseWith terminates the PTY and emits SessionExitedEvent with the given
// reason. Safe to call concurrently; only the first call performs the close.
func (s *Session) CloseWith(events chan<- Event, reason string) {
	s.closeFn.Do(func() {
		s.closeErr = s.pty.Close()
		close(s.stop)
		exitCode := 0
		if cmd := s.pty.Cmd(); cmd != nil && cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		emit(events, SessionExitedEvent{Session: s.ID, ExitCode: exitCode, Reason: reason})
	})
}

func emit(ch chan<- Event, e Event) {
	// Non-blocking best-effort emit so event channel backpressure
	// can't deadlock the supervisor. Consumers read promptly.
	select {
	case ch <- e:
	default:
		// Drop. The ws layer's priority queue has its own buffering;
		// dropping here is a last-resort safety and will log in production.
	}
}

// Ring returns a snapshot of the session's recent PTY output ring buffer.
// Used by the ws layer to replay buffered bytes after a reconnect.
func (s *Session) Ring() []byte { return s.ring.Snapshot() }
