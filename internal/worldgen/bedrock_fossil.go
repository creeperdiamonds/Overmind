package worldgen

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Overworld fossils: minecraft:fossil_feature, our own Go from NOTES-portals-fossils-ruins F1 (cited F1),
// validated there on seed 100's five fossils (388 of 388 template cells). The templates are the
// install's fossils/*.nbt (bone blocks only), read on first use. The feature draws from its rule's fresh
// MT (the scatter's second generator, at draw 0):
//   - rotation nextInt(4), template nextInt(8) (spines 1-4, skulls 1-4), offsets nextInt(16 - size) in
//     x and z of the turned size;
//   - y = the lowest first free height (water not counted) over the turned footprint, at most the rule's
//     y; then y - 15 - nextInt(10), at least the world's bottom + 10;
//   - no fossil when more than max_empty_corners of the 8 box corners are air or liquid;
//   - two passes on one integrity random L = MT(the rule seed), one word per template block in NBT
//     order: u <= 0.1 ore, u <= 0.9 bone block, else the terrain stays; written inside the decorating
//     chunk only.
// The feature's first test (a name the dimension returns for the position, likely a structure there)
// is not applied: not read.

type fossilTemplate struct {
	size   [3]int
	blocks []fossilBlock
}

type fossilBlock struct {
	pos  [3]int
	axis string
}

var fossilNames = [8]string{"fossil_spine_01", "fossil_spine_02", "fossil_spine_03", "fossil_spine_04",
	"fossil_skull_01", "fossil_skull_02", "fossil_skull_03", "fossil_skull_04"}

type fossilFeat struct {
	bd       *BedrockData
	ore      bs
	maxEmpty int
}

func (fc *featCompiler) fossil(v map[string]json.RawMessage) (bfeat, error) {
	b, err := parseBDesc(v["ore_block"])
	if err != nil {
		return nil, err
	}
	ore, err := javaStateFor(b)
	if err != nil {
		return nil, err
	}
	f := &fossilFeat{bd: fc.bd, ore: ore, maxEmpty: 4}
	if raw, ok := v["max_empty_corners"]; ok {
		if err := json.Unmarshal(raw, &f.maxEmpty); err != nil {
			return nil, fmt.Errorf("max_empty_corners %s", raw)
		}
	}
	return f, nil
}

// fossilTemplates reads the eight templates once; a missing or broken one stays nil (logged as a problem).
func (bd *BedrockData) fossilTemplates() *[8]*fossilTemplate {
	bd.fossilOnce.Do(func() {
		for i, n := range fossilNames {
			t, err := bd.readFossil("fossils", n)
			if err != nil {
				bd.problem("fossils/"+n, err.Error())
				continue
			}
			bd.fossils[i] = t
		}
		for i := range bd.netherFossils {
			n := fmt.Sprintf("fossil_%d", i+1)
			t, err := bd.readFossil("nether_fossils", n)
			if err != nil {
				bd.problem("nether_fossils/"+n, err.Error())
				continue
			}
			bd.netherFossils[i] = t
		}
	})
	return &bd.fossils
}

func (bd *BedrockData) readFossil(dir, name string) (*fossilTemplate, error) {
	packs, _, err := packsDir(bd.Dir)
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
	for _, pack := range names {
		file := filepath.Join(packs, pack, "structures", dir, name+".nbt")
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b {
			zr, err := gzip.NewReader(bytes.NewReader(raw))
			if err != nil {
				return nil, err
			}
			if raw, err = io.ReadAll(io.LimitReader(zr, 1<<20)); err != nil {
				return nil, err
			}
		}
		root, err := ReadJavaNBT(raw)
		if err != nil {
			return nil, err
		}
		return parseFossil(root)
	}
	return nil, fmt.Errorf("no template file")
}

func parseFossil(root map[string]any) (*fossilTemplate, error) {
	ints := func(v any) []int {
		l, _ := v.([]any)
		var out []int
		for _, e := range l {
			if n, ok := e.(int32); ok {
				out = append(out, int(n))
			}
		}
		return out
	}
	size := ints(root["size"])
	if len(size) != 3 || size[0] < 1 || size[2] < 1 || size[0] > 15 || size[2] > 15 || size[1] < 1 || size[1] > 64 {
		return nil, fmt.Errorf("size %v", size)
	}
	t := &fossilTemplate{size: [3]int{size[0], size[1], size[2]}}
	pal, _ := root["palette"].([]any)
	if pal == nil {
		if ps, ok := root["palettes"].([]any); ok && len(ps) > 0 {
			pal, _ = ps[0].([]any)
		}
	}
	axes := make([]string, len(pal))
	bone := make([]bool, len(pal))
	for i, e := range pal {
		m, _ := e.(map[string]any)
		n, _ := m["Name"].(string)
		bone[i] = strings.TrimPrefix(n, "minecraft:") == "bone_block"
		axes[i] = "y"
		if p, ok := m["Properties"].(map[string]any); ok {
			if a, ok := p["axis"].(string); ok {
				axes[i] = a
			}
		}
	}
	bl, _ := root["blocks"].([]any)
	for _, e := range bl {
		m, _ := e.(map[string]any)
		p := ints(m["pos"])
		s, _ := m["state"].(int32)
		if len(p) != 3 || int(s) < 0 || int(s) >= len(pal) {
			return nil, fmt.Errorf("bad block")
		}
		for a := range 3 {
			if p[a] < 0 || p[a] >= t.size[a] {
				return nil, fmt.Errorf("block outside the size")
			}
		}
		if !bone[s] {
			// F1: the Overworld templates hold only bone blocks; anything else (the Nether ones' air) is
			// taken as the settings' ignore block, which the notes do not name (labelled: air assumed). It
			// still draws its integrity word.
			t.blocks = append(t.blocks, fossilBlock{pos: [3]int{p[0], p[1], p[2]}})
			continue
		}
		t.blocks = append(t.blocks, fossilBlock{pos: [3]int{p[0], p[1], p[2]}, axis: axes[s]})
	}
	return t, nil
}

