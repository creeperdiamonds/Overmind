package structures

import (
	"embed"
	"strings"
	"sync"
)

// Shared pieces of the clean-room ocean monument, Nether fortress and woodland mansion,
// written from the facts-only specification of how they turn out on BDS 1.26.60 worlds. The tables in
// piecedata/ are that specification's facts (operation lists and final cells); nothing in them is code.

//go:embed piecedata/*.json
var crFiles embed.FS

// crTable reads one embedded table.
func crTable(name string) []byte {
	b, err := crFiles.ReadFile("piecedata/" + name)
	if err != nil {
		panic(err) // Embedded at build time: missing means a broken build.
	}
	return b
}

// crLocal maps a piece-local cell to the world, given the piece's world box and orientation: south
// keeps both axes, north counts z down from the far side, west and east run local x along world z and
// local z along world x (west from the box's east side, east from its west side).
func crLocal(b box, o orient, x, y, z int) (int, int, int) {
	switch o {
	case oN:
		return b[0] + x, b[1] + y, b[5] - z
	case oW:
		return b[3] - z, b[1] + y, b[2] + x
	case oE:
		return b[0] + z, b[1] + y, b[2] + x
	}
	return b[0] + x, b[1] + y, b[2] + z
}

// crSpan is the world box spanned by two piece-local corners.
func crSpan(b box, o orient, a, c [3]int) box {
	x0, y0, z0 := crLocal(b, o, a[0], a[1], a[2])
	x1, y1, z1 := crLocal(b, o, c[0], c[1], c[2])
	return box{min(x0, x1), min(y0, y1), min(z0, z1), max(x0, x1), max(y0, y1), max(z0, z1)}
}

// crChunk is the chunk's full-height box.
func crChunk(w *writer) box {
	return box{w.cx * 16, w.lo, w.cz * 16, w.cx*16 + 15, w.hi, w.cz*16 + 15}
}

// crClip intersects a world box with the chunk's box; ok is false when they do not meet.
func crClip(w *writer, b box) (box, bool) {
	c := crChunk(w)
	out := box{max(b[0], c[0]), max(b[1], c[1]), max(b[2], c[2]), min(b[3], c[3]), min(b[4], c[4]), min(b[5], c[5])}
	return out, out[0] <= out[3] && out[1] <= out[4] && out[2] <= out[5]
}

// crWet says whether a cell holds a liquid: water or lava as its block, or anything in its second layer.
func crWet(w *writer, x, y, z int) bool {
	switch w.blockName(x, y, z) {
	case "water", "flowing_water", "lava", "flowing_lava":
		return true
	}
	return w.chunk().Block(uint8(x&15), int16(y), uint8(z&15), 1) != airRID()
}

// crSoft is the "replaceable" test of the column fills: air, a liquid, glow lichen or seagrass.
func crSoft(w *writer, x, y, z int) bool {
	switch w.blockName(x, y, z) {
	case "air", "glow_lichen", "seagrass":
		return true
	}
	return crWet(w, x, y, z)
}

var crRIDs sync.Map // Block text to runtime ID.

// crBlock is the runtime ID of "name k=v k=v ...": the registered state of that name whose properties
// hold every given value (values as the registry prints them: booleans 0 or 1), or the name's default
// state without properties. Unknown names give air.
func crBlock(text string) uint32 {
	if v, ok := crRIDs.Load(text); ok {
		return v.(uint32)
	}
	fields := strings.Fields(text)
	rid := airRID()
	if len(fields) == 1 {
		if r, ok := ridOfName(fields[0]); ok {
			rid = r
		}
	} else if len(fields) > 1 {
		registry.load()
	states:
		for _, s := range registry.byName[fields[0]] {
			for _, kv := range fields[1:] {
				k, v, _ := strings.Cut(kv, "=")
				if s.props[k] != v {
					continue states
				}
			}
			rid = s.rid
			break
		}
	}
	crRIDs.Store(text, rid)
	return rid
}
