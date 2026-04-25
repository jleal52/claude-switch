package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jleal52/claude-switch/internal/api"
	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/oauth"
	"github.com/jleal52/claude-switch/internal/store"
)

const serverVersion = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	mongoURI := getenv("MONGO_URI", "mongodb://localhost:27017")
	mongoDB := getenv("MONGO_DB", "claude_switch")
	baseURL := mustenv("SERVER_BASE_URL")
	bindAddr := getenv("BIND_ADDR", ":8080")
	logLevel := getenv("LOG_LEVEL", "info")
	if os.Getenv("SESSION_SECRET") == "" {
		return fmt.Errorf("SESSION_SECRET is required")
	}

	configureLogger(logLevel)
	slog.Info("claude-switch-server starting", "version", serverVersion, "bind", bindAddr)

	ctx, cancel := signalContext()
	defer cancel()

	s, err := store.New(ctx, mongoURI, mongoDB)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer s.Close(context.Background())

	h := hub.New()
	providers := buildProviders(baseURL)
	if len(providers) == 0 {
		return fmt.Errorf("no OAuth providers configured (set OAUTH_GITHUB_* or OAUTH_GOOGLE_*)")
	}

	wsServerEndpoint := strings.Replace(baseURL, "https://", "wss://", 1)
	wsServerEndpoint = strings.Replace(wsServerEndpoint, "http://", "ws://", 1) + "/ws/wrapper"

	router := api.NewRouter(api.RouterConfig{
		Store: s, Hub: h, Providers: providers,
		BaseURL: baseURL, Secure: strings.HasPrefix(baseURL, "https://"),
		ServerEndpoint: wsServerEndpoint,
	})

	srv := &http.Server{
		Addr: bindAddr, Handler: router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errc <- err
		}
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		return srv.Shutdown(shutdownCtx)
	}
}

func buildProviders(baseURL string) []oauth.Provider {
	var out []oauth.Provider
	if id, secret := os.Getenv("OAUTH_GITHUB_CLIENT_ID"), os.Getenv("OAUTH_GITHUB_CLIENT_SECRET"); id != "" && secret != "" {
		out = append(out, oauth.NewGitHub(oauth.GitHubConfig{
			ClientID: id, ClientSecret: secret,
			RedirectURL: baseURL + "/auth/github/callback",
		}))
	}
	if id, secret := os.Getenv("OAUTH_GOOGLE_CLIENT_ID"), os.Getenv("OAUTH_GOOGLE_CLIENT_SECRET"); id != "" && secret != "" {
		out = append(out, oauth.NewGoogle(oauth.GoogleConfig{
			ClientID: id, ClientSecret: secret,
			RedirectURL: baseURL + "/auth/google/callback",
		}))
	}
	return out
}

func configureLogger(level string) {
	lvl := slog.LevelInfo
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() { <-ch; cancel() }()
	return ctx, cancel
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustenv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fmt.Fprintln(os.Stderr, k+" is required")
		os.Exit(2)
	}
	return v
}
