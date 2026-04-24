# Wrapper PTY Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go wrapper binary for claude-switch that hosts N `claude` PTY sessions on a user's machine and streams them to a central server over a single outbound WebSocket.

**Architecture:** One Go binary. A supervisor goroutine owns a table of `Session`s; each session has reader/writer goroutines bridging a PTY master fd with per-session inboxes. A single WebSocket client goroutine drains a prioritised queue of outbound frames (JSON for control, binary for raw PTY bytes) and dispatches inbound frames to the right session. Device-code flow handles pairing with the server; children are tied to the wrapper's life via `PR_SET_PDEATHSIG` (Linux) and a Windows Job Object.

**Tech Stack:** Go 1.22+, `github.com/coder/websocket`, `github.com/creack/pty`, `github.com/UserExistsError/conpty`, `github.com/oklog/ulid/v2`, `github.com/pelletier/go-toml/v2`, stdlib `log/slog` + `golang.org/x/sys`. Tests: stdlib `testing` + `github.com/stretchr/testify/require`.

**Spec:** `docs/superpowers/specs/2026-04-25-wrapper-pty-design.md` — read it first; this plan implements what that doc specifies, nothing more.

---

## Task 1: Project bootstrap

**Goal:** Empty Go module compiles, tests pass (zero tests), CI matrix runs. Every task builds on this.

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `.golangci.yml`
- Create: `.github/workflows/ci.yml`
- Create: `cmd/claude-switch/main.go`

- [ ] **Step 1: Initialise the module**

```bash
cd C:\Proyectos\claude-switch
go mod init github.com/jleal52/claude-switch
```

Expected: creates `go.mod` with `module github.com/jleal52/claude-switch` and `go 1.22`.

- [ ] **Step 2: Create minimal main**

`cmd/claude-switch/main.go`:

```go
package main

import "fmt"

func main() {
    fmt.Println("claude-switch")
}
```

- [ ] **Step 3: Add the Makefile**

`Makefile`:

```makefile
.PHONY: build test lint tidy

build:
	go build -o bin/claude-switch ./cmd/claude-switch

test:
	go test ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy
```

- [ ] **Step 4: Add the linter config**

`.golangci.yml`:

```yaml
run:
  timeout: 3m
linters:
  enable:
    - errcheck
    - gofmt
    - goimports
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
```

- [ ] **Step 5: Add the CI workflow**

`.github/workflows/ci.yml`:

```yaml
name: CI
on:
  push:
    branches: [main, master]
  pull_request:
jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go build ./...
      - run: go test ./...
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - uses: golangci/golangci-lint-action@v6
        with: { version: v1.60 }
```

- [ ] **Step 6: Verify build + test**

```bash
go build ./...
go test ./...
```

Expected: both succeed. `test` prints `ok` or `[no test files]` for each package.

- [ ] **Step 7: Commit**

```bash
git add go.mod Makefile .golangci.yml .github cmd
git commit -m "feat: bootstrap Go module, Makefile, lint, CI matrix"
```

---

## Task 2: Protocol envelope + JSON frame types

**Goal:** Structs + encode/decode for every JSON control frame defined in the spec, with a version byte.

**Files:**
- Create: `internal/proto/envelope.go`
- Create: `internal/proto/frames.go`
- Create: `internal/proto/envelope_test.go`

- [ ] **Step 1: Write failing test for envelope round-trip**

`internal/proto/envelope_test.go`:

```go
package proto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvelopeRoundTripHello(t *testing.T) {
	h := Hello{
		WrapperID: "w-abc",
		OS:        "linux",
		Arch:      "amd64",
		Version:   "0.1.0",
		Accounts:  []string{"default"},
		Capabilities: []string{"pty"},
	}
	raw, err := Encode("hello", "", h)
	require.NoError(t, err)

	typ, session, payload, err := Decode(raw)
	require.NoError(t, err)
	require.Equal(t, "hello", typ)
	require.Equal(t, "", session)

	var got Hello
	require.NoError(t, payload.Into(&got))
	require.Equal(t, h, got)
}

func TestEnvelopeRejectsWrongVersion(t *testing.T) {
	raw := []byte(`{"v":99,"type":"hello","session":"","payload":{}}`)
	_, _, _, err := Decode(raw)
	require.ErrorIs(t, err, ErrUnsupportedVersion)
}
```

- [ ] **Step 2: Add stretchr/testify dependency**

```bash
go get github.com/stretchr/testify@latest
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/proto/...
```

Expected: fails to compile — `undefined: Hello`, `undefined: Encode`, `undefined: Decode`, `undefined: ErrUnsupportedVersion`.

- [ ] **Step 4: Implement envelope.go**

`internal/proto/envelope.go`:

```go
// Package proto defines the wire format for the wrapper<->server WebSocket.
// JSON envelopes carry control frames; binary frames (see ptydata.go) carry
// raw PTY output/input on the hot path.
package proto

import (
	"encoding/json"
	"errors"
	"fmt"
)

const ProtocolVersion = 1

var ErrUnsupportedVersion = errors.New("proto: unsupported envelope version")

type RawPayload json.RawMessage

func (r RawPayload) Into(v any) error { return json.Unmarshal(r, v) }

type envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	Session string          `json:"session"`
	Payload json.RawMessage `json:"payload"`
}

// Encode marshals a typed payload into a versioned JSON envelope.
// session may be "" for non-session-scoped frames (hello, ping, pong).
func Encode(typ string, session string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("proto encode payload: %w", err)
	}
	return json.Marshal(envelope{V: ProtocolVersion, Type: typ, Session: session, Payload: body})
}

// Decode validates the envelope version and returns the raw payload for
// type-specific unmarshal by the caller (dispatch lives outside proto).
func Decode(raw []byte) (typ string, session string, payload RawPayload, err error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", "", nil, fmt.Errorf("proto decode: %w", err)
	}
	if env.V != ProtocolVersion {
		return "", "", nil, fmt.Errorf("proto: got v=%d: %w", env.V, ErrUnsupportedVersion)
	}
	return env.Type, env.Session, RawPayload(env.Payload), nil
}
```

- [ ] **Step 5: Implement frames.go**

`internal/proto/frames.go`:

```go
package proto

// Frame type constants. The wire `type` field carries these strings.
const (
	TypeHello           = "hello"
	TypeOpenSession     = "open_session"
	TypeCloseSession    = "close_session"
	TypeSessionStarted  = "session.started"
	TypeSessionExited   = "session.exited"
	TypePTYResize       = "pty.resize"
	TypePTYControlEvent = "pty.control_event"
	TypeJSONLTail       = "jsonl.tail"
	TypePing            = "ping"
	TypePong            = "pong"
)

// Wrapper -> server.

type Hello struct {
	WrapperID    string   `json:"wrapper_id"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Version      string   `json:"version"`
	Accounts     []string `json:"accounts"`
	Capabilities []string `json:"capabilities"`
	// Sessions is the list of sessions currently alive on this wrapper,
	// sent on every hello (reconnect-aware). Empty on first connect.
	Sessions []HelloSession `json:"sessions"`
}

type HelloSession struct {
	ID                   string `json:"id"`
	PID                  int    `json:"pid"`
	JSONLUUID            string `json:"jsonl_uuid"`
	Cwd                  string `json:"cwd"`
	Account              string `json:"account"`
	BytesSinceDisconnect int    `json:"bytes_since_disconnect"`
}

type SessionStarted struct {
	PID       int    `json:"pid"`
	JSONLUUID string `json:"jsonl_uuid"`
	Cwd       string `json:"cwd"`
	Account   string `json:"account"`
}

type SessionExited struct {
	ExitCode int    `json:"exit_code"`
	Reason   string `json:"reason"` // "normal" | "signal" | "wrapper_close" | "spawn_failed"
	Detail   string `json:"detail,omitempty"`
}

type PTYControlEvent struct {
	Event  string `json:"event"` // "resize_ack" | "error"
	Detail string `json:"detail,omitempty"`
}

type JSONLTail struct {
	Entry string `json:"entry"`
}

type Pong struct {
	Echo string `json:"echo"`
}

// Server -> wrapper.

type OpenSession struct {
	Session string   `json:"session"`
	Cwd     string   `json:"cwd"`
	Account string   `json:"account"`
	Args    []string `json:"args,omitempty"`
}

type CloseSession struct{}

type PTYResize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type Ping struct {
	Nonce string `json:"nonce"`
}
```

- [ ] **Step 6: Run test to verify it passes**

```bash
go test ./internal/proto/...
```

Expected: PASS for both tests.

- [ ] **Step 7: Commit**

```bash
git add internal/proto go.mod go.sum
git commit -m "feat(proto): versioned JSON envelope + control frame types"
```

---

## Task 3: Binary pty.data frames

**Goal:** Encode/decode the length-prefixed binary frame for raw PTY bytes: 1-byte version, 16-byte ULID session, payload.

**Files:**
- Create: `internal/proto/ptydata.go`
- Create: `internal/proto/ptydata_test.go`

- [ ] **Step 1: Add ULID dependency**

```bash
go get github.com/oklog/ulid/v2@latest
```

- [ ] **Step 2: Write failing tests**

`internal/proto/ptydata_test.go`:

```go
package proto

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestPTYDataRoundTrip(t *testing.T) {
	id := ulid.Make()
	payload := []byte("hello\x1b[0m\n")

	frame := EncodePTYData(id, payload)

	gotID, gotPayload, err := DecodePTYData(frame)
	require.NoError(t, err)
	require.Equal(t, id, gotID)
	require.Equal(t, payload, gotPayload)
}

func TestPTYDataRejectsWrongVersion(t *testing.T) {
	frame := make([]byte, 17)
	frame[0] = 0x99
	_, _, err := DecodePTYData(frame)
	require.ErrorIs(t, err, ErrUnsupportedVersion)
}

func TestPTYDataRejectsTruncated(t *testing.T) {
	_, _, err := DecodePTYData([]byte{0x01, 0x02})
	require.Error(t, err)
}

