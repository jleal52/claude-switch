package proto

import (
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"
)

const binaryPTYDataVersion byte = 0x01

var ErrMalformedBinaryFrame = errors.New("proto: malformed binary frame")

// EncodePTYData returns the wire representation of a pty.data frame:
//
//	byte 0     : version (0x01)
//	bytes 1..16: ULID session id (16 bytes)
//	bytes 17..: raw payload
//
// Zero-length payload is valid.
func EncodePTYData(session ulid.ULID, payload []byte) []byte {
	buf := make([]byte, 1+16+len(payload))
	buf[0] = binaryPTYDataVersion
	copy(buf[1:17], session[:])
	copy(buf[17:], payload)
	return buf
}

// DecodePTYData parses a binary pty.data frame. Returns ErrUnsupportedVersion
// if byte 0 is not the expected version, and ErrMalformedBinaryFrame if the
// frame is shorter than the header.
func DecodePTYData(frame []byte) (ulid.ULID, []byte, error) {
	if len(frame) < 17 {
		return ulid.ULID{}, nil, fmt.Errorf("proto: frame len=%d: %w", len(frame), ErrMalformedBinaryFrame)
	}
	if frame[0] != binaryPTYDataVersion {
		return ulid.ULID{}, nil, fmt.Errorf("proto: binary frame v=%d: %w", frame[0], ErrUnsupportedVersion)
	}
	var id ulid.ULID
	copy(id[:], frame[1:17])
	payload := make([]byte, len(frame)-17)
	copy(payload, frame[17:])
	return id, payload, nil
}
