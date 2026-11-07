package structures

import (
	"sync"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// The Nether fortress, our own Go of NOTES-structures-phase2 P15.2 and P16.2 (cited P152, P162) with the
// piece geometry of the study's P16-fortress-geometry.txt (outside the repo; transcribed here in our own
// form under ruling 43). Validated in the study on seed 100's (4,19) fortress: 133 of 133 pieces, the
// box exact, 99.57% of 41,103 cells, 2 of 2 chest loot seeds and both blaze spawners.
//
// Bedrock's changes from the reference model: the start continues the region random (nether.go), the
// orientation codes 0 S, 1 W, 2 N, 3 E, and a child foot more than 112 blocks from the start's corner
// gets an end filler that is added to the piece list and the pending list (P152). Blazes are mobs
// (stage 4): the spawners hold "minecraft:blaze" and no mob is spawned.

// Fortress piece kinds; the weight tables are P162's, in their order.
const (
	nfStraight = iota
	nfCrossing
	nfRoomCrossing
	nfStairsRoom
	nfThrone
	nfEntrance
	nfCorridor
	nfCorridorCrossing
	nfRightTurn
	nfLeftTurn
	nfCorridorStairs
	nfTBalcony
	nfStalkRoom
	nfFiller
)

type nfRecord struct {
	kind, weight, max, count int
	inRow                    bool
}

// nfBridge and nfCastle are P162's two weight tables (weight, max placements, allowed in a row).
func nfBridge() []*nfRecord {
	return []*nfRecord{{kind: nfStraight, weight: 30, inRow: true}, {kind: nfCrossing, weight: 10, max: 4},
		{kind: nfRoomCrossing, weight: 10, max: 4}, {kind: nfStairsRoom, weight: 10, max: 3},
		{kind: nfThrone, weight: 5, max: 2}, {kind: nfEntrance, weight: 5, max: 1}}
}

func nfCastle() []*nfRecord {
	return []*nfRecord{{kind: nfCorridor, weight: 25, inRow: true}, {kind: nfCorridorCrossing, weight: 15, max: 5},
		{kind: nfRightTurn, weight: 5, max: 10}, {kind: nfLeftTurn, weight: 5, max: 10},
		{kind: nfCorridorStairs, weight: 10, max: 3, inRow: true}, {kind: nfTBalcony, weight: 7, max: 2},
		{kind: nfStalkRoom, weight: 5, max: 2}}
}

// nfSizes are the pieces' boxes from their foot (P162 createPiece: orientBox offsets and size).
var nfSizes = [...][6]int{
	nfStraight: {-1, -3, 0, 5, 10, 19}, nfCrossing: {-8, -3, 0, 19, 10, 19}, nfRoomCrossing: {-2, 0, 0, 7, 9, 7},
	nfStairsRoom: {-2, 0, 0, 7, 11, 7}, nfThrone: {-2, 0, 0, 7, 8, 9}, nfEntrance: {-5, -3, 0, 13, 14, 13},
	nfCorridor: {-1, 0, 0, 5, 7, 5}, nfCorridorCrossing: {-1, 0, 0, 5, 7, 5}, nfRightTurn: {-1, 0, 0, 5, 7, 5},
	nfLeftTurn: {-1, 0, 0, 5, 7, 5}, nfCorridorStairs: {-1, -7, 0, 5, 14, 10}, nfTBalcony: {-3, 0, 0, 9, 7, 9},
	nfStalkRoom: {-5, -3, 0, 13, 14, 13}, nfFiller: {-1, -3, 0, 5, 10, 8},
}

const (
	nfMaxDepth = 30  // P162.
	nfFar      = 112 // A foot farther than this from the start's corner gets a filler (P152).
	nfMoveLow  = 48  // moveInsideHeights(48, 70): Nether sea level 32 + 16 and + 38 (P152).
	nfMoveHigh = 70
	nfStartY   = 64
	// fortressReach is how far (chunks) a piece reaches from its start, read from the placer: a foot up to
	// nfFar from the start piece's corner (cx*16+2) gets a piece of at most 19 blocks, whose exit past nfFar
	// gets an end filler of 8, so pieces lie within cx*16+2 +- 138: chunks cx-9 .. cx+8. Measured 9 on
	// 1,176 starts (TestPiecesStayWithinReach checks the span).
	fortressReach = 9
)

type nfPiece struct {
	opiece
	kind, depth int
	chest       bool  // A turn's isNeedingChest.
	seed        int32 // An end filler's own seed.
}

type fortress struct {
	pieces []*nfPiece
}

type nfAssembler struct {
	r               *worldgen.BedrockRandom
	start           *nfPiece
	pieces, pending []*nfPiece
	bridge, castle  []*nfRecord
	previous        *nfRecord
}

// fortressAtStart assembles the fortress of a start (P152): the start bridge crossing at
// (cx*16+2, 64, cz*16+2), its children, the pending loop, then the y move.
func (s *Structures) fortressAtStart(st Start) *fortress {
	r := st.random()
	o := orient(r.NextIntN(4))
	x, z := st.CX*16+2, st.CZ*16+2
	start := &nfPiece{opiece: opiece{b: box{x, nfStartY, z, x + 18, nfStartY + 9, z + 18}, o: o}, kind: nfCrossing}
	as := &nfAssembler{r: r, start: start, pieces: []*nfPiece{start}, bridge: nfBridge(), castle: nfCastle()}
	as.children(start)
	for len(as.pending) > 0 {
		i := int(r.NextIntN(int32(len(as.pending))))
		p := as.pending[i]
		as.pending = append(as.pending[:i], as.pending[i+1:]...)
		as.children(p)
	}
	boxes := make([]box, len(as.pieces))
	for i, p := range as.pieces {
		boxes[i] = p.b
	}
	all := boundsOf(boxes)
	k := nfMoveLow
	if j := nfMoveHigh - nfMoveLow + 1 - (all[4] - all[1] + 1); j > 1 {
		k += int(r.NextIntN(int32(j)))
	}
	for _, p := range as.pieces {
		p.b = p.b.shift(k - all[1])
	}
	return &fortress{pieces: as.pieces}
}

func (as *nfAssembler) okBox(b box) bool {
	if b[1] <= 10 {
		return false
	}
	for _, p := range as.pieces {
		if p.b.intersects(b) {
			return false
		}
	}
	return true
}

// create is a piece's createPiece and constructor draws (P162).
func (as *nfAssembler) create(k, x, y, z int, o orient, d int) *nfPiece {
	sz := nfSizes[k]
	b := orientBox(x, y, z, sz[0], sz[1], sz[2], sz[3], sz[4], sz[5], o)
	if !as.okBox(b) {
		return nil
	}
	p := &nfPiece{opiece: opiece{b: b, o: o}, kind: k, depth: d}
	switch k {
	case nfLeftTurn, nfRightTurn:
		p.chest = as.r.NextIntN(3) == 0
	case nfFiller:
		p.seed = as.r.NextInt()
	}
	return p
}

// generate is generatePiece (P162): up to five weighted tries, then an end filler.
func (as *nfAssembler) generate(recs *[]*nfRecord, x, y, z int, o orient, d int) *nfPiece {
	total, any := 0, false
	for _, rec := range *recs {
		if rec.max > 0 && rec.count < rec.max {
			any = true
		}
		total += rec.weight
	}
	if any && total > 0 && d <= nfMaxDepth {
		for range 5 {
			v := int(as.r.NextIntN(int32(total)))
			for i, rec := range *recs {
				v -= rec.weight
				if v >= 0 {
					continue
				}
				if rec.max != 0 && rec.count >= rec.max || rec == as.previous && !rec.inRow {
					break
				}
				p := as.create(rec.kind, x, y, z, o, d)
				if p == nil {
					continue
				}
				rec.count++
				as.previous = rec
				if rec.max != 0 && rec.count >= rec.max {
					*recs = append((*recs)[:i], (*recs)[i+1:]...)
				}
				return p
			}
		}
	}
	return as.create(nfFiller, x, y, z, o, d)
}

// add is generateAndAddPiece (P162, with Bedrock's far filler of P152).
func (as *nfAssembler) add(x, y, z int, o orient, depth int, castle bool) {
	var p *nfPiece
	if abs(x-as.start.b[0]) <= nfFar && abs(z-as.start.b[2]) <= nfFar {
		recs := &as.bridge
		if castle {
			recs = &as.castle
		}
		p = as.generate(recs, x, y, z, o, depth+1)
	} else {
		p = as.create(nfFiller, x, y, z, o, depth) // BEDROCK: added to both lists.
	}
	if p != nil {
		as.pieces = append(as.pieces, p)
		as.pending = append(as.pending, p)
	}
}

// The exits (P162 generateChildForward, Left, Right).
func (as *nfAssembler) forward(p *nfPiece, xo, yo int, castle bool) {
	b := p.b
	switch p.o {
	case oN:
		as.add(b[0]+xo, b[1]+yo, b[2]-1, oN, p.depth, castle)
	case oS:
		as.add(b[0]+xo, b[1]+yo, b[5]+1, oS, p.depth, castle)
	case oW:
		as.add(b[0]-1, b[1]+yo, b[2]+xo, oW, p.depth, castle)
	case oE:
		as.add(b[3]+1, b[1]+yo, b[2]+xo, oE, p.depth, castle)
	}
}

func (as *nfAssembler) left(p *nfPiece, yo, zo int, castle bool) {
	b := p.b
	switch p.o {
	case oN, oS:
		as.add(b[0]-1, b[1]+yo, b[2]+zo, oW, p.depth, castle)
	case oW, oE:
		as.add(b[0]+zo, b[1]+yo, b[2]-1, oN, p.depth, castle)
	}
}

func (as *nfAssembler) right(p *nfPiece, yo, zo int, castle bool) {
	b := p.b
	switch p.o {
	case oN, oS:
		as.add(b[3]+1, b[1]+yo, b[2]+zo, oE, p.depth, castle)
	case oW, oE:
		as.add(b[0]+zo, b[1]+yo, b[5]+1, oS, p.depth, castle)
	}
}

// children is a piece's addChildren (P162).
func (as *nfAssembler) children(p *nfPiece) {
	switch p.kind {
	case nfCrossing:
		as.forward(p, 8, 3, false)
		as.left(p, 3, 8, false)
		as.right(p, 3, 8, false)
	case nfStraight:
		as.forward(p, 1, 3, false)
	case nfCorridorStairs:
		as.forward(p, 1, 0, true)
	case nfTBalcony:
		zo := 1
		if p.o == oW || p.o == oN {
			zo = 5
		}
		a := as.r.NextIntN(8) > 0
		as.left(p, 0, zo, a)
		b := as.r.NextIntN(8) > 0
		as.right(p, 0, zo, b)
	case nfEntrance:
		as.forward(p, 5, 3, true)
	case nfCorridorCrossing:
		as.forward(p, 1, 0, true)
		as.left(p, 0, 1, true)
		as.right(p, 0, 1, true)
	case nfLeftTurn:
		as.left(p, 0, 1, true)
	case nfCorridor:
		as.forward(p, 1, 0, true)
	case nfRightTurn:
		as.right(p, 0, 1, true)
	case nfStalkRoom:
		as.forward(p, 5, 3, true)
		as.forward(p, 5, 11, true)
	case nfRoomCrossing:
		as.forward(p, 2, 0, false)
		as.left(p, 0, 2, false)
		as.right(p, 0, 2, false)
	case nfStairsRoom:
		as.right(p, 6, 2, false)
	}
}

// placeFortress post-processes the fortress's pieces that reach the chunk, in piece order.
func (s *Structures) placeFortress(w *writer, st Start) {
	f := s.fortresses.get([2]int{st.CX, st.CZ}, func() any { return s.fortressAtStart(st) }).(*fortress)
	cb := w.chunkBox()
	for _, p := range f.pieces {
		if p.b.intersects(cb) {
			s.nfPost(w, p)
		}
	}
}

// nfBlocks are the fortress's blocks (P162, Bedrock names).
type nfBlocks struct {
	bricks, fence, air, soulSand, wart, lava, chest, spawner uint32
}

var (
	nfOnce sync.Once
	nfB    nfBlocks
)

func fortressBlocks() *nfBlocks {
	nfOnce.Do(func() {
		nfB = nfBlocks{bricks: ridNamed("nether_brick", nil), fence: ridNamed("nether_brick_fence", nil), air: airRID(),
			soulSand: ridNamed("soul_sand", nil), wart: ridNamed("nether_wart", map[string]any{"age": int32(0)}),
			lava: ridNamed("lava", map[string]any{"liquid_depth": int32(0)}), chest: ridNamed("chest", nil),
			spawner: ridNamed("mob_spawner", nil)}
	})
	return &nfB
}

// nf is a fortress piece being post-processed.
type nf struct {
	ctx
	bl *nfBlocks
}

func (c *nf) fill(x0, y0, z0, x1, y1, z1 int, b uint32) { c.box(x0, y0, z0, x1, y1, z1, b, b, false) }

func (c *nf) bricks(x0, y0, z0, x1, y1, z1 int) { c.fill(x0, y0, z0, x1, y1, z1, c.bl.bricks) }
func (c *nf) air(x0, y0, z0, x1, y1, z1 int)    { c.fill(x0, y0, z0, x1, y1, z1, c.bl.air) }
func (c *nf) fence(x0, y0, z0, x1, y1, z1 int)  { c.fill(x0, y0, z0, x1, y1, z1, c.bl.fence) }

func (c *nf) stairs(x, y, z int, f facing) {
	c.set(x, y, z, stairsRID("nether_brick_stairs", facingByte(c.p.turn(f))))
}

// down is column-down of nether bricks (P161's rule: air or a liquid, to the dimension floor).
func (c *nf) down(x, y, z int) {
	wx, wy, wz := c.p.world(x, y, z)
	if !c.w.in(wx, wy, wz) {
		return
	}
	for ; wy > c.w.lo+1 && c.w.replaceable(wx, wy, wz); wy-- {
		c.w.set(wx, wy, wz, c.bl.bricks, false)
	}
}

// chest is createChest of the nether bridge table, with the next loot seed, when the cell is in the
// chunk (P162).
func (c *nf) chest(x, y, z int) {
	wx, wy, wz := c.p.world(x, y, z)
	if !c.w.in(wx, wy, wz) || c.w.blockName(wx, wy, wz) == "chest" {
		return
	}
	c.w.chest(wx, wy, wz, c.bl.chest, "loot_tables/chests/nether_bridge.json")
}

// pillarsUnder is the common "column-down from y -1" over a rectangle of local cells.
func (c *nf) pillarsUnder(x0, z0, x1, z1 int) {
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			c.down(x, -1, z)
		}
	}
}

