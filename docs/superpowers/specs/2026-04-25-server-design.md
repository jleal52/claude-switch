# Server (subsystem 2) — Design

**Project:** claude-switch — subsystem 2 of 4
**Status:** Design (for review)
**Date:** 2026-04-25
**Depends on:** `2026-04-25-wrapper-pty-design.md` — wire protocol and session lifecycle defined there are inputs to this design.

## Goal

Build the central Go HTTP+WebSocket server that connects browsers to wrappers (subsystem 1). It hosts: end-user authentication (GitHub + Google OAuth), the device-code pairing endpoints the wrapper already calls, the public catalog of sessions per user, and the relay that routes PTY traffic between browser and wrapper.

Success = a logged-in user on a browser can: (a) pair a new wrapper by entering the code printed by `claude-switch pair`, (b) see the list of their paired wrappers and live sessions, (c) open a new `claude` session on a chosen wrapper, type into it from the browser, and see PTY output streamed back in real time.

## Non-goals (explicitly out of scope for subsystem 2)

- Frontend implementation. The server only serves the SPA bundle (when built with embed) and the API; how that SPA looks is subsystem 3's concern.
- Multi-account credentials inside the wrapper (subsystem 4). The protocol's `account` field stays at `"default"` for now; the server stores it as-is.
- Billing, teams, RBAC beyond per-user scoping. Each user sees only their own wrappers and sessions; that's all the access control we ship.
- Mobile native apps. The browser is the only client we plan for. The API surface should not be browser-specific, but no mobile work happens here.
- Email notifications, password recovery, password login. OAuth is the only login.

## Architecture

```
                ┌─────────────────────────────────────────────────────────┐
                │                claude-switch-server (Go)                 │
                │                                                         │
                │  ┌─────────┐   ┌─────────┐   ┌──────────┐   ┌────────┐  │
   browser ───▶ │  │ HTTP    │   │ WS      │   │ Catalog  │   │ Mongo  │  │
                │  │ /api/*  │   │ /ws/... │   │ + Auth   │◀─▶│ driver │  │
                │  └─────────┘   └─────────┘   └──────────┘   └────────┘  │
                │       ▲             ▲             ▲             │       │
                │       │             │             │             │       │
                │       ▼             ▼             ▼             ▼       │
                │  ┌──────────────────────────────────────────────────┐   │
                │  │            in-memory hub (router)                │   │
                │  │  per wrapper:  outbound queue + inbound dispatch │   │
                │  │  per browser:  WS-per-session subscriber set     │   │
                │  └──────────────────────────────────────────────────┘   │
                │       ▲                                                  │
                │       │ WS                                               │
                │       ▼                                                  │
                └─────────────────────────────────────────────────────────┘
                        │
                        │ outbound from each user's wrapper
                        ▼
                ┌──────────────────┐
                │ wrapper (subsys.1│ ×N per user across machines
                └──────────────────┘
                        │
                        ▼ spawns
                ┌──────────────────┐
                │ claude (PTY)     │ ×M per wrapper
                └──────────────────┘
```

Key properties:

- **Single Go binary**, built with `go:embed` for the SPA by default; `-tags noweb` produces a headless API binary.
- Two TLS frontends are equivalent: behind Traefik (default deployment shape) or `--listen-tls` + autocert (future, optional).
- **In-memory hub** is the heart. Every connected wrapper has a goroutine reading its WS; every connected browser-per-session has a goroutine writing/reading its own WS. The hub is a small set of typed maps protected by a mutex — no external pub/sub.
- **MongoDB** for persistence: users, wrappers (with hashed refresh tokens), sessions catalog, pairing codes, browser auth sessions, optional transcripts (TTL).
- **No horizontal scaling in MVP.** A single instance owns the hub. If you need a second instance later, the wrapper-route table has to move to Redis or similar — but that's not now.

## Data model (MongoDB)

Each collection is described as `name { fields }` with required indexes. Fields are BSON-named (snake_case).

### `users`

