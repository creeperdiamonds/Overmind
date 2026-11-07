package structures

import (
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Shared machinery of the code-built pieces (mineshafts, strongholds, the scattered pieces, and the
// monument, fortress and mansion, which build on the frame and the ctx writes): a piece's local frame,
// the oriented boxes new pieces are placed with, the chunk-bounded writes and tests, and the lookup of
// exact block states. Written from the clean-room spec (piece_helpers/SPEC.md, INTERFACE.md).

// orient is a piece's orientation, in the order int(4) draws them.
type orient uint8

const (
	oS orient = iota
	oW
	oN
	oE
	oNone // Local coordinates are world coordinates.
)

// facing is a compass direction. As a local direction (op tables, turn): fN is local +z, fS local -z, fW
// local -x, fE local +x. As a world direction: north is -z, south +z, west -x, east +x.
type facing uint8

const (
	fN facing = iota
	fS
	fW
	fE
)

var facingNames = [4]string{"north", "south", "west", "east"}

// facingName is a world facing's compass name, as the facing tables key it.
func facingName(f facing) string { return facingNames[f&3] }

// facingByte is a world facing's letter, as stairsRID takes it.
func facingByte(f facing) byte { return "NSWE"[f&3] }

// facingIndex is a world facing's Bedrock facing_direction (north 2, south 3, west 4, east 5).
func facingIndex(f facing) int { return [4]int{2, 3, 4, 5}[f&3] }

// localStep is a local facing as a step in local x and z.
func (f facing) localStep() (dx, dz int) {
	switch f {
	case fN:
		return 0, 1
	case fS:
		return 0, -1
	case fW:
		return -1, 0
	}
	return 1, 0
}

// worldFacing is the world facing of a world step.
func worldFacing(dx, dz int) facing {
	switch {
	case dz < 0:
		return fN
	case dz > 0:
		return fS
	case dx > 0:
		return fE
	}
	return fW
}

// opiece is a piece placed in the world: its box and orientation.
type opiece struct {
	b box
	o orient
}

// world maps local coordinates to world coordinates (the frame table).
func (p *opiece) world(x, y, z int) (int, int, int) {
	b := p.b
	switch p.o {
	case oS:
		return b[0] + x, b[1] + y, b[2] + z
	case oN:
		return b[0] + x, b[1] + y, b[5] - z
	case oW:
		return b[3] - z, b[1] + y, b[2] + x
	case oE:
		return b[0] + z, b[1] + y, b[2] + x
	}
	return x, y, z
}

// turn is the world facing of a local facing.
func (p *opiece) turn(f facing) facing {
	dx, dz := f.localStep()
	switch p.o {
	case oN:
		dz = -dz
	case oW:
		dx, dz = -dz, dx
	case oE:
		dx, dz = dz, dx
	}
	return worldFacing(dx, dz)
}

// orientBox is the box of a piece w across, h high and d long whose foot is (fx, fy, fz), offset by
// (ox, oy, oz) across, up and along, going in direction o.
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

// boundsOf is the smallest box holding all the boxes.
func boundsOf(boxes []box) box {
	if len(boxes) == 0 {
		return box{}
	}
	out := boxes[0]
	for _, b := range boxes[1:] {
		for i := range 3 {
			out[i] = min(out[i], b[i])
			out[i+3] = max(out[i+3], b[i+3])
		}
	}
	return out
}

// moveBelowSea is the height rule of the underground pieces: how far to move a structure whose bounds
// are all so that its top lands at a random height up to 53 (one draw when there is room).
func moveBelowSea(all box, r *worldgen.BedrockRandom) int {
	j := minY + (all[4] - all[1]) + 2
	if j < 53 {
		j += int(r.NextIntN(int32(53 - j)))
	}
	return j - all[4]
}

// ctx writes one piece into one chunk: local coordinates through p's frame, writes only inside the
// chunk, random draws from r.
type ctx struct {
	w *writer
	p *opiece
	r *worldgen.BedrockRandom
}

func (c *ctx) in(x, y, z int) bool {
	wx, wy, wz := c.p.world(x, y, z)
	return c.w.in(wx, wy, wz)
}

// get is the main block at a local cell, air outside the chunk.
func (c *ctx) get(x, y, z int) uint32 {
	wx, wy, wz := c.p.world(x, y, z)
	return c.w.mainAt(wx, wy, wz)
}

// set writes a local cell; outside the chunk, or a block the registry lacks, nothing happens.
func (c *ctx) set(x, y, z int, rid uint32) {
	wx, wy, wz := c.p.world(x, y, z)
	c.w.put(wx, wy, wz, rid)
}

// box fills a local box: edge on its shell, fill inside; with skipAir, cells that are air are left.
func (c *ctx) box(x0, y0, z0, x1, y1, z1 int, edge, fill uint32, skipAir bool) {
	air := airRID()
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				wx, wy, wz := c.p.world(x, y, z)
				if !c.w.in(wx, wy, wz) {
					continue
				}
				if skipAir && c.w.mainAt(wx, wy, wz) == air {
					continue
				}
				rid := fill
				if x == x0 || x == x1 || y == y0 || y == y1 || z == z0 || z == z1 {
					rid = edge
				}
				c.w.put(wx, wy, wz, rid)
			}
		}
	}
}

