package worldgen

import "math"

// noiseSpread is the typical size (standard deviation) of one octave of gradient
// noise, measured by TestNoiseSpreadFitsMinusOneToOne. Dividing by three times this
// puts about 99.7% of values within -1 to 1.
const noiseSpread = 0.27

// mix64 is the SplitMix64 step. It turns a counter into a well-mixed number. We use
// our own so a Go release can never change the world a seed produces.
func mix64(v uint64) uint64 {
	v += 0x9E3779B97F4A7C15
	v = (v ^ (v >> 30)) * 0xBF58476D1CE4E5B9
	v = (v ^ (v >> 27)) * 0x94D049BB133111EB
	return v ^ (v >> 31)
}

// gradientNoise is one layer of seeded 3D gradient noise. It never changes after it
// is built, so workers can share it.
type gradientNoise struct {
	perm    [512]uint8
	x, y, z float64 // Random shift, so layers do not line up on whole numbers.
}

func newGradientNoise(seed uint64) *gradientNoise {
	n := &gradientNoise{}
	for i := range 256 {
		n.perm[i] = uint8(i)
	}
	for i := 255; i > 0; i-- {
		seed = mix64(seed)
		j := seed % uint64(i+1)
		n.perm[i], n.perm[j] = n.perm[j], n.perm[i]
	}
	copy(n.perm[256:], n.perm[:256])
	shift := func() float64 {
		seed = mix64(seed)
		return float64(seed%(1<<32)) / (1 << 16)
	}
	n.x, n.y, n.z = shift(), shift(), shift()
	return n
}

func fade(t float64) float64 {
	return t * t * t * (t*(t*6-15) + 10)
}

func lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}

// gradient takes the dot product of the offset with one of 12 cube-edge directions.
func gradient(hash uint8, x, y, z float64) float64 {
	switch hash % 12 {
	case 0:
		return x + y
	case 1:
		return y - x
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x + z
	case 5:
		return z - x
	case 6:
		return x - z
	case 7:
		return -x - z
	case 8:
		return y + z
	case 9:
		return z - y
	case 10:
		return y - z
	default:
		return -y - z
	}
}

func (n *gradientNoise) at(x, y, z float64) float64 {
	x, y, z = x+n.x, y+n.y, z+n.z
	fx, fy, fz := math.Floor(x), math.Floor(y), math.Floor(z)
	ix, iy, iz := int(fx)&255, int(fy)&255, int(fz)&255
	x, y, z = x-fx, y-fy, z-fz
	u, v, w := fade(x), fade(y), fade(z)

	p := &n.perm
	a, b := int(p[ix])+iy, int(p[ix+1])+iy
	aa, ab, ba, bb := int(p[a])+iz, int(p[a+1])+iz, int(p[b])+iz, int(p[b+1])+iz

	return lerp(
		lerp(
			lerp(gradient(p[aa], x, y, z), gradient(p[ba], x-1, y, z), u),
			lerp(gradient(p[ab], x, y-1, z), gradient(p[bb], x-1, y-1, z), u), v),
		lerp(
			lerp(gradient(p[aa+1], x, y, z-1), gradient(p[ba+1], x-1, y, z-1), u),
			lerp(gradient(p[ab+1], x, y-1, z-1), gradient(p[bb+1], x-1, y-1, z-1), u), v),
		w)
}

// fractal adds octaves of noise. Each octave has half the wavelength and half the
// strength of the one before it. Values mostly fall within -1 to 1 but can go a little outside.
type fractal struct {
	octaves       []*gradientNoise
	freqXZ, freqY float64
	gain          float64
}

// newFractal builds a fractal whose first octave has the given wavelengths in blocks.
func newFractal(seed uint64, wavelengthXZ, wavelengthY float64, octaves int) *fractal {
	f := &fractal{freqXZ: 1 / wavelengthXZ, freqY: 1 / wavelengthY}
	var power, strength float64 = 0, 1
	for i := range octaves {
		f.octaves = append(f.octaves, newGradientNoise(mix64(seed+uint64(i)*0x51ED27)))
		power += strength * strength
		strength /= 2
	}
	f.gain = 1 / (3 * noiseSpread * math.Sqrt(power))
	return f
}

