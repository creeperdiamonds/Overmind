package worldgen

import "math"

// Carvers follow Mojang's algorithms, per the owner's decision (NOTES-exact-features.md section 6),
// in our own Go: worm caves and canyons, walked by the study's 48-bit generator from every source
// chunk in reach, in the same order. Each target chunk has one mask, so the first cut through a block
// decides it. A tunnel depends on no block, so a column can be cut alone.

// carverReach is how far a source chunk's tunnels can reach, in chunks.
const carverReach = 8

const (
	carverLavaY = -56 // Carved space at or under this is lava.

	pi32       = float32(math.Pi)
	halfPi32   = float32(math.Pi / 2)
	twoPi32    = float32(2 * math.Pi)
	sinFactor  = 10430.378350470453
	sinTabSize = 65536
)

// carverStarts lists the three carvers: their chance per chunk, in their index order. Every Overworld
// biome has the same three.
var carverStarts = [3]float32{0.15, 0.07, 0.01}

var sinTable = func() (t [sinTabSize]float32) {
	for i := range t {
		t[i] = float32(math.Sin(float64(i) / sinFactor))
	}
	return t
}()

func tsin(a float32) float32 {
	return sinTable[int64(float64(float64(a)*sinFactor))&(sinTabSize-1)]
}

func tcos(a float32) float32 {
	return sinTable[int64(float64(float64(a)*sinFactor)+16384.0)&(sinTabSize-1)]
}

var sinHalfPi = tsin(float32(math.Pi / 2))

// carveMask marks the blocks of one column that tunnels have passed through.
type carveMask [(maxY - minY + 1) / 64]uint64

func (m *carveMask) has(y int) bool {
	i := y - minY
	return m[i>>6]&(1<<(i&63)) != 0
}

func (m *carveMask) set(y int) {
	i := y - minY
	m[i>>6] |= 1 << (i & 63)
}

// carveSink receives what tunnels cut in one target chunk, limited to some of its columns.
type carveSink struct {
	g              *Generator
	b              *baseChunk
	s              *chunkScratch
	x0, x1, z0, z1 int
	minX, minZ     int
	midX, midZ     float64
	masks          *[256]carveMask
}

// walkCarvers runs every carver of every source chunk within carverReach, in the game's order: outer
// x, inner z, then carver index.
func (g *Generator) walkCarvers(c *carveSink, cx, cz int) {
	var r lcg
	for dx := -carverReach; dx <= carverReach; dx++ {
		for dz := -carverReach; dz <= carverReach; dz++ {
			sx, sz := cx+dx, cz+dz
			for idx, chance := range carverStarts {
				s := g.seed + int64(idx)
				r.setSeed(s)
				a, b := r.nextLong(), r.nextLong()
				r.setSeed(int64(sx)*a ^ int64(sz)*b ^ s)
				if r.nextFloat() > chance {
					continue
				}
				switch idx {
				case 0:
					c.caves(&r, sx, sz, -56, 180)
				case 1:
					c.caves(&r, sx, sz, -56, 47)
				default:
					c.canyon(&r, sx, sz)
				}
			}
		}
	}
}

func uniformFloat(r *lcg, lo, hi float32) float32 {
	return float32(r.nextFloat()*(hi-lo)) + lo
}

func uniformInt(r *lcg, lo, hi int32) int32 { return r.nextInt(hi-lo+1) + lo }

func caveThickness(r *lcg) float32 {
	a := float32(r.nextFloat() * 2.0)
	t := a + r.nextFloat()
	if r.nextInt(10) == 0 {
		f := float32(r.nextFloat() * r.nextFloat())
		t = float32(t * (float32(f*3.0) + 1.0))
	}
	return t
}

// caves cuts one source chunk's worm caves.
func (c *carveSink) caves(r *lcg, sx, sz int, yMin, yMax int32) {
	n := r.nextInt(r.nextInt(r.nextInt(15)+1) + 1)
	for range n {
		x := float64(int32(sx*16) + r.nextInt(16))
		y := float64(uniformInt(r, yMin, yMax))
		z := float64(int32(sz*16) + r.nextInt(16))
		hm := float64(uniformFloat(r, 0.7, 1.4))
		vm := float64(uniformFloat(r, 0.8, 1.3))
		floor := float64(uniformFloat(r, -1.0, -0.4))
		tunnels := int32(1)
		if r.nextInt(4) == 0 {
			ys := float64(uniformFloat(r, 0.1, 0.9))
			th := 1.0 + float32(r.nextFloat()*6.0)
			hr := 1.5 + float64(float64(sinHalfPi)*float64(th))
			c.ellipsoid(x+1.0, y, z, hr, float64(hr*ys), floor, nil)
			tunnels += r.nextInt(4)
		}
		for range tunnels {
			yaw := float32(r.nextFloat() * twoPi32)
			pitch := (r.nextFloat() - 0.5) / 4.0
			th := caveThickness(r)
			dist := 112 - r.nextInt(28)
			seed := r.nextLong()
			c.tunnel(seed, x, y, z, hm, vm, th, yaw, pitch, 0, dist, 1.0, floor)
		}
	}
}

