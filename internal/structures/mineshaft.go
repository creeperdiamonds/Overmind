package structures

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Mineshafts, normal and mesa (clean-room spec mineshaft/SPEC.md): a room with corridors, crossings and
// stairs laid out depth first from the start chunk's own stream, sunk to a random depth, and built one
// chunk at a time from the chunk's mineshaft stream.

const (
	// msSpanLow and msSpanHigh bound every piece of a mineshaft, in blocks from its start chunk's corner
	// on each axis (the layout's limits: a foot at most 80 from the room, plus the longest piece).
	msSpanLow  = -97
	msSpanHigh = 101
	// msReach is the farthest, in chunks, a start can be from a chunk its pieces touch.
	msReach = 7

	msMaxDepth   = 8
	msMaxFoot    = 80
	msDeepDarkID = 190
)

// msStarts are the start chunks (on one axis) whose pieces can touch chunk c.
func msStarts(c int) (lo, hi int) {
	return -floorDiv(msSpanHigh-c*16, 16), floorDiv(c*16+15-msSpanLow, 16)
}

// mesaBiomes are the badlands family: their mineshafts are mesa ones.
var mesaBiomes = idSet(37, 38, 39, 165, 166, 167)

type msKind uint8

const (
	msRoom msKind = iota
	msCorridor
	msCrossing
	msStairs
)

// msPiece is one piece of a mineshaft. dir is the direction it was laid going (the room has none).
type msPiece struct {
	b        box
	kind     msKind
	dir      orient
	depth    int
	sections int  // Corridor: its 5-block sections.
	rails    bool // Corridor: rails along it.
	spider   bool // Corridor: a cave spider spawner and its webs.
}

// mineshaft is an assembled mineshaft. pieces[0] is the room; entrances are the openings cut into the
// room's walls where its corridors leave.
type mineshaft struct {
	start     [2]int
	pieces    []*msPiece
	entrances []box
	mesa      bool
}

// spiderKey names a spider corridor (start chunk, piece index) in one chunk.
type spiderKey struct {
	start [2]int
	idx   int
	chunk [2]int
}

// mineshaftAtStart is the mineshaft that would start at chunk (cx, cz) (whether or not it is a start).
func (s *Structures) mineshaftAtStart(cx, cz int) *mineshaft {
	return s.mineshafts.get([2]int{cx, cz}, func() any { return s.layoutMineshaft(cx, cz) }).(*mineshaft)
}

// msLayout lays one mineshaft out.
type msLayout struct {
	r    *worldgen.BedrockRandom
	m    *mineshaft
	room box
}

func (s *Structures) layoutMineshaft(cx, cz int) *mineshaft {
	r := worldgen.NewBedrockRandom(uint32(s.mineB)*uint32(int32(cz)) ^ uint32(s.mineA)*uint32(int32(cx)) ^ s.seed32)
	r.Consume(3) // The start test's three draws.
	m := &mineshaft{start: [2]int{cx, cz}}
	m.mesa = mesaBiomes[s.g.BiomeID(cx*16, s.g.NoiseHeight(cx*16, cz*16), cz*16)]
	l := &msLayout{r: r, m: m}

	za, yb, xc := int(r.NextIntN(6)), int(r.NextIntN(6)), int(r.NextIntN(6))
	x0, z0 := cx*16+2, cz*16+2
	l.room = box{x0, 50, z0, x0 + 7 + xc, 54 + yb, z0 + 7 + za}
	m.pieces = append(m.pieces, &msPiece{b: l.room, kind: msRoom, dir: oNone})
	l.roomExits()

	all := l.room
	for _, p := range m.pieces {
		all = boundsOf([]box{all, p.b})
	}
	var dy int
	if m.mesa {
		qx, qz := all[0]+(all[3]-all[0]+1)>>1, all[2]+(all[5]-all[2]+1)>>1
		h := s.g.PreliminarySurface(qx, qz)
		y := 63
		if 63 < h {
			y = 63 + int(r.NextIntN(int32(h-63+1)))
		}
		dy = y - (all[1] + (all[4]-all[1]+1)>>1)
	} else {
		dy = moveBelowSea(all, r)
	}
	for _, p := range m.pieces {
		p.b = p.b.shift(dy)
	}
	for i := range m.entrances {
		m.entrances[i] = m.entrances[i].shift(dy)
	}
	return m
}

