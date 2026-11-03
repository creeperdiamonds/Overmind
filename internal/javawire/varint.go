// Package javawire reads and writes the Java edition's framed byte stream:
// VarInt lengths, optional zlib compression and optional AES-CFB8 encryption.
// It knows nothing about packet contents.
package javawire

import (
	"errors"
	"io"
)

// ErrVarInt reports a VarInt longer than five bytes or wider than 32 bits.
var ErrVarInt = errors.New("javawire: malformed VarInt")

// ReadVarInt reads one VarInt.
func ReadVarInt(r io.ByteReader) (int32, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			if i > 0 && err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		if i == 4 && b > 0x0f {
			return 0, ErrVarInt
		}
		v |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int32(v), nil
		}
	}
	return 0, ErrVarInt
}

// AppendVarInt appends the VarInt encoding of v to b.
func AppendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}
