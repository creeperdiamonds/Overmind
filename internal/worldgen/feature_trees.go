package worldgen

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// Trees follow Mojang's algorithms from the study (section 10), in our own Go. Java's hash set order
// decides which random draw goes with which position, so posSet reproduces it (section 10.5). Buckets
// that the game would turn into trees are kept as lists, a rare miss the study allows.

type posSet struct {
	table [][]pos
	size  int
}

func (s *posSet) reset() {
	if len(s.table) != 16 {
		s.table = make([][]pos, 16)
	} else {
		for i := range s.table {
			s.table[i] = s.table[i][:0]
		}
	}
	s.size = 0
}

func posHash32(p pos) uint32 {
	h := (int32(p.y)+int32(p.z)*31)*31 + int32(p.x)
	u := uint32(h)
	return u ^ u>>16
}

func (s *posSet) add(p pos) {
	h := posHash32(p)
	i := h & uint32(len(s.table)-1)
	for _, q := range s.table[i] {
		if q == p {
			return
		}
	}
	s.table[i] = append(s.table[i], p)
	s.size++
	if s.size > len(s.table)*3/4 {
		old := s.table
		s.table = make([][]pos, len(old)*2)
		for _, b := range old {
			for _, q := range b {
				j := posHash32(q) & uint32(len(s.table)-1)
				s.table[j] = append(s.table[j], q)
			}
		}
	}
}

func (s *posSet) has(p pos) bool {
	for _, q := range s.table[posHash32(p)&uint32(len(s.table)-1)] {
		if q == p {
			return true
		}
	}
	return false
}

// popFirst removes the set's first element in iteration order.
func (s *posSet) popFirst() (pos, bool) {
	for i, b := range s.table {
		if len(b) > 0 {
			p := b[0]
			copy(b, b[1:])
			s.table[i] = b[:len(b)-1]
			s.size--
			return p, true
		}
	}
	return pos{}, false
}

func (s *posSet) list() []pos {
	out := make([]pos, 0, s.size)
	for _, b := range s.table {
		out = append(out, b...)
	}
	return out
}

// stateProvider chooses a state at a position.
type stateProvider func(d *decoration, p pos) bs

func simpleState(name string) stateProvider {
	s := stateOf(name)
	return func(*decoration, pos) bs { return s }
}

type weightedState struct {
	w int
	s bs
}

func weightedStates(list ...weightedState) stateProvider {
	total := 0
	for _, e := range list {
		total += e.w
	}
	return func(d *decoration, _ pos) bs {
		k := d.r.nextInt(total)
		for _, e := range list {
			if k < e.w {
				return e.s
			}
			k -= e.w
		}
		return list[0].s
	}
}

// withProp sets one property in a state's text and returns the state.
// withProp gives a state with one property changed. Results are kept, so a hot write builds no text.
func withProp(s bs, key, value string) bs {
	k := propKey{s, key, value}
	if v, ok := propMemo.Load(k); ok {
		return v.(bs)
	}
	v := withPropSlow(s, key, value)
	propMemo.Store(k, v)
	return v
}

type propKey struct {
	s          bs
	key, value string
}

// propMemo holds withProp's results. States and property values are few, so it stays small.
var propMemo sync.Map

func withPropSlow(s bs, key, value string) bs {
	name := s.name()
	i := strings.Index(name, key+"=")
	if i < 0 {
		return s
	}
	j := i + len(key) + 1
	k := j
	for k < len(name) && name[k] != ',' && name[k] != ']' {
		k++
	}
	return stateOf(name[:j] + value + name[k:])
}

// logState is a log by wood and axis; the states are kept after the first ask.
func logState(wood string, axis byte) bs {
	k := logKey{wood, axis}
	if v, ok := logMemo.Load(k); ok {
		return v.(bs)
	}
	v := stateOf(fmt.Sprintf("%s[axis=%c]", wood, axis))
	logMemo.Store(k, v)
	return v
}

type logKey struct {
	wood string
	axis byte
}

var logMemo sync.Map

func leafName(kind string) string {
	return kind + "[distance=7,persistent=false,waterlogged=false]"
}

// attachment is where foliage grows from.
type attachment struct {
	p      pos
	radius int
	dbl    bool
}

// treeRun is one tree's state while it is placed.
type treeRun struct {
	d                                   *decoration
	cfg                                 *treeCfg
	trunkSet, leafSet, decoSet, rootSet posSet
	atts                                []attachment
}

type treeCfg struct {
	log, foliageState    stateProvider
	below                stateProvider // Nil: dirt, unless the block is in the no-trunk-base set.
	forceDirt            bool          // The below provider replaces any block, dirt family too.
	trunk                trunkPlacer
	foliage              foliagePlacer
	layersLimit          int
	lower, middle, upper int
	upperLimit           int
	threeLayers          bool
	minClipped           int
	ignoreVines          bool
	decorators           []treeDecorator
	validExtra           map[uint16]bool // Upwards branching trunks also grow through these.
	axisLogs             string          // The log block's name, when a trunk sets an axis.
	roots                *mangroveRoots
}

// trunkPlacer places the trunk and fills t.atts. h is the height to use.
type trunkPlacer interface {
	height(r *featRand) int
	place(t *treeRun, h int, o pos)
}

type foliagePlacer interface {
	foliageHeight(r *featRand, h int) int
	foliageRadius(r *featRand, trunkH int) int
	offset(r *featRand) int
	create(t *treeRun, a attachment, h, fh, rad, off int)
	skip(t *treeRun, dx, y, dz, rr int, dbl bool) bool
}

// signedSkip is a foliage placer with its own signed rule.
type signedSkip interface {
	skipSigned(t *treeRun, dx, y, dz, rr int, dbl bool) (bool, bool)
}

// validPos is the trunk placer's test of a block a log may go in: the game's tree test, and for an
// upwards branching trunk the blocks it grows through.
func (t *treeRun) validPos(p pos) bool {
	s := t.d.at(p)
	return treeReplaceable(s) || (t.cfg.validExtra != nil && t.cfg.validExtra[stateBlock[s]])
}

// treeReplaceable is the game's tree test alone, which leaves and roots use.
func treeReplaceable(s bs) bool { return s.isAir() || s.tagged(tagTreeSwap) }

