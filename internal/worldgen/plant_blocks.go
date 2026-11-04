package worldgen

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Dragonfly lists the states of seagrass but has no Go type for it, so it would treat it as a solid,
// unbreakable and irreplaceable block. This type gives the server the behaviour for the one state the
// generator places. Hardness and drops are our own choices, in line with what Dragonfly gives its grass.
// There is no item for it, so a player cannot carry one.

var seagrassHash = block.NextHash()

// SnowLayer is snow lying on top of a block. The generator places single layers (SnowLayer{}); the type is
// the Dragonfly fork's (block.SnowLayer), where snowfall adds layers (Task W, third_party/dragonfly/NOTES.md).
type SnowLayer = block.SnowLayer

// Seagrass is a plant on the floor of a body of water. It has no box and can be replaced by any block.
type Seagrass struct{}

// EncodeBlock ...
func (Seagrass) EncodeBlock() (string, map[string]any) {
	return "minecraft:seagrass", map[string]any{"sea_grass_type": "default"}
}

// Hash ...
func (Seagrass) Hash() (uint64, uint64) { return seagrassHash, 0 }

// Model ...
func (Seagrass) Model() world.BlockModel { return model.Empty{} }

// ReplaceableBy ...
func (Seagrass) ReplaceableBy(world.Block) bool { return true }

// LightDiffusionLevel ...
func (Seagrass) LightDiffusionLevel() uint8 { return 0 }

// CanDisplace keeps water in the same place as the plant.
func (Seagrass) CanDisplace(l world.Liquid) bool {
	_, water := l.(block.Water)
	return water
}

// SideClosed ...
func (Seagrass) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo breaks at once and drops nothing.
func (Seagrass) BreakInfo() block.BreakInfo {
	return block.BreakInfo{
		Harvestable: func(item.Tool) bool { return true },
		Effective:   func(item.Tool) bool { return false },
		Drops:       func(item.Tool, []item.Enchantment) []item.Stack { return nil },
	}
}

// NeighbourUpdateTick removes the plant when its floor has gone.
func (Seagrass) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	down := pos.Side(cube.FaceDown)
	if !tx.Block(down).Model().FaceSolid(down, cube.FaceUp, tx) {
		tx.SetBlock(pos, nil, nil)
	}
}

// registerPlantBlocks adds the types to the default registry, once. It has to run before anything
// finalises that registry, so init does it, and so does newBlocks for package variables in tests.
var registerPlantBlocks = sync.OnceFunc(func() {
	world.RegisterBlock(Seagrass{})
})

func init() { registerPlantBlocks() }
