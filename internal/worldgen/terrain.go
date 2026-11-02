package worldgen

import (
	"math"
	"slices"
)

// Density is worked out on a coarse grid and blended between grid points: cells of 4 x 8
// x 4 blocks, the cell size the game uses (from studying Java 26.1).
const (
	cellXZ      = 4
	cellY       = 8
	columnNodes = (maxY+1-minY)/cellY + 1
)

const (
	inputContinentalness = iota
	inputErosion
	inputPV
	inputWeirdness
)

// splineInput holds the values a spline can read.
type splineInput [4]float64

// spline maps one input to a value through knots. A knot's value can itself be a spline
// over another input. Between knots it is a cubic Hermite curve, so knots with a slope
// keep the ground from terracing; a slope of zero on both sides gives a smooth step.
type spline struct {
	input int
	knots []knot
}

type knot struct {
	at, value, slope float64
	child            *spline
}

func (k *knot) resolve(in *splineInput) float64 {
	if k.child != nil {
		return k.child.eval(*in)
	}
	return k.value
}

func (s *spline) eval(in splineInput) float64 {
	v := in[s.input]
	last := len(s.knots) - 1
	if v <= s.knots[0].at {
		return s.knots[0].resolve(&in)
	}
	if v >= s.knots[last].at {
		return s.knots[last].resolve(&in)
	}
	i := 0
	for v >= s.knots[i+1].at {
		i++
	}
	a, b := &s.knots[i], &s.knots[i+1]
	span := b.at - a.at
	t := (v - a.at) / span
	y0, y1 := a.resolve(&in), b.resolve(&in)
	// Hermite form: a straight line plus a bump set by how far each end's slope differs
	// from the straight line.
	delta := y1 - y0
	return lerp(y0, y1, t) + t*(1-t)*lerp(a.slope*span-delta, delta-b.slope*span, t)
}

// curve builds a spline from location, value, slope triples, as the study's addendum (A1)
// writes its knots.
func curve(input int, t ...float64) *spline {
	s := &spline{input: input}
	for i := 0; i+2 < len(t); i += 3 {
		s.knots = append(s.knots, knot{at: t[i], value: t[i+1], slope: t[i+2]})
	}
	return s
}

// nest adds a knot that holds another spline, keeping the knots in order. All such knots
// have slope 0 in the study's tables.
func (s *spline) nest(at float64, child *spline) *spline {
	i := 0
	for i < len(s.knots) && s.knots[i].at < at {
		i++
	}
	s.knots = slices.Insert(s.knots, i, knot{at: at, child: child})
	return s
}

// byInput builds a spline whose knots all hold other splines.
func byInput(input int, at []float64, children ...*spline) *spline {
	s := &spline{input: input}
	for i, child := range children {
		s.knots = append(s.knots, knot{at: at[i], child: child})
	}
	return s
}

// offsetCurve is a PV row of the study's offset table (addendum A1), given as raw offset
// values and converted to blocks, where the ground lies at Y = 63.5 + 128 * offset.
func offsetCurve(t ...float64) *spline {
	s := curve(inputPV, t...)
	for i := range s.knots {
		s.knots[i].value = groundBase + groundPerOffset*s.knots[i].value
		s.knots[i].slope *= groundPerOffset
	}
	return s
}

const (
	groundBase      = 63.5
	groundPerOffset = 128.0
)

