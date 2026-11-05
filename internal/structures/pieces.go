package structures

import (
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Oriented code-built pieces (mineshafts and strongholds), with the conventions of
// NOTES-structures-phase2 P14.1 (cited P141): a piece has a world box and an orientation; a local
// (x, y, z) maps to the world by the orientation, and a local facing turns with the piece. Writes only
// happen inside the chunk being made; every piece reaching the chunk is post-processed with the chunk's
// random (P9.1), in its structure's piece order.

// orient is a piece's orientation, in Bedrock's codes (P14.4: 0 = S, 1 = W, 2 = N, 3 = E).
type orient int8

const (
	oS orient = iota
	oW
	oN
	oE
	oNone // No orientation: local is world (mineshaft rooms and crossings).
)

// opiece is an oriented piece box.
type opiece struct {
	b box
	o orient
}

// world maps a local position (P141: y = y0 + y; N (x0+x, z1-z); S (x0+x, z0+z); W (x1-z, z0+x);
// E (x0+z, z0+x); no orientation: local = world).
func (p *opiece) world(x, y, z int) (int, int, int) {
	b := p.b
	switch p.o {
	case oN:
		return b[0] + x, b[1] + y, b[5] - z
	case oS:
		return b[0] + x, b[1] + y, b[2] + z
	case oW:
		return b[3] - z, b[1] + y, b[2] + x
	case oE:
		return b[0] + z, b[1] + y, b[2] + x
	}
	return x, y, z
}

// Facings, as horizontal world directions.
type facing int8

const (
	fN facing = iota
	fS
	fW
	fE
)

// turn is a local facing in the world. The piece descriptions give facings in the frame of Java's
// pieces, where a local "north" points along local +z: a ladder "facing S" at the wall z = max points
// to -z (P14.4's library and room crossing ladders and both stair runs read this way). Turned with the
// piece's mapping, that is Java's mirror and rotation per orientation (S mirrors north and south, W
// mirrors and turns, E turns).
func (p *opiece) turn(f facing) facing {
	dx, dz := 0, 0
	switch f {
	case fN:
		dz = 1
	case fS:
		dz = -1
	case fE:
		dx = 1
	case fW:
		dx = -1
	}
	var wx, wz int
	switch p.o {
	case oS:
		wx, wz = dx, dz
	case oN:
		wx, wz = dx, -dz
	case oW:
		wx, wz = -dz, dx
	case oE:
		wx, wz = dz, dx
	default:
		return f
	}
	switch {
	case wz < 0:
		return fN
	case wz > 0:
		return fS
	case wx > 0:
		return fE
	}
	return fW
}

// ctx is one piece being post-processed into a chunk.
type ctx struct {
	w *writer
	p *opiece
	r *worldgen.BedrockRandom
}

func (c *ctx) in(x, y, z int) bool {
	wx, wy, wz := c.p.world(x, y, z)
	return c.w.in(wx, wy, wz)
}

// get is the block at a local cell; air outside the chunk (Java's getBlock).
func (c *ctx) get(x, y, z int) uint32 {
	wx, wy, wz := c.p.world(x, y, z)
	if !c.w.in(wx, wy, wz) {
		return airRID()
	}
	return c.w.chunk().Block(uint8(wx&15), int16(wy), uint8(wz&15), 0)
}

func (c *ctx) set(x, y, z int, rid uint32) {
	wx, wy, wz := c.p.world(x, y, z)
	if rid != noBlock && c.w.in(wx, wy, wz) {
		c.w.set(wx, wy, wz, rid, false)
	}
}

// box is BOX: the shell gets edge, the inside fill; with skipAir only cells that are not air. Loops y,
// then x, then z (P141).
func (c *ctx) box(x0, y0, z0, x1, y1, z1 int, edge, fill uint32, skipAir bool) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if skipAir && c.get(x, y, z) == airRID() {
					continue
				}
				b := fill
				if x == x0 || x == x1 || y == y0 || y == y1 || z == z0 || z == z1 {
					b = edge
				}
				c.set(x, y, z, b)
			}
		}
	}
}

