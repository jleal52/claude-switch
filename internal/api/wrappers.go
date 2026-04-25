package api

import (
	"errors"
	"net/http"

	"github.com/jleal52/claude-switch/internal/store"
)

type WrappersHandlers struct{ store *store.Store }

func NewWrappersHandlers(s *store.Store) *WrappersHandlers { return &WrappersHandlers{store: s} }

type wrapperJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Version    string `json:"version"`
	PairedAt   string `json:"paired_at"`
	LastSeenAt string `json:"last_seen_at"`
	Revoked    bool   `json:"revoked"`
}

func (h *WrappersHandlers) List(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	rows, err := h.store.Wrappers().ListByUser(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	out := make([]wrapperJSON, 0, len(rows))
	for _, row := range rows {
		j := wrapperJSON{
			ID: row.ID, Name: row.Name, OS: row.OS, Arch: row.Arch, Version: row.Version,
			PairedAt:   row.PairedAt.Format("2006-01-02T15:04:05Z07:00"),
			LastSeenAt: row.LastSeenAt.Format("2006-01-02T15:04:05Z07:00"),
			Revoked:    row.RevokedAt != nil,
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *WrappersHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	rows, err := h.store.Wrappers().ListByUser(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	owns := false
	for _, row := range rows {
		if row.ID == id {
			owns = true
			break
		}
	}
	if !owns {
		http.NotFound(w, r)
		return
	}
	if err := h.store.Wrappers().Revoke(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
