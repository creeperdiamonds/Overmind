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

// The helpers shared by every code-built structure (mineshafts, strongholds, the scattered pieces, the
// monument, the fortress and the mansion) live in this file: a piece's local frame, the oriented boxes
// new pieces are placed with, the chunk-bounded writes and tests, the block state lookups, and the
// block entities they write. Their embedded tables and the op-list runner are pieceops.go. Written from
// the clean-room spec (piece_helpers/SPEC.md, INTERFACE.md).

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

// world maps local coordinates to world coordinates (the frame table): south keeps both axes, north
// counts z down from the box's far side, west and east run local x along world z and local z along world
// x (west from the box's east side, east from its west side).
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

// span is the world box spanned by two local corners.
func (p *opiece) span(a, c [3]int) box {
	x0, y0, z0 := p.world(a[0], a[1], a[2])
	x1, y1, z1 := p.world(c[0], c[1], c[2])
	return box{min(x0, x1), min(y0, y1), min(z0, z1), max(x0, x1), max(y0, y1), max(z0, z1)}
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
// chunk, random draws from r. With keep set, set, box, maybeBox and maybe leave a cell alone when keep
// says its block is protected (mineshafts, mineshaft/ADDENDUM-1.md rule F); their draws still happen.
type ctx struct {
	w    *writer
	p    *opiece
	r    *worldgen.BedrockRandom
	keep func(rid uint32) bool
}

// put writes a world cell inside the chunk unless keep protects what it holds.
func (c *ctx) put(wx, wy, wz int, rid uint32) {
	if c.keep != nil && c.w.in(wx, wy, wz) && c.keep(c.w.mainAt(wx, wy, wz)) {
		return
	}
	c.w.put(wx, wy, wz, rid)
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

// set writes a local cell; outside the chunk, a block the registry lacks, or a cell keep protects,
// nothing happens.
func (c *ctx) set(x, y, z int, rid uint32) {
	wx, wy, wz := c.p.world(x, y, z)
	c.put(wx, wy, wz, rid)
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
				c.put(wx, wy, wz, rid)
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
				c.put(wx, wy, wz, rid)
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
	return c.w.solidAt(wx, wy, wz)
}

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

// clip intersects a world box with the chunk's box; ok is false when they do not meet.
func (w *writer) clip(b box) (box, bool) {
	c := w.chunkBox()
	out := box{max(b[0], c[0]), max(b[1], c[1]), max(b[2], c[2]), min(b[3], c[3]), min(b[4], c[4]), min(b[5], c[5])}
	return out, out[0] <= out[3] && out[1] <= out[4] && out[2] <= out[5]
}

// liquidAt says whether a world cell is in the chunk and holds a liquid: water or lava as its main
// block, or anything in its second layer.
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

// replaceable is the support columns' test (mineshaft pillars, fortress and monument columns): air, a
// liquid, glow lichen or seagrass.
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

// Block states. The piece tables write states in two formats, each with one parser: "name[k=v,...]"
// (specState, exact) everywhere, and "name k=v ..." (paletteState, the fortress palettes). Both resolve
// through findState.

// noBlock stands for a block the registry does not have: writing it does nothing.
const noBlock = uint32(math.MaxUint32)

// findState is the registered state of a block holding the given properties (values as the registry
// prints them: booleans 0 or 1). With exact, the state must have exactly those properties; without, it is
// the first registered state holding every given value, or with no properties the block's default state
// (ridOfName). noBlock when there is none.
func findState(name string, props map[string]string, exact bool) uint32 {
	name = strings.TrimPrefix(name, "minecraft:")
	if !exact && len(props) == 0 {
		if rid, ok := ridOfName(name); ok {
			return rid
		}
		return noBlock
	}
	registry.load()
states:
	for _, s := range registry.byName[name] {
		if exact && len(s.props) != len(props) {
			continue
		}
		for k, v := range props {
			if got, ok := s.props[k]; !ok || got != v {
				continue states
			}
		}
		return s.rid
	}
	return noBlock
}

var specRIDs sync.Map // spec state string -> uint32

// specState is the runtime ID of a state written "name[key=value,...]", noBlock when the registry does
// not have that exact state.
func specState(s string) uint32 {
	if v, ok := specRIDs.Load(s); ok {
		return v.(uint32)
	}
	name, rest, _ := strings.Cut(s, "[")
	props := map[string]string{}
	if rest = strings.TrimSuffix(rest, "]"); rest != "" {
		for _, kv := range strings.Split(rest, ",") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				props[k] = v
			}
		}
	}
	rid := findState(name, props, true)
	specRIDs.Store(s, rid)
	return rid
}

// mustState is specState for a state the code or an embedded table names, which must be registered:
// a missing one is a broken build, not a hole in the world.
func mustState(s string) uint32 {
	rid := specState(s)
	if rid == noBlock {
		panic("structures: block state " + s + " is not registered")
	}
	return rid
}

// paletteState is the runtime ID of a state written "name k=v k=v ...", the fortress palettes' format:
// the first registered state of that name holding every given value, or the name's default state
// without properties. noBlock when there is none. Only the table loader calls it.
func paletteState(s string) uint32 {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return noBlock
	}
	props := map[string]string{}
	for _, kv := range fields[1:] {
		k, v, _ := strings.Cut(kv, "=")
		props[k] = v
	}
	return findState(fields[0], props, false)
}

var nearRIDs sync.Map

// ridNamed is the registered state of a block nearest the given properties: the most properties
// matching, then the block's default state, then the lowest runtime ID. noBlock when the block is
// unknown. The template-built structures (ruined portals, ocean ruins, end cities) use it for states
// they work out in code; it is a different rule from findState's, so it keeps its own loop.
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

// Block entities.

// spawnerEntity is a mob spawner's block entity.
func spawnerEntity(x, y, z int, entity string) chunk.BlockEntity {
	if !strings.Contains(entity, ":") {
		entity = "minecraft:" + entity
	}
	return chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{"id": "MobSpawner", "EntityIdentifier": entity}}
}

// lootEntity is a container's block entity (id "Chest", "Barrel", "BrushableBlock", ...) with a loot
// table and seed.
func lootEntity(id string, x, y, z int, table string, seed int32) chunk.BlockEntity {
	return chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{"id": id, "LootTable": table, "LootTableSeed": seed}}
}
