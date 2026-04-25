package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jleal52/claude-switch/internal/store"
)

type MessagesHandlers struct{ store *store.Store }

func NewMessagesHandlers(s *store.Store) *MessagesHandlers { return &MessagesHandlers{store: s} }

type messageJSON struct {
	TS    string `json:"ts"`
	Entry string `json:"entry"`
}

const messagesPageMax = 1000

func (h *MessagesHandlers) List(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	id := r.PathValue("id")
	row, err := h.store.Sessions().GetByID(r.Context(), id)
	if err != nil || row.UserID != u.ID {
		http.NotFound(w, r)
		return
	}

	var since time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "bad since", http.StatusBadRequest)
			return
		}
		since = t
	}
	limit := messagesPageMax
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > messagesPageMax {
			http.Error(w, "bad limit", http.StatusBadRequest)
			return
		}
		limit = n
	}

	rows, err := h.store.Messages().List(r.Context(), id, since, limit)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	out := make([]messageJSON, 0, len(rows))
	for _, m := range rows {
		out = append(out, messageJSON{
			TS:    m.TS.Format(time.RFC3339Nano),
			Entry: m.Entry,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
