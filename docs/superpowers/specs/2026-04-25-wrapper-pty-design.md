# Wrapper PTY — Design

**Project:** claude-switch — subsystem 1 of 4
**Status:** Design (for review)
**Date:** 2026-04-25

## Goal

Build a native binary ("wrapper") that runs on a user's machine, hosts N pseudo-terminal (PTY) sessions of the `claude` CLI, and exposes each session's I/O to a central server over a single outbound WebSocket. The server (subsystem 2) relays I/O to a browser frontend (subsystem 3). This subsystem delivers only the wrapper; the server and frontend are later subsystems and are not implemented here.

Success = a user on machine M can run `claude-switch` and, from a remote browser connected to the server, open a new `claude` session on machine M, send keystrokes, and see the terminal output — with session continuity as long as the wrapper stays connected.

## Non-goals (explicitly out of scope for subsystem 1)

- Multi-account management. Only the user's logged-in `claude` account is used. The protocol reserves `account: "default"` on `open_session` so subsystem 4 can extend without a breaking change.
- Server, database, frontend, authentication of end users. Out here.
- `--resume` of the PTY stream after the wrapper restarts. Dead-session reopen is a frontend UX concern built on top of the catalog; the wrapper itself does not adopt orphan processes.
- Windows Scheduled Task / systemd service installation. The wrapper is just a binary; packaging as a service is a separate concern.

## Architecture

```
 ┌───────────────────────┐        WebSocket           ┌───────────────┐         HTTP/WS          ┌──────────────┐
 │  wrapper (Go binary)  │  ─────────────────────────▶│  server       │◀───────────────────────▶│  browser     │
 │  on user's machine    │   outbound, multiplexed    │  (subsys. 2)  │                          │  (subsys. 3) │
 └───────────────────────┘                            └───────────────┘                          └──────────────┘
          │                                                  ▲
          │ spawns PTYs                                      │ catalog of all sessions
          ▼                                                  │ (live + dead + historical)
 ┌──────────────────┐
 │  claude (PID X)  │  ×N
 │  inside its PTY  │
 └──────────────────┘
          │
          ▼
  ~/.claude/projects/<proj>/<uuid>.jsonl   (tailed for transcript channel)
```

Key properties:

