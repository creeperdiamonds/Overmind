package structures

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// The scattered feature's code-built pieces: the desert pyramid, the jungle temple and the witch hut
// (clean-room spec desert_pyramid/, jungle_temple/, witch_hut/). Each is an op list in the frame of its
// start chunk's corner, orientation identity. Igloos are igloo.go.

const (
	pyramidChestTable  = "loot_tables/chests/desert_pyramid.json"
	pyramidSandTable   = "loot_tables/entities/desert_pyramid_brushable_block.json"
	scatteredGroundMin = 63
)

// placeScattered builds the scattered feature's pieces that reach the chunk: the hut or temple of a
// start in the chunk, igloos and desert pyramids of starts beside it.
func (s *Structures) placeScattered(w *writer) {
	if st, ok := s.StartIn(Scattered, w.cx, w.cz); ok {
		switch st.Piece {
		case "witch_hut":
			s.witchHut(w)
		case "jungle_temple":
			s.jungleTemple(w)
		}
	}
	s.placeIgloos(w)
	s.placePyramids(w)
}

// groundHeight is the average ground height over nx x nz columns from (x0, z0), all in the chunk: in
// each column one above the highest liquid or full solid block that is not a log, wood or stem, at
// least 63.
func groundHeight(w *writer, x0, z0, nx, nz int) int {
	sum := 0
	for x := x0; x < x0+nx; x++ {
		for z := z0; z < z0+nz; z++ {
			h := minY
			for y := min(maxY, w.height(x, z)); y >= minY+1; y-- {
				rid := w.mainAt(x, y, z)
				if ridHas(rid, flagLiquid) {
					h = y + 1
					break
				}
				if ridHas(rid, flagSolid) {
					if n := ridName(rid); !strings.HasSuffix(n, "_log") && !strings.HasSuffix(n, "_wood") && !strings.HasSuffix(n, "_stem") {
						h = y + 1
						break
					}
				}
			}
			sum += max(h, scatteredGroundMin)
		}
	}
	return sum / (nx * nz)
}

// cornerPiece is a scattered piece's frame: the start chunk's corner at height y0.
func cornerPiece(cx, cz, y0 int) *opiece {
	return &opiece{b: box{cx * 16, y0, cz * 16, cx*16 + 15, y0, cz*16 + 15}, o: oS}
}

// witchHut builds the hut whose start is the chunk.
func (s *Structures) witchHut(w *writer) {
	x0, z0 := w.cx*16, w.cz*16
	y0 := groundHeight(w, x0, z0, 7, 9) + 1
	c := &ctx{w: w, p: cornerPiece(w.cx, w.cz, y0), r: w.rng}
	runOps(c, crData().hut, nil, func(c *ctx, op *crOp) bool {
		switch op.Op {
		case "flower_pot_entity":
			wx, wy, wz := c.p.world(op.At[0], op.At[1], op.At[2])
			if w.in(wx, wy, wz) {
				w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{wx, wy, wz}, Data: map[string]any{"id": "FlowerPot",
					"PlantBlock": map[string]any{"name": "minecraft:" + op.Plant, "states": map[string]any{}}}})
			}
		case "cauldron":
			fill := c.r.NextIntN(3)*2 + 2
			c.r.NextIntN(100) // BDS's potion; no cauldron entity is written.
			rid, _ := stateRID("cauldron", map[string]any{"cauldron_liquid": "water", "fill_level": fill})
			c.set(op.At[0], op.At[1], op.At[2], rid)
		default:
			return false
		}
		return true
	})
}

// jungleTemple builds the temple whose start is the chunk.
func (s *Structures) jungleTemple(w *writer) {
	x0, z0 := w.cx*16, w.cz*16
	y0 := groundHeight(w, x0, z0, 12, 15)
	c := &ctx{w: w, p: cornerPiece(w.cx, w.cz, y0), r: w.rng}
	cobble, mossy := specState("cobblestone"), specState("mossy_cobblestone")
	runOps(c, crData().jungle, nil, func(c *ctx, op *crOp) bool {
		switch op.Op {
		case "mossy_box":
			for y := op.From[1]; y <= op.To[1]; y++ {
				for x := op.From[0]; x <= op.To[0]; x++ {
					for z := op.From[2]; z <= op.To[2]; z++ {
						if c.r.NextFloat() < 0.4 {
							c.set(x, y, z, cobble)
						} else {
							c.set(x, y, z, mossy)
						}
					}
				}
			}
		case "container":
			seed := c.r.NextInt()
			wx, wy, wz := c.p.world(op.At[0], op.At[1], op.At[2])
			if w.in(wx, wy, wz) {
				w.put(wx, wy, wz, specState(op.Block))
				w.bes = append(w.bes, lootEntity(op.EntityID, wx, wy, wz, op.Loot, seed))
			}
		default:
			return false
		}
		return true
	})
}

