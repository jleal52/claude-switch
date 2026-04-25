package session

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/jleal52/claude-switch/internal/process"
	"github.com/jleal52/claude-switch/internal/pty"
	"github.com/jleal52/claude-switch/internal/ring"
)

// Config wires the supervisor's external dependencies.
type Config struct {
	ClaudeBin string   // path to `claude` (or test stand-in)
	BaseArgs  []string // prefix args before server-supplied args (default: --spawn same-dir)
	Start     StartFn  // PTY start function
	// Coalescing policy (defaults from spec).
	FlushMs    time.Duration
	FlushBytes int
	// Optional Job Object for child cleanup on Windows.
	Job interface{ Assign(*exec.Cmd) error }
}

func (c Config) defaulted() Config {
	if c.FlushMs == 0 {
		c.FlushMs = 16 * time.Millisecond
	}
	if c.FlushBytes == 0 {
		c.FlushBytes = 16 * 1024
	}
	if c.BaseArgs == nil {
		c.BaseArgs = []string{"remote-control", "--spawn", "same-dir"}
	}
	return c
}

// Supervisor owns the session table and dispatches control commands.
type Supervisor struct {
	cfg    Config
	events chan<- Event

	mu       sync.Mutex
	sessions map[string]*Session
}

func NewSupervisor(cfg Config, events chan<- Event) *Supervisor {
	return &Supervisor{cfg: cfg.defaulted(), events: events, sessions: map[string]*Session{}}
}

// Run blocks until ctx is cancelled, then closes all sessions.
func (s *Supervisor) Run(ctx context.Context) {
	<-ctx.Done()
	s.mu.Lock()
	for _, sess := range s.sessions {
		sess.CloseWith(s.events, "wrapper_close")
	}
	s.mu.Unlock()
}

// Open starts a new PTY session with the given id. Returns ErrSessionExists
// if the id is already in use.
func (s *Supervisor) Open(ctx context.Context, id, cwd, account string, extraArgs []string) error {
	s.mu.Lock()
	if _, exists := s.sessions[id]; exists {
		s.mu.Unlock()
		return fmt.Errorf("open %s: %w", id, ErrSessionExists)
	}

	args := append([]string{}, s.cfg.BaseArgs...)
	args = append(args, extraArgs...)
	cmd := exec.Command(s.cfg.ClaudeBin, args...)
	cmd.Dir = cwd
	process.ApplyPdeathsig(cmd)

	p, err := s.cfg.Start(cmd, pty.Size{Cols: 120, Rows: 32})
	if err != nil {
		s.mu.Unlock()
		emit(s.events, SessionExitedEvent{Session: id, ExitCode: -1, Reason: "spawn_failed", Detail: err.Error()})
		return fmt.Errorf("open %s: %w", id, err)
	}
	if s.cfg.Job != nil {
		if err := s.cfg.Job.Assign(cmd); err != nil {
			_ = p.Close()
			s.mu.Unlock()
			emit(s.events, SessionExitedEvent{Session: id, ExitCode: -1, Reason: "spawn_failed", Detail: err.Error()})
			return fmt.Errorf("assign %s to job: %w", id, err)
		}
	}

	sess := &Session{
		ID:      id,
		Cwd:     cwd,
		Account: account,
		Created: time.Now(),
		pty:     p,
		inbox:   make(chan []byte, 64),
		stop:    make(chan struct{}),
		ring:    ring.New(RingBytes),
	}
	s.sessions[id] = sess
	s.mu.Unlock()

	go s.reader(ctx, sess)
	go s.writer(ctx, sess)

	emit(s.events, SessionStartedEvent{
		Session: id, PID: sess.PID(), JSONLUUID: "", Cwd: cwd, Account: account,
	})
	return nil
}

// Close terminates an open session by id.
func (s *Supervisor) Close(id string) error {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("close %s: %w", id, ErrSessionNotFound)
	}
	delete(s.sessions, id)
	s.mu.Unlock()
	sess.CloseWith(s.events, "wrapper_close")
	return nil
}

// Input enqueues bytes to be written to the session's PTY stdin.
func (s *Supervisor) Input(id string, b []byte) error {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("input %s: %w", id, ErrSessionNotFound)
	}
	sess.Write(b)
	return nil
}

// Resize forwards a window-size change.
func (s *Supervisor) Resize(id string, cols, rows uint16) error {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("resize %s: %w", id, ErrSessionNotFound)
	}
	return sess.pty.Resize(pty.Size{Cols: cols, Rows: rows})
}

// Snapshot returns a list of sessions currently alive (for hello frames).
func (s *Supervisor) Snapshot() []*Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

// reader drains PTY output into PTYDataEvent frames (with coalescing).
func (s *Supervisor) reader(ctx context.Context, sess *Session) {
	defer func() {
		// When Read returns EOF we also want to emit SessionExited if the
		// closer did not already fire (e.g. child exited on its own).
		sess.CloseWith(s.events, "normal")
	}()

	err := Coalesce(ctx, sess.pty, s.cfg.FlushBytes, s.cfg.FlushMs, func(b []byte) {
		_, _ = sess.ring.Write(b)
		// Make a defensive copy since the coalescer re-uses its pending slice.
		cp := make([]byte, len(b))
		copy(cp, b)
		emit(s.events, PTYDataEvent{Session: sess.ID, Bytes: cp})
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		// Normal EOF path: Coalesce returned io.EOF from the PTY. Nothing to do.
	}
}

// writer drains the inbox into PTY stdin.
func (s *Supervisor) writer(ctx context.Context, sess *Session) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-sess.stop:
			return
		case msg := <-sess.inbox:
			_, _ = sess.pty.Write(msg)
		}
	}
}
