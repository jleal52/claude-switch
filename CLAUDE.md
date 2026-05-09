# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

Two Go binaries plus a React frontend that together let users drive multiple `claude` CLI sessions remotely:

- `cmd/claude-switch` — wrapper that runs on the user's machine, hosts `claude` PTY sessions, and streams them over a single outbound WebSocket to the server.
- `cmd/claude-switch-server` — central relay; HTTP+WS API, OAuth login, Mongo-backed catalog, and serves the embedded web bundle.
- `web/` — React + Vite + TanStack Router/Query SPA using xterm.js for terminals.

Design specs and plans live in `docs/superpowers/specs/` and `docs/superpowers/plans/` — read them before changing protocol/storage/session semantics.

## Common commands

Go (run from repo root):

```
make build            # bin/claude-switch
make build-server     # bin/claude-switch-server
make test             # go test ./...
make lint             # golangci-lint run ./...   (govet, ineffassign, staticcheck, unused, gofmt, goimports)
make tidy             # go mod tidy
make docker-server    # builds claude-switch-server:dev image

go test ./internal/store/... -run TestX -v       # single package / single test
go test -tags e2e ./cmd/claude-switch-server/... # server e2e (e2e build tag, uses testcontainers + Mongo)
```

Web (run from `web/`):

```
npm ci
npm run dev           # vite on :5173, proxies /api /auth /device /ws to :8080
npm run build         # tsc -b && vite build  → web/dist
npm run lint          # tsc --noEmit (the "lint" step)
npm test              # vitest (jsdom)
npm run test:e2e      # playwright (chromium)
```

Bundle pipeline — the server embeds the SPA via `go:embed` in `internal/webfs/dist`. To ship UI changes inside the server binary or Docker image:

```
make web              # npm ci && npm run build inside web/
make dist-sync        # rm -rf internal/webfs/dist && cp -R web/dist internal/webfs/dist
make build-server     # or make docker-server
```

TS API types are generated from the Go store types via `tygo`:

```
go install github.com/gzuidhof/tygo@latest   # one-time
make codegen-ts                              # writes web/src/api/types.ts (config: tygo.yaml)
```

After editing any struct in `internal/store/{users,wrappers,pairing,sessions,messages,auth_sessions}.go`, re-run `make codegen-ts`.

## Architecture

### Wire protocol (`internal/proto`)

`proto.Encode` / `proto.Decode` wrap a versioned JSON envelope `{v, type, session, payload}` (current `ProtocolVersion = 1`). Control frames use JSON; the hot path for PTY output/input uses **binary** WebSocket frames (`internal/proto/ptydata.go`). All wrapper⇄server and server⇄browser traffic goes through this.

### Server packages

- `cmd/claude-switch-server/main.go` wires `store` (Mongo) + `hub` + `oauth` providers into `api.NewRouter`.
- `internal/api/router.go` is the canonical map of HTTP+WS routes. Notable WS endpoints: `/ws/wrapper` (wrappers connect here, handled by `internal/wswrapper`) and `/ws/sessions/{id}` (browsers, handled by `internal/wsbrowser`).
- `internal/hub` is the in-memory fan-in/fan-out: wrappers register as `WrapperConn`, browsers subscribe per session as `BrowserConn`. Per-session ring buffers (`internal/ring`, 32 KiB) provide late-join replay. `OutboundFrame` is the hub's neutral type — wrapper/browser packages translate it to wire frames.
- `internal/store` is the only Mongo layer. Each `*_test.go` uses `testcontainers/mongodb`; `testhelpers.go` boots a shared container.
- `internal/oauth` has GitHub + Google providers behind a common `Provider` interface; at least one set of `OAUTH_*_CLIENT_ID/SECRET` env vars must be configured or the server refuses to start.
- `internal/auth` (wrapper-side) handles device-code pairing + token refresh; `internal/csrf` handles browser CSRF tokens.
- `internal/webfs` embeds the built SPA (`//go:embed all:dist`); falls back to `index.html` so client-side routes resolve.

### Wrapper packages

- `cmd/claude-switch/main.go` is the wrapper entrypoint. Subcommand: `claude-switch pair <server-base-url>` performs device-code pairing and writes credentials (path from `internal/auth.DefaultCredentialsPath`). Without paired creds the wrapper exits 2.
- `internal/session` is the PTY session supervisor. `Supervisor.Open` spawns `claude` (path resolved from `PATH` or `--command`) under a PTY using `internal/pty` (POSIX + Windows ConPTY split via build tags). `BaseArgs` defaults empty (plain interactive REPL). Output goes through `coalesce.go` (default 16 ms / 16 KiB flush window) before becoming events.
- `internal/process` enforces child cleanup: Linux uses prctl `PDEATHSIG`, Windows uses Job Objects (assigned via `cmd/claude-switch/job_*.go`).
- `internal/tail` discovers and tails Claude's JSONL transcripts under `~/.claude` so the server can relay structured assistant messages.
- `internal/ws` is the wrapper's WebSocket client; `reconnect.go` plus `replay_test.go` show the reconnect-with-replay contract.

### Frontend (`web/src`)

- TanStack Router (`src/router.ts`, file-style routes in `src/routes/`) + TanStack Query for server state.
- `src/api/types.ts` is **generated** by tygo — do not hand-edit; modify Go structs and re-run `make codegen-ts`.
- `src/proto/` mirrors the Go envelope/binary frame format; xterm.js is wired up in the terminal route.
- Vite dev server proxies `/api`, `/auth`, `/device`, `/ws` to `localhost:8080`, so run `make build-server && ./bin/claude-switch-server` alongside `npm run dev` for full-stack work.

## Configuration & deployment

- `.env.example` documents every env var. `SERVER_BASE_URL` is required; OAuth callbacks are derived from it (`/auth/github/callback`, `/auth/google/callback`) — keep it in sync with the registered OAuth apps. `SESSION_SECRET` (32+ bytes, base64) is required and HMAC-signs session cookies + CSRF tokens; rotating it logs everyone out.
- `docker-compose.yml` joins one **external** Docker network shared with Traefik and Mongo (`SHARED_NETWORK`, default `app-net`). Traefik labels expose port 8080 over `${SERVER_HOST}` with `${TRAEFIK_CERT_RESOLVER}`.
- Releases: `.goreleaser.yaml` builds both binaries (linux/darwin/windows × amd64/arm64, no windows/arm64) and publishes to `github.com/jleal52/claude-switch`.

## Testing notes

- Mongo-backed tests pull a real container via `testcontainers-go`; expect first run to be slow and Docker to be required.
- Server end-to-end suite is gated behind the `e2e` build tag (`cmd/claude-switch-server/e2e_test.go`).
- Wrapper PTY tests are split by GOOS (`pty_posix_test.go` / `pty_windows_test.go`); on Windows the tests rely on `UserExistsError/conpty`.
- Web tests: `vitest` for unit (jsdom + msw + mock-socket), Playwright for E2E (`web/playwright.config.ts`); CI runs `npm run build` before `test:e2e`.
