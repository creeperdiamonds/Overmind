package worldgen

import "strings"

// The placed-feature table: the study's global order (each feature's RNG stream depends on its step
// and index in this list, section 7), and which biome has which. Parameters were transcribed from
// knowledge of the game and corrected against the study's trace fixtures; the notes' data folder was
// not used. No Mojang file is in this repo.

// featureOrder lists each step's placed features in index order.
var featureOrder = map[int]string{
	1:  "lake_lava_underground lake_lava_surface",
	2:  "iceberg_packed iceberg_blue amethyst_geode large_dripstone forest_rock",
	3:  "fossil_upper fossil_lower monster_room monster_room_deep",
	4:  "ice_spike ice_patch desert_well blue_ice",
	6:  "ore_dirt ore_gravel ore_granite_upper ore_granite_lower ore_diorite_upper ore_diorite_lower ore_andesite_upper ore_andesite_lower ore_tuff ore_coal_upper ore_coal_lower ore_iron_upper ore_iron_middle ore_iron_small ore_gold ore_gold_lower ore_redstone ore_redstone_lower ore_diamond ore_diamond_medium ore_diamond_large ore_diamond_buried ore_lapis ore_lapis_buried ore_copper_large ore_copper underwater_magma ore_clay ore_gold_extra disk_grass disk_sand disk_clay disk_gravel ore_emerald",
	7:  "sculk_vein sculk_patch_deep_dark dripstone_cluster pointed_dripstone ore_infested",
	8:  "spring_water spring_lava spring_lava_frozen",
	9:  "glow_lichen trees_windswept_savanna bamboo bamboo_vegetation trees_sparse_jungle trees_badlands bamboo_light trees_jungle patch_tall_grass trees_savanna flower_warm patch_grass_jungle patch_grass_savanna pale_garden_vegetation pale_moss_patch pale_garden_flowers flower_pale_garden dark_forest_vegetation flower_forest_flowers trees_flower_forest flower_flower_forest forest_flowers wildflowers_birch_forest birch_tall trees_birch trees_birch_and_oak_leaf_litter patch_tall_grass_2 lush_caves_ceiling_vegetation cave_vines lush_caves_clay lush_caves_vegetation rooted_azalea_tree spore_blossom classic_vines_cave_feature patch_sunflower patch_grass_meadow flower_meadow trees_meadow wildflowers_meadow trees_windswept_forest patch_large_fern trees_old_growth_pine_taiga trees_old_growth_spruce_taiga trees_taiga trees_grove trees_windswept_hills trees_snowy trees_mangrove trees_swamp flower_swamp trees_water patch_bush trees_plains flower_plains patch_grass_plain flower_cherry trees_cherry flower_default patch_grass_taiga patch_grass_forest patch_grass_taiga_2 patch_grass_normal patch_dead_bush brown_mushroom_old_growth red_mushroom_old_growth patch_waterlily brown_mushroom_swamp red_mushroom_swamp patch_grass_badlands patch_dry_grass_desert patch_dead_bush_2 patch_dry_grass_badlands patch_dead_bush_badlands brown_mushroom_normal red_mushroom_normal patch_sugar_cane_desert patch_sugar_cane_badlands patch_leaf_litter patch_sugar_cane_swamp mushroom_island_vegetation brown_mushroom_taiga red_mushroom_taiga patch_pumpkin patch_cactus_desert patch_cactus_decorated patch_firefly_bush_swamp patch_firefly_bush_near_water_swamp seagrass_swamp patch_sugar_cane patch_firefly_bush_near_water seagrass_river vines patch_melon_sparse patch_melon patch_berry_common patch_berry_rare warm_ocean_vegetation seagrass_warm sea_pickle seagrass_deep_warm kelp_warm seagrass_normal seagrass_deep seagrass_cold seagrass_deep_cold kelp_cold",
	10: "freeze_top_layer",
}

