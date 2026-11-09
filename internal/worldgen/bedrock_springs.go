package worldgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The sulfur caves' springs (Task 9 round 18), in our own Go from the study of BDS 1.26.60.6:
// minecraft:sulfur_spring_trail_to_surface_feature (NOTES-bedrock-exact T36 read its place function,
// T42 named its blocks; NOTES-leftovers L11.1 to L11.4 read what it needs) and the
// structure_template_feature its springs are. The templates are the install's
// structures/minecraft/sulfur_spring_*.mcstructure, read when the feature is built; nothing of them is
// in this repo.
//
// The trail (0x4bc84d0), from the cave ceiling its snap found, climbs at most 184 cells to the first
// air cell with a solid-material block under it (L11.1: flag 0x10) and air or water in the two cells
// above (L11.2: the predicate is "replaceable and not liquid", so the cell itself is air), stopping
// once it has tested the chunk heightmap's height. There it places
// random_sulfur_spring_with_tuff_feature at (x, y, z): the template's offset d is 0, since the cell under
// a scan cell is solid and so never the water d needs (L11.1). When that places, a sulfur column (20
// tries a layer) from the snap position up to the spring and a 20-step sulfur walk (T36).
//
// READ in this round (disassembly 0x4bc8a33..0x4bc8bef, where the decompile's flag test reads a
// clobbered register): a spring that does not place does NOT end the trail, as L11.3 says; the code
// tests the returned position's flag (bt r12, 0x20 on the call's rdx) and on a miss goes on climbing
// (cur.y + 1, the same surface and 184-step tests). Likewise the sulfur column's last layer is the
// spring's own y (the loop is do { y++ ... } while y < cur.y, from the snap's y), where T36 and L11.3
// stop one below.

// trailSideList is the trail's list +0x48 (T42: 32 blocks, copied from 0xfc08408): the blocks its
// sulfur column may turn to sulfur.
var trailSideList = bnameSetOf("powder_snow snow sand gravel clay red_sand dirt mud moss_block grass_block stone granite diorite " +
	"andesite tuff deepslate white_terracotta orange_terracotta magenta_terracotta light_blue_terracotta yellow_terracotta " +
	"lime_terracotta pink_terracotta gray_terracotta light_gray_terracotta cyan_terracotta purple_terracotta blue_terracotta " +
	"brown_terracotta green_terracotta red_terracotta black_terracotta")

// sulfurTrail is sulfur_spring_trail_to_surface_feature with the spring feature it places (T42: +0x20
// names the FEATURE random_sulfur_spring_with_tuff_feature; walk and side blocks both sulfur).
type sulfurTrail struct {
	spring bfeat
}

const springFeatureName = "minecraft:random_sulfur_spring_with_tuff_feature"

// testUnwritten names code features left unwritten in data read while it is set (their rules then
// only draw their counts, as before round 18). Tests set it to score a feature against the world
// without it.
var testUnwritten map[string]bool

// trailTrace, when set (tests only), sees every spring the trail tries: the snap position, the scan
// cell and whether it placed.
var trailTrace func(p, at pos, ok bool)

func (fc *featCompiler) sulfurTrailFeature() (bfeat, error) {
	f, err := fc.feature(springFeatureName)
	if err != nil {
		return nil, fmt.Errorf("sulfur spring trail: %w", err)
	}
	return codeFeat((&sulfurTrail{f}).place), nil
}

func (t *sulfurTrail) place(d *decoration, r *bmt, p pos) bool {
	// The chunk heightmap at the column, counting water (L11.3, L9).
	surface := d.bedrockHeight(p.x, p.z)
	cur := p
	open := func(s bs) bool { return s.isAir() || bWaterMat(s) }
	for range 184 {
		if d.block(p.x, cur.y-1, p.z).has(fSolid) && d.at(cur).isAir() &&
			open(d.block(p.x, cur.y+1, p.z)) && open(d.block(p.x, cur.y+2, p.z)) {
			_, ok := t.spring.place(d, r, cur)
			if trailTrace != nil {
				trailTrace(p, cur, ok)
			}
			if ok {
				t.column(d, r, p, cur.y)
				return true
			}
		}
		if cur.y >= surface {
			return false
		}
		cur.y++
	}
	return false
}

