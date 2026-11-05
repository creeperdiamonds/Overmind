package structures

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Desert pyramids and jungle temples, from the study's own block-by-block description
// (NOTES-structures-phase2 P13.3 and P13.4, cited P133 / P134; validated there at 99.95% and 97.31% of
// cells on seeds 11 and 31). Local (x, y, z) from the piece box's corner (the start chunk's corner),
// orientation always identity; every write only inside the chunk being made.

// facing words to Bedrock's stairs weirdo_direction (Java FACING east 0, west 1, south 2, north 3).
var weirdoOf = map[byte]int32{'E': 0, 'W': 1, 'S': 2, 'N': 3}

// noBlock stands for a block Dragonfly has no state for: writes of it are skipped.
const noBlock = ^uint32(0)

func ridNamed(name string, props map[string]any) uint32 {
	r, ok := stateRID(name, props)
	if !ok {
		return noBlock
	}
	return r
}

func stairsRID(name string, f byte) uint32 {
	return ridNamed(name, map[string]any{"weirdo_direction": weirdoOf[f], "upside_down_bit": false})
}

// boxEF is BOX of P133: the box's shell with edge and its inside with fill; with skipAir only the cells
// that are not air are replaced.
func (c *codeBuilt) boxEF(x0, y0, z0, x1, y1, z1 int, edge, fill uint32, skipAir bool) {
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				b := fill
				if x == x0 || x == x1 || y == y0 || y == y1 || z == z0 || z == z1 {
					b = edge
				}
				wx, wy, wz := c.x0+x, c.y0+y, c.z0+z
				if !c.w.in(wx, wy, wz) || b == noBlock {
					continue
				}
				if skipAir && c.w.chunk().Block(uint8(wx&15), int16(wy), uint8(wz&15), 0) == airRID() {
					continue
				}
				c.w.set(wx, wy, wz, b, false)
			}
		}
	}
}

func (c *codeBuilt) inChunk(x, y, z int) bool { return c.w.in(c.x0+x, c.y0+y, c.z0+z) }

// pyramidHeight is the pyramid's y0. BDS takes it in the first chunk that post-processes the piece, so
// its runs differ (P11.2 item 2). Ours: the start chunk decides, from the generator's heights so every
// chunk agrees: the lowest first free y (the noise stage's top solid block plus one) over the piece's
// columns inside the start chunk, less the start chunk's first draw nextInt(3).
func (s *Structures) pyramidHeight(cx, cz int) int {
	low := math.MaxInt
	for x := range 16 {
		for z := range 16 {
			low = min(low, s.g.NoiseHeight(cx*16+x, cz*16+z)+1)
		}
	}
	return low - int(worldgen.NewBedrockRandom(s.g.DecorationSeed(cx, cz)).NextIntN(3))
}

// placePyramids writes the pyramid of a start in or beside the chunk (its 21 x 21 box reaches the next
// chunks in +x and +z).
func (s *Structures) placePyramids(w *writer) {
	for cx := w.cx - 1; cx <= w.cx; cx++ {
		for cz := w.cz - 1; cz <= w.cz; cz++ {
			if st, ok := s.StartIn(Scattered, cx, cz); ok && st.Piece == "desert_pyramid" {
				s.desertPyramid(w, cx, cz)
			}
		}
	}
}

