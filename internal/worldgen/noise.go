package worldgen

import "strconv"

// Noise follows Mojang's algorithms exactly, per the owner's decision, in our own Go from the study's
// notes (NOTES-exact-seed.md sections 3 to 5). Every product that meets a sum is wrapped in an
// explicit float64 conversion so that no platform fuses it into a multiply-add, which would change
// the low bits; Java never fuses.

// mix64 is the SplitMix64 step, used for our own position-based chances (ores, carvers, trees). It
// has no part in the game's noises.
func mix64(v uint64) uint64 {
	v += seedGolden
	v = (v ^ (v >> 30)) * 0xBF58476D1CE4E5B9
	v = (v ^ (v >> 27)) * 0x94D049BB133111EB
	return v ^ (v >> 31)
}

func lerp(a, b, t float64) float64 {
	return a + float64(t*(b-a))
}

func clamp(v, low, high float64) float64 {
	if v < low {
		return low
	}
	return min(v, high)
}

func floorInt32(v float64) int32 {
	i := int32(v)
	if float64(i) > v {
		i--
	}
	return i
}

func floorInt64(v float64) int64 {
	i := int64(v)
	if float64(i) > v {
		i--
	}
	return i
}

func fade(t float64) float64 {
	inner := float64(t*6) - 15
	inner = float64(t*inner) + 10
	return float64(float64(float64(t*t)*t) * inner)
}

// Gradient directions by hash & 15. Entries 12 to 15 repeat others, as in the game.
var gradients = [16][3]float64{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0}, {1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1}, {1, 1, 0}, {0, -1, 1}, {-1, 1, 0}, {0, -1, -1},
}

func gradDot(hash uint8, x, y, z float64) float64 {
	d := &gradients[hash&15]
	return float64(d[0]*x) + float64(d[1]*y) + float64(d[2]*z)
}

// octave is one layer of gradient noise. It never changes after it is built.
type octave struct {
	perm       [256]uint8
	xo, yo, zo float64
}

func (o *octave) init(r *xoro) {
	o.xo = float64(r.nextDouble() * 256)
	o.yo = float64(r.nextDouble() * 256)
	o.zo = float64(r.nextDouble() * 256)
	for i := range o.perm {
		o.perm[i] = uint8(i)
	}
	for i := range 256 {
		j := i + r.nextInt(uint32(256-i))
		o.perm[i], o.perm[j] = o.perm[j], o.perm[i]
	}
}

// sample reads the octave at a wrapped point, with no vertical smear.
func (o *octave) sample(ix, iy, iz float64) float64 {
	x, y, z := ix+o.xo, iy+o.yo, iz+o.zo
	fx, fy, fz := floorInt32(x), floorInt32(y), floorInt32(z)
	ry := y - float64(fy)
	return o.corners(fx, fy, fz, x-float64(fx), ry, z-float64(fz), ry)
}

// smearEpsilon is 1e-7 as a single, widened.
const smearEpsilon = float64(float32(1e-7))

// sampleSmeared is sample with the old-style vertical smear the terrain noise uses. The fade still
// reads the true fraction; only the gradients read the smeared one.
func (o *octave) sampleSmeared(ix, iy, iz, smear, fudge float64) float64 {
	x, y, z := ix+o.xo, iy+o.yo, iz+o.zo
	fx, fy, fz := floorInt32(x), floorInt32(y), floorInt32(z)
	ry := y - float64(fy)
	limit := ry
	if fudge >= 0 && fudge < ry {
		limit = fudge
	}
	drop := float64(float64(floorInt32(limit/smear+smearEpsilon)) * smear)
	return o.corners(fx, fy, fz, x-float64(fx), ry-drop, z-float64(fz), ry)
}

func (o *octave) corners(fx, fy, fz int32, rx, ryUsed, rz, ry float64) float64 {
	p := &o.perm
	a, b := int32(p[uint8(fx)]), int32(p[uint8(fx+1)])
	aa, ab := int32(p[uint8(a+fy)]), int32(p[uint8(a+fy+1)])
	ba, bb := int32(p[uint8(b+fy)]), int32(p[uint8(b+fy+1)])
	x1, y1, z1 := rx-1, ryUsed-1, rz-1
	d000 := gradDot(p[uint8(aa+fz)], rx, ryUsed, rz)
	d100 := gradDot(p[uint8(ba+fz)], x1, ryUsed, rz)
	d010 := gradDot(p[uint8(ab+fz)], rx, y1, rz)
	d110 := gradDot(p[uint8(bb+fz)], x1, y1, rz)
	d001 := gradDot(p[uint8(aa+fz+1)], rx, ryUsed, z1)
	d101 := gradDot(p[uint8(ba+fz+1)], x1, ryUsed, z1)
	d011 := gradDot(p[uint8(ab+fz+1)], rx, y1, z1)
	d111 := gradDot(p[uint8(bb+fz+1)], x1, y1, z1)
	u, v, w := fade(rx), fade(ry), fade(rz)
	return lerp(
		lerp(lerp(d000, d100, u), lerp(d010, d110, u), v),
		lerp(lerp(d001, d101, u), lerp(d011, d111, u), v),
		w)
}

// wrapCoord keeps a coordinate in the range the game's octaves read, leaving small ones as they are.
func wrapCoord(v float64) float64 {
	const span = 33554432.0
	if v > -span/2 && v < span/2 {
		return v
	}
	return v - float64(float64(floorInt64(v/span+0.5))*span)
}

