package worldgen

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// Bedrock's data-driven features (Task 4e round 3), interpreted from the feature files read at startup,
// in our own Go from the study of BDS 1.26.60.6 (NOTES-bedrock-exact Q1, S5, T1 and the pseudocode files
// next to them). Every feature draws from the generator its caller passes, as Bedrock's composite
// features share the caller's Random. A feature the reader does not know makes its rule be skipped
// (BedrockData.Skipped), never run half.

// bfeat is a placed feature: it places at a position and gives the position it ended at and whether
// it placed.
type bfeat interface {
	place(d *decoration, r *bmt, p pos) (pos, bool)
}

// codeFeat is a feature written as one function.
type codeFeat func(d *decoration, r *bmt, p pos) bool

func (f codeFeat) place(d *decoration, r *bmt, p pos) (pos, bool) { return p, f(d, r, p) }

// voidFeat adapts a placer that reports nothing (ores, disks).
type voidFeat func(d *decoration, r *bmt, p pos)

func (f voidFeat) place(d *decoration, r *bmt, p pos) (pos, bool) { f(d, r, p); return p, true }

// ---------------------------------------------------------------- Bedrock block names

// Bedrock's names of our states, as the client sees them, interned. A state's name is worked out the
// first time it is asked for.
var (
	bnameLock  sync.Mutex
	bnameIndex = map[string]uint16{}
	bnameList  = []string{""}
	bnameOf    [maxStates]atomic.Uint32 // 0: not worked out yet; else index+1.
)

func internBName(n string) uint16 {
	bnameLock.Lock()
	defer bnameLock.Unlock()
	if i, ok := bnameIndex[n]; ok {
		return i
	}
	i := uint16(len(bnameList))
	bnameList = append(bnameList, n)
	bnameIndex[n] = i
	return i
}

// bname is the Bedrock block name of a state, interned.
func bname(s bs) uint16 {
	if v := bnameOf[s].Load(); v != 0 {
		return uint16(v - 1)
	}
	n, _ := dragonflyBlock(s.name()).EncodeBlock()
	i := internBName(strings.TrimPrefix(n, "minecraft:"))
	bnameOf[s].Store(uint32(i) + 1)
	return i
}

func bnameString(s bs) string {
	i := bname(s)
	bnameLock.Lock()
	defer bnameLock.Unlock()
	return bnameList[i]
}

// bnameSet is a set of interned Bedrock names.
type bnameSet []uint64

func (b *bnameSet) add(i uint16) {
	for int(i>>6) >= len(*b) {
		*b = append(*b, 0)
	}
	(*b)[i>>6] |= 1 << (i & 63)
}

func (b bnameSet) has(i uint16) bool { return int(i>>6) < len(b) && b[i>>6]&(1<<(i&63)) != 0 }

// Bedrock's block tags the feature files test, by Bedrock name.
var bedrockTagMembers = map[string]string{
	"dirt":   "dirt grass_block podzol coarse_dirt mycelium dirt_with_roots moss_block mud muddy_mangrove_roots farmland pale_moss_block grass_path",
	"sand":   "sand red_sand suspicious_sand",
	"gravel": "gravel suspicious_gravel",
	"leaves": "oak_leaves spruce_leaves birch_leaves jungle_leaves acacia_leaves dark_oak_leaves mangrove_leaves cherry_leaves pale_oak_leaves azalea_leaves azalea_leaves_flowered yellow_poplar_leaves orange_poplar_leaves red_poplar_leaves",
	"logs":   "oak_log spruce_log birch_log jungle_log acacia_log dark_oak_log mangrove_log cherry_log pale_oak_log poplar_log mangrove_roots",
}