func (l *msLayout) roomExits() {
	p := float32(1.0)
	if l.m.mesa {
		p = 0.5
	}
	if l.r.NextFloat() > p {
		return
	}
	b := l.room
	hs := max(b[4]-b[1]+1-4, 1)
	xn, zn := b[3]-b[0]+1, b[5]-b[2]+1
	for side := range 4 {
		n := xn
		if side >= 2 {
			n = zn
		}
		for pos := 0; pos < n; {
			pos += int(l.r.NextIntN(int32(n)))
			if pos+3 > n {
				break
			}
			y := b[1] + int(l.r.NextIntN(int32(hs))) + 1
			var np *msPiece
			switch side {
			case 0:
				np = l.add(b[0]+pos, y, b[2]-1, oN, 0)
			case 1:
				np = l.add(b[0]+pos, y, b[5]+1, oS, 0)
			case 2:
				np = l.add(b[0]-1, y, b[2]+pos, oW, 0)
			default:
				np = l.add(b[3]+1, y, b[2]+pos, oE, 0)
			}
			if np != nil {
				q := np.b
				switch side {
				case 0:
					l.m.entrances = append(l.m.entrances, box{q[0], q[1], b[2], q[3], q[4], b[2] + 1})
				case 1:
					l.m.entrances = append(l.m.entrances, box{q[0], q[1], b[5] - 1, q[3], q[4], b[5]})
				case 2:
					l.m.entrances = append(l.m.entrances, box{b[0], q[1], q[2], b[0] + 1, q[4], q[5]})
				default:
					l.m.entrances = append(l.m.entrances, box{b[3] - 1, q[1], q[2], b[3], q[4], q[5]})
				}
			}
			pos += 4
		}
	}
}

func (l *msLayout) free(b box) bool {
	for _, p := range l.m.pieces {
		if p.b.intersects(b) {
			return false
		}
	}
	return true
}

// add lays a piece at a foot going d from a piece of depth k, and its exits; nil when none fits.
func (l *msLayout) add(x, y, z int, d orient, k int) *msPiece {
	if k > msMaxDepth || abs(x-l.room[0]) > msMaxFoot || abs(z-l.room[2]) > msMaxFoot {
		return nil
	}
	r := l.r
	roll := r.NextIntN(100)
	var p *msPiece
	switch {
	case roll >= 80:
		top := 2
		if r.NextIntN(4) == 0 {
			top = 6
		}
		b := orientBox(x, y, z, -1, 0, 0, 5, top+1, 5, d)
		if !l.free(b) {
			return nil
		}
		p = &msPiece{b: b, kind: msCrossing, dir: d}
	case roll >= 70:
		b := orientBox(x, y, z, 0, -5, 0, 3, 8, 9, d)
		if !l.free(b) {
			return nil
		}
		p = &msPiece{b: b, kind: msStairs, dir: d}
	default:
		n := int(r.NextIntN(3)) + 2
		for sec := n; sec >= 1; sec-- {
			if b := orientBox(x, y, z, 0, 0, 0, 3, 3, 5*sec, d); l.free(b) {
				p = &msPiece{b: b, kind: msCorridor, dir: d, sections: sec}
				break
			}
		}
		if p == nil {
			return nil
		}
		p.rails = r.NextIntN(3) == 0
		if !p.rails {
			p.spider = r.NextIntN(23) == 0
		}
	}
	p.depth = k + 1
	l.m.pieces = append(l.m.pieces, p)
	switch p.kind {
	case msCorridor:
		l.corridorExits(p)
	case msCrossing:
		l.crossingExits(p)
	case msStairs:
		l.stairsExit(p)
	}
	return p
}

