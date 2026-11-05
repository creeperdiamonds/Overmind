package structures

import (
	"sync"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Stronghold pieces, our own Go of NOTES-structures-phase2 P14.4 (cited P144), with the assembly of
// P13.2 (cited P132), the filler corridor of P14.2 and the stone selector of P14.3. Validated in the
// study on seed 100's (53,100) stronghold: 149/149 pieces, 99.33% of cells, 9/9 chest seeds.

// stronghold piece kinds, in the weight table's order (P144).
const (
	shStraight = iota
	shPrisonHall
	shLeftTurn
	shRightTurn
	shRoomCrossing
	shStraightStairsDown
	shStairsDown
	shFiveCrossing
	shChestCorridor
	shLibrary
	shPortalRoom
	shFiller
	shNone
)

// door types (P144: d = nextInt(5): 0, 1 opening; 2 wood door; 3 grates; 4 iron door).
const (
	doorOpening = iota
	doorWood
	doorGrates
	doorIron
)

type shPiece struct {
	opiece
	kind, depth int
	door        int
	source      bool // The start (a StairsDown).
	// Straight.
	left, right bool
	// FiveCrossing.
	leftLow, leftHigh, rightLow, rightHigh bool
	room                                   int  // RoomCrossing's type.
	tall                                   bool // Library.
	steps                                  int  // FillerCorridor.
}

type shRecord struct {
	kind, weight, max, minDepth, count int
}

// shWeights is the weight table (P144: name, weight, max, minDepth; Bedrock's portal room weight is 10).
func shWeights() []*shRecord {
	return []*shRecord{
		{kind: shStraight, weight: 40}, {kind: shPrisonHall, weight: 5, max: 5}, {kind: shLeftTurn, weight: 20},
		{kind: shRightTurn, weight: 20}, {kind: shRoomCrossing, weight: 10, max: 6},
		{kind: shStraightStairsDown, weight: 5, max: 5}, {kind: shStairsDown, weight: 5, max: 5},
		{kind: shFiveCrossing, weight: 5, max: 4}, {kind: shChestCorridor, weight: 5, max: 4},
		{kind: shLibrary, weight: 10, max: 2, minDepth: 5}, {kind: shPortalRoom, weight: 10, max: 1, minDepth: 6},
	}
}

// strongholdReach is how far (in chunks) a stronghold's pieces can reach from its start: the factory's
// 112 blocks plus a piece.
const strongholdReach = 9

type stronghold struct {
	pieces []*shPiece
}

type shAssembler struct {
	r        *worldgen.BedrockRandom
	start    *shPiece
	pieces   []*shPiece
	pending  []*shPiece
	records  []*shRecord
	previous *shRecord
	imposed  int
}

// strongholdAtStart assembles the stronghold of start chunk (cx, cz) (P132): the start gets the
// blueprint stream MT(the chunk seed) with one draw discarded.
func (s *Structures) strongholdAtStart(cx, cz int) *stronghold {
	r := worldgen.NewBedrockRandom(s.g.DecorationSeed(cx, cz))
	r.U32()
	o := orient(r.NextIntN(4))
	x, z := cx*16+2, cz*16+2
	start := &shPiece{opiece: opiece{b: box{x, 64, z, x + 4, 74, z + 4}, o: o}, kind: shStairsDown, source: true, door: doorOpening}
	as := &shAssembler{r: r, start: start, pieces: []*shPiece{start}, records: shWeights(), imposed: shNone}
	as.children(start)
	for len(as.pending) > 0 {
		i := int(r.NextIntN(int32(len(as.pending))))
		p := as.pending[i]
		as.pending = append(as.pending[:i], as.pending[i+1:]...)
		as.children(p)
	}
	// Bedrock's portal fallback (P132).
	portal := false
	for _, p := range as.pieces {
		portal = portal || p.kind == shPortalRoom
	}
	if !portal && len(as.pieces) > 1 {
		removed := as.pieces[len(as.pieces)-1]
		as.pieces = as.pieces[:len(as.pieces)-1]
		last := as.pieces[len(as.pieces)-1]
		B, ro := last.b, removed.o
		if B[1] > 10 {
			var pb box
			switch ro {
			case oS:
				c := (B[0] + B[3]) / 2
				pb = box{c - 3, B[1], B[5], c + 7, B[1] + 7, B[5] + 15}
			case oW:
				c := (B[2] + B[5]) / 2
				pb = box{B[0] - 15, B[1], c - 5, B[0], B[1] + 7, c + 5}
			case oN:
				c := (B[0] + B[3]) / 2
				pb = box{c - 5, B[1], B[2] - 15, c + 5, B[1] + 7, B[2]}
			default:
				c := (B[2] + B[5]) / 2
				pb = box{B[3], B[1], c - 3, B[3] + 15, B[1] + 7, c + 7}
			}
			as.pieces = append(as.pieces, &shPiece{opiece: opiece{b: pb, o: ro}, kind: shPortalRoom, depth: last.depth})
		}
	}
	boxes := make([]box, len(as.pieces))
	for i, p := range as.pieces {
		boxes[i] = p.b
	}
	dy := moveBelowSea(boundsOf(boxes), r)
	for _, p := range as.pieces {
		p.b = p.b.shift(dy)
	}
	return &stronghold{pieces: as.pieces}
}

func (as *shAssembler) collision(b box) *shPiece {
	for _, p := range as.pieces {
		if p.b.intersects(b) {
			return p
		}
	}
	return nil
}

func (as *shAssembler) okBox(b box) bool { return b[1] > 10 && as.collision(b) == nil }

// gen is one exit: a factory call with the piece's depth (P144), on a copy of the main random (P132).
func (as *shAssembler) gen(x, y, z int, o orient, depth int) {
	if depth > 50 || abs(x-as.start.b[0]) > 112 || abs(z-as.start.b[2]) > 112 {
		return
	}
	cp := *as.r
	if p := as.fromDoor(&cp, x, y, z, o, depth+1); p != nil {
		as.pieces = append(as.pieces, p)
		as.pending = append(as.pending, p)
	}
}

func (as *shAssembler) fromDoor(r *worldgen.BedrockRandom, x, y, z int, o orient, d int) *shPiece {
	if as.imposed != shNone {
		k := as.imposed
		as.imposed = shNone
		if p := as.create(k, r, x, y, z, o, d); p != nil {
			return p
		}
	}
	room, total := false, 0
	for _, rec := range as.records {
		if rec.max > 0 && rec.count < rec.max {
			room = true
		}
		total += rec.weight
	}
	if !room {
		return nil
	}
	for range 5 {
		v := int(r.NextIntN(int32(total)))
		for i, rec := range as.records {
			v -= rec.weight
			if v >= 0 {
				continue
			}
			if rec.max != 0 && rec.count >= rec.max || d < rec.minDepth || rec == as.previous && len(as.records) > 1 {
				break
			}
			p := as.create(rec.kind, r, x, y, z, o, d)
			if p == nil {
				continue
			}
			rec.count++
			as.previous = rec
			if rec.max != 0 && rec.count >= rec.max {
				as.records = append(as.records[:i], as.records[i+1:]...)
			}
			return p
		}
	}
	// The filler corridor (P14.2).
	front := orientBox(x, y, z, -1, -1, 0, 5, 5, 4, o)
	hit := as.collision(front)
	if hit == nil || hit.b[1] != front[1] {
		return nil
	}
	for i := 2; i >= 1; i-- {
		if !hit.b.intersects(orientBox(x, y, z, -1, -1, 0, 5, 5, i, o)) {
			return as.filler(orientBox(x, y, z, -1, -1, 0, 5, 5, i+1, o), o, d)
		}
	}
	// Bedrock's depth-0 box, with raw bounds along the door direction.
	raw := front
	switch o {
	case oN:
		raw[2], raw[5] = z+1, z
	case oS:
		raw[2], raw[5] = z, z-1
	case oW:
		raw[0], raw[3] = x+1, x
	case oE:
		raw[0], raw[3] = x, x-1
	}
	if !hit.b.intersects(raw) {
		return as.filler(orientBox(x, y, z, -1, -1, 0, 5, 5, 1, o), o, d)
	}
	return nil
}

func (as *shAssembler) filler(b box, o orient, d int) *shPiece {
	if b[1] <= 1 {
		return nil
	}
	steps := b[5] - b[2] + 1
	if o == oW || o == oE {
		steps = b[3] - b[0] + 1
	}
	return &shPiece{opiece: opiece{b: b, o: o}, kind: shFiller, depth: d, steps: steps}
}

func randomDoor(r *worldgen.BedrockRandom) int {
	switch r.NextIntN(5) {
	case 2:
		return doorWood
	case 3:
		return doorGrates
	case 4:
		return doorIron
	}
	return doorOpening
}

// create is a piece's constructor: its box from the door's foot, then its draws (P144).
func (as *shAssembler) create(k int, r *worldgen.BedrockRandom, x, y, z int, o orient, d int) *shPiece {
	sizes := map[int][6]int{
		shStraight: {-1, -1, 0, 5, 5, 7}, shChestCorridor: {-1, -1, 0, 5, 5, 7}, shPrisonHall: {-1, -1, 0, 9, 5, 11},
		shLeftTurn: {-1, -1, 0, 5, 5, 5}, shRightTurn: {-1, -1, 0, 5, 5, 5}, shRoomCrossing: {-4, -1, 0, 11, 7, 11},
		shStraightStairsDown: {-1, -7, 0, 5, 11, 8}, shStairsDown: {-1, -7, 0, 5, 11, 5}, shFiveCrossing: {-4, -3, 0, 10, 9, 11},
		shPortalRoom: {-4, -1, 0, 11, 8, 16},
	}
	p := &shPiece{kind: k, depth: d}
	p.o = o
	if k == shLibrary {
		p.b = orientBox(x, y, z, -4, -1, 0, 14, 11, 15, o)
		if !as.okBox(p.b) {
			p.b = orientBox(x, y, z, -4, -1, 0, 14, 6, 15, o)
			if !as.okBox(p.b) {
				return nil
			}
		}
		p.door = randomDoor(r)
		p.tall = p.b[4]-p.b[1]+1 > 6
		return p
	}
	sz := sizes[k]
	p.b = orientBox(x, y, z, sz[0], sz[1], sz[2], sz[3], sz[4], sz[5], o)
	if !as.okBox(p.b) {
		return nil
	}
	if k == shPortalRoom {
		return p
	}
	p.door = randomDoor(r)
	switch k {
	case shStraight:
		p.left = r.NextIntN(2) == 0
		p.right = r.NextIntN(2) == 0
	case shFiveCrossing:
		p.leftLow = r.NextBool()
		p.leftHigh = r.NextBool()
		p.rightLow = r.NextBool()
		p.rightHigh = r.NextIntN(3) > 0
	case shRoomCrossing:
		p.room = int(r.NextIntN(5))
	}
	return p
}

// The exits (P144).
func (as *shAssembler) forward(p *shPiece, xo, yo int) {
	b := p.b
	switch p.o {
	case oN:
		as.gen(b[0]+xo, b[1]+yo, b[2]-1, oN, p.depth)
	case oS:
		as.gen(b[0]+xo, b[1]+yo, b[5]+1, oS, p.depth)
	case oW:
		as.gen(b[0]-1, b[1]+yo, b[2]+xo, oW, p.depth)
	case oE:
		as.gen(b[3]+1, b[1]+yo, b[2]+xo, oE, p.depth)
	}
}

func (as *shAssembler) left(p *shPiece, yo, zo int) {
	b := p.b
	switch p.o {
	case oN, oS:
		as.gen(b[0]-1, b[1]+yo, b[2]+zo, oW, p.depth)
	case oW, oE:
		as.gen(b[0]+zo, b[1]+yo, b[2]-1, oN, p.depth)
	}
}

func (as *shAssembler) right(p *shPiece, yo, zo int) {
	b := p.b
	switch p.o {
	case oN, oS:
		as.gen(b[3]+1, b[1]+yo, b[2]+zo, oE, p.depth)
	case oW, oE:
		as.gen(b[0]+zo, b[1]+yo, b[5]+1, oS, p.depth)
	}
}

func (as *shAssembler) children(p *shPiece) {
	switch p.kind {
	case shStairsDown:
		if p.source {
			as.imposed = shFiveCrossing
		}
		as.forward(p, 1, 1)
	case shStraight:
		as.forward(p, 1, 1)
		if p.left {
			as.left(p, 1, 2)
		}
		if p.right {
			as.right(p, 1, 2)
		}
	case shChestCorridor, shStraightStairsDown, shPrisonHall:
		as.forward(p, 1, 1)
	case shLeftTurn, shRightTurn:
		ne := p.o == oN || p.o == oE
		if ne == (p.kind == shLeftTurn) {
			as.left(p, 1, 1)
		} else {
			as.right(p, 1, 1)
		}
	case shRoomCrossing:
		as.forward(p, 4, 1)
		as.left(p, 1, 4)
		as.right(p, 1, 4)
	case shFiveCrossing:
		a, b := 3, 5
		if p.o == oW || p.o == oN {
			a, b = 5, 3
		}
		as.forward(p, 5, 1)
		if p.leftLow {
			as.left(p, a, 1)
		}
		if p.leftHigh {
			as.left(p, b, 7)
		}
		if p.rightLow {
			as.right(p, a, 1)
		}
		if p.rightHigh {
			as.right(p, b, 7)
		}
	}
}

// shBlocks are the stronghold's block states.
type shBlocks struct {
	air, bricks, cracked, mossy, infested, cobble, planks, bookshelf, web, fence, bars, water, lava uint32
	brickSlab, smoothSlab, smoothDouble, endPortal, spawner, torchUp                                uint32
}

var (
	shOnce sync.Once
	shB    shBlocks
)

func strongholdBlocks() *shBlocks {
	shOnce.Do(func() {
		bottom := map[string]any{"minecraft:vertical_half": "bottom"}
		shB = shBlocks{
			air: airRID(), bricks: ridNamed("stone_bricks", nil), cracked: ridNamed("cracked_stone_bricks", nil),
			mossy: ridNamed("mossy_stone_bricks", nil), infested: ridNamed("infested_stone_bricks", nil),
			cobble: ridNamed("cobblestone", nil), planks: ridNamed("oak_planks", nil), bookshelf: ridNamed("bookshelf", nil),
			web: ridNamed("web", nil), fence: ridNamed("oak_fence", nil), bars: ridNamed("iron_bars", nil),
			water: waterRID(), lava: ridNamed("lava", map[string]any{"liquid_depth": int32(0)}),
			brickSlab: ridNamed("stone_brick_slab", bottom), smoothSlab: ridNamed("smooth_stone_slab", bottom),
			smoothDouble: ridNamed("smooth_stone_double_slab", bottom), endPortal: ridNamed("end_portal", nil),
			spawner: ridNamed("mob_spawner", nil), torchUp: ridNamed("torch", map[string]any{"torch_facing_direction": "top"}),
		}
	})
	return &shB
}

// facingIndex is Bedrock's facing_direction number of a horizontal facing.
func facingIndex(f facing) int32 { return [...]int32{2, 3, 4, 5}[f] }

func facingName(f facing) string { return [...]string{"north", "south", "west", "east"}[f] }

func facingByte(f facing) byte { return "NSWE"[f] }

// sh is a stronghold piece being post-processed.
type sh struct {
	ctx
	bl *shBlocks
	// skipAir makes SBOX leave air cells alone (Java's behaviour); Bedrock writes them (P14.3).
	skipAir bool
}

// sbox is SBOX (P144): the stone selector for every cell of the box, inside the chunk or not and air or
// not (Bedrock); shell cells draw a float, inner cells are cave air.
func (c *sh) sbox(x0, y0, z0, x1, y1, z1 int) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				b := c.bl.air
				if x == x0 || x == x1 || y == y0 || y == y1 || z == z0 || z == z1 {
					f := c.r.NextFloat()
					switch {
					case f < 0.2:
						b = c.bl.cracked
					case f < 0.5:
						b = c.bl.mossy
					case f < 0.55:
						b = c.bl.infested
					default:
						b = c.bl.bricks
					}
				}
				if c.skipAir && c.get(x, y, z) == airRID() {
					continue
				}
				c.set(x, y, z, b)
			}
		}
	}
}

