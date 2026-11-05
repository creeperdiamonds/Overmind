package worldgen

import "math"

// Bedrock's frozen-ocean ice: the rule frozen_ocean_after_surface_ice_feature places
// minecraft:legacy:ice_feature, LegacyIceFeature (0x53fcb50), which runs an iceberg (0x516f1d0) and blue
// ice (0x4ba1ff0) on the rule's random. Our own Go of NOTES-bedrock-exact T26 (read from the code and
// simulated there: deep_frozen_ocean_4 99.58% / 99.55% recall / precision of the ice blocks,
// frozen_ocean_4 97.16% / 99.36%). One icy-ocean feature, not Java's two: its main block is packed ice,
// or blue ice one time in a hundred. Bedrock's changes from Java's iceberg are marked BEDROCK.
//
// Not modelled: an unread extra test in the smoothing step (an object at dimension +0x1b0, T26).

const icebergSea = 63 // The origin's y: the dimension's sea level (T26; 62 scores worse).

// legacyIce is LegacyIceFeature's place at a chunk's rule position (its corner).
func legacyIce(d *decoration, r *bmt, p pos) bool {
	if r.nextIntN(8) == 0 {
		dx, dz := int(r.nextIntN(8)), int(r.nextIntN(8))
		bedrockIceberg(d, r, pos{p.x + 12 + dx, icebergSea, p.z + 12 + dz})
	}
	n := int(r.nextIntN(20))
	for range n {
		x := p.x + 8 + int(r.nextIntN(16))
		y := 30 + int(r.nextIntN(32))
		z := p.z + 8 + int(r.nextIntN(16))
		bedrockBlueIce(d, r, pos{x, y, z})
	}
	return true
}

// tableSinCos is sin and cos of an angle from Bedrock's 65536-entry table (T26).
func tableSinCos(angle float32) (float64, float64) {
	i := int(angle * 10430.378)
	return float64(bedrockSin[i&0xffff]), float64(bedrockSin[(i+16384)&0xffff])
}

// bIceberg is one iceberg being placed.
type bIceberg struct {
	d        *decoration
	r        *bmt
	o        pos
	main     bs
	ellipse  bool
	snowTop  bool
	ea, ec   int
	width    int
	angle    float32
	sin, cos float64
}

func isBergIce(s bs) bool { return s == sPacked || s == sSnowBl || s.block() == sBlueIce.block() }

// roundRadius is the round iceberg's radius at a height above water (T26), with its draws.
func (b *bIceberg) roundRadius(y, h int) int {
	r := b.r
	k := (3.5 - r.nextFloat()) * float32(h)
	var s float64
	if int(r.nextIntN(5))+15 < h {
		t := int(r.nextIntN(6))
		yy := y
		if y < t+3 {
			yy = y >> 1
		}
		s = float64((float32(yy)/(k*-0.4) + 1) * float32(b.width))
	} else {
		s = (1 - float64(y*y)/float64(k)) * float64(b.width)
	}
	return int(math.Ceil(s * 0.5))
}

// steepRadius is the radius under water (and the cut-out's under part) at depth i of height h.
func (b *bIceberg) steepRadius(i, h int) int {
	f := float64(b.r.nextFloat())*0.5 + 1
	return int(math.Ceil((1 - float64(i)/(f*float64(h))) * float64(b.width) * 0.5))
}