func TestPTYDataAllowsEmptyPayload(t *testing.T) {
	// Edge case: valid envelope with zero-length payload (we coalesce only
	// non-empty output, but decoder must accept empty).
	id := ulid.Make()
	frame := EncodePTYData(id, nil)
	gotID, gotPayload, err := DecodePTYData(frame)
	require.NoError(t, err)
	require.Equal(t, id, gotID)
	require.Empty(t, gotPayload)
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/proto/...
```

Expected: fails to compile — `undefined: EncodePTYData`, `undefined: DecodePTYData`.

- [ ] **Step 4: Implement ptydata.go**

`internal/proto/ptydata.go`:

```go
package proto

import (
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"
)

const binaryPTYDataVersion byte = 0x01

var ErrMalformedBinaryFrame = errors.New("proto: malformed binary frame")

// EncodePTYData returns the wire representation of a pty.data frame:
//
//	byte 0     : version (0x01)
//	bytes 1..16: ULID session id (16 bytes)
//	bytes 17..: raw payload
//
// Zero-length payload is valid.
func EncodePTYData(session ulid.ULID, payload []byte) []byte {
	buf := make([]byte, 1+16+len(payload))
	buf[0] = binaryPTYDataVersion
	copy(buf[1:17], session[:])
	copy(buf[17:], payload)
	return buf
}

// DecodePTYData parses a binary pty.data frame. Returns ErrUnsupportedVersion
// if byte 0 is not the expected version, and ErrMalformedBinaryFrame if the
// frame is shorter than the header.
func DecodePTYData(frame []byte) (ulid.ULID, []byte, error) {
	if len(frame) < 17 {
		return ulid.ULID{}, nil, fmt.Errorf("proto: frame len=%d: %w", len(frame), ErrMalformedBinaryFrame)
	}
	if frame[0] != binaryPTYDataVersion {
		return ulid.ULID{}, nil, fmt.Errorf("proto: binary frame v=%d: %w", frame[0], ErrUnsupportedVersion)
	}
	var id ulid.ULID
	copy(id[:], frame[1:17])
	payload := make([]byte, len(frame)-17)
	copy(payload, frame[17:])
	return id, payload, nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/proto/...
```

Expected: all four tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/proto go.mod go.sum
git commit -m "feat(proto): binary pty.data frame with ULID session id"
```

---

## Task 4: Per-session ring buffer

**Goal:** Fixed-capacity byte ring used to replay the most recent PTY output when a WS reconnects.

**Files:**
- Create: `internal/ring/ring.go`
- Create: `internal/ring/ring_test.go`

- [ ] **Step 1: Write failing tests**

`internal/ring/ring_test.go`:

```go
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
	r.Write([]byte("IJK"))       // pushes out "abc"
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ring/...
```

Expected: fails to compile — `undefined: New`.

- [ ] **Step 3: Implement ring.go**

`internal/ring/ring.go`:

```go
// Package ring provides a fixed-capacity byte ring buffer for replaying
// the most recent output of a PTY session when the WebSocket reconnects.
package ring

import "sync"

type Buffer struct {
	mu   sync.Mutex
	data []byte
	cap  int
	head int // next write index in data[:cap]
	size int // bytes currently stored (≤ cap)
}

// New returns a ring buffer with fixed capacity.
func New(capacity int) *Buffer {
	return &Buffer{data: make([]byte, capacity), cap: capacity}
}

// Write appends bytes, evicting oldest bytes past capacity. If p is longer
// than cap, only the last cap bytes of p are retained.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(p) >= b.cap {
		copy(b.data, p[len(p)-b.cap:])
		b.head = 0
		b.size = b.cap
		return len(p), nil
	}

	for _, c := range p {
		b.data[b.head] = c
		b.head = (b.head + 1) % b.cap
		if b.size < b.cap {
			b.size++
		}
	}
	return len(p), nil
}

// Snapshot returns a copy of the current contents in chronological order.
func (b *Buffer) Snapshot() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]byte, b.size)
	if b.size < b.cap {
		copy(out, b.data[:b.size])
		return out
	}
	// Buffer is full: oldest byte is at head, newest is at head-1.
	copy(out, b.data[b.head:])
	copy(out[b.cap-b.head:], b.data[:b.head])
	return out
}

// Len returns how many bytes are currently stored.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size
}
```

- [ ] **Step 4: Run tests with race detector**

```bash
go test -race ./internal/ring/...
```

Expected: all four tests PASS, no data races reported.

- [ ] **Step 5: Commit**

```bash
git add internal/ring
git commit -m "feat(ring): fixed-capacity byte ring buffer for pty replay"
```

---

## Task 5: PTY interface + POSIX implementation

**Goal:** Cross-platform PTY interface; POSIX implementation using `creack/pty`. A unit test spawns `/bin/sh` and verifies `echo hi` round-trips.

**Files:**
- Create: `internal/pty/pty.go`
- Create: `internal/pty/pty_posix.go`
- Create: `internal/pty/pty_posix_test.go`

- [ ] **Step 1: Add creack/pty dependency**

```bash
go get github.com/creack/pty@latest
```

- [ ] **Step 2: Write failing test**

`internal/pty/pty_posix_test.go`:

```go
//go:build !windows

package pty

import (
	"bufio"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPOSIXSpawnEchoRoundTrip(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "read line; echo got:$line")
	p, err := Start(cmd, Size{Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.Write([]byte("hello\n"))
	require.NoError(t, err)

	r := bufio.NewReader(p)
	deadline := time.Now().Add(3 * time.Second)
	var line string
	for time.Now().Before(deadline) {
		s, err := r.ReadString('\n')
		if err == nil && len(s) > 0 {
			line = s
			if containsGotHello(line) {
				break
			}
		}
	}
	require.Contains(t, line, "got:hello")
}

func containsGotHello(s string) bool {
	for i := 0; i+7 <= len(s); i++ {
		if s[i:i+7] == "got:hel" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/pty/...
```

Expected: fails to compile — `undefined: Start`, `undefined: Size`.

- [ ] **Step 4: Implement pty.go (interface)**

`internal/pty/pty.go`:

```go
// Package pty abstracts the platform-specific pseudo-terminal. A PTY wraps
// a running child process; callers read its output via io.Reader, write
// input via io.Writer, and request window-size changes via Resize.
package pty

import (
	"io"
	"os/exec"
)

// Size is the terminal viewport in character cells.
type Size struct {
	Cols uint16
	Rows uint16
}

// PTY is a running child attached to a pseudo-terminal master.
// Read() drains the child's combined stdout/stderr. Write() sends to stdin.
// Close() terminates the child and frees fds/handles.
type PTY interface {
	io.ReadWriteCloser
	Resize(Size) error
	// Cmd returns the underlying *exec.Cmd so the session layer can inspect
	// PID, wait for exit, and read ExitCode. Do not call Start() on it.
	Cmd() *exec.Cmd
}

// Start launches cmd attached to a new PTY of the requested size.
// Cmd must be configured (Path, Args, Env, Dir) but NOT yet started.
func Start(cmd *exec.Cmd, size Size) (PTY, error) {
	return start(cmd, size)
}
```

- [ ] **Step 5: Implement pty_posix.go**

`internal/pty/pty_posix.go`:

```go
//go:build !windows

package pty

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type posixPTY struct {
	master *os.File
	cmd    *exec.Cmd
}

func start(cmd *exec.Cmd, size Size) (PTY, error) {
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: size.Cols, Rows: size.Rows})
	if err != nil {
		return nil, err
	}
	return &posixPTY{master: master, cmd: cmd}, nil
}

func (p *posixPTY) Read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *posixPTY) Write(b []byte) (int, error) { return p.master.Write(b) }

func (p *posixPTY) Resize(s Size) error {
	return pty.Setsize(p.master, &pty.Winsize{Cols: s.Cols, Rows: s.Rows})
}

func (p *posixPTY) Close() error {
	// Kill the child first, then close the master so Read() in the session
	// reader goroutine unblocks with EOF.
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return p.master.Close()
}

func (p *posixPTY) Cmd() *exec.Cmd { return p.cmd }
```

- [ ] **Step 6: Run test**

```bash
go test ./internal/pty/...
```

Expected: `TestPOSIXSpawnEchoRoundTrip` PASSes (skip on Windows — the `//go:build !windows` tag excludes it there).

- [ ] **Step 7: Commit**

```bash
git add internal/pty go.mod go.sum
git commit -m "feat(pty): POSIX pseudo-terminal wrapper via creack/pty"
```

---

## Task 6: Windows PTY implementation (ConPTY)

**Goal:** Same `PTY` interface on Windows using `UserExistsError/conpty`. A unit test spawns `cmd.exe` and round-trips a command.

**Files:**
- Create: `internal/pty/pty_windows.go`
- Create: `internal/pty/pty_windows_test.go`

- [ ] **Step 1: Add conpty dependency**

```bash
go get github.com/UserExistsError/conpty@latest
```

- [ ] **Step 2: Write failing test**

`internal/pty/pty_windows_test.go`:

```go
//go:build windows

package pty

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWindowsSpawnEchoRoundTrip(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo got:hello")
	p, err := Start(cmd, Size{Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	var total []byte
	for time.Now().Before(deadline) {
		n, err := p.Read(buf)
		if n > 0 {
			total = append(total, buf[:n]...)
			if strings.Contains(string(total), "got:hello") {
				return
			}
		}
		if err != nil {
			break
		}
	}
	t.Fatalf("did not see got:hello in PTY output: %q", string(total))
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
GOOS=windows go vet ./internal/pty/...
```

Expected: build fails — `undefined: start` (windows-specific impl missing).

- [ ] **Step 4: Implement pty_windows.go**

`internal/pty/pty_windows.go`:

```go
//go:build windows

package pty

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/UserExistsError/conpty"
)

type windowsPTY struct {
	cp  *conpty.ConPty
	cmd *exec.Cmd
}

func start(cmd *exec.Cmd, size Size) (PTY, error) {
	// conpty.Start takes a single command line string; reconstruct it from cmd.
	var sb strings.Builder
	sb.WriteString(quoteArg(cmd.Path))
	for _, a := range cmd.Args[1:] {
		sb.WriteByte(' ')
		sb.WriteString(quoteArg(a))
	}

	opts := []conpty.ConPtyOption{conpty.ConPtyDimensions(int(size.Cols), int(size.Rows))}
	if cmd.Dir != "" {
		opts = append(opts, conpty.ConPtyWorkDir(cmd.Dir))
	}
	if len(cmd.Env) > 0 {
		opts = append(opts, conpty.ConPtyEnv(cmd.Env))
	}

	cp, err := conpty.Start(sb.String(), opts...)
	if err != nil {
		return nil, fmt.Errorf("conpty start: %w", err)
	}

	// conpty manages its own process; expose Pid through the Cmd for callers
	// that expect *exec.Cmd. We do not call cmd.Start() — the real process
	// is owned by cp — but we wire up a best-effort Process reference.
	cmd.Process = &fakeOSProcess{pid: cp.Pid()}

	return &windowsPTY{cp: cp, cmd: cmd}, nil
}

func (p *windowsPTY) Read(b []byte) (int, error)  { return p.cp.Read(b) }
func (p *windowsPTY) Write(b []byte) (int, error) { return p.cp.Write(b) }

func (p *windowsPTY) Resize(s Size) error {
	return p.cp.Resize(int(s.Cols), int(s.Rows))
}

func (p *windowsPTY) Close() error {
	// Wait briefly for graceful exit then force close.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = p.cp.Wait(ctx)
	return p.cp.Close()
}

func (p *windowsPTY) Cmd() *exec.Cmd { return p.cmd }

func quoteArg(a string) string {
	if a == "" {
		return `""`
	}
	if !strings.ContainsAny(a, " \t\"") {
		return a
	}
	return `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
}
```

- [ ] **Step 5: Add the fakeOSProcess helper**

Append to `internal/pty/pty_windows.go`:

```go
import "os"

// fakeOSProcess wraps a PID so *exec.Cmd.Process.Pid works for callers that
// only need PID reporting. conpty owns the real handle.
type fakeOSProcess struct {
	*os.Process
	pid uint32
}

func (f *fakeOSProcess) Pid() int { return int(f.pid) }
```

Actually the simpler approach: `cmd.Process = &os.Process{Pid: int(cp.Pid())}`. Use that instead of the fake-wrapper dance. Rewrite the single line in `start`:

```go
	cmd.Process = &os.Process{Pid: int(cp.Pid())}
```

and drop the `fakeOSProcess` struct and its import.

- [ ] **Step 6: Run test on Windows**

```bash
go test ./internal/pty/...
```

On Windows: `TestWindowsSpawnEchoRoundTrip` PASSes.
On non-Windows: build tag excludes the test; package still compiles clean.

- [ ] **Step 7: Commit**

```bash
git add internal/pty go.mod go.sum
git commit -m "feat(pty): Windows ConPTY implementation behind shared interface"
```

---

## Task 7: Linux PDEATHSIG wiring

**Goal:** When the wrapper process dies, every child receives SIGTERM automatically. Wire it via `syscall.SysProcAttr.Pdeathsig`.

**Files:**
- Create: `internal/process/pdeath_linux.go`
- Create: `internal/process/pdeath_other.go`
- Create: `internal/process/pdeath_linux_test.go`

- [ ] **Step 1: Write failing test (linux-only)**

`internal/process/pdeath_linux_test.go`:

```go
//go:build linux

package process

import (
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyPdeathsigSetsSignal(t *testing.T) {
	cmd := exec.Command("/bin/true")
	ApplyPdeathsig(cmd)
	require.NotNil(t, cmd.SysProcAttr)
	require.Equal(t, syscall.SIGTERM, cmd.SysProcAttr.Pdeathsig)
}

func TestApplyPdeathsigPreservesExistingSysProcAttr(t *testing.T) {
	cmd := exec.Command("/bin/true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	ApplyPdeathsig(cmd)
	require.True(t, cmd.SysProcAttr.Setsid)
	require.Equal(t, syscall.SIGTERM, cmd.SysProcAttr.Pdeathsig)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
GOOS=linux go test ./internal/process/...
```

Expected: fails to compile — `undefined: ApplyPdeathsig`.

- [ ] **Step 3: Implement pdeath_linux.go**

`internal/process/pdeath_linux.go`:

```go
//go:build linux

// Package process centralises OS-specific wiring that ties children to
// the wrapper's lifetime (PDEATHSIG on Linux, Job Objects on Windows).
package process

import (
	"os/exec"
	"syscall"
)

// ApplyPdeathsig configures cmd so the kernel sends SIGTERM to the child
// when the current (wrapper) process dies for any reason. Idempotent.
func ApplyPdeathsig(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}
```

- [ ] **Step 4: Implement pdeath_other.go (no-op on non-Linux)**

`internal/process/pdeath_other.go`:

```go
//go:build !linux

package process

import "os/exec"

// ApplyPdeathsig is a no-op on non-Linux platforms. macOS has no direct
// equivalent; Windows uses the Job Object mechanism (see job_windows.go).
func ApplyPdeathsig(_ *exec.Cmd) {}
```

- [ ] **Step 5: Run tests on all platforms**

```bash
go test ./internal/process/...
```

Expected: on linux both tests PASS; on macos/windows package compiles with no tests run.

- [ ] **Step 6: Commit**

```bash
git add internal/process
git commit -m "feat(process): Linux PDEATHSIG wiring for child cleanup"
```

---

## Task 8: Windows Job Object

**Goal:** Create a Job Object at wrapper startup with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, assign the wrapper process itself, and expose `Assign(*exec.Cmd)` to be called before resuming each child (spawned with `CREATE_SUSPENDED`).

**Files:**
- Create: `internal/process/job_windows.go`
- Create: `internal/process/job_windows_test.go`

- [ ] **Step 1: Add golang.org/x/sys dependency**

```bash
go get golang.org/x/sys/windows@latest
```

- [ ] **Step 2: Write failing test**

`internal/process/job_windows_test.go`:

```go
//go:build windows

package process

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJobKillsChildWhenJobCloses(t *testing.T) {
	job, err := NewKillOnCloseJob()
	require.NoError(t, err)

	cmd := exec.Command("cmd.exe", "/c", "ping -n 60 127.0.0.1 >nul")
	require.NoError(t, cmd.Start())
	require.NoError(t, job.Assign(cmd))

	// Close the job: child must die.
	require.NoError(t, job.Close())

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		// Exited promptly — expected.
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("child survived job close")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
GOOS=windows go test ./internal/process/...
```

Expected: compile failure — `undefined: NewKillOnCloseJob`.

- [ ] **Step 4: Implement job_windows.go**

`internal/process/job_windows.go`:

```go
//go:build windows

package process

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Job is a handle to a Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. When the process holding this handle
// exits (for any reason), every process assigned to the job is killed.
type Job struct {
	handle windows.Handle
}

// NewKillOnCloseJob creates and configures a fresh Job Object. The caller
// must hold the Job struct for the lifetime of the wrapper; closing it
// (or wrapper exit) kills every assigned child.
func NewKillOnCloseJob() (*Job, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	if _, err := windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("set job information: %w", err)
	}
	return &Job{handle: h}, nil
}

// Assign adds cmd.Process to the job. Must be called AFTER cmd.Start().
// To avoid the child running briefly outside the job, callers should
// start the process suspended and resume it only after this returns.
func (j *Job) Assign(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return fmt.Errorf("cmd has no Process (not started)")
	}
	procHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("open child process: %w", err)
	}
	defer windows.CloseHandle(procHandle)
	return windows.AssignProcessToJobObject(j.handle, procHandle)
}

// Close destroys the job, killing every assigned process.
func (j *Job) Close() error {
	return windows.CloseHandle(j.handle)
}
```

- [ ] **Step 5: Run test on Windows**

```bash
go test ./internal/process/...
```

Expected: `TestJobKillsChildWhenJobCloses` PASS on Windows, skipped on other OSes.

- [ ] **Step 6: Commit**

```bash
git add internal/process go.mod go.sum
git commit -m "feat(process): Windows Job Object with KILL_ON_JOB_CLOSE"
```

---

## Task 9: Coalescing encoder for pty.data

**Goal:** Read bytes from a PTY and emit `pty.data` binary frames with coalescing rules: flush when ≥16 KiB buffered OR ≥16 ms since first unflushed byte.

**Files:**
- Create: `internal/session/coalesce.go`
- Create: `internal/session/coalesce_test.go`

- [ ] **Step 1: Write failing tests**

`internal/session/coalesce_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/session/...
```

Expected: fails to compile — `undefined: Coalesce`.

- [ ] **Step 3: Implement coalesce.go**

`internal/session/coalesce.go`:

```go
package session

import (
	"context"
	"io"
	"time"
)

// Coalesce reads from r and calls flush with buffered bytes, enforcing
// two rules (whichever fires first):
//
//	1. size: buffered bytes reach maxBytes.
//	2. time: maxWait elapsed since the first byte of the current buffer.
//
// On r error (including io.EOF), flushes any remaining bytes and returns
// the error. On ctx cancel, returns ctx.Err() without flushing the tail.
func Coalesce(ctx context.Context, r io.Reader, maxBytes int, maxWait time.Duration, flush func([]byte)) error {
	type readResult struct {
		n   int
		err error
	}
	buf := make([]byte, 4096)
	pending := make([]byte, 0, maxBytes)
	reads := make(chan readResult, 1)

	go func() {
		for {
			n, err := r.Read(buf)
			select {
			case reads <- readResult{n: n, err: err}:
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
			if rr.n > 0 {
				// Accumulate, flushing in maxBytes chunks if this read alone exceeds.
				remaining := buf[:rr.n]
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
```

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/session/...
```

Expected: three tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session
git commit -m "feat(session): pty.data coalescing by size and time"
```

---

## Task 10: Session struct + supervisor (core lifecycle)

**Goal:** Central supervisor handles `open_session` / `close_session` / process-exit. One Session owns one PTY + its goroutines. Testable with an in-memory fake PTY.

**Files:**
- Create: `internal/session/session.go`
- Create: `internal/session/supervisor.go`
- Create: `internal/session/supervisor_test.go`

- [ ] **Step 1: Write failing test**

`internal/session/supervisor_test.go`:

```go
package session

import (
	"context"
	"errors"
	"io"
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
	f.cmd.Process = &osProcessStub{pid: 4242}
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

// osProcessStub fills *exec.Cmd.Process.Pid only.
type osProcessStub struct{ pid int }

// exec.Cmd.Process is *os.Process not an interface, so we can't sub it
// cleanly. The fake PTY puts an *os.Process value with just Pid populated;
// Kill() on it is a no-op because Pid=4242 is unlikely to exist. That is
// OK for these tests — supervisor calls PTY.Close(), not Process.Kill.
func init() {
	// This file is imported only from test; the stub above is there so that
	// reading Cmd().Process.Pid works. We fix it up in newFakePTY by
	// constructing an *os.Process{Pid: 4242} directly.
}
```

Note: the `osProcessStub` type above is a dead helper; replace the `f.cmd.Process = &osProcessStub{pid: 4242}` line with:

```go
f.cmd.Process = &os.Process{Pid: 4242}
```

and add `"os"` to the imports. Remove the stub struct and `init` block.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/session/...
```

Expected: compile errors — `undefined: NewSupervisor`, `undefined: Config`, `undefined: Event`, etc.

- [ ] **Step 3: Implement session.go**

`internal/session/session.go`:

```go
package session

import (
	"context"
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

// ctx-aware wait with a timeout helper.
func waitCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
```

- [ ] **Step 4: Implement supervisor.go**

`internal/session/supervisor.go`:

```go
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
```

- [ ] **Step 5: Run tests with race detector**

```bash
go test -race ./internal/session/...
```

Expected: both supervisor tests PASS, race detector quiet.

- [ ] **Step 6: Commit**

```bash
git add internal/session
git commit -m "feat(session): supervisor with open/close, reader/writer goroutines"
```

---

## Task 11: JSONL UUID discovery

**Goal:** After `open_session` starts `claude`, discover which `~/.claude/projects/<slug>/<uuid>.jsonl` belongs to this session (it is the newest file appearing in the project directory after spawn). Update `Session.JSONLUUID` and re-emit `SessionStarted` with it populated.

**Files:**
- Create: `internal/tail/discover.go`
- Create: `internal/tail/discover_test.go`

- [ ] **Step 1: Write failing test**

`internal/tail/discover_test.go`:

```go
package tail

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForJSONLPicksNewestAfterStart(t *testing.T) {
	dir := t.TempDir()
	// Pre-existing file — must NOT be picked.
	older := filepath.Join(dir, "old.jsonl")
	require.NoError(t, os.WriteFile(older, []byte("{}\n"), 0o644))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Drop a new file 100 ms in the future.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "new.jsonl"), []byte("{}\n"), 0o644)
	}()

	got, err := WaitForNewJSONL(ctx, dir, time.Now())
	require.NoError(t, err)
	require.Equal(t, "new.jsonl", filepath.Base(got))
}

func TestWaitForJSONLTimesOut(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := WaitForNewJSONL(ctx, dir, time.Now())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tail/...
```

Expected: compile failure — `undefined: WaitForNewJSONL`.

- [ ] **Step 3: Implement discover.go**

`internal/tail/discover.go`:

```go
// Package tail discovers and tails the .jsonl file backing a claude session.
// Claude Code writes one file per session under ~/.claude/projects/<slug>/.
// The wrapper correlates its freshly-spawned child with the jsonl that
// appeared in that directory after the child started.
package tail

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WaitForNewJSONL polls projectDir for a *.jsonl created at or after notBefore.
// Returns the first match or ctx.Err() on timeout.
func WaitForNewJSONL(ctx context.Context, projectDir string, notBefore time.Time) (string, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			if p := scanNewest(projectDir, notBefore); p != "" {
				return p, nil
			}
		}
	}
}

func scanNewest(dir string, notBefore time.Time) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(notBefore) {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best = filepath.Join(dir, e.Name())
			bestTime = info.ModTime()
		}
	}
	return best
}

// ProjectDirForCwd returns the Claude Code project directory for a given cwd.
// Claude slugifies cwd by replacing path separators with "-" and leading "/" with "".
func ProjectDirForCwd(claudeHome, cwd string) string {
	slug := slugifyCwd(cwd)
	return filepath.Join(claudeHome, "projects", slug)
}

func slugifyCwd(cwd string) string {
	// Claude Code's slug rule (observed empirically):
	//   /c/Users/Usuario         -> "C--Users-Usuario"
	//   /home/usuario            -> "-home-usuario"
	//   C:\Proyectos\jorge       -> "C--Proyectos-jorge"
	// We apply: replace ':' and path separators with '-'. First two chars
	// of an absolute Windows drive path ("C:") become "C-" (":" -> "-"),
	// then the "\" after produces a second "-" → "C--".
	s := strings.ReplaceAll(cwd, ":", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	// On POSIX the leading slash becomes a leading "-"; Claude keeps that.
	return s
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/tail/...
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tail
git commit -m "feat(tail): discover .jsonl file after claude spawn"
```

---

## Task 12: JSONL tailer (emits JSONLTail events)

**Goal:** Given a jsonl file path, emit `JSONLTailEvent` for each new line written, until cancelled.

**Files:**
- Create: `internal/tail/tailer.go`
- Create: `internal/tail/tailer_test.go`

- [ ] **Step 1: Write failing test**

`internal/tail/tailer_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tail/...
```

Expected: compile failure — `undefined: Tail`.

- [ ] **Step 3: Implement tailer.go**

`internal/tail/tailer.go`:

```go
package tail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"time"
)

// Tail reads path line-by-line, calling emit for each line. Existing content
// is emitted first, then the tailer polls for appended content until ctx is
// cancelled. Returns nil on ctx cancel, error on open/read failure.
func Tail(ctx context.Context, path string, emit func(string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			// Strip trailing newline.
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			emit(line)
		}
		if err == nil {
			continue
		}
		if !errors.Is(err, io.EOF) {
			return err
		}
		// EOF: wait for more data or ctx cancel.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/tail/...
```

Expected: three tests (this one + the two from Task 11) all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tail
git commit -m "feat(tail): line-by-line jsonl tailer with polling fallback"
```

---

## Task 13: WebSocket client + frame dispatch

**Goal:** Connect to the server with a Bearer token, send `hello`, dispatch inbound frames to the supervisor, and drain supervisor events into the WS. No reconnect yet (that comes in Task 14).

**Files:**
- Create: `internal/ws/client.go`
- Create: `internal/ws/client_test.go`

- [ ] **Step 1: Add coder/websocket dependency**

```bash
go get github.com/coder/websocket@latest
```

- [ ] **Step 2: Write failing test**

`internal/ws/client_test.go`:

```go
package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/session"
)

func TestClientSendsHelloOnConnect(t *testing.T) {
	helloCh := make(chan proto.Hello, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		c, err := websocket.Accept(w, r, nil)
		require.NoError(t, err)
		defer c.CloseNow()
		_, data, err := c.Read(r.Context())
		require.NoError(t, err)
		typ, _, payload, err := proto.Decode(data)
		require.NoError(t, err)
		require.Equal(t, proto.TypeHello, typ)
		var h proto.Hello
		require.NoError(t, json.Unmarshal([]byte(payload), &h))
		helloCh <- h
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]

	events := make(chan session.Event, 8)
	sup := session.NewSupervisor(session.Config{ClaudeBin: "/bin/true"}, events)

	cli := NewClient(Config{
		URL:       wsURL,
		Token:     "test-token",
		WrapperID: "w-test",
		Version:   "test",
	}, sup, events)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = cli.runOnce(ctx) }()

	select {
	case h := <-helloCh:
		require.Equal(t, "w-test", h.WrapperID)
		require.Contains(t, h.Accounts, "default")
	case <-ctx.Done():
		t.Fatal("server never received hello")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/ws/...
```

Expected: compile failure — `undefined: NewClient`, etc.

- [ ] **Step 4: Implement client.go**

`internal/ws/client.go`:

```go
// Package ws wires the wrapper's supervisor to the server over a single
// WebSocket. JSON frames (via proto.Encode) carry control; binary frames
// carry raw PTY data (proto.EncodePTYData).
package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"

	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/session"
)

type Config struct {
	URL       string
	Token     string
	WrapperID string
	Version   string
}

type Client struct {
	cfg    Config
	sup    *session.Supervisor
	events <-chan session.Event
}

func NewClient(cfg Config, sup *session.Supervisor, events <-chan session.Event) *Client {
	return &Client{cfg: cfg, sup: sup, events: events}
}

// runOnce dials the server, sends hello, and pumps frames both ways until
// the connection drops or ctx is cancelled. Task 14 wraps this in reconnect.
func (c *Client) runOnce(ctx context.Context) error {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.cfg.Token)
	conn, _, err := websocket.Dial(ctx, c.cfg.URL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 * 1024 * 1024) // 4 MiB per frame

	// Send hello first.
	if err := c.sendHello(ctx, conn); err != nil {
		return err
	}

	// Fan-in: inbound frames and outbound events.
	readErr := make(chan error, 1)
	go func() { readErr <- c.readLoop(ctx, conn) }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case ev := <-c.events:
			if err := c.writeEvent(ctx, conn, ev); err != nil {
				return err
			}
		}
	}
}

func (c *Client) sendHello(ctx context.Context, conn *websocket.Conn) error {
	sessions := c.sup.Snapshot()
	hs := make([]proto.HelloSession, 0, len(sessions))
	for _, s := range sessions {
		hs = append(hs, proto.HelloSession{
			ID: s.ID, PID: s.PID(), JSONLUUID: s.JSONLUUID,
			Cwd: s.Cwd, Account: s.Account,
		})
	}
	hello := proto.Hello{
		WrapperID: c.cfg.WrapperID,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Version:   c.cfg.Version,
		Accounts:  []string{"default"},
		Capabilities: []string{"pty"},
		Sessions:  hs,
	}
	raw, err := proto.Encode(proto.TypeHello, "", hello)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, raw)
}

func (c *Client) writeEvent(ctx context.Context, conn *websocket.Conn, ev session.Event) error {
	switch e := ev.(type) {
	case session.SessionStartedEvent:
		raw, err := proto.Encode(proto.TypeSessionStarted, e.Session, proto.SessionStarted{
			PID: e.PID, JSONLUUID: e.JSONLUUID, Cwd: e.Cwd, Account: e.Account,
		})
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, raw)
	case session.SessionExitedEvent:
		raw, err := proto.Encode(proto.TypeSessionExited, e.Session, proto.SessionExited{
			ExitCode: e.ExitCode, Reason: e.Reason, Detail: e.Detail,
		})
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, raw)
	case session.PTYDataEvent:
		id, err := ulid.ParseStrict(e.Session)
		if err != nil {
			return fmt.Errorf("pty.data: bad session id %q: %w", e.Session, err)
		}
		frame := proto.EncodePTYData(id, e.Bytes)
		return conn.Write(ctx, websocket.MessageBinary, frame)
	case session.JSONLTailEvent:
		raw, err := proto.Encode(proto.TypeJSONLTail, e.Session, proto.JSONLTail{Entry: e.Entry})
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, raw)
	default:
		return fmt.Errorf("ws: unknown event %T", ev)
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		switch typ {
		case websocket.MessageBinary:
			id, payload, err := proto.DecodePTYData(data)
			if err != nil {
				return fmt.Errorf("bad binary frame: %w", err)
			}
			_ = c.sup.Input(id.String(), payload)
		case websocket.MessageText:
			if err := c.handleControl(ctx, data); err != nil {
				return err
			}
		}
	}
}

