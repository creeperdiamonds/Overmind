package worldgen

import "math"

// Dripstone caves: dripstone clusters, single pointed dripstones and large dripstones. The study does
// not write these up, so the algorithms and parameters were written from knowledge of Java 26.1's
// dripstone features and corrected against the study's trace fixtures (TestGoldenFeatureTraces). No
// Mojang code or file is in this repo.

func emptyOrWater(s bs) bool { return s.isAir() || s.block() == sWater.block() }

func emptyWaterOrLava(s bs) bool { return emptyOrWater(s) || s.block() == sLava.block() }

func dripstoneBase(s bs) bool { return s.block() == sDripstone.block() || s.tagged(tagBaseStone) }

var (
	sDripstone = laterState("dripstone_block")
	// pointedStates holds pointed dripstone by direction (0 up, 1 down), thickness and water.
	pointedStates = func() (t [2][5][2]bs) {
		for d, dn := range [2]string{"up", "down"} {
			for k, th := range dripThickness {
				for w, wn := range bools {
					t[d][k][w] = laterState("pointed_dripstone[thickness=" + th + ",vertical_direction=" + dn + ",waterlogged=" + wn + "]")
				}
			}
		}
		return t
	}()
)

var dripThickness = [5]string{"tip_merge", "tip", "frustum", "middle", "base"}

const (
	thMerge = iota
	thTip
	thFrustum
	thMiddle
	thBase
)

func (d *decoration) dripstoneBlock(p pos) bool {
	if d.at(p).tagged(tagBaseStone) {
		d.set(p.x, p.y, p.z, sDripstone)
		return true
	}
	return false
}

// growPointed grows a pointed dripstone of n blocks from start towards tip, base first, if the block
// behind start can hold it.
func (d *decoration) growPointed(start pos, tip dir, n int, merged bool) {
	if !dripstoneBase(d.at(start.step(tip.opposite()))) {
		return
	}
	di := 0
	if tip == dDown {
		di = 1
	}
	p := start
	put := func(th int) {
		w := btoi(d.at(p).has(fWater))
		d.set(p.x, p.y, p.z, pointedStates[di][th][w])
		p = p.step(tip)
	}
	if n >= 3 {
		put(thBase)
		for range n - 3 {
			put(thMiddle)
		}
	}
	if n >= 2 {
		put(thFrustum)
	}
	if n >= 1 {
		if merged {
			put(thMerge)
		} else {
			put(thTip)
		}
	}
}

// pointedDripstone is the pointed_dripstone feature: one or two blocks on a patch of dripstone.
func pointedDripstone(taller, spread, spread2, spread3 float32) feature {
	return func(d *decoration, o pos) bool {
		r := &d.r
		up := dripstoneBase(d.at(o.step(dUp)))
		down := dripstoneBase(d.at(o.step(dDown)))
		var tip dir
		switch {
		case up && down:
			tip = dUp
			if r.nextBool() {
				tip = dDown
			}
		case up:
			tip = dDown
		case down:
			tip = dUp
		default:
			return false
		}
		root := o.step(tip.opposite())
		d.dripstoneBlock(root)
		for _, a := range horizontal {
			if r.nextFloat() > spread {
				continue
			}
			p := root.step(a)
			d.dripstoneBlock(p)
			if r.nextFloat() > spread2 {
				continue
			}
			p2 := p.step(allDirs[r.nextInt(6)])
			d.dripstoneBlock(p2)
			if r.nextFloat() > spread3 {
				continue
			}
			d.dripstoneBlock(p2.step(allDirs[r.nextInt(6)]))
		}
		n := 1
		if r.nextFloat() < taller && emptyOrWater(d.at(o.step(tip))) {
			n = 2
		}
		d.growPointed(o, tip, n, false)
		return true
	}
}

// dripColumn is the game's column scan: the edges up and down of a run of cells inside it.
type dripColumn struct {
	floor, ceil       int
	hasFloor, hasCeil bool
}

func scanColumn(d *decoration, o pos, reach int, inside, edge func(bs) bool) (dripColumn, bool) {
	if !inside(d.at(o)) {
		return dripColumn{}, false
	}
	var c dripColumn
	one := func(a dir) (int, bool) {
		p := o
		for i := 1; i < reach && inside(d.at(p)); i++ {
			p = p.step(a)
		}
		return p.y, edge(d.at(p))
	}
	c.ceil, c.hasCeil = one(dUp)
	c.floor, c.hasFloor = one(dDown)
	return c, true
}

