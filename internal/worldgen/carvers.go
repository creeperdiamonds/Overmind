package worldgen

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Carvers are the old-style worm caves and canyons. Chances, height ranges and shape ranges
// come from studying Java 26.1. The walk itself is our own: every tunnel is rolled from its
// source chunk's seed alone, so any chunk it reaches traces the same path, in any order.
const (
	carverReach    = 8    // Source chunks this far away can reach a chunk.
	caveChance     = 0.15 // Per chunk.
	caveExtra      = 0.07 // Per chunk, low caves only.
	canyonChance   = 0.01 // Per chunk.
	caveBottom     = -56
	caveTop        = 180
	caveExtraTop   = 47
	canyonBottom   = 10
	canyonTop      = 67
	carverLavaY    = -56 // Carved space at or under this is lava.
	tunnelSteps    = 112
	canyonMaxSteps = 112
)

func (r *rng) intn(n int) int { return int(r.next() % uint64(n)) }

func (m *carveMask) set(y int) {
	i := y - minY
	m[i>>6] |= 1 << (i & 63)
}

func (m *carveMask) any() bool {
	for _, v := range m {
		if v != 0 {
			return true
		}
	}
	return false
}

// carveSink collects what tunnels cut, either for a chunk or for one column.
type carveSink struct {
	x0, x1, z0, z1 int // Block bounds, inclusive.
	chunk          *[16][16]carveMask
	column         *carveMask
}

func (s *carveSink) mark(x, y, z int) {
	if s.column != nil {
		s.column.set(y)
		return
	}
	s.chunk[x-s.x0][z-s.z0].set(y)
}

// sphere cuts an ellipsoid. floor keeps blocks low in the sphere solid, so caves have flat
// floors; widths, if set, varies the width by height, which gives canyons their ledges.
func (s *carveSink) sphere(cx, cy, cz, rh, rv, floor float64, widths *[(maxY - minY + 1)]float32) {
	if cx+rh < float64(s.x0) || cx-rh > float64(s.x1)+1 || cz+rh < float64(s.z0) || cz-rh > float64(s.z1)+1 {
		return
	}
	x0, x1 := max(int(math.Floor(cx-rh)), s.x0), min(int(math.Ceil(cx+rh)), s.x1)
	z0, z1 := max(int(math.Floor(cz-rh)), s.z0), min(int(math.Ceil(cz+rh)), s.z1)
	y0, y1 := max(int(math.Floor(cy-rv)), minY+1), min(int(math.Ceil(cy+rv)), maxY)
	for x := x0; x <= x1; x++ {
		dx := (float64(x) + 0.5 - cx) / rh
		for z := z0; z <= z1; z++ {
			dz := (float64(z) + 0.5 - cz) / rh
			flat := dx*dx + dz*dz
			if flat >= 1 {
				continue
			}
			for y := y0; y <= y1; y++ {
				dy := (float64(y) + 0.5 - cy) / rv
				if dy <= floor {
					continue
				}
				f := flat
				if widths != nil {
					f *= float64(widths[y-minY])
				}
				if f+dy*dy < 1 {
					s.mark(x, y, z)
				}
			}
		}
	}
}

// walkCarvers cuts every tunnel from chunks within reach of the chunk at cx, cz.
func (g *Generator) walkCarvers(cx, cz int, s *carveSink) {
	for sx := cx - carverReach; sx <= cx+carverReach; sx++ {
		for sz := cz - carverReach; sz <= cz+carverReach; sz++ {
			h := posHash(g.carveSeed, sx, 0, sz)
			// Three separate chances from different bits of one hash.
			if float64(h&0xFFFFF)/(1<<20) < caveChance {
				g.caveStart(sx, sz, h, caveTop, s)
			}
			if float64((h>>20)&0xFFFFF)/(1<<20) < caveExtra {
				g.caveStart(sx, sz, mix64(h), caveExtraTop, s)
			}
			if float64((h>>40)&0xFFFFF)/(1<<20) < canyonChance {
				g.canyonStart(sx, sz, mix64(h+1), s)
			}
		}
	}
}

