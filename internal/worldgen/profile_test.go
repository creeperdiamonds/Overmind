package worldgen

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func init() { defaultReuseBases = true }

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

// TestReuseBasesChangesNothing: keeping neighbour bases, the speed option, gives the same chunks.
func TestReuseBasesChangesNothing(t *testing.T) {
	for _, profile := range []*Profile{ProfileBedrock, ProfileOverjava} {
		a, b := NewWithProfile(100, profile), NewWithProfile(100, profile)
		a.reuseBases, b.reuseBases = false, true
		for _, p := range [][2]int32{{5, 5}, {6, 5}, {5, 6}, {-40, 30}, {6, 6}} {
			x, y := chunkRuntimeIDs(a, p[0], p[1]), chunkRuntimeIDs(b, p[0], p[1])
			for i := range x {
				if x[i] != y[i] {
					t.Fatalf("%s chunk %v differs at %d", profile.Name, p, i)
				}
			}
		}
	}
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
