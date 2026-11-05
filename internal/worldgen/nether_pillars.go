package worldgen

// Two more of the Nether's code features, our own Go of NOTES-bedrock-exact T29 and T30:
// BasaltPillarFeature (0x4ba00c0; soul sand valleys; 100% of the basalt on both golden soul sand
// valleys in the study) and TwistingVinesClusterFeature (0x4bc93f0; warped forests; read from the code;
// seed 100's warped window: 992 of BDS's 1,203 vine blocks, 1,238 placed). Both draw on the rule's feature random.

var (
	sSoulSand      = laterState("soul_sand")
	sTwistingVines = func() (t [26]bs) {
		for a := range t {
			t[a] = laterState("twisting_vines[age=" + itoaSmall(a) + "]")
		}
		return t
	}()
	pillarRoof  = blockSet("soul_sand soul_soil netherrack")
	twistGround = blockSet("netherrack warped_nylium warped_wart_block")
)

// basaltPillar is BasaltPillarFeature (T29): a column of basalt hanging down from soul sand, soul soil
// or netherrack through air, with side blocks by a chance per side fixed at the start, hang-offs where
// the column ends, and a 7 x 7 base whose cells pass when nextInt(10) >= |dx * dz|. "Air" is air only.
func basaltPillar(d *decoration, r *bmt, p pos) bool {
	if !d.at(p).isAir() || !pillarRoof[stateBlock[d.at(p.step(dUp))]] {
		return false
	}
	sides := [4]pos{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}}
	var c [4]float32
	for i := range c {
		c[i] = r.nextFloat()
	}
	f := r.nextFloat()
	open := func(q pos) bool { return d.at(q).isAir() && !d.at(q.step(dUp)).isAir() }
	if f < 100 && open(p) {
		for i := range c {
			c[i] = c[i]*0.07999998 + 0.87
		}
		for {
			d.bput(p, sBasalt)
			for i, s := range sides {
				q := pos{p.x + s.x, p.y, p.z + s.z}
				if r.nextFloat() < c[i] && open(q) {
					d.bput(q, sBasalt)
				}
			}
			p.y--
			if f = r.nextFloat(); !(f < 100 && p.y >= minY && open(p)) {
				break
			}
		}
	}
	for _, s := range sides {
		if q := (pos{p.x + s.x, p.y, p.z + s.z}); !d.at(q).isAir() {
			d.bput(q.step(dUp), sBasalt)
		}
	}
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			k := dx * dz
			if k < 0 {
				k = -k
			}
			if int(r.nextIntN(10)) < k {
				continue
			}
			q := pos{p.x + dx, p.y, p.z + dz}
			for range 3 {
				if !d.at(q.step(dDown)).isAir() {
					d.bput(q, sBasalt)
					break
				}
				q.y--
			}
		}
	}
	return true
}

// twistingVinesCluster is TwistingVinesClusterFeature (T30; 82% / 80% of the vine blocks on seed 100's warped window): 100 tries around the
// origin, skewed to -x and -z, each on netherrack, warped nylium or warped wart under air: a vine of
// 1 to 14 blocks up through air, each block with its own age.
func twistingVinesCluster(d *decoration, r *bmt, p pos) bool {
	for range 100 {
		u1, u2, u3, u4 := r.u32(), r.u32(), r.u32(), r.u32()
		dy := int(r.nextIntN(5))
		q := pos{p.x + int(u1%10) - int(u2%10) - 5, p.y + dy - 4, p.z + int(u3%10) - int(u4%10) - 5}
		if !d.at(q).isAir() || !twistGround[stateBlock[d.at(q.step(dDown))]] {
			continue
		}
		n := int(r.nextIntN(7)) + 1
		if r.nextIntN(6) == 0 {
			n <<= 1
		}
		if r.nextIntN(5) == 0 {
			n = 1
		}
		cur := q
		for i := 0; i <= n; i++ {
			if d.at(cur).isAir() {
				d.bput(cur, sTwistingVines[r.nextIntN(25)])
				if i == n || !d.at(cur.step(dUp)).isAir() {
					break
				}
			}
			cur.y++
		}
	}
	return true
}
