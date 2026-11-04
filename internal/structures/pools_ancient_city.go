package structures

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
