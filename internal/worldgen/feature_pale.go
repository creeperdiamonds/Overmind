package worldgen

// The pale garden: pale oaks with pale moss and creaking hearts, pale moss patches and eyeblossoms.
// The study does not write these up and no traced chunk holds a pale garden, so these follow Java
// 26.1's pale garden features as written from knowledge of the game, unverified against a Java run.
// No Mojang code or file is in this repo.

var (
	sPaleMoss   = laterState("pale_moss_block")
	sHangingTip = laterState("pale_hanging_moss[tip=true]")
	sHanging    = laterState("pale_hanging_moss[tip=false]")
	sHeart      = laterState("creaking_heart[axis=y,creaking_heart_state=dormant,natural=true]")
)

var mossSides = [3]string{"none", "low", "tall"}

// paleCarpet is a pale moss carpet state: bottom or topper, with a side kind by horizontal direction
// (north, east, south, west).
func paleCarpet(bottom bool, sides [4]int) bs {
	_ = sAir // Made after the base states, so air stays state zero.
	return stateOf("pale_moss_carpet[bottom=" + boolWord(btoi(bottom)) + ",east=" + mossSides[sides[1]] + ",north=" + mossSides[sides[0]] +
		",south=" + mossSides[sides[2]] + ",west=" + mossSides[sides[3]] + "]")
}

func carpetSides(s bs) (bottom bool, sides [4]int) {
	if s.block() != paleCarpet(true, [4]int{}).block() {
		return false, sides
	}
	bottom = propOf(s.name(), "bottom") == "true"
	for i, a := range horizontal {
		switch propOf(s.name(), dirName(a)) {
		case "low":
			sides[i] = 1
		case "tall":
			sides[i] = 2
		}
	}
	return bottom, sides
}

// carpetUpdated is the carpet's shape from its neighbours.
func carpetUpdated(d *decoration, bottom bool, sides [4]int, p pos, create bool) [4]int {
	create = create || bottom
	for i, a := range horizontal {
		side := 0
		if sturdyFace(d.at(p.step(a)), a.opposite()) {
			side = sides[i]
			if create {
				side = 1
			}
		}
		if side == 1 {
			ab, as := carpetSides(d.at(p.step(dUp)))
			if d.at(p.step(dUp)).block() == sPaleCarpetBlock && as[i] != 0 && !ab {
				side = 2
			}
			if !bottom {
				if d.at(p.step(dDown)).block() == sPaleCarpetBlock {
					if _, bs := carpetSides(d.at(p.step(dDown))); bs[i] == 0 {
						side = 0
					}
				}
			}
		}
		sides[i] = side
	}
	return sides
}

var sPaleCarpetBlock = paleCarpet(true, [4]int{}).block()

// placeCarpet is the mossy carpet's own placement: a bottom carpet, maybe with a topper over it.
func placeCarpet(d *decoration, p pos) {
	sides := carpetUpdated(d, true, [4]int{}, p, true)
	d.set(p.x, p.y, p.z, paleCarpet(true, sides))
	up := p.step(dUp)
	top := carpetUpdated(d, false, [4]int{}, up, true)
	any := false
	for i := range top {
		if top[i] != 0 && !d.r.nextBool() {
			top[i] = 0
		}
		any = any || top[i] != 0
	}
	if any {
		d.set(up.x, up.y, up.z, paleCarpet(false, top))
		sides = carpetUpdated(d, true, sides, p, true)
		d.set(p.x, p.y, p.z, paleCarpet(true, sides))
	}
}

// paleMoss is the pale oak's moss decorator: a moss patch at its foot and hanging moss under logs and
// leaves.
func paleMoss(patch *placement, leafChance, trunkChance, groundChance float32) treeDecorator {
	return func(t *treeRun, logs, leaves, _ []pos) {
		if len(logs) == 0 {
			return
		}
		r := &t.d.r
		l := shuffledPos(r, logs)
		low := l[0]
		for _, p := range l {
			if p.y < low.y {
				low = p
			}
		}
		if r.nextFloat() < groundChance {
			t.d.place(patch, low.step(dUp))
		}
		hang := func(list []pos, chance float32) {
			for _, p := range list {
				if r.nextFloat() < chance {
					if below := p.step(dDown); t.d.at(below).isAir() {
						hanger(t, below)
					}
				}
			}
		}
		hang(logs, trunkChance)
		hang(leaves, leafChance)
	}
}

