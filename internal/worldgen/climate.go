package worldgen

import "math"

// Climate holds the values biome choice and terrain shape read at one column, exactly as the game
// computes them (Mojang's algorithm, per the owner's decision; notes section 6). They are not clamped:
// the game's run past 1 in places.
type Climate struct {
	Temperature, Humidity, Continentalness, Erosion, Weirdness float64
}

// climateNoises holds the noises of the climate and the domain warp.
type climateNoises struct {
	temperature, humidity, continentalness, erosion, weirdness, shift *normalNoise
}

func newClimateNoises(w seedFactory) climateNoises {
	return climateNoises{
		temperature:     newNamedNoise(w, "temperature"),
		humidity:        newNamedNoise(w, "vegetation"),
		continentalness: newNamedNoise(w, "continentalness"),
		erosion:         newNamedNoise(w, "erosion"),
		weirdness:       newNamedNoise(w, "ridge"),
		shift:           newNamedNoise(w, "offset"),
	}
}

func (c *climateNoises) shiftX(bx, bz float64) float64 {
	return float64(c.shift.at(float64(bx*0.25), 0, float64(bz*0.25)) * 4.0)
}

// shiftZ reads its noise with z, x, 0, as the game does.
func (c *climateNoises) shiftZ(bx, bz float64) float64 {
	return float64(c.shift.at(float64(bz*0.25), float64(bx*0.25), 0) * 4.0)
}

// warped returns a column's climate coordinates: every climate noise reads the same shifted point.
func (g *Generator) warped(x, z int) (qx, qz float64) {
	bx, bz := float64(x), float64(z)
	return float64(bx*0.25) + g.climate.shiftX(bx, bz), float64(bz*0.25) + g.climate.shiftZ(bx, bz)
}

// shapeClimate returns the three climate values terrain shape reads, at warped coordinates.
func (g *Generator) shapeClimate(qx, qz float64) (continentalness, erosion, weirdness float64) {
	return g.climate.continentalness.at(qx, 0, qz), g.climate.erosion.at(qx, 0, qz), g.climate.weirdness.at(qx, 0, qz)
}

// Climate returns the climate values at a column.
func (g *Generator) Climate(x, z int) Climate {
	qx, qz := g.warped(x, z)
	c, e, w := g.shapeClimate(qx, qz)
	return Climate{
		Temperature:     g.climate.temperature.at(qx, 0, qz),
		Humidity:        g.climate.humidity.at(qx, 0, qz),
		Continentalness: c,
		Erosion:         e,
		Weirdness:       w,
	}
}

// peaksAndValleys folds weirdness. A river valley is where this is near -1.
func peaksAndValleys(weirdness float64) float64 {
	return float64((math.Abs(math.Abs(weirdness)-0.6666666666666666) - 0.3333333333333333) * -3.0)
}