// heightSpline gives the nominal ground Y from continentalness, erosion and PV. Every knot
// and slope is from the study's addendum, table A1 "Offset". Rivers need nothing else: a
// valley is PV near -1, which sits at Y 44 to 57 when erosion is high, so it cuts below
// sea level.
var heightSpline = func() *spline {
	// Rows shared by several continentalness tables.
	e85 := func() *spline { return offsetCurve(-1, -0.0888, 0.3894, 1, 0.69, 0.3894) }
	e70 := func() *spline { return offsetCurve(-1, -0.1158, 0.3779, 1, 0.64, 0.3779) }
	e40 := func() *spline {
		return offsetCurve(-1, -0.2222, 0, -0.75, -0.2222, 0, -0.65, 0, 0, 0.5955, 2.98e-08, 0, 0.6055, 2.98e-08, 0.2535, 1, 0.1, 0.2535)
	}
	lowland := func(valley float64) *spline {
		return offsetCurve(-1, valley, 0.5, -0.4, 0.01, 0, 0, 0.01, 0, 0.4, 0.03, 0.04, 1, 0.1, 0.049)
	}
	coastErosion := []float64{-0.85, -0.7, -0.4, -0.35, -0.1, 0.2, 0.7}
	beach := func() *spline {
		return byInput(inputErosion, coastErosion, e85(), e70(), e40(),
			offsetCurve(-1, -0.3, 0.5, -0.4, 0.05, 0, 0, 0.05, 0, 0.4, 0.05, 0, 1, 0.06, 0.007),
			offsetCurve(-1, -0.15, 0.5, -0.4, 0, 0, 0, 0, 0, 0.4, 0.05, 0.1, 1, 0.06, 0.007),
			offsetCurve(-1, -0.15, 0.5, -0.4, 0, 0, 0, 0, 0, 0.4, 0, 0, 1, 0, 0),
			offsetCurve(-1, -0.02, 0, -0.4, -0.03, 0, 0, -0.03, 0, 0.4, 0, 0.06, 1, 0, 0))
	}
	low := byInput(inputErosion, coastErosion, e85(), e70(), e40(),
		offsetCurve(-1, -0.25, 0.5, -0.4, 0.05, 0, 0, 0.05, 0, 0.4, 0.05, 0, 1, 0.06, 0.007),
		offsetCurve(-1, -0.1, 0.5, -0.4, 0.001, 0.01, 0, 0.003, 0.01, 0.4, 0.05, 0.094, 1, 0.06, 0.007),
		lowland(-0.1),
		offsetCurve(-1, -0.02, 0, -0.4, -0.03, 0, 0, -0.03, 0, 0.4, 0.03, 0.12, 1, 0.1, 0.049))
	// The erosion 0.45 and 0.55 rows hold one more nested row at PV -0.4.
	shattered := func(valley float64) *spline {
		return offsetCurve(-1, valley, 0, 0, 0.17, 0).nest(-0.4, lowland(valley))
	}
	inlandErosion := []float64{-0.85, -0.7, -0.4, -0.35, -0.1, 0.2, 0.4, 0.45, 0.55, 0.58, 0.7}
	mid := byInput(inputErosion, inlandErosion,
		offsetCurve(-1, 0.2024, 0, 0, 0.7162, 0.5138, 1, 1.23, 0.5138),
		offsetCurve(-1, 0.2, 0, 0, 0.4468, 0.4332, 1, 0.88, 0.4332),
		offsetCurve(-1, 0.2, 0, 0, 0.3083, 0.3917, 1, 0.7, 0.3917),
		offsetCurve(-1, -0.25, 0.5, -0.4, 0.35, 0, 0, 0.35, 0, 0.4, 0.35, 0, 1, 0.42, 0.049),
		offsetCurve(-1, -0.1, 0.5, -0.4, 0.007, 0.07, 0, 0.021, 0.07, 0.4, 0.35, 0.658, 1, 0.42, 0.049),
		lowland(-0.1), lowland(-0.1), shattered(-0.1), shattered(-0.1), lowland(-0.1),
		offsetCurve(-1, -0.02, 0, -0.4, -0.03, 0, 0, -0.03, 0, 0.4, 0.03, 0.12, 1, 0.1, 0.049))
	high := byInput(inputErosion, inlandErosion,
		offsetCurve(-1, 0.3479, 0, 0, 0.924, 0.576, 1, 1.5, 0.576),
		offsetCurve(-1, 0.2, 0, 0, 0.5392, 0.4608, 1, 1, 0.4608),
		offsetCurve(-1, 0.2, 0, 0, 0.5392, 0.4608, 1, 1, 0.4608),
		offsetCurve(-1, -0.2, 0.5, -0.4, 0.5, 0, 0, 0.5, 0, 0.4, 0.5, 0, 1, 0.6, 0.07),
		offsetCurve(-1, -0.05, 0.5, -0.4, 0.01, 0.1, 0, 0.03, 0.1, 0.4, 0.5, 0.94, 1, 0.6, 0.07),
		lowland(-0.05), lowland(-0.05), shattered(-0.05), shattered(-0.05), lowland(-0.05),
		offsetCurve(-1, -0.02, 0.015, -0.4, 0.01, 0, 0, 0.01, 0, 0.4, 0.03, 0.04, 1, 0.1, 0.049))
	top := curve(inputContinentalness, -1.1, 0.044, 0, -1.02, -0.2222, 0, -0.51, -0.2222, 0, -0.44, -0.12, 0, -0.18, -0.12, 0)
	for i := range top.knots {
		top.knots[i].value = groundBase + groundPerOffset*top.knots[i].value
	}
	return top.nest(-0.16, beach()).nest(-0.15, beach()).nest(-0.1, low).nest(0.25, mid).nest(1, high)
}()

