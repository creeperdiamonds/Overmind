package worldgen

import "strings"

// Bedrock's freeze and snow pass (NOTES-bedrock-exact R6, S4; the study's p6freeze.py), for the Bedrock
// profile: after a chunk's passes, each column, x outer and z inner, takes the first cell above its
// top blocking block (plants, vines and snow do not block); ice goes on water below it where the
// temperature there is under 0.15, else a snow layer goes on that cell if it is air, the temperature is
// at most 0.15 and the block below may hold snow.
//
// What is read from the code (S4): the temperature, the biome's base temperature less the altitude term
// above Y 80 (Biome::getTemperature 0xcd539e0) from Random(1234)'s simplex, and the snow test
// (shouldSnow). What is the study's model, not read: the ice rule (Java's, as S4 says), the two block
// lists below (from p6freeze.py), and the column walk. The light test (light < 10) is not applied, and
// the frozen-ocean modifier is not read.

var (
	// bnNoBlock and bnNoSnow are the study's model (p6freeze.py), not the code's lists.
	bnNoBlock = bnameSetOf("air short_grass tall_grass fern large_fern vine snow_layer poppy dandelion deadbush sweet_berry_bush leaf_litter wildflowers brown_mushroom red_mushroom glow_lichen")
	bnNoSnow  = bnameSetOf("ice blue_ice barrier water lava")
)

// temperatureNoise is Random(1234)'s one-octave simplex, read at (x, z) * 0.125 with no offsets.
var temperatureNoise = oneOctaveSimplex(1234)

// bedrockFreezeTemperature is the temperature the freeze pass reads at a cell of a biome.
func (d *decoration) bedrockFreezeTemperature(biome uint8, x, y, z int) float32 {
	t := d.g.bdata.surfaceBiomes[biome].temp
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
			t := d.bedrockFreezeTemperature(d.storedBiome(x, p, z), x, p, z)
			below := d.block(x, y, z)
			if bWaterMat(below) && t < 0.15 {
				d.set(x, y, z, sIce)
				continue
			}
			if t <= 0.15 && d.block(x, p, z).isAir() && !bnNoSnow.has(bname(below)) {
				d.set(x, p, z, sSnowLayer)
				if strings.Contains(below.name(), "snowy=false") {
					d.set(x, y, z, withProp(below, "snowy", "true"))
				}
			}
		}
	}
}
