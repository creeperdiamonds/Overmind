// Package javaproto encodes and decodes the Java edition packets used by
// status, login and configuration. Packet IDs come from java261, never from
// literals here.
package javaproto

import (
	"encoding/binary"
	"errors"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/overmind/overmind/internal/javawire"
)

// ErrMalformed reports a packet that is short, too long or otherwise wrong.
var ErrMalformed = errors.New("javaproto: malformed packet")

// Reader decodes one packet body. The first failure sticks: later reads
// return zero values and Err reports it. Nothing is allocated from a length
// the peer claims; lengths are checked against the bytes left in the frame.
type Reader struct {
	b   []byte
	err error
}

// NewReader reads from b, which must stay unchanged while the Reader is used.
func NewReader(b []byte) *Reader { return &Reader{b: b} }

// Err is the first error met, or nil.
func (r *Reader) Err() error { return r.err }

// Len is the number of unread bytes.
func (r *Reader) Len() int { return len(r.b) }

func (r *Reader) fail() { r.err = ErrMalformed; r.b = nil }

func (r *Reader) take(n int) []byte {
	if r.err != nil || n < 0 || n > len(r.b) {
		r.fail()
		return nil
	}
	out := r.b[:n:n]
	r.b = r.b[n:]
	return out
}

// VarInt reads a VarInt.
func (r *Reader) VarInt() int32 {
	if r.err != nil {
		return 0
	}
	v, err := javawire.ReadVarInt(r)
	if err != nil {
		r.fail()
		return 0
	}
	return v
}

// ReadByte makes Reader a io.ByteReader.
func (r *Reader) ReadByte() (byte, error) {
	if len(r.b) == 0 {
		return 0, errors.New("eof")
	}
	c := r.b[0]
	r.b = r.b[1:]
	return c, nil
}

// Bool reads one byte that must be 0 or 1.
func (r *Reader) Bool() bool {
	b := r.take(1)
	if b == nil {
		return false
	}
	if b[0] > 1 {
		r.fail()
	}
	return b[0] == 1
}

// UShort reads a big-endian uint16.
func (r *Reader) UShort() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

// Long reads a big-endian int64.
func (r *Reader) Long() int64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(b))
}

// UUID reads 16 bytes.
func (r *Reader) UUID() uuid.UUID {
	var u uuid.UUID
	copy(u[:], r.take(16))
	return u
}

// Bytes reads a VarInt-prefixed byte array of at most max bytes. The result
// aliases the frame.
func (r *Reader) Bytes(max int) []byte {
	n := r.VarInt()
	if r.err != nil || int(n) > max {
		r.fail()
		return nil
	}
	return r.take(int(n))
}

// String reads a VarInt-prefixed UTF-8 string of at most maxChars UTF-16-ish
// characters (counted as runes).
func (r *Reader) String(maxChars int) string {
	b := r.Bytes(maxChars * 3)
	if r.err != nil {
		return ""
	}
	if !utf8.Valid(b) || utf8.RuneCount(b) > maxChars {
		r.fail()
		return ""
	}
	return string(b)
}
