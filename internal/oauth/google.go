package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/jleal52/claude-switch/internal/store"
)

type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Override URLs (test injection). Empty values use real Google.
	AuthURL     string
	TokenURL    string
	UserInfoURL string
}

type Google struct {
	cfg         GoogleConfig
	oauthCfg    *oauth2.Config
	userInfoURL string
	tokenURL    string
	hc          *http.Client
}

func NewGoogle(cfg GoogleConfig) *Google {
	if cfg.AuthURL == "" {
		cfg.AuthURL = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://oauth2.googleapis.com/token"
	}
	if cfg.UserInfoURL == "" {
		cfg.UserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"
	}
	return &Google{
		cfg: cfg,
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     oauth2.Endpoint{AuthURL: cfg.AuthURL, TokenURL: cfg.TokenURL},
			Scopes:       []string{"openid", "email", "profile"},
		},
		userInfoURL: cfg.UserInfoURL,
		tokenURL:    cfg.TokenURL,
		hc:          &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *Google) Name() string { return "google" }

func (g *Google) AuthCodeURL(state string) string {
	return g.oauthCfg.AuthCodeURL(state)
}

// exchangeToken posts to the token URL and returns the access token.
// Google returns JSON; we parse it directly to avoid oauth2's form-encoding
// heuristic that misidentifies text/plain JSON responses in httptest servers.
func (g *Google) exchangeToken(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", g.cfg.ClientID)
	form.Set("client_secret", g.cfg.ClientSecret)
	if g.cfg.RedirectURL != "" {
		form.Set("redirect_uri", g.cfg.RedirectURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tj struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tj); err != nil {
		return "", fmt.Errorf("google token: cannot parse response: %w", err)
	}
	if tj.Error != "" {
		return "", fmt.Errorf("google token: %s: %s", tj.Error, tj.ErrorDesc)
	}
	if tj.AccessToken == "" {
		return "", fmt.Errorf("google exchange: server response missing access_token")
	}
	return tj.AccessToken, nil
}

func (g *Google) Exchange(ctx context.Context, code string) (*store.OAuthProfile, error) {
	accessToken, err := g.exchangeToken(ctx, code)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.userInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("google userinfo: http %d", resp.StatusCode)
	}

	var u struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	return &store.OAuthProfile{
		Provider:  "google",
		Subject:   u.ID,
		Email:     u.Email,
		Name:      u.Name,
		AvatarURL: u.Picture,
	}, nil
}