// maybeBox is the chance box: every cell (y, then x, then z) draws a float first, in the chunk or not,
// and is written when the float is at most p (and, with skipAir, the cell is not air; with interior,
// the cell is interior).
func (c *ctx) maybeBox(p float32, x0, y0, z0, x1, y1, z1 int, edge, fill uint32, skipAir, interior bool) {
	air := airRID()
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				f := c.r.NextFloat()
				if !(f <= p) {
					continue
				}
				wx, wy, wz := c.p.world(x, y, z)
				if !c.w.in(wx, wy, wz) {
					continue
				}
				if skipAir && c.w.mainAt(wx, wy, wz) == air {
					continue
				}
				if interior && !c.w.interior(wx, wy, wz) {
					continue
				}
				rid := fill
				if x == x0 || x == x1 || y == y0 || y == y1 || z == z0 || z == z1 {
					rid = edge
				}
				c.w.put(wx, wy, wz, rid)
			}
		}
	}
}

// maybe draws a float and writes rid at the local cell when it is below p.
func (c *ctx) maybe(p float32, x, y, z int, rid uint32) {
	if c.r.NextFloat() < p {
		c.set(x, y, z, rid)
	}
}

// interior says whether a local cell is in the chunk and below its column's top.
func (c *ctx) interior(x, y, z int) bool {
	wx, wy, wz := c.p.world(x, y, z)
	return c.w.interior(wx, wy, wz)
}

// solid says whether a local cell is in the chunk and holds a full solid block.
func (c *ctx) solid(x, y, z int) bool {
	wx, wy, wz := c.p.world(x, y, z)
	return c.solidAt(wx, wy, wz)
}

// solidAt is solid for a world cell.
func (c *ctx) solidAt(wx, wy, wz int) bool { return c.w.solidAt(wx, wy, wz) }

// The writer's chunk-bounded reads and tests.

// mainAt is the main block at a world cell, air outside the chunk.
func (w *writer) mainAt(x, y, z int) uint32 {
	if !w.in(x, y, z) {
		return airRID()
	}
	return w.chunk().Block(uint8(x&15), int16(y), uint8(z&15), 0)
}

// put writes a world cell inside the chunk; noBlock and cells outside the chunk are skipped.
func (w *writer) put(x, y, z int, rid uint32) {
	if rid == noBlock || !w.in(x, y, z) {
		return
	}
	w.set(x, y, z, rid, false)
}

// interior says whether a world cell is in the chunk and below the top of its column.
func (w *writer) interior(x, y, z int) bool {
	return w.in(x, y, z) && y < w.height(x, z)
}

func (w *writer) solidAt(x, y, z int) bool {
	return w.in(x, y, z) && ridHas(w.mainAt(x, y, z), flagSolid)
}

// chunkBox is the chunk's box over the dimension's height.
func (w *writer) chunkBox() box {
	return box{w.cx * 16, w.lo, w.cz * 16, w.cx*16 + 15, w.hi, w.cz*16 + 15}
}

// liquidAt says whether a world cell is in the chunk and holds a liquid, as its main block or in its
// second layer.
func (w *writer) liquidAt(x, y, z int) bool {
	if !w.in(x, y, z) {
		return false
	}
	c := w.chunk()
	lx, lz := uint8(x&15), uint8(z&15)
	if ridHas(c.Block(lx, int16(y), lz, 0), flagLiquid) {
		return true
	}
	return c.Block(lx, int16(y), lz, 1) != airRID()
}

// replaceable is the mineshaft pillar's test: air, a liquid, glow lichen or seagrass.
func (w *writer) replaceable(x, y, z int) bool {
	rid := w.mainAt(x, y, z)
	return rid == airRID() || ridHas(rid, flagLichenOrGrass) || w.liquidAt(x, y, z)
}

// falling says whether the main block is one that falls (sand, red sand, gravel, the suspicious ones).
func (w *writer) falling(x, y, z int) bool { return ridHas(w.mainAt(x, y, z), flagFalling) }

// isLava says whether the main block is lava, still or flowing.
func (w *writer) isLava(x, y, z int) bool { return ridHas(w.mainAt(x, y, z), flagLava) }

// Block flags by runtime ID, worked out once over the registry.
const (
	flagSolid uint8 = 1 << iota
	flagLiquid
	flagLava
	flagFalling
	flagLichenOrGrass
)

