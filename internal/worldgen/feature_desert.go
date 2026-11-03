package worldgen

// The desert well, written from knowledge of Java 26.1's desert well feature; no traced chunk holds a
// desert, so it is unverified against a Java run. Its suspicious sand holds no loot here, as chests
// hold none. Fossils are not written: Java builds them from structure templates in the game's files,
// which may not enter this repo.

var (
	sSandSlab   = laterState("sandstone_slab[type=bottom,waterlogged=false]")
	sSuspicious = laterState("suspicious_sand[dusted=0]")
)

func desertWell(d *decoration, o pos) bool {
	o = o.step(dUp)
	for d.at(o).isAir() && o.y > minY+2 {
		o = o.step(dDown)
	}
	if d.at(o) != sSand {
		return false
	}
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			if d.at(o.add(x, -1, z)).isAir() && d.at(o.add(x, -2, z)).isAir() {
				return false
			}
		}
	}
	put := func(p pos, s bs) { d.set(p.x, p.y, p.z, s) }
	for y := -2; y <= 0; y++ {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				put(o.add(x, y, z), sSandst)
			}
		}
	}
	put(o, sWater)
	for _, a := range horizontal {
		put(o.step(a), sWater)
	}
	below := o.step(dDown)
	put(below, sSand)
	for _, a := range horizontal {
		put(below.step(a), sSand)
	}
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			if x == -2 || x == 2 || z == -2 || z == 2 {
				put(o.add(x, 1, z), sSandst)
			}
		}
	}
	for _, p := range [4]pos{{2, 1, 0}, {-2, 1, 0}, {0, 1, 2}, {0, 1, -2}} {
		put(o.add(p.x, p.y, p.z), sSandSlab)
	}
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			if x == 0 && z == 0 {
				put(o.add(x, 4, z), sSandst)
			} else {
				put(o.add(x, 4, z), sSandSlab)
			}
		}
	}
	for y := 1; y <= 3; y++ {
		for _, p := range [4]pos{{-1, y, -1}, {-1, y, 1}, {1, y, -1}, {1, y, 1}} {
			put(o.add(p.x, p.y, p.z), sSandst)
		}
	}
	middle := [5]pos{o, o.step(dEast), o.step(dSouth), o.step(dWest), o.step(dNorth)}
	put(middle[d.r.nextInt(5)].step(dDown), sSuspicious)
	put(middle[d.r.nextInt(5)].add(0, -2, 0), sSuspicious)
	return true
}

func defineDesert(t *tableBuilder) {
	t.def("desert_well", desertWell, rarityMod{1000}, inSquareMod{}, heightmapMod{hmMotionBlocking}, biomeMod{})
}
