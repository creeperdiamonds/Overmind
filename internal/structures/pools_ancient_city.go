package structures

import "strings"

// TRANSCRIBED TABLE: the ancient city's jigsaw pools.
//
// Where from: BDS 1.26.60.6's pool registration code (0x53ac3e0), as the study read it (structures
// notes N2 1.7 and phase 3; the study's table out/anc_pools_v2.txt, kept outside the repo). Every pool
// name, element name, weight and fallback below is transcribed from that reading. It is Mojang's
// content restated as Go, kept here, in one file per structure, as an interim measure while the owner
// decides whether code-registered tables may be transcribed (review r6 I6). Nothing else in the
// package holds such a table; later structures (villages, bastions) get their own pools_*.go file.

// ancientPools are the ancient city's pools as registered in the server (N2 1.7 and phase 3; the study's
// table out/anc_pools_v2.txt, read from the registration code at 0x53ac3e0). Fallback "": the pool's
// helper builds no fallback (0x53c8830, used by sculk and walls; N2 phase 3).
var ancientPools = []struct {
	name, fallback string
	entries        []poolEntry
}{
	{"minecraft:ancient_city/city_center", "minecraft:empty", []poolEntry{
		{1, "city_center_city_center_1"}, {1, "city_center_city_center_2"}, {1, "city_center_city_center_3"},
	}},
	{"minecraft:ancient_city/structures", "minecraft:empty", []poolEntry{
		{7, ""}, {4, "structures_barracks"}, {4, "structures_chamber_1"}, {4, "structures_chamber_2"}, {4, "structures_chamber_3"},
		{4, "structures_sauna_1"}, {4, "structures_small_statue"}, {1, "structures_large_ruin_1"}, {1, "structures_tall_ruin_1"},
		{1, "structures_tall_ruin_2"}, {2, "structures_tall_ruin_3"}, {2, "structures_tall_ruin_4"}, {1, "structures_medium_ruin_1"},
		{1, "structures_medium_ruin_2"}, {1, "structures_small_ruin_1"}, {1, "structures_small_ruin_2"}, {1, "structures_large_pillar_1"},
		{1, "structures_medium_pillar_1"}, {1, "structures_ice_box_1"}, {1, "structures_camp_1"}, {1, "structures_camp_2"},
		{1, "structures_camp_3"},
	}},
	// The empty element after the feature was lost by the first extraction (N2 phase 3, "ancient city
	// divergence FOUND"; out/anc_pools_v2.txt).
	{"minecraft:ancient_city/sculk", "", []poolEntry{{6, "minecraft:sculk_catalyst_with_patches_city"}, {1, ""}}},
	{"minecraft:ancient_city/walls", "", []poolEntry{
		{1, "walls_intact_corner_wall_1"}, {1, "walls_intact_intersection_wall_1"}, {1, "walls_intact_lshape_wall_1"},
		{1, "walls_intact_horizontal_wall_1"}, {1, "walls_intact_horizontal_wall_2"}, {1, "walls_intact_horizontal_wall_stairs_1"},
		{1, "walls_intact_horizontal_wall_stairs_2"}, {1, "walls_intact_horizontal_wall_stairs_3"}, {4, "walls_intact_horizontal_wall_stairs_4"},
		{3, "walls_intact_horizontal_wall_passage_1"}, {1, "walls_ruined_corner_wall_1"}, {1, "walls_ruined_corner_wall_2"},
		{2, "walls_ruined_horizontal_wall_stairs_1"}, {2, "walls_ruined_horizontal_wall_stairs_2"}, {3, "walls_ruined_horizontal_wall_stairs_3"},
		{3, "walls_ruined_horizontal_wall_stairs_4"},
	}},
	{"minecraft:ancient_city/walls/no_corners", "minecraft:empty", []poolEntry{
		{1, "walls_intact_horizontal_wall_1"}, {1, "walls_intact_horizontal_wall_2"}, {1, "walls_intact_horizontal_wall_stairs_1"},
		{1, "walls_intact_horizontal_wall_stairs_2"}, {1, "walls_intact_horizontal_wall_stairs_3"}, {1, "walls_intact_horizontal_wall_stairs_4"},
		{1, "walls_intact_horizontal_wall_bridge"},
	}},
	{"minecraft:ancient_city/city_center/walls", "minecraft:empty", []poolEntry{
		{1, "city_center_walls_bottom_1"}, {1, "city_center_walls_bottom_2"}, {1, "city_center_walls_bottom_left_corner"},
		{1, "city_center_walls_bottom_right_corner_1"}, {1, "city_center_walls_bottom_right_corner_2"}, {1, "city_center_walls_left"},
		{1, "city_center_walls_right"}, {1, "city_center_walls_top"}, {1, "city_center_walls_top_right_corner"},
		{1, "city_center_walls_top_left_corner"},
	}},
	{"minecraft:ancient_city/city/entrance", "minecraft:empty", []poolEntry{
		{1, "city_entrance_entrance_connector"}, {1, "city_entrance_entrance_path_1"}, {1, "city_entrance_entrance_path_2"},
		{1, "city_entrance_entrance_path_3"}, {1, "city_entrance_entrance_path_4"}, {1, "city_entrance_entrance_path_5"},
	}},
}

