package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/session"
)

func TestClientSendsHelloOnConnect(t *testing.T) {
	helloCh := make(chan proto.Hello, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		c, err := websocket.Accept(w, r, nil)
		require.NoError(t, err)
		defer c.CloseNow()
		_, data, err := c.Read(r.Context())
		require.NoError(t, err)
		typ, _, payload, err := proto.Decode(data)
		require.NoError(t, err)
		require.Equal(t, proto.TypeHello, typ)
		var h proto.Hello
		require.NoError(t, json.Unmarshal([]byte(payload), &h))
		helloCh <- h
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]

	events := make(chan session.Event, 8)
	sup := session.NewSupervisor(session.Config{ClaudeBin: "/bin/true"}, events)

	cli := NewClient(Config{
		URL:       wsURL,
		Token:     "test-token",
		WrapperID: "w-test",
		Version:   "test",
	}, sup, events)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = cli.runOnce(ctx, nil) }()

	select {
	case h := <-helloCh:
		require.Equal(t, "w-test", h.WrapperID)
		require.Contains(t, h.Accounts, "default")
	case <-ctx.Done():
		t.Fatal("server never received hello")
	}
}

func TestRunOnceReadsTokenFromTokenSourceEachDial(t *testing.T) {
	var seen atomic.Value
	seen.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.Header.Get("Authorization"))
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		_, _, _ = c.Read(r.Context())
		<-r.Context().Done()
	}))
	defer srv.Close()

	events := make(chan session.Event, 4)
	sup := session.NewSupervisor(session.Config{ClaudeBin: "/bin/true"}, events)
	cli := NewClient(Config{
		URL:         "ws" + srv.URL[len("http"):],
		Token:       "static-fallback-should-not-be-used",
		TokenSource: func() string { return "dynamic-token" },
		WrapperID:   "w",
		Version:     "test",
		ReadTimeout: 100 * time.Millisecond,
	}, sup, events)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = cli.runOnce(ctx, nil)

	require.Equal(t, "Bearer dynamic-token", seen.Load())
}
