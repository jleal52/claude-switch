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

func seedProject(t *testing.T, s *store.Store, userID, wrapperID, slug, cwd string) string {
	t.Helper()
	ids, err := s.Projects().UpsertMany(context.Background(), userID, wrapperID, []store.ProjectUpsert{{
		Slug: slug, Cwd: cwd, Name: cwd[1:], SessionCount: 1,
		FirstActivityAt: time.Now().UTC().Add(-time.Hour),
		LastActivityAt:  time.Now().UTC(),
	}})
	require.NoError(t, err)
	return ids[slug]
}

func TestProjectsListReturnsOnlyOwn(t *testing.T) {
	s := newTestStore(t, "api_proj_own")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("p-own"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("p-other"))
	wMine, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "m", OS: "linux", Arch: "amd64"})
	wOther, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "x", OS: "linux", Arch: "amd64"})
	seedProject(t, s, u.ID, wMine.ID, "-x", "/x")
	seedProject(t, s, other.ID, wOther.ID, "-y", "/y")

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	h := NewProjectsHandlers(s)
	req := httptest.NewRequest("GET", "/api/projects", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got, 1)
	require.Equal(t, "-x", got[0]["slug"])
}

func TestProjectsListWrapperFilter(t *testing.T) {
	s := newTestStore(t, "api_proj_wfilter")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("p-wfilter"))
	w1, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "w1", OS: "linux", Arch: "amd64"})
	w2, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "w2", OS: "linux", Arch: "amd64"})
	seedProject(t, s, u.ID, w1.ID, "-a", "/a")
	seedProject(t, s, u.ID, w2.ID, "-b", "/b")

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	h := NewProjectsHandlers(s)
	req := httptest.NewRequest("GET", "/api/projects?wrapper_id="+w1.ID, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)

	var got []map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got, 1)
	require.Equal(t, "-a", got[0]["slug"])
}

func TestProjectsListWrapperFilterRejectsOtherUserWrapper(t *testing.T) {
	s := newTestStore(t, "api_proj_xown")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("p-xown1"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("p-xown2"))
	wo, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "o", OS: "linux", Arch: "amd64"})

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	h := NewProjectsHandlers(s)
	req := httptest.NewRequest("GET", "/api/projects?wrapper_id="+wo.ID, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code)
}
