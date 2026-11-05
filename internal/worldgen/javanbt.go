package worldgen

import (
	"encoding/binary"
	"errors"
	"math"
)

// A small reader for Java's big-endian NBT, the format of structure templates (the structures
// package's, and the fossil feature's, NOTES-bedrock-structures 1.3). Compounds are
// map[string]any, lists []any, numbers their Go types (int8 as byte, int16, int32, int64, float32,
// float64), arrays []byte, []int32 and []int64.

type nbtReader struct {
	b   []byte
	off int
	err error
}

var errNBT = errors.New("nbt: truncated or malformed")

func (r *nbtReader) take(n int) []byte {
	if r.err != nil || n < 0 || r.off+n > len(r.b) {
		r.err = errNBT
		return nil
	}
	p := r.b[r.off : r.off+n]
	r.off += n
	return p
}

func (r *nbtReader) u8() byte {
	if p := r.take(1); p != nil {
		return p[0]
	}
	return 0
}

func (r *nbtReader) u16() uint16 {
	if p := r.take(2); p != nil {
		return binary.BigEndian.Uint16(p)
	}
	return 0
}

func (r *nbtReader) u32() uint32 {
	if p := r.take(4); p != nil {
		return binary.BigEndian.Uint32(p)
	}
	return 0
}

func (r *nbtReader) u64() uint64 {
	if p := r.take(8); p != nil {
		return binary.BigEndian.Uint64(p)
	}
	return 0
}

func (r *nbtReader) str() string { return string(r.take(int(r.u16()))) }

func (r *nbtReader) payload(tag byte, depth int) any {
	if depth > 64 {
		r.err = errNBT
	}
	if r.err != nil {
		return nil
	}
	switch tag {
	case 1:
		return r.u8()
	case 2:
		return int16(r.u16())
	case 3:
		return int32(r.u32())
	case 4:
		return int64(r.u64())
	case 5:
		return math.Float32frombits(r.u32())
	case 6:
		return math.Float64frombits(r.u64())
	case 7:
		return append([]byte(nil), r.take(int(int32(r.u32())))...)
	case 8:
		return r.str()
	case 9:
		t := r.u8()
		n := int(int32(r.u32()))
		if n < 0 || n > len(r.b) {
			r.err = errNBT
			return nil
		}
		l := make([]any, 0, n)
		for range n {
			l = append(l, r.payload(t, depth+1))
			if r.err != nil {
				return nil
			}
		}
		return l
	case 10:
		m := map[string]any{}
		for r.err == nil {
			t := r.u8()
			if t == 0 {
				break
			}
			k := r.str()
			m[k] = r.payload(t, depth+1)
		}
		return m
	case 11:
		n := int(int32(r.u32()))
		if n < 0 || n*4 > len(r.b) {
			r.err = errNBT
			return nil
		}
		a := make([]int32, n)
		for i := range a {
			a[i] = int32(r.u32())
		}
		return a
	case 12:
		n := int(int32(r.u32()))
		if n < 0 || n*8 > len(r.b) {
			r.err = errNBT
			return nil
		}
		a := make([]int64, n)
		for i := range a {
			a[i] = int64(r.u64())
		}
		return a
	}
	r.err = errNBT
	return nil
}

// readNBT reads a root compound.
// ReadJavaNBT reads a Java big-endian NBT root compound.
func ReadJavaNBT(b []byte) (map[string]any, error) {
	r := &nbtReader{b: b}
	if r.u8() != 10 {
		return nil, errNBT
	}
	r.str()
	m, _ := r.payload(10, 0).(map[string]any)
	return m, r.err
}
