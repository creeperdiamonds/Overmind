package worldgen

import "github.com/df-mc/dragonfly/server/block/cube"

// NearestStronghold is where an eye of ender thrown at pos leads: the stronghold nearest it. The Dragonfly
// fork's worlds ask their generator (world.StrongholdLocator). The strongholds come from the structures
// attached with SetStructures (internal/structures, Task 9); with none attached (no Bedrock install,
// so no structures generate) it reports none, and an eye is not thrown.
func (g *Generator) NearestStronghold(pos cube.Pos) (cube.Pos, bool) {
	if g.structs == nil {
		return cube.Pos{}, false
	}
	return g.structs.NearestStronghold(pos)
}