const (
	everyBiomeFeatures = "lake_lava_underground lake_lava_surface amethyst_geode monster_room monster_room_deep ore_dirt ore_gravel ore_granite_upper ore_granite_lower ore_diorite_upper ore_diorite_lower ore_andesite_upper ore_andesite_lower ore_tuff ore_coal_upper ore_coal_lower ore_iron_upper ore_iron_middle ore_iron_small ore_gold ore_gold_lower ore_redstone ore_redstone_lower ore_diamond ore_diamond_medium ore_diamond_large ore_diamond_buried ore_lapis ore_lapis_buried ore_copper underwater_magma disk_sand disk_clay disk_gravel spring_water spring_lava glow_lichen brown_mushroom_normal red_mushroom_normal patch_sugar_cane patch_pumpkin patch_firefly_bush_near_water freeze_top_layer "
	mountainFeatures   = "ore_emerald ore_infested "
	defaultGrass       = "flower_default patch_grass_badlands "
	plainsVegetation   = "patch_tall_grass_2 patch_bush trees_plains flower_plains patch_grass_plain "
	forestVegetation   = "forest_flowers flower_default patch_grass_forest "
	oceanBase          = "trees_water flower_default patch_grass_badlands "
)

// biomeOwnFeatures lists what each biome has beyond everyBiomeFeatures.
var biomeOwnFeatures = map[string]string{
	"plains":                   plainsVegetation,
	"sunflower_plains":         "patch_sunflower " + plainsVegetation,
	"snowy_plains":             "trees_snowy " + defaultGrass,
	"ice_spikes":               "ice_spike ice_patch trees_snowy " + defaultGrass,
	"desert":                   "fossil_upper fossil_lower desert_well patch_dry_grass_desert patch_dead_bush_2 patch_sugar_cane_desert patch_cactus_desert " + defaultGrass,
	"swamp":                    "fossil_upper fossil_lower disk_grass trees_swamp flower_swamp patch_grass_normal patch_dead_bush patch_waterlily brown_mushroom_swamp red_mushroom_swamp patch_sugar_cane_swamp patch_firefly_bush_swamp patch_firefly_bush_near_water_swamp seagrass_swamp",
	"mangrove_swamp":           "fossil_upper fossil_lower disk_grass trees_mangrove patch_grass_normal patch_dead_bush patch_waterlily seagrass_swamp patch_firefly_bush_near_water_swamp",
	"forest":                   "trees_birch_and_oak_leaf_litter patch_bush patch_tall_grass_2 " + forestVegetation,
	"flower_forest":            "flower_forest_flowers trees_flower_forest flower_flower_forest " + defaultGrass,
	"birch_forest":             "wildflowers_birch_forest trees_birch patch_bush patch_tall_grass_2 " + forestVegetation,
	"old_growth_birch_forest":  "wildflowers_birch_forest birch_tall patch_bush patch_tall_grass_2 " + forestVegetation,
	"dark_forest":              "dark_forest_vegetation patch_leaf_litter patch_tall_grass_2 " + forestVegetation,
	"pale_garden":              "pale_garden_vegetation pale_moss_patch pale_garden_flowers flower_pale_garden patch_tall_grass_2 " + forestVegetation,
	"taiga":                    "trees_taiga patch_large_fern patch_berry_common brown_mushroom_taiga red_mushroom_taiga patch_grass_taiga_2 patch_tall_grass_2 flower_default",
	"snowy_taiga":              "trees_taiga patch_large_fern patch_berry_rare brown_mushroom_taiga red_mushroom_taiga patch_grass_taiga_2 patch_tall_grass_2 flower_default",
	"old_growth_pine_taiga":    "forest_rock patch_large_fern trees_old_growth_pine_taiga patch_grass_taiga patch_dead_bush brown_mushroom_old_growth red_mushroom_old_growth patch_berry_common patch_tall_grass_2 flower_default",
	"old_growth_spruce_taiga":  "forest_rock patch_large_fern trees_old_growth_spruce_taiga patch_grass_taiga patch_dead_bush brown_mushroom_old_growth red_mushroom_old_growth patch_berry_common patch_tall_grass_2 flower_default",
	"savanna":                  "patch_tall_grass trees_savanna flower_warm patch_grass_savanna",
	"savanna_plateau":          "patch_tall_grass trees_savanna flower_warm patch_grass_savanna",
	"windswept_savanna":        "trees_windswept_savanna flower_default patch_grass_normal",
	"windswept_hills":          "trees_windswept_hills flower_default patch_grass_normal " + mountainFeatures,
	"windswept_gravelly_hills": "trees_windswept_hills flower_default patch_grass_normal " + mountainFeatures,
	"windswept_forest":         "trees_windswept_forest flower_default patch_grass_normal " + mountainFeatures,
	"jungle":                   "bamboo_light trees_jungle flower_warm patch_grass_jungle patch_melon vines",
	"sparse_jungle":            "trees_sparse_jungle flower_warm patch_grass_jungle patch_melon_sparse vines",
	"bamboo_jungle":            "bamboo bamboo_vegetation flower_warm patch_grass_jungle patch_melon vines",
	"badlands":                 "ore_gold_extra patch_grass_badlands patch_dry_grass_badlands patch_dead_bush_badlands patch_sugar_cane_badlands patch_cactus_decorated",
	"eroded_badlands":          "ore_gold_extra patch_grass_badlands patch_dry_grass_badlands patch_dead_bush_badlands patch_sugar_cane_badlands patch_cactus_decorated",
	"wooded_badlands":          "ore_gold_extra trees_badlands patch_grass_badlands patch_dry_grass_badlands patch_dead_bush_badlands patch_sugar_cane_badlands patch_cactus_decorated",
	"meadow":                   "patch_grass_meadow flower_meadow trees_meadow wildflowers_meadow " + mountainFeatures,
	"cherry_grove":             "trees_cherry flower_cherry patch_grass_plain " + mountainFeatures,
	"grove":                    "spring_lava_frozen trees_grove patch_tall_grass_2 patch_bush flower_default " + forestVegetation + mountainFeatures,
	"snowy_slopes":             "spring_lava_frozen patch_tall_grass_2 flower_default " + mountainFeatures,
	"frozen_peaks":             "spring_lava_frozen patch_tall_grass_2 patch_bush flower_default " + forestVegetation + mountainFeatures,
	"jagged_peaks":             "spring_lava_frozen patch_tall_grass_2 flower_default " + mountainFeatures,
	"stony_peaks":              "patch_tall_grass_2 patch_bush " + forestVegetation + mountainFeatures,
	"beach":                    defaultGrass,
	"snowy_beach":              defaultGrass,
	"stony_shore":              defaultGrass,
	"river":                    "trees_water seagrass_river patch_bush " + defaultGrass,
	"frozen_river":             "trees_water " + defaultGrass,
	"mushroom_fields":          "mushroom_island_vegetation",
	"dripstone_caves":          "large_dripstone dripstone_cluster pointed_dripstone ore_copper_large " + plainsVegetation,
	"lush_caves":               "ore_clay lush_caves_ceiling_vegetation cave_vines lush_caves_clay lush_caves_vegetation rooted_azalea_tree spore_blossom classic_vines_cave_feature patch_tall_grass_2",
	"deep_dark":                "sculk_vein sculk_patch_deep_dark " + plainsVegetation,
	"ocean":                    oceanBase + "seagrass_normal kelp_cold",
	"deep_ocean":               oceanBase + "seagrass_deep kelp_cold",
	"cold_ocean":               oceanBase + "seagrass_cold kelp_cold",
	"deep_cold_ocean":          oceanBase + "seagrass_deep_cold kelp_cold",
	"lukewarm_ocean":           oceanBase + "seagrass_warm kelp_warm sea_pickle",
	"deep_lukewarm_ocean":      oceanBase + "seagrass_deep_warm kelp_warm sea_pickle",
	"warm_ocean":               oceanBase + "warm_ocean_vegetation seagrass_warm sea_pickle",
	"frozen_ocean":             oceanBase + "iceberg_packed iceberg_blue blue_ice seagrass_cold",
	"deep_frozen_ocean":        oceanBase + "iceberg_packed iceberg_blue blue_ice",
}

