package worldgen

import "math"

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

// pvKnots are the peaks-and-valleys places of every height row.
var pvKnots = [5]float64{-1, -0.4, 0, 0.4, 1}

// pvRow builds one height row from the ground Y at the PV knots. The Y values come from
// studying the game. Its slopes were not transcribed; ours are worked out so that a row
// never overshoots between knots: the harmonic mean of the slopes on each side, or zero
// where the row turns or lies flat. This keeps mountain rows nearly straight in PV and
// flat rows flat, which is how the notes describe the game's rows.
func pvRow(y [5]float64) *spline {
	s := &spline{input: inputPV, knots: make([]knot, 5)}
	sec := func(i int) float64 { return (y[i+1] - y[i]) / (pvKnots[i+1] - pvKnots[i]) }
	for i := range y {
		k := knot{at: pvKnots[i], value: y[i]}
		switch i {
		case 0:
			k.slope = sec(0)
		case 4:
			k.slope = sec(3)
		default:
			if a, b := sec(i-1), sec(i); a*b > 0 {
				k.slope = 2 * a * b / (a + b)
			}
		}
		s.knots[i] = k
	}
	return s
}

// heightRows is one column of the study's height table: erosion knot, then the row.
type heightRow struct {
	erosion float64
	y       [5]float64
}

func byErosion(rows []heightRow) *spline {
	s := &spline{input: inputErosion}
	for _, r := range rows {
		s.knots = append(s.knots, knot{at: r.erosion, child: pvRow(r.y)})
	}
	return s
}

// heightSpline gives the nominal ground Y from continentalness, erosion and PV. The knot
// places and heights come from studying Java 26.1, where the ground lies at
// Y = 63.5 + 128 * offset; all numbers here are in blocks. Rivers need nothing else: a
// valley is PV near -1, which sits at Y 44 to 57 when erosion is high, so it cuts below
// sea level. Between erosion and continentalness knots the slope is zero.
var heightSpline = func() *spline {
	mountainLow := []heightRow{
		{-0.85, [5]float64{52, 82, 102, 122, 152}},
		{-0.7, [5]float64{49, 78, 97, 116, 145}},
		{-0.4, [5]float64{35, 64, 64, 64, 76}},
	}
	low := byErosion(append(mountainLow[:3:3],
		heightRow{-0.35, [5]float64{32, 70, 70, 70, 71}},
		heightRow{-0.1, [5]float64{51, 64, 64, 70, 71}},
		heightRow{0.2, [5]float64{51, 65, 65, 67, 76}},
		heightRow{0.7, [5]float64{61, 60, 60, 67, 76}},
	))
	beachTable := byErosion(append(mountainLow[:3:3],
		heightRow{-0.35, [5]float64{25, 70, 70, 70, 71}},
		heightRow{-0.1, [5]float64{44, 64, 64, 70, 71}},
		heightRow{0.2, [5]float64{44, 64, 64, 64, 64}},
		heightRow{0.7, [5]float64{61, 60, 60, 64, 64}},
	))
	// The shattered rows are given in the notes only as "51 (or 57) at PV -1, about 85
	// from PV 0 up"; the value at PV -0.4 is our own, halfway up.
	mid := byErosion([]heightRow{
		{-0.85, [5]float64{89, 129, 155, 181, 221}},
		{-0.7, [5]float64{65, 99, 121, 143, 176}},
		{-0.4, [5]float64{53, 83, 103, 123, 153}},
		{-0.35, [5]float64{32, 108, 108, 108, 117}},
		{-0.1, [5]float64{51, 64, 66, 108, 117}},
		{0.2, [5]float64{51, 65, 65, 67, 76}},
		{0.4, [5]float64{51, 65, 65, 67, 76}},
		{0.45, [5]float64{51, 70, 85, 85, 85}},
		{0.55, [5]float64{51, 70, 85, 85, 85}},
		{0.58, [5]float64{51, 65, 65, 67, 76}},
		{0.7, [5]float64{61, 60, 60, 67, 76}},
	})
	high := byErosion([]heightRow{
		{-0.85, [5]float64{108, 152, 182, 211, 255}},
		{-0.7, [5]float64{74, 109, 133, 156, 191}},
		{-0.4, [5]float64{74, 109, 133, 156, 191}},
		{-0.35, [5]float64{38, 128, 128, 128, 140}},
		{-0.1, [5]float64{57, 65, 67, 128, 140}},
		{0.2, [5]float64{57, 65, 65, 67, 76}},
		{0.4, [5]float64{57, 65, 65, 67, 76}},
		{0.45, [5]float64{57, 71, 85, 85, 85}},
		{0.55, [5]float64{57, 71, 85, 85, 85}},
		{0.58, [5]float64{57, 65, 65, 67, 76}},
		{0.7, [5]float64{61, 65, 65, 67, 76}},
	})
	return &spline{input: inputContinentalness, knots: []knot{
		{at: -1.1, value: 69}, // Mushroom islands.
		{at: -1.02, value: 35},
		{at: -0.51, value: 35}, // Deep ocean.
		{at: -0.44, value: 48},
		{at: -0.18, value: 48}, // Ocean.
		{at: -0.16, child: beachTable},
		{at: -0.15, child: beachTable},
		{at: -0.1, child: low},
		{at: 0.25, child: mid},
		{at: 1, child: high},
	}}
}()

