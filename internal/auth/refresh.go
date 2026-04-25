package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var ErrRevoked = errors.New("auth: refresh token revoked")

// Refresh exchanges a refresh token for a new access+refresh pair.
// Returns ErrRevoked on HTTP 401 so the caller can delete credentials
// and prompt the user to re-pair.
func Refresh(ctx context.Context, serverBase, refreshToken string, hc *http.Client) (*Credentials, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverBase+"/device/token/refresh", bodyReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrRevoked
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("refresh: http %d", resp.StatusCode)
	}
	var c Credentials
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
