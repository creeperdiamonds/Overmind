package worldgen

import (
	"math"
	"testing"
)

// Rules of Task 4d's untraced features, checked against Java 26.1's source and data files rather
// than against a Java run, since no traced chunk holds these biomes.

// java4dPlacements are Task 4d's placed features as Java 26.1 lists them (worldgen/placed_feature/
// <name>.json), written in describePlacement's words. Patch counts, offsets and filters sit in the
// placed feature in 26.1, as they do here.
var java4dPlacements = map[string]string{
	"lush_caves_clay":                     "count:62 in_square height_range:-64..256/flat environment_scan:down:12 random_offset:0:1 biome",
	"lush_caves_ceiling_vegetation":       "count:125 in_square height_range:-64..256/flat environment_scan:up:12 random_offset:0:-1 biome",
	"lush_caves_vegetation":               "count:125 in_square height_range:-64..256/flat environment_scan:down:12 random_offset:0:1 biome",
	"cave_vines":                          "count:188 in_square height_range:-64..256/flat environment_scan:up:12 random_offset:0:-1 biome",
	"spore_blossom":                       "count:25 in_square height_range:-64..256/flat environment_scan:up:12 random_offset:0:-1 biome",
	"classic_vines_cave_feature":          "count:256 in_square height_range:-64..256/flat biome",
	"rooted_azalea_tree":                  "count:1..2/flat in_square height_range:-64..256/flat environment_scan:up:12 random_offset:0:-1 biome",
	"dripstone_cluster":                   "count:48..96/flat in_square height_range:-64..256/flat biome",
	"pointed_dripstone":                   "count:192..256/flat in_square height_range:-64..256/flat count:1..5/flat random_offset:-10..10/shaped:-2..2/shaped biome",
	"large_dripstone":                     "count:10..48/flat in_square height_range:-64..256/flat biome",
	"sculk_patch_deep_dark":               "count:256 in_square height_range:-64..256/flat biome",
	"sculk_vein":                          "count:204..250/flat in_square height_range:-64..256/flat biome",
	"flower_flower_forest":                "count:3 rarity_filter:2 in_square heightmap:MOTION_BLOCKING biome count:96 random_offset:-6..6/shaped:-2..2/shaped filter",
	"wildflowers_birch_forest":            "count:3 rarity_filter:2 in_square heightmap:MOTION_BLOCKING biome count:64 random_offset:-6..6/shaped:-2..2/shaped filter",
	"wildflowers_meadow":                  "noise_threshold_count:-0.8:5:10 in_square heightmap:MOTION_BLOCKING biome count:8 random_offset:-6..6/shaped:-2..2/shaped filter",
	"flower_cherry":                       "noise_threshold_count:-0.8:5:10 in_square heightmap:MOTION_BLOCKING biome count:96 random_offset:-6..6/shaped:-2..2/shaped filter",
	"flower_meadow":                       "in_square heightmap:MOTION_BLOCKING biome count:96 random_offset:-6..6/shaped:-2..2/shaped filter",
	"patch_leaf_litter":                   "count:2 in_square heightmap:WORLD_SURFACE_WG biome count:32 random_offset:-7..7/shaped:-3..3/shaped filter",
	"patch_dry_grass_desert":              "rarity_filter:3 in_square heightmap:MOTION_BLOCKING biome count:64 random_offset:-7..7/shaped:-3..3/shaped filter",
	"patch_dry_grass_badlands":            "rarity_filter:6 in_square heightmap:MOTION_BLOCKING biome count:64 random_offset:-7..7/shaped:-3..3/shaped filter",
	"patch_firefly_bush_swamp":            "rarity_filter:8 in_square heightmap:MOTION_BLOCKING biome count:20 random_offset:-4..4/shaped:-3..3/shaped filter",
	"patch_firefly_bush_near_water_swamp": "count:3 in_square heightmap:MOTION_BLOCKING biome filter count:20 random_offset:-4..4/shaped:-3..3/shaped filter",
	"patch_firefly_bush_near_water":       "count:2 in_square heightmap:MOTION_BLOCKING_NO_LEAVES biome filter count:20 random_offset:-4..4/shaped:-3..3/shaped filter",
	"mushroom_island_vegetation":          "in_square heightmap:MOTION_BLOCKING biome",
	"warm_ocean_vegetation":               "noise_based_count:20:400:0 in_square heightmap:OCEAN_FLOOR_WG biome",
	"iceberg_packed":                      "rarity_filter:16 in_square biome",
	"iceberg_blue":                        "rarity_filter:200 in_square biome",
	"trees_mangrove":                      "count:25 in_square surface_water_depth_filter:5 heightmap:OCEAN_FLOOR biome",
	"pale_moss_patch":                     "count:1 in_square heightmap:MOTION_BLOCKING_NO_LEAVES biome",
	"pale_garden_vegetation":              "count:16 in_square surface_water_depth_filter:0 heightmap:OCEAN_FLOOR biome",
	"pale_garden_flowers":                 "rarity_filter:8 in_square heightmap:MOTION_BLOCKING_NO_LEAVES biome count:96 random_offset:-7..7/shaped:-3..3/shaped filter",
	"flower_pale_garden":                  "rarity_filter:32 in_square heightmap:MOTION_BLOCKING biome",
	"desert_well":                         "rarity_filter:1000 in_square heightmap:MOTION_BLOCKING biome",
	"underwater_magma":                    "count:44..52/flat in_square height_range:-64..256/flat surface_relative_threshold_filter:OCEAN_FLOOR_WG:_:-2 biome",
}

