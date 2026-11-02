package worldgen

// Layers of rock at the bottom of the world.
const (
	// deepslateTop: minecraft.wiki "Deepslate" says stone is gradually replaced from Y 8
	// down to Y 0, and below that it is deepslate. The chance rises evenly across the band
	// (own tuning: the page does not describe the blend).
	deepslateTop = 8
	// bedrockTop: minecraft.wiki "Bedrock" says bedrock fills the bottom five layers of the
	// Overworld, Y -64 to -60, the lowest layer solid and the next four mostly flat with rare
	// gaps. Here the chance falls evenly from certain at Y -64 to none at Y -60 (own tuning).
	bedrockTop = -60
)

// stoneAt gives the rock of a solid block that is not bedrock: stone, or deepslate below
// the blend band. It depends only on the position, so chunks agree on it.
func (g *Generator) stoneAt(x, y, z int) uint32 {
	if y >= deepslateTop {
		return g.blocks.stone
	}
	if y > 0 && unit(posHash(g.strataSeed, x, y, z)) >= float64(deepslateTop-y)/deepslateTop {
		return g.blocks.stone
	}
	return g.blocks.deepslate
}

// isBedrock says whether a block in the bottom layers is bedrock.
func (g *Generator) isBedrock(x, y, z int) bool {
	if y >= bedrockTop {
		return false
	}
	return unit(posHash(g.strataSeed^0x5bd1e995, x, y, z)) < float64(bedrockTop-y)/(bedrockTop-minY)
}
