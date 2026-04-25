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

func TestGitHubExchangeReturnsProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			require.Equal(t, "the-code", r.FormValue("code"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-1",
				"token_type":   "bearer",
				"scope":        "user:email",
			})
		case "/user":
			require.Equal(t, "Bearer tok-1", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         99,
				"login":      "ada",
				"name":       "Ada Lovelace",
				"avatar_url": "https://avatars/x.png",
				"email":      "ada@example.com",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	gh := NewGitHub(GitHubConfig{
		ClientID: "id", ClientSecret: "secret",
		AuthURL:  srv.URL + "/login/oauth/authorize",
		TokenURL: srv.URL + "/login/oauth/access_token",
		APIBase:  srv.URL,
	})

	prof, err := gh.Exchange(context.Background(), "the-code")
	require.NoError(t, err)
	require.Equal(t, store.OAuthProfile{
		Provider: "github", Subject: "99", Email: "ada@example.com",
		Name: "Ada Lovelace", AvatarURL: "https://avatars/x.png",
	}, *prof)
}
