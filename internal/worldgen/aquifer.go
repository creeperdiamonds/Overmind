package worldgen

import "math"

// Aquifers decide what fills space the density leaves open. The grid, thresholds and
// rules below come from studying Java 26.1: cells of 16 x 12 x 16, each with one jittered
// centre, of which a block looks at 12 and finds the nearest. Where the study describes a
// term in words rather than numbers (the barrier pressure, the surface sample pattern) the
// exact form is our own.
const (
	lavaLevel    = -54 // The global fluid is lava below this.
	aquiferCellX = 16
	aquiferCellY = 12
	lavaAquiferY = -10 // A random level at or under this can be lava.
)

// fluidStatus is what one aquifer centre holds: a fluid up to a level (blocks under level).
type fluidStatus struct {
	level int32
	lava  bool
}

func (s fluidStatus) kindAt(y int) fillKind {
	switch {
	case y >= int(s.level):
		return fillAir
	case s.lava:
		return fillLava
	}
	return fillWater
}

// globalFluid is lava below Y -54 and the sea up to and including sea level.
func globalFluid(y int) fluidStatus {
	if y < lavaLevel {
		return fluidStatus{level: lavaLevel, lava: true}
	}
	return fluidStatus{level: SeaLevel + 1}
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
	aqGround = 7
)

// aquiferCache keeps centres and surface samples within one chunk or column walk. Every
// value in it is a function of the seed and position, so it changes speed and nothing else.
type aquiferCache struct {
	ready        bool
	baseX, baseZ int // Cell coordinates of index 0.
	chunkX       int
	chunkZ       int
	centres      [aqAcross][aqAcross][aqLayers]aquiferCentre
	grounds      [aqGround][aqGround]float64
	haveGround   [aqGround][aqGround]bool
}

func (a *aquiferCache) reset(x, z int) {
	a.ready = true
	a.baseX, a.baseZ = floorDiv(x-5, aquiferCellX), floorDiv(z-5, aquiferCellX)
	a.chunkX, a.chunkZ = x>>4, z>>4
	a.centres = [aqAcross][aqAcross][aqLayers]aquiferCentre{}
	a.haveGround = [aqGround][aqGround]bool{}
}

func aqLayerIndex(ay int) int { return ay - floorDiv(minY+1, aquiferCellY) + 1 }

func (g *Generator) centreAt(a *aquiferCache, ax, ay, az int) *aquiferCentre {
	c := &a.centres[ax-a.baseX][az-a.baseZ][aqLayerIndex(ay)]
	if c.state&1 == 0 {
		h := posHash(g.aquiferSeed, ax, ay, az)
		c.x = int32(ax*aquiferCellX) + int32(h%10)
		c.y = int32(ay*aquiferCellY) + int32((h>>16)%9)
		c.z = int32(az*aquiferCellX) + int32((h>>32)%10)
		c.state |= 1
	}
	return c
}

// chunkGround is the nominal ground at the middle of a chunk.
func (g *Generator) chunkGround(a *aquiferCache, cx, cz int) float64 {
	i, j := cx-a.chunkX+aqGround/2, cz-a.chunkZ+aqGround/2
	if !a.haveGround[i][j] {
		a.haveGround[i][j] = true
		x, z := cx*16+8, cz*16+8
		qx, qz := g.warped(x, z)
		a.grounds[i][j] = nominalGround(
			clamp(g.climate.continentalness.at(qx, 0, qz), -1.2, 1),
			clamp(g.climate.erosion.at(qx, 0, qz), -1, 1),
			clamp(g.climate.weirdness.at(qx, 0, qz), -1, 1))
	}
	return a.grounds[i][j]
}

// groundSamples are the 13 chunk offsets a centre looks at. The study gives their number;
// this pattern is our own. Offsets stay within 2 chunks so the cache of 7 x 7 holds them,
// given that a centre is at most one chunk from the chunk being built.
var groundSamples = [13][2]int{
	{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}, {2, 0}, {-2, 0}, {0, 2}, {0, -2},
}

// statusOf works out what a centre holds.
func (g *Generator) statusOf(a *aquiferCache, c *aquiferCentre) fluidStatus {
	if c.state&2 != 0 {
		return c.status
	}
	c.state |= 2
	c.status = g.computeStatus(a, int(c.x), int(c.y), int(c.z))
	return c.status
}

