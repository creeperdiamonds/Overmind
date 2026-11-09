package worldgen

// The warm oceans' coral reefs (Task 9 round 23): minecraft:coral_feature (CoralFeature::place
// 0x5154900, its shape builder 0x5155340 and helpers 0x515aa10, 0x515b1b0) and minecraft:coral_hang_feature
// (CoralHang_place 0x515d3a0), in our own Go from NOTES-bedrock-exact T32, T35, T40, T44, T48 and T52, which
// read them from the code; the study's simulation of the whole feature on a BDS world got the first
// feature of a chunk exact in about 39 of 49 chunks (T40) and 71.3 / 43.1% of the live coral blocks
// (precision / recall, T52) with the base it had. Java's warm_ocean_vegetation no longer stands in.
//
// Shared tests, as T35 names them, and how they are taken here:
//   - "water" (material byte 5): the block itself is water (bWaterMat), as the coral crust's walk; seagrass
//     and kelp keep their water in Bedrock's second layer and are not;
//   - solid (region vt+0x1b0): bSolid, as the crust and the anemone;
//   - CHK(q): q is one of the five live coral blocks and not this feature's colour -> abort (where each
//     site says);
//   - mayPlace (region vt+0x58, BlockSource 0xce68ce0, not read to the end): ASSUMED "the target is water
//     or waterlogged" (crustWater), the study's MAYPL=water, which scored better than "always" (T40), and
//     the coral crust's assumption;
//   - set: a write our decoration may make (bput); a write it refuses is skipped (ASSUMED, as the crust).
// List A (T52): floor fans only, nextInt(5) colour (tube, brain, bubble, fire, horn) and nextInt(2)
// coral_fan_direction, where the cell is water, the cell below is not water and is solid (T48's survive
// test: top-face support below, taken as bSolid; the flowing-water half never fails on generated water).
// List B: the wall fans; the side table and colours of T40 / T52 (u = nextInt(2) + 2 nextInt(3), at most
// 4: tube, brain, bubble, fire, horn; facing coral_direction [2,3,0,1][side-2]).

// bedrockCoral is CoralFeature::place on the rule's feature random (T40 "Coral_place").
func bedrockCoral(d *decoration, r *bmt, o pos) bool {
	cs := crustOnce()
	x, y, z := o.x, o.y, o.z
	const top = 63 - 4 // The dimension's sea level - 4.
	for y < top && !bWaterMat(d.block(x, y, z)) {
		y++
	}
	if y >= top || !bSolid(d.block(x, y-1, z)) || !bWaterMat(d.block(x, y, z)) || cs.live.has(bname(d.block(x, y-1, z))) {
		return false
	}
	c := int(r.nextIntN(5))
	sh := &coralShape{d: d, r: r, cs: cs, col: c, block: cs.coral[c]}
	sh.run(pos{x, y, z})
	for _, p := range sh.a {
		if below := d.at(p.add(0, -1, 0)); bWaterMat(d.at(p)) && !bWaterMat(below) && bSolid(below) {
			i, dir := r.nextIntN(5), r.nextIntN(2)
			d.bput(p, cs.fans[dir][i])
		}
	}
	for _, e := range sh.b {
		isC := cs.coral10.has(bname(d.at(e.p))) || bname(d.at(e.p)) == bnStone // Tested first, no draw.
		sides, ok := crustWallSide[e.face]
		if !ok {
			sides = [3]int{5, 3, 2}
		}
		side := sides[r.nextIntN(3)] // Always drawn.
		st := crustSideStep[side]
		q := e.p.add(st[0], 0, st[1])
		if bWaterMat(d.at(q)) && isC && crustWater(d.at(q)) {
			u := min(int(r.nextIntN(2))+2*int(r.nextIntN(3)), 4)
			d.bput(q, cs.wall[crustSideFacing[side]][u])
		}
	}
	return true
}

type coralFace struct {
	p    pos
	face int
}