func (l *msLayout) corridorExits(p *msPiece) {
	b, k, r := p.b, p.depth, l.r
	e := r.NextIntN(4)
	y := b[1] - 1 + int(r.NextIntN(3))
	switch p.dir {
	case oN:
		switch {
		case e <= 1:
			l.add(b[0], y, b[2]-1, oN, k)
		case e == 2:
			l.add(b[0]-1, y, b[2], oW, k)
		default:
			l.add(b[3]+1, y, b[2], oE, k)
		}
	case oS:
		switch {
		case e <= 1:
			l.add(b[0], y, b[5]+1, oS, k)
		case e == 2:
			l.add(b[0]-1, y, b[5]-3, oW, k)
		default:
			l.add(b[3]+1, y, b[5]-3, oE, k)
		}
	case oW:
		switch {
		case e <= 1:
			l.add(b[0]-1, y, b[2], oW, k)
		case e == 2:
			l.add(b[0], y, b[2]-1, oN, k)
		default:
			l.add(b[0], y, b[5]+1, oS, k)
		}
	case oE:
		switch {
		case e <= 1:
			l.add(b[3]+1, y, b[2], oE, k)
		case e == 2:
			l.add(b[3]-3, y, b[2]-1, oN, k)
		default:
			l.add(b[3]-3, y, b[5]+1, oS, k)
		}
	}
	if k >= msMaxDepth {
		return
	}
	if p.dir == oW || p.dir == oE {
		for x := b[0] + 3; x+3 <= b[3]; x += 5 {
			switch r.NextIntN(5) {
			case 0:
				l.add(x, b[1], b[2]-1, oN, k+1)
			case 1:
				l.add(x, b[1], b[5]+1, oS, k+1)
			}
		}
		return
	}
	for z := b[2] + 3; z+3 <= b[5]; z += 5 {
		switch r.NextIntN(5) {
		case 0:
			l.add(b[0]-1, b[1], z, oW, k+1)
		case 1:
			l.add(b[3]+1, b[1], z, oE, k+1)
		}
	}
}

func (l *msLayout) crossingExits(p *msPiece) {
	b, k, y := p.b, p.depth, p.b[1]
	north := func(y int) { l.add(b[0]+1, y, b[2]-1, oN, k) }
	south := func(y int) { l.add(b[0]+1, y, b[5]+1, oS, k) }
	west := func(y int) { l.add(b[0]-1, y, b[2]+1, oW, k) }
	east := func(y int) { l.add(b[3]+1, y, b[2]+1, oE, k) }
	switch p.dir {
	case oN:
		north(y)
		west(y)
		east(y)
	case oS:
		south(y)
		west(y)
		east(y)
	case oW:
		north(y)
		south(y)
		west(y)
	case oE:
		north(y)
		south(y)
		east(y)
	}
	if b[4]-b[1]+1 > 3 {
		for _, exit := range []func(int){north, west, east, south} {
			if l.r.NextBool() {
				exit(y + 4)
			}
		}
	}
}

func (l *msLayout) stairsExit(p *msPiece) {
	b, k := p.b, p.depth
	switch p.dir {
	case oN:
		l.add(b[0], b[1], b[2]-1, oN, k)
	case oS:
		l.add(b[0], b[1], b[5]+1, oS, k)
	case oW:
		l.add(b[0]-1, b[1], b[2], oW, k)
	case oE:
		l.add(b[3]+1, b[1], b[2], oE, k)
	}
}

// Building (mineshaft/SPEC.md section 5 with ADDENDUM-1.md).

