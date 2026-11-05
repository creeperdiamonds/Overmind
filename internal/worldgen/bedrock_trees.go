package worldgen

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Bedrock's trees for the Bedrock profile (Task 4e round 3), in our own Go from the study of BDS
// 1.26.60.6: tree_feature (NOTES-bedrock-exact Q1 "tree_feature", R3, re/phase4/trees/TREES-pseudocode.md)
// and the legacy tree table (Q1 "Legacy tree features", T5). Every draw comes from the feature's
// generator, in the code's order.

// bRange is a range from the files: {range_min, range_max} or one number.
type bRange struct {
	lo, hi  int
	isRange bool
}

func parseBRange(raw json.RawMessage) (bRange, error) {
	if len(raw) == 0 {
		return bRange{}, nil
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return bRange{lo: int(n), hi: int(n)}, nil
	}
	var r struct {
		Min *float64 `json:"range_min"`
		Max *float64 `json:"range_max"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.Min == nil || r.Max == nil {
		return bRange{}, fmt.Errorf("range %s", raw)
	}
	return bRange{int(*r.Min), int(*r.Max), true}, nil
}

// excl is EXCL: min + nextInt(max-min) when min < max-1 (max exclusive); a number draws nothing.
func (b bRange) excl(r *bmt) int {
	if b.isRange && b.lo < b.hi-1 {
		return b.lo + int(r.nextIntN(int32(b.hi-b.lo)))
	}
	return b.lo
}

// inclGT is INCL_GT: min + nextInt(max-min+1) when max > min.
func (b bRange) inclGT(r *bmt) int {
	if b.hi > b.lo {
		return b.lo + int(r.nextIntN(int32(b.hi-b.lo+1)))
	}
	return b.lo
}

// inclGE is INCL_GE: min + nextInt(max-min+1) when max >= min, which draws for a single value too
// (TREES-pseudocode U1: a plain number is taken as the range {v, v}).
func (b bRange) inclGE(r *bmt) int {
	if b.hi >= b.lo {
		return b.lo + int(r.nextIntN(int32(b.hi-b.lo+1)))
	}
	return b.lo
}

// bChance is ChanceInformation: a fraction or a percentage (TREES-pseudocode section 0).
type bChance struct {
	kind     int // 0 none (never), 1 fraction, 2 percent
	num, den int
	pct      float32
}

func parseBChance(raw json.RawMessage) (bChance, error) {
	if len(raw) == 0 {
		return bChance{}, nil
	}
	var f float32
	if json.Unmarshal(raw, &f) == nil {
		return bChance{kind: 2, pct: f}, nil
	}
	var fr struct{ Numerator, Denominator int }
	if err := json.Unmarshal(raw, &fr); err != nil {
		return bChance{}, fmt.Errorf("chance %s", raw)
	}
	return bChance{kind: 1, num: fr.Numerator, den: fr.Denominator}, nil
}

// test draws the chance: a fraction takes a raw word % den < num (no draw when num == den, none when
// den is 0), a percentage nextFloat()*100 < pct (no draw at 100 or more or at 0 or less).
func (c bChance) test(r *bmt) bool {
	switch c.kind {
	case 1:
		if c.den == 0 {
			return false
		}
		if c.num == c.den {
			return true
		}
		return int(r.u32()%uint32(c.den)) < c.num
	case 2:
		if c.pct >= 100 {
			return true
		}
		if c.pct <= 0 {
			return false
		}
		return float32(r.nextFloat()*100) < c.pct
	}
	return false
}

// treeCtx holds a tree's block lists (TreeFeature params).
type treeCtx struct {
	growOn, replace, through *bmatch // nil for an empty list
	base                     []bs
	baseMatch                *bmatch
	clusterN, clusterR       int
	clusterReplace           *bmatch
}

func optMatch(raw json.RawMessage) (*bmatch, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	m, err := parseBMatch(raw)
	if err != nil || m.empty {
		return nil, err
	}
	return m, nil
}

func joinLists(a, b json.RawMessage) json.RawMessage {
	var x, y []json.RawMessage
	json.Unmarshal(a, &x)
	json.Unmarshal(b, &y)
	out, _ := json.Marshal(append(x, y...))
	return out
}

func (t *treeCtx) canReplace(s bs) bool { return t.replace == nil || t.replace.has(s) }

// logOK is the acacia trunk's log test as the study models it: air, the replace list, or a plant a
// canopy may overwrite.
func (t *treeCtx) logOK(s bs) bool { return s.isAir() || t.canReplace(s) || bReplant(s) }

// prepare is prepareSpawn (0xd8a9600): inside the world, ground below in may_grow_on, and every block of
// the volume in may_replace + may_grow_through (radius 0 at the base, 1 above, 2 from h-1).
func (t *treeCtx) prepare(d *decoration, p pos, h int) bool {
	if !(minY < p.y && p.y+h <= maxY) {
		return false
	}
	if t.growOn != nil && !t.growOn.has(d.at(p.add(0, -1, 0))) {
		return false
	}
	for yy := p.y; yy <= p.y+h+1; yy++ {
		r := 1
		if yy == p.y {
			r = 0
		}
		if yy >= p.y+h-1 {
			r = 2
		}
		if yy > maxY {
			return false
		}
		if t.through == nil {
			continue
		}
		for xx := p.x - r; xx <= p.x+r; xx++ {
			for zz := p.z - r; zz <= p.z+r; zz++ {
				if !t.through.has(d.block(xx, yy, zz)) {
					return false
				}
			}
		}
	}
	return true
}

// setBase turns the block under a trunk into base_block[0] unless it is in base_block.
func (t *treeCtx) setBase(d *decoration, p pos) {
	if len(t.base) == 0 || p.y < minY {
		return
	}
	if !t.baseMatch.has(d.at(p)) {
		d.bput(p, t.base[0])
	}
}

// bDeco is a trunk or canopy decoration (0xd8a8340, vine column 0xd8b03d0).
type bDeco struct {
	on      bool
	chance  bChance
	block   bdesc
	vine    bool
	steps   bRange
	stepDir int
	state   bs
}

func parseBDeco(raw json.RawMessage) (bDeco, error) {
	if len(raw) == 0 {
		return bDeco{}, nil
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal(raw, &v); err != nil {
		return bDeco{}, err
	}
	dc := bDeco{on: true, stepDir: -1}
	var err error
	if dc.chance, err = parseBChance(v["decoration_chance"]); err != nil {
		return dc, err
	}
	if dc.block, err = parseBDesc(v["decoration_block"]); err != nil {
		return dc, err
	}
	dc.vine = dc.block.name == "vine"
	if !dc.vine {
		if dc.state, err = javaStateFor(dc.block); err != nil {
			return dc, unimpl("%v", err)
		}
	}
	dc.steps = bRange{1, 1, false}
	if raw, ok := v["num_steps"]; ok {
		if dc.steps, err = parseBRange(raw); err != nil {
			return dc, err
		}
	}
	var sd string
	json.Unmarshal(v["step_direction"], &sd)
	switch sd {
	case "", "down":
	case "up":
		dc.stepDir = 1
	default:
		dc.stepDir = 0
	}
	return dc, nil
}

// decoSides are the sides a decoration visits, in the code's order: -x, +x, -z, +z, +y, -y.
var decoSides = [6]pos{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, 1, 0}, {0, -1, 0}}

// vineFaceFor names the face a vine at an offset from its support holds on to.
var vineFaceFor = [6]string{"east", "west", "south", "north", "", ""}

// place decorates around a position on the flagged sides.
func (dc *bDeco) place(d *decoration, r *bmt, p pos, sides [6]bool) {
	if !dc.on {
		return
	}
	for i, off := range decoSides {
		if !sides[i] || !dc.chance.test(r) {
			continue
		}
		q := p.add(off.x, off.y, off.z)
		if !d.at(q).isAir() {
			continue
		}
		s := dc.state
		if dc.vine {
			f := vineFaceFor[i]
			if f == "" {
				f = "north"
			}
			s = vineState(f)
		}
		dc.column(d, r, q, s)
	}
}

var fourSides = [6]bool{true, true, true, true, false, false}

func (dc *bDeco) column(d *decoration, r *bmt, q pos, s bs) {
	n := dc.steps.lo
	if dc.steps.hi != dc.steps.lo && dc.steps.lo <= dc.steps.hi {
		n = dc.steps.lo + int(r.nextIntN(int32(dc.steps.hi-dc.steps.lo+1)))
	}
	if n <= 0 || !d.at(q).isAir() {
		return
	}
	for {
		d.bput(q, s)
		q.y += dc.stepDir
		if !d.at(q).isAir() {
			break
		}
		n--
		if n < 1 {
			break
		}
	}
}

// ---------------------------------------------------------------- tree_feature

type bTrunk interface {
	height(r *bmt) int
	place(d *decoration, t *treeCtx, r *bmt, p pos, h int, c bCanopy) bool
}

// bCanopy places leaves at a position; logs are the trunk's recorded logs (acacia, mangrove) and
// width its trunk width.
type bCanopy interface {
	place(d *decoration, t *treeCtx, r *bmt, p pos, logs []pos, width int) bool
}

// bTreeFeat is tree_feature (VanillaTreeFeature::place 0xd9c15a0): the height first, then the trunk,
// which calls the canopy.
type bTreeFeat struct {
	t      treeCtx
	trunk  bTrunk
	canopy bCanopy
}

// place grows the tree; its output position is above the trunk's top log (trunk x and z, top log y + 1),
// which a sequence hands on (NOTES-bedrock-exact T31: the jungle tree's cocoa sits 4 and 5 under it).
func (f *bTreeFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	h := f.trunk.height(r)
	if !f.trunk.place(d, &f.t, r, p, h, f.canopy) {
		return p, false
	}
	top := p
	if !d.at(p).tagged(tagLogs) {
		return pos{p.x, p.y + h, p.z}, true
	}
	for top.y < maxY && d.at(top.step(dUp)).tagged(tagLogs) {
		top.y++
	}
	return pos{p.x, top.y + 1, p.z}, true
}

func (fc *featCompiler) tree(v map[string]json.RawMessage) (bfeat, error) {
	f := &bTreeFeat{}
	var err error
	if f.t.growOn, err = optMatch(v["may_grow_on"]); err != nil {
		return nil, err
	}
	if f.t.replace, err = optMatch(v["may_replace"]); err != nil {
		return nil, err
	}
	if f.t.through, err = optMatch(joinLists(v["may_replace"], v["may_grow_through"])); err != nil {
		return nil, err
	}
	if raw, ok := v["base_block"]; ok {
		var list []json.RawMessage
		json.Unmarshal(raw, &list)
		for _, e := range list {
			b, err := parseBDesc(e)
			if err != nil {
				return nil, err
			}
			s, err := javaStateFor(b)
			if err != nil {
				return nil, unimpl("%v", err)
			}
			f.t.base = append(f.t.base, s)
		}
		if f.t.baseMatch, err = parseBMatch(raw); err != nil {
			return nil, err
		}
	}
	if raw, ok := v["base_cluster"]; ok {
		var bc struct {
			N int             `json:"num_clusters"`
			R int             `json:"cluster_radius"`
			M json.RawMessage `json:"may_replace"`
		}
		json.Unmarshal(raw, &bc)
		f.t.clusterN, f.t.clusterR = bc.N, bc.R
		if f.t.clusterReplace, err = parseBMatch(bc.M); err != nil {
			return nil, err
		}
	}
	for k, raw := range v {
		if k == "roots" || k == "mangrove_roots" {
			return nil, unimpl("tree roots %s", k)
		}
		var body map[string]json.RawMessage
		if strings.HasSuffix(k, "trunk") {
			if err := json.Unmarshal(raw, &body); err != nil {
				return nil, err
			}
			if f.trunk, err = fc.trunk(k, body); err != nil {
				return nil, err
			}
		}
		if strings.HasSuffix(k, "canopy") {
			if err := json.Unmarshal(raw, &body); err != nil {
				return nil, err
			}
			if f.canopy, err = fc.canopy(k, body); err != nil {
				return nil, err
			}
		}
	}
	if f.trunk == nil {
		return nil, unimpl("tree with no trunk")
	}
	return f, nil
}

// canopyOf reads a {kind: body} canopy object (branch canopies).
func (fc *featCompiler) canopyOf(raw json.RawMessage) (bCanopy, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || len(m) != 1 {
		return nil, fmt.Errorf("canopy %s", raw)
	}
	for k, b := range m {
		return fc.canopy(k, b)
	}
	return nil, nil
}

func blockState(raw json.RawMessage) (bs, bdesc, error) {
	b, err := parseBDesc(raw)
	if err != nil {
		return 0, b, err
	}
	s, err := javaStateFor(b)
	if err != nil {
		return 0, b, unimpl("%v", err)
	}
	return s, b, nil
}

func (fc *featCompiler) trunk(kind string, v map[string]json.RawMessage) (bTrunk, error) {
	log, _, err := blockState(v["trunk_block"])
	if err != nil {
		return nil, err
	}
	deco, err := parseBDeco(v["trunk_decoration"])
	if err != nil {
		return nil, err
	}
	switch kind {
	case "trunk":
		t := &simpleTrunk{log: log, deco: deco}
		if t.h, err = parseBRange(v["trunk_height"]); err != nil {
			return nil, err
		}
		if t.mod, err = parseBRange(v["height_modifier"]); err != nil {
			return nil, err
		}
		var sub struct {
			Depth int `json:"max_depth"`
		}
		json.Unmarshal(v["can_be_submerged"], &sub)
		t.sink = sub.Depth
		return t, nil
	case "acacia_trunk":
		return fc.acaciaTrunk(v, log, deco)
	case "fancy_trunk":
		return fancyTrunkOf(v, log)
	case "fallen_trunk":
		t := &fallenTrunk{log: log, deco: deco}
		if t.length, err = parseBRange(v["log_length"]); err != nil {
			return nil, err
		}
		if t.mod, err = parseBRange(v["height_modifier"]); err != nil {
			return nil, err
		}
		t.stump = bRange{1, 1, false}
		if raw, ok := v["stump_height"]; ok {
			if t.stump, err = parseBRange(raw); err != nil {
				return nil, err
			}
		}
		if raw, ok := v["log_decoration_feature"]; ok {
			if t.logDeco, err = fc.child(raw); err != nil {
				return nil, err
			}
		}
		return t, nil
	case "mega_trunk":
		return fc.megaTrunk(v, log, deco)
	case "cherry_trunk":
		return fc.bCherryTrunk(v, log)
	}
	return nil, unimpl("trunk %s", kind)
}

func (fc *featCompiler) canopy(kind string, v map[string]json.RawMessage) (bCanopy, error) {
	leafRaw := v["leaf_block"]
	switch kind {
	case "canopy":
		leaf, _, err := blockState(leafRaw)
		if err != nil {
			return nil, err
		}
		c := &simpleCanopy{leaf: leaf, lo: -3, hi: 0, minW: 1, slope: 0.5}
		var off struct{ Min, Max int }
		if raw, ok := v["canopy_offset"]; ok {
			json.Unmarshal(raw, &off)
			c.lo, c.hi = off.Min, off.Max
		}
		if raw, ok := v["min_width"]; ok {
			json.Unmarshal(raw, &c.minW)
		}
		if raw, ok := v["canopy_slope"]; ok {
			var sl struct{ Rise, Run float32 }
			json.Unmarshal(raw, &sl)
			if sl.Run != 0 {
				c.slope = sl.Rise / sl.Run
			}
		}
		raw := json.RawMessage(strings.TrimSpace(string(v["variation_chance"])))
		if len(raw) > 0 && raw[0] == '[' {
			var list []json.RawMessage
			json.Unmarshal(raw, &list)
			for _, e := range list {
				ch, err := parseBChance(e)
				if err != nil {
					return nil, err
				}
				c.vary = append(c.vary, ch)
			}
		} else if len(raw) > 0 {
			ch, err := parseBChance(raw)
			if err != nil {
				return nil, err
			}
			for range c.hi - c.lo + 1 {
				c.vary = append(c.vary, ch)
			}
		}
		if c.deco, err = parseBDeco(v["canopy_decoration"]); err != nil {
			return nil, err
		}
		return c, nil
	case "acacia_canopy":
		leaf, _, err := blockState(leafRaw)
		if err != nil {
			return nil, err
		}
		c := &acaciaCanopy{leaf: leaf, size: 2}
		if raw, ok := v["canopy_size"]; ok {
			json.Unmarshal(raw, &c.size)
		}
		json.Unmarshal(v["simplify_canopy"], &c.simple)
		return c, nil
	case "roofed_canopy":
		leaf, _, err := blockState(leafRaw)
		if err != nil {
			return nil, err
		}
		c := &roofedCanopy{leaf: leaf, h: 4, core: 2, outer: 2, inner: 3}
		json.Unmarshal(v["canopy_height"], &c.h)
		c.h -= 3 // The schema setter 0x4d07f90 stores canopy_height - 3 (R3).
		if raw, ok := v["core_width"]; ok {
			json.Unmarshal(raw, &c.core)
			c.useCore = true
		}
		json.Unmarshal(v["outer_radius"], &c.outer)
		json.Unmarshal(v["inner_radius"], &c.inner)
		return c, nil
	case "fancy_canopy":
		leaf, _, err := blockState(leafRaw)
		if err != nil {
			return nil, err
		}
		c := &fancyCanopy{leaf: leaf, h: 4, r: 3}
		json.Unmarshal(v["height"], &c.h)
		json.Unmarshal(v["radius"], &c.r)
		return c, nil
	case "pine_canopy":
		leaf, _, err := blockState(leafRaw)
		if err != nil {
			return nil, err
		}
		c := &pineCanopy{leaf: leaf, base: 1}
		json.Unmarshal(v["base_radius"], &c.base)
		c.h = bRange{3, 3, false}
		if raw, ok := v["canopy_height"]; ok {
			if c.h, err = parseBRange(raw); err != nil {
				return nil, err
			}
		}
		return c, nil
	case "spruce_canopy":
		leaf, _, err := blockState(leafRaw)
		if err != nil {
			return nil, err
		}
		c := &spruceCanopy{leaf: leaf}
		if c.lower, err = parseBRange(v["lower_offset"]); err != nil {
			return nil, err
		}
		if c.upper, err = parseBRange(v["upper_offset"]); err != nil {
			return nil, err
		}
		c.maxR = bRange{2, 4, true}
		if raw, ok := v["max_radius"]; ok {
			if c.maxR, err = parseBRange(raw); err != nil {
				return nil, err
			}
		}
		return c, nil
	case "random_spread_canopy":
		c := &randomSpreadCanopy{}
		var err error
		if c.h, err = parseBRange(v["canopy_height"]); err != nil {
			return nil, err
		}
		if c.r, err = parseBRange(v["canopy_radius"]); err != nil {
			return nil, err
		}
		json.Unmarshal(v["leaf_placement_attempts"], &c.attempts)
		var list [][2]json.RawMessage
		json.Unmarshal(v["leaf_blocks"], &list)
		for _, e := range list {
			s, _, err := blockState(e[0])
			if err != nil {
				return nil, err
			}
			var w float32
			json.Unmarshal(e[1], &w)
			c.leaves, c.w = append(c.leaves, s), append(c.w, w)
		}
		return c, nil
	case "mega_canopy":
		return megaCanopyOf(v)
	case "mega_pine_canopy":
		return megaPineCanopyOf(v)
	case "cherry_canopy":
		return cherryCanopyOf(v)
	}
	return nil, unimpl("canopy %s", kind)
}

// simpleTrunk is SimpleTreeTrunk (Q1).
type simpleTrunk struct {
	log  bs
	h    bRange
	mod  bRange
	sink int
	deco bDeco
}

func (t *simpleTrunk) height(r *bmt) int { return t.h.excl(r) }

func (t *simpleTrunk) place(d *decoration, c *treeCtx, r *bmt, p pos, h int, can bCanopy) bool {
	y0 := p.y
	for range t.sink {
		if c.through != nil && c.through.has(d.at(p.add(0, -1, 0))) {
			p.y--
		} else {
			break
		}
	}
	if !c.prepare(d, p, h) {
		return false
	}
	h2 := h + t.mod.excl(r)
	var top pos
	has := false
	for i := range max(0, h2) {
		q := p.add(0, i, 0)
		list := c.replace
		if q.y < y0 {
			list = c.through
		}
		if list != nil && !list.has(d.at(q)) {
			continue
		}
		if !d.bput(q, t.log) {
			return false
		}
		t.deco.place(d, r, q, fourSides)
		top, has = q.add(0, 1, 0), true
	}
	if !has {
		return false
	}
	if can != nil {
		can.place(d, c, r, top, nil, 1)
	}
	c.setBase(d, p.add(0, -1, 0))
	return true
}

// simpleCanopy is SimpleTreeCanopy, Java's blob (Q1).
type simpleCanopy struct {
	leaf         bs
	lo, hi, minW int
	slope        float32
	vary         []bChance
	deco         bDeco
}

func (c *simpleCanopy) radius(dy int) int {
	return c.minW + int(float32(c.hi)*c.slope) - int(float32(dy)*c.slope)
}

func (c *simpleCanopy) place(d *decoration, t *treeCtx, r *bmt, p pos, _ []pos, _ int) bool {
	for dy := c.lo; dy <= c.hi; dy++ {
		rad := c.radius(dy)
		yy := p.y + dy
		var vc *bChance
		if i := dy - c.lo; i < len(c.vary) {
			vc = &c.vary[i]
		}
		for xx := p.x - rad; xx <= p.x+rad; xx++ {
			for zz := p.z - rad; zz <= p.z+rad; zz++ {
				if absInt(xx-p.x) == rad && absInt(zz-p.z) == rad && vc != nil && vc.test(r) {
					continue
				}
				if yy < minY || yy > maxY {
					continue
				}
				if s := d.block(xx, yy, zz); s.isAir() || bReplant(s) {
					d.bput(pos{xx, yy, zz}, c.leaf)
				}
			}
		}
	}
	if c.deco.on {
		lb := c.leaf.block()
		for dy := c.lo; dy <= c.hi; dy++ {
			rad := c.radius(dy)
			yy := p.y + dy
			for xx := p.x - rad; xx <= p.x+rad; xx++ {
				for zz := p.z - rad; zz <= p.z+rad; zz++ {
					if d.block(xx, yy, zz).block() == lb {
						c.deco.place(d, r, pos{xx, yy, zz}, fourSides)
					}
				}
			}
		}
	}
	return true
}

// acaciaCanopy (0xd8fb4b0): no draws; overwrites anything in may_replace, leaves included.
type acaciaCanopy struct {
	leaf   bs
	size   int
	simple bool
}

func (c *acaciaCanopy) place(d *decoration, t *treeCtx, r *bmt, p pos, _ []pos, _ int) bool {
	put := func(x, y, z int) {
		if y >= minY && y <= maxY && t.canReplace(d.block(x, y, z)) {
			d.bput(pos{x, y, z}, c.leaf)
		}
	}
	s := c.size
	m := max(1, s)
	if c.simple {
		for dx := 1 - m; dx < m; dx++ {
			for dz := 1 - m; dz < m; dz++ {
				put(p.x+dx, p.y+1, p.z+dz)
			}
		}
	} else {
		n2 := max(2, s)
		for dx := 2 - n2; dx < n2-1; dx++ {
			for dz := 2 - n2; dz < n2-1; dz++ {
				put(p.x+dx, p.y+1, p.z+dz)
			}
		}
		put(p.x+m-1, p.y+1, p.z)
		put(p.x+1-m, p.y+1, p.z)
		put(p.x, p.y+1, p.z+m-1)
		put(p.x, p.y+1, p.z+1-m)
	}
	for dx := -s; dx <= s; dx++ {
		for dz := -s; dz <= s; dz++ {
			if absInt(dx) != s || absInt(dz) != s {
				put(p.x+dx, p.y, p.z+dz)
			}
		}
	}
	return true
}

// acaciaTrunk (0xd8fbac0), for acacia, dark oak, pale oak and azalea.
type acaciaTrunk struct {
	log                      bs
	base                     int
	intervals                []int
	width                    int
	diag                     bool
	leanH, leanSteps, leanLn bRange
	hasLeanLn                bool
	minCanopy                int
	deco                     bDeco
	branches                 bool
	bChance                  bChance
	bLength, bPos            bRange
	bCanopy                  bCanopy
}

var (
	bedrockDX = [4]int{0, -1, 0, 1}
	bedrockDZ = [4]int{1, 0, -1, 0}
)

func (fc *featCompiler) acaciaTrunk(v map[string]json.RawMessage, log bs, deco bDeco) (bTrunk, error) {
	t := &acaciaTrunk{log: log, deco: deco, width: 1}
	var th struct {
		Base      int   `json:"base"`
		Intervals []int `json:"intervals"`
		MinCanopy *int  `json:"min_height_for_canopy"`
	}
	if err := json.Unmarshal(v["trunk_height"], &th); err != nil {
		return nil, err
	}
	t.base, t.intervals = th.Base, th.Intervals
	if th.MinCanopy != nil {
		t.minCanopy = *th.MinCanopy
	}
	if raw, ok := v["min_height_for_canopy"]; ok {
		json.Unmarshal(raw, &t.minCanopy)
	}
	json.Unmarshal(v["trunk_width"], &t.width)
	var lean map[string]json.RawMessage
	json.Unmarshal(v["trunk_lean"], &lean)
	json.Unmarshal(lean["allow_diagonal_growth"], &t.diag)
	var err error
	if t.leanH, err = parseBRange(lean["lean_height"]); err != nil {
		return nil, err
	}
	if t.leanSteps, err = parseBRange(lean["lean_steps"]); err != nil {
		return nil, err
	}
	if raw, ok := lean["lean_length"]; ok {
		t.hasLeanLn = true
		if t.leanLn, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	if raw, ok := v["branches"]; ok {
		var br map[string]json.RawMessage
		json.Unmarshal(raw, &br)
		t.branches = true
		if t.bChance, err = parseBChance(br["branch_chance"]); err != nil {
			return nil, err
		}
		if t.bLength, err = parseBRange(br["branch_length"]); err != nil {
			return nil, err
		}
		if t.bPos, err = parseBRange(br["branch_position"]); err != nil {
			return nil, err
		}
		if t.bCanopy, err = fc.canopyOf(br["branch_canopy"]); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func (t *acaciaTrunk) height(r *bmt) int {
	h := t.base
	for _, iv := range t.intervals {
		h += int(r.nextIntN(int32(iv)))
	}
	return h
}

func (t *acaciaTrunk) place(d *decoration, c *treeCtx, r *bmt, p pos, h int, can bCanopy) bool {
	if !c.prepare(d, p, h) || p.y >= maxY+1-h-1 {
		return false
	}
	w := t.width
	for i := range w {
		for j := range w {
			c.setBase(d, p.add(i, -1, j))
		}
	}
	dirn := int(r.nextIntN(4))
	lh := t.leanH.excl(r)
	steps := t.leanSteps.excl(r)
	extra := 0
	if t.hasLeanLn {
		extra = t.leanLn.excl(r)
	}
	start := h - lh
	cx, cy, cz := p.x, p.y, p.z
	var logs []pos
	for i := range h + extra {
		if i >= start && steps > 0 {
			cx += bedrockDX[dirn]
			cz += bedrockDZ[dirn]
			steps--
		}
		if i < h {
			cy = p.y + i
		}
		probe := d.block(cx, cy, cz)
		if cy > maxY || cy < minY {
			probe = sStone
		}
		if c.replace != nil && !c.logOK(probe) {
			continue
		}
		for a := range w {
			for b := range w {
				q := pos{p.x + a, p.y + i, p.z + b}
				if t.diag {
					q = pos{cx + a, cy, cz + b}
				}
				if q.y < minY || q.y > maxY {
					continue
				}
				if n := d.at(q); c.replace != nil && !c.logOK(n) {
					continue
				}
				if d.bput(q, t.log) {
					t.deco.place(d, r, q, [6]bool{a == 0, a == w-1, b == 0, b == w-1})
					if i >= t.minCanopy {
						logs = append(logs, q)
					}
				}
			}
		}
	}
	var last pos
	if len(logs) > 0 {
		last = logs[len(logs)-1]
	}
	if can != nil {
		can.place(d, c, r, last, logs, w)
	}
	if !t.branches {
		return true
	}
	if t.diag {
		bd := int(r.nextIntN(4))
		if bd != dirn && t.bChance.test(r) {
			bp := t.bPos.excl(r)
			bl := t.bLength.excl(r)
			px, py, pz := p.x, last.y, p.z
			k := start - bp
			placed, savey := false, py
			if k < h && bl > 0 {
				for {
					ok := false
					if k > 0 {
						py = p.y + k
						px += bedrockDX[bd]
						pz += bedrockDZ[bd]
						if py >= minY && py <= maxY {
							n := d.block(px, py, pz)
							ok = c.replace == nil || c.logOK(n)
						}
					}
					if ok {
						if d.bput(pos{px, py, pz}, t.log) {
							t.deco.place(d, r, pos{px, py, pz}, fourSides)
						}
						savey, placed = py, true
						k++
						if k >= h {
							break
						}
						bl--
						if bl < 1 {
							break
						}
						continue
					}
					k++
					if k >= h || bl < 2 {
						break
					}
					bl--
				}
				if placed && t.bCanopy != nil {
					t.bCanopy.place(d, c, r, pos{px, savey, pz}, nil, 1)
				}
			}
		}
		return true
	}
	for fx := -1; fx <= w; fx++ {
		for fz := -1; fz <= w; fz++ {
			if !(fx < 0 || fz >= w || fz < 0 || fx >= w) {
				continue
			}
			if !t.bChance.test(r) {
				continue
			}
			bl := t.bLength.excl(r)
			bp := t.bPos.excl(r)
			for i := range bl {
				q := pos{p.x + fx, last.y - i - bp, p.z + fz}
				if q.y >= minY && q.y <= maxY && (c.replace == nil || c.logOK(d.at(q))) {
					d.bput(q, t.log)
				}
			}
			if t.bCanopy != nil {
				t.bCanopy.place(d, c, r, pos{last.x + fx, last.y - bp, last.z + fz}, nil, 1)
			}
		}
	}
	return true
}

// roofedCanopy (0xd8ae3d0, R3): air only.
type roofedCanopy struct {
	leaf                  bs
	h, core, outer, inner int
	useCore               bool
}

func (c *roofedCanopy) place(d *decoration, t *treeCtx, r *bmt, p pos, _ []pos, width int) bool {
	w1 := width
	if c.useCore {
		w1 = c.core
	}
	w2 := w1
	a, b := w1-1, w2-1
	o, ri, h := c.outer, c.inner, c.h
	put := func(x, y, z int) {
		if y >= minY && y <= maxY && d.block(x, y, z).isAir() {
			d.bput(pos{x, y, z}, c.leaf)
		}
	}
	x, y, z := p.x, p.y, p.z
	for i := -o; i <= 0; i++ {
		for j := -o; j <= 0; j++ {
			put(x+i, y-1, z+j)
			put(x+a-i, y-1, z+j)
			put(x+i, y-1, z+b-j)
			put(x+a-i, y-1, z+b-j)
			if (i > -o || j > 1-o) && !(j == -o && i == 1-o) {
				put(x+i, y+h, z+j)
				put(x+a-i, y+h, z+j)
				put(x+i, y+h, z+b-j)
				put(x+a-i, y+h, z+b-j)
			}
		}
	}
	if r.nextBool() {
		for i := range w1 {
			for j := range w2 {
				put(x+i, y+h+1, z+j)
			}
		}
	}
	for k := range h {
		for i := -ri; i < w1+ri; i++ {
			for j := -ri; j < w2+ri; j++ {
				switch {
				case j == -ri && i == -ri, i == -ri && j == b+ri, i == a+ri && (j == -ri || j == b+ri), ri <= absInt(i) && ri <= absInt(j):
					continue
				}
				put(x+i, y+k, z+j)
			}
		}
	}
	return true
}

// fancyCanopy (0xd8ff0b0, layer 0xd8ff210): layers of radius r-1, r, ..., r-1.
type fancyCanopy struct {
	leaf bs
	h, r int
}

func (c *fancyCanopy) layer(d *decoration, t *treeCtx, p pos, y, rad int) {
	if y < minY || y > maxY {
		return
	}
	r2 := float32(rad * rad)
	for dx := -rad; dx <= rad; dx++ {
		for dz := -rad; dz <= rad; dz++ {
			a, b := float32(absInt(dz))+0.5, float32(absInt(dx))+0.5
			if float32(a*a)+float32(b*b) <= r2 && t.canReplace(d.block(p.x+dx, y, p.z+dz)) {
				d.bput(pos{p.x + dx, y, p.z + dz}, c.leaf)
			}
		}
	}
}

func (c *fancyCanopy) place(d *decoration, t *treeCtx, r *bmt, p pos, _ []pos, _ int) bool {
	if c.h == 0 {
		return false
	}
	c.layer(d, t, p, p.y, c.r-1)
	for i := 1; i < c.h; i++ {
		rad := c.r
		if i == c.h-1 {
			rad--
		}
		c.layer(d, t, p, p.y+i, rad)
	}
	return true
}

// bFancyTrunk (0xd8ff450): Java's big oak with Bedrock's float casts and truncations.
type bFancyTrunk struct {
	log                          bs
	base, variance, width        int
	scale, slope, density        float32
	minAlt, widthScale, foliage  float32
}

func fancyTrunkOf(v map[string]json.RawMessage, log bs) (bTrunk, error) {
	t := &bFancyTrunk{log: log, variance: 12, scale: 0.618, slope: 0.381, density: 1, minAlt: 0.2, widthScale: 1, foliage: 0.3, width: 1}
	var th struct {
		Base     int      `json:"base"`
		Variance *int     `json:"variance"`
		Scale    *float32 `json:"scale"`
	}
	json.Unmarshal(v["trunk_height"], &th)
	t.base = th.Base
	if th.Variance != nil {
		t.variance = *th.Variance
	}
	if th.Scale != nil {
		t.scale = *th.Scale
	}
	var br struct {
		Slope   *float32 `json:"slope"`
		Density *float32 `json:"density"`
		MinAlt  *float32 `json:"min_altitude_factor"`
	}
	json.Unmarshal(v["branches"], &br)
	if br.Slope != nil {
		t.slope = *br.Slope
	}
	if br.Density != nil {
		t.density = *br.Density
	}
	if br.MinAlt != nil {
		t.minAlt = *br.MinAlt
	}
	if raw, ok := v["width_scale"]; ok {
		json.Unmarshal(raw, &t.widthScale)
	}
	if raw, ok := v["foliage_altitude_factor"]; ok {
		json.Unmarshal(raw, &t.foliage)
	}
	if raw, ok := v["trunk_width"]; ok {
		json.Unmarshal(raw, &t.width)
	}
	return t, nil
}

func (t *bFancyTrunk) height(r *bmt) int { return t.base + int(r.nextIntN(int32(t.variance))) }

// fancyLine is the points of a limb, each offset truncated after adding 0.5.
func fancyLine(a, b pos, visit func(q pos) bool) int {
	dx, dy, dz := b.x-a.x, b.y-a.y, b.z-a.z
	n := max(absInt(dx), absInt(dy), absInt(dz))
	for i := 0; i <= n; i++ {
		q := a
		if n != 0 {
			fi := float32(i)
			q = pos{a.x + int(float32(fi*(float32(dx)/float32(n)))+0.5), a.y + int(float32(fi*(float32(dy)/float32(n)))+0.5),
				a.z + int(float32(fi*(float32(dz)/float32(n)))+0.5)}
		}
		if !visit(q) {
			return i
		}
	}
	return -1
}

func (t *bFancyTrunk) place(d *decoration, c *treeCtx, r *bmt, p pos, hl int, can bCanopy) bool {
	if c.growOn != nil && !c.growOn.has(d.at(p.add(0, -1, 0))) {
		return false
	}
	lineOK := func(a, b pos) int {
		return fancyLine(a, b, func(q pos) bool {
			if q.y < minY || q.y > maxY {
				return false
			}
			return c.through == nil || c.through.has(d.at(q))
		})
	}
	if k := lineOK(p, p.add(0, max(0, hl-1), 0)); k != -1 {
		if k <= t.base {
			return false
		}
		hl = k
	}
	fh := float32(hl)
	trunkH := min(int(t.scale*fh), hl-1)
	ratio := float32(t.density * fh / float32(t.variance+1))
	cl := max(1, int(float32(ratio*ratio)+1.382))
	ldl := (t.variance + 1) / 3
	topY := p.y + trunkH
	type node struct{ p pos; base int }
	nodes := []node{{pos{p.x, p.y + hl - ldl, p.z}, topY}}
	half := float32(0.5) * fh
	for kk := hl - ldl; kk >= 0; kk-- {
		if !(t.foliage*fh <= float32(kk)) {
			continue
		}
		f1 := half - float32(kk)
		var rad float32
		switch {
		case f1 == 0:
			rad = half
		case float32(math.Abs(float64(f1))) >= half:
			rad = 0
		default:
			rad = float32(math.Sqrt(float64(half*half - f1*f1)))
		}
		rad *= 0.5
		if rad < 0 {
			continue
		}
		for range cl {
			d0 := float32(float32(r.nextFloat()+0.328)*t.widthScale) * rad
			a2 := r.nextFloat()
			ang := float32(float32(a2+a2)*3.1415927) * 10430.378
			sx, sz := bedrockSin[int32(ang)&0xffff], bedrockSin[int32(float32(ang+16384))&0xffff]
			bx := p.x + int(float32(sx*d0)+0.5)
			bz := p.z + int(float32(d0*sz)+0.5)
			by := p.y + kk - 1
			if lineOK(pos{bx, by, bz}, pos{bx, by + ldl, bz}) != -1 {
				continue
			}
			dd := (p.z-bz)*(p.z-bz) + (p.x-bx)*(p.x-bx)
			d4 := float32(by) - float32(int(math.Sqrt(float64(dd))))*t.slope
			k1 := topY
			if !(float32(topY) < d4) {
				k1 = int(d4)
			}
			if lineOK(pos{p.x, k1, p.z}, pos{bx, by, bz}) != -1 {
				continue
			}
			nodes = append(nodes, node{pos{bx, by, bz}, k1})
		}
	}
	if can != nil {
		for _, n := range nodes {
			can.place(d, c, r, n.p, nil, t.width)
		}
	}
	limb := func(a, b pos) {
		fancyLine(a, b, func(q pos) bool {
			if q.y >= minY && q.y <= maxY {
				d.bput(q, t.log)
			}
			return true
		})
	}
	for i := range t.width {
		for j := range t.width {
			limb(p.add(i, 0, j), pos{p.x + i, topY, p.z + j})
		}
	}
	for _, n := range nodes {
		sx, sz := p.x, p.z
		if n.p.x > p.x {
			sx = p.x - 1 + t.width
		}
		if n.p.z > p.z {
			sz = p.z - 1 + t.width
		}
		if t.minAlt*fh <= float32(n.base-p.y) {
			limb(pos{sx, n.base, sz}, n.p)
		}
	}
	c.setBase(d, p.add(0, -1, 0))
	return true
}

// pineCanopy (0xd8ac580).
type pineCanopy struct {
	leaf bs
	base int
	h    bRange
}

func (c *pineCanopy) place(d *decoration, t *treeCtx, r *bmt, p pos, _ []pos, _ int) bool {
	ch := c.h.excl(r)
	k := int(r.nextIntN(int32(ch+1))) + c.base
	if ch == -1 {
		return false
	}
	rad := 0
	for dy := 1; dy > -ch; dy-- {
		for dx := -rad; dx <= rad; dx++ {
			for dz := -rad; dz <= rad; dz++ {
				if rad != 0 && absInt(dx) == rad && absInt(dz) == rad {
					continue
				}
				y := p.y + dy
				if y >= minY && y <= maxY && t.canReplace(d.block(p.x+dx, y, p.z+dz)) {
					d.bput(pos{p.x + dx, y, p.z + dz}, c.leaf)
				}
			}
		}
		nr := rad + 1
		if k <= rad {
			nr = rad
		}
		if dy == 2-ch && rad >= 1 {
			nr = rad - 1
		}
		rad = nr
	}
	return true
}

// spruceCanopy (0xd8afc40).
type spruceCanopy struct {
	leaf               bs
	lower, upper, maxR bRange
}

func (c *spruceCanopy) place(d *decoration, t *treeCtx, r *bmt, p pos, _ []pos, _ int) bool {
	yg := p.y
	for t.growOn != nil && !t.growOn.has(d.block(p.x, yg-1, p.z)) {
		if !(minY < yg) {
			return false
		}
		yg--
	}
	lo := c.lower.excl(r)
	up := c.upper.excl(r)
	maxr := c.maxR.excl(r)
	mmin, mmax := c.maxR.lo, c.maxR.hi
	if !c.maxR.isRange {
		mmax = mmin + 1
	}
	rad := int(r.nextIntN(int32(mmax - mmin)))
	j3, k3 := mmax-mmin-1, 0
	top, bottom := p.y+up, yg+lo
	for yy := top; yy >= bottom; yy-- {
		for dx := -rad; dx <= rad; dx++ {
			for dz := -rad; dz <= rad; dz++ {
				if rad != 0 && absInt(dx) == rad && absInt(dz) == rad {
					continue
				}
				if yy >= minY && yy <= maxY {
					if s := d.block(p.x+dx, yy, p.z+dz); s.isAir() || bReplant(s) {
						d.bput(pos{p.x + dx, yy, p.z + dz}, c.leaf)
					}
				}
			}
		}
		if rad < j3 {
			rad++
		} else {
			rad, k3 = k3, 1
			j3 = min(j3+1, maxr)
		}
	}
	return true
}

// fallenTrunk (0xd8fe9e0).
type fallenTrunk struct {
	log                 bs
	length, mod, stump  bRange
	deco                bDeco
	logDeco             bfeat
}

func (t *fallenTrunk) height(r *bmt) int { return t.length.excl(r) }

var fallenDirs = [4]pos{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} // facing 2..5

func (t *fallenTrunk) place(d *decoration, c *treeCtx, r *bmt, p pos, h int, _ bCanopy) bool {
	if !c.prepare(d, p, h) {
		return false
	}
	hm := t.mod.excl(r)
	dirn := int(r.nextIntN(4))
	dist := int(r.nextIntN(2)) + 2
	off := fallenDirs[dirn]
	sx, sz := p.x+off.x*dist, p.z+off.z*dist
	stump := t.stump.excl(r)
	sy := p.y + stump
	for sy-1 > minY {
		if n := d.block(sx, sy-1, sz); !n.isAir() && !bReplant(n) {
			break
		}
		sy--
	}
	length := h + hm - 2
	ok, gap := true, 0
	px, pz := sx, sz
	for range length {
		if n := d.block(px, sy, pz); !n.isAir() && !bReplant(n) {
			ok = false
		}
		b := d.block(px, sy-1, pz)
		sup := !b.isAir() && !bWaterMat(b) && !bReplant(b) && !bLeaves(b)
		if !(gap < 2 || sup) {
			ok = false
		}
		if sup {
			gap = 0
		} else {
			gap++
		}
		px += off.x
		pz += off.z
	}
	if ok && length >= 1 {
		axis := "x"
		if off.x == 0 {
			axis = "z"
		}
		lying := stateOf(strings.Replace(t.log.name(), "axis=y", "axis="+axis, 1))
		px, pz = sx, sz
		for range length {
			d.bput(pos{px, sy, pz}, lying)
			if t.logDeco != nil {
				t.logDeco.place(d, r, pos{px, sy, pz})
			}
			px += off.x
			pz += off.z
		}
	}
	for i := range stump {
		q := p.add(0, i, 0)
		if c.replace != nil && !c.replace.has(d.at(q)) {
			continue
		}
		if q.y <= maxY && d.bput(q, t.log) {
			t.deco.place(d, r, q, fourSides)
		}
	}
	c.setBase(d, p.add(0, -1, 0))
	return true
}

// randomSpreadCanopy (azalea, 0xd8ade50): per recorded log, attempts of a weighted leaf and offsets
// a+b-r+1 each.
type randomSpreadCanopy struct {
	h, r     bRange
	attempts int
	leaves   []bs
	w        []float32
}

func (c *randomSpreadCanopy) place(d *decoration, t *treeCtx, r *bmt, p pos, logs []pos, _ int) bool {
	h := c.h.inclGE(r)
	rad := c.r.inclGE(r)
	for _, e := range logs {
		for range c.attempts {
			k := weightedPick(r, c.w)
			a, b := int(r.nextIntN(int32(rad))), int(r.nextIntN(int32(rad)))
			cc, dd := int(r.nextIntN(int32(h))), int(r.nextIntN(int32(h)))
			f, g := int(r.nextIntN(int32(rad))), int(r.nextIntN(int32(rad)))
			q := pos{e.x + a + b - rad + 1, e.y + cc + dd - h + 1, e.z + f + g - rad + 1}
			if q.y < minY || q.y > maxY || k < 0 {
				continue
			}
			if n := d.at(q); t.canReplace(n) || bPassable(n) || bLeaves(n) {
				d.bput(q, c.leaves[k])
			}
		}
	}
	return true
}

// bPassable is the trail's open test (0xd8a9dc0) as the study models it: air, water, a replaceable
// plant, moss carpet, hanging roots or glow lichen.
func bPassable(s bs) bool {
	return s.isAir() || bWaterMat(s) || bReplant(s) || bnPassable.has(bname(s))
}

var bnPassable = bnameSetOf("moss_carpet hanging_roots glow_lichen")

// ---------------------------------------------------------------- huge mushrooms

// bHugeMushroom is HugeMushroomFeature (0xd9c0580): kind 0 brown, 1 red, 3 random; Java 1.12's shape.
type bHugeMushroom struct{ kind int }

var (
	sBrownCap    = laterState("brown_mushroom_block[down=false,east=true,north=true,south=true,up=true,west=true]")
	sRedCap      = laterState("red_mushroom_block[down=false,east=true,north=true,south=true,up=true,west=true]")
	sStem        = laterState("mushroom_stem[down=false,east=true,north=true,south=true,up=false,west=true]")
	bnMushGround = bnameSetOf("dirt grass_block mycelium podzol coarse_dirt dirt_with_roots moss_block mud")
	bnMushrooms  = bnameSetOf("brown_mushroom red_mushroom")
)

// mushSolid is the study's "solid" for the mushroom: not air, leaves, a plant, water or a small mushroom.
func mushSolid(s bs) bool {
	return !(s.isAir() || bLeaves(s) || bReplant(s) || bWaterMat(s) || bnMushrooms.has(bname(s)))
}

func (f bHugeMushroom) place(d *decoration, r *bmt, p pos) bool {
	kind := f.kind
	if kind == 3 {
		kind = int(r.nextIntN(2))
	}
	h := int(r.nextIntN(3)) + 4
	if r.nextIntN(12) == 0 {
		h *= 2
	}
	x, y, z := p.x, p.y, p.z
	if !(minY < y && y+h+1 <= maxY) {
		return false
	}
	for yy := y; yy <= y+h+1; yy++ {
		rr := 0
		if yy > y+3 {
			rr = 3
		}
		for xx := x - rr; xx <= x+rr; xx++ {
			for zz := z - rr; zz <= z+rr; zz++ {
				n := d.block(xx, yy, zz)
				if !(n.isAir() || bLeaves(n)) && (xx != x || yy != y || zz != z || !bnMushrooms.has(bname(n))) {
					return false
				}
			}
		}
	}
	if !bnMushGround.has(bname(d.block(x, y-1, z))) {
		return false
	}
	cap := sBrownCap
	if kind != 0 {
		cap = sRedCap
	}
	top := y + h
	from := top
	if kind == 1 {
		from = top - 3
	}
	for yy := from; yy <= top; yy++ {
		rr := 3
		if kind != 0 {
			rr = 1
			if yy < top {
				rr = 2
			}
		}
		for xx := x - rr; xx <= x+rr; xx++ {
			for zz := z - rr; zz <= z+rr; zz++ {
				edgeX, edgeZ := xx == x-rr || xx == x+rr, zz == z-rr || zz == z+rr
				if (kind == 0 || yy < top) && edgeX && edgeZ {
					continue
				}
				if !edgeX && !edgeZ && yy < top {
					continue
				}
				if n := d.block(xx, yy, zz); !mushSolid(n) {
					d.bput(pos{xx, yy, zz}, cap)
				}
			}
		}
	}
	for i := range h {
		if n := d.block(x, y+i, z); !mushSolid(n) {
			d.bput(pos{x, y + i, z}, sStem)
		}
	}
	return true
}

// ---------------------------------------------------------------- legacy trees

// legacyTree is LegacyTreeFeature (ctor 0x53fe1d0, Q1 table, T5): a count from the density, then per
// tree the chooser's draws, z then x within the chunk, y from the heightmap, and the chosen feature.
type legacyTree struct {
	density float32
	list    []bfeat
	choose  func(r *bmt) int
}

func (f *legacyTree) place(d *decoration, r *bmt, p pos) bool {
	var n int
	switch {
	case r.nextIntN(10) == 0:
		n = int(f.density * r.nextFloat())
	case f.density >= 1:
		n = max(7, int(r.nextIntN(10))) + int(f.density) - 7
	default:
		if r.nextFloat() < f.density {
			n = 1
		}
	}
	for range n {
		k := f.choose(r)
		z := p.z + int(r.nextIntN(14)) + 1
		x := p.x + int(r.nextIntN(14)) + 1
		y := d.bedrockHeight(x, z)
		f.list[k].place(d, r, pos{x, y, z})
	}
	return true
}

// legacyTreeTable is the ctor's cases by rule feature name: density, feature list and chooser.
var legacyTreeTable = map[string]struct {
	density float32
	names   string
	choose  func(r *bmt) int
}{
	"savanna_tree_feature":         {1, "savanna_tree_feature select_oak_tree_feature", chooseSavanna},
	"savanna_mutated_tree_feature": {2, "savanna_tree_feature select_oak_tree_feature", chooseSavanna},
	"plains_tree_feature": {0.05, "fancy_oak_tree_with_optional_beehive_feature select_oak_tree_feature oak_tree_with_beehive_feature",
		func(r *bmt) int { return b2i(r.nextIntN(3) != 0) }},
	"forest_tree_feature": {10, "select_birch_tree_with_leaf_litter_feature fancy_oak_tree_with_leaf_litter_and_optional_beehive select_oak_tree_with_leaf_litter_feature",
		chooseForest},
	"birch_forest_tree_feature": {10, "select_birch_tree_feature birch_tree_with_beehive_feature", func(*bmt) int { return 0 }},
	"birch_forest_mutated_tree_feature": {10, "select_birch_tree_feature select_super_birch_tree_feature", func(r *bmt) int {
		if r.nextIntN(5) == 0 {
			return 0
		}
		return b2i(r.nextBool())
	}},
	"taiga_tree_feature": {10, "pine_tree_feature select_spruce_tree_feature", func(r *bmt) int { return b2i(r.nextIntN(3) != 0) }},
	"flower_forest_tree_feature": {6, "select_birch_tree_feature fancy_oak_tree_with_optional_beehive_feature oak_tree_with_optional_beehive_feature",
		chooseForest},
	"meadow_trees_feature": {0.01, "fancy_oak_tree_with_beehive_feature super_birch_tree_with_beehive_feature", func(r *bmt) int { return b2i(!r.nextBool()) }},
	"extreme_hills_plus_trees_tree_feature": {3, "select_spruce_tree_feature fancy_oak_tree_feature select_oak_tree_feature", func(r *bmt) int {
		if r.nextIntN(3) >= 1 {
			return 0
		}
		if r.nextIntN(10) == 0 {
			return 1
		}
		return 2
	}},
	"ice_tree_feature":                {0.1, "select_spruce_tree_feature", func(*bmt) int { return 0 }},
	"mesa_plateau_stone_tree_feature": {5, "select_oak_tree_with_leaf_litter_feature", func(*bmt) int { return 0 }},
	"jungle_tree_feature": {50, "fancy_oak_tree_feature jungle_bush_feature mega_jungle_tree_feature select_jungle_tree_feature", func(r *bmt) int {
		switch {
		case r.nextIntN(10) == 0:
			return 0
		case r.nextIntN(2) == 0:
			return 1
		case r.nextIntN(3) == 0:
			return 2
		}
		return 3
	}},
	"jungle_edge_tree_feature": {2, "fancy_oak_tree_with_optional_beehive_feature jungle_bush_feature select_jungle_tree_feature", func(r *bmt) int {
		if r.nextIntN(10) == 0 {
			return 0
		}
		if r.nextIntN(2) == 0 {
			return 1
		}
		return 2
	}},
	// T17 (ctor cases 0 and 16): bamboo jungle's fourth entry is a grass scatter, not a tree.
	"bamboo_jungle_tree_feature": {30, "fancy_oak_tree_feature jungle_bush_feature mega_jungle_tree_feature scatter_tall_grass_around_tree_feature", func(r *bmt) int {
		switch {
		case r.nextIntN(20) == 0:
			return 0
		case r.nextIntN(20) <= 2:
			return 1
		case r.nextIntN(10) <= 6:
			return 2
		}
		return 3
	}},
	"redwood_taiga_mutated_tree_feature": {10, "mega_spruce_tree_feature pine_tree_feature select_spruce_tree_feature", func(r *bmt) int {
		if r.nextIntN(3) == 0 {
			return 0
		}
		if r.nextIntN(3) == 0 {
			return 1
		}
		return 2
	}},
	"mega_taiga_tree_feature": {10, "mega_spruce_tree_feature mega_pine_tree_feature pine_tree_feature select_spruce_tree_feature", func(r *bmt) int {
		if r.nextIntN(3) == 0 {
			if r.nextIntN(13) == 0 {
				return 0
			}
			return 1
		}
		if r.nextIntN(3) == 0 {
			return 2
		}
		return 3
	}},
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func chooseSavanna(r *bmt) int { return b2i(r.nextIntN(5) <= 0) }

func chooseForest(r *bmt) int {
	if r.nextIntN(5) == 0 {
		return 0
	}
	if r.nextIntN(10) == 0 {
		return 1
	}
	return 2
}

func (fc *featCompiler) legacyTreeFeature(name string) (bfeat, bool, error) {
	e, ok := legacyTreeTable[name]
	if !ok {
		return nil, false, nil
	}
	f := &legacyTree{density: e.density, choose: e.choose}
	for _, n := range strings.Fields(e.names) {
		c, err := fc.feature("minecraft:" + n)
		if err != nil {
			return nil, true, err
		}
		f.list = append(f.list, c)
	}
	return codeFeat(f.place), true, nil
}
