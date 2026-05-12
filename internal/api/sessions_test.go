package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

type fakeDispatcher struct {
	mu       sync.Mutex
	opens    []hub.OpenSessionRequest
	closes   []string
	openErr  error
	closeErr error
}

func (f *fakeDispatcher) OpenSession(ctx context.Context, req hub.OpenSessionRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens = append(f.opens, req)
	return f.openErr
}
func (f *fakeDispatcher) CloseSession(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes = append(f.closes, id)
	return f.closeErr
}

func TestSessionsCreateInsertsRowAndDispatches(t *testing.T) {
	s := newTestStore(t, "sess_create")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)
	body, _ := json.Marshal(map[string]any{"wrapper_id": w.ID, "cwd": "/tmp", "account": "default"})
	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Create)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	require.NotEmpty(t, got.ID)
	require.Len(t, d.opens, 1)
	require.Equal(t, got.ID, d.opens[0].SessionID)
	require.Equal(t, w.ID, d.opens[0].WrapperID)
}

func TestSessionsCreateRejectsForeignWrapper(t *testing.T) {
	s := newTestStore(t, "sess_foreign")
	ctx := context.Background()
	owner, _ := s.Users().UpsertOAuth(ctx, fakeProfile("o"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("x"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "x", OS: "linux", Arch: "amd64"})
	sess, _ := s.AuthSessions().Create(ctx, owner.ID, time.Hour)

	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)
	body, _ := json.Marshal(map[string]any{"wrapper_id": w.ID, "cwd": "/tmp", "account": "default"})
	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Create)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Empty(t, d.opens)
}

func TestSessionsListReturnsOnlyOwn(t *testing.T) {
	s := newTestStore(t, "sess_list")
	ctx := context.Background()
	owner, _ := s.Users().UpsertOAuth(ctx, fakeProfile("o"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("x"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: owner.ID, Name: "x", OS: "linux", Arch: "amd64"})
	w2, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "x", OS: "linux", Arch: "amd64"})

	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "s1", UserID: owner.ID, WrapperID: w.ID, Cwd: "/", Account: "default"})
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "s2", UserID: other.ID, WrapperID: w2.ID, Cwd: "/", Account: "default"})

	sess, _ := s.AuthSessions().Create(ctx, owner.ID, time.Hour)
	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	require.Len(t, got, 1)
	require.Equal(t, "s1", got[0].ID)
}

func TestSessionsResumeStartsClaudeWithResumeFlag(t *testing.T) {
	s := newTestStore(t, "sess_resume_ok")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u-resume"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "w", OS: "linux", Arch: "amd64"})
	seedTranscripts(t, s, u.ID, w.ID, []string{"abc-uuid"})

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)

	body, _ := json.Marshal(map[string]any{"jsonl_uuid": "abc-uuid"})
	req := httptest.NewRequest("POST", "/api/sessions/resume", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Resume)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var got struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	require.NotEmpty(t, got.ID)
	require.Len(t, d.opens, 1)
	require.Equal(t, w.ID, d.opens[0].WrapperID)
	require.Equal(t, "/x", d.opens[0].Cwd)
	require.Equal(t, []string{"--resume", "abc-uuid"}, d.opens[0].Args)
}

func TestSessionsResumeForeignTranscriptReturns404(t *testing.T) {
	s := newTestStore(t, "sess_resume_foreign")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u-mine"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u-other"))
	wo, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "w", OS: "linux", Arch: "amd64"})
	seedTranscripts(t, s, other.ID, wo.ID, []string{"foreign-uuid"})

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)

	body, _ := json.Marshal(map[string]any{"jsonl_uuid": "foreign-uuid"})
	req := httptest.NewRequest("POST", "/api/sessions/resume", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Resume)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Empty(t, d.opens)
}

func TestSessionsResumeSoftDeletedReturns404(t *testing.T) {
	s := newTestStore(t, "sess_resume_deleted")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u-deleted"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "w", OS: "linux", Arch: "amd64"})
	seedTranscripts(t, s, u.ID, w.ID, []string{"gone-uuid"})
	mine, _ := s.Transcripts().ListByWrapper(ctx, w.ID, 10)
	require.NoError(t, s.Transcripts().SoftDelete(ctx, mine[0].ID))

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)

	body, _ := json.Marshal(map[string]any{"jsonl_uuid": "gone-uuid"})
	req := httptest.NewRequest("POST", "/api/sessions/resume", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Resume)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Empty(t, d.opens)
}

func TestSessionsDeleteOwnDispatches(t *testing.T) {
	s := newTestStore(t, "sess_del")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "ss", UserID: u.ID, WrapperID: w.ID, Cwd: "/", Account: "default"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)
	req := httptest.NewRequest("DELETE", "/api/sessions/ss", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	req.SetPathValue("id", "ss")
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Delete)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Equal(t, []string{"ss"}, d.closes)
}