func (c *Client) handleControl(ctx context.Context, data []byte) error {
	typ, session, payload, err := proto.Decode(data)
	if err != nil {
		return err
	}
	switch typ {
	case proto.TypeOpenSession:
		var p proto.OpenSession
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		sid := p.Session
		if sid == "" {
			sid = session
		}
		return c.sup.Open(ctx, sid, p.Cwd, p.Account, p.Args)
	case proto.TypeCloseSession:
		return c.sup.Close(session)
	case proto.TypePTYResize:
		var p proto.PTYResize
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		return c.sup.Resize(session, p.Cols, p.Rows)
	case proto.TypePing:
		var p proto.Ping
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		// Handled via a dedicated path? For MVP inline: noop, Task 14 adds it.
		_ = p
		return nil
	}
	return nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test -race ./internal/ws/...
```

Expected: `TestClientSendsHelloOnConnect` PASSes.

- [ ] **Step 6: Commit**

```bash
git add internal/ws go.mod go.sum
git commit -m "feat(ws): single-connection client with frame dispatch"
```

---

## Task 14: Reconnect loop + ping/pong + ring-buffer replay

**Goal:** Wrap `runOnce` in an exponential-backoff reconnect loop. Implement `ping` → `pong`. On reconnect, replay each session's ring buffer before live output resumes.

**Files:**
- Create: `internal/ws/reconnect.go`
- Create: `internal/ws/reconnect_test.go`
- Modify: `internal/ws/client.go` (add ping handling + replay hook)

- [ ] **Step 1: Write failing test**

`internal/ws/reconnect_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ws/...
```

Expected: compile failure — `undefined: NewBackoff`.

- [ ] **Step 3: Implement reconnect.go**

`internal/ws/reconnect.go`:

```go
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
		// Success-on-first-read resets backoff, but we don't know yet that
		// this attempt will succeed. Reset when hello completes inside
		// runOnce (see Task 14 step 5 below).
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ws/...
```

Expected: PASS.

- [ ] **Step 5: Implement ping/pong + replay in client.go**

Modify `runOnce` in `internal/ws/client.go`. After `sendHello` succeeds, iterate `c.sup.Snapshot()` and for each session, write a binary `pty.data` frame containing `sess.ring.Snapshot()` (if non-empty). Add ping handling in `handleControl`:

```go
case proto.TypePing:
    var p proto.Ping
    if err := json.Unmarshal([]byte(payload), &p); err != nil {
        return err
    }
    raw, err := proto.Encode(proto.TypePong, "", proto.Pong{Echo: p.Nonce})
    if err != nil {
        return err
    }
    return conn.Write(ctx, websocket.MessageText, raw)
```

And the replay block, inserted right after `c.sendHello(ctx, conn)` returns:

```go
// Ring-buffer replay: re-send what each alive session has buffered so the
// browser sees "what it missed" during disconnect. Order: hello, replay,
// then live event loop.
for _, s := range c.sup.Snapshot() {
    buf := s.Ring() // add this accessor: returns s.ring.Snapshot()
    if len(buf) == 0 {
        continue
    }
    id, err := ulid.ParseStrict(s.ID)
    if err != nil {
        continue
    }
    frame := proto.EncodePTYData(id, buf)
    if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
        return err
    }
}
```

Also add `Ring() []byte` method to `Session` in `internal/session/session.go`:

```go
func (s *Session) Ring() []byte { return s.ring.Snapshot() }
```

- [ ] **Step 6: Run all tests with race**

```bash
go test -race ./...
```

Expected: all existing tests still PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ws internal/session
git commit -m "feat(ws): reconnect with backoff, ping/pong, ring-buffer replay"
```

