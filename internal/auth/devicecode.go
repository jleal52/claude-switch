// Package auth implements the device-code pairing flow between the wrapper
// and the central server (spec section "Authentication — device-code flow").
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Credentials is what pairing returns and what subsequent runs persist.
type Credentials struct {
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"refresh_token"`
	ExpiresAt      time.Time `json:"expires_at"`
	ServerEndpoint string    `json:"server_endpoint"`
}

type PairConfig struct {
	ServerBase   string        // e.g. https://server.example.com
	WrapperName  string        // typically os.Hostname()
	OS, Arch     string        // runtime.GOOS, runtime.GOARCH
	Version      string        // wrapper version string
	PollInterval time.Duration // default 5s
	Announce     func(code string)
	HTTPClient   *http.Client
}

type startResp struct {
	Code      string `json:"code"`
	PollURL   string `json:"poll_url"`
	ExpiresIn int    `json:"expires_in"`
}

// Pair performs the device-code dance and returns the issued credentials.
func Pair(ctx context.Context, cfg PairConfig) (*Credentials, error) {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}

	// 1. Start.
	body, _ := json.Marshal(map[string]any{
		"name": cfg.WrapperName, "os": cfg.OS, "arch": cfg.Arch, "version": cfg.Version,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ServerBase+"/device/pair/start", bodyReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pair start: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("pair start: http %d", resp.StatusCode)
	}
	var s startResp
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	cfg.Announce(s.Code)

	pollURL := s.PollURL
	if !isAbsoluteURL(pollURL) {
		pollURL = cfg.ServerBase + pollURL
	}

	// 2. Poll.
	tick := time.NewTicker(cfg.PollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
		pr, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return nil, err
		}
		pres, err := cfg.HTTPClient.Do(pr)
		if err != nil {
			return nil, fmt.Errorf("pair poll: %w", err)
		}
		if pres.StatusCode == http.StatusAccepted {
			_, _ = io.Copy(io.Discard, pres.Body)
			pres.Body.Close()
			continue
		}
		if pres.StatusCode/100 != 2 {
			pres.Body.Close()
			return nil, fmt.Errorf("pair poll: http %d", pres.StatusCode)
		}
		var c Credentials
		if err := json.NewDecoder(pres.Body).Decode(&c); err != nil {
			pres.Body.Close()
			return nil, err
		}
		pres.Body.Close()
		return &c, nil
	}
}

func isAbsoluteURL(s string) bool {
	return len(s) >= 8 && (s[:7] == "http://" || s[:8] == "https://")
}

func bodyReader(b []byte) *bytesBody { return &bytesBody{b: b} }

type bytesBody struct{ b []byte }

func (r *bytesBody) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func (*bytesBody) Close() error { return nil }
