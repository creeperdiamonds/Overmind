package worldgen

import "math"

// Aquifers decide what fills space the density leaves open. This follows Mojang's algorithm exactly,
// per the owner's decision, in our own Go from the study's notes (NOTES-exact-seed.md sections 9 and
// 10): the preliminary surface, the candidate centres, the status of a centre, and the pressure that
// raises stone walls between centres.
const (
	lavaLevel    = -54 // The global fluid is lava below this.
	seaFluidTop  = SeaLevel
	aquiferCellX = 16
	aquiferCellY = 12
	dryLevel     = -32512 // The level of a centre that holds no fluid.
)

// fluidStatus is what one aquifer centre holds: a fluid up to a level (blocks under level).
type fluidStatus struct {
	level int32
	kind  fillKind
}

func (s fluidStatus) at(y int) fillKind {
	if y < int(s.level) {
		return s.kind
	}
	return fillAir
}

// globalFluid is lava below Y -54 and the sea under Y 63.
func globalFluid(y int) fluidStatus {
	if y < lavaLevel {
		return fluidStatus{level: lavaLevel, kind: fillLava}
	}
	return fluidStatus{level: seaFluidTop, kind: fillWater}
}

// prelimCache remembers preliminary surface levels by quart, for one generator. A level is a pure
// function of the seed and the position, so an entry says nothing about any chunk and neighbouring
// chunks share most of theirs; it stays in pooled scratch only, and changes speed and nothing else.
type prelimCache struct {
	owner   *Generator
	entries [prelimSlots]struct {
		qx, qz int32
		level  int32 // Plus one, so zero means empty.
	}
}

const prelimSlots = 1 << 11

// use empties the cache if it last served another generator.
func (c *prelimCache) use(g *Generator) {
	if c.owner != g {
		c.owner = g
		c.entries = [prelimSlots]struct{ qx, qz, level int32 }{}
	}
}

// preliminarySurface is the highest cell corner likely to be ground, at a quart. It snaps its input.
func (g *Generator) preliminarySurface(c *prelimCache, bx, bz int) int {
	qx, qz := int32(bx>>2), int32(bz>>2)
	e := &c.entries[(uint32(qx)*31+uint32(qz))&(prelimSlots-1)]
	if e.level != 0 && e.qx == qx && e.qz == qz {
		return int(e.level - 1)
	}
	level := preliminaryLevel(g.surfaceShape(int(qx)*4, int(qz)*4))
	// A level is a multiple of 8, so plus one it is never zero.
	e.qx, e.qz, e.level = qx, qz, int32(level)+1
	return level
}

func preliminaryLevel(s columnShape) int {
	ub := float64(float64(float64(0.2734375*(1.0/s.factor))+float64(-1.0*s.offset))*-128.0) + 128.0
	ub = clamp(ub, -40, 320)
	top := int(math.Floor(ub/8.0)) * 8
	if top <= -64 {
		return -64
	}
	for y := top; y >= -64; y -= 8 {
		d := clamp(float64(4.0*quarterNeg(float64(depthAt(y, s.offset)*s.factor)))+(-0.703125), -64, 64)
		if slides(d, y)+(-0.390625) > 0 {
			return y
		}
	}
	return -64
}

// aquiferCentre is one cached centre.
type aquiferCentre struct {
	x, y, z int32
	status  fluidStatus
	state   uint8 // Bit 0: position worked out. Bit 1: status worked out.
}

const (
	aqAcross = 3
	aqLayers = (maxY-minY+1)/aquiferCellY + 4
)

// aquiferCache keeps centres and the skip height of one chunk. Every value in it is a function of the
// seed and the chunk, so it changes speed and nothing else.
type aquiferCache struct {
	prelims      *prelimCache
	baseX, baseZ int // Cell coordinates of index 0.
	chunkX       int
	chunkZ       int
	skip         int
	haveSkip     bool
	centres      [aqAcross][aqAcross][aqLayers]aquiferCentre
	// The candidates of the block last asked about, in the game's order. Blocks close together share them.
	candX, candY, candZ int
	haveCand            bool
	cand                [12]aquiferCandidate
}

type aquiferCandidate struct {
	x, y, z int32
	c       *aquiferCentre
}

// reset points the cache at the chunk holding a block.
func (a *aquiferCache) reset(x, z int, prelims *prelimCache) {
	a.prelims = prelims
	a.chunkX, a.chunkZ = x>>4, z>>4
	a.baseX, a.baseZ = (a.chunkX<<4-5)>>4, (a.chunkZ<<4-5)>>4
	a.haveSkip = false
	a.haveCand = false
	a.centres = [aqAcross][aqAcross][aqLayers]aquiferCentre{}
}