---

## Task 15: Device-code flow

**Goal:** `claude-switch pair` subcommand talks to server pairing endpoints, polls until the user confirms, writes `credentials.json` with mode 0600.

**Files:**
- Create: `internal/auth/devicecode.go`
- Create: `internal/auth/credentials.go`
- Create: `internal/auth/devicecode_test.go`
- Create: `cmd/claude-switch/pair.go`

- [ ] **Step 1: Write failing test with fake server**

`internal/auth/devicecode_test.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPairHappyPath(t *testing.T) {
	var polls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device/pair/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":       "ABCD-1234",
				"poll_url":   "/device/pair/poll?c=ABCD-1234",
				"expires_in": 300,
			})
		case "/device/pair/poll":
			n := atomic.AddInt32(&polls, 1)
			if n < 2 {
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(`{"status":"pending"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":    "tok-abc",
				"refresh_token":   "ref-xyz",
				"expires_at":      time.Now().Add(time.Hour).Format(time.RFC3339),
				"server_endpoint": "wss://example.com/ws",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	var announced string
	res, err := Pair(context.Background(), PairConfig{
		ServerBase:   srv.URL,
		PollInterval: 20 * time.Millisecond,
		Announce:     func(code string) { announced = code },
	})
	require.NoError(t, err)
	require.Equal(t, "ABCD-1234", announced)
	require.Equal(t, "tok-abc", res.AccessToken)
	require.Equal(t, "wss://example.com/ws", res.ServerEndpoint)
}

func TestPairTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device/pair/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "XXXX", "poll_url": "/device/pair/poll", "expires_in": 300,
			})
		case "/device/pair/poll":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"pending"}`))
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := Pair(ctx, PairConfig{
		ServerBase:   srv.URL,
		PollInterval: 10 * time.Millisecond,
		Announce:     func(string) {},
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/auth/...
```

Expected: `undefined: Pair`, `undefined: PairConfig`.

- [ ] **Step 3: Implement devicecode.go**

`internal/auth/devicecode.go`:

```go
// Package auth implements the device-code pairing flow between the wrapper
// and the central server (spec section "Authentication — device-code flow").
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Credentials is what pairing returns and what subsequent runs persist.
type Credentials struct {
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"refresh_token"`
	ExpiresAt      time.Time `json:"expires_at"`
	ServerEndpoint string    `json:"server_endpoint"`
}

type PairConfig struct {
	ServerBase   string        // e.g. https://server.example.com
	WrapperName  string        // typically os.Hostname()
	OS, Arch     string        // runtime.GOOS, runtime.GOARCH
	Version      string        // wrapper version string
	PollInterval time.Duration // default 5s
	Announce     func(code string)
	HTTPClient   *http.Client
}

type startResp struct {
	Code      string `json:"code"`
	PollURL   string `json:"poll_url"`
	ExpiresIn int    `json:"expires_in"`
}

// Pair performs the device-code dance and returns the issued credentials.
func Pair(ctx context.Context, cfg PairConfig) (*Credentials, error) {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}

	// 1. Start.
	body, _ := json.Marshal(map[string]any{
		"name": cfg.WrapperName, "os": cfg.OS, "arch": cfg.Arch, "version": cfg.Version,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ServerBase+"/device/pair/start", bodyReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pair start: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("pair start: http %d", resp.StatusCode)
	}
	var s startResp
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	cfg.Announce(s.Code)

	pollURL := s.PollURL
	if !isAbsoluteURL(pollURL) {
		pollURL = cfg.ServerBase + pollURL
	}

	// 2. Poll.
	tick := time.NewTicker(cfg.PollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
		pr, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return nil, err
		}
		pres, err := cfg.HTTPClient.Do(pr)
		if err != nil {
			return nil, fmt.Errorf("pair poll: %w", err)
		}
		func() { defer pres.Body.Close() }()
		if pres.StatusCode == http.StatusAccepted {
			_, _ = io.Copy(io.Discard, pres.Body)
			pres.Body.Close()
			continue
		}
		if pres.StatusCode/100 != 2 {
			_ = pres.Body.Close()
			return nil, fmt.Errorf("pair poll: http %d", pres.StatusCode)
		}
		var c Credentials
		if err := json.NewDecoder(pres.Body).Decode(&c); err != nil {
			pres.Body.Close()
			return nil, err
		}
		pres.Body.Close()
		return &c, nil
	}
}

func isAbsoluteURL(s string) bool {
	return len(s) >= 8 && (s[:7] == "http://" || s[:8] == "https://")
}

func bodyReader(b []byte) *bytesBody { return &bytesBody{b: b} }

type bytesBody struct{ b []byte }

func (r *bytesBody) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func (*bytesBody) Close() error { return nil }
```

- [ ] **Step 4: Implement credentials.go (persistence)**

`internal/auth/credentials.go`:

```go
package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultCredentialsPath returns the standard location:
//   POSIX:   $XDG_CONFIG_HOME/claude-switch/credentials.json (or ~/.config/...)
//   Windows: %AppData%/claude-switch/credentials.json
func DefaultCredentialsPath() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("AppData")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "claude-switch", "credentials.json"), nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "claude-switch", "credentials.json"), nil
}

// Save writes credentials with mode 0600. Creates parent directories.
func Save(path string, c *Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	return nil
}

// Load reads credentials from disk. Returns os.ErrNotExist if not paired yet.
func Load(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return &c, nil
}
```

- [ ] **Step 5: Add pair subcommand**

`cmd/claude-switch/pair.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/jleal52/claude-switch/internal/auth"
)

