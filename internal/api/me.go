package api

import (
	"encoding/json"
	"net/http"

	"github.com/jleal52/claude-switch/internal/store"
)

type MeConfig struct {
	Store               *store.Store
	ProvidersConfigured []string // ["github"], ["google"], or both
}

type MeHandlers struct{ cfg MeConfig }

func NewMeHandlers(cfg MeConfig) *MeHandlers { return &MeHandlers{cfg: cfg} }

type meResponse struct {
	User struct {
		ID              string `json:"id"`
		Email           string `json:"email,omitempty"`
		Name            string `json:"name,omitempty"`
		AvatarURL       string `json:"avatar_url,omitempty"`
		KeepTranscripts bool   `json:"keep_transcripts"`
	} `json:"user"`
	ProvidersConfigured []string `json:"providers_configured"`
}

// Get is GET /api/me.
func (h *MeHandlers) Get(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	resp := meResponse{ProvidersConfigured: h.cfg.ProvidersConfigured}
	resp.User.ID = u.ID
	resp.User.Email = u.Email
	resp.User.Name = u.Name
	resp.User.AvatarURL = u.AvatarURL
	resp.User.KeepTranscripts = u.KeepTranscripts
	writeJSON(w, http.StatusOK, resp)
}

// UpdateSettings is POST /api/me/settings.
func (h *MeHandlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		KeepTranscripts *bool `json:"keep_transcripts,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	u := MustUser(r.Context())
	if body.KeepTranscripts != nil {
		if err := h.cfg.Store.Users().SetKeepTranscripts(r.Context(), u.ID, *body.KeepTranscripts); err != nil {
			http.Error(w, "store", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeJSON marshals v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