func (c *carveSink) canReach(x, z float64, cur, dist int32, th float32) bool {
	dx, dz := x-c.midX, z-c.midZ
	rem := float64(dist - cur)
	rr := float64(float32(th + 2.0 + 16.0))
	return float64(dx*dx)+float64(dz*dz)-float64(rem*rem) <= float64(rr*rr)
}

// tunnel is one worm, which can fork into two at its split point.
func (c *carveSink) tunnel(seed int64, x, y, z, hm, vm float64, th, yaw, pitch float32, step, dist int32, yScale, floor float64) {
	t := newLCG(seed)
	split := t.nextInt(dist/2) + dist/4
	steep := t.nextInt(6) == 0
	var dYaw, dPitch float32
	for cur := step; cur < dist; cur++ {
		s := tsin(float32(pi32*float32(cur)) / float32(dist))
		hr := 1.5 + float64(float64(s)*float64(th))
		vr := float64(hr * yScale)
		cp := tcos(pitch)
		x += float64(float32(tcos(yaw) * cp))
		y += float64(tsin(pitch))
		z += float64(float32(tsin(yaw) * cp))
		if steep {
			pitch = float32(pitch * 0.92)
		} else {
			pitch = float32(pitch * 0.7)
		}
		pitch += float32(dPitch * 0.1)
		yaw += float32(dYaw * 0.1)
		dPitch = float32(dPitch * 0.9)
		dYaw = float32(dYaw * 0.75)
		a := t.nextFloat() - t.nextFloat()
		dPitch += float32(float32(a*t.nextFloat()) * 2.0)
		a = t.nextFloat() - t.nextFloat()
		dYaw += float32(float32(a*t.nextFloat()) * 4.0)
		if cur == split && th > 1.0 {
			s1 := t.nextLong()
			t1 := float32(t.nextFloat()*0.5) + 0.5
			c.tunnel(s1, x, y, z, hm, vm, t1, yaw-halfPi32, pitch/3.0, cur, dist, 1.0, floor)
			s2 := t.nextLong()
			t2 := float32(t.nextFloat()*0.5) + 0.5
			c.tunnel(s2, x, y, z, hm, vm, t2, yaw+halfPi32, pitch/3.0, cur, dist, 1.0, floor)
			return
		}
		if t.nextInt(4) != 0 {
			if !c.canReach(x, z, cur, dist, th) {
				return
			}
			c.ellipsoid(x, y, z, float64(hr*hm), float64(vr*vm), floor, nil)
		}
	}
}

// canyon cuts one source chunk's canyon.
func (c *carveSink) canyon(r *lcg, sx, sz int) {
	x := float64(int32(sx*16) + r.nextInt(16))
	y := float64(uniformInt(r, 10, 67))
	z := float64(int32(sz*16) + r.nextInt(16))
	yaw := float32(r.nextFloat() * twoPi32)
	pitch := uniformFloat(r, -0.125, 0.125)
	const ys = 3.0
	th := canyonThickness(r)
	dist := int32(float32(float32(112) * uniformFloat(r, 0.75, 1.0)))
	seed := r.nextLong()
	t := newLCG(seed)
	var width [maxY - minY + 1]float32
	f := float32(1)
	for i := range width {
		if i == 0 || t.nextInt(3) == 0 {
			f = 1.0 + float32(t.nextFloat()*t.nextFloat())
		}
		width[i] = float32(f * f)
	}
	var dYaw, dPitch float32
	for cur := int32(0); cur < dist; cur++ {
		s := tsin(float32(float32(cur)*pi32) / float32(dist))
		hr := 1.5 + float64(float64(s)*float64(th))
		vr := float64(hr * ys)
		hr = float64(hr * float64(float32(t.nextFloat()*0.25)+0.75))
		vr = float64(vr * float64(float32(t.nextFloat()*0.25)+0.75))
		xc, xs := tcos(pitch), tsin(pitch)
		x += float64(float32(tcos(yaw) * xc))
		y += float64(xs)
		z += float64(float32(tsin(yaw) * xc))
		pitch = float32(pitch * 0.7)
		pitch += float32(dPitch * 0.05)
		yaw += float32(dYaw * 0.05)
		dPitch = float32(dPitch * 0.8)
		dYaw = float32(dYaw * 0.5)
		a := t.nextFloat() - t.nextFloat()
		dPitch += float32(float32(a*t.nextFloat()) * 2.0)
		a = t.nextFloat() - t.nextFloat()
		dYaw += float32(float32(a*t.nextFloat()) * 4.0)
		if t.nextInt(4) != 0 {
			if !c.canReach(x, z, cur, dist, th) {
				return
			}
			c.ellipsoid(x, y, z, hr, vr, 0, &width)
		}
	}
}

// canyonThickness samples the trapezoid(0, 6, 2) float provider.
func canyonThickness(r *lcg) float32 {
	const lo, hi, plateau = float32(0), float32(6), float32(2)
	rng := hi - lo
	side := (rng - plateau) / 2
	end := rng - side
	return lo + float32(r.nextFloat()*end) + float32(r.nextFloat()*side)
}

