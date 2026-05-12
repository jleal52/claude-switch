// Package wswrapper handles the /ws/wrapper endpoint where wrappers
// connect. It owns the goroutines that read frames from a wrapper, fan them
// out to browser subscribers via the hub, and persist state changes to Mongo.
package wswrapper

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/store"
)

// pingWriteTimeout caps how long a single ping write can block before we give
// up and let the read loop notice the dead connection.
const pingWriteTimeout = 5 * time.Second

// DefaultWrapperPingInterval is how often the server sends an application-level
// ping to each connected wrapper. The wrapper's read loop applies a 45s
// deadline (see internal/ws.Config.ReadTimeout); this interval must stay well
// under that deadline so an idle wrapper never times out.
const DefaultWrapperPingInterval = 20 * time.Second

// SearchSink receives search.results frames from connected wrappers. The
// searchhub package implements this and routes them to waiting Dispatch
// callers. A nil sink (zero-value) is safe; results are dropped.
type SearchSink interface {
	Deliver(requestID, wrapperID string, results proto.SearchResults)
}

type nopSearchSink struct{}

func (nopSearchSink) Deliver(string, string, proto.SearchResults) {}

type Handler struct {
	store        *store.Store
	hub          *hub.Hub
	searchSink   SearchSink
	pingInterval time.Duration
}

func NewHandler(s *store.Store, h *hub.Hub) *Handler {
	return &Handler{store: s, hub: h, searchSink: nopSearchSink{}, pingInterval: DefaultWrapperPingInterval}
}

// SetSearchSink wires the searchhub into the handler. Called from the
// server bootstrap after both have been constructed.
func (h *Handler) SetSearchSink(sink SearchSink) {
	if sink == nil {
		sink = nopSearchSink{}
	}
	h.searchSink = sink
}

// newHandlerWithPingInterval is for tests that need a faster ping cadence.
func newHandlerWithPingInterval(s *store.Store, h *hub.Hub, interval time.Duration) *Handler {
	return &Handler{store: s, hub: h, searchSink: nopSearchSink{}, pingInterval: interval}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok := bearerToken(r.Header.Get("Authorization"))
	if tok == "" {
		http.Error(w, "missing bearer", http.StatusUnauthorized)
		return
	}
	at, err := h.store.WrapperTokens().Verify(r.Context(), tok)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(8 * 1024 * 1024)

	wrapperID := at.WrapperID
	conn := newWrapperConn(c)
	h.hub.RegisterWrapper(wrapperID, conn)
	defer func() {
		h.hub.UnregisterWrapper(wrapperID)
		_, _ = h.store.Sessions().MarkWrapperOffline(context.Background(), wrapperID)
		c.CloseNow()
	}()

	ctx := r.Context()
	if h.pingInterval > 0 {
		pingCtx, cancelPing := context.WithCancel(ctx)
		defer cancelPing()
		go h.pingLoop(pingCtx, c)
	}

	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			id, payload, err := proto.DecodePTYData(data)
			if err == nil {
				h.hub.FanoutPTYData(id.String(), payload)
			}
		case websocket.MessageText:
			h.handleText(ctx, wrapperID, at.UserID, data)
		}
	}
}

func (h *Handler) handleText(ctx context.Context, wrapperID, userID string, data []byte) {
	t, sessionID, payload, err := proto.Decode(data)
	if err != nil {
		return
	}
	switch t {
	case proto.TypeHello:
		var hello proto.Hello
		_ = payload.Into(&hello)
		_ = h.store.Wrappers().UpdateLastSeen(ctx, wrapperID)
		h.reconcile(ctx, wrapperID, hello.Sessions)
	case proto.TypeSessionStarted:
		var ss proto.SessionStarted
		_ = payload.Into(&ss)
		_ = h.store.Sessions().MarkRunning(ctx, sessionID, ss.JSONLUUID)
		h.hub.AssignSession(sessionID, wrapperID)
		h.hub.FanoutControl(sessionID, "session.started", ss)
	case proto.TypeSessionExited:
		var se proto.SessionExited
		_ = payload.Into(&se)
		_ = h.store.Sessions().MarkExited(ctx, sessionID, se.ExitCode, se.Reason, se.Detail)
		h.hub.FanoutControl(sessionID, "session.exited", se)
	case proto.TypeJSONLTail:
		var jt proto.JSONLTail
		_ = payload.Into(&jt)
		if u, err := h.store.Users().GetByID(ctx, userID); err == nil && u.KeepTranscripts {
			_ = h.store.Messages().Append(ctx, sessionID, userID, time.Now(), jt.Entry)
		}
		h.hub.FanoutControl(sessionID, "jsonl.tail", jt)
	case proto.TypePong:
		// liveness only.
	case proto.TypeCatalogDiff:
		var cd proto.CatalogDiff
		_ = payload.Into(&cd)
		h.applyCatalogDiff(ctx, userID, wrapperID, cd)
	case proto.TypeSearchResults:
		var sr proto.SearchResults
		_ = payload.Into(&sr)
		h.searchSink.Deliver(sessionID, wrapperID, sr)
	}
}