// placeMineshafts builds every mineshaft piece touching the chunk, with the chunk's mineshaft stream.
func (s *Structures) placeMineshafts(w *writer) {
	xlo, xhi := msStarts(w.cx)
	zlo, zhi := msStarts(w.cz)
	x0, z0 := w.cx*16, w.cz*16
	for sx := xlo; sx <= xhi; sx++ {
		for sz := zlo; sz <= zhi; sz++ {
			if !s.mineshaftAt(sx, sz) {
				continue
			}
			m := s.mineshaftAtStart(sx, sz)
			for i, p := range m.pieces {
				if p.b[0] <= x0+15 && p.b[3] >= x0 && p.b[2] <= z0+15 && p.b[5] >= z0 {
					s.buildMsPiece(w, m, i, p)
				}
			}
		}
	}
}

// msBlocks are a mineshaft type's wood, and the blocks its pieces leave alone (addendum rule F): any
// state of the type's planks, log and fence, and of iron_chain.
type msBlocks struct {
	planks, log, fence, chain uint32
	protected, chains         map[uint32]bool
}

// keep says whether a cell holding rid is protected from the piece writes rule F covers.
func (b *msBlocks) keep(rid uint32) bool { return b.protected[rid] }

// msWoods are the normal and the mesa mineshaft's blocks.
var msWoods = sync.OnceValue(func() (out [2]*msBlocks) {
	t := pieceData().msStates
	registry.load()
	statesOf := func(rid uint32, into map[uint32]bool) {
		if rid == noBlock {
			return
		}
		for _, st := range registry.byName[ridName(rid)] {
			into[st.rid] = true
		}
	}
	for i, kind := range [2]string{" normal", " mesa"} {
		b := &msBlocks{planks: specState(t["planks"+kind]), log: specState(t["log"+kind]), fence: specState(t["fence"+kind]),
			chain: specState(t["chain"]), protected: map[uint32]bool{}, chains: map[uint32]bool{}}
		for _, rid := range [4]uint32{b.planks, b.log, b.fence, b.chain} {
			statesOf(rid, b.protected)
		}
		statesOf(b.chain, b.chains)
		out[i] = b
	}
	return out
})

func mineshaftBlocks(mesa bool) *msBlocks {
	if mesa {
		return msWoods()[1]
	}
	return msWoods()[0]
}

func (s *Structures) buildMsPiece(w *writer, m *mineshaft, idx int, p *msPiece) {
	if s.msSkipped(w, p.b) {
		return
	}
	wood := mineshaftBlocks(m.mesa)
	switch p.kind {
	case msRoom:
		buildMsRoom(w, p.b, m.entrances, wood)
	case msCrossing:
		buildMsCrossing(w, p.b, wood)
	case msStairs:
		c := &ctx{w: w, p: &opiece{p.b, p.dir}, r: w.rng, keep: wood.keep}
		runOps(c, pieceData().msStairs, nil, nil)
	case msCorridor:
		s.buildMsCorridor(w, m, idx, p, wood)
	}
}

// msSkipped is the skip test: the piece's box grown by one and clipped to the chunk is deep dark at its
// centre, or has a liquid on its faces. The piece meets the chunk in x and z, so only an empty y range
// leaves nothing to test.
func (s *Structures) msSkipped(w *writer, b box) bool {
	g, ok := w.clip(box{b[0] - 1, b[1] - 1, b[2] - 1, b[3] + 1, b[4] + 1, b[5] + 1})
	if !ok {
		return false
	}
	// The centre's divisions truncate toward zero, not floor: the spec says so (mineshaft/SPEC.md 5.1,
	// "-269.5 becomes -269"), so (a+b)/2 is meant here, also at negative coordinates.
	if s.g.BiomeID((g[0]+g[3])/2, (g[1]+g[4])/2, (g[2]+g[5])/2) == msDeepDarkID {
		return true
	}
	for y := g[1]; y <= g[4]; y++ {
		for x := g[0]; x <= g[3]; x++ {
			if w.liquidAt(x, y, g[2]) || w.liquidAt(x, y, g[5]) {
				return true
			}
		}
		for z := g[2]; z <= g[5]; z++ {
			if w.liquidAt(g[0], y, z) || w.liquidAt(g[3], y, z) {
				return true
			}
		}
	}
	for x := g[0]; x <= g[3]; x++ {
		for z := g[2]; z <= g[5]; z++ {
			if w.liquidAt(x, g[1], z) || w.liquidAt(x, g[4], z) {
				return true
			}
		}
	}
	return false
}