// caveStart rolls one cave system: a nested random count of starts (up to 15), one in four
// with a room and up to 3 extra tunnels.
func (g *Generator) caveStart(sx, sz int, seed uint64, top int, s *carveSink) {
	r := rng(seed)
	starts := r.intn(r.intn(r.intn(15)+1) + 1)
	for range starts {
		x := float64(sx*16 + r.intn(16))
		y := float64(caveBottom + r.intn(top-caveBottom+1))
		z := float64(sz*16 + r.intn(16))
		hScale := 0.7 + 0.7*r.float()
		vScale := 0.8 + 0.5*r.float()
		floor := -1 + 0.6*r.float()
		yScale := 0.1 + 0.8*r.float()
		tunnels := 1
		if r.intn(4) == 0 {
			room := 1 + 6*r.float()
			rh := (1.5 + room) * hScale
			s.sphere(x, y, z, rh, rh*0.5*vScale, floor, nil)
			tunnels += r.intn(4)
		}
		for range tunnels {
			yaw := r.float() * 2 * math.Pi
			pitch := (r.float() - 0.5) / 4
			thick := r.float()*2 + r.float()
			if r.intn(10) == 0 {
				thick *= r.float()*r.float()*3 + 1
			}
			steps := tunnelSteps - r.intn(tunnelSteps/4)
			tunnel(r.next(), x, y, z, yaw, pitch, thick, hScale, vScale, yScale, floor, 0, steps, s)
		}
	}
}

// tunnel walks a worm from step to steps, cutting a swelling sphere at most steps, and
// splits into two once if thick enough. Its rolls come from its own seed, so it costs the
// same wherever it is asked from.
func tunnel(seed uint64, x, y, z, yaw, pitch, thick, hScale, vScale, yScale, floor float64, step, steps int, s *carveSink) {
	r := rng(seed)
	split := r.intn(steps/2+1) + steps/4
	steep := r.intn(6) == 0
	var dYaw, dPitch float64
	// The widest this tunnel or a branch of it can be, for the reach check below.
	widest := (1.5 + math.Max(thick, 1)) * hScale
	for i := step; i < steps; i++ {
		// Too far to get back within the steps left? Then neither it nor a branch can reach
		// the sink. A branch rolls from its own seed, so stopping changes nothing else.
		if i%8 == 0 {
			left := float64(steps-i) + widest + 1
			if x+left < float64(s.x0) || x-left > float64(s.x1)+1 || z+left < float64(s.z0) || z-left > float64(s.z1)+1 {
				return
			}
		}
		cos := math.Cos(pitch)
		x += math.Cos(yaw) * cos
		y += math.Sin(pitch)
		z += math.Sin(yaw) * cos
		if steep {
			pitch *= 0.92
		} else {
			pitch *= 0.7
		}
		pitch += dPitch * 0.1
		yaw += dYaw * 0.1
		dPitch = dPitch*0.9 + (r.float()-r.float())*r.float()*2
		dYaw = dYaw*0.75 + (r.float()-r.float())*r.float()*4
		if i == split && thick > 1 {
			tunnel(r.next(), x, y, z, yaw-math.Pi/2, pitch/3, r.float()*0.5+0.5, hScale, vScale, yScale, floor, i, steps, s)
			tunnel(r.next(), x, y, z, yaw+math.Pi/2, pitch/3, r.float()*0.5+0.5, hScale, vScale, yScale, floor, i, steps, s)
			return
		}
		if r.intn(4) == 0 {
			continue
		}
		rh := 1.5 + math.Sin(math.Pi*float64(i)/float64(steps))*thick
		s.sphere(x, y, z, rh*hScale, rh*yScale*vScale, floor, nil)
	}
}

