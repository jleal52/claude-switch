package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jleal52/claude-switch/internal/store"
)

type PairHandlers struct{ store *store.Store }

func NewPairHandlers(s *store.Store) *PairHandlers { return &PairHandlers{store: s} }

func (h *PairHandlers) Redeem(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	var in struct {
		Code string `json:"code"`
		Deny bool   `json:"deny"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	pc, err := h.store.Pairing().GetByCode(r.Context(), in.Code)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if in.Deny {
		_ = h.store.Pairing().Delete(r.Context(), pc.Code)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.store.Pairing().Approve(r.Context(), pc.Code, u.ID); err != nil {
		if errors.Is(err, store.ErrAlreadyApproved) {
			http.Error(w, "already approved", http.StatusConflict)
			return
		}
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    pc.Wrapper.Name,
		"os":      pc.Wrapper.OS,
		"arch":    pc.Wrapper.Arch,
		"version": pc.Wrapper.Version,
	})
}