func (c dripColumn) height() (int, bool) {
	if c.hasFloor && c.hasCeil {
		return c.ceil - c.floor - 1, true
	}
	return 0, false
}

func normalF(r *featRand, mean, dev float32) float32 {
	return mean + float32(float32(r.gaussian())*dev)
}

func clampF(v, a, b float32) float32 { return min(max(v, a), b) }

// clampedMapF is the game's float clamped map.
func clampedMapF(v, a, b, c, e float32) float32 {
	t := (v - a) / (b - a)
	if t < 0 {
		return c
	}
	if t > 1 {
		return e
	}
	return c + float32(t*(e-c))
}

// clusterCfg is a dripstone cluster's configuration.
type clusterCfg struct {
	reach                         int
	height, radius, thickness     intProvider
	heightDiff, heightDev         int
	density                       [2]float32 // Uniform.
	wetMean, wetDev, wetLo, wetHi float32
	edgeChance                    float32
	edgeReach, centreReach        int
}

func dripstoneCluster(c *clusterCfg) feature {
	return func(d *decoration, o pos) bool {
		if !emptyOrWater(d.at(o)) {
			return false
		}
		r := &d.r
		h := c.height(r)
		wet := clampF(normalF(r, c.wetMean, c.wetDev), c.wetLo, c.wetHi)
		density := r.betweenF(c.density[0], c.density[1])
		xr, zr := c.radius(r), c.radius(r)
		for dx := -xr; dx <= xr; dx++ {
			for dz := -zr; dz <= zr; dz++ {
				k := min(xr-absInt(dx), zr-absInt(dz))
				chance := float64(clampedMapF(float32(k), 0, float32(c.edgeReach), c.edgeChance, 1))
				c.column(d, o.add(dx, 0, dz), dx, dz, wet, chance, h, density)
			}
		}
		return true
	}
}

func (c *clusterCfg) dripHeight(r *featRand, dx, dz int, density float32, maxH int) int {
	if r.nextFloat() > density {
		return 0
	}
	dist := absInt(dx) + absInt(dz)
	mean := float32(clampedMap(float64(dist), 0, float64(c.centreReach), float64(maxH)/2, 0))
	return int(clampF(normalF(r, mean, float32(c.heightDev)), 0, float32(maxH)))
}

func (c *clusterCfg) column(d *decoration, p pos, dx, dz int, wetness float32, chance float64, h int, density float32) {
	col, ok := scanColumn(d, p, c.reach, emptyOrWater, func(s bs) bool { return !emptyOrWater(s) })
	if !ok || (!col.hasCeil && !col.hasFloor) {
		return
	}
	r := &d.r
	ceil, hasCeil := col.ceil, col.hasCeil
	wet := r.nextFloat() < wetness
	if wet && col.hasFloor && canPool(d, pos{p.x, col.floor, p.z}) {
		d.set(p.x, col.floor, p.z, sWater)
		col.floor--
	}
	floor, hasFloor := col.floor, col.hasFloor
	isLava := func(y int) bool { return d.at(pos{p.x, y, p.z}).block() == sLava.block() }
	top := 0
	// Both chances are drawn whether or not the column has that end.
	if stalactite := r.nextDouble() < chance; hasCeil && stalactite && !isLava(ceil) {
		n := c.thickness(r)
		c.layer(d, pos{p.x, ceil, p.z}, n, dUp)
		maxH := h
		if hasFloor {
			maxH = min(h, ceil-floor)
		}
		top = c.dripHeight(r, dx, dz, density, maxH)
	}
	bottom := 0
	if stalagmite := r.nextDouble() < chance; hasFloor && stalagmite && !isLava(floor) {
		n := c.thickness(r)
		c.layer(d, pos{p.x, floor, p.z}, n, dDown)
		if hasCeil {
			bottom = max(0, top+r.between(-c.heightDiff, c.heightDiff))
		} else {
			bottom = c.dripHeight(r, dx, dz, density, h)
		}
	}
	down, up := top, bottom
	if hasCeil && hasFloor && ceil-top <= floor+bottom {
		lo := max(ceil-top, floor+1)
		hi := min(floor+bottom, ceil-1)
		meet := r.between(lo, hi+1)
		down = ceil - meet
		up = meet - 1 - floor
	}
	ch, hok := col.height()
	merged := r.nextBool() && down > 0 && up > 0 && hok && down+up == ch
	if hasCeil {
		d.growPointed(pos{p.x, ceil - 1, p.z}, dDown, down, merged)
	}
	if hasFloor {
		d.growPointed(pos{p.x, floor + 1, p.z}, dUp, up, merged)
	}
}