// airBox empties a world box inside the chunk, leaving the cells wood protects.
func airBox(w *writer, wood *msBlocks, x0, y0, z0, x1, y1, z1 int) {
	air := airRID()
	for x := max(x0, w.cx*16); x <= min(x1, w.cx*16+15); x++ {
		for z := max(z0, w.cz*16); z <= min(z1, w.cz*16+15); z++ {
			for y := max(y0, w.lo); y <= min(y1, w.hi); y++ {
				if !wood.keep(w.mainAt(x, y, z)) {
					w.set(x, y, z, air, false)
				}
			}
		}
	}
}

func buildMsRoom(w *writer, b box, entrances []box, wood *msBlocks) {
	airBox(w, wood, b[0], b[1]+1, b[2], b[3], min(b[1]+3, b[4]), b[5])
	for _, e := range entrances {
		airBox(w, wood, e[0], e[4]-2, e[2], e[3], e[4], e[5])
	}
	base := b[1] + 4
	if base > b[4] {
		return
	}
	fx, fy, fz := float32(b[3]-b[0]+1), float32(b[4]-base+1), float32(b[5]-b[2]+1)
	mx, mz := float32(b[0])+fx/2, float32(b[2])+fz/2
	hx, hz := float32(fx*0.5), float32(fz*0.5)
	air := airRID()
	for y := base; y <= b[4]; y++ {
		for x := max(b[0], w.cx*16); x <= min(b[3], w.cx*16+15); x++ {
			for z := max(b[2], w.cz*16); z <= min(b[5], w.cz*16+15); z++ {
				if !w.in(x, y, z) {
					continue
				}
				a := float32(float32(x)-mx) / hx
				v := float32(y-base) / fy
				c := float32(float32(z)-mz) / hz
				sum := float32(float32(a*a) + float32(v*v))
				sum = float32(sum + float32(c*c))
				if sum <= 1.05 && !wood.keep(w.mainAt(x, y, z)) {
					w.set(x, y, z, air, false)
				}
			}
		}
	}
}

func buildMsCrossing(w *writer, b box, wood *msBlocks) {
	x0, y0, z0, x1, y1, z1 := b[0], b[1], b[2], b[3], b[4], b[5]
	if y1-y0+1 > 3 {
		airBox(w, wood, x0+1, y0, z0, x1-1, y0+2, z1)
		airBox(w, wood, x0, y0, z0+1, x1, y0+2, z1-1)
		airBox(w, wood, x0+1, y1-2, z0, x1-1, y1, z1)
		airBox(w, wood, x0, y1-2, z0+1, x1, y1, z1-1)
		airBox(w, wood, x0+1, y0+3, z0+1, x1-1, y0+3, z1-1)
	} else {
		airBox(w, wood, x0+1, y0, z0, x1-1, y1, z1)
		airBox(w, wood, x0, y0, z0+1, x1, y1, z1-1)
	}
	for _, c := range [4][2]int{{x0 + 1, z0 + 1}, {x0 + 1, z1 - 1}, {x1 - 1, z0 + 1}, {x1 - 1, z1 - 1}} {
		if w.mainAt(c[0], y1+1, c[1]) != airRID() {
			for y := y0; y <= y1; y++ {
				if !wood.keep(w.mainAt(c[0], y, c[1])) {
					w.put(c[0], y, c[1], wood.planks)
				}
			}
		}
	}
	// The floor is not among the writes rule F lists (as the corridor's floor, rule D, is exempt).
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			if w.interior(x, y0-1, z) && !w.solidAt(x, y0-1, z) {
				w.put(x, y0-1, z, wood.planks)
			}
		}
	}
}