// nfPost is a piece's postProcess (P162).
func (s *Structures) nfPost(w *writer, p *nfPiece) {
	c := &nf{ctx: ctx{w: w, p: &p.opiece, r: w.rng}, bl: fortressBlocks()}
	switch p.kind {
	case nfCrossing:
		c.crossing()
	case nfFiller:
		c.filler(p.seed)
	case nfStraight:
		c.straight()
	case nfCorridorStairs:
		c.corridorStairs()
	case nfTBalcony:
		c.tBalcony()
	case nfEntrance:
		c.hall(false)
	case nfStalkRoom:
		c.hall(true)
	case nfCorridorCrossing:
		c.corridorCrossing()
	case nfLeftTurn:
		c.turnPiece(true, p.chest)
	case nfRightTurn:
		c.turnPiece(false, p.chest)
	case nfCorridor:
		c.corridor()
	case nfThrone:
		c.throne()
	case nfRoomCrossing:
		c.roomCrossing()
	case nfStairsRoom:
		c.stairsRoom()
	}
}

func (c *nf) crossing() {
	c.bricks(7, 3, 0, 11, 4, 18)
	c.bricks(0, 3, 7, 18, 4, 11)
	c.air(8, 5, 0, 10, 7, 18)
	c.air(0, 5, 8, 18, 7, 10)
	c.bricks(7, 5, 0, 7, 5, 7)
	c.bricks(7, 5, 11, 7, 5, 18)
	c.bricks(11, 5, 0, 11, 5, 7)
	c.bricks(11, 5, 11, 11, 5, 18)
	c.bricks(0, 5, 7, 7, 5, 7)
	c.bricks(11, 5, 7, 18, 5, 7)
	c.bricks(0, 5, 11, 7, 5, 11)
	c.bricks(11, 5, 11, 18, 5, 11)
	c.bricks(7, 2, 0, 11, 2, 5)
	c.bricks(7, 2, 13, 11, 2, 18)
	c.bricks(7, 0, 0, 11, 1, 3)
	c.bricks(7, 0, 15, 11, 1, 18)
	for x := 7; x <= 11; x++ {
		for z := range 3 {
			c.down(x, -1, z)
			c.down(x, -1, 18-z)
		}
	}
	c.bricks(0, 2, 7, 5, 2, 11)
	c.bricks(13, 2, 7, 18, 2, 11)
	c.bricks(0, 0, 7, 3, 1, 11)
	c.bricks(15, 0, 7, 18, 1, 11)
	for x := range 3 {
		for z := 7; z <= 11; z++ {
			c.down(x, -1, z)
			c.down(18-x, -1, z)
		}
	}
}