func (t *treeRun) isFree(p pos) bool {
	return t.validPos(p) || t.d.at(p).tagged(tagLogs)
}

func (t *treeRun) log(p pos) bool { return t.logAxis(p, 0) }

// logAxis places a log; axis, if not zero, sets its axis.
func (t *treeRun) logAxis(p pos, axis byte) bool {
	if !t.validPos(p) {
		return false
	}
	s := t.cfg.log(t.d, p)
	if axis != 0 {
		s = withProp(s, "axis", string(axis))
	}
	t.trunkSet.add(p)
	t.d.set(p.x, p.y, p.z, s)
	return true
}

func (t *treeRun) belowTrunk(p pos) {
	s := t.d.at(p)
	if t.cfg.below != nil {
		if s.tagged(tagNoTrunkBase) && !t.cfg.forceDirt {
			return
		}
		t.trunkSet.add(p)
		t.d.set(p.x, p.y, p.z, t.cfg.below(t.d, p))
		return
	}
	if s.tagged(tagNoTrunkBase) {
		return
	}
	t.trunkSet.add(p)
	t.d.set(p.x, p.y, p.z, sDirt)
}

func (t *treeRun) tryLeaf(p pos) bool {
	cur := t.d.at(p)
	if strings.Contains(cur.name(), "persistent=true") {
		return false
	}
	if !treeReplaceable(cur) {
		return false
	}
	s := t.cfg.foliageState(t.d, p)
	if cur.has(fWater) && cur.has(fSource) {
		s = withProp(s, "waterlogged", "true")
	}
	t.leafSet.add(p)
	t.d.set(p.x, p.y, p.z, s)
	return true
}

func (t *treeRun) row(c pos, rr, y int, dbl bool) {
	extra := 0
	if dbl {
		extra = 1
	}
	f := t.cfg.foliage
	for dx := -rr; dx <= rr+extra; dx++ {
		for dz := -rr; dz <= rr+extra; dz++ {
			if !t.skipSigned(f, dx, y, dz, rr, dbl) {
				t.tryLeaf(c.add(dx, y, dz))
			}
		}
	}
}

func (t *treeRun) skipSigned(f foliagePlacer, dx, y, dz, rr int, dbl bool) bool {
	if sk, ok := f.(signedSkip); ok {
		if v, done := sk.skipSigned(t, dx, y, dz, rr, dbl); done {
			return v
		}
	}
	mx, mz := absInt(dx), absInt(dz)
	if dbl {
		mx, mz = min(absInt(dx), absInt(dx-1)), min(absInt(dz), absInt(dz-1))
	}
	return f.skip(t, mx, y, mz, rr, dbl)
}

func (c *treeCfg) sizeAt(h, y int) int {
	if c.threeLayers {
		if y < c.layersLimit {
			return c.lower
		}
		if y >= h-c.upperLimit {
			return c.upper
		}
		return c.middle
	}
	if y < c.layersLimit {
		return c.lower
	}
	return c.upper
}

func (t *treeRun) maxFree(h int, o pos) int {
	vine := stateOf("vine[east=false,north=false,south=false,up=false,west=false]").block()
	for y := 0; y <= h+1; y++ {
		r := t.cfg.sizeAt(h, y)
		for x := -r; x <= r; x++ {
			for z := -r; z <= r; z++ {
				p := o.add(x, y, z)
				if !t.isFree(p) || (!t.cfg.ignoreVines && t.d.at(p).block() == vine) {
					return y - 2
				}
			}
		}
	}
	return h
}

// treeFeature places a tree (section 10).
func treeFeature(cfg *treeCfg) feature {
	return func(d *decoration, o pos) bool {
		t := &treeRun{d: d, cfg: cfg}
		t.trunkSet.reset()
		t.leafSet.reset()
		t.decoSet.reset()
		t.rootSet.reset()
		r := &d.r
		h := cfg.trunk.height(r)
		fh := cfg.foliage.foliageHeight(r, h)
		rad := cfg.foliage.foliageRadius(r, h-fh)
		start := o
		if cfg.roots != nil {
			start = cfg.roots.trunkOrigin(o, r)
		}
		lo, hi := min(o.y, start.y), max(o.y, start.y)+h+1
		if lo < minY+1 || hi > maxY+1 {
			return false
		}
		clip := t.maxFree(h, start)
		if clip < h && !(cfg.minClipped >= 0 && clip >= cfg.minClipped) {
			return false
		}
		if cfg.roots != nil && !cfg.roots.place(t, o, start) {
			return false
		}
		cfg.trunk.place(t, clip, start)
		for _, a := range t.atts {
			off := cfg.foliage.offset(r)
			cfg.foliage.create(t, a, clip, fh, rad, off)
		}
		if t.trunkSet.size == 0 && t.leafSet.size == 0 {
			return false
		}
		logs, leaves, roots := sortedByY(&t.trunkSet), sortedByY(&t.leafSet), sortedByY(&t.rootSet)
		for _, dec := range cfg.decorators {
			dec(t, logs, leaves, roots)
		}
		t.leafPass()
		t.edgeUpdate()
		return true
	}
}

// edgeUpdate is the part of the edge shape update the study saw: half a tall plant whose other half a
// new tree replaced cannot stay.
func (t *treeRun) edgeUpdate() {
	for _, set := range []*posSet{&t.trunkSet, &t.leafSet, &t.decoSet, &t.rootSet} {
		for _, b := range set.table {
			for _, p := range b {
				up, down := p.step(dUp), p.step(dDown)
				if s := t.d.at(up); strings.Contains(s.name(), "half=upper") && withProp(s, "half", "lower") != t.d.at(p) {
					t.d.set(up.x, up.y, up.z, sAir)
				}
				if s := t.d.at(down); strings.Contains(s.name(), "half=lower") && withProp(s, "half", "upper") != t.d.at(p) {
					t.d.set(down.x, down.y, down.z, sAir)
				}
			}
		}
	}
}

// sortedByY is the decorators' list: the set's order, stably sorted by height.
func sortedByY(s *posSet) []pos {
	l := s.list()
	sort.SliceStable(l, func(i, j int) bool { return l[i].y < l[j].y })
	return l
}

type box struct{ x0, y0, z0, x1, y1, z1 int }

