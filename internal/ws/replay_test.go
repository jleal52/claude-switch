package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/session"
)

func TestReplaySendsRingContentsAfterHello(t *testing.T) {
	var gotReplay atomic.Bool
	replayPayload := []byte("replay-bytes-xyz")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := websocket.Accept(w, r, nil)
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		// 1. hello.
		_, data, err := c.Read(ctx)
		require.NoError(t, err)
		typ, _, _, err := proto.Decode(data)
		require.NoError(t, err)
		require.Equal(t, proto.TypeHello, typ)

		// 2. immediately after hello, expect a binary pty.data replay frame.
		msgType, data, err := c.Read(ctx)
		require.NoError(t, err)
		require.Equal(t, websocket.MessageBinary, msgType)
		_, payload, err := proto.DecodePTYData(data)
		require.NoError(t, err)
		if string(payload) == string(replayPayload) {
			gotReplay.Store(true)
		}
	}))
	defer srv.Close()

	events := make(chan session.Event, 4)
	sup := session.NewSupervisor(session.Config{ClaudeBin: "/bin/true"}, events)

	sid := ulid.Make().String()
	require.NoError(t, sup.InjectForTest(sid, replayPayload))

	cli := NewClient(Config{
		URL: "ws" + srv.URL[len("http"):], Token: "t", WrapperID: "w", Version: "test",
	}, sup, events)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = cli.runOnce(ctx)
	require.True(t, gotReplay.Load(), "server did not receive replay frame")
}