func aqLayerIndex(ay int) int { return ay - floorDiv(minY+1, aquiferCellY) + 1 }

// skipAbove is the height over which a chunk takes the global fluid without asking its centres.
func (g *Generator) skipAbove(a *aquiferCache) int {
	if a.haveSkip {
		return a.skip
	}
	x0, z0 := a.chunkX<<4, a.chunkZ<<4
	minGX, maxGX := (x0-5)>>4, ((x0+15-5)>>4)+1
	minGZ, maxGZ := (z0-5)>>4, ((z0+15-5)>>4)+1
	m := math.MinInt
	for q := minGX * 16; q <= maxGX*16+9; q += 4 {
		for r := minGZ * 16; r <= maxGZ*16+9; r += 4 {
			m = max(m, g.preliminarySurface(a.prelims, q, r))
		}
	}
	grid := floorDiv(m+8+12, 12) + 1
	a.skip, a.haveSkip = grid*12+11-1, true
	return a.skip
}

func (g *Generator) centreAt(a *aquiferCache, gx, gy, gz int) *aquiferCentre {
	c := &a.centres[gx-a.baseX][gz-a.baseZ][aqLayerIndex(gy)]
	if c.state&1 == 0 {
		r := g.aquiferFac.at(int32(gx), int32(gy), int32(gz))
		c.x = int32(gx*16 + r.nextInt(10))
		c.y = int32(gy*12 + r.nextInt(9))
		c.z = int32(gz*16 + r.nextInt(10))
		c.state |= 1
	}
	return c
}

// statusOf works out what a centre holds.
func (g *Generator) statusOf(a *aquiferCache, c *aquiferCentre) fluidStatus {
	if c.state&2 == 0 {
		c.state |= 2
		c.status = g.computeStatus(a, int(c.x), int(c.y), int(c.z))
	}
	return c.status
}

// statusSamples are the chunk offsets a centre looks at, in the game's order.
var statusSamples = [13][2]int{
	{0, 0}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {-3, 0}, {-2, 0}, {-1, 0}, {1, 0}, {-2, 1}, {-1, 1}, {0, 1}, {1, 1},
}

func (g *Generator) computeStatus(a *aquiferCache, x, y, z int) fluidStatus {
	global := globalFluid(y)
	lowest := math.MaxInt
	top, bottom := y+12, y-12
	centreUnder := false
	for _, o := range statusSamples {
		ps := g.preliminarySurface(a.prelims, x+o[0]*16, z+o[1]*16)
		adj := ps + 8
		start := o[0] == 0 && o[1] == 0
		if start && bottom > adj {
			return global
		}
		poke := top > adj
		if poke || start {
			if s := globalFluid(adj); s.at(adj) != fillAir {
				if start {
					centreUnder = true
				}
				if poke {
					return s
				}
			}
		}
		lowest = min(lowest, ps)
	}

	full, part := -1.0, -1.0
	if !g.inDeepDark(a, x, y, z) {
		ff := 0.0
		if centreUnder {
			ff = clampedMapUnit(float64(lowest + 8 - y))
		}
		n := clamp(g.caves.flood.at(float64(x), float64(float64(y)*0.67), float64(z)), -1, 1)
		part, full = n-remap(ff, 1, 0, -0.8, 0.4), n-remap(ff, 1, 0, -0.3, 0.8)
	}

	level := dryLevel
	switch {
	case full > 0:
		level = int(global.level)
	case part > 0:
		cy := floorDiv(y, 40)
		spread := float64(g.caves.spread.at(float64(floorDiv(x, 16)), float64(float64(cy)*0.7142857142857143), float64(floorDiv(z, 16))) * 10.0)
		q := int(math.Floor(spread/3.0)) * 3
		level = min(lowest, cy*40+20+q)
	}

	kind := global.kind
	if level <= -10 && level != dryLevel && kind != fillLava {
		n := g.caves.lavaCell.at(float64(floorDiv(x, 64)), float64(floorDiv(y, 40)), float64(floorDiv(z, 64)))
		if math.Abs(n) > 0.3 {
			kind = fillLava
		}
	}
	return fluidStatus{level: int32(level), kind: kind}
}

// clampedMapUnit maps a distance from 0..64 onto 1..0, held at the ends.
func clampedMapUnit(dist float64) float64 {
	t := dist / 64
	if t < 0 {
		return 1
	}
	if t > 1 {
		return 0
	}
	return lerp(1, 0, t)
}

// remap is the unclamped map of v from a..b onto c..d.
func remap(v, a, b, c, d float64) float64 { return lerp(c, d, (v-a)/(b-a)) }

