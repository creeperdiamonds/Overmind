package worldgen

import "math"

// Bedrock's mesa and frozen-ocean surface builders (Task 4e round 6), for the Bedrock profile with the
// data read, in our own Go from the study of BDS 1.26.60.6 (NOTES-bedrock-exact T13 and T16; the
// study's scratch harnesses zz_p7mesa and zz_p7frozen). Both run in the surface pass in place of the
// default builder for their biomes' columns. Index space: yi = Y - minY, sea level index 127.

const bSeaIndex = 127

// bedrockMesa is MesaSurfaceBuilder's state (T13, init 0x4b490a0): the 192-entry band table (16 is
// plain terracotta, else a stained colour) and four noises.
type bedrockMesa struct {
	bands                       [192]uint8
	clayOff, surf, pillar, roof *normalNoise
	stained                     [16]bs
}

var terracottaColours = [16]string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray", "light_gray",
	"cyan", "purple", "blue", "brown", "green", "red", "black"}

func newBedrockMesa(seed int64) *bedrockMesa {
	m := &bedrockMesa{}
	// The noises come from the level Xoroshiro seeded with the sign-extended 32-bit seed (T13).
	root := xoroFromSeed(int64(int32(seed)))
	w := root.forkFactory()
	mk := func(name string, first int, amps ...float64) *normalNoise {
		r := w.named(name)
		return newNormalNoise(&r, first, amps)
	}
	m.clayOff = mk("minecraft:clay_bands_offset", -8, 1)
	m.surf = mk("minecraft:badlands_surface", -6, 1, 1, 1)
	m.pillar = mk("minecraft:badlands_pillar", -2, 1, 1, 1, 1)
	m.roof = mk("minecraft:badlands_pillar_roof", -8, 1)
	for i, c := range terracottaColours {
		m.stained[i] = stateOf(c + "_terracotta")
	}
	// The band table: Java 1.18's generateBands with Bedrock's MT draws from the level seed.
	r := newBMT(uint32(seed))
	for i := range m.bands {
		m.bands[i] = 16
	}
	for i := 0; i < 192; i++ {
		i += int(r.nextIntN(5)) + 1
		if i < 192 {
			m.bands[i] = 1
		}
	}
	makeBands := func(base int, colour uint8) {
		n := int(r.nextIntN(10)) + 6
		for range n {
			w := base + int(r.nextIntN(3))
			s := int(r.nextIntN(192))
			for p := 0; s+p < 192 && p < w; p++ {
				m.bands[s+p] = colour
			}
		}
	}
	makeBands(1, 4)  // yellow
	makeBands(2, 12) // brown
	makeBands(1, 14) // red
	white := int(r.nextIntN(7)) + 9
	for placed, s := 0, 0; placed < white && s < 192; s += int(r.nextIntN(16)) + 4 {
		m.bands[s] = 0
		if s-1 > 0 && r.nextBool() {
			m.bands[s-1] = 8
		}
		if s+1 < 192 && r.nextBool() {
			m.bands[s+1] = 8
		}
		placed++
	}
	return m
}

func f32abs(v float32) float32 { return float32(math.Abs(float64(v))) }

// band is a band's block.
func (m *bedrockMesa) band(c uint8) bs {
	if c < 16 {
		return m.stained[c]
	}
	return sTerracotta
}

// column builds one column (T13, build 0x4b48570). r is the surface pass's stream: one nextFloat a
// column. low is the lowest index built (the preliminary surface - 8, inclusive).
func (m *bedrockMesa) column(b *baseChunk, r *bmt, lx, lz int, sb *surfaceBiome, low int, depthN float32) {
	x, z := b.cx*16+lx, b.cz*16+lz
	fx, fz := float32(x), float32(z)
	var h float32
	if sb.bryce {
		a := f32abs(float32(m.surf.at(float64(fx), 0, float64(fz))) * 8.25)
		a = min(a, float32(m.pillar.at(float64(fx*0.2), 0, float64(0.2*fz)))*15)
		if a > 0 {
			rr := f32abs(float32(m.roof.at(float64(fx*0.75), 0, float64(fz*0.75)))*1.5) * 50
			h = min(a*a*2.5, float32(math.Ceil(float64(rr)))+24) + 64
		}
	}
	dn := depthN / 3
	f := r.nextFloat()
	cold := float32(math.Cos(float64(float32(math.Pi)*dn))) <= 0
	const top = maxY - minY
	// A column whose topmost block is a liquid gets no mesa surface.
	for yi := top; yi >= low; yi-- {
		blk := b.b[cellIndex(lx, yi+minY, lz)]
		if blk.has(fWater) || blk.has(fLava) {
			return
		}
		if !blk.isAir() {
			break
		}
	}
	depth := int32(f*0.25 + dn + 3)
	hf := int(math.Floor(float64(h)))
	n := int(float32(math.Round(float64(float32(m.clayOff.at(float64(fx), 0, float64(fz))) * 4))))
	bandAt := func(yi int) bs { return m.band(m.bands[((yi+n)%192+192)%192]) }
	set := func(yi int, s bs) { b.set(lx, yi+minY, lz, s) }
	run, onTop, stop, count := int32(-1), false, false, 0
	under := sb.mats.mid
	for yi := top; yi >= low; yi-- {
		blk := b.b[cellIndex(lx, yi+minY, lz)]
		if blk == sBedrock {
			return
		}
		inc := 1
		if blk.isAir() {
			if yi <= hf+64 && !stop { // The bryce pillar's fill.
				set(yi, sStone)
				blk, inc = sStone, 0
			}
		} else {
			stop = true
		}
		if count >= 15 {
			continue
		}
		if blk.isAir() {
			run = -1
			continue
		}
		if blk != sStone {
			continue // Only stone counts towards the 15 layers.
		}
		switch {
		case run == -1:
			u := sb.mats.mid
			if yi > bSeaIndex+1 {
				u = under
			}
			if yi >= bSeaIndex-4 {
				under = u
			}
			if depth < 1 {
				under = sb.mats.foundation
			}
			run = max(0, int32(yi)-bSeaIndex) + depth
			y32 := int32(yi)
			switch {
			case yi < bSeaIndex-1:
				if under == m.stained[0] {
					set(yi, m.stained[1])
				} else {
					set(yi, under)
				}
				onTop = false
			case !sb.forest || y32 <= bSeaIndex+33+2*depth:
				if y32 <= bSeaIndex+10+depth {
					set(yi, sb.mats.top)
					onTop = true
				} else {
					in := yi > bSeaIndex && yi <= bSeaIndex+192
					c := uint8(1)
					if in {
						c = 16
						if cold {
							c = m.bands[((yi+n)%192+192)%192]
						}
					}
					set(yi, m.band(c))
					onTop = false
				}
			default:
				// cos <= 0: grass block; else coarse dirt (T13, both blocks named from the code).
				if cold {
					set(yi, sGrass)
				} else {
					set(yi, stateOf("coarse_dirt"))
				}
				onTop = false
			}
		case run > 0:
			run--
			if onTop {
				set(yi, m.stained[1])
			} else {
				set(yi, bandAt(yi))
			}
		}
		count += inc
	}
}