func bedrockIceberg(d *decoration, r *bmt, o pos) {
	n5 := int(r.nextIntN(5))
	b := &bIceberg{d: d, r: r, o: o, ea: 11 - n5}
	b.ec = int(r.nextIntN(3)) + 3
	i6 := int(r.nextIntN(6))
	i15 := int(r.nextIntN(15))
	b.ellipse = r.nextFloat() > 0.8 // BEDROCK: 0.8 on a float (Java 0.7 on a double).
	var h int
	if b.ellipse {
		h = i6 + 6
	} else if r.nextFloat() > 0.9 {
		h = i15 + int(r.nextIntN(19)) + 10
	} else {
		h = i15 + 3
	}
	b.snowTop = r.nextFloat() > 0.7
	angle := r.nextFloat() * 2 * math.Pi
	b.angle = angle
	b.sin, b.cos = tableSinCos(angle)
	b.width = min(int(r.nextIntN(7))+h-int(r.nextIntN(5)), 11)
	b.main = sPacked
	if r.nextFloat() <= 0.01 {
		b.main = sBlueIce
	}
	a := 11
	if b.ellipse {
		a = b.ea
	}
	// Above water.
	for x := -a; x < a; x++ {
		for z := -a; z < a; z++ {
			for y := 0; y < h; y++ {
				if b.ellipse {
					rad := int(math.Ceil(float64(float32((1-float64(y*y)/float64(h))*float64(b.width))) * 0.5))
					b.block(h, x, y, z, rad, a)
				} else if rad := b.roundRadius(y, h); x < rad {
					b.block(h, x, y, z, rad, a)
				}
			}
		}
	}
	b.smooth(h)
	// Under water, drawn after the smoothing (BEDROCK; Java draws it first).
	u := min(int(r.nextIntN(11))+h, 18)
	if u > 1 {
		for x := -a; x < a; x++ {
			for z := -a; z < a; z++ {
				for i := 1; i < u; i++ {
					rad := b.steepRadius(i, u)
					if x >= rad {
						continue
					}
					da := a
					if b.ellipse {
						da = int(math.Ceil((1 - float64(i*i)/float64(8*u)) * float64(a)))
					}
					b.block(u, x, -i, z, rad, da)
				}
			}
		}
	}
	b.cutOut(h)
}

// block is one cell of the iceberg's shape (0x5170580).
func (b *bIceberg) block(height, x, y, z, rad, a int) {
	r := b.r
	var dist float64
	if b.ellipse {
		u := (float64(x)*b.cos - float64(z)*b.sin) / float64(a)
		v := (float64(x)*b.sin + float64(z)*b.cos) / float64(b.ec) // BEDROCK: no shrink near the top.
		dist = u*u + v*v - 1
	} else {
		f := r.nextFloat()
		off := float64(8)
		if f <= 0.8 {
			off = float64(10 * max(f, 0.2))
		}
		dist = float64(x*x+z*z-rad*rad) + off/float64(rad) // BEDROCK: plus (Java: minus).
	}
	if dist >= 0 {
		return
	}
	cmp := -0.5
	if !b.ellipse {
		cmp = float64(-6 - int(r.nextIntN(3)))
	}
	if dist > cmp && r.nextFloat() > 0.9 {
		return
	}
	px, py, pz := b.o.x+x, b.o.y+y, b.o.z+z
	s := b.d.block(px, py, pz)
	water := waterBlock(s)
	if !(s.isAir() || s == sSnowBl || s == sIce || water) {
		return
	}
	rnd, div := true, 2
	if b.ellipse {
		rnd, div = r.nextFloat() > 0.05, 3
	}
	if b.snowTop && !water {
		r2 := int(r.nextIntN(int32(max(1, height/div))))
		if rnd && float64(height-y) <= float64(r2)+0.6*float64(height) {
			b.d.set(px, py, pz, sSnowBl)
			return
		}
	}
	b.d.set(px, py, pz, b.main)
}

// smooth removes floating and thin ice above water (T26), with no draws.
func (b *bIceberg) smooth(h int) {
	d := b.d
	sa := b.width / 2
	if b.ellipse {
		sa = b.ea
	}
	for x := -sa; x <= sa; x++ {
		for z := -sa; z <= sa; z++ {
			for y := 0; y <= h; y++ {
				px, py, pz := b.o.x+x, b.o.y+y, b.o.z+z
				s := d.block(px, py, pz)
				if !isBergIce(s) && s.block() != sSnowLayer.block() {
					continue
				}
				if d.block(px, py-1, pz).isAir() {
					d.set(px, py, pz, sAir)
					d.set(px, py+1, pz, sAir)
				} else if isBergIce(s) {
					n := 0
					for _, o := range [4][2]int{{0, 1}, {-1, 0}, {0, -1}, {1, 0}} {
						if !isBergIce(d.block(px+o[0], py, pz+o[1])) {
							n++
						}
					}
					if n >= 3 {
						d.set(px, py, pz, sAir)
					}
				}
			}
		}
	}
}

