package java261

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"
)

func rid(name string, props map[string]any) uint32 {
	world.DefaultBlockRegistry.Finalize()
	id, ok := world.DefaultBlockRegistry.StateToRuntimeID(name, props)
	if !ok {
		panic("unknown block " + name)
	}
	return id
}

func TestStateRule(t *testing.T) {
	require.Equal(t, uint16(0), JavaState(rid("minecraft:air", nil)))
	require.Equal(t, uint16(137), JavaState(rid("minecraft:oak_log", map[string]any{"pillar_axis": "y"})))
}

func TestEveryJavaBlockHasExactStateCount(t *testing.T) {
	var blocks []struct {
		Name       string `json:"name"`
		MinStateID int    `json:"minStateId"`
		MaxStateID int    `json:"maxStateId"`
		States     []struct {
			NumValues int `json:"num_values"`
		} `json:"states"`
	}
	raw, err := os.ReadFile("testdata/blocks.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &blocks))
	require.Len(t, blocks, 1168)
	for _, b := range blocks {
		n := 1
		for _, s := range b.States {
			n *= s.NumValues
		}
		require.Equal(t, b.MaxStateID-b.MinStateID+1, n, b.Name)
	}
}

func TestUnmappedBlockIsStone(t *testing.T) {
	var logged []string
	logf := func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	src := func(id uint32) (string, map[string]any, bool) {
		switch id {
		case 0:
			return "minecraft:air", nil, true
		case 1, 2:
			return "minecraft:made_up", map[string]any{"a": int32(id)}, true
		case 3:
			return "minecraft:oak_log", map[string]any{"pillar_axis": "y"}, true
		case 4: // a property value the mapping lacks: the block's default state, not stone
			return "minecraft:oak_log", map[string]any{"pillar_axis": "w"}, true
		case 5: // a property the mapping does not know: still mapped, but not exact
			return "minecraft:oak_log", map[string]any{"pillar_axis": "y", "brand_new": int32(1)}, true
		}
		return "", nil, false
	}
	states, st, _ := buildStates(6, src, logf)
	stone := JavaState(rid("minecraft:stone", nil))
	require.Equal(t, uint16(1), stone)
	require.Equal(t, []uint16{0, stone, stone, 137, 137, 137}, states)
	require.NotEqual(t, stone, states[4])
	require.Equal(t, Stats{Exact: 2, Fallback: 2, Stone: 2}, st)
	require.Len(t, logged, 2, "one line per block name, not per state")
	require.Contains(t, logged[0], "minecraft:made_up")
}

// javaBlock is the reference copy of the Java block list.
type javaBlock struct {
	Name         string `json:"name"`
	DefaultState int    `json:"defaultState"`
	MinStateID   int    `json:"minStateId"`
	MaxStateID   int    `json:"maxStateId"`
	States       []struct {
		Name      string   `json:"name"`
		Type      string   `json:"type"`
		NumValues int      `json:"num_values"`
		Values    []string `json:"values"`
	} `json:"states"`
}

func loadJava(t *testing.T) map[string]*javaBlock {
	raw, err := os.ReadFile("testdata/blocks.json")
	require.NoError(t, err)
	var bl []javaBlock
	require.NoError(t, json.Unmarshal(raw, &bl))
	byName := map[string]*javaBlock{}
	for i := range bl {
		byName["minecraft:"+bl[i].Name] = &bl[i]
	}
	return byName
}

// state computes a Java state ID by the plan rule, independently of the generator: the default
// state with the given properties changed, last property fastest, true before false.
func (b *javaBlock) state(t *testing.T, props map[string]string) uint16 {
	idx := make([]int, len(b.States))
	rem := b.DefaultState - b.MinStateID
	for i := len(b.States) - 1; i >= 0; i-- {
		idx[i] = rem % b.States[i].NumValues
		rem /= b.States[i].NumValues
	}
	used := 0
	id, stride := b.MinStateID, 1
	for i := len(b.States) - 1; i >= 0; i-- {
		st := b.States[i]
		if v, ok := props[st.Name]; ok {
			used++
			idx[i] = -1
			if st.Type == "bool" {
				idx[i] = map[string]int{"true": 0, "false": 1}[v]
			} else {
				for j, x := range st.Values {
					if x == v {
						idx[i] = j
					}
				}
			}
			require.GreaterOrEqual(t, idx[i], 0, b.Name+" "+st.Name+"="+v)
		}
		id += idx[i] * stride
		stride *= st.NumValues
	}
	require.Equal(t, len(props), used, "unknown property for "+b.Name)
	return uint16(id)
}

// TestMappedShare replaces a flat 98% goal. The Dragonfly palette holds 23960 states, many of
// which no world can contain (hanging signs with impossible combinations, facing values 0 and 1
// on blocks that only face sideways) or that Java does not have at all (wool and concrete stairs,
// sulfur, cinnabar). A share over all of them says nothing about what players see, so this checks
// what matters: no block with a Go type whose name exists in Java ends up as stone. The stone
// bounds below were measured with Dragonfly v0.11.6 and minecraft-data bd8b5c6: 2861 states in 152
// block names, all with no Java counterpart (wool and concrete slabs and stairs, sulfur, cinnabar,
// frames, poplar). A higher number means a mapping was lost.
func TestMappedShare(t *testing.T) {
	java := loadJava(t)
	world.DefaultBlockRegistry.Finalize()
	n := world.DefaultBlockRegistry.BlockCount()
	states, st, kinds := buildStates(n, world.DefaultBlockRegistry.RuntimeIDToState, func(string, ...any) {})
	blockOf := func(s uint16) string {
		for name, b := range java {
			if int(s) >= b.MinStateID && int(s) <= b.MaxStateID {
				return name
			}
		}
		return ""
	}
	stone := java["minecraft:stone"].MinStateID
	stoneNames := map[string]int{}
	typed, typedStone, renamed, typedFallback := 0, 0, 0, 0
	renamedPairs, fallbackNames := map[string]bool{}, map[string]int{}
	for i := 0; i < n; i++ {
		b, _ := world.DefaultBlockRegistry.BlockByRuntimeID(uint32(i))
		if reflect.TypeOf(b).Name() == "unknownBlock" {
			continue
		}
		name, _, _ := world.DefaultBlockRegistry.RuntimeIDToState(uint32(i))
		typed++
		if _, inJava := java[name]; inJava && name != "minecraft:stone" {
			// The mapping may rename (a floor head with a side facing is a wall head), but never to stone.
			require.NotEqual(t, "minecraft:stone", blockOf(states[i]), "%s state %d", name, i)
			if blockOf(states[i]) != name {
				renamed++
				renamedPairs[name+" -> "+blockOf(states[i])] = true
			}
		}
		if kinds[i] == kindFallback {
			typedFallback++
			fallbackNames[name]++
		}
		if int(states[i]) == stone && name != "minecraft:stone" {
			typedStone++
			stoneNames[name]++
		}
	}
	var names []string
	for k := range stoneNames {
		names = append(names, k)
	}
	sort.Strings(names)
	t.Logf("all palette states: %d exact, %d nearest or default, %d stone (of %d)", st.Exact, st.Fallback, st.Stone, n)
	t.Logf("typed blocks: %d states, %d stone in %d block names, %d resolved to a differently named Java block", typed, typedStone, len(names), renamed)
	t.Logf("stone block names (no Java block of that name): %v", names)
	var pairs, fb []string
	for k := range renamedPairs {
		pairs = append(pairs, k)
	}
	for k, v := range fallbackNames {
		fb = append(fb, fmt.Sprintf("%s:%d", k, v))
	}
	sort.Strings(pairs)
	sort.Strings(fb)
	t.Logf("renamed: %q", pairs)
	t.Logf("typed fallback states: %d, in %d names: %v", typedFallback, len(fb), fb)
	// The blocks the mapping turns into a differently named Java block, all correct renames.
	require.Equal(t, []string{
		"minecraft:copper_torch -> minecraft:copper_wall_torch", "minecraft:creeper_head -> minecraft:creeper_wall_head",
		"minecraft:dragon_head -> minecraft:dragon_wall_head", "minecraft:kelp -> minecraft:kelp_plant",
		"minecraft:melon_stem -> minecraft:attached_melon_stem", "minecraft:piglin_head -> minecraft:piglin_wall_head",
		"minecraft:player_head -> minecraft:player_wall_head", "minecraft:pumpkin_stem -> minecraft:attached_pumpkin_stem",
		"minecraft:redstone_torch -> minecraft:redstone_wall_torch", "minecraft:skeleton_skull -> minecraft:skeleton_wall_skull",
		"minecraft:snow -> minecraft:snow_block", "minecraft:soul_torch -> minecraft:soul_wall_torch",
		"minecraft:stone_stairs -> minecraft:cobblestone_stairs", "minecraft:torch -> minecraft:wall_torch",
		"minecraft:wither_skeleton_skull -> minecraft:wither_skeleton_wall_skull", "minecraft:zombie_head -> minecraft:zombie_wall_head",
	}, pairs)
	// States of Java blocks that took a nearest or default state, or ignored properties the mapping
	// lacks (fence, pane and bar connections, fire age, impossible facings), measured: 2920 in 153 names.
	require.LessOrEqual(t, typedFallback, 2920)
	require.LessOrEqual(t, len(fb), 153)
	require.LessOrEqual(t, typedStone, 2861)
	require.LessOrEqual(t, len(names), 152)
}

// TestSurvivalSet checks the common vanilla blocks state by state, with properties, against the
// state rule applied to the reference Java block list.
func TestSurvivalSet(t *testing.T) {
	java := loadJava(t)
	type P = map[string]string
	check := func(name string, bedrock map[string]any, javaName string, props P) {
		t.Helper()
		want := java["minecraft:"+javaName].state(t, props)
		require.Equal(t, want, JavaState(rid(name, bedrock)), "%s %v", name, bedrock)
	}
	for _, axis := range []string{"x", "y", "z"} {
		check("minecraft:oak_log", map[string]any{"pillar_axis": axis}, "oak_log", P{"axis": axis})
	}
	check("minecraft:grass_block", nil, "grass_block", P{"snowy": "false"})
	// Stairs: weirdo_direction 0..3 is east, west, south, north.
	for dir, face := range []string{"east", "west", "south", "north"} {
		for up, half := range []string{"bottom", "top"} {
			for corner, shape := range map[string]string{"none": "straight", "inner_left": "inner_left", "inner_right": "inner_right", "outer_left": "outer_left", "outer_right": "outer_right"} {
				check("minecraft:oak_stairs", map[string]any{"weirdo_direction": int32(dir), "upside_down_bit": uint8(up), "minecraft:corner": corner},
					"oak_stairs", P{"facing": face, "half": half, "shape": shape, "waterlogged": "false"})
			}
		}
	}
	for _, half := range []string{"bottom", "top"} {
		check("minecraft:oak_slab", map[string]any{"minecraft:vertical_half": half}, "oak_slab", P{"type": half, "waterlogged": "false"})
	}
	check("minecraft:oak_double_slab", map[string]any{"minecraft:vertical_half": "bottom"}, "oak_slab", P{"type": "double", "waterlogged": "false"})
	// Doors: the Bedrock cardinal direction is rotated against the Java facing, as the mapping has it.
	for _, d := range []struct{ card, face string }{{"east", "north"}, {"west", "south"}, {"north", "west"}, {"south", "east"}} {
		for open := 0; open < 2; open++ {
			for upper := 0; upper < 2; upper++ {
				for hinge := 0; hinge < 2; hinge++ {
					check("minecraft:wooden_door", map[string]any{"minecraft:cardinal_direction": d.card, "open_bit": uint8(open), "upper_block_bit": uint8(upper), "door_hinge_bit": uint8(hinge)},
						"oak_door", P{"facing": d.face, "open": []string{"false", "true"}[open], "half": []string{"lower", "upper"}[upper],
							"hinge": []string{"left", "right"}[hinge], "powered": "false"})
				}
			}
		}
	}
	for persistent := 0; persistent < 2; persistent++ {
		check("minecraft:oak_leaves", map[string]any{"persistent_bit": uint8(persistent), "update_bit": uint8(0)}, "oak_leaves",
			P{"persistent": []string{"false", "true"}[persistent], "distance": "7", "waterlogged": "false"})
	}
	for depth := 0; depth < 16; depth++ {
		check("minecraft:water", map[string]any{"liquid_depth": int32(depth)}, "water", P{"level": strconv.Itoa(depth)})
		check("minecraft:lava", map[string]any{"liquid_depth": int32(depth)}, "lava", P{"level": strconv.Itoa(depth)})
	}
	for h := 0; h < 8; h++ {
		check("minecraft:snow_layer", map[string]any{"covered_bit": uint8(0), "height": int32(h)}, "snow", P{"layers": strconv.Itoa(h + 1)})
	}
	check("minecraft:seagrass", map[string]any{"sea_grass_type": "default"}, "seagrass", P{})
	check("minecraft:seagrass", map[string]any{"sea_grass_type": "double_top"}, "tall_seagrass", P{"half": "upper"})
	check("minecraft:seagrass", map[string]any{"sea_grass_type": "double_bot"}, "tall_seagrass", P{"half": "lower"})

	// Every ore Dragonfly has maps to the Java ore of the same name in its default state.
	ores := 0
	world.DefaultBlockRegistry.Finalize()
	for i := 0; i < world.DefaultBlockRegistry.BlockCount(); i++ {
		name, props, _ := world.DefaultBlockRegistry.RuntimeIDToState(uint32(i))
		if !strings.HasSuffix(name, "_ore") && name != "minecraft:ancient_debris" || strings.Contains(name, "lit_") {
			continue
		}
		b, ok := java[name]
		if !ok {
			continue
		}
		require.Equal(t, uint16(b.DefaultState), JavaState(uint32(i)), "%s %v", name, props)
		ores++
	}
	require.GreaterOrEqual(t, ores, 16)
}

func TestRegistriesMatchLoginPacket(t *testing.T) {
	regs := Registries()
	require.Len(t, regs, 28)
	require.Equal(t, "minecraft:worldgen/biome", regs[0].ID)
	require.Contains(t, regs[0].Entries, "minecraft:plains")
}

func TestPacketID(t *testing.T) {
	raw, err := os.ReadFile("testdata/packets.json")
	require.NoError(t, err)
	var ref map[string]map[string]string
	require.NoError(t, json.Unmarshal(raw, &ref))
	total := 0
	for key, m := range ref {
		state, dir, _ := strings.Cut(key, "/")
		for hex, name := range m {
			want, err := strconv.ParseInt(strings.TrimPrefix(hex, "0x"), 16, 32)
			require.NoError(t, err)
			require.Equal(t, int32(want), PacketID(state, dir, name), key+" "+name)
			total++
		}
	}
	require.Greater(t, total, 200)
	require.Equal(t, int32(-1), PacketID("play", "toClient", "no_such_packet"))
	require.Equal(t, int32(0x08), PacketID("play", "toClient", "block_change"))
}

func TestJavaItemRoundTrip(t *testing.T) {
	for _, name := range []string{"minecraft:diamond_pickaxe", "minecraft:oak_log"} {
		id, ok := JavaItem(name, 0)
		require.True(t, ok, name)
		back, meta, ok := BedrockItem(id)
		require.True(t, ok)
		require.Equal(t, name, back)
		require.Zero(t, meta)
	}
	id, ok := JavaItem("minecraft:stone", 0)
	require.True(t, ok)
	require.Equal(t, int32(1), id)
	// Variants keep the base ID; the variant itself travels as a component.
	base, _ := JavaItem("minecraft:potion", 0)
	withMeta, ok := JavaItem("minecraft:potion", 5)
	require.True(t, ok)
	require.Equal(t, base, withMeta)
	_, ok = JavaItem("minecraft:no_such_item", 0)
	require.False(t, ok)
	_, _, ok = BedrockItem(-5)
	require.False(t, ok)
}

func TestJavaBiome(t *testing.T) {
	require.Equal(t, int32(0), JavaBiome("minecraft:badlands"))
	require.Equal(t, JavaBiome("minecraft:plains"), JavaBiome("plains"))
	require.Equal(t, int32(-1), JavaBiome("nope"))
	require.Equal(t, int32(775), int32(ProtocolVersion))
}

func TestMemoryAdded(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	heap := func() int64 {
		var m runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m)
		return int64(m.HeapAlloc)
	}
	for _, step := range []struct {
		name string
		do   func()
	}{
		{"block states", func() { JavaState(0) }},
		{"items", func() { JavaItem("minecraft:stone", 0) }},
		{"biomes", func() { JavaBiome("plains") }},
		{"packets", func() { PacketID("play", "toClient", "block_change") }},
	} {
		before := heap()
		step.do()
		t.Logf("%s: %d bytes kept", step.name, heap()-before)
	}
}

