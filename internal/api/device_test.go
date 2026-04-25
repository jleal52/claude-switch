package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

func TestDevicePairStartCreatesPendingCode(t *testing.T) {
	s := newTestStore(t, "dev_start")
	h := NewDeviceHandlers(s)

	body, _ := json.Marshal(map[string]string{
		"name": "ireland", "os": "linux", "arch": "amd64", "version": "0.1.0",
	})
	req := httptest.NewRequest("POST", "/device/pair/start", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.PairStart(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp struct {
		Code      string `json:"code"`
		PollURL   string `json:"poll_url"`
		ExpiresIn int    `json:"expires_in"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NotEmpty(t, resp.Code)
	require.Equal(t, "/device/pair/poll?c="+resp.Code, resp.PollURL)
}

func TestDevicePairPollPendingThenApproved(t *testing.T) {
	s := newTestStore(t, "dev_poll")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	pc, _ := s.Pairing().Create(ctx, store.WrapperDescriptor{Name: "x", OS: "linux", Arch: "amd64"}, time.Minute)

	h := NewDeviceHandlers(s, WithServerEndpoint("wss://server.example.com/ws/wrapper"))

	req := httptest.NewRequest("GET", "/device/pair/poll?c="+pc.Code, nil)
	rr := httptest.NewRecorder()
	h.PairPoll(rr, req)
	require.Equal(t, http.StatusAccepted, rr.Code)

	require.NoError(t, s.Pairing().Approve(ctx, pc.Code, u.ID))

	rr = httptest.NewRecorder()
	h.PairPoll(rr, httptest.NewRequest("GET", "/device/pair/poll?c="+pc.Code, nil))
	require.Equal(t, http.StatusOK, rr.Code)

	var creds struct {
		AccessToken    string `json:"access_token"`
		RefreshToken   string `json:"refresh_token"`
		ServerEndpoint string `json:"server_endpoint"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &creds)
	require.NotEmpty(t, creds.AccessToken)
	require.NotEmpty(t, creds.RefreshToken)
	require.Equal(t, "wss://server.example.com/ws/wrapper", creds.ServerEndpoint)

	_, err := s.Pairing().GetByCode(ctx, pc.Code)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestDeviceTokenRefresh(t *testing.T) {
	s := newTestStore(t, "dev_refresh")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	_, plain, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})

	h := NewDeviceHandlers(s, WithServerEndpoint("wss://server.example.com/ws/wrapper"))
	body, _ := json.Marshal(map[string]string{"refresh_token": plain})
	req := httptest.NewRequest("POST", "/device/token/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.TokenRefresh(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestDeviceTokenRefreshRevoked(t *testing.T) {
	s := newTestStore(t, "dev_refresh_rev")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	w, plain, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	_ = s.Wrappers().Revoke(ctx, w.ID)

	h := NewDeviceHandlers(s, WithServerEndpoint("wss://server.example.com/ws/wrapper"))
	body, _ := json.Marshal(map[string]string{"refresh_token": plain})
	req := httptest.NewRequest("POST", "/device/token/refresh", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.TokenRefresh(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}