// legacyBName resolves a descriptor's name and states to a current Bedrock name.
func legacyBName(name string, st map[string]any) string {
	n := strings.TrimPrefix(name, "minecraft:")
	str := func(k string) string { v, _ := st[k].(string); return v }
	switch n {
	case "leaves":
		if t := str("old_leaf_type"); t != "" {
			return t + "_leaves"
		}
		return "oak_leaves"
	case "leaves2":
		if t := str("new_leaf_type"); t != "" {
			return t + "_leaves"
		}
		return "acacia_leaves"
	case "log":
		if t := str("old_log_type"); t != "" {
			return t + "_log"
		}
		return "oak_log"
	case "log2":
		if t := str("new_log_type"); t != "" {
			return t + "_log"
		}
		return "acacia_log"
	case "grass":
		return "grass_block"
	case "tallgrass":
		if str("tall_grass_type") == "fern" {
			return "fern"
		}
		return "short_grass"
	case "dirt":
		if str("dirt_type") == "coarse" {
			return "coarse_dirt"
		}
	case "stone":
		switch t := str("stone_type"); t {
		case "granite", "diorite", "andesite":
			return t
		case "granite_smooth", "diorite_smooth", "andesite_smooth":
			return "polished_" + strings.TrimSuffix(t, "_smooth")
		}
	case "sand":
		if str("sand_type") == "red" {
			return "red_sand"
		}
	case "flowing_water":
		return "water"
	case "flowing_lava":
		return "lava"
	}
	return n
}

// bdesc is a block descriptor: a Bedrock name with its states.
type bdesc struct {
	name   string
	states map[string]any
}

func parseBDesc(raw json.RawMessage) (bdesc, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return bdesc{name: legacyBName(s, nil)}, nil
	}
	var d struct {
		Name   string         `json:"name"`
		States map[string]any `json:"states"`
	}
	if err := json.Unmarshal(raw, &d); err != nil || d.Name == "" {
		return bdesc{}, fmt.Errorf("block %s", raw)
	}
	return bdesc{legacyBName(d.Name, d.States), d.States}, nil
}

