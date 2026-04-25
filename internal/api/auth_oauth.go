package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/jleal52/claude-switch/internal/oauth"
	"github.com/jleal52/claude-switch/internal/store"
)

const (
	stateCookieName   = "cs_oauth_state"
	sessionCookieName = "cs_session"
	authSessionTTL    = 30 * 24 * time.Hour
	stateTTL          = 10 * time.Minute
)

type AuthConfig struct {
	Store     *store.Store
	Providers []oauth.Provider
	BaseURL   string
	Secure    bool
}

type AuthHandlers struct {
	cfg       AuthConfig
	providers map[string]oauth.Provider
}

func NewAuthHandlers(cfg AuthConfig) *AuthHandlers {
	m := map[string]oauth.Provider{}
	for _, p := range cfg.Providers {
		m[p.Name()] = p
	}
	return &AuthHandlers{cfg: cfg, providers: m}
}

// Login handles GET /auth/{provider}/login.
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	name := pathSuffix(r.URL.Path, "/auth/", "/login")
	p, ok := h.providers[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	state, err := randomBase64URL(24)
	if err != nil {
		http.Error(w, "state", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(stateTTL.Seconds()),
	})
	http.Redirect(w, r, p.AuthCodeURL(state), http.StatusFound)
}

// Callback handles GET /auth/{provider}/callback?code=&state=.
func (h *AuthHandlers) Callback(w http.ResponseWriter, r *http.Request) {
	name := pathSuffix(r.URL.Path, "/auth/", "/callback")
	p, ok := h.providers[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	state := r.URL.Query().Get("state")
	c, err := r.Cookie(stateCookieName)
	if err != nil || c.Value == "" || c.Value != state {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Value: "", MaxAge: -1, Path: "/"})

	code := r.URL.Query().Get("code")
	prof, err := p.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "oauth exchange", http.StatusBadGateway)
		return
	}

	user, err := h.cfg.Store.Users().UpsertOAuth(r.Context(), *prof)
	if err != nil {
		http.Error(w, "user upsert", http.StatusInternalServerError)
		return
	}
	sess, err := h.cfg.Store.AuthSessions().Create(r.Context(), user.ID, authSessionTTL)
	if err != nil {
		http.Error(w, "session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(authSessionTTL.Seconds()),
	})
	csrf.Set(w, sess.CSRFToken, h.cfg.Secure)

	http.Redirect(w, r, "/", http.StatusFound)
}

// Logout invalidates the auth session row and clears cookies.
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		_ = h.cfg.Store.AuthSessions().Delete(r.Context(), c.Value)
	}
	for _, n := range []string{sessionCookieName, csrf.CookieName} {
		http.SetCookie(w, &http.Cookie{Name: n, Value: "", Path: "/", MaxAge: -1})
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathSuffix extracts middle of a path like /auth/X/login -> X.
func pathSuffix(path, prefix, suffix string) string {
	if len(path) < len(prefix)+len(suffix)+1 || path[:len(prefix)] != prefix {
		return ""
	}
	rest := path[len(prefix):]
	if len(rest) < len(suffix) || rest[len(rest)-len(suffix):] != suffix {
		return ""
	}
	return rest[:len(rest)-len(suffix)]
}

func randomBase64URL(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// silence imports not directly used here.
var _ = strings.HasPrefix
