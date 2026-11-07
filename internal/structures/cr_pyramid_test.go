package structures

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// TestPyramidEqualsOpsTable: the desert pyramid the generator builds equals, cell for cell and block entity
// for block entity, a plain reading of the spec's op table (desert_pyramid/ops.json) done here on its own:
// seed 11's pyramid forced at (8,-16) (not a start on our generator, see the spec) and seed 100's three
// real starts. Each of the four chunks a pyramid reaches is checked on its base terrain.
func TestPyramidEqualsOpsTable(t *testing.T) {
	for _, c := range []struct {
		seed   int64
		cx, cz int
		forced bool
	}{{11, 8, -16, true}, {100, -146, 143, false}, {100, -125, 176, false}, {100, -127, 203, false}} {
		g, s := worldFor(t, c.seed)
		if !c.forced {
			if st, ok := s.StartIn(Scattered, c.cx, c.cz); !ok || st.Piece != "desert_pyramid" {
				t.Fatalf("seed %d (%d,%d) is not a desert pyramid start", c.seed, c.cx, c.cz)
			}
		}
		y0 := s.pyramidHeight(c.cx, c.cz)
		cells, bes := 0, 0
		for wx := c.cx; wx <= c.cx+1; wx++ {
			for wz := c.cz; wz <= c.cz+1; wz++ {
				ours := baseChunk(g, wx, wz)
				w := &writer{cx: wx, cz: wz, c: ours, lo: minY, hi: maxY, spawned: map[spiderKey]bool{}}
				s.desertPyramid(w, c.cx, c.cz, y0)
				ref := baseChunk(g, wx, wz)
				refBEs := refPyramid(t, g, ref, wx, wz, c.cx, c.cz)
				n := 0
				for x := range 16 {
					for z := range 16 {
						for y := minY; y <= maxY; y++ {
							for layer := uint8(0); layer < 2; layer++ {
								a := ours.Block(uint8(x), int16(y), uint8(z), layer)
								b := ref.Block(uint8(x), int16(y), uint8(z), layer)
								if a != b {
									n++
									if n <= 5 {
										t.Errorf("seed %d pyramid (%d,%d) chunk (%d,%d): (%d,%d,%d) layer %d: ours %s, table %s", c.seed, c.cx, c.cz,
											wx, wz, wx*16+x, y, wz*16+z, layer, ridName(a), ridName(b))
									}
								}
							}
						}
					}
				}
				cells += 16 * 16 * (maxY - minY + 1)
				if !reflect.DeepEqual(beKeys(w.bes), beKeys(refBEs)) {
					t.Errorf("seed %d pyramid (%d,%d) chunk (%d,%d): block entities\n ours %v\n table %v", c.seed, c.cx, c.cz, wx, wz,
						beKeys(w.bes), beKeys(refBEs))
				}
				bes += len(refBEs)
			}
		}
		t.Logf("seed %d pyramid (%d,%d) at y %d: %d cells of its four chunks and %d block entities checked", c.seed, c.cx, c.cz, y0, cells, bes)
	}
}

func baseChunk(g *worldgen.Generator, cx, cz int) *chunk.Chunk {
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	g.GenerateBase(cx, cz, c)
	return c
}

func beKeys(bes []chunk.BlockEntity) []string {
	var out []string
	for _, b := range bes {
		out = append(out, fmt.Sprint(b.Pos, b.Data))
	}
	sort.Strings(out)
	return out
}

