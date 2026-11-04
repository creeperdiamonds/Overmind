package worldgen

import (
	"math"
)

// The Nether (Task 10), Bedrock's NetherGenerator in our own Go from the study of BDS 1.26.60.6
// (NOTES-bedrock-structures section 4, NOTES-structures-phase2 section 4 and its phase 3 Nether-biome
// section) and the constants of the fill read from the binary (Nether_prepareHeights 0x52d3990). The
// terrain is the legacy Nether noise of Java 1.12's ChunkGeneratorHell in single precision: a 5 x 17 x 5
// grid of densities per chunk from three Perlin octave stacks, interpolated over 4 x 8 x 4 cells.
//
// All of it is read from the code (NOTES-bedrock-nether N1 to N3, R2-6): the noise construction and
// draw order, the octave sampler (Perlin_getRegion, region in end.go), the fill's constants (684.412,
// 2053.2358, 8.55515, 34.220596, 1/512, the cosine and fall-off table, the top fall-off) and its
// interpolation, the bedrock rows and the biome source.

const (
	netherHeight = 128
	netherLava   = 32 // Lava sea: open cells below Y 32 (Java's 32; BDS's dimension field read as the same, checked on the worlds).
)

// legacyOctave is one octave of Bedrock's PerlinNoise (ctor 0xd814e90): float offsets and a shuffle.
type legacyOctave struct {
	xo, yo, zo float32
	p          [512]int32
}

func newLegacyOctave(r *bmt) legacyOctave {
	var o legacyOctave
	o.xo = r.nextFloat() * 256
	o.yo = r.nextFloat() * 256
	o.zo = r.nextFloat() * 256
	for i := range 256 {
		o.p[i] = int32(i)
	}
	for i := range 256 {
		j := i + int(r.nextIntN(int32(256-i)))
		o.p[i], o.p[j] = o.p[j], o.p[i]
		o.p[i+256] = o.p[i]
	}
	return o
}

// legacyPerlin is a stack of octaves, built in order.
type legacyPerlin []legacyOctave

func newLegacyPerlin(r *bmt, n int) legacyPerlin {
	l := make(legacyPerlin, n)
	for i := range l {
		l[i] = newLegacyOctave(r)
	}
	return l
}

var (
	legacyGradX = [16]float32{1, -1, 1, -1, 1, -1, 1, -1, 0, 0, 0, 0, 1, 0, -1, 0}
	legacyGradY = [16]float32{1, 1, -1, -1, 0, 0, 0, 0, 1, -1, 1, -1, 1, -1, 1, -1}
	legacyGradZ = [16]float32{0, 0, 0, 0, 1, 1, -1, -1, 1, 1, -1, -1, 0, 1, 0, -1}
)

// The improved-noise arithmetic in the code's order (the study's reading of PerlinNoise 0xd814e90 for
// the Nether biome noise): the gradient's z term first, the fade as ((t*6 - 15)*t + 10)*t*t*t, the
// lerp as (b - a)*t + a, each step rounded to float32.
func legacyGrad(h int32, x, y, z float32) float32 {
	i := h & 15
	return float32(float32(legacyGradZ[i]*z)+float32(legacyGradX[i]*x)) + float32(legacyGradY[i]*y)
}

func legacyFade(t float32) float32 {
	return float32(float32(float32(float32(float32(float32(t*6)+-15)*t)+10)*t)*t) * t
}

func legacyLerp(t, a, b float32) float32 { return float32(float32(b-a)*t) + a }

func ffloor(v float32) int32 {
	i := int32(v)
	if v < float32(i) {
		i--
	}
	return i
}

// netherNoise is what a world's Nether keeps: the generator's noises in the ctor's order (MT of the
// 32-bit level seed, NOTES-bedrock-nether N1): min limit 16, max limit 16, main 8, a 4-octave Perlin
// (unused), the 4-octave surface simplex, scale 10 and depth 16 (computed by BDS, unused), and the
// 3-octave material-adjustment simplex. Both simplex stacks are order 0 (their x and z offsets swapped).
type netherNoise struct {
	min, max, main legacyPerlin
	surface        *bSimplexStack // The surface builders' depth noise.
	adjust         *bSimplexStack // The surface material adjustments' noise.
	offs           [17]float32
}

func newNetherNoise(seed int64) *netherNoise {
	r := newBMT(uint32(seed))
	n := &netherNoise{}
	n.min = newLegacyPerlin(r, 16)
	n.max = newLegacyPerlin(r, 16)
	n.main = newLegacyPerlin(r, 8)
	below := func(k uint32) int32 { return int32(r.u32() % k) }
	newLegacyPerlin(r, 4)
	n.surface = newBSimplexStack(4, r.nextFloat, below, true)
	newLegacyPerlin(r, 10)
	newLegacyPerlin(r, 16)
	n.adjust = newBSimplexStack(3, r.nextFloat, below, true)
	// The y table: 2 cos(j * pi * 6 / 17) from the sine table, less a cubic fall-off near the ends.
	for j := range n.offs {
		fj := float32(j)
		a := fj * float32(math.Pi)
		if j > 8 {
			fj = 16 - fj
		}
		c := bedrockSin[int32(float32(float32(float32(a*6)/17)*10430.378)+16384)&0xffff]
		v := c + c
		if fj < 4 {
			d := 4 - fj
			v += d * d * d * -10
		}
		n.offs[j] = v
	}
	return n
}

