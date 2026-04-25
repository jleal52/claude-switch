package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRefreshHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/device/token/refresh", r.URL.Path)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "ref-xyz", body["refresh_token"])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "tok-new",
			"refresh_token": "ref-new",
			"expires_at":    time.Now().Add(time.Hour).Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	got, err := Refresh(context.Background(), srv.URL, "ref-xyz", nil)
	require.NoError(t, err)
	require.Equal(t, "tok-new", got.AccessToken)
	require.Equal(t, "ref-new", got.RefreshToken)
}

func TestRefreshRevokedReturnsErrRevoked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"revoked"}`))
	}))
	defer srv.Close()

	_, err := Refresh(context.Background(), srv.URL, "ref-xyz", nil)
	require.ErrorIs(t, err, ErrRevoked)
}
