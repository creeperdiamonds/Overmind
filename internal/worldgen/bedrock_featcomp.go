package worldgen

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// The composite features of Bedrock's feature files (NOTES-bedrock-exact Q1 "Composite features",
// section 3) and single_block_feature.

// feature builds a feature by identifier.
func (fc *featCompiler) feature(id string) (bfeat, error) {
	if s, ok := fc.slots[id]; ok {
		if s.on {
			// Only broken data names a feature from inside itself: vanilla has no cycles, and a cycle
			// would recurse without end at the first placement.
			return nil, fmt.Errorf("feature %s names itself through its parts", id)
		}
		return s.f, s.err
	}
	s := &featSlot{on: true}
	fc.slots[id] = s
	s.f, s.err = fc.build(id)
	s.on = false
	return s.f, s.err
}

func (fc *featCompiler) build(id string) (bfeat, error) {
	fc.bd.lastOre = nil
	if f, ok, err := fc.codeFeature(id); ok {
		if fc.bd.lastOre != nil {
			fc.ores[id] = fc.bd.lastOre
		}
		return f, err
	}
	pf, ok := fc.bd.features[id]
	if !ok {
		return nil, unimpl("unknown feature %s", id)
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal(pf.body, &v); err != nil {
		return nil, fmt.Errorf("%s: %w", id, err)
	}
	if err := checkDataNumbers(pf.body); err != nil {
		return nil, fmt.Errorf("%s: %w", id, err)
	}
	var f bfeat
	var err error
	switch pf.kind {
	case "minecraft:ore_feature":
		place, why := fc.bd.oreFeature(pf.body)
		if place == nil {
			return nil, fmt.Errorf("ore: %s", why)
		}
		f = voidFeat(place)
		fc.ores[id] = fc.bd.lastOre
	case "minecraft:aggregate_feature":
		f, err = fc.aggregate(v)
	case "minecraft:sequence_feature":
		f, err = fc.sequence(v)
	case "minecraft:weighted_random_feature":
		f, err = fc.weighted(v)
	case "minecraft:scatter_feature":
		f, err = fc.scatterFeature(v)
	case "minecraft:single_block_feature":
		f, err = fc.singleBlock(v)
	case "minecraft:search_feature":
		f, err = fc.search(v)
	case "minecraft:snap_to_surface_feature":
		f, err = fc.snap(v)
	case "minecraft:vegetation_patch_feature":
		f, err = fc.vegetationPatch(v)
	case "minecraft:growing_plant_feature":
		f, err = fc.growingPlant(v)
	case "minecraft:geode_feature":
		f, err = fc.geode(v)
	case "minecraft:tree_feature":
		f, err = fc.tree(v)
	case "minecraft:fossil_feature":
		f, err = fc.fossil(v)
	default:
		return nil, unimpl("%s", pf.kind)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", id, err)
	}
	return f, nil
}

// child builds a feature named in another's body.
func (fc *featCompiler) child(raw json.RawMessage) (bfeat, error) {
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil, fmt.Errorf("feature name %s", raw)
	}
	return fc.feature(id)
}

// aggregateFeat runs its features in order on one position (0xd9cc6c0): first_failure stops at the
// first that fails, first_success at the first that places; it placed if any did.
type aggregateFeat struct {
	list                      []bfeat
	stopOnFail, stopOnSuccess bool
}

func (f *aggregateFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	ok := false
	for _, c := range f.list {
		_, k := c.place(d, r, p)
		if k {
			ok = true
			if f.stopOnSuccess {
				return p, true
			}
		} else if f.stopOnFail {
			return p, ok
		}
	}
	return p, ok
}

