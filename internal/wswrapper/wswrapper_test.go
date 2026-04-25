package wswrapper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/store"
)

func TestWrapperHelloRegistersAndReconciles(t *testing.T) {
	s := store.NewTestStore(t, "wsw_hello")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, store.OAuthProfile{Provider: "github", Subject: "u1"})
	wRow, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	access, _, _ := s.WrapperTokens().Issue(ctx, wRow.ID, u.ID, time.Hour)

	h := hub.New()
	srv := httptest.NewServer(NewHandler(s, h))
	defer srv.Close()
	wsURL := "ws" + srv.URL[len("http"):]

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+access)
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer conn.CloseNow()

	hello := proto.Hello{
		WrapperID: "x", OS: "linux", Arch: "amd64", Version: "0.1.0",
		Accounts: []string{"default"}, Capabilities: []string{"pty"},
	}
	raw, _ := proto.Encode(proto.TypeHello, "", hello)
	require.NoError(t, conn.Write(context.Background(), websocket.MessageText, raw))

	time.Sleep(100 * time.Millisecond)

	got, _ := s.Wrappers().ListByUser(ctx, u.ID)
	require.Len(t, got, 1)
	require.True(t, got[0].LastSeenAt.After(wRow.LastSeenAt))
}

func TestWrapperRejectsBadToken(t *testing.T) {
	s := store.NewTestStore(t, "wsw_bad")
	h := hub.New()
	srv := httptest.NewServer(NewHandler(s, h))
	defer srv.Close()
	wsURL := "ws" + srv.URL[len("http"):]

	headers := http.Header{}
	headers.Set("Authorization", "Bearer not-a-token")
	_, resp, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestWrapperRelaysSessionStartedToBrowsers(t *testing.T) {
	s := store.NewTestStore(t, "wsw_started")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, store.OAuthProfile{Provider: "github", Subject: "u1"})
	wRow, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "s1", UserID: u.ID, WrapperID: wRow.ID, Cwd: "/", Account: "default"})

	access, _, _ := s.WrapperTokens().Issue(ctx, wRow.ID, u.ID, time.Hour)
	h := hub.New()
	srv := httptest.NewServer(NewHandler(s, h))
	defer srv.Close()
	wsURL := "ws" + srv.URL[len("http"):]

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+access)
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer conn.CloseNow()

	hello := proto.Hello{
		WrapperID: "x", OS: "linux", Arch: "amd64", Version: "0.1.0",
		Accounts: []string{"default"}, Capabilities: []string{"pty"},
	}
	raw, _ := proto.Encode(proto.TypeHello, "", hello)
	_ = conn.Write(context.Background(), websocket.MessageText, raw)

	ssRaw, _ := proto.Encode(proto.TypeSessionStarted, "s1", proto.SessionStarted{
		PID: 99, JSONLUUID: "u1", Cwd: "/", Account: "default",
	})
	_ = conn.Write(context.Background(), websocket.MessageText, ssRaw)

	require.Eventually(t, func() bool {
		got, err := s.Sessions().GetByID(ctx, "s1")
		return err == nil && got.Status == "running" && got.JSONLUUID == "u1"
	}, 2*time.Second, 20*time.Millisecond)
}
