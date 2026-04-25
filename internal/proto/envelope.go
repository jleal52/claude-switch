// Package proto defines the wire format for the wrapper<->server WebSocket.
// JSON envelopes carry control frames; binary frames (see ptydata.go) carry
// raw PTY output/input on the hot path.
package proto

import (
	"encoding/json"
	"errors"
	"fmt"
)

const ProtocolVersion = 1

var ErrUnsupportedVersion = errors.New("proto: unsupported envelope version")

type RawPayload json.RawMessage

func (r RawPayload) Into(v any) error { return json.Unmarshal(r, v) }

type envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	Session string          `json:"session"`
	Payload json.RawMessage `json:"payload"`
}

// Encode marshals a typed payload into a versioned JSON envelope.
// session may be "" for non-session-scoped frames (hello, ping, pong).
func Encode(typ string, session string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("proto encode payload: %w", err)
	}
	return json.Marshal(envelope{V: ProtocolVersion, Type: typ, Session: session, Payload: body})
}

// Decode validates the envelope version and returns the raw payload for
// type-specific unmarshal by the caller (dispatch lives outside proto).
func Decode(raw []byte) (typ string, session string, payload RawPayload, err error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", "", nil, fmt.Errorf("proto decode: %w", err)
	}
	if env.V != ProtocolVersion {
		return "", "", nil, fmt.Errorf("proto: got v=%d: %w", env.V, ErrUnsupportedVersion)
	}
	return env.Type, env.Session, RawPayload(env.Payload), nil
}
