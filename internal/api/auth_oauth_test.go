package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jleal52/claude-switch/internal/oauth"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

// fakeProvider returns a fixed profile on Exchange and a deterministic auth URL.
type fakeProvider struct {
	name    string
	profile store.OAuthProfile
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) AuthCodeURL(state string) string {
	return "https://oauth.test/auth?state=" + state
}
func (f *fakeProvider) Exchange(ctx context.Context, code string) (*store.OAuthProfile, error) {
	cp := f.profile
	return &cp, nil
}

func TestLoginRedirectsAndSetsStateCookie(t *testing.T) {
	s := newTestStore(t, "auth_login")
	prov := &fakeProvider{name: "github", profile: store.OAuthProfile{Provider: "github", Subject: "1"}}
	h := NewAuthHandlers(AuthConfig{
		Store: s, Providers: []oauth.Provider{prov},
		BaseURL: "https://server.example.com", Secure: true,
	})

	req := httptest.NewRequest("GET", "/auth/github/login", nil)
	rr := httptest.NewRecorder()
	h.Login(rr, req)

	require.Equal(t, http.StatusFound, rr.Code)
	loc := rr.Header().Get("Location")
	require.True(t, strings.HasPrefix(loc, "https://oauth.test/auth?state="))

	cookies := rr.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "cs_oauth_state" {
			stateCookie = c
		}
	}
	require.NotNil(t, stateCookie)
	require.NotEmpty(t, stateCookie.Value)
}

func TestCallbackUpsertsUserAndIssuesSession(t *testing.T) {
	s := newTestStore(t, "auth_cb")
	prov := &fakeProvider{name: "github", profile: store.OAuthProfile{
		Provider: "github", Subject: "42", Email: "u@x", Name: "U",
	}}
	h := NewAuthHandlers(AuthConfig{
		Store: s, Providers: []oauth.Provider{prov},
		BaseURL: "https://server.example.com", Secure: true,
	})

	loginReq := httptest.NewRequest("GET", "/auth/github/login", nil)
	loginResp := httptest.NewRecorder()
	h.Login(loginResp, loginReq)
	state := getCookieValue(loginResp.Result().Cookies(), "cs_oauth_state")

	cbReq := httptest.NewRequest("GET", "/auth/github/callback?code=ok&state="+state, nil)
	cbReq.AddCookie(&http.Cookie{Name: "cs_oauth_state", Value: state})
	cbResp := httptest.NewRecorder()
	h.Callback(cbResp, cbReq)

	require.Equal(t, http.StatusFound, cbResp.Code)
	require.Equal(t, "/", cbResp.Header().Get("Location"))

	cs := getCookieValue(cbResp.Result().Cookies(), "cs_session")
	require.NotEmpty(t, cs)
	csrf := getCookieValue(cbResp.Result().Cookies(), "cs_csrf")
	require.NotEmpty(t, csrf)

	users, _ := s.Users().UpsertOAuth(context.Background(), prov.profile)
	require.NotEmpty(t, users.ID)
}

func TestCallbackRejectsStateMismatch(t *testing.T) {
	s := newTestStore(t, "auth_cb_mismatch")
	prov := &fakeProvider{name: "github", profile: store.OAuthProfile{Provider: "github", Subject: "1"}}
	h := NewAuthHandlers(AuthConfig{Store: s, Providers: []oauth.Provider{prov}, BaseURL: "https://x", Secure: true})

	req := httptest.NewRequest("GET", "/auth/github/callback?code=ok&state=A", nil)
	req.AddCookie(&http.Cookie{Name: "cs_oauth_state", Value: "B"})
	rr := httptest.NewRecorder()
	h.Callback(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func getCookieValue(cookies []*http.Cookie, name string) string {
	for _, c := range cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}
