# Transcripts catalog + search across wrappers

Status: design approved 2026-05-11. Implementation plan to be written before coding the first PR.

## Goal

Surface the history of Claude conversations stored under `~/.claude/projects/` on each wrapper in the portal, so a user can:

- Browse projects and sessions across all their wrappers.
- Search transcripts by literal text, optionally scoped to one project or one wrapper, optionally across every online wrapper at once.

The server keeps only metadata. Content lives on the wrapper and is searched on demand via RPC over the existing WebSocket.

## Decisions

| Question | Choice |
| --- | --- |
| Search semantics | Literal substring (case-insensitive option). No FTS / no embeddings. |
| What the server stores | Metadata only — no message bodies, no extracts. |
| Directories scanned | `~/.claude/projects/` only. |
| Scan schedule | Full scan at wrapper startup + fsnotify watcher (poll fallback) for live deltas. |
| Wrapper-local cache | Metadata in-memory only. No disk persistence. JSONL files are the source of truth. |
| Offline wrappers during search | Fan-out to online wrappers; offline ones reported as `unavailable` in the response. |

Explicitly **out of v1** (YAGNI): viewing full conversation contents in the portal, cross-wrapper project matching by `cwd`, persisted search history, language-aware highlighting, streaming partial results.

## Data model (server)

Two Mongo collections, both scoped to the wrapper that produced them.

### `projects`

| Field | Notes |
| --- | --- |
| `_id` | ObjectID |
| `user_id`, `wrapper_id` | ownership |
| `slug` | original `~/.claude/projects/` dir name; unique key per wrapper |
| `cwd` | absolute path Claude recorded |
| `name` | `filepath.Base(cwd)` — displayed in portal |
| `session_count` | maintained by upserts |
| `first_activity_at`, `last_activity_at` | min/max of transcripts |

Index: `(user_id, wrapper_id, slug)` unique. `(user_id, last_activity_at desc)`.

### `transcripts`

| Field | Notes |
| --- | --- |
| `_id` | ObjectID |
| `user_id`, `wrapper_id`, `project_id` | ownership + grouping |
| `jsonl_uuid` | filename without extension; matches `internal/tail` semantics |
| `path` | relative to `~/.claude/projects/`; opaque to server, used by wrapper |
| `started_at`, `ended_at` | first/last timestamp in the JSONL |
| `message_count` | line count |
| `title` | first user prompt truncated to 120 chars; placeholder if none |
| `bytes` | size at the moment of the last diff |

Index: `(wrapper_id, jsonl_uuid)` unique. `(user_id, project_id, last_activity_at desc)`. `(user_id, started_at desc)`.

## Wire protocol (additions to `internal/proto`)

Four new envelope types over the existing `{v, type, session, payload}` JSON frame.

### `catalog.diff` (wrapper → server)

```json
{ "type": "catalog.diff", "payload": {
    "full": true,
    "projects":   [ {…project record…} ],
    "transcripts":[ {…transcript record…} ],
    "removed_transcripts": ["jsonl_uuid", "…"]
}}
```

- `full: true` on the first frame after every connect. Server treats it as ground truth for this wrapper and replaces its slice of the catalog atomically.
- `full: false` for incremental diffs emitted by the watcher: new transcript, append (changed `ended_at`/`message_count`/`bytes`), or deletion.
- Idempotent by design: if the server loses state (Mongo wipe, fresh container), the next wrapper reconnect repopulates everything.

### `search.request` (server → wrapper)

```json
{ "type": "search.request", "session": "<request_id>", "payload": {
    "query": "literal substring",
    "project_id": "…",
    "transcript_ids": ["…"],
    "max_results": 100,
    "snippet_chars": 120,
    "case_insensitive": true
}}
```

`project_id` and `transcript_ids` are both optional and additive (intersection). `session` field of the envelope carries the correlation id.

### `search.results` (wrapper → server)

```json
{ "type": "search.results", "session": "<request_id>", "payload": {
    "matches": [
      {"transcript_id":"…","msg_index":17,"role":"assistant","snippet":"…<mark>X</mark>…","ts":"…"}
    ],
    "truncated": false,
    "elapsed_ms": 43
}}
```

`truncated: true` when the wrapper hit `max_results` or its internal 10 s timeout.

### `search.cancel` (server → wrapper)

Optional v1.5; lets the dispatcher tell wrappers to stop a search that already returned enough hits. Not blocking for v1 — wrappers honor `max_results` and the per-search 10 s ceiling.

## Wrapper architecture

New package `internal/transcripts`, pure logic, no networking.

- **`Scanner.Scan(ctx) (*Catalog, error)`** — walks `~/.claude/projects/*/*.jsonl`. Reads only the first and last lines of each JSONL to derive `started_at`, `ended_at`, `title`. `message_count` and `bytes` come from `os.Stat` + a streaming line counter. Target budget: < 100 ms for 500 sessions.
- **`Catalog`** — concurrent map of projects and transcripts. `Snapshot()` returns a value suitable for `catalog.diff full=true`. `Diff(prev *Catalog)` returns the incremental payload.
- **`Watcher`** — fsnotify recursive over `~/.claude/projects/`, with a 30 s polling fallback for filesystems without notify (NFS, macOS Time Machine volumes). Emits `transcriptChanged(uuid)`, `transcriptRemoved(uuid)`, `projectAdded(slug)`. The wrapper handles those by calling `Scanner.RescanOne(path)` and updating the `Catalog`.
- **`Searcher`** — executes a `search.request`. Resolves scope to JSONL paths via the catalog, streams each file line-by-line, decodes the message body, runs `strings.Contains` (case-insensitive optional). Emits matches with `msg_index` and a snippet of ±`snippet_chars/2` around the hit. Hard timeout 10 s.