func (c *clusterCfg) layer(d *decoration, p pos, n int, a dir) {
	for range n {
		if !d.dripstoneBlock(p) {
			return
		}
		p = p.step(a)
	}
}

func canPool(d *decoration, p pos) bool {
	s := d.at(p)
	if s.block() == sWater.block() || s.block() == sDripstone.block() || stateBlock[s] == pointedStates[0][0][0].block() {
		return false
	}
	if d.at(p.step(dUp)).has(fWater) {
		return false
	}
	nextToWater := func(q pos) bool { s := d.at(q); return s.tagged(tagBaseStone) || s.has(fWater) }
	for _, a := range horizontal {
		if !nextToWater(p.step(a)) {
			return false
		}
	}
	return nextToWater(p.step(dDown))
}

// largeCfg is a large dripstone's configuration.
type largeCfg struct {
	reach                 int
	radiusLo, radiusHi    int
	scale                 [2]float32
	ratio                 float32
	topBlunt, bottomBlunt [2]float32
	wind                  [2]float32
	windRadius            int
	windBlunt             float32
}

// dripstoneHeight is the shape of a large dripstone: its height at a distance from the middle.
func dripstoneHeight(radius, maxRadius, scale, minRadius float64) float64 {
	if radius < minRadius {
		radius = minRadius
	}
	r := radius / maxRadius * 0.384
	a := 0.75 * math.Pow(r, 1.3333333333333333)
	b := math.Pow(r, 0.6666666666666666)
	c := 0.3333333333333333 * math.Log(r)
	h := max(scale*(a-b-c), 0)
	return h / 0.384 * maxRadius
}

type largeDrip struct {
	root   pos
	up     bool
	radius int
	blunt  float64
	scale  float64
}

func (l *largeDrip) heightAt(r float32) int {
	return int(dripstoneHeight(float64(r), float64(l.radius), l.scale, l.blunt))
}

type windOffset struct {
	y      int
	on     bool
	vx, vz float64
}

func (w windOffset) offset(p pos) pos {
	if !w.on {
		return p
	}
	dy := float64(w.y - p.y)
	return pos{p.x + floorI(w.vx*dy), p.y, p.z + floorI(w.vz*dy)}
}

func embedded(d *decoration, c pos, radius int) bool {
	if emptyWaterOrLava(d.at(c)) {
		return false
	}
	step := float32(6.0) / float32(radius)
	for a := float32(0); a < float32(math.Pi*2); a += step {
		dx := int(float32(tcos(a) * float32(radius)))
		dz := int(float32(tsin(a) * float32(radius)))
		if emptyWaterOrLava(d.at(c.add(dx, 0, dz))) {
			return false
		}
	}
	return true
}

func (l *largeDrip) settle(d *decoration, w windOffset) bool {
	for l.radius > 1 {
		p := l.root
		tries := min(10, l.heightAt(0))
		for range tries {
			if d.at(p).block() == sLava.block() {
				return false
			}
			if embedded(d, w.offset(p), l.radius) {
				l.root = p
				return true
			}
			if l.up {
				p = p.step(dDown)
			} else {
				p = p.step(dUp)
			}
		}
		l.radius /= 2
	}
	return false
}

func (l *largeDrip) place(d *decoration, w windOffset) {
	r := &d.r
	for dx := -l.radius; dx <= l.radius; dx++ {
		for dz := -l.radius; dz <= l.radius; dz++ {
			dist := float32(math.Sqrt(float64(float32(dx*dx + dz*dz))))
			if dist > float32(l.radius) {
				continue
			}
			h := l.heightAt(dist)
			if h <= 0 {
				continue
			}
			if float64(r.nextFloat()) < 0.2 {
				h = int(float32(float32(h) * r.betweenF(0.8, 1.0)))
			}
			p := l.root.add(dx, 0, dz)
			any := false
			maxY := math.MaxInt
			if l.up {
				maxY = d.height(hmWorldSurfaceWG, p.x, p.z)
			}
			for i := 0; i < h && p.y < maxY; i++ {
				q := w.offset(p)
				if emptyWaterOrLava(d.at(q)) {
					any = true
					d.set(q.x, q.y, q.z, sDripstone)
				} else if any && d.at(q).tagged(tagBaseStone) {
					break
				}
				if l.up {
					p = p.step(dUp)
				} else {
					p = p.step(dDown)
				}
			}
		}
	}
}

