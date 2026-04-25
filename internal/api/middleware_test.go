package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddlewareRejectsAnonymous(t *testing.T) {
	s := newTestStore(t, "mw_anon")
	mw := NewAuthMiddleware(s)
	called := false
	h := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/me", nil))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.False(t, called)
}

func TestAuthMiddlewareInjectsUser(t *testing.T) {
	s := newTestStore(t, "mw_inject")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	mw := NewAuthMiddleware(s)
	var seen string
	h := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := UserFromContext(r.Context())
		seen = got.ID
	}))
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, u.ID, seen)
}

func TestCSRFMiddlewareRejectsMutatingWithoutHeader(t *testing.T) {
	s := newTestStore(t, "mw_csrf")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u2"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	mw := NewAuthMiddleware(s)
	called := false
	h := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	req := httptest.NewRequest("POST", "/api/foo", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	// no X-CSRF-Token header
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)
	require.False(t, called)
}