func (b *box) include(p pos) {
	b.x0, b.y0, b.z0 = min(b.x0, p.x), min(b.y0, p.y), min(b.z0, p.z)
	b.x1, b.y1, b.z1 = max(b.x1, p.x), max(b.y1, p.y), max(b.z1, p.z)
}

func (b *box) inside(p pos) bool {
	return p.x >= b.x0 && p.x <= b.x1 && p.y >= b.y0 && p.y <= b.y1 && p.z >= b.z0 && p.z <= b.z1
}

// leafDistance reads a state's distance: logs 0, leaves their property, others none.
func leafDistance(s bs) (int, bool) {
	if s.tagged(tagLogs) {
		return 0, true
	}
	if !s.tagged(tagLeaves) {
		return 0, false
	}
	n := s.name()
	if i := strings.Index(n, "distance="); i >= 0 {
		return int(n[i+9] - '0'), true
	}
	return 0, false
}

// leafPass is section 10.4, with the study's exact queue order. It also runs the edge shape update the
// study saw: a plant whose ground became a trunk's dirt cannot stay. Only that case is modelled.
func (t *treeRun) leafPass() {
	sets := []*posSet{&t.rootSet, &t.trunkSet, &t.leafSet, &t.decoSet}
	bounds := box{1 << 30, 1 << 30, 1 << 30, -1 << 30, -1 << 30, -1 << 30}
	any := false
	for _, s := range sets {
		for _, b := range s.table {
			for _, p := range b {
				bounds.include(p)
				any = true
			}
		}
	}
	if !any {
		return
	}
	shape := map[pos]bool{}
	for _, s := range []*posSet{&t.decoSet, &t.rootSet} {
		for _, b := range s.table {
			for _, p := range b {
				shape[p] = true
			}
		}
	}
	var q [7]posSet
	for i := range q {
		q[i].reset()
	}
	for _, b := range t.trunkSet.table {
		for _, p := range b {
			q[0].add(p)
		}
	}
	d := 0
	for d < 7 {
		p, ok := q[d].popFirst()
		if !ok {
			d++
			continue
		}
		if !bounds.inside(p) {
			continue
		}
		if d != 0 {
			s := t.d.at(p)
			if s.tagged(tagLeaves) {
				if n := withProp(s, "distance", string(rune('0'+d))); n != s {
					t.d.set(p.x, p.y, p.z, n)
				}
			}
		}
		shape[p] = true
		for _, a := range allDirs {
			n := p.step(a)
			if !bounds.inside(n) || shape[n] {
				continue
			}
			if dist, ok := leafDistance(t.d.at(n)); ok {
				nd := min(dist, d+1)
				if nd < 7 {
					q[nd].add(n)
					d = min(d, nd)
				}
			}
		}
	}
}

// Trunk placers.

type trunkHeights struct{ base, a, b int }

func (t trunkHeights) height(r *featRand) int { return t.base + r.nextInt(t.a+1) + r.nextInt(t.b+1) }

type straightTrunk struct{ trunkHeights }

func (s straightTrunk) place(t *treeRun, h int, o pos) {
	t.belowTrunk(o.add(0, -1, 0))
	for y := range h {
		t.log(o.add(0, y, 0))
	}
	t.atts = append(t.atts, attachment{o.add(0, h, 0), 0, false})
}

func pickHorizontal(r *featRand) dir { return horizontal[r.nextInt(4)] }

type forkingTrunk struct{ trunkHeights }

func (s forkingTrunk) place(t *treeRun, h int, o pos) {
	r := &t.d.r
	t.belowTrunk(o.add(0, -1, 0))
	lean := pickHorizontal(r)
	leanH := h - r.nextInt(4) - 1
	leanS := 3 - r.nextInt(3)
	tx, tz := o.x, o.z
	last := minY - 1000
	for yo := range h {
		if yo >= leanH && leanS > 0 {
			v := dirVec[lean]
			tx, tz = tx+v.x, tz+v.z
			leanS--
		}
		if t.log(pos{tx, o.y + yo, tz}) {
			last = o.y + yo + 1
		}
	}
	if last != minY-1000 {
		t.atts = append(t.atts, attachment{pos{tx, last, tz}, 1, false})
	}
	tx, tz = o.x, o.z
	br := pickHorizontal(r)
	if br != lean {
		bp := leanH - r.nextInt(2) - 1
		bs := 1 + r.nextInt(3)
		last = minY - 1000
		for yo := bp; yo < h && bs > 0; yo, bs = yo+1, bs-1 {
			if yo >= 1 {
				v := dirVec[br]
				tx, tz = tx+v.x, tz+v.z
				if t.log(pos{tx, o.y + yo, tz}) {
					last = o.y + yo + 1
				}
			}
		}
		if last != minY-1000 {
			t.atts = append(t.atts, attachment{pos{tx, last, tz}, 0, false})
		}
	}
}

type fancyTrunk struct{ trunkHeights }

func fancyShape(h, y int) float32 {
	if float32(y) < float32(float32(h)*0.3) {
		return -1
	}
	r := float32(h) / 2
	a := r - float32(y)
	d := float32(math.Sqrt(float64(float32(r*r) - float32(a*a))))
	if a == 0 {
		d = r
	} else if float32(math.Abs(float64(a))) >= r {
		return 0
	}
	return float32(d * 0.5)
}

func (s fancyTrunk) limb(t *treeRun, a, e pos, place bool) bool {
	if !place && a == e {
		return true
	}
	dx, dy, dz := e.x-a.x, e.y-a.y, e.z-a.z
	n := max(absInt(dx), absInt(dy), absInt(dz))
	if n == 0 {
		return true
	}
	fx, fy, fz := float32(dx)/float32(n), float32(dy)/float32(n), float32(dz)/float32(n)
	for i := 0; i <= n; i++ {
		f := func(v float32) int { return int(math.Floor(float64(0.5 + float32(float32(i)*v)))) }
		p := a.add(f(fx), f(fy), f(fz))
		if place {
			ax, az := absInt(p.x-a.x), absInt(p.z-a.z)
			m := max(ax, az)
			axis := byte('y')
			if m != 0 {
				if ax == m {
					axis = 'x'
				} else {
					axis = 'z'
				}
			}
			t.logAxis(p, axis)
		} else if !t.isFree(p) {
			return false
		}
	}
	return true
}