// factorTable is the part every continentalness table of the factor spline shares (addendum
// A1, "Factor"): erosion knots -0.6 to 0.03.
func factorTable(base float64) *spline {
	row := curve(inputWeirdness, -0.2, 6.3, 0, 0.2, base, 0)
	return byInput(inputErosion, []float64{-0.6, -0.5, -0.35, -0.25, -0.1, 0.03},
		row,
		curve(inputWeirdness, -0.05, 6.3, 0, 0.05, 2.67, 0),
		row, row,
		curve(inputWeirdness, -0.05, 2.67, 0, 0.05, 6.3, 0),
		row)
}

// coastFactor is a factor table with the shattered band at erosion 0.45 and 0.55.
func coastFactor(base float64) *spline {
	band := curve(inputPV, -0.9, base, 0).nest(-0.69, curve(inputWeirdness, 0, base, 0, 0.1, 0.625, 0))
	s := factorTable(base)
	s.knots = append(s.knots,
		knot{at: 0.35, value: base}, knot{at: 0.45, child: band}, knot{at: 0.55, child: band}, knot{at: 0.62, value: base})
	return s
}

// inlandFactor is the last factor table, at continentalness 0.06.
func inlandFactor(base float64) *spline {
	row := curve(inputWeirdness, -0.2, 6.3, 0, 0.2, base, 0)
	ground := curve(inputPV, 0.7, 1.56, 0).nest(0.45, row)
	band := curve(inputPV, -0.15, 1.37, 0).nest(-0.7, row)
	s := factorTable(base)
	s.knots = append(s.knots,
		knot{at: 0.05, child: ground}, knot{at: 0.4, child: ground},
		knot{at: 0.45, child: band}, knot{at: 0.55, child: band}, knot{at: 0.58, value: base})
	return s
}

// factorSpline says how steep the vertical gradient is: a low factor lets the 3D noise move
// the ground a long way. All values from addendum A1.
var factorSpline = curve(inputContinentalness, -0.19, 3.95, 0).
	nest(-0.15, coastFactor(6.25)).nest(-0.1, coastFactor(5.47)).nest(0.03, coastFactor(5.08)).nest(0.06, inlandFactor(4.69))

// jaggedSpline gives extra spikes on peaks. All values from addendum A1, "Jaggedness".
var jaggedSpline = func() *spline {
	bySign := func(negative, positive float64) *spline {
		return curve(inputWeirdness, -0.01, negative, 0, 0.01, positive, 0)
	}
	flatToFull := func(negative, positive float64) *spline {
		return curve(inputPV, 0.2, 0, 0, 0.45, 0, 0).nest(1, bySign(negative, positive))
	}
	erosion := []float64{-1, -0.78, -0.5775}
	near := byInput(inputErosion, erosion, flatToFull(0.63, 0.3), flatToFull(0.315, 0.15), flatToFull(0.315, 0.15))
	near.knots = append(near.knots, knot{at: -0.375})
	far := byInput(inputErosion, erosion,
		curve(inputPV, 0.2, 0, 0).nest(0.45, bySign(0.63, 0.3)).nest(1, bySign(0.63, 0.3)),
		flatToFull(0.63, 0.3), flatToFull(0.63, 0.3))
	far.knots = append(far.knots, knot{at: -0.375})
	return curve(inputContinentalness, -0.11, 0, 0).nest(0.03, near).nest(0.65, far)
}()

func quarterNeg(v float64) float64 {
	if v < 0 {
		return v / 4
	}
	return v
}

func halfNeg(v float64) float64 {
	if v < 0 {
		return v / 2
	}
	return v
}