func runPair(ctx context.Context, serverBase string) int {
	host, _ := os.Hostname()
	creds, err := auth.Pair(ctx, auth.PairConfig{
		ServerBase:  serverBase,
		WrapperName: host,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Version:     wrapperVersion,
		Announce: func(code string) {
			fmt.Printf("Pair at:  %s/pair\nCode:     %s\nWaiting...\n", serverBase, code)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pairing failed:", err)
		return 1
	}
	path, err := auth.DefaultCredentialsPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := auth.Save(path, creds); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("Paired. Credentials saved to", path)
	return 0
}
```

Also add `const wrapperVersion = "0.1.0-dev"` to `cmd/claude-switch/main.go` (top-level, used by both `main.go` and `pair.go`).

- [ ] **Step 6: Run tests**

```bash
go test -race ./...
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/auth cmd/claude-switch
git commit -m "feat(auth): device-code pairing + credential persistence"
```

---

## Task 16: CLI entry + config loading

**Goal:** `main.go` reads `config.toml` + env + flags, dispatches to `pair` subcommand or the main `run` mode, wires supervisor + WS client.

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Modify: `cmd/claude-switch/main.go`

- [ ] **Step 1: Add TOML dependency**

```bash
go get github.com/pelletier/go-toml/v2@latest
```

- [ ] **Step 2: Write failing config test**

`internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
server_url = "wss://example.com/ws"
log_level = "debug"
pty_data_flush_ms = 32
pty_data_flush_bytes = 8192
default_cols = 100
default_rows = 40
`), 0o644))

	cfg, err := LoadFromPath(path)
	require.NoError(t, err)
	require.Equal(t, "wss://example.com/ws", cfg.ServerURL)
	require.Equal(t, "debug", cfg.LogLevel)
	require.Equal(t, 32, cfg.PTYDataFlushMs)
	require.Equal(t, 8192, cfg.PTYDataFlushBytes)
	require.Equal(t, uint16(100), cfg.DefaultCols)
	require.Equal(t, uint16(40), cfg.DefaultRows)
}

func TestDefaultsApplied(t *testing.T) {
	cfg, err := LoadFromPath("") // empty path -> all defaults
	require.NoError(t, err)
	require.Equal(t, "info", cfg.LogLevel)
	require.Equal(t, 16, cfg.PTYDataFlushMs)
	require.Equal(t, 16384, cfg.PTYDataFlushBytes)
	require.Equal(t, uint16(120), cfg.DefaultCols)
	require.Equal(t, uint16(32), cfg.DefaultRows)
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	require.NoError(t, os.WriteFile(path, []byte(`server_url = "wss://file"`), 0o644))
	t.Setenv("CLAUDE_SWITCH_SERVER_URL", "wss://env")

	cfg, err := LoadFromPath(path)
	require.NoError(t, err)
	require.Equal(t, "wss://env", cfg.ServerURL)
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/config/...
```

Expected: compile failure.

- [ ] **Step 4: Implement config.go**

`internal/config/config.go`:

```go
// Package config merges TOML file, environment variables, and defaults.
// CLI flags (handled in main) override config file values.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	ServerURL         string `toml:"server_url"          env:"CLAUDE_SWITCH_SERVER_URL"`
	LogLevel          string `toml:"log_level"           env:"CLAUDE_SWITCH_LOG_LEVEL"`
	LogFile           string `toml:"log_file"            env:"CLAUDE_SWITCH_LOG_FILE"`
	PTYDataFlushMs    int    `toml:"pty_data_flush_ms"   env:"CLAUDE_SWITCH_PTY_FLUSH_MS"`
	PTYDataFlushBytes int    `toml:"pty_data_flush_bytes" env:"CLAUDE_SWITCH_PTY_FLUSH_BYTES"`
	DefaultCols       uint16 `toml:"default_cols"        env:"CLAUDE_SWITCH_DEFAULT_COLS"`
	DefaultRows       uint16 `toml:"default_rows"        env:"CLAUDE_SWITCH_DEFAULT_ROWS"`
}

func defaults() Config {
	return Config{
		LogLevel:          "info",
		PTYDataFlushMs:    16,
		PTYDataFlushBytes: 16 * 1024,
		DefaultCols:       120,
		DefaultRows:       32,
	}
}

// LoadFromPath reads the TOML file at path (if non-empty), merges env vars
// on top, and fills defaults for anything still zero.
func LoadFromPath(path string) (*Config, error) {
	cfg := defaults()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("read %s: %w", path, err)
			}
		} else {
			var fromFile Config
			if err := toml.Unmarshal(b, &fromFile); err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
			mergeNonZero(&cfg, &fromFile)
		}
	}
	applyEnv(&cfg)
	return &cfg, nil
}

// DefaultPath returns the OS-specific config path.
func DefaultPath() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("AppData")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "claude-switch", "config.toml"), nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "claude-switch", "config.toml"), nil
}

func mergeNonZero(dst, src *Config) {
	if src.ServerURL != "" {
		dst.ServerURL = src.ServerURL
	}
	if src.LogLevel != "" {
		dst.LogLevel = src.LogLevel
	}
	if src.LogFile != "" {
		dst.LogFile = src.LogFile
	}
	if src.PTYDataFlushMs != 0 {
		dst.PTYDataFlushMs = src.PTYDataFlushMs
	}
	if src.PTYDataFlushBytes != 0 {
		dst.PTYDataFlushBytes = src.PTYDataFlushBytes
	}
	if src.DefaultCols != 0 {
		dst.DefaultCols = src.DefaultCols
	}
	if src.DefaultRows != 0 {
		dst.DefaultRows = src.DefaultRows
	}
}

func applyEnv(dst *Config) {
	if v := os.Getenv("CLAUDE_SWITCH_SERVER_URL"); v != "" {
		dst.ServerURL = v
	}
	if v := os.Getenv("CLAUDE_SWITCH_LOG_LEVEL"); v != "" {
		dst.LogLevel = v
	}
	if v := os.Getenv("CLAUDE_SWITCH_LOG_FILE"); v != "" {
		dst.LogFile = v
	}
	if v := os.Getenv("CLAUDE_SWITCH_PTY_FLUSH_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			dst.PTYDataFlushMs = n
		}
	}
	if v := os.Getenv("CLAUDE_SWITCH_PTY_FLUSH_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			dst.PTYDataFlushBytes = n
		}
	}
	if v := os.Getenv("CLAUDE_SWITCH_DEFAULT_COLS"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 16); err == nil {
			dst.DefaultCols = uint16(n)
		}
	}
	if v := os.Getenv("CLAUDE_SWITCH_DEFAULT_ROWS"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 16); err == nil {
			dst.DefaultRows = uint16(n)
		}
	}
}
```

- [ ] **Step 5: Rewrite main.go**

`cmd/claude-switch/main.go`:

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/jleal52/claude-switch/internal/auth"
	"github.com/jleal52/claude-switch/internal/config"
	"github.com/jleal52/claude-switch/internal/pty"
	"github.com/jleal52/claude-switch/internal/session"
	"github.com/jleal52/claude-switch/internal/ws"
)

const wrapperVersion = "0.1.0-dev"

func main() { os.Exit(run()) }

func run() int {
	fs := flag.NewFlagSet("claude-switch", flag.ExitOnError)
	var (
		configPath = fs.String("config", "", "config file (default: platform-specific)")
		serverURL  = fs.String("server-url", "", "override server WebSocket URL")
		logLevel   = fs.String("log-level", "", "override log level")
		claudeBin  = fs.String("command", "", "override path to claude binary (testing)")
	)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  claude-switch [flags]         run the wrapper")
		fmt.Fprintln(os.Stderr, "  claude-switch pair <server>   pair with a server by device code")
		fs.PrintDefaults()
	}

	// Subcommand dispatch.
	if len(os.Args) >= 2 && os.Args[1] == "pair" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "pair requires a server base URL")
			return 2
		}
		return runPair(signalCtx(), os.Args[2])
	}
	_ = fs.Parse(os.Args[1:])

	// Load config.
	cfgPath := *configPath
	if cfgPath == "" {
		p, err := config.DefaultPath()
		if err == nil {
			cfgPath = p
		}
	}
	cfg, err := config.LoadFromPath(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 1
	}
	if *serverURL != "" {
		cfg.ServerURL = *serverURL
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}

	// Logger.
	lvl := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))

	// Load credentials (pair-if-needed is left to explicit subcommand).
	credsPath, err := auth.DefaultCredentialsPath()
	if err != nil {
		slog.Error("credentials path", "err", err)
		return 1
	}
	creds, err := auth.Load(credsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "not paired — run: claude-switch pair <server-base-url>")
			return 2
		}
		slog.Error("load credentials", "err", err)
		return 1
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = creds.ServerEndpoint
	}

	// Locate claude binary.
	bin := *claudeBin
	if bin == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			fmt.Fprintln(os.Stderr, "`claude` not on PATH; use --command to override")
			return 1
		}
		bin = p
	}

	// Build supervisor.
	events := make(chan session.Event, 256)
	sup := session.NewSupervisor(session.Config{
		ClaudeBin: bin,
		Start:     pty.Start,
	}, events)

	// Wrapper ID: hostname + 4 hex bytes of PID.
	host, _ := os.Hostname()
	wid := fmt.Sprintf("%s-%x", filepath.Base(host), os.Getpid()&0xffff)

	cli := ws.NewClient(ws.Config{
		URL:       cfg.ServerURL,
		Token:     creds.AccessToken,
		WrapperID: wid,
		Version:   wrapperVersion,
	}, sup, events)

	ctx := signalCtx()
	go sup.Run(ctx)
	if err := cli.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("ws run", "err", err)
		return 1
	}
	return 0
}

func signalCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() { <-ch; cancel() }()
	_ = runtime.GOOS // silence import if not used on a platform
	return ctx
}
```

- [ ] **Step 6: Run all tests**

```bash
go test -race ./...
go build ./...
```

Expected: all PASS and `bin/claude-switch` builds.

- [ ] **Step 7: Commit**

```bash
git add internal/config cmd/claude-switch go.mod go.sum
git commit -m "feat(cli): main entry wires config, auth, supervisor, ws client"
```

---

## Task 17: JSONL discovery + tail integration into supervisor

**Goal:** After spawning `claude`, the supervisor discovers the new `.jsonl` and emits an updated `SessionStartedEvent` with `JSONLUUID`, then starts a tailer goroutine that feeds `JSONLTailEvent`s.

**Files:**
- Modify: `internal/session/supervisor.go` (hook discover + tail)
- Create: `internal/session/jsonl_integration_test.go`

- [ ] **Step 1: Write failing integration test**

`internal/session/jsonl_integration_test.go`:

```go
package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSupervisorEmitsJSONLUUIDAfterDiscovery(t *testing.T) {
	// Build a fake "claude home" with projects/<slug>/ and simulate a new
	// jsonl appearing shortly after open.
	claudeHome := t.TempDir()
	cwd := t.TempDir()
	projects := filepath.Join(claudeHome, "projects")
	require.NoError(t, os.MkdirAll(projects, 0o755))

	events := make(chan Event, 32)
	sup := NewSupervisor(Config{
		Start:      fakeStartFn,
		ClaudeBin:  "/bin/true",
		ClaudeHome: claudeHome,
	}, events)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go sup.Run(ctx)

	require.NoError(t, sup.Open(ctx, "s", cwd, "default", nil))

	// Compute the slug the way Claude does.
	slug := slugForTest(cwd)
	projDir := filepath.Join(projects, slug)
	require.NoError(t, os.MkdirAll(projDir, 0o755))

	// Simulate claude creating its jsonl 100 ms later.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(projDir, "abc123.jsonl"), []byte("{}\n"), 0o644)
	}()

	// We should see a SessionStartedEvent first (empty JSONLUUID), then a
	// follow-up event with the UUID populated.
	gotUUID := false
	deadline := time.After(3 * time.Second)
	for !gotUUID {
		select {
		case e := <-events:
			if ss, ok := e.(SessionStartedEvent); ok && ss.JSONLUUID != "" {
				require.Equal(t, "abc123", ss.JSONLUUID)
				gotUUID = true
			}
		case <-deadline:
			t.Fatal("never saw SessionStartedEvent with JSONLUUID")
		}
	}
}

