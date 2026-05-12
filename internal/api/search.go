package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/oklog/ulid/v2"

	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/searchhub"
	"github.com/jleal52/claude-switch/internal/store"
)

type SearchHandlers struct {
	store      *store.Store
	dispatcher *searchhub.Hub
}

func NewSearchHandlers(s *store.Store, d *searchhub.Hub) *SearchHandlers {
	return &SearchHandlers{store: s, dispatcher: d}
}

type searchRequestJSON struct {
	Query           string   `json:"query"`
	ProjectID       string   `json:"project_id,omitempty"`
	WrapperIDs      []string `json:"wrapper_ids,omitempty"`
	TranscriptIDs   []string `json:"transcript_ids,omitempty"`
	MaxResults      int      `json:"max_results,omitempty"`
	CaseInsensitive bool     `json:"case_insensitive,omitempty"`
}

type searchResponseJSON struct {
	Matches   []proto.SearchMatch                   `json:"matches"`
	ByWrapper map[string]searchhub.WrapperStatus    `json:"by_wrapper"`
}

// Search fans out to the user's online wrappers and returns aggregated
// matches. CSRF is enforced by the middleware applied at the router level.
func (h *SearchHandlers) Search(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())

	var in searchRequestJSON
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if in.Query == "" {
		http.Error(w, "query required", http.StatusBadRequest)
		return
	}
	if in.MaxResults <= 0 || in.MaxResults > 500 {
		in.MaxResults = 100
	}

	wrappers, err := h.store.Wrappers().ListByUser(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	// Resolve which wrappers to fan out to.
	ownedSet := map[string]bool{}
	for _, wr := range wrappers {
		if wr.RevokedAt == nil {
			ownedSet[wr.ID] = true
		}
	}
	var target []string
	if len(in.WrapperIDs) > 0 {
		for _, wid := range in.WrapperIDs {
			if ownedSet[wid] {
				target = append(target, wid)
			}
		}
	} else {
		for wid := range ownedSet {
			target = append(target, wid)
		}
	}

	resp, err := h.dispatcher.Dispatch(r.Context(), searchhub.Query{
		RequestID:  ulid.Make().String(),
		WrapperIDs: target,
		Body: proto.SearchRequest{
			Query:           in.Query,
			ProjectID:       in.ProjectID,
			TranscriptIDs:   in.TranscriptIDs,
			MaxResults:      in.MaxResults,
			SnippetChars:    120,
			CaseInsensitive: in.CaseInsensitive,
		},
	})
	if err != nil {
		http.Error(w, "dispatch: "+err.Error(), http.StatusInternalServerError)
		return
	}
	resp.Matches = filterDeletedMatches(r.Context(), h.store, u.ID, resp.Matches)
	if resp.Matches == nil {
		resp.Matches = []proto.SearchMatch{}
	}
	writeJSON(w, http.StatusOK, searchResponseJSON{
		Matches:   resp.Matches,
		ByWrapper: resp.ByWrapper,
	})
}

// filterDeletedMatches drops matches that belong to soft-deleted
// transcripts. The wrapper doesn't know about portal-level soft deletes,
// so the server post-filters using the transcripts collection's
// deleted_at flag.
func filterDeletedMatches(ctx context.Context, s *store.Store, userID string, matches []proto.SearchMatch) []proto.SearchMatch {
	if len(matches) == 0 {
		return matches
	}
	// Build a unique set of jsonl_uuids referenced by the matches and ask
	// the store which ones are still live.
	uuids := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, m := range matches {
		if !seen[m.TranscriptID] {
			seen[m.TranscriptID] = true
			uuids = append(uuids, m.TranscriptID)
		}
	}
	liveUUIDs, err := s.Transcripts().LiveUUIDsForUser(ctx, userID, uuids)
	if err != nil {
		// On failure, fail open (return all matches) rather than dropping
		// every result. The user can still see hits; the worst case is a
		// deleted transcript briefly surfacing.
		return matches
	}
	live := make(map[string]bool, len(liveUUIDs))
	for _, u := range liveUUIDs {
		live[u] = true
	}
	out := matches[:0]
	for _, m := range matches {
		if live[m.TranscriptID] {
			out = append(out, m)
		}
	}
	return out
}
