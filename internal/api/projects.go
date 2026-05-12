package api

import (
	"context"
	"net/http"

	"github.com/jleal52/claude-switch/internal/store"
)

type ProjectsHandlers struct{ store *store.Store }

func NewProjectsHandlers(s *store.Store) *ProjectsHandlers { return &ProjectsHandlers{store: s} }

type projectJSON struct {
	ID              string `json:"id"`
	WrapperID       string `json:"wrapper_id"`
	Slug            string `json:"slug"`
	Cwd             string `json:"cwd"`
	Name            string `json:"name"`
	SessionCount    int    `json:"session_count"`
	FirstActivityAt string `json:"first_activity_at"`
	LastActivityAt  string `json:"last_activity_at"`
}

// List returns every project owned by the authenticated user. Optional
// query param `wrapper_id` scopes the result to a single wrapper.
func (h *ProjectsHandlers) List(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	var (
		rows []*store.Project
		err  error
	)
	if wid := r.URL.Query().Get("wrapper_id"); wid != "" {
		if !userOwnsWrapper(r.Context(), h.store, u.ID, wid) {
			http.NotFound(w, r)
			return
		}
		rows, err = h.store.Projects().ListByWrapper(r.Context(), wid)
	} else {
		rows, err = h.store.Projects().ListByUser(r.Context(), u.ID)
	}
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	out := make([]projectJSON, 0, len(rows))
	for _, p := range rows {
		out = append(out, projectJSON{
			ID: p.ID, WrapperID: p.WrapperID, Slug: p.Slug, Cwd: p.Cwd, Name: p.Name,
			SessionCount:    p.SessionCount,
			FirstActivityAt: p.FirstActivityAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			LastActivityAt:  p.LastActivityAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// userOwnsWrapper returns true if userID owns wrapperID. Returning false on
// any error is intentional — the caller maps to 404 either way.
func userOwnsWrapper(ctx context.Context, s *store.Store, userID, wrapperID string) bool {
	rows, err := s.Wrappers().ListByUser(ctx, userID)
	if err != nil {
		return false
	}
	for _, w := range rows {
		if w.ID == wrapperID {
			return true
		}
	}
	return false
}