// column is T36's two loops after a spring placed: 20 tries a layer around the trail's column from the
// snap position's y + 1 up to the spring's y, turning a listed block to sulfur, then a 20-step walk from
// the snap position that sets sulfur in air under a solid block. All on the trail's random.
func (t *sulfurTrail) column(d *decoration, r *bmt, p pos, top int) {
	for y := p.y + 1; y <= top; y++ {
		for range 20 {
			a, b := int(r.nextIntN(3)), int(r.nextIntN(3))
			c, e := int(r.nextIntN(3)), int(r.nextIntN(3))
			q := pos{p.x + a - b, y, p.z + c - e}
			if trailSideList.has(bname(d.at(q))) {
				d.bput(q, sSulfur)
			}
		}
	}
	q := p
	for range 20 {
		a, b := int(r.nextIntN(3)), int(r.nextIntN(3))
		c, e := int(r.nextIntN(3)), int(r.nextIntN(3))
		g, h := int(r.nextIntN(2)), int(r.nextIntN(2))
		q = q.add(a-b, g-h, c-e)
		if d.at(q).isAir() && d.at(q.add(0, 1, 0)).has(fSolid) {
			d.bput(q, sSulfur)
		}
	}
}

// ---------------------------------------------------------------- structure_template_feature

// mcTemplate is a Bedrock structure template (.mcstructure, little-endian NBT): its size and, per cell
// in x, y, z order, the state, whether the cell is a structure void (it places nothing), and whether it
// is air or a void (the constraints' "air").
type mcTemplate struct {
	size  [3]int
	cells []bs
	void  []bool
	air   []bool
}

func (t *mcTemplate) index(x, y, z int) int { return (x*t.size[1]+y)*t.size[2] + z }

// readMCStructure reads a template: layer 0's palette indices (-1 is a structure void) and layer 1's
// water, which waterlogs a waterloggable block (the springs' spikes). Water of any liquid_depth is taken
// as a source block (ASSUMED; one cell of medium_1 has depth 8).
func readMCStructure(file string) (*mcTemplate, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	root, err := ReadBedrockNBT(raw)
	if err != nil {
		return nil, err
	}
	sz := nbtInts(root["size"])
	st, _ := root["structure"].(map[string]any)
	if len(sz) != 3 || st == nil {
		return nil, fmt.Errorf("%s: no size or structure", file)
	}
	t := &mcTemplate{size: [3]int{sz[0], sz[1], sz[2]}}
	n := sz[0] * sz[1] * sz[2]
	if n <= 0 || n > 64*64*64 {
		return nil, fmt.Errorf("%s: size %v", file, sz)
	}
	layers, _ := st["block_indices"].([]any)
	if len(layers) < 1 {
		return nil, fmt.Errorf("%s: no block indices", file)
	}
	l0 := nbtInts(layers[0])
	var l1 []int
	if len(layers) > 1 {
		l1 = nbtInts(layers[1])
	}
	pal, _ := st["palette"].(map[string]any)
	def, _ := pal["default"].(map[string]any)
	bp, _ := def["block_palette"].([]any)
	if len(l0) != n || l1 != nil && len(l1) != n {
		return nil, fmt.Errorf("%s: %d indices for %d cells", file, len(l0), n)
	}
	states := make([]bs, len(bp))
	water := make([]bool, len(bp))
	isAir := make([]bool, len(bp))
	for i, e := range bp {
		m, _ := e.(map[string]any)
		name, _ := m["name"].(string)
		s, err := templateState(name, m["states"])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		states[i], water[i], isAir[i] = s, bname(s) == bnWater, s.isAir()
	}
	t.cells, t.void, t.air = make([]bs, n), make([]bool, n), make([]bool, n)
	for i, k := range l0 {
		if k < 0 {
			t.void[i], t.air[i] = true, true
			continue
		}
		if k >= len(states) {
			return nil, fmt.Errorf("%s: palette index %d of %d", file, k, len(states))
		}
		s := states[k]
		if l1 != nil && l1[i] >= 0 && l1[i] < len(water) && water[l1[i]] {
			s = waterlogged(s)
		}
		t.cells[i], t.air[i] = s, isAir[k]
	}
	return t, nil
}

