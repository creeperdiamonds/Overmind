package worldgen

import (
	"encoding/json"
	"math"
)

// Mega and cherry trees (re/phase4/trees/TREES-pseudocode.md sections 2 and 4), read from the code; the
// study has not checked them block for block yet.

// megaTrunk is MegaTreeTrunk (height 0xd8ac520, place 0xd8aaec0).
type megaTrunk struct {
	log              bs
	base             int
	intervals        []int
	width            bRange
	branchLen        int
	interval         bRange
	slope            float32
	altMin, altMax   float32
	branchCanopy     bCanopy
	deco             bDeco
}

func (fc *featCompiler) megaTrunk(v map[string]json.RawMessage, log bs, deco bDeco) (bTrunk, error) {
	t := &megaTrunk{log: log, deco: deco, width: bRange{2, 2, false}, slope: 0.5}
	var th struct {
		Base      int   `json:"base"`
		Intervals []int `json:"intervals"`
	}
	json.Unmarshal(v["trunk_height"], &th)
	t.base, t.intervals = th.Base, th.Intervals
	var err error
	if raw, ok := v["trunk_width"]; ok {
		if t.width, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	if raw, ok := v["branches"]; ok {
		var br map[string]json.RawMessage
		json.Unmarshal(raw, &br)
		json.Unmarshal(br["branch_length"], &t.branchLen)
		if t.interval, err = parseBRange(br["branch_interval"]); err != nil {
			return nil, err
		}
		if raw, ok := br["branch_slope"]; ok {
			json.Unmarshal(raw, &t.slope)
		}
		var alt struct{ Min, Max float32 }
		json.Unmarshal(br["branch_altitude_factor"], &alt)
		t.altMin, t.altMax = alt.Min, alt.Max
		if t.branchCanopy, err = fc.canopyOf(br["branch_canopy"]); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func (t *megaTrunk) height(r *bmt) int {
	v := t.base
	for _, n := range t.intervals {
		v += int(r.nextIntN(int32(n)))
	}
	return v
}

func (t *megaTrunk) place(d *decoration, c *treeCtx, r *bmt, p pos, h int, can bCanopy) bool {
	w := t.width.inclGT(r)
	x, y, z := p.x, p.y, p.z
	if !(minY < y && y+h <= maxY) {
		return false
	}
	if c.growOn != nil && !c.growOn.has(d.block(x, y-1, z)) {
		return false
	}
	if c.through != nil {
		for i := 0; i <= h+1; i++ {
			rr := w
			if i == 0 {
				rr = int(max(math.Abs(float64(w-1)), 1))
			}
			if y+i > maxY {
				return false
			}
			for dx := -rr; dx <= rr; dx++ {
				for dz := -rr; dz <= rr; dz++ {
					if !c.through.has(d.block(x+dx, y+i, z+dz)) {
						return false
					}
				}
			}
		}
	}
	if can != nil {
		can.place(d, c, r, pos{x, y + h, z}, nil, w)
	}
	if t.branchLen > 0 {
		fh := float32(h)
		yb := y - t.interval.excl(r) + int(t.altMax*fh)
		for y+int(t.altMin*fh) < yb {
			f := r.nextFloat()
			ang := float32(f*bedrockPi+f*bedrockPi) * 10430.378
			si, co := bedrockSin[int32(ang)&0xffff], bedrockSin[int32(ang+16384)&0xffff]
			base := float32(int(float32(yb-1) - float32(t.branchLen-1)*t.slope))
			bx, bz := x+1, z+1
			for k := range t.branchLen {
				bx = x + int(co*float32(k)+1.5)
				bz = z + int(si*float32(k)+1.5)
				by := int(float32(k)*t.slope + base)
				if by >= minY && by <= maxY {
					d.bput(pos{bx, by, bz}, t.log)
				}
			}
			if t.branchCanopy != nil {
				t.branchCanopy.place(d, c, r, pos{bx, yb, bz}, nil, 1)
			}
			yb -= t.interval.excl(r)
		}
	}
	list := func(q pos) bool { return q.y >= minY && q.y <= maxY && (c.replace == nil || c.replace.has(d.at(q))) }
	for dx := range w {
		for dz := range w {
			if q := (pos{x + dx, y, z + dz}); list(q) {
				d.bput(q, t.log)
			}
		}
	}
	for dx := range w {
		for yy := y + 1; yy <= y+h-2; yy++ {
			for dz := range w {
				q := pos{x + dx, yy, z + dz}
				if !list(q) || !d.bput(q, t.log) {
					continue
				}
				var sides [6]bool
				sides[0] = dx == 0
				sides[1] = dx != 0 && dx == w-1
				sides[2] = dz == 0
				sides[3] = dz != 0 && dz == w-1
				t.deco.place(d, r, q, sides)
			}
		}
	}
	if q := (pos{x, y + h - 1, z}); list(q) {
		d.bput(q, t.log)
	}
	if len(c.base) > 0 {
		for dx := range w {
			for dz := range w {
				c.setBase(d, pos{x + dx, y - 1, z + dz})
			}
		}
	}
	if c.clusterN > 0 && len(c.base) > 0 {
		rc := c.clusterR
		column := func(cx, cz int) {
			for _, k := range [6]int{2, 1, 0, -1, -2, -3} {
				q := pos{cx, y - 1 + k, cz}
				b := d.at(q)
				if b.block() == c.base[0].block() {
					return
				}
				if c.clusterReplace.has(b) {
					d.bput(q, c.base[0])
					return
				}
			}
		}
		circle := func(cx, cz int) {
			for dx := -rc; dx <= rc; dx++ {
				for dz := -rc; dz <= rc; dz++ {
					if absInt(dx) == rc && absInt(dz) == rc {
						continue
					}
					column(cx+dx, cz+dz)
				}
			}
		}
		for _, cc := range [4][2]int{{x - 1, z - 1}, {x + w, z - 1}, {x - 1, z + w}, {x + w, z + w}} {
			circle(cc[0], cc[1])
		}
		for range c.clusterN {
			v := int(r.nextIntN(64))
			i, j := v%8, v/8
			if j == 7 || j == 0 || i == 0 || i == 7 {
				circle(x-3+i, z-3+j)
			}
		}
	}
	return true
}

// ring is the ring helper (0xd8aa9f0): Java 1.12's leaf layer, strict unless simple.
func ring(d *decoration, t *treeCtx, p pos, leaf bs, r, wx, wz int, simple bool) {
	if p.y < minY || p.y > maxY {
		return
	}
	r2 := r * r
	for dx := -r; dx <= wx+r-1; dx++ {
		for dz := -r; dz <= wz+r-1; dz++ {
			keep := dx*dx+dz*dz <= r2
			if !simple {
				keep = keep || (dx-1)*(dx-1)+(dz-1)*(dz-1) <= r2 || dx*dx+(dz-1)*(dz-1) <= r2 || (dx-1)*(dx-1)+dz*dz <= r2
			}
			if !keep {
				continue
			}
			q := pos{p.x + dx, p.y, p.z + dz}
			if t.canReplace(d.at(q)) {
				d.bput(q, leaf)
			}
		}
	}
}

// megaCanopy is MegaTreeCanopy (0xd8aad40).
type megaCanopy struct {
	leaf           bs
	h              bRange
	base, core     int
	useCore, simple bool
}

func megaCanopyOf(v map[string]json.RawMessage) (bCanopy, error) {
	leaf, _, err := blockState(v["leaf_block"])
	if err != nil {
		return nil, err
	}
	c := &megaCanopy{leaf: leaf, h: bRange{3, 3, false}, base: 2, core: 1}
	if raw, ok := v["canopy_height"]; ok {
		if c.h, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	json.Unmarshal(v["base_radius"], &c.base)
	if raw, ok := v["core_width"]; ok {
		json.Unmarshal(raw, &c.core)
		c.useCore = true
	}
	json.Unmarshal(v["simplify_canopy"], &c.simple)
	return c, nil
}

func (c *megaCanopy) place(d *decoration, t *treeCtx, r *bmt, p pos, _ []pos, width int) bool {
	n := c.h.excl(r)
	w := width
	if c.useCore {
		w = c.core
	}
	for i := 1; i <= n; i++ {
		ring(d, t, pos{p.x, p.y - n + i, p.z}, c.leaf, c.core/2+c.base+(n-i), w, w, c.simple)
	}
	return true
}

// megaPineCanopy is MegaPineTreeCanopy (0xd8aa7e0).
type megaPineCanopy struct {
	leaf       bs
	h          bRange
	base, core int
	useCore    bool
	step       float32
}

func megaPineCanopyOf(v map[string]json.RawMessage) (bCanopy, error) {
	leaf, _, err := blockState(v["leaf_block"])
	if err != nil {
		return nil, err
	}
	c := &megaPineCanopy{leaf: leaf, h: bRange{3, 8, true}, base: 2, step: 3.5, core: 1}
	if raw, ok := v["canopy_height"]; ok {
		if c.h, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	json.Unmarshal(v["base_radius"], &c.base)
	if raw, ok := v["radius_step_modifier"]; ok {
		json.Unmarshal(raw, &c.step)
	}
	if raw, ok := v["core_width"]; ok {
		json.Unmarshal(raw, &c.core)
		c.useCore = true
	}
	return c, nil
}

func (c *megaPineCanopy) place(d *decoration, t *treeCtx, r *bmt, p pos, _ []pos, width int) bool {
	n := c.h.excl(r)
	w := width
	if c.useCore {
		w = c.core
	}
	prev := 0
	for dy := -n; dy <= 0; dy++ {
		rel := -dy
		yy := p.y + dy
		rad := int(math.Floor(float64(float32(float32(rel)/float32(n))*c.step))) + c.base
		bump := 0
		if rel >= 1 && rad == prev && yy&1 == 0 {
			bump = 1
		}
		ring(d, t, pos{p.x, yy, p.z}, c.leaf, rad+bump, w, w, false)
		prev = rad
	}
	return true
}

// bCherryTrunk is CherryTreeTrunk (height 0xd8fe680, place 0xd8fd6e0, branch 0xd8fe0d0).
type bCherryTrunk struct {
	log                    bs
	base                   int
	intervals              []int
	weights                [3]float32
	length, start, end     bRange
	branchCanopy           bCanopy
}

func (fc *featCompiler) bCherryTrunk(v map[string]json.RawMessage, log bs) (bTrunk, error) {
	t := &bCherryTrunk{log: log}
	var th struct {
		Base      int   `json:"base"`
		Intervals []int `json:"intervals"`
	}
	json.Unmarshal(v["trunk_height"], &th)
	t.base, t.intervals = th.Base, th.Intervals
	var br map[string]json.RawMessage
	json.Unmarshal(v["branches"], &br)
	var w struct {
		One  float32 `json:"one_branch"`
		Two  float32 `json:"two_branches"`
		TwoT float32 `json:"two_branches_and_trunk"`
	}
	json.Unmarshal(br["tree_type_weights"], &w)
	t.weights = [3]float32{w.One, w.Two, w.TwoT}
	var err error
	if t.length, err = parseBRange(br["branch_horizontal_length"]); err != nil {
		return nil, err
	}
	if t.start, err = parseBRange(br["branch_start_offset_from_top"]); err != nil {
		return nil, err
	}
	if t.end, err = parseBRange(br["branch_end_offset_from_top"]); err != nil {
		return nil, err
	}
	if t.branchCanopy, err = fc.canopyOf(br["branch_canopy"]); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *bCherryTrunk) height(r *bmt) int {
	v := t.base
	for _, n := range t.intervals {
		if n >= 0 {
			v += int(r.nextIntN(int32(n + 1)))
		}
	}
	return v
}

func (t *bCherryTrunk) place(d *decoration, c *treeCtx, r *bmt, p pos, h int, can bCanopy) bool {
	if !c.prepare(d, p, h) || !(p.y < maxY+1-h-1) {
		return false
	}
	c.setBase(d, p.add(0, -1, 0))
	idx := weightedPick(r, t.weights[:])
	a := t.start.inclGT(r)
	i := max(0, a+h-1)
	b := t.start.excl(r)
	j := max(0, b+h-1)
	dn := int(r.nextIntN(4))
	dx, dz := bedrockDX[dn], bedrockDZ[dn]
	axis := "z"
	if dx != 0 {
		axis = "x"
	}
	branchLog := stateOf(replaceAxis(t.log.name(), axis))
	if a+h-1 <= j {
		j++
	}
	n := i + 1
	switch {
	case idx == 2:
		n = h
	case idx > 0:
		n = max(i, j) + 1
	}
	ok := func(q pos) bool { return q.y >= minY && q.y <= maxY && c.logOK(d.at(q)) }
	for k := range n {
		if q := p.add(0, k, 0); ok(q) {
			d.bput(q, t.log)
		}
	}
	can2 := t.branchCanopy
	if can2 == nil {
		can2 = can
	}
	branch := func(ddx, ddz, s int, mid bool) pos {
		m := pos{p.x, p.y + s, p.z}
		e := t.end.inclGT(r)
		k := h - 1 + e
		bl2 := mid || k < s
		l := t.length.inclGT(r)
		reps := 1
		if bl2 {
			reps = 2
		}
		for range reps {
			m.x += ddx
			m.z += ddz
			if ok(m) {
				d.bput(m, branchLog)
			}
		}
		tg := pos{p.x + ddx*(l+b2i(bl2)), p.y + k, p.z + ddz*(l+b2i(bl2))}
		vy := -1
		if tg.y > m.y {
			vy = 1
		}
		for m != tg {
			man := absInt(tg.x-m.x) + absInt(tg.y-m.y) + absInt(tg.z-m.z)
			f := float32(absInt(tg.y-m.y)) / float32(man)
			if r.nextFloat() < f {
				m.y += vy
				if ok(m) {
					d.bput(m, t.log)
				}
			} else {
				m.x += ddx
				m.z += ddz
				if ok(m) {
					d.bput(m, branchLog)
				}
			}
		}
		return tg.add(0, 1, 0)
	}
	var tops []pos
	if idx == 2 {
		tops = append(tops, p.add(0, h, 0))
	}
	tops = append(tops, branch(dx, dz, i, i < n-1))
	if idx > 0 {
		tops = append(tops, branch(-dx, -dz, j, j < n-1))
	}
	for _, tp := range tops {
		if can2 != nil {
			can2.place(d, c, r, tp, nil, 1)
		}
	}
	return true
}

func replaceAxis(name, axis string) string {
	for _, a := range [3]string{"axis=x", "axis=y", "axis=z"} {
		if i := indexOf(name, a); i >= 0 {
			return name[:i] + "axis=" + axis + name[i+len(a):]
		}
	}
	return name
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// cherryCanopy is CherryTreeCanopy (0xd8fcd20), width C = 1.
type cherryCanopy struct {
	leaf                            bs
	radius, height                  bRange
	wide, corner, hanging, extension bChance
}

func cherryCanopyOf(v map[string]json.RawMessage) (bCanopy, error) {
	leaf, _, err := blockState(v["leaf_block"])
	if err != nil {
		return nil, err
	}
	c := &cherryCanopy{leaf: leaf, radius: bRange{4, 4, false}, height: bRange{5, 5, false}}
	if raw, ok := v["radius"]; ok {
		if c.radius, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	if raw, ok := v["height"]; ok {
		if c.height, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	for k, ch := range map[string]*bChance{"wide_bottom_layer_hole_chance": &c.wide, "corner_hole_chance": &c.corner,
		"hanging_leaves_chance": &c.hanging, "hanging_leaves_extension_chance": &c.extension} {
		if *ch, err = parseBChance(v[k]); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *cherryCanopy) place(d *decoration, t *treeCtx, r *bmt, p pos, _ []pos, _ int) bool {
	const C = 1
	a := c.radius.inclGT(r)
	b := c.height.inclGT(r)
	list := func(q pos) bool { return q.y >= minY && q.y <= maxY && t.canReplace(d.at(q)) }
	row := func(dy, rad int) {
		for dx := -rad; dx < rad+C; dx++ {
			for dz := -rad; dz < rad+C; dz++ {
				ax, az := absInt(dx), absInt(dz)
				if dy == -1 && (ax == rad || az == rad) && c.wide.test(r) {
					continue
				}
				corner := ax == rad && az == rad
				if rad >= 3 {
					if corner {
						continue
					}
					if ax+az > 2*rad-2 && c.corner.test(r) {
						continue
					}
				} else if corner && c.corner.test(r) {
					continue
				}
				if q := (pos{p.x + dx, p.y + dy, p.z + dz}); list(q) {
					d.bput(q, c.leaf)
				}
			}
		}
	}
	hang := func(dy, rad int) {
		row(dy, rad)
		lb := c.leaf.block()
		for dn := range 4 {
			e := [4]int{3, 0, 1, 2}[dn]
			off := rad
			if dn < 2 {
				off = rad + C - 1
			}
			for tt := -rad; tt < rad+C-1; tt++ {
				cx := p.x + bedrockDX[e]*off + bedrockDX[dn]*tt
				cz := p.z + bedrockDZ[e]*off + bedrockDZ[dn]*tt
				if d.block(cx, p.y+dy, cz).block() != lb {
					continue
				}
				if !c.hanging.test(r) {
					continue
				}
				q := pos{cx, p.y + dy - 1, cz}
				if !list(q) || !d.bput(q, c.leaf) {
					continue
				}
				if c.extension.test(r) {
					if q2 := q.add(0, -1, 0); list(q2) {
						d.bput(q2, c.leaf)
					}
				}
			}
		}
	}
	row(b-3, C+a-4)
	row(b-4, C+a-3)
	if b > 4 {
		for k := b - 5; k >= 0; k-- {
			row(k, C+a-2)
		}
	}
	hang(-1, C+a-2)
	hang(-2, C+a-3)
	return true
}
