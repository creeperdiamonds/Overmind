package worldgen

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// The generator's states are translated to Dragonfly's blocks at the end of a chunk. A generator state
// keeps properties Dragonfly has no field for, such as a leaf's distance, and those are dropped here.
// Blocks Dragonfly has no type for get OwnBlock, so they can be broken.

// OwnBlock is one state of a block Dragonfly lists but has no Go type for.
type OwnBlock struct{ id uint16 }

type ownState struct {
	name  string
	props map[string]any
	solid bool
}

var (
	ownStates []ownState
	ownByJava = map[string]uint16{}
	ownHash   = block.NextHash()
)

func addOwn(java, name string, props map[string]any, solid bool) {
	if _, ok := ownByJava[java]; ok {
		return
	}
	ownByJava[java] = uint16(len(ownStates))
	ownStates = append(ownStates, ownState{name, props, solid})
}

// EncodeBlock ...
func (o OwnBlock) EncodeBlock() (string, map[string]any) {
	s := &ownStates[o.id]
	return "minecraft:" + s.name, s.props
}

// Hash ...
func (o OwnBlock) Hash() (uint64, uint64) { return ownHash, uint64(o.id) }

// Model ...
func (o OwnBlock) Model() world.BlockModel {
	if ownStates[o.id].solid {
		return model.Solid{}
	}
	return model.Empty{}
}

// BreakInfo gives every own block a short hand break, with no drop.
func (o OwnBlock) BreakInfo() block.BreakInfo {
	return block.BreakInfo{Hardness: 0.5, Effective: func(item.Tool) bool { return true }, Harvestable: func(item.Tool) bool { return true }, Drops: func(item.Tool, []item.Enchantment) []item.Stack { return nil }}
}

func boolWord(v int) string { return strconv.FormatBool(v != 0) }

// ownBlocksList fills the table of own blocks. A Java state not listed is dropped to air: NOTES.md
// lists the gaps.
func ownBlocksList() {
	plain := func(java, name string, solid bool) { addOwn(java, name, map[string]any{}, solid) }
	plain("moss_block", "moss_block", true)
	plain("powder_snow", "powder_snow", true)
	plain("budding_amethyst", "budding_amethyst", true)
	plain("spawner", "mob_spawner", true)
	plain("bush", "bush", false)
	plain("firefly_bush", "firefly_bush", false)
	plain("short_dry_grass", "short_dry_grass", false)
	plain("tall_dry_grass", "tall_dry_grass", false)
	plain("azalea", "azalea", false)
	plain("flowering_azalea", "flowering_azalea", false)
	plain("brown_mushroom", "brown_mushroom", false)
	plain("red_mushroom", "red_mushroom", false)
	addOwn("sweet_berry_bush[age=3]", "sweet_berry_bush", map[string]any{"growth": int32(3)}, false)
	addOwn("bee_nest[facing=south,honey_level=0]", "bee_nest", map[string]any{"direction": int32(0), "honey_level": int32(0)}, true)
	for mask := range 64 {
		down, up, north, south, west, east := mask&1, mask>>1&1, mask>>2&1, mask>>3&1, mask>>4&1, mask>>5&1
		for _, water := range [2]string{"false", "true"} {
			j := fmt.Sprintf("glow_lichen[down=%s,east=%s,north=%s,south=%s,up=%s,waterlogged=%s,west=%s]", boolWord(down), boolWord(east), boolWord(north), boolWord(south), boolWord(up), water, boolWord(west))
			bits := int32(down) | int32(up)<<1 | int32(south)<<2 | int32(west)<<3 | int32(north)<<4 | int32(east)<<5
			addOwn(j, "glow_lichen", map[string]any{"multi_face_direction_bits": bits}, false)
		}
	}
	for _, name := range [2]string{"leaf_litter", "wildflowers"} {
		key := "segment_amount"
		if name == "wildflowers" {
			key = "flower_amount"
		}
		for _, f := range [4]string{"north", "east", "south", "west"} {
			for n := 1; n <= 4; n++ {
				addOwn(fmt.Sprintf("%s[facing=%s,%s=%d]", name, f, key, n), name, map[string]any{"growth": int32(n - 1), "minecraft:cardinal_direction": f}, false)
			}
		}
	}
	for _, name := range [4]string{"small_amethyst_bud", "medium_amethyst_bud", "large_amethyst_bud", "amethyst_cluster"} {
		for _, f := range [6]string{"down", "up", "north", "south", "west", "east"} {
			for _, w := range [2]string{"false", "true"} {
				addOwn(fmt.Sprintf("%s[facing=%s,waterlogged=%s]", name, f, w), name, map[string]any{"minecraft:block_face": f}, false)
			}
		}
	}
	addOwn("tall_seagrass[half=lower]", "seagrass", map[string]any{"sea_grass_type": "double_bot"}, false)
	addOwn("tall_seagrass[half=upper]", "seagrass", map[string]any{"sea_grass_type": "double_top"}, false)
	bits := func(name string, up, north, south, east, west bool, stem bool) int32 {
		if stem {
			return 10
		}
		if !up {
			return 0
		}
		switch {
		case north && west:
			return 1
		case north && east:
			return 3
		case south && west:
			return 7
		case south && east:
			return 9
		case north:
			return 2
		case west:
			return 4
		case east:
			return 6
		case south:
			return 8
		}
		return 5
	}
	for _, cap := range [2]string{"brown_mushroom_block", "red_mushroom_block"} {
		for mask := range 64 {
			down, east, north, south, up, west := mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0, mask&16 != 0, mask&32 != 0
			j := fmt.Sprintf("%s[down=%v,east=%v,north=%v,south=%v,up=%v,west=%v]", cap, down, east, north, south, up, west)
			v := bits(cap, up, north, south, east, west, false)
			if !up && (north || south || east || west) {
				switch {
				case north:
					v = 2
				case south:
					v = 8
				case east:
					v = 6
				default:
					v = 4
				}
			}
			addOwn(j, cap, map[string]any{"huge_mushroom_bits": v}, true)
		}
	}
	addOwn("mushroom_stem[down=false,east=true,north=true,south=true,up=false,west=true]", "brown_mushroom_block", map[string]any{"huge_mushroom_bits": int32(10)}, true)
	lushOwnBlocks()
}