// waterlogged is a state with water in it, when it has a waterlogged variant (the spikes do).
func waterlogged(s bs) bs {
	n := s.name()
	if !strings.Contains(n, "waterlogged=false") {
		return s
	}
	w, err := safeState(strings.Replace(n, "waterlogged=false", "waterlogged=true", 1))
	if err != nil {
		return s
	}
	return w
}

// templateState is our state for a template palette entry (a Bedrock name and its NBT states).
func templateState(name string, raw any) (bs, error) {
	m, _ := raw.(map[string]any)
	st := map[string]any{}
	for k, v := range m {
		switch v := v.(type) {
		case byte:
			st[k] = v != 0 // Bedrock's boolean states are bytes.
		case int32:
			st[k] = float64(v)
		default:
			st[k] = v
		}
	}
	n := strings.TrimPrefix(name, "minecraft:")
	if n == "air" {
		return sAir, nil
	}
	s, err := javaStateFor(bdesc{legacyBName(n, st), st})
	if err != nil {
		return 0, fmt.Errorf("template block %s: %w", name, err)
	}
	return s, nil
}

// readPackStructure reads a structure_name's template ("minecraft:sulfur_spring_small_1" is
// structures/minecraft/sulfur_spring_small_1.mcstructure) from the newest pack that has it.
func (bd *BedrockData) readPackStructure(id string) (*mcTemplate, error) {
	ns, name, ok := strings.Cut(id, ":")
	if !ok {
		ns, name = "minecraft", id
	}
	if strings.ContainsAny(ns+name, `/\.`) || name == "" {
		return nil, fmt.Errorf("structure name %q", id)
	}
	packs, err := VanillaPacks(bd.Dir)
	if err != nil {
		return nil, err
	}
	for _, pack := range packs {
		file := filepath.Join(pack, "structures", ns, name+".mcstructure")
		if _, err := os.Stat(file); err == nil {
			return readMCStructure(file)
		}
	}
	return nil, fmt.Errorf("no template file for %s", id)
}

// structTemplateFeat is StructureTemplateFeature::place (0xd8eb860, NOTES-leftovers L11.4): a rotation
// (nextInt(4), its only draw), the template centred on the input column with its layer ground_level on
// the input y, placed when every constraint passes there. Only adjustment_radius 0 is written (the
// springs'; the spiral order of a wider search is not read).
type structTemplateFeat struct {
	t          *mcTemplate
	centre     bool
	ground     int
	leveled    [][3]int // Cells (x, -1, z) under the template's ground layer.
	levelTol   int
	unburied   [][3]int // Cells (x, sizeY, z) over the template's top layer.
	intersects [][3]int // Every non-void cell, template air included.
	allow      *bmatch  // block_intersection's block_allowlist; nil: no such constraint.
}