```
{
  _id:               ObjectID,
  oauth_provider:    "github" | "google",
  oauth_subject:     string,            // provider's stable user id
  email:             string,
  name:              string,
  avatar_url:        string,
  keep_transcripts:  bool,              // opt-in to store jsonl.tail content
  created_at:        Date,
  last_login_at:     Date
}
```

Indexes:
- `{ oauth_provider: 1, oauth_subject: 1 }` unique
- `{ email: 1 }` non-unique (email may collide if same person uses GitHub-and-Google with same address)

### `wrappers`

Each paired wrapper instance.

```
{
  _id:                ObjectID,
  user_id:            ObjectID,         // owner
  name:               string,           // user-supplied or hostname
  os:                 "linux" | "darwin" | "windows",
  arch:               string,
  version:            string,           // wrapper binary version
  paired_at:          Date,
  last_seen_at:       Date,             // updated on each WS connect
  refresh_token_hash: string,           // bcrypt of the refresh token (NOT the token itself)
  refresh_token_id:   string,           // ULID; the token sent to the wrapper is "<token_id>.<random>"
  revoked_at:         Date | null
}
```

Indexes:
- `{ user_id: 1, paired_at: -1 }`
- `{ refresh_token_id: 1 }` unique

Refresh tokens themselves are never stored in plaintext; we store a bcrypt hash plus an ID prefix to look up the right row in O(1).

### `wrapper_access_tokens`

Short-lived (≈1 hour) access tokens used as Bearer in WS upgrade headers. Server-side issued so we can revoke a wrapper instantly.

```
{
  _id:        ObjectID,
  wrapper_id: ObjectID,
  user_id:    ObjectID,
  token_hash: string,        // sha256(token)
  expires_at: Date           // TTL index
}
```

Indexes:
- `{ token_hash: 1 }` unique
- `{ expires_at: 1 }` TTL index

### `pairing_codes`

Short-lived (≈10 min) device-code records. The wrapper polls; the user redeems.

```
{
  _id:        ObjectID,
  code:       string,        // "ABCD-1234", uniformly random alphanumeric
  status:     "pending" | "approved" | "denied" | "expired",
  user_id:    ObjectID | null,   // set when redeemed
  wrapper:    {                  // descriptors the wrapper provided at /start
    name: string, os: string, arch: string, version: string
  },
  expires_at: Date           // TTL index
}
```

Indexes:
- `{ code: 1 }` unique
- `{ expires_at: 1 }` TTL index

### `sessions`

The catalog. One document per `claude` session ever opened.

```
{
  _id:          string,        // ULID, assigned by server
  user_id:      ObjectID,
  wrapper_id:   ObjectID,
  jsonl_uuid:   string,        // reported by wrapper after spawn
  cwd:          string,
  account:      string,        // "default" in MVP
  status:       "starting" | "running" | "exited" | "wrapper_offline",
  created_at:   Date,
  exited_at:    Date | null,
  exit_code:    int | null,
  exit_reason:  string | null
}
```

Indexes:
- `{ user_id: 1, created_at: -1 }`
- `{ wrapper_id: 1, status: 1 }`

### `session_messages` (only when `users.keep_transcripts == true`)

Captured `jsonl.tail` entries. TTL determined by user setting (default 90 days).

```
{
  _id:        ObjectID,
  session_id: string,         // FK to sessions._id
  user_id:    ObjectID,       // denormalized for index efficiency
  ts:         Date,
  entry:      string          // raw jsonl line
}
```

Indexes:
- `{ session_id: 1, ts: 1 }`
- `{ user_id: 1, ts: -1 }`
- `{ ts: 1 }` TTL with `expireAfterSeconds = 90*86400`. The TTL is set on the index; per-user overrides require recreating the index — for MVP we keep one global retention.

### `auth_sessions`

Browser sessions (cookie-keyed).

```
{
  _id:        string,         // ULID, used as session cookie value
  user_id:    ObjectID,
  csrf_token: string,         // random; sent to client in a non-HttpOnly cookie for double-submit
  created_at: Date,
  last_seen:  Date,
  expires_at: Date            // TTL index, default 30 days, refreshed on activity
}
```

