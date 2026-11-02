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
	bedrock, deepslate, lava               uint32
	tuff, granite, diorite, andesite       uint32
	coarseDirt, packedIce, ice, calcite    uint32
	mud, rawIron, rawCopper                uint32
	amethyst, smoothBasalt                 uint32
	// veins holds the blocks the vein pass places, by veinBlock: the stone variant first,
	// then the deepslate variant. The rock patches have the same block in both.
	veins [numVeinBlocks][2]uint32
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
	ore := func(make func(block.OreType) world.Block) [2]uint32 {
		return [2]uint32{id(make(block.StoneOre())), id(make(block.DeepslateOre()))}
	}
	rock := func(b world.Block) [2]uint32 { return [2]uint32{id(b), id(b)} }
	b := blocks{
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
		bedrock:      id(block.Bedrock{}),
		deepslate:    id(block.Deepslate{}),
		lava:         id(block.Lava{Still: true, Depth: 8}),
		tuff:         id(block.Tuff{}),
		granite:      id(block.Granite{}),
		diorite:      id(block.Diorite{}),
		andesite:     id(block.Andesite{}),
		coarseDirt:   id(block.Dirt{Coarse: true}),
		packedIce:    id(block.PackedIce{}),
		ice:          id(block.Ice{}),
		calcite:      id(block.Calcite{}),
		mud:          id(block.Mud{}),
		rawIron:      id(block.RawIron{}),
		amethyst:     id(block.Amethyst{}),
		smoothBasalt: id(block.SmoothBasalt{}),
		rawCopper:    id(block.RawCopper{}),
		terracotta: []uint32{
			id(block.Terracotta{}), stained(item.ColourOrange()), stained(item.ColourYellow()),
			stained(item.ColourBrown()), stained(item.ColourRed()), stained(item.ColourWhite()),
			stained(item.ColourLightGrey()),
		},
	}
	b.veins[veinCoal] = ore(func(t block.OreType) world.Block { return block.CoalOre{Type: t} })
	b.veins[veinIron] = ore(func(t block.OreType) world.Block { return block.IronOre{Type: t} })
	b.veins[veinCopper] = ore(func(t block.OreType) world.Block { return block.CopperOre{Type: t} })
	b.veins[veinGold] = ore(func(t block.OreType) world.Block { return block.GoldOre{Type: t} })
	b.veins[veinRedstone] = ore(func(t block.OreType) world.Block { return block.RedstoneOre{Type: t} })
	b.veins[veinLapis] = ore(func(t block.OreType) world.Block { return block.LapisOre{Type: t} })
	b.veins[veinDiamond] = ore(func(t block.OreType) world.Block { return block.DiamondOre{Type: t} })
	b.veins[veinEmerald] = ore(func(t block.OreType) world.Block { return block.EmeraldOre{Type: t} })
	b.veins[veinGranite] = rock(block.Granite{})
	b.veins[veinDiorite] = rock(block.Diorite{})
	b.veins[veinAndesite] = rock(block.Andesite{})
	b.veins[veinTuff] = rock(block.Tuff{})
	b.veins[veinDirt] = rock(block.Dirt{})
	b.veins[veinGravel] = rock(block.Gravel{})
	b.veins[veinClay] = rock(block.Clay{})
	b.veins[veinInfested] = [2]uint32{id(block.InfestedStone{}), id(block.InfestedDeepslate{})}
	return b
}
