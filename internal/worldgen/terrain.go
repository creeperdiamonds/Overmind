package worldgen

// Terrain shape and the density function follow Mojang's algorithm exactly, per the owner's decision,
// in our own Go from the study's notes (NOTES-exact-seed.md sections 6 to 9). Density is worked out
// on a coarse grid and blended between grid points: cells of 4 x 8 x 4 blocks.
const (
	cellXZ      = 4
	cellY       = 8
	columnNodes = (maxY+1-minY)/cellY + 1
)

func quarterNeg(v float64) float64 {
	if v > 0 {
		return v
	}
	return float64(v * 0.25)
}

func halfNeg(v float64) float64 {
	if v > 0 {
		return v
	}
	return float64(v * 0.5)
}

// yGrad is the game's clamped map of a block Y onto a value range.
func yGrad(y int, y0, y1, v0, v1 float64) float64 {
	t := (float64(y) - y0) / (y1 - y0)
	if t < 0 {
		return v0
	}
	if t > 1 {
		return v1
	}
	return lerp(v0, v1, t)
}

// offsetConstant is -0.50375 as a single, widened.
const offsetConstant = float64(float32(-0.50375))

// columnShape is what the splines say about a column.
type columnShape struct {
	offset, factor float64
	jag            float64 // Jaggedness times its halved noise, added to the depth.
	erosion        float64
}

func coordsOf(c, e, w float64) splineCoords {
	return splineCoords{coordC: float32(c), coordE: float32(e), coordW: float32(w), coordPV: float32(peaksAndValleys(w))}
}

func offsetFrom(in *splineCoords) float64 { return offsetConstant + float64(offsetSpline.eval(in)) }

func factorFrom(in *splineCoords) float64 {
	return float64((float64(factorSpline.eval(in))+(-10.0))*1.0) + 10.0
}

// surfaceShape is a column's offset and factor, with no jagged noise. That is all the preliminary
// surface and the biome depth need.
func (g *Generator) surfaceShape(x, z int) columnShape {
	qx, qz := g.warped(x, z)
	c, e, w := g.shapeClimate(qx, qz)
	in := coordsOf(c, e, w)
	return columnShape{offset: offsetFrom(&in), factor: factorFrom(&in), erosion: e}
}

// shapeAt is a column's full shape. The jagged noise, 32 octaves, is read only where the jaggedness
// is not zero; a product with zero adds nothing.
func (g *Generator) shapeAt(x, z int) columnShape {
	qx, qz := g.warped(x, z)
	c, e, w := g.shapeClimate(qx, qz)
	in := coordsOf(c, e, w)
	s := columnShape{offset: offsetFrom(&in), factor: factorFrom(&in), erosion: e}
	if j := float64(jaggedSpline.eval(&in)); j != 0 {
		n := g.caves.jagged.at(float64(float64(x)*1500.0), 0, float64(float64(z)*1500.0))
		s.jag = float64(j * halfNeg(n))
	}
	return s
}

// depthAt is the distance under the nominal ground, in the game's units.
func depthAt(y int, offset float64) float64 {
	return yGrad(y, -64, 320, 1.5, -1.5) + offset
}

// nominalGround is the Y where the depth is zero: the ground from the offset spline alone.
func nominalGround(offset float64) float64 {
	return float64(128*offset) + 128
}

// ramp is the part of the sloped density without the 3D noise.
func (s *columnShape) ramp(y int) float64 {
	return float64(4.0 * quarterNeg(float64((depthAt(y, s.offset)+s.jag)*s.factor)))
}

// slopedAt is the ground before caves.
func (g *Generator) slopedAt(s *columnShape, x, y, z int) float64 {
	return s.ramp(y) + g.caves.base3D.at(x, y, z)
}

// slides fades density at the top and bottom of the world.
func slides(v float64, y int) float64 {
	s1 := float64(yGrad(y, 240, 256, 1, 0)*(v+0.078125)) + (-0.078125)
	return float64(yGrad(y, -64, -40, 0, 1)*(s1+(-0.1171875))) + 0.1171875
}

// squeeze eases the density after blending.
func squeeze(v float64) float64 {
	c := clamp(v, -1, 1)
	return c/2.0 - float64(float64(c*c)*c)/24.0
}

// Below this sloped value nothing can bring a corner above zero; see certainlyAir.
func (s *columnShape) certainlyAir(y int) bool {
	return y >= 256 || s.ramp(y)+base3DLimit <= 0
}