// msWebChances are the cobwebs by each support, in the order they draw (addendum rule C): local x, the
// offset along local z from the section's centre, and the chance.
var msWebChances = [8]struct {
	x, dz int
	p     float32
}{{0, -1, 0.1}, {2, -2, 0.1}, {0, 0, 0.1}, {2, 0, 0.1}, {0, -3, 0.05}, {2, -3, 0.05}, {0, 1, 0.05}, {2, 1, 0.05}}

func (s *Structures) buildMsCorridor(w *writer, m *mineshaft, idx int, p *msPiece, wood *msBlocks) {
	c := &ctx{w: w, p: &opiece{p.b, p.dir}, r: w.rng, keep: wood.keep}
	r := w.rng
	L := 5*p.sections - 1
	air := airRID()
	web := specState(pieceData().msStates["cobweb"])
	key := spiderKey{m.start, idx, [2]int{w.cx, w.cz}}
	placed := p.spider && s.spiderPlacedBefore(key, p.b)
	// filled reads a local cell as it really is, in the chunk or in a neighbour's terrain (rule B).
	filled := func(x, y, z int) bool {
		wx, wy, wz := c.p.world(x, y, z)
		return !s.msAirAt(w, wx, wy, wz)
	}

	c.box(0, 0, 0, 2, 1, L, air, air, false)
	c.maybeBox(0.8, 0, 2, 0, 2, 2, L, air, air, false, false)
	if p.spider {
		c.maybeBox(0.6, 0, 0, 0, 2, 1, L, web, web, false, true)
	}
	for sec := range p.sections {
		z := 2 + 5*sec
		if filled(0, 3, z) && filled(1, 3, z) && filled(2, 3, z) {
			c.set(0, 0, z, wood.fence)
			c.set(0, 1, z, wood.fence)
			c.set(2, 0, z, wood.fence)
			c.set(2, 1, z, wood.fence)
			if r.NextIntN(4) == 0 {
				c.set(0, 2, z, wood.planks)
				c.set(2, 2, z, wood.planks)
			} else {
				c.box(0, 2, z, 2, 2, z, wood.planks, wood.planks, false)
				c.maybe(0.05, 1, 2, z-1, blockOf("TORCH", "-z", c.p))
				c.maybe(0.05, 1, 2, z+1, blockOf("TORCH", "+z", c.p))
			}
		}
		for _, wc := range msWebChances {
			lz := z + wc.dz
			// The test column, one cell further along local +z: more than a chunk away it draws nothing;
			// in the chunk it must be interior at local y 3; in a neighbour it counts as interior.
			tx, ty, tz := c.p.world(wc.x, 3, lz+1)
			if abs(tx>>4-w.cx) > 1 || abs(tz>>4-w.cz) > 1 {
				continue
			}
			if tx>>4 == w.cx && tz>>4 == w.cz && !w.interior(tx, ty, tz) {
				continue
			}
			if r.NextFloat() < wc.p {
				wx, wy, wz := c.p.world(wc.x, 2, lz)
				if w.in(wx, wy, wz) && filledNeighbours(w, wx, wy, wz) >= 2 {
					c.put(wx, wy, wz, web)
				}
			}
		}
		if p.spider && !placed {
			zz := z - 1 + int(r.NextIntN(3))
			wx, wy, wz := c.p.world(1, 0, zz)
			if w.in(wx, wy, wz) && wy < w.hi && w.interior(wx, wy, wz) {
				w.put(wx, wy, wz, specState(pieceData().msStates["spawner"]))
				w.bes = append(w.bes, spawnerEntity(wx, wy, wz, "minecraft:cave_spider"))
				placed = true
				w.spawned[key] = true
			}
		}
	}
	// The floor (rule D): planks over air or a chain, where the corridor's own floor level is interior.
	for x := 0; x <= 2; x++ {
		for z := 0; z <= L; z++ {
			if !c.interior(x, 0, z) {
				continue
			}
			if cur := c.get(x, -1, z); cur == air || wood.chains[cur] {
				wx, wy, wz := c.p.world(x, -1, z)
				w.put(wx, wy, wz, wood.planks)
			}
		}
	}
	pillarZ := []int{2}
	if p.sections > 1 {
		pillarZ = append(pillarZ, L-2)
	}
	for _, z := range pillarZ {
		for _, x := range [2]int{0, 2} {
			if c.get(x, -1, z) == wood.planks {
				wx, wy, wz := c.p.world(x, -1, z)
				msPillar(w, wx, wy, wz, wood)
			}
		}
	}
	if p.rails {
		rail := railRID(p.dir == oN || p.dir == oS)
		for z := 0; z <= L; z++ {
			if c.get(1, -1, z) != air && c.solid(1, -1, z) {
				f := r.NextFloat()
				chance := float32(0.9)
				if c.interior(1, 0, z) {
					chance = 0.7
				}
				if f < chance {
					c.set(1, 0, z, rail)
				}
			}
		}
	}
	// No chest minecarts: BDS picks them in a later pass with a random of its own (rule A), so nothing
	// here draws for them.
}

