package worldgen

import "math"

// Bedrock's surface builder for the Bedrock profile (Task 4e), in our own Go from the study of BDS
// 1.26.60.6, phase 5 (OverworldDefaultSurfaceBuilder::buildSurfaceAt, reached from loadChunk through
// the per-column pass; the study's scratch harness zz_p5surf). Columns whose biome uses another
// builder (mesa, swamp, frozen ocean) keep the Java surface rules, which those builders are not read
// for yet.
//
// Per column, x outer and z inner, the builder draws from the chunk's bedrock-floor stream, carried on
// from the floor (R1): a soil depth from that float, a "minecraft:surface" simplex noise and 3, and,
// below sand, a sandstone run. A second simplex stack, from MT(level seed) after 20202 draws, picks the
// biome's material adjustments.

// bSimplex is one octave of Bedrock's float simplex noise (2D and 3D evaluation).
type bSimplex struct {
	xo, yo, zo float32
	p          [512]int32
}

// bSimplexStack is Bedrock's PerlinSimplexNoise: octaves and their normaliser.
type bSimplexStack struct {
	oct  []bSimplex
	norm float32
}

var bSimplexGrad = [12][3]float32{{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0}, {1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1}}

// The 2D skew factors as Bedrock stores them (single precision bit patterns).
var (
	bSkew2   = math.Float32frombits(0x3ebb67ae)
	bUnskew2 = math.Float32frombits(0x3e58658c)
)

// newBSimplexStack draws n octaves: three offsets (in the order given) and a permutation each.
func newBSimplexStack(n int, float func() float32, below func(n uint32) int32, reversed bool) *bSimplexStack {
	st := &bSimplexStack{oct: make([]bSimplex, n)}
	for o := range st.oct {
		s := &st.oct[o]
		a, b, c := float(), float(), float()
		if reversed {
			a, c = c, a
		}
		s.xo, s.yo, s.zo = a*256, b*256, c*256
		for i := range 256 {
			s.p[i] = int32(i)
		}
		for i := range 256 {
			j := int(below(uint32(256-i))) + i
			s.p[i], s.p[j] = s.p[j], s.p[i]
			s.p[i+256] = s.p[i]
		}
	}
	var sum float32
	sc := float32(1)
	for range n {
		sum += 1 / sc
		sc *= 0.5
	}
	st.norm = 1 / sum
	return st
}

func bFloor(v float32) int32 {
	i := int32(v)
	if v <= 0 {
		i--
	}
	return i
}

// at2 is one octave's 2D value at a scaled point, before the 70 factor.
func (s *bSimplex) at2(xx, yy float32) float32 {
	g2 := bUnskew2
	sk := float32(yy+xx) * bSkew2
	i := bFloor(sk + xx)
	j := bFloor(sk + yy)
	t := float32(i+j) * g2
	x0 := xx - (float32(i) - t)
	y0 := yy - (float32(j) - t)
	var i1, j1 int32 = 1, 0
	if x0 <= y0 {
		i1, j1 = 0, 1
	}
	ii, jj := i&255, j&255
	var n0, n1, n2 float32
	if t0 := float32(0.5-float32(x0*x0)) - float32(y0*y0); t0 >= 0 {
		g := s.p[s.p[jj]+ii] % 12
		n0 = float32(float32(bSimplexGrad[g][0]*x0)+float32(bSimplexGrad[g][1]*y0)) * t0 * t0 * t0 * t0
	}
	x1 := (x0 - float32(i1)) + g2
	y1 := (y0 - float32(j1)) + g2
	if t1 := float32(0.5-float32(x1*x1)) - float32(y1*y1); t1 >= 0 {
		g := s.p[s.p[jj+j1]+ii+i1] % 12
		n1 = float32(float32(x1*bSimplexGrad[g][0])+float32(y1*bSimplexGrad[g][1])) * t1 * t1 * t1 * t1
	}
	x2 := x0 + -1 + (g2 + g2)
	y2 := y0 + -1 + (g2 + g2)
	if t2 := float32(0.5-float32(x2*x2)) - float32(y2*y2); t2 >= 0 {
		g := s.p[s.p[jj+1]+ii+1] % 12
		n2 = float32(float32(x2*bSimplexGrad[g][0])+float32(y2*bSimplexGrad[g][1])) * t2 * t2 * t2 * t2
	}
	return n0 + n1 + n2
}