// filler is the end filler, drawn from its own random MT(seed) (P162; the least checked part).
func (c *nf) filler(seed int32) {
	r := worldgen.NewBedrockRandom(uint32(seed))
	for x := range 5 {
		for y := 3; y <= 4; y++ {
			z := int(r.NextIntN(8))
			c.bricks(x, y, 0, x, y, z)
		}
	}
	z := int(r.NextIntN(8))
	c.bricks(0, 5, 0, 0, 5, z)
	z = int(r.NextIntN(8))
	c.bricks(4, 5, 0, 4, 5, z)
	for x := range 5 {
		zx := int(r.NextIntN(5))
		c.bricks(x, 2, 0, x, 2, zx)
	}
	for x := range 5 {
		for y := range 2 {
			zx := int(r.NextIntN(3))
			c.bricks(x, y, 0, x, y, zx)
		}
	}
}

func (c *nf) straight() {
	c.bricks(0, 3, 0, 4, 4, 18)
	c.air(1, 5, 0, 3, 7, 18)
	c.bricks(0, 5, 0, 0, 5, 18)
	c.bricks(4, 5, 0, 4, 5, 18)
	c.bricks(0, 2, 0, 4, 2, 5)
	c.bricks(0, 2, 13, 4, 2, 18)
	c.bricks(0, 0, 0, 4, 1, 3)
	c.bricks(0, 0, 15, 4, 1, 18)
	for x := range 5 {
		for z := range 3 {
			c.down(x, -1, z)
			c.down(x, -1, 18-z)
		}
	}
	c.fence(0, 1, 1, 0, 4, 1)
	c.fence(0, 3, 4, 0, 4, 4)
	c.fence(0, 3, 14, 0, 4, 14)
	c.fence(0, 1, 17, 0, 4, 17)
	c.fence(4, 1, 1, 4, 4, 1)
	c.fence(4, 3, 4, 4, 4, 4)
	c.fence(4, 3, 14, 4, 4, 14)
	c.fence(4, 1, 17, 4, 4, 17)
}

