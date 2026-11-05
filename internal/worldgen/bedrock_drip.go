package worldgen

import "math"

// Bedrock's dripstone features (Task 4e round 6), code-registered, in our own Go from the study of BDS
// 1.26.60.6 (NOTES-bedrock-exact T14; the study's sim p7drip.py): pointed_dripstone_feature
// (SpeleothemFeature 0x4bc61d0), dripstone_cluster_feature (SpeleothemClusterFeature 0x4bc4520) and
// large_dripstone_feature (LargeSpeleothemFeature 0x53fab40). The large dripstone's replace list
// (vector 0xff122d8) is not read; the study found an empty list fits (89.7% precision), and it is
// empty here (MEASURED, ruling 39).

var (
	bnDripRepl     = bnameSetOf("dirt coarse_dirt stone tuff deepslate") // The code's list 0xfc083c0.
	bnDripPoolWall = bnameSetOf("stone deepslate tuff")
	bnDripBlock    = internBName("dripstone_block")
	bnPointed      = internBName("pointed_dripstone")
	sDripBlock     = laterState("dripstone_block")
)

// speleoCfg is a speleothem's configuration: dripstone's, and the sulfur caves' features that reuse the
// same code with other blocks and numbers (NOTES-bedrock-exact T42, read from their registration).
type speleoCfg struct {
	repl                            bnameSet // Blocks the base block replaces.
	baseName                        uint16
	base                            bs
	spike                           string // The pointed block's name.
	spikeName                       uint16
	hLo, hN                         int     // Cluster height lo + nextInt(hN).
	wetMean, wetDev, wetMin, wetMax float32 // The cluster's clamped normal wetness.
	rLo, rN                         int     // Cluster radius lo + nextInt(rN) in x and z.
	densLo, densHi                  float32
	search                          int
}

var (
	dripCfg = &speleoCfg{repl: bnDripRepl, baseName: bnDripBlock, base: sDripBlock, spike: "pointed_dripstone", spikeName: bnPointed,
		hLo: 3, hN: 3, wetMean: 0.1, wetDev: 0.3, wetMin: 0.1, wetMax: 1, rLo: 2, rN: 6, densLo: 0.3, densHi: 0.7, search: 12}
	// sulfurCfg is sulfur_spike_feature's and sulfur_spike_cluster_feature's (T42): base sulfur, spike
	// sulfur_spike, list sulfur and cinnabar, height 1..4, wetness 0 (every draw still made), radius
	// 2..7, density 0.3..0.7, search 12.
	sulfurCfg = &speleoCfg{repl: bnameSetOf("sulfur cinnabar"), baseName: internBName("sulfur"), base: laterState("sulfur"),
		spike: "sulfur_spike", spikeName: internBName("sulfur_spike"), hLo: 1, hN: 4, rLo: 2, rN: 6, densLo: 0.3, densHi: 0.7, search: 12}
)

// Facing order: down, up, north, south, west, east.
var facingVec = [6]pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

const (
	dirDown = 0
	dirUp   = 1
)

func dripOpen(s bs) bool { return s.isAir() || bWaterMat(s) }
func dripLava(s bs) bool { return s.has(fLava) }
func (c *speleoCfg) isBase(s bs) bool { n := bname(s); return c.repl.has(n) || n == c.baseName }

// dripAt reads a block, nil-safe outside the world (reported as not there).
func dripAt(d *decoration, p pos) (bs, bool) {
	if p.y < minY || p.y > maxY {
		return 0, false
	}
	return d.at(p), true
}

// replaceDrip makes a listed block the base block (0x4cc4160).
func (c *speleoCfg) replaceDrip(d *decoration, p pos) bool {
	if b, ok := dripAt(d, p); ok && c.repl.has(bname(b)) {
		return d.bput(p, c.base)
	}
	return false
}

// pointedState is a pointed dripstone of a thickness, pointing up or down.
func (c *speleoCfg) pointedState(thickness string, up bool) bs {
	dir := "down"
	if up {
		dir = "up"
	}
	return stateOf(c.spike + "[thickness=" + thickness + ",vertical_direction=" + dir + ",waterlogged=false]")
}