var cardinals = [4]string{"north", "east", "south", "west"}

var bools = [2]string{"false", "true"}

// lushOwnBlocks adds the lush cave blocks Dragonfly has no type for, in Bedrock's names and states.
func lushOwnBlocks() {
	plain := func(java, name string, solid bool) { addOwn(java, name, map[string]any{}, solid) }
	plain("rooted_dirt", "dirt_with_roots", true)
	for _, w := range bools {
		plain("hanging_roots[waterlogged="+w+"]", "hanging_roots", false)
	}
	// Java's cave vines are a head with an age and a body without; Bedrock's are one block, with or
	// without berries, whose head and body share an age. A body takes age 0.
	for _, b := range bools {
		head, bodyName := "cave_vines", "cave_vines"
		if b == "true" {
			head, bodyName = "cave_vines_head_with_berries", "cave_vines_body_with_berries"
		}
		for age := range 26 {
			addOwn(fmt.Sprintf("cave_vines[age=%d,berries=%s]", age, b), head, map[string]any{"growing_plant_age": int32(age)}, false)
		}
		addOwn("cave_vines_plant[berries="+b+"]", bodyName, map[string]any{"growing_plant_age": int32(0)}, false)
	}
	for di, dn := range [2]string{"up", "down"} {
		for _, th := range dripThickness {
			b := th
			if th == "tip_merge" {
				b = "merge"
			}
			for _, w := range bools {
				addOwn("pointed_dripstone[thickness="+th+",vertical_direction="+dn+",waterlogged="+w+"]", "pointed_dripstone", map[string]any{"dripstone_thickness": b, "hanging": uint8(di)}, true)
			}
		}
	}
	coralDir := map[string]int32{"west": 0, "east": 1, "north": 2, "south": 3}
	for _, k := range [5]string{"tube", "brain", "bubble", "fire", "horn"} {
		addOwn(k+"_coral_fan[waterlogged=true]", k+"_coral_fan", map[string]any{"coral_fan_direction": int32(0)}, false)
		for _, f := range cardinals {
			addOwn(k+"_coral_wall_fan[facing="+f+",waterlogged=true]", k+"_coral_wall_fan", map[string]any{"coral_direction": coralDir[f]}, false)
		}
	}
	for _, w := range bools {
		plain("mangrove_roots[waterlogged="+w+"]", "mangrove_roots", true)
		for age := range 5 {
			for _, hang := range bools {
				addOwn(fmt.Sprintf("mangrove_propagule[age=%d,hanging=%s,stage=0,waterlogged=%s]", age, hang, w), "mangrove_propagule",
					map[string]any{"hanging": uint8(btoi(hang == "true")), "propagule_stage": int32(age)}, false)
			}
		}
	}
	plain("pale_moss_block", "pale_moss_block", true)
	plain("closed_eyeblossom", "closed_eyeblossom", false)
	for i, tip := range bools {
		addOwn("pale_hanging_moss[tip="+tip+"]", "pale_hanging_moss", map[string]any{"tip": uint8(i)}, false)
	}
	addOwn("creaking_heart[axis=y,creaking_heart_state=dormant,natural=true]", "creaking_heart", map[string]any{"creaking_heart_state": "dormant", "natural": uint8(1), "pillar_axis": "y"}, true)
	sideNames := map[string]string{"none": "none", "low": "short", "tall": "tall"}
	for _, bottom := range bools {
		for n := range 81 {
			side := [4]string{}
			props := map[string]any{"upper_block_bit": uint8(btoi(bottom == "false"))}
			for i, f := range cardinals {
				side[i] = [3]string{"none", "low", "tall"}[n/[4]int{1, 3, 9, 27}[i]%3]
				props["pale_moss_carpet_side_"+f] = sideNames[side[i]]
			}
			addOwn("pale_moss_carpet[bottom="+bottom+",east="+side[1]+",north="+side[0]+",south="+side[2]+",west="+side[3]+"]", "pale_moss_carpet", props, false)
		}
	}
	plain("sculk", "sculk", true)
	addOwn("sculk_catalyst[bloom=false]", "sculk_catalyst", map[string]any{"bloom": uint8(0)}, true)
	for _, w := range bools {
		addOwn("sculk_sensor[power=0,sculk_sensor_phase=inactive,waterlogged="+w+"]", "sculk_sensor", map[string]any{"sculk_sensor_phase": int32(0)}, false)
		addOwn("sculk_shrieker[can_summon=true,shrieking=false,waterlogged="+w+"]", "sculk_shrieker", map[string]any{"active": uint8(0), "can_summon": uint8(1)}, false)
	}
	for mask := range 64 {
		down, up, north, south, west, east := mask&1, mask>>1&1, mask>>2&1, mask>>3&1, mask>>4&1, mask>>5&1
		for _, water := range bools {
			j := fmt.Sprintf("sculk_vein[down=%s,east=%s,north=%s,south=%s,up=%s,waterlogged=%s,west=%s]", boolWord(down), boolWord(east), boolWord(north), boolWord(south), boolWord(up), water, boolWord(west))
			bits := int32(down) | int32(up)<<1 | int32(south)<<2 | int32(west)<<3 | int32(north)<<4 | int32(east)<<5
			addOwn(j, "sculk_vein", map[string]any{"multi_face_direction_bits": bits}, false)
		}
	}
	// Bedrock's big dripleaf is one block: the leaf is its head, the stem the rest.
	for _, f := range cardinals {
		for _, w := range bools {
			addOwn("big_dripleaf_stem[facing="+f+",waterlogged="+w+"]", "big_dripleaf", map[string]any{"big_dripleaf_head": uint8(0), "big_dripleaf_tilt": "none", "minecraft:cardinal_direction": f}, false)
			addOwn("big_dripleaf[facing="+f+",tilt=none,waterlogged="+w+"]", "big_dripleaf", map[string]any{"big_dripleaf_head": uint8(1), "big_dripleaf_tilt": "none", "minecraft:cardinal_direction": f}, false)
			for bit, half := range [2]string{"lower", "upper"} {
				addOwn("small_dripleaf[facing="+f+",half="+half+",waterlogged="+w+"]", "small_dripleaf_block", map[string]any{"minecraft:cardinal_direction": f, "upper_block_bit": uint8(bit)}, false)
			}
		}
	}
}

