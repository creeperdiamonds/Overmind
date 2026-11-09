package worldgen

import (
	"maps"
	"math"
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// How our own blocks break (Task 9 round 19). Every block the generator or the structures write that
// Dragonfly has no Go type for is one of our own blocks, and each breaks as the game's does: its hardness,
// the tool that mines it faster, whether that tool is needed for a drop, and what it drops. A block with a
// wrong hardness misleads clients: one the client breaks at once (hardness 0) and the server times at
// half a second never broke for a Java player, whose client sends nothing more. The values are public
// game knowledge (the game's hardness and drop rules); the few blocks of 1.26 not described anywhere
// public are marked as assumed. Plants of 1.21.5 and later have Go types in the Dragonfly fork instead
// (bush, firefly bush, dry grass, cactus flower, leaf litter, wildflowers).

// ownKind is how the blocks of one Bedrock name break.
type ownKind struct {
	hard      float64       // Hardness; ignored when fixed.
	tool      item.ToolType // The tool that mines it faster (item.TypeNone: none).
	needTool  bool          // Only that tool gets a drop (pickaxe blocks).
	drop      string        // The item dropped ("" nothing; "self" the block's own item).
	count     [2]int        // Dropped count range; {0, 0} is one. A negative low end is a chance of none.
	chance    float64       // A drop chance; 0 is always.
	silk      bool          // Silk touch drops the block itself (and the drop above is the plain drop).
	shears    bool          // Only shears or silk touch get the drop.
	replace   bool          // Any placed block replaces it.
	xp        [2]int        // Experience dropped without silk touch.
	fn        func(props map[string]any, t item.Tool, silk bool) []item.Stack
	placeable bool // Its item places it (as the block's first state).
	entity    bool // It keeps block entity data (a chest's loot table, a banner's patterns).
}

var (
	pick   = item.TypePickaxe
	axe    = item.TypeAxe
	shovel = item.TypeShovel
	hoe    = item.TypeHoe
	none   = item.TypeNone
)

// self is a block that drops itself and whose item places it.
func self(hard float64, tool item.ToolType) ownKind {
	return ownKind{hard: hard, tool: tool, drop: "self", placeable: true}
}

// pickSelf is a block that drops itself only to a pickaxe.
func pickSelf(hard float64) ownKind {
	return ownKind{hard: hard, tool: pick, needTool: true, drop: "self", placeable: true}
}

// silkOnly is a block that drops itself only to silk touch.
func silkOnly(hard float64, tool item.ToolType, xp int) ownKind {
	return ownKind{hard: hard, tool: tool, silk: true, xp: [2]int{xp, xp}, placeable: true}
}

// shearsOnly is a plant that drops itself only to shears or silk touch.
func shearsOnly(replace bool) ownKind {
	return ownKind{drop: "self", shears: true, replace: replace, placeable: true}
}

// woodNames are Bedrock's wood names of the blocks whose oak one has no prefix.
var woodNames = []string{"spruce", "birch", "jungle", "acacia", "dark_oak", "mangrove", "cherry", "pale_oak", "bamboo", "crimson", "warped"}

var ownKinds = func() map[string]ownKind {
	k := map[string]ownKind{
		// Terrain blocks (dfblocks.go's own blocks).
		"moss_block":                   self(0.1, hoe),
		"pale_moss_block":              self(0.1, hoe),
		"pale_moss_carpet":             {hard: 0.1, placeable: true, fn: bottomOnly},
		"pale_hanging_moss":            shearsOnly(false),
		"weeping_vines":                {drop: "self", chance: 0.33, silk: true, placeable: true},
		"twisting_vines":               {drop: "self", chance: 0.33, silk: true, placeable: true},
		"crimson_nylium":               {hard: 0.4, tool: pick, needTool: true, drop: "netherrack", silk: true, placeable: true},
		"warped_nylium":                {hard: 0.4, tool: pick, needTool: true, drop: "netherrack", silk: true, placeable: true},
		"crimson_fungus":               self(0, none),
		"warped_fungus":                self(0, none),
		"crimson_roots":                {drop: "self", replace: true, placeable: true},
		"warped_roots":                 {drop: "self", replace: true, placeable: true},
		"powder_snow":                  {hard: 0.25},
		"chorus_plant":                 {hard: 0.4, tool: axe, drop: "chorus_fruit", count: [2]int{0, 1}},
		"chorus_flower":                self(0.4, axe),
		"budding_amethyst":             {hard: 1.5, tool: pick},
		"azalea":                       self(0, none),
		"flowering_azalea":             self(0, none),
		"brown_mushroom":               self(0, none),
		"red_mushroom":                 self(0, none),
		"sweet_berry_bush":             {fn: sweetBerries},
		"bee_nest":                     silkOnly(0.3, axe, 0),
		"glow_lichen":                  {hard: 0.2, tool: axe, drop: "self", shears: true, replace: true},
		"small_amethyst_bud":           silkOnly(1.5, pick, 0),
		"medium_amethyst_bud":          silkOnly(1.5, pick, 0),
		"large_amethyst_bud":           silkOnly(1.5, pick, 0),
		"amethyst_cluster":             {hard: 1.5, tool: pick, silk: true, fn: amethystShards},
		"seagrass":                     {drop: "seagrass", shears: true, replace: true},
		"brown_mushroom_block":         {hard: 0.2, tool: axe, drop: "brown_mushroom", count: [2]int{-6, 2}, silk: true, placeable: true},
		"red_mushroom_block":           {hard: 0.2, tool: axe, drop: "red_mushroom", count: [2]int{-6, 2}, silk: true, placeable: true},
		"mushroom_stem":                {hard: 0.2, tool: axe, silk: true, placeable: true},
		"dirt_with_roots":              self(0.5, shovel),
		"hanging_roots":                shearsOnly(true),
		"cave_vines":                   {},
		"cave_vines_body_with_berries": {drop: "glow_berries"},
		"cave_vines_head_with_berries": {drop: "glow_berries"},
		"pointed_dripstone":            self(1.5, pick),
		// Assumed: 1.26's sulfur spike and potent sulfur are not described publicly; they take pointed
		// dripstone's and sulfur's values (the spike shares the dripstone's code, T42).
		"sulfur_spike":         self(1.5, pick),
		"potent_sulfur":        pickSelf(1.5),
		"mangrove_roots":       self(0.7, axe),
		"mangrove_propagule":   self(0, none),
		"closed_eyeblossom":    self(0, none),
		"creaking_heart":       {hard: 10, tool: axe, drop: "resin_clump", count: [2]int{1, 3}, silk: true, placeable: true},
		"sculk":                silkOnly(0.2, hoe, 1),
		"sculk_vein":           silkOnly(0.2, hoe, 0),
		"sculk_catalyst":       silkOnly(3, hoe, 5),
		"sculk_shrieker":       silkOnly(3, hoe, 5),
		"sculk_sensor":         silkOnly(1.5, hoe, 5),
		"big_dripleaf":         self(0.1, axe),
		"small_dripleaf_block": shearsOnly(false),
		"end_gateway":          {}, // Unbreakable: never a stand-in, its own type.
		// Structure blocks Dragonfly lists without a Go type (the structures' templates and pieces).
		"bell":                               pickSelf(5),
		"cartography_table":                  self(2.5, axe),
		"cauldron":                           pickSelf(2),
		"iron_door":                          {hard: 5, tool: pick, needTool: true, fn: lowerHalf("iron_door")},
		"iron_trapdoor":                      pickSelf(5),
		"stone_button":                       self(0.5, pick),
		"stone_pressure_plate":               pickSelf(0.5),
		"polished_blackstone_button":         self(0.5, pick),
		"polished_blackstone_pressure_plate": pickSelf(0.5),
		"light_weighted_pressure_plate":      pickSelf(0.5),
		"heavy_weighted_pressure_plate":      pickSelf(0.5),
		"redstone_lamp":                      self(0.3, none),
		"lit_redstone_lamp":                  {hard: 0.3, drop: "redstone_lamp"},
		"piston_arm_collision":               {hard: 1.5, tool: pick},
		"sticky_piston_arm_collision":        {hard: 1.5, tool: pick},
		"piston":                             self(1.5, pick),
		"sticky_piston":                      self(1.5, pick),
		"powered_comparator":                 {drop: "comparator", entity: true},
		"unpowered_comparator":               {drop: "comparator", entity: true},
		"powered_repeater":                   {drop: "repeater"},
		"unpowered_repeater":                 {drop: "repeater"},
		"rail":                               self(0.7, pick),
		"golden_rail":                        self(0.7, pick),
		"detector_rail":                      self(0.7, pick),
		"activator_rail":                     self(0.7, pick),
		"snow_layer":                         {hard: 0.1, tool: shovel, needTool: true, drop: "snowball", replace: true},
		"stonecutter":                        {hard: 3.5, tool: pick, needTool: true, drop: "stonecutter_block"},
		"target":                             self(0.5, hoe),
		"tripwire_hook":                      self(0, none),
		"trip_wire":                          {drop: "string"},
		"trapped_chest":                      {hard: 2.5, tool: axe, drop: "self", placeable: true, entity: true},
		"trial_spawner":                      {hard: 50, tool: pick, entity: true},
		"vault":                              {hard: 50, tool: pick, entity: true},
		"wall_banner":                        {hard: 1, tool: axe, drop: "banner", entity: true},
		"standing_banner":                    {hard: 1, tool: axe, drop: "banner", entity: true},
		"lectern":                            {hard: 2.5, tool: axe, drop: "self", entity: true},
		"wall_sign":                          {hard: 1, tool: axe, drop: "oak_sign", entity: true},
		"wooden_button":                      self(0.5, axe),
		"wooden_pressure_plate":              self(0.5, axe),
	}
	// Live coral fans drop only to silk touch; a wall fan drops the floor fan's item.
	for _, c := range []string{"tube", "brain", "bubble", "fire", "horn"} {
		k[c+"_coral_fan"] = silkOnly(0, none, 0)
		fan := c + "_coral_fan"
		k[c+"_coral_wall_fan"] = ownKind{fn: func(_ map[string]any, _ item.Tool, silk bool) []item.Stack {
			if silk {
				return namedDrop(fan, 1)
			}
			return nil
		}}
	}
	for _, w := range woodNames {
		k[w+"_button"] = self(0.5, axe)
		k[w+"_pressure_plate"] = self(0.5, axe)
		sign := w
		if w == "dark_oak" {
			sign = "darkoak"
		}
		k[sign+"_wall_sign"] = ownKind{hard: 1, tool: axe, drop: w + "_sign", entity: true}
	}
	// Copper: chests, bulbs and the like by their oxidation and wax. Assumed for the 1.21.9 copper chest:
	// copper's hardness 3 and the pickaxe (not checked against a public source).
	for _, wax := range []string{"", "waxed_"} {
		for _, ox := range []string{"", "exposed_", "weathered_", "oxidized_"} {
			k[wax+ox+"copper_chest"] = ownKind{hard: 3, tool: pick, needTool: true, drop: "self", placeable: true, entity: true}
			k[wax+ox+"copper_bulb"] = pickSelf(3)
		}
	}
	return k
}()

// ownKindOf is the kind of a Bedrock name (without "minecraft:").
func ownKindOf(name string) (ownKind, bool) {
	k, ok := ownKinds[strings.TrimPrefix(name, "minecraft:")]
	return k, ok
}

func hasSilk(e []item.Enchantment) bool {
	for _, x := range e {
		if x.Type() == enchantment.SilkTouch {
			return true
		}
	}
	return false
}

// bottomOnly: a pale moss carpet drops itself from its bottom part only.
func bottomOnly(props map[string]any, _ item.Tool, _ bool) []item.Stack {
	if v, _ := props["upper_block_bit"].(uint8); v != 0 {
		return nil
	}
	return selfDrop("pale_moss_carpet", 1)
}

// sweetBerries: a ripe bush gives two or three berries, a growing one with berries one or two.
func sweetBerries(props map[string]any, _ item.Tool, _ bool) []item.Stack {
	switch g, _ := props["growth"].(int32); {
	case g >= 3:
		return namedDrop("sweet_berries", 2+rand.IntN(2))
	case g == 2:
		return namedDrop("sweet_berries", 1+rand.IntN(2))
	}
	return nil
}

// amethystShards: a cluster gives four shards to a pickaxe and two otherwise.
func amethystShards(_ map[string]any, t item.Tool, _ bool) []item.Stack {
	if t.ToolType() == item.TypePickaxe {
		return namedDrop("amethyst_shard", 4)
	}
	return namedDrop("amethyst_shard", 2)
}

// lowerHalf: a door drops its item from its lower half.
func lowerHalf(it string) func(map[string]any, item.Tool, bool) []item.Stack {
	return func(props map[string]any, _ item.Tool, _ bool) []item.Stack {
		if v, _ := props["upper_block_bit"].(uint8); v != 0 {
			return nil
		}
		return namedDrop(it, 1)
	}
}

// namedDrop is n of the item of a Bedrock name, nothing if the server has no such item.
func namedDrop(name string, n int) []item.Stack {
	if n <= 0 {
		return nil
	}
	if it, ok := world.ItemByName("minecraft:"+name, 0); ok {
		return []item.Stack{item.NewStack(it, n)}
	}
	return nil
}

// selfDrop is n of a block's own item.
func selfDrop(name string, n int) []item.Stack { return namedDrop(name, n) }

// breakInfo is a kind's break info for one state.
func (k ownKind) breakInfo(name string, props map[string]any) block.BreakInfo {
	name = strings.TrimPrefix(name, "minecraft:")
	effective := func(t item.Tool) bool { return k.tool != none && t.ToolType() == k.tool }
	harvest := func(t item.Tool) bool { return !k.needTool || t.ToolType() == k.tool }
	drops := func(t item.Tool, e []item.Enchantment) []item.Stack {
		silk := hasSilk(e)
		if k.silk && silk {
			return selfDrop(name, 1)
		}
		if k.shears && t.ToolType() != item.TypeShears && !silk {
			return nil
		}
		if k.fn != nil {
			return k.fn(props, t, silk)
		}
		if k.drop == "" || k.chance > 0 && rand.Float64() >= k.chance && !silk && t.ToolType() != item.TypeShears {
			return nil
		}
		n := 1
		if k.count != [2]int{} {
			n = k.count[0] + rand.IntN(k.count[1]-k.count[0]+1)
		}
		if k.drop == "self" {
			return selfDrop(name, n)
		}
		return namedDrop(k.drop, n)
	}
	info := block.BreakInfo{Hardness: k.hard, BlastResistance: k.hard, Harvestable: harvest, Effective: effective, Drops: drops}
	if k.xp != [2]int{} {
		info.XPDrops = block.XPDropRange(k.xp)
	}
	return info
}

// The stand-ins: every state of a name in ownKinds that is still a registered state with no Go type
// (Dragonfly's unknown blocks: unbreakable, and solid to its block tests) becomes one of our own blocks.
// The model stays a full box that is not model.Solid, as the unknown block's, so nothing the generator
// or the structures test changes; only breaking, replacing and drops do.

// OwnEntityBlock is one of our own blocks that keeps block entity data as it was written (a trapped
// chest's loot table, a banner's patterns, a comparator's output), as Dragonfly's unknown block did.
type OwnEntityBlock struct {
	id   uint16
	data *map[string]any
}

var ownEntityHash = block.NextHash()

// EncodeBlock ...
func (o OwnEntityBlock) EncodeBlock() (string, map[string]any) {
	return OwnBlock{id: o.id}.EncodeBlock()
}

// Hash ...
func (o OwnEntityBlock) Hash() (uint64, uint64) { return ownEntityHash, uint64(o.id) }

// Model ...
func (o OwnEntityBlock) Model() world.BlockModel { return OwnBlock{id: o.id}.Model() }

// EncodeNBT ...
func (o OwnEntityBlock) EncodeNBT() map[string]any {
	if o.data == nil {
		return map[string]any{}
	}
	return maps.Clone(*o.data)
}

// DecodeNBT ...
func (o OwnEntityBlock) DecodeNBT(data map[string]any) any {
	d := maps.Clone(data)
	o.data = &d
	return o
}

// BreakInfo breaks as its kind; a container drops the items it holds (a mansion's decorative trapped
// chest, round 21) and rolls the loot table it still holds and drops it, as a chest does when broken
// unopened.
func (o OwnEntityBlock) BreakInfo() block.BreakInfo {
	info := OwnBlock{id: o.id}.BreakInfo()
	if o.data == nil {
		return info
	}
	table, _ := (*o.data)["LootTable"].(string)
	seed, _ := (*o.data)["LootTableSeed"].(int32)
	held, _ := (*o.data)["Items"].([]any)
	if (table == "" || block.LootFiller == nil) && len(held) == 0 {
		return info
	}
	drops := info.Drops
	info.Drops = func(t item.Tool, e []item.Enchantment) []item.Stack {
		var out []item.Stack
		for _, x := range held {
			if m, ok := x.(map[string]any); ok {
				if st := item.ReadNBT(m, nil); !st.Empty() {
					out = append(out, st)
				}
			}
		}
		if table != "" && block.LootFiller != nil {
			inv := inventory.New(27, nil)
			block.LootFiller(table, seed, inv)
			out = append(out, inv.Items()...)
		}
		return append(out, drops(t, e)...)
	}
	return info
}

// ownCubeModel is the unknown block's model: a full box with solid faces that is not model.Solid.
type ownCubeModel struct{}

// BBox ...
func (ownCubeModel) BBox(p cube.Pos, s world.BlockSource) []cube.BBox {
	return model.Solid{}.BBox(p, s)
}

// FaceSolid ...
func (ownCubeModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return true }

// unbreakableNames are the blocks a survival player cannot mine in the game either, and the liquids and
// air, which are not mined. Fire goes out when punched (Dragonfly's StartBreaking), not by breaking.
var unbreakableNames = map[string]bool{
	"air": true, "water": true, "flowing_water": true, "lava": true, "flowing_lava": true, "bedrock": true,
	"fire": true, "soul_fire": true, "end_gateway": true, "end_portal": true, "end_portal_frame": true,
	"structure_block": true, "structure_void": true, "jigsaw": true, "barrier": true, "portal": true,
}

// CheckBlockBreaks reports why a block a generator writes would not behave as a block for players: it
// has no Go type (Dragonfly's unknown block: unbreakable and solid), it cannot be mined although the game
// mines it, or it is one of our own blocks with no kind (own_kinds.go). "" when it is fine. The
// structures' test uses it too.
func CheckBlockBreaks(b world.Block) string {
	name, _ := b.EncodeBlock()
	short := name[len("minecraft:"):]
	if unbreakableNames[short] {
		return ""
	}
	if _, h := b.Hash(); h == math.MaxUint64 {
		return name + " has no Go type"
	}
	if _, ok := b.(block.Breakable); !ok {
		return name + " cannot be mined"
	}
	switch b.(type) {
	case OwnBlock, OwnEntityBlock:
		if _, ok := ownKindOf(short); !ok {
			return name + " is an own block with no kind"
		}
	}
	return ""
}