// growPointed is 0x4cc4280: h pointed dripstones from p along dir; each cell must be open and not lava.
func (c *speleoCfg) growPointed(d *decoration, p pos, dir, h int, merge bool) {
	if h <= 0 {
		return
	}
	tip := "tip"
	if merge {
		tip = "tip_merge"
	}
	seq := make([]string, 0, h)
	switch {
	case h == 2:
		seq = append(seq, "frustum")
	case h == 1:
		seq = append(seq, tip)
	default:
		seq = append(seq, "base")
	}
	for n := h; n >= 2; n-- {
		switch n {
		case 3:
			seq = append(seq, "frustum")
		case 2:
			seq = append(seq, tip)
		default:
			seq = append(seq, "middle")
		}
	}
	v := facingVec[dir]
	for _, th := range seq {
		b, ok := dripAt(d, p)
		if !ok || !dripOpen(b) || dripLava(b) {
			return
		}
		d.bput(p, c.pointedState(th, dir == dirUp))
		p = p.add(v.x, v.y, v.z)
	}
}

// bedrockPointedDripstone is SpeleothemFeature::place (T14).
func bedrockPointedDripstone(d *decoration, r *bmt, p pos) bool { return dripCfg.pointed(d, r, p) }

func (c *speleoCfg) pointed(d *decoration, r *bmt, p pos) bool {
	bl, _ := dripAt(d, p.add(0, -1, 0))
	ab, _ := dripAt(d, p.add(0, 1, 0))
	below, above := c.isBase(bl), c.isBase(ab)
	var dir int
	switch {
	case below && above:
		dir = dirDown
		if r.nextBool() {
			dir = dirUp
		}
	case below:
		dir = dirUp
	case above:
		dir = dirDown
	default:
		return false
	}
	root := p.add(0, 1, 0)
	if dir == dirUp {
		root = p.add(0, -1, 0)
	}
	c.replaceDrip(d, root)
	for k := range 6 {
		if r.nextFloat() >= 0.3 {
			a := root.add(facingVec[k].x, facingVec[k].y, facingVec[k].z)
			c.replaceDrip(d, a)
			if !r.nextBool() {
				e := facingVec[r.nextIntN(6)]
				b := a.add(e.x, e.y, e.z)
				c.replaceDrip(d, b)
				if !r.nextBool() {
					e := facingVec[r.nextIntN(6)]
					c.replaceDrip(d, b.add(e.x, e.y, e.z))
				}
			}
		}
	}
	f := r.nextFloat()
	h := 1
	if t, ok := dripAt(d, p.add(facingVec[dir].x, facingVec[dir].y, facingVec[dir].z)); f < 0.2 && ok && dripOpen(t) {
		h = 2
	}
	c.growPointed(d, p, dir, h, false)
	return true
}

// nextGaussian is Random::nextGaussian in float (T14): the polar method on the MT, the second value
// cached in the Random.
func (r *bmt) nextGaussian() float32 {
	if r.haveGauss {
		r.haveGauss = false
		return r.gauss
	}
	var v1, v2, s float32
	for {
		a := float32(float64(r.u32()) * 2.3283064365386963e-10)
		v1 = float32(a+a) + -1
		b := float32(float64(r.u32()) * 2.3283064365386963e-10)
		v2 = float32(b+b) + -1
		s = float32(v1*v1) + float32(v2*v2)
		if s < 1 && s != 0 {
			break
		}
	}
	m := float32(math.Sqrt(float64(float32(float32(float32(math.Log(float64(s)))*-2) / s))))
	r.gauss, r.haveGauss = float32(v2*m), true
	return float32(v1 * m)
}

// dripScan is ColumnScan (0xd8fe840) from p's neighbour along dy: at most rng-1 steps through inside
// cells, then the edge test; the edge cell's Y, or false.
func dripScan(d *decoration, p pos, dy, rng int, inside, edge func(bs) bool) (int, bool) {
	q := p.y + dy
	for k := 0; k < rng-1; k++ {
		b, ok := dripAt(d, pos{p.x, q, p.z})
		if !ok || !inside(b) {
			break
		}
		q += dy
	}
	b, ok := dripAt(d, pos{p.x, q, p.z})
	if ok && edge(b) {
		return q, true
	}
	return 0, false
}

