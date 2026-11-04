package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
)

// Overmind fork: what the weather check of a chunk tick does at the top of a column (world.RegisterPrecipitation).
// Both editions (minecraft.wiki "Tick", oldid 3868751; "Ice", oldid 3877975; "Snow", oldid 3859371):
//   - Water freezes. A water source on top of its column turns to ice where the temperature is under 0.15
//     and the block light under 10, if a block beside it is not water. A waterlogged block counts as water
//     on Java, and not on Bedrock.
//   - Snow falls. While it snows on the cell above the column's top (the temperature there at most 0.15)
//     and the block light there is under 10, a layer of snow is laid on a block that can hold it, or a
//     layer added to the snow there. Java lays one layer at most (max_snow_accumulation_height defaults to
//     1). Bedrock builds up to a biome's own height: its snow accumulation's upper value in eighths of a
//     block, 1 to 12 layers ("Biome", oldid 3878263; "Snow": "anywhere from 1-12 layers ... depending on the
//     biome"), held here to 8, one full block.
// Cauldrons would fill with rain or snow, but the fork has no cauldron.

func init() { world.RegisterPrecipitation(precipitate) }

func precipitate(tx *world.Tx, top, above cube.Pos) {
	java := tx.World().Mechanics().JavaWeather
	if freezes(tx, top, java) {
		tx.SetBlock(top, Ice{}, nil)
	}
	if !tx.SnowingAt(above) || tx.BlockLight(above) >= 10 {
		return
	}
	limit := 1
	if !java {
		limit = bedrockSnowLayers(tx.Biome(above))
	}
	switch b := tx.Block(above).(type) {
	case Air:
		if _, snow := tx.Block(top).(SnowLayer); !snow && canHoldSnow(tx.Block(top), top, tx) {
			tx.SetBlock(above, SnowLayer{}, nil)
		}
	case SnowLayer:
		if b.Height+1 < limit {
			b.Height++
			tx.SetBlock(above, b, nil)
		}
	}
}

// freezes reports whether the water at pos, the top of its column, turns to ice.
func freezes(tx *world.Tx, pos cube.Pos, java bool) bool {
	if w, ok := tx.Block(pos).(Water); !ok || w.Depth != 8 || w.Falling {
		return false
	}
	if tx.Temperature(pos) >= 0.15 || tx.BlockLight(pos) >= 10 {
		return false
	}
	for _, f := range cube.HorizontalFaces() {
		n := pos.Side(f)
		if java {
			if l, ok := tx.Liquid(n); !ok || l.LiquidType() != "water" {
				return true
			}
		} else if _, water := tx.Block(n).(Water); !water {
			return true
		}
	}
	return false
}

// bedrockSnowLayers is how many layers snowfall builds in a biome on Bedrock: the upper value of the biome's
// snow accumulation times 8 ("Biome", oldid 3878263, "List of biome climates"), at most 8.
func bedrockSnowLayers(b world.Biome) int {
	switch b.(type) {
	case biome.SnowyPlains: // 0.25-1.0
		return 8
	case biome.IceSpikes: // 0.5-1.5: 12 layers, held to one block
		return 8
	case biome.SnowySlopes, biome.SnowyTaiga: // 0.125-0.5
		return 4
	case biome.WindsweptHills, biome.StonyShore, // 0.0-0.25
		biome.SnowyBeach, biome.FrozenRiver, biome.FrozenOcean, biome.DeepFrozenOcean, // 0.125-0.25
		biome.Grove, biome.FrozenPeaks, biome.JaggedPeaks: // 0.125-0.25
		return 2
	}
	return 1 // 0.0-0.125, every other biome
}