// placePyramids builds every desert pyramid whose start is the chunk or its -x, -z or -x-z neighbour.
func (s *Structures) placePyramids(w *writer) {
	for cx := w.cx - 1; cx <= w.cx; cx++ {
		for cz := w.cz - 1; cz <= w.cz; cz++ {
			if st, ok := s.StartIn(Scattered, cx, cz); ok && st.Piece == "desert_pyramid" {
				s.desertPyramid(w, cx, cz, s.pyramidHeight(cx, cz))
			}
		}
	}
}

// pyramidHeight is the pyramid's base: the lowest ground of its start chunk (the noise stage's top solid
// block + 1), less int(3) of the start chunk's stream.
func (s *Structures) pyramidHeight(cx, cz int) int {
	return s.pyramids.get([2]int{cx, cz}, func() any {
		lo := math.MaxInt
		for x := cx * 16; x < cx*16+16; x++ {
			for z := cz * 16; z < cz*16+16; z++ {
				lo = min(lo, s.g.NoiseHeight(x, z)+1)
			}
		}
		return lo - int(worldgen.NewBedrockRandom(s.g.DecorationSeed(cx, cz)).NextIntN(3))
	}).(int)
}

// posSeed is the position hash the pyramid's archaeology random is seeded from.
func posSeed(x, y, z int) int64 {
	s := int64(int32(x)*3129871) ^ int64(z)*116129781 ^ int64(y)
	s = s*s*42317861 + s*11
	return s >> 16
}

// desertPyramid builds the pyramid of start (cx, cz) at base y0 into the chunk.
func (s *Structures) desertPyramid(w *writer, cx, cz, y0 int) {
	p := worldgen.NewBedrockRandom(s.g.DecorationSeed(w.cx, w.cz))
	p.NextIntN(3)
	c := &ctx{w: w, p: cornerPiece(cx, cz, y0), r: p}
	misc := crData().misc
	var (
		cellar bool
		x      *worldgen.Xoroshiro
		sand   [][3]int
	)
	sandRID, sandstone := specState("sand"), specState("sandstone")
	runOps(c, crData().pyramid, nil, func(c *ctx, op *crOp) bool {
		switch op.Op {
		case "chests_in_order":
			for _, cell := range op.Cells {
				wx, wy, wz := c.p.world(cell[0], cell[1], cell[2])
				if !w.in(wx, wy, wz) {
					continue
				}
				w.put(wx, wy, wz, specState(op.Block))
				w.bes = append(w.bes, lootEntity("Chest", wx, wy, wz, pyramidChestTable, p.NextInt()))
			}
		case "cellar_variant":
			cellar = p.NextBool()
		case "set_by_variant":
			b := op.IfFalse
			if cellar {
				b = op.IfTrue
			}
			c.set(op.At[0], op.At[1], op.At[2], specState(b))
		case "archaeology_begin":
			wx, wy, wz := c.p.world(op.Centre[0], op.Centre[1], op.Centre[2])
			x = worldgen.NewXoroshiro(int64(int32(posSeed(wx, wy, wz))))
			sand = sand[:0]
		case "listed_sand":
			c.set(op.At[0], op.At[1], op.At[2], sandRID)
			sand = append(sand, op.At)
		case "roof_choice":
			if x.NextIntN(3) == 0 {
				c.set(op.At[0], op.At[1], op.At[2], sandstone)
			} else {
				c.set(op.At[0], op.At[1], op.At[2], sandRID)
			}
		case "suspicious_pass":
			n := min(int(x.NextIntN(3))+5, len(sand))
			for i := 1; i < len(sand); i++ {
				if j := int(x.NextIntN(int32(i + 1))); j != i {
					sand[i], sand[j] = sand[j], sand[i]
				}
			}
			sus := specState(misc["suspicious_sand"])
			mark := func(cell [3]int) {
				wx, wy, wz := c.p.world(cell[0], cell[1], cell[2])
				if !w.in(wx, wy, wz) {
					return
				}
				w.put(wx, wy, wz, sus)
				w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{wx, wy, wz}, Data: map[string]any{"id": "BrushableBlock",
					"LootTable": pyramidSandTable, "LootTableSeed": x.NextInt()}})
			}
			for _, cell := range sand[:max(n, 1)] {
				mark(cell)
			}
			rx := int(x.NextIntN(5)) + 14
			rz := int(x.NextIntN(5)) + 11
			mark([3]int{rx, 0, rz})
		default:
			return false
		}
		return true
	})
}