// coralShape is one run of the shape builder 0x5155340: its colour, list A (floor fan candidates) and
// list B (wall fan candidates with a face).
type coralShape struct {
	d     *decoration
	r     *bmt
	cs    *crustStates
	col   int
	block bs
	a     []pos
	b     []coralFace
}

// coralDir is the direction table 0x18c8138: 0 down, 1 up, 2 -z, 3 +z, 4 -x, 5 +x.
var coralDir = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

var coralOpp = [6]int{1, 0, 3, 2, 5, 4}

func (p pos) dir(d, k int) pos {
	v := coralDir[d]
	return pos{p.x + v[0]*k, p.y + v[1]*k, p.z + v[2]*k}
}

func upBy(p pos, k int) pos { return pos{p.x, p.y + k, p.z} }

// chk is CHK: true when q is not a live coral block of another colour.
func (s *coralShape) chk(q pos) bool {
	n := bname(s.d.at(q))
	return !s.cs.live.has(n) || n == bname(s.block)
}

func (s *coralShape) put(q pos)             { s.d.bput(q, s.block) }
func (s *coralShape) mayPlace(q pos) bool   { return crustWater(s.d.at(q)) } // ASSUMED (see the file's comment).
func (s *coralShape) water(q pos) bool      { return bWaterMat(s.d.at(q)) }
func (s *coralShape) solidBelow(q pos) bool { return bSolid(s.d.at(upBy(q, -1))) }

// run: kind = nextInt(3); kind 0 runs parts A, B and C, kind 1 parts B and C, kind 2 part C (T35: the
// three fall through).
func (s *coralShape) run(o pos) {
	kind := s.r.nextIntN(3)
	if kind == 0 {
		s.partA(o)
	}
	if kind <= 1 {
		s.partB(o)
	}
	s.partC(o)
}

// branch is the helper 0x515aa10: the position, then up to four steps, each on nextFloat() < f; a failed
// test ends it (the remaining draws are skipped).
func (s *coralShape) branch(f float32, p pos, step int) {
	if !s.chk(p) {
		return
	}
	s.put(p)
	for _, d := range [4][2]int{{step, 0}, {-step, 0}, {0, step}, {0, -step}} {
		if s.r.nextFloat() < f {
			q := p.add(d[0], 0, d[1])
			if !s.chk(q) || !s.solidBelow(q) {
				return
			}
			s.put(q)
		}
	}
}

// push is the fan helper's list-B push: k = nextInt(4), then nextFloat() < 0.4 adds (c, face k + 2).
func (s *coralShape) push(c pos) {
	k := int(s.r.nextIntN(4))
	if s.r.nextFloat() < 0.4 {
		s.b = append(s.b, coralFace{c, k + 2})
	}
}