func (s fancyTrunk) place(t *treeRun, h int, o pos) {
	r := &t.d.r
	h2 := h + 2
	tH := int(math.Floor(float64(float64(h2) * 0.618)))
	t.belowTrunk(o.add(0, -1, 0))
	cl := min(1, int(math.Floor(1.382+math.Pow(float64(1.0*float64(h2)/13.0), 2.0))))
	top := o.y + tH
	ry := h2 - 5
	type limbEnd struct {
		p    pos
		base int
	}
	fc := []limbEnd{{o.add(0, ry, 0), top}}
	for ; ry >= 0; ry-- {
		sh := fancyShape(h2, ry)
		if sh < 0 {
			continue
		}
		for range cl {
			rd := float64(float64(sh) * (float64(r.nextFloat()) + 0.328))
			ang := float64(float64(float32(r.nextFloat()*2.0)) * math.Pi)
			x := float64(rd*math.Sin(ang)) + 0.5
			z := float64(rd*math.Cos(ang)) + 0.5
			a := o.add(floorI(x), ry-1, floorI(z))
			b := a.add(0, 5, 0)
			if s.limb(t, a, b, false) {
				dx, dz := o.x-a.x, o.z-a.z
				bh := float64(a.y) - float64(math.Sqrt(float64(dx*dx+dz*dz))*0.381)
				bt := top
				if !(bh > float64(top)) {
					bt = int(bh)
				}
				c := pos{o.x, bt, o.z}
				if s.limb(t, c, a, false) {
					fc = append(fc, limbEnd{a, c.y})
				}
			}
		}
	}
	s.limb(t, o, o.add(0, tH, 0), true)
	lim := float64(float64(h2) * 0.2)
	for _, e := range fc {
		c := pos{o.x, e.base, o.z}
		if c != e.p && float64(e.base-o.y) >= lim {
			s.limb(t, c, e.p, true)
		}
	}
	for _, e := range fc {
		if float64(e.base-o.y) >= lim {
			t.atts = append(t.atts, attachment{e.p, 0, false})
		}
	}
}

type giantTrunk struct{ trunkHeights }

func (t *treeRun) logIfFree(p pos) {
	if t.isFree(p) {
		t.log(p)
	}
}

func (s giantTrunk) place(t *treeRun, h int, o pos) {
	for _, q := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		t.belowTrunk(o.add(q[0], -1, q[1]))
	}
	for y := range h {
		t.logIfFree(o.add(0, y, 0))
		if y < h-1 {
			t.logIfFree(o.add(1, y, 0))
			t.logIfFree(o.add(1, y, 1))
			t.logIfFree(o.add(0, y, 1))
		}
	}
	t.atts = append(t.atts, attachment{o.add(0, h, 0), 0, true})
}

type megaJungleTrunk struct{ trunkHeights }

func (s megaJungleTrunk) place(t *treeRun, h int, o pos) {
	r := &t.d.r
	giantTrunk{s.trunkHeights}.place(t, h, o)
	for b := h - 2 - r.nextInt(4); b > h/2; b -= 2 + r.nextInt(4) {
		ang := float32(r.nextFloat() * twoPi32)
		bx, bz := 0, 0
		for i := range 5 {
			bx = int(1.5 + float32(tcos(ang)*float32(i)))
			bz = int(1.5 + float32(tsin(ang)*float32(i)))
			t.log(o.add(bx, b-3+i/2, bz))
		}
		t.atts = append(t.atts, attachment{o.add(bx, b, bz), -2, false})
	}
}

type darkOakTrunk struct{ trunkHeights }

func (s darkOakTrunk) place(t *treeRun, h int, o pos) {
	r := &t.d.r
	for _, q := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		t.belowTrunk(o.add(q[0], -1, q[1]))
	}
	lean := pickHorizontal(r)
	leanH := h - r.nextInt(4)
	leanS := 2 - r.nextInt(3)
	tx, tz := o.x, o.z
	ey := o.y + h - 1
	for dy := range h {
		if dy >= leanH && leanS > 0 {
			v := dirVec[lean]
			tx, tz = tx+v.x, tz+v.z
			leanS--
		}
		p := pos{tx, o.y + dy, tz}
		if s := t.d.at(p); s.isAir() || s.tagged(tagLeaves) {
			t.log(p)
			t.log(p.add(1, 0, 0))
			t.log(p.add(0, 0, 1))
			t.log(p.add(1, 0, 1))
		}
	}
	t.atts = append(t.atts, attachment{pos{tx, ey, tz}, 0, true})
	for ox := -1; ox <= 2; ox++ {
		for oz := -1; oz <= 2; oz++ {
			if (ox < 0 || ox > 1 || oz < 0 || oz > 1) && r.nextInt(3) <= 0 {
				l := r.nextInt(3) + 2
				for by := range l {
					t.log(pos{o.x + ox, ey - by - 1, o.z + oz})
				}
				t.atts = append(t.atts, attachment{pos{o.x + ox, ey, o.z + oz}, 0, false})
			}
		}
	}
}

type bendingTrunk struct {
	trunkHeights
	minLeaves int
	bend      intProvider
}

func (s bendingTrunk) place(t *treeRun, h int, o pos) {
	r := &t.d.r
	a := pickHorizontal(r)
	lh := h - 1
	p := o
	t.belowTrunk(p.add(0, -1, 0))
	for i := 0; i <= lh; i++ {
		if i+1 >= lh+r.nextInt(2) {
			p = p.step(a)
		}
		if t.validPos(p) {
			t.log(p)
		}
		if i >= s.minLeaves {
			t.atts = append(t.atts, attachment{p, 0, false})
		}
		p = p.step(dUp)
	}
	n := s.bend(r)
	for i := 0; i <= n; i++ {
		if t.validPos(p) {
			t.log(p)
		}
		t.atts = append(t.atts, attachment{p, 0, false})
		p = p.step(a)
	}
}

type branchingTrunk struct {
	trunkHeights
	steps, length intProvider
	chance        float32
}

