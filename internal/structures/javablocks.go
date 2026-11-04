package structures

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// Templates name Java block states (N1 1.3); BDS turns them into Bedrock states with a table built into
// the server, which is not in any data file. This is our own conversion: Java's property names and
// values rewritten as Bedrock 1.26's, then the closest of Dragonfly's registered states of that name
// (the most properties equal). Names are what the golden tests compare; states are our best mapping.

// rotations of horizontal facings, clockwise (N2 1.2: rot1 turns N->E->S->W).
var cwFacing = map[string]string{"north": "east", "east": "south", "south": "west", "west": "north"}

func rotateFacing(f string, rot int) string {
	for range rot {
		if n, ok := cwFacing[f]; ok {
			f = n
		}
	}
	return f
}

// rotateProps turns a Java state's properties by rot clockwise quarter turns.
func rotateProps(p map[string]string, rot int) map[string]string {
	if rot == 0 {
		return p
	}
	out := make(map[string]string, len(p))
	sides := map[string]string{}
	for k, v := range p {
		switch k {
		case "facing":
			v = rotateFacing(v, rot)
		case "axis":
			if rot%2 == 1 {
				switch v {
				case "x":
					v = "z"
				case "z":
					v = "x"
				}
			}
		case "rotation":
			n, _ := strconv.Atoi(v)
			v = strconv.Itoa((n + 4*rot) % 16)
		case "north", "east", "south", "west":
			sides[rotateFacing(k, rot)] = v
			continue
		}
		out[k] = v
	}
	for k, v := range sides {
		out[k] = v
	}
	return out
}

// javaRenames are Java names whose Bedrock 1.26 block has another name.
var javaRenames = map[string]string{
	"cave_air": "air", "void_air": "air", "note_block": "noteblock", "stone_slab": "normal_stone_slab",
	"stone_stairs": "normal_stone_stairs", "cobblestone_stairs": "stone_stairs", "spawner": "mob_spawner",
	"short_grass": "short_grass", "grass": "short_grass", "dirt_path": "grass_path", "snow_block": "snow",
	"snow": "snow_layer", "melon": "melon_block", "lily_pad": "waterlily", "cobweb": "web", "sugar_cane": "reeds",
	"nether_bricks": "nether_brick", "magma_block": "magma", "redstone_wire": "redstone_wire",
	"jack_o_lantern": "lit_pumpkin", "slime_block": "slime", "rose_bush": "rose_bush", "dead_bush": "deadbush",
	"powered_rail": "golden_rail", "terracotta": "hardened_clay", "end_stone_bricks": "end_bricks",
	"water": "water", "lava": "lava", "piston_head": "piston_arm_collision", "tripwire": "trip_wire",
	"wall_torch": "torch", "soul_wall_torch": "soul_torch", "redstone_wall_torch": "redstone_torch",
	"oak_sign": "standing_sign", "oak_wall_sign": "wall_sign", "skeleton_wall_skull": "skeleton_skull",
}

var cardinalBlocks = []string{"chest", "trapped_chest", "furnace", "blast_furnace", "smoker", "lectern", "campfire",
	"soul_campfire", "loom", "stonecutter_block", "barrel", "carved_pumpkin", "lit_pumpkin", "_door", "beehive",
	"bee_nest", "repeater", "comparator", "_fence_gate", "grindstone", "bell", "anvil", "_bed", "decorated_pot",
	"vault", "chiseled_bookshelf", "pink_petals", "wildflowers", "leaf_litter", "copper_golem_statue"}

// stairs and trapdoor direction numbers (Bedrock's weirdo_direction and direction): east 0, west 1,
// south 2, north 3.
var weirdo = map[string]int32{"east": 0, "west": 1, "south": 2, "north": 3}

// facingDirection is Bedrock's facing_direction: 0 down, 1 up, 2 north, 3 south, 4 west, 5 east.
var facingDirection = map[string]int32{"down": 0, "up": 1, "north": 2, "south": 3, "west": 4, "east": 5}

