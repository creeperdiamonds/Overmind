package worldgen

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Carvers are worm caves and canyons. From the notes (section 8): chances per chunk, height
// ranges, reach, the ranges of the shape numbers, rooms, and the behaviour around water, lava
// and grass. The way a tunnel is laid out is our own design, tuned by us: a tunnel is a curve
// written as a formula (see tunnelPath), not a walk, and it has no step-by-step state. The
// notes give no measured output for carvers (addendum A6). The air in full chunks is tuned to
// Bedrock's, measured with carvers on 4 seeds (TestCaveAirMatchesBedrock).
const (
	oldCarverReach = 8    // Source chunks this far away can reach a chunk (notes).
	caveChance     = 0.15 // Per chunk (notes).
	caveExtra      = 0.07 // Per chunk, low caves only (notes).
	canyonChance   = 0.01 // Per chunk (notes).
	caveBottom     = -56
	caveTop        = 180
	caveExtraTop   = 47
	canyonBottom   = 10
	canyonTop      = 67
)

// Own numbers for the size of a tunnel. A tunnel runs both ways from its start, so its full
// length can be about 14 chunks, near the notes' "up to about 15". What matters is the sum:
// the longest half, plus the most the bends can move it, plus the widest radius, is how far
// any cut can be from the start, and carverReachBlocks says that fits within carverReach.
const (
	tunnelHalfMax  = 104.0
	tunnelBendMax  = 8.0 // The two sideways waves together, each way.
	tunnelWidthMax = 8.0 // Radius in blocks at its widest.
	canyonBendMax  = 6.0
	canyonWidthMax = 10.0
	// A source chunk's nearest block outside carverReach is carverReach*16 + 1 blocks away.
	carverReachBlocks = oldCarverReach * 16
)

// These fail to compile if a tunnel's size could overrun the reach.
const (
	_ = uint(carverReachBlocks - (tunnelHalfMax + 2*tunnelBendMax + tunnelWidthMax))
	_ = uint(carverReachBlocks - (tunnelHalfMax + 2*canyonBendMax + canyonWidthMax))
)

func (r *rng) intn(n int) int { return int(r.next() % uint64(n)) }

func (m *carveMask) any() bool {
	for _, v := range m {
		if v != 0 {
			return true
		}
	}
	return false
}

// carveSink collects what tunnels cut, either for a chunk or for one column.
type oldCarveSink struct {
	x0, x1, z0, z1 int // Block bounds, inclusive.
	chunk          *[carvedSize][carvedSize]carveMask
	column         *carveMask
}

func (s *oldCarveSink) mark(x, y, z int) {
	if s.column != nil {
		s.column.set(y)
		return
	}
	s.chunk[x-s.x0][z-s.z0].set(y)
}

// carveAround is the reach of walkCarvers for a chunk: one chunk more than carverReach, because the
// columns around the chunk that trees ask about (treeMargin of them) may be in the next chunk over.
const carveAround = oldCarverReach + 1

// sphere cuts an ellipsoid. floor keeps blocks low in the sphere solid, so caves have flat
// floors; walls, if not zero, seeds a canyon's wall ledges.
func (s *oldCarveSink) sphere(cx, cy, cz, rh, rv, floor float64, walls uint64) {
	if cx+rh < float64(s.x0) || cx-rh > float64(s.x1)+1 || cz+rh < float64(s.z0) || cz-rh > float64(s.z1)+1 {
		return
	}
	x0, x1 := max(int(math.Floor(cx-rh)), s.x0), min(int(math.Ceil(cx+rh)), s.x1)
	z0, z1 := max(int(math.Floor(cz-rh)), s.z0), min(int(math.Ceil(cz+rh)), s.z1)
	y0, y1 := max(int(math.Floor(cy-rv)), minY+1), min(int(math.Ceil(cy+rv)), maxY)
	for y := y0; y <= y1; y++ {
		dy := (float64(y) + 0.5 - cy) / rv
		if dy <= floor {
			continue
		}
		reach := 1 - dy*dy
		if reach <= 0 {
			continue
		}
		if walls != 0 {
			reach *= canyonLedge(walls, y)
		}
		for x := x0; x <= x1; x++ {
			dx := (float64(x) + 0.5 - cx) / rh
			for z := z0; z <= z1; z++ {
				dz := (float64(z) + 0.5 - cz) / rh
				if dx*dx+dz*dz < reach {
					s.mark(x, y, z)
				}
			}
		}
	}
}

// canyonLedge narrows a canyon's walls by a step that changes every 3 blocks of height (the
// notes' width smoothness of 3). The size of the step is our own.
func canyonLedge(walls uint64, y int) float64 {
	return 0.55 + 0.45*unit(posHash(walls, 0, floorDiv(y, 3), 0))
}

