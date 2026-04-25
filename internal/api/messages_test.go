package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

func TestMessagesListReturnsForOwnSession(t *testing.T) {
	s := newTestStore(t, "msg_list")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "s1", UserID: u.ID, WrapperID: w.ID, Cwd: "/", Account: "default"})

	t0 := time.Now().UTC()
	require.NoError(t, s.Messages().Append(ctx, "s1", u.ID, t0, `{"role":"user","content":"hi"}`))
	require.NoError(t, s.Messages().Append(ctx, "s1", u.ID, t0.Add(time.Second), `{"role":"assistant","content":"hello"}`))

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	h := NewMessagesHandlers(s)
	req := httptest.NewRequest("GET", "/api/sessions/s1/messages", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.SetPathValue("id", "s1")
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []struct {
		Entry string `json:"entry"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	require.Len(t, got, 2)
}

func TestMessagesListForeignSessionIs404(t *testing.T) {
	s := newTestStore(t, "msg_foreign")
	ctx := context.Background()
	owner, _ := s.Users().UpsertOAuth(ctx, fakeProfile("o"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("x"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "x", OS: "linux", Arch: "amd64"})
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "s1", UserID: other.ID, WrapperID: w.ID, Cwd: "/", Account: "default"})

	sess, _ := s.AuthSessions().Create(ctx, owner.ID, time.Hour)
	h := NewMessagesHandlers(s)
	req := httptest.NewRequest("GET", "/api/sessions/s1/messages", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.SetPathValue("id", "s1")
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code)
}
