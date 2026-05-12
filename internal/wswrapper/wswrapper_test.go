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

func TestServerPingsWrapperPeriodically(t *testing.T) {
	s := store.NewTestStore(t, "wsw_ping")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, store.OAuthProfile{Provider: "github", Subject: "u1"})
	wRow, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	access, _, _ := s.WrapperTokens().Issue(ctx, wRow.ID, u.ID, time.Hour)

	h := hub.New()
	srv := httptest.NewServer(newHandlerWithPingInterval(s, h, 50*time.Millisecond))
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

	// Expect at least one TypePing JSON frame within 1s. Then a second one
	// shortly after to prove the ticker keeps going.
	readCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	pings := 0
	for pings < 2 {
		typ, data, err := conn.Read(readCtx)
		require.NoError(t, err, "expected at least 2 pings within 1s, got %d", pings)
		if typ != websocket.MessageText {
			continue
		}
		t2, _, _, err := proto.Decode(data)
		require.NoError(t, err)
		if t2 == proto.TypePing {
			pings++
		}
	}
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

func TestWrapperPersistsCatalogFullThenIncremental(t *testing.T) {
	s := store.NewTestStore(t, "wsw_catalog")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, store.OAuthProfile{Provider: "github", Subject: "u-catalog"})
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

	hello := proto.Hello{WrapperID: "x", OS: "linux", Arch: "amd64", Version: "0.1.0", Accounts: []string{"default"}, Capabilities: []string{"pty"}}
	raw, _ := proto.Encode(proto.TypeHello, "", hello)
	require.NoError(t, conn.Write(context.Background(), websocket.MessageText, raw))

	full := proto.CatalogDiff{
		Full: true,
		Projects: []proto.CatalogProject{
			{Slug: "-a", Cwd: "/a", Name: "a", SessionCount: 1, FirstActivityAt: "2026-05-09T10:00:00Z", LastActivityAt: "2026-05-09T10:00:00Z"},
			{Slug: "-b", Cwd: "/b", Name: "b", SessionCount: 1, FirstActivityAt: "2026-05-09T10:00:00Z", LastActivityAt: "2026-05-09T10:00:00Z"},
		},
		Transcripts: []proto.CatalogTranscript{
			{JSONLUUID: "u1", Slug: "-a", Path: "-a/u1.jsonl", StartedAt: "2026-05-09T10:00:00Z", EndedAt: "2026-05-09T10:30:00Z", MessageCount: 5, Title: "hello", Bytes: 1024},
			{JSONLUUID: "u2", Slug: "-b", Path: "-b/u2.jsonl", StartedAt: "2026-05-09T11:00:00Z", EndedAt: "2026-05-09T11:30:00Z", MessageCount: 3, Title: "yo", Bytes: 512},
		},
	}
	raw, _ = proto.Encode(proto.TypeCatalogDiff, "", full)
	require.NoError(t, conn.Write(context.Background(), websocket.MessageText, raw))

	require.Eventually(t, func() bool {
		got, _ := s.Transcripts().ListByWrapper(ctx, wRow.ID, 10)
		return len(got) == 2
	}, 2*time.Second, 20*time.Millisecond)

	// Now send an incremental diff: add u3, remove u1.
	incr := proto.CatalogDiff{
		Full: false,
		Transcripts: []proto.CatalogTranscript{
			{JSONLUUID: "u3", Slug: "-a", Path: "-a/u3.jsonl", StartedAt: "2026-05-09T12:00:00Z", EndedAt: "2026-05-09T12:30:00Z", MessageCount: 1, Title: "new", Bytes: 64},
		},
		RemovedTranscripts: []string{"u1"},
	}
	raw, _ = proto.Encode(proto.TypeCatalogDiff, "", incr)
	require.NoError(t, conn.Write(context.Background(), websocket.MessageText, raw))

	require.Eventually(t, func() bool {
		got, _ := s.Transcripts().ListByWrapper(ctx, wRow.ID, 10)
		if len(got) != 2 {
			return false
		}
		uuids := []string{got[0].JSONLUUID, got[1].JSONLUUID}
		hasU2, hasU3 := false, false
		for _, u := range uuids {
			if u == "u2" {
				hasU2 = true
			}
			if u == "u3" {
				hasU3 = true
			}
		}
		return hasU2 && hasU3
	}, 2*time.Second, 20*time.Millisecond)

	// And a smaller full snapshot replaces everything to one row.
	again := proto.CatalogDiff{
		Full: true,
		Projects: []proto.CatalogProject{
			{Slug: "-a", Cwd: "/a", Name: "a", SessionCount: 1, FirstActivityAt: "2026-05-09T12:00:00Z", LastActivityAt: "2026-05-09T12:30:00Z"},
		},
		Transcripts: []proto.CatalogTranscript{
			{JSONLUUID: "u3", Slug: "-a", Path: "-a/u3.jsonl", StartedAt: "2026-05-09T12:00:00Z", EndedAt: "2026-05-09T12:30:00Z", MessageCount: 1, Title: "new", Bytes: 64},
		},
	}
	raw, _ = proto.Encode(proto.TypeCatalogDiff, "", again)
	require.NoError(t, conn.Write(context.Background(), websocket.MessageText, raw))

	require.Eventually(t, func() bool {
		got, _ := s.Transcripts().ListByWrapper(ctx, wRow.ID, 10)
		projs, _ := s.Projects().ListByWrapper(ctx, wRow.ID)
		return len(got) == 1 && len(projs) == 1
	}, 2*time.Second, 20*time.Millisecond)
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
