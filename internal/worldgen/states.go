package worldgen

import (
	"strings"
	"sync"
)

// The generator works on its own table of block states, written the way the study's fixtures print
// them ("oak_leaves[distance=1,persistent=false,waterlogged=false]"), because the rules that follow
// Mojang's algorithms read and write properties Dragonfly's blocks do not hold, such as a leaf's
// distance. A finished chunk is translated to Dragonfly's runtime IDs at the end (dfblocks.go).
//
// The flags are facts about each block, checked against the study's list of block flags by
// TestGoldenStateFlags. No Mojang file is in this repo.

// bs is a block state: an index into the state table. Zero is air.
type bs uint16

// Flags of a state.
const (
	fAir uint16 = 1 << iota
	fMotion
	fSolid
	fLiquid
	fSolidRender
	fSturdyUp
	fSturdyDown
	fSturdySide
	fReplaceable
	fWater  // Holds water, as a liquid block or waterlogged.
	fLava   // Holds lava.
	fSource // Its fluid is a source.
)

const fCube = fMotion | fSolid | fSolidRender | fSturdyUp | fSturdyDown | fSturdySide

// blockFlagGroups lists blocks that are not full cubes. Anything not listed is a full cube.
var blockFlagGroups = []struct {
	flags uint16
	names string
}{
	{0, "allium azure_bluet big_dripleaf big_dripleaf_stem blue_orchid brown_mushroom cactus_flower cave_vines cave_vines_plant closed_eyeblossom cocoa cornflower dandelion firefly_bush kelp kelp_plant lilac lily_of_the_valley lily_pad mangrove_propagule open_eyeblossom orange_tulip oxeye_daisy pale_hanging_moss peony pink_petals pink_tulip poppy powder_snow red_mushroom red_tulip rose_bush sea_pickle small_dripleaf spore_blossom sugar_cane sunflower sweet_berry_bush torchflower white_tulip wildflowers"},
	{fMotion | fSolid, "acacia_leaves amethyst_cluster azalea_leaves bamboo birch_leaves cactus cherry_leaves chest dark_oak_leaves flowering_azalea_leaves jungle_leaves large_amethyst_bud mangrove_leaves medium_amethyst_bud oak_leaves pale_oak_leaves pointed_dripstone sculk_vein small_amethyst_bud spruce_leaves"},
	{fReplaceable, "bush dead_bush fern glow_lichen hanging_roots large_fern leaf_litter resin_clump seagrass short_dry_grass short_grass tall_dry_grass tall_grass tall_seagrass vine"},
	{fAir | fReplaceable, "air cave_air void_air"},
	{fMotion | fSolid | fSturdyUp | fSturdyDown | fSturdySide, "ice mangrove_roots spawner"},
	{fLiquid | fReplaceable, "lava water"},
	{fMotion | fSolid | fSturdyDown, "sculk_sensor sculk_shrieker"},
	{fSturdyUp, "azalea flowering_azalea"},
	{fSolid, "bamboo_sapling"},
	{fSturdyDown, "moss_carpet"},
}

// Tags are named sets of blocks, as block-level bits. The members come from the study's rules and
// the fixtures.
const (
	tagCarvable uint32 = 1 << iota // Carvers may cut it.
	tagDirtLike                    // The dirt family, which plants and trees stand on.
	tagLogs
	tagLeaves
	tagStoneOre     // Stone-level ores replace it.
	tagDeepslateOre // Deepslate-level ores replace it.
	tagBaseStone
	tagNoFeature // A feature may not replace it.
	tagTreeSwap  // Trees may replace it.
	tagFlower
	tagSand
	tagTerracotta
	tagNoLavaPool  // A lava lake's rim leaves it alone.
	tagNoTrunkBase // A trunk does not turn it to dirt.
	tagGeodeBad    // A geode's points may not sit in it.
	tagMossSwap    // Moss patches replace it.
	tagLushGround  // Lush cave ground replaces it.
	tagAzaleaSwap  // An azalea's roots grow through it.
	tagDripSwap    // Dripstone clusters replace it.
	tagSculkSwap   // Sculk replaces it.
	tagBambooSoil  // Bamboo stands on it.
	tagPodzolSwap  // Bamboo's podzol replaces it.
	tagVineHolder  // Marks blocks a vine may not cling to.
	tagHangs       // Holds on to a side or a ceiling, not the block below: vines, lichen, cocoa.
	tagAzaleaSoil  // An azalea, and a rooted azalea's tree, grows on it.
)