// place grows the upwards branching trunk. It sets no dirt under itself: the mangrove's roots hold it.
func (s branchingTrunk) place(t *treeRun, h int, o pos) {
	r := &t.d.r
	for hp := range h {
		cy := o.y + hp
		if t.log(pos{o.x, cy, o.z}) && hp < h-1 && r.nextFloat() < s.chance {
			a := pickHorizontal(r)
			l := s.length(r)
			bp := max(0, l-s.length(r)-1)
			st := s.steps(r)
			ha := cy + bp
			lx, lz := o.x, o.z
			for i := bp; i < h && st > 0; i, st = i+1, st-1 {
				if i >= 1 {
					v := dirVec[a]
					lx, lz = lx+v.x, lz+v.z
					py := cy + i
					ha = py
					if t.log(pos{lx, py, lz}) {
						ha++
					}
					t.atts = append(t.atts, attachment{pos{lx, py, lz}, 0, false})
				}
			}
			if ha-cy > 1 {
				t.atts = append(t.atts, attachment{pos{lx, ha, lz}, 0, false}, attachment{pos{lx, ha - 2, lz}, 0, false})
			}
		}
		if hp == h-1 {
			t.atts = append(t.atts, attachment{pos{o.x, cy + 1, o.z}, 0, false})
		}
	}
}

type cherryTrunk struct {
	trunkHeights
	startMin, startMax int
	count, length      intProvider
	endOffset          intProvider
}

func (s cherryTrunk) place(t *treeRun, h int, o pos) {
	r := &t.d.r
	t.belowTrunk(o.add(0, -1, 0))
	a := max(0, h-1+r.between(s.startMin, s.startMax))
	b := max(0, h-1+r.between(s.startMin, s.startMax-1))
	if b >= a {
		b++
	}
	c := s.count(r)
	mid, both := c == 3, c >= 2
	th := a + 1
	if mid {
		th = h
	} else if both {
		th = max(a, b) + 1
	}
	for y := range th {
		t.log(o.add(0, y, 0))
	}
	if mid {
		t.atts = append(t.atts, attachment{o.add(0, th, 0), 0, false})
	}
	d0 := pickHorizontal(r)
	t.atts = append(t.atts, s.branch(t, h, o, d0, a, a < th-1))
	if both {
		t.atts = append(t.atts, s.branch(t, h, o, d0.opposite(), b, b < th-1))
	}
}

func (s cherryTrunk) branch(t *treeRun, h int, o pos, a dir, off int, cont bool) attachment {
	r := &t.d.r
	p := o.add(0, off, 0)
	eo := h - 1 + s.endOffset(r)
	ext := cont || eo < off
	dist := s.length(r)
	if ext {
		dist++
	}
	e := o.stepN(a, dist).add(0, eo, 0)
	axis := byte('x')
	if a == dNorth || a == dSouth {
		axis = 'z'
	}
	n := 1
	if ext {
		n = 2
	}
	for range n {
		p = p.step(a)
		t.logAxis(p, axis)
	}
	vd := dDown
	if e.y > p.y {
		vd = dUp
	}
	for {
		m := absInt(p.x-e.x) + absInt(p.y-e.y) + absInt(p.z-e.z)
		if m == 0 {
			return attachment{e.add(0, 1, 0), 0, false}
		}
		vert := r.nextFloat() < float32(absInt(e.y-p.y))/float32(m)
		if vert {
			p = p.step(vd)
			t.log(p)
		} else {
			p = p.step(a)
			t.logAxis(p, axis)
		}
	}
}

// Foliage placers.

type foliageBase struct{ radius, offs intProvider }

func (f foliageBase) foliageRadius(r *featRand, _ int) int { return f.radius(r) }
func (f foliageBase) offset(r *featRand) int               { return f.offs(r) }

type blobFoliage struct {
	foliageBase
	fh int
}

func (f blobFoliage) foliageHeight(*featRand, int) int { return f.fh }
func (f blobFoliage) create(t *treeRun, a attachment, _, fh, rad, off int) {
	for yo := off; yo >= off-fh; yo-- {
		rr := max(rad+a.radius-1-yo/2, 0)
		t.row(a.p, rr, yo, a.dbl)
	}
}
func (f blobFoliage) skip(t *treeRun, dx, y, dz, rr int, _ bool) bool {
	return dx == rr && dz == rr && (t.d.r.nextInt(2) == 0 || y == 0)
}

type bushFoliage struct {
	foliageBase
	fh int
}

func (f bushFoliage) foliageHeight(*featRand, int) int { return f.fh }
func (f bushFoliage) create(t *treeRun, a attachment, _, fh, rad, off int) {
	for yo := off; yo >= off-fh; yo-- {
		t.row(a.p, rad+a.radius-1-yo, yo, a.dbl)
	}
}
func (f bushFoliage) skip(t *treeRun, dx, _, dz, rr int, _ bool) bool {
	return dx == rr && dz == rr && t.d.r.nextInt(2) == 0
}

type fancyFoliage struct {
	foliageBase
	fh int
}

func (f fancyFoliage) foliageHeight(*featRand, int) int { return f.fh }
func (f fancyFoliage) create(t *treeRun, a attachment, _, fh, rad, off int) {
	for yo := off; yo >= off-fh; yo-- {
		rr := rad
		if yo != off && yo != off-fh {
			rr++
		}
		t.row(a.p, rr, yo, a.dbl)
	}
}
func (f fancyFoliage) skip(_ *treeRun, dx, _, dz, rr int, _ bool) bool {
	x, z := float32(dx)+0.5, float32(dz)+0.5
	return float32(x*x)+float32(z*z) > float32(rr*rr)
}

type acaciaFoliage struct{ foliageBase }

func (f acaciaFoliage) foliageHeight(*featRand, int) int { return 0 }
func (f acaciaFoliage) create(t *treeRun, a attachment, _, fh, rad, off int) {
	c := a.p.add(0, off, 0)
	t.row(c, rad+a.radius, -1-fh, a.dbl)
	t.row(c, rad-1, -fh, a.dbl)
	t.row(c, rad+a.radius-1, 0, a.dbl)
}
func (f acaciaFoliage) skip(_ *treeRun, dx, y, dz, rr int, _ bool) bool {
	if y == 0 {
		return (dx > 1 || dz > 1) && dx != 0 && dz != 0
	}
	return dx == rr && dz == rr && rr > 0
}

