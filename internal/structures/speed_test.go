package structures

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// BenchmarkPlaceChunk is what structures add to a chunk's generation: PlaceChunk on finished chunks,
// over seed 100's ancient city (start (1, -13), the costliest case: every chunk rebuilds or finds the
// city), over open ground away from any city, seed 100's stronghold and seed 32's (-12,6) monument.
func BenchmarkPlaceChunk(b *testing.B) {
	for _, area := range []struct {
		name   string
		seed   int64
		cx, cz int
	}{{"city", 100, -2, -17}, {"open", 100, 200, 200}, {"stronghold", 100, 50, 100}, {"monument", 32, -16, 2}} {
		b.Run(area.name, func(b *testing.B) {
			bd, d := installData(b)
			g := worldgen.NewWithData(area.seed, worldgen.ProfileBedrock, bd)
			var base []*chunk.Chunk
			var pos [][2]int
			for i := range 8 {
				for j := range 8 {
					c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
					g.GenerateChunk(world.ChunkPos{int32(area.cx + i), int32(area.cz + j)}, c)
					base, pos = append(base, c), append(pos, [2]int{area.cx + i, area.cz + j})
				}
			}
			s := New(g, d)
			b.ResetTimer()
			n := 0
			for b.Loop() {
				k := n % len(base)
				s.PlaceChunk(pos[k][0], pos[k][1], base[k].Clone())
				n++
			}
		})
	}
}

// BenchmarkGenerateWithStructures is whole-chunk generation with and without structures attached, over
// seed 100's ancient city (where every base build also gathers the city's terrain adaptation) and open
// ground. Each run uses a fresh generator, so the shared cache starts empty.
func BenchmarkGenerateWithStructures(b *testing.B) {
	for _, area := range []struct {
		name   string
		cx, cz int
	}{{"city", -2, -17}, {"open", 200, 200}} {
		for _, with := range []bool{false, true} {
			name := area.name + "/without"
			if with {
				name = area.name + "/with"
			}
			b.Run(name, func(b *testing.B) {
				bd, d := installData(b)
				n := 0
				for b.Loop() {
					g := worldgen.NewWithData(100, worldgen.ProfileBedrock, bd)
					if with {
						g.SetStructures(New(g, d))
					}
					for i := range 4 {
						for j := range 4 {
							c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
							g.GenerateChunk(world.ChunkPos{int32(area.cx + i), int32(area.cz + j)}, c)
							n++
						}
					}
				}
				b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(n), "ms/chunk")
			})
		}
	}
}

// BenchmarkPlaceNetherChunk is PlaceChunk on finished Nether chunks of seed 100: over the (4,19)
// fortress, the (-13,-20) bastion, and open Nether away from both. Each area takes a fresh Structures,
// so the first chunk also assembles the structure.
func BenchmarkPlaceNetherChunk(b *testing.B) {
	for _, area := range []struct {
		name   string
		cx, cz int
	}{{"fortress", 4, 22}, {"bastion", -16, -23}, {"open", 200, 200}} {
		b.Run(area.name, func(b *testing.B) {
			bd, d := installData(b)
			g := worldgen.NewWithData(100, worldgen.ProfileBedrock, bd).Nether()
			var base []*chunk.Chunk
			var pos [][2]int
			for i := range 8 {
				for j := range 8 {
					c := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
					g.GenerateChunk(world.ChunkPos{int32(area.cx + i), int32(area.cz + j)}, c)
					base, pos = append(base, c), append(pos, [2]int{area.cx + i, area.cz + j})
				}
			}
			s := New(g, d)
			b.ResetTimer()
			n := 0
			for b.Loop() {
				k := n % len(base)
				s.PlaceChunk(pos[k][0], pos[k][1], base[k].Clone())
				n++
			}
		})
	}
}