// inDeepDark is the dry zone of a centre: low erosion, deep down. Inside a chunk's own noise setup the
// game caches erosion and offset by quart, and the cache reaches 5 quarts from the chunk's first;
// outside that it reads the exact column.
func (g *Generator) inDeepDark(a *aquiferCache, x, y, z int) bool {
	qx, qz := x>>2, z>>2
	sx, sz := x, z
	if q0x, q0z := a.chunkX<<2, a.chunkZ<<2; qx >= q0x && qx <= q0x+4 && qz >= q0z && qz <= q0z+4 {
		sx, sz = qx*4, qz*4
	}
	s := g.surfaceShape(sx, sz)
	return s.erosion < float64(float32(-0.225)) && depthAt(y, s.offset) > float64(float32(0.9))
}

// fluidAt says what a block with a density of at most zero becomes. final is its density: it can turn
// the block to stone at a border between centres.
func (g *Generator) fluidAt(a *aquiferCache, x, y, z int, final float64) fillKind {
	global := globalFluid(y)
	if y > g.skipAbove(a) {
		return global.at(y)
	}
	if global.at(y) == fillLava {
		return fillLava
	}

	ax, ay, az := (x-5)>>4, floorDiv(y+1, 12), (z-5)>>4
	if !a.haveCand || a.candX != ax || a.candY != ay || a.candZ != az {
		a.candX, a.candY, a.candZ, a.haveCand = ax, ay, az, true
		i := 0
		for gx := ax; gx <= ax+1; gx++ {
			for gy := ay - 1; gy <= ay+1; gy++ {
				for gz := az; gz <= az+1; gz++ {
					c := g.centreAt(a, gx, gy, gz)
					a.cand[i] = aquiferCandidate{c.x, c.y, c.z, c}
					i++
				}
			}
		}
	}
	var near [3]*aquiferCentre
	dist := [3]int{math.MaxInt32, math.MaxInt32, math.MaxInt32}
	for i := range a.cand {
		c := &a.cand[i]
		dx, dy, dz := int(c.x)-x, int(c.y)-y, int(c.z)-z
		d := dx*dx + dy*dy + dz*dz
		// A later candidate wins a tie.
		switch {
		case dist[0] >= d:
			near[2], dist[2] = near[1], dist[1]
			near[1], dist[1] = near[0], dist[0]
			near[0], dist[0] = c.c, d
		case dist[1] >= d:
			near[2], dist[2] = near[1], dist[1]
			near[1], dist[1] = c.c, d
		case dist[2] >= d:
			near[2], dist[2] = c.c, d
		}
	}

	s1 := g.statusOf(a, near[0])
	s12 := similarity(dist[0], dist[1])
	fluid := s1.at(y)
	if s12 <= 0 {
		return fluid
	}
	if fluid == fillWater {
		if below := globalFluid(y - 1); below.at(y-1) == fillLava {
			return fluid
		}
	}
	s2 := g.statusOf(a, near[1])
	if final+float64(s12*g.pressure(s1, s2, x, y, z)) > 0 {
		return fillSolid
	}
	s3 := g.statusOf(a, near[2])
	if s13 := similarity(dist[0], dist[2]); s13 > 0 && final+float64(float64(s12*s13)*g.pressure(s1, s3, x, y, z)) > 0 {
		return fillSolid
	}
	if s23 := similarity(dist[1], dist[2]); s23 > 0 && final+float64(float64(s12*s23)*g.pressure(s2, s3, x, y, z)) > 0 {
		return fillSolid
	}
	return fluid
}

// pressure is how strongly two centres want stone between them at a block.
func (g *Generator) pressure(a, b fluidStatus, x, y, z int) float64 {
	ta, tb := a.at(y), b.at(y)
	if (ta == fillLava && tb == fillWater) || (ta == fillWater && tb == fillLava) {
		return 2.0
	}
	diff := a.level - b.level
	if diff < 0 {
		diff = -diff
	}
	if diff == 0 {
		return 0.0
	}
	avg := float64(0.5 * float64(a.level+b.level))
	h := float64(y) + 0.5 - avg
	edge := float64(diff)/2.0 - math.Abs(h)
	var gap float64
	if h > 0 {
		c := 0.0 + edge
		if c > 0 {
			gap = c / 1.5
		} else {
			gap = c / 2.5
		}
	} else {
		c := 3.0 + edge
		if c > 0 {
			gap = c / 3.0
		} else {
			gap = c / 10.0
		}
	}
	n := 0.0
	if gap >= -2 && gap <= 2 {
		n = g.caves.barrier.at(float64(x), float64(float64(y)*0.5), float64(z))
	}
	return float64(2.0 * (n + gap))
}

// similarity is near 1 where a block is about as close to two centres, using squared distances, and
// at or under 0 away from the border between them.
func similarity(d1, d2 int) float64 {
	return 1.0 - float64(d2-d1)/25.0
}