type spruceFoliage struct {
	foliageBase
	trunkH intProvider
}

func (f spruceFoliage) foliageHeight(r *featRand, h int) int { return max(4, h-f.trunkH(r)) }
func (f spruceFoliage) create(t *treeRun, a attachment, _, fh, rad, off int) {
	rr := t.d.r.nextInt(2)
	mx, mn := 1, 0
	for yo := off; yo >= -fh; yo-- {
		t.row(a.p, rr, yo, a.dbl)
		if rr >= mx {
			rr = mn
			mn = 1
			mx = min(mx+1, rad+a.radius)
		} else {
			rr++
		}
	}
}
func (f spruceFoliage) skip(_ *treeRun, dx, _, dz, rr int, _ bool) bool {
	return dx == rr && dz == rr && rr > 0
}

type pineFoliage struct {
	foliageBase
	fh intProvider
}

func (f pineFoliage) foliageHeight(r *featRand, _ int) int { return f.fh(r) }
func (f pineFoliage) foliageRadius(r *featRand, trunkH int) int {
	return f.radius(r) + r.nextInt(max(trunkH+1, 1))
}
func (f pineFoliage) create(t *treeRun, a attachment, _, fh, rad, off int) {
	rr := 0
	for yo := off; yo >= off-fh; yo-- {
		t.row(a.p, rr, yo, a.dbl)
		if rr >= 1 && yo == off-fh+1 {
			rr--
		} else if rr < rad+a.radius {
			rr++
		}
	}
}
func (f pineFoliage) skip(_ *treeRun, dx, _, dz, rr int, _ bool) bool {
	return dx == rr && dz == rr && rr > 0
}

type megaPineFoliage struct {
	foliageBase
	crown intProvider
}

func (f megaPineFoliage) foliageHeight(r *featRand, _ int) int { return f.crown(r) }
func (f megaPineFoliage) create(t *treeRun, a attachment, _, fh, rad, off int) {
	prev := 0
	for yy := a.p.y - fh + off; yy <= a.p.y+off; yy++ {
		yo := a.p.y - yy
		sm := rad + a.radius + floorI(float64(float32(float32(yo)/float32(fh))*3.5))
		jg := sm
		if yo > 0 && sm == prev && yy&1 == 0 {
			jg = sm + 1
		}
		t.row(pos{a.p.x, yy, a.p.z}, jg, 0, a.dbl)
		prev = sm
	}
}
func (f megaPineFoliage) skip(_ *treeRun, dx, _, dz, rr int, _ bool) bool {
	return dx+dz >= 7 || dx*dx+dz*dz > rr*rr
}

type jungleFoliage struct {
	foliageBase
	fh int
}

func (f jungleFoliage) foliageHeight(*featRand, int) int { return f.fh }
func (f jungleFoliage) create(t *treeRun, a attachment, _, fh, rad, off int) {
	lh := fh
	if !a.dbl {
		lh = 1 + t.d.r.nextInt(2)
	}
	for yo := off; yo >= off-lh; yo-- {
		t.row(a.p, rad+a.radius+1-yo, yo, a.dbl)
	}
}
func (f jungleFoliage) skip(_ *treeRun, dx, _, dz, rr int, _ bool) bool {
	return dx+dz >= 7 || dx*dx+dz*dz > rr*rr
}

type darkOakFoliage struct{ foliageBase }

func (f darkOakFoliage) foliageHeight(*featRand, int) int { return 4 }
func (f darkOakFoliage) create(t *treeRun, a attachment, _, _, rad, off int) {
	c := a.p.add(0, off, 0)
	if a.dbl {
		t.row(c, rad+2, -1, true)
		t.row(c, rad+3, 0, true)
		t.row(c, rad+2, 1, true)
		if t.d.r.nextBool() {
			t.row(c, rad, 2, true)
		}
	} else {
		t.row(c, rad+2, -1, false)
		t.row(c, rad+1, 0, false)
	}
}
func (f darkOakFoliage) skip(_ *treeRun, dx, y, dz, rr int, dbl bool) bool {
	if y == -1 && !dbl {
		return dx == rr && dz == rr
	}
	if y == 1 {
		return dx+dz > rr*2-2
	}
	return false
}
func (f darkOakFoliage) skipSigned(_ *treeRun, dx, y, dz, rr int, dbl bool) (bool, bool) {
	if y == 0 && dbl && (dx == -rr || dx >= rr) && (dz == -rr || dz >= rr) {
		return true, true
	}
	return false, false
}

type spreadFoliage struct {
	foliageBase
	fh       intProvider
	attempts int
}

func (f spreadFoliage) foliageHeight(r *featRand, _ int) int { return f.fh(r) }
func (f spreadFoliage) create(t *treeRun, a attachment, _, fh, rad, off int) {
	r := &t.d.r
	for range f.attempts {
		x := r.nextInt(rad) - r.nextInt(rad)
		y := r.nextInt(fh) - r.nextInt(fh)
		z := r.nextInt(rad) - r.nextInt(rad)
		t.tryLeaf(a.p.add(x, y, z))
	}
}
func (f spreadFoliage) skip(*treeRun, int, int, int, int, bool) bool { return false }

type cherryFoliage struct {
	foliageBase
	fh                              intProvider
	wideHole, cornerHole, hang, ext float32
}