func (c *nf) corridorStairs() {
	for step := range 10 {
		floor := max(1, 7-step)
		roof := min(max(floor+5, 14-step), 13)
		z := step
		c.bricks(0, 0, z, 4, floor, z)
		c.air(1, floor+1, z, 3, roof-1, z)
		if step <= 6 {
			c.stairs(1, floor+1, z, fS)
			c.stairs(2, floor+1, z, fS)
			c.stairs(3, floor+1, z, fS)
		}
		c.bricks(0, roof, z, 4, roof, z)
		c.bricks(0, floor+1, z, 0, roof-1, z)
		c.bricks(4, floor+1, z, 4, roof-1, z)
		if step&1 == 0 {
			c.fence(0, floor+2, z, 0, floor+3, z)
			c.fence(4, floor+2, z, 4, floor+3, z)
		}
		for x := range 5 {
			c.down(x, -1, z)
		}
	}
}

func (c *nf) tBalcony() {
	c.bricks(0, 0, 0, 8, 1, 8)
	c.air(0, 2, 0, 8, 5, 8)
	c.bricks(0, 6, 0, 8, 6, 5)
	c.bricks(0, 2, 0, 2, 5, 0)
	c.bricks(6, 2, 0, 8, 5, 0)
	c.fence(1, 3, 0, 1, 4, 0)
	c.fence(7, 3, 0, 7, 4, 0)
	c.bricks(0, 2, 4, 8, 2, 8)
	c.air(1, 1, 4, 2, 2, 4)
	c.air(6, 1, 4, 7, 2, 4)
	c.fence(1, 3, 8, 7, 3, 8)
	c.set(0, 3, 8, c.bl.fence)
	c.set(8, 3, 8, c.bl.fence)
	c.fence(0, 3, 6, 0, 3, 7)
	c.fence(8, 3, 6, 8, 3, 7)
	c.bricks(0, 3, 4, 0, 5, 5)
	c.bricks(8, 3, 4, 8, 5, 5)
	c.bricks(1, 3, 5, 2, 5, 5)
	c.bricks(6, 3, 5, 7, 5, 5)
	c.fence(1, 4, 5, 1, 5, 5)
	c.fence(7, 4, 5, 7, 5, 5)
	for z := range 6 {
		for x := range 9 {
			c.down(x, -1, z)
		}
	}
}

