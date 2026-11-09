package worldgen

import (
	"path/filepath"
	"testing"
)

// TestRound22BlocksMatchBDS measures the blocks of Task 9 round 22 (NOTES-leftovers L24 to L29) on the
// study's windows (golden-bedrock/whole/windows, every chunk) and the four whole seeds (structure chunks
// left out, as TestBedrockWorldsMatchBDS scores them), on their stable masks: for each block BDS's count,
// ours and how many agree (recall and precision). Nothing is held: the figures measure the rules.
func TestRound22BlocksMatchBDS(t *testing.T) {
	dir := goldenBedrockDir(t)
	testBedrockData(t)
	flowers := []string{"dandelion", "poppy", "azure_bluet", "oxeye_daisy", "cornflower", "red_tulip", "orange_tulip", "white_tulip",
		"pink_tulip", "allium", "short_grass", "tall_grass"}
	seedBlocks := []string{"lava", "flowing_lava", "water", "flowing_water", "cobblestone", "mossy_cobblestone", "mob_spawner", "chest",
		"packed_ice", "blue_ice", "emerald_ore", "deepslate_emerald_ore", "grass_block", "podzol", "brown_mushroom", "red_mushroom",
		"firefly_bush", "closed_eyeblossom", "suspicious_sand", "sandstone_slab"}
	sets := []struct {
		name   string
		seed   bool
		blocks []string
	}{
		{"ice_plains_spikes_4", false, []string{"packed_ice", "snow", "ice", "grass_block", "dirt"}},
		{"frozen_ocean_4", false, []string{"packed_ice", "blue_ice", "ice", "snow"}},
		{"deep_frozen_ocean_4", false, []string{"packed_ice", "blue_ice", "ice", "snow"}},
		{"mangrove_swamp_20", false, []string{"grass_block", "dirt", "mud", "firefly_bush", "water"}},
		{"swampland_17", false, []string{"grass_block", "dirt", "oak_log", "oak_leaves", "vine", "firefly_bush", "seagrass",
			"brown_mushroom", "red_mushroom", "brown_mushroom_block", "red_mushroom_block", "short_grass", "blue_orchid"}},
		{"bamboo_jungle_3", false, []string{"podzol", "bamboo", "grass_block", "dirt"}},
		{"plains_9", false, append([]string{"firefly_bush"}, flowers...)},
		{"sunflower_plains_17", false, append([]string{"firefly_bush"}, flowers...)},
		{"meadow_987654321", false, append([]string{"firefly_bush"}, flowers...)},
		{"pale_garden_33", false, []string{"closed_eyeblossom", "open_eyeblossom", "firefly_bush"}},
		{"desert_28", false, []string{"suspicious_sand", "sandstone", "sandstone_slab", "water"}},
		{"taiga_24", false, []string{"brown_mushroom", "red_mushroom", "firefly_bush"}},
		{"mega_taiga_8", false, []string{"brown_mushroom", "red_mushroom", "podzol", "firefly_bush"}},
		{"redwood_taiga_mutated_9", false, []string{"brown_mushroom", "red_mushroom", "podzol"}},
		{"extreme_hills_5", false, []string{"emerald_ore", "deepslate_emerald_ore", "lava", "flowing_lava", "water", "flowing_water"}},
		{"mushroom_island_11", false, []string{"brown_mushroom", "red_mushroom"}},
		{"seed100", true, seedBlocks}, {"seed1", true, seedBlocks}, {"seed12345", true, seedBlocks}, {"seed987654321", true, seedBlocks},
	}
	for _, set := range sets {
		t.Run(set.name, func(t *testing.T) {
			f := filepath.Join(dir, "whole", "windows", set.name+".bin.gz")
			if set.seed {
				f = filepath.Join(dir, "whole", set.name+".bin.gz")
			}
			w := loadBDSFile(t, f)
			defer bdsWorlds.Delete(f)
			var pos [][2]int32
			for k := range w.chunks {
				if !set.seed || !w.skipForStructure(k[0], k[1]) {
					pos = append(pos, [2]int32{int32(k[0]), int32(k[1])})
				}
			}
			got := generateNames(NewWithProfile(w.seed, ProfileBedrock), pos)
			type tally struct{ bds, ours, both int }
			all := make([]tally, len(set.blocks))
			idx := map[string]int{}
			for i, n := range set.blocks {
				idx[n] = i
			}
			var total, same int
			for _, p := range pos {
				k := [2]int{int(p[0]), int(p[1])}
				bc, ours := w.chunks[k], got[k]
				for cell := range chunkCells - 256 { // Y 319 holds the study's loading marker.
					if bc.masked(cell) {
						continue
					}
					real, mine := w.names[bc.ids[cell]], ours.names[cell]
					total++
					if real == mine {
						same++
					}
					i, a := idx[real]
					j, b := idx[mine]
					if a {
						all[i].bds++
					}
					if b {
						all[j].ours++
					}
					if a && b && i == j {
						all[i].both++
					}
				}
			}
			t.Logf("%s: %d chunks, same %d/%d = %.4f%%", set.name, len(pos), same, total, 100*float64(same)/float64(max(total, 1)))
			for i, n := range set.blocks {
				c := all[i]
				if c.bds == 0 && c.ours == 0 {
					continue
				}
				t.Logf("  %-22s BDS %8d ours %8d both %8d: recall %6.2f%%, precision %6.2f%%", n, c.bds, c.ours, c.both,
					100*float64(c.both)/float64(max(c.bds, 1)), 100*float64(c.both)/float64(max(c.ours, 1)))
			}
		})
	}
}

// TestBedrockStandInsStop: with the data read, the Java placements Bedrock's code features replace since
// round 22 no longer run in the Bedrock profile, and a rule BDS never runs (a filter no biome passes, or
// 0 iterations) keeps no Java placement (NOTES-leftovers L24).
func TestBedrockStandInsStop(t *testing.T) {
	bd := testBedrockData(t)
	for _, n := range []string{"lake_lava_underground", "lake_lava_surface", "monster_room", "monster_room_deep", "ice_spike", "ice_patch",
		"desert_well", "blue_ice", "iceberg_packed", "iceberg_blue", "disk_grass", "spring_water", "spring_lava", "spring_lava_frozen",
		"ore_emerald", "flower_plains", "flower_meadow", "brown_mushroom_taiga", "red_mushroom_swamp", "trees_swamp", "bamboo",
		"pale_garden_flowers", "patch_firefly_bush_near_water"} {
		var pl *placement
		for _, p := range ProfileBedrock.placements {
			if p.name == n {
				pl = p
			}
		}
		if pl == nil {
			t.Errorf("%s: no such Java placement in the Bedrock profile", n)
			continue
		}
		if !bd.javaReplaced(ProfileBedrock, pl) {
			t.Errorf("%s still runs in the Bedrock profile", n)
		}
	}
	for _, id := range []string{"minecraft:taiga_surface_mushrooms_feature", "minecraft:plains_surface_flowers_feature",
		"minecraft:meadow_flowers_feature", "minecraft:extreme_hills_after_surface_emerald_ore_feature"} {
		if r := bd.rules[id]; r == nil || !bd.deadRule(r) {
			t.Errorf("%s: not read as a rule BDS never runs", id)
		}
	}
}
