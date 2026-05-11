package auth

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// RefresherConfig configures a background access-token refresher.
type RefresherConfig struct {
	// ServerBase is the HTTP base URL of the server (e.g. https://host).
	ServerBase string
	// Margin: refresh when the access token expires within this duration.
	// Defaults to 10 min if zero.
	Margin time.Duration
	// Interval: how often to re-check expiry. Defaults to 1 min if zero.
	Interval time.Duration
	// HTTPClient is used for the refresh POST. Defaults to a 15s-timeout client.
	HTTPClient *http.Client
	// OnSave, if non-nil, is invoked with the refreshed credentials. The
	// wrapper supplies one that persists them to disk.
	OnSave func(*Credentials) error
}

// Refresher keeps an access token current by periodically calling /device/token/refresh
// before the token expires. The current Credentials snapshot can be read at
// any time via Snapshot or Token; Set replaces the initial value.
type Refresher struct {
	cfg RefresherConfig

	mu      sync.RWMutex
	current *Credentials
}

func NewRefresher(cfg RefresherConfig) *Refresher {
	if cfg.Margin == 0 {
		cfg.Margin = 10 * time.Minute
	}
	if cfg.Interval == 0 {
		cfg.Interval = time.Minute
	}
	return &Refresher{cfg: cfg}
}

// Set replaces the current credentials. Call once after Load() at startup.
func (r *Refresher) Set(c *Credentials) {
	r.mu.Lock()
	r.current = c
	r.mu.Unlock()
}

// Snapshot returns a copy-by-pointer of the current credentials.
func (r *Refresher) Snapshot() *Credentials {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// Token returns the current access token. Suitable as ws.Config.TokenSource.
func (r *Refresher) Token() string {
	c := r.Snapshot()
	if c == nil {
		return ""
	}
	return c.AccessToken
}

// Run ticks on Interval and refreshes whenever the current token expires
// within Margin. Returns when ctx is cancelled.
func (r *Refresher) Run(ctx context.Context) error {
	t := time.NewTicker(r.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			_ = r.refreshIfNear(ctx)
		}
	}
}

// RefreshNow forces a refresh regardless of expiry. Useful at startup and
// for reactive recovery after auth failures.
func (r *Refresher) RefreshNow(ctx context.Context) error {
	return r.doRefresh(ctx)
}

func (r *Refresher) refreshIfNear(ctx context.Context) error {
	cur := r.Snapshot()
	if cur == nil {
		return nil
	}
	if time.Now().Add(r.cfg.Margin).Before(cur.ExpiresAt) {
		return nil
	}
	return r.doRefresh(ctx)
}

func (r *Refresher) doRefresh(ctx context.Context) error {
	cur := r.Snapshot()
	if cur == nil {
		return nil
	}
	refreshed, err := Refresh(ctx, r.cfg.ServerBase, cur.RefreshToken, r.cfg.HTTPClient)
	if err != nil {
		return err
	}
	// Server's refresh response omits ServerEndpoint; preserve the one we had.
	if refreshed.ServerEndpoint == "" {
		refreshed.ServerEndpoint = cur.ServerEndpoint
	}
	r.mu.Lock()
	r.current = refreshed
	r.mu.Unlock()
	if r.cfg.OnSave != nil {
		return r.cfg.OnSave(refreshed)
	}
	return nil
}
