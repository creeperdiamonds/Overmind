package worldgen

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Dragonfly lists the states of snow layers and seagrass but has no Go type for them, so it would
// treat them as solid, unbreakable and irreplaceable blocks. These types give the server the
// behaviour for the one state of each that the generator places. Hardness and drops are our own
// choices, in line with what Dragonfly gives its snow block and grass. Since round 19 both have
// items: a snow layer item places the thinnest layer (layers do not stack), a seagrass item places
// seagrass into a water source on a solid floor, and shears or silk touch drop seagrass. A snow layer
// does not melt.

var (
	snowLayerHash = block.NextHash()
	seagrassHash  = block.NextHash()
)

// SnowLayer is the thinnest layer of snow, lying on top of a block. Any block can be placed into it.
type SnowLayer struct{}

// EncodeBlock ...
func (SnowLayer) EncodeBlock() (string, map[string]any) {
	return "minecraft:snow_layer", map[string]any{"height": int32(0), "covered_bit": uint8(0)}
}

// Hash ...
func (SnowLayer) Hash() (uint64, uint64) { return snowLayerHash, 0 }

// Model ...
func (SnowLayer) Model() world.BlockModel { return snowLayerModel{} }

// ReplaceableBy ...
func (SnowLayer) ReplaceableBy(world.Block) bool { return true }

// LightDiffusionLevel lets light through, so snow does not darken the column under it.
func (SnowLayer) LightDiffusionLevel() uint8 { return 0 }

// BreakInfo ...
func (SnowLayer) BreakInfo() block.BreakInfo {
	return block.BreakInfo{
		Hardness:        0.1,
		BlastResistance: 0.1,
		Harvestable:     func(item.Tool) bool { return true },
		Effective:       func(t item.Tool) bool { return t.ToolType() == item.TypeShovel },
		Drops: func(item.Tool, []item.Enchantment) []item.Stack {
			return []item.Stack{item.NewStack(item.Snowball{}, 1)}
		},
	}
}

// EncodeItem ...
func (SnowLayer) EncodeItem() (string, int16) { return "minecraft:snow_layer", 0 }

// NeighbourUpdateTick removes the layer when what it lay on has gone.
func (SnowLayer) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	down := pos.Side(cube.FaceDown)
	if !tx.Block(down).Model().FaceSolid(down, cube.FaceUp, tx) {
		tx.SetBlock(pos, nil, nil)
	}
}

// snowLayerModel is a slab of snow one eighth of a block high.
type snowLayerModel struct{}

// BBox ...
func (snowLayerModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 0, 0, 1, 0.125, 1)}
}

// FaceSolid ...
func (snowLayerModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }

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

// BreakInfo breaks at once and drops itself only to shears or silk touch, as in the game.
func (s Seagrass) BreakInfo() block.BreakInfo {
	return block.BreakInfo{
		Harvestable: func(item.Tool) bool { return true },
		Effective:   func(t item.Tool) bool { return t.ToolType() == item.TypeShears },
		Drops: func(t item.Tool, e []item.Enchantment) []item.Stack {
			if t.ToolType() == item.TypeShears || hasSilk(e) {
				return []item.Stack{item.NewStack(s, 1)}
			}
			return nil
		},
	}
}

// EncodeItem ...
func (Seagrass) EncodeItem() (string, int16) { return "minecraft:seagrass", 0 }

// UseOnBlock places seagrass into a still water source with a solid floor: the clicked block's own
// cell when it is that water, else the cell on the clicked face.
func (s Seagrass) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	at := pos
	if !seagrassFits(at, tx) {
		at = pos.Side(face)
	}
	if !seagrassFits(at, tx) {
		return false
	}
	if p, ok := user.(block.Placer); ok {
		p.PlaceBlock(at, s, ctx)
	} else {
		tx.SetBlock(at, s, nil)
		ctx.SubtractFromCount(1)
	}
	return ctx.CountSub > 0
}

// seagrassFits says whether pos is a water source with a block under it whose top is solid.
func seagrassFits(pos cube.Pos, tx *world.Tx) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	w, ok := tx.Block(pos).(block.Water)
	if !ok || w.Depth != 8 || w.Falling {
		return false
	}
	down := pos.Side(cube.FaceDown)
	return tx.Block(down).Model().FaceSolid(down, cube.FaceUp, tx)
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
	world.RegisterBlock(SnowLayer{})
	world.RegisterBlock(Seagrass{})
	world.RegisterItem(SnowLayer{})
	world.RegisterItem(Seagrass{})
})

func init() { registerPlantBlocks() }