var blockTags = func() map[string]uint32 {
	m := map[string]uint32{}
	add := func(tag uint32, names string) {
		for _, n := range strings.Fields(names) {
			m[n] |= tag
		}
	}
	const baseStone = "stone granite diorite andesite tuff deepslate "
	const dirtFamily = "dirt coarse_dirt rooted_dirt grass_block podzol mycelium moss_block pale_moss_block mud muddy_mangrove_roots "
	const terracotta = "terracotta white_terracotta orange_terracotta yellow_terracotta brown_terracotta red_terracotta light_gray_terracotta "
	add(tagBaseStone, baseStone)
	add(tagDirtLike, dirtFamily)
	add(tagSand, "sand red_sand suspicious_sand")
	add(tagTerracotta, terracotta)
	add(tagStoneOre, "stone granite diorite andesite")
	add(tagDeepslateOre, "deepslate tuff")
	// Lush cave tags: moss replaces base stone, cave vines and dirt; lush ground adds clay, gravel
	// and sand; azalea roots grow through stone, dirt, terracotta, sands, clay, gravel and snow.
	add(tagMossSwap, baseStone+dirtFamily+"cave_vines cave_vines_plant")
	add(tagLushGround, baseStone+dirtFamily+"cave_vines cave_vines_plant clay gravel sand")
	add(tagAzaleaSwap, baseStone+dirtFamily+terracotta+"red_sand clay gravel sand snow_block powder_snow")
	add(tagAzaleaSoil, dirtFamily+terracotta+"sand red_sand suspicious_sand snow_block powder_snow")
	// What sculk may replace in world generation: stone, dirt, sands, clay and the like.
	add(tagSculkSwap, baseStone+dirtFamily+terracotta+"sand red_sand gravel calcite smooth_basalt clay dripstone_block sandstone red_sandstone")
	add(tagCarvable, baseStone+dirtFamily+terracotta+"sand red_sand water gravel sandstone red_sandstone calcite snow snow_block packed_ice raw_iron_block raw_copper_block iron_ore deepslate_iron_ore copper_ore deepslate_copper_ore")
	return m
}()

const (
	maxStates = 8192
	maxBlocks = 1024
)

var (
	stateName  [maxStates]string
	stateBlock [maxStates]uint16 // The block's index, shared by all its states.
	stateFlag  [maxStates]uint16
	stateHalf  [maxStates]int8 // A double block's half: 1 upper, -1 lower, 0 neither.
	blockName  [maxBlocks]string
	blockTag   [maxBlocks]uint32
	blockFlags = func() map[string]uint16 {
		m := map[string]uint16{}
		for _, g := range blockFlagGroups {
			for _, n := range strings.Fields(g.names) {
				m[n] = g.flags
			}
		}
		return m
	}()
	blockIndex = map[string]uint16{}
	stateIndex = map[string]bs{}
	stateLock  sync.Mutex
)