// placementInBiomes is the driver's skip: a feature no biome of the 3 x 3 lists does not run.
func placementInBiomes(pl *placement, set *[2]uint64) bool {
	return pl.biomes[0]&set[0] != 0 || pl.biomes[1]&set[1] != 0
}

func biomeHasFeature(b uint8, pl *placement) bool { return pl.biomes[b>>6]&(1<<(b&63)) != 0 }

// placed builds a placement's usual chain ending in a biome filter where the game has one.
type tableBuilder struct {
	prof  *Profile
	byN   map[string]*placement
	extra []*placement
}

func (t *tableBuilder) def(name string, feat feature, mods ...modifier) {
	pl := t.byN[name]
	if pl == nil {
		pl = newNestedPlacement(nil)
		pl.name, pl.step = name, -1
		t.byN[name] = pl
	}
	for i, m := range mods {
		if _, ok := m.(biomeMod); ok {
			mods[i] = biomeMod{pl}
		}
	}
	pl.mods, pl.feat = mods, feat
	pl.tracked = func(d *decoration, p pos) bool {
		ok := feat(d, p)
		d.placed = d.placed || ok
		return ok
	}
}

// newNestedPlacement makes a placed feature used inside a selector, with its own modifiers and no biome.
func newNestedPlacement(feat feature, mods ...modifier) *placement {
	return &placement{mods: mods, feat: feat, tracked: func(d *decoration, p pos) bool {
		ok := feat(d, p)
		d.placed = d.placed || ok
		return ok
	}}
}