// applyCatalogDiff writes the wrapper's transcripts-catalog payload to the
// store. Full=true is the ground-truth path used after every reconnect:
// the server replaces its slice of the catalog atomically with what the
// wrapper sent. Full=false carries only deltas.
func (h *Handler) applyCatalogDiff(ctx context.Context, userID, wrapperID string, cd proto.CatalogDiff) {
	projects := make([]store.ProjectUpsert, 0, len(cd.Projects))
	for _, p := range cd.Projects {
		projects = append(projects, store.ProjectUpsert{
			Slug:            p.Slug,
			Cwd:             p.Cwd,
			Name:            p.Name,
			SessionCount:    p.SessionCount,
			FirstActivityAt: parseTime(p.FirstActivityAt),
			LastActivityAt:  parseTime(p.LastActivityAt),
		})
	}
	transcripts := make([]store.TranscriptUpsert, 0, len(cd.Transcripts))
	for _, t := range cd.Transcripts {
		transcripts = append(transcripts, store.TranscriptUpsert{
			JSONLUUID:    t.JSONLUUID,
			ProjectSlug:  t.Slug,
			Path:         t.Path,
			StartedAt:    parseTime(t.StartedAt),
			EndedAt:      parseTime(t.EndedAt),
			MessageCount: t.MessageCount,
			Title:        t.Title,
			Bytes:        t.Bytes,
		})
	}

	if cd.Full {
		_ = h.store.Transcripts().ReplaceForWrapper(ctx, userID, wrapperID, projects, transcripts)
		return
	}
	// Incremental: upsert projects we mention (without deleting unmentioned
	// ones — the wrapper would only diff something that changed), upsert
	// transcripts, then delete the ones listed as removed.
	slugMap, err := h.store.Projects().UpsertMany(ctx, userID, wrapperID, projects)
	if err != nil {
		return
	}
	// Some incremental diffs touch only transcripts; fetch the slug→id map
	// for any project slugs we reference but didn't upsert this round.
	missing := map[string]bool{}
	for _, t := range cd.Transcripts {
		if _, ok := slugMap[t.Slug]; !ok {
			missing[t.Slug] = true
		}
	}
	if len(missing) > 0 {
		known, err := h.store.Projects().ListByWrapper(ctx, wrapperID)
		if err == nil {
			for _, p := range known {
				if missing[p.Slug] {
					slugMap[p.Slug] = p.ID
				}
			}
		}
	}
	_ = h.store.Transcripts().UpsertMany(ctx, userID, wrapperID, slugMap, transcripts)
	_ = h.store.Transcripts().DeleteByUUIDs(ctx, wrapperID, cd.RemovedTranscripts)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// pingLoop emits an application-level ping frame on the cadence configured
// for the handler. This keeps the wrapper's per-read deadline (45s) from
// firing on idle connections; the wrapper responds with TypePong, which is
// dropped by handleText after updating liveness implicitly via the read.
func (h *Handler) pingLoop(ctx context.Context, c *websocket.Conn) {
	t := time.NewTicker(h.pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			raw, err := proto.Encode(proto.TypePing, "", proto.Ping{Nonce: ulid.Make().String()})
			if err != nil {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, pingWriteTimeout)
			err = c.Write(wctx, websocket.MessageText, raw)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// reconcile compares the wrapper's live sessions with the server's catalog.
// Sessions in the DB as live for this wrapper but missing from `live` are
// marked exited (reason: wrapper_restart).
func (h *Handler) reconcile(ctx context.Context, wrapperID string, helloSessions []proto.HelloSession) {
	live := map[string]bool{}
	for _, hs := range helloSessions {
		live[hs.ID] = true
		if err := h.store.Sessions().MarkRunningFromOffline(ctx, hs.ID); err == nil {
			h.hub.AssignSession(hs.ID, wrapperID)
		}
	}
	rows, err := h.store.Sessions().ListLiveByWrapper(ctx, wrapperID)
	if err != nil {
		return
	}
	for _, row := range rows {
		if !live[row.ID] {
			_ = h.store.Sessions().MarkExited(ctx, row.ID, -1, "wrapper_restart", "")
		}
	}
}

func bearerToken(h string) string {
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return ""
	}
	return h[len(p):]
}

// wrapperConn implements hub.WrapperConn over a *websocket.Conn.
type wrapperConn struct {
	c *websocket.Conn
}

func newWrapperConn(c *websocket.Conn) *wrapperConn { return &wrapperConn{c: c} }

func (w *wrapperConn) Send(fr hub.OutboundFrame) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch fr.Type {
	case hub.FrameTypeOpenSession:
		raw, err := proto.Encode(proto.TypeOpenSession, fr.SessionID, fr.JSON)
		if err != nil {
			return err
		}
		return w.c.Write(ctx, websocket.MessageText, raw)
	case hub.FrameTypeCloseSession:
		raw, err := proto.Encode(proto.TypeCloseSession, fr.SessionID, struct{}{})
		if err != nil {
			return err
		}
		return w.c.Write(ctx, websocket.MessageText, raw)
	case hub.FrameTypePTYInput:
		id, err := ulid.ParseStrict(fr.SessionID)
		if err != nil {
			return err
		}
		raw := proto.EncodePTYData(id, fr.Binary)
		return w.c.Write(ctx, websocket.MessageBinary, raw)
	case hub.FrameTypePTYResize:
		raw, err := proto.Encode(proto.TypePTYResize, fr.SessionID, fr.JSON)
		if err != nil {
			return err
		}
		return w.c.Write(ctx, websocket.MessageText, raw)
	}
	return errors.New("wrapperConn: unknown frame type")
}

func (w *wrapperConn) Close() { _ = w.c.CloseNow() }
