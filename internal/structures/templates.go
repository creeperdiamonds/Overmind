package structures

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
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
	blocks  []tBlock // In file order: jigsaws and chests are taken in this order (N2 1.2 "template order").
	jigsaws []jigsaw // Unrotated, in template order.
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

// Limits on a template file from the install, so a broken or hostile one cannot exhaust memory or make
// boxes that reach every chunk. Vanilla's largest template is well under a megabyte unpacked and 48
// blocks across.
const (
	maxTemplateBytes = 4 << 20 // Unpacked NBT.
	maxTemplateSide  = 256     // Size on each axis.
)

// readTemplate parses a template file.
func readTemplate(path, file string) (*Template, error) {
	if st, err := os.Stat(file); err != nil {
		return nil, err
	} else if st.Size() > maxTemplateBytes {
		return nil, fmt.Errorf("%s: %d bytes, more than %d", file, st.Size(), maxTemplateBytes)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		if raw, err = io.ReadAll(io.LimitReader(zr, maxTemplateBytes+1)); err != nil {
			return nil, err
		}
		if len(raw) > maxTemplateBytes {
			return nil, fmt.Errorf("%s: unpacks to more than %d bytes", file, maxTemplateBytes)
		}
	}
	root, err := readNBT(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	t := &Template{Path: path}
	size := ints(root["size"])
	if len(size) != 3 {
		return nil, fmt.Errorf("%s: no size", file)
	}
	t.Size = [3]int{size[0], size[1], size[2]}
	for _, n := range t.Size {
		if n < 1 || n > maxTemplateSide {
			return nil, fmt.Errorf("%s: size %v outside 1..%d", file, t.Size, maxTemplateSide)
		}
	}
	pal, _ := root["palette"].([]any)
	if pal == nil { // Variants: the first palette (N1 1.3). Which one a piece takes is not read.
		if ps, ok := root["palettes"].([]any); ok && len(ps) > 0 {
			pal, _ = ps[0].([]any)
		}
	}
	for _, e := range pal {
		m, _ := e.(map[string]any)
		name, _ := m["Name"].(string)
		st := tState{name: strings.TrimPrefix(name, "minecraft:"), props: map[string]string{}}
		if p, ok := m["Properties"].(map[string]any); ok {
			for k, v := range p {
				st.props[k], _ = v.(string)
			}
		}
		t.palette = append(t.palette, st)
	}
	bl, _ := root["blocks"].([]any)
	for _, e := range bl {
		m, _ := e.(map[string]any)
		p := ints(m["pos"])
		s, _ := m["state"].(int32)
		if len(p) != 3 || int(s) < 0 || int(s) >= len(t.palette) {
			return nil, fmt.Errorf("%s: bad block %v", file, m)
		}
		for a := range 3 {
			if p[a] < 0 || p[a] >= t.Size[a] {
				return nil, fmt.Errorf("%s: block at %v outside the size %v", file, p, t.Size)
			}
		}
		b := tBlock{pos: [3]int{p[0], p[1], p[2]}, state: int(s)}
		b.nbt, _ = m["nbt"].(map[string]any)
		t.blocks = append(t.blocks, b)
		if st := t.palette[s]; st.name == "jigsaw" {
			t.jigsaws = append(t.jigsaws, newJigsaw(b, st))
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

// ints reads an NBT list or array of ints.
func ints(v any) []int {
	switch l := v.(type) {
	case []any:
		out := make([]int, 0, len(l))
		for _, e := range l {
			switch n := e.(type) {
			case int32:
				out = append(out, int(n))
			case float64:
				out = append(out, int(n))
			}
		}
		return out
	case []int32:
		out := make([]int, len(l))
		for i, n := range l {
			out[i] = int(n)
		}
		return out
	}
	return nil
}