// hall is the castle entrance (stalk = false) and the stalk room (true): the same shell, then the
// entrance's lava well or the stalk room's stairs and wart beds (P162).
func (c *nf) hall(stalk bool) {
	c.bricks(0, 3, 0, 12, 4, 12)
	c.air(0, 5, 0, 12, 13, 12)
	c.bricks(0, 5, 0, 1, 12, 12)
	c.bricks(11, 5, 0, 12, 12, 12)
	c.bricks(2, 5, 11, 4, 12, 12)
	c.bricks(8, 5, 11, 10, 12, 12)
	c.bricks(5, 9, 11, 7, 12, 12)
	c.bricks(2, 5, 0, 4, 12, 1)
	c.bricks(8, 5, 0, 10, 12, 1)
	c.bricks(5, 9, 0, 7, 12, 1)
	c.bricks(2, 11, 2, 10, 12, 10)
	if !stalk {
		c.fence(5, 8, 0, 7, 8, 0)
	}
	for i := 1; i <= 11; i += 2 {
		c.fence(i, 10, 0, i, 11, 0)
		c.fence(i, 10, 12, i, 11, 12)
		c.fence(0, 10, i, 0, 11, i)
		c.fence(12, 10, i, 12, 11, i)
		c.set(i, 13, 0, c.bl.bricks)
		c.set(i, 13, 12, c.bl.bricks)
		c.set(0, 13, i, c.bl.bricks)
		c.set(12, 13, i, c.bl.bricks)
		if i != 11 {
			c.set(i+1, 13, 0, c.bl.fence)
			c.set(i+1, 13, 12, c.bl.fence)
			c.set(0, 13, i+1, c.bl.fence)
			c.set(12, 13, i+1, c.bl.fence)
		}
	}
	c.set(0, 13, 0, c.bl.fence)
	c.set(0, 13, 12, c.bl.fence)
	c.set(12, 13, 12, c.bl.fence)
	c.set(12, 13, 0, c.bl.fence)
	for z := 3; z <= 9; z += 2 {
		c.fence(1, 7, z, 1, 8, z)
		c.fence(11, 7, z, 11, 8, z)
	}
	if stalk {
		for i := range 7 {
			z := i + 4
			for x := 5; x <= 7; x++ {
				c.stairs(x, 5+i, z, fN)
			}
			if z >= 5 && z <= 8 {
				c.bricks(5, 5, z, 7, i+4, z)
			} else if z >= 9 && z <= 10 {
				c.bricks(5, 8, z, 7, i+4, z)
			}
			if i >= 1 {
				c.air(5, 6+i, z, 7, 9+i, z)
			}
		}
		for x := 5; x <= 7; x++ {
			c.stairs(x, 12, 11, fN)
		}
		c.fence(5, 6, 7, 5, 7, 7)
		c.fence(7, 6, 7, 7, 7, 7)
		c.air(5, 13, 12, 7, 13, 12)
		c.bricks(2, 5, 2, 3, 5, 3)
		c.bricks(2, 5, 9, 3, 5, 10)
		c.bricks(2, 5, 4, 2, 5, 8)
		c.bricks(9, 5, 2, 10, 5, 3)
		c.bricks(9, 5, 9, 10, 5, 10)
		c.bricks(10, 5, 4, 10, 5, 8)
		for _, z := range []int{2, 3, 9, 10} {
			c.stairs(4, 5, z, fW)
		}
		for _, z := range []int{2, 3, 9, 10} {
			c.stairs(8, 5, z, fE)
		}
		c.fill(3, 4, 4, 4, 4, 8, c.bl.soulSand)
		c.fill(8, 4, 4, 9, 4, 8, c.bl.soulSand)
		c.fill(3, 5, 4, 4, 5, 8, c.bl.wart)
		c.fill(8, 5, 4, 9, 5, 8, c.bl.wart)
	}
	c.bricks(4, 2, 0, 8, 2, 12)
	c.bricks(0, 2, 4, 12, 2, 8)
	c.bricks(4, 0, 0, 8, 1, 3)
	c.bricks(4, 0, 9, 8, 1, 12)
	c.bricks(0, 0, 4, 3, 1, 8)
	c.bricks(9, 0, 4, 12, 1, 8)
	for x := 4; x <= 8; x++ {
		for z := range 3 {
			c.down(x, -1, z)
			c.down(x, -1, 12-z)
		}
	}
	for x := range 3 {
		for z := 4; z <= 8; z++ {
			c.down(x, -1, z)
			c.down(12-x, -1, z)
		}
	}
	if !stalk {
		c.bricks(5, 5, 5, 7, 5, 7)
		c.air(6, 1, 6, 6, 4, 6)
		c.set(6, 0, 6, c.bl.bricks)
		c.set(6, 5, 6, c.bl.lava) // Java also schedules a fluid tick; BDS's world has the source (P162).
	}
}