var ridFlagTable = sync.OnceValue(func() []uint8 {
	registry.load()
	n := world.DefaultBlockRegistry.BlockCount()
	out := make([]uint8, n)
	for rid := range uint32(n) {
		b, ok := world.BlockByRuntimeID(rid)
		if !ok {
			continue
		}
		name, _ := b.EncodeBlock()
		name = strings.TrimPrefix(name, "minecraft:")
		var f uint8
		if solidModel(b) {
			f |= flagSolid
		}
		switch name {
		case "water", "flowing_water":
			f |= flagLiquid
		case "lava", "flowing_lava":
			f |= flagLiquid | flagLava
		case "sand", "red_sand", "gravel", "suspicious_sand", "suspicious_gravel":
			f |= flagFalling
		case "glow_lichen", "seagrass":
			f |= flagLichenOrGrass
		}
		out[rid] = f
	}
	return out
})

// solidModel says whether a block's model is a full solid cube. A few registered blocks (shulker boxes)
// cannot build their model outside a world; they are not full cubes.
func solidModel(b world.Block) (solid bool) {
	defer func() {
		if recover() != nil {
			solid = false
		}
	}()
	_, solid = b.Model().(model.Solid)
	return solid
}

func ridHas(rid uint32, f uint8) bool {
	t := ridFlagTable()
	return int(rid) < len(t) && t[rid]&f != 0
}

// Block states.

// noBlock stands for a block the registry does not have: writing it does nothing.
const noBlock = uint32(math.MaxUint32)

var stateRIDs sync.Map // string key -> uint32 (noBlock when missing)

// stateRID is the registered state with exactly this name and exactly this property set.
func stateRID(name string, props map[string]any) (uint32, bool) {
	name = strings.TrimPrefix(name, "minecraft:")
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(name)
	for _, k := range keys {
		sb.WriteString("|" + k + "=" + propString(props[k]))
	}
	key := sb.String()
	if v, ok := stateRIDs.Load(key); ok {
		rid := v.(uint32)
		return rid, rid != noBlock
	}
	registry.load()
	rid := noBlock
	for _, s := range registry.byName[name] {
		if len(s.props) != len(props) {
			continue
		}
		same := true
		for k, v := range props {
			if got, ok := s.props[k]; !ok || got != propString(v) {
				same = false
				break
			}
		}
		if same {
			rid = s.rid
			break
		}
	}
	stateRIDs.Store(key, rid)
	return rid, rid != noBlock
}

var specRIDs sync.Map // spec state string -> uint32

// specState is the runtime ID of a state written "name[key=value,...]", noBlock when the registry does
// not have that exact state.
func specState(s string) uint32 {
	if v, ok := specRIDs.Load(s); ok {
		return v.(uint32)
	}
	name, rest, _ := strings.Cut(s, "[")
	props := map[string]any{}
	if rest = strings.TrimSuffix(rest, "]"); rest != "" {
		for _, kv := range strings.Split(rest, ",") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				props[k] = v
			}
		}
	}
	rid, _ := stateRID(name, props)
	specRIDs.Store(s, rid)
	return rid
}

var nearRIDs sync.Map

// ridNamed is the registered state of a block nearest the given properties: the most properties
// matching, then the block's default state, then the lowest runtime ID. noBlock when the block is
// unknown.
func ridNamed(name string, props map[string]any) uint32 {
	name = strings.TrimPrefix(name, "minecraft:")
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	key := name
	for _, k := range keys {
		key += "|" + k + "=" + propString(props[k])
	}
	if v, ok := nearRIDs.Load(key); ok {
		return v.(uint32)
	}
	registry.load()
	best, bestN, bestDef := noBlock, -1, false
	for _, s := range registry.byName[name] {
		n := 0
		for k, v := range props {
			if s.props[k] == propString(v) {
				n++
			}
		}
		def := defaultState(s.rid)
		if n > bestN || n == bestN && def && !bestDef {
			best, bestN, bestDef = s.rid, n, def
		}
	}
	nearRIDs.Store(key, best)
	return best
}

// weirdoOf is the stairs' weirdo_direction for a world facing letter.
var weirdoOf = map[byte]int32{'E': 0, 'W': 1, 'S': 2, 'N': 3}

// stairsRID is a stairs block, not upside down, facing f ('N', 'S', 'E' or 'W', world).
func stairsRID(name string, f byte) uint32 {
	return ridNamed(name, map[string]any{"weirdo_direction": weirdoOf[f], "upside_down_bit": false, "minecraft:corner": "none"})
}

// spawnerEntity is a mob spawner's block entity.
func spawnerEntity(x, y, z int, entity string) chunk.BlockEntity {
	if !strings.Contains(entity, ":") {
		entity = "minecraft:" + entity
	}
	return chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{"id": "MobSpawner", "EntityIdentifier": entity}}
}