// buildPlacements makes a profile's whole table in the driver's order.
func buildPlacements(prof *Profile) []*placement {
	t := &tableBuilder{prof: prof, byN: map[string]*placement{}}
	defineOres(t)
	defineSteps(t)
	var out []*placement
	for step := 0; step <= 10; step++ {
		names, ok := featureOrder[step]
		if !ok {
			continue
		}
		for i, n := range strings.Fields(names) {
			pl := t.byN[n]
			if pl == nil || pl.feat == nil {
				continue // A known gap: see NOTES.md.
			}
			pl.step, pl.idx = step, i
			out = append(out, pl)
		}
	}
	for biome, own := range biomeOwnFeatures {
		id := biomeIdx[biome]
		for _, n := range strings.Fields(everyBiomeFeatures + own) {
			if n == "ore_copper" && biome == "dripstone_caves" {
				continue // It has the large copper ore instead.
			}
			if pl := t.byN[n]; pl != nil {
				pl.biomes[id>>6] |= 1 << (id & 63)
			}
		}
	}
	// A profile's own additions sit past the Java indexes, so Java's streams stay unchanged.
	return append(out, t.extra...)
}

func common(n intProvider, h heightProvider) []modifier {
	return []modifier{countMod{n}, inSquareMod{}, heightRangeMod{h}, biomeMod{}}
}

func rare(chance int, h heightProvider) []modifier {
	return []modifier{rarityMod{chance}, inSquareMod{}, heightRangeMod{h}, biomeMod{}}
}

