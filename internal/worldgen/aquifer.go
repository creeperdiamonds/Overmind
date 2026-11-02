package worldgen

import "math"

// Aquifers decide what fills space the density leaves open. From the notes (section 5): the
// cell grid, the jitter, the anchor, the 12 candidate centres of which the 4 nearest count, the
// nearest deciding the fluid, the similarity measure, flood thresholds, random levels and lava
// cells. Noise octaves and scales are from addendum A3. Our own design, because the notes
// describe it only in words: when a border between two centres becomes a stone wall (see
// wallStrength), and the 13 surface sample offsets.
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
		a.grounds[i][j] = nominalGround(g.shapeClimate(g.warped(x, z)))
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
	n := g.caves.spread.at(float64(floorDiv(cx, 16)*16), float64(ry*40)*0.714, float64(floorDiv(cz, 16)*16))
	level := 40*ry + 20 + int(math.Round(n*10/3))*3
	level = min(level, int(lowest))
	st := fluidStatus{level: int32(level)}
	if level <= lavaAquiferY {
		l := g.caves.lavaCell.at(float64(floorDiv(cx, 64)), float64(ry), float64(floorDiv(cz, 64)))
		st.lava = math.Abs(l) > 0.3
	}
	return st
}

// Own numbers for walls, measured and tuned against the water and air shares in the notes.
const (
	wallFade      = 4.0  // Blocks past the span between two levels where a wall fades out.
	wallFullGap   = 6.0  // A level difference this large gets the full strength.
	wallThreshold = 0.35 // How strong the border must be, before noise.
	wallRoughness = 0.5  // How much the barrier noise moves that threshold.
)

// wallStrength says from 0 to 1 how much two neighbouring aquifers need stone between them at
// height y. Our own form of what the notes describe: water against lava always counts in
// full. Otherwise a wall is needed in the span where one side holds fluid and the other does
// not, which is the span between their levels, so strength is full across that span, fades
// just past its ends, and grows with the size of the level difference.
func wallStrength(a, b fluidStatus, y int) float64 {
	wetA, wetB := a.kindAt(y) != fillAir, b.kindAt(y) != fillAir
	if !wetA && !wetB {
		return 0
	}
	if wetA && wetB && a.lava != b.lava {
		return 1
	}
	gap := math.Abs(float64(a.level - b.level))
	if gap == 0 {
		return 0
	}
	middle := (float64(a.level) + float64(b.level)) / 2
	past := math.Abs(float64(y)+0.5-middle) - gap/2
	return clamp(gap/wallFullGap, 0, 1) * clamp(1-past/wallFade, 0, 1)
}

// aquiferNear is how many of the 12 candidate centres a block weighs (notes section 5).
const aquiferNear = 4

// aquiferAt finds the block's fluid from the nearest centres, or a stone wall between two.
func (g *Generator) aquiferAt(a *aquiferCache, x, y, z int) fillKind {
	if !a.ready {
		a.reset(x, z)
	}
	ax, ay, az := floorDiv(x-5, aquiferCellX), floorDiv(y+1, aquiferCellY), floorDiv(z-5, aquiferCellX)
	var near [aquiferNear]*aquiferCentre
	var dist [aquiferNear]int
	found := 0
	for i := range 2 {
		for j := -1; j <= 1; j++ {
			for k := range 2 {
				c := g.centreAt(a, ax+i, ay+j, az+k)
				dx, dy, dz := int(c.x)-x, int(c.y)-y, int(c.z)-z
				d := dx*dx + dy*dy + dz*dz
				// Keep the nearest few in order.
				at := found
				for at > 0 && d < dist[at-1] {
					at--
				}
				if at == aquiferNear {
					continue
				}
				found = min(found+1, aquiferNear)
				for m := found - 1; m > at; m-- {
					near[m], dist[m] = near[m-1], dist[m-1]
				}
				near[at], dist[at] = c, d
			}
		}
	}
	s0 := g.statusOf(a, near[0])
	barrier, haveBarrier := 0.0, false
	for i := 1; i < aquiferNear; i++ {
		sim := similarity(dist[0], dist[i])
		if sim <= 0 {
			break
		}
		strength := sim * wallStrength(s0, g.statusOf(a, near[i]), y)
		if strength == 0 {
			continue
		}
		if !haveBarrier {
			barrier, haveBarrier = g.caves.barrier.at(float64(x), float64(y)*0.5, float64(z)), true
		}
		if strength > wallThreshold+wallRoughness*barrier {
			return fillSolid
		}
	}
	return s0.kindAt(y)
}

// similarity is near 1 where a block is about as close to two centres, using squared
// distances, and at or under 0 away from the border between them.
func similarity(d1, d2 int) float64 {
	return 1 - float64(d2-d1)/25
}