// maybeBox is MAYBEBOX: a float for every cell first, the cell written when f <= p (and, with
// skipAir, not air now; with interior, only an interior cell).
func (c *ctx) maybeBox(p float32, x0, y0, z0, x1, y1, z1 int, edge, fill uint32, skipAir, interior bool) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				f := c.r.NextFloat()
				if f > p || skipAir && c.get(x, y, z) == airRID() || interior && !c.interior(x, y, z) {
					continue
				}
				b := fill
				if x == x0 || x == x1 || y == y0 || y == y1 || z == z0 || z == z1 {
					b = edge
				}
				c.set(x, y, z, b)
			}
		}
	}
}

// maybe is MAYBE: f = nextFloat(); written when f < p.
func (c *ctx) maybe(p float32, x, y, z int, rid uint32) {
	if c.r.NextFloat() < p {
		c.set(x, y, z, rid)
	}
}

// interior is Java's isInterior as Bedrock computes it (P141): inside the chunk and below the chunk's
// surface heightmap at the column.
func (c *ctx) interior(x, y, z int) bool {
	wx, wy, wz := c.p.world(x, y, z)
	return c.w.in(wx, wy, wz) && wy < c.w.height(wx, wz)
}

// solidAt says whether the world cell holds a full solid block.
func (c *ctx) solidAt(wx, wy, wz int) bool {
	if !c.w.in(wx, wy, wz) {
		return false
	}
	b := c.w.block(wx, wy, wz)
	if b == nil {
		return false
	}
	_, ok := b.Model().(model.Solid)
	return ok
}

func (c *ctx) solid(x, y, z int) bool {
	wx, wy, wz := c.p.world(x, y, z)
	return c.solidAt(wx, wy, wz)
}

// liquidAt says whether a world cell inside the chunk holds a liquid (or water in its second layer).
func (w *writer) liquidAt(x, y, z int) bool {
	if !w.in(x, y, z) {
		return false
	}
	if _, ok := w.block(x, y, z).(world.Liquid); ok {
		return true
	}
	return w.chunk().Block(uint8(x&15), int16(y), uint8(z&15), 1) != airRID()
}

// chunkBox is the chunk's box.
func (w *writer) chunkBox() box {
	return box{w.cx * 16, minY, w.cz * 16, w.cx*16 + 15, maxY, w.cz*16 + 15}
}

// orientBox is P14.4's orientBox: the box of w (across) x h x d (along dir) whose local origin is foot
// + (ox, oy, oz) turned by dir.
func orientBox(fx, fy, fz, ox, oy, oz, w, h, d int, o orient) box {
	y0, y1 := fy+oy, fy+oy+h-1
	switch o {
	case oN:
		return box{fx + ox, y0, fz - oz - d + 1, fx + ox + w - 1, y1, fz - oz}
	case oE:
		return box{fx + oz, y0, fz + ox, fx + oz + d - 1, y1, fz + ox + w - 1}
	case oW:
		return box{fx - oz - d + 1, y0, fz + ox, fx - oz, y1, fz + ox + w - 1}
	}
	return box{fx + ox, y0, fz + oz, fx + ox + w - 1, y1, fz + oz + d - 1}
}

// boundsOf is the box around boxes.
func boundsOf(boxes []box) box {
	b := boxes[0]
	for _, o := range boxes[1:] {
		b = box{min(b[0], o[0]), min(b[1], o[1]), min(b[2], o[2]), max(b[3], o[3]), max(b[4], o[4]), max(b[5], o[5])}
	}
	return b
}

// moveBelowSea is the y move of mineshafts and strongholds (P13.1, P13.2): j = -64 + (maxY - minY) +
// 2; when j < 53, j += nextInt(53 - j); the shift that puts the top at j.
func moveBelowSea(all box, r *worldgen.BedrockRandom) int {
	j := minY + (all[4] - all[1]) + 2
	if j < 53 {
		j += int(r.NextIntN(int32(53 - j)))
	}
	return j - all[4]
}