func (g *Generator) computeStatus(a *aquiferCache, cx, cy, cz int) fluidStatus {
	global := globalFluid(cy)
	lowest, highest := math.Inf(1), math.Inf(-1)
	seaNear, submerged := false, false
	chunkX, chunkZ := cx>>4, cz>>4
	for _, o := range groundSamples {
		s := g.chunkGround(a, chunkX+o[0], chunkZ+o[1])
		lowest, highest = math.Min(lowest, s), math.Max(highest, s)
		if s < SeaLevel-4 {
			seaNear = true
			// The cell pokes above a surface sample that lies under the sea.
			if float64(cy+aquiferCellY) >= s {
				submerged = true
			}
		}
	}
	if float64(cy) > highest+8 {
		return global // Far above the ground.
	}
	if submerged {
		return globalFluid(SeaLevel)
	}
	// Thresholds: -0.3 and -0.8 just under an ocean, up to 0.8 and 0.4 at 64 or more blocks
	// under the surface, or away from the sea.
	t := 1.0
	if seaNear {
		t = clamp((lowest+8-float64(cy))/64, 0, 1)
	}
	full, part := lerp(-0.3, 0.8, t), lerp(-0.8, 0.4, t)
	fx, fy, fz := float64(cx), float64(cy), float64(cz)
	flood := g.caves.flood.at(fx, fy*0.67, fz)
	if flood > full {
		return global
	}
	if flood <= part {
		return fluidStatus{level: minY - 1}
	}
	// The deep dark zone stays dry: erosion under -0.225 with depth above 0.9.
	if (lowest-fy)/128 > 0.8 {
		sh := g.shapeAt(cx, cz)
		if sh.erosion < -0.225 && (sh.ground-fy)/128 > 0.9 {
			return fluidStatus{level: minY - 1}
		}
	}
	// A random level in a cell of 16 x 40 x 16, capped at the lowest nearby surface.
	ry := floorDiv(cy, 40)
	n := g.caves.spread.at(float64(floorDiv(cx, 16)*16), float64(ry*40)*0.71, float64(floorDiv(cz, 16)*16))
	level := 40*ry + 20 + int(math.Round(n*10/3))*3
	level = min(level, int(lowest))
	st := fluidStatus{level: int32(level)}
	if level <= lavaAquiferY {
		l := g.caves.lavaCell.at(float64(floorDiv(cx, 64)), float64(ry), float64(floorDiv(cz, 64)))
		st.lava = math.Abs(l) > 0.3
	}
	return st
}

// aquiferPressure says how strongly two neighbouring aquifers want a wall between them at
// height y: water against lava always strongly, otherwise by the difference in level and
// how far y lies from their average.
func aquiferPressure(a, b fluidStatus, y int) float64 {
	af, bf := a.kindAt(y) != fillAir, b.kindAt(y) != fillAir
	if !af && !bf {
		return 0
	}
	if (af && a.lava && bf && !b.lava) || (bf && b.lava && af && !a.lava) {
		return 2
	}
	diff := math.Abs(float64(a.level - b.level))
	if diff == 0 {
		return 0
	}
	h := float64(y) + 0.5 - (float64(a.level)+float64(b.level))/2
	o := diff/2 - math.Abs(h)
	if h > 0 {
		if o > 0 {
			return o / 1.5
		}
		return o / 2.5
	}
	if q := 3 + o; q > 0 {
		return q / 3
	}
	return (3 + o) / 10
}

// aquiferAt finds the block's fluid from the nearest centres, or a stone wall between two.
func (g *Generator) aquiferAt(a *aquiferCache, x, y, z int) fillKind {
	if !a.ready {
		a.reset(x, z)
	}
	ax, ay, az := floorDiv(x-5, aquiferCellX), floorDiv(y+1, aquiferCellY), floorDiv(z-5, aquiferCellX)
	var near [3]*aquiferCentre
	dist := [3]int{math.MaxInt, math.MaxInt, math.MaxInt}
	for i := range 2 {
		for j := -1; j <= 1; j++ {
			for k := range 2 {
				c := g.centreAt(a, ax+i, ay+j, az+k)
				dx, dy, dz := int(c.x)-x, int(c.y)-y, int(c.z)-z
				d := dx*dx + dy*dy + dz*dz
				switch {
				case d < dist[0]:
					dist[2], near[2], dist[1], near[1], dist[0], near[0] = dist[1], near[1], dist[0], near[0], d, c
				case d < dist[1]:
					dist[2], near[2], dist[1], near[1] = dist[1], near[1], d, c
				case d < dist[2]:
					dist[2], near[2] = d, c
				}
			}
		}
	}
	s0 := g.statusOf(a, near[0])
	if sim := similarity(dist[0], dist[1]); sim > 0 {
		s1 := g.statusOf(a, near[1])
		if g.barrierAt(sim, aquiferPressure(s0, s1, y), x, y, z) {
			return fillSolid
		}
		if sim2 := similarity(dist[0], dist[2]); sim2 > 0 {
			if g.barrierAt(sim2, aquiferPressure(s0, g.statusOf(a, near[2]), y), x, y, z) {
				return fillSolid
			}
		}
	}
	return s0.kindAt(y)
}

// similarity is near 1 where a block is about as close to two centres, using squared
// distances, and at or under 0 away from the border between them.
func similarity(d1, d2 int) float64 {
	return 1 - float64(d2-d1)/25
}

func (g *Generator) barrierAt(sim, pressure float64, x, y, z int) bool {
	if pressure == 0 {
		return false
	}
	n := g.caves.barrier.at(float64(x), float64(y)*0.5, float64(z))
	return 2*sim*pressure+n > 0
}
