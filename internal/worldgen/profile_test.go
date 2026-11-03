package worldgen

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)


func chunkRuntimeIDs(g *Generator, cx, cz int32) []uint32 {
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	g.GenerateChunk(world.ChunkPos{cx, cz}, c)
	out := make([]uint32, 0, 1<<16)
	for y := int16(minY); y <= maxY; y++ {
		for x := uint8(0); x < 16; x++ {
			for z := uint8(0); z < 16; z++ {
				out = append(out, c.Block(x, y, z, 0))
			}
		}
	}
	return out
}

// TestProfilesDiffer: the two profiles share algorithms and differ in data.
func TestProfilesDiffer(t *testing.T) {
	a, b := NewWithProfile(100, ProfileBedrock), NewWithProfile(100, ProfileOverjava)
	same := 0
	x, y := chunkRuntimeIDs(a, 5, 5), chunkRuntimeIDs(b, 5, 5)
	for i := range x {
		if x[i] == y[i] {
			same++
		}
	}
	if same == len(x) {
		t.Fatal("the profiles gave the same chunk")
	}
	t.Logf("profiles agree on %.2f%% of blocks", 100*float64(same)/float64(len(x)))
}