func floorI(v float64) int { return int(math.Floor(v)) }

// ellipsoid cuts one step of a tunnel. width, for canyons, holds each height's wall factor; otherwise
// floor is the cave's flat floor.
func (c *carveSink) ellipsoid(x, y, z, hr, vr, floor float64, width *[maxY - minY + 1]float32) {
	maxD := 16.0 + float64(hr*2.0)
	if math.Abs(x-c.midX) > maxD || math.Abs(z-c.midZ) > maxD {
		return
	}
	ix0, ix1 := max(floorI(x-hr)-c.minX-1, c.x0), min(floorI(x+hr)-c.minX, c.x1)
	iz0, iz1 := max(floorI(z-hr)-c.minZ-1, c.z0), min(floorI(z+hr)-c.minZ, c.z1)
	y0, y1 := max(floorI(y-vr)-1, minY+1), min(floorI(y+vr)+1, 312)
	for ix := ix0; ix <= ix1; ix++ {
		wx := c.minX + ix
		xd := (float64(wx) + 0.5 - x) / hr
		xd2 := float64(xd * xd)
		for iz := iz0; iz <= iz1; iz++ {
			wz := c.minZ + iz
			zd := (float64(wz) + 0.5 - z) / hr
			zd2 := float64(zd * zd)
			if xd2+zd2 >= 1.0 {
				continue
			}
			grass := false
			m := &c.masks[iz<<4|ix]
			for wy := y1; wy > y0; wy-- {
				yd := (float64(wy) - 0.5 - y) / vr
				yd2 := float64(yd * yd)
				if width != nil {
					if float64((xd2+zd2)*float64(width[wy+64-1]))+yd2/6.0 >= 1.0 {
						continue
					}
				} else if yd <= floor || xd2+yd2+zd2 >= 1.0 {
					continue
				}
				if m.has(wy) {
					continue
				}
				m.set(wy)
				c.carveBlock(ix, wy, iz, wx, wz, &grass)
			}
		}
	}
}

// carveBlock settles one cut block. Water is carved as the aquifer says, with nothing refused: under
// the sea and beside aquifers a carver floods, as the study's fixtures show, and water that then hangs
// over air stays until a block update, as it does in the game before a chunk first ticks.
func (c *carveSink) carveBlock(ix, wy, iz, wx, wz int, grass *bool) {
	b := c.b
	cell := cellIndex(ix, wy, iz)
	st := b.b[cell]
	if st == sGrass || st == sMycelium {
		*grass = true
	}
	if !st.tagged(tagCarvable) {
		return
	}
	var fill bs
	if wy <= carverLavaY {
		fill = sLava
	} else {
		k := c.g.fluidAt(&c.s.aq, wx, wy, wz, 0)
		if k == fillSolid {
			return
		}
		fill = fluidState(k)
	}
	b.set(ix, wy, iz, fill)
	if *grass && b.b[cellIndex(ix, wy-1, iz)] == sDirt {
		rc := &c.s.rule
		rc.startColumn(wx, wz)
		water := noWaterTop
		if fill.hasFluid() {
			water = wy
		}
		rc.setY(1, wy-1, water)
		rc.haveBelow, rc.below = true, 1
		if t := rc.ruleAt(); t != noRule {
			b.set(ix, wy-1, iz, t)
		}
	}
}

// applyJavaCarvers is the carver pass over the columns given.
func (g *Generator) applyJavaCarvers(b *baseChunk, s *chunkScratch, x0, x1, z0, z1 int) {
	clear(s.masks[:])
	s.rule.g, s.rule.b, s.rule.s = g, b, s
	c := carveSink{g: g, b: b, s: s, x0: x0, x1: x1, z0: z0, z1: z1, minX: b.cx * 16, minZ: b.cz * 16,
		midX: float64(b.cx*16 + 8), midZ: float64(b.cz*16 + 8), masks: &s.masks}
	g.walkCarvers(&c, b.cx, b.cz)
}

// SurfaceHeight returns the Y of the highest block in a column at the carvers stage that is not air or
// water: the terrain before any feature, so without trees, plants, snow, lakes or ore. It builds that
// one column, cutting every tunnel that reaches it (walkCarvers, carverReach, one mask), and costs far
// more than NoiseHeight.
func (g *Generator) SurfaceHeight(x, z int) int {
	s := scratchPool.Get().(*chunkScratch)
	defer scratchPool.Put(s)
	lx, lz := x&15, z&15
	// Only this column is built. Its height is the whole chunk's: what a part-chunk build can get
	// wrong is surface materials, not heights (see applyBedrockSurface).
	g.buildBase(&s.base, s, x>>4, z>>4, stageCarvers, lx, lx, lz, lz)
	for y := maxY; y >= minY; y-- {
		if st := s.base.b[cellIndex(lx, y, lz)]; !st.isAir() && st != sWater {
			return y
		}
	}
	return minY - 1
}
