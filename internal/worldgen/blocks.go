package worldgen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// blocks holds the runtime IDs of the blocks the generator places, looked up once.
type blocks struct {
	air, stone, water                      uint32
	grass, dirt, podzol, mycelium          uint32
	sand, redSand, sandstone, redSandstone uint32
	gravel, clay, snow                     uint32
	// terracotta holds the badlands band colours, from the plain block to the stained ones.
	terracotta []uint32
}

// newBlocks uses Dragonfly's default block registry. A generator can be built before the
// server config finalizes that registry, and looking up an ID then would panic.
// Finalize is safe to repeat, but it does stop later RegisterBlock calls.
func newBlocks() blocks {
	world.DefaultBlockRegistry.Finalize()
	id := func(b world.Block) uint32 { return world.BlockRuntimeID(b) }
	stained := func(c item.Colour) uint32 { return id(block.StainedTerracotta{Colour: c}) }
	return blocks{
		air:          id(block.Air{}),
		stone:        id(block.Stone{}),
		water:        id(block.Water{Still: true, Depth: 8}),
		grass:        id(block.Grass{}),
		dirt:         id(block.Dirt{}),
		podzol:       id(block.Podzol{}),
		mycelium:     id(block.Mycelium{}),
		sand:         id(block.Sand{}),
		redSand:      id(block.Sand{Red: true}),
		sandstone:    id(block.Sandstone{}),
		redSandstone: id(block.Sandstone{Red: true}),
		gravel:       id(block.Gravel{}),
		clay:         id(block.Clay{}),
		snow:         id(block.Snow{}),
		terracotta: []uint32{
			id(block.Terracotta{}), stained(item.ColourOrange()), stained(item.ColourYellow()),
			stained(item.ColourBrown()), stained(item.ColourRed()), stained(item.ColourWhite()),
			stained(item.ColourLightGrey()),
		},
	}
}