var registerOwnBlocks = sync.OnceFunc(func() {
	ownBlocksList()
	seen := map[string]bool{}
	for i := range ownStates {
		s := &ownStates[i]
		k := fmt.Sprint(s.name, s.props)
		if seen[k] {
			// Two Java states share a Bedrock state: the first registered answers for both.
			continue
		}
		seen[k] = true
		world.RegisterBlock(OwnBlock{uint16(i)})
	}
})

func init() { registerOwnBlocks() }

func propOf(name, key string) string {
	i := strings.Index(name, key+"=")
	if i < 0 {
		return ""
	}
	v := name[i+len(key)+1:]
	if j := strings.IndexAny(v, ",]"); j >= 0 {
		v = v[:j]
	}
	return v
}

func cubeDir(s string) cube.Direction {
	switch s {
	case "south":
		return cube.South
	case "west":
		return cube.West
	case "east":
		return cube.East
	}
	return cube.North
}

// Wood and leaf kinds by the state's name.
var woods = map[string]block.WoodType{"oak": block.OakWood(), "spruce": block.SpruceWood(), "birch": block.BirchWood(), "jungle": block.JungleWood(),
	"acacia": block.AcaciaWood(), "dark_oak": block.DarkOakWood(), "mangrove": block.MangroveWood(), "cherry": block.CherryWood(), "pale_oak": block.PaleOakWood()}