// refPyramid writes the pyramid of start (sx, sz) into chunk (cx, cz) as the op table reads, on its own.
func refPyramid(t *testing.T, g *worldgen.Generator, c *chunk.Chunk, cx, cz, sx, sz int) []chunk.BlockEntity {
	ops := crData().pyramid
	lo := math.MaxInt
	for x := sx * 16; x < sx*16+16; x++ {
		for z := sz * 16; z < sz*16+16; z++ {
			lo = min(lo, g.NoiseHeight(x, z)+1)
		}
	}
	y0 := lo - int(worldgen.NewBedrockRandom(g.DecorationSeed(sx, sz)).NextIntN(3))
	at := func(p [3]int) (int, int, int) { return sx*16 + p[0], y0 + p[1], sz*16 + p[2] }
	inside := func(x, y, z int) bool { return x>>4 == cx && z>>4 == cz && y >= minY && y <= maxY }
	air := airRID()
	get := func(x, y, z int) uint32 {
		if !inside(x, y, z) {
			return air
		}
		return c.Block(uint8(x&15), int16(y), uint8(z&15), 0)
	}
	put := func(x, y, z int, state string) {
		rid := specState(state)
		if rid == noBlock {
			t.Fatalf("state %q is not registered", state)
		}
		if inside(x, y, z) {
			c.SetBlock(uint8(x&15), int16(y), uint8(z&15), 0, rid)
			c.SetBlock(uint8(x&15), int16(y), uint8(z&15), 1, air)
		}
	}
	var bes []chunk.BlockEntity
	P := worldgen.NewBedrockRandom(g.DecorationSeed(cx, cz))
	P.NextIntN(3)
	var X *worldgen.Xoroshiro
	var list [][3]int
	variant := false
	for _, op := range ops {
		switch op.Op {
		case "fill":
			for x := op.From[0]; x <= op.To[0]; x++ {
				for y := op.From[1]; y <= op.To[1]; y++ {
					for z := op.From[2]; z <= op.To[2]; z++ {
						wx, wy, wz := at([3]int{x, y, z})
						if op.OnlyNonAir && get(wx, wy, wz) == air {
							continue
						}
						shell := x == op.From[0] || x == op.To[0] || y == op.From[1] || y == op.To[1] || z == op.From[2] || z == op.To[2]
						if !shell && op.Inner != "" {
							put(wx, wy, wz, op.Inner)
						} else {
							put(wx, wy, wz, op.Block)
						}
					}
				}
			}
		case "set":
			x, y, z := at(op.At)
			put(x, y, z, op.Block)
		case "column_down_each":
			for x := op.X[0]; x <= op.X[1]; x++ {
				for z := op.Z[0]; z <= op.Z[1]; z++ {
					wx, wy, wz := at([3]int{x, op.Y, z})
					for ; wy > minY && inside(wx, wy, wz); wy-- {
						rid := get(wx, wy, wz)
						if n := ridName(rid); rid != air && n != "water" && n != "flowing_water" && n != "lava" && n != "flowing_lava" {
							if n == "grass_block" {
								put(wx, wy, wz, "dirt")
							}
							break
						}
						put(wx, wy, wz, op.Block)
					}
				}
			}
		case "chests_in_order":
			for _, cell := range op.Cells {
				x, y, z := at(cell)
				if inside(x, y, z) {
					put(x, y, z, op.Block)
					bes = append(bes, chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{"id": "Chest", "LootTable": op.Loot, "LootTableSeed": P.NextInt()}})
				}
			}
		case "cellar_variant":
			variant = P.NextBool()
		case "set_by_variant":
			x, y, z := at(op.At)
			if variant {
				put(x, y, z, op.IfTrue)
			} else {
				put(x, y, z, op.IfFalse)
			}
		case "archaeology_begin":
			x, y, z := at(op.Centre)
			s := int64(int32(x)*3129871) ^ int64(z)*116129781 ^ int64(y)
			s = s*s*42317861 + s*11
			X = worldgen.NewXoroshiro(int64(int32(s >> 16)))
			list = nil
		case "listed_sand":
			x, y, z := at(op.At)
			put(x, y, z, "sand")
			list = append(list, op.At)
		case "roof_choice":
			x, y, z := at(op.At)
			if X.NextIntN(3) == 0 {
				put(x, y, z, "sandstone")
			} else {
				put(x, y, z, "sand")
			}
		case "suspicious_pass":
			n := min(int(X.NextIntN(3))+5, len(list))
			for i := 1; i < len(list); i++ {
				j := int(X.NextIntN(int32(i + 1)))
				list[i], list[j] = list[j], list[i]
			}
			brush := func(p [3]int) {
				x, y, z := at(p)
				if !inside(x, y, z) {
					return
				}
				put(x, y, z, "suspicious_sand[brushed_progress=0,hanging=0]")
				bes = append(bes, chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{"id": "BrushableBlock",
					"LootTable": "loot_tables/entities/desert_pyramid_brushable_block.json", "LootTableSeed": X.NextInt()}})
			}
			for _, p := range list[:max(n, 1)] {
				brush(p)
			}
			rx := int(X.NextIntN(5)) + 14
			rz := int(X.NextIntN(5)) + 11
			brush([3]int{rx, 0, rz})
		default:
			t.Fatalf("pyramid op %q", op.Op)
		}
	}
	return bes
}