Indexes:
- `{ user_id: 1 }`
- `{ expires_at: 1 }` TTL

## API surface

All paths under one origin. JSON content-type unless noted.

### Public (no auth)

- `GET /healthz` — liveness probe; returns `{status:"ok"}`.
- `GET /` and any non-`/api`, non-`/auth`, non-`/ws` path with no extension — serves the SPA's `index.html` (when built with embed). Static assets (`/assets/*`) are served from the embed FS too. With `-tags noweb`, all of these return 404.
- `POST /device/pair/start` — wrapper pairing kickoff (defined in subsystem 1).
- `GET  /device/pair/poll?c=<code>` — wrapper poll.
- `POST /device/token/refresh` — wrapper refresh.
- `GET  /auth/github/login` → 302 to GitHub authorize URL.
- `GET  /auth/github/callback` — GitHub OAuth completion; sets cookie; redirects to `/`.
- `GET  /auth/google/login` / `GET /auth/google/callback` — same shape.

### User-authenticated (cookie required, CSRF required for non-GET)

- `GET    /api/me` — `{ user: {…}, providers_configured: ["github","google"] }`.
- `POST   /api/auth/logout` — invalidates the auth_session row.

- `GET    /api/wrappers` — list paired wrappers for current user.
- `DELETE /api/wrappers/:id` — revoke wrapper (sets `revoked_at`; the next WS hello from it is rejected).
- `POST   /api/wrappers/:id/rename` — change `name` (small, non-critical).

- `POST   /api/pair/redeem` — body: `{ code }`. Marks pairing_code as approved by current user. Returns the wrapper-descriptor so the UI can show "you just paired Linux/x86_64 'ireland'".

- `GET    /api/sessions?wrapper=<id?>&status=<live|all>` — list. Defaults to all live sessions of the user.
- `POST   /api/sessions` — body: `{ wrapper_id, cwd, account?: "default", args?: [] }`. Server allocates a ULID, sends `open_session` to the wrapper over its WS, and returns the session row immediately (status = "starting"). The browser then opens `/ws/sessions/:id`.
- `DELETE /api/sessions/:id` — sends `close_session` to the wrapper; updates row to `status = "exited"` when the wrapper confirms.
- `GET    /api/sessions/:id/messages?since=<rfc3339>` — returns up to 1000 stored jsonl entries (only if user opted in).

- `POST   /api/me/settings` — `{ keep_transcripts?: bool, transcript_retention_days?: int }`. Validates and persists.

### CSRF

State-changing endpoints (POST/DELETE) require:
- The session cookie `cs_session=<auth_session._id>` (HttpOnly, Secure, SameSite=Lax).
- A second cookie `cs_csrf=<auth_sessions.csrf_token>` (NOT HttpOnly) which the SPA mirrors in the request header `X-CSRF-Token`. Server rejects if missing or mismatched. This is the standard double-submit cookie pattern.

## Browser ↔ server WebSocket

`GET /ws/sessions/:id` (Upgrade: websocket).

Auth: same cookie + CSRF token as REST. Server checks the auth_session, verifies the user owns the session row, then upgrades. The CSRF check on WS upgrade is performed via a query parameter `?ct=<csrf-token>` since browsers can't set custom headers on WS upgrades cross-cleanly.

Frame model — a SUBSET of the wrapper protocol's envelope, plus a couple of browser-specific frames:

| Frame                    | Direction         | Payload                                |
| ------------------------ | ----------------- | -------------------------------------- |
| `pty.data` (binary)      | bidi              | raw PTY bytes; ULID inside the frame's session position is redundant since the URL pinned it, but we keep the binary format identical to the wrapper protocol so the same decoder works on both ends |
| `pty.resize`             | browser → server  | `{ cols, rows }` — proxied to wrapper  |
| `replay.start`            | server → browser  | sent immediately after upgrade; followed by 0..1 binary frames containing the wrapper's ring-buffer snapshot for that session |
| `replay.end`              | server → browser  | marker; live frames begin after this   |
| `session.exited`          | server → browser  | `{ exit_code, reason, detail }` — same as the wrapper-emitted frame, relayed |
| `wrapper.offline`         | server → browser  | sent when the wrapper for this session disconnects; server then closes the WS with code 1011 |