var leafTypes = map[string]block.LeavesType{"oak": block.OakLeaves(), "spruce": block.SpruceLeaves(), "birch": block.BirchLeaves(), "jungle": block.JungleLeaves(),
	"acacia": block.AcaciaLeaves(), "dark_oak": block.DarkOakLeaves(), "mangrove": block.MangroveLeaves(), "cherry": block.CherryLeaves(), "pale_oak": block.PaleOakLeaves(),
	"azalea": block.AzaleaLeaves(), "flowering_azalea": block.FloweringAzaleaLeaves()}

var flowerTypes = map[string]block.FlowerType{"dandelion": block.Dandelion(), "poppy": block.Poppy(), "blue_orchid": block.BlueOrchid(), "allium": block.Allium(),
	"azure_bluet": block.AzureBluet(), "red_tulip": block.RedTulip(), "orange_tulip": block.OrangeTulip(), "white_tulip": block.WhiteTulip(), "pink_tulip": block.PinkTulip(),
	"oxeye_daisy": block.OxeyeDaisy(), "cornflower": block.Cornflower(), "lily_of_the_valley": block.LilyOfTheValley()}

var coralTypes = map[string]block.CoralType{"tube": block.TubeCoral(), "brain": block.BrainCoral(), "bubble": block.BubbleCoral(), "fire": block.FireCoral(), "horn": block.HornCoral()}

