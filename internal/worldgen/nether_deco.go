package worldgen

// The Nether's own decoration (Task 10), NetherGenerator::decorationPostProcessChunk (0x52d6c70), in
// our own Go from the study of BDS 1.26.60.6 (NOTES-bedrock-nether N5): Java 1.12's
// ChunkGeneratorHell.populate order, every draw from one MT seeded with the chunk's decoration seed.
// What runs here, in the code's order: 8 lava springs, fire, glowstone (two kinds), brown and red
// mushrooms, nether gold and quartz ores, 16 closed lava springs. Then the data-driven passes run, on a
// reseeded MT, as everywhere.
//
// Not placed, and why: the magma step (9 blobs at Y 23..36 read from the code, but its blob does not
// match BDS: 29% of BDS's magma, the study's open item 1), and the gravel and blackstone blobs and the
// ancient debris after it, which the study could not match either (N6). They draw from the same MT
// after everything placed here and before the reseed, so leaving them out changes nothing else.
//
// Ore sizes are Java's (gold 10, quartz 14), not read from the code; measured by the study on the BDS
// worlds at 99.3% / 99.6% (gold) and 99.2% / 99.1% (quartz).

var (
	sFire       = laterState("fire")
	sSoulFire   = laterState("soul_fire")
	sGlowstone  = laterState("glowstone")
	sBrownMush  = laterState("brown_mushroom")
	sRedMush    = laterState("red_mushroom")
	sSoulSoil   = laterState("soul_soil")
	netherGold  = newBedrockOre(10, 0, []oreRule{{"nether_gold_ore", "netherrack"}})
	netherQuarz = newBedrockOre(14, 0, []oreRule{{"nether_quartz_ore", "netherrack"}})
	// glowstoneRoof is what a glowstone cluster may hang from.
	glowstoneRoof = blockSet("netherrack basalt blackstone")
	// netherMushroomGround is what the study's model lets a mushroom stand on in the Nether.
	netherMushroomGround = blockSet("netherrack soul_sand soul_soil crimson_nylium warped_nylium basalt blackstone gravel magma_block nether_gold_ore nether_quartz_ore glowstone bedrock")
)

// netherDecorate runs the hard-coded part of a Nether source chunk on r.
func (d *decoration) netherDecorate(r *bmt, cx, cz int) {
	x8, z8, x0, z0 := cx*16|8, cz*16|8, cx*16, cz*16
	at := func(ox, h, add, oz int) (int, int, int) {
		x := int(r.nextIntN(16)) + ox
		y := int(r.nextIntN(int32(h))) + add
		z := int(r.nextIntN(16)) + oz
		return x, y, z
	}
	for range 8 {
		x, y, z := at(x8, 120, 4, z8)
		d.netherSpring(x, y, z, false)
	}
	n := int(r.nextIntN(r.nextIntN(10) + 1))
	for range n + 1 {
		x, y, z := at(x8, 120, 4, z8)
		d.netherFire(r, x, y, z)
	}
	n = int(r.nextIntN(r.nextIntN(10) + 1))
	for range n {
		x, y, z := at(x8, 120, 4, z8)
		d.glowstoneCluster(r, x, y, z)
	}
	for range 10 {
		x, y, z := at(x8, 128, 0, z8)
		d.glowstoneCluster(r, x, y, z)
	}
	for _, m := range [2]bs{sBrownMush, sRedMush} {
		if r.nextIntN(1) == 0 {
			x, y, z := at(x8, 128, 0, z8)
			d.netherMushrooms(r, x, y, z, m)
		}
	}
	for range 10 {
		x, y, z := at(x0, 108, 10, z0)
		netherGold.place(d, r, pos{x, y, z})
	}
	for range 16 {
		x, y, z := at(x0, 108, 10, z0)
		netherQuarz.place(d, r, pos{x, y, z})
	}
	for range 16 {
		x, y, z := at(x0, 108, 10, z0)
		d.netherSpring(x, y, z, true)
	}
}

// netherSpring is the Nether spring (0x54094c0): under netherrack, in air or netherrack, lava when the
// four sides and the block below hold five netherrack, or (an open spring) four and one air.
func (d *decoration) netherSpring(x, y, z int, inside bool) {
	if d.block(x, y+1, z) != sNetherrack {
		return
	}
	if b := d.block(x, y, z); !b.isAir() && b != sNetherrack {
		return
	}
	rock, air := 0, 0
	for _, o := range [5][3]int{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, -1, 0}} {
		switch b := d.block(x+o[0], y+o[1], z+o[2]); {
		case b == sNetherrack:
			rock++
		case b.isAir():
			air++
		}
	}
	if !inside && rock == 4 && air == 1 || rock == 5 {
		d.set(x, y, z, sLava)
	}
}

// spread6 is the legacy scatter: six raw words, z, y and x offsets of (a & 7) - (b & 7), y by & 3.
func spread6(r *bmt) (dx, dy, dz int) {
	u0, u1, u2, u3, u4, u5 := r.u32(), r.u32(), r.u32(), r.u32(), r.u32(), r.u32()
	return int(u4&7) - int(u5&7), int(u2&3) - int(u3&3), int(u0&7) - int(u1&7)
}

// netherFire is NetherFireFeature (0x54075d0): 64 tries around the position, fire in air over
// netherrack, soul fire over the one soul block the code tests. Which soul block that is was not read;
// soul soil is measured: on seed 100's 1024 chunks BDS has 253 soul fires and we place all 253 and one
// more (with soul sand as well the study measured 45% precision).
func (d *decoration) netherFire(r *bmt, x, y, z int) {
	for range 64 {
		dx, dy, dz := spread6(r)
		px, py, pz := x+dx, y+dy, z+dz
		if !d.block(px, py, pz).isAir() {
			continue
		}
		switch d.block(px, py-1, pz) {
		case sNetherrack:
			d.set(px, py, pz, sFire)
		case sSoulSoil:
			d.set(px, py, pz, sSoulFire)
		}
	}
}

// glowstoneCluster is GlowStoneFeature (0x516bfa0): in air under netherrack, basalt or blackstone, a
// glowstone, then 1500 tries below it, each kept where air touches exactly one glowstone.
func (d *decoration) glowstoneCluster(r *bmt, x, y, z int) {
	if !d.block(x, y, z).isAir() || !glowstoneRoof[stateBlock[d.block(x, y+1, z)]] {
		return
	}
	d.set(x, y, z, sGlowstone)
	for range 1500 {
		u0, u1 := r.u32(), r.u32()
		dy := int(r.nextIntN(12))
		u3, u4 := r.u32(), r.u32()
		px, py, pz := x+int(u3&7)-int(u4&7), y-dy, z+int(u0&7)-int(u1&7)
		if !d.block(px, py, pz).isAir() {
			continue
		}
		n := 0
		for _, v := range dirVec {
			if d.block(px+v.x, py+v.y, pz+v.z) == sGlowstone {
				n++
			}
		}
		if n == 1 {
			d.set(px, py, pz, sGlowstone)
		}
	}
}

// netherMushrooms is MushroomFeature (0x54042a0): 64 tries, the first at the position, each next one
// scattered from it.
func (d *decoration) netherMushrooms(r *bmt, x, y, z int, m bs) {
	px, py, pz := x, y, z
	for range 64 {
		if d.block(px, py, pz).isAir() && netherMushroomGround[stateBlock[d.block(px, py-1, pz)]] {
			d.set(px, py, pz, m)
		}
		dx, dy, dz := spread6(r)
		px, py, pz = x+dx, y+dy, z+dz
	}
}