func (fc *featCompiler) list(raw json.RawMessage) ([]bfeat, error) {
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	var out []bfeat
	for _, e := range list {
		c, err := fc.child(e)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (fc *featCompiler) aggregate(v map[string]json.RawMessage) (bfeat, error) {
	f := &aggregateFeat{}
	var eo string
	json.Unmarshal(v["early_out"], &eo)
	switch eo {
	case "", "none":
	case "first_failure":
		f.stopOnFail = true
	case "first_success":
		f.stopOnSuccess = true
	default:
		return nil, unimpl("early_out %s", eo)
	}
	var err error
	f.list, err = fc.list(v["features"])
	return f, err
}

// sequenceFeat gives each feature the position the one before ended at, and stops at a failure.
type sequenceFeat struct{ list []bfeat }

func (f *sequenceFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	for _, c := range f.list {
		q, ok := c.place(d, r, p)
		if !ok {
			return p, false
		}
		p = q
	}
	return p, true
}

func (fc *featCompiler) sequence(v map[string]json.RawMessage) (bfeat, error) {
	l, err := fc.list(v["features"])
	return &sequenceFeat{l}, err
}

// weightedFeat picks one feature (0xd8f8cb0).
type weightedFeat struct {
	list []bfeat
	w    []float32
}

// weightedPick is Bedrock's weighted pick: the total is the float sum of the weights cast to int,
// r = nextInt(total) with no draw for 0, and the weights are taken off r until it goes negative.
// -1 when none is picked.
func weightedPick(r *bmt, w []float32) int {
	var tot int32
	for _, x := range w {
		tot = int32(float32(tot) + x)
	}
	var k int32
	if tot != 0 {
		k = r.nextIntN(tot)
	}
	for i, x := range w {
		k = int32(float32(k) - x)
		if k < 0 {
			return i
		}
	}
	return -1
}

func (f *weightedFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	if i := weightedPick(r, f.w); i >= 0 {
		return f.list[i].place(d, r, p)
	}
	return p, false
}

func (fc *featCompiler) weighted(v map[string]json.RawMessage) (bfeat, error) {
	var list [][2]json.RawMessage
	if err := json.Unmarshal(v["features"], &list); err != nil {
		return nil, err
	}
	f := &weightedFeat{}
	for _, e := range list {
		c, err := fc.child(e[0])
		if err != nil {
			return nil, err
		}
		var w float32
		if err := json.Unmarshal(e[1], &w); err != nil {
			return nil, err
		}
		f.list, f.w = append(f.list, c), append(f.w, w)
	}
	return f, nil
}

// searchFeat tries its feature along the search volume until enough succeed (the study's reading for
// the beehive search: the +y axis over a one-column volume, from the bottom up; approximate).
type searchFeat struct {
	f        bfeat
	min, max [3]int
	need     int
}

// place tries the volume along +y; across x and z (the creaking heart's 3 x 3 columns) the order is not
// read: y outer, then x, then z, the study's approximation (T17). Every try costs one placement of the
// source's budget, so no volume the limits allow can stall a worker.
func (f *searchFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	got := 0
	for dy := f.min[1]; dy <= f.max[1]; dy++ {
		for dx := f.min[0]; dx <= f.max[0]; dx++ {
			for dz := f.min[2]; dz <= f.max[2]; dz++ {
				if !d.spend() {
					return p, false
				}
				if _, ok := f.f.place(d, r, p.add(dx, dy, dz)); ok {
					got++
					if got >= f.need {
						return p, true
					}
				}
			}
		}
	}
	return p, false
}

func (fc *featCompiler) search(v map[string]json.RawMessage) (bfeat, error) {
	var vol struct {
		Min [3]int `json:"min"`
		Max [3]int `json:"max"`
	}
	var axis string
	var need int
	json.Unmarshal(v["search_volume"], &vol)
	json.Unmarshal(v["search_axis"], &axis)
	json.Unmarshal(v["required_successes"], &need)
	if axis != "+y" || vol.Min[0] > vol.Max[0] || vol.Min[1] > vol.Max[1] || vol.Min[2] > vol.Max[2] {
		return nil, unimpl("search %s %v", axis, vol)
	}
	for _, a := range [2]int{0, 2} {
		if vol.Min[a] < -maxDataRadius || vol.Max[a] > maxDataRadius {
			// Across x and z a search is a radius (vanilla's widest is the creaking heart's 3 x 3).
			return nil, fmt.Errorf("search volume %v..%v is wider than %d", vol.Min, vol.Max, maxDataRadius)
		}
	}
	c, err := fc.child(v["places_feature"])
	if err != nil {
		return nil, err
	}
	return &searchFeat{c, vol.Min, vol.Max, max(need, 1)}, nil
}

// bCoord is one coordinate of a scatter: a number, a distribution over an extent (numbers or
// expressions), or an expression giving the coordinate itself.
type bCoord struct {
	kind     bAxisKind
	lo, hi   int
	step     int // fixed_grid and jittered_grid: step_size (default 1) and grid_offset.
	offset   int
	loE, hiE mexpr
	expr     mexpr
}

// bScatter is ScatterParams (section 3): the chance, the iterations and the coordinates.
type bScatter struct {
	iterations int
	itExpr     mexpr
	num, den   int     // scatter_chance as a fraction; den 0 for none.
	percent    float32 // scatter_chance as a percentage, with hasPercent.
	hasPercent bool
	chanceExpr mexpr
	order      [3]int
	axes       [3]bCoord
}

// molangRound turns an expression's value into a block coordinate or a count.
func molangRound(v float32) int { return int(math.RoundToEven(float64(v))) }

// chance draws the scatter chance and says whether the scatter goes on.
func (sc *bScatter) chance(r *bmt, e *molangEnv) bool {
	if sc.den > 0 && sc.num != sc.den && int(r.nextIntN(int32(sc.den))) >= sc.num {
		return false
	}
	pct, has := sc.percent, sc.hasPercent
	if sc.chanceExpr != nil {
		pct, has = sc.chanceExpr(e), true
	}
	if has && pct < 100 && (pct <= 0 || float32(r.nextFloat()*100) >= pct) {
		return false
	}
	return true
}

// maxIterations bounds a scatter's iterations (vanilla's largest is a few hundred).
const maxIterations = 4096

// isAxisOrder says whether s orders x, y and z, each once.
func isAxisOrder(s string) bool {
	return len(s) == 3 && strings.Count(s, "x") == 1 && strings.Count(s, "y") == 1 && strings.Count(s, "z") == 1
}

func (sc *bScatter) count(e *molangEnv) int {
	if sc.itExpr != nil {
		return min(max(molangRound(sc.itExpr(e)), 0), maxIterations)
	}
	return sc.iterations
}

// position draws one position from the base, coordinate by coordinate in the evaluation order; an
// expression gives the coordinate itself, a number or distribution is added to the base.
// idx is the iteration index (counting down): the grid distributions' state starts from it and is
// carried from axis to axis in the evaluation order (T31).
func (sc *bScatter) position(r *bmt, e *molangEnv, base [3]int, idx int) [3]int {
	p := base
	grid := uint32(idx)
	for _, a := range sc.order {
		c := &sc.axes[a]
		if c.expr != nil {
			p[a] = molangRound(c.expr(e))
		} else if c.kind == bFixedGrid || c.kind == bJitteredGrid {
			lo, hi := c.lo, c.hi
			if c.loE != nil {
				lo = molangRound(c.loE(e))
			}
			if c.hiE != nil {
				hi = molangRound(c.hiE(e))
			}
			p[a] += c.gridEval(r, lo, hi, &grid)
		} else {
			ax := bAxis{c.kind, c.lo, c.hi}
			if c.loE != nil {
				ax.lo = molangRound(c.loE(e))
			}
			if c.hiE != nil {
				ax.hi = molangRound(c.hiE(e))
			}
			p[a] += ax.eval(r)
		}
		switch a {
		case 0:
			e.worldX = float32(p[0])
		case 1:
			e.worldY = float32(p[1])
		default:
			e.worldZ = float32(p[2])
		}
	}
	return p
}

// gridEval is CoordRange_eval's fixed_grid (case 4) and jittered_grid (case 5), NOTES-bedrock-exact T31:
// value = (min + offset + k*step) % range with C's signed remainder, not shifted by min again; the state
// carried to the next axis is (k*step + offset) / range, unsigned. The jittered grid adds nextInt(step)
// when the step is above 1.
func (c *bCoord) gridEval(r *bmt, lo, hi int, state *uint32) int {
	rng := int32(hi - lo + 1)
	if rng <= 0 {
		return lo
	}
	k := int32(*state)
	step := int32(c.step)
	v := (int32(lo) + int32(c.offset) + k*step) % rng
	*state = uint32(k*step+int32(c.offset)) / uint32(rng)
	if c.kind == bJitteredGrid && step > 1 {
		v += r.nextIntN(step)
	}
	return int(v)
}

// parseScatterParams reads a distribution (section 3): iterations, scatter chance, coordinate order
// (default xzy, S3) and the three coordinates. Older files keep them in a "distribution" object.
func parseScatterParams(d map[string]json.RawMessage, bd *BedrockData) (*bScatter, error) {
	if raw, ok := d["distribution"]; ok {
		var inner map[string]json.RawMessage
		if json.Unmarshal(raw, &inner) == nil {
			m := map[string]json.RawMessage{}
			for k, v := range d {
				m[k] = v
			}
			for k, v := range inner {
				m[k] = v
			}
			d = m
		}
	}
	sc := &bScatter{order: evalOrder("xzy"), iterations: 1}
	value := func(raw json.RawMessage) (f mexpr, n float64, isNum bool, err error) {
		if json.Unmarshal(raw, &n) == nil {
			return nil, n, true, nil
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, 0, false, fmt.Errorf("value %s", raw)
		}
		f, err = parseMolang(s, bd)
		return f, 0, false, err
	}
	if raw, ok := d["iterations"]; ok {
		f, n, isNum, err := value(raw)
		if err != nil {
			return nil, err
		}
		if isNum {
			if n < 0 || n > maxIterations {
				return nil, fmt.Errorf("iterations %v out of range", n)
			}
			sc.iterations = int(math.Round(n))
		} else {
			sc.itExpr = f
		}
	}
	if raw, ok := d["scatter_chance"]; ok {
		var frac struct{ Numerator, Denominator int }
		if strings.Contains(string(raw), "numerator") {
			if err := json.Unmarshal(raw, &frac); err != nil {
				return nil, err
			}
			if frac.Denominator > 0 {
				sc.num, sc.den = frac.Numerator, frac.Denominator
			}
		} else {
			f, n, isNum, err := value(raw)
			if err != nil {
				return nil, err
			}
			if isNum {
				sc.percent, sc.hasPercent = float32(n), true
			} else {
				sc.chanceExpr = f
			}
		}
	}
	if raw, ok := d["coordinate_eval_order"]; ok {
		var s string
		if json.Unmarshal(raw, &s) != nil || !isAxisOrder(s) {
			return nil, fmt.Errorf("coordinate_eval_order %s is not an order of x, y and z", raw)
		}
		sc.order = evalOrder(s)
	}
	if raw, ok := d["project_input_to_floor"]; ok {
		var b bool
		json.Unmarshal(raw, &b)
		if b {
			return nil, unimpl("project_input_to_floor")
		}
	}
	for i, k := range []string{"x", "y", "z"} {
		raw, ok := d[k]
		if !ok {
			continue // Absent: 0.
		}
		if f, n, isNum, err := value(raw); err == nil {
			if isNum {
				v := int(math.Round(n))
				sc.axes[i] = bCoord{kind: bSingle, lo: v, hi: v}
			} else {
				sc.axes[i] = bCoord{expr: f}
			}
			continue
		} else if !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
			return nil, err
		}
		var dist struct {
			Distribution string             `json:"distribution"`
			Extent       [2]json.RawMessage `json:"extent"`
			Step         *int               `json:"step_size"`
			Offset       int                `json:"grid_offset"`
		}
		if err := json.Unmarshal(raw, &dist); err != nil {
			return nil, fmt.Errorf("coordinate %s", raw)
		}
		kind, ok := map[string]bAxisKind{"": bUniform, "uniform": bUniform, "gaussian": bGaussian,
			"inverse_gaussian": bInverseGaussian, "triangle": bTriangle, "fixed_grid": bFixedGrid,
			"jittered_grid": bJitteredGrid}[dist.Distribution]
		if !ok {
			return nil, unimpl("distribution %s", dist.Distribution)
		}
		c := bCoord{kind: kind, step: 1, offset: dist.Offset}
		if dist.Step != nil {
			if *dist.Step < 1 || *dist.Step > 1<<16 {
				return nil, fmt.Errorf("step_size %d out of range", *dist.Step)
			}
			c.step = *dist.Step
		}
		for j, raw := range dist.Extent {
			f, n, isNum, err := value(raw)
			if err != nil {
				return nil, err
			}
			switch {
			case isNum && j == 0:
				c.lo = int(math.Round(n))
			case isNum:
				c.hi = int(math.Round(n))
			case j == 0:
				c.loE = f
			default:
				c.hiE = f
			}
		}
		sc.axes[i] = c
	}
	return sc, nil
}

// scatterFeat is scatter_feature (0xd8d2910): the rule scatter on the caller's generator from the input
// position, with no height or biome test.
type scatterFeat struct {
	sc *bScatter
	f  bfeat
}

func (f *scatterFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	// The copied context shares the run's variable map (0x5525c10 copies its pointer), and
	// MolangCtx_init moves variable.origin* to the input position for this and every later rule (T3b).
	e := &d.menv
	e.originX, e.originY, e.originZ = float32(p.x), float32(p.y), float32(p.z)
	e.worldX, e.worldY, e.worldZ = float32(p.x), float32(p.y), float32(p.z)
	// The copy's math.random draws from the feature's Random (T14, T21: MolangCtx_init with it).
	saved := e.r1
	e.r1 = r
	defer func() { e.r1 = saved }()
	if !f.sc.chance(r, e) {
		return p, false
	}
	n := f.sc.count(e)
	ok, last := false, p
	for i := n - 1; i >= 0 && d.spend(); i-- {
		q := f.sc.position(r, e, [3]int{p.x, p.y, p.z}, i)
		if at, k := f.f.place(d, r, pos{q[0], q[1], q[2]}); k {
			ok, last = true, at
		}
	}
	return last, ok
}

func (fc *featCompiler) scatterFeature(v map[string]json.RawMessage) (bfeat, error) {
	sc, err := parseScatterParams(v, fc.bd)
	if err != nil {
		return nil, err
	}
	c, err := fc.child(v["places_feature"])
	if err != nil {
		return nil, err
	}
	return &scatterFeat{sc, c}, nil
}

// singleBlockFeat is single_block_feature (0xd8e03b0): the attach tests first, then ONE draw of the
// weighted pick even for one block, then may_replace, survival and placement.
type singleBlockFeat struct {
	blocks     []bs
	names      []uint16
	w          []float32
	mayReplace *bmatch // nil: air only.
	survive    bool
	placement  bool
	attach     [6]*bmatch // down, up, north, south, west, east
	attachMin  int
	notAttach  [6]*bmatch
}

var faceKeys = map[string]int{"bottom": 0, "top": 1, "north": 2, "south": 3, "west": 4, "east": 5}

func (f *singleBlockFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	if f.attachMin > 0 {
		n := 0
		for i, m := range f.attach {
			if m != nil && m.has(d.at(p.step(dir(i)))) {
				n++
			}
		}
		if n < f.attachMin {
			return p, false
		}
	}
	for i, m := range f.notAttach {
		if m != nil && m.has(d.at(p.step(dir(i)))) {
			return p, false
		}
	}
	k := weightedPick(r, f.w)
	if k < 0 || p.y < minY || p.y > maxY {
		return p, false
	}
	cur := d.at(p)
	if f.mayReplace != nil {
		if !f.mayReplace.has(cur) {
			return p, false
		}
	} else if !cur.isAir() {
		return p, false
	}
	name := f.names[k]
	if f.survive && !bedrockSurvives(d, name, p) {
		return p, false
	}
	if f.placement && !bedrockMayPlace(d, name, p) {
		return p, false
	}
	b := f.blocks[k]
	if bname(b) == bnCocoa {
		// Cocoa faces the log it hangs on (T31: Bedrock's direction state, the side of the log).
		for _, a := range [4]dir{dNorth, dSouth, dWest, dEast} {
			if d.at(p.step(a)).tagged(tagLogs) {
				b = cocoaFacing(b, a)
				break
			}
		}
	}
	return p, d.bput(p, b)
}

var bnCocoa = internBName("cocoa")

// cocoaFacing is a cocoa state of the same age facing a.
func cocoaFacing(b bs, a dir) bs {
	for age := range 3 {
		if b == stateOf(fmt.Sprintf("cocoa[age=%d,facing=north]", age)) || b == stateOf(fmt.Sprintf("cocoa[age=%d,facing=south]", age)) ||
			b == stateOf(fmt.Sprintf("cocoa[age=%d,facing=west]", age)) || b == stateOf(fmt.Sprintf("cocoa[age=%d,facing=east]", age)) {
			return stateOf(fmt.Sprintf("cocoa[age=%d,facing=%s]", age, dirName(a)))
		}
	}
	return b
}

func (fc *featCompiler) singleBlock(v map[string]json.RawMessage) (bfeat, error) {
	f := &singleBlockFeat{}
	raw := json.RawMessage(strings.TrimSpace(string(v["places_block"])))
	type entry struct {
		b bdesc
		w float32
	}
	var entries []entry
	if len(raw) > 0 && raw[0] == '[' {
		var list []json.RawMessage
		json.Unmarshal(raw, &list)
		for _, e := range list {
			var pair []json.RawMessage
			var obj struct {
				Block  json.RawMessage `json:"block"`
				Weight float32         `json:"weight"`
			}
			switch {
			case json.Unmarshal(e, &pair) == nil && len(pair) == 2:
				b, err := parseBDesc(pair[0])
				if err != nil {
					return nil, err
				}
				var w float32
				json.Unmarshal(pair[1], &w)
				entries = append(entries, entry{b, w})
			case json.Unmarshal(e, &obj) == nil && obj.Block != nil:
				b, err := parseBDesc(obj.Block)
				if err != nil {
					return nil, err
				}
				entries = append(entries, entry{b, obj.Weight})
			default:
				b, err := parseBDesc(e)
				if err != nil {
					return nil, err
				}
				entries = append(entries, entry{b, 1})
			}
		}
	} else {
		b, err := parseBDesc(raw)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{b, 1})
	}
	for _, e := range entries {
		s, err := javaStateFor(e.b)
		if err != nil {
			return nil, unimpl("%v", err)
		}
		f.blocks, f.names, f.w = append(f.blocks, s), append(f.names, internBName(e.b.name)), append(f.w, e.w)
	}
	if raw, ok := v["may_replace"]; ok {
		m, err := parseBMatch(raw)
		if err != nil {
			return nil, err
		}
		f.mayReplace = m
	}
	json.Unmarshal(v["enforce_survivability_rules"], &f.survive)
	json.Unmarshal(v["enforce_placement_rules"], &f.placement)
	faces := func(raw json.RawMessage, into *[6]*bmatch) error {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		for k, val := range m {
			if k == "min_sides_must_attach" || k == "auto_rotate" {
				continue
			}
			bm, err := parseBMatch(val)
			if err != nil {
				return err
			}
			if k == "sides" {
				for i := 2; i < 6; i++ {
					into[i] = bm
				}
				continue
			}
			i, ok := faceKeys[k]
			if !ok {
				return unimpl("attach key %s", k)
			}
			into[i] = bm
		}
		return nil
	}
	if raw, ok := v["may_attach_to"]; ok {
		if err := faces(raw, &f.attach); err != nil {
			return nil, err
		}
		var m struct {
			Min *int `json:"min_sides_must_attach"`
		}
		json.Unmarshal(raw, &m)
		f.attachMin = 1
		if m.Min != nil {
			f.attachMin = max(*m.Min, 1)
		}
	}
	if raw, ok := v["may_not_attach_to"]; ok {
		if err := faces(raw, &f.notAttach); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// bput writes a block where a feature may write; false where it may not.
func (d *decoration) bput(p pos, s bs) bool {
	if !d.canWrite(p.x, p.y, p.z) {
		return false
	}
	d.set(p.x, p.y, p.z, s)
	return true
}

// Plant survival and placement as the study approximates them (btrees, p4_land): plants of the grass
// family need soil below; double plants also air above; a spore blossom hangs from a sturdy block.
var (
	bnSoil         = bnameSetOf(bedrockTagMembers["dirt"])
	bnGrassFamily  = bnameSetOf("short_grass fern tall_grass poppy dandelion large_fern")
	bnDoublePlants = bnameSetOf("tall_grass large_fern sunflower lilac rose_bush peony")
	bnSporeBlossom = internBName("spore_blossom")
	// Dry plants stand on sand, terracotta or the dirt family (the game's dry-vegetation ground).
	bnDryPlants = bnameSetOf("short_dry_grass tall_dry_grass deadbush")
	bnDryGround = bnameSetOf(bedrockTagMembers["dirt"] + " " + bedrockTagMembers["sand"] + " hardened_clay terracotta white_terracotta orange_terracotta " +
		"magenta_terracotta light_blue_terracotta yellow_terracotta lime_terracotta pink_terracotta gray_terracotta light_gray_terracotta " +
		"cyan_terracotta purple_terracotta blue_terracotta brown_terracotta green_terracotta red_terracotta black_terracotta")
)

func bnameSetOf(names string) (b bnameSet) {
	for _, n := range strings.Fields(names) {
		b.add(internBName(n))
	}
	return b
}

func bedrockSurvives(d *decoration, name uint16, p pos) bool {
	switch {
	case bnGrassFamily.has(name):
		return bnSoil.has(bname(d.at(p.add(0, -1, 0))))
	case name == bnSporeBlossom:
		return bSolid(d.at(p.add(0, 1, 0)))
	case bnDryPlants.has(name):
		return bnDryGround.has(bname(d.at(p.add(0, -1, 0))))
	}
	return true
}

func bedrockMayPlace(d *decoration, name uint16, p pos) bool {
	if bnDoublePlants.has(name) {
		return bnSoil.has(bname(d.at(p.add(0, -1, 0)))) && d.at(p.add(0, 1, 0)).isAir()
	}
	if bnNetherPlants.has(name) {
		return bnNetherGround.has(bname(d.at(p.add(0, -1, 0))))
	}
	return true
}

// The Nether's small plants (fungi, roots, sprouts) are placed on nylium, soul soil or the dirt family
// (the game's placement rule, by name as the other survival rules here).
var (
	bnNetherPlants = bnameSetOf("crimson_fungus warped_fungus crimson_roots warped_roots nether_sprouts")
	bnNetherGround = bnameSetOf(bedrockTagMembers["dirt"] + " crimson_nylium warped_nylium soul_soil")
)
