package worldgen

import "strings"

// Bedrock's freeze and snow pass (NOTES-bedrock-exact R6, S4; NOTES-leftovers L17), for the Bedrock
// profile: after a chunk's passes, each column, x outer and z inner, takes the first cell p above its
// top blocking block (plants, vines and snow do not block); ice goes on a water source at p - 1 where
// the temperature there is at most 0.15, else a snow layer goes on p if it is air, the temperature at p
// is at most 0.15 and the block below may hold snow.
//
// What is read from the code: the temperature (Biome::getTemperature 0xcd539e0, S4): the biome's base
// temperature, the frozen-ocean modifier for a biome with minecraft:partially_frozen (L17, T16), less the
// altitude term above Y 80 from Random(1234)'s simplex; the snow test (shouldSnow, S4); the ice test
// (shouldFreeze 0xcd9ff40, L17): the biome and temperature of the water cell itself, t <= 0.15, a water
// source, and no neighbour test (checkNeighbours is false in the post-process pass). Validated by the
// study on four BDS windows (frozen_ocean_4 open water 9899 of 9899 columns predicted). What is the
// study's model, not read: the two block lists below (from p6freeze.py) and the column walk. The light
// test (light < 10) is not applied (ASSUMED block light, never binding at generation).

var (
	// bnNoBlock and bnNoSnow are the study's model (p6freeze.py), not the code's lists.
	bnNoBlock = bnameSetOf("air short_grass tall_grass fern large_fern vine snow_layer poppy dandelion deadbush sweet_berry_bush leaf_litter wildflowers brown_mushroom red_mushroom glow_lichen")
	bnNoSnow  = bnameSetOf("ice blue_ice barrier water lava")
)

// temperatureNoise is Random(1234)'s one-octave simplex, read at (x, z) * 0.125 with no offsets.
var temperatureNoise = oneOctaveSimplex(1234)

// bedrockFreezeTemperature is the temperature the freeze pass reads at a cell of a biome.
func (d *decoration) bedrockFreezeTemperature(biome uint8, x, y, z int) float32 {
	return d.g.bedrockFreezeTemperature(biome, x, y, z)
}

func (g *Generator) bedrockFreezeTemperature(biome uint8, x, y, z int) float32 {
	sb := &g.bdata.surfaceBiomes[biome]
	t := sb.temp
	if sb.partiallyFrozen && g.bsurf != nil {
		t = g.bsurf.frozen.temperature(t, x, z) // L17: before the altitude term, as getTemperature does.
	}
	if y > 80 {
		v := temperatureNoise.at2(float32(x)*0.125, float32(z)*0.125) * 70
		// BDS's (char) cast (S4): the value wraps into -128..127 on purpose, as the code's byte does.
		n := int8(int32(float32(v*8) * 8))
		t += float32(float32(float32(y)+float32(n)*0.125)-80) * -0.05 / 40
	}
	return t
}

// bedrockFreeze runs the pass over a source chunk.
func (d *decoration) bedrockFreeze(cx, cz int) {
	for lx := range 16 {
		for lz := range 16 {
			x, z := cx*16+lx, cz*16+lz
			y := d.height(hmWorldSurface, x, z) - 1
			for y > minY && bnNoBlock.has(bname(d.block(x, y, z))) {
				y--
			}
			p := y + 1
			if p > maxY {
				continue
			}
			below := d.block(x, y, z)
			// shouldFreeze (L17): the water cell's own biome and temperature; a source (liquid_depth 0: our
			// level 0 water, or the surface builder's plain water, which has no level).
			if bWaterMat(below) && (below == sWater || !strings.Contains(below.name(), "level=")) && d.bedrockFreezeTemperature(d.storedBiome(x, y, z), x, y, z) <= 0.15 {
				d.set(x, y, z, sIce)
				continue // shouldSnow runs too in BDS, but refuses a cell over ice (L17: 0 of 7800 ice columns snowed).
			}
			t := d.bedrockFreezeTemperature(d.storedBiome(x, p, z), x, p, z)
			if t <= 0.15 && d.block(x, p, z).isAir() && !bnNoSnow.has(bname(below)) {
				d.set(x, p, z, sSnowLayer)
				if strings.Contains(below.name(), "snowy=false") {
					d.set(x, y, z, withProp(below, "snowy", "true"))
				}
			}
		}
	}
}