`internal/tail` (the live PTY-side transcript follower) is untouched. The new package is for historical files regardless of whether the wrapper is currently supervising that session.

Wiring in `cmd/claude-switch/main.go`:

1. `Scanner.Scan` runs before `cli.Run` so the wrapper's first frame after hello is `catalog.diff full=true`.
2. `Watcher` runs in a goroutine; its events feed back into the catalog and emit `catalog.diff full=false`.
3. `Searcher` is invoked from the `ws.handleControl` dispatcher when `search.request` arrives. Each search runs in its own goroutine so concurrent searches and PTY traffic don't block each other.

## Server architecture

### Storage

Two new repos in `internal/store`:

- `ProjectsRepo` — `UpsertMany`, `DeleteForWrapperExcept(wrapperID, slugs []string)`, `ListByUser`, `ListByWrapper`.
- `TranscriptsRepo` — `UpsertMany`, `DeleteByUUIDs`, `ReplaceForWrapper(wrapperID string, projects, transcripts)`, `ListByProject`, `ListRecentByUser`, `GetByID`.

`ReplaceForWrapper` is the path used by `catalog.diff full=true`; it deletes anything the wrapper doesn't mention. Idempotent and crash-safe.

### Catalog ingestion

New `case` in `internal/wswrapper/wswrapper.go::handleText`:

- `proto.TypeCatalogDiff` with `full=true` → `TranscriptsRepo.ReplaceForWrapper` + `ProjectsRepo` upsert/delete.
- `proto.TypeCatalogDiff` with `full=false` → `UpsertMany` + `DeleteByUUIDs`.

### Search dispatcher

New package `internal/searchhub`, separate from `internal/hub` so PTY routing stays clean.

- `Dispatch(ctx, query SearchQuery) (Response, error)` — mints a `request_id`, asks `searchhub` to fan out `search.request` to the matching wrappers (by `user_id`, optionally further filtered by `wrapper_ids`), waits with a 15 s server-side ceiling for `search.results` to come back through `wswrapper`, aggregates and returns.
- Offline wrappers and wrappers that didn't respond before the timeout are reported per-wrapper: `{wid: {status: "ok"|"timeout"|"offline", count, elapsed_ms}}`.
- New `FrameType` in `internal/hub`: `FrameTypeSearchRequest`, with its branch in `wrapperConn.Send`.
- The existing `wswrapper.handleText` gets a new case for `search.results` that routes the frame to `searchhub` via a request-id channel map.

### HTTP API

Routes in `internal/api/router.go`:

- `GET /api/projects?wrapper_id=…` — list user's projects, filters.
- `GET /api/transcripts?project_id=…&wrapper_id=…&limit=…` — list catalog entries.
- `GET /api/transcripts/{id}` — single metadata record.
- `POST /api/search` — body `{query, project_id?, wrapper_ids?, transcript_ids?, max_results?, case_insensitive?}`. Responds with aggregated matches and per-wrapper status.

Authorization: existing session cookie + CSRF on `POST /api/search` to stay consistent with the project's existing write-CSRF policy, even though the request only reads.

### Type generation

Structs in `internal/store/{projects,transcripts}.go` get `bson` and `json` tags. `make codegen-ts` regenerates `web/src/api/types.ts` with `Project` and `Transcript`.

## Frontend (`web/`)

- New TanStack Router routes:
  - `/transcripts` — sidebar tree "Wrapper → Projects"; main pane is a filterable table of sessions with a search input.
  - `/transcripts/$id` — metadata detail for a single transcript + a "search inside this session" affordance.
- New TanStack Query mutation `useSearch` wrapping `POST /api/search`. Renders per-wrapper status (`ok` / `timeout` / `offline`) in a side panel so the user understands why a wrapper had zero hits.
- Snippets render as plain text with a `<mark>` span around the query. No xterm.

## Delivery plan (PRs / commits)

1. `feat(transcripts): scanner+catalog in wrapper` — pure package + unit tests against synthetic JSONLs.
2. `feat(transcripts): watcher with fsnotify+poll fallback` — built on the scanner.
3. `feat(store): projects + transcripts repos` — Mongo schemas, repos, `make codegen-ts`.
4. `feat(proto): catalog.diff frames + search request/results` — protocol + Go types only, no handlers.
5. `feat(wswrapper): persist catalog diff` — server-side ingestion + testcontainer tests.
6. `feat(searchhub): dispatcher with timeout+offline` — package + unit tests against a fake `WrapperConn`.
7. `feat(api): /api/projects, /api/transcripts, /api/search` — handlers + tests.
8. `feat(wrapper): wire scanner+watcher+search responder` — main.go integration; e2e test under the `e2e` build tag.
9. `feat(web): transcripts routes + search UI` — frontend, vitest + playwright.

Steps 1–7 are mergeable independently of the portal change.

## Open questions parked for later

- Cross-wrapper project deduplication when the same `cwd` shows up on multiple machines (e.g. a repo cloned in two laptops). Today they're separate `projects` rows; the portal can group them client-side. Promote to server-side dedup when a real use case appears.
- Fetching the full conversation body to view in the portal (`POST /api/transcripts/{id}/fetch` RPC'd to a single wrapper). v2.
- Surfacing search results progressively (SSE / Server-Sent Events) instead of waiting for the 15 s timeout. v2 if perception of latency becomes an issue.