// at3 is one octave's 3D value with no offsets.
func (s *bSimplex) at3(x, y, z float32) float32 {
	const f3, g3 = float32(0.33333334), float32(0.16666667)
	sk := float32(x+y+z) * f3
	jy := bFloor(y + sk)
	ix := bFloor(sk + x)
	kz := bFloor(sk + z)
	t := float32(ix+jy+kz) * g3
	x0 := x - (float32(ix) - t)
	y0 := y - (float32(jy) - t)
	z0 := z - (float32(kz) - t)
	var i1, j1, k1, i2, j2, k2 int32
	if y0 <= x0 {
		switch {
		case z0 <= y0:
			i1, j1, k1, i2, j2, k2 = 1, 0, 0, 1, 1, 0
		case x0 < z0:
			i1, j1, k1, i2, j2, k2 = 0, 0, 1, 1, 0, 1
		default:
			i1, j1, k1, i2, j2, k2 = 1, 0, 0, 1, 0, 1
		}
	} else {
		switch {
		case y0 < z0:
			i1, j1, k1, i2, j2, k2 = 0, 0, 1, 0, 1, 1
		case z0 <= x0:
			i1, j1, k1, i2, j2, k2 = 0, 1, 0, 1, 1, 0
		default:
			i1, j1, k1, i2, j2, k2 = 0, 1, 0, 0, 1, 1
		}
	}
	ii, jj, kk := ix&255, jy&255, kz&255
	p := &s.p
	corner := func(cx, cy, cz float32, gi int32) float32 {
		tt := float32(float32(0.6-float32(cx*cx))-float32(cy*cy)) - float32(cz*cz)
		if tt < 0 {
			return 0
		}
		g := gi % 12
		return float32(float32(float32(bSimplexGrad[g][2]*cz)+float32(cx*bSimplexGrad[g][0]))+float32(cy*bSimplexGrad[g][1])) * tt * tt * tt * tt
	}
	n0 := corner(x0, y0, z0, p[ii+p[jj+p[kk]]])
	n1 := corner((x0-float32(i1))+g3, (y0-float32(j1))+g3, (z0-float32(k1))+g3, p[ii+i1+p[jj+j1+p[kk+k1]]])
	n2 := corner((x0-float32(i2))+f3, (y0-float32(j2))+f3, (z0-float32(k2))+f3, p[ii+i2+p[jj+j2+p[kk+k2]]])
	n3 := corner(x0+-1+0.5, y0+-1+0.5, z0+-1+0.5, p[ii+1+p[jj+1+p[kk+1]]])
	return (n0 + n1 + n2 + n3) * 32
}

// surfaceAdjust is one of a biome's surface_material_adjustments: a noise scale and range, and the
// materials it swaps in (empty for those it leaves).
type surfaceAdjust struct {
	scale, lo, hi                    float32
	top, mid, floor, foundation, sea string
}

// steepAdjust is a biome's steep_material_adjustment: the material and which slopes take it.
type steepAdjust struct {
	material                 string
	north, south, west, east bool
}

// surfaceRow is a biome's surface builder as read from its biome file (bedrock_pack.go): the
// components minecraft:surface_builder, minecraft:mountain_parameters' steep adjustment,
// minecraft:surface_material_adjustments and minecraft:climate's temperature.
type surfaceRow struct {
	biome, kind                      string
	top, mid, floor, foundation, sea string
	seaDepth                         int
	temperature                      float32
	steep                            steepAdjust
	adjust                           []surfaceAdjust
}

// surfaceMats is the five materials a column is built with.
type surfaceMats struct{ top, mid, floor, foundation, sea bs }

// surfaceAdj is an adjustment in our states, with a mask of which materials it sets.
type surfaceAdj struct {
	scale, lo, hi float32
	mats          surfaceMats
	has           [5]bool
}

