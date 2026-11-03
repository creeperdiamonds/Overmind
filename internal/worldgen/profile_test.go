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

// TestBedrockMeadowFreezesHigher: Bedrock's file gives meadow and cherry grove 0.3 against Java's 0.5,
// and savanna 1.2 against 2. The freeze pass and the surface rules both ask the profile's cold test,
// so a Bedrock meadow freezes from about Y 200 (0.05 colder per 40 blocks over Y 80) while Java's never
// does under the world top. No meadow stands that high in practice: none on seed 3 within 8192 blocks
// on a 32-block grid, so the rule is pinned here at the blocks rather than found in a world.
func TestBedrockMeadowFreezesHigher(t *testing.T) {
	bed, java := New(3), NewWithProfile(3, ProfileOverjava)
	meadow := biomeIdx["meadow"]
	froze := 0
	for x := 0; x < 256; x += 16 {
		if bed.coldEnough(meadow, x, 230, 0) {
			froze++
		}
		if java.coldEnough(meadow, x, maxY, 0) {
			t.Fatalf("Java meadow freezes at (%d, %d)", x, maxY)
		}
		if bed.coldEnough(meadow, x, 150, 0) {
			t.Fatalf("Bedrock meadow freezes at (%d, 150)", x)
		}
	}
	if froze < 12 {
		t.Errorf("Bedrock meadow froze at only %d of 16 blocks at Y 230", froze)
	}
	if got := ProfileBedrock.climate.temperature(biomeIdx["savanna"], 0, 64, 0); got != 1.2 {
		t.Errorf("Bedrock savanna %.2f, want 1.2", got)
	}
}
