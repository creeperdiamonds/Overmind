package worldgen

// The Nether's code features that its data-driven rules place (Task 10), in our own Go from the study
// of BDS 1.26.60.6 (NOTES-bedrock-nether R2-4b to R2-4e): DeltaFeature, BasaltColumnsFeature,
// HugeFungusFeature (crimson and warped) and WeepingVinesClusterFeature. Each draws on the rule's
// feature MT. Not written: the basalt pillar (read in part; its rule's iterations are BDS-unstable),
// twisting vines (decompiled, not read), the nether fossil (not read).

var (
	sMagma        = laterState("magma_block")
	sBasalt       = laterState("basalt")
	sBlackstone   = laterState("blackstone")
	sCrimsonStem  = laterState("crimson_stem[axis=y]")
	sWarpedStem   = laterState("warped_stem[axis=y]")
	sNetherWart   = laterState("nether_wart_block")
	sWarpedWart   = laterState("warped_wart_block")
	sShroomlight  = laterState("shroomlight")
	sCrimsonFung  = laterState("crimson_fungus")
	sWarpedFung   = laterState("warped_fungus")
	sCrimsonNyl   = laterState("crimson_nylium")
	sWarpedNyl    = laterState("warped_nylium")
	netherOpen    = blockSet("air cave_air lava water fire soul_fire crimson_roots warped_roots crimson_fungus warped_fungus nether_sprouts weeping_vines twisting_vines brown_mushroom red_mushroom")
	sWeepingVines = func() (t [26]bs) {
		for a := range t {
			t[a] = laterState("weeping_vines[age=" + itoaSmall(a) + "]")
		}
		return t
	}()
	netherMagma = newBedrockOre(28, 0, []oreRule{{"magma_block", "netherrack blackstone"}})
	netherGrav  = newBedrockOre(33, 0, []oreRule{{"gravel", "netherrack"}})
	netherBlack = newBedrockOre(33, 0, []oreRule{{"blackstone", "netherrack"}})
)