// surfaceBiome is a biome's builder in our states.
type surfaceBiome struct {
	overworld                bool // The default builder; other kinds keep the Java rules here.
	gradient                 bool // A noise_gradient builder: the subsurface pass builds the column.
	mats                     surfaceMats
	seaDepth                 int32
	temp                     float32
	steep                    bs
	hasSteep                 bool
	north, south, west, east bool
	adj                      []surfaceAdj
}

// bedrockSurface is what a generator keeps for the builder.
type bedrockSurface struct {
	depth  *bSimplexStack
	adjust *bSimplexStack
	biomes [len(biomeTableNames)]surfaceBiome
	sub    [len(biomeTableNames)]*gradientDef // Subsurface builders, with this world's noises.
}

// surfaceState is a material's state. Bedrock's snow block is our snow_block, hardened clay terracotta.
func surfaceState(name string) bs {
	switch name {
	case "":
		return sAir
	case "snow":
		return stateOf("snow_block")
	case "hardened_clay":
		return sTerracotta
	}
	return stateOf(name)
}

func newBedrockSurface(seed int64, w seedFactory, bd *BedrockData) *bedrockSurface {
	bsf := &bedrockSurface{biomes: bd.surfaceBiomes}
	for i, gd := range bd.subsurface {
		if gd != nil {
			bsf.sub[i] = gd.withNoise(w)
		}
	}
	x := w.named("minecraft:surface")
	bsf.depth = newBSimplexStack(4, x.nextFloat, func(n uint32) int32 { return int32(x.nextInt(n)) }, false)
	m := newBMT(bedrockLevelSeed(seed))
	for range 20202 {
		m.u32()
	}
	bsf.adjust = newBSimplexStack(3, m.nextFloat, func(n uint32) int32 { return int32(m.u32() % n) }, true)
	return bsf
}

// compile turns a biome's builder as read into our states.
func (r *surfaceRow) compile() (sb surfaceBiome) {
	sb.overworld = r.kind == "overworld"
	sb.gradient = r.kind == "noise_gradient"
	sb.mats = surfaceMats{surfaceState(r.top), surfaceState(r.mid), surfaceState(r.floor), surfaceState(r.foundation), surfaceState(r.sea)}
	sb.seaDepth, sb.temp = int32(r.seaDepth), r.temperature
	if r.steep.material != "" {
		sb.hasSteep, sb.steep = true, surfaceState(r.steep.material)
		sb.north, sb.south, sb.west, sb.east = r.steep.north, r.steep.south, r.steep.west, r.steep.east
	}
	for _, a := range r.adjust {
		x := surfaceAdj{scale: a.scale, lo: a.lo, hi: a.hi}
		for k, n := range [5]string{a.top, a.mid, a.floor, a.foundation, a.sea} {
			if n == "" {
				continue
			}
			x.has[k] = true
			s := surfaceState(n)
			switch k {
			case 0:
				x.mats.top = s
			case 1:
				x.mats.mid = s
			case 2:
				x.mats.floor = s
			case 3:
				x.mats.foundation = s
			case 4:
				x.mats.sea = s
			}
		}
		sb.adj = append(sb.adj, x)
	}
	return sb
}

// adjustValue is the adjustment noise at a column: octaves at falling frequency, each weighted by the
// inverse of its frequency, at height 0.
func (bsf *bedrockSurface) adjustValue(a *surfaceAdj, x, z int) float32 {
	f := a.scale
	fx, fy, fz := float32(f*float32(x)), float32(0), float32(float32(z)*f)
	var v float32
	sc := float32(1)
	for o := range bsf.adjust.oct {
		e := bsf.adjust.oct[o].at3(float32(sc*fx), float32(sc*fy), float32(fz*sc))
		v += e / sc
		sc *= 0.5
	}
	return v * bsf.adjust.norm
}

// depthNoise works out the depth noise of a chunk's 256 columns, index z*16+x.
func (bsf *bedrockSurface) depthNoise(cx, cz int, out *[256]float32) {
	clear(out[:])
	ox, oz := float32(cx*16), float32(cz*16)
	sc := float32(1)
	for o := range bsf.depth.oct {
		s := &bsf.depth.oct[o]
		xs, amp := float32(sc*0.125), float32(0.55/sc)
		for j := range 16 {
			yy := float32(float32(float32(j)+oz)*xs) + s.yo
			for k := range 16 {
				xx := float32(float32(float32(k)+ox)*xs) + s.xo
				out[j*16+k] = float32(float32(s.at2(xx, yy)*70)*amp) + out[j*16+k]
			}
		}
		sc *= 0.5
	}
}