// canyonStart rolls one canyon: y scale 3, thickness a trapezoid of 0 to 6 with a plateau
// of 2, length factor 0.75 to 1, pitch within 0.125, and a width that changes every 3 blocks.
func (g *Generator) canyonStart(sx, sz int, seed uint64, s *carveSink) {
	r := rng(seed)
	x := float64(sx*16 + r.intn(16))
	y := float64(canyonBottom + r.intn(canyonTop-canyonBottom+1))
	z := float64(sz*16 + r.intn(16))
	yaw := r.float() * 2 * math.Pi
	pitch := (r.float() - 0.5) * 0.25
	thick := trapezoid(&r, 0, 6, 2)
	steps := int(canyonMaxSteps * (0.75 + 0.25*r.float()))
	var widths [maxY - minY + 1]float32
	var w float32 = 1
	for i := range widths {
		if i%3 == 0 {
			f := r.float()
			w = float32(1 + f*f)
		}
		widths[i] = w
	}
	var dYaw, dPitch float64
	for i := range steps {
		rh := 1.5 + math.Sin(math.Pi*float64(i)/float64(steps))*thick
		rv := rh * 3
		cos := math.Cos(pitch)
		x += math.Cos(yaw) * cos
		y += math.Sin(pitch)
		z += math.Sin(yaw) * cos
		pitch *= 0.7
		pitch += dPitch * 0.05
		yaw += dYaw * 0.05
		dPitch = dPitch*0.8 + (r.float()-r.float())*r.float()*2
		dYaw = dYaw*0.5 + (r.float()-r.float())*r.float()*4
		if r.intn(4) == 0 {
			continue
		}
		s.sphere(x, y, z, rh, rv, -2, &widths)
	}
}

// trapezoid draws from low to high with a flat middle of the given width.
func trapezoid(r *rng, low, high, plateau float64) float64 {
	span := high - low
	slope := (span - plateau) / 2
	return low + r.float()*(span-slope) + r.float()*slope
}

// carveFill says what a carved block becomes. A wall in the aquifer leaves the block alone.
func (g *Generator) carveFill(a *aquiferCache, x, y, z int, ground float64) fillKind {
	if y <= carverLavaY {
		return fillLava
	}
	return g.fillAt(a, x, y, z, ground, false)
}

// applyCarvers cuts the tunnels into a chunk. noiseTop holds each column's highest noise
// block: icebergs and hoodoos above it are not carved. Removing grass turns the dirt below
// into grass, so the floor of an opened cave has a surface.
func (g *Generator) applyCarvers(c *chunk.Chunk, s *chunkScratch, originX, originZ int, noiseTop *[16][16]int16, cols *[4][4]biomeColumn) {
	s.carved = [16][16]carveMask{}
	sink := carveSink{x0: originX, x1: originX + 15, z0: originZ, z1: originZ + 15, chunk: &s.carved}
	g.walkCarvers(originX>>4, originZ>>4, &sink)
	b := &g.blocks
	for lx := range 16 {
		for lz := range 16 {
			m := &s.carved[lx][lz]
			if !m.any() {
				continue
			}
			i, j := lx/cellXZ, lz/cellXZ
			tx, tz := float64(lx-i*cellXZ)/cellXZ, float64(lz-j*cellXZ)/cellXZ
			ground := bilerp(s.shapes[i][j].ground, s.shapes[i+1][j].ground, s.shapes[i][j+1].ground, s.shapes[i+1][j+1].ground, tx, tz)
			wx, wz := originX+lx, originZ+lz
			x, z := uint8(lx), uint8(lz)
			lost := uint32(0) // A grass-like top block removed just above.
			for y := int(noiseTop[lx][lz]); y > minY; y-- {
				r := c.Block(x, int16(y), z, 0)
				if !m.has(y) {
					if lost != 0 && r == b.dirt {
						c.SetBlock(x, int16(y), z, 0, lost)
					}
					lost = 0
					continue
				}
				if r == b.air || r == b.water || r == b.lava || r == b.bedrock {
					lost = 0
					continue
				}
				f := g.carveFill(&s.aq, wx, y, wz, ground)
				if f == fillSolid || (f == fillAir && c.Block(x, int16(y+1), z, 0) == b.water) {
					lost = 0
					continue
				}
				if r == b.grass || r == b.mycelium {
					lost = r
				} else if r != b.dirt {
					lost = 0
				}
				switch f {
				case fillAir:
					c.SetBlock(x, int16(y), z, 0, b.air)
				case fillWater:
					c.SetBlock(x, int16(y), z, 0, b.water)
				case fillLava:
					c.SetBlock(x, int16(y), z, 0, b.lava)
				}
			}
		}
	}
}

// lake is a lava lake: a flattened ellipsoid, lava under its level and air above.
type lake struct {
	x, y, z, rx, ry, rz float64
	level               int
}

const (
	lakeNone = iota
	lakeAir
	lakeLava
)

