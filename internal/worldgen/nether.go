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
// What is read and what is not: the noise construction and draw order, the fill's constants (684.412,
// 2053.236, 8.55515, 34.2206, 1/512, the cosine and falloff table, the top falloff), the bedrock rows and
// the biome source are from the code. The octave sampler (0xd9127e0) is not decompiled; it is taken to
// be Java's NoiseGeneratorOctaves with its y-cell cache, in float32, and checked against the BDS Nether
// worlds (TestNetherMatchesBDS).

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

// populate adds one octave over a grid, Java 1.12's populateNoiseArray (3D path): the y lattice values
// are only recomputed when the y cell changes, and reused otherwise, including their stale fractions.
func (o *legacyOctave) populate(out []float32, xOff, yOff, zOff float32, xs, ys, zs int, sx, sy, sz, freq float32) {
	amp := 1 / freq
	last := int32(-1)
	var d1, d2, d3, d4 float32
	j := 0
	for ix := range xs {
		fx := xOff + float32(ix)*sx + o.xo
		xi := ffloor(fx)
		X := xi & 255
		fx -= float32(xi)
		u := legacyFade(fx)
		for iz := range zs {
			fz := zOff + float32(iz)*sz + o.zo
			zi := ffloor(fz)
			Z := zi & 255
			fz -= float32(zi)
			w := legacyFade(fz)
			for iy := range ys {
				fy := yOff + float32(iy)*sy + o.yo
				yi := ffloor(fy)
				Y := yi & 255
				fy -= float32(yi)
				v := legacyFade(fy)
				if iy == 0 || Y != last {
					last = Y
					p := o.p[:]
					a := p[X] + Y
					aa, ab := p[a]+Z, p[a+1]+Z
					b := p[X+1] + Y
					ba, bb := p[b]+Z, p[b+1]+Z
					d1 = legacyLerp(u, legacyGrad(p[aa], fx, fy, fz), legacyGrad(p[ba], fx-1, fy, fz))
					d2 = legacyLerp(u, legacyGrad(p[ab], fx, fy-1, fz), legacyGrad(p[bb], fx-1, fy-1, fz))
					d3 = legacyLerp(u, legacyGrad(p[aa+1], fx, fy, fz-1), legacyGrad(p[ba+1], fx-1, fy, fz-1))
					d4 = legacyLerp(u, legacyGrad(p[ab+1], fx, fy-1, fz-1), legacyGrad(p[bb+1], fx-1, fy-1, fz-1))
				}
				out[j] += legacyLerp(w, legacyLerp(v, d1, d2), legacyLerp(v, d3, d4)) * amp
				j++
			}
		}
	}
}

// sample is NoiseGeneratorOctaves.generateNoiseOctaves over a grid at origin (x, y, z): octave i at
// frequency 2^-i, its x and z origins wrapped at 2^24.
func (l legacyPerlin) sample(out []float32, x, y, z float32, xs, ys, zs int, sx, sy, sz float32) {
	clear(out)
	freq := float32(1)
	for i := range l {
		dx, dy, dz := x*freq*sx, y*freq*sy, z*freq*sz
		kx, kz := math.Floor(float64(dx)), math.Floor(float64(dz))
		dx -= float32(kx)
		dz -= float32(kz)
		dx += float32(math.Mod(kx, 16777216))
		dz += float32(math.Mod(kz, 16777216))
		l[i].populate(out, dx, dy, dz, xs, ys, zs, sx*freq, sy*freq, sz*freq, freq)
		freq *= 0.5
	}
}

// netherNoise is what a world's Nether keeps: the generator's noises in the ctor's order (MT of the
// 32-bit level seed): min limit 16, max limit 16, main 8, then a 4-octave Perlin, a 4-octave simplex,
// scale 10, depth 16 and the 3-octave surface simplex.
type netherNoise struct {
	min, max, main legacyPerlin
	surface        *bSimplexStack
	offs           [17]float32
}

func newNetherNoise(seed int64) *netherNoise {
	r := newBMT(uint32(seed))
	n := &netherNoise{}
	n.min = newLegacyPerlin(r, 16)
	n.max = newLegacyPerlin(r, 16)
	n.main = newLegacyPerlin(r, 8)
	newLegacyPerlin(r, 4)
	newBSimplexStack(4, r.nextFloat, func(k uint32) int32 { return int32(r.u32() % k) }, false)
	newLegacyPerlin(r, 10)
	newLegacyPerlin(r, 16)
	n.surface = newBSimplexStack(3, r.nextFloat, func(k uint32) int32 { return int32(r.u32() % k) }, false)
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

// depthNoise is the surface simplex over a chunk's columns (index z*16 + x): three octaves at the
// chunk origin, amplitude 1 halving per octave, sampled as the Overworld builder's depth noise is.
func (n *netherNoise) depthNoise(cx, cz int) (out [256]float32) {
	ox, oz := float32(cx*16), float32(cz*16)
	sc := float32(1)
	for o := range n.surface.oct {
		s := &n.surface.oct[o]
		xs, amp := float32(sc*0.125), float32(1/sc)
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
// lava sea, air above; ids in Y, Z, X order (index y<<8 | z<<4 | x).
func (n *netherNoise) fill(cx, cz int, out *[netherHeight * 256]bs, rack, lava bs) {
	var mn, mx, mainN [5 * 17 * 5]float32
	ox, oz := float32(cx*4), float32(cz*4)
	n.min.sample(mn[:], ox, 0, oz, 5, 17, 5, 684.412, 2053.236, 684.412)
	n.max.sample(mx[:], ox, 0, oz, 5, 17, 5, 684.412, 2053.236, 684.412)
	n.main.sample(mainN[:], ox, 0, oz, 5, 17, 5, 8.55515, 34.2206, 8.55515)
	var dens [5 * 5 * 17]float32
	for i := range dens {
		y := i % 17
		lo, hi := mn[i]*(1.0/512), mx[i]*(1.0/512)
		t := (mainN[i]/10 + 1) * 0.5
		var d float32
		switch {
		case !(0 <= t):
			d = lo
		case 1 < t:
			d = hi
		default:
			d = (hi-lo)*t + lo
		}
		d -= n.offs[y]
		switch y {
		case 14:
			d = d*0.6666666 + -3.3333335
		case 15:
			d = d*0.3333333 + -6.666667
		case 16:
			d = d*0 + -10
		}
		dens[i] = d
	}
	at := func(x, z, y int) float32 { return dens[(x*5+z)*17+y] }
	for x := range 4 {
		for z := range 4 {
			for y := range 16 {
				d1, d2, d3, d4 := at(x, z, y), at(x, z+1, y), at(x+1, z, y), at(x+1, z+1, y)
				s1 := (at(x, z, y+1) - d1) * 0.125
				s2 := (at(x, z+1, y+1) - d2) * 0.125
				s3 := (at(x+1, z, y+1) - d3) * 0.125
				s4 := (at(x+1, z+1, y+1) - d4) * 0.125
				for yy := range 8 {
					e1, e2 := d1, d2
					t1, t2 := (d3-d1)*0.25, (d4-d2)*0.25
					for xx := range 4 {
						v := e1
						step := (e2 - e1) * 0.25
						for zz := range 4 {
							by, bx, bz := y*8+yy, x*4+xx, z*4+zz
							var s bs
							switch {
							case v > 0:
								s = rack
							case by < netherLava:
								s = lava
							}
							out[by<<8|bz<<4|bx] = s
							v += step
						}
						e1 += t1
						e2 += t2
					}
					d1 += s1
					d2 += s2
					d3 += s3
					d4 += s4
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