// dragonflyBlock gives the Dragonfly block of a state. Air stands in for what has no block.
func dragonflyBlock(name string) world.Block {
	if id, ok := ownByJava[name]; ok {
		return ownBlockOf(id)
	}
	base := stateBase(name)
	if id, ok := ownByJava[base]; ok {
		return ownBlockOf(id)
	}
	if w, ok := strings.CutSuffix(base, "_log"); ok {
		if t, ok := woods[w]; ok {
			a := cube.Y
			switch propOf(name, "axis") {
			case "x":
				a = cube.X
			case "z":
				a = cube.Z
			}
			return block.Log{Wood: t, Axis: a}
		}
	}
	if w, ok := strings.CutSuffix(base, "_leaves"); ok {
		if t, ok := leafTypes[w]; ok {
			return block.Leaves{Type: t}
		}
	}
	if t, ok := flowerTypes[base]; ok {
		return block.Flower{Type: t}
	}
	ore := func(deep bool) block.OreType {
		if deep {
			return block.DeepslateOre()
		}
		return block.StoneOre()
	}
	if o, ok := strings.CutSuffix(base, "_ore"); ok {
		deep := strings.HasPrefix(o, "deepslate_")
		o = strings.TrimPrefix(o, "deepslate_")
		switch o {
		case "coal":
			return block.CoalOre{Type: ore(deep)}
		case "iron":
			return block.IronOre{Type: ore(deep)}
		case "copper":
			return block.CopperOre{Type: ore(deep)}
		case "gold":
			return block.GoldOre{Type: ore(deep)}
		case "redstone":
			return block.RedstoneOre{Type: ore(deep)}
		case "lapis":
			return block.LapisOre{Type: ore(deep)}
		case "diamond":
			return block.DiamondOre{Type: ore(deep)}
		case "emerald":
			return block.EmeraldOre{Type: ore(deep)}
		}
	}
	upper := propOf(name, "half") == "upper"
	switch base {
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
	case "snow_block":
		return block.Snow{}
	case "ice":
		return block.Ice{}
	case "packed_ice":
		return block.PackedIce{}
	case "blue_ice":
		return block.BlueIce{}
	case "granite":
		return block.Granite{}
	case "diorite":
		return block.Diorite{}
	case "andesite":
		return block.Andesite{}
	case "tuff":
		return block.Tuff{}
	case "clay":
		return block.Clay{}
	case "raw_copper_block":
		return block.RawCopper{}
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
	case "infested_stone":
		return block.InfestedStone{}
	case "infested_deepslate":
		return block.InfestedDeepslate{}
	case "amethyst_block":
		return block.Amethyst{}
	case "smooth_basalt":
		return block.SmoothBasalt{}
	case "dripstone_block":
		return block.Dripstone{}
	case "magma_block":
		return block.Magma{}
	case "cobblestone":
		return block.Cobblestone{}
	case "mossy_cobblestone":
		return block.Cobblestone{Mossy: true}
	case "chest":
		return block.Chest{Facing: cubeDir(propOf(name, "facing"))}
	case "melon":
		return block.Melon{}
	case "pumpkin":
		return block.Pumpkin{Facing: cube.South}
	case "short_grass":
		return block.ShortGrass{}
	case "fern":
		return block.Fern{}
	case "dead_bush":
		return block.DeadBush{}
	case "tall_grass":
		return block.DoubleTallGrass{Type: block.NormalDoubleTallGrass(), UpperPart: upper}
	case "large_fern":
		return block.DoubleTallGrass{Type: block.FernDoubleTallGrass(), UpperPart: upper}
	case "sunflower":
		return block.DoubleFlower{Type: block.Sunflower(), UpperPart: upper}
	case "lilac":
		return block.DoubleFlower{Type: block.Lilac(), UpperPart: upper}
	case "rose_bush":
		return block.DoubleFlower{Type: block.RoseBush(), UpperPart: upper}
	case "peony":
		return block.DoubleFlower{Type: block.Peony(), UpperPart: upper}
	case "sugar_cane":
		return block.SugarCane{}
	case "cactus":
		return block.Cactus{}
	case "lily_pad":
		return block.LilyPad{}
	case "kelp":
		return block.Kelp{Age: 25}
	case "kelp_plant":
		return block.Kelp{Age: 25}
	case "seagrass":
		return Seagrass{}
	case "snow":
		return SnowLayer{}
	case "vine":
		return block.Vines{NorthDirection: propOf(name, "north") == "true", EastDirection: propOf(name, "east") == "true",
			SouthDirection: propOf(name, "south") == "true", WestDirection: propOf(name, "west") == "true"}
	case "cocoa":
		age, _ := strconv.Atoi(propOf(name, "age"))
		return block.CocoaBean{Facing: cubeDir(propOf(name, "facing")), Age: age}
	case "sea_pickle":
		n, _ := strconv.Atoi(propOf(name, "pickles"))
		return block.SeaPickle{AdditionalCount: max(n-1, 0)}
	case "bamboo":
		leaf := block.BambooSizeNoLeaves()
		switch propOf(name, "leaves") {
		case "small":
			leaf = block.BambooSizeSmallLeaves()
		case "large":
			leaf = block.BambooSizeLargeLeaves()
		}
		return block.Bamboo{Thick: true, LeafSize: leaf, Ready: propOf(name, "stage") == "1"}
	case "moss_carpet":
		return block.MossCarpet{}
	case "tube_coral", "brain_coral", "bubble_coral", "fire_coral", "horn_coral":
		return block.Coral{Type: coralTypes[strings.TrimSuffix(base, "_coral")]}
	case "tube_coral_block", "brain_coral_block", "bubble_coral_block", "fire_coral_block", "horn_coral_block":
		return block.CoralBlock{Type: coralTypes[strings.TrimSuffix(base, "_coral_block")]}
	case "muddy_mangrove_roots":
		a := cube.Y
		switch propOf(name, "axis") {
		case "x":
			a = cube.X
		case "z":
			a = cube.Z
		}
		return block.MuddyMangroveRoots{Axis: a}
	case "pink_petals":
		n, _ := strconv.Atoi(propOf(name, "flower_amount"))
		return block.PinkPetals{AdditionalCount: max(n-1, 0), Facing: cubeDir(propOf(name, "facing"))}
	case "spore_blossom":
		return block.SporeBlossom{}
	case "air", "cave_air", "void_air":
		return block.Air{}
	}
	return unknownState(name)
}

