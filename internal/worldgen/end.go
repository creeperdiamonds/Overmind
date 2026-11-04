package worldgen

import (
	"math"
)

// The End (Task 11), Bedrock's TheEndGenerator in our own Go from the study of BDS 1.26.60.6
// (NOTES-bedrock-end, phase 8 "TheEndGenerator terrain"): Java 1.12's ChunkGeneratorEnd in single
// precision with Bedrock's MT. A 3 x 33 x 3 grid of densities per chunk from three Perlin octave stacks
// and the island shape, interpolated over 8 x 4 x 8 cells; end stone where the density is positive.
// Everything here is read from the code: the noises and their draw order, the scales, the island
// value and the outer islands' simplex test, the slides at the top and bottom, the fill.

const endHeight = 128 // The terrain's rows; spikes, chorus and gateways stand inside them too.

// region adds an octave stack over a grid, Bedrock's Perlin_getRegion (0xd8127e0) on its legacy 3D
// path, used by the End and the Nether: octave k (the first built is k 0) at frequency f = 0.5^k and
// amplitude 1/f, each sample at ((i + pos) * scale * f) + offset in float32, no wrap. x outer, z, then
// y; out is indexed (x*zs + z)*ys + y.
func (l legacyPerlin) region(out []float32, pos [3]float32, xs, ys, zs int, scale [3]float32) {
	clear(out)
	f := float32(1)
	for i := range l {
		s := [3]float32{f * scale[0], f * scale[1], f * scale[2]}
		l[i].regionOctave(out, pos, xs, ys, zs, s, 1/f)
		f *= 0.5
	}
}

// regionOctave is one octave of region. The four x-lerped corner pairs are recomputed only when the
// y lattice cell changes; the cell remembered is kept across columns (initialised once per call, as
// the code does). The y fraction the gradients take on a recompute is the offset's own fraction while
// the sample is still in the offset's cell, and the sample's fraction modulo the y step otherwise; the
// fade always takes the sample's fraction.
func (o *legacyOctave) regionOctave(out []float32, pos [3]float32, xs, ys, zs int, s [3]float32, amp float32) {
	p := o.p[:]
	yoI := int32(o.yo)
	yoFrac := o.yo - float32(yoI)
	last := int32(-1)
	var a0, a1, a2, a3 float32
	j := 0
	for ix := range xs {
		fx := float32(float32(ix)+pos[0])*s[0] + o.xo
		xi := ffloor(fx)
		X := xi & 255
		fx -= float32(xi)
		u := legacyFade(fx)
		for iz := range zs {
			fz := float32(float32(iz)+pos[2])*s[2] + o.zo
			zi := ffloor(fz)
			Z := zi & 255
			fz -= float32(zi)
			w := legacyFade(fz)
			for iy := range ys {
				ya := float32(float32(iy)+pos[1])*s[1] + o.yo
				yi := ffloor(ya)
				Y := yi & 255
				fy := ya - float32(yi)
				v := legacyFade(fy)
				var yy float32
				rec := false
				if yoI == int32(ya) {
					yy, rec = yoFrac, true
				} else if Y != last {
					yy = fy - float32(float32(ffloor(fy/s[1]))*s[1])
					rec = true
				}
				if rec {
					A := p[X] + Y
					B := p[X+1] + Y
					y1 := yy + -1
					c0 := legacyGrad(p[p[A]+Z], fx, yy, fz)
					c1 := legacyGrad(p[p[A]+Z+1], fx, yy, fz-1)
					c2 := legacyGrad(p[p[A+1]+Z], fx, y1, fz)
					c3 := legacyGrad(p[p[A+1]+Z+1], fx, y1, fz-1)
					a0 = legacyLerp(u, c0, legacyGrad(p[p[B]+Z], fx-1, yy, fz))
					a1 = legacyLerp(u, c1, legacyGrad(p[p[B]+Z+1], fx-1, yy, fz-1))
					a2 = legacyLerp(u, c2, legacyGrad(p[p[B+1]+Z], fx-1, y1, fz))
					a3 = legacyLerp(u, c3, legacyGrad(p[p[B+1]+Z+1], fx-1, y1, fz-1))
					last = Y
				}
				out[j] += legacyLerp(w, legacyLerp(v, a0, a2), legacyLerp(v, a1, a3)) * amp
				j++
			}
		}
	}
}

// endNoise is what a world's End keeps: the generator's noises in the ctor's order from one MT of the
// 32-bit level seed (two 16-octave limits, the 8-octave selector, the island simplex; the material
// noise after them is not used by the terrain).
type endNoise struct {
	min, max, main legacyPerlin
	island         *bSimplex
}

func newEndNoise(seed int64) *endNoise {
	r := newBMT(bedrockLevelSeed(seed))
	n := &endNoise{}
	n.min = newLegacyPerlin(r, 16)
	n.max = newLegacyPerlin(r, 16)
	n.main = newLegacyPerlin(r, 8)
	st := newBSimplexStack(1, r.nextFloat, func(k uint32) int32 { return int32(r.u32() % k) }, false)
	n.island = &st.oct[0]
	return n
}

