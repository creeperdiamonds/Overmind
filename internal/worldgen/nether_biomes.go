package worldgen

import "math"

// The Nether's biomes (Task 10), in our own Go from the study's reading of the code
// (NOTES-structures-phase2, "phase 3: Nether biomes -- SOLVED from the code", 100% of columns on seeds
// 100 and 1). The biome is 2D: four noises (temperature, humidity, altitude, weirdness) at the quart, the
// nearest of the five biomes' points, then Java 1.12's Voronoi zoom to blocks.

// Our indexes of the Nether biomes, in biomeTableNames.
var (
	bNetherWastes   = biomeIdx["nether_wastes"]
	bSoulSandValley = biomeIdx["soul_sand_valley"]
	bCrimsonForest  = biomeIdx["crimson_forest"]
	bWarpedForest   = biomeIdx["warped_forest"]
	bBasaltDeltas   = biomeIdx["basalt_deltas"]
)

// isNetherBiome says whether a biome of the table is the Nether's.
func isNetherBiome(b uint8) bool { return b >= bNetherWastes && b <= bBasaltDeltas }

// netherPoints are the biomes' multinoise targets (the biome files' multinoise_generation_rules):
// temperature, humidity, altitude, weirdness, weight.
var netherPoints = [5]struct {
	b    *uint8
	t, h float64
	a, w float64
	off  float64
}{{&bNetherWastes, 0, 0, 0, 0, 0}, {&bSoulSandValley, 0, -0.5, 0, 0, 0}, {&bCrimsonForest, 0.4, 0, 0, 0, 0},
	{&bWarpedForest, 0, 0.5, 0, 0, 0.375}, {&bBasaltDeltas, -0.5, 0, 0, 0, 0.175}}

// bPerlin is the PerlinNoise of the biome noise: octaves of improved noise, each at a frequency f
// halving from 1 and weighted 1/f, and the stack's amplitude 1 / sum(1/0.5^k).
type bPerlin struct {
	oct []legacyOctave
	amp float32
}

func newBPerlin(r *bmt, n int) bPerlin {
	p := bPerlin{oct: make([]legacyOctave, n)}
	for i := range p.oct {
		p.oct[i] = newLegacyOctave(r)
	}
	var s float32
	f := float32(1)
	for range n {
		s = s + 1/f
		f = f * 0.5
	}
	p.amp = 1 / s
	return p
}

// improved is Java's ImprovedNoise at one point, in float32: Perlin_eval 0xd815700 for one octave (the
// biome noises and the capped builder's).
func (o *legacyOctave) improved(x, y, z float32) float32 {
	x, y, z = x+o.xo, y+o.yo, z+o.zo
	fx, fy, fz := float32(math.Floor(float64(x))), float32(math.Floor(float64(y))), float32(math.Floor(float64(z)))
	X, Y, Z := int32(fx)&255, int32(fy)&255, int32(fz)&255
	x, y, z = x-fx, y-fy, z-fz
	u, v, w := legacyFade(x), legacyFade(y), legacyFade(z)
	p := o.p[:]
	a := p[X] + Y
	aa, ab := p[a]+Z, p[a+1]+Z
	b := p[X+1] + Y
	ba, bb := p[b]+Z, p[b+1]+Z
	x1, y1, z1 := x-1, y-1, z-1
	return legacyLerp(w,
		legacyLerp(v, legacyLerp(u, legacyGrad(p[aa], x, y, z), legacyGrad(p[ba], x1, y, z)), legacyLerp(u, legacyGrad(p[ab], x, y1, z), legacyGrad(p[bb], x1, y1, z))),
		legacyLerp(v, legacyLerp(u, legacyGrad(p[aa+1], x, y, z1), legacyGrad(p[ba+1], x1, y, z1)), legacyLerp(u, legacyGrad(p[ab+1], x, y1, z1), legacyGrad(p[bb+1], x1, y1, z1))))
}

func (p *bPerlin) value(x, y, z float32) float32 {
	var t float32
	f := float32(1)
	for i := range p.oct {
		t = t + float32(p.oct[i].improved(float32(x*f), float32(y*f), float32(z*f))/f)
		f = f * 0.5
	}
	return t
}

