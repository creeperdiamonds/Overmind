package structures

import (
	"fmt"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// The witch hut (SwamplandHut postProcess 0x5381aa0), read block for block: NOTES-structures-phase2
// P11.1 (cited P11 below), checked there against seed 5's hut in all four BDS runs. A scattered piece:
// 7 x 7 x 9 at the start chunk's corner, orientation 0 (local x, z -> box x0 + x, box z0 + z), built by
// the start chunk's post-process with its random (P9.1).

const hutSeaLevel = 63 // The dimension's +0xf0 value the average ground height takes at least (P11).

// codeBuilt is a code-built piece being written into a chunk.
type codeBuilt struct {
	w      *writer
	x0, y0 int
	z0     int
}

func (c *codeBuilt) set(x, y, z int, rid uint32) {
	wx, wy, wz := c.x0+x, c.y0+y, c.z0+z
	if c.w.in(wx, wy, wz) && rid != noBlock {
		c.w.set(wx, wy, wz, rid, false)
	}
}

// box is generateBox (vt+0x60, P11): every cell, or with skipAir only the cells that are not air.
func (c *codeBuilt) box(x0, y0, z0, x1, y1, z1 int, rid uint32, skipAir bool) {
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				wx, wy, wz := c.x0+x, c.y0+y, c.z0+z
				if !c.w.in(wx, wy, wz) {
					continue
				}
				if skipAir && c.w.c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0) == airRID() {
					continue
				}
				c.w.set(wx, wy, wz, rid, false)
			}
		}
	}
}

// columnDown is fillColumnDown (0xd97f560, P11): from y down while the cell is air or a liquid.
// A grass block the column ends on becomes dirt (P11: "the grass_block directly under each post bottom
// became dirt", a side effect of placing logs on grass in BDS).
func (c *codeBuilt) columnDown(x, y, z int, rid uint32) {
	for wy := c.y0 + y; wy > minY; wy-- {
		wx, wz := c.x0+x, c.z0+z
		if !c.w.in(wx, wy, wz) {
			return
		}
		b := c.w.block(wx, wy, wz)
		if _, liquid := b.(world.Liquid); b != nil && c.w.c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0) != airRID() && !liquid {
			if n, _ := b.EncodeBlock(); n == "minecraft:grass_block" {
				if dirt, ok := stateRID("dirt", nil); ok {
					c.w.set(wx, wy, wz, dirt, false)
				}
			}
			return
		}
		c.w.set(wx, wy, wz, rid, false)
	}
}

var stateCache sync.Map

// stateRID is a Bedrock block in given states, the nearest registered state (as javablocks does);
// ok is false when Dragonfly has no such block.
func stateRID(name string, props map[string]any) (uint32, bool) {
	key := name + fmt.Sprint(props)
	if v, ok := stateCache.Load(key); ok {
		r := v.([2]uint32)
		return r[0], r[1] == 1
	}
	registry.load()
	best, bestN := -1, -1
	states := registry.byName[strings.TrimPrefix(name, "minecraft:")]
	for i, s := range states {
		n := 0
		for k, v := range props {
			if s.props[k] == propString(v) {
				n++
			}
		}
		if n > bestN {
			best, bestN = i, n
		}
	}
	var r [2]uint32
	if best >= 0 {
		r = [2]uint32{states[best].rid, 1}
	}
	stateCache.Store(key, r)
	return r[0], r[1] == 1
}