// endSlide is the fall-off at the bottom (y 0..7) and top (y 15..32) of the 33 rows, constants from
// the code (0x14e8880 on): v*a + b.
var endSlide = func() (t [33][2]float32) {
	lo := [8][2]float32{{-0.1428572, -34.285717}, {0, -30}, {0.1428571, -25.7142868}, {0.2857143, -21.4285717},
		{0.4285714, -17.1428585}, {0.5714285, -12.8571434}, {0.7142857, -8.5714293}, {0.8571429, -4.2857146}}
	for y := range 33 {
		switch {
		case y < 8:
			t[y] = lo[y]
		case y > 14:
			t[y] = [2]float32{float32(1 - float64(y-14)/64), float32(-3000 * float64(y-14) / 64)}
		default:
			t[y] = [2]float32{1, 0}
		}
	}
	return t
}()

// islandSimplex is the outer islands' test values around a chunk: for the 25 x 25 chunks within 12,
// the simplex at the chunk's coordinates where it is beyond 64 chunks of the centre (NaN elsewhere).
func (n *endNoise) islandSimplex(cx, cz int) *[25][25]float32 {
	var t [25][25]float32
	for dx := -12; dx <= 12; dx++ {
		for dz := -12; dz <= 12; dz++ {
			X, Z := int64(cx+dx), int64(cz+dz)
			v := float32(math.NaN())
			if X*X+Z*Z > 4096 {
				v = n.island.at2(float32(X), float32(Z)) * 70
			}
			t[dx+12][dz+12] = v
		}
	}
	return &t
}

// islandValue is getIslandHeightValue (0x541fbf0) at the point (2cx + i, 2cz + k): the main island's
// 100 - 8 r, clamped to -100..80, raised by any outer island whose chunk's simplex is below -0.9.
func (n *endNoise) islandValue(cx, cz, i, k int, sim *[25][25]float32) float32 {
	fx, fz := float32(i+cx*2), float32(k+cz*2)
	f := float32(float32(float32(math.Sqrt(float64(float32(fx*fx)+float32(fz*fz)))))*-8) + 100
	f = min(max(f, -100), 80)
	for dx := -12; dx <= 12; dx++ {
		X := cx + dx
		for dz := -12; dz <= 12; dz++ {
			Z := cz + dz
			v := sim[dx+12][dz+12]
			if !(v < -0.9) {
				continue
			}
			f3 := float32((absInt(Z)*147+absInt(X)*3439)%13 + 9)
			zz, xx := float32(k-2*dz), float32(i-2*dx)
			f6 := 100 - float32(float32(float32(math.Sqrt(float64(float32(zz*zz)+float32(xx*xx)))))*f3)
			f6 = min(max(f6, -100), 80)
			if f6 > f {
				f = f6
			}
		}
	}
	return f
}

// heights is a chunk's 3 x 3 x 33 densities, index (i*3 + k)*33 + y.
func (n *endNoise) heights(cx, cz int) (out [3 * 3 * 33]float32) {
	var sel, lo, hi [3 * 3 * 33]float32
	pos := [3]float32{float32(cx * 2), 0, float32(cz * 2)}
	n.main.region(sel[:], pos, 3, 33, 3, [3]float32{17.1103, 4.277575, 17.1103})
	n.min.region(lo[:], pos, 3, 33, 3, [3]float32{1368.824, 684.412, 1368.824})
	n.max.region(hi[:], pos, 3, 33, 3, [3]float32{1368.824, 684.412, 1368.824})
	sim := n.islandSimplex(cx, cz)
	idx := 0
	for i := range 3 {
		for k := range 3 {
			isl := n.islandValue(cx, cz, i, k, sim)
			for y := range 33 {
				a, b := lo[idx]*0.001953125, hi[idx]*0.001953125
				t := (sel[idx]/10 + 1) * 0.5
				var v float32
				switch {
				case t < 0:
					v = a
				case t > 1:
					v = b
				default:
					v = (b-a)*t + a
				}
				v = (v + -8) + isl
				if sl := endSlide[y]; sl != [2]float32{1, 0} {
					v = v*sl[0] + sl[1]
				}
				out[idx] = v
				idx++
			}
		}
	}
	return out
}

// fill writes a chunk's end stone, index y<<8 | z<<4 | x for Y 0..127: 2 x 32 x 2 cells of 8 x 4 x 8,
// solid where the running value is above zero, tested before it steps.
func (n *endNoise) fill(cx, cz int, out *[endHeight * 256]bs, stone bs) {
	h := n.heights(cx, cz)
	at := func(i, k, y int) float32 { return h[(i*3+k)*33+y] }
	for i := range 2 {
		for k := range 2 {
			for j := range 32 {
				d1, d2, d3, d4 := at(i, k, j), at(i, k+1, j), at(i+1, k, j), at(i+1, k+1, j)
				d5 := (at(i, k, j+1) - d1) * 0.25
				d6 := (at(i, k+1, j+1) - d2) * 0.25
				d7 := (at(i+1, k, j+1) - d3) * 0.25
				d8 := (at(i+1, k+1, j+1) - d4) * 0.25
				for l := range 4 {
					d10, d11 := d1, d2
					d12, d13 := (d3-d1)*0.125, (d4-d2)*0.125
					for xx := range 8 {
						d16 := (d11 - d10) * 0.125
						d15 := d10
						for zz := range 8 {
							if d15 > 0 {
								out[(j*4+l)<<8|(k*8+zz)<<4|(i*8+xx)] = stone
							}
							d15 += d16
						}
						d10 += d12
						d11 += d13
					}
					d1 += d5
					d2 += d6
					d3 += d7
					d4 += d8
				}
			}
		}
	}
}
