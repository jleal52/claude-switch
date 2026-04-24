# claude-switch

Native wrapper + central server + web frontend to drive multiple `claude` CLI sessions remotely, across machines and accounts.

Designed in four subsystems (each with its own spec + plan):

1. **Wrapper PTY** — Go binary on the user's machine; hosts N `claude` PTY sessions and streams them over a single outbound WebSocket.
2. **Server** — central relay; public API, session catalog, authentication.
3. **Frontend** — browser UI that connects to the server and exposes terminals, transcripts, and session management.
4. **Multi-account** — profiles per account, credential isolation, account-aware routing.

Current status: design phase for subsystem 1. See `docs/superpowers/specs/`.
