package api

import (
	"errors"
	"net/http"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/store"
)

// WrapperPresence reports whether a wrapper currently has a live WebSocket
// connection. The hub satisfies this; tests substitute a fake.
type WrapperPresence interface {
	WrapperOnline(id string) bool
}

type WrappersHandlers struct {
	store    *store.Store
	presence WrapperPresence
}

func NewWrappersHandlers(s *store.Store, p WrapperPresence) *WrappersHandlers {
	return &WrappersHandlers{store: s, presence: p}
}

// compile-time check that *hub.Hub satisfies WrapperPresence.
var _ WrapperPresence = (*hub.Hub)(nil)

type wrapperJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Version    string `json:"version"`
	PairedAt   string `json:"paired_at"`
	LastSeenAt string `json:"last_seen_at"`
	Revoked    bool   `json:"revoked"`
	Online     bool   `json:"online"`
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
			Online:     h.presence != nil && row.RevokedAt == nil && h.presence.WrapperOnline(row.ID),
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
	// Refuse to revoke a wrapper that is currently connected — the row's
	// `revoked_at` would kick in on the next reconnect, but the live WS
	// session would keep streaming until then. Make the caller wait for
	// the wrapper to go offline (or shut it down themselves) so the
	// portal state matches reality after the click.
	if h.presence != nil && h.presence.WrapperOnline(id) {
		http.Error(w, "wrapper online; stop it before deleting", http.StatusConflict)
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