// dripHeight is H(ml): a column's stalactite or stalagmite length.
func dripHeight(r *bmt, ml, dist int) int {
	g := r.nextGaussian()
	fd := float32(float32(dist) * 0.125)
	m0 := float32(float32(ml) * 0.5)
	var mean float32
	if !(1 < fd) {
		mean = float32(float32(-m0)*fd) + m0
	}
	v := float32(g*3) + mean
	c := max(v, 0)
	if float32(ml) < v {
		c = float32(ml)
	}
	return int(c)
}

// bedrockDripstoneCluster is SpeleothemClusterFeature::place with Bedrock's config (T14).
func bedrockDripstoneCluster(d *decoration, r *bmt, p pos) bool { return dripCfg.cluster(d, r, p) }

func (cf *speleoCfg) cluster(d *decoration, r *bmt, p pos) bool {
	if s, ok := dripAt(d, p); !ok || !dripOpen(s) {
		return false
	}
	hgt := cf.hLo + int(r.nextIntN(int32(cf.hN)))
	v := float32(r.nextGaussian()*cf.wetDev) + cf.wetMean
	wet := v
	switch {
	case cf.wetMax < v:
		wet = cf.wetMax
	case v <= cf.wetMin:
		wet = cf.wetMin
	}
	lo, hi := cf.densLo, cf.densHi
	dens := float32(r.nextFloat()*(hi-lo)) + lo
	rx, rz := cf.rLo+int(r.nextIntN(int32(cf.rN))), cf.rLo+int(r.nextIntN(int32(cf.rN)))
	// The scan's edge (T42, read from the code's lambda): a listed block, the base block or lava. Any
	// other block (granite, gravel, an ore ...) is no ceiling or floor.
	edge := func(s bs) bool { return cf.isBase(s) || dripLava(s) }
	for dx := -rx; dx <= rx; dx++ {
		for dz := -rz; dz <= rz; dz++ {
			k := min(rx-absInt(dx), rz-absInt(dz))
			f := float32(float32(k) / 3)
			var chance float64
			switch {
			case f < 0:
				chance = float64(float32(0.1))
			case f > 1:
				chance = 1
			default:
				chance = float64(float32(f*0.9) + 0.1)
			}
			c := pos{p.x + dx, p.y, p.z + dz}
			if s, ok := dripAt(d, c); !ok || !dripOpen(s) {
				continue
			}
			ceil, hasCeil := dripScan(d, c, 1, cf.search, dripOpen, edge)
			floor, hasFloor := dripScan(d, c, -1, cf.search, dripOpen, edge)
			if !hasCeil && !hasFloor {
				continue
			}
			floor2, hasFloor2 := floor, hasFloor
			if r.nextFloat() < wet && hasFloor {
				fb, _ := dripAt(d, pos{c.x, floor, c.z})
				up, _ := dripAt(d, pos{c.x, floor + 1, c.z})
				if !bWaterMat(fb) && bname(fb) != cf.baseName && bname(fb) != cf.spikeName && !bWaterMat(up) {
					ok := true
					for _, o := range [5]pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}, {0, -1, 0}} {
						nb, there := dripAt(d, pos{c.x + o.x, floor + o.y, c.z + o.z})
						if !there || !(bnDripPoolWall.has(bname(nb)) || bWaterMat(nb)) {
							ok = false
							break
						}
					}
					if ok {
						d.bput(pos{c.x, floor, c.z}, sWater)
						floor2 = floor - 1
					}
				}
			}
			a := 0
			if r.nextDouble() < chance && hasCeil {
				if cb, _ := dripAt(d, pos{c.x, ceil, c.z}); !dripLava(cb) {
					t := int(r.nextIntN(2)) + 2
					for i := range t {
						if !cf.replaceDrip(d, pos{c.x, ceil + i, c.z}) {
							break
						}
					}
					ml := hgt
					if hasFloor2 {
						ml = min(hgt, ceil-floor2)
					}
					if r.nextFloat() <= dens {
						a = dripHeight(r, ml, absInt(dx)+absInt(dz))
					}
				}
			}
			b := 0
			if r.nextDouble() < chance && hasFloor2 {
				if fb, _ := dripAt(d, pos{c.x, floor2, c.z}); !dripLava(fb) {
					t := int(r.nextIntN(2)) + 2
					for i := range t {
						if !cf.replaceDrip(d, pos{c.x, floor2 - i, c.z}) {
							break
						}
					}
					if hasCeil {
						b = a + int(r.nextIntN(3)) - 1
						if b < 1 {
							b = 0
						}
					} else if r.nextFloat() <= dens {
						b = dripHeight(r, hgt, absInt(dx)+absInt(dz))
					}
				}
			}
			if hasCeil {
				if hasFloor2 && ceil-a <= floor2+b {
					lo, hi := max(floor2+1, ceil-a), min(floor2+b, ceil-1)
					m := lo
					if lo <= hi+1 {
						m = lo + int(r.nextIntN(int32(hi-lo+2)))
					}
					a, b = ceil-m, m-1-floor2
				}
				n := 0
				for n < a {
					s, ok := dripAt(d, pos{c.x, ceil - 1 - n, c.z})
					if !ok || !dripOpen(s) {
						break
					}
					n++
				}
				a = n
			}
			if hasFloor2 {
				n := 0
				for n < b {
					s, ok := dripAt(d, pos{c.x, floor2 + 1 + n, c.z})
					if !ok || !dripOpen(s) {
						break
					}
					n++
				}
				b = n
			}
			bb := r.nextBool()
			merge := hasCeil && bb && a > 0 && b > 0 && hasFloor2 && a+b == ceil-floor2-1
			if hasCeil {
				cf.growPointed(d, pos{c.x, ceil - 1, c.z}, dirDown, a, merge)
			}
			if hasFloor2 {
				cf.growPointed(d, pos{c.x, floor2 + 1, c.z}, dirUp, b, merge)
			}
		}
	}
	return true
}