// javaFixedPlacements are older placed features found differing from Java's files by the same check
// in Task 4d's fix round, and corrected (worldgen/placed_feature/<name>.json).
var javaFixedPlacements = map[string]string{
	"flower_default":            "rarity_filter:32 in_square heightmap:MOTION_BLOCKING biome",
	"flower_warm":               "rarity_filter:16 in_square heightmap:MOTION_BLOCKING biome",
	"forest_flowers":            "rarity_filter:7 in_square heightmap:MOTION_BLOCKING count:0..1/shaped biome",
	"flower_forest_flowers":     "rarity_filter:7 in_square heightmap:MOTION_BLOCKING count:0..3/shaped biome",
	"brown_mushroom_old_growth": "count:3 rarity_filter:4 in_square heightmap:MOTION_BLOCKING biome count:96 random_offset:-7..7/shaped:-3..3/shaped filter",
	"patch_bush":                "rarity_filter:4 in_square heightmap:MOTION_BLOCKING biome count:24 random_offset:-5..5/shaped:-3..3/shaped filter",
	"patch_cactus_desert":       "rarity_filter:6 in_square heightmap:MOTION_BLOCKING biome count:10 random_offset:-7..7/shaped:-3..3/shaped filter",
	"patch_cactus_decorated":    "rarity_filter:13 in_square heightmap:MOTION_BLOCKING biome count:10 random_offset:-7..7/shaped:-3..3/shaped filter",
	"patch_melon":               "rarity_filter:6 in_square heightmap:MOTION_BLOCKING biome count:64 random_offset:-7..7/shaped:-3..3/shaped filter",
	"patch_melon_sparse":        "rarity_filter:64 in_square heightmap:MOTION_BLOCKING biome count:64 random_offset:-7..7/shaped:-3..3/shaped filter",
	"patch_pumpkin":             "rarity_filter:300 in_square heightmap:MOTION_BLOCKING biome count:96 random_offset:-7..7/shaped:-3..3/shaped filter",
	"patch_sugar_cane":          "rarity_filter:6 in_square heightmap:MOTION_BLOCKING biome count:20 random_offset:-4..4/shaped:0 filter",
	"patch_sugar_cane_desert":   "in_square heightmap:MOTION_BLOCKING biome count:20 random_offset:-4..4/shaped:0 filter",
	"patch_sugar_cane_badlands": "rarity_filter:5 in_square heightmap:MOTION_BLOCKING biome count:20 random_offset:-4..4/shaped:0 filter",
	"patch_sugar_cane_swamp":    "rarity_filter:3 in_square heightmap:MOTION_BLOCKING biome count:20 random_offset:-4..4/shaped:0 filter",
	"disk_grass":                "count:1 in_square heightmap:OCEAN_FLOOR_WG random_offset:0:-1 filter biome",
	"ice_patch":                 "count:2 in_square heightmap:MOTION_BLOCKING random_offset:0:-1 filter biome",
	"patch_grass_meadow":        "noise_threshold_count:-0.8:5:10 in_square heightmap:WORLD_SURFACE_WG biome count:16 random_offset:-7..7/shaped:-3..3/shaped filter",
	"trees_plains":              "count:0..1/shaped in_square surface_water_depth_filter:0 heightmap:OCEAN_FLOOR filter biome",
	"trees_birch":               "count:10..11/shaped in_square surface_water_depth_filter:0 heightmap:OCEAN_FLOOR biome filter",
	"trees_snowy":               "count:0..1/shaped in_square surface_water_depth_filter:0 heightmap:OCEAN_FLOOR biome filter",
	"trees_badlands":            "count:5..6/shaped in_square surface_water_depth_filter:0 heightmap:OCEAN_FLOOR biome filter",
}