func (s *Structures) desertPyramid(w *writer, cx, cz int) {
	c := &codeBuilt{w: w, x0: cx * 16, y0: s.pyramidHeight(cx, cz), z0: cz * 16}
	r := worldgen.NewBedrockRandom(s.g.DecorationSeed(w.cx, w.cz)) // This chunk's post-process random.
	r.NextIntN(3)                                                  // d, drawn in every pass (P133 1).
	air := airRID()
	ss := ridNamed("sandstone", nil)
	cut := ridNamed("cut_sandstone", nil)
	chis := ridNamed("chiseled_sandstone", nil)
	orange := ridNamed("orange_terracotta", nil)
	blue := ridNamed("blue_terracotta", nil)
	sand := ridNamed("sand", nil)
	st := func(f byte) uint32 { return stairsRID("sandstone_stairs", f) }
	box := func(x0, y0, z0, x1, y1, z1 int, e, f uint32) { c.boxEF(x0, y0, z0, x1, y1, z1, e, f, false) }
	// 2. Body.
	box(0, -4, 0, 20, 0, 20, ss, ss)
	for p := 1; p <= 9; p++ {
		box(p, p, p, 20-p, p, 20-p, ss, ss)
		box(p+1, p, p+1, 19-p, p, 19-p, air, air)
	}
	for x := range 21 {
		for z := range 21 {
			c.columnDown(x, -5, z, ss)
		}
	}
	// 3. Towers.
	for _, tx := range []int{0, 16} {
		box(tx, 0, 0, tx+4, 9, 4, ss, air)
		box(tx+1, 10, 1, tx+3, 10, 3, ss, ss)
		c.set(tx+2, 10, 0, st('N'))
		c.set(tx+2, 10, 4, st('S'))
		c.set(tx, 10, 2, st('E'))
		c.set(tx+4, 10, 2, st('W'))
	}
	// 4. Entrance and side passages.
	box(8, 0, 0, 12, 4, 4, ss, air)
	box(9, 1, 0, 11, 3, 4, air, air)
	for _, p := range [][3]int{{9, 1, 1}, {9, 2, 1}, {9, 3, 1}, {10, 3, 1}, {11, 3, 1}, {11, 2, 1}, {11, 1, 1}} {
		c.set(p[0], p[1], p[2], cut)
	}
	box(4, 1, 1, 8, 3, 3, ss, air)
	box(4, 1, 2, 8, 2, 2, air, air)
	box(12, 1, 1, 16, 3, 3, ss, air)
	box(12, 1, 2, 16, 2, 2, air, air)
	// 5. Hall.
	box(5, 4, 5, 15, 4, 15, ss, ss)
	box(9, 4, 9, 11, 4, 11, air, air)
	for _, p := range [][2]int{{8, 8}, {12, 8}, {8, 12}, {12, 12}} {
		box(p[0], 1, p[1], p[0], 3, p[1], cut, cut)
	}
	box(1, 1, 5, 4, 4, 11, ss, ss)
	box(16, 1, 5, 19, 4, 11, ss, ss)
	box(6, 7, 9, 6, 7, 11, ss, ss)
	box(14, 7, 9, 14, 7, 11, ss, ss)
	box(5, 5, 9, 5, 7, 11, cut, cut)
	box(15, 5, 9, 15, 7, 11, cut, cut)
	for _, p := range [][3]int{{5, 5, 10}, {5, 6, 10}, {6, 6, 10}, {15, 5, 10}, {15, 6, 10}, {14, 6, 10}} {
		c.set(p[0], p[1], p[2], air)
	}
	// 6. Tower stairs.
	box(2, 4, 4, 2, 6, 4, air, air)
	box(18, 4, 4, 18, 6, 4, air, air)
	for _, p := range [][3]int{{2, 4, 5}, {2, 3, 4}, {18, 4, 5}, {18, 3, 4}} {
		c.set(p[0], p[1], p[2], st('N'))
	}
	box(1, 1, 3, 2, 2, 3, ss, ss)
	box(18, 1, 3, 19, 2, 3, ss, ss)
	def := ridNamed("sandstone_stairs", map[string]any{"weirdo_direction": int32(0), "upside_down_bit": false})
	c.set(1, 1, 2, def)
	c.set(19, 1, 2, def)
	slab := ridNamed("sandstone_slab", map[string]any{"minecraft:vertical_half": "bottom"})
	c.set(1, 2, 2, slab)
	c.set(19, 2, 2, slab)
	c.set(2, 1, 2, st('W'))
	c.set(18, 1, 2, st('E'))
	// 7. Galleries.
	box(4, 3, 5, 4, 3, 17, ss, ss)
	box(16, 3, 5, 16, 3, 17, ss, ss)
	box(3, 1, 5, 4, 2, 16, air, air)
	box(15, 1, 5, 16, 2, 16, air, air)
	for z := 5; z <= 17; z += 2 {
		c.set(4, 1, z, cut)
		c.set(16, 1, z, cut)
		c.set(4, 2, z, chis)
		c.set(16, 2, z, chis)
	}
	// 8. Floor mosaic.
	for _, p := range [][2]int{{10, 7}, {10, 8}, {9, 9}, {11, 9}, {8, 10}, {12, 10}, {7, 10}, {13, 10}, {9, 11}, {11, 11}, {10, 12}, {10, 13}} {
		c.set(p[0], 0, p[1], orange)
	}
	c.set(10, 0, 10, blue)
	// 9. Tower faces and the entrance face.
	rows := [7][3]uint32{{cut, orange, cut}, {cut, orange, cut}, {orange, chis, orange}, {cut, orange, cut}, {orange, chis, orange},
		{orange, orange, orange}, {cut, cut, cut}}
	for _, x := range []int{0, 20} {
		for i, row := range rows {
			for k := range 3 {
				c.set(x, 2+i, 1+k, row[k])
			}
		}
	}
	for _, x := range []int{2, 18} {
		for i, row := range rows {
			for k := range 3 {
				c.set(x-1+k, 2+i, 0, row[k])
			}
		}
	}
	box(8, 4, 0, 12, 6, 0, cut, cut)
	c.set(8, 6, 0, air)
	c.set(12, 6, 0, air)
	c.set(9, 5, 0, orange)
	c.set(10, 5, 0, chis)
	c.set(11, 5, 0, orange)
	// 10. Treasure room.
	box(8, -14, 8, 12, -11, 12, cut, cut)
	box(8, -10, 8, 12, -10, 12, chis, chis)
	box(8, -9, 8, 12, -9, 12, cut, cut)
	box(8, -8, 8, 12, -1, 12, ss, ss)
	box(9, -11, 9, 11, -1, 11, air, air)
	c.set(10, -11, 10, ridNamed("stone_pressure_plate", nil))
	tnt := ridNamed("tnt", nil)
	box(9, -13, 9, 11, -13, 11, tnt, tnt)
	for _, p := range [][3]int{{8, -11, 10}, {8, -10, 10}, {12, -11, 10}, {12, -10, 10}, {10, -11, 8}, {10, -10, 8}, {10, -11, 12}, {10, -10, 12}} {
		c.set(p[0], p[1], p[2], air)
	}
	for _, p := range [][2]int{{7, 10}, {13, 10}} {
		c.set(p[0], -10, p[1], chis)
		c.set(p[0], -11, p[1], cut)
	}
	for _, p := range [][2]int{{10, 7}, {10, 13}} {
		c.set(p[0], -10, p[1], chis)
		c.set(p[0], -11, p[1], cut)
	}
	// 11. Chests, in the order S, W, N, E.
	chest := ridNamed("chest", nil)
	for _, d := range [][2]int{{0, 1}, {-1, 0}, {0, -1}, {1, 0}} {
		x, z := 10+2*d[0], 10+2*d[1]
		if !c.inChunk(x, -11, z) {
			continue
		}
		c.set(x, -11, z, chest)
		w.bes = append(w.bes, chestEntity(c.x0+x, c.y0-11, c.z0+z, "loot_tables/chests/desert_pyramid.json", r.NextInt()))
	}
	// 12. Cellar stairs.
	for _, p := range [][3]int{{13, -1, 17}, {14, -2, 17}, {15, -3, 17}} {
		c.set(p[0], p[1], p[2], st('W'))
	}
	v := r.NextBool()
	for x := 12; x <= 16; x++ {
		c.set(x, 0, 17, sand)
	}
	c.set(14, -1, 17, sand)
	if v {
		c.set(15, -1, 17, sand)
		c.set(16, -1, 17, ss)
	} else {
		c.set(15, -1, 17, ss)
		c.set(16, -1, 17, sand)
	}
	c.set(15, -2, 17, sand)
	c.set(16, -2, 17, ss)
	c.set(16, -3, 17, sand)
	// 13. The cellar room and its archaeology.
	const cx0, cy0, cz0 = 16, -4, 13
	x := worldgen.NewXoroshiro(int64(int32(getSeed(c.x0+cx0, c.y0+cy0, c.z0+cz0))))
	for _, y := range []int{-3, -2, -1} {
		b := cut
		if y == -2 {
			b = chis
		}
		c.boxEF(13, y, 10, 13, y, 15, b, b, true)
		c.boxEF(19, y, 10, 19, y, 15, b, b, true)
		c.boxEF(13, y, 10, 19, y, 11, b, b, true)
		c.boxEF(13, y, 16, 19, y, 16, b, b, true)
	}
	var sandList [][3]int
	placeSand := func(px, py, pz int) {
		c.set(px, py, pz, sand)
		sandList = append(sandList, [3]int{px, py, pz})
	}
	for y := -3; y <= -1; y++ {
		for px := 14; px <= 18; px++ {
			for pz := 11; pz <= 15; pz++ {
				placeSand(px, y, pz)
			}
		}
	}
	for px := 14; px <= 18; px++ {
		for pz := 11; pz <= 15; pz++ {
			if x.NextIntN(3) == 0 {
				c.set(px, 0, pz, ss)
			} else {
				c.set(px, 0, pz, sand)
			}
		}
	}
	c.set(cx0, -4, cz0, blue)
	for _, p := range [][2]int{{1, -1}, {1, 1}, {-1, -1}, {-1, 1}, {2, 0}, {-2, 0}, {0, 2}, {0, -2}} {
		c.set(cx0+p[0], -4, cz0+p[1], orange)
	}
	for _, sgn := range []int{3, -3} {
		c.set(cx0+sgn, -4, cz0, orange)
		placeSand(cx0+sgn, -3, cz0)
		placeSand(cx0+sgn, -2, cz0)
		c.set(cx0+sgn+sgn/3, -3, cz0, cut)
		c.set(cx0+sgn+sgn/3, -2, cz0, chis)
	}
	for _, sgn := range []int{3, -3} {
		c.set(cx0, -4, cz0+sgn, orange)
		placeSand(cx0, -3, cz0+sgn)
		placeSand(cx0, -2, cz0+sgn)
	}
	c.set(cx0, -3, cz0-4, cut)
	c.set(cx0, -2, cz0-4, chis)
	n := min(int(x.NextIntN(3))+5, len(sandList))
	for i := 1; i < len(sandList); i++ { // Forward shuffle (P11.2 6c).
		if j := int(x.NextIntN(int32(i + 1))); j != i {
			sandList[i], sandList[j] = sandList[j], sandList[i]
		}
	}
	sus := func(p [3]int) {
		if !c.inChunk(p[0], p[1], p[2]) {
			return // Not brushable in this pass: this chunk alone is written (see pyramidHeight).
		}
		c.set(p[0], p[1], p[2], ridNamed("suspicious_sand", map[string]any{"brushed_progress": int32(0), "hanging": false}))
		w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{c.x0 + p[0], c.y0 + p[1], c.z0 + p[2]}, Data: map[string]any{
			"id": "BrushableBlock", "LootTable": "loot_tables/entities/desert_pyramid_brushable_block.json", "LootTableSeed": x.NextInt()}})
	}
	for i := range max(n, 1) {
		sus(sandList[i])
	}
	rx, rz := int(x.NextIntN(5))+14, int(x.NextIntN(5))+11
	sus([3]int{rx, 0, rz})
}

