package javaproto

import (
	"encoding/binary"

	"github.com/google/uuid"
	"github.com/overmind/overmind/internal/javawire"
)

// Writer builds one packet body.
type Writer struct{ B []byte }

// VarInt appends a VarInt.
func (w *Writer) VarInt(v int32) { w.B = javawire.AppendVarInt(w.B, v) }

// Bool appends one byte.
func (w *Writer) Bool(v bool) {
	if v {
		w.B = append(w.B, 1)
	} else {
		w.B = append(w.B, 0)
	}
}

// Long appends a big-endian int64.
func (w *Writer) Long(v int64) { w.B = binary.BigEndian.AppendUint64(w.B, uint64(v)) }

// UUID appends 16 bytes.
func (w *Writer) UUID(u uuid.UUID) { w.B = append(w.B, u[:]...) }

// Bytes appends a VarInt-prefixed byte array.
func (w *Writer) Bytes(b []byte) { w.VarInt(int32(len(b))); w.B = append(w.B, b...) }

// String appends a VarInt-prefixed string.
func (w *Writer) String(s string) { w.VarInt(int32(len(s))); w.B = append(w.B, s...) }