// bedrock4dPlacements are where the Bedrock profile keeps Bedrock's own file values instead
// (BEDROCK-NOTES section 5 row 9: magma 18 to 30 attempts).
var bedrock4dPlacements = map[string]string{
	"underwater_magma": "count:18..30/flat in_square height_range:-64..256/flat surface_relative_threshold_filter:OCEAN_FLOOR_WG:_:-2 biome",
}

// TestPlacementsFollowJava4d holds each of Task 4d's placed features, and the older ones corrected
// with them, in both profiles, against
// Java's placed feature file: modifier by modifier, with counts, rarities, heightmaps and offsets.
// Block predicates are only checked to be where Java has one.
func TestPlacementsFollowJava4d(t *testing.T) {
	for _, prof := range []*Profile{ProfileOverjava, ProfileBedrock} {
		byName := map[string]*placement{}
		for _, pl := range prof.placements {
			byName[pl.name] = pl
		}
		for name, want := range mergedMaps(java4dPlacements, javaFixedPlacements) {
			if b, ok := bedrock4dPlacements[name]; ok && prof == ProfileBedrock {
				want = b
			}
			pl := byName[name]
			if pl == nil {
				t.Errorf("%s: no %s", prof.Name, name)
				continue
			}
			if got := describePlacement(pl); got != want {
				t.Errorf("%s: %s\n ours: %s\n Java: %s", prof.Name, name, got, want)
			}
		}
	}
}

