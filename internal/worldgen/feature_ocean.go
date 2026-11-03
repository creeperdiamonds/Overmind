package worldgen

import (
	"fmt"
	"math"
)

// Coral reefs and icebergs. The study does not write these up and the traced chunks hold no warm or
// frozen ocean, so these follow Java 26.1's coral tree, claw and mushroom and its iceberg as written
// from knowledge of the game, unverified against a Java run. No Mojang code or file is in this repo.

var coralKinds = [5]string{"tube", "brain", "bubble", "fire", "horn"}

var (
	// coralBlocks, corals (plants then fans) and wallFans in the game's tag order.
	coralBlocks [5]bs
	corals      [10]bs
	wallFans    [5][4]bs // By kind and facing north, east, south, west.
)

func init() {
	_ = sAir
	for i, k := range coralKinds {
		coralBlocks[i] = stateOf(k + "_coral_block")
		corals[i] = stateOf(k + "_coral[waterlogged=true]")
		corals[5+i] = stateOf(k + "_coral_fan[waterlogged=true]")
		for j, a := range horizontal {
			wallFans[i][j] = stateOf(fmt.Sprintf("%s_coral_wall_fan[facing=%s,waterlogged=true]", k, dirName(a)))
		}
	}
}

func isCoralPlant(s bs) bool {
	for _, c := range corals {
		if c == s {
			return true
		}
	}
	return false
}

// placeCoral puts one coral block in water, with maybe a coral or a sea pickle on it and fans on its
// sides.
func placeCoral(d *decoration, p pos, block bs) bool {
	r := &d.r
	up := p.step(dUp)
	cur := d.at(p)
	if !(waterBlock(cur) || isCoralPlant(cur)) || !waterBlock(d.at(up)) {
		return false
	}
	d.set(p.x, p.y, p.z, block)
	if r.nextFloat() < 0.25 {
		c := corals[r.nextInt(len(corals))]
		d.set(up.x, up.y, up.z, c)
	} else if r.nextFloat() < 0.05 {
		d.set(up.x, up.y, up.z, stateOf(fmt.Sprintf("sea_pickle[pickles=%d,waterlogged=true]", r.nextInt(4)+1)))
	}
	for i, a := range horizontal {
		if r.nextFloat() < 0.2 {
			side := p.step(a)
			if waterBlock(d.at(side)) {
				k := r.nextInt(len(coralKinds))
				d.set(side.x, side.y, side.z, wallFans[k][i])
			}
		}
	}
	return true
}

func coralFeature(shape func(d *decoration, o pos, block bs) bool) feature {
	return func(d *decoration, o pos) bool {
		return shape(d, o, coralBlocks[d.r.nextInt(len(coralBlocks))])
	}
}

func coralTree(d *decoration, o pos, block bs) bool {
	r := &d.r
	p := o
	h := r.nextInt(3) + 1
	for range h {
		if !placeCoral(d, p, block) {
			return true
		}
		p = p.step(dUp)
	}
	top := p
	n := r.nextInt(3) + 2
	dirs := shuffledDirs(r, horizontal[:])
	for _, a := range dirs[:n] {
		q := top.step(a)
		length := r.nextInt(5) + 2
		k := 0
		for j := 0; j < length && placeCoral(d, q, block); j++ {
			k++
			q = q.step(dUp)
			if j == 0 || k >= 2 && r.nextFloat() < 0.25 {
				q = q.step(a)
				k = 0
			}
		}
	}
	return true
}

func coralClaw(d *decoration, o pos, block bs) bool {
	r := &d.r
	if !placeCoral(d, o, block) {
		return false
	}
	main := pickHorizontal(r)
	n := r.nextInt(2) + 2
	dirs := shuffledDirs(r, []dir{main, main.clockwise(), main.clockwise().opposite()})
	for _, a := range dirs[:n] {
		p := o.step(a)
		side := r.nextInt(2) + 1
		var up dir
		var length int
		if a == main {
			up = main
			length = r.nextInt(3) + 2
		} else {
			p = p.step(dUp)
			up = [2]dir{a, dUp}[r.nextInt(2)]
			length = r.nextInt(3) + 3
		}
		for i := 0; i < side && placeCoral(d, p, block); i++ {
			p = p.step(up)
		}
		p = p.step(up.opposite()).step(dUp)
		for range length {
			p = p.step(main)
			if !placeCoral(d, p, block) {
				break
			}
			if r.nextFloat() < 0.25 {
				p = p.step(dUp)
			}
		}
	}
	return true
}

func coralMushroom(d *decoration, o pos, block bs) bool {
	r := &d.r
	ys := r.nextInt(3) + 3
	xs := r.nextInt(3) + 3
	zs := r.nextInt(3) + 3
	down := r.nextInt(3) + 1
	for x := 0; x <= xs; x++ {
		for y := 0; y <= ys; y++ {
			for z := 0; z <= zs; z++ {
				p := pos{o.x + x, o.y + y - down, o.z + z}
				xe, ye, ze := x == 0 || x == xs, y == 0 || y == ys, z == 0 || z == zs
				if (!xe || !ye) && (!ze || !ye) && (!xe || !ze) && (xe || ye || ze) && !(r.nextFloat() < 0.1) {
					placeCoral(d, p, block)
				}
			}
		}
	}
	return true
}

