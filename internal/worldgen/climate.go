package worldgen

import "math"

// Climate holds the values biome choice and terrain shape read at one column. Ranges
// come from minecraft.wiki "World generation": continentalness runs from -1.2 to 1,
// the others from -1 to 1.
type Climate struct {
	Temperature, Humidity, Continentalness, Erosion, Weirdness float64
}

// climateNoises holds one fractal per climate value. The wiki publishes no noise
// settings, so wavelengths and octaves here are our own tuning, checked on the map:
// continents a few thousand blocks across, valleys a few hundred blocks apart.
type climateNoises struct {
	temperature, humidity, continentalness, erosion, weirdness *fractal
}

func newClimateNoises(seed int64) climateNoises {
	sub := func(id uint64) uint64 { return mix64(uint64(seed) ^ mix64(id)) }
	return climateNoises{
		temperature:     newFractal(sub(1), 3000, 3000, 4),
		humidity:        newFractal(sub(2), 2400, 2400, 4),
		continentalness: newFractal(sub(3), 1500, 1500, 6),
		erosion:         newFractal(sub(4), 1000, 1000, 5),
		weirdness:       newFractal(sub(5), 500, 500, 4),
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

// peaksAndValleys is the wiki's formula, from minecraft.wiki "World generation":
// PV = 1 - |3*|weirdness| - 2|.
func peaksAndValleys(weirdness float64) float64 {
	return 1 - math.Abs(3*math.Abs(weirdness)-2)
}
