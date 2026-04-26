# Frontend (subsystem 3) — Design

**Project:** claude-switch — subsystem 3 of 4
**Status:** Design (for review)
**Date:** 2026-04-26
**Depends on:** `2026-04-25-server-design.md` — REST + WebSocket contracts and cookie/CSRF semantics defined there are inputs to this design.

## Goal

Build the React SPA that turns the server's API into a usable interface for end users. Logged-in users see their paired wrappers and live sessions in a sidebar, click to open a session in a terminal pane (xterm.js), redeem pairing codes, and toggle transcript storage. The bundle is built into `web/` and embedded into `claude-switch-server` via `go:embed` so the server ships as one binary serving both API and UI on the same origin.

Success = a logged-in user on a browser can: pair a wrapper, see it in the sidebar, open a session on it, type into the terminal, and see PTY output streamed back in real time — all without touching the server's REST API directly.

## Non-goals (out of scope for subsystem 3)

- Mobile native apps. The SPA is responsive; no React Native shell.
- Multi-account profile picker per session. The protocol's `account` field stays at `"default"` (subsystem 4 lights it up).
- Drag-to-split / multi-pane terminal layouts. Single session visible at a time, with optional transcript pane.
- Server-side rendering. This is a client-only SPA bundled via Vite's static build; the server treats `/` as a fallback that returns `index.html`.
- Internationalization. English UI strings only in MVP; structure leaves room for later i18n but no `react-intl` setup.

## Tech stack

- **React 18+ with TypeScript**, `strict: true` everywhere.
- **Vite** for dev server + production bundle.
- **Tailwind CSS** for styling, **shadcn/ui** for component primitives (copy-paste, no runtime dependency).
- **TanStack Query** for REST data fetching + cache + mutations.
- **TanStack Router** for type-safe code-based routing.
- **xterm.js** + addons: `@xterm/addon-fit`, `@xterm/addon-web-links`, `@xterm/addon-search`.
- **`tygo`** (Go → TypeScript struct codegen) for API types — single source of truth in Go structs.
- **Vitest** + **@testing-library/react** for unit/component tests.
- **Playwright** for one or two end-to-end tests against a dev build of the server.

The frontend lives at `<repo>/web/` and is its own npm package (separate from the Go module). It builds to `<repo>/web/dist/`. The server's `internal/webfs/webfs.go` embeds that directory at build time when subsystem 3 ships; until then the stub from Task 9 is served.

## Architecture

```
   ┌────────────────────────────────────────────────────────────────┐
   │                        Browser SPA                              │
   │                                                                  │
   │  ┌───────────────┐   ┌───────────────────┐   ┌────────────────┐ │
   │  │ TanStack      │   │ TanStack Router   │   │ Auth gate      │ │
   │  │ Query (REST   │   │ (typed routes)    │   │ (cookie + CSRF │ │
   │  │ cache + muts) │   │                   │   │  read from DOM)│ │
   │  └───────┬───────┘   └─────────┬─────────┘   └───────┬────────┘ │
   │          │                     │                     │          │
   │          └─────────────────────▼─────────────────────┘          │
   │                       ┌───────────────┐                          │
   │                       │  AppShell     │                          │
   │                       │ ┌───────────┐ │                          │
   │                       │ │ Sidebar   │ │  ← wrappers + sessions  │
   │                       │ ├───────────┤ │                          │
   │                       │ │ MainPane  │ │  ← session terminal     │
   │                       │ │  ┌──────┐ │ │     + transcript pane   │
   │                       │ │  │xterm.│ │ │                          │
   │                       │ │  │  js  │ │ │                          │
   │                       │ │  └──────┘ │ │                          │
   │                       │ └───────────┘ │                          │
   │                       └───────────────┘                          │
   │                              ▲                                   │
   │                              │ WS-per-session (binary pty.data)  │
   │                              ▼                                   │
   └─────────────────────────────server─────────────────────────────┘
```

Key properties:

- **Single-page**, client-routed. All non-API paths return `index.html`; the SPA owns navigation.
- **Same-origin**: cookies (`cs_session`, `cs_csrf`) ride every request automatically. CSRF token mirrored from cookie into `X-CSRF-Token` header for mutating REST calls and into `?ct=<token>` query param for WS upgrades.
- **One WS connection per visible session.** The session-detail route opens its own `/ws/sessions/:id` socket; closing the route closes the socket. The server's per-session ring-buffer replay handles the brief reconnect window.
- **No global app state library** (no Zustand/Redux). TanStack Query owns server data; React state owns UI. Anything that needs to cross routes lives in TanStack Query.
- **Type-safe API client**: `tygo` generates `web/src/api/types.ts` from selected Go structs. A small `apiClient` wrapper (`web/src/api/client.ts`) wraps `fetch` with cookie credentials + CSRF header; per-resource hooks (`useSessions`, `useWrappers`, `useMe`) live in `web/src/api/hooks.ts`.

## Routes

Code-based TanStack Router tree:

| Path                  | Component                                | Auth required | Notes                                                    |
| --------------------- | ---------------------------------------- | ------------- | -------------------------------------------------------- |
| `/`                   | `<CatalogRoute />`                       | yes           | Default landing for logged-in users; sidebar + empty main pane (or last-viewed session). |
| `/pair`               | `<PairRoute />`                          | yes           | Form to enter `ABCD-1234`, shows wrapper descriptor on success. |
| `/sessions/:id`       | `<SessionRoute />`                       | yes           | Main pane shows the live terminal for `:id`.             |
| `/settings`           | `<SettingsRoute />`                      | yes           | Toggle `keep_transcripts`, set retention days (1-90), see linked OAuth providers, logout button. |
| `/login`              | `<LoginRoute />`                         | no            | Shown when `useMe()` returns 401. Buttons for each provider in `providers_configured`. |
| `*`                   | `<NotFoundRoute />`                      | n/a           | Catch-all for unknown SPA routes.                        |

Route-level auth gate: a `requireAuth` loader on protected routes calls `queryClient.fetchQuery(['me'])`; on 401, redirects to `/login` with a `?next=<original-path>` param.

## Data fetching with TanStack Query

Query keys (the cache namespace):

| Key                        | Endpoint                                | Stale time | Invalidations           |
| -------------------------- | --------------------------------------- | ---------- | ----------------------- |
| `['me']`                   | `GET /api/me`                           | 5 min      | logout, settings update |
| `['wrappers']`             | `GET /api/wrappers`                     | 30 s       | pair redeem, wrapper delete |
| `['sessions']`             | `GET /api/sessions?status=live`         | 10 s       | session create, session close, ws frame `session.exited` |
| `['session', id, 'messages']` | `GET /api/sessions/:id/messages?since=…` | 10 s       | new ws `jsonl.tail` frame |

Mutations (with optimistic updates where it matters):

- `useCreateSession()` — POST `/api/sessions`, on success invalidate `['sessions']` and navigate to `/sessions/:id`.
- `useDeleteSession(id)` — DELETE `/api/sessions/:id`.
- `useDeleteWrapper(id)` — DELETE `/api/wrappers/:id`.
- `useRedeemPair(code)` — POST `/api/pair/redeem`.
- `useUpdateSettings()` — POST `/api/me/settings`.
- `useLogout()` — POST `/api/auth/logout`, on success clear all caches, navigate to `/login`.

CSRF header is added by `apiClient` automatically on POST/PUT/PATCH/DELETE: it reads `document.cookie` for `cs_csrf` and sets `X-CSRF-Token`.

## WebSocket integration per session

`useSessionStream(sessionID)` is a custom hook owned by `<SessionRoute />`. It:

1. Constructs WS URL `wss://<host>/ws/sessions/:id?ct=<csrf>` (CSRF read from `cs_csrf` cookie).
2. Opens `WebSocket`, sets `binaryType = 'arraybuffer'`.
3. Routes inbound frames:
   - `MessageEvent.data instanceof ArrayBuffer` → it's `pty.data`. Strips the 17-byte header (version + ULID) — these are validated against the session id once at connect, then ignored — and writes the payload to the xterm.js instance via `term.write(uint8array)`.
   - String frames are JSON; `type === 'replay.start'` / `'replay.end'` are no-ops in MVP (xterm.js handles bytes regardless of order). `type === 'wrapper.offline'` → toast "wrapper offline" + close. `type === 'session.exited'` → toast with exit code and close.
4. Forwards user input from xterm.js's `onData` callback to the WS by encoding a binary frame (1-byte version + 16-byte ULID + payload bytes), matching the server's expectation.
5. On `pty.resize` (xterm.js's `onResize`), sends a JSON frame `{v:1, type:"pty.resize", session:"…", payload:{cols,rows}}`.
6. Reconnect: exponential backoff with ±25 % jitter (1 s base, 30 s cap). On reconnect, replay frames re-arrive automatically from the server's ring buffer.
7. Cleanup on route exit: explicit `socket.close(1000, "leaving")`.

A small protocol module (`web/src/proto/`) mirrors the binary `pty.data` framing from `internal/proto/ptydata.go` so encode/decode lives in one tested place.

## Layout (`<AppShell />`)

```
+----------------------------------------------------------+
|  TopBar: app title · me email · settings · logout        |
+----------+-----------------------------------------------+
|          |                                               |
| Sidebar  |    MainPane                                    |
|          |                                               |
| - search |    ┌──────────────────────────────────────┐   |
|          |    │ Session header: name · status · ⋯    │   |
| Wrapper1 |    └──────────────────────────────────────┘   |
| ├─ s1    |    ┌──────────────────────────────────────┐   |
| ├─ s2    |    │                                       │   |
| └─ +new  |    │   xterm.js terminal                   │   |
|          |    │                                       │   |
| Wrapper2 |    │                                       │   |
| └─ +new  |    └──────────────────────────────────────┘   |
|          |    ┌──────────────────────────────────────┐   |
| ─────    |    │ Transcript (collapsible) — markdown  │   |
| + pair   |    │  rendered jsonl entries, oldest top  │   |
| settings |    └──────────────────────────────────────┘   |
+----------+-----------------------------------------------+
```

- **TopBar**: minimal. Logo/title left; user menu (avatar from `oauth_provider`) right with Settings + Logout.
- **Sidebar** (collapses to drawer below 768px):
  - Search input filters wrappers and sessions by name/cwd.
  - Each wrapper is a collapsible group: name + OS icon, count of live sessions, "+ Nueva sesión" button (opens modal).
  - Sessions under each wrapper: status dot (running/starting/wrapper_offline/exited), short cwd, click to navigate to `/sessions/:id`. Right-click / long-press for context menu (Close, Copy URL).
  - Bottom of sidebar: "Pair wrapper" button → `/pair`. Settings link → `/settings`.
- **MainPane** when no session selected: empty state ("Select a session or create a new one").
- **MainPane** when session selected:
  - Header: session name (auto-derived from cwd basename or wrapper name), status pill, kebab menu (Close session, Copy session URL, Toggle transcript).
  - xterm.js fills available height. `@xterm/addon-fit` recomputes on container resize.
  - Optional transcript pane below (or right, on landscape ≥1280 px). Markdown-rendered entries, scrolls independently. Hidden by default; toggle via header kebab.

## Session creation modal

Triggered from sidebar's "+ Nueva sesión" or top-bar "+ New session" button.

```
+-----------------------------------------+
| New session                             |
|                                         |
| Wrapper:    [ ▼ select wrapper ]        |
|                                         |
| Working dir: [ /home/usuario          ] |
| Account:     [ default            ▼ ]   |  (single option in MVP)
|                                         |
| [ Cancel ]                  [ Create ] |
+-----------------------------------------+
```

`Create` calls `useCreateSession`, on success closes modal and navigates to `/sessions/:id`.

## Pairing UX

`/pair` is a single-step form:

```
+-------------------------------------------+
|  Pair a wrapper                           |
|                                           |
|  Run on the machine you want to pair:     |
|     claude-switch pair https://…/         |
|                                           |
|  Then enter the code printed below:       |
|                                           |
|  [ ABCD-1234            ]                 |
|                                           |
|  [ Cancel ]              [ Approve ]      |
+-------------------------------------------+
```

On Approve: POST `/api/pair/redeem`. Success shows a 1-line confirmation ("Paired Linux/amd64 'ireland'") and a button to `/`. Error states: 404 ("Code not found or expired"), 409 ("Code already used"), other (generic "Something went wrong").

## Settings

```
+---------------------------------------------+
|  Settings                                   |
|                                             |
|  Signed in as: ada@example.com (github)     |
|                                             |
|  Transcripts                                |
|   [x] Keep transcripts of my sessions       |
|   Retention: [ 30 ] days  (1–90)            |
|                                             |
|  [ Save ]      [ Logout ]                   |
+---------------------------------------------+
```

`Save` calls `useUpdateSettings({keep_transcripts, transcript_retention_days})`. The retention field is disabled when `keep_transcripts == false`.

## Login

When unauthed (any protected route's loader sees a 401):

```
+--------------------------------------+
|  Sign in to claude-switch             |
|                                       |
|  [ Continue with GitHub ]             |
|  [ Continue with Google ]             |
+--------------------------------------+
```

Each button is a plain `<a href="/auth/<p>/login">`. The browser follows the OAuth round-trip; cookies come back set by the server, and the SPA reloads at `/`.

## Auth gate flow

- App boots → `<AppShell>` runs `useMe()` (TanStack Query).
- If 401: redirect to `/login` with `?next=` of the current path.
- If 200: render the children.
- After `useLogout()`: clear all queries, navigate to `/login`.

The cookie `cs_csrf` is HttpOnly **false** (per server design). The SPA reads it from `document.cookie` on every mutating request; if missing/empty, refuses to send (logs a console warning so it's discoverable in dev).

## Theme & accessibility

- Tailwind dark mode (`class` strategy, system-default + manual toggle in TopBar).
- shadcn/ui already supports keyboard navigation in dialogs and dropdowns; preserve.
- xterm.js: theme matches the SPA's CSS variables for foreground/background/cursor.
- Focus rings on all interactive elements.
- `aria-live="polite"` regions for "session opened", "wrapper offline", etc.

## Testing strategy

| Layer            | Tool                            | What it verifies                                                  |
| ---------------- | ------------------------------- | ----------------------------------------------------------------- |
| Hooks (data)     | Vitest + MSW (Mock Service Worker) | `useMe`, `useWrappers`, `useCreateSession` shape requests correctly, parse responses, set cache. |
| Hooks (WS)       | Vitest + `mock-socket`          | `useSessionStream` opens WS with CSRF, decodes binary frames, writes to a fake terminal mock. |
| Components       | Vitest + @testing-library/react | `<Sidebar>`, `<NewSessionModal>`, `<PairForm>` render given props, fire user events. |
| Routing          | Vitest                          | Auth-gate redirects on 401; login `?next=` preserves intended path. |
| End-to-end       | Playwright                      | One happy-path test against a built server image with stub OAuth: login → pair (with backend pre-seeded code) → create session → see banner. |

Bundle size budget: ≤ 350 KB gzipped initial JS at ship-time. CI fails if exceeded; we'd address with `react-virtual` or code splitting before raising the budget.

## Build & deployment

- `web/package.json` declares deps. `npm install` in `web/` is a one-shot install.
- `web/vite.config.ts` configures dev proxy: `/api/*` and `/ws/*` to `http://localhost:8080` (the dev backend) so `npm run dev` works against a running `claude-switch-server`.
- `web/dist/` is the production output. The server's `internal/webfs/webfs.go` `//go:embed all:../../web/dist` (paths resolved at compile time) ships it embedded.
- Top-level `Makefile` gets `make web` (runs `npm run build` in `web/`) and `make build-server-with-web` (runs `make web` then `make build-server`).
- CI: a new job builds `web/` (Node 20, cache npm) and uploads `web/dist/` as an artifact, plus runs `npm test`. The existing `server-image` job picks up `web/dist/` because the Dockerfile copies `.` after `web/dist/` is already built (we'll add a small README note for self-hosters that they need a built `web/dist/` before `docker compose build`, or alternatively a `web-build` stage in the Dockerfile that runs `npm ci` + `npm run build`).

## Codegen for API types (`tygo`)

`tygo.yaml` at repo root:

```yaml
packages:
  - path: "github.com/jleal52/claude-switch/internal/store"
    type_mappings: { time.Time: "string" }
    output_path: "web/src/api/types.go.ts"
    include_files:
      - "users.go"
      - "wrappers.go"
      - "pairing.go"
      - "sessions.go"
      - "messages.go"
      - "auth_sessions.go"
```

`make codegen-ts` runs `tygo generate`. Output is committed (not generated at install time) so contributors don't need Go to install the frontend.

The `apiClient` consumes `User`, `Wrapper`, `Session`, etc. directly from the generated module.

## Deliverables of this subsystem

1. `web/` directory with full Vite + React + TS setup, Tailwind, shadcn/ui initialized.
2. `web/src/api/` — `client.ts`, `hooks.ts`, generated `types.ts`.
3. `web/src/proto/ptydata.ts` — encode/decode of the 1+16+N binary frame, mirrored from Go.
4. `web/src/components/` — `AppShell`, `Sidebar`, `MainPane`, `Terminal` (xterm wrapper), `Transcript`, `NewSessionModal`, `PairForm`, `SettingsForm`, `Login`.
5. `web/src/routes/` — TanStack Router definitions per the table above.
6. `web/src/hooks/useSessionStream.ts` — WS hook.
7. `web/tests/` — Vitest specs; one Playwright e2e under `web/tests/e2e/`.
8. `tygo.yaml` + `make codegen-ts`.
9. CI extension: `web-build` + `web-test` jobs.
10. `internal/webfs/webfs.go` updated to embed `web/dist/` instead of the stub once the bundle exists. (One-line change at the end.)
11. Updated `README.md` with the dev workflow (run server + run `npm run dev` in `web/` against the dev proxy).

## Open questions (revisit before subsystem 4)

- **Service Worker / PWA**. Out of scope here. Could come later if "install on mobile home screen" matters.
- **i18n**. Not in MVP. The component layer would need to switch to keyed strings before Spanish (or any other) translation lands.
- **Terminal addons beyond fit/web-links/search**. `@xterm/addon-canvas` for performance, `@xterm/addon-image` for graphical output — not needed in MVP, easy to add later.

## Decisions log (for the plan)

| #   | Decision                                                                            |
| --- | ----------------------------------------------------------------------------------- |
| 1   | React 18 + Vite + TypeScript (`strict: true`).                                       |
| 2   | Tailwind CSS + shadcn/ui for components; no global CSS framework beyond.             |
| 3   | TanStack Query for REST, TanStack Router for typed routing.                           |
| 4   | `tygo` generates TS API types from selected Go structs; output committed.            |
| 5   | xterm.js + fit + web-links + search addons.                                          |
| 6   | Sidebar + main-pane layout; one session visible at a time; transcript pane optional.  |
| 7   | One WebSocket per visible session, opened by the session-detail route, closed on exit.|
| 8   | Auth gate at the route loader level via `useMe`; 401 → `/login?next=`.                |
| 9   | Same-origin cookies; CSRF mirrored from cookie into header (REST) and query (WS).     |
| 10  | Bundle output at `web/dist/`, embedded into the server binary via `go:embed`.         |
| 11  | Dev workflow: `npm run dev` in `web/` proxies `/api` + `/ws` to localhost:8080.       |
| 12  | Bundle size budget: ≤ 350 KB gzipped JS initial.                                      |