// isFoundationStone is what the builder replaces: stone and its three rocks, plain or polished.
func isFoundationStone(s bs) bool {
	switch stateBlock[s] {
	case stateBlockIndex("stone"), stateBlockIndex("granite"), stateBlockIndex("polished_granite"), stateBlockIndex("diorite"),
		stateBlockIndex("polished_diorite"), stateBlockIndex("andesite"), stateBlockIndex("polished_andesite"):
		return true
	}
	return false
}

const noSeaLevel = math.MinInt32 + 1000

// bedrockColumn builds one column with the default builder. low is the first index (Y - minY) left
// alone, from the preliminary surface; depthN is the column's depth noise; r is the floor stream.
func (bsf *bedrockSurface) bedrockColumn(b *baseChunk, r *bmt, lx, lz int, sb *surfaceBiome, low int, depthN float32) {
	x, z := b.cx*16+lx, b.cz*16+lz
	var hits []*surfaceAdj
	for i := range sb.adj {
		a := &sb.adj[i]
		if v := bsf.adjustValue(a, x, z); a.lo <= v && v < a.hi {
			hits = append(hits, a)
		}
	}
	steep := false
	if sb.hasSteep {
		h := func(x, z int) int { return int(b.wgTop[z<<4|x]) - 1 - minY }
		if sb.north || sb.south {
			d := h(lx, max(lz-1, 0)) - h(lx, min(lz, 14)+1)
			steep = sb.south && d > 3 || sb.north && d < -3
		}
		if !steep && (sb.west || sb.east) {
			d := h(max(lx, 1)-1, lz) - h(min(lx, 14)+1, lz)
			steep = sb.east && d > 3 || sb.west && d < -3
		}
	}
	f := r.nextFloat()
	depth := int32(float32(float32(f*0.25)+float32(depthN/3.0)) + 3.0)
	// The column starts at its first free index, capped at index 319 as the study's harness has it.
	top := min(int(b.wgTop[lz<<4|lx])-minY, 319)
	if low >= top {
		return
	}
	apply := func(m surfaceMats) surfaceMats {
		for _, a := range hits {
			if a.has[0] {
				m.top = a.mats.top
			}
			if a.has[1] {
				m.mid = a.mats.mid
			}
			if a.has[2] {
				m.floor = a.mats.floor
			}
			if a.has[3] {
				m.foundation = a.mats.foundation
			}
			if a.has[4] {
				m.sea = a.mats.sea
			}
		}
		return m
	}
	cur := apply(sb.mats)
	topM, midM := cur.top, cur.mid
	if steep {
		topM, midM = sb.steep, sb.steep
	}
	run, water := int32(-1), int32(noSeaLevel)
	for yi := top; yi > low; yi-- {
		y := yi + minY
		blk := b.b[cellIndex(lx, y, lz)]
		if blk == sBedrock {
			continue
		}
		if run >= 0 && blk.isAir() && y+1 <= maxY {
			// An overhang: loose ground left hanging becomes its stone.
			switch b.b[cellIndex(lx, y+1, lz)] {
			case sSand:
				b.set(lx, y+1, lz, sSandst)
			case sRedSand:
				b.set(lx, y+1, lz, sRedSands)
			case sGravel:
				b.set(lx, y+1, lz, cur.foundation)
			}
		}
		if blk.isAir() {
			run, water = -1, noSeaLevel
			continue
		}
		topC, midC := cur.top, cur.mid
		if steep {
			topC, midC, topM, midM = sb.steep, sb.steep, sb.steep, sb.steep
		}
		yy := int32(yi)
		if !isFoundationStone(blk) {
			if blk.has(fWater) {
				b.set(lx, y, lz, cur.sea)
				water = max(water, yy+1)
			} else if blk == sIce || blk == sPacked || stateBlock[blk] == stateBlockIndex("blue_ice") {
				water = max(water, yy+1)
			}
			continue
		}
		switch {
		case run == -1:
			if depth <= 0 {
				topM, midM = sAir, cur.foundation
			} else if water-4 <= yy && yy <= water+1 {
				topM, midM = topC, midC
			}
			if yy < water && topM.isAir() {
				topM = cur.sea
				if cur.sea == sWater && sb.temp < 0.15 {
					topM = sIce
				}
			}
			run = depth
			switch {
			case yy >= water-1:
				b.set(lx, y, lz, topM)
			case yi == low+1 || yy < water-(sb.seaDepth+depth):
				b.set(lx, y, lz, cur.floor)
				midM, topM = cur.foundation, sAir
			default:
				b.set(lx, y, lz, midM)
			}
		case run < 1:
			b.set(lx, y, lz, cur.foundation)
		default:
			b.set(lx, y, lz, midM)
			run--
			if run == 0 && (midM == sSand || midM == sRedSand || stateBlock[midM] == stateBlockIndex("suspicious_sand")) {
				run = r.nextIntN(4) + max(yy-water, 0)
				midM = sSandst
			}
		}
	}
}