// walkCarvers cuts every tunnel from chunks within reach of the chunk at cx, cz.
func (g *Generator) oldWalkCarvers(cx, cz, reach int, s *oldCarveSink) {
	for sx := cx - reach; sx <= cx+reach; sx++ {
		for sz := cz - reach; sz <= cz+reach; sz++ {
			h := posHash(g.carveSeed, sx, 0, sz)
			// Three separate chances from different bits of one hash.
			if float64(h&0xFFFFF)/(1<<20) < caveChance {
				caveStart(sx, sz, h, caveTop, s)
			}
			if float64((h>>20)&0xFFFFF)/(1<<20) < caveExtra {
				caveStart(sx, sz, mix64(h), caveExtraTop, s)
			}
			if float64((h>>40)&0xFFFFF)/(1<<20) < canyonChance {
				canyonStart(sx, sz, mix64(h+1), s)
			}
		}
	}
}

// caveStart rolls one cave system. From the notes: a count of starts with a cap of 15; one in
// four with a room of thickness 1 to 7 and up to 3 extra tunnels; the ranges of the radius,
// vertical, floor and y scales. The count's distribution (startScale), and the room's height,
// are our own.
func caveStart(sx, sz int, seed uint64, top int, s *oldCarveSink) {
	r := rng(seed)
	starts := min(int(-math.Log(1-r.float())*startScale), maxStarts)
	for range starts {
		x := float64(sx*16 + r.intn(16))
		y := float64(caveBottom) + math.Floor(r.float()*float64(top-caveBottom+1))
		z := float64(sz*16 + r.intn(16))
		hScale := 0.7 + 0.7*r.float()
		vScale := 0.8 + 0.5*r.float()
		floor := -1 + 0.6*r.float()
		yScale := 0.1 + 0.8*r.float()
		tunnels := 1
		if r.intn(4) == 0 {
			rh := (1 + 6*r.float()) * hScale
			s.sphere(x, y, z, rh, max(rh*(0.3+0.3*yScale)*vScale, minTunnelHeight), floor, 0)
			tunnels += r.intn(4)
		}
		for range tunnels {
			p := newTunnelPath(r.next(), x, y, z, tunnelHalfMax, tunnelBendMax, caveTilt)
			radius := tunnelRadius(r.float()) * hScale
			p.cut(s, radius, radius*yScale*vScale, floor, 0)
		}
	}
}

// Our own start count: the floor of an exponential draw, so most systems have one or two starts
// and a few have many, capped at the notes' 15. Start heights are even over the notes' range. Both
// were tuned by us to Bedrock's air by band (TestCaveAirMatchesBedrock); an earlier lean to high
// starts is gone, because its cause was the old noise caves, and the noise layer is now exact.
const (
	maxStarts  = 15
	startScale = 4
)

// Own numbers: how far a cave tunnel tilts, its lowest ceiling, and its radius at the middle.
const (
	caveTilt        = 0.3
	minTunnelHeight = 1.6
)

// tunnelRadius turns an even draw into a tunnel's radius before the notes' scales apply. Most
// tunnels are narrow and few are wide. At most 5.4, so at most 7.56 after the largest scale of
// 1.4, and at most 7.94 with the lumps in cut, under tunnelWidthMax. Own.
func tunnelRadius(u float64) float64 { return 1 + 4.4*u*u*u*u }

// tunnelPath is a curve through a start point, written as a formula of t from -1 to 1: a
// straight line, bent sideways by two sine waves and up and down by a third. Every number is
// drawn when the path is made, so it is the same whichever chunk asks, and nothing is left to
// chance along the way.
type tunnelPath struct {
	x, y, z, axisX, axisZ, half, tilt float64
	wave                              [2]pathWave // Sideways.
	lift                              pathWave    // Up and down.
	lump                              pathWave    // Changes in the radius.
	bend                              float64     // The sideways waves' amplitudes added up.
}

// pathWave is one sine wave of t, shifted to be zero at the start, t = 0.
type pathWave struct{ amp, freq, phase float64 }

func (w *pathWave) at(t float64) float64 {
	return w.amp * (math.Sin(w.freq*t+w.phase) - math.Sin(w.phase))
}

func newTunnelPath(seed uint64, x, y, z, halfMax, bendMax, tilt float64) tunnelPath {
	r := rng(seed)
	yaw := r.float() * 2 * math.Pi
	wave := func(amp, freqLow, freqSpan float64) pathWave {
		return pathWave{amp: amp, freq: math.Pi * (freqLow + freqSpan*r.float()), phase: r.float() * 2 * math.Pi}
	}
	first := bendMax * (0.25 + 0.4*r.float())
	second := (bendMax - first) * r.float()
	p := tunnelPath{
		x: x, y: y, z: z, axisX: math.Cos(yaw), axisZ: math.Sin(yaw),
		half: halfMax * (0.1 + 0.9*cubed(r.float())),
		tilt: (2*r.float() - 1) * tilt,
		bend: first + second,
	}
	p.wave[0], p.wave[1] = wave(first, 0.8, 1.2), wave(second, 2.5, 3)
	p.lift = wave(1+5*r.float(), 0.7, 1.6)
	p.lump = wave(1, 3, 5)
	return p
}

func cubed(v float64) float64 { return float64(float64(v*v) * v) }

