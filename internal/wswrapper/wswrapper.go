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

type Handler struct {
	store *store.Store
	hub   *hub.Hub
}

func NewHandler(s *store.Store, h *hub.Hub) http.Handler {
	return &Handler{store: s, hub: h}
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
