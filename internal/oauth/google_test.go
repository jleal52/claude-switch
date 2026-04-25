package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGoogleExchangeReturnsProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-g", "token_type": "Bearer", "expires_in": 3600,
			})
		case "/oauth2/v2/userinfo":
			require.Equal(t, "Bearer tok-g", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "g-77",
				"email":   "alan@example.com",
				"name":    "Alan T",
				"picture": "https://gpic/x.jpg",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	g := NewGoogle(GoogleConfig{
		ClientID: "id", ClientSecret: "s",
		AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token",
		UserInfoURL: srv.URL + "/oauth2/v2/userinfo",
	})

	prof, err := g.Exchange(context.Background(), "code")
	require.NoError(t, err)
	require.Equal(t, store.OAuthProfile{
		Provider: "google", Subject: "g-77", Email: "alan@example.com",
		Name: "Alan T", AvatarURL: "https://gpic/x.jpg",
	}, *prof)
}