// bDripstoneHeight is Java's DripstoneUtils.getDripstoneHeight, in double after a float divide.
func bDripstoneHeight(rr, radius, scale, blunt float32) float64 {
	dd := max(rr, blunt)
	e := float64(float32(dd/radius)) * 0.384
	v := (math.Pow(e, 1.3333333333333333)*0.75 - math.Pow(e, 0.6666666666666666) + math.Log(e)*-0.3333333333333333) * float64(scale)
	if v <= 0 {
		v = 0
	}
	return float64(radius) * (v / 0.384)
}

type dripWind struct {
	on     bool
	oy     int
	wx, wz float32
}

func (w dripWind) at(p pos) pos {
	if !w.on {
		return p
	}
	f := float32(w.oy - p.y)
	return pos{p.x + int(float32(w.wx*f)), p.y, p.z + int(float32(f*w.wz))}
}

type speleo struct {
	p           pos
	up          bool
	r           int
	blunt, scal float32
}

// dripCircle is 0x4cf0860: false if the centre or a ring point is open or lava.
func dripCircle(d *decoration, p pos, r int) bool {
	b, ok := dripAt(d, p)
	if !ok || dripOpen(b) || dripLava(b) {
		return false
	}
	fr := float32(r)
	step := float32(6 / fr)
	var g float32
	for {
		ci := int32(float32(float32(g*10430.378)+16384)) & 0xffff
		si := int32(float32(g*10430.378)) & 0xffff
		q := pos{p.x + int(float32(bedrockSin[ci]*fr)), p.y, p.z + int(float32(bedrockSin[si]*fr))}
		n, ok := dripAt(d, q)
		if !ok || dripOpen(n) || dripLava(n) {
			return false
		}
		g = g + step
		if 6.2831855 <= g {
			return true
		}
	}
}

// moveBack is 0x54fb0b0.
func (s *speleo) moveBack(d *decoration, w dripWind) bool {
	r := s.r
	if r < 2 {
		return false
	}
	for {
		bl := max(s.blunt, 0)
		h := int(bDripstoneHeight(bl, float32(r), s.scal, bl))
		n := min(10, h)
		if h > 0 {
			for j := range n {
				yy := s.p.y + j
				if s.up {
					yy = s.p.y - j
				}
				if b, _ := dripAt(d, pos{s.p.x, yy, s.p.z}); dripLava(b) {
					return false
				}
				if dripCircle(d, w.at(pos{s.p.x, yy, s.p.z}), r) {
					s.p.y, s.r = yy, r
					return true
				}
			}
		}
		old := r
		r /= 2
		s.r = r
		if old < 4 {
			return false
		}
	}
}