func (fc *featCompiler) structureTemplate(v map[string]json.RawMessage) (bfeat, error) {
	var name string
	var radius int
	json.Unmarshal(v["structure_name"], &name)
	json.Unmarshal(v["adjustment_radius"], &radius)
	if radius != 0 {
		return nil, unimpl("structure_template_feature adjustment_radius %d", radius)
	}
	if raw, ok := v["facing_direction"]; ok && strings.Trim(string(raw), `" `) != "random" {
		return nil, unimpl("structure_template_feature facing_direction %s", raw)
	}
	t, err := fc.bd.readPackStructure(name)
	if err != nil {
		return nil, err
	}
	f := &structTemplateFeat{t: t, levelTol: 2}
	json.Unmarshal(v["rotate_around_center"], &f.centre)
	json.Unmarshal(v["ground_level"], &f.ground)
	sx, sy, sz := t.size[0], t.size[1], t.size[2]
	f.ground = min(max(f.ground, 0), max(sy, 1)-1)
	var cons map[string]json.RawMessage
	json.Unmarshal(v["constraints"], &cons)
	for k, raw := range cons {
		switch k {
		case "leveled":
			// The tolerance is the ctor's default 2 (+0x8, L11.4); the JSON gives none.
			for x := range sx {
				for z := range sz {
					if !t.air[t.index(x, f.ground, z)] {
						f.leveled = append(f.leveled, [3]int{x, -1, z})
					}
				}
			}
		case "unburied":
			// ASSUMED: a void cell in the top layer counts as air here too (L11.4 says so for leveled).
			for x := range sx {
				for z := range sz {
					if !t.air[t.index(x, sy-1, z)] {
						f.unburied = append(f.unburied, [3]int{x, sy, z})
					}
				}
			}
		case "block_intersection":
			var bi struct {
				Only bool            `json:"only_check_intersection_for_motion_blocking_blocks"`
				List json.RawMessage `json:"block_allowlist"`
			}
			json.Unmarshal(raw, &bi)
			if bi.Only {
				return nil, unimpl("block_intersection for motion-blocking blocks only")
			}
			if f.allow, err = parseBMatch(bi.List); err != nil {
				return nil, err
			}
			for x := range sx {
				for y := range sy {
					for z := range sz {
						if !t.void[t.index(x, y, z)] {
							f.intersects = append(f.intersects, [3]int{x, y, z})
						}
					}
				}
			}
		default:
			return nil, unimpl("structure_template_feature constraint %s", k)
		}
	}
	return f, nil
}

// turn is L11.4's Rot about the input column: rot 1 (-w, u), rot 2 (-u, -w), rot 3 (w, -u); x east,
// z south.
func turn(u, w, rot int) (int, int) {
	switch rot & 3 {
	case 1:
		return -w, u
	case 2:
		return -u, -w
	case 3:
		return w, -u
	}
	return u, w
}

func (f *structTemplateFeat) place(d *decoration, r *bmt, c pos) (pos, bool) {
	rot := int(r.nextIntN(4))
	a, b := 0, 0
	if f.centre {
		a, b = -(f.t.size[0] / 2), -(f.t.size[2] / 2) // -floor(size * 0.5).
	}
	at := func(cell [3]int) pos {
		u, w := turn(cell[0]+a, cell[2]+b, rot)
		return pos{c.x + u, c.y + cell[1], c.z + w} // L11.4: no -ground_level shift for the constraints.
	}
	for _, cell := range f.leveled {
		q := at(cell)
		ok := false
		for y := q.y - f.levelTol; y <= q.y+f.levelTol && !ok; y++ {
			ok = bSolid(d.block(q.x, y, q.z)) && !bSolid(d.block(q.x, y+1, q.z))
		}
		if !ok {
			return c, false
		}
	}
	for _, cell := range f.unburied {
		if !d.at(at(cell)).isAir() {
			return c, false
		}
	}
	if f.allow != nil {
		for _, cell := range f.intersects {
			if !f.allow.has(d.at(at(cell))) {
				return c, false
			}
		}
	}
	// Placed with integrity 1 (no draws), entities ignored, voids skipped, template air written
	// (ASSUMED: Bedrock's template placement was not re-read, L11.4).
	sx, sy, sz := f.t.size[0], f.t.size[1], f.t.size[2]
	for x := range sx {
		for y := range sy {
			for z := range sz {
				i := f.t.index(x, y, z)
				if f.t.void[i] {
					continue
				}
				s := f.t.cells[i]
				u, w := turn(x+a, z+b, rot)
				d.bput(pos{c.x + u, c.y + y - f.ground, c.z + w}, s)
			}
		}
	}
	return c, true
}