// Lake rates from the study: underground 1 chunk in 9, surface 1 in 200. Sizes and the
// height range are our own.
const (
	lakeUnderChance   = 1.0 / 9
	lakeSurfaceChance = 1.0 / 200
	lakeUnderBottom   = -58
	lakeUnderTop      = 60
	lakeMarginAbove   = 16 // Underground lakes keep this far under the nominal ground.
)

// lakesNear lists the lakes that can reach the chunk at cx, cz, into buf.
func (g *Generator) lakesNear(cx, cz int, buf []lake) []lake {
	buf = buf[:0]
	for sx := cx - 1; sx <= cx+1; sx++ {
		for sz := cz - 1; sz <= cz+1; sz++ {
			h := posHash(g.lakeSeed, sx, 0, sz)
			r := rng(h)
			under, surface := float64(h&0xFFFFF)/(1<<20) < lakeUnderChance, float64((h>>20)&0xFFFFF)/(1<<20) < lakeSurfaceChance
			if !under && !surface {
				continue
			}
			x, z := sx*16+r.intn(16), sz*16+r.intn(16)
			l := lake{x: float64(x) + 0.5, z: float64(z) + 0.5, rx: 3 + 4*r.float(), ry: 2 + r.float(), rz: 3 + 4*r.float()}
			y := lakeUnderBottom + r.intn(lakeUnderTop-lakeUnderBottom+1)
			if under {
				cl := g.Climate(x, z)
				if float64(y)+lakeMarginAbove >= nominalGround(cl.Continentalness, cl.Erosion, cl.Weirdness) {
					under = false
				}
			}
			if !under {
				if !surface {
					continue
				}
				y = g.noiseHeight(x, z)
				if y <= SeaLevel+1 {
					continue
				}
			}
			l.y, l.level = float64(y)+0.5, y
			buf = append(buf, l)
		}
	}
	return buf
}

// lakeAt says what the lakes make of a solid block. The first lake that covers it decides.
func lakeAt(lakes []lake, x, y, z int) int {
	for i := range lakes {
		l := &lakes[i]
		dx, dy, dz := (float64(x)+0.5-l.x)/l.rx, (float64(y)+0.5-l.y)/l.ry, (float64(z)+0.5-l.z)/l.rz
		if dx*dx+dy*dy+dz*dz < 1 {
			if y >= l.level {
				return lakeAir
			}
			return lakeLava
		}
	}
	return lakeNone
}

// applyLakes writes lakes into a chunk, top down so a block can see the one above.
func (g *Generator) applyLakes(c *chunk.Chunk, s *chunkScratch, originX, originZ, highest int) {
	s.lakes = g.lakesNear(originX>>4, originZ>>4, s.lakes)
	if len(s.lakes) == 0 {
		return
	}
	b := &g.blocks
	for lx := range 16 {
		for lz := range 16 {
			x, z := uint8(lx), uint8(lz)
			for y := highest; y > minY; y-- {
				k := lakeAt(s.lakes, originX+lx, y, originZ+lz)
				if k == lakeNone {
					continue
				}
				r := c.Block(x, int16(y), z, 0)
				if r == b.air || r == b.water || r == b.lava || r == b.bedrock {
					continue
				}
				if k == lakeLava {
					c.SetBlock(x, int16(y), z, 0, b.lava)
				} else if y == maxY || c.Block(x, int16(y+1), z, 0) != b.water {
					c.SetBlock(x, int16(y), z, 0, b.air)
				}
			}
		}
	}
}

// SurfaceHeight returns the Y of the highest solid block in a column, with caves, carvers
// and lakes taken into account: the same answer as the generated chunk, without needing the
// chunk to exist. Where a cave opens the ground this is lower than terrainHeight. It walks
// every tunnel that could reach the column, so it costs far more than noiseHeight.
func (g *Generator) SurfaceHeight(x, z int) int {
	var mask carveMask
	sink := carveSink{x0: x, x1: x, z0: z, z1: z, column: &mask}
	g.walkCarvers(x>>4, z>>4, &sink)
	var buf [18]lake
	return g.newColumnWalk(x, z).top(&mask, g.lakesNear(x>>4, z>>4, buf[:0]))
}
