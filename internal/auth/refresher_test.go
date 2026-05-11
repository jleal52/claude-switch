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

func TestRefresherRefreshesWhenWithinMargin(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/device/token/refresh", r.URL.Path)
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_at":    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	var saved atomic.Pointer[Credentials]
	rfr := NewRefresher(RefresherConfig{
		ServerBase: srv.URL,
		Margin:     time.Minute,
		Interval:   10 * time.Millisecond,
		OnSave:     func(c *Credentials) error { saved.Store(c); return nil },
	})
	rfr.Set(&Credentials{
		AccessToken:  "old",
		RefreshToken: "ref",
		ExpiresAt:    time.Now().Add(20 * time.Second), // within margin
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = rfr.Run(ctx)

	require.GreaterOrEqual(t, int(calls.Load()), 1, "expected at least one refresh call")
	require.Equal(t, "new-access", rfr.Token())
	require.NotNil(t, saved.Load())
	require.Equal(t, "new-access", saved.Load().AccessToken)
	require.Equal(t, "new-refresh", saved.Load().RefreshToken)
}

func TestRefresherSkipsWhenFarFromExpiry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	rfr := NewRefresher(RefresherConfig{
		ServerBase: srv.URL,
		Margin:     time.Minute,
		Interval:   10 * time.Millisecond,
	})
	rfr.Set(&Credentials{
		AccessToken:  "fresh",
		RefreshToken: "ref",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = rfr.Run(ctx)

	require.Equal(t, int32(0), calls.Load(), "expected no refresh calls while token is fresh")
	require.Equal(t, "fresh", rfr.Token())
}

func TestRefresherPreservesServerEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "a", "refresh_token": "r",
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			// note: server omits server_endpoint
		})
	}))
	defer srv.Close()

	rfr := NewRefresher(RefresherConfig{
		ServerBase: srv.URL,
		Margin:     time.Minute,
	})
	rfr.Set(&Credentials{
		AccessToken: "old", RefreshToken: "r",
		ServerEndpoint: "wss://orig.example/ws/wrapper",
		ExpiresAt:      time.Now().Add(10 * time.Second),
	})

	require.NoError(t, rfr.RefreshNow(context.Background()))
	got := rfr.Snapshot()
	require.Equal(t, "wss://orig.example/ws/wrapper", got.ServerEndpoint)
}