func (c *sh) fill(x0, y0, z0, x1, y1, z1 int, b uint32) { c.box(x0, y0, z0, x1, y1, z1, b, b, false) }

func (c *sh) torch(x, y, z int, f facing) { c.set(x, y, z, torchRID(c.p.turn(f))) }

func (c *sh) door(t, x, y, z int) {
	bl := c.bl
	switch t {
	case doorOpening:
		c.fill(x, y, z, x+2, y+2, z, bl.air)
	case doorWood, doorIron:
		for _, q := range [][2]int{{x, y}, {x, y + 1}, {x, y + 2}, {x + 1, y + 2}, {x + 2, y}, {x + 2, y + 1}, {x + 2, y + 2}} {
			c.set(q[0], q[1], z, bl.bricks)
		}
		name := "wooden_door"
		if t == doorIron {
			name = "iron_door"
		}
		f := facingName(c.p.turn(fN))
		c.set(x+1, y, z, ridNamed(name, map[string]any{"minecraft:cardinal_direction": f, "upper_block_bit": false, "open_bit": false, "door_hinge_bit": false}))
		c.set(x+1, y+1, z, ridNamed(name, map[string]any{"minecraft:cardinal_direction": f, "upper_block_bit": true, "open_bit": false, "door_hinge_bit": false}))
		if t == doorIron {
			c.set(x+2, y+1, z+1, ridNamed("stone_button", map[string]any{"facing_direction": facingIndex(c.p.turn(fN)), "button_pressed_bit": false}))
			c.set(x+2, y+1, z-1, ridNamed("stone_button", map[string]any{"facing_direction": facingIndex(c.p.turn(fS)), "button_pressed_bit": false}))
		}
	case doorGrates:
		c.set(x+1, y, z, bl.air)
		c.set(x+1, y+1, z, bl.air)
		for _, q := range [][2]int{{x, y}, {x, y + 1}, {x, y + 2}, {x + 1, y + 2}, {x + 2, y + 2}, {x + 2, y + 1}, {x + 2, y}} {
			c.set(q[0], q[1], z, bl.bars)
		}
	}
}

