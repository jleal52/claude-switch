package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/stretchr/testify/require"
)

func TestMeReturnsUser(t *testing.T) {
	s := newTestStore(t, "me_basic")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := NewMeHandlers(MeConfig{
		Store:               s,
		ProvidersConfigured: []string{"github"},
	})
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Get)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		ProvidersConfigured []string `json:"providers_configured"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Equal(t, u.ID, got.User.ID)
	require.Contains(t, got.ProvidersConfigured, "github")
}

func TestMePostSettingsRequiresCSRF(t *testing.T) {
	s := newTestStore(t, "me_settings")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u2"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := NewMeHandlers(MeConfig{Store: s, ProvidersConfigured: []string{"github"}})
	body := []byte(`{"keep_transcripts":true}`)
	req := httptest.NewRequest("POST", "/api/me/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)

	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.UpdateSettings)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNoContent, rr.Code)

	got, _ := s.Users().GetByID(ctx, u.ID)
	require.True(t, got.KeepTranscripts)
	_ = io.EOF
}

func TestMeSettingsClampsRetention(t *testing.T) {
	s := newTestStore(t, "me_clamp")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u3"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := NewMeHandlers(MeConfig{Store: s, ProvidersConfigured: []string{"github"}})

	// Above 90 → clamped to 90.
	body := []byte(`{"transcript_retention_days":1000}`)
	req := httptest.NewRequest("POST", "/api/me/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.UpdateSettings)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNoContent, rr.Code)

	got, _ := s.Users().GetByID(ctx, u.ID)
	require.Equal(t, 90, got.TranscriptRetentionDays)

	// Below 1 → clamped to 1.
	body = []byte(`{"transcript_retention_days":0}`)
	req = httptest.NewRequest("POST", "/api/me/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr = httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.UpdateSettings)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNoContent, rr.Code)

	got, _ = s.Users().GetByID(ctx, u.ID)
	require.Equal(t, 1, got.TranscriptRetentionDays)
}