// bedrockFrozen is what FrozenOceanSurfaceBuilder adds (T16): the temperature noises of its
// frozen-ocean modifier, Random(3456)'s three octaves and Random(2345)'s one (query.noise's).
type bedrockFrozen struct {
	t3 *bSimplexStack
}

func newBedrockFrozen() *bedrockFrozen {
	m := newBMT(3456)
	return &bedrockFrozen{t3: newBSimplexStack(3, m.nextFloat, func(n uint32) int32 { return int32(m.u32() % n) }, true)}
}

// temperature is Biome::getTemperature's frozen-ocean modifier (T16) for a biome of base temperature t0.
func (f *bedrockFrozen) temperature(t0 float32, x, z int) float32 {
	var a float32
	s := float32(1)
	for o := range f.t3.oct {
		a += f.t3.oct[o].at2(s*(float32(x)*0.05), s*(float32(z)*0.05)) * 70 / s
		s *= 0.5
	}
	if a+queryNoiseOctave.at2(float32(x)*0.2, float32(z)*0.2)*70 < 0.3 &&
		queryNoiseOctave.at2(float32(x)*0.09, float32(z)*0.09)*70 < 0.8 {
		return 0.2
	}
	return t0
}

// iceberg is the frozen-ocean extension, run before the default builder (T16, build 0x4b4c390). It
// says whether the default builder runs after it: where the iceberg noise is at most 1.8 the code
// returns first (no sea floor, no surface, no column draw).
func (g *Generator) iceberg(b *baseChunk, lx, lz int, sb *surfaceBiome, low int) bool {
	n := &g.surface
	x, z := b.cx*16+lx, b.cz*16+lz
	fx, fz := float32(x), float32(z)
	a := f32abs(float32(n.bergSurface.at(float64(fx), 0, float64(fz))) * 8.25)
	a = min(a, float32(n.berg.at(float64(fx*1.28), 0, float64(fz*1.28)))*15)
	if a <= 1.8 {
		return false
	}
	roof := f32abs(float32(n.bergCap.at(float64(fx*1.17), 0, float64(fz*1.17))) * 1.5)
	i := min(a*a*1.2, float32(math.Ceil(float64(roof*40)))+14)
	if g.bsurf.frozen.temperature(sb.temp, x, z) > 0.1 {
		i += -2
	}
	var k, j float32
	if i > 2 {
		k = float32(bSeaIndex) + i
		j = (float32(bSeaIndex) - i) + -7
	}
	// The column's Xoroshiro: the level's positional factory at (x, 0, z), with the x product in 64 bits
	// (Java's is 32).
	h := int64(z)*116129781 ^ int64(x)*3129871
	h = (h*h*42317861 + h*11) >> 16
	fac := g.sf.world
	r := newXoro(uint64(h)^fac.lo, fac.hi)
	l := 2 + int(uint32(r.next())>>30)
	m := bSeaIndex + 18 + r.nextInt(10)
	start := max(int(b.wgTop[lz<<4|lx])-minY, int(float32(1)+k))
	start = min(start, maxY-minY)
	snow := stateOf("snow_block")
	count := 0
	ki, ji := int(k), int(j)
	for yi := start; yi >= low; yi-- {
		blk := b.b[cellIndex(lx, yi+minY, lz)]
		place := false
		if blk.isAir() && yi < ki {
			place = r.nextFloat() > 0.01
		} else if j != 0 && blk.has(fWater) && ji < yi && yi < bSeaIndex {
			place = r.nextFloat() > 0.15
		}
		if place {
			if l < count || yi <= m {
				b.set(lx, yi+minY, lz, sPacked)
			} else {
				b.set(lx, yi+minY, lz, snow)
				count++
			}
		}
	}
	return true
}
