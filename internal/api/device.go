package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jleal52/claude-switch/internal/store"
)

type DeviceHandlers struct {
	store          *store.Store
	serverEndpoint string
}

type DeviceOption func(*DeviceHandlers)

func WithServerEndpoint(url string) DeviceOption {
	return func(h *DeviceHandlers) { h.serverEndpoint = url }
}

func NewDeviceHandlers(s *store.Store, opts ...DeviceOption) *DeviceHandlers {
	h := &DeviceHandlers{store: s}
	for _, o := range opts {
		o(h)
	}
	return h
}

const pairTTL = 10 * time.Minute
const accessTokenTTL = 1 * time.Hour

func (h *DeviceHandlers) PairStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	pc, err := h.store.Pairing().Create(r.Context(), store.WrapperDescriptor{
		Name: in.Name, OS: in.OS, Arch: in.Arch, Version: in.Version,
	}, pairTTL)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":       pc.Code,
		"poll_url":   "/device/pair/poll?c=" + pc.Code,
		"expires_in": int(pairTTL.Seconds()),
	})
}

func (h *DeviceHandlers) PairPoll(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("c")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	pc, err := h.store.Pairing().GetByCode(r.Context(), code)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch pc.Status {
	case "pending":
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"pending"}`))
		return
	case "denied":
		http.Error(w, `{"status":"denied"}`, http.StatusForbidden)
		_ = h.store.Pairing().Delete(r.Context(), code)
		return
	case "approved":
		wRow, refresh, err := h.store.Wrappers().Create(r.Context(), store.WrapperCreate{
			UserID: pc.UserID, Name: pc.Wrapper.Name,
			OS: pc.Wrapper.OS, Arch: pc.Wrapper.Arch, Version: pc.Wrapper.Version,
		})
		if err != nil {
			http.Error(w, "store", http.StatusInternalServerError)
			return
		}
		access, expiresAt, err := h.store.WrapperTokens().Issue(r.Context(), wRow.ID, pc.UserID, accessTokenTTL)
		if err != nil {
			http.Error(w, "store", http.StatusInternalServerError)
			return
		}
		_ = h.store.Pairing().Delete(r.Context(), code)
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":    access,
			"refresh_token":   refresh,
			"expires_at":      expiresAt.Format(time.RFC3339),
			"server_endpoint": h.serverEndpoint,
		})
	default:
		http.Error(w, "unknown status", http.StatusInternalServerError)
	}
}

func (h *DeviceHandlers) TokenRefresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	wRow, err := h.store.Wrappers().VerifyRefreshToken(r.Context(), in.RefreshToken)
	if err != nil {
		if errors.Is(err, store.ErrRevoked) {
			http.Error(w, `{"error":"revoked"}`, http.StatusUnauthorized)
			return
		}
		http.Error(w, "invalid refresh token", http.StatusUnauthorized)
		return
	}
	access, expiresAt, err := h.store.WrapperTokens().Issue(r.Context(), wRow.ID, wRow.UserID, accessTokenTTL)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"refresh_token": in.RefreshToken,
		"expires_at":    expiresAt.Format(time.RFC3339),
	})
}
