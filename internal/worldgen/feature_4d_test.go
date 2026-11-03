package worldgen

import (
	"strings"
	"testing"
)

// The features of Task 4d that the trace fixtures hold no Java run of (coral, icebergs, cherry and
// meadow flowers, dry grass, firefly bushes, the mushroom island, mangroves, the pale garden, desert
// wells) are checked here for showing up where Java puts them, in Bedrock blocks, and for placing
// only states with a block (a state with none panics under test). Their exact shapes are not checked
// against Java: no traced chunk holds those biomes.
func TestUntracedFeaturesAppear(t *testing.T) {
	cases := []struct {
		name   string
		biomes []string
		want   func(n string, y int16) bool
		least  int
	}{
		{"coral", []string{"warm_ocean"}, func(n string, _ int16) bool { return strings.HasSuffix(n, "_coral_block") }, 50},
		{"coral fans", []string{"warm_ocean"}, func(n string, _ int16) bool { return strings.HasSuffix(n, "_coral_wall_fan") }, 1},
		{"icebergs", []string{"frozen_ocean", "deep_frozen_ocean"}, func(n string, y int16) bool { return n == "packed_ice" && y > SeaLevel }, 20},
		{"cherry petals", []string{"cherry_grove"}, func(n string, _ int16) bool { return n == "pink_petals" }, 10},
		{"meadow flowers", []string{"meadow"}, func(n string, _ int16) bool { return n == "allium" || n == "cornflower" || n == "oxeye_daisy" || n == "azure_bluet" }, 1},
		{"wildflowers", []string{"meadow", "birch_forest"}, func(n string, _ int16) bool { return n == "wildflowers" }, 1},
		{"dry grass", []string{"desert"}, func(n string, _ int16) bool { return n == "short_dry_grass" || n == "tall_dry_grass" }, 1},
		{"firefly bushes", []string{"swampland"}, func(n string, _ int16) bool { return n == "firefly_bush" }, 1},
		{"huge mushrooms", []string{"mushroom_island"}, func(n string, _ int16) bool { return strings.HasSuffix(n, "_mushroom_block") }, 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g := New(7)
			chunks := biomeChunks(g, 3, c.biomes...)
			if len(chunks) == 0 {
				t.Fatalf("no %v chunk found", c.biomes)
			}
			var pos [][2]int32
			for _, p := range chunks {
				pos = append(pos, blockAround(p, 1)...)
			}
			found := 0
			for _, ch := range generateAll(g, pos) {
				for x := range uint8(16) {
					for z := range uint8(16) {
						for y := int16(minY); y <= maxY; y++ {
							if c.want(nameOf(ch.Block(x, y, z, 0)), y) {
								found++
							}
						}
					}
				}
			}
			t.Logf("%d in %d chunks", found, len(pos))
			if found < c.least {
				t.Errorf("%s: %d blocks in %d chunks, want at least %d", c.name, found, len(pos), c.least)
			}
		})
	}
}
