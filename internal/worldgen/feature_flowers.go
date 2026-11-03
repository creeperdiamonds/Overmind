package worldgen

import "fmt"

// Flower patches and other surface plants the study does not write up: flower forest, cherry and
// meadow flowers, wildflowers, leaf litter, dry grass, firefly bushes and the mushroom island's huge
// mushrooms. Written from knowledge of Java 26.1's vegetation features and placements and checked on
// the study's traces where they hold runs (TestGoldenFeatureTraces); the rest have no Java run in the
// traced chunks and are unverified. No Mojang code or file is in this repo.

// noiseState is the game's noise provider: a list of states picked by a noise value.
func noiseState(n *normalNoise, scale float32, list []bs) stateProvider {
	return func(_ *decoration, p pos) bs {
		s := float64(scale)
		v := n.at(float64(p.x)*s, float64(p.y)*s, float64(p.z)*s)
		return pickByNoise(list, v)
	}
}

func pickByNoise(list []bs, v float64) bs {
	i := int(min(max((1.0+v)/2.0*float64(len(list)), 0), float64(len(list)-1)))
	return list[i]
}

// dualNoiseState is the game's dual noise provider: a slow noise picks how many and which states
// are on offer at a place, and the main noise picks one of them.
func dualNoiseState(lo, hi int, slow *normalNoise, slowScale float32, main *normalNoise, scale float32, list []bs) stateProvider {
	slowAt := func(x, y, z int) float64 {
		return slow.at(float64(float32(float32(x)*slowScale)), float64(float32(float32(y)*slowScale)), float64(float32(float32(z)*slowScale)))
	}
	return func(_ *decoration, p pos) bs {
		variety := int(clampedMap(slowAt(p.x, p.y, p.z), -1, 1, float64(lo), float64(hi+1)))
		offer := make([]bs, 0, variety)
		for i := range variety {
			offer = append(offer, pickByNoise(list, slowAt(p.x+i*54545, p.y, p.z+i*34234)))
		}
		s := float64(scale)
		return pickByNoise(offer, main.at(float64(p.x)*s, float64(p.y)*s, float64(p.z)*s))
	}
}

// segmented lists a segmented block (petals, wildflowers, leaf litter) by amount, then facing, each
// with weight one, as the game builds them.
func segmented(block, key string, lo, hi int) stateProvider {
	var list []weightedState
	_ = sAir // Made after the base states, so air stays state zero.
	for n := lo; n <= hi; n++ {
		for _, a := range horizontal {
			list = append(list, weightedState{1, stateOf(fmt.Sprintf("%s[facing=%s,%s=%d]", block, dirName(a), key, n))})
		}
	}
	return weightedStates(list...)
}

func defineFlowers(t *tableBuilder) {
	mb := heightmapMod{hmMotionBlocking}
	ws := heightmapMod{hmWorldSurfaceWG}
	names := func(list string) []bs {
		var out []bs
		for _, n := range fieldsOf(list) {
			out = append(out, stateOf(n))
		}
		return out
	}

	// Flower forest: eleven flowers by a noise.
	forestNoise := legacyNormal(2345, 0, []float64{1})
	forestList := names("dandelion poppy allium azure_bluet red_tulip orange_tulip white_tulip pink_tulip oxeye_daisy cornflower lily_of_the_valley")
	t.def("flower_flower_forest", simpleBlock(noiseState(forestNoise, 0.020833334, forestList)),
		chain(mods(countMod{constInt(3)}, rarityMod{2}, inSquareMod{}, mb, biomeMod{}), patch(96, 6, 2, airOnly))...)

	// Wildflowers in birch forests and meadows, petals in cherry groves.
	wild := simpleBlock(segmented("wildflowers", "flower_amount", 1, 4))
	t.def("wildflowers_birch_forest", wild, chain(mods(countMod{constInt(3)}, rarityMod{2}, inSquareMod{}, mb, biomeMod{}), patch(64, 6, 2, airOnly))...)
	t.def("wildflowers_meadow", wild, chain(mods(noiseThresholdMod{-0.8, 5, 10}, inSquareMod{}, mb, biomeMod{}), patch(8, 6, 2, airOnly))...)
	t.def("flower_cherry", simpleBlock(segmented("pink_petals", "flower_amount", 1, 4)),
		chain(mods(noiseThresholdMod{-0.8, 5, 10}, inSquareMod{}, mb, biomeMod{}), patch(96, 6, 2, airOnly))...)

	// Meadow flowers: the dual noise provider.
	slow := legacyNormal(2345, -10, []float64{1})
	main := legacyNormal(2345, -3, []float64{1})
	meadowList := names("tall_grass[half=lower] allium poppy azure_bluet dandelion cornflower oxeye_daisy short_grass")
	t.def("flower_meadow", simpleBlock(dualNoiseState(1, 3, slow, 1, main, 1, meadowList)),
		chain(mods(inSquareMod{}, mb, biomeMod{}), patch(96, 6, 2, airOnly))...)

	// Leaf litter in dark forests, on grass.
	onGrass := allOf(onlyAir, offsetP(0, -1, 0, matchBlocks("grass_block")))
	t.def("patch_leaf_litter", simpleBlock(segmented("leaf_litter", "segment_amount", 1, 3)),
		chain(mods(countMod{constInt(2)}, inSquareMod{}, ws, biomeMod{}), patch(32, 7, 3, onGrass))...)

	// Dry grass in deserts and badlands.
	dry := simpleBlock(weightedStates(weightedState{1, stateOf("short_dry_grass")}, weightedState{1, stateOf("tall_dry_grass")}))
	t.def("patch_dry_grass_desert", dry, chain(mods(rarityMod{3}, inSquareMod{}, ws, biomeMod{}), patch(64, 7, 3, airOnly))...)
	t.def("patch_dry_grass_badlands", dry, chain(mods(rarityMod{6}, inSquareMod{}, ws, biomeMod{}), patch(64, 7, 3, airOnly))...)

	// Firefly bushes: in swamps, and beside water anywhere.
	firefly := simpleBlock(simpleState("firefly_bush"))
	survives := wouldSurvive("firefly_bush")
	waterBelowSide := func(d *decoration, p pos) bool {
		for _, o := range [4]pos{{1, -1, 0}, {-1, -1, 0}, {0, -1, 1}, {0, -1, -1}} {
			if d.at(p.add(o.x, o.y, o.z)).has(fWater) {
				return true
			}
		}
		return false
	}
	nearWater := filterMod{allOf(onlyAir, survives, waterBelowSide)}
	fireflyPatch := patch(20, 4, 3, airOnly)
	t.def("patch_firefly_bush_swamp", firefly, chain(mods(countMod{constInt(2)}, inSquareMod{}, mb, biomeMod{}), fireflyPatch)...)
	t.def("patch_firefly_bush_near_water_swamp", firefly, chain(mods(countMod{constInt(3)}, inSquareMod{}, mb, biomeMod{}, nearWater), fireflyPatch)...)
	t.def("patch_firefly_bush_near_water", firefly, chain(mods(countMod{constInt(2)}, inSquareMod{}, heightmapMod{hmMotionNoLeaves}, biomeMod{}, nearWater), fireflyPatch)...)

	// Mushroom fields: one huge mushroom, red or brown.
	t.def("mushroom_island_vegetation", boolSelector(newNestedPlacement(hugeMushroom(true, 2)), newNestedPlacement(hugeMushroom(false, 3))), inSquareMod{}, mb, biomeMod{})
}