// bedrockOf rewrites a Java state as a Bedrock name and wanted properties.
func bedrockOf(name string, p map[string]string) (string, map[string]any) {
	want := map[string]any{}
	if n, ok := javaRenames[name]; ok {
		name = n
	}
	flag := func(k string) bool { return p[k] == "true" }
	switch {
	case strings.HasSuffix(name, "_stairs"):
		want["weirdo_direction"] = weirdo[p["facing"]]
		want["upside_down_bit"] = p["half"] == "top"
	case strings.HasSuffix(name, "_slab"):
		switch p["type"] {
		case "double":
			name = strings.TrimSuffix(name, "_slab") + "_double_slab"
		case "top":
			want["minecraft:vertical_half"] = "top"
		default:
			want["minecraft:vertical_half"] = "bottom"
		}
	case strings.HasSuffix(name, "_trapdoor"):
		want["direction"] = weirdo[p["facing"]]
		want["upside_down_bit"] = p["half"] == "top"
		want["open_bit"] = flag("open")
	case strings.HasSuffix(name, "_wall"):
		for _, s := range []string{"north", "east", "south", "west"} {
			v := p[s]
			if v == "low" {
				v = "short"
			}
			want["wall_connection_type_"+s] = v
		}
		want["wall_post_bit"] = flag("up")
	case name == "candle" || strings.HasSuffix(name, "_candle"):
		n, _ := strconv.Atoi(p["candles"])
		want["candles"] = int32(max(n-1, 0))
		want["lit"] = flag("lit")
	case strings.HasSuffix(name, "lantern"):
		want["hanging"] = flag("hanging")
	case name == "snow_layer":
		n, _ := strconv.Atoi(p["layers"])
		want["height"] = int32(max(n-1, 0))
	case name == "water" || name == "lava":
		n, _ := strconv.Atoi(p["level"])
		want["liquid_depth"] = int32(n)
	case name == "ladder" || name == "skeleton_skull" || name == "wall_sign":
		want["facing_direction"] = facingDirection[p["facing"]]
	case name == "torch" || name == "soul_torch" || name == "redstone_torch":
		if f, ok := p["facing"]; ok {
			want["torch_facing_direction"] = f
		} else {
			want["torch_facing_direction"] = "top"
		}
		if name == "redstone_torch" && p["lit"] == "false" {
			name = "unlit_redstone_torch"
		}
	case name == "comparator" || name == "repeater":
		if flag("powered") {
			name = "powered_" + name
		} else {
			name = "unpowered_" + name
		}
	case name == "redstone_lamp" && flag("lit"):
		name = "lit_redstone_lamp"
	case name == "redstone_wire":
		n, _ := strconv.Atoi(p["power"])
		want["redstone_signal"] = int32(n)
	case name == "sculk_sensor":
		want["sculk_sensor_phase"] = int32(0)
	}
	if a, ok := p["axis"]; ok {
		want["pillar_axis"] = a
	}
	if f, ok := p["facing"]; ok {
		for _, c := range cardinalBlocks {
			if name == c || strings.HasSuffix(name, c) {
				want["minecraft:cardinal_direction"] = f
			}
		}
	}
	if strings.HasSuffix(name, "_door") {
		want["upper_block_bit"] = p["half"] == "upper"
		want["door_hinge_bit"] = p["hinge"] == "right"
		want["open_bit"] = flag("open")
	}
	if strings.HasSuffix(name, "campfire") {
		want["extinguished"] = p["lit"] == "false"
	}
	if name == "furnace" && flag("lit") {
		name = "lit_furnace"
	}
	return name, want
}

// registryIndex groups Dragonfly's registered states by name.
type registryIndex struct {
	once   sync.Once
	byName map[string][]regState
}

type regState struct {
	rid   uint32
	props map[string]string
}

var registry registryIndex

func propString(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "1"
		}
		return "0"
	case uint8:
		return strconv.Itoa(int(x))
	}
	return fmt.Sprint(v)
}

func (r *registryIndex) load() {
	r.once.Do(func() {
		world.DefaultBlockRegistry.Finalize()
		r.byName = map[string][]regState{}
		for rid := range uint32(world.DefaultBlockRegistry.BlockCount()) {
			b, ok := world.BlockByRuntimeID(rid)
			if !ok {
				continue
			}
			n, props := b.EncodeBlock()
			ps := make(map[string]string, len(props))
			for k, v := range props {
				ps[k] = propString(v)
			}
			n = strings.TrimPrefix(n, "minecraft:")
			r.byName[n] = append(r.byName[n], regState{rid, ps})
		}
	})
}

// converter caches each template state's runtime ID per rotation.
type converter struct {
	cache sync.Map // string key -> convResult
}

type convResult struct {
	rid      uint32
	ok       bool
	name     string // Bedrock name.
	waterlog bool
}

var convert converter

// state gives the Dragonfly runtime ID of a template state turned by rot. ok is false for a block
// Dragonfly does not have (it is then not placed) and for structure voids.
func (c *converter) state(st tState, rot int) convResult {
	key := st.name + fmt.Sprint(st.props) + strconv.Itoa(rot)
	if v, ok := c.cache.Load(key); ok {
		return v.(convResult)
	}
	res := convResult{}
	if st.name != "structure_void" && st.name != "jigsaw" {
		p := rotateProps(st.props, rot)
		name, want := bedrockOf(st.name, p)
		res.name, res.waterlog = name, p["waterlogged"] == "true"
		registry.load()
		best, bestN := -1, -1
		for i, s := range registry.byName[name] {
			n := 0
			for k, v := range want {
				if s.props[k] == propString(v) {
					n++
				}
			}
			if n > bestN {
				best, bestN = i, n
			}
		}
		if best >= 0 {
			res.rid, res.ok = registry.byName[name][best].rid, true
		}
	}
	c.cache.Store(key, res)
	return res
}