// stackLevel is one octave of a stack, with what the stack multiplies it by.
type stackLevel struct {
	o          *octave
	inFactor   float64
	amplitude  float64
	valueScale float64
}

// octaveStack adds octaves whose wavelengths halve. Levels with amplitude 0 have no octave, but
// they still count towards the weights.
type octaveStack struct {
	levels []stackLevel
}

func newOctaveStack(r *xoro, first int, amps []float64) octaveStack {
	pf := r.forkFactory()
	n := len(amps)
	inFactor := pow2(first)
	valueScale := pow2(n-1) / (pow2(n) - 1)
	s := octaveStack{levels: make([]stackLevel, 0, n)}
	for i, a := range amps {
		if a != 0 {
			g := pf.named("octave_" + strconv.Itoa(first+i))
			o := new(octave)
			o.init(&g)
			s.levels = append(s.levels, stackLevel{o, inFactor, a, valueScale})
		}
		inFactor *= 2
		valueScale /= 2
	}
	return s
}

func (s *octaveStack) value(x, y, z float64) float64 {
	var sum float64
	for i := range s.levels {
		l := &s.levels[i]
		v := l.o.sample(wrapCoord(float64(x*l.inFactor)), wrapCoord(float64(y*l.inFactor)), wrapCoord(float64(z*l.inFactor)))
		sum += float64(float64(l.amplitude*v) * l.valueScale)
	}
	return sum
}

func pow2(e int) float64 {
	v := 1.0
	for ; e > 0; e-- {
		v *= 2
	}
	for ; e < 0; e++ {
		v /= 2
	}
	return v
}

// secondStackFactor is how much further along the second stack of a normal noise reads.
const secondStackFactor = 1.0181268882175227

// normalNoise is two octave stacks added and rescaled.
type normalNoise struct {
	first, second octaveStack
	scale         float64
}

func newNormalNoise(r *xoro, first int, amps []float64) *normalNoise {
	n := &normalNoise{first: newOctaveStack(r, first, amps)}
	n.second = newOctaveStack(r, first, amps)
	lo, hi := len(amps), -1
	for i, a := range amps {
		if a != 0 {
			lo, hi = min(lo, i), max(hi, i)
		}
	}
	deviation := 0.1
	deviation *= 1.0 + 1.0/float64(hi-lo+1)
	n.scale = 0.16666666666666666 / deviation
	return n
}

// at samples the noise at a point in noise units.
func (n *normalNoise) at(x, y, z float64) float64 {
	a := n.first.value(x, y, z)
	b := n.second.value(float64(x*secondStackFactor), float64(y*secondStackFactor), float64(z*secondStackFactor))
	return float64((a + b) * n.scale)
}

// blendedNoise is the terrain's 3D noise: a main stack chooses between two limit stacks.
type blendedNoise struct {
	minLimit, maxLimit [16]octave
	main               [8]octave
	// Scales, worked out at run time in double precision as the game does.
	xz, y, limitSmear, mainSmear float64
}

func newBlendedNoise(r *xoro) *blendedNoise {
	b := new(blendedNoise)
	for i := range b.minLimit {
		b.minLimit[i].init(r)
	}
	for i := range b.maxLimit {
		b.maxLimit[i].init(r)
	}
	for i := range b.main {
		b.main[i].init(r)
	}
	base := 684.412
	b.xz, b.y = float64(base*0.25), float64(base*0.125)
	b.limitSmear = float64(b.y * 8.0)
	b.mainSmear = b.limitSmear / 160.0
	return b
}

// at is the noise at a block position.
func (b *blendedNoise) at(bx, by, bz int) float64 {
	lx, ly, lz := float64(float64(bx)*b.xz), float64(float64(by)*b.y), float64(float64(bz)*b.xz)
	mx, my, mz := lx/80.0, ly/160.0, lz/80.0

	var m float64
	pow := 1.0
	for i := range b.main {
		m += b.main[i].sampleSmeared(wrapCoord(float64(mx*pow)), wrapCoord(float64(my*pow)), wrapCoord(float64(mz*pow)),
			float64(b.mainSmear*pow), float64(my*pow)) / pow
		pow /= 2
	}
	t := (m/10.0 + 1.0) / 2.0

	var low, high float64
	pow = 1.0
	for i := range b.minLimit {
		wx, wy, wz := wrapCoord(float64(lx*pow)), wrapCoord(float64(ly*pow)), wrapCoord(float64(lz*pow))
		smear, fudge := float64(b.limitSmear*pow), float64(ly*pow)
		if t < 1.0 {
			low += b.minLimit[i].sampleSmeared(wx, wy, wz, smear, fudge) / pow
		}
		if t > 0.0 {
			high += b.maxLimit[i].sampleSmeared(wx, wy, wz, smear, fudge) / pow
		}
		pow /= 2
	}
	switch {
	case t < 0:
		return low / 512.0 / 128.0
	case t > 1:
		return high / 512.0 / 128.0
	}
	return lerp(low/512.0, high/512.0, t) / 128.0
}

// base3DLimit bounds the size of blendedNoise.at. Its octave sums are weighted to at most 65535/65536
// of the largest octave value, and a value is a blend of gradient dots, none over 2. It is a safe
// bound for skipping the noise where the answer cannot depend on it, and much wider than the 0.9 the
// study measured.
const base3DLimit = 2.0