// icebergDims are an iceberg's shape draws, the first the feature makes.
type icebergDims struct {
	snowy, ellipse             bool
	angle                      float64
	ea, ec, over, under, width int
}

// icebergShape draws an iceberg's shape. An ellipse draws its height above water from 6 to 11, a
// round one from 3 to 17 and maybe a tall extra: one draw or the other, never both.
func icebergShape(r *featRand) (s icebergDims) {
	s.snowy = r.nextDouble() > 0.7
	s.angle = r.nextDouble() * 2 * math.Pi
	s.ea = 11 - r.nextInt(5)
	s.ec = 3 + r.nextInt(3)
	s.ellipse = r.nextDouble() > 0.7
	if s.ellipse {
		s.over = r.nextInt(6) + 6
	} else {
		s.over = r.nextInt(15) + 3
		if r.nextDouble() > 0.9 {
			s.over += r.nextInt(19) + 7
		}
	}
	s.under = min(s.over+r.nextInt(11), 18)
	s.width = min(s.over+r.nextInt(7)-r.nextInt(5), 11)
	return s
}

// iceberg is the iceberg feature, of packed or blue ice.
func iceberg(main bs) feature {
	return func(d *decoration, o pos) bool {
		r := &d.r
		o.y = SeaLevel
		dims := icebergShape(r)
		snowy, angle, ea, ec, ellipse := dims.snowy, dims.angle, dims.ea, dims.ec, dims.ellipse
		over, under, width := dims.over, dims.under, dims.width
		a := 11
		if ellipse {
			a = ea
		}
		ib := icebergRun{d: d, o: o, main: main, snowy: snowy, ellipse: ellipse, ec: ec, angle: angle}
		for x := -a; x < a; x++ {
			for z := -a; z < a; z++ {
				for y := 0; y < over; y++ {
					var rad int
					if ellipse {
						rad = radiusEllipse(y, over, width)
					} else {
						rad = radiusRound(r, y, over, width)
					}
					if ellipse || x < rad {
						ib.block(over, x, y, z, rad, a)
					}
				}
			}
		}
		ib.smooth(width, over, ea)
		for x := -a; x < a; x++ {
			for z := -a; z < a; z++ {
				for y := -1; y > -under; y-- {
					da := a
					if ellipse {
						da = int(math.Ceil(float64(float32(a) * float32(1-float32(math.Pow(float64(y), 2))/float32(float32(under)*8)))))
					}
					rad := radiusSteep(r, -y, under, width)
					if x < rad {
						ib.block(under, x, y, z, rad, da)
					}
				}
			}
		}
		cut := r.nextDouble() > 0.7
		if ellipse {
			cut = r.nextDouble() > 0.1
		}
		if cut {
			ib.cutOut(width, over, ea)
		}
		return true
	}
}

type icebergRun struct {
	d       *decoration
	o       pos
	main    bs
	snowy   bool
	ellipse bool
	ec      int
	angle   float64
}

func isIcebergState(s bs) bool { return s == sPacked || s == sSnowBl || s.block() == sBlueIce.block() }

var sBlueIce = laterState("blue_ice")

func (ib *icebergRun) block(height, x, y, z, rad, a int) {
	d, r := ib.d, &ib.d.r
	var dist float64
	if ib.ellipse {
		c := ib.ec
		if y > 0 && height-y <= 3 {
			c = ib.ec - (4 - (height - y))
		}
		dist = ellipseDist(x, z, 0, 0, a, c, ib.angle)
	} else {
		off := float32(10*min(max(r.nextFloat(), 0.2), 0.8)) / float32(rad)
		dist = float64(off) + math.Pow(float64(x), 2) + math.Pow(float64(z), 2) - math.Pow(float64(rad), 2)
	}
	if dist >= 0 {
		return
	}
	p := ib.o.add(x, y, z)
	cutoff := -0.5
	if !ib.ellipse {
		cutoff = float64(-6 - r.nextInt(3))
	}
	if dist > cutoff && r.nextDouble() > 0.9 {
		return
	}
	s := d.at(p)
	if !(s.isAir() || s == sSnowBl || s == sIce || waterBlock(s)) {
		return
	}
	random := !ib.ellipse || r.nextDouble() > 0.05
	div := 2
	if ib.ellipse {
		div = 3
	}
	if ib.snowy && !waterBlock(s) && float64(height-y) <= float64(r.nextInt(max(1, height/div)))+float64(height)*0.6 && random {
		d.set(p.x, p.y, p.z, sSnowBl)
	} else {
		d.set(p.x, p.y, p.z, ib.main)
	}
}