// blendGround is the nominal ground at a block inside a grid cell, from the cell's corners.
func blendGround(a, b, c, d *columnShape, tx, tz float64) float64 {
	return bilerp(a.ground, b.ground, c.ground, d.ground, tx, tz)
}

// nominalGround is the ground Y from the offset spline alone, with no 3D noise.
func nominalGround(c, e, w float64) float64 {
	return heightSpline.eval(splineInput{inputContinentalness: c, inputErosion: e, inputPV: peaksAndValleys(w), inputWeirdness: w})
}

// columnShape is what the splines say about a column.
type columnShape struct {
	ground  float64 // Nominal ground Y.
	factor  float64
	jag     float64 // Jaggedness times the jagged noise, in depth units.
	erosion float64
}

func (g *Generator) shapeAt(x, z int) columnShape {
	c, e, w := g.shapeClimate(g.warped(x, z))
	in := splineInput{inputContinentalness: c, inputErosion: e, inputPV: peaksAndValleys(w), inputWeirdness: w}
	s := columnShape{ground: heightSpline.eval(in), factor: factorSpline.eval(in), erosion: e}
	if j := jaggedSpline.eval(in); j > 0 {
		// xz scale 1500 on a first octave of -16 gives a wavelength of about 44 blocks.
		s.jag = j * halfNeg(g.jagged.at(float64(x)*jaggedScale, 0, float64(z)*jaggedScale))
	}
	return s
}

const jaggedScale = 1500.0 / 65536

// maxBase is the largest size we let the blended 3D noise reach. The study measured a
// largest value of 0.90 with a standard deviation of 0.22, so this clamp is almost never
// touched, and it lets a point far from the ground skip the noise.
const maxBase = 1.2

// base3D is the blended noise: a selector picks between two limit noises. Wavelengths and
// the spread (std 0.22) come from the notes. Our own choices: limit noises stop at 6 octaves
// and the selector at 4 (the notes list 16 and 8), since finer ones fall inside one 4 x 8 x 4
// grid cell; and the selector's sharpness of 5 below.
func (g *Generator) base3D(x, y, z float64) float64 {
	t := clamp(0.5+g.terrain.selector.at(x/baseSelectorXZ, y/baseSelectorY, z/baseSelectorXZ)*5, 0, 1)
	var v float64
	lx, ly, lz := x/baseLimitXZ, y/baseLimitY, z/baseLimitXZ
	switch {
	case t <= 0:
		v = g.terrain.limitA.at(lx, ly, lz)
	case t >= 1:
		v = g.terrain.limitB.at(lx, ly, lz)
	default:
		v = lerp(g.terrain.limitA.at(lx, ly, lz), g.terrain.limitB.at(lx, ly, lz), t)
	}
	return clamp(v, -maxBase, maxBase)
}

const (
	baseSelectorXZ = 60.0
	baseSelectorY  = 240.0
	baseLimitXZ    = 190.0
	baseLimitY     = 380.0
)

// terrainNoises holds the 3D ground noises.
type terrainNoises struct {
	selector, limitA, limitB *fractal
}

func newTerrainNoises(seed int64) terrainNoises {
	sub := func(id uint64) uint64 { return mix64(uint64(seed) ^ mix64(id)) }
	return terrainNoises{
		selector: newFractal(sub(30), 1, 1, 4),
		limitA:   newFractalSpread(sub(31), 6, baseSpread),
		limitB:   newFractalSpread(sub(32), 6, baseSpread),
	}
}

// baseSpread is the limit noises' standard deviation, set so the blended noise measures
// about 0.22, the figure from the study.
const baseSpread = 0.232

// groundFine moves the zero of the depth gradient off nominal ground: the study's offset
// constant -0.50375 and gradient (+1.5 at Y -64 to -1.5 at Y 320) put it 0.02 blocks above
// 63.5 + 128 * offset (notes section 3).
const groundFine = 0.02

// ramp is 4*quarterNeg(depth*factor), the part of the sloped density without 3D noise.
func (s *columnShape) ramp(y float64) float64 {
	return 4 * quarterNeg(((s.ground+groundFine-y)/128+s.jag)*s.factor)
}