func hanger(t *treeRun, p pos) {
	for t.d.at(p.step(dDown)).isAir() && !(float64(t.d.r.nextFloat()) < 0.5) {
		t.deco(p, sHanging)
		p = p.step(dDown)
	}
	t.deco(p, sHangingTip)
}

func shuffledPos(r *featRand, list []pos) []pos {
	out := append([]pos{}, list...)
	for i := len(out); i > 1; i-- {
		j := r.nextInt(i)
		out[i-1], out[j] = out[j], out[i-1]
	}
	return out
}

// creakingHeart puts a dormant heart in a log closed in by logs on every side.
func creakingHeart(chance float32) treeDecorator {
	return func(t *treeRun, logs, _, _ []pos) {
		if len(logs) == 0 {
			return
		}
		r := &t.d.r
		if r.nextFloat() >= chance {
			return
		}
		for _, p := range shuffledPos(r, logs) {
			closed := true
			for _, a := range allDirs {
				if !t.d.at(p.step(a)).tagged(tagLogs) {
					closed = false
					break
				}
			}
			if closed {
				t.deco(p, sHeart)
				return
			}
		}
	}
}

func definePale(t *tableBuilder) {
	carpet := laterState("pale_moss_carpet[bottom=true,east=none,north=none,south=none,west=none]")
	paleVegetation := weightedStates(weightedState{25, carpet}, weightedState{25, stateOf("short_grass")}, weightedState{10, stateOf("tall_grass[half=lower]")})
	plant := newNestedPlacement(func(d *decoration, p pos) bool {
		s := paleVegetation(d, p)
		if s == carpet {
			if !canSurvive(d, s, p) {
				return false
			}
			placeCarpet(d, p)
			return true
		}
		return simpleBlock(always1(s))(d, p)
	})
	mossPatch := &patchCfg{replace: tagMossSwap, ground: always1(sPaleMoss), plant: plant, depth: constInt(1), verticalRange: 5, plantChance: 0.3, radius: uniInt(2, 4), edgeChance: 0.75}
	patchPl := newNestedPlacement(vegetationPatch(mossPatch))
	t.def("pale_moss_patch", vegetationPatch(mossPatch), countMod{constInt(1)}, inSquareMod{}, heightmapMod{hmMotionNoLeaves}, biomeMod{})

	paleOak := func(decos ...treeDecorator) *treeCfg {
		c := treeOf("pale_oak_log", "pale_oak_leaves", darkOakTrunk{h(6, 2, 1)}, darkOakFoliage{foliageBase{constInt(0), constInt(0)}}, 1, 0, 2, true, decos...)
		c.threeLayers, c.layersLimit, c.upperLimit, c.lower, c.middle, c.upper = true, 1, 1, 0, 1, 2
		return c
	}
	moss := paleMoss(patchPl, 0.15, 0.4, 0.8)
	plain := checked(paleOak(moss), "pale_oak_sapling")
	creaking := checked(paleOak(moss, creakingHeart(1)), "pale_oak_sapling")
	t.def("pale_garden_vegetation", randomSelector(plain, selEntry{0.1, creaking}, selEntry{0.9, plain}),
		mods(countMod{constInt(16)}, inSquareMod{}, waterDepthMod{0}, heightmapMod{hmOceanFloor}, biomeMod{})...)

	eyeblossom := simpleBlock(simpleState("closed_eyeblossom"))
	t.def("pale_garden_flowers", eyeblossom, chain(mods(rarityMod{8}, inSquareMod{}, heightmapMod{hmMotionNoLeaves}, biomeMod{}), patch(96, 7, 3, airOnly))...)
	t.def("flower_pale_garden", eyeblossom, chain(mods(rarityMod{32}, inSquareMod{}, heightmapMod{hmMotionBlocking}, biomeMod{}), patch(1, 0, 0, airOnly))...)
}