// groundHeight is ScatteredFeaturePiece::updateAverageGroundHeight (0x5375df0, P11): the mean, over the
// piece's columns inside the chunk, of max(first free y above the ground, sea level); false when no
// column is in the chunk. The ground is the highest full block or liquid (not plants or leaves): what
// "first free y above the ground" counts was not read beyond the hut on grass.
func (s *Structures) groundHeight(w *writer, x0, z0, sx, sz int) (int, bool) {
	sum, n := 0, 0
	for x := x0; x < x0+sx; x++ {
		for z := z0; z < z0+sz; z++ {
			if x>>4 != w.cx || z>>4 != w.cz {
				continue
			}
			h := minY
			for y := maxY; y > minY; y-- {
				b := w.block(x, y, z)
				if b == nil {
					continue
				}
				_, solid := b.Model().(model.Solid)
				_, liquid := b.(world.Liquid)
				if solid && treeBlock(b) {
					continue // BDS builds the piece before the trees grow; a trunk is not the ground.
				}
				if solid || liquid {
					h = y + 1
					break
				}
			}
			sum += max(h, hutSeaLevel)
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / n, true
}

// treeBlock says whether a block is a tree's trunk or wood, which our chunk already has when its
// structures are placed and BDS's does not.
func treeBlock(b world.Block) bool {
	n, _ := b.EncodeBlock()
	return strings.HasSuffix(n, "_log") || strings.HasSuffix(n, "_wood") || strings.HasSuffix(n, "_stem")
}

// witchHut builds the hut of the start in chunk w (P11).
func (s *Structures) witchHut(w *writer) {
	x0, z0 := w.cx*16, w.cz*16
	avg, ok := s.groundHeight(w, x0, z0, 7, 9)
	if !ok {
		return
	}
	c := &codeBuilt{w: w, x0: x0, y0: avg + 1, z0: z0} // Offset 1 for the hut (P11).
	rid := func(name string, props map[string]any) uint32 {
		r, _ := stateRID(name, props)
		return r
	}
	air := airRID()
	spruce := rid("spruce_planks", nil)
	planks := spruce // P11's "planks": spruce in the seed 5 world (TestWitchHutMatchesBDS).
	logY := rid("oak_log", map[string]any{"pillar_axis": "y"})
	fence := rid("oak_fence", nil)
	c.box(0, 2, 0, 6, 3, 8, air, true)
	c.box(1, 1, 1, 5, 1, 7, spruce, false)
	c.box(1, 5, 2, 5, 5, 7, spruce, false)
	c.box(2, 1, 0, 4, 1, 0, spruce, false)
	c.box(2, 2, 2, 3, 4, 2, planks, false)
	c.box(1, 2, 3, 1, 4, 6, planks, false)
	c.box(5, 2, 3, 5, 4, 6, planks, false)
	c.box(2, 2, 7, 4, 4, 7, planks, false)
	for _, p := range [][2]int{{1, 2}, {5, 2}, {1, 7}, {5, 7}} {
		c.box(p[0], 0, p[1], p[0], 4, p[1], logY, false)
	}
	c.set(2, 3, 2, fence)
	c.set(3, 3, 7, fence)
	c.set(1, 3, 4, air)
	c.set(5, 3, 4, air)
	c.set(5, 3, 5, air)
	if pot, ok := stateRID("flower_pot", nil); ok {
		c.set(1, 3, 5, pot)
		if wx, wy, wz := c.x0+1, c.y0+3, c.z0+5; w.in(wx, wy, wz) { // P11: PlantBlock = red_mushroom.
			w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{wx, wy, wz}, Data: map[string]any{"id": "FlowerPot",
				"PlantBlock": map[string]any{"name": "minecraft:red_mushroom", "states": map[string]any{}}}})
		}
	}
	c.set(4, 4, 2, spruce)
	// The cauldron's two draws (P11: fill level, potion) are made whether or not Dragonfly has the block.
	fill := int32(w.rng.NextIntN(3))*2 + 2
	w.rng.NextIntN(100)
	if cauldron, ok := stateRID("cauldron", map[string]any{"fill_level": fill, "cauldron_liquid": "water"}); ok {
		c.set(4, 2, 6, cauldron)
	}
	c.set(3, 2, 6, rid("crafting_table", nil))
	c.set(1, 2, 1, fence)
	c.set(5, 2, 1, fence)
	stairs := func(dir int32) uint32 {
		return rid("spruce_stairs", map[string]any{"weirdo_direction": dir, "upside_down_bit": false})
	}
	for x := 0; x <= 6; x++ {
		c.set(x, 5, 1, stairs(2))
		c.set(x, 5, 8, stairs(3))
	}
	for z := 2; z <= 7; z++ {
		c.set(0, 5, z, stairs(0))
		c.set(6, 5, z, stairs(1))
	}
	for _, p := range [][2]int{{1, 2}, {5, 2}, {1, 7}, {5, 7}} {
		c.columnDown(p[0], -1, p[1], logY)
	}
}