When a browser opens the WS, the server:
1. Validates auth + ownership.
2. Looks up the session's `wrapper_id`. If the wrapper isn't connected, sends `wrapper.offline` and closes.
3. Subscribes the browser into the hub's per-session subscriber set. From now on:
   - `pty.data` arriving from the wrapper for this session is fanned out to every subscribing browser.
   - `pty.data` arriving from a browser is forwarded to the wrapper.
4. Sends `replay.start`, then the wrapper's most recent ring-buffer (which the wrapper already replayed on its own reconnect — the server has cached the most recent snapshot per session in memory), then `replay.end`.

Multiple browsers subscribed to the same session see the same stream (collaborative viewing). Input from any subscriber goes to the wrapper.

## Wrapper ↔ server WebSocket

Already specified in subsystem 1. The server side of that protocol is implemented here:

- The wrapper connects to `wss://claude-switch.dns.nom.es/ws/wrapper` (new path; subsystem 1's spec didn't pin a path, only `server_endpoint`). Auth: `Authorization: Bearer <access_token>`.
- The server validates the token (looks up `wrapper_access_tokens` by `sha256(token)`, checks not expired) and identifies the wrapper.
- The wrapper's `hello` is processed: reconcile its alive sessions against the server's `sessions` collection (sessions on the wrapper but not in DB → close locally; sessions in DB as live on this wrapper but missing from hello → mark `status = "exited"` with `reason = "wrapper_restart"`). Update `last_seen_at`.
- For every subsequent inbound frame the server routes:
  - `session.started` / `session.exited` / `pty.control_event` → update DB row, fan out to subscribed browsers.
  - `pty.data` (binary) → fan out to subscribed browsers AND update an in-memory per-session ring snapshot (32 KiB, smaller than the wrapper's 64 KiB; we don't need a full mirror, just enough for the next "browser opens this session" replay).
  - `jsonl.tail` → fan out to subscribed browsers AND, if user has `keep_transcripts == true`, insert into `session_messages`.
  - `pong` → liveness only (no business logic).

