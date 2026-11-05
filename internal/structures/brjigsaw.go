package structures

import (
	"slices"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// The data-driven jigsaw structures (trail ruins, abandoned camps, trial chambers) assemble with
// br::worldgen::JigsawStructure, which is Java's JigsawPlacement on Java's WorldgenRandom
// (NOTES-structures-phase2, "2026-11-05 (phase 8): br::worldgen jigsaw ... = Java's JigsawPlacement",
// cited BR below: start piece, its rotation and the first pieces identical to Java 26.1's on seed 100).
// Our own Go of that algorithm, from the study of Java 26.1's server (outside the repo):
//   - start: rotation = nextInt(4) (none, cw 90, 180, ccw 90), element = expanded pool[nextInt(size)],
//     the start jigsaw if named, the box centre (min + max) / 2, the start projected to the WG heightmap;
//   - children: a source's jigsaws shuffled (Java's backward Fisher-Yates) and stably sorted by selection
//     priority; for each, the target pool's elements shuffled then the fallback's, an empty element
//     ending the search, rotations shuffled, the target's jigsaws shuffled; the first that fits the free
//     space (the source's own box for targets inside it, else the space it was placed in) is placed;
//   - placement priority orders the queue, first in first out within a priority.

// javaRandom is Java's WorldgenRandom on the legacy LCG.
type javaRandom = worldgen.JavaRandom

// largeFeatureRandom is WorldgenRandom.setLargeFeatureSeed(levelSeed, cx, cz) (BR: "Java's WorldgenRandom
// (LCG, setLargeFeatureSeed)").
func largeFeatureRandom(seed int64, cx, cz int) *javaRandom {
	r := worldgen.NewJavaRandom(seed)
	a, b := r.NextLong(), r.NextLong()
	return worldgen.NewJavaRandom(int64(cx)*a ^ int64(cz)*b ^ seed)
}

// javaShuffle is Java's Util.shuffle: for i = n down to 2, swap a[i-1] with a[nextInt(i)].
func javaShuffle[T any](r *javaRandom, a []T) {
	for i := len(a); i > 1; i-- {
		j := int(r.NextInt(int32(i)))
		a[i-1], a[j] = a[j], a[i-1]
	}
}

// jsonJigsaw is a data-driven jigsaw structure's parameters (its JSON in the install).
type jsonJigsaw struct {
	id           string
	startPool    string
	startJigsaw  string
	maxDepth     int
	maxH, maxV   int
	startY       int    // Constant absolute start height.
	projection   string // Heightmap projection, "" for none.
	padBottom    int
	padTop       int
	biomeFilters []byte
}

// javaJigsaws are an element's jigsaws in Java's template order (blocks with NBT sorted by y, x, z),
// turned by rot and moved to p.
func (e *element) javaJigsaws(p [3]int, rot int) []jigsaw {
	js := e.jigsaws(p, rot)
	if e.template != nil {
		order := e.javaOrder()
		out := make([]jigsaw, len(js))
		for i, k := range order {
			out[i] = js[k]
		}
		js = out
	}
	return js
}

// javaOrder is the index order of the template's jigsaws sorted by local (y, x, z).
func (e *element) javaOrder() []int {
	j := e.template.jigsaws
	idx := make([]int, len(j))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		pa, pb := j[a].pos, j[b].pos
		if pa[1] != pb[1] {
			return pa[1] - pb[1]
		}
		if pa[0] != pb[0] {
			return pa[0] - pb[0]
		}
		return pa[2] - pb[2]
	})
	return idx
}

// shuffledJigsaws is getShuffledJigsawBlocks: shuffled, then stably sorted by selection priority.
func (e *element) shuffledJigsaws(r *javaRandom, p [3]int, rot int) []jigsaw {
	js := e.javaJigsaws(p, rot)
	javaShuffle(r, js)
	slices.SortStableFunc(js, func(a, b jigsaw) int { return b.selectPri - a.selectPri })
	return js
}

// javaCanAttach is Java's JigsawBlock.canAttach: fronts opposite, the source's target names the target,
// and an aligned joint keeps the top. A joint not given is aligned for a horizontal front, rollable for
// a vertical one.
func javaCanAttach(s, t *jigsaw) bool {
	if t.front != faceOpposite[s.front] || s.target != t.name {
		return false
	}
	aligned := s.aligned
	if !s.jointSet {
		aligned = s.front > 1
	}
	return !aligned || s.top == t.top
}

type javaQueued struct {
	prio, seq int
	p         *piece
	free      *region
	depth     int
}

type javaAssembly struct {
	r        *javaRandom
	pools    map[string]*pool
	maxDepth int
	height   func(x, z int) int // getFirstFreeHeight on WORLD_SURFACE_WG.
	pieces   []*piece
	queue    []javaQueued
	seq      int
}