// fan is the helper 0x515b1b0 at p on side d (T40); a failed mayPlace or CHK returns.
func (s *coralShape) fan(p pos, d int, last bool) {
	var kind int32
	if last {
		kind = s.r.nextIntN(2) + 1
	} else {
		kind = s.r.nextIntN(3)
	}
	switch kind {
	case 0:
		if !s.mayPlace(upBy(p, 1)) || !s.chk(p) {
			return
		}
		s.put(p)
		p1 := p.dir(d, 1)
		if !s.mayPlace(upBy(p1, 2)) || !s.chk(upBy(p1, 1)) {
			return
		}
		s.put(upBy(p1, 1))
		c := upBy(p1.dir(d, 1), 2)
		if !s.mayPlace(upBy(c, 1)) || !s.chk(c) {
			return
		}
		s.put(c)
		s.push(c)
		c = upBy(c, 1)
		if !s.mayPlace(upBy(c, 1)) || !s.chk(c) {
			return
		}
		s.put(c)
		s.push(c)
		e := upBy(c.dir(coralOpp[d], 1), 1)
		if !s.mayPlace(upBy(e, 1)) || !s.chk(e) {
			return
		}
		s.put(e)
		if s.r.nextFloat() < 0.2 {
			s.a = append(s.a, upBy(e, 1))
		}
	case 1:
		if !s.mayPlace(upBy(p, 1)) || !s.chk(p) {
			return
		}
		s.put(p)
		s.push(p)
		if !s.mayPlace(upBy(p, 2)) || !s.chk(upBy(p, 1)) {
			return
		}
		s.put(upBy(p, 1))
		s.push(p) // p, not p + up (T40: the asm copies [rsp]).
		c := upBy(p.dir(d, 1), 1)
		for i := range 3 {
			if !s.mayPlace(upBy(c, i+1)) || !s.chk(upBy(c, i)) {
				return
			}
			s.put(upBy(c, i))
			s.push(c) // Always the base c (T40).
		}
		if s.r.nextFloat() < 0.2 {
			s.a = append(s.a, upBy(c, 3))
		}
	default:
		if !s.chk(p) {
			return
		}
		s.put(p)
		c := upBy(p.dir(d, 1), 1)
		if !s.mayPlace(upBy(c, 1)) || !s.chk(c) {
			return
		}
		s.put(c)
		s.push(c)
		if !s.mayPlace(upBy(c, 2)) || !s.chk(upBy(c, 1)) {
			return
		}
		s.put(upBy(c, 1))
		s.push(c)
		d2 := [4]int{4, 5, 3, 2}[d-2]
		if s.r.nextBool() {
			d2 = [4]int{5, 4, 2, 3}[d-2]
		}
		c2 := upBy(c.dir(d2, 1), 1)
		if !s.mayPlace(upBy(c2, 1)) || !s.chk(c2) {
			return
		}
		s.put(c2)
		s.push(c2)
		if !s.mayPlace(upBy(c2, 2)) || !s.chk(upBy(c2, 1)) {
			return
		}
		s.put(upBy(c2, 1))
		s.push(c2)
		if s.r.nextFloat() < 0.2 {
			s.a = append(s.a, upBy(c2, 2))
		}
	}
}

// coralF1, coralRest are part A's face tables (T40, bytes 0x1543ff0..): F1 = coralF1[d0][g], F2 =
// coralRest[d0][F1][r].
var (
	coralF1   = map[int][3]int{2: {5, 3, 4}, 3: {5, 2, 4}, 4: {5, 3, 2}, 5: {2, 3, 4}}
	coralRest = map[int]map[int][2]int{
		2: {5: {3, 4}, 3: {5, 4}, 4: {3, 5}},
		3: {5: {2, 4}, 2: {5, 4}, 4: {2, 5}},
		4: {5: {3, 2}, 3: {2, 5}, 2: {3, 5}},
		5: {2: {3, 4}, 3: {2, 4}, 4: {3, 2}},
	}
)

// partA is kind 0's first part (T40, disassembly 0x5155690..0x5156660); a failed test goes on to part B.
func (s *coralShape) partA(o pos) {
	r := s.r
	x, y, z := o.x, o.y, o.z
	h := int(r.nextIntN(2))
	w := 2*h + 3
	for i := 0; i <= h; i++ {
		if !s.mayPlace(pos{x, y + i + 1, z}) || !s.chk(o) {
			return
		}
		s.put(o) // The origin each time (T40).
		if w > 1 {
			for j := 1; j <= w/2; j++ {
				f := float32(float32(-i)*0.15) + float32(float32(float32(j)*0.1)+0.5) // mulss / addss in this order.
				s.branch(f, pos{x, y + i, z}, i)
				if h == 1 && i == 0 {
					// Up to four singles; the repeated (-j, +j) is in the binary (T40). A failed test ends
					// this j.
					for _, dd := range [4][2]int{{j, j}, {-j, j}, {-j, j}, {j, -j}} {
						if r.nextFloat() < 0.25 {
							q := pos{x + dd[0], y, z + dd[1]}
							if !s.chk(q) || !s.solidBelow(q) {
								break
							}
							s.put(q)
						}
					}
				}
			}
		}
		w -= 2
	}
	tp := pos{x, y + h + 1, z}
	if !s.mayPlace(pos{x, y + h + 2, z}) {
		return
	}
	d0 := int(r.nextIntN(4)) + 2
	g := r.nextIntN(3)
	f1 := coralF1[d0][g]
	f2 := coralRest[d0][f1][r.nextIntN(2)] // All three draws, always.
	faces := [3]int{d0, f1, f2}
	if !s.chk(tp) {
		return
	}
	s.put(tp)
	s.fan(tp.dir(d0, 1), d0, false)
	n := int(r.nextIntN(2))
	for i := 1; i <= n+1; i++ {
		q := pos{x, y + h + 1 + i, z}
		if !s.chk(q) {
			return
		}
		s.put(q) // No mayPlace (T40).
		s.fan(q.dir(faces[i], 1), faces[i], i == n+1)
	}
}