func (f cherryFoliage) foliageHeight(r *featRand, _ int) int { return f.fh(r) }
func (f cherryFoliage) create(t *treeRun, a attachment, _, fh, rad, off int) {
	c := a.p.add(0, off, 0)
	rr := rad + a.radius - 1
	t.row(c, rr-2, fh-3, a.dbl)
	t.row(c, rr-1, fh-4, a.dbl)
	for y := fh - 5; y >= 0; y-- {
		t.row(c, rr, y, a.dbl)
	}
	f.hanging(t, c, rr, -1, a.dbl)
	f.hanging(t, c, rr-1, -2, a.dbl)
}
func (f cherryFoliage) hanging(t *treeRun, c pos, rr, y int, dbl bool) {
	t.row(c, rr, y, dbl)
	extra := 0
	if dbl {
		extra = 1
	}
	base := c.add(0, -1, 0)
	ext := func(ch float32, q pos) bool {
		if absInt(q.x-base.x)+absInt(q.y-base.y)+absInt(q.z-base.z) >= 7 {
			return false
		}
		if t.d.r.nextFloat() > ch {
			return false
		}
		return t.tryLeaf(q)
	}
	for _, along := range horizontal {
		to := along.clockwise()
		oE := rr
		if to == dEast || to == dSouth {
			oE = rr + extra
		}
		q := c.add(0, y-1, 0).stepN(to, oE).stepN(along, -rr)
		for i := -rr; i < rr+extra; i++ {
			if t.leafSet.has(q.step(dUp)) {
				if ext(f.hang, q) {
					ext(f.ext, q.step(dDown))
				}
			}
			q = q.step(along)
		}
	}
}
func (f cherryFoliage) skip(t *treeRun, dx, y, dz, rr int, _ bool) bool {
	r := &t.d.r
	if y == -1 && (dx == rr || dz == rr) && r.nextFloat() < f.wideHole {
		return true
	}
	corner := dx == rr && dz == rr
	if rr > 2 {
		return corner || (dx+dz > rr*2-2 && r.nextFloat() < f.cornerHole)
	}
	return corner && r.nextFloat() < f.cornerHole
}

// Decorators.

type treeDecorator func(t *treeRun, logs, leaves, roots []pos)

func (t *treeRun) deco(p pos, s bs) {
	t.decoSet.add(p)
	t.d.set(p.x, p.y, p.z, s)
}

func vineState(face string) bs {
	s := "vine[east=false,north=false,south=false,up=false,west=false]"
	return stateOf(strings.Replace(s, face+"=false", face+"=true", 1))
}

var (
	vineFaces = [4]struct {
		side dir
		face string
	}{{dWest, "east"}, {dEast, "west"}, {dNorth, "south"}, {dSouth, "north"}}
)

func leaveVine(p float32) treeDecorator {
	return func(t *treeRun, _, leaves, _ []pos) {
		r := &t.d.r
		for _, l := range leaves {
			for _, f := range vineFaces {
				if r.nextFloat() < p {
					q := l.step(f.side)
					if t.d.at(q).isAir() {
						st := vineState(f.face)
						t.deco(q, st)
						q2 := q.step(dDown)
						for n := 4; t.d.at(q2).isAir() && n > 0; n-- {
							t.deco(q2, st)
							q2 = q2.step(dDown)
						}
					}
				}
			}
		}
	}
}

func trunkVine(t *treeRun, logs, _, _ []pos) {
	r := &t.d.r
	for _, l := range logs {
		for _, f := range vineFaces {
			if r.nextInt(3) > 0 {
				q := l.step(f.side)
				if t.d.at(q).isAir() {
					t.deco(q, vineState(f.face))
				}
			}
		}
	}
}

func dirName(a dir) string {
	return [6]string{"down", "up", "north", "south", "west", "east"}[a]
}

func cocoa(p float32) treeDecorator {
	return func(t *treeRun, logs, _, _ []pos) {
		r := &t.d.r
		if r.nextFloat() >= p || len(logs) == 0 {
			return
		}
		y0 := logs[0].y
		for _, l := range logs {
			if l.y-y0 > 2 {
				continue
			}
			for _, a := range horizontal {
				if r.nextFloat() <= 0.25 {
					q := l.step(a.opposite())
					if t.d.at(q).isAir() {
						t.deco(q, stateOf(fmt.Sprintf("cocoa[age=%d,facing=%s]", r.nextInt(3), dirName(a))))
					}
				}
			}
		}
	}
}

func beehive(p float32) treeDecorator {
	nest := stateOf("bee_nest[facing=south,honey_level=0]")
	return func(t *treeRun, logs, leaves, _ []pos) {
		if len(logs) == 0 {
			return
		}
		r := &t.d.r
		if r.nextFloat() >= p {
			return
		}
		var hy int
		if len(leaves) > 0 {
			hy = max(leaves[0].y-1, logs[0].y+1)
		} else {
			hy = min(logs[0].y+1+r.nextInt(3), logs[len(logs)-1].y)
		}
		var list []pos
		for _, l := range logs {
			if l.y == hy {
				list = append(list, l.step(dNorth), l.step(dEast), l.step(dWest))
			}
		}
		if len(list) == 0 {
			return
		}
		for i := len(list); i > 1; i-- {
			j := r.nextInt(i)
			list[i-1], list[j] = list[j], list[i-1]
		}
		for _, q := range list {
			if t.d.at(q).isAir() && t.d.at(q.step(dSouth)).isAir() {
				t.deco(q, nest)
				n := 2 + r.nextInt(2)
				for range n {
					r.nextInt(599)
				}
				return
			}
		}
	}
}

func lowestList(logs, roots []pos) []pos {
	if len(roots) == 0 {
		return logs
	}
	if len(logs) > 0 && roots[0].y == logs[0].y {
		return append(append([]pos{}, logs...), roots...)
	}
	return roots
}

func alterGround(pv optionalState) treeDecorator {
	return func(t *treeRun, logs, _, roots []pos) {
		base := lowestList(logs, roots)
		if len(base) == 0 {
			return
		}
		r := &t.d.r
		my := base[0].y
		column := func(q pos) {
			for dy := 2; dy >= -3; dy-- {
				c := q.add(0, dy, 0)
				if s, ok := pv(t.d, c); ok {
					t.d.set(c.x, c.y, c.z, s)
					return
				}
				if !t.d.at(c).isAir() && dy < 0 {
					return
				}
			}
		}
		circle := func(q pos) {
			for x := -2; x <= 2; x++ {
				for z := -2; z <= 2; z++ {
					if !(absInt(x) == 2 && absInt(z) == 2) {
						column(q.add(x, 0, z))
					}
				}
			}
		}
		for _, p := range base {
			if p.y != my {
				continue
			}
			circle(p.add(-1, 0, -1))
			circle(p.add(2, 0, -1))
			circle(p.add(-1, 0, 2))
			circle(p.add(2, 0, 2))
			for range 5 {
				k := r.nextInt(64)
				x, z := k%8, k/8
				if x == 0 || x == 7 || z == 0 || z == 7 {
					circle(p.add(-3+x, 0, -3+z))
				}
			}
		}
	}
}