// javaStateFor is our state for a Bedrock block, by our own table of the names that differ.
func javaStateFor(b bdesc) (bs, error) {
	st := func(k string) any { return b.states[k] }
	upper := false
	if v, ok := st("upper_block_bit").(bool); ok {
		upper = v
	}
	half := "lower"
	if upper {
		half = "upper"
	}
	var java string
	switch b.name {
	case "snow_layer":
		java = "snow[layers=1]"
	case "dirt_with_roots":
		java = "rooted_dirt"
	case "hardened_clay":
		java = "terracotta"
	case "magma":
		java = "magma_block"
	case "sulfur_spike":
		// Bedrock's dripstone states (the sulfur spring templates' spikes, round 18): hanging 1 points
		// down; Bedrock's "merge" is our tip_merge.
		th, _ := st("dripstone_thickness").(string)
		switch th {
		case "merge":
			th = "tip_merge"
		case "tip", "frustum", "middle", "base":
		default:
			th = "tip"
		}
		vd := "up"
		if h, _ := st("hanging").(bool); h {
			vd = "down"
		}
		java = b.name + "[thickness=" + th + ",vertical_direction=" + vd + ",waterlogged=false]"
	case "waterlily":
		java = "lily_pad"
	case "reeds":
		java = "sugar_cane"
	case "deadbush":
		java = "dead_bush"
	case "azalea_leaves_flowered":
		java = "flowering_azalea_leaves"
	case "melon_block":
		java = "melon"
	case "grass_block", "podzol", "mycelium":
		java = b.name + "[snowy=false]"
	case "water":
		java = "water[level=0]"
	case "lava":
		java = "lava[level=0]"
	case "tall_grass", "large_fern", "sunflower", "lilac", "rose_bush", "peony":
		java = b.name + "[half=" + half + "]"
	case "seagrass":
		switch st("sea_grass_type") {
		case "double_bot":
			java = "tall_seagrass[half=lower]"
		case "double_top":
			java = "tall_seagrass[half=upper]"
		default:
			java = "seagrass"
		}
	case "bee_nest", "beehive":
		java = "bee_nest[facing=south,honey_level=0]"
	case "hanging_roots":
		java = "hanging_roots[waterlogged=false]"
	case "mangrove_roots":
		java = "mangrove_roots[waterlogged=false]"
	case "mangrove_propagule":
		java = "mangrove_propagule[age=4,hanging=true,stage=0,waterlogged=false]"
	case "leaf_litter":
		java = "leaf_litter[facing=north,segment_amount=1]"
	case "wildflowers":
		java = "wildflowers[facing=north,flower_amount=1]"
	case "brown_mushroom_block", "red_mushroom_block":
		java = b.name + "[down=false,east=true,north=true,south=true,up=true,west=true]"
	case "mushroom_stem":
		java = "mushroom_stem[down=false,east=true,north=true,south=true,up=false,west=true]"
	case "sweet_berry_bush":
		java = "sweet_berry_bush[age=3]"
	case "pale_hanging_moss":
		java = "pale_hanging_moss[tip=true]"
	case "pale_moss_carpet":
		java = "pale_moss_carpet[bottom=true,east=none,north=none,south=none,west=none]"
	case "weeping_vines", "twisting_vines":
		age := 0
		if v, ok := st(b.name + "_age").(float64); ok {
			age = min(max(int(v), 0), 25)
		}
		java = fmt.Sprintf("%s[age=%d]", b.name, age)
	case "cave_vines":
		java = "cave_vines_plant[berries=false]"
	case "cave_vines_body_with_berries":
		java = "cave_vines_plant[berries=true]"
	case "cave_vines_head_with_berries":
		java = "cave_vines[age=0,berries=true]"
	case "small_dripleaf_block":
		java = "small_dripleaf[facing=north,half=" + half + ",waterlogged=false]"
	case "big_dripleaf":
		f := cardinals[0]
		if v, ok := st("direction").(float64); ok {
			f = bedrockDirection(int(v))
		}
		if h, _ := st("big_dripleaf_head").(bool); h {
			java = "big_dripleaf[facing=" + f + ",tilt=none,waterlogged=false]"
		} else {
			java = "big_dripleaf_stem[facing=" + f + ",waterlogged=false]"
		}
	case "creaking_heart":
		java = "creaking_heart[axis=y,creaking_heart_state=dormant,natural=true]"
	case "potent_sulfur":
		w, _ := st("potent_sulfur_state").(string)
		if !slices.Contains(potentSulfurStates[:], w) {
			w = "dry"
		}
		java = "potent_sulfur[potent_sulfur_state=" + w + "]"
	case "glow_lichen", "sculk_vein", "cactus_flower":
		return 0, fmt.Errorf("no state for %s", b.name)
	default:
		switch {
		case strings.HasSuffix(b.name, "_log"):
			axis, _ := st("pillar_axis").(string)
			if axis == "" {
				axis = "y"
			}
			java = b.name + "[axis=" + axis + "]"
		default:
			java = b.name
		}
	}
	s, err := safeState(java)
	if err != nil {
		return 0, err
	}
	return s, nil
}

// bedrockDirection names Bedrock's direction state 0..3: south, west, north, east.
func bedrockDirection(d int) string { return [4]string{"south", "west", "north", "east"}[d&3] }

// potentSulfurStates are potent_sulfur's potent_sulfur_state values in the 1.26.60 block palette.
var potentSulfurStates = [5]string{"dry", "wet", "dormant", "erupting", "continuous"}

// safeState makes a state and checks the client has a block for it.
func safeState(java string) (s bs, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("no block for %s", java)
		}
	}()
	s = stateOf(java)
	bname(s)
	return s, nil
}

// bmatch tests a block against a descriptor list (may_replace and the like): names, and tags.
type bmatch struct {
	set   bnameSet
	empty bool
}

func (m *bmatch) has(s bs) bool { return m.set.has(bname(s)) }

var anyTagRe = regexp.MustCompile(`'([a-z_:]+)'`)