func ellipseDist(x, z, ox, oz, a, c int, angle float64) float64 {
	dx, dz := float64(x-ox), float64(z-oz)
	u := (dx*math.Cos(angle) - dz*math.Sin(angle)) / float64(a)
	v := (dx*math.Sin(angle) + dz*math.Cos(angle)) / float64(c)
	return math.Pow(u, 2) + math.Pow(v, 2) - 1
}

func radiusRound(r *featRand, y, height, width int) int {
	k := 3.5 - r.nextFloat()
	scale := float32(1-float32(math.Pow(float64(y), 2))/float32(float32(height)*k)) * float32(width)
	if height > 15+r.nextInt(5) {
		ty := y
		if y < 3+r.nextInt(6) {
			ty = y / 2
		}
		scale = float32(1-float32(ty)/float32(float32(float32(height)*k)*0.4)) * float32(width)
	}
	return int(math.Ceil(float64(scale / 2)))
}

func radiusEllipse(y, height, width int) int {
	scale := float32(1-float32(math.Pow(float64(y), 2))/float32(height)) * float32(width)
	return int(math.Ceil(float64(scale / 2)))
}

func radiusSteep(r *featRand, y, height, width int) int {
	k := 1 + r.nextFloat()/2
	scale := float32(1-float32(y)/float32(float32(height)*k)) * float32(width)
	return int(math.Ceil(float64(scale / 2)))
}

func (ib *icebergRun) smooth(width, height, ea int) {
	d := ib.d
	a := width / 2
	if ib.ellipse {
		a = ea
	}
	snow := stateOf("snow[layers=1]").block()
	for x := -a; x <= a; x++ {
		for z := -a; z <= a; z++ {
			for y := 0; y <= height; y++ {
				p := ib.o.add(x, y, z)
				s := d.at(p)
				if !isIcebergState(s) && s.block() != snow {
					continue
				}
				if d.at(p.step(dDown)).isAir() {
					d.set(p.x, p.y, p.z, sAir)
					d.set(p.x, p.y+1, p.z, sAir)
				} else if isIcebergState(s) {
					n := 0
					for _, a := range [4]dir{dWest, dEast, dNorth, dSouth} {
						if !isIcebergState(d.at(p.step(a))) {
							n++
						}
					}
					if n >= 3 {
						d.set(p.x, p.y, p.z, sAir)
					}
				}
			}
		}
	}
}

func (ib *icebergRun) cutOut(width, height, ea int) {
	d, r := ib.d, &ib.d.r
	sx, sz := 1, 1
	if r.nextBoolean() {
		sx = -1
	}
	if r.nextBoolean() {
		sz = -1
	}
	xo := r.nextInt(max(width/2-2, 1))
	if r.nextBoolean() {
		xo = width/2 + 1 - r.nextInt(max(width-width/2-1, 1))
	}
	zo := r.nextInt(max(width/2-2, 1))
	if r.nextBoolean() {
		zo = width/2 + 1 - r.nextInt(max(width-width/2-1, 1))
	}
	if ib.ellipse {
		xo = r.nextInt(max(ea-5, 1))
		zo = xo
	}
	lx, lz := sx*xo, sz*zo
	angle := 0.0
	if ib.ellipse {
		angle = ib.angle + math.Pi/2
	} else {
		angle = r.nextDouble() * 2 * math.Pi
	}
	carve := func(rad, y int, underWater bool) {
		a := rad + 1 + ea/3
		c := min(rad-3, 3) + ib.ec/2 - 1
		for x := -a; x < a; x++ {
			for z := -a; z < a; z++ {
				if ellipseDist(x, z, lx, lz, a, c, angle) >= 0 {
					continue
				}
				p := ib.o.add(x, y, z)
				if s := d.at(p); isIcebergState(s) || s == sSnowBl {
					if underWater {
						d.set(p.x, p.y, p.z, sWater)
					} else {
						d.set(p.x, p.y, p.z, sAir)
						if up := p.step(dUp); d.at(up).block() == stateOf("snow[layers=1]").block() {
							d.set(up.x, up.y, up.z, sAir)
						}
					}
				}
			}
		}
	}
	for y := 0; y < height-3; y++ {
		carve(radiusRound(r, y, height, width), y, false)
	}
	for y := -1; y > -height+r.nextInt(5); y-- {
		carve(radiusSteep(r, -y, height, width), y, true)
	}
}

func (r *featRand) nextBoolean() bool { return r.nextBool() }

func defineOcean(t *tableBuilder) {
	reef := simpleSelector(newNestedPlacement(coralFeature(coralTree)), newNestedPlacement(coralFeature(coralClaw)), newNestedPlacement(coralFeature(coralMushroom)))
	t.def("warm_ocean_vegetation", reef, noiseCountMod{20, 400, 0}, inSquareMod{}, heightmapMod{hmOceanFloorWG}, biomeMod{})
	t.def("iceberg_packed", iceberg(sPacked), rarityMod{16}, inSquareMod{}, biomeMod{})
	t.def("iceberg_blue", iceberg(sBlueIce), rarityMod{200}, inSquareMod{}, biomeMod{})
}
