package worldgen

import "fmt"

// Lush caves: moss and clay patches with their plants, cave vines, spore blossoms and rooted azaleas.
// The study does not write these up (NOTES-exact-features.md section 17), so the algorithms and
// parameters were written from knowledge of Java 26.1's vegetation patch, block column and root system
// features and corrected against the study's trace fixtures, which are the evidence
// (TestGoldenFeatureTraces). No Mojang code or file is in this repo.

// patchCfg is a vegetation patch: a round patch of ground on a floor or ceiling, with plants on it.
type patchCfg struct {
	replace       uint32        // Tag of what the ground may replace.
	ground        stateProvider // The ground block.
	plant         *placement    // Placed on a share of the patch.
	ceiling       bool          // On a ceiling, growing down; else on a floor.
	depth         intProvider
	extraBottom   float32 // Chance of one more ground block.
	verticalRange int
	plantChance   float32
	radius        intProvider
	edgeChance    float32 // Chance an edge column (not a corner) is used.
	waterlogged   bool    // The pool kind: the top of the ground turns to water, plants stand in it.
}

// inward is the direction from a patch's surface into its ground.
func (c *patchCfg) inward() dir {
	if c.ceiling {
		return dUp
	}
	return dDown
}

// vegetationPatch is the vegetation_patch feature, or waterlogged_vegetation_patch for a pool.
func vegetationPatch(c *patchCfg) feature {
	return func(d *decoration, o pos) bool {
		r := &d.r
		xr := c.radius(r) + 1
		zr := c.radius(r) + 1
		surface := c.groundPatch(d, o, xr, zr)
		if c.waterlogged {
			surface = c.flood(d, surface)
		}
		up := c.inward().opposite()
		for _, p := range surface.list() {
			if c.plantChance > 0 && r.nextFloat() < c.plantChance {
				if c.waterlogged {
					// The pool's plant stands in the water block itself, and is waterlogged after.
					if d.place(c.plant, p) {
						if s := d.at(p); hasProp(s, "waterlogged") && propOf(s.name(), "waterlogged") == "false" {
							d.set(p.x, p.y, p.z, withProp(s, "waterlogged", "true"))
						}
					}
				} else {
					d.place(c.plant, p.step(up))
				}
			}
		}
		return surface.size > 0
	}
}

func hasProp(s bs, key string) bool { return propOf(s.name(), key) != "" }

// groundPatch lays the ground and gives the set of surface blocks, in Java's hash set order.
func (c *patchCfg) groundPatch(d *decoration, o pos, xr, zr int) *posSet {
	r := &d.r
	in := c.inward()
	out := in.opposite()
	surface := &posSet{}
	surface.reset()
	for dx := -xr; dx <= xr; dx++ {
		xEdge := dx == -xr || dx == xr
		for dz := -zr; dz <= zr; dz++ {
			zEdge := dz == -zr || dz == zr
			edge := xEdge || zEdge
			corner := xEdge && zEdge
			if corner {
				continue
			}
			if edge && (c.edgeChance == 0 || r.nextFloat() > c.edgeChance) {
				continue
			}
			p := o.add(dx, 0, dz)
			for i := 0; d.at(p).isAir() && i < c.verticalRange; i++ {
				p = p.step(in)
			}
			for i := 0; !d.at(p).isAir() && i < c.verticalRange; i++ {
				p = p.step(out)
			}
			below := p.step(in)
			face := fSturdyUp
			if c.ceiling {
				face = fSturdyDown
			}
			if d.at(p).isAir() && d.at(below).has(face) {
				n := c.depth(r)
				if c.extraBottom > 0 && r.nextFloat() < c.extraBottom {
					n++
				}
				if c.placeGround(d, below, n) {
					surface.add(below)
				}
			}
		}
	}
	return surface
}

// placeGround writes up to n ground blocks inwards. A block that is already the ground block is kept
// and the position does not move, as in the game.
func (c *patchCfg) placeGround(d *decoration, p pos, n int) bool {
	in := c.inward()
	for i := range n {
		s := c.ground(d, p)
		cur := d.at(p)
		if s.block() == cur.block() {
			continue
		}
		if !cur.tagged(c.replace) {
			return i != 0
		}
		d.set(p.x, p.y, p.z, s)
		p = p.step(in)
	}
	return true
}

// flood is the pool's step: surface blocks closed on every side but up become water, and only those
// keep plants.
func (c *patchCfg) flood(d *decoration, surface *posSet) *posSet {
	water := &posSet{}
	water.reset()
	for _, p := range surface.list() {
		if !poolExposed(d, p) {
			water.add(p)
		}
	}
	for _, p := range water.list() {
		d.set(p.x, p.y, p.z, sWater)
	}
	return water
}

func poolExposed(d *decoration, p pos) bool {
	for _, a := range [5]dir{dNorth, dEast, dSouth, dWest, dDown} {
		q := p.step(a)
		if !sturdyFace(d.at(q), a.opposite()) {
			return true
		}
	}
	return false
}