// partB is the second part (T35, 0x515678f); a failed test goes on to part C.
func (s *coralShape) partB(o pos) {
	r := s.r
	x, y, z := o.x, o.y, o.z
	sz := int(r.nextIntN(2))
	n := 2*sz + 3
	cx, cz := x-(sz+1), z-(sz+1)
	for i := range n {
		for j := range n {
			if (i == 0 || i == n-1) && (j == 0 || j == n-1) {
				continue // The corners.
			}
			c := pos{cx + i, y, cz + j}
			if r.nextIntN(5) != 0 && s.solidBelow(c) {
				if !s.chk(c) {
					return
				}
				s.put(c)
			}
		}
	}
	if s.chk(o) {
		s.put(o)
	}
	c1 := pos{x, y + 1, z}
	if !s.mayPlace(pos{x, y + 2, z}) || !s.chk(c1) {
		return
	}
	s.put(c1)
	for _, e := range [4]struct {
		k  int32
		dx int
		dz int
	}{{2, 1, 0}, {3, -1, 0}, {4, 0, 1}, {3, 0, -1}} {
		if r.nextIntN(e.k) != 0 {
			q := pos{x + e.dx, y + 1, z + e.dz}
			if !s.chk(q) {
				return
			}
			s.put(q)
		}
	}
	s.branch(0.33, pos{x, y + 2, z}, 1)
	s.branch(0.7, pos{x, y + 3, z}, 1)
	m := int(r.nextIntN(2))
	for i := 0; i <= m; i++ {
		if !s.mayPlace(pos{x, y + 5 + i, z}) || !s.chk(pos{x, y + 4 + i, z}) {
			return
		}
		s.put(pos{x, y + 4 + i, z})
		f4 := int(r.nextIntN(4))
		if r.nextFloat() < 0.4 {
			s.b = append(s.b, coralFace{pos{x, y + 4, z}, f4 + 2}) // y + 4 for every i (T35).
		}
	}
	if r.nextFloat() < 0.4 {
		s.a = append(s.a, pos{x, y + 5 + m, z})
	}
}

