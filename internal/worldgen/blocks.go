package worldgen

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// treeKind names a wood; veinBlock names an ore or rock. They index the table tests read.
type treeKind uint8

const (
	treeNone treeKind = iota
	treeOak
	treeBirch
	treeSpruce
	treePine
	treeDarkOak
	treeJungle
	treeAcacia
	treeCherry
	treePaleOak
	treeMangrove
	numTreeKinds
)

type veinBlock uint8

const (
	veinCoal veinBlock = iota
	veinIron
	veinCopper
	veinGold
	veinRedstone
	veinLapis
	veinDiamond
	veinEmerald
	veinInfested
	veinGranite
	veinDiorite
	veinAndesite
	veinTuff
	veinDirt
	veinGravel
	veinClay
	numVeinBlocks
)

// blocks holds the runtime IDs that tests and GroundBlocks compare with, looked up once.
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
	// veins holds ore and rock blocks by veinBlock: the stone variant first, then the deepslate one.
	veins [numVeinBlocks][2]uint32
	// terracotta holds the badlands band colours, from the plain block to the stained ones.
	terracotta []uint32
	// logs holds each tree's log by axis X, Y, Z, and leaves its leaves, by treeKind.
	logs   [numTreeKinds][3]uint32
	leaves [numTreeKinds]uint32
	// Plants.
	shortGrass, fern, deadBush, cactus, sugarCane, lilyPad, kelp, blueOrchid uint32
	tallGrass, largeFern, sunflower                                          [2]uint32
	tallFlowers                                                              [][2]uint32
	commonFlowers, flowers                                                   []uint32
	// snowLayer and seagrass are our own types (plant_blocks.go).
	snowLayer, seagrass uint32
}

// isLeaves says whether a block is the leaves of one of our trees.
func (b *blocks) isLeaves(id uint32) bool {
	for _, l := range b.leaves {
		if l == id && id != 0 {
			return true
		}
	}
	return false
}

func (b *blocks) isTerracotta(id uint32) bool {
	for _, t := range b.terracotta {
		if t == id {
			return true
		}
	}
	return false
}

// holdsWaterPlant says whether kelp and seagrass can root on a floor block.
func (b *blocks) holdsWaterPlant(id uint32) bool {
	switch id {
	case b.dirt, b.sand, b.gravel, b.clay, b.stone, b.grass, b.mud, b.deepslate, b.granite, b.diorite, b.andesite:
		return true
	}
	return false
}

// newBlocks uses Dragonfly's default block registry. A generator can be built before the
// server config finalizes that registry, and looking up an ID then would panic.
// Finalize is safe to repeat, but it does stop later RegisterBlock calls.
func newBlocks() blocks {
	registerPlantBlocks()
	registerOwnBlocks()
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
	tree := func(kind treeKind, w block.WoodType, l block.LeavesType) {
		for i, a := range []cube.Axis{cube.X, cube.Y, cube.Z} {
			b.logs[kind][i] = id(block.Log{Wood: w, Axis: a})
		}
		b.leaves[kind] = id(block.Leaves{Type: l})
	}
	tree(treeOak, block.OakWood(), block.OakLeaves())
	tree(treeBirch, block.BirchWood(), block.BirchLeaves())
	tree(treeSpruce, block.SpruceWood(), block.SpruceLeaves())
	tree(treePine, block.SpruceWood(), block.SpruceLeaves())
	tree(treeDarkOak, block.DarkOakWood(), block.DarkOakLeaves())
	tree(treeJungle, block.JungleWood(), block.JungleLeaves())
	tree(treeAcacia, block.AcaciaWood(), block.AcaciaLeaves())
	tree(treeCherry, block.CherryWood(), block.CherryLeaves())
	tree(treePaleOak, block.PaleOakWood(), block.PaleOakLeaves())
	tree(treeMangrove, block.MangroveWood(), block.MangroveLeaves())
	two := func(lower, upper world.Block) [2]uint32 { return [2]uint32{id(lower), id(upper)} }
	flower := func(t block.FlowerType) uint32 { return id(block.Flower{Type: t}) }
	b.shortGrass = id(block.ShortGrass{})
	b.fern = id(block.Fern{})
	b.tallGrass = two(block.DoubleTallGrass{Type: block.NormalDoubleTallGrass()}, block.DoubleTallGrass{Type: block.NormalDoubleTallGrass(), UpperPart: true})
	b.largeFern = two(block.DoubleTallGrass{Type: block.FernDoubleTallGrass()}, block.DoubleTallGrass{Type: block.FernDoubleTallGrass(), UpperPart: true})
	b.sunflower = two(block.DoubleFlower{Type: block.Sunflower()}, block.DoubleFlower{Type: block.Sunflower(), UpperPart: true})
	for _, t := range []block.DoubleFlowerType{block.Lilac(), block.RoseBush(), block.Peony()} {
		b.tallFlowers = append(b.tallFlowers, two(block.DoubleFlower{Type: t}, block.DoubleFlower{Type: t, UpperPart: true}))
	}
	b.commonFlowers = []uint32{flower(block.Dandelion()), flower(block.Poppy())}
	b.flowers = []uint32{flower(block.Dandelion()), flower(block.Poppy()), flower(block.Allium()), flower(block.AzureBluet()),
		flower(block.RedTulip()), flower(block.OrangeTulip()), flower(block.WhiteTulip()), flower(block.PinkTulip()),
		flower(block.OxeyeDaisy()), flower(block.Cornflower()), flower(block.LilyOfTheValley())}
	b.blueOrchid = flower(block.BlueOrchid())
	b.deadBush = id(block.DeadBush{})
	b.cactus = id(block.Cactus{})
	b.sugarCane = id(block.SugarCane{})
	b.lilyPad = id(block.LilyPad{})
	b.kelp = id(block.Kelp{Age: 25}) // At full age, so it does not grow past what was generated.
	b.snowLayer = id(SnowLayer{})
	b.seagrass = id(Seagrass{})
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

// groundNames is the Bedrock study's list of ground blocks (measure.py, GROUND_EXACT), by Bedrock
// name. Every ore and terracotta counts too. hardened_clay is Dragonfly's name for plain terracotta.
var groundNames = map[string]bool{}

func init() {
	for _, n := range strings.Fields(`stone granite diorite andesite deepslate tuff calcite dirt coarse_dirt grass_block
		podzol mycelium dirt_with_roots mud clay sand red_sand suspicious_sand sandstone red_sandstone gravel
		suspicious_gravel bedrock terracotta hardened_clay packed_ice blue_ice snow powder_snow dripstone_block
		moss_block pale_moss_block obsidian magma amethyst_block budding_amethyst smooth_basalt raw_iron_block
		raw_copper_block sculk infested_stone infested_deepslate mud_bricks packed_mud`) {
		groundNames[n] = true
	}
}

// GroundBlocks is the study's idea of ground, by runtime ID: not air, water, lava, plants, logs,
// leaves or anything lying on top. It follows the study's measuring tool for Bedrock worlds name by
// name, over every registered block, so a height read with it compares with theirs.
func GroundBlocks() map[uint32]bool {
	m := map[uint32]bool{}
	for _, b := range world.Blocks() {
		name, _ := b.EncodeBlock()
		n := strings.TrimPrefix(name, "minecraft:")
		if groundNames[n] || strings.HasSuffix(n, "_ore") || strings.HasSuffix(n, "_terracotta") {
			m[world.BlockRuntimeID(b)] = true
		}
	}
	return m
}