func (c *nf) corridorCrossing() {
	c.bricks(0, 0, 0, 4, 1, 4)
	c.air(0, 2, 0, 4, 5, 4)
	c.bricks(0, 2, 0, 0, 5, 0)
	c.bricks(4, 2, 0, 4, 5, 0)
	c.bricks(0, 2, 4, 0, 5, 4)
	c.bricks(4, 2, 4, 4, 5, 4)
	c.bricks(0, 6, 0, 4, 6, 4)
	c.pillarsUnder(0, 0, 4, 4)
}

func (c *nf) turnPiece(left, chest bool) {
	c.bricks(0, 0, 0, 4, 1, 4)
	c.air(0, 2, 0, 4, 5, 4)
	if left {
		c.bricks(4, 2, 0, 4, 5, 4)
		c.fence(4, 3, 1, 4, 4, 1)
		c.fence(4, 3, 3, 4, 4, 3)
		c.bricks(0, 2, 0, 0, 5, 0)
		c.bricks(0, 2, 4, 3, 5, 4)
		c.fence(1, 3, 4, 1, 4, 4)
		c.fence(3, 3, 4, 3, 4, 4)
		if chest {
			c.chest(3, 2, 3)
		}
	} else {
		c.bricks(0, 2, 0, 0, 5, 4)
		c.fence(0, 3, 1, 0, 4, 1)
		c.fence(0, 3, 3, 0, 4, 3)
		c.bricks(4, 2, 0, 4, 5, 0)
		c.bricks(1, 2, 4, 4, 5, 4)
		c.fence(1, 3, 4, 1, 4, 4)
		c.fence(3, 3, 4, 3, 4, 4)
		if chest {
			c.chest(1, 2, 3)
		}
	}
	c.bricks(0, 6, 0, 4, 6, 4)
	c.pillarsUnder(0, 0, 4, 4)
}