// netherClimateNoise is one of the four noises (init 0x531b9b0): two PerlinNoises of two octaves from
// MT(seed32 + index), and the value factor 1/6 / ((1/3 + 1) * 0.1).
type netherClimateNoise struct {
	a, b bPerlin
	vf   float32
}

func newNetherClimateNoise(seed32 uint32, i int) netherClimateNoise {
	r := newBMT(seed32 + uint32(i))
	n := netherClimateNoise{a: newBPerlin(r, 2)}
	n.b = newBPerlin(r, 2)
	n.vf = float32(1.0/6) / float32((1.0/3+1.0)*0.1)
	return n
}

func (n *netherClimateNoise) at(qx, qz int) float32 {
	const s, k = float32(0.015625), float32(1.0181268)
	x, z := float32(float32(qx)*s), float32(float32(qz)*s)
	x2, z2 := float32(float32(float32(qx)*k)*s), float32(float32(float32(qz)*k)*s)
	return float32(float32(float32(n.b.value(x2, 0, z2)*n.b.amp)+float32(n.a.value(x, 0, z)*n.a.amp)) * n.vf)
}

// netherBiomes is a world's Nether biome source.
type netherBiomes struct {
	noise [4]netherClimateNoise
	zoom  int64 // Java 1.12's world seed of GenLayerVoronoiZoom(10).
}

func newNetherBiomes(seed int64) *netherBiomes {
	nb := &netherBiomes{}
	for i := range nb.noise {
		nb.noise[i] = newNetherClimateNoise(uint32(seed), i)
	}
	const salt = int64(-0x7945492989db0428) // 0x86bab6d7626f4bd8
	w := seed
	for range 3 {
		w = layerMix(w, salt)
	}
	nb.zoom = w
	return nb
}

func layerMix(s, add int64) int64 { return s*(s*6364136223846793005+1442695040888963407) + add }

// quart is the biome of a quart column: the nearest point, first on ties.
func (nb *netherBiomes) quart(qx, qz int) uint8 {
	var v [4]float64
	for i := range v {
		v[i] = float64(nb.noise[i].at(qx, qz))
	}
	best, bestD := uint8(0), math.Inf(1)
	for _, p := range netherPoints {
		d := (v[0]-p.t)*(v[0]-p.t) + (v[1]-p.h)*(v[1]-p.h) + (v[2]-p.a)*(v[2]-p.a) + (v[3]-p.w)*(v[3]-p.w) + p.off*p.off
		if d < bestD {
			best, bestD = *p.b, d
		}
	}
	return best
}

// block is the biome at a block column: GenLayerVoronoiZoom(10) over the quarts, in float32.
func (nb *netherBiomes) block(bx, bz int, quart func(qx, qz int) uint8) uint8 {
	X, Z := (bx-2)>>2, (bz-2)>>2
	ix, iz := float32((bx-2)&3), float32((bz-2)&3)
	var d [4]float32
	corners := [4][4]int{{X, Z, 0, 0}, {X + 1, Z, 4, 0}, {X, Z + 1, 0, 4}, {X + 1, Z + 1, 4, 4}}
	draw := func(s int64) (float32, int64) {
		r := (s >> 24) % 1024
		if r < 0 {
			r += 1024
		}
		return float32(float32(float32(float32(r)*0.0009765625)+-0.5) * 3.6), layerMix(s, nb.zoom)
	}
	for i, c := range corners {
		s := layerMix(nb.zoom, int64(c[0]*4))
		s = layerMix(s, int64(c[1]*4))
		s = layerMix(s, int64(c[0]*4))
		s = layerMix(s, int64(c[1]*4))
		jx, s := draw(s)
		jz, _ := draw(s)
		dx, dz := jx+float32(c[2]), jz+float32(c[3])
		a, b := iz-dz, ix-dx
		d[i] = float32(a*a) + float32(b*b)
	}
	pick := 3
	switch {
	case d[0] < d[1] && d[0] < d[2] && d[0] < d[3]:
		pick = 0
	case d[1] < d[0] && d[1] < d[2] && d[1] < d[3]:
		pick = 1
	case d[2] < d[0] && d[2] < d[1] && d[2] < d[3]:
		pick = 2
	}
	return quart(corners[pick][0], corners[pick][1])
}
