package worldgen

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// dfMap holds the Dragonfly runtime ID of each state.
var (
	dfOnce sync.Once
	dfRT   [maxStates]uint32
	dfDone [maxStates]bool
	dfLock sync.RWMutex
)

// dragonflyBlock gives the Dragonfly block of a state. Air stands in for what has no block: see the
// list of gaps in NOTES.md.
func dragonflyBlock(name string) world.Block {
	switch stateBase(name) {
	case "stone":
		return block.Stone{}
	case "water":
		return block.Water{Still: true, Depth: 8}
	case "lava":
		return block.Lava{Still: true, Depth: 8}
	case "bedrock":
		return block.Bedrock{}
	case "deepslate":
		return block.Deepslate{}
	case "dirt":
		return block.Dirt{}
	case "coarse_dirt":
		return block.Dirt{Coarse: true}
	case "grass_block":
		return block.Grass{}
	case "podzol":
		return block.Podzol{}
	case "mycelium":
		return block.Mycelium{}
	case "mud":
		return block.Mud{}
	case "gravel":
		return block.Gravel{}
	case "sand":
		return block.Sand{}
	case "red_sand":
		return block.Sand{Red: true}
	case "sandstone":
		return block.Sandstone{}
	case "red_sandstone":
		return block.Sandstone{Red: true}
	case "calcite":
		return block.Calcite{}
	case "snow_block", "powder_snow":
		return block.Snow{}
	case "ice":
		return block.Ice{}
	case "packed_ice":
		return block.PackedIce{}
	case "granite":
		return block.Granite{}
	case "tuff":
		return block.Tuff{}
	case "copper_ore":
		return block.CopperOre{Type: block.StoneOre()}
	case "raw_copper_block":
		return block.RawCopper{}
	case "deepslate_iron_ore":
		return block.IronOre{Type: block.DeepslateOre()}
	case "raw_iron_block":
		return block.RawIron{}
	case "terracotta":
		return block.Terracotta{}
	case "orange_terracotta":
		return block.StainedTerracotta{Colour: item.ColourOrange()}
	case "yellow_terracotta":
		return block.StainedTerracotta{Colour: item.ColourYellow()}
	case "brown_terracotta":
		return block.StainedTerracotta{Colour: item.ColourBrown()}
	case "red_terracotta":
		return block.StainedTerracotta{Colour: item.ColourRed()}
	case "white_terracotta":
		return block.StainedTerracotta{Colour: item.ColourWhite()}
	case "light_gray_terracotta":
		return block.StainedTerracotta{Colour: item.ColourLightGrey()}
	}
	return block.Air{}
}

// runtimeOf finds a state's Dragonfly runtime ID.
func runtimeOf(s bs) uint32 {
	dfLock.RLock()
	done, id := dfDone[s], dfRT[s]
	dfLock.RUnlock()
	if done {
		return id
	}
	id = world.BlockRuntimeID(dragonflyBlock(s.name()))
	dfLock.Lock()
	dfRT[s], dfDone[s] = id, true
	dfLock.Unlock()
	return id
}

// writeChunk puts a base's blocks into a Dragonfly chunk.
func (g *Generator) writeChunk(c *chunk.Chunk, b *baseChunk) {
	air := bs(0)
	for i, s := range b.b {
		if s == air {
			continue
		}
		c.SetBlock(uint8(i&15), int16(i>>8)+minY, uint8(i>>4&15), 0, runtimeOf(s))
	}
}