// randomizedAge is a provider whose result gets one property drawn after the state.
func randomizedProp(pv stateProvider, key string, values intProvider) stateProvider {
	return func(d *decoration, p pos) bs {
		s := pv(d, p)
		if !hasProp(s, key) {
			return s
		}
		return withProp(s, key, fmt.Sprint(values(&d.r)))
	}
}

// rootSystem is the root_system feature of the rooted azalea: a tree on the surface above a cave
// ceiling, with a column of rooted dirt down to the cave and hanging roots under it.
type rootSystem struct {
	tree                                  *placement
	space, radius                         int
	replace                               uint32
	root                                  bs
	attempts, maxHeight                   int
	hangRadius, hangSpan, hangAttempts    int
	hang                                  bs
	allowedWater                          int
	allowed                               predicate
}

func (c *rootSystem) feature(d *decoration, o pos) bool {
	if !d.at(o).isAir() {
		return false
	}
	if c.dirtAndTree(d, o) {
		c.hangingRoots(d, o)
	}
	return true
}

func (c *rootSystem) treeSpace(d *decoration, p pos) bool {
	for i := 1; i <= c.space; i++ {
		p = p.step(dUp)
		s := d.at(p)
		if s.isAir() {
			continue
		}
		if !(i+1 <= c.allowedWater && s.has(fWater)) {
			return false
		}
	}
	return true
}

func (c *rootSystem) dirtAndTree(d *decoration, o pos) bool {
	p := o
	for i := range c.maxHeight {
		p = p.step(dUp)
		if !c.allowed(d, p) || !c.treeSpace(d, p) {
			continue
		}
		below := d.at(p.step(dDown))
		if below.has(fLava) || !below.has(fSolid) {
			return false
		}
		if d.place(c.tree, p) {
			for y := o.y; y < o.y+i; y++ {
				c.rootedDirt(d, pos{o.x, y, o.z})
			}
			return true
		}
	}
	return false
}

func (c *rootSystem) rootedDirt(d *decoration, p pos) {
	r := &d.r
	for range c.attempts {
		x := r.nextInt(c.radius) - r.nextInt(c.radius)
		z := r.nextInt(c.radius) - r.nextInt(c.radius)
		q := p.add(x, 0, z)
		if d.at(q).tagged(c.replace) {
			d.set(q.x, q.y, q.z, c.root)
		}
	}
}

func (c *rootSystem) hangingRoots(d *decoration, o pos) {
	r := &d.r
	for range c.hangAttempts {
		x := r.nextInt(c.hangRadius) - r.nextInt(c.hangRadius)
		y := r.nextInt(c.hangSpan) - r.nextInt(c.hangSpan)
		z := r.nextInt(c.hangRadius) - r.nextInt(c.hangRadius)
		q := o.add(x, y, z)
		if d.at(q).isAir() && canSurvive(d, c.hang, q) && d.at(q.step(dUp)).has(fSturdyDown) {
			d.set(q.x, q.y, q.z, c.hang)
		}
	}
}

// Vertical anchors and the scans lush cave placements share.
func lushScan(up bool, target predicate) []modifier {
	a, off := dDown, 1
	if up {
		a, off = dUp, -1
	}
	return []modifier{inSquareMod{}, heightRangeMod{uniHeight(minY, 256)}, scanMod{a, target, onlyAir, 12}, offsetMod{constInt(0), constInt(off)}, biomeMod{}}
}

// onlyAir is the game's only-in-air predicate: the air block itself, not cave air.
func onlyAir(d *decoration, p pos) bool { return d.at(p) == sAir }