// slopedDensity is the ground before caves: 4*quarterNeg(depth*factor), plus the blended
// noise when the point is near enough to the ground for it to matter. The lopsided
// quarterNeg lifts ground 128/factor blocks per unit of noise but lowers it only 32/factor,
// so bumps and overhangs rise from a firm base.
func (g *Generator) slopedDensity(s *columnShape, x, y, z float64) float64 {
	sloped := s.ramp(y)
	if sloped < -maxBase || sloped > maxBase+slopedKeepNoise {
		return sloped
	}
	return sloped + g.base3D(x, y, z)
}

// slopedKeepNoise: above this sum, caves no longer read the sloped value (their bias term
// is zero from a sloped value of 2.34), so the noise can be left out deep underground.
const slopedKeepNoise = 2.4

// slide fades density at the top and bottom of the world: to -0.078 between Y 240 and 256,
// and to +0.117 between Y -40 and -64, so the top opens and the floor closes. From the study.
func slide(v, y float64) float64 {
	if y > 240 {
		v = lerp(v, -0.078, math.Min((y-240)/16, 1))
	}
	if y < -40 {
		v = lerp(v, 0.117, math.Min((-40-y)/24, 1))
	}
	return v
}

const densityEase = 0.8

// finishDensity applies the last steps: the slides and the 0.64 scale, both from the notes.
// The notes also name a squeeze step with no formula. Ours is a tanh, our own choice: it keeps
// the sign and eases large values, so blending is not pulled far by a point deep in rock.
// densityEase is tuned by us. Of the curves we measured, this one passed the chunk edge test
// and came nearest the inland share of columns under Y 63 (see NOTES.md).
func finishDensity(v, y float64) float64 {
	return math.Tanh(0.64 * densityEase * slide(v, y))
}

// fillColumn works out final density at each grid point of the column at a block position.
// Above 0 is solid.
func (g *Generator) fillColumn(x, z int, column *[columnNodes]float64) {
	s := g.shapeAt(x, z)
	fx, fz := float64(x), float64(z)
	for i := range column {
		y := float64(minY + i*cellY)
		column[i] = finishDensity(g.slopedDensity(&s, fx, y, fz), y)
	}
}

// cell holds the density at the corners of one grid cell. The first index is x, the
// second z, the third y.
type cell [2][2][2]float64

// uniform reports whether all corners agree, and whether they agree on solid.
func (c *cell) uniform() (same, solid bool) {
	solid = c[0][0][0] > 0
	for x := range 2 {
		for z := range 2 {
			for y := range 2 {
				if (c[x][z][y] > 0) != solid {
					return false, false
				}
			}
		}
	}
	return true, solid
}

// solidAt blends the corners at a block's offset inside the cell.
func (c *cell) solidAt(dx, dz, dy int) bool {
	tx, tz, ty := float64(dx)/cellXZ, float64(dz)/cellXZ, float64(dy)/cellY
	return lerp(
		lerp(lerp(c[0][0][0], c[1][0][0], tx), lerp(c[0][1][0], c[1][1][0], tx), tz),
		lerp(lerp(c[0][0][1], c[1][0][1], tx), lerp(c[0][1][1], c[1][1][1], tx), tz),
		ty) > 0
}

// highestSolid returns the Y of the highest block of the terrain alone in a column, before
// caves are carved. Caves can open the top, so use SurfaceHeight for the real highest block.
func (g *Generator) highestSolid(x, z int) int {
	gridX, gridZ := x&^(cellXZ-1), z&^(cellXZ-1)
	dx, dz := x-gridX, z-gridZ
	var corners [2][2][columnNodes]float64
	for i := range 2 {
		for j := range 2 {
			g.fillColumn(gridX+i*cellXZ, gridZ+j*cellXZ, &corners[i][j])
		}
	}
	for layer := columnNodes - 2; layer >= 0; layer-- {
		c := cell{
			{{corners[0][0][layer], corners[0][0][layer+1]}, {corners[0][1][layer], corners[0][1][layer+1]}},
			{{corners[1][0][layer], corners[1][0][layer+1]}, {corners[1][1][layer], corners[1][1][layer+1]}},
		}
		if same, solid := c.uniform(); same && !solid {
			continue
		}
		for dy := cellY - 1; dy >= 0; dy-- {
			y := minY + layer*cellY + dy
			if c.solidAt(dx, dz, dy) {
				return y
			}
		}
	}
	return minY - 1
}