// msAirAt says whether a world cell is air: in the chunk as it stands, elsewhere in the terrain of the
// chunk holding it before any structure (the addendum's stand-in for BDS's live neighbour, rule B). The
// neighbours' terrain comes from the generator's shared cache, so a chunk never depends on the order
// chunks are made in.
func (s *Structures) msAirAt(w *writer, x, y, z int) bool {
	if x>>4 == w.cx && z>>4 == w.cz {
		return w.mainAt(x, y, z) == airRID()
	}
	k := [2]int{x >> 4, z >> 4}
	b, ok := w.bases[k]
	if !ok {
		if w.bases == nil {
			w.bases = map[[2]int]worldgen.BaseCells{}
		}
		b = s.g.BaseCells(k[0], k[1])
		w.bases[k] = b
	}
	return b.Air(x, y, z)
}

// filledNeighbours counts the face neighbours of a cell that are in the chunk and not air.
func filledNeighbours(w *writer, x, y, z int) int {
	n, air := 0, airRID()
	for _, d := range faceStep {
		if nx, ny, nz := x+d[0], y+d[1], z+d[2]; w.in(nx, ny, nz) && w.mainAt(nx, ny, nz) != air {
			n++
		}
	}
	return n
}

// msPillar props a corridor's floor plank at (x, y, z) (SPEC 5.7 with addendum rule G): a log down to
// the first cell within 11 below that is neither replaceable nor lava, unless that cell is a rail; or
// else a fence and chain up to the first solid block within 21 above that does not fall.
func msPillar(w *writer, x, y, z int, wood *msBlocks) {
	if !w.in(x, y, z) {
		return
	}
	down, up := true, true
	for d := 1; down || up; d++ {
		if down {
			yy := y - d
			switch {
			case yy < w.lo:
				down = false
			case !w.replaceable(x, yy, z) || w.isLava(x, yy, z):
				if !w.isLava(x, yy, z) && !isRail(w.mainAt(x, yy, z)) {
					for ly := yy + 1; ly <= y-1; ly++ {
						w.put(x, ly, z, wood.log)
					}
					return
				}
				down = false
			default:
				down = d < 11 && yy > w.lo+1
			}
		}
		if up {
			yy := y + d
			ok := yy <= w.hi && w.replaceable(x, yy, z)
			if yy <= w.hi && !ok && w.solidAt(x, yy, z) && !w.falling(x, yy, z) {
				w.put(x, y+1, z, wood.fence)
				for cy := y + 2; cy <= yy-1; cy++ {
					w.put(x, cy, z, wood.chain)
				}
				return
			}
			up = d < 21 && ok && yy < w.hi
		}
	}
}

// isRail says whether a block is a rail of any kind.
func isRail(rid uint32) bool {
	n := ridName(rid)
	return n == "rail" || strings.HasSuffix(n, "_rail")
}