// assembleJava is JigsawPlacement.addPieces for one start (BR). It returns nil when the start pool's
// pick is empty or the start is too near the world's limits.
func assembleJava(r *javaRandom, pools map[string]*pool, j *jsonJigsaw, cx, cz int, height func(x, z int) int) (pieces []*piece, stub [3]int) {
	start := pools[j.startPool]
	if start == nil || len(start.elems) == 0 {
		return nil, stub
	}
	rot := int(r.NextInt(4))
	e := start.elems[r.NextInt(int32(len(start.elems)))]
	if e == nil {
		return nil, stub
	}
	pos := [3]int{cx * 16, j.startY, cz * 16}
	anchor := pos
	if j.startJigsaw != "" {
		found := false
		for _, jg := range e.shuffledJigsaws(r, pos, rot) {
			if jg.name == j.startJigsaw {
				anchor, found = jg.pos, true
				break
			}
		}
		if !found {
			return nil, stub
		}
	}
	local := [3]int{anchor[0] - pos[0], anchor[1] - pos[1], anchor[2] - pos[2]}
	adj := [3]int{pos[0] - local[0], pos[1] - local[1], pos[2] - local[2]}
	b := e.bbox(adj, rot)
	centreX, centreZ := (b[3]+b[0])/2, (b[5]+b[2])/2
	bottom := adj[1]
	if j.projection != "" {
		bottom = pos[1] + height(centreX, centreZ)
	}
	const groundDelta = 1 // StructurePoolElement.getGroundLevelDelta.
	dy := bottom - (b[1] + groundDelta)
	b = b.shift(dy)
	adj[1] += dy
	if (j.padBottom != 0 || j.padTop != 0) && (b[1] < minY+j.padBottom || b[4] > maxY-j.padTop) {
		return nil, stub
	}
	centreY := bottom + local[1]
	stub = [3]int{centreX, centreY, centreZ}
	p := &piece{elem: e, pos: adj, rot: rot, box: b, ground: groundDelta}
	a := &javaAssembly{r: r, pools: pools, maxDepth: j.maxDepth, height: height, pieces: []*piece{p}}
	if j.maxDepth <= 0 {
		return a.pieces, stub
	}
	outer := box{centreX - j.maxH, max(centreY-j.maxV, minY+j.padBottom), centreZ - j.maxH,
		centreX + j.maxH, min(centreY+j.maxV, maxY-j.padTop), centreZ + j.maxH}
	free := &region{outer: []box{outer}, excl: []box{b}}
	a.children(p, free, 0)
	for len(a.queue) > 0 {
		best := 0
		for i, q := range a.queue {
			if q.prio > a.queue[best].prio || q.prio == a.queue[best].prio && q.seq < a.queue[best].seq {
				best = i
			}
		}
		q := a.queue[best]
		a.queue = append(a.queue[:best], a.queue[best+1:]...)
		a.children(q.p, q.free, q.depth)
	}
	return a.pieces, stub
}

func (r *region) fits(b box) bool {
	in := false
	for _, o := range r.outer {
		if o.contains(b) {
			in = true
		}
	}
	if !in {
		return false
	}
	for _, x := range r.excl {
		if x.intersects(b) {
			return false
		}
	}
	return true
}

// children is Placer.tryPlacingChildren (BR, Java's algorithm).
func (a *javaAssembly) children(p *piece, ctxFree *region, depth int) {
	sourceRigid := !p.elem.terrain
	sbox := p.box
	var ownFree *region
	for _, sj := range p.elem.shuffledJigsaws(a.r, p.pos, p.rot) {
		sj := sj
		st := faceStep[sj.front]
		target := [3]int{sj.pos[0] + st[0], sj.pos[1] + st[1], sj.pos[2] + st[2]}
		localY := sj.pos[1] - sbox[1]
		baseHeight, haveBase := 0, false
		pl := a.pools[sj.pool]
		if pl == nil {
			continue
		}
		fb := a.pools[pl.fallback]
		free := ctxFree
		if sbox.inside(target) {
			if ownFree == nil {
				ownFree = &region{outer: []box{sbox}}
			}
			free = ownFree
		}
		var cands []*element
		if depth != a.maxDepth {
			c := slices.Clone(pl.elems)
			javaShuffle(a.r, c)
			cands = append(cands, c...)
		}
		if fb != nil {
			c := slices.Clone(fb.elems)
			javaShuffle(a.r, c)
			cands = append(cands, c...)
		}
	elements:
		for _, e := range cands {
			if e == nil {
				break // The empty element ends the search.
			}
			rots := []int{0, 1, 2, 3}
			javaShuffle(a.r, rots)
			for _, rot := range rots {
				tjs := e.shuffledJigsaws(a.r, [3]int{}, rot)
				for k := range tjs {
					tj := &tjs[k]
					if !javaCanAttach(&sj, tj) {
						continue
					}
					raw := [3]int{target[0] - tj.pos[0], target[1] - tj.pos[1], target[2] - tj.pos[2]}
					rb := e.bbox(raw, rot)
					targetRigid := !e.terrain
					tLocalY := tj.pos[1]
					deltaY := localY - tLocalY + st[1]
					var boxY int
					if sourceRigid && targetRigid {
						boxY = sbox[1] + deltaY
					} else {
						if !haveBase {
							baseHeight, haveBase = a.height(sj.pos[0], sj.pos[2]), true
						}
						boxY = baseHeight - tLocalY
					}
					off := boxY - rb[1]
					tb := rb.shift(off)
					raw[1] += off
					if !free.fits(tb) {
						continue
					}
					free.excl = append(free.excl, tb)
					ground := e.groundDelta()
					if targetRigid {
						ground = p.ground - deltaY
					}
					child := &piece{elem: e, pos: raw, rot: rot, depth: depth + 1, box: tb, ground: ground, deltaY: tLocalY}
					a.pieces = append(a.pieces, child)
					if depth+1 <= a.maxDepth {
						a.seq++
						a.queue = append(a.queue, javaQueued{sj.placePrio, a.seq, child, free, depth + 1})
					}
					break elements
				}
			}
		}
	}
}

// groundDelta is getGroundLevelDelta: 1 for every element type the packs use.
func (e *element) groundDelta() int { return 1 }
