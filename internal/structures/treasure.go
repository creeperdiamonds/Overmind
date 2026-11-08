package structures

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
)

// Buried treasure (N2 2.1, BuriedTreasurePiece::postProcess): one chest at the start chunk's middle
// column, buried a draw's depth under the heightmap and moved down until the cell above it and its four
// sides are solid, then on down until its own cell is (NOTES-leftovers L8).

const treasureTable = "loot_tables/chests/buriedtreasure.json" // N2 2.1.

// solid is the material test of N2 2.1 (material flag 0x40000). Which blocks carry the flag is not in
// the notes; measured on the 27 chests of the four worlds (TestBuriedTreasureMatchesBDS): full cubes
// (stone, sand, gravel ...) pass, water and air do not.
func solid(b world.Block) bool {
	_, ok := b.Model().(model.Solid)
	return ok
}

func (s *Structures) buriedTreasure(w *writer) {
	x, z := w.cx*16+8, w.cz*16+8 // Box min 3 + field 5 (N2 2.1).
	// region.height, measured: one above the highest solid block (the sea floor under water).
	h := minY
	for y := maxY; y >= minY; y-- {
		if solid(w.block(x, y, z)) {
			h = y + 1
			break
		}
	}
	n := int(w.rng.NextIntN(2))
	y := h - n - 4
	// The first search (NOTES-leftovers L8, read): the cell ABOVE the chest's and the chest cell's four
	// sides must all be solid; the chest cell itself is left to the second search.
	around := func(y int) bool {
		if !solid(w.block(x, y+1, z)) {
			return false
		}
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if !solid(w.block(x+d[0], y, z+d[1])) {
				return false
			}
		}
		return true
	}
	for y >= 6 && !around(y) {
		y--
	}
	for y >= 6 && !solid(w.block(x, y, z)) {
		y--
	}
	if y < 6 {
		return
	}
	w.chest(x, y, z, world.BlockRuntimeID(block.NewChest()), treasureTable)
}