// cutOut is the iceberg's cut-out (T26).
func (b *bIceberg) cutOut(h int) {
	r := b.r
	f1 := r.nextFloat()
	cut := f1 > 0.1
	if !b.ellipse {
		cut = r.nextFloat() > 0.7
	}
	if !cut {
		return
	}
	w := b.width
	off := func() int {
		v := int(r.nextIntN(int32(max(w/2, 3) - 2)))
		if r.nextBool() {
			v = w/2 + 1 - int(r.nextIntN(int32(max(w-w/2-1, 1))))
		}
		return v
	}
	xo := off()
	zo := off()
	if b.ellipse {
		xo = int(r.nextIntN(int32(max(b.ea-5, 1))))
		zo = xo
	}
	if r.nextBool() { // BEDROCK: the signs after the offsets.
		xo = -xo
	}
	if r.nextBool() {
		zo = -zo
	}
	var sin, cos float64
	if b.ellipse {
		sin, cos = tableSinCos(b.angle + math.Pi/2) // The iceberg's angle + pi/2 (0x1217d08).
	} else {
		sin, cos = tableSinCos(r.nextFloat() * 2 * math.Pi)
	}
	if h > 3 {
		for y := 0; y < h-3; y++ {
			b.carve(b.roundRadius(y, h), y, xo, zo, sin, cos, false)
		}
	}
	for y, i := -1, 1; int(r.nextIntN(5))-h < y; y, i = y-1, i+1 {
		b.carve(b.steepRadius(i, h), y, xo, zo, sin, cos, true)
	}
}

// carve is the cut-out at one height (0x5170990), with no draws.
func (b *bIceberg) carve(rad, y, xo, zo int, sin, cos float64, under bool) {
	d := b.d
	a := rad + b.ea/3 + 1
	c := min(rad, 6) + b.ec/2 - 4
	for x := -a; x < a; x++ {
		for z := -a; z < a; z++ {
			dx, dz := float64(x-xo), float64(z-zo)
			u := (dx*sin + dz*cos) / float64(c)
			v := (dx*cos - dz*sin) / float64(a)
			if u*u+v*v-1 >= 0 {
				continue
			}
			px, py, pz := b.o.x+x, b.o.y+y, b.o.z+z
			s := d.block(px, py, pz)
			if !isBergIce(s) && s.block() != sSnowLayer.block() {
				continue
			}
			if under {
				d.set(px, py, pz, sWater)
			} else {
				d.set(px, py, pz, sAir)
				if d.block(px, py+1, pz).block() == sSnowLayer.block() {
					d.set(px, py+1, pz, sAir)
				}
			}
		}
	}
}

// bedrockBlueIce is BlueIceFeature (0x4ba1ff0, T26): below the sea in or over water next to packed
// ice; then 200 tries around it, each placed only beside one to four blue ice blocks (BEDROCK; Java:
// any).
func bedrockBlueIce(d *decoration, r *bmt, p pos) {
	if p.y >= icebergSea {
		return
	}
	if !waterBlock(d.block(p.x, p.y, p.z)) && !waterBlock(d.block(p.x, p.y-1, p.z)) {
		return
	}
	found := false
	for _, o := range [5][3]int{{0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if d.block(p.x+o[0], p.y+o[1], p.z+o[2]) == sPacked {
			found = true
			break
		}
	}
	if !found {
		return
	}
	d.set(p.x, p.y, p.z, sBlueIce)
	for range 200 {
		dy := int(r.nextIntN(5)) - int(r.nextIntN(6))
		rr := 3
		if dy < 2 {
			rr = dy/2 + 3
		}
		qx := p.x + int(r.nextIntN(int32(rr))) - int(r.nextIntN(int32(rr)))
		qz := p.z + int(r.nextIntN(int32(rr))) - int(r.nextIntN(int32(rr)))
		qy := p.y + dy
		s := d.block(qx, qy, qz)
		if !(s.isAir() || waterBlock(s) || s == sPacked || s == sIce) {
			continue
		}
		n := 0
		for _, v := range dirVec {
			if d.block(qx+v.x, qy+v.y, qz+v.z).block() == sBlueIce.block() {
				n++
			}
		}
		if n >= 1 && n <= 4 {
			d.set(qx, qy, qz, sBlueIce)
		}
	}
}