// javaItems reads the reference copy of the Java item list.
func javaItems(t *testing.T) map[string]int32 {
	raw, err := os.ReadFile("testdata/items.json")
	require.NoError(t, err)
	var m map[string]int32
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

func TestBedrockItemNames(t *testing.T) {
	java := javaItems(t)
	for _, name := range []string{"oak_slab", "redstone_ore", "deepslate_redstone_ore", "smoker", "blast_furnace", "furnace", "redstone_lamp", "campfire"} {
		id, ok := JavaItem("minecraft:"+name, 0)
		require.True(t, ok, name)
		require.Equal(t, java[name], id, name)
		back, meta, ok := BedrockItem(id)
		require.True(t, ok, name)
		require.Equal(t, "minecraft:"+name, back)
		require.Zero(t, meta)
	}
	// No Java item may come back as a lit or double-slab name.
	for id := int32(0); id < 2000; id++ {
		if name, _, ok := BedrockItem(id); ok {
			require.False(t, strings.HasPrefix(name, "minecraft:lit_") || strings.HasSuffix(name, "_double_slab") || name == "minecraft:unknown", name)
		}
	}
	_, ok := JavaItem("minecraft:unknown", 0)
	require.False(t, ok)
	// The lit forms still go forward, to the same Java item.
	lit, ok := JavaItem("minecraft:lit_redstone_ore", 0)
	require.True(t, ok)
	require.Equal(t, java["redstone_ore"], lit)
}

// TestColourItems checks beds and banners against Dragonfly: the item it writes for each colour
// must map to the Java item of that colour, and back.
func TestColourItems(t *testing.T) {
	java := javaItems(t)
	for _, c := range item.Colours() {
		for kind, mk := range map[string]func() world.Item{
			"bed":    func() world.Item { return block.Bed{Colour: c} },
			"banner": func() world.Item { return block.Banner{Colour: c} },
		} {
			name, meta := mk().EncodeItem()
			want, found := java[c.String()+"_"+kind]
			require.True(t, found, c.String()+"_"+kind)
			id, ok := JavaItem(name, meta)
			require.True(t, ok, "%s %d", name, meta)
			require.Equal(t, want, id, "%s meta %d", name, meta)
			back, bmeta, ok := BedrockItem(id)
			require.True(t, ok)
			require.Equal(t, name, back)
			require.Equal(t, meta, bmeta)
		}
	}
	_, ok := JavaItem("minecraft:bed", 16)
	require.False(t, ok, "colour out of range")
	_, ok = JavaItem("minecraft:oak_log", 3)
	require.False(t, ok, "meta on an item without variants")
}

// TestEveryDragonflyItemWithMeta: an item Dragonfly writes with a meta either maps, to the item
// of its variant or to the one component item, or does not map at all; never to a wrong item.
func TestEveryDragonflyItemWithMeta(t *testing.T) {
	java := javaItems(t)
	withMeta, mapped := 0, 0
	var unmapped []string
	for _, it := range world.Items() {
		name, meta := it.EncodeItem()
		if meta == 0 {
			continue
		}
		withMeta++
		id, ok := JavaItem(name, meta)
		if !ok {
			unmapped = append(unmapped, fmt.Sprintf("%s#%d", name, meta))
			continue
		}
		mapped++
		base := strings.TrimPrefix(name, "minecraft:")
		switch base {
		case "bed", "banner":
		case "arrow":
			require.Equal(t, java["tipped_arrow"], id)
		default:
			require.Equal(t, java[base], id, name)
		}
	}
	sort.Strings(unmapped)
	t.Logf("%d Dragonfly items with a meta: %d map, %d do not: %v", withMeta, mapped, len(unmapped), unmapped)
	require.Empty(t, unmapped)
}

func TestFlowingLiquids(t *testing.T) {
	java := loadJava(t)
	for _, liquid := range []string{"water", "lava"} {
		for depth := 1; depth < 16; depth++ {
			want := java["minecraft:"+liquid].state(t, map[string]string{"level": strconv.Itoa(depth)})
			require.Equal(t, want, JavaState(rid("minecraft:flowing_"+liquid, map[string]any{"liquid_depth": int32(depth)})), "flowing_%s %d", liquid, depth)
		}
	}
}

// stairSource is a tiny block source holding a few stairs.
type stairSource struct {
	world.BlockSource
	at map[cube.Pos]world.Block
}

func (s stairSource) Block(p cube.Pos) world.Block {
	if b, ok := s.at[p]; ok {
		return b
	}
	return block.Air{}
}

// TestStairShapeFromNeighbours does not take the shape from Dragonfly's corner names. Dragonfly
// works the corner out from the neighbouring stairs; the expected Java shape is worked out here
// from the same neighbours by the vanilla rule: a stair in front turned to my left makes
// outer_left, to my right outer_right; a stair behind turned to my left inner_left, right inner_right.
func TestStairShapeFromNeighbours(t *testing.T) {
	java := loadJava(t)
	left := map[cube.Direction]cube.Direction{cube.North: cube.West, cube.West: cube.South, cube.South: cube.East, cube.East: cube.North}
	right := map[cube.Direction]cube.Direction{}
	for k, v := range left {
		right[v] = k
	}
	stair := func(f cube.Direction, up bool, c block.StairsCorner) block.Stairs {
		return block.Stairs{Block: block.Planks{Wood: block.OakWood()}, Facing: f, UpsideDown: up, Corner: c}
	}
	shapes := map[string]bool{}
	for f := range left {
		for _, up := range []bool{false, true} {
			for _, sc := range []struct {
				name           string
				front, back    cube.Direction
				hasFront, hasB bool
				shape          string
			}{
				{"none", 0, 0, false, false, "straight"},
				{"front left", left[f], 0, true, false, "outer_left"},
				{"front right", right[f], 0, true, false, "outer_right"},
				{"back left", 0, left[f], false, true, "inner_left"},
				{"back right", 0, right[f], false, true, "inner_right"},
			} {
				src := stairSource{at: map[cube.Pos]world.Block{}}
				centre := cube.Pos{}
				if sc.hasFront {
					src.at[centre.Side(f.Face())] = stair(sc.front, up, block.NoStairsCorner())
				}
				if sc.hasB {
					src.at[centre.Side(f.Opposite().Face())] = stair(sc.back, up, block.NoStairsCorner())
				}
				derived := stair(f, up, block.NoStairsCorner()).DeriveState(centre, src)
				half := "bottom"
				if up {
					half = "top"
				}
				want := java["minecraft:oak_stairs"].state(t, map[string]string{"facing": f.String(), "half": half, "shape": sc.shape, "waterlogged": "false"})
				require.Equal(t, want, JavaState(world.BlockRuntimeID(derived)), "%s facing %s up=%v", sc.name, f, up)
				shapes[sc.shape] = true
			}
		}
	}
	require.Len(t, shapes, 5)
}

func TestJavaStateNeedsFinalizedRegistry(t *testing.T) {
	require.False(t, registryReady(world.NewBlockRegistry()), "an unfinalized registry must be reported as not ready")
	world.DefaultBlockRegistry.Finalize()
	require.True(t, registryReady(world.DefaultBlockRegistry))
}

func BenchmarkJavaState(b *testing.B) {
	world.DefaultBlockRegistry.Finalize()
	JavaState(0)
	n := uint32(world.DefaultBlockRegistry.BlockCount())
	b.ReportAllocs()
	b.ResetTimer()
	var sum uint16
	for i := 0; i < b.N; i++ {
		sum += JavaState(uint32(i) % n)
	}
	_ = sum
}
