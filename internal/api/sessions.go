package api

import (
	"encoding/json"
	"net/http"

	"github.com/oklog/ulid/v2"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/store"
)

type SessionsHandlers struct {
	store      *store.Store
	dispatcher hub.Dispatcher
}

func NewSessionsHandlers(s *store.Store, d hub.Dispatcher) *SessionsHandlers {
	return &SessionsHandlers{store: s, dispatcher: d}
}

type sessionJSON struct {
	ID         string `json:"id"`
	WrapperID  string `json:"wrapper_id"`
	JSONLUUID  string `json:"jsonl_uuid,omitempty"`
	Cwd        string `json:"cwd"`
	Account    string `json:"account"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	ExitedAt   string `json:"exited_at,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	ExitReason string `json:"exit_reason,omitempty"`
}

func (h *SessionsHandlers) List(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	statusFilter := r.URL.Query().Get("status")
	rows, err := h.store.Sessions().ListByUser(r.Context(), u.ID, statusFilter)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	out := make([]sessionJSON, 0, len(rows))
	for _, row := range rows {
		j := sessionJSON{
			ID: row.ID, WrapperID: row.WrapperID, JSONLUUID: row.JSONLUUID,
			Cwd: row.Cwd, Account: row.Account, Status: row.Status,
			CreatedAt:  row.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			ExitCode:   row.ExitCode,
			ExitReason: row.ExitReason,
		}
		if row.ExitedAt != nil {
			j.ExitedAt = row.ExitedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *SessionsHandlers) Create(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	var in struct {
		WrapperID string   `json:"wrapper_id"`
		Cwd       string   `json:"cwd"`
		Account   string   `json:"account"`
		Args      []string `json:"args,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if in.Account == "" {
		in.Account = "default"
	}

	// Verify ownership of wrapper.
	wrappers, err := h.store.Wrappers().ListByUser(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	owns := false
	for _, wr := range wrappers {
		if wr.ID == in.WrapperID && wr.RevokedAt == nil {
			owns = true
			break
		}
	}
	if !owns {
		http.NotFound(w, r)
		return
	}

	sid := ulid.Make().String()
	row, err := h.store.Sessions().Create(r.Context(), store.SessionCreate{
		ID: sid, UserID: u.ID, WrapperID: in.WrapperID, Cwd: in.Cwd, Account: in.Account,
	})
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	if err := h.dispatcher.OpenSession(r.Context(), hub.OpenSessionRequest{
		WrapperID: in.WrapperID, SessionID: sid,
		Cwd: in.Cwd, Account: in.Account, Args: in.Args,
	}); err != nil {
		_ = h.store.Sessions().MarkExited(r.Context(), sid, -1, "spawn_failed", err.Error())
		http.Error(w, "dispatcher: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, sessionJSON{
		ID: row.ID, WrapperID: row.WrapperID, Cwd: row.Cwd, Account: row.Account,
		Status: row.Status, CreatedAt: row.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h *SessionsHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	row, err := h.store.Sessions().GetByID(r.Context(), id)
	if err != nil || row.UserID != u.ID {
		http.NotFound(w, r)
		return
	}
	if err := h.dispatcher.CloseSession(r.Context(), id); err != nil {
		http.Error(w, "dispatcher", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
