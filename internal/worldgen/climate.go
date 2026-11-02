package worldgen

import "math"

// Climate holds the values biome choice and terrain shape read at one column. The spreads
// and octave lists below come from studying the Java 26.1 generator (see NOTES.md).
type Climate struct {
	Temperature, Humidity, Continentalness, Erosion, Weirdness float64
}

// climateNoises holds one noise per climate value and the two domain-warp noises. First
// octaves and amplitudes come from the study; a noise with first octave -9, read at
// block/4, has a longest wavelength of 2048 blocks. The target spreads are the standard
// deviations measured from the game over 3 seeds of 8192 x 8192 blocks, so that band
// shares come out alike.
type climateNoises struct {
	temperature, humidity, continentalness, erosion, weirdness *layered
	shiftX, shiftZ                                             *layered
}

func newClimateNoises(seed int64) climateNoises {
	sub := func(id uint64) uint64 { return mix64(uint64(seed) ^ mix64(id)) }
	return climateNoises{
		temperature:     newLayered(sub(1), -10, []float64{1.5, 0, 1}, 0.380),
		humidity:        newLayered(sub(2), -8, []float64{1, 1}, 0.252),
		continentalness: newLayered(sub(3), -9, []float64{1, 1, 2, 2, 2, 1, 1, 1, 1}, 0.373),
		erosion:         newLayered(sub(4), -9, []float64{1, 1, 0, 1, 1}, 0.309),
		weirdness:       newLayered(sub(5), -7, []float64{1, 2, 1}, 0.353),
		shiftX:          newLayered(sub(9), -3, []float64{1, 1, 1}, warpSpread),
		shiftZ:          newLayered(sub(10), -3, []float64{1, 1, 1}, warpSpread),
	}
}

// warped returns the climate coordinates of a column in noise units (block/4). The warp
// moves samples about 5 blocks typically and about 25 at most, wiggling every 8 to 32
// blocks, which is what makes biome borders and coasts ragged. All climate noises read
// the same warped point, as in the game.
func (g *Generator) warped(x, z int) (qx, qz float64) {
	bx, bz := float64(x)/4, float64(z)/4
	return bx + g.climate.shiftX.at(bx, 0, bz)*4, bz + g.climate.shiftZ.at(bz, 0, bx)*4
}

// shapeClimate returns the three climate values terrain shape reads, at warped coordinates.
// The clamps are our own bounds, wide enough that the measured spreads rarely reach them.
func (g *Generator) shapeClimate(qx, qz float64) (continentalness, erosion, weirdness float64) {
	return clamp(g.climate.continentalness.at(qx, 0, qz), -1.2, 1),
		clamp(g.climate.erosion.at(qx, 0, qz), -1, 1),
		clamp(g.climate.weirdness.at(qx, 0, qz), -1, 1)
}

// Climate returns the climate values at a column.
func (g *Generator) Climate(x, z int) Climate {
	qx, qz := g.warped(x, z)
	c, e, w := g.shapeClimate(qx, qz)
	return Climate{
		Temperature:     clamp(g.climate.temperature.at(qx, 0, qz), -1, 1),
		Humidity:        clamp(g.climate.humidity.at(qx, 0, qz), -1, 1),
		Continentalness: c,
		Erosion:         e,
		Weirdness:       w,
	}
}

// peaksAndValleys folds weirdness: PV = 1 - |3*|weirdness| - 2|. A river valley is where
// this is near -1, which is where weirdness is near zero.
func peaksAndValleys(weirdness float64) float64 {
	return 1 - math.Abs(3*math.Abs(weirdness)-2)
}

// warpSpread is the spread of the domain-warp noises. The study gives none. 0.312 is tuned by us:
// the spread the ratios in gameSpread give would be 0.284, and with that the chunk edge test
// (TestNoSeamAtChunkEdges) sits on its limit, edge steps p99 4 against 3 inside, with equal means.
const warpSpread = 0.312
