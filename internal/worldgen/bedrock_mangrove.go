package worldgen

import (
	"encoding/json"
)

// Bedrock's mangrove (tree_feature with mangrove_roots, mangrove_trunk and mangrove_canopy), in our own
// Go from the study of BDS 1.26.60.6: re/phase4/trees/TREES-pseudocode.md section 1 (roots 0xd8a8a10,
// root walk 0xd8a9830, trunk 0xd8a9eb0, trunk height 0xd8aa7a0, canopy 0xd8a75d0) as corrected by
// NOTES-bedrock-exact T17 (a setBlock of the block already there fails: logs and leaves) and T27 (the
// roots and trunk read again from the code; the above-root chance is drawn whether or not the root's
// setBlock took). The order in tree_feature is the trunk's height, then the roots (which give the
// trunk's base), then the trunk, which calls the canopy. Every draw comes from the feature's generator.

var (
	bnFlowingWater = internBName("flowing_water")
	bnMud          = internBName("mud")
)

// bTreeMaterial is the material half of the trunk, root and canopy tests (TREES-pseudocode section 0,
// TREEOK): air, a replaceable plant, water, leaves. The study reads it as legacy flag +0x108 & 0x20,
// material 8 with region vt+0x98, and materials 5 and 7, and names them from the code's use, not from
// a table (U3); the T17 sim uses this list.
func bTreeMaterial(s bs) bool {
	if s.isAir() || bReplant(s) || bLeaves(s) {
		return true
	}
	n := bname(s)
	return n == bnWater || n == bnFlowingWater
}

// treeOK is TREEOK: the tree's may_replace list (empty passes) or the material test.
func (t *treeCtx) treeOK(s bs) bool { return t.canReplace(s) || bTreeMaterial(s) }

// putNew is setBlock as T17 reads it for mangrove logs and leaves: a block already there is not set
// again, and the caller sees a failure (no list entry, no follow-up draws).
func putNew(d *decoration, p pos, s bs) bool {
	if bname(d.at(p)) == bname(s) {
		return false
	}
	return d.bput(p, s)
}

// ---------------------------------------------------------------- roots

// mangroveRootsB is MangroveTreeRoots (0xd8a8a10; fields: TREES-pseudocode 1.2).
type mangroveRootsB struct {
	width, length    int // max_root_width, max_root_length
	yOff             bRange
	root, muddy, mud bs
	rootWet          bs // root_block waterlogged, for a root set into a water source
	above            bs
	hasAbove         bool
	aboveChance      bChance
	through          *bmatch // roots_may_grow_through, nil when empty
	deco             bDeco   // The roots' decoration: none in the vanilla files, so no draws.
}

func (fc *featCompiler) mangroveRoots(raw json.RawMessage) (*mangroveRootsB, error) {
	var v map[string]json.RawMessage
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	m := &mangroveRootsB{width: 8, length: 15} // The ctor's defaults (0x500aaca).
	if raw, ok := v["max_root_width"]; ok {
		json.Unmarshal(raw, &m.width)
	}
	if raw, ok := v["max_root_length"]; ok {
		json.Unmarshal(raw, &m.length)
	}
	var err error
	if m.yOff, err = parseBRange(v["y_offset"]); err != nil {
		return nil, err
	}
	if m.root, _, err = blockState(v["root_block"]); err != nil {
		return nil, err
	}
	if m.muddy, _, err = blockState(v["muddy_root_block"]); err != nil {
		return nil, err
	}
	if m.mud, _, err = blockState(v["mud_block"]); err != nil {
		return nil, err
	}
	m.rootWet = wetState(m.root)
	if raw, ok := v["above_root"]; ok {
		var ab map[string]json.RawMessage
		if err := json.Unmarshal(raw, &ab); err != nil {
			return nil, err
		}
		if b, ok := ab["above_root_block"]; ok {
			if m.above, _, err = blockState(b); err != nil {
				return nil, err
			}
			m.hasAbove = true
		}
		if m.aboveChance, err = parseBChance(ab["above_root_chance"]); err != nil {
			return nil, err
		}
	}
	if m.through, err = optMatch(v["roots_may_grow_through"]); err != nil {
		return nil, err
	}
	if m.deco, err = parseBDeco(v["decoration"]); err != nil {
		return nil, err
	}
	return m, nil
}