// partC is the last part (T35, T40, 0x51579f9): a block (6 in 7) or a claw.
func (s *coralShape) partC(o pos) {
	r := s.r
	x, y, z := o.x, o.y, o.z
	if r.nextIntN(7) != 0 {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				c := pos{x + dx, y, z + dz}
				if r.nextIntN(3) != 0 && !s.water(pos{c.x, y - 1, c.z}) {
					if !s.chk(c) {
						return
					}
					s.put(c)
				}
			}
		}
		c1 := pos{x, y + 1, z}
		if !s.mayPlace(pos{x, y + 2, z}) || !s.chk(c1) {
			return
		}
		s.put(c1)
		for _, q := range [4]pos{{x + 1, y + 1, z}, {x - 1, y + 1, z}, {x, y + 1, z + 1}, {x, y + 1, z - 1}} {
			if r.nextIntN(4) != 0 && !s.water(pos{q.x, y, q.z}) {
				if !s.chk(q) {
					return
				}
				s.put(q)
				if r.nextFloat() < 0.2 {
					s.a = append(s.a, upBy(q, 1))
				}
			}
		}
		if s.mayPlace(pos{x, y + 3, z}) {
			if !s.chk(pos{x, y + 2, z}) {
				return
			}
			s.put(pos{x, y + 2, z})
			if r.nextFloat() < 0.2 {
				s.a = append(s.a, pos{x, y + 3, z})
			}
		}
		if r.nextFloat() < 0.25 {
			if !s.mayPlace(pos{x, y + 4, z}) || !s.chk(pos{x, y + 3, z}) {
				return
			}
			s.put(pos{x, y + 3, z})
			if r.nextFloat() < 0.1 {
				s.a = append(s.a, pos{x, y + 4, z})
			}
		}
		return
	}
	// The claw.
	k := int(r.nextIntN(4))
	d1 := [4]int{5, 4, 2, 3}[k]
	d2 := coralOpp[d1]
	v := k + 2
	b := r.nextIntN(2)
	h := int(r.nextIntN(3))
	if !s.mayPlace(pos{x, y + h + 3, z}) {
		return
	}
	for i := 0; i <= h; i++ {
		if !s.chk(pos{x, y + i, z}) {
			return
		}
		s.put(pos{x, y + i, z})
	}
	tp := pos{x, y + h + 1, z}
	var p1, p2 pos
	var seq []pos
	if b == 0 {
		p1, p2 = tp.dir(d1, 1), tp.dir(d2, 1)
		seq = []pos{p1, p2}
	} else if !r.nextBool() {
		p1, p2 = tp.dir(d1, 1), tp.dir(d2, 2)
		seq = []pos{tp.dir(d2, 1), p1, p2}
	} else {
		p1, p2 = tp.dir(d1, 2), tp.dir(d2, 1)
		seq = []pos{tp.dir(d1, 1), p1, p2}
	}
	for _, q := range seq {
		if !s.chk(q) {
			return
		}
		s.put(q)
	}
	starts := [3]pos{tp.dir(v, 1), upBy(p1.dir(v, 1), 1), upBy(p2.dir(v, 1), 1)}
	for a := range 3 {
		ln := int(r.nextIntN(2)) + 2
		if a != 0 {
			ln += 2
		}
		cur := starts[a]
		for range ln {
			if !s.chk(cur) {
				return
			}
			s.put(cur)
			if r.nextFloat() < 0.1 {
				s.a = append(s.a, upBy(cur, 1))
			}
			if r.nextFloat() < 0.6 {
				s.b = append(s.b, coralFace{cur, d1})
			}
			if r.nextFloat() < 0.6 {
				s.b = append(s.b, coralFace{cur, d2})
			}
			if r.nextFloat() < 0.25 && s.mayPlace(upBy(cur, 2)) {
				cur = upBy(cur, 1)
			}
			cur = cur.dir(v, 1)
		}
	}
}

// bedrockCoralHang is CoralHang_place 0x515d3a0 (T48) on the rule's feature random: from the position down to a
// solid block (not below the sea level - 13), which must be a coral block (any of the ten) or stone, then
// a shuffled side with water, and a tube or brain wall fan there. mayPlace is tested at the ORIGIN's side
// (T48), ASSUMED "water or waterlogged" as in bedrockCoral.
func bedrockCoralHang(d *decoration, r *bmt, o pos) bool {
	cs := crustOnce()
	p := o
	for !bSolid(d.at(p)) {
		if p.y <= 63-13 {
			break
		}
		p.y--
	}
	if n := bname(d.at(p)); !bSolid(d.at(p)) || !(cs.coral10.has(n) || n == bnStone) {
		return false
	}
	dirs := [4]int{5, 3, 4, 2}
	side := -1
	for k := 4; k >= 1; k-- {
		i := int(r.nextIntN(int32(k)))
		if bWaterMat(d.at(p.dir(dirs[i], 1))) {
			side = dirs[i]
			break
		}
		dirs[i], dirs[k-1] = dirs[k-1], dirs[i]
	}
	if side < 0 {
		side = 2 // No further draw.
	}
	q := p.dir(side, 1)
	if !bWaterMat(d.at(q)) || !crustWater(d.at(o.dir(side, 1))) {
		return false
	}
	u := 0 // tube
	if r.nextIntN(2) > 0 {
		u = 1 // brain
	}
	d.bput(q, cs.wall[crustSideFacing[side]][u])
	return true
}
