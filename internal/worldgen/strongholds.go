package worldgen

import "github.com/df-mc/dragonfly/server/block/cube"

// NearestStronghold is where an eye of ender thrown at pos leads: the stronghold nearest it. The Dragonfly
// fork's worlds ask their generator (world.StrongholdLocator). There are none yet, so it reports none, and
// an eye is not thrown.
//
// TODO(Task 9, strongholds): return the nearest stronghold's staircase chunk once strongholds generate.
func (g *Generator) NearestStronghold(cube.Pos) (cube.Pos, bool) { return cube.Pos{}, false }