// stateOf finds or adds a state. Adding is rare and happens almost all at start-up; states already
// added are never moved, so reading the tables by a state needs no lock.
func stateOf(name string) bs {
	stateLock.Lock()
	defer stateLock.Unlock()
	if s, ok := stateIndex[name]; ok {
		return s
	}
	s := bs(len(stateIndex))
	base, props, _ := strings.Cut(name, "[")
	b, ok := blockIndex[base]
	if !ok {
		b = uint16(len(blockIndex))
		blockIndex[base] = b
		blockName[b] = base
		blockTag[b] = blockTags[base]
	}
	fl, listed := blockFlags[base]
	if !listed {
		fl = fCube
	}
	switch base {
	case "water":
		fl |= fWater
		if strings.Contains(props, "level=0") {
			fl |= fSource
		}
	case "lava":
		fl |= fLava
		if strings.Contains(props, "level=0") {
			fl |= fSource
		}
	case "seagrass", "tall_seagrass", "kelp", "kelp_plant":
		fl |= fWater | fSource
	case "snow":
		fl = fSturdyDown | fReplaceable // One layer, the only height generation places.
	case "pale_moss_carpet":
		if strings.Contains(props, "bottom=true") {
			fl = fSturdyDown
		}
	}
	if strings.Contains(props, "waterlogged=true") {
		fl |= fWater | fSource
	}
	switch {
	case strings.Contains(props, "half=upper"):
		stateHalf[s] = 1
	case strings.Contains(props, "half=lower"):
		stateHalf[s] = -1
	}
	stateName[s], stateBlock[s], stateFlag[s] = name, b, fl
	stateIndex[name] = s
	return s
}

func (s bs) has(f uint16) bool { return stateFlag[s]&f != 0 }

// isAir says the state is air, cave air or void air.
func (s bs) isAir() bool { return stateFlag[s]&fAir != 0 }

// hasFluid says the state holds water or lava.
func (s bs) hasFluid() bool { return stateFlag[s]&(fWater|fLava) != 0 }

// tagged says whether a state's block is in a tag.
func (s bs) tagged(tag uint32) bool { return blockTag[stateBlock[s]]&tag != 0 }

func (s bs) block() uint16 { return stateBlock[s] }

// half is a double plant's half: 1 upper, -1 lower, 0 for any other block.
func (s bs) half() int8 { return stateHalf[s] }

// name is the state's text, as the fixtures print it, without the namespace.
func (s bs) name() string { return stateName[s] }

// The states the noise, surface and carver passes use. Air comes first, as state zero.
var (
	sAir      = stateOf("air")
	sStone    = stateOf("stone")
	sWater    = stateOf("water[level=0]")
	sLava     = stateOf("lava[level=0]")
	sBedrock  = stateOf("bedrock")
	sDeepsl   = stateOf("deepslate[axis=y]")
	sDirt     = stateOf("dirt")
	sCoarse   = stateOf("coarse_dirt")
	sGrass    = stateOf("grass_block[snowy=false]")
	sPodzol   = stateOf("podzol[snowy=false]")
	sMycelium = stateOf("mycelium[snowy=false]")
	sMud      = stateOf("mud")
	sGravel   = stateOf("gravel")
	sSand     = stateOf("sand")
	sRedSand  = stateOf("red_sand")
	sSandst   = stateOf("sandstone")
	sRedSands = stateOf("red_sandstone")
	sCalcite  = stateOf("calcite")
	sSnowBl   = stateOf("snow_block")
	sPowder   = stateOf("powder_snow")
	sIce      = stateOf("ice")
	sPacked   = stateOf("packed_ice")
	sGranite  = stateOf("granite")
	sTuff     = stateOf("tuff")
	sCopper   = stateOf("copper_ore")
	sRawCu    = stateOf("raw_copper_block")
	sDIron    = stateOf("deepslate_iron_ore")
	sRawFe    = stateOf("raw_iron_block")

	sTerracotta = stateOf("terracotta")
	sTerrOrange = stateOf("orange_terracotta")
	sTerrYellow = stateOf("yellow_terracotta")
	sTerrBrown  = stateOf("brown_terracotta")
	sTerrRed    = stateOf("red_terracotta")
	sTerrWhite  = stateOf("white_terracotta")
	sTerrLight  = stateOf("light_gray_terracotta")
)