// bedrockDeepslate lays the deepslate gradient over the noise stage's stone, as Bedrock's noise fill
// does (R2): deepslate below Y 0; at Y 0 to 8 where a positional draw under the name
// "minecraft:depth_based_layer" falls under 1 - Y/8.
func (g *Generator) bedrockDeepslate(b *baseChunk, x0, x1, z0, z1 int) {
	ox, oz := b.cx*16, b.cz*16
	for y := minY; y <= 8; y++ {
		for lz := z0; lz <= z1; lz++ {
			for lx := x0; lx <= x1; lx++ {
				c := cellIndex(lx, y, lz)
				if b.b[c] != sStone {
					continue
				}
				if y < 0 {
					b.b[c] = sDeepsl
					continue
				}
				r := g.sf.deepslate.at(int32(ox+lx), int32(y), int32(oz+lz))
				if r.nextFloat() < max(0, 1-float32(y)*0.125) {
					b.b[c] = sDeepsl
				}
			}
		}
	}
}

// applyBedrockSurface is the Bedrock profile's surface pass over the columns given. Every column of
// the chunk draws from the floor stream in order, so a column built alone skips the ones before it by
// one draw each; it differs from the whole chunk's only after a sandstone run's draw in an earlier
// column of the chunk.
//
// A part-chunk build (SurfaceHeight builds one column) draws one word of the floor stream for each
// column it skips, as a plain column does. A skipped column with a sandstone run draws one more, and
// the steep test reads the neighbours' tops, so the columns built after such a column can get other
// surface materials than in the whole chunk. Heights never change: the builder only picks materials.
func (g *Generator) applyBedrockSurface(b *baseChunk, s *chunkScratch, floor *bmt, x0, x1, z0, z1 int) {
	c := &s.rule
	c.g, c.b, c.s = g, b, s
	var noise [256]float32
	g.bsurf.depthNoise(b.cx, b.cz, &noise)
	ox, oz := b.cx*16, b.cz*16
	// The column's biome is the stored one at its top (T11), read through the copy rule of the sky
	// sections; a part-chunk build has no whole-chunk top and reads it as computed.
	topSec := buildTopSection(b, x0, x1, z0, z1)
	for lx := range 16 {
		for lz := range 16 {
			if lx < x0 || lx > x1 || lz < z0 || lz > z1 {
				floor.u32()
				continue
			}
			x, z := ox+lx, oz+lz
			var bio uint8
			if g.bedrockBiomes {
				bio = s.bedrockBiome(g, x, storedY(int(b.wgTop[lz<<4|lx]), topSec), z)
			} else {
				bio = s.blockBiome(g, x, int(b.wgTop[lz<<4|lx]), z)
			}
			sb := &g.bsurf.biomes[bio]
			if !sb.overworld {
				floor.u32() // As a default column would; the other builders are not read yet.
				if !sb.gradient {
					g.surfaceRulesColumn(b, s, lx, lz)
				}
				continue
			}
			low := min(max(g.preliminarySurface(&s.prelims, x, z)-8, minY), maxY-1)
			g.bsurf.bedrockColumn(b, floor, lx, lz, sb, low-minY, noise[lz*16+lx])
		}
	}
}