- Wrapper opens **one outbound WebSocket** to the server. NAT-friendly, one connection per wrapper, no inbound ports to open.
- Wrapper hosts **N processes** concurrently (multiplexor), routing frames by `session` id.
- Process `claude` runs inside a persistent PTY per session. A process stays alive as long as the wrapper is connected; it is only killed by (a) `close_session` from the server, (b) the process exiting on its own, or (c) wrapper shutdown (which cascades via `PR_SET_PDEATHSIG` on Linux and a Windows Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`).
- Authority is split: wrapper owns processes + local `.jsonl` files; server owns the public catalog (mapping public `session-id` → wrapper + local UUID + account + cwd + status). Neither duplicates the other.

## Wire protocol

### Envelope

Every frame sent over the WebSocket is an envelope with two forms. JSON for control, binary for raw PTY data. The JSON form:

```json
{
  "v": 1,
  "type": "<frame-type>",
  "session": "<session-id | null for non-session frames>",
  "payload": { /* per-type */ }
}
```

The binary form is used only for `pty.data` frames (the hot path). To avoid per-frame JSON overhead for hundreds of bytes of terminal output, binary frames are length-prefixed:

```
byte 0        : 0x01  (version)
bytes 1..16   : session id (ULID, 16 bytes)
bytes 17..end : raw PTY output bytes
```

Client and server MUST support both WebSocket opcode `text` (JSON envelope) and `binary` (pty.data). A WS frame = exactly one logical frame; no chunking across WS frames.

### Control frame types (JSON)

All frames are versioned by the envelope's `v`. Version 1 defines:

**Wrapper → server:**

| type                | payload                                                                              | when                                            |
| ------------------- | ------------------------------------------------------------------------------------ | ----------------------------------------------- |
| `hello`             | `{wrapper_id, os, arch, version, accounts: ["default"], capabilities: ["pty"]}`      | first frame after auth, once per connection     |
| `session.started`   | `{pid, jsonl_uuid, cwd, account}`                                                    | after wrapper spawned the PTY and it is ready   |
| `session.exited`    | `{exit_code, reason: "normal" \| "signal" \| "wrapper_close"}`                       | when the `claude` process ends                  |
| `pty.control_event` | `{event: "resize_ack" \| "error", detail: string}`                                   | as needed                                       |
| `jsonl.tail`        | `{entry: <raw jsonl line>}`                                                          | (optional) each new line in the session's jsonl |
| `pong`              | `{echo: <value from ping>}`                                                          | reply to server ping                            |

**Server → wrapper:**

| type            | payload                                                                                                    | effect                                             |
| --------------- | ---------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| `open_session`  | `{session, cwd, account: "default", args?: string[]}`                                                      | spawn `claude` in a PTY with that `cwd`            |
| `pty.input`     | *(binary frame)*                                                                                           | write bytes to the PTY stdin                       |
| `pty.resize`    | `{cols, rows}`                                                                                             | `TIOCSWINSZ` on the PTY; ack via `pty.control_event`|
| `close_session` | `{}`                                                                                                       | SIGTERM to the process, then SIGKILL after 5s      |
| `ping`          | `{nonce}`                                                                                                  | heartbeat                                          |

### Invariants

- `session` id is assigned **by the server** and included in `open_session`. The wrapper never generates session ids. This keeps the server as the source of truth for the public catalog.
- `jsonl_uuid` is reported by the wrapper after the `claude` process writes its first entry; the server stores this in the catalog for the historical-transcript feature.
- `pty.data` (binary) is sent by the wrapper as bytes arrive from the PTY, with short coalescing (≤16 ms or ≤16 KiB) to reduce frame overhead without noticeable latency.

## Wrapper internals

### Process model (Go, single binary)

Each PTY session is managed by three goroutines:

1. **reader**: reads bytes from the PTY master, coalesces into `pty.data` frames, enqueues onto the shared write queue.
2. **writer**: drains a per-session inbox of `pty.input` byte-chunks and writes them into the PTY master.
3. **jsonl tailer** (optional): reads the new session's `.jsonl` file as it grows, emits `jsonl.tail` frames. Enabled by default; disable with `--no-jsonl-tail`.

A single **ws-writer** goroutine drains a global priority queue of outbound frames: JSON control frames are high-priority (small, infrequent), binary `pty.data` frames are lower priority. One **ws-reader** goroutine dispatches incoming frames to the right per-session inbox.

A **supervisor** goroutine owns session lifecycle: reacts to `open_session` / `close_session` from the ws-reader, starts/stops the per-session goroutines, maintains the session table, handles process-exit signals.

### Session table

In-memory only. No local SQLite. If the wrapper restarts, the table is empty; the server will learn about the dead sessions when the wrapper connects and sends `hello` (i.e. there's no state to reconcile, so `hello` is authoritative for the wrapper's current reality).

```go
type Session struct {
    ID         string          // server-assigned
    Cmd        *exec.Cmd
    PTY        *os.File        // PTY master fd (POSIX) / ConPTY handle (Windows)
    Cwd        string
    Account    string          // "default" in MVP
    JsonlUUID  string          // discovered after spawn
    Created    time.Time
    InboxCh    chan []byte     // bytes from server → PTY
    StopCh     chan struct{}
}
```

### Child lifecycle

- On spawn: the wrapper creates the PTY, allocates a Session, and execs `claude` (resolved from PATH — same convention as claude-hub). Env is inherited from the wrapper; `TERM=xterm-256color` set explicitly.
- Children are tied to the wrapper's life:
  - Linux: each child sets `PR_SET_PDEATHSIG` to SIGTERM in its pre-exec fork handler.
  - macOS: no `PDEATHSIG` equivalent; accept that children may orphan on wrapper crash (rare; SIGKILL of wrapper PID is the only case). For clean exits the wrapper sends SIGTERM to each child in a shutdown handler, with a 5 s timeout then SIGKILL.
  - Windows: **before spawning any child**, the wrapper creates a Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, then assigns its own process to the job so the handle is held by the wrapper itself. Every spawned child is `AssignProcessToJobObject`'d immediately after `CreateProcess` with `CREATE_SUSPENDED`, then resumed. This closes the race where a child would run briefly outside the job if the wrapper died between spawn and assign. When the wrapper process terminates for any reason, the last handle to the job closes and the OS kills every child.

### PTY libraries

- POSIX: `github.com/creack/pty` (stable, widely used).
- Windows (ConPTY): `github.com/UserExistsError/conpty` or `github.com/ActiveState/termtest/conpty` — pick in implementation after a small compatibility test.
- WebSocket: `nhooyr.io/websocket` (small, context-aware, supports per-message deflate; preferred over gorilla which is on life support).

## Authentication — device-code flow

First run on a machine:

```
$ claude-switch
claude-switch isn't paired with a server yet.
Pair at:  https://server.example.com/pair
Code:     ABCD-1234
Waiting...
```

Flow:

1. Wrapper `POST /device/pair/start` with `{wrapper_id, name: "<hostname>", os, arch, version}`. Gets `{code, poll_url, expires_in}`.
2. Wrapper prints the `code` and begins polling `poll_url` every 5 s.
3. User opens `https://server/pair`, logs in (the server's regular auth — not this subsystem's problem), enters the code.
4. Server returns `{access_token, refresh_token, expires_at, server_endpoint}` on the next poll.
5. Wrapper writes credentials to `~/.config/claude-switch/credentials.json` (mode `0600`). Disconnects the pairing transport and opens the main WebSocket to `server_endpoint` using the `access_token` in the `Authorization: Bearer …` header of the WS upgrade request.

Subsequent runs: the wrapper reads `credentials.json` and connects directly. If the access token is expired, it refreshes via `POST /device/token/refresh` before connecting. If the refresh fails with "revoked", it deletes `credentials.json` and re-enters pairing mode.

## Configuration

`~/.config/claude-switch/config.toml`:

```toml
# Server endpoint (can be overridden by pairing response).
server_url = "wss://server.example.com/ws"

# Logging.
log_level = "info"              # trace | debug | info | warn | error
log_file  = ""                  # "" = stderr only

# Coalescing knobs for pty.data framing.
pty_data_flush_ms    = 16
pty_data_flush_bytes = 16384

# PTY default size until server sends resize.
default_cols = 120
default_rows = 32
```

All values overridable by env vars with `CLAUDE_SWITCH_` prefix and by CLI flags where practical (`--server-url`, `--log-level`).

## Reconnection

- WebSocket disconnect (any cause): exponential backoff with jitter, base 1 s, cap 60 s. Ping every 20 s (`ping` frame above), fail after 2 missed pongs.
- **Children keep running while disconnected.** The WS going away does NOT kill the PTY sessions — they are kept alive (producing output, which is captured into the ring buffer) until the wrapper itself exits or the server explicitly asks for `close_session`. This is what Q8 buys us: "wrapper connected" is the liveness condition, not "WS currently up".
- On reconnect, the wrapper sends a fresh `hello`. The `hello` envelope carries a `sessions` array with each alive session's `{id, pid, jsonl_uuid, cwd, account, bytes_since_disconnect}`. The server reconciles:
  - Sessions in the server catalog as "alive on this wrapper" that are missing from `hello.sessions` → marked dead (child died while the WS was down).
  - Sessions in `hello.sessions` that the server already knows → resume, wrapper streams ring-buffer contents first, then live output.
  - Sessions in `hello.sessions` that the server does NOT know → the wrapper closes them locally (defensive: if the server's catalog is the public truth, a session the server doesn't recognize has no public identity and should not keep running). This is rare but possible if the server's catalog storage was wiped.
- Per-session ring buffer of 64 KiB of `pty.data` keeps the most recent output; re-sent at the start of each reconnection so the browser sees "what it missed" if re-connect is fast. Older bytes are lost; the browser falls back to the `.jsonl` transcript for full history.

## Error handling surface

- Spawn failure (e.g. `claude` not on PATH): reply to the `open_session` with `session.exited {exit_code: -1, reason: "spawn_failed", detail: "..."}` and never emit `session.started`.
- PTY write failure: emit `pty.control_event {event: "error", detail: "write failed: ..."}` and keep the session alive (the user can still read output).
- PTY read EOF (child exited): emit `session.exited` with the process's real exit code and close the session state.
- Auth failure at WS upgrade: the wrapper retries with a fresh token (refresh), then falls back to re-pairing if refresh fails.

## Observability

- Structured logs (`logfmt` or JSON; stdlib `log/slog` is fine) with fields: `session`, `pid`, `frame_type`, `bytes`.
- Optional `--debug-file <path>` dumps every frame envelope (redacting `pty.data` bodies) for debugging protocol issues.
- No metrics endpoint for MVP (that's the server's concern).

## Testing strategy

Table of test types and what each proves. Writing-plans will expand these into concrete TDD steps.

| Layer               | Test type               | What it verifies                                                                              |
| ------------------- | ----------------------- | --------------------------------------------------------------------------------------------- |
| Frame encoder       | Unit                    | Round-trip of every JSON type; binary envelope byte layout; version byte.                     |
| Session lifecycle   | Unit (fake PTY + fake WS) | `open_session` → `session.started`; `close_session` → SIGTERM then SIGKILL after 5 s; exit code reported. |
| Coalescing          | Unit                    | `pty.data` frames respect `flush_ms` and `flush_bytes`.                                       |
| Reconnect           | Unit                    | Backoff schedule; ring-buffer replay on reconnect.                                            |
| Device-code flow    | Integration (fake server) | Pairing writes credentials at `0600`; refresh on expired token; re-pair on revoked.          |
| Real PTY end-to-end | Integration             | Launch `/bin/sh` (or `cmd.exe`) in the wrapper, issue `echo hi`, read `hi\r\n` back.          |
| Cross-platform PTY  | CI matrix               | Same end-to-end test runs on ubuntu-latest, macos-latest, windows-latest.                     |

Unit tests do NOT spawn `claude` (it's slow and requires auth). A `--command <path>` flag on the wrapper, hidden from user-facing docs, lets tests substitute a fake executable.

## Deliverables of this subsystem

1. `cmd/claude-switch/main.go` — CLI entry, flags, config loading, device-pair, connect.
2. `internal/proto/` — frame encode/decode, versioned.
3. `internal/pty/` — PTY wrappers per platform.
4. `internal/session/` — supervisor, session table, spawn/kill logic, signal/Job-Object setup.
5. `internal/ws/` — WebSocket client with reconnect + ring-buffer replay.
6. `internal/auth/` — device-code flow + token refresh.
7. `internal/tail/` — `.jsonl` tailer.
8. Cross-compiled release binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`.
9. A fake server harness (Go test helpers) that the implementation plan's integration tests use — and that subsystem 2 can later adopt as its reference test fixture.

## Open questions (to revisit in subsystem 2)

- Exact shape of `access_token` / refresh lifetimes. Subsystem 1 just treats them opaquely.
- Whether `jsonl.tail` should compress old lines on reconnect. Out of scope here; the server decides what to keep.
- Whether to support a `capabilities` negotiation beyond `["pty"]` later (e.g. `["pty","exec"]` for non-PTY one-shots). Kept as a list in `hello` for future expansion.

## Decisions log (for the plan)

| Q   | Decision                                                                     |
| --- | ---------------------------------------------------------------------------- |
| 1   | Start with subsystem 1 (wrapper PTY) only.                                   |
| 2   | PTY persistent (not `--print --resume` stateless).                           |
| 3   | 1 wrapper hosts N processes; mux by session id.                              |
| 4   | Authority split: wrapper owns processes + jsonl; server owns public catalog.  |
| 5   | Hybrid I/O: raw PTY bytes + structured control events + optional jsonl tail. |
| 6   | Single WebSocket, multiplexed envelope.                                      |
| 7   | Stack: Go.                                                                   |
| 8   | Children live until `close_session`, process exit, or wrapper shutdown.      |
| 9   | Auth: device-code flow; tokens persisted at `~/.config/claude-switch/credentials.json`. |
| 10  | Children die with wrapper (PDEATHSIG / Job Object). Server catalog persists. |
| 11  | Single account in MVP; protocol reserves `account` field for subsystem 4.    |
