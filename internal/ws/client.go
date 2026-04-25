// Package ws wires the wrapper's supervisor to the server over a single
// WebSocket. JSON frames (via proto.Encode) carry control; binary frames
// carry raw PTY data (proto.EncodePTYData).
package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"

	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/session"
)

type Config struct {
	URL       string
	Token     string
	WrapperID string
	Version   string
}

type Client struct {
	cfg    Config
	sup    *session.Supervisor
	events <-chan session.Event
}

func NewClient(cfg Config, sup *session.Supervisor, events <-chan session.Event) *Client {
	return &Client{cfg: cfg, sup: sup, events: events}
}

// runOnce dials the server, sends hello, and pumps frames both ways until
// the connection drops or ctx is cancelled. Task 14 wraps this in reconnect.
func (c *Client) runOnce(ctx context.Context) error {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.cfg.Token)
	conn, _, err := websocket.Dial(ctx, c.cfg.URL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 * 1024 * 1024) // 4 MiB per frame

	// Send hello first.
	if err := c.sendHello(ctx, conn); err != nil {
		return err
	}

	// Fan-in: inbound frames and outbound events.
	readErr := make(chan error, 1)
	go func() { readErr <- c.readLoop(ctx, conn) }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case ev := <-c.events:
			if err := c.writeEvent(ctx, conn, ev); err != nil {
				return err
			}
		}
	}
}

func (c *Client) sendHello(ctx context.Context, conn *websocket.Conn) error {
	sessions := c.sup.Snapshot()
	hs := make([]proto.HelloSession, 0, len(sessions))
	for _, s := range sessions {
		hs = append(hs, proto.HelloSession{
			ID: s.ID, PID: s.PID(), JSONLUUID: s.JSONLUUID,
			Cwd: s.Cwd, Account: s.Account,
		})
	}
	hello := proto.Hello{
		WrapperID:    c.cfg.WrapperID,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Version:      c.cfg.Version,
		Accounts:     []string{"default"},
		Capabilities: []string{"pty"},
		Sessions:     hs,
	}
	raw, err := proto.Encode(proto.TypeHello, "", hello)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, raw)
}

func (c *Client) writeEvent(ctx context.Context, conn *websocket.Conn, ev session.Event) error {
	switch e := ev.(type) {
	case session.SessionStartedEvent:
		raw, err := proto.Encode(proto.TypeSessionStarted, e.Session, proto.SessionStarted{
			PID: e.PID, JSONLUUID: e.JSONLUUID, Cwd: e.Cwd, Account: e.Account,
		})
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, raw)
	case session.SessionExitedEvent:
		raw, err := proto.Encode(proto.TypeSessionExited, e.Session, proto.SessionExited{
			ExitCode: e.ExitCode, Reason: e.Reason, Detail: e.Detail,
		})
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, raw)
	case session.PTYDataEvent:
		id, err := ulid.ParseStrict(e.Session)
		if err != nil {
			return fmt.Errorf("pty.data: bad session id %q: %w", e.Session, err)
		}
		frame := proto.EncodePTYData(id, e.Bytes)
		return conn.Write(ctx, websocket.MessageBinary, frame)
	case session.JSONLTailEvent:
		raw, err := proto.Encode(proto.TypeJSONLTail, e.Session, proto.JSONLTail{Entry: e.Entry})
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, raw)
	default:
		return fmt.Errorf("ws: unknown event %T", ev)
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		switch typ {
		case websocket.MessageBinary:
			id, payload, err := proto.DecodePTYData(data)
			if err != nil {
				return fmt.Errorf("bad binary frame: %w", err)
			}
			_ = c.sup.Input(id.String(), payload)
		case websocket.MessageText:
			if err := c.handleControl(ctx, data); err != nil {
				return err
			}
		}
	}
}

func (c *Client) handleControl(ctx context.Context, data []byte) error {
	typ, sess, payload, err := proto.Decode(data)
	if err != nil {
		return err
	}
	switch typ {
	case proto.TypeOpenSession:
		var p proto.OpenSession
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		sid := p.Session
		if sid == "" {
			sid = sess
		}
		return c.sup.Open(ctx, sid, p.Cwd, p.Account, p.Args)
	case proto.TypeCloseSession:
		return c.sup.Close(sess)
	case proto.TypePTYResize:
		var p proto.PTYResize
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		return c.sup.Resize(sess, p.Cols, p.Rows)
	case proto.TypePing:
		var p proto.Ping
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		// Handled via a dedicated path? For MVP inline: noop, Task 14 adds it.
		_ = p
		return nil
	}
	return nil
}
