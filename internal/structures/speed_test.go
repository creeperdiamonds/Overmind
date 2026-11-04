package structures

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// BenchmarkPlaceChunk is what structures add to a chunk's generation: PlaceChunk on finished chunks,
// over seed 100's ancient city (start (1, -13), the costliest case: every chunk rebuilds or finds the
// city) and over open ground away from any city.
func BenchmarkPlaceChunk(b *testing.B) {
	for _, area := range []struct {
		name   string
		cx, cz int
	}{{"city", -2, -17}, {"open", 200, 200}} {
		b.Run(area.name, func(b *testing.B) {
			bd, d := installData(b)
			g := worldgen.NewWithData(100, worldgen.ProfileBedrock, bd)
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
