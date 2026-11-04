package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Overmind fork: Dragonfly lists the states of snow layers but had no type for them. Overmind's worldgen
// placed its own single layer; snowfall needs all eight, so the type lives here and worldgen uses it.

// SnowLayer is snow lying on top of a block, one to eight layers deep.
type SnowLayer struct {
	// Height is the number of layers less one: 0 to 7.
	Height int
}

var snowLayerHash = NextHash()

// EncodeBlock ...
func (s SnowLayer) EncodeBlock() (string, map[string]any) {
	return "minecraft:snow_layer", map[string]any{"height": int32(s.Height), "covered_bit": uint8(0)}
}

// Hash ...
func (s SnowLayer) Hash() (uint64, uint64) { return snowLayerHash, uint64(s.Height) }

// Model ...
func (s SnowLayer) Model() world.BlockModel { return snowLayerModel{layers: s.Height + 1} }

// ReplaceableBy reports whether a block placed into the snow replaces it: only a single layer is
// replaced (minecraft.wiki "Snow", oldid 3859371).
func (s SnowLayer) ReplaceableBy(world.Block) bool { return s.Height == 0 }

// LightDiffusionLevel lets light through, so snow does not darken the column under it.
func (SnowLayer) LightDiffusionLevel() uint8 { return 0 }

// BreakInfo drops Bedrock's snowballs for the layers: 1 for 1 to 3 layers, 2 for 4 or 5, 3 for 6 or 7
// and 4 for 8 ("Snow", oldid 3859371). DropsIn gives Java's in a Java world.
func (s SnowLayer) BreakInfo() BreakInfo {
	return BreakInfo{
		Hardness:        0.1,
		BlastResistance: 0.1,
		Harvestable:     func(item.Tool) bool { return true },
		Effective:       func(t item.Tool) bool { return t.ToolType() == item.TypeShovel },
		Drops: func(item.Tool, []item.Enchantment) []item.Stack {
			n := [8]int{1, 1, 1, 2, 2, 3, 3, 4}[s.Height]
			return []item.Stack{item.NewStack(item.Snowball{}, n)}
		},
	}
}

// DropsIn gives Java's snowballs in a Java world (world.Mechanics.JavaWeather, Overjava), 1 a layer
// ("Snow", oldid 3859371), and Bedrock's from BreakInfo otherwise.
func (s SnowLayer) DropsIn(tx *world.Tx, t item.Tool, e []item.Enchantment) []item.Stack {
	if tx.World().Mechanics().JavaWeather {
		return []item.Stack{item.NewStack(item.Snowball{}, s.Height+1)}
	}
	return s.BreakInfo().Drops(t, e)
}

// NeighbourUpdateTick removes the snow when what it lay on has gone.
func (SnowLayer) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	down := pos.Side(cube.FaceDown)
	if !canHoldSnow(tx.Block(down), down, tx) {
		tx.SetBlock(pos, nil, nil)
	}
}

// canHoldSnow reports whether snow may lie on b at pos: a full top face, or a full stack of snow, but not
// on some blocks, by edition.
//   - Java 26.1 (world.Mechanics.JavaWeather, Overjava): not ice, packed ice or a barrier, the block tag
//     cannot_support_snow_layer (minecraft.wiki "Block tag (Java Edition)": packed ice is in it until Java
//     26.4); and always on soul sand and mud, from support_override_snow_layer (its honey block is not in
//     the fork). Blue ice is in neither, so it holds snow.
//   - Bedrock: not ice ("Snow": "a solid block that is not ice"), so on packed ice. The wiki says nothing
//     of blue ice on Bedrock; it is kept off it, as before.
func canHoldSnow(b world.Block, pos cube.Pos, tx *world.Tx) bool {
	java := tx.World().Mechanics().JavaWeather
	switch s := b.(type) {
	case Ice:
		return false
	case PackedIce, Barrier:
		if java {
			return false
		}
	case BlueIce:
		if !java {
			return false
		}
	case SoulSand, Mud:
		if java {
			return true
		}
	case SnowLayer:
		return s.Height == 7
	}
	return b.Model().FaceSolid(pos, cube.FaceUp, tx)
}

// snowLayerModel is a slab of snow an eighth of a block high per layer.
type snowLayerModel struct{ layers int }

// BBox ...
func (m snowLayerModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 0, 0, 1, float64(m.layers)/8, 1)}
}

// FaceSolid ...
func (m snowLayerModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return m.layers == 8
}

func init() {
	for h := range 8 {
		world.RegisterBlock(SnowLayer{Height: h})
	}
}
