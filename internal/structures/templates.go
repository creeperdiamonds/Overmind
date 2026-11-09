package structures

import (
	"strings"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Structure templates are Java-format structure NBT, gzip and big-endian, in the install's behaviour
// packs (N1 1.1, 1.3): size, palette (or palettes) of Java block names with Properties, blocks with a
// palette index and optional NBT. They are read from the install when a structure first needs them.

// tState is one palette entry.
type tState struct {
	name  string            // Java name without "minecraft:".
	props map[string]string // Java properties.
}

// tBlock is one block of a template.
type tBlock struct {
	pos   [3]int
	state int
	nbt   map[string]any
}

// Template is a parsed structure template.
type Template struct {
	Path    string
	Size    [3]int
	palette []tState
	blocks  []tBlock  // In file order: jigsaws and chests are taken in this order (N2 1.2 "template order").
	jigsaws []jigsaw  // Unrotated, in template order.
	ents    []tEntity // In template order.
}

// tEntity is an entity of a template, with what placement reads of its NBT (template_entities.go).
type tEntity struct {
	pos      [3]float64 // Relative to the template's origin.
	blockPos [3]int
	id       string  // "minecraft:cushion".
	color    string  // A cushion's dye colour.
	yaw      float32 // Rotation[0].
}

// jigsaw is a jigsaw block of a template (N2 1.2 record).
type jigsaw struct {
	pos                  [3]int
	front, top           int // Facing indices (N2 1.2 table): 0 down, 1 up, 2 north, 3 south, 4 west, 5 east.
	name, target, pool   string
	finalState           string
	aligned              bool
	jointSet             bool // The joint was given (Java defaults it by the front).
	placePrio, selectPri int
}

var faceIndex = map[string]int{"down": 0, "up": 1, "north": 2, "south": 3, "west": 4, "east": 5}

// readTemplate parses a template file through worldgen's shared reader (worldgen/javatemplate.go: its
// limits on the file, the unpacked NBT and the size, and its block checks).
func readTemplate(path, file string) (*Template, error) {
	jt, err := worldgen.ReadJavaTemplate(file)
	if err != nil {
		return nil, err
	}
	t := &Template{Path: path, Size: jt.Size, palette: make([]tState, len(jt.Palette)), blocks: make([]tBlock, len(jt.Blocks))}
	for i, st := range jt.Palette {
		t.palette[i] = tState{name: st.Name, props: st.Props}
	}
	for _, e := range jt.Entities {
		te := tEntity{pos: e.Pos, blockPos: e.BlockPos}
		te.id, _ = e.NBT["id"].(string)
		te.color, _ = e.NBT["color"].(string)
		if r, ok := e.NBT["Rotation"].([]any); ok && len(r) > 0 {
			te.yaw, _ = r[0].(float32)
		}
		t.ents = append(t.ents, te)
	}
	for i, b := range jt.Blocks {
		t.blocks[i] = tBlock{pos: b.Pos, state: b.State, nbt: b.NBT}
		if st := t.palette[b.State]; st.name == "jigsaw" {
			t.jigsaws = append(t.jigsaws, newJigsaw(t.blocks[i], st))
		}
	}
	return t, nil
}

func newJigsaw(b tBlock, st tState) jigsaw {
	o := st.props["orientation"]
	if o == "" {
		// The 1.14 jigsaw format (the village templates): a facing, which Java's data fixer turns into an
		// orientation with the top up, or south / north for a jigsaw facing down / up.
		switch f := st.props["facing"]; f {
		case "down":
			o = "down_south"
		case "up":
			o = "up_north"
		default:
			o = f + "_up"
		}
	}
	fr, top, _ := strings.Cut(o, "_")
	j := jigsaw{pos: b.pos, front: faceIndex[fr], top: faceIndex[top]}
	str := func(k string) string { v, _ := b.nbt[k].(string); return v }
	j.name, j.pool, j.target, j.finalState = str("name"), str("pool"), str("target"), str("final_state")
	if at := str("attachement_type"); at != "" && j.name == "" && j.target == "" {
		// 1.14 format: one attachment type is both the name and the target; target_pool is the pool.
		j.name, j.target = at, at
	}
	if j.pool == "" {
		j.pool = str("target_pool")
	}
	j.aligned = str("joint") == "aligned"
	j.jointSet = str("joint") != ""
	num := func(k string) int {
		switch v := b.nbt[k].(type) {
		case int32:
			return int(v)
		case int16:
			return int(v)
		case byte:
			return int(v)
		}
		return 0
	}
	j.placePrio, j.selectPri = num("placement_priority"), num("selection_priority")
	return j
}
