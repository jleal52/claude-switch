package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/jleal52/claude-switch/internal/store"
)

type GitHubConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Override URLs (test injection). Empty values use real GitHub.
	AuthURL  string
	TokenURL string
	APIBase  string
}

type GitHub struct {
	cfg      GitHubConfig
	oauthCfg *oauth2.Config
	apiBase  string
	tokenURL string
	hc       *http.Client
}

func NewGitHub(cfg GitHubConfig) *GitHub {
	if cfg.AuthURL == "" {
		cfg.AuthURL = "https://github.com/login/oauth/authorize"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://github.com/login/oauth/access_token"
	}
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.github.com"
	}
	return &GitHub{
		cfg: cfg,
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     oauth2.Endpoint{AuthURL: cfg.AuthURL, TokenURL: cfg.TokenURL},
			Scopes:       []string{"read:user", "user:email"},
		},
		apiBase:  cfg.APIBase,
		tokenURL: cfg.TokenURL,
		hc:       &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *GitHub) Name() string { return "github" }

func (g *GitHub) AuthCodeURL(state string) string {
	return g.oauthCfg.AuthCodeURL(state)
}

// exchangeToken posts to the token URL and returns the access token.
// GitHub returns JSON; we parse it directly to avoid oauth2's form-encoding
// heuristic that misidentifies text/plain JSON responses.
func (g *GitHub) exchangeToken(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", g.cfg.ClientID)
	form.Set("client_secret", g.cfg.ClientSecret)
	form.Set("code", code)
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
		return "", fmt.Errorf("github token: cannot parse response: %w", err)
	}
	if tj.Error != "" {
		return "", fmt.Errorf("github token: %s: %s", tj.Error, tj.ErrorDesc)
	}
	if tj.AccessToken == "" {
		return "", fmt.Errorf("github exchange: server response missing access_token")
	}
	return tj.AccessToken, nil
}

func (g *GitHub) Exchange(ctx context.Context, code string) (*store.OAuthProfile, error) {
	accessToken, err := g.exchangeToken(ctx, code)
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, g.apiBase+"/user", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("github /user: http %d", resp.StatusCode)
	}
	var u struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		Email     string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	name := u.Name
	if name == "" {
		name = u.Login
	}
	return &store.OAuthProfile{
		Provider:  "github",
		Subject:   strconv.FormatInt(u.ID, 10),
		Email:     u.Email,
		Name:      name,
		AvatarURL: u.AvatarURL,
	}, nil
}
