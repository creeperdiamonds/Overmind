package structures

import (
	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Stronghold layout, bury and building (clean-room spec stronghold/SPEC.md). Where strongholds start is
// strongholds.go.

const (
	// strongholdReach is how far, in chunks, a stronghold's pieces lie from its start chunk.
	strongholdReach = 9
	// shBuryReach is the reach of the bury adjustments: a piece's box grown by 12 blocks.
	shBuryReach = 10

	shMaxDepth = 50
	shMaxFoot  = 112
	shMinY     = 10 // A piece's box must start above this.
)

// Stronghold piece kinds, as the weight table and the op tables name them.
const (
	shCorridor = iota
	shCellBlock
	shTurnLeft
	shTurnRight
	shJunction
	shStraightStair
	shSpiralStair
	shFiveWay
	shChestCorridor
	shLibrary
	shPortalRoom
	shStub
	shNoKind = -1
)

var shKindNames = [...]string{"corridor", "cell_block", "turn_left", "turn_right", "junction_room", "straight_stair",
	"spiral_stair", "five_way_hall", "corridor_with_chest", "library", "portal_room", "stub_corridor"}

// Door types, as int(5) draws them (0 and 1 are open).
const (
	shDoorOpen = iota
	shDoorWooden
	shDoorBars
	shDoorIron
	shDoorNone // Stub corridors.
)

var shDoorNames = [...]string{"open", "wooden_door", "bars", "iron_door", ""}

// shPiece is one stronghold piece. flags: a corridor's side exits (x0 side, x1 side); a five-way hall's
// openings (x0 low, x0 high, x1 low, x1 high).
type shPiece struct {
	b      box
	o      orient
	kind   int
	depth  int
	door   int
	flags  [4]bool
	jtype  int  // Junction room type.
	tall   bool // Library: two floors.
	length int  // Stub corridor.
}

type stronghold struct {
	start  [2]int
	pieces []*shPiece
}

// shRecord is a row of the weight table.
type shRecord struct {
	kind, weight, max, minDepth, count int
}

func shTable() []*shRecord {
	return []*shRecord{
		{kind: shCorridor, weight: 40},
		{kind: shCellBlock, weight: 5, max: 5},
		{kind: shTurnLeft, weight: 20},
		{kind: shTurnRight, weight: 20},
		{kind: shJunction, weight: 10, max: 6},
		{kind: shStraightStair, weight: 5, max: 5},
		{kind: shSpiralStair, weight: 5, max: 5},
		{kind: shFiveWay, weight: 5, max: 4},
		{kind: shChestCorridor, weight: 5, max: 4},
		{kind: shLibrary, weight: 10, max: 2, minDepth: 5},
		{kind: shPortalRoom, weight: 10, max: 1, minDepth: 6},
	}
}

// shSizes are each kind's box at an exit: offsets across, up, along, and size across, up, along.
var shSizes = map[int][6]int{
	shCorridor:      {-1, -1, 0, 5, 5, 7},
	shChestCorridor: {-1, -1, 0, 5, 5, 7},
	shCellBlock:     {-1, -1, 0, 9, 5, 11},
	shTurnLeft:      {-1, -1, 0, 5, 5, 5},
	shTurnRight:     {-1, -1, 0, 5, 5, 5},
	shJunction:      {-4, -1, 0, 11, 7, 11},
	shStraightStair: {-1, -7, 0, 5, 11, 8},
	shSpiralStair:   {-1, -7, 0, 5, 11, 5},
	shFiveWay:       {-4, -3, 0, 10, 9, 11},
	shPortalRoom:    {-4, -1, 0, 11, 8, 16},
}

// strongholdAtStart is the stronghold that would start at chunk (cx, cz).
func (s *Structures) strongholdAtStart(cx, cz int) *stronghold {
	return s.strongholds.get([2]int{cx, cz}, func() any { return s.layoutStronghold(cx, cz) }).(*stronghold)
}

type shLayout struct {
	r       *worldgen.BedrockRandom
	h       *stronghold
	pending []*shPiece
	recs    []*shRecord
	last    *shRecord
	forced  int
	x0, z0  int
}

func (s *Structures) layoutStronghold(cx, cz int) *stronghold {
	r := worldgen.NewBedrockRandom(s.g.DecorationSeed(cx, cz))
	r.U32()
	l := &shLayout{r: r, h: &stronghold{start: [2]int{cx, cz}}, recs: shTable(), forced: shNoKind, x0: cx*16 + 2, z0: cz*16 + 2}
	o := orient(r.NextIntN(4))
	start := &shPiece{b: box{l.x0, 64, l.z0, l.x0 + 4, 74, l.z0 + 4}, o: o, kind: shSpiralStair, door: shDoorOpen}
	l.h.pieces = []*shPiece{start}
	l.forced = shFiveWay
	l.exits(start)
	for len(l.pending) > 0 {
		i := int(r.NextIntN(int32(len(l.pending))))
		p := l.pending[i]
		l.pending = append(l.pending[:i], l.pending[i+1:]...)
		l.exits(p)
	}
	l.portalFallback()
	all := boundsOf(l.boxes())
	dy := moveBelowSea(all, r)
	for _, p := range l.h.pieces {
		p.b = p.b.shift(dy)
	}
	return l.h
}

func (l *shLayout) boxes() []box {
	out := make([]box, len(l.h.pieces))
	for i, p := range l.h.pieces {
		out[i] = p.b
	}
	return out
}

func (l *shLayout) portalFallback() {
	ps := l.h.pieces
	for _, p := range ps {
		if p.kind == shPortalRoom {
			return
		}
	}
	if len(ps) <= 1 {
		return
	}
	last := ps[len(ps)-1]
	ps = ps[:len(ps)-1]
	l.h.pieces = ps
	B := ps[len(ps)-1].b
	if B[1] <= shMinY {
		return
	}
	// c is (a+b)/2 truncated toward zero, not floored, also at negative coordinates: the spec says so
	// (stronghold/SPEC.md 2.7, "division truncating toward zero").
	var b box
	switch last.o {
	case oS:
		c := (B[0] + B[3]) / 2
		b = box{c - 3, B[1], B[5], c + 7, B[1] + 7, B[5] + 15}
	case oW:
		c := (B[2] + B[5]) / 2
		b = box{B[0] - 15, B[1], c - 5, B[0], B[1] + 7, c + 5}
	case oN:
		c := (B[0] + B[3]) / 2
		b = box{c - 5, B[1], B[2] - 15, c + 5, B[1] + 7, B[2]}
	default:
		c := (B[2] + B[5]) / 2
		b = box{B[3], B[1], c - 3, B[3] + 15, B[1] + 7, c + 7}
	}
	l.h.pieces = append(ps, &shPiece{b: b, o: last.o, kind: shPortalRoom, depth: ps[len(ps)-1].depth, door: shDoorBars})
}

// forward, left and right lay the exits of a piece (the parent's box, from its low corner).
func (l *shLayout) forward(p *shPiece, xo, yo int) {
	b := p.b
	switch p.o {
	case oN:
		l.exit(b[0]+xo, b[1]+yo, b[2]-1, oN, p.depth)
	case oS:
		l.exit(b[0]+xo, b[1]+yo, b[5]+1, oS, p.depth)
	case oW:
		l.exit(b[0]-1, b[1]+yo, b[2]+xo, oW, p.depth)
	case oE:
		l.exit(b[3]+1, b[1]+yo, b[2]+xo, oE, p.depth)
	}
}

func (l *shLayout) left(p *shPiece, yo, zo int) {
	b := p.b
	if p.o == oN || p.o == oS {
		l.exit(b[0]-1, b[1]+yo, b[2]+zo, oW, p.depth)
	} else {
		l.exit(b[0]+zo, b[1]+yo, b[2]-1, oN, p.depth)
	}
}

func (l *shLayout) right(p *shPiece, yo, zo int) {
	b := p.b
	if p.o == oN || p.o == oS {
		l.exit(b[3]+1, b[1]+yo, b[2]+zo, oE, p.depth)
	} else {
		l.exit(b[0]+zo, b[1]+yo, b[5]+1, oS, p.depth)
	}
}

func (l *shLayout) exits(p *shPiece) {
	ne := p.o == oN || p.o == oE
	switch p.kind {
	case shSpiralStair, shChestCorridor, shStraightStair, shCellBlock:
		l.forward(p, 1, 1)
	case shCorridor:
		l.forward(p, 1, 1)
		if p.flags[0] {
			l.left(p, 1, 2)
		}
		if p.flags[1] {
			l.right(p, 1, 2)
		}
	case shTurnLeft:
		if ne {
			l.left(p, 1, 1)
		} else {
			l.right(p, 1, 1)
		}
	case shTurnRight:
		if ne {
			l.right(p, 1, 1)
		} else {
			l.left(p, 1, 1)
		}
	case shJunction:
		l.forward(p, 4, 1)
		l.left(p, 1, 4)
		l.right(p, 1, 4)
	case shFiveWay:
		a, b := 3, 5
		if p.o == oW || p.o == oN {
			a, b = 5, 3
		}
		l.forward(p, 5, 1)
		if p.flags[0] {
			l.left(p, a, 1)
		}
		if p.flags[1] {
			l.left(p, b, 7)
		}
		if p.flags[2] {
			l.right(p, a, 1)
		}
		if p.flags[3] {
			l.right(p, b, 7)
		}
	}
}

func (l *shLayout) free(b box) bool {
	for _, p := range l.h.pieces {
		if p.b.intersects(b) {
			return false
		}
	}
	return true
}

// exit lays out the piece at an exit of a piece of depth k, drawing from a copy of the main stream.
func (l *shLayout) exit(x, y, z int, d orient, k int) {
	if k > shMaxDepth || abs(x-l.x0) > shMaxFoot || abs(z-l.z0) > shMaxFoot {
		return
	}
	// Intended: each exit draws its kind, door and flags from a copy of the whole random, so the main
	// stream does not advance here and all the exits of one piece start from the same state. Only the
	// start's two draws (a word, the orientation), the pending-list picks (layoutStronghold) and the final height draw advance it. The spec's 149-piece layout
	// (TestStrongholdLayoutMatchesSpec) depends on this.
	cp := *l.r
	r := &cp
	D := k + 1
	var p *shPiece
	if l.forced != shNoKind {
		kind := l.forced
		l.forced = shNoKind
		p = l.build(kind, x, y, z, d, D, r)
	}
	if p == nil {
		var ok bool
		p, ok = l.pick(x, y, z, d, D, r)
		if !ok {
			return
		}
	}
	if p == nil {
		p = l.stub(x, y, z, d, D)
	}
	if p != nil {
		l.h.pieces = append(l.h.pieces, p)
		l.pending = append(l.pending, p)
	}
}

// pick draws a kind from the weight table and builds it. ok is false when the table has no limited
// kind left (then no piece at all, not even a stub).
func (l *shLayout) pick(x, y, z int, d orient, D int, r *worldgen.BedrockRandom) (*shPiece, bool) {
	left := false
	total := 0
	for _, rec := range l.recs {
		if rec.max > 0 && rec.count < rec.max {
			left = true
		}
		total += rec.weight
	}
	if !left {
		return nil, false
	}
	for range 5 {
		v := int(r.NextIntN(int32(total)))
		for i, rec := range l.recs {
			v -= rec.weight
			if v >= 0 {
				continue
			}
			if rec.max != 0 && rec.count >= rec.max || D < rec.minDepth || rec == l.last && len(l.recs) > 1 {
				break
			}
			if p := l.build(rec.kind, x, y, z, d, D, r); p != nil {
				rec.count++
				l.last = rec
				if rec.max != 0 && rec.count >= rec.max {
					l.recs = append(l.recs[:i:i], l.recs[i+1:]...)
				}
				return p, true
			}
		}
	}
	return nil, true
}

// build makes a piece of a kind at an exit if its box is free, with its door and flags.
func (l *shLayout) build(kind, x, y, z int, d orient, D int, r *worldgen.BedrockRandom) *shPiece {
	ok := func(b box) bool { return b[1] > shMinY && l.free(b) }
	var b box
	tall := false
	if kind == shLibrary {
		b = orientBox(x, y, z, -4, -1, 0, 14, 11, 15, d)
		if tall = ok(b); !tall {
			b = orientBox(x, y, z, -4, -1, 0, 14, 6, 15, d)
			if !ok(b) {
				return nil
			}
		}
	} else {
		sz := shSizes[kind]
		b = orientBox(x, y, z, sz[0], sz[1], sz[2], sz[3], sz[4], sz[5], d)
		if !ok(b) {
			return nil
		}
	}
	p := &shPiece{b: b, o: d, kind: kind, depth: D, tall: tall, door: shDoorBars}
	if kind == shPortalRoom {
		return p
	}
	switch r.NextIntN(5) {
	case 2:
		p.door = shDoorWooden
	case 3:
		p.door = shDoorBars
	case 4:
		p.door = shDoorIron
	default:
		p.door = shDoorOpen
	}
	switch kind {
	case shCorridor:
		p.flags[0] = r.NextIntN(2) == 0
		p.flags[1] = r.NextIntN(2) == 0
	case shFiveWay:
		p.flags[0] = r.NextBool()
		p.flags[1] = r.NextBool()
		p.flags[2] = r.NextBool()
		p.flags[3] = r.NextIntN(3) > 0
	case shJunction:
		p.jtype = int(r.NextIntN(5))
	}
	return p
}

// stub is the short corridor closing an exit that ran into a piece at its own floor level.
func (l *shLayout) stub(x, y, z int, d orient, D int) *shPiece {
	front := orientBox(x, y, z, -1, -1, 0, 5, 5, 4, d)
	var hit *shPiece
	for _, p := range l.h.pieces {
		if p.b.intersects(front) {
			hit = p
			break
		}
	}
	if hit == nil || hit.b[1] != front[1] {
		return nil
	}
	n := 0
	for m := 2; m >= 1; m-- {
		if !hit.b.intersects(orientBox(x, y, z, -1, -1, 0, 5, 5, m, d)) {
			n = m + 1
			break
		}
	}
	if n == 0 {
		plane := front
		switch d {
		case oN:
			plane[2], plane[5] = z+1, z
		case oS:
			plane[2], plane[5] = z, z-1
		case oW:
			plane[0], plane[3] = x+1, x
		case oE:
			plane[0], plane[3] = x, x-1
		}
		if hit.b.intersects(plane) {
			return nil
		}
		n = 1
	}
	b := orientBox(x, y, z, -1, -1, 0, 5, 5, n, d)
	if b[1] <= 1 {
		return nil
	}
	return &shPiece{b: b, o: d, kind: shStub, depth: D, door: shDoorNone, length: n}
}

// strongholdStarts are the stronghold starts within reach chunks of chunk (cx, cz) on both axes: the
// ring starts in ring order, then the grid starts of the cells holding the four corner chunks.
func (s *Structures) strongholdStarts(cx, cz, reach int) [][2]int {
	var out [][2]int
	near := func(c [2]int) bool { return abs(c[0]-cx) <= reach && abs(c[1]-cz) <= reach }
	has := func(c [2]int) bool {
		for _, o := range out {
			if o == c {
				return true
			}
		}
		return false
	}
	for _, c := range s.rings() {
		if near(c) && !has(c) {
			out = append(out, c)
		}
	}
	for _, d := range [4][2]int{{-reach, -reach}, {-reach, reach}, {reach, -reach}, {reach, reach}} {
		if c, ok := s.gridStronghold(cx+d[0], cz+d[1]); ok && near(c) && !has(c) {
			out = append(out, c)
		}
	}
	return out
}

// strongholdAdjustments are the bury adjustments of the stronghold pieces near chunk (cx, cz).
func (s *Structures) strongholdAdjustments(cx, cz int, into []worldgen.TerrainAdjustment) []worldgen.TerrainAdjustment {
	for _, st := range s.strongholdStarts(cx, cz, shBuryReach) {
		for _, p := range s.strongholdAtStart(st[0], st[1]).pieces {
			b := p.b
			if (b[0]-12)>>4 <= cx && cx <= (b[3]+12)>>4 && (b[2]-12)>>4 <= cz && cz <= (b[5]+12)>>4 {
				into = append(into, worldgen.TerrainAdjustment{Mode: worldgen.AdaptLegacyBury, Box: b})
			}
		}
	}
	return into
}

// placeStrongholds builds every stronghold piece touching the chunk, with the chunk's stronghold stream.
func (s *Structures) placeStrongholds(w *writer) {
	cb := w.chunkBox()
	for _, st := range s.strongholdStarts(w.cx, w.cz, strongholdReach) {
		for _, p := range s.strongholdAtStart(st[0], st[1]).pieces {
			if p.b[0] <= cb[3] && p.b[3] >= cb[0] && p.b[2] <= cb[5] && p.b[5] >= cb[2] {
				buildShPiece(w, p)
			}
		}
	}
}

// shOpsKey names a piece's op list in the table.
func shOpsKey(p *shPiece) string {
	name := shKindNames[p.kind]
	switch p.kind {
	case shTurnLeft, shTurnRight:
		if p.o == oS || p.o == oW {
			return name + "[orientation S or W]"
		}
		return name + "[orientation N or E]"
	case shStub:
		return name + "[length " + string(rune('0'+p.length)) + "]"
	case shJunction:
		return name + "[type " + string(rune('0'+p.jtype)) + "]"
	case shLibrary:
		if p.tall {
			return name + "[tall]"
		}
		return name + "[short]"
	}
	return name
}

var shFlagNames = map[int][4]string{
	shCorridor: {"side_exit_x0", "side_exit_x1"},
	shFiveWay:  {"opening_x0_low", "opening_x0_high", "opening_x1_low", "opening_x1_high"},
}

func buildShPiece(w *writer, p *shPiece) {
	t := pieceData()
	c := &ctx{w: w, p: &opiece{p.b, p.o}, r: w.rng}
	flag := func(name string) bool {
		for i, n := range shFlagNames[p.kind] {
			if n == name {
				return p.flags[i]
			}
		}
		return false
	}
	runOps(c, t.shPieces[shOpsKey(p)], flag, func(c *ctx, op *pieceOp) {
		switch op.Op {
		case "stone_box":
			shStoneBox(c, op.From, op.To)
		case "door":
			kind := op.Type
			if kind == "own" {
				kind = shDoorNames[p.door]
			}
			for _, d := range t.shDoors[kind] {
				c.set(op.At[0]+d.At[0], op.At[1]+d.At[1], op.At[2]+d.At[2], blockOf(d.Block, d.Facing, c.p))
			}
		case "chest":
			wx, wy, wz := c.p.world(op.At[0], op.At[1], op.At[2])
			if !w.in(wx, wy, wz) || ridName(w.mainAt(wx, wy, wz)) == "chest" {
				return // No seed is drawn for a chest outside the chunk or already there.
			}
			w.put(wx, wy, wz, specState(t.misc["chest"]))
			w.bes = append(w.bes, lootEntity("Chest", wx, wy, wz, op.Loot, c.r.NextInt()))
		case "portal_frames":
			shPortalFrames(c)
		}
	})
}

// shStoneBox is the stronghold's wall: its shell random stone bricks (one draw per shell cell, in the
// chunk or not), its inside air.
func shStoneBox(c *ctx, from, to [3]int) {
	t := pieceData().shStates
	cracked, mossy, infested, plain := specState(t["cracked"]), specState(t["mossy"]), specState(t["infested"]), specState(t["stone_bricks"])
	air := airRID()
	for y := from[1]; y <= to[1]; y++ {
		for x := from[0]; x <= to[0]; x++ {
			for z := from[2]; z <= to[2]; z++ {
				if x != from[0] && x != to[0] && y != from[1] && y != to[1] && z != from[2] && z != to[2] {
					c.set(x, y, z, air)
					continue
				}
				f := c.r.NextFloat()
				switch {
				case f < 0.2:
					c.set(x, y, z, cracked)
				case f < 0.5:
					c.set(x, y, z, mossy)
				case f < 0.55:
					c.set(x, y, z, infested)
				default:
					c.set(x, y, z, plain)
				}
			}
		}
	}
}

// shFrames are the portal room's end portal frames: local x, z and facing, in draw order.
var shFrames = [12]struct {
	x, z int
	f    string
}{{4, 8, "+z"}, {5, 8, "+z"}, {6, 8, "+z"}, {4, 12, "-z"}, {5, 12, "-z"}, {6, 12, "-z"},
	{3, 9, "+x"}, {3, 10, "+x"}, {3, 11, "+x"}, {7, 9, "-x"}, {7, 10, "-x"}, {7, 11, "-x"}}

func shPortalFrames(c *ctx) {
	var eyes [12]bool
	all := true
	for i := range eyes {
		eyes[i] = c.r.NextFloat() > 0.9
		all = all && eyes[i]
	}
	for i, fr := range shFrames {
		key := "FRAME eye=false"
		if eyes[i] {
			key = "FRAME eye=true"
		}
		c.set(fr.x, 3, fr.z, blockOf(key, fr.f, c.p))
	}
	if all {
		portal := specState(pieceData().shStates["end_portal"])
		for x := 4; x <= 6; x++ {
			for z := 9; z <= 11; z++ {
				c.set(x, 3, z, portal)
			}
		}
	}
	if c.in(5, 3, 6) {
		wx, wy, wz := c.p.world(5, 3, 6)
		c.w.put(wx, wy, wz, specState(pieceData().shStates["spawner"]))
		c.w.bes = append(c.w.bes, spawnerEntity(wx, wy, wz, "minecraft:silverfish"))
	}
}
