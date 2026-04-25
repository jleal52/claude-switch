//go:build !windows

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/pty"
	"github.com/jleal52/claude-switch/internal/session"
	"github.com/jleal52/claude-switch/internal/ws"
)

func TestEndToEndOpenWriteReceive(t *testing.T) {
	sid := ulidString(t)

	srvGotData := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		require.NoError(t, err)
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		// 1. hello.
		_, data, err := c.Read(ctx)
		require.NoError(t, err)
		typ, _, _, err := proto.Decode(data)
		require.NoError(t, err)
		require.Equal(t, proto.TypeHello, typ)

		// 2. send open_session.
		openRaw, _ := proto.Encode(proto.TypeOpenSession, sid, proto.OpenSession{
			Session: sid, Cwd: os.TempDir(), Account: "default",
			Args: []string{"-c", `read l; echo got:$l`},
		})
		require.NoError(t, c.Write(ctx, websocket.MessageText, openRaw))

		// 3. read frames; on session.started send pty.input; collect pty.data
		// until "got:hello" appears.
		sentInput := false
		for ctx.Err() == nil {
			msgType, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if msgType == websocket.MessageText {
				tt, _, _, _ := proto.Decode(data)
				if tt == proto.TypeSessionStarted && !sentInput {
					id, err := ulidFromString(sid)
					require.NoError(t, err)
					require.NoError(t, c.Write(ctx, websocket.MessageBinary, proto.EncodePTYData(id, []byte("hello\n"))))
					sentInput = true
				}
				continue
			}
			// Binary = pty.data.
			_, payload, err := proto.DecodePTYData(data)
			require.NoError(t, err)
			if strings.Contains(string(payload), "got:hello") {
				srvGotData <- "got:hello"
				return
			}
		}
	}))
	defer srv.Close()
	wsURL := "ws" + srv.URL[len("http"):]

	events := make(chan session.Event, 64)
	sup := session.NewSupervisor(session.Config{
		ClaudeBin: "/bin/sh",
		BaseArgs:  []string{}, // empty so server-supplied args are used as-is
		Start:     pty.Start,
	}, events)

	cli := ws.NewClient(ws.Config{
		URL: wsURL, Token: "t", WrapperID: "w-" + runtime.GOOS, Version: "test",
	}, sup, events)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go sup.Run(ctx)
	go func() { _ = cli.Run(ctx) }()

	select {
	case got := <-srvGotData:
		require.Equal(t, "got:hello", got)
	case <-ctx.Done():
		t.Fatal("did not receive expected pty.data")
	}
}

func ulidString(t *testing.T) string { t.Helper(); return ulid.Make().String() }
func ulidFromString(s string) (ulid.ULID, error) { return ulid.ParseStrict(s) }
