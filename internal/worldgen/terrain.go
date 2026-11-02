package worldgen

// Density is worked out on a coarse grid and blended between grid points, as
// minecraft.wiki "Density function" describes for `interpolated`. Cell sizes are our
// own tuning; the wiki gives only the multiplier of 4.
const (
	cellXZ      = 4
	cellY       = 8
	columnNodes = (maxY+1-minY)/cellY + 1
)

const (
	inputContinentalness = iota
	inputErosion
	inputPV
)

// spline maps one input to a value through knots, as minecraft.wiki "Density function"
// describes for `spline`. A knot's value can itself be a spline over another input.
// Every knot is flat, which gives wide beaches and plateaus at the knot heights.
type spline struct {
	input int
	knots []knot
}

type knot struct {
	at, value float64
	child     *spline
}

func (k *knot) resolve(in [3]float64) float64 {
	if k.child != nil {
		return k.child.eval(in)
	}
	return k.value
}

func (s *spline) eval(in [3]float64) float64 {
	v := in[s.input]
	last := len(s.knots) - 1
	if v <= s.knots[0].at {
		return s.knots[0].resolve(in)
	}
	if v >= s.knots[last].at {
		return s.knots[last].resolve(in)
	}
	i := 0
	for v >= s.knots[i+1].at {
		i++
	}
	a, b := &s.knots[i], &s.knots[i+1]
	t := (v - a.at) / (b.at - a.at)
	return lerp(a.resolve(in), b.resolve(in), t*t*(3-2*t))
}

// byPV builds the inner spline of the height table. Knot places are the
// peaks-and-valleys bands on minecraft.wiki "World generation". Valley is the valley
// floor, mid the ordinary ground and peak the top of a ridge.
func byPV(valley, mid, peak float64) *spline {
	return &spline{input: inputPV, knots: []knot{
		{at: -1, value: valley},
		{at: -0.85, value: mid - (peak-mid)/4},
		{at: 0.2, value: mid},
		{at: 0.7, value: lerp(mid, peak, 0.75)},
		{at: 1, value: peak},
	}}
}

// byErosion builds a spline of five erosion rows, low erosion first. Knot places are
// erosion level edges on minecraft.wiki "World generation".
func byErosion(rows [5]*spline) *spline {
	return &spline{input: inputErosion, knots: []knot{
		{at: -0.78, child: rows[0]},
		{at: -0.375, child: rows[1]},
		{at: 0.05, child: rows[2]},
		{at: 0.45, child: rows[3]},
		{at: 1, child: rows[4]},
	}}
}

// heightSpline gives the ground height in blocks. Source: minecraft.wiki "World
// generation" for the continentalness band edges and for the rule that continentalness
// and high PV raise ground while erosion flattens it. The wiki does not publish spline
// heights, so the heights below are our own tuning, checked against the map and the
// terrain statistics test.
var heightSpline = &spline{input: inputContinentalness, knots: []knot{
	// Own tuning: mushroom fields (continentalness under -1.05) rise as islands.
	{at: -1.2, value: 68},
	{at: -1.06, value: 64},
	{at: -1.05, value: 30}, // Deep ocean.
	{at: -0.455, value: 38},
	{at: -0.19, value: 54}, // Ocean to coast.
	{at: -0.11, child: byErosion([5]*spline{ // Near-inland.
		byPV(62, 86, 125), byPV(62, 76, 100), byPV(62, 69, 84), byPV(61, 66, 72), byPV(61, 64, 67),
	})},
	{at: 0.03, child: byErosion([5]*spline{ // Mid-inland.
		byPV(80, 118, 180), byPV(62, 94, 135), byPV(61, 76, 100), byPV(61, 70, 80), byPV(60, 66, 72),
	})},
	{at: 0.3, child: byErosion([5]*spline{ // Far-inland starts.
		byPV(120, 165, 240), byPV(80, 118, 175), byPV(62, 86, 118), byPV(61, 74, 88), byPV(60, 68, 76),
	})},
	{at: 1, child: byErosion([5]*spline{ // Far-inland ends.
		byPV(150, 190, 270), byPV(100, 135, 200), byPV(64, 94, 130), byPV(62, 78, 94), byPV(60, 70, 78),
	})},
}}

// roughnessSpline gives how many blocks the 3D noise can move the ground up or down.
// Source for the shape: minecraft.wiki "World generation", where low erosion gives
// rugged terrain. The block counts are our own tuning. Above about 25 blocks the noise
// can fold the ground over itself, which makes overhangs.
var roughnessSpline = &spline{input: inputContinentalness, knots: []knot{
	{at: -0.19, value: 3},
	{at: -0.11, value: 4},
	{at: 0.1, child: &spline{input: inputErosion, knots: []knot{
		{at: -0.78, value: 32},
		{at: -0.375, value: 20},
		{at: 0.05, value: 9},
		{at: 0.45, value: 5},
	}}},
}}

// fillColumn works out density at each grid point of the column at a block position.
// Above 0 is solid, as on minecraft.wiki "World generation". The 3D noise is clamped to
// -1..1, so a point two roughness spans from the ground has a known answer and skips the
// noise. That is what keeps generation fast, and it gives the same value as computing it.
func (g *Generator) fillColumn(x, z int, column *[columnNodes]float64) {
	fx, fz := float64(x), float64(z)
	in := [3]float64{
		inputContinentalness: g.continentalness(fx, fz),
		inputErosion:         g.erosion(fx, fz),
		inputPV:              g.terrainPV(fx, fz),
	}
	roughness := roughnessSpline.eval(in)
	// Cap the ground so that the noise and one cell of blending stay under the build limit.
	ground := min(heightSpline.eval(in), maxY-roughness-cellY)

	for i := range column {
		y := float64(minY + i*cellY)
		switch bias := (ground - y) / roughness; {
		case bias >= 2:
			column[i] = 1
		case bias <= -2:
			column[i] = -1
		default:
			column[i] = clamp(bias+clamp(g.overhang.at(fx, y, fz), -1, 1), -1, 1)
		}
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

// SurfaceHeight returns the Y of the highest solid block in a column. It gives the same
// answer as the generated chunk, without needing the chunk to exist.
func (g *Generator) SurfaceHeight(x, z int) int {
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
			if c.solidAt(dx, dz, dy) {
				return minY + layer*cellY + dy
			}
		}
	}
	return minY - 1
}
