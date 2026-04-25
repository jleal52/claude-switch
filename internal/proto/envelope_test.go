package proto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvelopeRoundTripHello(t *testing.T) {
	h := Hello{
		WrapperID:    "w-abc",
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
		Accounts:     []string{"default"},
		Capabilities: []string{"pty"},
	}
	raw, err := Encode("hello", "", h)
	require.NoError(t, err)

	typ, session, payload, err := Decode(raw)
	require.NoError(t, err)
	require.Equal(t, "hello", typ)
	require.Equal(t, "", session)

	var got Hello
	require.NoError(t, payload.Into(&got))
	require.Equal(t, h, got)
}

func TestEnvelopeRejectsWrongVersion(t *testing.T) {
	raw := []byte(`{"v":99,"type":"hello","session":"","payload":{}}`)
	_, _, _, err := Decode(raw)
	require.ErrorIs(t, err, ErrUnsupportedVersion)
}