func mergedMaps(list ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range list {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// TestIcebergDrawsLikeJava replays the iceberg's shape draws in IcebergFeature's order: an ellipse
// draws its height above water with nextInt(6), a round iceberg with nextInt(15) and maybe a tall
// extra, never both. Both the values and the generator's state after must agree.
func TestIcebergDrawsLikeJava(t *testing.T) {
	ellipses, round := 0, 0
	for seed := range int64(4000) {
		var ours, java featRand
		ours.setSeed(seed)
		java.setSeed(seed)
		got := icebergShape(&ours)

		var want icebergDims
		want.snowy = java.nextDouble() > 0.7
		want.angle = java.nextDouble() * 2 * math.Pi
		want.ea = 11 - java.nextInt(5)
		want.ec = 3 + java.nextInt(3)
		want.ellipse = java.nextDouble() > 0.7
		if want.ellipse {
			want.over = java.nextInt(6) + 6
			ellipses++
		} else {
			want.over = java.nextInt(15) + 3
			round++
		}
		if !want.ellipse && java.nextDouble() > 0.9 {
			want.over += java.nextInt(19) + 7
		}
		want.under = min(want.over+java.nextInt(11), 18)
		want.width = min(want.over+java.nextInt(7)-java.nextInt(5), 11)

		if got != want {
			t.Fatalf("seed %d: shape %+v, Java draws %+v", seed, got, want)
		}
		if ours.x != java.x {
			t.Fatalf("seed %d: the generator is not where Java's is after the shape draws", seed)
		}
	}
	if ellipses == 0 || round == 0 {
		t.Fatalf("seeds gave %d ellipses and %d round icebergs, want both kinds", ellipses, round)
	}
}

// TestMangrovesFollowJava holds both mangroves against Java 26.1's configured features mangrove and
// tall_mangrove (worldgen/configured_feature/*.json) and the block tags they name.
func TestMangrovesFollowJava(t *testing.T) {
	logsThrough := "mud muddy_mangrove_roots mangrove_roots mangrove_leaves mangrove_log mangrove_propagule moss_carpet vine" // #mangrove_logs_can_grow_through
	rootsThrough := "mud muddy_mangrove_roots mangrove_roots moss_carpet vine mangrove_propagule snow"                        // #mangrove_roots_can_grow_through
	for _, c := range []struct {
		tall          bool
		base, a, b    int
		steps, offset string
		limit         int
	}{
		{false, 2, 1, 4, "1..4/flat", "1..3/flat", 2},
		{true, 4, 1, 9, "1..6/flat", "3..7/flat", 3},
	} {
		cfg := mangroveTree(c.tall)
		tr, ok := cfg.trunk.(branchingTrunk)
		if !ok {
			t.Fatalf("tall %v: trunk is %T, want the upwards branching trunk", c.tall, cfg.trunk)
		}
		fo, ok := cfg.foliage.(spreadFoliage)
		if !ok {
			t.Fatalf("tall %v: foliage is %T, want random spread foliage", c.tall, cfg.foliage)
		}
		ro := cfg.roots
		got := []any{tr.base, tr.a, tr.b, describeInts(tr.steps), describeInts(tr.length), tr.chance,
			describeInts(fo.radius), describeInts(fo.offs), describeInts(fo.fh), fo.attempts,
			cfg.layersLimit, cfg.lower, cfg.upper, cfg.ignoreVines,
			describeInts(ro.offset), ro.aboveChance, ro.width, ro.length, ro.skew, ro.root.name(), ro.above.name(), ro.muddy.name()}
		want := []any{c.base, c.a, c.b, c.steps, "0..1/flat", float32(0.5),
			"3", "0", "2", 70,
			c.limit, 0, 2, true,
			c.offset, float32(0.5), 8, 15, float32(0.2), "mangrove_roots[waterlogged=false]", "moss_carpet", "muddy_mangrove_roots[axis=y]"}
		names := []string{"base_height", "height_rand_a", "height_rand_b", "extra_branch_steps", "extra_branch_length", "place_branch_per_log_probability",
			"radius", "offset", "foliage_height", "leaf_placement_attempts",
			"limit", "lower_size", "upper_size", "ignore_vines",
			"trunk_offset_y", "above_root_placement_chance", "max_root_width", "max_root_length", "random_skew_chance", "root_provider", "above_root_provider", "muddy_roots_provider"}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("tall %v: %s is %v, Java's is %v", c.tall, names[i], got[i], want[i])
			}
		}
		for _, set := range []struct {
			what string
			got  map[uint16]bool
			want string
		}{{"logs grow through", cfg.validExtra, logsThrough}, {"roots grow through", ro.through, rootsThrough}, {"muddy roots in", ro.muddyIn, "mud muddy_mangrove_roots"}} {
			if want := blockSet(set.want); len(set.got) != len(want) {
				t.Errorf("tall %v: %s holds %d blocks, Java's %d", c.tall, set.what, len(set.got), len(want))
			} else {
				for b := range want {
					if !set.got[b] {
						t.Errorf("tall %v: %s lacks %s", c.tall, set.what, blockName[b])
					}
				}
			}
		}
	}
}

