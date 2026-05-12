package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jleal52/claude-switch/internal/store"
)

type TranscriptsHandlers struct{ store *store.Store }

func NewTranscriptsHandlers(s *store.Store) *TranscriptsHandlers { return &TranscriptsHandlers{store: s} }

type transcriptJSON struct {
	ID           string `json:"id"`
	WrapperID    string `json:"wrapper_id"`
	ProjectID    string `json:"project_id"`
	JSONLUUID    string `json:"jsonl_uuid"`
	Path         string `json:"path"`
	StartedAt    string `json:"started_at"`
	EndedAt      string `json:"ended_at"`
	MessageCount int    `json:"message_count"`
	Title        string `json:"title"`
	Bytes        int64  `json:"bytes"`
}

// List returns transcripts owned by the authenticated user. Filters:
//   - project_id: scope to one project (ownership inferred from the project).
//   - wrapper_id: scope to one wrapper.
//   - limit:      cap result count (default 200, max 1000).
//
// Without filters: most recent N transcripts across all of the user's wrappers.
func (h *TranscriptsHandlers) List(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	limit := parseLimit(r.URL.Query().Get("limit"), 200, 1000)

	var (
		rows []*store.Transcript
		err  error
	)
	switch {
	case r.URL.Query().Get("project_id") != "":
		pid := r.URL.Query().Get("project_id")
		// Verify ownership: the project must belong to a wrapper of u.ID.
		owners, _ := h.store.Projects().ListByUser(r.Context(), u.ID)
		owned := false
		for _, p := range owners {
			if p.ID == pid {
				owned = true
				break
			}
		}
		if !owned {
			http.NotFound(w, r)
			return
		}
		rows, err = h.store.Transcripts().ListByProject(r.Context(), pid, limit)
	case r.URL.Query().Get("wrapper_id") != "":
		wid := r.URL.Query().Get("wrapper_id")
		if !userOwnsWrapper(r.Context(), h.store, u.ID, wid) {
			http.NotFound(w, r)
			return
		}
		rows, err = h.store.Transcripts().ListByWrapper(r.Context(), wid, limit)
	default:
		rows, err = h.store.Transcripts().ListRecentByUser(r.Context(), u.ID, limit)
	}
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, transcriptsToJSON(rows))
}

// Get returns a single transcript by id; 404 if not owned by the user.
func (h *TranscriptsHandlers) Get(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	t, err := h.store.Transcripts().GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	if t.UserID != u.ID {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, transcriptsToJSON([]*store.Transcript{t})[0])
}

// Delete soft-deletes a transcript. The row stays in the DB; future
// listings hide it and a subsequent wrapper full-snapshot does not
// resurrect it.
func (h *TranscriptsHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	t, err := h.store.Transcripts().GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	if t.UserID != u.ID {
		http.NotFound(w, r)
		return
	}
	if err := h.store.Transcripts().SoftDelete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func transcriptsToJSON(rows []*store.Transcript) []transcriptJSON {
	out := make([]transcriptJSON, 0, len(rows))
	for _, t := range rows {
		out = append(out, transcriptJSON{
			ID: t.ID, WrapperID: t.WrapperID, ProjectID: t.ProjectID,
			JSONLUUID: t.JSONLUUID, Path: t.Path,
			StartedAt:    t.StartedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			EndedAt:      t.EndedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			MessageCount: t.MessageCount, Title: t.Title, Bytes: t.Bytes,
		})
	}
	return out
}

func parseLimit(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