func defineLush(t *tableBuilder) {
	moss := stateOf("moss_block")
	clay := stateOf("clay")
	mossPlants := weightedStates(weightedState{4, stateOf("flowering_azalea")}, weightedState{7, stateOf("azalea")},
		weightedState{25, stateOf("moss_carpet")}, weightedState{50, stateOf("short_grass")}, weightedState{10, stateOf("tall_grass[half=lower]")})
	mossVegetation := newNestedPlacement(simpleBlock(mossPlants))

	// Cave vines: a column down from a ceiling, body then a head with an age.
	body := weightedStates(weightedState{4, stateOf("cave_vines_plant[berries=false]")}, weightedState{1, stateOf("cave_vines_plant[berries=true]")})
	head := randomizedProp(weightedStates(weightedState{4, stateOf("cave_vines[age=0,berries=false]")}, weightedState{1, stateOf("cave_vines[age=0,berries=true]")}), "age", uniInt(23, 25))
	vineColumn := func(length intProvider) feature {
		return blockColumn(dDown, onlyAir, true, columnLayer{length, body}, columnLayer{constInt(1), head})
	}
	caveVine := vineColumn(weightedInts(weightedInt{2, uniInt(0, 19)}, weightedInt{3, uniInt(0, 2)}, weightedInt{10, uniInt(0, 6)}))
	vineInMoss := newNestedPlacement(vineColumn(weightedInts(weightedInt{5, uniInt(0, 3)}, weightedInt{1, uniInt(1, 7)})))

	// Dripleaves: a small one, or a big one's stem and leaf facing one of four ways.
	small := weightedStates(weightedState{1, stateOf("small_dripleaf[facing=east,half=lower,waterlogged=false]")}, weightedState{1, stateOf("small_dripleaf[facing=west,half=lower,waterlogged=false]")},
		weightedState{1, stateOf("small_dripleaf[facing=north,half=lower,waterlogged=false]")}, weightedState{1, stateOf("small_dripleaf[facing=south,half=lower,waterlogged=false]")})
	inAirOrWater := func(d *decoration, p pos) bool { s := d.at(p); return s == sAir || s.block() == sWater.block() }
	big := func(face string) *placement {
		stem := simpleState("big_dripleaf_stem[facing=" + face + ",waterlogged=false]")
		leaf := simpleState("big_dripleaf[facing=" + face + ",tilt=none,waterlogged=false]")
		return newNestedPlacement(blockColumn(dUp, inAirOrWater, true,
			columnLayer{weightedInts(weightedInt{2, uniInt(0, 4)}, weightedInt{1, constInt(0)}), stem}, columnLayer{constInt(1), leaf}))
	}
	dripleaf := newNestedPlacement(simpleSelector(newNestedPlacement(simpleBlock(small)), big("east"), big("west"), big("south"), big("north")))

	mossPatch := &patchCfg{replace: tagMossSwap, ground: always1(moss), plant: mossVegetation, depth: constInt(1), verticalRange: 5, plantChance: 0.8, radius: uniInt(4, 7), edgeChance: 0.3}
	mossCeiling := &patchCfg{replace: tagMossSwap, ground: always1(moss), plant: vineInMoss, ceiling: true, depth: uniInt(1, 2), verticalRange: 5, plantChance: 0.08, radius: uniInt(4, 7), edgeChance: 0.3}
	clayPatch := &patchCfg{replace: tagLushGround, ground: always1(clay), plant: dripleaf, depth: constInt(3), extraBottom: 0.8, verticalRange: 2, plantChance: 0.05, radius: uniInt(4, 7), edgeChance: 0.7}
	clayPool := &patchCfg{replace: tagLushGround, ground: always1(clay), plant: dripleaf, depth: constInt(3), extraBottom: 0.8, verticalRange: 5, plantChance: 0.1, radius: uniInt(4, 7), edgeChance: 0.7, waterlogged: true}

	solid := func(d *decoration, p pos) bool { return d.at(p).has(fSolid) }
	sturdyDown := func(d *decoration, p pos) bool { return d.at(p).has(fSturdyDown) }
	t.def("lush_caves_ceiling_vegetation", vegetationPatch(mossCeiling), chain(mods(countMod{constInt(125)}), lushScan(true, solid))...)
	t.def("cave_vines", caveVine, chain(mods(countMod{constInt(188)}), lushScan(true, sturdyDown))...)
	t.def("lush_caves_clay", boolSelector(newNestedPlacement(vegetationPatch(clayPatch)), newNestedPlacement(vegetationPatch(clayPool))), chain(mods(countMod{constInt(62)}), lushScan(false, solid))...)
	t.def("lush_caves_vegetation", vegetationPatch(mossPatch), chain(mods(countMod{constInt(125)}), lushScan(false, solid))...)
	t.def("spore_blossom", simpleBlock(simpleState("spore_blossom")), chain(mods(countMod{constInt(25)}), lushScan(true, solid))...)
	t.def("classic_vines_cave_feature", vinesFeature, countMod{constInt(256)}, inSquareMod{}, heightRangeMod{uniHeight(minY, 256)}, biomeMod{})

	azalea := treeOf("oak_log", "azalea_leaves", bendingTrunk{h(4, 2, 0), 3, uniInt(1, 2)}, spreadFoliage{foliageBase{constInt(3), constInt(0)}, constInt(2), 50}, 1, 0, 1, true)
	azalea.foliageState = weightedStates(weightedState{3, stateOf(leafName("azalea_leaves"))}, weightedState{1, stateOf(leafName("flowering_azalea_leaves"))})
	azalea.below, azalea.forceDirt = simpleState("rooted_dirt"), true
	matchAny := matchBlocks("air cave_air void_air water")
	roots := &rootSystem{tree: newNestedPlacement(treeFeature(azalea)), space: 3, radius: 3, replace: tagAzaleaSwap, root: stateOf("rooted_dirt"),
		attempts: 20, maxHeight: 100, hangRadius: 3, hangSpan: 2, hang: stateOf("hanging_roots[waterlogged=false]"), hangAttempts: 20, allowedWater: 2,
		allowed: allOf(anyOf(matchAny, matchTag(tagLeaves), matchTag(tagTreeSwap)), offsetP(0, -1, 0, matchTag(tagAzaleaSoil)))}
	t.def("rooted_azalea_tree", roots.feature, chain(mods(countMod{uniInt(1, 2)}), lushScan(true, solid))...)
}

// always1 is a provider of one state.
func always1(s bs) stateProvider { return func(*decoration, pos) bs { return s } }
