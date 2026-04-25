package wsbrowser

import (
	"context"
	"net/http"
	"sync"
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

const (
	sessionCookieName = "cs_session"
)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	auth, err := h.store.AuthSessions().GetByID(r.Context(), cookie.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	csrfToken := r.URL.Query().Get("ct")
	if csrfToken == "" || csrfToken != auth.CSRFToken {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}

	sid := r.PathValue("id")
	row, err := h.store.Sessions().GetByID(r.Context(), sid)
	if err != nil || row.UserID != auth.UserID {
		http.NotFound(w, r)
		return
	}
	sessULID, err := ulid.ParseStrict(sid)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(4 * 1024 * 1024)
	bc := newBrowserConn(c, sessULID)

	replay, subErr := h.hub.Subscribe(sid, bc)
	if subErr != nil {
		_ = bc.SendControl("wrapper.offline", nil)
		bc.Close(1011, "wrapper offline")
		return
	}
	defer h.hub.Unsubscribe(sid, bc)

	_ = bc.SendControl("replay.start", map[string]string{"session": sid})
	if len(replay) > 0 {
		_ = bc.SendPTYData(replay)
	}
	_ = bc.SendControl("replay.end", map[string]string{"session": sid})

	ctx := r.Context()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			_, payload, err := proto.DecodePTYData(data)
			if err == nil {
				_ = h.hub.SendInput(sid, payload)
			}
		case websocket.MessageText:
			// pty.resize handled inline; ignore other text frames in MVP.
		}
	}
}

// browserConn implements hub.BrowserConn.
type browserConn struct {
	c  *websocket.Conn
	mu sync.Mutex
	id ulid.ULID
}

func newBrowserConn(c *websocket.Conn, id ulid.ULID) *browserConn {
	return &browserConn{c: c, id: id}
}

func (b *browserConn) SendPTYData(payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	frame := proto.EncodePTYData(b.id, payload)
	return b.c.Write(ctx, websocket.MessageBinary, frame)
}

func (b *browserConn) SendControl(typ string, payload any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := proto.Encode(typ, "", payload)
	if err != nil {
		return err
	}
	return b.c.Write(ctx, websocket.MessageText, raw)
}

func (b *browserConn) Close(code int, reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_ = b.c.Close(websocket.StatusCode(code), reason)
}