func largeDripstone(c *largeCfg) feature {
	return func(d *decoration, o pos) bool {
		if !emptyOrWater(d.at(o)) {
			return false
		}
		col, ok := scanColumn(d, o, c.reach, emptyOrWater, func(s bs) bool { return dripstoneBase(s) || s.block() == sLava.block() })
		ch, hok := col.height()
		if !ok || !hok {
			return false
		}
		if ch < 4 {
			return false
		}
		r := &d.r
		maxR := min(max(int(float32(float32(ch)*c.ratio)), c.radiusLo), c.radiusHi)
		radius := r.between(c.radiusLo, maxR)
		make := func(root pos, up bool, blunt [2]float32) *largeDrip {
			b := float64(r.betweenF(blunt[0], blunt[1]))
			s := float64(r.betweenF(c.scale[0], c.scale[1]))
			return &largeDrip{root: root, up: up, radius: radius, blunt: b, scale: s}
		}
		top := make(pos{o.x, col.ceil - 1, o.z}, false, c.topBlunt)
		bottom := make(pos{o.x, col.floor + 1, o.z}, true, c.bottomBlunt)
		windy := func(l *largeDrip) bool { return l.radius >= c.windRadius && l.blunt >= float64(c.windBlunt) }
		var w windOffset
		if windy(top) && windy(bottom) {
			speed := r.betweenF(c.wind[0], c.wind[1])
			angle := r.betweenF(0, float32(math.Pi))
			w = windOffset{y: o.y, on: true, vx: float64(float32(tcos(angle) * speed)), vz: float64(float32(tsin(angle) * speed))}
		}
		okTop := top.settle(d, w)
		okBottom := bottom.settle(d, w)
		if okTop {
			top.place(d, w)
		}
		if okBottom {
			bottom.place(d, w)
		}
		return true
	}
}

func defineDripstone(t *tableBuilder) {
	cluster := &clusterCfg{reach: 12, height: uniInt(3, 6), radius: uniInt(2, 8), heightDiff: 1, heightDev: 3, thickness: uniInt(2, 4),
		density: [2]float32{0.3, 0.7}, wetMean: 0.1, wetDev: 0.3, wetLo: 0.1, wetHi: 0.9, edgeChance: 0.1, edgeReach: 3, centreReach: 8}
	t.def("dripstone_cluster", dripstoneCluster(cluster), countMod{uniInt(48, 96)}, inSquareMod{}, heightRangeMod{uniHeight(minY, 256)}, biomeMod{})
	airOrWater := func(d *decoration, p pos) bool { s := d.at(p); return s == sAir || s.block() == sWater.block() }
	solid := func(d *decoration, p pos) bool { return d.at(p).has(fSolid) }
	pd := pointedDripstone(0.2, 0.7, 0.5, 0.5)
	floor := newNestedPlacement(pd, scanMod{dDown, solid, airOrWater, 12}, offsetMod{constInt(0), constInt(1)})
	ceiling := newNestedPlacement(pd, scanMod{dUp, solid, airOrWater, 12}, offsetMod{constInt(0), constInt(-1)})
	t.def("pointed_dripstone", simpleSelector(floor, ceiling), countMod{uniInt(192, 256)}, inSquareMod{}, heightRangeMod{uniHeight(minY, 256)},
		countMod{uniInt(1, 5)}, offsetMod{clampedNormal(0, 3, -10, 10), clampedNormal(0, 0.6, -2, 2)}, biomeMod{})
	large := &largeCfg{reach: 30, radiusLo: 3, radiusHi: 19, scale: [2]float32{0.4, 2}, ratio: 0.33, topBlunt: [2]float32{0.3, 0.9}, bottomBlunt: [2]float32{0.4, 1},
		wind: [2]float32{0, 0.3}, windRadius: 4, windBlunt: 0.6}
	t.def("large_dripstone", largeDripstone(large), countMod{uniInt(10, 48)}, inSquareMod{}, heightRangeMod{uniHeight(minY, 256)}, biomeMod{})
}