// placeBlocks is 0x54fb2f0.
func (s *speleo) placeBlocks(d *decoration, r *bmt, w dripWind) {
	rad := s.r
	for dx := -rad; dx <= rad; dx++ {
		for dz := -rad; dz <= rad; dz++ {
			f := float32(math.Sqrt(float64(float32(dz*dz + dx*dx))))
			if !(f <= float32(rad)) {
				continue
			}
			h := int(bDripstoneHeight(f, float32(rad), s.scal, s.blunt))
			if h <= 0 {
				continue
			}
			if float64(r.nextFloat()) < 0.2 {
				h = int(float32(float32(float32(r.nextFloat()*0.19999998807907104)+0.8) * float32(h)))
			}
			x, z := s.p.x+dx, s.p.z+dz
			maxy := math.MaxInt32
			if s.up {
				maxy = d.bedrockHeight(x, z)
			}
			y := s.p.y
			if !(h > 0 && y < maxy) {
				continue
			}
			placed := false
			for k := 0; ; {
				q := w.at(pos{x, y, z})
				if n, ok := dripAt(d, q); ok && (dripOpen(n) || dripLava(n)) {
					d.bput(q, sDripBlock)
					placed = true
				} else if placed {
					break
				}
				k++
				if h <= k {
					break
				}
				if s.up {
					y++
				} else {
					y--
				}
				if maxy <= y {
					break
				}
			}
		}
	}
}

// bedrockLargeDripstone is LargeSpeleothemFeature::place with Bedrock's config (T14).
func bedrockLargeDripstone(d *decoration, r *bmt, p pos) bool {
	if s, ok := dripAt(d, p); !ok || !dripOpen(s) {
		return false
	}
	edge := func(s bs) bool { return dripCfg.isBase(s) || dripLava(s) }
	ceil, okc := dripScan(d, p, 1, 30, dripOpen, edge)
	floor, okf := dripScan(d, p, -1, 30, dripOpen, edge)
	if !okc || !okf || floor-ceil > -5 {
		return false
	}
	j := min(max(int(float32(float32(ceil-floor-1)*0.33)), 3), 18)
	k := 3 + int(r.nextIntN(int32(j-3+1)))
	// Ranges as the code's float pairs: stalactite bluntness 0.3..0.9, stalagmite 0.4..1.0, scale 0.4..2.0.
	lo3, hi9, lo4, hi1, hi2 := float32(0.3), float32(0.9), float32(0.4), float32(1.0), float32(2.0)
	tb := float32(r.nextFloat()*(hi9-lo3)) + lo3
	ts := float32(r.nextFloat()*(hi2-lo4)) + lo4
	gb := float32(r.nextFloat()*(hi1-lo4)) + lo4
	gs := float32(r.nextFloat()*(hi2-lo4)) + lo4
	st := &speleo{pos{p.x, ceil - 1, p.z}, false, k, tb, ts}
	sm := &speleo{pos{p.x, floor + 1, p.z}, true, k, gb, gs}
	var w dripWind
	if k >= 4 && tb >= 0.6 && gb >= 0.6 {
		sp := float32(float32(0.3*r.nextFloat()) + 0)
		a := float32(float32(float32(r.nextFloat()*float32(math.Pi))+0) * 10430.378)
		sn, cs := bedrockSin[int32(a)&0xffff], bedrockSin[int32(float32(16384+a))&0xffff]
		w = dripWind{true, p.y, float32(cs * sp), float32(sp * sn)}
	}
	ok1 := st.moveBack(d, w)
	ok2 := sm.moveBack(d, w)
	placed := false
	if ok1 && st.p.y-int(float32(bDripstoneHeight(0, float32(st.r), st.scal, st.blunt))) > minY {
		st.placeBlocks(d, r, w)
		placed = true
	}
	if ok2 && sm.p.y+int(float32(bDripstoneHeight(0, float32(sm.r), sm.scal, sm.blunt))) < maxY+1 {
		sm.placeBlocks(d, r, w)
		placed = true
	}
	return placed
}