// unknownLogged holds the states already reported as having no block.
var unknownLogged sync.Map

// unknownState is what a state with no Dragonfly block becomes: air, so a chunk is still made, with a
// warning the first time. Under test it panics instead, so a missing mapping cannot pass unseen.
func unknownState(name string) world.Block {
	if testing.Testing() {
		panic(fmt.Sprintf("worldgen: state %q has no Dragonfly block", name))
	}
	if _, seen := unknownLogged.LoadOrStore(name, true); !seen {
		slog.Default().Warn("worldgen: a generated state has no block and becomes air", "state", name)
	}
	return block.Air{}
}

func ownBlockOf(id uint16) world.Block {
	s := &ownStates[id]
	for i := range ownStates {
		o := &ownStates[i]
		if o.name == s.name && fmt.Sprint(o.props) == fmt.Sprint(s.props) {
			return OwnBlock{uint16(i)}
		}
	}
	return OwnBlock{id}
}

// dfMap holds the Dragonfly runtime ID of each state, filled on first use.
var (
	dfRT   [maxStates]uint32
	dfDone [maxStates]bool
	dfLock sync.RWMutex
)

// runtimeOf finds a state's Dragonfly runtime ID.
func runtimeOf(s bs) uint32 {
	dfLock.RLock()
	done, id := dfDone[s], dfRT[s]
	dfLock.RUnlock()
	if done {
		return id
	}
	world.DefaultBlockRegistry.Finalize()
	id = world.BlockRuntimeID(dragonflyBlock(s.name()))
	dfLock.Lock()
	dfRT[s], dfDone[s] = id, true
	dfLock.Unlock()
	return id
}

// writeChunk puts a base's blocks into a Dragonfly chunk.
func (g *Generator) writeChunk(c *chunk.Chunk, b *baseChunk) { writeCells(c, &b.b) }

func writeCells(c *chunk.Chunk, cells *[chunkCells]bs) {
	last, lastID := bs(0), uint32(0)
	water := runtimeOf(sWater)
	for i, s := range cells {
		if s == 0 {
			continue
		}
		if s != last {
			last, lastID = s, runtimeOf(s)
		}
		x, y, z := uint8(i&15), int16(i>>8)+minY, uint8(i>>4&15)
		c.SetBlock(x, y, z, 0, lastID)
		// Bedrock keeps the water of a waterlogged block, kelp and seagrass in the second layer.
		if s.has(fWater) && s.block() != sWater.block() {
			c.SetBlock(x, y, z, 1, water)
		}
	}
}