// newFractalSpread builds a fractal in unit coordinates with a set standard deviation.
func newFractalSpread(seed uint64, octaves int, spread float64) *fractal {
	f := newFractal(seed, 1, 1, octaves)
	f.gain *= spread * 3
	return f
}

// at samples the fractal at a block position.
func (f *fractal) at(x, y, z float64) float64 {
	x, y, z = x*f.freqXZ, y*f.freqY, z*f.freqXZ
	var sum float64
	strength := 1.0
	for _, o := range f.octaves {
		sum += o.at(x, y, z) * strength
		x, y, z = x*2, y*2, z*2
		strength /= 2
	}
	return sum * f.gain
}

// layer is one octave of a layered noise: a gradient noise, how many wavelengths it fits
// into one noise unit, and its weight.
type layer struct {
	noise        *gradientNoise
	freq, weight float64
}

// layered is our double noise: two octave stacks added together, the second read at
// 1.01813 times the coordinates so that the lattice of one never lines up with the other.
// Octave i has a wavelength of 2^(-first-i) noise units and a weight of amps[i]*2^-i, and
// the sum is scaled so its standard deviation is about target. The shape of this noise
// and the octave lists passed to it come from studying the Java 26.1 generator; the code
// is our own.
type layered struct {
	first, second []layer
	gain          float64
}

// secondStackScale is the coordinate scale of the second stack, from the same study.
const secondStackScale = 1.01813

// newLayered builds a layered noise. Octaves with amplitude zero cost nothing.
func newLayered(seed uint64, first int, amps []float64, target float64) *layered {
	l := &layered{}
	var power float64
	freq, strength := math.Pow(2, float64(first)), 1.0
	for i, amp := range amps {
		if amp != 0 {
			w := amp * strength
			power += w * w
			l.first = append(l.first, layer{newGradientNoise(mix64(seed + uint64(i)*2 + 1)), freq, w})
			l.second = append(l.second, layer{newGradientNoise(mix64(seed + uint64(i)*2 + 2)), freq, w})
		}
		freq, strength = freq*2, strength/2
	}
	l.gain = target / (noiseSpread * math.Sqrt(2*power))
	return l
}

// gameSpread is the standard deviation an octave list has in the game. The study gives
// measured spreads for the five climate noises only; this formula is our own fit to those
// (it reproduces them within about 0.01). The first octave weighs 2^(n-1)/(2^n-1) over n
// listed octaves, and the sum is scaled by (5/3)*m/(m+1), m being the span from the first
// to the last non-zero amplitude.
func gameSpread(amps []float64) float64 {
	n := float64(len(amps))
	f := math.Pow(2, n-1) / (math.Pow(2, n) - 1)
	var power float64
	first, last := -1, -1
	strength := f
	for i, a := range amps {
		power += a * strength * a * strength
		strength /= 2
		if a != 0 {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	m := float64(last - first + 1)
	return noiseSpread * math.Sqrt(2*power) * (5.0 / 3) * m / (m + 1)
}

// newGameLayered builds a layered noise with the spread its octave list has in the game.
func newGameLayered(seed uint64, first int, amps []float64) *layered {
	return newLayered(seed, first, amps, gameSpread(amps))
}

// at samples the noise at a point in noise units.
func (l *layered) at(x, y, z float64) float64 {
	var sum float64
	for i := range l.first {
		o := &l.first[i]
		sum += o.noise.at(x*o.freq, y*o.freq, z*o.freq) * o.weight
	}
	x, y, z = x*secondStackScale, y*secondStackScale, z*secondStackScale
	for i := range l.second {
		o := &l.second[i]
		sum += o.noise.at(x*o.freq, y*o.freq, z*o.freq) * o.weight
	}
	return sum * l.gain
}

func clamp(v, low, high float64) float64 {
	return math.Max(low, math.Min(high, v))
}
