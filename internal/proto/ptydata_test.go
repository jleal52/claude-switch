package proto

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestPTYDataRoundTrip(t *testing.T) {
	id := ulid.Make()
	payload := []byte("hello\x1b[0m\n")

	frame := EncodePTYData(id, payload)

	gotID, gotPayload, err := DecodePTYData(frame)
	require.NoError(t, err)
	require.Equal(t, id, gotID)
	require.Equal(t, payload, gotPayload)
}

func TestPTYDataRejectsWrongVersion(t *testing.T) {
	frame := make([]byte, 17)
	frame[0] = 0x99
	_, _, err := DecodePTYData(frame)
	require.ErrorIs(t, err, ErrUnsupportedVersion)
}

func TestPTYDataRejectsTruncated(t *testing.T) {
	_, _, err := DecodePTYData([]byte{0x01, 0x02})
	require.Error(t, err)
}

func TestPTYDataAllowsEmptyPayload(t *testing.T) {
	// Edge case: valid envelope with zero-length payload (we coalesce only
	// non-empty output, but decoder must accept empty).
	id := ulid.Make()
	frame := EncodePTYData(id, nil)
	gotID, gotPayload, err := DecodePTYData(frame)
	require.NoError(t, err)
	require.Equal(t, id, gotID)
	require.Empty(t, gotPayload)
}