// factorRow runs from 6.3 at weirdness -0.2 to base at weirdness 0.2.
func factorRow(base float64) *spline {
	return &spline{input: inputWeirdness, knots: []knot{{at: -0.2, value: 6.3}, {at: 0.2, value: base}}}
}

// factorTable is one erosion table of the factor spline. The structure and the factors
// come from the study's description; how sharply each dip is entered is our own reading,
// because the notes name the values and places but not every knot.
func factorTable(base float64, shatteredBand bool) *spline {
	row := &knot{child: factorRow(base)}
	k := func(at float64) knot { return knot{at: at, child: row.child} }
	dipHigh := &spline{input: inputWeirdness, knots: []knot{{at: -0.05, value: 6.3}, {at: 0.05, value: 2.67}}}
	dipLow := &spline{input: inputWeirdness, knots: []knot{{at: -0.05, value: 2.67}, {at: 0.05, value: base}}}
	knots := []knot{
		k(-0.6), {at: -0.5, child: dipHigh}, k(-0.35), k(-0.25), {at: -0.1, child: dipLow}, k(0.03),
	}
	if shatteredBand {
		// Coast and near inland: factor 0.625 at weirdness above 0.1.
		band := &spline{input: inputWeirdness, knots: []knot{{at: 0, child: row.child}, {at: 0.1, child: &spline{
			input: inputPV, knots: []knot{{at: -0.75, value: base}, {at: -0.69, value: 0.625}}}}}}
		knots = append(knots, k(0.05), k(0.4), knot{at: 0.45, child: band}, knot{at: 0.55, child: band}, k(0.58))
	} else {
		// Far inland: 1.56 at PV 0.7 on ordinary ground, 1.37 from PV -0.15 in the windswept band.
		ground := &spline{input: inputPV, knots: []knot{{at: 0.2, child: row.child}, {at: 0.7, value: 1.56}}}
		band := &spline{input: inputPV, knots: []knot{{at: -0.6, child: row.child}, {at: -0.15, value: 1.37}}}
		knots = append(knots, knot{at: 0.05, child: ground}, knot{at: 0.4, child: ground},
			knot{at: 0.45, child: band}, knot{at: 0.55, child: band}, k(0.58))
	}
	return &spline{input: inputErosion, knots: knots}
}