// depthNoise is the surface noise over a chunk's columns (index z*16 + x), as loadChunk 0x52d5ad0 adds
// it (N3): octave k at scale 0.0625 * 0.5^k and amplitude 0.55 / 0.5^k, each 2D simplex at the column
// plus the octave's x and y offsets.
func (n *netherNoise) depthNoise(cx, cz int) (out [256]float32) {
	ox, oz := float32(cx*16), float32(cz*16)
	sc := float32(1)
	for o := range n.surface.oct {
		s := &n.surface.oct[o]
		xs, amp := float32(sc*0.0625), float32(0.55/sc)
		for j := range 16 {
			yy := float32(float32(float32(j)+oz)*xs) + s.yo
			for k := range 16 {
				xx := float32(float32(float32(k)+ox)*xs) + s.xo
				out[j*16+k] = float32(float32(s.at2(xx, yy)*70)*amp) + out[j*16+k]
			}
		}
		sc *= 0.5
	}
	return out
}

// fill writes a chunk's terrain: netherrack where the density is positive, lava in the open below the
// lava sea, air above; ids in Y, Z, X order (index y<<8 | z<<4 | x). The cells are 4 x 8 x 4; each
// block's value is worked out from the cell's corners as the code does (N2), not stepped.
func (n *netherNoise) fill(cx, cz int, out *[netherHeight * 256]bs, rack, lava bs) {
	var mn, mx, mainN [5 * 17 * 5]float32
	pos := [3]float32{float32(cx * 4), 0, float32(cz * 4)}
	n.min.region(mn[:], pos, 5, 17, 5, [3]float32{684.412, 2053.2358, 684.412})
	n.max.region(mx[:], pos, 5, 17, 5, [3]float32{684.412, 2053.2358, 684.412})
	n.main.region(mainN[:], pos, 5, 17, 5, [3]float32{8.55515, 34.220596, 8.55515})
	var dens [5 * 5 * 17]float32
	for i := range dens {
		y := i % 17
		lo, hi := mn[i]*0.001953125, mx[i]*0.001953125
		t := (mainN[i]/10 + 1) * 0.5
		var d float32
		switch {
		case t < 0:
			d = lo
		case t > 1:
			d = hi
		default:
			d = (hi-lo)*t + lo
		}
		d -= n.offs[y]
		switch y {
		case 14:
			d = d*0.6666666 + -3.3333335
		case 15:
			d = d*0.33333331 + -6.666667
		case 16:
			d = d*0 + -10
		}
		dens[i] = d
	}
	at := func(x, z, y int) float32 { return dens[(x*5+z)*17+y] }
	for X := range 4 {
		for Z := range 4 {
			for Y := range 16 {
				for zs := range 4 {
					zf := float32(zs) * 0.25
					b0 := (at(X, Z+1, Y)-at(X, Z, Y))*zf + at(X, Z, Y)
					t0 := (at(X, Z+1, Y+1)-at(X, Z, Y+1))*zf + at(X, Z, Y+1)
					dB := (zf*(at(X+1, Z+1, Y)-at(X+1, Z, Y)) + at(X+1, Z, Y)) - b0
					dT := (zf*(at(X+1, Z+1, Y+1)-at(X+1, Z, Y+1)) + at(X+1, Z, Y+1)) - t0
					for xs := range 4 {
						xf := float32(xs) * 0.25
						B := dB*xf + b0
						T := (dT*xf + t0) - B
						for ys := range 8 {
							y := Y*8 + ys
							v := T*(float32(ys)*0.125) + B
							var s bs
							switch {
							case v > 0:
								s = rack
							case y < netherLava:
								s = lava
							}
							out[y<<8|(Z*4+zs)<<4|(X*4+xs)] = s
						}
					}
				}
			}
		}
	}
}

// netherBedrock writes the floor and roof (NOTES-bedrock-structures section 4): an MT seeded with
// cx*0x9939F508 + cz*0xF1565BD5, then for every column (x outer) nextInt(4)+2 rows of floor, then for
// every column nextInt(5)+1 rows of roof down from Y 127.
func netherBedrock(cx, cz int, out *[netherHeight * 256]bs, bedrock bs) *bmt {
	r := newBMT(uint32(int32(cx))*0x9939F508 + uint32(int32(cz))*0xF1565BD5)
	for x := range 16 {
		for z := range 16 {
			for y := range int(r.nextIntN(4)) + 2 {
				out[y<<8|z<<4|x] = bedrock
			}
		}
	}
	for x := range 16 {
		for z := range 16 {
			n := int(r.nextIntN(5))
			for y := 127 - n; y <= 127; y++ {
				out[y<<8|z<<4|x] = bedrock
			}
		}
	}
	return r
}
