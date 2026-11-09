package worldgen

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Java structure templates from the install's behaviour packs (NOTES-bedrock-structures 1.1, 1.3): gzip,
// big-endian NBT with a size, a palette (or palettes) of Java block names with Properties, and blocks
// with a palette index and optional NBT. One reader serves the structures package and the fossil
// feature, with the limits below.

// JavaTemplate is a parsed template.
type JavaTemplate struct {
	Size    [3]int
	Palette []JavaState
	Blocks  []JavaBlock // In file order.
	// Entities are the template's entities in file order (abandoned camps' cushions, village
	// villagers, one item in a swamp camp).
	Entities []JavaEntity
}

// JavaEntity is one entity of a template: its position relative to the template's origin, the block
// it stands in, and its NBT (with its "id").
type JavaEntity struct {
	Pos      [3]float64
	BlockPos [3]int
	NBT      map[string]any
}

// JavaState is one palette entry: a Java block name without "minecraft:" and its properties.
type JavaState struct {
	Name  string
	Props map[string]string
}

// JavaBlock is one block of a template: its position, palette index and NBT (nil when it has none).
type JavaBlock struct {
	Pos   [3]int
	State int
	NBT   map[string]any
}

// Limits on a template file from the install, so a broken or hostile one cannot exhaust memory or make
// boxes that reach every chunk. Vanilla's largest template is well under a megabyte unpacked and 48
// blocks across.
const (
	MaxTemplateBytes = 4 << 20 // The file, and its NBT unpacked.
	MaxTemplateSide  = 256     // Size on each axis.
)

// ReadJavaTemplate reads and checks a template file: every block's palette index and position lie
// inside the palette and the size.
func ReadJavaTemplate(file string) (*JavaTemplate, error) {
	if st, err := os.Stat(file); err != nil {
		return nil, err
	} else if st.Size() > MaxTemplateBytes {
		return nil, fmt.Errorf("%s: %d bytes, more than %d", file, st.Size(), MaxTemplateBytes)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if raw, err = io.ReadAll(io.LimitReader(zr, MaxTemplateBytes+1)); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if len(raw) > MaxTemplateBytes {
			return nil, fmt.Errorf("%s: unpacks to more than %d bytes", file, MaxTemplateBytes)
		}
	}
	root, err := ReadJavaNBT(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	t := &JavaTemplate{}
	size := nbtInts(root["size"])
	if len(size) != 3 {
		return nil, fmt.Errorf("%s: no size", file)
	}
	t.Size = [3]int{size[0], size[1], size[2]}
	for _, n := range t.Size {
		if n < 1 || n > MaxTemplateSide {
			return nil, fmt.Errorf("%s: size %v outside 1..%d", file, t.Size, MaxTemplateSide)
		}
	}
	pal, _ := root["palette"].([]any)
	if pal == nil { // Variants: the first palette (NOTES-bedrock-structures 1.3). Which one a piece takes is not read.
		if ps, ok := root["palettes"].([]any); ok && len(ps) > 0 {
			pal, _ = ps[0].([]any)
		}
	}
	for _, e := range pal {
		m, _ := e.(map[string]any)
		name, _ := m["Name"].(string)
		st := JavaState{Name: strings.TrimPrefix(name, "minecraft:"), Props: map[string]string{}}
		if p, ok := m["Properties"].(map[string]any); ok {
			for k, v := range p {
				st.Props[k], _ = v.(string)
			}
		}
		t.Palette = append(t.Palette, st)
	}
	bl, _ := root["blocks"].([]any)
	for _, e := range bl {
		m, _ := e.(map[string]any)
		p := nbtInts(m["pos"])
		s, _ := m["state"].(int32)
		if len(p) != 3 || int(s) < 0 || int(s) >= len(t.Palette) {
			return nil, fmt.Errorf("%s: bad block %v", file, m)
		}
		for a := range 3 {
			if p[a] < 0 || p[a] >= t.Size[a] {
				return nil, fmt.Errorf("%s: block at %v outside the size %v", file, p, t.Size)
			}
		}
		b := JavaBlock{Pos: [3]int{p[0], p[1], p[2]}, State: int(s)}
		b.NBT, _ = m["nbt"].(map[string]any)
		t.Blocks = append(t.Blocks, b)
	}
	ents, _ := root["entities"].([]any)
	for _, e := range ents {
		m, _ := e.(map[string]any)
		bp, fp := nbtInts(m["blockPos"]), nbtFloats(m["pos"])
		nb, _ := m["nbt"].(map[string]any)
		if len(bp) != 3 || len(fp) != 3 || nb == nil {
			return nil, fmt.Errorf("%s: bad entity %v", file, m)
		}
		for a := range 3 {
			if bp[a] < 0 || bp[a] >= t.Size[a] {
				return nil, fmt.Errorf("%s: entity at %v outside the size %v", file, bp, t.Size)
			}
		}
		t.Entities = append(t.Entities, JavaEntity{Pos: [3]float64{fp[0], fp[1], fp[2]}, BlockPos: [3]int{bp[0], bp[1], bp[2]}, NBT: nb})
	}
	return t, nil
}

// nbtFloats reads an NBT list of doubles.
func nbtFloats(v any) []float64 {
	l, _ := v.([]any)
	out := make([]float64, 0, len(l))
	for _, e := range l {
		if f, ok := e.(float64); ok {
			out = append(out, f)
		}
	}
	return out
}

// nbtInts reads an NBT list or array of ints.
func nbtInts(v any) []int {
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

// VanillaPacks are an install's vanilla behaviour pack folders, newest first: where a file is in
// several, the newest copy wins (NOTES-bedrock-structures 1.1).
func VanillaPacks(dir string) ([]string, error) {
	packs, _, err := packsDir(dir)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(packs)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() && vanillaPack.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.SortFunc(names, func(a, b string) int { return slices.Compare(packVersion(b), packVersion(a)) })
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(packs, n)
	}
	return out, nil
}