// factorSpline says how steep the vertical gradient is: a low factor lets the 3D noise
// move the ground a long way. It is almost everywhere 3.95 to 6.3. Values from the study.
var factorSpline = &spline{input: inputContinentalness, knots: []knot{
	{at: -0.19, value: 3.95},
	{at: -0.15, child: factorTable(6.25, true)},
	{at: -0.1, child: factorTable(5.47, true)},
	{at: 0.03, child: factorTable(5.08, true)},
	{at: 0.06, child: factorTable(4.69, false)},
}}

// jaggedRow scales jaggedness with PV: zero up to about PV 0.2, a mid value at 0.45, full
// at 1, scaled 0.63 at negative weirdness and 0.3 at positive. The study gives those
// places and scales; "mid" is ours, 45% of full.
func jaggedRow(table float64) *spline {
	byPV := func(scale float64) *spline {
		full := table * scale
		return &spline{input: inputPV, knots: []knot{{at: 0.2, value: 0}, {at: 0.45, value: full * 0.45}, {at: 1, value: full}}}
	}
	return &spline{input: inputWeirdness, knots: []knot{{at: -0.01, child: byPV(0.63)}, {at: 0.01, child: byPV(0.3)}}}
}

// jaggedSpline gives extra spikes on peaks: zero at continentalness -0.11 and at erosion
// -0.375 or above, using table values 0.5 and 1.
var jaggedSpline = func() *spline {
	byE := func(table float64) *spline {
		return &spline{input: inputErosion, knots: []knot{
			{at: -0.78, child: jaggedRow(table)},
			{at: -0.5775, child: jaggedRow(table * 0.5)},
			{at: -0.375, value: 0},
		}}
	}
	return &spline{input: inputContinentalness, knots: []knot{
		{at: -0.11, value: 0},
		{at: 0.03, child: byE(0.5)},
		{at: 0.65, child: byE(1)},
	}}
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

// squeeze softens large density values, keeping the sign.
func squeeze(v float64) float64 {
	v = clamp(v, -1, 1)
	return v/2 - v*v*v/24
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
	qx, qz := g.warped(x, z)
	c := clamp(g.climate.continentalness.at(qx, 0, qz), -1.2, 1)
	e := clamp(g.climate.erosion.at(qx, 0, qz), -1, 1)
	w := clamp(g.climate.weirdness.at(qx, 0, qz), -1, 1)
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
// the spread (std 0.22) come from the study. Our own limit noises stop at 6 octaves, since
// finer ones fall inside one 4 x 8 x 4 grid cell.
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
const baseSpread = 0.24

// slopedDensity is the ground before caves: 4*quarterNeg(depth*factor), plus the blended
// noise when the point is near enough to the ground for it to matter. The lopsided
// quarterNeg lifts ground 128/factor blocks per unit of noise but lowers it only 32/factor,
// so bumps and overhangs rise from a firm base.
func (g *Generator) slopedDensity(s *columnShape, x, y, z float64) float64 {
	depth := (s.ground-0.5-y)/128 + s.jag
	sloped := 4 * quarterNeg(depth*s.factor)
	if sloped < -maxBase || sloped > maxBase+slopedKeepNoise {
		return sloped
	}
	return sloped + g.base3D(x, y, z)
}

// slopedKeepNoise: above this, caves no longer read the sloped value (see caves.go), so
// the noise can be left out without changing anything but its value deep underground.
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

// finishDensity applies the last steps: the slides, the 0.64 scale and the squeeze.
func finishDensity(v, y float64) float64 {
	return squeeze(0.64 * slide(v, y))
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

// terrainHeight returns the Y of the highest solid block of the terrain in a column,
// before caves are carved. Caves can open the top, so use SurfaceHeight for the real
// highest solid block.
func (g *Generator) terrainHeight(x, z int) int {
	return g.highestSolid(x, z, nil)
}

// highestSolid returns the Y of the highest terrain block in a column that is solid and
// not skipped. skip may be nil.
func (g *Generator) highestSolid(x, z int, skip func(y int) bool) int {
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
			if c.solidAt(dx, dz, dy) && (skip == nil || !skip(y)) {
				return y
			}
		}
	}
	return minY - 1
}
