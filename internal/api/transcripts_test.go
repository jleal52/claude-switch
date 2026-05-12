package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/store"
)

func seedTranscripts(t *testing.T, s *store.Store, userID, wrapperID string, uuids []string) {
	t.Helper()
	t0 := time.Now().UTC().Add(-24 * time.Hour)
	projects := []store.ProjectUpsert{{
		Slug: "-x", Cwd: "/x", Name: "x", SessionCount: len(uuids),
		FirstActivityAt: t0, LastActivityAt: time.Now().UTC(),
	}}
	transcripts := make([]store.TranscriptUpsert, 0, len(uuids))
	for i, u := range uuids {
		transcripts = append(transcripts, store.TranscriptUpsert{
			JSONLUUID: u, ProjectSlug: "-x", Path: "-x/" + u + ".jsonl",
			StartedAt:    t0.Add(time.Duration(i) * time.Hour),
			EndedAt:      t0.Add(time.Duration(i)*time.Hour + time.Minute),
			MessageCount: 3, Title: u + "-title", Bytes: 100,
		})
	}
	require.NoError(t, s.Transcripts().ReplaceForWrapper(context.Background(), userID, wrapperID, projects, transcripts))
}

func TestTranscriptsListRecentByUser(t *testing.T) {
	s := newTestStore(t, "api_tr_recent")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("tr-recent"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "w", OS: "linux", Arch: "amd64"})
	seedTranscripts(t, s, u.ID, w.ID, []string{"a", "b", "c"})

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	h := NewTranscriptsHandlers(s)
	req := httptest.NewRequest("GET", "/api/transcripts", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got, 3)
	// Sorted by started_at desc: "c" most recent (i=2), then "b", then "a".
	require.Equal(t, "c", got[0]["jsonl_uuid"])
}

func TestTranscriptsGetReturnsOwnOnly(t *testing.T) {
	s := newTestStore(t, "api_tr_get")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("tr-get1"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("tr-get2"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "mine", OS: "linux", Arch: "amd64"})
	wo, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "other", OS: "linux", Arch: "amd64"})
	seedTranscripts(t, s, u.ID, w.ID, []string{"mine-u"})
	seedTranscripts(t, s, other.ID, wo.ID, []string{"other-u"})

	owned, _ := s.Transcripts().ListByWrapper(ctx, w.ID, 10)
	require.Len(t, owned, 1)
	notOwned, _ := s.Transcripts().ListByWrapper(ctx, wo.ID, 10)

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	h := NewTranscriptsHandlers(s)

	// Mine: 200
	req := httptest.NewRequest("GET", "/api/transcripts/"+owned[0].ID, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.SetPathValue("id", owned[0].ID)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Get)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	// Other user's: 404
	req = httptest.NewRequest("GET", "/api/transcripts/"+notOwned[0].ID, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.SetPathValue("id", notOwned[0].ID)
	rr = httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Get)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code)
}