// optionalState is a provider that may give nothing, like a rule-based one.
type optionalState func(d *decoration, p pos) (bs, bool)

func placeOnGround(tries, radius, height int, pv stateProvider) treeDecorator {
	vine := stateOf("vine[east=false,north=false,south=false,up=false,west=false]").block()
	return func(t *treeRun, logs, _, roots []pos) {
		base := lowestList(logs, roots)
		if len(base) == 0 {
			return
		}
		r := &t.d.r
		o := base[0]
		x0, x1, z0, z1 := o.x, o.x, o.z, o.z
		for _, p := range base {
			if p.y == o.y {
				x0, x1, z0, z1 = min(x0, p.x), max(x1, p.x), min(z0, p.z), max(z1, p.z)
			}
		}
		for range tries {
			px := r.between(x0-radius, x1+radius)
			py := r.between(o.y-height, o.y+height)
			pz := r.between(z0-radius, z1+radius)
			p := pos{px, py, pz}
			a := p.step(dUp)
			s := t.d.at(a)
			if (s.isAir() || s.block() == vine) && t.d.at(p).has(fSolidRender) && t.d.height(hmMotionNoLeaves, px, pz) <= a.y {
				t.deco(a, pv(t.d, a))
			}
		}
	}
}

func attachedToLeaves(prob float32, exXZ, exY int, pv stateProvider, reqEmpty int, dirs []dir) treeDecorator {
	return func(t *treeRun, _, leaves, _ []pos) {
		r := &t.d.r
		black := map[pos]bool{}
		l := append([]pos{}, leaves...)
		for i := len(l); i > 1; i-- {
			j := r.nextInt(i)
			l[i-1], l[j] = l[j], l[i-1]
		}
		for _, p := range l {
			a := dirs[r.nextInt(len(dirs))]
			q := p.step(a)
			if black[q] || r.nextFloat() >= prob {
				continue
			}
			ok := true
			for i := 1; i <= reqEmpty; i++ {
				if !t.d.at(p.stepN(a, i)).isAir() {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			for x := -exXZ; x <= exXZ; x++ {
				for y := -exY; y <= exY; y++ {
					for z := -exXZ; z <= exXZ; z++ {
						black[q.add(x, y, z)] = true
					}
				}
			}
			t.deco(q, pv(t.d, q))
		}
	}
}

func attachedToLogs(prob float32, pv stateProvider, dirs []dir) treeDecorator {
	return func(t *treeRun, logs, _, _ []pos) {
		r := &t.d.r
		l := append([]pos{}, logs...)
		for i := len(l); i > 1; i-- {
			j := r.nextInt(i)
			l[i-1], l[j] = l[j], l[i-1]
		}
		for _, p := range l {
			a := dirs[r.nextInt(len(dirs))]
			q := p.step(a)
			if r.nextFloat() <= prob && t.d.at(q).isAir() {
				t.deco(q, pv(t.d, q))
			}
		}
	}
}

// mangroveRoots is the mangrove's root placer, which the study does not write up: written from
// knowledge of Java 26.1 and unverified against a Java run (no traced chunk holds a mangrove swamp).
type mangroveRoots struct {
	offset        intProvider
	root          bs
	above         bs // Placed on a root by chance, if the block over it is air.
	aboveChance   float32
	through       map[uint16]bool // Roots also grow through these.
	muddyIn       map[uint16]bool // Roots in these are muddy roots.
	muddy         bs
	width, length int
	skew          float32
}

func (m *mangroveRoots) trunkOrigin(o pos, r *featRand) pos { return o.add(0, m.offset(r), 0) }

func (m *mangroveRoots) canPlace(t *treeRun, p pos) bool {
	s := t.d.at(p)
	return treeReplaceable(s) || m.through[stateBlock[s]]
}

func (m *mangroveRoots) place(t *treeRun, o, trunk pos) bool {
	for p := o; p.y < trunk.y; p = p.step(dUp) {
		if !m.canPlace(t, p) {
			return false
		}
	}
	list := []pos{trunk.step(dDown)}
	for _, a := range horizontal {
		q := trunk.step(a)
		var l []pos
		if !m.simulate(t, q, a, trunk, &l, 0) {
			return false
		}
		list = append(list, l...)
		list = append(list, q)
	}
	for _, q := range list {
		m.placeRoot(t, q)
	}
	return true
}

func (m *mangroveRoots) simulate(t *treeRun, p pos, a dir, origin pos, l *[]pos, layer int) bool {
	if layer == m.length || len(*l) > m.length {
		return false
	}
	for _, q := range m.potential(t, p, a, origin) {
		if m.canPlace(t, q) {
			*l = append(*l, q)
			if !m.simulate(t, q, a, origin, l, layer+1) {
				return false
			}
		}
	}
	return true
}

func (m *mangroveRoots) potential(t *treeRun, p pos, a dir, origin pos) []pos {
	r := &t.d.r
	below, side := p.step(dDown), p.step(a)
	dist := absInt(p.x-origin.x) + absInt(p.y-origin.y) + absInt(p.z-origin.z)
	switch {
	case dist > m.width-3 && dist <= m.width:
		if r.nextFloat() < m.skew {
			return []pos{below, side.step(dDown)}
		}
		return []pos{below}
	case dist > m.width:
		return []pos{below}
	case r.nextFloat() < m.skew:
		return []pos{below}
	case r.nextBool():
		return []pos{side}
	}
	return []pos{below}
}

func (m *mangroveRoots) put(t *treeRun, p pos, s bs) {
	if hasProp(s, "waterlogged") {
		s = withProp(s, "waterlogged", boolWord(btoi(t.d.at(p).has(fWater))))
	}
	t.rootSet.add(p)
	t.d.set(p.x, p.y, p.z, s)
}

func (m *mangroveRoots) placeRoot(t *treeRun, p pos) {
	if m.muddyIn[stateBlock[t.d.at(p)]] {
		m.put(t, p, m.muddy)
		return
	}
	if !m.canPlace(t, p) {
		return
	}
	m.put(t, p, m.root)
	up := p.step(dUp)
	if t.d.r.nextFloat() < m.aboveChance && t.d.at(up).isAir() {
		m.put(t, up, m.above)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
