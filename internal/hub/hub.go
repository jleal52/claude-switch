package hub

import (
	"context"
	"errors"
	"sync"

	"github.com/jleal52/claude-switch/internal/ring"
)

const ringBytesPerSession = 32 * 1024

var ErrWrapperOffline = errors.New("hub: wrapper offline")

type FrameType int

const (
	FrameTypeOpenSession FrameType = iota
	FrameTypeCloseSession
	FrameTypePTYInput
	FrameTypePTYResize
	FrameTypePing
)

// OutboundFrame is what the hub asks a WrapperConn to send. The wswrapper
// package translates these into wire frames using internal/proto.
type OutboundFrame struct {
	Type      FrameType
	SessionID string
	JSON      any    // for open_session, close_session, pty.resize, ping
	Binary    []byte // for pty.input
}

// WrapperConn is implemented by the wswrapper package; the hub uses it as
// a write-only channel.
type WrapperConn interface {
	Send(OutboundFrame) error
	Close()
}

// BrowserConn is implemented by wsbrowser; hub fans pty.data out to all subscribers.
type BrowserConn interface {
	SendPTYData(b []byte) error
	SendControl(typ string, payload any) error
	Close(code int, reason string)
}

type Hub struct {
	mu          sync.RWMutex
	wrappers    map[string]WrapperConn
	sessionWrap map[string]string                   // sessionID -> wrapperID
	subscribers map[string]map[BrowserConn]struct{} // sessionID -> set
	rings       map[string]*ring.Buffer
}

func New() *Hub {
	return &Hub{
		wrappers:    map[string]WrapperConn{},
		sessionWrap: map[string]string{},
		subscribers: map[string]map[BrowserConn]struct{}{},
		rings:       map[string]*ring.Buffer{},
	}
}

func (h *Hub) RegisterWrapper(id string, conn WrapperConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.wrappers[id]; ok && old != conn {
		old.Close()
	}
	h.wrappers[id] = conn
}

func (h *Hub) UnregisterWrapper(id string) {
	h.mu.Lock()
	conn := h.wrappers[id]
	delete(h.wrappers, id)
	var orphans []string
	for sid, wid := range h.sessionWrap {
		if wid == id {
			orphans = append(orphans, sid)
		}
	}
	subs := make(map[string]map[BrowserConn]struct{}, len(orphans))
	for _, sid := range orphans {
		delete(h.sessionWrap, sid)
		if set, ok := h.subscribers[sid]; ok {
			subs[sid] = set
			delete(h.subscribers, sid)
		}
	}
	h.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	for _, set := range subs {
		for b := range set {
			_ = b.SendControl("wrapper.offline", nil)
			b.Close(1011, "wrapper offline")
		}
	}
}

func (h *Hub) OpenSession(ctx context.Context, req OpenSessionRequest) error {
	h.mu.Lock()
	conn, ok := h.wrappers[req.WrapperID]
	if !ok {
		h.mu.Unlock()
		return ErrWrapperOffline
	}
	h.sessionWrap[req.SessionID] = req.WrapperID
	h.mu.Unlock()
	return conn.Send(OutboundFrame{
		Type: FrameTypeOpenSession, SessionID: req.SessionID,
		JSON: map[string]any{
			"session": req.SessionID, "cwd": req.Cwd,
			"account": req.Account, "args": req.Args,
		},
	})
}

func (h *Hub) CloseSession(ctx context.Context, sessionID string) error {
	h.mu.Lock()
	wid, ok := h.sessionWrap[sessionID]
	if !ok {
		h.mu.Unlock()
		return nil
	}
	conn := h.wrappers[wid]
	delete(h.sessionWrap, sessionID)
	delete(h.rings, sessionID)
	subs := h.subscribers[sessionID]
	delete(h.subscribers, sessionID)
	h.mu.Unlock()

	if conn != nil {
		_ = conn.Send(OutboundFrame{Type: FrameTypeCloseSession, SessionID: sessionID})
	}
	for b := range subs {
		b.Close(1000, "session closed")
	}
	return nil
}

func (h *Hub) Subscribe(sessionID string, b BrowserConn) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.sessionWrap[sessionID]; !ok {
		return nil, ErrWrapperOffline
	}
	set, ok := h.subscribers[sessionID]
	if !ok {
		set = map[BrowserConn]struct{}{}
		h.subscribers[sessionID] = set
	}
	set[b] = struct{}{}
	if rb, ok := h.rings[sessionID]; ok {
		return rb.Snapshot(), nil
	}
	return nil, nil
}

func (h *Hub) Unsubscribe(sessionID string, b BrowserConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.subscribers[sessionID]; ok {
		delete(set, b)
		if len(set) == 0 {
			delete(h.subscribers, sessionID)
		}
	}
}

func (h *Hub) FanoutPTYData(sessionID string, payload []byte) {
	h.UpdateRing(sessionID, payload)
	h.mu.RLock()
	subs := make([]BrowserConn, 0, len(h.subscribers[sessionID]))
	for b := range h.subscribers[sessionID] {
		subs = append(subs, b)
	}
	h.mu.RUnlock()
	for _, b := range subs {
		_ = b.SendPTYData(payload)
	}
}

func (h *Hub) FanoutControl(sessionID, typ string, payload any) {
	h.mu.RLock()
	subs := make([]BrowserConn, 0, len(h.subscribers[sessionID]))
	for b := range h.subscribers[sessionID] {
		subs = append(subs, b)
	}
	h.mu.RUnlock()
	for _, b := range subs {
		_ = b.SendControl(typ, payload)
	}
}

func (h *Hub) SendInput(sessionID string, payload []byte) error {
	h.mu.RLock()
	wid, ok := h.sessionWrap[sessionID]
	if !ok {
		h.mu.RUnlock()
		return ErrWrapperOffline
	}
	conn := h.wrappers[wid]
	h.mu.RUnlock()
	if conn == nil {
		return ErrWrapperOffline
	}
	return conn.Send(OutboundFrame{Type: FrameTypePTYInput, SessionID: sessionID, Binary: payload})
}

func (h *Hub) UpdateRing(sessionID string, payload []byte) {
	h.mu.Lock()
	rb, ok := h.rings[sessionID]
	if !ok {
		rb = ring.New(ringBytesPerSession)
		h.rings[sessionID] = rb
	}
	h.mu.Unlock()
	_, _ = rb.Write(payload)
}

func (h *Hub) SnapshotRing(sessionID string) []byte {
	h.mu.RLock()
	rb := h.rings[sessionID]
	h.mu.RUnlock()
	if rb == nil {
		return nil
	}
	return rb.Snapshot()
}

func (h *Hub) AssignSession(sessionID, wrapperID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessionWrap[sessionID] = wrapperID
}