func (c *nf) corridor() {
	c.bricks(0, 0, 0, 4, 1, 4)
	c.air(0, 2, 0, 4, 5, 4)
	c.bricks(0, 2, 0, 0, 5, 4)
	c.bricks(4, 2, 0, 4, 5, 4)
	c.fence(0, 3, 1, 0, 4, 1)
	c.fence(0, 3, 3, 0, 4, 3)
	c.fence(4, 3, 1, 4, 4, 1)
	c.fence(4, 3, 3, 4, 4, 3)
	c.bricks(0, 6, 0, 4, 6, 4)
	c.pillarsUnder(0, 0, 4, 4)
}

func (c *nf) throne() {
	c.air(0, 2, 0, 6, 7, 7)
	c.bricks(1, 0, 0, 5, 1, 7)
	c.bricks(1, 2, 1, 5, 2, 7)
	c.bricks(1, 3, 2, 5, 3, 7)
	c.bricks(1, 4, 3, 5, 4, 7)
	c.bricks(1, 2, 0, 1, 4, 2)
	c.bricks(5, 2, 0, 5, 4, 2)
	c.bricks(1, 5, 2, 1, 5, 3)
	c.bricks(5, 5, 2, 5, 5, 3)
	c.bricks(0, 5, 3, 0, 5, 8)
	c.bricks(6, 5, 3, 6, 5, 8)
	c.bricks(1, 5, 8, 5, 5, 8)
	f := c.bl.fence
	c.set(1, 6, 3, f)
	c.set(5, 6, 3, f)
	c.set(0, 6, 3, f)
	c.set(6, 6, 3, f)
	c.fence(0, 6, 4, 0, 6, 7)
	c.fence(6, 6, 4, 6, 6, 7)
	c.set(0, 6, 8, f)
	c.set(6, 6, 8, f)
	c.fence(1, 6, 8, 5, 6, 8)
	c.set(1, 7, 8, f)
	c.fence(2, 7, 8, 4, 7, 8)
	c.set(5, 7, 8, f)
	c.set(2, 8, 8, f)
	c.set(3, 8, 8, f)
	c.set(4, 8, 8, f)
	// The spawner, once, when its cell is in the chunk (P162).
	if wx, wy, wz := c.p.world(3, 5, 5); c.w.in(wx, wy, wz) {
		c.w.set(wx, wy, wz, c.bl.spawner, false)
		c.w.bes = append(c.w.bes, spawnerEntity(wx, wy, wz, "minecraft:blaze"))
	}
	c.pillarsUnder(0, 0, 6, 6)
}