// defineOres writes the ore table: Java's values, with the Bedrock data files' differences where the
// profile has them.
func defineOres(t *tableBuilder) {
	p := t.prof
	def := func(feat, block, list string, size int, mods []modifier) {
		tg := oreTarget{test: tagBaseStone, state: stateOf(block)}
		if list != "" {
			tg = oreTarget{list: blockSet(list), state: stateOf(block)}
		}
		t.def(feat, oreFeature(oreSpec{size: size, targets: []oreTarget{tg}}), mods...)
	}
	def("ore_dirt", "dirt", p.dirtReplaces, 33, common(constInt(7), uniHeight(0, 160)))
	def("ore_gravel", "gravel", p.gravelReplaces, 33, common(constInt(14), uniHeight(minY, maxY)))
	def("ore_granite_upper", "granite", p.rockReplaces, 64, rare(6, uniHeight(64, 128)))
	def("ore_granite_lower", "granite", p.rockReplaces, 64, common(constInt(2), uniHeight(0, 60)))
	def("ore_diorite_upper", "diorite", p.rockReplaces, 64, rare(6, uniHeight(64, 128)))
	def("ore_diorite_lower", "diorite", p.rockReplaces, 64, common(constInt(2), uniHeight(0, 60)))
	def("ore_andesite_upper", "andesite", p.rockReplaces, 64, rare(6, uniHeight(64, 128)))
	def("ore_andesite_lower", "andesite", p.rockReplaces, 64, common(constInt(2), uniHeight(0, 60)))
	def("ore_tuff", "tuff", p.gravelReplaces, 64, common(constInt(2), uniHeight(minY, 0)))
	def("ore_clay", "clay", "", 33, common(constInt(46), uniHeight(minY, 256)))

	ore := func(feat, stone, deep string, size int, discard float32, mods []modifier) {
		t.def(feat, oreFeature(oreSpec{size: size, discard: discard, targets: stoneThenDeep(p, stone, deep)}), mods...)
	}
	coal, dcoal := "coal_ore", "deepslate_coal_ore"
	iron, diron := "iron_ore", "deepslate_iron_ore"
	gold, dgold := "gold_ore", "deepslate_gold_ore"
	red, dred := "redstone_ore[lit=false]", "deepslate_redstone_ore[lit=false]"
	dia, ddia := "diamond_ore", "deepslate_diamond_ore"
	lap, dlap := "lapis_ore", "deepslate_lapis_ore"
	cu, dcu := "copper_ore", "deepslate_copper_ore"
	ore("ore_coal_upper", coal, dcoal, 17, 0, common(constInt(30), uniHeight(136, maxY)))
	ore("ore_coal_lower", coal, dcoal, 17, 0.5, common(constInt(20), trapHeight(0, 192, 0)))
	ore("ore_iron_upper", iron, diron, p.ironSize, 0, common(constInt(90), trapHeight(80, 384, 0)))
	ore("ore_iron_middle", iron, diron, p.ironSize, 0, common(constInt(10), trapHeight(-24, 56, 0)))
	ore("ore_iron_small", iron, diron, 4, 0, common(constInt(10), uniHeight(minY, 72)))
	ore("ore_gold", gold, dgold, 9, 0.5, common(constInt(4), trapHeight(-64, 32, 0)))
	ore("ore_gold_lower", gold, dgold, 9, 0.5, common(uniInt(0, 1), uniHeight(-64, -48)))
	ore("ore_gold_extra", gold, dgold, 9, 0, common(constInt(50), uniHeight(32, 256)))
	ore("ore_redstone", red, dred, 8, 0, common(constInt(4), uniHeight(minY, 15)))
	ore("ore_redstone_lower", red, dred, 8, 0, common(constInt(8), trapHeight(-96, -32, 0)))
	ore("ore_diamond", dia, ddia, 4, 0.5, common(constInt(7), trapHeight(-144, 16, 0)))
	ore("ore_diamond_medium", dia, ddia, p.diamondMediumSize, 0.5, common(constInt(2), uniHeight(-64, -4)))
	ore("ore_diamond_large", dia, ddia, 12, 0.7, rare(9, trapHeight(-144, 16, 0)))
	ore("ore_diamond_buried", dia, ddia, 8, 1, common(constInt(4), trapHeight(-144, 16, 0)))
	ore("ore_lapis", lap, dlap, 7, 0, common(constInt(2), trapHeight(-32, 32, 0)))
	ore("ore_lapis_buried", lap, dlap, 7, 1, common(constInt(4), uniHeight(minY, 64)))
	ore("ore_copper", cu, dcu, 10, 0, common(constInt(16), trapHeight(-16, 112, 0)))
	ore("ore_copper_large", cu, dcu, 20, 0, common(constInt(16), trapHeight(-16, 112, 0)))
	emerald := []oreTarget{{list: blockSet("stone"), state: stateOf("emerald_ore")}, {list: blockSet("deepslate"), state: stateOf("deepslate_emerald_ore")}}
	t.def("ore_emerald", oreFeature(oreSpec{size: 3, targets: emerald}), common(constInt(100), uniHeight(-16, 480))...)
	if p.infestedBedrock {
		tg := []oreTarget{{list: blockSet("stone"), state: stateOf("infested_stone")}, {list: blockSet("deepslate"), state: stateOf("infested_deepslate[axis=y]")}}
		t.def("ore_infested", oreFeature(oreSpec{size: 8, targets: tg}), common(constInt(14), uniHeight(0, 64))...)
	} else {
		tg := []oreTarget{{test: tagStoneOre, state: stateOf("infested_stone")}, {test: tagDeepslateOre, state: stateOf("infested_deepslate[axis=y]")}}
		t.def("ore_infested", oreFeature(oreSpec{size: 9, targets: tg}), common(constInt(14), uniHeight(minY, 63))...)
	}
	if p.extraMountainCoal {
		extra := &placement{name: "ore_coal_mountains", step: 6, idx: 40, biomes: [2]uint64{}}
		mods := common(constInt(20), uniHeight(128, 256))
		mods[len(mods)-1] = biomeMod{extra}
		extra.mods, extra.feat = mods, oreFeature(oreSpec{size: 17, discard: 0.5, targets: stoneThenDeep(p, coal, dcoal)})
		for _, b := range []string{"cherry_grove", "frozen_peaks", "grove", "jagged_peaks", "meadow", "snowy_slopes", "stony_peaks"} {
			id := biomeIdx[b]
			extra.biomes[id>>6] |= 1 << (id & 63)
		}
		t.extra = append(t.extra, extra)
	}
}

// defineSteps is filled in with the other features.
func defineSteps(t *tableBuilder) {}
