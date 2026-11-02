package worldgen

import "math"

// Climate holds the values biome choice and terrain shape read at one column. Ranges
// come from minecraft.wiki "World generation": continentalness runs from -1.2 to 1,
// the others from -1 to 1.
type Climate struct {
	Temperature, Humidity, Continentalness, Erosion, Weirdness float64
	// River is a separate noise whose zero line is the course of a river.
	River float64
}

// climateNoises holds one fractal per climate value. The wiki publishes no noise
// settings, so wavelengths and octaves here are our own tuning, checked on the map:
// continents a few thousand blocks across, valleys a few hundred blocks apart.
type climateNoises struct {
	temperature, humidity, continentalness, erosion, weirdness, river *fractal
}

func newClimateNoises(seed int64) climateNoises {
	sub := func(id uint64) uint64 { return mix64(uint64(seed) ^ mix64(id)) }
	return climateNoises{
		temperature:     newFractal(sub(1), 3000, 3000, 3),
		humidity:        newFractal(sub(2), 2400, 2400, 3),
		continentalness: newFractal(sub(3), 2200, 2200, 3),
		erosion:         newFractal(sub(4), 1400, 1400, 3),
		weirdness:       newFractal(sub(5), 1600, 1600, 2),
		// Own tuning: long wavelength and few octaves, so the zero line is a smooth
		// course and neighbouring courses lie several hundred blocks apart.
		river: newFractal(sub(8), 1600, 1600, 2),
	}
}

// Climate returns the climate values at a column.
func (g *Generator) Climate(x, z int) Climate {
	fx, fz := float64(x), float64(z)
	return Climate{
		Temperature:     clamp(g.climate.temperature.at(fx, 0, fz), -1, 1),
		Humidity:        clamp(g.climate.humidity.at(fx, 0, fz), -1, 1),
		Continentalness: g.continentalness(fx, fz),
		Erosion:         g.erosion(fx, fz),
		Weirdness:       g.weirdness(fx, fz),
		River:           g.riverNoise(fx, fz),
	}
}

// continentalnessShift lowers continentalness a little. Own tuning: without it, 16 of
// 100 seeds had under 25% ocean across 4096x4096 blocks.
const continentalnessShift = -0.1

func (g *Generator) continentalness(x, z float64) float64 {
	return clamp(g.climate.continentalness.at(x, 0, z)+continentalnessShift, -1.2, 1)
}

func (g *Generator) erosion(x, z float64) float64 {
	return clamp(g.climate.erosion.at(x, 0, z), -1, 1)
}

func (g *Generator) weirdness(x, z float64) float64 {
	return clamp(g.climate.weirdness.at(x, 0, z), -1, 1)
}

func (g *Generator) riverNoise(x, z float64) float64 {
	return clamp(g.climate.river.at(x, 0, z), -1, 1)
}

// riverHalfWidth is how far from the zero line of the river noise a river reaches, in
// noise units; about 8 blocks on each side. Own tuning: the wiki gives no river layout.
const riverHalfWidth = 0.012

// terrainPV is the peaks-and-valleys value the height spline reads. Along a river
// course it is pulled down to the valley floor, so the river has a bed to lie in. The
// pull fades out over three times the river's half width.
func (g *Generator) terrainPV(x, z float64) float64 {
	pv := peaksAndValleys(g.weirdness(x, z))
	f := math.Abs(g.riverNoise(x, z)) / (3 * riverHalfWidth)
	if f >= 1 {
		return pv
	}
	return lerp(-1, pv, f*f*(3-2*f))
}

// peaksAndValleys is the wiki's formula, from minecraft.wiki "World generation":
// PV = 1 - |3*|weirdness| - 2|.
func peaksAndValleys(weirdness float64) float64 {
	return 1 - math.Abs(3*math.Abs(weirdness)-2)
}
