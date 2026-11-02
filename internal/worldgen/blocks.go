package worldgen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

// blocks holds the runtime IDs of the blocks the generator places, looked up once.
type blocks struct {
	air, stone, water uint32
}

// newBlocks uses Dragonfly's default block registry. A generator can be built before the
// server config finalizes that registry, and looking up an ID then would panic.
// Finalize is safe to repeat, but it does stop later RegisterBlock calls.
func newBlocks() blocks {
	world.DefaultBlockRegistry.Finalize()
	return blocks{
		air:   world.BlockRuntimeID(block.Air{}),
		stone: world.BlockRuntimeID(block.Stone{}),
		water: world.BlockRuntimeID(block.Water{Still: true, Depth: 8}),
	}
}
