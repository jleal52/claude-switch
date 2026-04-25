package api

import (
	"net/http"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/oauth"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/jleal52/claude-switch/internal/webfs"
	"github.com/jleal52/claude-switch/internal/wsbrowser"
	"github.com/jleal52/claude-switch/internal/wswrapper"
)

type RouterConfig struct {
	Store          *store.Store
	Hub            *hub.Hub
	Providers      []oauth.Provider
	BaseURL        string
	Secure         bool
	ServerEndpoint string // wss URL the wrapper should connect to
}

func NewRouter(cfg RouterConfig) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	auth := NewAuthHandlers(AuthConfig{
		Store: cfg.Store, Providers: cfg.Providers,
		BaseURL: cfg.BaseURL, Secure: cfg.Secure,
	})
	mux.HandleFunc("GET /auth/{provider}/login", auth.Login)
	mux.HandleFunc("GET /auth/{provider}/callback", auth.Callback)

	device := NewDeviceHandlers(cfg.Store, WithServerEndpoint(cfg.ServerEndpoint))
	mux.HandleFunc("POST /device/pair/start", device.PairStart)
	mux.HandleFunc("GET /device/pair/poll", device.PairPoll)
	mux.HandleFunc("POST /device/token/refresh", device.TokenRefresh)

	mw := NewAuthMiddleware(cfg.Store)
	me := NewMeHandlers(MeConfig{Store: cfg.Store, ProvidersConfigured: providerNames(cfg.Providers)})
	wrappers := NewWrappersHandlers(cfg.Store)
	pair := NewPairHandlers(cfg.Store)
	sessions := NewSessionsHandlers(cfg.Store, cfg.Hub)
	messages := NewMessagesHandlers(cfg.Store)

	mux.Handle("GET /api/me", mw.Require(http.HandlerFunc(me.Get)))
	mux.Handle("POST /api/me/settings", mw.Require(http.HandlerFunc(me.UpdateSettings)))
	mux.Handle("POST /api/auth/logout", mw.Require(http.HandlerFunc(auth.Logout)))
	mux.Handle("GET /api/wrappers", mw.Require(http.HandlerFunc(wrappers.List)))
	mux.Handle("DELETE /api/wrappers/{id}", mw.Require(http.HandlerFunc(wrappers.Delete)))
	mux.Handle("POST /api/pair/redeem", mw.Require(http.HandlerFunc(pair.Redeem)))
	mux.Handle("GET /api/sessions", mw.Require(http.HandlerFunc(sessions.List)))
	mux.Handle("POST /api/sessions", mw.Require(http.HandlerFunc(sessions.Create)))
	mux.Handle("DELETE /api/sessions/{id}", mw.Require(http.HandlerFunc(sessions.Delete)))
	mux.Handle("GET /api/sessions/{id}/messages", mw.Require(http.HandlerFunc(messages.List)))

	mux.Handle("/ws/wrapper", wswrapper.NewHandler(cfg.Store, cfg.Hub))
	mux.Handle("/ws/sessions/{id}", wsbrowser.NewHandler(cfg.Store, cfg.Hub))

	mux.Handle("/", webfs.Handler())

	return mux
}

func providerNames(ps []oauth.Provider) []string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name())
	}
	return names
}
