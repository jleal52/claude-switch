// Package oauth implements provider-specific OAuth2 callbacks. Each
// provider type implements Provider; the API layer wires them to
// /auth/<name>/login and /auth/<name>/callback.
package oauth

import (
	"context"

	"github.com/jleal52/claude-switch/internal/store"
)

// Provider abstracts a single OAuth2 provider.
type Provider interface {
	// Name returns the lowercase provider identifier ("github", "google").
	Name() string
	// AuthCodeURL returns the URL the browser should be redirected to.
	AuthCodeURL(state string) string
	// Exchange completes the callback: code -> access token -> user profile.
	Exchange(ctx context.Context, code string) (*store.OAuthProfile, error)
}