The server pings every 20 s with a JSON `ping {nonce}`, expecting `pong {echo: nonce}` within 45 s (matches subsystem 1's read-deadline).

## OAuth flows

### GitHub

1. User clicks "Sign in with GitHub" → browser navigates to `/auth/github/login`.
2. Server: generates a random `state` (32 bytes, base64url), stores it in a short-lived `oauth_states` collection (TTL 10 min) along with the intended return URL. Sets a `cs_oauth_state=<state>` cookie. Redirects 302 to GitHub's `https://github.com/login/oauth/authorize?...&state=<state>`.
3. GitHub redirects to `/auth/github/callback?code=...&state=...`.
4. Server: verifies `state` matches the cookie AND exists in `oauth_states`, then deletes that record. Exchanges `code` for an access token at `https://github.com/login/oauth/access_token`. Calls `https://api.github.com/user` and `/user/emails` to get the GitHub user info.
5. Server upserts `users` row by `(github, github_user.id)`. Creates an `auth_sessions` row with a fresh ULID. Sets cookies `cs_session=<ulid>` (HttpOnly) and `cs_csrf=<csrf>`. 302s to `/`.

### Google

Same shape, different endpoints (`https://accounts.google.com/o/oauth2/v2/auth`, `https://oauth2.googleapis.com/token`, `https://www.googleapis.com/oauth2/v2/userinfo`). Scopes: `openid email profile`.

The OAuth client ID/secret pairs come from env vars (`OAUTH_GITHUB_CLIENT_ID`, `OAUTH_GITHUB_CLIENT_SECRET`, etc.); if a provider's credentials are unset, that provider's `/auth/<provider>/login` endpoint returns 404 and the SPA hides the button via `/api/me`'s `providers_configured` field.

## Pairing flow (multi-tenant, end to end)

1. User runs `claude-switch pair https://claude-switch.dns.nom.es` on a machine.
2. Wrapper POSTs `/device/pair/start` with `{ name, os, arch, version }`. Server creates a `pairing_codes` row with a freshly-generated 8-char code (`ABCD-1234`) and the wrapper descriptor; status = `pending`. Returns `{ code, poll_url, expires_in: 600 }`.
3. Wrapper prints the code and starts polling `GET /device/pair/poll?c=ABCD-1234` every 5 s.
4. User opens `https://claude-switch.dns.nom.es/pair` in a browser. If not logged in, redirected to OAuth. After login, lands on the pair page which shows a single input box.
5. User types `ABCD-1234` and clicks **Approve**. Frontend POSTs `/api/pair/redeem` with `{ code }` (CSRF cookie + header included).
6. Server validates: pairing_codes row exists, status=pending, not expired. Sets `user_id = current_user`, `status = "approved"`. Returns the wrapper descriptor.
7. The wrapper's next poll sees `approved`. Server: creates a `wrappers` row, generates a refresh token (`<token_id>.<random_64_bytes>`, hashes the random part with bcrypt) and an access token (random 32 bytes, `wrapper_access_tokens` row), and returns `{ access_token, refresh_token, expires_at, server_endpoint: "wss://claude-switch.dns.nom.es/ws/wrapper" }`. Deletes the pairing_codes row.
8. Wrapper persists credentials. Server-side, on next `claude-switch` start it'll connect via WS with the access token; if expired, it'll refresh first.

If the user denies the code (Q for subsystem 3 UX, but the API supports it): the SPA POSTs `/api/pair/redeem` with `{ code, deny: true }`; server marks the row `denied`; wrapper's next poll receives `403` and the wrapper exits with a clear message.

## Catalog & wrapper offline behaviour

- When a wrapper disconnects (clean or abrupt): the server marks all its `running` sessions as `wrapper_offline`. Browsers subscribed to those sessions receive `wrapper.offline` and have their WS closed (code 1011). Sessions remain visible in the catalog so the user can see "session X was at machine Y when it dropped offline".
- When the wrapper reconnects, its `hello` lists alive sessions. Each one in `hello.sessions`:
  - Found in DB and was `wrapper_offline` → flip back to `running`.
  - Found in DB and was `running` → no-op.
  - In `hello.sessions` but the server's row is missing (server's DB was wiped, etc.) → server tells the wrapper to close it (server is authoritative on the public catalog; per Q4=C, the wrapper closes orphans). This is rare and defensive.
- When a wrapper is `revoked_at != null`: the server rejects its WS upgrade with 401, and the wrapper falls back to re-pair on revoked (Task 20 sub-piece 2 of subsystem 1).

## Configuration

All via env (see `.env.example` already in the repo):

| Var                              | Purpose                                                           |
| -------------------------------- | ----------------------------------------------------------------- |
| `MONGO_URI`, `MONGO_DB`           | Mongo connection                                                  |
| `SERVER_BASE_URL`                | Public URL; used for OAuth redirect URLs and absolute links       |
| `OAUTH_<PROVIDER>_CLIENT_ID/SECRET` | Per provider; unset = provider disabled                          |
| `SESSION_SECRET`                 | Required at startup; HMAC key for cookie + CSRF                   |
| `LOG_LEVEL`                      | `debug | info | warn | error`                                     |
| `BIND_ADDR`                      | Default `:8080`. The container exposes this; Traefik routes to it |
| `WS_READ_TIMEOUT`                | Default `45s`; per spec.                                          |

Behind Traefik, TLS terminates upstream, so the binary itself never opens 443. There is no `--listen-tls` mode in MVP — that's a follow-up if/when self-hosting outside Traefik becomes a use case.

## Logging

Structured logs via `log/slog`. Every request logs `method path status duration request_id user_id wrapper_id session_id` (whichever apply). Generated `request_id` is also returned to the client as `X-Request-Id` for correlated debugging.

## Testing strategy