// jungleTemple builds the temple of the start in chunk w (P134); its 12 x 15 box is inside the start
// chunk, so only the start chunk writes it.
func (s *Structures) jungleTemple(w *writer) {
	x0, z0 := w.cx*16, w.cz*16
	avg, ok := s.groundHeight(w, x0, z0, 12, 15)
	if !ok {
		return
	}
	c := &codeBuilt{w: w, x0: x0, y0: avg, z0: z0} // updateAverageGroundHeight offset 0 (P134).
	r := w.rng
	cobble := ridNamed("cobblestone", nil)
	mossy := ridNamed("mossy_cobblestone", nil)
	air := airRID()
	m := func(x0, y0, z0, x1, y1, z1 int) { // M of P134: a draw for every cell, inside the chunk or not.
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				for z := z0; z <= z1; z++ {
					b := mossy
					if r.NextFloat() < 0.4 {
						b = cobble
					}
					c.set(x, y, z, b)
				}
			}
		}
	}
	a := func(x0, y0, z0, x1, y1, z1 int) { c.boxEF(x0, y0, z0, x1, y1, z1, air, air, false) }
	st := func(f byte) uint32 { return stairsRID("stone_stairs", f) } // Bedrock's cobblestone stairs.
	// 1.
	m(0, -4, 0, 11, 0, 14)
	m(2, 1, 2, 9, 2, 2)
	m(2, 1, 12, 9, 2, 12)
	m(2, 1, 3, 2, 2, 11)
	m(9, 1, 3, 9, 2, 11)
	m(1, 3, 1, 10, 6, 1)
	m(1, 3, 13, 10, 6, 13)
	m(1, 3, 2, 1, 6, 12)
	m(10, 3, 2, 10, 6, 12)
	m(2, 3, 2, 9, 3, 12)
	m(2, 6, 2, 9, 6, 12)
	m(3, 7, 3, 8, 7, 11)
	m(4, 8, 4, 7, 8, 10)
	// 2.
	a(3, 1, 3, 8, 2, 11)
	a(4, 3, 6, 7, 3, 9)
	a(2, 4, 2, 9, 5, 12)
	a(4, 6, 5, 7, 6, 9)
	a(5, 7, 6, 6, 7, 8)
	a(5, 1, 2, 6, 2, 2)
	a(5, 2, 12, 6, 2, 12)
	a(5, 5, 1, 6, 5, 1)
	a(5, 5, 13, 6, 5, 13)
	for _, p := range [][3]int{{1, 5, 5}, {10, 5, 5}, {1, 5, 9}, {10, 5, 9}} {
		c.set(p[0], p[1], p[2], air)
	}
	// 3.
	for _, z := range []int{0, 14} {
		for _, x := range []int{2, 4, 7, 9} {
			m(x, 4, z, x, 5, z)
		}
	}
	m(5, 6, 0, 6, 6, 0)
	for _, x := range []int{0, 11} {
		for z := 2; z <= 12; z += 2 {
			m(x, 4, z, x, 5, z)
		}
		m(x, 6, 5, x, 6, 5)
		m(x, 6, 9, x, 6, 9)
	}
	// 4. Roof.
	for _, p := range [][2]int{{2, 2}, {9, 2}, {2, 12}, {9, 12}} {
		m(p[0], 7, p[1], p[0], 9, p[1])
	}
	for _, p := range [][2]int{{4, 4}, {7, 4}, {4, 10}, {7, 10}} {
		m(p[0], 9, p[1], p[0], 9, p[1])
	}
	m(5, 9, 7, 6, 9, 7)
	c.set(5, 9, 6, st('N'))
	c.set(6, 9, 6, st('N'))
	c.set(5, 9, 8, st('S'))
	c.set(6, 9, 8, st('S'))
	// 5. Steps.
	for x := 4; x <= 7; x++ {
		c.set(x, 0, 0, st('N'))
	}
	for _, p := range [][3]int{{4, 1, 8}, {4, 2, 9}, {4, 3, 10}, {7, 1, 8}, {7, 2, 9}, {7, 3, 10}} {
		c.set(p[0], p[1], p[2], st('N'))
	}
	m(4, 1, 9, 4, 1, 9)
	m(7, 1, 9, 7, 1, 9)
	m(4, 1, 10, 7, 2, 10)
	m(5, 4, 5, 6, 4, 5)
	c.set(4, 4, 5, st('E'))
	c.set(7, 4, 5, st('W'))
	// 6. Down stairs.
	for i := range 4 {
		c.set(5, -i, 6+i, st('S'))
		c.set(6, -i, 6+i, st('S'))
		a(5, -i, 7+i, 6, -i, 9+i)
	}
	// 7. Basement.
	a(1, -3, 12, 10, -1, 13)
	a(1, -3, 1, 3, -1, 13)
	a(1, -3, 1, 9, -1, 5)
	for z := 1; z <= 13; z += 2 {
		m(1, -3, z, 1, -2, z)
	}
	for z := 2; z <= 12; z += 2 {
		m(1, -1, z, 3, -1, z)
	}
	m(2, -2, 1, 5, -2, 1)
	m(7, -2, 1, 9, -2, 1)
	m(6, -3, 1, 6, -3, 1)
	m(6, -1, 1, 6, -1, 1)
	wire := ridNamed("redstone_wire", nil)
	hook := func(dir int32) uint32 {
		return ridNamed("tripwire_hook", map[string]any{"direction": dir, "attached_bit": true})
	}
	trip := ridNamed("trip_wire", map[string]any{"attached_bit": true})
	container := func(x, y, z int, name string, props map[string]any, table, id string) {
		seed := r.NextInt()
		if rid, ok := stateRID(name, props); ok {
			c.set(x, y, z, rid)
			if name == "chest" {
				w.bes = append(w.bes, chestEntity(c.x0+x, c.y0+y, c.z0+z, table, seed))
			} else {
				w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{c.x0 + x, c.y0 + y, c.z0 + z}, Data: map[string]any{"id": id,
					"LootTable": table, "LootTableSeed": seed}})
			}
		}
	}
	// 8. Trap 1.
	c.set(1, -3, 8, hook(3))
	c.set(4, -3, 8, hook(1))
	c.set(2, -3, 8, trip)
	c.set(3, -3, 8, trip)
	for z := 7; z >= 1; z-- {
		c.set(5, -3, z, wire)
	}
	c.set(4, -3, 1, wire)
	c.set(3, -3, 1, mossy)
	container(3, -2, 1, "dispenser", map[string]any{"facing_direction": int32(2)}, "loot_tables/chests/jungle_temple_dispenser.json", "Dispenser")
	c.set(3, -2, 2, ridNamed("vine", map[string]any{"vine_direction_bits": int32(1)}))
	// 9. Trap 2.
	c.set(7, -3, 1, hook(2))
	c.set(7, -3, 5, hook(0))
	for z := 2; z <= 4; z++ {
		c.set(7, -3, z, trip)
	}
	c.set(8, -3, 6, wire)
	c.set(9, -3, 6, wire)
	c.set(9, -3, 5, wire)
	c.set(9, -3, 4, mossy)
	c.set(9, -2, 4, wire)
	container(9, -2, 3, "dispenser", map[string]any{"facing_direction": int32(4)}, "loot_tables/chests/jungle_temple_dispenser.json", "Dispenser")
	vineE := ridNamed("vine", map[string]any{"vine_direction_bits": int32(8)})
	c.set(8, -1, 3, vineE)
	c.set(8, -2, 3, vineE)
	// 10. Main chest.
	container(8, -3, 3, "chest", nil, "loot_tables/chests/jungle_temple.json", "Chest")
	for _, p := range [][3]int{{9, -3, 2}, {8, -3, 1}, {4, -3, 5}, {5, -2, 5}, {5, -1, 5}, {6, -3, 5}, {7, -2, 5}, {7, -1, 5}, {8, -3, 5}} {
		c.set(p[0], p[1], p[2], mossy)
	}
	m(9, -1, 1, 9, -1, 5)
	// 11. Hidden room.
	a(8, -3, 8, 10, -1, 10)
	csb := ridNamed("chiseled_stone_bricks", nil)
	lever := ridNamed("lever", map[string]any{"lever_direction": "north"})
	for x := 8; x <= 10; x++ {
		c.set(x, -2, 11, csb)
		c.set(x, -2, 12, lever)
	}
	m(8, -3, 8, 8, -3, 10)
	m(10, -3, 8, 10, -3, 10)
	c.set(10, -2, 9, mossy)
	c.set(8, -2, 9, wire)
	c.set(8, -2, 10, wire)
	c.set(10, -1, 9, wire)
	c.set(9, -2, 8, ridNamed("sticky_piston", map[string]any{"facing_direction": int32(1)}))
	sp := ridNamed("sticky_piston", map[string]any{"facing_direction": int32(4)})
	c.set(10, -2, 8, sp)
	c.set(10, -1, 8, sp)
	c.set(10, -2, 10, ridNamed("unpowered_repeater", map[string]any{"minecraft:cardinal_direction": "north"}))
	container(9, -3, 10, "chest", nil, "loot_tables/chests/jungle_temple.json", "Chest")
}
