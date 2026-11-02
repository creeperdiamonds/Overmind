package worldgen

// Layers of rock at the bottom of the world.
const (
	// deepslateTop: deepslate is certain at Y 0, falling evenly to none at Y 8, from the study.
	deepslateTop = 8
	// bedrockTop: certain at Y -64, falling evenly to none at Y -59 (1.0, 0.8, 0.6, 0.4, 0.2
	// at Y -60), from the study.
	bedrockTop = -59
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
