package worldgen

import "strings"

// The non-ore placed features. Parameters were transcribed from knowledge of the game and corrected
// against the study's trace fixtures. Features the study does not write up are left out: see NOTES.md.

// geodeNoise is the study's fresh noise for a geode, the same for every geode of a seed. It is made on
// first use and kept by the generator, so it goes when the generator does.
func (g *Generator) geodeNoise() *normalNoise {
	g.geodeOnce.Do(func() { g.geode = legacyNormal(g.seed, -4, []float64{1}) })
	return g.geode
}

func patch(n, xz, y int, filter predicate) []modifier {
	return []modifier{countMod{constInt(n)}, offsetMod{trapInt(xz), trapInt(y)}, filterMod{filter}}
}

func chain(parts ...[]modifier) []modifier {
	var out []modifier
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func mods(list ...modifier) []modifier { return list }

var airOnly = func(d *decoration, p pos) bool { return d.at(p) == sAir }

// countExtra is the game's weighted count: base most times, base plus extra sometimes.
func countExtra(base int, oneIn, extra int) intProvider {
	return weightedInts(weightedInt{oneIn - 1, constInt(base)}, weightedInt{1, constInt(base + extra)})
}

func treeBase(n intProvider, depth int) []modifier {
	return mods(countMod{n}, inSquareMod{}, waterDepthMod{depth}, heightmapMod{hmOceanFloor}, biomeMod{})
}

// survivingTrees is treeBase for the tree placements that also check, before choosing a tree, that
// the sapling would survive there, as Java's trees_birch, trees_snowy and trees_badlands do.
func survivingTrees(n intProvider, sapling string) []modifier {
	return append(treeBase(n, 0), filterMod{wouldSurvive(sapling)})
}

func checked(cfg *treeCfg, sapling string) *placement {
	return newNestedPlacement(treeFeature(cfg), filterMod{wouldSurvive(sapling)})
}

func logP(name string) stateProvider { return simpleState(name + "[axis=y]") }

func leafP(name string) stateProvider { return simpleState(leafName(name)) }

func h(base, a, b int) trunkHeights { return trunkHeights{base, a, b} }

func two(limit, lower, upper int) (int, int, int) { return limit, lower, upper }

func treeOf(log, leaf string, tr trunkPlacer, f foliagePlacer, limit, lower, upper int, ignore bool, decos ...treeDecorator) *treeCfg {
	return &treeCfg{log: logP(log), foliageState: leafP(leaf), trunk: tr, foliage: f, layersLimit: limit, lower: lower, upper: upper, minClipped: -1, ignoreVines: ignore, decorators: decos}
}

func blob(r, o, fh int) foliagePlacer {
	return blobFoliage{foliageBase{constInt(r), constInt(o)}, fh}
}

func oakTree(decos ...treeDecorator) *treeCfg {
	return treeOf("oak_log", "oak_leaves", straightTrunk{h(4, 2, 0)}, blob(2, 0, 3), 1, 0, 1, true, decos...)
}

func fancyOakTree(decos ...treeDecorator) *treeCfg {
	c := treeOf("oak_log", "oak_leaves", fancyTrunk{h(3, 11, 0)}, fancyFoliage{foliageBase{constInt(2), constInt(4)}, 4}, 0, 0, 0, true, decos...)
	c.minClipped = 4
	return c
}

func birchTree(decos ...treeDecorator) *treeCfg {
	return treeOf("birch_log", "birch_leaves", straightTrunk{h(5, 2, 0)}, blob(2, 0, 3), 1, 0, 1, true, decos...)
}

func superBirchTree(decos ...treeDecorator) *treeCfg {
	return treeOf("birch_log", "birch_leaves", straightTrunk{h(5, 2, 6)}, blob(2, 0, 3), 1, 0, 1, true, decos...)
}

func spruceTree() *treeCfg {
	return treeOf("spruce_log", "spruce_leaves", straightTrunk{h(5, 2, 1)}, spruceFoliage{foliageBase{uniInt(2, 3), uniInt(0, 2)}, uniInt(1, 2)}, 2, 0, 2, true)
}

func pineTree() *treeCfg {
	return treeOf("spruce_log", "spruce_leaves", straightTrunk{h(6, 4, 0)}, pineFoliage{foliageBase{constInt(1), constInt(1)}, uniInt(3, 4)}, 2, 0, 2, true)
}

func acaciaTree() *treeCfg {
	return treeOf("acacia_log", "acacia_leaves", forkingTrunk{h(5, 2, 2)}, acaciaFoliage{foliageBase{constInt(2), constInt(0)}}, 1, 0, 2, true)
}

func jungleTree(decos ...treeDecorator) *treeCfg {
	return treeOf("jungle_log", "jungle_leaves", straightTrunk{h(4, 8, 0)}, blob(2, 0, 3), 1, 0, 1, true, decos...)
}

func jungleBush() *treeCfg {
	return treeOf("jungle_log", "oak_leaves", straightTrunk{h(1, 0, 0)}, bushFoliage{foliageBase{constInt(2), constInt(1)}, 2}, 0, 0, 0, true)
}

func megaJungle() *treeCfg {
	return treeOf("jungle_log", "jungle_leaves", megaJungleTrunk{h(10, 2, 19)}, jungleFoliage{foliageBase{constInt(2), constInt(0)}, 2}, 1, 1, 2, false, trunkVine, leaveVine(0.25))
}

func podzolGround(d *decoration, p pos) (bs, bool) { return sPodzol, true }

func megaSpruce(crownLo, crownHi int) *treeCfg {
	alter := alterGround(func(d *decoration, p pos) (bs, bool) {
		s := d.at(p)
		if s.tagged(tagDirtLike) && (d.at(p.step(dUp)).isAir() || d.at(p.step(dUp)).has(fReplaceable)) {
			return sPodzol, true
		}
		return 0, false
	})
	return treeOf("spruce_log", "spruce_leaves", giantTrunk{h(13, 2, 14)}, megaPineFoliage{foliageBase{constInt(0), constInt(0)}, uniInt(crownLo, crownHi)}, 1, 1, 2, false, alter)
}

func darkOak(decos ...treeDecorator) *treeCfg {
	c := treeOf("dark_oak_log", "dark_oak_leaves", darkOakTrunk{h(6, 2, 1)}, darkOakFoliage{foliageBase{constInt(0), constInt(0)}}, 1, 0, 2, true, decos...)
	c.threeLayers, c.layersLimit, c.upperLimit, c.lower, c.middle, c.upper = true, 1, 1, 0, 1, 2
	return c
}

// litter is the ground decorator of forest trees: leaf litter of one to most segments.
func litter(tries, radius, most int) treeDecorator {
	return placeOnGround(tries, radius, 2, segmented("leaf_litter", "segment_amount", 1, most))
}

func defineSteps(t *tableBuilder) {
	// Lakes, geode, rooms, rocks, springs, magma, disks.
	t.def("lake_lava_underground", lakeFeature(sLava, sStone), rarityMod{9}, inSquareMod{}, heightRangeMod{uniHeight(0, maxY)},
		scanMod{dDown, allOf(notP(emptyP), func(d *decoration, p pos) bool { return p.y-5 >= minY && p.y-5 <= maxY }), func(*decoration, pos) bool { return true }, 32},
		surfaceThresholdMod{hmOceanFloorWG, -1 << 30, -5}, biomeMod{})
	t.def("lake_lava_surface", lakeFeature(sLava, sStone), rarityMod{200}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{})
	t.def("amethyst_geode", geodeFeature, rarityMod{24}, inSquareMod{}, heightRangeMod{uniHeight(minY+6, 30)}, biomeMod{})
	t.def("monster_room", roomFeature, countMod{constInt(10)}, inSquareMod{}, heightRangeMod{uniHeight(0, maxY)}, biomeMod{})
	t.def("monster_room_deep", roomFeature, countMod{constInt(4)}, inSquareMod{}, heightRangeMod{uniHeight(minY+6, -1)}, biomeMod{})
	t.def("forest_rock", blobFeature(stateOf("mossy_cobblestone"), anyOf(matchTag(tagDirtLike), matchTag(tagBaseStone))), countMod{constInt(2)}, inSquareMod{}, heightmapMod{hmMotionBlocking}, biomeMod{})
	t.def("ice_spike", spikeFeature(sPacked, blockSet("snow_block"), blockSet("dirt sand gravel snow_block ice")), countMod{constInt(3)}, inSquareMod{}, heightmapMod{hmMotionBlocking}, biomeMod{})
	t.def("ice_patch", diskFeature(uniInt(2, 3), 1, matchBlocks("dirt grass_block podzol coarse_dirt mycelium snow_block ice"), always(sPacked)),
		countMod{constInt(2)}, inSquareMod{}, heightmapMod{hmMotionBlocking}, offsetMod{constInt(0), constInt(-1)}, filterMod{matchBlocks("snow_block")}, biomeMod{})
	t.def("blue_ice", blueIceFeature, countMod{uniInt(0, 19)}, inSquareMod{}, heightRangeMod{uniHeight(30, 61)}, biomeMod{})
	waterOnly := filterMod{func(d *decoration, p pos) bool { return d.at(p) == sWater }}
	diskMods := func(n int) []modifier {
		return mods(countMod{constInt(n)}, inSquareMod{}, heightmapMod{hmOceanFloorWG}, waterOnly, biomeMod{})
	}
	sandstone := func(d *decoration, p pos) (bs, bool) {
		if d.at(p.step(dDown)).isAir() {
			return sSandst, true
		}
		return sSand, true
	}
	t.def("disk_sand", diskFeature(uniInt(2, 6), 2, matchBlocks("dirt grass_block"), sandstone), diskMods(3)...)
	t.def("disk_clay", diskFeature(uniInt(2, 3), 1, matchBlocks("dirt clay"), always(stateOf("clay"))), diskMods(1)...)
	t.def("disk_gravel", diskFeature(uniInt(2, 5), 2, matchBlocks("dirt grass_block"), always(sGravel)), diskMods(1)...)
	t.def("disk_grass", diskFeature(uniInt(2, 6), 2, matchBlocks("dirt mud"), func(d *decoration, p pos) (bs, bool) {
		u := d.at(p.step(dUp))
		if u.has(fSolid) || u == sWater {
			return sDirt, true
		}
		return sGrass, true
	}), countMod{constInt(1)}, inSquareMod{}, heightmapMod{hmOceanFloorWG}, offsetMod{constInt(0), constInt(-1)}, filterMod{matchBlocks("mud")}, biomeMod{})
	t.def("underwater_magma", magmaFeature(5, 1, 0.5), dataCountMod{"underwater_magma", t.prof.magmaCount}, inSquareMod{}, heightRangeMod{uniHeight(minY, 256)}, surfaceThresholdMod{hmOceanFloorWG, -1 << 30, -2}, biomeMod{})
	waterRock := blockSet("stone granite diorite andesite deepslate tuff calcite dirt snow_block powder_snow packed_ice")
	lavaRock := blockSet("stone granite diorite andesite deepslate tuff")
	frozenRock := blockSet("snow_block powder_snow packed_ice")
	t.def("spring_water", springFeature(sWater, waterRock, 4, 1, true), countMod{constInt(25)}, inSquareMod{}, heightRangeMod{uniHeight(minY, 192)}, biomeMod{})
	t.def("spring_lava", springFeature(sLava, lavaRock, 4, 1, true), countMod{constInt(20)}, inSquareMod{}, heightRangeMod{veryBiasedHeight(minY, maxY-8, 8)}, biomeMod{})
	t.def("spring_lava_frozen", springFeature(sLava, frozenRock, 4, 1, true), countMod{constInt(20)}, inSquareMod{}, heightRangeMod{veryBiasedHeight(minY, maxY-8, 8)}, biomeMod{})
	t.def("glow_lichen", lichenFeature(blockSet("stone andesite diorite granite dripstone_block calcite tuff deepslate"), 20, 0.5),
		dataCountMod{"glow_lichen", t.prof.lichenCount}, heightRangeMod{uniHeight(minY, 256)}, inSquareMod{}, surfaceThresholdMod{hmOceanFloorWG, -1 << 30, -13}, biomeMod{})
	t.def("freeze_top_layer", freezeFeature, biomeMod{})

	// Trees.
	bee2, bee5 := beehive(0.002), beehive(0.05)
	oak, birch := "oak_sapling", "birch_sapling"
	treeSel := func(def *placement, list ...selEntry) feature { return randomSelector(def, list...) }
	checkedOak := func(decos ...treeDecorator) *placement { return checked(oakTree(decos...), oak) }
	checkedFancy := func(decos ...treeDecorator) *placement { return checked(fancyOakTree(decos...), oak) }
	checkedBirch := func(decos ...treeDecorator) *placement { return checked(birchTree(decos...), birch) }
	spruce := func() *placement { return checked(spruceTree(), "spruce_sapling") }
	pine := func() *placement { return checked(pineTree(), "spruce_sapling") }
	// Java's grove trees grow on snow: scan up through powder snow, at most 8 blocks, then the block
	// below must be snow block or powder snow (spruce_on_snow, pine_on_snow). The trunk turns it to dirt.
	onSnow := func(cfg *treeCfg) *placement {
		notPowder := notP(matchBlocks("powder_snow"))
		yes := func(*decoration, pos) bool { return true }
		return newNestedPlacement(treeFeature(cfg), scanMod{dUp, notPowder, yes, 8},
			filterMod{offsetP(0, -1, 0, matchBlocks("snow_block powder_snow"))})
	}
	third := float32(0.33333334)

	shrooms := attachedToLogs(0.1, weightedStates(weightedState{2, stateOf("red_mushroom")}, weightedState{1, stateOf("brown_mushroom")}), []dir{dUp})
	fallen := func(log string, lo, hi int, sapling string) *placement {
		return newNestedPlacement(fallenTree(log, uniInt(lo, hi), []treeDecorator{trunkVine}, []treeDecorator{shrooms}), filterMod{wouldSurvive(sapling)})
	}
	fallenOak := func() *placement { return fallen("oak_log", 4, 7, oak) }
	fallenBirch := func(lo, hi int) *placement { return fallen("birch_log", lo, hi, birch) }
	fallenSpruce := func() *placement { return fallen("spruce_log", 6, 10, "spruce_sapling") }
	fallenJungle := func() *placement { return fallen("jungle_log", 4, 11, "jungle_sapling") }
	lit := []treeDecorator{litter(96, 4, 3), litter(150, 2, 4)}
	withLit := func(extra ...treeDecorator) []treeDecorator {
		return append(append([]treeDecorator{}, extra...), lit...)
	}

	t.def("trees_plains", treeSel(checkedOak(bee5), selEntry{third, checkedFancy(bee5)}, selEntry{0.0125, fallenOak()}),
		mods(countMod{countExtra(0, 20, 1)}, inSquareMod{}, waterDepthMod{0}, heightmapMod{hmOceanFloor}, filterMod{wouldSurvive(oak)}, biomeMod{})...)
	t.def("trees_birch_and_oak_leaf_litter", treeSel(checkedOak(withLit(bee2)...), selEntry{0.0025, fallenBirch(5, 8)}, selEntry{0.2, checkedBirch(withLit(bee2)...)}, selEntry{0.1, checkedFancy(withLit(bee2)...)}, selEntry{0.0125, fallenOak()}), treeBase(countExtra(10, 10, 1), 0)...)
	t.def("trees_flower_forest", treeSel(checkedOak(bee2), selEntry{0.0025, fallenBirch(5, 8)}, selEntry{0.2, checkedBirch(bee2)}, selEntry{0.1, checkedFancy(bee2)}, selEntry{0.0125, fallenOak()}), treeBase(countExtra(6, 10, 1), 0)...)
	t.def("trees_birch", treeSel(checkedBirch(bee2), selEntry{0.0125, fallenBirch(5, 8)}), survivingTrees(countExtra(10, 10, 1), birch)...)
	t.def("birch_tall", treeSel(checked(birchTree(bee2), birch), selEntry{0.00625, fallenBirch(5, 15)}, selEntry{0.5, checked(superBirchTree(bee2), birch)}, selEntry{0.0125, fallenBirch(5, 8)}), treeBase(countExtra(10, 10, 1), 0)...)
	t.def("dark_forest_vegetation", treeSel(checkedOak(lit...),
		selEntry{0.025, newNestedPlacement(hugeMushroom(false, 3))}, selEntry{0.05, newNestedPlacement(hugeMushroom(true, 2))},
		selEntry{0.6666667, checked(darkOak(lit...), "dark_oak_sapling")}, selEntry{0.0025, fallenBirch(5, 8)}, selEntry{0.2, checkedBirch(lit...)}, selEntry{0.0125, fallenOak()}, selEntry{0.1, checkedFancy(lit...)}),
		mods(countMod{constInt(16)}, inSquareMod{}, waterDepthMod{0}, heightmapMod{hmOceanFloor}, biomeMod{})...)
	t.def("trees_taiga", treeSel(spruce(), selEntry{third, pine()}, selEntry{0.0125, fallenSpruce()}), treeBase(countExtra(10, 10, 1), 0)...)
	t.def("trees_old_growth_spruce_taiga", treeSel(spruce(), selEntry{third, checked(megaSpruce(13, 17), "spruce_sapling")}, selEntry{third, pine()}, selEntry{0.0125, fallenSpruce()}), treeBase(countExtra(10, 10, 1), 0)...)
	t.def("trees_old_growth_pine_taiga", treeSel(spruce(), selEntry{0.025641026, checked(megaSpruce(13, 17), "spruce_sapling")}, selEntry{0.30769232, checked(megaSpruce(3, 7), "spruce_sapling")}, selEntry{third, pine()}, selEntry{0.0125, fallenSpruce()}), treeBase(countExtra(10, 10, 1), 0)...)
	t.def("trees_snowy", treeSel(spruce(), selEntry{0.0125, fallenSpruce()}), survivingTrees(countExtra(0, 10, 1), "spruce_sapling")...)
	t.def("trees_grove", treeSel(onSnow(spruceTree()), selEntry{third, onSnow(pineTree())}), treeBase(countExtra(10, 10, 1), 0)...)
	t.def("trees_savanna", treeSel(checkedOak(), selEntry{0.8, checked(acaciaTree(), "acacia_sapling")}, selEntry{0.0125, fallenOak()}), treeBase(countExtra(1, 10, 1), 0)...)
	t.def("trees_windswept_savanna", treeSel(checkedOak(), selEntry{0.8, checked(acaciaTree(), "acacia_sapling")}, selEntry{0.0125, fallenOak()}), treeBase(countExtra(2, 10, 1), 0)...)
	windswept := func() feature {
		return treeSel(checkedOak(), selEntry{0.008325, fallenSpruce()}, selEntry{0.666, spruce()}, selEntry{0.1, checkedFancy()}, selEntry{0.0125, fallenOak()})
	}
	t.def("trees_windswept_hills", windswept(), treeBase(countExtra(0, 10, 1), 0)...)
	t.def("trees_windswept_forest", windswept(), treeBase(countExtra(3, 10, 1), 0)...)
	jungleVines := func() *placement {
		return checked(jungleTree(cocoa(0.2), trunkVine, leaveVine(0.25)), "jungle_sapling")
	}
	t.def("trees_jungle", treeSel(jungleVines(), selEntry{0.1, checkedFancy()}, selEntry{0.5, checked(jungleBush(), "jungle_sapling")}, selEntry{third, checked(megaJungle(), "jungle_sapling")}, selEntry{0.0125, fallenJungle()}), treeBase(countExtra(50, 10, 1), 0)...)
	t.def("trees_sparse_jungle", treeSel(jungleVines(), selEntry{0.1, checkedFancy()}, selEntry{0.5, checked(jungleBush(), "jungle_sapling")}, selEntry{0.0125, fallenJungle()}), treeBase(countExtra(2, 10, 1), 0)...)
	t.def("trees_water", treeSel(checkedOak(), selEntry{0.1, checkedFancy()}, selEntry{0.0125, fallenOak()}), treeBase(countExtra(0, 10, 1), 0)...)
	swampOak := treeOf("oak_log", "oak_leaves", straightTrunk{h(5, 3, 0)}, blob(3, 0, 3), 1, 0, 1, false, leaveVine(0.25))
	t.def("trees_swamp", newNestedOnly(checked(swampOak, oak)), treeBase(countExtra(2, 10, 1), 2)...)
	t.def("trees_badlands", treeSel(checkedOak(lit...), selEntry{0.0125, fallenOak()}), survivingTrees(countExtra(5, 10, 1), oak)...)
	t.def("trees_meadow", treeSel(checked(superBirchTree(bee5), birch), selEntry{0.5, checkedFancy(bee5)}), mods(rarityMod{100}, inSquareMod{}, waterDepthMod{0}, heightmapMod{hmOceanFloor}, biomeMod{})...)
	cherry := treeOf("cherry_log", "cherry_leaves", cherryTrunk{h(7, 1, 0), -4, -3, weightedInts(weightedInt{1, constInt(1)}, weightedInt{1, constInt(2)}, weightedInt{1, constInt(3)}), uniInt(2, 4), uniInt(-1, 0)},
		cherryFoliage{foliageBase{constInt(4), constInt(0)}, constInt(5), 0.25, 0.5, 0.16666667, 0.33333334}, 1, 0, 2, true, bee5)
	t.def("trees_cherry", newNestedOnly(checked(cherry, "cherry_sapling")), treeBase(countExtra(10, 10, 1), 0)...)

	// Mangroves: an upwards branching trunk on roots, unverified against a Java run (no traced chunk
	// holds a mangrove swamp).
	mangroveShort, mangroveTall := checked(mangroveTree(false), "mangrove_propagule"), checked(mangroveTree(true), "mangrove_propagule")
	t.def("trees_mangrove", treeSel(mangroveShort, selEntry{0.85, mangroveTall}),
		mods(countMod{constInt(25)}, inSquareMod{}, waterDepthMod{5}, heightmapMod{hmOceanFloor}, biomeMod{})...) // Each tree checks the propagule survives.

	// Plants. A patch is a count, an offset, a filter and simple_block.
	grass := func(n int, name string, f predicate) {
		t.def(name, simpleBlock(simpleState("short_grass")), chain(mods(countMod{constInt(n)}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{}), patch(32, 7, 3, f))...)
	}
	fernMix := weightedStates(weightedState{1, stateOf("short_grass")}, weightedState{4, stateOf("fern")})
	jungleMix := weightedStates(weightedState{3, stateOf("short_grass")}, weightedState{1, stateOf("fern")})
	grass(2, "patch_grass_forest", airOnly)
	grass(1, "patch_grass_badlands", airOnly)
	grass(5, "patch_grass_normal", airOnly)
	t.def("patch_grass_plain", simpleBlock(simpleState("short_grass")), chain(mods(noiseThresholdMod{-0.8, 5, 10}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{}), patch(32, 7, 3, airOnly))...)
	grass(20, "patch_grass_savanna", airOnly)
	t.def("patch_grass_meadow", simpleBlock(simpleState("short_grass")), chain(mods(noiseThresholdMod{-0.8, 5, 10}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{}), patch(16, 7, 3, airOnly))...)
	t.def("patch_grass_taiga", simpleBlock(fernMix), chain(mods(countMod{constInt(7)}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{}), patch(32, 7, 3, airOnly))...)
	t.def("patch_grass_taiga_2", simpleBlock(fernMix), chain(mods(inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{}), patch(32, 7, 3, airOnly))...)
	t.def("patch_grass_jungle", simpleBlock(jungleMix), chain(mods(countMod{constInt(25)}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{}),
		patch(32, 7, 3, allOf(airOnly, notP(offsetP(0, -1, 0, matchBlocks("podzol"))))))...)
	tall := func(name, block string, m []modifier, n int) {
		t.def(name, simpleBlock(simpleState(block+"[half=lower]")), chain(m, patch(n, 7, 3, airOnly))...)
	}
	mb := heightmapMod{hmMotionBlocking}
	tall("patch_tall_grass", "tall_grass", mods(rarityMod{5}, inSquareMod{}, mb, biomeMod{}), 96)
	tall("patch_tall_grass_2", "tall_grass", mods(noiseThresholdMod{-0.8, 0, 7}, rarityMod{32}, inSquareMod{}, mb, biomeMod{}), 96)
	tall("patch_large_fern", "large_fern", mods(rarityMod{5}, inSquareMod{}, mb, biomeMod{}), 96)
	tall("patch_sunflower", "sunflower", mods(rarityMod{3}, inSquareMod{}, mb, biomeMod{}), 96)
	plantAt := func(name string, pv stateProvider, n int, m ...modifier) {
		t.def(name, simpleBlock(pv), chain(m, patch(n, 7, 3, airOnly))...)
	}
	poppyMix := weightedStates(weightedState{2, stateOf("poppy")}, weightedState{1, stateOf("dandelion")})
	// One flower each, with no patch round it (Java 26.1's flower_default and flower_warm).
	t.def("flower_default", simpleBlock(poppyMix), rarityMod{32}, inSquareMod{}, mb, biomeMod{})
	t.def("flower_warm", simpleBlock(poppyMix), rarityMod{16}, inSquareMod{}, mb, biomeMod{})
	plantAt("patch_dead_bush", simpleState("dead_bush"), 4, countMod{constInt(1)}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{})
	plantAt("patch_dead_bush_2", simpleState("dead_bush"), 4, countMod{constInt(2)}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{})
	plantAt("patch_dead_bush_badlands", simpleState("dead_bush"), 4, countMod{constInt(20)}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{})
	t.def("patch_bush", simpleBlock(simpleState("bush")), chain(mods(rarityMod{4}, inSquareMod{}, mb, biomeMod{}), patch(24, 5, 3, airOnly))...)
	plantAt("patch_berry_common", simpleState("sweet_berry_bush[age=3]"), 96, rarityMod{32}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{})
	plantAt("patch_berry_rare", simpleState("sweet_berry_bush[age=3]"), 96, rarityMod{384}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{})
	onGrass := allOf(airOnly, offsetP(0, -1, 0, matchBlocks("grass_block")))
	gourd := func(name, block string, tries int, chance int) {
		t.def(name, simpleBlock(simpleState(block)), chain(mods(rarityMod{chance}, inSquareMod{}, mb, biomeMod{}), patch(tries, 7, 3, onGrass))...)
	}
	gourd("patch_pumpkin", "pumpkin", 96, 300)
	gourd("patch_melon", "melon", 64, 6)
	gourd("patch_melon_sparse", "melon", 64, 64)
	mushroomBrown, mushroomRed := simpleState("brown_mushroom"), simpleState("red_mushroom")
	plantAt("brown_mushroom_normal", mushroomBrown, 96, rarityMod{256}, inSquareMod{}, mb, biomeMod{})
	plantAt("red_mushroom_normal", mushroomRed, 96, rarityMod{512}, inSquareMod{}, mb, biomeMod{})
	plantAt("brown_mushroom_taiga", mushroomBrown, 96, rarityMod{4}, inSquareMod{}, mb, biomeMod{})
	plantAt("red_mushroom_taiga", mushroomRed, 96, rarityMod{256}, inSquareMod{}, mb, biomeMod{})
	plantAt("brown_mushroom_old_growth", mushroomBrown, 96, countMod{constInt(3)}, rarityMod{4}, inSquareMod{}, mb, biomeMod{})
	plantAt("red_mushroom_old_growth", mushroomRed, 96, rarityMod{171}, inSquareMod{}, mb, biomeMod{})
	plantAt("brown_mushroom_swamp", mushroomBrown, 96, countMod{constInt(2)}, inSquareMod{}, mb, biomeMod{})
	plantAt("red_mushroom_swamp", mushroomRed, 96, rarityMod{64}, inSquareMod{}, mb, biomeMod{})
	plantAt("patch_waterlily", simpleState("lily_pad"), 10, countMod{constInt(4)}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{})
	cane := blockColumn(dUp, airOnly, false, columnLayer{biasedInt(2, 4), simpleState("sugar_cane[age=0]")})
	caneFilter := allOf(airOnly, wouldSurvive("sugar_cane[age=0]"))
	caneAt := func(name string, m ...modifier) { t.def(name, cane, chain(m, patch(20, 4, 0, caneFilter))...) }
	caneAt("patch_sugar_cane", rarityMod{6}, inSquareMod{}, mb, biomeMod{})
	caneAt("patch_sugar_cane_desert", inSquareMod{}, mb, biomeMod{})
	caneAt("patch_sugar_cane_badlands", rarityMod{5}, inSquareMod{}, mb, biomeMod{})
	caneAt("patch_sugar_cane_swamp", rarityMod{3}, inSquareMod{}, mb, biomeMod{})
	cactus := blockColumn(dUp, airOnly, false, columnLayer{biasedInt(1, 3), simpleState("cactus[age=0]")})
	t.def("patch_cactus_desert", cactus, chain(mods(rarityMod{6}, inSquareMod{}, mb, biomeMod{}), patch(10, 7, 3, allOf(airOnly, wouldSurvive("cactus[age=0]"))))...)
	t.def("patch_cactus_decorated", cactus, chain(mods(rarityMod{13}, inSquareMod{}, mb, biomeMod{}), patch(10, 7, 3, allOf(airOnly, wouldSurvive("cactus[age=0]"))))...)
	t.def("vines", vinesFeature, mods(countMod{constInt(127)}, inSquareMod{}, heightRangeMod{uniHeight(64, 100)}, biomeMod{})...)
	t.def("bamboo_light", bambooFeature(0), mods(rarityMod{4}, inSquareMod{}, mb, biomeMod{})...)
	t.def("bamboo", bambooFeature(0.2), mods(noiseCountMod{160, 80, 0.3}, inSquareMod{}, heightmapMod{hmWorldSurfaceWG}, biomeMod{})...)

	// Water plants.
	sea := func(name string, n int, prob float64) {
		t.def(name, seagrassFeature(prob), mods(inSquareMod{}, heightmapMod{hmOceanFloorWG}, countMod{constInt(n)}, biomeMod{})...)
	}
	sea("seagrass_swamp", 64, 0.6)
	sea("seagrass_river", 48, 0.4)
	sea("seagrass_cold", 32, 0.3)
	sea("seagrass_deep_cold", 40, 0.8)
	sea("seagrass_normal", 48, 0.3)
	sea("seagrass_deep", 48, 0.8)
	sea("seagrass_warm", 80, 0.3)
	sea("seagrass_deep_warm", 80, 0.8)
	t.def("kelp_cold", kelpFeature, mods(noiseCountMod{120, 80, 0}, inSquareMod{}, heightmapMod{hmOceanFloorWG}, biomeMod{})...)
	t.def("kelp_warm", kelpFeature, mods(noiseCountMod{80, 80, 0}, inSquareMod{}, heightmapMod{hmOceanFloorWG}, biomeMod{})...)
	t.def("sea_pickle", picklesFeature(20), mods(rarityMod{16}, inSquareMod{}, heightmapMod{hmOceanFloorWG}, biomeMod{})...)

	// Noise-chosen flowers. Inferred from the game's providers and tested on the traces.
	flowerNoise := legacyNormal(2345, 0, []float64{1})
	tulips := []bs{stateOf("orange_tulip"), stateOf("red_tulip"), stateOf("pink_tulip"), stateOf("white_tulip")}
	bright := []bs{stateOf("poppy"), stateOf("azure_bluet"), stateOf("oxeye_daisy"), stateOf("cornflower")}
	dandelion := stateOf("dandelion")
	plainsFlower := func(d *decoration, p pos) bs {
		v := flowerNoise.at(float64(float64(p.x)*0.005), float64(float64(p.y)*0.005), float64(float64(p.z)*0.005))
		if v < -0.8 {
			return tulips[d.r.nextInt(len(tulips))]
		}
		if d.r.nextFloat() < 0.33 {
			return bright[d.r.nextInt(len(bright))]
		}
		return dandelion
	}
	t.def("flower_plains", simpleBlock(plainsFlower), chain(mods(noiseThresholdMod{-0.8, 15, 4}, rarityMod{32}, inSquareMod{}, mb, biomeMod{}), patch(64, 6, 2, airOnly))...)
	tallFlower := func(block string) *placement {
		return newNestedPlacement(simpleBlock(simpleState(block+"[half=lower]")), patch(96, 7, 3, airOnly)...)
	}
	lily := newNestedPlacement(simpleBlock(simpleState("lily_of_the_valley")), patch(96, 7, 3, airOnly)...)
	forestPick := simpleSelector(tallFlower("lilac"), tallFlower("rose_bush"), tallFlower("peony"), lily)
	t.def("forest_flowers", forestPick, mods(rarityMod{7}, inSquareMod{}, mb, countMod{clampedInt(uniInt(-3, 1), 0, 1)}, biomeMod{})...)
	t.def("flower_forest_flowers", forestPick, mods(rarityMod{7}, inSquareMod{}, mb, countMod{clampedInt(uniInt(-1, 3), 0, 3)}, biomeMod{})...)
	lushGrass := newNestedPlacement(simpleBlock(jungleMix), patch(32, 7, 3, airOnly)...)
	t.def("bamboo_vegetation", treeSel(lushGrass, selEntry{0.05, checkedFancy()}, selEntry{0.15, checked(jungleBush(), "jungle_sapling")}, selEntry{0.7, checked(megaJungle(), "jungle_sapling")}), treeBase(countExtra(30, 10, 1), 0)...)
}

// mangroveTree is the mangrove or the tall mangrove. The two differ in trunk height, branch steps,
// the size limit and how far up the trunk starts over its roots (Java 26.1's configured features
// mangrove and tall_mangrove).
func mangroveTree(tall bool) *treeCfg {
	base, a, b, steps, limit, offset := 2, 1, 4, uniInt(1, 4), 2, uniInt(1, 3)
	if tall {
		base, a, b, steps, limit, offset = 4, 1, 9, uniInt(1, 6), 3, uniInt(3, 7)
	}
	propagule := randomizedProp(simpleState("mangrove_propagule[age=0,hanging=true,stage=0,waterlogged=false]"), "age", uniInt(0, 4))
	c := treeOf("mangrove_log", "mangrove_leaves", branchingTrunk{h(base, a, b), steps, uniInt(0, 1), 0.5},
		spreadFoliage{foliageBase{constInt(3), constInt(0)}, constInt(2), 70}, limit, 0, 2, true,
		leaveVine(0.125), attachedToLeaves(0.14, 1, 0, propagule, 2, []dir{dDown}), beehive(0.01))
	c.validExtra = blockSet("mud muddy_mangrove_roots mangrove_roots mangrove_leaves mangrove_log mangrove_propagule moss_carpet vine")
	c.roots = &mangroveRoots{offset: offset, root: stateOf("mangrove_roots[waterlogged=false]"), above: stateOf("moss_carpet"), aboveChance: 0.5,
		through: blockSet("mud muddy_mangrove_roots mangrove_roots moss_carpet vine mangrove_propagule snow"), muddyIn: blockSet("mud muddy_mangrove_roots"),
		muddy: stateOf("muddy_mangrove_roots[axis=y]"), width: 8, length: 15, skew: 0.2}
	return c
}

// newNestedOnly turns a nested placed feature into a plain feature.
func newNestedOnly(pl *placement) feature {
	return func(d *decoration, p pos) bool { return d.place(pl, p) }
}

// Tag members for decoration, kept beside the features that read them.
func init() {
	const logs = "oak_log spruce_log birch_log jungle_log acacia_log dark_oak_log mangrove_log cherry_log pale_oak_log"
	const leaves = "oak_leaves spruce_leaves birch_leaves jungle_leaves acacia_leaves dark_oak_leaves mangrove_leaves cherry_leaves pale_oak_leaves azalea_leaves flowering_azalea_leaves"
	add := func(tag uint32, names string) {
		for _, n := range strings.Fields(names) {
			blockTag[stateBlockIndex(n)] |= tag
		}
	}
	add(tagLogs, logs)
	add(tagHangs, "vine glow_lichen cave_vines cave_vines_plant hanging_roots spore_blossom pale_hanging_moss cocoa resin_clump sculk_vein")
	add(tagLeaves, leaves)
	add(tagNoFeature, "bedrock spawner chest end_portal_frame reinforced_deepslate trial_spawner vault")
	add(tagNoLavaPool, "bedrock spawner chest end_portal_frame reinforced_deepslate trial_spawner vault "+logs+" "+leaves)
	add(tagGeodeBad, "bedrock water lava ice packed_ice blue_ice")
	add(tagTreeSwap, leaves+" azalea flowering_azalea bush cave_vines cave_vines_plant dandelion poppy dead_bush fern firefly_bush glow_lichen hanging_roots large_fern moss_carpet short_dry_grass short_grass tall_dry_grass tall_grass vine water seagrass tall_seagrass kelp kelp_plant lily_pad snow[layers=1] sugar_cane sweet_berry_bush[age=3] wildflowers leaf_litter pink_petals red_mushroom brown_mushroom")
	add(tagNoTrunkBase, "dirt coarse_dirt rooted_dirt podzol moss_block pale_moss_block mud muddy_mangrove_roots")
	add(tagBambooSoil, "dirt coarse_dirt rooted_dirt grass_block podzol mycelium moss_block mud sand red_sand gravel suspicious_sand suspicious_gravel bamboo bamboo_sapling")
	add(tagPodzolSwap, "dirt coarse_dirt rooted_dirt grass_block podzol mycelium moss_block mud")
}