// fossilTurn turns a template position by a Bedrock rotation about the zero corner (F1).
func fossilTurn(x, z, rot int) (int, int) {
	switch rot & 3 {
	case 1:
		return -z, x
	case 2:
		return -x, -z
	case 3:
		return z, -x
	}
	return x, z
}

var boneStates = sync.OnceValue(func() map[string]bs {
	return map[string]bs{"x": stateOf("bone_block[axis=x]"), "y": stateOf("bone_block[axis=y]"), "z": stateOf("bone_block[axis=z]")}
})

func (f *fossilFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	ts := f.bd.fossilTemplates()
	rot := int(r.nextIntN(4))
	t := ts[r.nextIntN(8)]
	if t == nil {
		return p, false
	}
	sx, sz := t.size[0], t.size[2]
	if rot&1 == 1 {
		sx, sz = sz, sx
	}
	ox, oz := int(r.nextIntN(int32(16-sx))), int(r.nextIntN(int32(16-sz)))
	y := p.y
	for i := range sx {
		for j := range sz {
			y = min(y, d.height(hmOceanFloor, p.x+ox+i, p.z+oz+j))
		}
	}
	y = max(y-15-int(r.nextIntN(10)), minY+10)
	zx, zz := p.x+ox, p.z+oz
	qx, qz := fossilTurn(t.size[0]-1, t.size[2]-1, rot)
	empty := 0
	for _, x := range [2]int{zx, zx + qx} {
		for _, yy := range [2]int{y, y + t.size[1] - 1} {
			for _, z := range [2]int{zz, zz + qz} {
				if b := d.block(x, yy, z); b.isAir() || b.has(fLiquid) {
					empty++
				}
			}
		}
	}
	if empty > f.maxEmpty {
		return p, false
	}
	var l bmt
	l.setSeed(r.seed)
	u := make([]float64, len(t.blocks))
	for i := range u {
		u[i] = float64(l.u32()) * (1.0 / 4294967296.0)
	}
	bones := boneStates()
	write := func(b fossilBlock, s bs) {
		dx, dz := fossilTurn(b.pos[0], b.pos[2], rot)
		x, z := zx+dx, zz+dz
		if x>>4 == d.cx && z>>4 == d.cz {
			d.set(x, y+b.pos[1], z, s)
		}
	}
	placed := false
	for i, b := range t.blocks {
		if b.axis != "" && u[i] <= float64(float32(0.9)) {
			a := b.axis
			if rot&1 == 1 && a != "y" {
				a = map[string]string{"x": "z", "z": "x"}[a]
			}
			write(b, bones[a])
			placed = true
		}
	}
	for i, b := range t.blocks {
		if b.axis != "" && u[i] <= float64(float32(0.1)) {
			write(b, f.ore)
		}
	}
	return p, placed
}

// netherFossilFeat is minecraft:nether_fossil_feature, a code feature: NOTES-portals-fossils-ruins F2,
// read, not validated (no soul sand valley world was scanned). rot nextInt(4), template nextInt(14); the
// base walks down from the rule's y until the template's top layer is air over all four footprint corners
// and the corners stand on solid blocks, above the sea level 32 and without lava at the corners; the
// template is placed one above, inside the decorating chunk, its air not written (assumed, as F1's
// ignore block). Then the dried ghast draws (nextFloat() <= 0.5, a direction and an offset); the block
// is not placed: Dragonfly has no dried ghast.
type netherFossilFeat struct{ bd *BedrockData }

func (f netherFossilFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	rot := int(r.nextIntN(4))
	t := f.bd.netherFossils[r.nextIntN(14)]
	if t == nil {
		return p, false
	}
	qx, qz := fossilTurn(t.size[0]-1, t.size[2]-1, rot)
	corners := [4][2]int{{p.x, p.z}, {p.x + qx, p.z}, {p.x, p.z + qz}, {p.x + qx, p.z + qz}}
	sy := t.size[1]
	y := p.y
	for {
		y--
		if y <= 32 || netherHeight <= y+sy+1 {
			return p, false
		}
		lava := false
		for _, c := range corners {
			if d.block(c[0], y, c[1]).has(fLava) {
				lava = true
			}
		}
		if lava {
			return p, false
		}
		if !d.block(p.x, y+sy-1, p.z).isAir() {
			continue
		}
		ok := true
		for _, c := range corners {
			if !d.block(c[0], y+sy-1, c[1]).isAir() || !d.block(c[0], y, c[1]).has(fSolid) {
				ok = false
				break
			}
		}
		if ok {
			break
		}
	}
	bones := boneStates()
	for _, b := range t.blocks {
		if b.axis == "" {
			continue
		}
		dx, dz := fossilTurn(b.pos[0], b.pos[2], rot)
		x, z := p.x+dx, p.z+dz
		if x>>4 != d.cx || z>>4 != d.cz {
			continue
		}
		a := b.axis
		if rot&1 == 1 && a != "y" {
			a = map[string]string{"x": "z", "z": "x"}[a]
		}
		d.set(x, y+1+b.pos[1], z, bones[a])
	}
	if r.nextFloat() <= 0.5 {
		r.nextIntN(4)
		if qx != 0 {
			r.nextIntN(int32(absInt(qx)))
		}
		if qz != 0 {
			r.nextIntN(int32(absInt(qz)))
		}
	}
	return p, true
}