// rootOK is ROOTOK: TREEOK with roots_may_grow_through in place of may_replace.
func (m *mangroveRootsB) rootOK(s bs) bool {
	return m.through == nil || m.through.has(s) || bTreeMaterial(s)
}

// rootDirs are the four walks' directions in the code's order (table 0x154b5ac): north, east, south, west.
var rootDirs = [4][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

// place grows the roots from the tree's position and gives the trunk's base; false fails the tree.
func (m *mangroveRootsB) place(d *decoration, t *treeCtx, r *bmt, p pos, h int) (pos, bool) {
	off := m.yOff.excl(r)
	top := p.add(0, off, 0)
	if !t.prepare(d, p, h+off) {
		return p, false
	}
	if m.through != nil {
		for y := p.y; y < top.y; y++ {
			if !m.rootOK(d.block(p.x, y, p.z)) {
				return p, false
			}
		}
	}
	list := []pos{top.add(0, -1, 0)}
	var out []pos
	for _, dir := range rootDirs {
		start := top.add(dir[0], 0, dir[1])
		out = out[:0]
		m.walk(d, r, start, top, dir, &out, 0)
		if len(out) == 0 || len(out) > m.length {
			return p, false
		}
		list = append(list, out...)
		list = append(list, start)
	}
	for _, q := range list {
		cur := d.at(q)
		if !m.rootOK(cur) {
			continue
		}
		if bname(cur) == bnMud {
			if d.bput(q, m.muddy) {
				m.deco.place(d, r, q, fourSides)
			}
			continue // No above-root block on muddy roots.
		}
		s := m.root
		if bname(cur) == bnWater {
			s = m.rootWet
		}
		if d.bput(q, s) {
			m.deco.place(d, r, q, fourSides)
		}
		// T27: the chance is drawn whether or not the root was set, and before the air test.
		if m.hasAbove && m.aboveChance.enabled() && m.aboveChance.test(r) {
			if a := q.add(0, 1, 0); d.at(a).isAir() {
				if d.bput(a, m.above) {
					m.deco.place(d, r, a, fourSides)
				}
			}
		}
	}
	return top, true
}

// wetState is a state waterlogged, for a root or leaf the mangrove sets into a water source (as Java's
// mangrove does). Assumed, not read: Bedrock keeps the water as the block's liquid layer. The block
// names BDS reports, all the study compares, are the same either way.
func wetState(s bs) bs {
	if hasProp(s, "waterlogged") {
		return withProp(s, "waterlogged", "true")
	}
	return s
}

// walk is the root walk (0xd8a9830): from p, one or two candidates by the distance from the trunk's
// base; each that passes ROOTOK is listed and walked on. It fails (and clears the list) at depth
// max_root_length or when the list is longer than that.
func (m *mangroveRootsB) walk(d *decoration, r *bmt, p, o pos, dir [2]int, out *[]pos, depth int) bool {
	if depth == m.length || len(*out) > m.length {
		*out = (*out)[:0]
		return false
	}
	below := p.add(0, -1, 0)
	side := p.add(dir[0], 0, dir[1])
	dist := absInt(p.x-o.x) + absInt(p.y-o.y) + absInt(p.z-o.z)
	var cand [2]pos
	n := 1
	cand[0] = below
	switch {
	case dist > m.width-3 && dist <= m.width:
		if r.nextFloat() < float32(0.2) {
			cand[1], n = side.add(0, -1, 0), 2
		}
	case dist > m.width:
	default:
		if !(r.nextFloat() < float32(0.2)) && r.nextBool() {
			cand[0] = side
		}
	}
	for _, c := range cand[:n] {
		if m.rootOK(d.at(c)) {
			*out = append(*out, c)
			if !m.walk(d, r, c, o, dir, out, depth+1) {
				return false
			}
		}
	}
	return true
}

// ---------------------------------------------------------------- trunk

// mangroveTrunk is MangroveTreeTrunk (height 0xd8aa7a0, place 0xd8a9eb0). branch_chance is not read
// by the code: each log draws nextBoolean.
type mangroveTrunk struct {
	log          bs
	base, ra, rb int
	length       bRange // branch_length, drawn twice
	steps        bRange // branch_steps
	deco         bDeco
}

func (fc *featCompiler) mangroveTrunk(v map[string]json.RawMessage, log bs, deco bDeco) (bTrunk, error) {
	t := &mangroveTrunk{log: log, deco: deco}
	var th struct {
		Base int `json:"base"`
		A    int `json:"height_rand_a"`
		B    int `json:"height_rand_b"`
	}
	if err := json.Unmarshal(v["trunk_height"], &th); err != nil {
		return nil, err
	}
	t.base, t.ra, t.rb = th.Base, th.A, th.B
	var br map[string]json.RawMessage
	json.Unmarshal(v["branches"], &br)
	var err error
	if t.length, err = parseBRange(br["branch_length"]); err != nil {
		return nil, err
	}
	t.steps = bRange{1, 1, false}
	if raw, ok := br["branch_steps"]; ok {
		if t.steps, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// height is base + nextInt(a+1) + nextInt(b+1), always two draws, a first.
func (t *mangroveTrunk) height(r *bmt) int {
	return t.base + int(r.nextIntN(int32(t.ra+1))) + int(r.nextIntN(int32(t.rb+1)))
}

// place sets the trunk from the roots' base (the roots did prepareSpawn; no base block), branching
// up and out, and hands the canopy every log the branches listed.
func (t *mangroveTrunk) place(d *decoration, c *treeCtx, r *bmt, p pos, h int, can bCanopy) bool {
	logs := make([]pos, 0, 2*h+2)
	for i := range h {
		q := p.add(0, i, 0)
		if c.treeOK(d.at(q)) && putNew(d, q, t.log) {
			t.deco.place(d, r, q, fourSides)
			if i < h-1 && r.nextBool() {
				dn := int(r.nextIntN(4))
				a1, a2 := t.length.inclGT(r), t.length.inclGT(r)
				steps := t.steps.inclGT(r)
				k := a1 - a2 - 1
				n := max(0, k)
				var sx, sz int
				switch dn {
				case 0:
					sz = -1
				case 1:
					sz = 1
				case 2:
					sx = -1
				case 3:
					sx = 1
				}
				bx, bz := p.x, p.z
				topy := q.y + n
				if k < h && steps > 0 {
					for {
						if n != 0 {
							bx += sx
							bz += sz
							b := pos{bx, q.y + n, bz}
							topy = b.y
							if c.treeOK(d.at(b)) && putNew(d, b, t.log) {
								t.deco.place(d, r, b, fourSides)
								topy++
							}
							logs = append(logs, b) // Listed even when not placed.
						}
						n++
						if n >= h || steps <= 1 {
							break
						}
						steps--
					}
				}
				if topy-q.y > 1 {
					logs = append(logs, pos{bx, topy, bz}, pos{bx, topy - 2, bz})
				}
			}
		}
		if i == h-1 {
			logs = append(logs, p.add(0, h, 0))
		}
	}
	if can != nil && len(logs) > 0 {
		can.place(d, c, r, logs[len(logs)-1], logs, 1)
	}
	return true
}

// ---------------------------------------------------------------- canopy

// mangroveCanopy is MangroveTreeCanopy (0xd8a75d0). The hanging block wants two air blocks under its
// leaf and keeps a 1-block exclusion box around each one placed (ctor defaults 2, 1, 1; no keys).
type mangroveCanopy struct {
	h, rad    bRange // canopy_height, canopy_radius (INCL_GE: a plain number draws nextInt(1))
	attempts  int
	leaves    []bs
	wet       []bs // leaves waterlogged (wetState)
	w         []float32
	deco      bDeco
	hang      bs
	hasHang   bool
	hangCh    bChance
	hangBelow int
	exXZ, exY int
}

func mangroveCanopyOf(v map[string]json.RawMessage) (bCanopy, error) {
	c := &mangroveCanopy{h: bRange{2, 2, false}, rad: bRange{3, 3, false}, attempts: 70, hangBelow: 2, exXZ: 1, exY: 1}
	var err error
	if raw, ok := v["canopy_height"]; ok {
		if c.h, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	if raw, ok := v["canopy_radius"]; ok {
		if c.rad, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	if raw, ok := v["leaf_placement_attempts"]; ok {
		json.Unmarshal(raw, &c.attempts)
	}
	var list [][2]json.RawMessage
	json.Unmarshal(v["leaf_blocks"], &list)
	for _, e := range list {
		s, _, err := blockState(e[0])
		if err != nil {
			return nil, err
		}
		var w float32
		json.Unmarshal(e[1], &w)
		c.leaves, c.wet, c.w = append(c.leaves, s), append(c.wet, wetState(s)), append(c.w, w)
	}
	if len(c.leaves) == 0 {
		return nil, unimpl("mangrove canopy with no leaves")
	}
	if c.deco, err = parseBDeco(v["canopy_decoration"]); err != nil {
		return nil, err
	}
	if raw, ok := v["hanging_block"]; ok {
		if c.hang, _, err = blockState(raw); err != nil {
			return nil, err
		}
		c.hasHang = true
	}
	if c.hangCh, err = parseBChance(v["hanging_block_placement_chance"]); err != nil {
		return nil, err
	}
	return c, nil
}

// enabled is the code's test for a chance being set: a percentage of 0 or more, a fraction with a
// denominator.
func (c bChance) enabled() bool {
	switch c.kind {
	case 1:
		return c.den != 0
	case 2:
		return c.pct >= 0
	}
	return false
}

func (c *mangroveCanopy) place(d *decoration, t *treeCtx, r *bmt, _ pos, logs []pos, _ int) bool {
	hh := c.h.inclGE(r)
	rr := c.rad.inclGE(r)
	var leaves []pos
	for _, e := range logs {
		for range c.attempts {
			i := weightedPick(r, c.w)
			if i < 0 {
				i = len(c.leaves) - 1
			}
			dx := int(r.nextIntN(int32(rr)))
			dx -= int(r.nextIntN(int32(rr)))
			dy := int(r.nextIntN(int32(hh)))
			dy -= int(r.nextIntN(int32(hh)))
			dz := int(r.nextIntN(int32(rr)))
			dz -= int(r.nextIntN(int32(rr)))
			q := e.add(dx, dy, dz)
			if q.y < minY || q.y > maxY {
				continue
			}
			cur := d.at(q)
			if !t.treeOK(cur) {
				continue
			}
			leaf := c.leaves[i]
			if bname(cur) == bnWater {
				leaf = c.wet[i]
			}
			if putNew(d, q, leaf) {
				leaves = append(leaves, q)
			}
		}
	}
	// Fisher-Yates, forward, on raw words.
	for i := 1; i < len(leaves); i++ {
		if j := int(r.u32() % uint32(i+1)); j != i {
			leaves[i], leaves[j] = leaves[j], leaves[i]
		}
	}
	if c.deco.on && c.deco.chance.enabled() {
		for _, q := range leaves {
			c.deco.place(d, r, q, fourSides)
		}
	}
	if c.hasHang && c.hangCh.enabled() && len(leaves) > 0 {
		taken := make(map[pos]struct{})
	next:
		for _, q := range leaves {
			b := q.add(0, -1, 0)
			if _, ok := taken[b]; ok {
				continue
			}
			if !c.hangCh.test(r) {
				continue
			}
			for k := range c.hangBelow {
				if !d.block(q.x, q.y-1-k, q.z).isAir() {
					continue next
				}
			}
			for ax := -c.exXZ; ax <= c.exXZ; ax++ {
				for ay := -c.exY; ay <= c.exY; ay++ {
					for az := -c.exXZ; az <= c.exXZ; az++ {
						taken[b.add(ax, ay, az)] = struct{}{}
					}
				}
			}
			d.bput(b, c.hang)
		}
	}
	return true
}