func parseBMatch(raw json.RawMessage) (*bmatch, error) {
	m := &bmatch{}
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		m.empty = true
		return m, nil
	}
	var list []json.RawMessage
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
	} else {
		list = []json.RawMessage{raw}
	}
	m.empty = len(list) == 0
	for _, e := range list {
		var t struct {
			Tags string `json:"tags"`
		}
		if json.Unmarshal(e, &t) == nil && t.Tags != "" {
			for _, g := range anyTagRe.FindAllStringSubmatch(t.Tags, -1) {
				members, ok := bedrockTagMembers[strings.TrimPrefix(g[1], "minecraft:")]
				if !ok {
					return nil, fmt.Errorf("block tag %s", g[1])
				}
				for _, n := range strings.Fields(members) {
					m.set.add(internBName(n))
				}
			}
			continue
		}
		d, err := parseBDesc(e)
		if err != nil {
			return nil, err
		}
		m.set.add(internBName(d.name))
		if d.name == "water" {
			m.set.add(internBName("flowing_water"))
		}
	}
	return m, nil
}

// ---------------------------------------------------------------- block classes

var (
	bnAir   = internBName("air")
	bnWater = internBName("water")
)

// bAir is Bedrock's air.
func bAir(s bs) bool { return s.isAir() }

// bWaterMat is a block of water material: water itself, not a plant standing in it.
func bWaterMat(s bs) bool { return bname(s) == bnWater }

// bSolid is BlockSource::isSolidBlockingBlock as the study reads it (p4base): full, solid blocks;
// no plants, liquids, leaves or partial blocks.
func bSolid(s bs) bool { return s.has(fMotion) && s.has(fSturdyUp) && !s.tagged(tagLeaves) }

// bReplantSet are the plants a canopy and a trunk may overwrite (btrees REPL_PLANT).
var bReplantSet = func() (b bnameSet) {
	for _, n := range strings.Fields("short_grass tall_grass fern large_fern deadbush snow_layer vine seagrass leaf_litter wildflowers pink_petals bush firefly_bush short_dry_grass tall_dry_grass") {
		b.add(internBName(n))
	}
	return b
}()

func bReplant(s bs) bool { return bReplantSet.has(bname(s)) }

// bLeaves is any leaves block.
func bLeaves(s bs) bool { return s.tagged(tagLeaves) || bnLeaves.has(bname(s)) }

var bnLeaves = bnameSetOf(bedrockTagMembers["leaves"])

// ---------------------------------------------------------------- reading the files

// featCompiler builds features from the data, once each.
type featCompiler struct {
	bd    *BedrockData
	slots map[string]*featSlot
	ores  map[string]*bedrockOre // The ore features built, for tests.
}

type featSlot struct {
	f   bfeat
	err error
	on  bool // Being built: a reference back to it is a cycle, refused as broken data.
}

// errUnimpl marks a feature the reader does not know yet.
type errUnimpl string

func (e errUnimpl) Error() string { return string(e) }

func unimpl(f string, a ...any) error { return errUnimpl(fmt.Sprintf(f, a...)) }

// isUnimpl says whether an error only says a feature is not written yet.
func isUnimpl(err error) bool { var u errUnimpl; return errors.As(err, &u) }

// biomesWithTag is the set of our biomes whose pack file carries a tag.
func (bd *BedrockData) biomesWithTag(tag string) (set [2]uint64) {
	for i := range biomeTableNames {
		if b := bd.biomes[biomeRowDefs[i].String()]; b != nil && b.tags[tag] {
			set[i>>6] |= 1 << (i & 63)
		}
	}
	return set
}

// bedrockHeight is the chunk heightmap Bedrock's features read (query.heightmap and the legacy tree y,
// Q1.4): one above the highest block that is not air, leaves or a plant that lets things through.
func (d *decoration) bedrockHeight(x, z int) int { return d.height(hmMotionNoLeaves, x, z) }