// The spawner mark: whether a spider corridor already placed its spawner in a chunk built before this
// one (mineshaft/SPEC.md 5.6).
//
// BDS keeps a "spawner placed" mark on the corridor while it builds the corridor's chunks, so a corridor
// gets one spawner however many chunks it crosses. Our chunks are made from the seed and their position
// alone, in any order and on any thread, with nothing shared between them, so a chunk cannot see a mark
// a neighbour left. Instead the spec fixes an order of a corridor's chunks, chunk x ascending, then chunk
// z ascending (ADDENDUM-1.md rule E, fitted; the same order for every corridor), and a chunk works the
// mark out by replaying the chunks before it in that order: a chunk counts as having placed the spawner
// when, made on its own (its base terrain, then the structure types before mineshafts, then its
// mineshaft pass), it places it. BDS's real order is the order its chunks happen to be built, which
// differs between runs; this one puts every spawner of the two reference mineshafts at BDS's cell.
//
// The replay ends: replaying chunk A asks, for each spider corridor in A, only about chunks strictly
// before A in that one global order, and only within that corridor's box, so every chain of replays moves
// one way through a finite set of chunks. Replays are deterministic, so their results are cached per
// (start, piece index, chunk), with a mark per replayed chunk; the cache is dropped whole when it would
// pass spiderCacheMax entries (it changes speed, never a result).

const spiderCacheMax = 1 << 14

// spiderPlacedBefore says whether a chunk before key.chunk, in the corridor's order, placed its spawner.
func (s *Structures) spiderPlacedBefore(key spiderKey, b box) bool {
	for ax := b[0] >> 4; ax <= b[3]>>4; ax++ {
		for az := b[2] >> 4; az <= b[5]>>4; az++ {
			if ax == key.chunk[0] && az == key.chunk[1] {
				return false
			}
			if s.spiderPlacedIn(spiderKey{key.start, key.idx, [2]int{ax, az}}) {
				return true
			}
		}
	}
	return false
}

// spiderReplayed marks a chunk whose spawner marks are in spiderDone.
func spiderReplayed(cx, cz int) spiderKey { return spiderKey{idx: -1, chunk: [2]int{cx, cz}} }

// spiderPlacedIn says whether the corridor places its spawner in its chunk when that chunk is made on
// its own.
func (s *Structures) spiderPlacedIn(k spiderKey) bool {
	s.spiderMu.Lock()
	if s.spiderDone[spiderReplayed(k.chunk[0], k.chunk[1])] {
		v := s.spiderDone[k]
		s.spiderMu.Unlock()
		return v
	}
	s.spiderMu.Unlock()
	placed := s.replaySpiders(k.chunk[0], k.chunk[1])
	s.spiderMu.Lock()
	defer s.spiderMu.Unlock()
	if s.spiderDone == nil || len(s.spiderDone)+len(placed)+1 > spiderCacheMax {
		s.spiderDone = map[spiderKey]bool{}
	}
	for pk, v := range placed {
		s.spiderDone[pk] = v
	}
	s.spiderDone[spiderReplayed(k.chunk[0], k.chunk[1])] = true
	return placed[k]
}

// replaySpiders makes chunk (cx, cz) on its own up to its mineshafts and returns the spawners it placed.
func (s *Structures) replaySpiders(cx, cz int) map[spiderKey]bool {
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	s.g.GenerateBase(cx, cz, c)
	seed := s.g.DecorationSeed(cx, cz)
	w := &writer{cx: cx, cz: cz, c: c, rng: worldgen.NewBedrockRandom(seed), seed: seed, spawned: map[spiderKey]bool{},
		lo: minY, hi: maxY}
	for _, place := range []func(*writer){s.placeVillages, s.placeStrongholds, s.placeScattered, s.placeMineshafts} {
		w.reseed()
		place(w)
	}
	return w.spawned
}