| Layer            | Test type        | What it verifies                                                                 |
| ---------------- | ---------------- | -------------------------------------------------------------------------------- |
| Mongo data layer | Unit + miniredis-style | Each repository (users/wrappers/sessions/...) round-trip + index uniqueness. We use a real mongod via `testcontainers-go` so query semantics match prod. |
| OAuth flows      | Integration      | Fake GitHub / Google OAuth servers (httptest); full callback path sets cookie + creates user row. |
| Pairing flow     | Integration      | Wrapper → start → poll → redeem from "browser" → poll resolves with creds. |
| Hub routing      | Unit + httptest WS | Two browsers subscribed to one session see the same `pty.data`; offline wrapper triggers `wrapper.offline`. |
| End-to-end       | One docker-compose-up test | Spin up server + a Mongo + a fake wrapper; create a session via REST; subscribe via WS; assert input → output round-trip. Same shape as wrapper's E2E test in subsystem 1. |
| CSRF             | Unit             | POST without `X-CSRF-Token` is 403; with mismatched token is 403; with matching token is 200. |

## Deliverables of this subsystem

1. `cmd/claude-switch-server/main.go` — flags, env, slog, listen.
2. `Dockerfile.server` — multi-stage Go build, distroless final.
3. `internal/api/` — REST handlers per endpoint, grouped by resource (`auth.go`, `wrappers.go`, `pair.go`, `sessions.go`, `me.go`).
4. `internal/wsbrowser/` — `/ws/sessions/:id` upgrade and frame loop.
5. `internal/wswrapper/` — `/ws/wrapper` upgrade and frame loop; reconciliation logic on hello.
6. `internal/hub/` — in-memory routing of sessions ↔ wrappers ↔ browsers.
7. `internal/store/` — Mongo repositories per collection, plus index init at startup.
8. `internal/auth/` — OAuth client (extends the auth package the wrapper already uses for device-code; refactor as needed to share `Credentials` shape but not browser-session logic).
9. `internal/csrf/` — cookie issuance and verification.
10. `web/` — directory placeholder; subsystem 3 will populate it. The server's embed reads from this path; build with empty `web/` is fine and serves a "Frontend not built yet" stub at `/`.
11. Updated `docker-compose.yml` (already drafted) referencing `Dockerfile.server`.
12. CI matrix in `.github/workflows/ci.yml` extended with: build the server image, smoke-test it against a Mongo testcontainer.

## Open questions (revisit before subsystem 3)

- **Push notifications / email when a wrapper goes offline unexpectedly.** Out of scope here; document.
- **Rate-limiting** on `/auth/...` and `/device/pair/start`. The MVP relies on Traefik middlewares (configurable per-deploy) rather than baking limits into the app. Document an example Traefik label set as a follow-up.
- **Search / aggregation across `session_messages`.** The collection is indexed for filter-by-session and filter-by-time. Full-text would need either a Mongo Atlas Search index or external indexing; not now.

## Decisions log (for the plan)

| #   | Decision                                                                       |
| --- | ------------------------------------------------------------------------------ |
| 1   | Multi-tenant from day 1.                                                       |
| 2   | Stack: Go + `net/http` stdlib + `coder/websocket`.                             |
| 3   | DB: MongoDB (joining shared container network).                                |
| 4   | OAuth: GitHub + Google multi-provider.                                         |
| 5   | Frontend coupling: hybrid monolith (`go:embed` default; `-tags noweb` opt-out).|
| 6   | Browser↔server transport: REST control + WS-per-session for live PTY.          |
| 7   | Deployment: Docker Compose, Traefik for TLS, domain `claude-switch.dns.nom.es`.|
| 8   | Repo: monorepo, extends existing `claude-switch`.                              |
| 9   | Catalog: metadata permanent; transcripts opt-in per user with TTL (90 days).   |
| 10  | Browser auth: server-side sessions in Mongo; HttpOnly cookie + double-submit CSRF. |
| 11  | Wrapper assignment for new sessions: explicit user choice in UI.               |
| 12  | Wrapper offline mid-stream: server emits `wrapper.offline` and closes WS 1011. |