// testDecoration readies a decoration on a chunk for placing by hand, as a source run would.
func testDecoration(t *testing.T, g *Generator, cx, cz int) *decoration {
	t.Helper()
	d := newScratch().startDecoration(g, cx, cz)
	d.cx, d.cz = cx, cz
	d.gen++
	d.r = featRand{}
	d.r.setSeed(1)
	return d
}

func (d *decoration) undo() {
	d.rollback()
	d.log = d.log[:0]
}

// TestMossCarpetToppersLikeJava checks MossyCarpetBlock.placeAt as simple_block runs it: the topper's
// side coins come from the level random (one per side the topper could have, north, east, south,
// west), never from the feature random, and there is no topper and no coin where the block above is
// solid or a bottom carpet. A side under a kept topper side grows tall.
func TestMossCarpetToppersLikeJava(t *testing.T) {
	g := NewWithProfile(7, ProfileOverjava)
	d := testDecoration(t, g, 0, 0)
	p := pos{8, 200, 8} // In the air, far over any ground.
	up := p.step(dUp)
	put := func(q pos, s bs) { d.set(q.x, q.y, q.z, s) }
	walls := func() {
		put(p.step(dDown), sStone)
		for _, q := range []pos{p.step(dNorth), p.step(dEast), up.step(dNorth), up.step(dEast)} {
			put(q, sStone)
		}
	}
	grass := stateOf("short_grass")
	toppers, bare := 0, 0
	for seed := range uint64(64) {
		for _, above := range []bs{sAir, grass} {
			walls()
			put(up, above)
			d.region = newXoro(seed*0x9E3779B97F4A7C15+1, seed+7)
			ref := d.region
			north, east := ref.next()&1 != 0, ref.next()&1 != 0
			featBefore := d.r
			placeCarpet(d, p)
			if d.region != ref {
				t.Fatalf("seed %d: the topper drew other than one coin for each of its two sides", seed)
			}
			if d.r != featBefore {
				t.Fatalf("seed %d: the carpet drew from the feature random", seed)
			}
			low := func(keep bool) int {
				if keep {
					return 2
				}
				return 1
			}
			if want := paleCarpet(true, [4]int{low(north), low(east), 0, 0}); d.at(p) != want {
				t.Fatalf("seed %d: bottom %s, want %s", seed, d.at(p).name(), want.name())
			}
			wantUp := above
			if north || east {
				wantUp = paleCarpet(false, [4]int{btoi(north), btoi(east), 0, 0})
				toppers++
			} else {
				bare++
			}
			if d.at(up) != wantUp {
				t.Fatalf("seed %d over %s: above %s, want %s", seed, above.name(), d.at(up).name(), wantUp.name())
			}
			d.undo()
		}
	}
	if toppers == 0 || bare == 0 {
		t.Fatalf("%d toppers and %d bare carpets, want both", toppers, bare)
	}
	for _, above := range []bs{sStone, paleCarpet(true, [4]int{})} {
		walls()
		put(up, above)
		d.region = newXoro(5, 6)
		ref := d.region
		placeCarpet(d, p)
		if d.at(up) != above {
			t.Errorf("over %s: a topper replaced it (%s)", above.name(), d.at(up).name())
		}
		if d.region != ref {
			t.Errorf("over %s: a coin was drawn with no topper possible", above.name())
		}
		if want := paleCarpet(true, [4]int{1, 1, 0, 0}); d.at(p) != want {
			t.Errorf("over %s: bottom %s, want %s", above.name(), d.at(p).name(), want.name())
		}
		d.undo()
	}
}