func slugForTest(cwd string) string {
	// Mirror internal/tail/discover.go's slugifyCwd (kept in the test so
	// that refactoring the real one does not silently break this test).
	out := cwd
	for _, c := range []string{":", "/", "\\"} {
		out = replaceAll(out, c, "-")
	}
	return out
}

func replaceAll(s, old, new string) string {
	for {
		idx := indexOf(s, old)
		if idx < 0 {
			return s
		}
		s = s[:idx] + new + s[idx+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/session/...
```

Expected: failure (no `ClaudeHome` field on Config, no discovery yet).

- [ ] **Step 3: Extend Config + Open flow**

Modify `internal/session/supervisor.go`. Add `ClaudeHome string` to `Config`. In `Open`, capture `now := time.Now()` before `Start`, and AFTER `emit(SessionStartedEvent{})`, launch a goroutine:

```go
if s.cfg.ClaudeHome != "" {
    go s.discoverAndTail(ctx, sess, now)
}
```

Add the method:

```go
func (s *Supervisor) discoverAndTail(ctx context.Context, sess *Session, notBefore time.Time) {
    projDir := tail.ProjectDirForCwd(s.cfg.ClaudeHome, sess.Cwd)
    path, err := tail.WaitForNewJSONL(ctx, projDir, notBefore)
    if err != nil {
        return
    }
    uuid := filenameStem(path)
    sess.JSONLUUID = uuid
    emit(s.events, SessionStartedEvent{
        Session: sess.ID, PID: sess.PID(), JSONLUUID: uuid, Cwd: sess.Cwd, Account: sess.Account,
    })
    _ = tail.Tail(ctx, path, func(line string) {
        emit(s.events, JSONLTailEvent{Session: sess.ID, Entry: line})
    })
}

func filenameStem(p string) string {
    base := filepath.Base(p)
    if ext := filepath.Ext(base); ext != "" {
        base = base[:len(base)-len(ext)]
    }
    return base
}
```

Add imports: `"path/filepath"` and `"github.com/jleal52/claude-switch/internal/tail"`.

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/session/...
```

Expected: `TestSupervisorEmitsJSONLUUIDAfterDiscovery` PASSes along with the existing supervisor tests.

- [ ] **Step 5: Wire ClaudeHome in main.go**

Modify `cmd/claude-switch/main.go`. Before `session.NewSupervisor`, compute:

```go
home, _ := os.UserHomeDir()
claudeHome := filepath.Join(home, ".claude")
```

And set `session.Config{..., ClaudeHome: claudeHome}`.

- [ ] **Step 6: Commit**

```bash
git add internal/session cmd/claude-switch
git commit -m "feat(session): discover session jsonl, tail for JSONLTail events"
```

---

## Task 18: End-to-end integration test

**Goal:** Stand up a fake server on a real WebSocket, connect the wrapper to it, drive `open_session` → `pty.input` → `pty.data` round-trip through a fake shell (the `--command /bin/echo` trick on POSIX, `cmd.exe /c echo ...` on Windows via a separate test file).

**Files:**
- Create: `cmd/claude-switch/main_integration_test.go`

- [ ] **Step 1: Write failing integration test**

`cmd/claude-switch/main_integration_test.go`:

```go
//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/pty"
	"github.com/jleal52/claude-switch/internal/session"
	"github.com/jleal52/claude-switch/internal/ws"
)

func TestEndToEndOpenWriteReceive(t *testing.T) {
	// ULID for the session the "server" will open.
	const sessID = "01HABCDEFGHIJKLMNOPQRSTUV0" // any valid 26-char Crockford ULID
	// We'll craft a real one instead of hard-coding.
	sid := ulidString(t)

	srvGotData := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		require.NoError(t, err)
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		// Read hello.
		typ, data, err := c.Read(ctx)
		require.NoError(t, err)
		require.Equal(t, websocket.MessageText, typ)
		tt, _, _, err := proto.Decode(data)
		require.NoError(t, err)
		require.Equal(t, proto.TypeHello, tt)

		// Send open_session.
		openRaw, _ := proto.Encode(proto.TypeOpenSession, sid, proto.OpenSession{
			Session: sid, Cwd: os.TempDir(), Account: "default",
			Args: []string{"-c", `read l; echo got:$l`},
		})
		require.NoError(t, c.Write(ctx, websocket.MessageText, openRaw))

		// Read until we've seen session.started, then send pty.input, then
		// collect pty.data until we see "got:hello".
		sentInput := false
		for ctx.Err() == nil {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageText {
				tt, _, _, _ := proto.Decode(data)
				if tt == proto.TypeSessionStarted && !sentInput {
					// send keystrokes via binary pty.input
					id, err := ulidFromString(sid)
					require.NoError(t, err)
					require.NoError(t, c.Write(ctx, websocket.MessageBinary, proto.EncodePTYData(id, []byte("hello\n"))))
					sentInput = true
				}
				continue
			}
			// Binary = pty.data.
			_, payload, err := proto.DecodePTYData(data)
			require.NoError(t, err)
			if strings.Contains(string(payload), "got:hello") {
				srvGotData <- "got:hello"
				return
			}
		}
	}))
	defer srv.Close()
	wsURL := "ws" + srv.URL[len("http"):]

	events := make(chan session.Event, 64)
	sup := session.NewSupervisor(session.Config{
		ClaudeBin: "/bin/sh",
		BaseArgs:  nil, // allow server-supplied args as-is
		Start:     pty.Start,
	}, events)

	cli := ws.NewClient(ws.Config{
		URL:       wsURL,
		Token:     "t",
		WrapperID: "w-" + runtime.GOOS,
		Version:   "test",
	}, sup, events)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go sup.Run(ctx)
	go func() { _ = cli.Run(ctx) }()

	select {
	case got := <-srvGotData:
		require.Equal(t, "got:hello", got)
	case <-ctx.Done():
		t.Fatal("did not receive expected pty.data")
	}
	_ = filepath.Base // silence import if needed
}
```

*(`ulidString` and `ulidFromString` are small helpers you add at the bottom of the test file; they wrap `github.com/oklog/ulid/v2`'s `Make().String()` and `ulid.ParseStrict(s)`.)*

- [ ] **Step 2: Add helpers**

Append to the same test file:

```go
import "github.com/oklog/ulid/v2"

func ulidString(t *testing.T) string { t.Helper(); return ulid.Make().String() }
func ulidFromString(s string) (ulid.ULID, error) { return ulid.ParseStrict(s) }
```

- [ ] **Step 3: Run the test**

```bash
go test ./cmd/claude-switch/...
```

Expected: PASS on Linux/macOS. The `//go:build !windows` tag excludes Windows (ConPTY fake shell handled in a separate test if desired; not required for this subsystem's merge).

- [ ] **Step 4: Commit**

```bash
git add cmd/claude-switch
git commit -m "test(e2e): open_session → pty.input → pty.data round-trip over real ws"
```

---

## Task 19: Release workflow + goreleaser

**Goal:** Automated cross-compile and GitHub release on tag push. Produces `claude-switch_{version}_{os}_{arch}.tar.gz` (or `.zip`) for linux/macos/windows × amd64/arm64.

**Files:**
- Create: `.goreleaser.yaml`
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Add goreleaser config**

`.goreleaser.yaml`:

```yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - id: claude-switch
    main: ./cmd/claude-switch
    binary: claude-switch
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ignore:
      - goos: windows
        goarch: arm64

archives:
  - formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"

release:
  github:
    owner: jleal52
    name: claude-switch
```

- [ ] **Step 2: Add release workflow**

`.github/workflows/release.yml`:

```yaml
name: Release
on:
  push:
    tags: ["v*"]
permissions:
  contents: write
jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 3: Verify locally (dry-run)**

```bash
go install github.com/goreleaser/goreleaser/v2@latest
goreleaser release --snapshot --clean
```

Expected: produces binaries in `dist/`, no real GitHub release.

- [ ] **Step 4: Commit**

```bash
git add .goreleaser.yaml .github/workflows/release.yml
git commit -m "ci: goreleaser cross-compile + GitHub release on tag"
```

---

## Task 20: Close remaining spec gaps

**Goal:** Four spec requirements not covered earlier: (1) Windows Job Object wired into main, (2) access-token refresh + re-pair on revoked, (3) read deadline so the wrapper notices a dead server, (4) unit test for ring-buffer replay on reconnect.

**Files:**
- Modify: `cmd/claude-switch/main.go` (wire Job on Windows)
- Create: `internal/auth/refresh.go`
- Create: `internal/auth/refresh_test.go`
- Modify: `cmd/claude-switch/main.go` (refresh before connect, re-pair on 401)
- Modify: `internal/ws/client.go` (read deadline via ping tracking)
- Create: `internal/ws/replay_test.go`

- [ ] **Step 1: Job Object wiring on Windows (unit test first)**

`cmd/claude-switch/job_windows.go`:

```go
//go:build windows

package main

import "github.com/jleal52/claude-switch/internal/process"

func newJob() (jobHandle, error) {
	j, err := process.NewKillOnCloseJob()
	if err != nil {
		return nil, err
	}
	return j, nil
}

type jobHandle = interface {
	Assign(cmd interface{ GetProcess() interface{} }) error
	Close() error
}
```

Actually simpler — drop the interface adapter and wire `session.Config.Job` directly. Replace the above with:

`cmd/claude-switch/job_windows.go`:

```go
//go:build windows

package main

import (
	"os/exec"

	"github.com/jleal52/claude-switch/internal/process"
)

func createJob() (jobCloser, func(cmd *exec.Cmd) error, error) {
	j, err := process.NewKillOnCloseJob()
	if err != nil {
		return nil, nil, err
	}
	return j, j.Assign, nil
}

type jobCloser interface{ Close() error }
```

`cmd/claude-switch/job_other.go`:

```go
//go:build !windows

package main

import "os/exec"

func createJob() (jobCloser, func(cmd *exec.Cmd) error, error) {
	return noopCloser{}, func(*exec.Cmd) error { return nil }, nil
}

type jobCloser interface{ Close() error }

type noopCloser struct{}

func (noopCloser) Close() error { return nil }
```

Then in `cmd/claude-switch/main.go`, just before building the supervisor:

```go
job, assignToJob, err := createJob()
if err != nil {
    slog.Error("create job object", "err", err)
    return 1
}
defer job.Close()

sup := session.NewSupervisor(session.Config{
    ClaudeBin:  bin,
    Start:      pty.Start,
    ClaudeHome: claudeHome,
    Job:        jobAdapter{assign: assignToJob},
}, events)
```

And add `jobAdapter` to `main.go`:

```go
type jobAdapter struct {
	assign func(*exec.Cmd) error
}

func (j jobAdapter) Assign(cmd *exec.Cmd) error { return j.assign(cmd) }
```

- [ ] **Step 2: Build + test**

```bash
go build ./...
go test -race ./...
```

Expected: compiles on all three OSes; tests still green.

- [ ] **Step 3: Commit Job wiring**

```bash
git add cmd/claude-switch
git commit -m "feat(main): wire Windows Job Object so children die with wrapper"
```

- [ ] **Step 4: Write failing test for token refresh**

`internal/auth/refresh_test.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRefreshHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/device/token/refresh", r.URL.Path)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "ref-xyz", body["refresh_token"])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "tok-new",
			"refresh_token": "ref-new",
			"expires_at":    time.Now().Add(time.Hour).Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	got, err := Refresh(context.Background(), srv.URL, "ref-xyz", nil)
	require.NoError(t, err)
	require.Equal(t, "tok-new", got.AccessToken)
	require.Equal(t, "ref-new", got.RefreshToken)
}

func TestRefreshRevokedReturnsErrRevoked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"revoked"}`))
	}))
	defer srv.Close()

	_, err := Refresh(context.Background(), srv.URL, "ref-xyz", nil)
	require.ErrorIs(t, err, ErrRevoked)
}
```

- [ ] **Step 5: Run to verify it fails**

```bash
go test ./internal/auth/...
```

Expected: `undefined: Refresh`, `undefined: ErrRevoked`.

- [ ] **Step 6: Implement refresh.go**

`internal/auth/refresh.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var ErrRevoked = errors.New("auth: refresh token revoked")

// Refresh exchanges a refresh token for a new access+refresh pair.
// Returns ErrRevoked on HTTP 401 so the caller can delete credentials
// and prompt the user to re-pair.
func Refresh(ctx context.Context, serverBase, refreshToken string, hc *http.Client) (*Credentials, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverBase+"/device/token/refresh", bodyReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrRevoked
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("refresh: http %d", resp.StatusCode)
	}
	var c Credentials
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
```

- [ ] **Step 7: Hook refresh into main.go**

In `cmd/claude-switch/main.go`, between loading credentials and building the WS client, insert:

```go
// Refresh access token if it's near expiry (5 min margin) or expired.
if time.Now().Add(5 * time.Minute).After(creds.ExpiresAt) {
    // Infer serverBase from server_endpoint: strip "wss://" / "ws://" prefix
    // and the path suffix (e.g. "/ws"). This is brittle; acceptable because
    // subsystem 2 will persist server_base explicitly alongside endpoint.
    serverBase := httpBaseFromWs(creds.ServerEndpoint)
    refreshed, err := auth.Refresh(context.Background(), serverBase, creds.RefreshToken, nil)
    if err != nil {
        if errors.Is(err, auth.ErrRevoked) {
            _ = os.Remove(credsPath)
            fmt.Fprintln(os.Stderr, "credentials revoked — run: claude-switch pair <server-base-url>")
            return 2
        }
        slog.Error("token refresh", "err", err)
        return 1
    }
    refreshed.ServerEndpoint = creds.ServerEndpoint
    if err := auth.Save(credsPath, refreshed); err != nil {
        slog.Error("save refreshed creds", "err", err)
        return 1
    }
    creds = refreshed
}
```

And add a helper:

```go
func httpBaseFromWs(endpoint string) string {
    base := endpoint
    if strings.HasPrefix(base, "wss://") {
        base = "https://" + base[len("wss://"):]
    } else if strings.HasPrefix(base, "ws://") {
        base = "http://" + base[len("ws://"):]
    }
    // Strip trailing path (e.g. /ws).
    if i := strings.LastIndex(base, "/"); i > len("https://") {
        base = base[:i]
    }
    return base
}
```

Add imports `"strings"` and `"time"` to `main.go` if not already there.

- [ ] **Step 8: Run tests**

```bash
go test -race ./...
```

Expected: all tests PASS, including the two new refresh tests.

- [ ] **Step 9: Commit refresh**

```bash
git add internal/auth cmd/claude-switch
git commit -m "feat(auth): token refresh with re-pair on revoked"
```

- [ ] **Step 10: Write failing test for inbound-read deadline**

`internal/ws/reconnect_test.go` — append:

```go
import "context"

func TestRunOnceExitsWhenNoPingArrives(t *testing.T) {
	// Server accepts but never writes anything. The wrapper should give up
	// after ~ReadTimeout (set to 100 ms for the test) rather than block
	// forever.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := websocket.Accept(w, r, nil)
		defer c.CloseNow()
		_, _, _ = c.Read(r.Context()) // consume hello; then idle until close.
		<-r.Context().Done()
	}))
	defer srv.Close()

	events := make(chan session.Event, 4)
	sup := session.NewSupervisor(session.Config{ClaudeBin: "/bin/true"}, events)
	cli := NewClient(Config{
		URL:         "ws" + srv.URL[len("http"):],
		Token:       "t",
		WrapperID:   "w",
		Version:     "test",
		ReadTimeout: 100 * time.Millisecond,
	}, sup, events)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := cli.runOnce(ctx)
	require.Error(t, err) // a timeout, not a clean exit
}
```

*(Move the `import "net/http"`, `"net/http/httptest"`, `"github.com/coder/websocket"` imports alongside existing ones in the file.)*

- [ ] **Step 11: Run test to verify it fails**

```bash
go test ./internal/ws/...
```

Expected: unknown field `ReadTimeout` in `Config`.

- [ ] **Step 12: Implement read deadline**

Modify `internal/ws/client.go`:

- Add to `Config`:
  ```go
  ReadTimeout time.Duration // per-read timeout; default 45s
  ```
- In `runOnce`, before the read loop:
  ```go
  timeout := c.cfg.ReadTimeout
  if timeout == 0 {
      timeout = 45 * time.Second
  }
  ```
- Change `readLoop` to use a per-read context:
  ```go
  func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, timeout time.Duration) error {
      for {
          rctx, cancel := context.WithTimeout(ctx, timeout)
          typ, data, err := conn.Read(rctx)
          cancel()
          if err != nil {
              return err
          }
          // ...existing dispatch...
      }
  }
  ```
- Pass `timeout` when launching the goroutine inside `runOnce`:
  ```go
  go func() { readErr <- c.readLoop(ctx, conn, timeout) }()
  ```

The spec's "fail after 2 missed pongs" translates into practice as "if the server's 20 s-interval pings stop arriving, we'll hit the 45 s read deadline and reconnect." Good enough for MVP; finer-grained pong tracking can come later.

- [ ] **Step 13: Run tests**

```bash
go test -race ./internal/ws/...
```

Expected: new test PASSes, existing tests still PASS.

- [ ] **Step 14: Commit read deadline**

```bash
git add internal/ws
git commit -m "feat(ws): per-read timeout so dead server triggers reconnect"
```

- [ ] **Step 15: Write failing test for ring-buffer replay on reconnect**

`internal/ws/replay_test.go`:

```go
package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/session"
)

func TestReplaySendsRingContentsAfterHello(t *testing.T) {
	var gotReplay atomic.Bool
	replayPayload := []byte("replay-bytes-xyz")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := websocket.Accept(w, r, nil)
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		// 1. hello.
		_, data, err := c.Read(ctx)
		require.NoError(t, err)
		typ, _, _, err := proto.Decode(data)
		require.NoError(t, err)
		require.Equal(t, proto.TypeHello, typ)

		// 2. immediately after hello, expect a binary pty.data replay frame.
		msgType, data, err := c.Read(ctx)
		require.NoError(t, err)
		require.Equal(t, websocket.MessageBinary, msgType)
		_, payload, err := proto.DecodePTYData(data)
		require.NoError(t, err)
		if string(payload) == string(replayPayload) {
			gotReplay.Store(true)
		}
	}))
	defer srv.Close()

	events := make(chan session.Event, 4)
	sup := session.NewSupervisor(session.Config{ClaudeBin: "/bin/true"}, events)

	// Pre-populate a session with ring contents.
	sid := ulid.Make().String()
	_ = sup.InjectForTest(sid, replayPayload) // test helper added below.

	cli := NewClient(Config{
		URL: "ws" + srv.URL[len("http"):], Token: "t", WrapperID: "w", Version: "test",
	}, sup, events)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = cli.runOnce(ctx)
	require.True(t, gotReplay.Load(), "server did not receive replay frame")
}
```

- [ ] **Step 16: Add `InjectForTest` helper to session package**

Append to `internal/session/session.go`:

```go
// InjectForTest pre-populates a session row with ring contents. Used only
// by ws package tests that want to assert replay behavior without spawning
// a real PTY. Callers MUST NOT use this in production code paths.
func (s *Supervisor) InjectForTest(id string, ringSeed []byte) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if _, exists := s.sessions[id]; exists {
        return ErrSessionExists
    }
    sess := &Session{
        ID: id, Cwd: "/tmp", Account: "default", Created: time.Now(),
        inbox: make(chan []byte, 1), stop: make(chan struct{}),
        ring:  ring.New(RingBytes),
    }
    _, _ = sess.ring.Write(ringSeed)
    s.sessions[id] = sess
    return nil
}
```

- [ ] **Step 17: Run tests**

```bash
go test -race ./...
```

Expected: the replay test PASSes along with everything else.

- [ ] **Step 18: Commit replay test**

```bash
git add internal/session internal/ws
git commit -m "test(ws): verify ring-buffer replay frame follows hello on connect"
```

---

## Final steps (post-plan execution)

After every task is green:

1. Run the full suite once more: `go test -race ./...`
2. Run the linter: `golangci-lint run ./...`
3. Build each target OS from the CI matrix one last time locally if possible.
4. Tag `v0.1.0` and push. CI + release workflow should produce binaries.
5. Hand off to subsystem 2 (server) with the wrapper binary + this spec as the contract.

---

## Notes for the implementer

- **Do not add features not in the spec.** Multi-account, SIGSTOP throttling, token encryption at rest, metrics — all out of scope for subsystem 1.
- **Every task must leave `go test ./...` green.** If a test fails on a platform the code should support, stop and fix.
- **Keep files focused.** If a file grows past ~400 lines during implementation, split before committing.
- **Commit messages follow the subject lines shown in each task.** Keep them short (≤70 chars), imperative mood.
- **Cross-platform tests run in CI.** Don't "fix" a test by adding build tags unless the spec says that code path is platform-specific.