func (c *sh) ladder(x, y, z int, f facing) {
	c.set(x, y, z, ridNamed("ladder", map[string]any{"facing_direction": facingIndex(c.p.turn(f))}))
}

func (c *sh) stairs(name string, x, y, z int, f facing) {
	c.set(x, y, z, stairsRID(name, facingByte(c.p.turn(f))))
}

// chest is createChest: inside the chunk and not already a chest, with the table and seed nextInt().
func (c *sh) chest(x, y, z int, table string) {
	wx, wy, wz := c.p.world(x, y, z)
	if !c.w.in(wx, wy, wz) {
		return
	}
	if n, _ := c.w.block(wx, wy, wz).EncodeBlock(); n == "minecraft:chest" {
		return
	}
	c.w.chest(wx, wy, wz, ridNamed("chest", nil), "loot_tables/chests/"+table+".json")
}

// placeStrongholds post-processes every stronghold that reaches the chunk.
func (s *Structures) placeStrongholds(w *writer) {
	cb := w.chunkBox()
	for _, st := range s.strongholdsNear(w.cx, w.cz, strongholdReach) {
		h := s.strongholds.get(st, func() any { return s.strongholdAtStart(st[0], st[1]) }).(*stronghold)
		for _, p := range h.pieces {
			if p.b.intersects(cb) {
				s.shPost(w, p, false)
			}
		}
	}
}