func (c *nf) roomCrossing() {
	c.bricks(0, 0, 0, 6, 1, 6)
	c.air(0, 2, 0, 6, 7, 6)
	c.bricks(0, 2, 0, 1, 6, 0)
	c.bricks(0, 2, 6, 1, 6, 6)
	c.bricks(5, 2, 0, 6, 6, 0)
	c.bricks(5, 2, 6, 6, 6, 6)
	c.bricks(0, 2, 0, 0, 6, 1)
	c.bricks(0, 2, 5, 0, 6, 6)
	c.bricks(6, 2, 0, 6, 6, 1)
	c.bricks(6, 2, 5, 6, 6, 6)
	c.bricks(2, 6, 0, 4, 6, 0)
	c.fence(2, 5, 0, 4, 5, 0)
	c.bricks(2, 6, 6, 4, 6, 6)
	c.fence(2, 5, 6, 4, 5, 6)
	c.bricks(0, 6, 2, 0, 6, 4)
	c.fence(0, 5, 2, 0, 5, 4)
	c.bricks(6, 6, 2, 6, 6, 4)
	c.fence(6, 5, 2, 6, 5, 4)
	c.pillarsUnder(0, 0, 6, 6)
}

func (c *nf) stairsRoom() {
	c.bricks(0, 0, 0, 6, 1, 6)
	c.air(0, 2, 0, 6, 10, 6)
	c.bricks(0, 2, 0, 1, 8, 0)
	c.bricks(5, 2, 0, 6, 8, 0)
	c.bricks(0, 2, 1, 0, 8, 6)
	c.bricks(6, 2, 1, 6, 8, 6)
	c.bricks(1, 2, 6, 5, 8, 6)
	c.fence(0, 3, 2, 0, 5, 4)
	c.fence(6, 3, 2, 6, 5, 2)
	c.fence(6, 3, 4, 6, 5, 4)
	c.set(5, 2, 5, c.bl.bricks)
	c.bricks(4, 2, 5, 4, 3, 5)
	c.bricks(3, 2, 5, 3, 4, 5)
	c.bricks(2, 2, 5, 2, 5, 5)
	c.bricks(1, 2, 5, 1, 6, 5)
	c.bricks(1, 7, 1, 5, 7, 4)
	c.air(6, 8, 2, 6, 8, 4)
	c.bricks(2, 6, 0, 4, 8, 0)
	c.fence(2, 5, 0, 4, 5, 0)
	c.pillarsUnder(0, 0, 6, 6)
}