// TRANSCRIBED TABLE: the ancient city's block-rule lists (degradation), from the list builder 0x53b6c70 as
// the study emulated it (NOTES-structures-phase2 P9.1; the study's p9/rules_ancient.txt). Every rule's
// target is AlwaysTrueExcept(ancientProtected, 1) and its position AlwaysTrue. Bff18c08 in the dump is
// deepslate_bricks (P9.1's reading), Bff130e0 an unnamed block left out here.
var (
	ancientProtected = []string{"mob_spawner", "sculk_sensor", "redstone_lamp", "lit_redstone_lamp", "reinforced_deepslate",
		"redstone_wire", "powered_repeater", "unpowered_repeater", "powered_comparator", "unpowered_comparator"}
	ancientReplaceable = []string{"deepslate", "deepslate_bricks", "deepslate_tiles", "cobbled_deepslate", "cracked_deepslate_bricks",
		"cracked_deepslate_tiles", "deepslate_tile_slab", "deepslate_brick_slab", "deepslate_brick_stairs", "gray_wool"}
)

func ancientRule(src rulePred, result string) blockRule {
	return blockRule{src: src, tgt: rulePred{kind: predExcept, names: ancientProtected, p: 1}, result: result}
}

func bmr(name string, p float32) rulePred {
	return rulePred{kind: predMatchRandom, names: []string{name}, p: p}
}

var (
	ancientCrackBricks = ancientRule(bmr("deepslate_bricks", 0.3), "cracked_deepslate_bricks")
	ancientCrackTiles  = ancientRule(bmr("deepslate_tiles", 0.3), "cracked_deepslate_tiles")
	ancientLantern     = ancientRule(bmr("soul_lantern", 0.05), "air")
	ancientRemove      = ancientRule(rulePred{kind: predFound, names: ancientReplaceable, p: 0.05}, "air")
	ancientRuleLists   = map[string][]blockRule{
		"start":   {ancientCrackBricks, ancientCrackTiles, ancientLantern},
		"generic": {ancientCrackBricks, ancientCrackTiles, ancientLantern, ancientRemove},
		"walls": {ancientCrackBricks, ancientCrackTiles, ancientRule(bmr("deepslate_tile_slab", 0.3), "air"), ancientLantern,
			ancientRemove},
	}
)

// ancientListOf is an element's rule list (P8, from out/anc_elem.ev): city centres the start list,
// structures the generic one, walls the walls one.
func ancientListOf(name string) []blockRule {
	switch {
	case strings.HasPrefix(name, "city_center_"):
		return ancientRuleLists["start"]
	case strings.HasPrefix(name, "structures_"):
		return ancientRuleLists["generic"]
	case strings.HasPrefix(name, "walls_"):
		return ancientRuleLists["walls"]
	}
	return nil
}