// at gives the point on the path at t.
func (p *tunnelPath) at(t float64) (x, y, z float64) {
	along := t * p.half
	side := p.wave[0].at(t) + p.wave[1].at(t)
	return p.x + p.axisX*along - p.axisZ*side,
		p.y + p.tilt*along + p.lift.at(t),
		p.z + p.axisZ*along + p.axisX*side
}

// alongRange gives the span of the path's axis, from its start, that can touch the sink.
func (p *tunnelPath) alongRange(s *oldCarveSink, slack float64) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, x := range [2]int{s.x0, s.x1 + 1} {
		for _, z := range [2]int{s.z0, s.z1 + 1} {
			d := (float64(x)-p.x)*p.axisX + (float64(z)-p.z)*p.axisZ
			lo, hi = math.Min(lo, d), math.Max(hi, d)
		}
	}
	return lo - slack, hi + slack
}

// cut carves the path as a chain of ellipsoids, about a block apart. The radius tapers to the
// ends and varies a little along the way, by at most 1/20 of the middle radius each way.
func (p *tunnelPath) cut(s *oldCarveSink, radius, height, floor float64, walls uint64) {
	margin := p.half + 2*p.bend + radius*1.1 + 1
	if p.x+margin < float64(s.x0) || p.x-margin > float64(s.x1)+1 || p.z+margin < float64(s.z0) || p.z-margin > float64(s.z1)+1 {
		return
	}
	steps := int(2 * p.half)
	// A cut lies within margin - half of its place along the axis, so only that part of the
	// path can reach the sink. Skipping the rest changes nothing it cuts.
	lo, hi := p.alongRange(s, margin-p.half)
	for i := max(int(math.Floor((lo/p.half+1)/2*float64(steps))), 0); i <= min(int(math.Ceil((hi/p.half+1)/2*float64(steps))), steps); i++ {
		t := 2*float64(i)/float64(steps) - 1
		taper := 1 - t*t*t*t
		size := taper * (1 + p.lump.at(t)/40)
		rh := radius * size
		if rh < 0.7 {
			continue
		}
		x, y, z := p.at(t)
		s.sphere(x, y, z, rh, max(height*size, minTunnelHeight), floor, walls)
	}
}

// canyonStart rolls one canyon. From the notes: y scale 3, thickness 0 to 6
// (the notes say a trapezoid with a plateau of 2; ours is even), distance factor 0.75 to 1,
// vertical rotation within 0.125. How thickness becomes a radius is ours.
func canyonStart(sx, sz int, seed uint64, s *oldCarveSink) {
	r := rng(seed)
	x := float64(sx*16 + r.intn(16))
	y := float64(canyonBottom + r.intn(canyonTop-canyonBottom+1))
	z := float64(sz*16 + r.intn(16))
	thick := 6 * r.float() // Own: even over the notes' range of 0 to 6.
	halfMax := tunnelHalfMax * (0.75 + 0.25*r.float())
	p := newTunnelPath(r.next(), x, y, z, halfMax, canyonBendMax, 0.125)
	p.half = halfMax // The notes' factor alone sets a canyon's length.
	radius := (canyonCore + thick) * canyonMiddleWidth
	p.cut(s, radius, radius*3, -2, r.next()|1)
}

// A canyon's radius at the middle, from the notes' thickness. At thickness 6 and with the lumps
// in cut that is at most 6.1, under canyonWidthMax. Own.
const (
	canyonCore        = 0.8
	canyonMiddleWidth = 0.85
)

// carveFill says what a carved block becomes. Tunnels avoid water by asking the aquifer (notes
// section 8): where it says water, or a wall, the block stays as it is.
func (g *Generator) carveFill(a *aquiferCache, x, y, z int) fillKind {
	if y <= carverLavaY {
		return fillLava
	}
	if f := g.fluidAt(a, x, y, z, 0); f != fillWater {
		return f
	}
	return fillSolid
}

// cutCarvers marks what tunnels cut in a chunk and the treeMargin columns around it.
func (g *Generator) cutCarvers(s *chunkScratch, originX, originZ int) {
	s.carved = [carvedSize][carvedSize]carveMask{}
	sink := oldCarveSink{x0: originX - treeMargin, x1: originX + 15 + treeMargin, z0: originZ - treeMargin, z1: originZ + 15 + treeMargin, chunk: &s.carved}
	g.oldWalkCarvers(originX>>4, originZ>>4, carveAround, &sink)
}

// applyCarvers cuts the tunnels into a chunk. noiseTop holds each column's highest noise
// block: icebergs and hoodoos above it are not carved. Removing grass turns the dirt below
// into grass, so the floor of an opened cave has a surface.
func (g *Generator) applyCarvers(c *chunk.Chunk, s *chunkScratch, originX, originZ int, noiseTop *[16][16]int16) {
	g.cutCarvers(s, originX, originZ)
	b := &g.blocks
	for lx := range 16 {
		for lz := range 16 {
			m := &s.carved[lx+treeMargin][lz+treeMargin]
			if !m.any() {
				continue
			}
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
				f := g.carveFill(&s.aq, wx, y, wz)
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
				if float64(y)+lakeMarginAbove >= nominalGround(g.surfaceShape(x, z).offset) {
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
