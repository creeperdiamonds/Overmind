package structures

import (
	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// The legacy code-pool jigsaw placer (N2 1.2 to 1.6), which villages, bastions and ancient cities use,
// on the structure's MT random.

// box is an inclusive block box: min x, y, z, max x, y, z.
type box [6]int

func (b box) contains(o box) bool {
	return b[0] <= o[0] && o[3] <= b[3] && b[1] <= o[1] && o[4] <= b[4] && b[2] <= o[2] && o[5] <= b[5]
}

func (b box) intersects(o box) bool {
	return b[0] <= o[3] && o[0] <= b[3] && b[1] <= o[4] && o[1] <= b[4] && b[2] <= o[5] && o[2] <= b[5]
}

func (b box) inside(p [3]int) bool {
	return b[0] <= p[0] && p[0] <= b[3] && b[1] <= p[1] && p[1] <= b[4] && b[2] <= p[2] && p[2] <= b[5]
}

func (b box) shift(dy int) box { b[1] += dy; b[4] += dy; return b }

// element is a pool element (N2 1.2): a template, a feature, or empty (nil in a pool).
type element struct {
	name     string
	template *Template // nil for a feature element.
	terrain  bool      // Terrain-matching projection (vt5); the ancient city's are rigid.
	legacy   bool      // A legacy single element: its air blocks are not placed.
	// bedrockLegacy marks an element of a code-registered pool, placed by LegacyStructureTemplate::place
	// (P9.1): jigsaw blocks are skipped, and its rules run with the chunk's rule random.
	bedrockLegacy bool
	rules         []blockRule
	procs         []brProcessor // A data-driven element's processors (brprocess.go).
	// abandonedTargets: the element's tag rules point its jigsaws at the abandoned pools (P10.1).
	abandonedTargets bool
	// missing is an element whose template file the install lacks: it keeps its weight slots and draws
	// its rotation shuffle, but has no jigsaws, so it never attaches (P16.3 rule 1).
	missing bool
	// mob is a bastion mob element (bastion/mobs/*.mcstructure): a 1 x 3 x 1 piece that places no
	// blocks; its mob is recorded, not spawned (P16.3 rule 2).
	mob string
}

// rotate turns a local point by rot clockwise quarter turns (N2 1.2).
func rotate(p [3]int, rot int) [3]int {
	switch rot & 3 {
	case 1:
		return [3]int{-p[2], p[1], p[0]}
	case 2:
		return [3]int{-p[0], p[1], -p[2]}
	case 3:
		return [3]int{p[2], p[1], -p[0]}
	}
	return p
}

var faceStep = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
var faceOpposite = [6]int{1, 0, 3, 2, 5, 4}
var faceCW = [6]int{0, 1, 5, 4, 2, 3} // north->east->south->west.

func rotateFace(f, rot int) int {
	for range rot & 3 {
		f = faceCW[f]
	}
	return f
}

// size is the element's unrotated size; a feature element is one block.
func (e *element) size() [3]int {
	if e.template == nil {
		return [3]int{1, 1, 1}
	}
	return e.template.Size
}

// bbox is the element's box at pos turned by rot (N2 1.2).
func (e *element) bbox(p [3]int, rot int) box {
	s := e.size()
	sx, sy, sz := s[0], s[1], s[2]
	if rot&1 == 1 {
		sx, sz = sz, sx
	}
	var b box
	switch rot & 3 {
	case 0:
		b = box{p[0], 0, p[2], p[0] + sx - 1, 0, p[2] + sz - 1}
	case 1:
		b = box{p[0] + 1 - sx, 0, p[2], p[0], 0, p[2] + sz - 1}
	case 2:
		b = box{p[0] + 1 - sx, 0, p[2] + 1 - sz, p[0], 0, p[2]}
	default:
		b = box{p[0], 0, p[2] + 1 - sz, p[0] + sx - 1, 0, p[2]}
	}
	b[1], b[4] = p[1], p[1]+sy-1
	return b
}

// jigsaws are the element's jigsaws in template order, turned and moved to pos (vt1).
func (e *element) jigsaws(p [3]int, rot int) []jigsaw {
	if e.missing {
		return nil
	}
	if e.template == nil { // A feature element's one jigsaw (N2 1.9's model): facing down, top south.
		return []jigsaw{{pos: p, front: 0, top: 3, name: "minecraft:bottom", target: "minecraft:empty", pool: "minecraft:empty"}}
	}
	out := make([]jigsaw, len(e.template.jigsaws))
	for i, j := range e.template.jigsaws {
		q := rotate(j.pos, rot)
		j.pos = [3]int{q[0] + p[0], q[1] + p[1], q[2] + p[2]}
		j.front, j.top = rotateFace(j.front, rot), rotateFace(j.top, rot)
		out[i] = j
	}
	return out
}

// canAttach is N2 1.2's test.
func canAttach(s, t *jigsaw) bool {
	if s.target != t.name || t.front != faceOpposite[s.front] {
		return false
	}
	if s.front <= 1 && s.aligned {
		return s.top == t.top
	}
	return true
}

// pool is a registered pool: elements expanded by weight in order, nil for the empty element (N2 1.2).
type pool struct {
	elems    []*element
	fallback string
}

// piece is a placed element.
type piece struct {
	elem  *element
	pos   [3]int
	rot   int
	depth int
	box   box
	// deltaY lifts the terrain adaptation's base above the box bottom: 1 for the start piece, the target
	// jigsaw's local y for a child (N2 phase 3 round 2, terrain adaptation).
	deltaY int
	// ground is a data-driven piece's ground level delta (Java's PoolElementStructurePiece).
	ground int
	// junctions are a data-driven piece's jigsaw junctions: x, ground y, z (Java's JigsawJunction).
	junctions [][3]int
}

type region struct{ outer, excl []box }

type queued struct {
	prio, seq int
	p         *piece
	free      int
}

// assembly is one run of the placer.
type assembly struct {
	r        *worldgen.BedrockRandom
	pools    map[string]*pool
	maxDepth int
	height   func(x, z int) int
	// expansion is the expansion hack (villages; not in the notes' 1.6, measured: see village.go).
	expansion bool
	pieces    []*piece
	free      []region
	queue     []queued
	seq       int
}

func shuffleJigsaws(r *worldgen.BedrockRandom, a []jigsaw) {
	for i := 1; i < len(a); i++ {
		if j := int(r.U32() % uint32(i+1)); j != i {
			a[i], a[j] = a[j], a[i]
		}
	}
}

// assemble is addPieces (N2 1.4) from a chosen start element.
func assemble(r *worldgen.BedrockRandom, pools map[string]*pool, start *element, x, z, rot int, startJigsaw string,
	maxDepth, maxDist int, height func(x, z int) int) []*piece {
	return assembleWith(r, pools, start, x, z, rot, startJigsaw, maxDepth, maxDist, height, false)
}

// assembleWith is assemble with the expansion hack chosen.
func assembleWith(r *worldgen.BedrockRandom, pools map[string]*pool, start *element, x, z, rot int, startJigsaw string,
	maxDepth, maxDist int, height func(x, z int) int, expansion bool) []*piece {
	a := &assembly{r: r, pools: pools, maxDepth: maxDepth, height: height, expansion: expansion}
	pos := [3]int{x, 0, z}
	var anchor [3]int
	if startJigsaw != "" {
		js := start.jigsaws(pos, rot)
		shuffleJigsaws(r, js)
		for _, j := range js {
			if j.name == startJigsaw {
				anchor = [3]int{j.pos[0] - pos[0], j.pos[1] - pos[1], j.pos[2] - pos[2]}
				break
			}
		}
	}
	apos := [3]int{x - anchor[0], 0, z - anchor[2]}
	b := start.bbox(apos, rot)
	cx := b[0] + (b[3]-b[0]+1)/2
	cz := b[2] + (b[5]-b[2]+1)/2
	dy := height(cx, cz) - 1 - (anchor[1] + b[1])
	b = b.shift(dy)
	apos[1] += dy
	p := &piece{elem: start, pos: apos, rot: rot, box: b, deltaY: 1}
	a.pieces = append(a.pieces, p)
	cy := b[1] + (b[4]-b[1]+1)/2
	d := maxDist
	a.free = []region{{outer: []box{{cx - d, cy - d, cz - d, cx + d, cy + d, cz + d}}, excl: []box{b}}}
	a.push(0, p, 0)
	for len(a.queue) > 0 {
		best := 0
		for i, q := range a.queue {
			if q.prio > a.queue[best].prio || q.prio == a.queue[best].prio && q.seq > a.queue[best].seq {
				best = i
			}
		}
		q := a.queue[best]
		a.queue = append(a.queue[:best], a.queue[best+1:]...)
		a.children(q.p, q.free)
	}
	return a.pieces
}

func (a *assembly) push(prio int, p *piece, free int) {
	a.seq--
	a.queue = append(a.queue, queued{prio, a.seq, p, free})
}

// children is tryPlacingChildren (N2 1.5).
func (a *assembly) children(p *piece, free int) {
	if p.depth > a.maxDepth {
		return
	}
	sbox := p.box
	js := p.elem.jigsaws(p.pos, p.rot)
	shuffleJigsaws(a.r, js)
	sortBySelection(js)
	for i := range js {
		sj := &js[i]
		st := faceStep[sj.front]
		target := [3]int{sj.pos[0] + st[0], sj.pos[1] + st[1], sj.pos[2] + st[2]}
		poolName := sj.pool
		if p.elem.abandonedTargets {
			poolName = abandonedPool(poolName)
		}
		pl := a.pools[poolName]
		if pl == nil || len(pl.elems) == 0 {
			continue
		}
		var fb *pool
		if pl.fallback != "" {
			if f := a.pools[pl.fallback]; f != nil && len(f.elems) > 0 {
				fb = f
			}
		}
		if p.depth != a.maxDepth {
			ok := a.tryPool(p, sbox, sj, target, pl, free)
			if fb == nil || ok {
				continue
			}
		}
		if fb != nil {
			a.tryPool(p, sbox, sj, target, fb, free)
		}
	}
}

// sortBySelection is std::sort by selection priority, descending (N2 1.5). Legacy templates have all
// zero, which leaves the order alone; a stable sort keeps that case exact.
func sortBySelection(js []jigsaw) {
	for i := 1; i < len(js); i++ {
		for j := i; j > 0 && js[j].selectPri > js[j-1].selectPri; j-- {
			js[j], js[j-1] = js[j-1], js[j]
		}
	}
}

// tryPool is N2 1.6.
func (a *assembly) tryPool(p *piece, sbox box, sj *jigsaw, target [3]int, pl *pool, fi int) bool {
	inside := sbox.inside(target)
	h := a.height(sj.pos[0], sj.pos[2])
	idx := make([]int, len(pl.elems))
	for i := range idx {
		idx[i] = i
	}
	shuffleInts(a.r, idx)
	for _, i := range idx {
		e := pl.elems[i]
		if e == nil {
			return true
		}
		rots := []int{1, 2, 3, 0}
		shuffleInts(a.r, rots)
		for _, rot := range rots {
			tjs := e.jigsaws([3]int{}, rot)
			shuffleJigsaws(a.r, tjs)
			expandTo := 0
			if a.expansion {
				expandTo = a.expandTo(e, rot, tjs)
			}
			for k := range tjs {
				tj := &tjs[k]
				if !canAttach(sj, tj) {
					continue
				}
				dyStep := 0
				switch sj.front {
				case 0:
					dyStep = -1
				case 1:
					dyStep = 1
				}
				base := sbox[1]
				if p.elem.terrain || e.terrain {
					base = h - 1
				}
				raw := [3]int{target[0] - tj.pos[0], target[1] - tj.pos[1], target[2] - tj.pos[2]}
				tb := e.bbox(raw, rot)
				newMin := (sj.pos[1] - (sbox[1] + tj.pos[1])) + base + dyStep
				sh := newMin - tb[1]
				tb = tb.shift(sh)
				if expandTo > 0 {
					tb[4] = max(tb[4], tb[1]+max(expandTo+1, tb[4]-tb[1]))
				}
				raw[1] += sh
				if inside { // One new index per attempt, as coded (N2 1.6).
					fi++
					if fi > len(a.free)-1 {
						a.free = append(a.free, region{outer: []box{sbox}})
					} else {
						a.free[fi].outer = append(a.free[fi].outer, sbox)
					}
				}
				reg := &a.free[fi]
				ok := false
				for _, o := range reg.outer {
					if o.contains(tb) {
						ok = true
						break
					}
				}
				for _, x := range reg.excl {
					if ok && x.intersects(tb) {
						ok = false
					}
				}
				if !ok {
					continue
				}
				reg.excl = append(reg.excl, tb)
				child := &piece{elem: e, pos: raw, rot: rot, depth: p.depth + 1, box: tb, deltaY: tj.pos[1]}
				a.pieces = append(a.pieces, child)
				a.push(tj.placePrio, child, fi)
				return true
			}
		}
	}
	return false
}

// expandTo is the expansion hack's height for a target element (Java's Placer: a target no more than 16
// tall grows to hold the tallest element of the pools its inward-pointing jigsaws draw from).
func (a *assembly) expandTo(e *element, rot int, tjs []jigsaw) int {
	hb := e.bbox([3]int{}, rot)
	if hb[4]-hb[1]+1 > 16 {
		return 0
	}
	best := 0
	for _, tj := range tjs {
		st := faceStep[tj.front]
		if !hb.inside([3]int{tj.pos[0] + st[0], tj.pos[1] + st[1], tj.pos[2] + st[2]}) {
			continue
		}
		for _, name := range []string{tj.pool, ""} {
			pl := a.pools[tj.pool]
			if name == "" && pl != nil {
				pl = a.pools[pl.fallback]
			}
			if pl == nil {
				continue
			}
			for _, c := range pl.elems {
				if c != nil {
					best = max(best, c.size()[1])
				}
			}
		}
	}
	return best
}
