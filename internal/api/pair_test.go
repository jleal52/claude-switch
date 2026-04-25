package api

import (
	"bytes"
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

func TestPairRedeemApproves(t *testing.T) {
	s := newTestStore(t, "pair_redeem")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	pc, _ := s.Pairing().Create(ctx, store.WrapperDescriptor{Name: "x", OS: "linux", Arch: "amd64"}, time.Minute)
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := NewPairHandlers(s)
	body, _ := json.Marshal(map[string]string{"code": pc.Code})
	req := httptest.NewRequest("POST", "/api/pair/redeem", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)

	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Redeem)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	got, _ := s.Pairing().GetByCode(ctx, pc.Code)
	require.Equal(t, "approved", got.Status)
	require.Equal(t, u.ID, got.UserID)
}

func TestPairRedeemUnknownCode(t *testing.T) {
	s := newTestStore(t, "pair_unknown")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := NewPairHandlers(s)
	body, _ := json.Marshal(map[string]string{"code": "ZZZZ-9999"})
	req := httptest.NewRequest("POST", "/api/pair/redeem", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Redeem)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code)
}
