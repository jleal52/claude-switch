package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPairHappyPath(t *testing.T) {
	var polls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device/pair/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":       "ABCD-1234",
				"poll_url":   "/device/pair/poll?c=ABCD-1234",
				"expires_in": 300,
			})
		case "/device/pair/poll":
			n := atomic.AddInt32(&polls, 1)
			if n < 2 {
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(`{"status":"pending"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":    "tok-abc",
				"refresh_token":   "ref-xyz",
				"expires_at":      time.Now().Add(time.Hour).Format(time.RFC3339),
				"server_endpoint": "wss://example.com/ws",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	var announced string
	res, err := Pair(context.Background(), PairConfig{
		ServerBase:   srv.URL,
		PollInterval: 20 * time.Millisecond,
		Announce:     func(code string) { announced = code },
	})
	require.NoError(t, err)
	require.Equal(t, "ABCD-1234", announced)
	require.Equal(t, "tok-abc", res.AccessToken)
	require.Equal(t, "wss://example.com/ws", res.ServerEndpoint)
}

func TestPairTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device/pair/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "XXXX", "poll_url": "/device/pair/poll", "expires_in": 300,
			})
		case "/device/pair/poll":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"pending"}`))
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := Pair(ctx, PairConfig{
		ServerBase:   srv.URL,
		PollInterval: 10 * time.Millisecond,
		Announce:     func(string) {},
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
