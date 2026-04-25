package wsbrowser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/store"
)

// fakeWrapper records what the hub asks it to send.
type fakeWrapper struct {
	frames []hub.OutboundFrame
}

func (f *fakeWrapper) Send(fr hub.OutboundFrame) error {
	f.frames = append(f.frames, fr)
	return nil
}
func (f *fakeWrapper) Close() {}

func TestBrowserSubscribeReceivesReplay(t *testing.T) {
	s := store.NewTestStore(t, "wsb_replay")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, store.OAuthProfile{Provider: "github", Subject: "u1"})
	wRow, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	sid := ulid.Make().String()
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: sid, UserID: u.ID, WrapperID: wRow.ID, Cwd: "/", Account: "default"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := hub.New()
	fw := &fakeWrapper{}
	h.RegisterWrapper(wRow.ID, fw)
	h.AssignSession(sid, wRow.ID)
	h.UpdateRing(sid, []byte("replayed-bytes"))

	mux := http.NewServeMux()
	mux.Handle("/ws/sessions/{id}", NewHandler(s, h))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):] + "/ws/sessions/" + sid + "?ct=" + sess.CSRFToken
	headers := http.Header{}
	headers.Set("Cookie", "cs_session="+sess.ID)
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer conn.CloseNow()

	mtyp, raw, err := conn.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, mtyp)
	tt, _, _, _ := proto.Decode(raw)
	require.Equal(t, "replay.start", tt)

	mtyp, raw, err = conn.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, websocket.MessageBinary, mtyp)

	mtyp, raw, err = conn.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, mtyp)
	tt, _, _, _ = proto.Decode(raw)
	require.Equal(t, "replay.end", tt)
}

func TestBrowserRejectsBadCSRF(t *testing.T) {
	s := store.NewTestStore(t, "wsb_csrf")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, store.OAuthProfile{Provider: "github", Subject: "u1"})
	wRow, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	sid := "s1"
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: sid, UserID: u.ID, WrapperID: wRow.ID, Cwd: "/", Account: "default"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	mux := http.NewServeMux()
	mux.Handle("/ws/sessions/{id}", NewHandler(s, hub.New()))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):] + "/ws/sessions/" + sid + "?ct=BAD"
	headers := http.Header{}
	headers.Set("Cookie", "cs_session="+sess.ID)
	_, resp, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}
