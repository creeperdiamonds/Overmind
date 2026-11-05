package worldgen

import (
	"fmt"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestFeaturePlantsStandOnGround: no plant any feature places stands on water, air or another plant
// (but its own lower half), on a few seeds' chunks, among them the owner's floating bushes (seed
// -4323653748954477428, a river at (-42, 63, 169)). Bedrock profile with the install's data.
func TestFeaturePlantsStandOnGround(t *testing.T) {
	bd := testBedrockData(t)
	plants := map[string]bool{}
	for _, n := range strings.Fields("short_grass fern tall_grass large_fern bush firefly_bush poppy dandelion blue_orchid allium azure_bluet " +
		"red_tulip orange_tulip white_tulip pink_tulip oxeye_daisy cornflower lily_of_the_valley torchflower sunflower lilac rose_bush peony " +
		"pink_petals wildflowers open_eyeblossom closed_eyeblossom sweet_berry_bush deadbush dead_bush short_dry_grass tall_dry_grass " +
		"brown_mushroom red_mushroom crimson_fungus warped_fungus crimson_roots warped_roots nether_sprouts oak_sapling spruce_sapling " +
		"birch_sapling jungle_sapling acacia_sapling dark_oak_sapling cherry_sapling pale_oak_sapling") {
		plants[n] = true
	}
	bad := map[string]bool{"air": true, "water": true, "flowing_water": true, "lava": true, "flowing_lava": true}
	for _, c := range []struct {
		seed   int64
		cx, cz int
	}{{-4323653748954477428, -3, 10}, {100, 0, 0}, {5, -12, -52}, {12345, -8, 8}, {987654321, 2, 3}} {
		g := NewWithData(c.seed, ProfileBedrock, bd)
		var wrong []string
		n := 0
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				ch := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
				g.GenerateChunk(world.ChunkPos{int32(c.cx + dx), int32(c.cz + dz)}, ch)
				name := func(x, y, z int) string {
					b, _ := world.BlockByRuntimeID(ch.Block(uint8(x), int16(y), uint8(z), 0))
					s, _ := b.EncodeBlock()
					return strings.TrimPrefix(s, "minecraft:")
				}
				for x := range 16 {
					for z := range 16 {
						for y := minY + 1; y <= maxY; y++ {
							p := name(x, y, z)
							if !plants[p] {
								continue
							}
							n++
							below := name(x, y-1, z)
							if bad[below] || plants[below] && below != p {
								if len(wrong) < 8 {
									wrong = append(wrong, fmt.Sprintf("%s on %s at (%d,%d,%d)", p, below, (c.cx+dx)*16+x, y, (c.cz+dz)*16+z))
								}
							}
						}
					}
				}
			}
		}
		t.Logf("seed %d around chunk (%d,%d): %d plants", c.seed, c.cx, c.cz, n)
		if len(wrong) > 0 {
			t.Errorf("seed %d: plants on no ground: %v", c.seed, wrong)
		}
	}
}
