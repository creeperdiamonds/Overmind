package javaproto

import (
	"encoding/binary"
	"math"

	"github.com/creeperdiamonds/overmind/internal/javawire"
)

// Entity packets. Layouts are minecraft-data's for 26.1.
var (
	PlayEntityMetadata = id("play", "toClient", "entity_metadata")
	PlayEntityVelocity = id("play", "toClient", "entity_velocity")
	PlayCollect        = id("play", "toClient", "collect")
)

// lpVec3 limits. minecraft-data marks lpVec3 "native"; this follows the codec
// of minecraft-protocol 1.68.0 (src/datatypes/lpVec3.js), which the test bot
// parses with, checked against its real server payloads (javaproto tests).
const (
	lpMaxQuantized = 32766.0
	lpMinValue     = 3.051944088384301e-5
	lpMaxValue     = 1.7179869183e10
)

// AppendLpVec3 writes a velocity in blocks per tick as an lpVec3: one zero
// byte for none, else 15 bits per axis scaled by a whole number, packed in six
// bytes, and the scale's upper part as a VarInt when it is over 3.
func AppendLpVec3(b []byte, x, y, z float64) []byte {
	clean := func(v float64) float64 {
		if math.IsNaN(v) {
			return 0
		}
		return math.Max(-lpMaxValue, math.Min(v, lpMaxValue))
	}
	x, y, z = clean(x), clean(y), clean(z)
	biggest := math.Max(math.Abs(x), math.Max(math.Abs(y), math.Abs(z)))
	if biggest < lpMinValue {
		return append(b, 0)
	}
	scale := math.Ceil(biggest)
	markers := uint64(scale)
	more := scale > 3
	if more {
		markers = uint64(math.Mod(scale, 4)) | 4
	}
	pack := func(v float64) uint64 { return uint64(math.Round((v*0.5 + 0.5) * lpMaxQuantized)) }
	packed := markers | pack(x/scale)<<3 | pack(y/scale)<<18 | pack(z/scale)<<33
	b = append(b, byte(packed), byte(packed>>8))
	b = binary.BigEndian.AppendUint32(b, uint32(packed>>16))
	if more {
		b = javawire.AppendVarInt(b, int32(scale/4))
	}
	return b
}

// ReadLpVec3 reads an lpVec3.
func (r *Reader) ReadLpVec3() (x, y, z float64) {
	a := r.Byte()
	if a == 0 || r.err != nil {
		return 0, 0, 0
	}
	rest := r.take(5)
	if rest == nil {
		return 0, 0, 0
	}
	packed := uint64(a) | uint64(rest[0])<<8 | uint64(binary.BigEndian.Uint32(rest[1:]))<<16
	scale := float64(a & 3)
	if a&4 != 0 {
		scale += float64(r.VarInt()) * 4
	}
	unpack := func(shift uint) float64 {
		q := math.Min(float64(packed>>shift&0x7FFF), lpMaxQuantized)
		return (q*2/lpMaxQuantized - 1) * scale
	}
	return unpack(3), unpack(18), unpack(33)
}

// Meta is entity metadata being built: entries of index, value type and value,
// in the order written. The type IDs are minecraft-data's (java261.MetadataType).
type Meta struct{ B []byte }

func (m *Meta) head(index byte, typ int32) {
	m.B = append(m.B, index)
	m.B = javawire.AppendVarInt(m.B, typ)
}

// Byte adds a byte entry.
func (m *Meta) Byte(index byte, typ int32, v byte) { m.head(index, typ); m.B = append(m.B, v) }

// VarInt adds an entry whose value is a VarInt (int, pose).
func (m *Meta) VarInt(index byte, typ int32, v int32) {
	m.head(index, typ)
	m.B = javawire.AppendVarInt(m.B, v)
}

// Float adds a float entry.
func (m *Meta) Float(index byte, typ int32, v float32) {
	m.head(index, typ)
	m.B = binary.BigEndian.AppendUint32(m.B, math.Float32bits(v))
}

// Item adds an item stack entry.
func (m *Meta) Item(index byte, typ int32, it Item) { m.head(index, typ); m.B = appendSlot(m.B, it) }

// EntityMetadata sets some of an entity's metadata. The list ends with 0xFF.
func EntityMetadata(entityID int32, m Meta) []byte {
	w := packet(PlayEntityMetadata)
	w.VarInt(entityID)
	w.B = append(w.B, m.B...)
	w.Byte(0xFF)
	return w.B
}

// EntityVelocity sets an entity's velocity, in blocks per tick.
func EntityVelocity(entityID int32, x, y, z float64) []byte {
	w := packet(PlayEntityVelocity)
	w.VarInt(entityID)
	w.B = AppendLpVec3(w.B, x, y, z)
	return w.B
}

// Collect shows an entity picking up count items of an item entity.
func Collect(collected, collector, count int32) []byte {
	w := packet(PlayCollect)
	w.VarInt(collected)
	w.VarInt(collector)
	w.VarInt(count)
	return w.B
}
