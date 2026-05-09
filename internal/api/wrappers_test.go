package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

type fakePresence map[string]bool

func (f fakePresence) WrapperOnline(id string) bool { return f[id] }

func TestWrappersListReturnsOnlyOwn(t *testing.T) {
	s := newTestStore(t, "wr_list")
	ctx := context.Background()
	u1, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	u2, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u2"))
	_, _, _ = s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u1.ID, Name: "mine", OS: "linux", Arch: "amd64"})
	_, _, _ = s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u2.ID, Name: "other", OS: "linux", Arch: "amd64"})

	sess, _ := s.AuthSessions().Create(ctx, u1.ID, time.Hour)
	h := NewWrappersHandlers(s, fakePresence{})
	req := httptest.NewRequest("GET", "/api/wrappers", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	require.Len(t, got, 1)
	require.Equal(t, "mine", got[0].Name)
}

func TestWrappersListReportsOnline(t *testing.T) {
	s := newTestStore(t, "wr_online")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("uo"))
	wOnline, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "live", OS: "linux", Arch: "amd64"})
	_, _, _ = s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "dead", OS: "linux", Arch: "amd64"})

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	h := NewWrappersHandlers(s, fakePresence{wOnline.ID: true})
	req := httptest.NewRequest("GET", "/api/wrappers", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []struct {
		Name   string `json:"name"`
		Online bool   `json:"online"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got, 2)
	byName := map[string]bool{}
	for _, w := range got {
		byName[w.Name] = w.Online
	}
	require.True(t, byName["live"], "live wrapper should be online")
	require.False(t, byName["dead"], "dead wrapper should be offline")
}

func TestWrappersDeleteOnlyOwn(t *testing.T) {
	s := newTestStore(t, "wr_delete")
	ctx := context.Background()
	owner, _ := s.Users().UpsertOAuth(ctx, fakeProfile("o"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("x"))
	w1, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: owner.ID, Name: "mine", OS: "linux", Arch: "amd64"})
	w2, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "other", OS: "linux", Arch: "amd64"})

	sess, _ := s.AuthSessions().Create(ctx, owner.ID, time.Hour)
	h := NewWrappersHandlers(s, fakePresence{})

	req := httptest.NewRequest("DELETE", "/api/wrappers/"+w1.ID, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	req.SetPathValue("id", w1.ID)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Delete)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNoContent, rr.Code)

	req = httptest.NewRequest("DELETE", "/api/wrappers/"+w2.ID, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	req.SetPathValue("id", w2.ID)
	rr = httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Delete)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code)
}