// strongholdsNear are the stronghold starts within reach chunks of (cx, cz).
func (s *Structures) strongholdsNear(cx, cz, reach int) [][2]int {
	var out [][2]int
	add := func(c [2]int) {
		if abs(c[0]-cx) > reach || abs(c[1]-cz) > reach {
			return
		}
		for _, o := range out {
			if o == c {
				return
			}
		}
		out = append(out, c)
	}
	for _, c := range s.rings() {
		add(c)
	}
	for _, x := range []int{cx - reach, cx + reach} {
		for _, z := range []int{cz - reach, cz + reach} {
			if c, ok := s.gridStronghold(x, z); ok {
				add(c)
			}
		}
	}
	return out
}

func (s *Structures) shPost(w *writer, p *shPiece, skipAir bool) {
	c := &sh{ctx: ctx{w: w, p: &p.opiece, r: w.rng}, bl: strongholdBlocks(), skipAir: skipAir}
	bl := c.bl
	switch p.kind {
	case shStairsDown:
		c.sbox(0, 0, 0, 4, 10, 4)
		c.door(p.door, 1, 7, 0)
		c.door(doorOpening, 1, 1, 4)
		for _, q := range [][4]int{{2, 6, 1, 0}, {1, 5, 1, 0}, {1, 6, 1, 1}, {1, 5, 2, 0}, {1, 4, 3, 0}, {1, 5, 3, 1}, {2, 4, 3, 0},
			{3, 3, 3, 0}, {3, 4, 3, 1}, {3, 3, 2, 0}, {3, 2, 1, 0}, {3, 3, 1, 1}, {2, 2, 1, 0}, {1, 1, 1, 0}, {1, 2, 1, 1},
			{1, 1, 2, 0}, {1, 1, 3, 1}} {
			b := bl.bricks
			if q[3] == 1 {
				b = bl.brickSlab // Bedrock: stone_brick_slab (Java smooth_stone_slab).
			}
			c.set(q[0], q[1], q[2], b)
		}
	case shStraight:
		c.sbox(0, 0, 0, 4, 4, 6)
		c.door(p.door, 1, 1, 0)
		c.door(doorOpening, 1, 1, 6)
		for _, q := range []struct {
			x, z int
			f    facing
		}{{1, 1, fE}, {3, 1, fW}, {1, 5, fE}, {3, 5, fW}} {
			if c.r.NextFloat() < 0.1 {
				c.torch(q.x, 2, q.z, q.f)
			}
		}
		if p.left {
			c.fill(0, 1, 2, 0, 3, 4, bl.air)
		}
		if p.right {
			c.fill(4, 1, 2, 4, 3, 4, bl.air)
		}
	case shChestCorridor:
		c.sbox(0, 0, 0, 4, 4, 6)
		c.door(p.door, 1, 1, 0)
		c.door(doorOpening, 1, 1, 6)
		c.fill(3, 1, 2, 3, 1, 4, bl.bricks)
		for _, q := range [][3]int{{3, 1, 1}, {3, 1, 5}, {3, 2, 2}, {3, 2, 4}, {2, 1, 2}, {2, 1, 3}, {2, 1, 4}} {
			c.set(q[0], q[1], q[2], bl.brickSlab)
		}
		if c.in(3, 2, 3) {
			c.chest(3, 2, 3, "stronghold_corridor")
		}
	case shStraightStairsDown:
		c.sbox(0, 0, 0, 4, 10, 7)
		c.door(p.door, 1, 7, 0)
		c.door(doorOpening, 1, 1, 7)
		for i := 0; i <= 5; i++ {
			for x := 1; x <= 3; x++ {
				c.stairs("stone_stairs", x, 6-i, 1+i, fS) // Bedrock's stone_stairs is cobblestone stairs.
				if i < 5 {
					c.set(x, 5-i, 1+i, bl.bricks)
				}
			}
		}
	case shLeftTurn, shRightTurn:
		c.sbox(0, 0, 0, 4, 4, 4)
		c.door(p.door, 1, 1, 0)
		sw := p.o == oS || p.o == oW
		if sw == (p.kind == shLeftTurn) {
			c.fill(4, 1, 1, 4, 3, 3, bl.air)
		} else {
			c.fill(0, 1, 1, 0, 3, 3, bl.air)
		}
	case shFiller:
		for i := 0; i < p.steps; i++ {
			c.fill(0, 0, i, 4, 0, i, bl.bricks)
			for y := 1; y <= 3; y++ {
				c.set(0, y, i, bl.bricks)
				c.fill(1, y, i, 3, y, i, bl.air)
				c.set(4, y, i, bl.bricks)
			}
			c.fill(0, 4, i, 4, 4, i, bl.bricks)
		}
	case shFiveCrossing:
		c.sbox(0, 0, 0, 9, 8, 10)
		c.door(p.door, 4, 3, 0)
		if p.leftLow {
			c.fill(0, 3, 1, 0, 5, 3, bl.air)
		}
		if p.rightLow {
			c.fill(9, 3, 1, 9, 5, 3, bl.air)
		}
		if p.leftHigh {
			c.fill(0, 5, 7, 0, 7, 9, bl.air)
		}
		if p.rightHigh {
			c.fill(9, 5, 7, 9, 7, 9, bl.air)
		}
		c.fill(5, 1, 10, 7, 3, 10, bl.air)
		c.sbox(1, 2, 1, 8, 2, 6)
		c.sbox(4, 1, 5, 4, 4, 9)
		c.sbox(8, 1, 5, 8, 4, 9)
		c.sbox(1, 4, 7, 3, 4, 9)
		c.sbox(1, 3, 5, 3, 3, 6)
		// P144 gives these slabs as smooth_stone_slab (Java's); BDS has stone_brick_slab in every one of
		// the cells measured (both five-way crossings of seed 100's stronghold, TestStrongholdMatchesBDS:
		// no smooth slab anywhere in the scored chunks), as in StairsDown.
		slab := bl.brickSlab
		c.fill(1, 3, 4, 3, 3, 4, slab)
		c.fill(1, 4, 6, 3, 4, 6, slab)
		c.sbox(5, 1, 7, 7, 1, 8)
		c.fill(5, 1, 9, 7, 1, 9, slab)
		c.fill(5, 2, 7, 7, 2, 7, slab)
		c.fill(4, 5, 7, 4, 5, 9, slab)
		c.fill(8, 5, 7, 8, 5, 9, slab)
		c.fill(5, 5, 7, 7, 5, 9, bl.smoothDouble)
		c.torch(6, 5, 6, fS)
	case shPrisonHall:
		c.sbox(0, 0, 0, 8, 4, 10)
		c.door(p.door, 1, 1, 0)
		c.fill(1, 1, 10, 3, 3, 10, bl.air)
		c.sbox(4, 1, 1, 4, 3, 1)
		c.sbox(4, 1, 3, 4, 3, 3)
		c.sbox(4, 1, 7, 4, 3, 7)
		c.sbox(4, 1, 9, 4, 3, 9)
		for y := 1; y <= 3; y++ {
			for _, q := range [][2]int{{4, 4}, {4, 5}, {4, 6}, {5, 5}, {6, 5}, {7, 5}} {
				c.set(q[0], y, q[1], bl.bars)
			}
		}
		c.set(4, 3, 2, bl.bars)
		c.set(4, 3, 8, bl.bars)
		// Java's cell doors (4, 1..2, 2) and (4, 1..2, 8): Bedrock leaves air there (P14.3).
	case shRoomCrossing:
		c.sbox(0, 0, 0, 10, 6, 10)
		c.door(p.door, 4, 1, 0)
		c.fill(4, 1, 10, 6, 3, 10, bl.air)
		c.fill(0, 1, 4, 0, 3, 6, bl.air)
		c.fill(10, 1, 4, 10, 3, 6, bl.air)
		switch p.room {
		case 0:
			c.fill(5, 1, 5, 5, 3, 5, bl.bricks)
			c.torch(4, 3, 5, fW)
			c.torch(6, 3, 5, fE)
			c.torch(5, 3, 4, fS)
			c.torch(5, 3, 6, fN)
			for _, q := range [][2]int{{4, 4}, {4, 5}, {4, 6}, {6, 4}, {6, 5}, {6, 6}, {5, 4}, {5, 6}} {
				c.set(q[0], 1, q[1], bl.smoothSlab)
			}
		case 1:
			for i := 0; i <= 4; i++ {
				c.set(3, 1, 3+i, bl.bricks)
				c.set(7, 1, 3+i, bl.bricks)
				c.set(3+i, 1, 3, bl.bricks)
				c.set(3+i, 1, 7, bl.bricks)
			}
			c.fill(5, 1, 5, 5, 3, 5, bl.bricks)
			c.set(5, 4, 5, bl.water)
		case 2:
			for z := 1; z <= 9; z++ {
				c.set(1, 3, z, bl.cobble)
				c.set(9, 3, z, bl.cobble)
			}
			for x := 1; x <= 9; x++ {
				c.set(x, 3, 1, bl.cobble)
				c.set(x, 3, 9, bl.cobble)
			}
			for _, q := range [][3]int{{5, 1, 4}, {5, 1, 6}, {5, 3, 4}, {5, 3, 6}, {4, 1, 5}, {6, 1, 5}, {4, 3, 5}, {6, 3, 5}} {
				c.set(q[0], q[1], q[2], bl.cobble)
			}
			for y := 1; y <= 3; y++ {
				for _, q := range [][2]int{{4, 4}, {6, 4}, {4, 6}, {6, 6}} {
					c.set(q[0], y, q[1], bl.cobble)
				}
			}
			c.torch(5, 3, 5, fN)
			for z := 2; z <= 8; z++ {
				c.set(2, 3, z, bl.planks)
				c.set(3, 3, z, bl.planks)
				if z <= 3 || z >= 7 {
					c.fill(4, 3, z, 8, 3, z, bl.planks)
				}
			}
			for y := 1; y <= 3; y++ {
				c.ladder(9, y, 3, fW)
			}
			c.chest(3, 4, 8, "stronghold_crossing")
		}
	case shLibrary:
		h := 6
		if p.tall {
			h = 11
		}
		c.sbox(0, 0, 0, 13, h-1, 14)
		c.door(p.door, 4, 1, 0)
		c.maybeBox(0.07, 2, 1, 1, 11, 4, 13, bl.web, bl.web, false, false)
		for d := 1; d <= 13; d++ {
			b := bl.bookshelf
			if (d-1)%4 == 0 {
				b = bl.planks
			}
			c.fill(1, 1, d, 1, 4, d, b)
			c.fill(12, 1, d, 12, 4, d, b)
			if b == bl.planks {
				c.torch(2, 3, d, fE)
				c.torch(11, 3, d, fW)
			}
			if p.tall {
				c.fill(1, 6, d, 1, 9, d, b)
				c.fill(12, 6, d, 12, 9, d, b)
			}
		}
		for d := 3; d <= 11; d += 2 {
			c.fill(3, 1, d, 4, 3, d, bl.bookshelf)
			c.fill(6, 1, d, 7, 3, d, bl.bookshelf)
			c.fill(9, 1, d, 10, 3, d, bl.bookshelf)
		}
		if p.tall {
			c.fill(1, 5, 1, 3, 5, 13, bl.planks)
			c.fill(10, 5, 1, 12, 5, 13, bl.planks)
			c.fill(4, 5, 1, 9, 5, 2, bl.planks)
			c.fill(4, 5, 12, 9, 5, 13, bl.planks)
			c.set(9, 5, 11, bl.planks)
			c.set(8, 5, 11, bl.planks)
			c.set(9, 5, 10, bl.planks)
			c.fill(3, 6, 3, 3, 6, 11, bl.fence)
			c.fill(10, 6, 3, 10, 6, 9, bl.fence)
			c.fill(4, 6, 2, 9, 6, 2, bl.fence)
			c.fill(4, 6, 12, 7, 6, 12, bl.fence)
			c.set(3, 6, 2, bl.fence)
			c.set(3, 6, 12, bl.fence)
			c.set(10, 6, 2, bl.fence)
			for i := 0; i <= 2; i++ {
				c.set(8+i, 6, 12-i, bl.fence)
				if i != 2 {
					c.set(8+i, 6, 11-i, bl.fence)
				}
			}
			for y := 1; y <= 7; y++ {
				c.ladder(10, y, 13, fS)
			}
			for _, q := range [][3]int{{6, 9, 7}, {7, 9, 7}, {6, 8, 7}, {7, 8, 7}, {6, 7, 7}, {7, 7, 7}, {5, 7, 7}, {8, 7, 7}, {6, 7, 6}, {6, 7, 8}, {7, 7, 6}, {7, 7, 8}} {
				c.set(q[0], q[1], q[2], bl.fence)
			}
			for _, q := range [][3]int{{5, 8, 7}, {8, 8, 7}, {6, 8, 6}, {6, 8, 8}, {7, 8, 6}, {7, 8, 8}} {
				c.set(q[0], q[1], q[2], bl.torchUp)
			}
		}
		c.chest(3, 3, 5, "stronghold_library")
		if p.tall {
			c.set(12, 9, 1, bl.air)
			c.chest(12, 8, 1, "stronghold_library")
		}
	case shPortalRoom:
		c.sbox(0, 0, 0, 10, 7, 15)
		c.door(doorGrates, 4, 1, 0)
		c.sbox(1, 6, 1, 1, 6, 14)
		c.sbox(9, 6, 1, 9, 6, 14)
		c.sbox(2, 6, 1, 8, 6, 2)
		c.sbox(2, 6, 14, 8, 6, 14)
		c.sbox(1, 1, 1, 2, 1, 4)
		c.sbox(8, 1, 1, 9, 1, 4)
		c.fill(1, 1, 1, 1, 1, 3, bl.lava)
		c.fill(9, 1, 1, 9, 1, 3, bl.lava)
		c.sbox(3, 1, 8, 7, 1, 12)
		c.fill(4, 1, 9, 6, 1, 11, bl.lava)
		for z := 3; z <= 13; z += 2 {
			c.fill(0, 3, z, 0, 4, z, bl.bars)
			c.fill(10, 3, z, 10, 4, z, bl.bars)
		}
		for x := 2; x <= 8; x += 2 {
			c.fill(x, 3, 15, x, 4, 15, bl.bars)
		}
		c.sbox(4, 1, 5, 6, 1, 7)
		c.sbox(4, 2, 6, 6, 2, 7)
		c.sbox(4, 3, 7, 6, 3, 7)
		for x := 4; x <= 6; x++ {
			c.stairs("stone_brick_stairs", x, 1, 4, fN)
			c.stairs("stone_brick_stairs", x, 2, 5, fN)
			c.stairs("stone_brick_stairs", x, 3, 6, fN)
		}
		var eyes [12]bool
		all := true
		for i := range eyes {
			eyes[i] = c.r.NextFloat() > 0.9
			all = all && eyes[i]
		}
		frames := []struct {
			x, z int
			f    facing
		}{{4, 8, fN}, {5, 8, fN}, {6, 8, fN}, {4, 12, fS}, {5, 12, fS}, {6, 12, fS}, {3, 9, fE}, {3, 10, fE}, {3, 11, fE}, {7, 9, fW}, {7, 10, fW}, {7, 11, fW}}
		for i, q := range frames {
			c.set(q.x, 3, q.z, ridNamed("end_portal_frame", map[string]any{"minecraft:cardinal_direction": facingName(c.p.turn(q.f)), "end_portal_eye_bit": eyes[i]}))
		}
		if all {
			c.fill(4, 3, 9, 6, 3, 11, bl.endPortal)
		}
		if c.in(5, 3, 6) {
			wx, wy, wz := c.p.world(5, 3, 6)
			w.set(wx, wy, wz, bl.spawner, false)
			w.bes = append(w.bes, spawnerEntity(wx, wy, wz, "minecraft:silverfish"))
		}
	}
}

// strongholdAdjustments are the strongholds' terrain adaptation (P17.4): 1.26.60 worlds take the same
// version branch as the mineshaft's validated height rule, and there every piece of a stronghold gets
// the legacy bury (FeatureTerrainAdjustments setBury, the box only), registered in the chunks its box
// grown by 12 in x and z reaches. Not measured on a world: seed 100's stronghold lies deep.
func (s *Structures) strongholdAdjustments(cx, cz int, into []worldgen.TerrainAdjustment) []worldgen.TerrainAdjustment {
	for _, st := range s.strongholdsNear(cx, cz, strongholdReach+1) {
		h := s.strongholds.get(st, func() any { return s.strongholdAtStart(st[0], st[1]) }).(*stronghold)
		for _, p := range h.pieces {
			b := p.b
			if (b[0]-12)>>4 <= cx && cx <= (b[3]+12)>>4 && (b[2]-12)>>4 <= cz && cz <= (b[5]+12)>>4 {
				into = append(into, worldgen.TerrainAdjustment{Mode: worldgen.AdaptLegacyBury, Box: b})
			}
		}
	}
	return into
}