func itoaSmall(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// netherSolid is the study's solid test for these features: not open (air, liquids, fire, plants).
func netherSolid(s bs) bool { return !netherOpen[stateBlock[s]] }

// deltaFeature is DeltaFeature (0x515e1e0): from the rule's position, up through solid or down through
// open blocks to an open block over solid ground (not bedrock, not lava); a diamond of a by b
// (3..6), where every clear block may get a magma rim and its neighbour at (rx, rz) lava. A block is
// clear when it is not lava, has air above, and none of the other five sides is air.
func deltaFeature(d *decoration, r *bmt, p pos) bool {
	x, y, z := p.x, p.y, p.z
	up := netherSolid(d.block(x, y, z))
	found := false
	for y > 1 {
		if y >= netherHeight {
			return false
		}
		if d.block(x, y, z).isAir() {
			if b := d.block(x, y-1, z); b != sBedrock && netherSolid(b) && !b.has(fLava) {
				found = true
				break
			}
		}
		if up {
			y++
		} else {
			y--
		}
	}
	if !found {
		return false
	}
	f := r.nextFloat()
	rx, rz := 0, 0
	if f < 0.9 {
		rx, rz = int(r.nextIntN(3)), int(r.nextIntN(3))
	}
	rim := f < 0.9 && rx != 0 && rz != 0
	a, b := int(r.nextIntN(4))+3, int(r.nextIntN(4))+3
	m := max(a, b)
	clear := func(qx, qy, qz int) bool {
		if d.block(qx, qy, qz).has(fLava) || !d.block(qx, qy+1, qz).isAir() {
			return false
		}
		for _, o := range [5][3]int{{0, -1, 0}, {0, 0, -1}, {0, 0, 1}, {1, 0, 0}, {-1, 0, 0}} {
			if d.block(qx+o[0], qy+o[1], qz+o[2]).isAir() || d.chunkAt((qx+o[0])>>4, (qz+o[2])>>4) == nil {
				return false
			}
		}
		return true
	}
	placed := false
	for dx := -a; dx <= a; dx++ {
		for dz := -b; dz <= b; dz++ {
			if absInt(dx)+absInt(dz) > m {
				continue
			}
			qx, qz := x+dx, z+dz
			if !clear(qx, y, qz) {
				continue
			}
			if rim {
				d.set(qx, y, qz, sMagma)
				placed = true
			}
			if clear(qx+rx, y, qz+rz) {
				d.set(qx+rx, y, qz+rz, sLava)
				placed = true
			}
		}
	}
	return placed
}

// basaltColumns is BasaltColumnsFeature (0x4b9f950): down from the position to an open block on solid
// ground; a small (height 1..4) or big (5..10) cluster; 50 tries within 5 (or 15 within 8), each
// raising one column of basalt on basalt or blackstone ground near its point, shorter with distance.
func basaltColumns(d *decoration, r *bmt, p pos) bool {
	x, y, z := p.x, p.y, p.z
	surf := func(cx, cy, cz int) bool {
		return !netherSolid(d.block(cx, cy, cz)) && netherSolid(d.block(cx, cy-1, cz))
	}
	for {
		if y >= netherHeight {
			return false
		}
		if surf(x, y, z) {
			break
		}
		y--
		if y <= 0 {
			return false
		}
	}
	big := 0
	if r.nextFloat() >= 0.3 {
		big = 1
	}
	r0 := big + 1
	height := int(r.nextIntN(int32(big*2+4))) + big*4 + 1
	cnt, rng, off := 15, 17, -8
	if r.nextFloat() < 0.9 {
		cnt, rng, off = 50, 11, -5
	}
	for range cnt {
		px := int(r.nextIntN(int32(rng))) + x + off
		pz := int(r.nextIntN(int32(rng))) + z + off
		dist := absInt(px-x) + absInt(pz-z)
		k := height - dist
		if k <= 0 {
			continue
		}
		reach := int(r.nextIntN(int32(r0))) + r0
	columns:
		for dx := -reach; dx <= reach; dx++ {
			for dz := -reach; dz <= reach; dz++ {
				cx, cz := px+dx, pz+dz
				cy := y
				bud := absInt(dx) + absInt(dz)
				upward := netherSolid(d.block(cx, cy, cz))
				found := -1
				for cy < netherHeight && bud >= 1 {
					if surf(cx, cy, cz) {
						if b := d.block(cx, cy-1, cz); b == sBasalt || b == sBlackstone {
							found = cy
						}
						break
					}
					if upward {
						cy++
					} else {
						cy--
					}
					bud--
					if cy <= 0 {
						break
					}
				}
				if found < 0 {
					continue
				}
				if d.block(cx, found, cz) != sBedrock {
					kk := k - (absInt(dx)+absInt(dz))/2
					for cy = found; ; {
						d.set(cx, cy, cz, sBasalt)
						cy++
						kk--
						if d.block(cx, cy, cz) == sBedrock || cy >= netherHeight || kk < 0 {
							break
						}
					}
				}
				break columns
			}
		}
	}
	return true
}

// hugeFungus is HugeFungusFeature (0xd9bf720): Java 1.16's with Bedrock's changes, for the crimson
// (with weeping vines) or warped kind.
type hugeFungus struct {
	stem, hat bs
	vines     bool
}

// fungusReplaceable is what a huge fungus may grow into: open blocks a plant may take.
func fungusReplaceable(s bs) bool {
	return s.isAir() || s.has(fReplaceable) || s == sFire || s == sSoulFire || s == sCrimsonFung || s == sWarpedFung
}

func (f hugeFungus) place(d *decoration, r *bmt, p pos) bool {
	x, y, z := p.x, p.y, p.z
	h := int(r.nextIntN(10)) + 4
	if r.nextIntN(12) == 0 {
		h *= 2
	}
	if y <= 0 || y+h+1 >= netherHeight {
		return false
	}
	found := false
	for range 100 {
		if d.block(x, y, z).has(fLava|fWater) || d.block(x, y-1, z).has(fLava|fWater) {
			return false
		}
		if b := d.block(x, y-1, z); b == sSoulSoil || b == sWarpedNyl || b == sCrimsonNyl {
			found = true
			break
		}
		y--
		if y <= 0 || y+h >= netherHeight {
			return false
		}
	}
	if !found {
		return false
	}
	t := 0
	if float64(r.nextFloat()) < 0.06 {
		t = 1
	}
	for dx := -t; dx <= t; dx++ {
		for dz := -t; dz <= t; dz++ {
			corner := t == 1 && absInt(dx) == 1 && absInt(dz) == 1
			for k := range h {
				if s := d.block(x+dx, y+k, z+dz); !fungusReplaceable(s) {
					continue
				}
				if corner && !(r.nextFloat() < 0.1) {
					continue
				}
				d.set(x+dx, y+k, z+dz, f.stem)
			}
		}
	}
	rr := int(r.nextIntN(int32(h/3 + 1)))
	i := rr + 5
	if h <= i {
		i = h
	}
	j := h - i
	for k := j; k <= h; k++ {
		l := 1
		if k < h-int(r.nextIntN(3)) {
			l = 2
		}
		if k < j+4 && i >= 9 {
			l = 3
		}
		l += t
		for dx := -l; dx <= l; dx++ {
			for dz := -l; dz <= l; dz++ {
				px, py, pz := x+dx, y+k, z+dz
				if s := d.block(px, py, pz); netherSolid(s) || !fungusReplaceable(s) {
					continue
				}
				xe, ze := dx == -l || dx == l, dz == -l || dz == l
				edge := xe || ze || k == h
				corner := xe && ze
				if k < j+3 {
					if !edge {
						continue
					}
					if d.block(px, py-1, pz) == f.hat {
						d.set(px, py, pz, f.hat)
					} else if r.nextFloat() < 0.15 {
						d.set(px, py, pz, f.hat)
						if f.vines && r.nextIntN(11) == 0 {
							weepingUnder(d, r, px, py, pz, 23, 25)
						}
					}
					continue
				}
				v := r.nextFloat()
				switch {
				case !edge:
					if v < 0.1 {
						d.set(px, py, pz, sShroomlight)
					} else if r.nextFloat() < 0.2 {
						d.set(px, py, pz, f.hat)
						if f.vines && r.nextIntN(10) == 0 {
							weepingUnder(d, r, px, py, pz, 23, 25)
						}
					}
				case corner:
					if v < 0.7 {
						d.set(px, py, pz, f.hat)
						if f.vines && r.nextIntN(12) == 0 {
							weepingUnder(d, r, px, py, pz, 23, 25)
						}
					} else if r.nextFloat() < 0.02 {
						d.set(px, py, pz, sShroomlight)
					}
				default:
					if v < 0.98 {
						d.set(px, py, pz, f.hat)
						if f.vines && r.nextIntN(14) == 0 {
							weepingUnder(d, r, px, py, pz, 23, 25)
						}
					} else if r.nextFloat() < 0.02 {
						d.set(px, py, pz, sShroomlight)
					}
				}
			}
		}
	}
	return true
}

// weepingUnder hangs weeping vines under a hat block when the block below it is air: n = nextInt(5),
// doubled one time in 6, set to 1 one time in 5.
func weepingUnder(d *decoration, r *bmt, x, y, z, lo, hi int) {
	if !d.block(x, y-1, z).isAir() {
		return
	}
	n := int(r.nextIntN(5))
	if r.nextIntN(6) == 0 {
		n *= 2
	}
	if r.nextIntN(5) == 0 {
		n = 1
	}
	weepingColumn(d, r, x, y-1, z, n, lo, hi)
}

// weepingColumn is the weeping vines column (0xd4297a0): n + 1 steps down; each air block takes a vine
// with an age of lo + nextInt(hi - lo), and the column ends at the last step or above a block that is
// not air.
func weepingColumn(d *decoration, r *bmt, x, y, z, n, lo, hi int) {
	for i := 0; i <= n; i++ {
		if d.block(x, y, z).isAir() {
			age := lo + int(r.nextIntN(int32(hi-lo)))
			d.set(x, y, z, sWeepingVines[age])
			if i == n || !d.block(x, y-1, z).isAir() {
				return
			}
		}
		y--
	}
}

// weepingVinesCluster is WeepingVinesClusterFeature (0x4bf0020): a wart block under netherrack or wart,
// 200 tries to grow the clump (air touching exactly one netherrack or wart), then 100 tries to hang
// vines (ages 17..25) under netherrack or wart.
func weepingVinesCluster(d *decoration, r *bmt, p pos) bool {
	roof := func(s bs) bool { return s == sNetherrack || s == sNetherWart }
	x, y, z := p.x, p.y, p.z
	if !d.block(x, y, z).isAir() || !roof(d.block(x, y+1, z)) {
		return false
	}
	d.set(x, y, z, sNetherWart)
	for range 200 {
		qx := x + int(r.nextIntN(12)) - 6
		qy := y + int(r.nextIntN(7)) - 5
		qz := z + int(r.nextIntN(12)) - 6
		if !d.block(qx, qy, qz).isAir() {
			continue
		}
		n := 0
		for _, v := range dirVec {
			if roof(d.block(qx+v.x, qy+v.y, qz+v.z)) {
				n++
			}
		}
		if n == 1 {
			d.set(qx, qy, qz, sNetherWart)
		}
	}
	for range 100 {
		qx := x - 8 + int(r.nextIntN(16))
		qy := y - 8 + int(r.nextIntN(9))
		qz := z - 8 + int(r.nextIntN(16))
		if !d.block(qx, qy, qz).isAir() || !roof(d.block(qx, qy+1, qz)) {
			continue
		}
		n := int(r.nextIntN(7)) + 1
		if r.nextIntN(6) == 0 {
			n *= 2
		}
		if r.nextIntN(5) == 0 {
			n = 1
		}
		weepingColumn(d, r, qx, qy, qz, n, 17, 25)
	}
	return true
}
