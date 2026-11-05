package structures

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Mineshafts, our own Go of NOTES-structures-phase2 P14.1 (cited P141; assembly P11.8, P12.1, P13.1;
// the minecart pass P14.5). The layout was validated in the study at 86.6% / 88.5% of cells on seeds 5
// and 100. New worlds only: the legacy switch (dirt room floor, planks only over air, no pillars) is off.

// mineshaft piece kinds.
const (
	msRoom = iota
	msCorridor
	msCrossing
	msStairs
)

type msPiece struct {
	opiece
	kind  int
	depth int
	// Corridor.
	rails, spider bool
	sections      int
	// Crossing: the direction it was entered with.
	dir orient
	// Room: the entrance boxes of its children (P141 step 2).
	entrances []box
}

// mineshaft is an assembled mineshaft: its pieces in list order and its type.
type mineshaft struct {
	pieces []*msPiece
	mesa   bool
}

// msMesaBiomes are the badlands biomes (Bedrock ids), the mesa mineshaft's test at (cx*16, cz*16)
// (P11.8: the biome passes 0xce67db0; the set is the badlands family).
var msMesaBiomes = idSet(37, 38, 39, 165, 166, 167)

// msReach is how far (in chunks) a mineshaft's pieces can reach from its start: the factory's 80
// blocks from the room's corner plus a piece.
const msReach = 6

type msAssembler struct {
	r      *worldgen.BedrockRandom
	pieces []*msPiece
	room   *msPiece
}

// mineshaftAtStart builds the mineshaft of start chunk (cx, cz), on the start's stream (P11.8: it
// continues the isFeatureChunk draws).
func (s *Structures) mineshaftAtStart(cx, cz int) *mineshaft {
	r := worldgen.NewBedrockRandom(uint32(s.mineB)*uint32(int32(cz)) ^ uint32(s.mineA)*uint32(int32(cx)) ^ s.seed32)
	r.NextInt()
	r.NextFloat()
	r.NextIntN(80)
	m := &mineshaft{mesa: msMesaBiomes[s.g.BiomeID(cx*16, s.surface(cx, cz), cz*16)]}
	// 1. Room: a, b, c drawn in the order z, y, x.
	a, b, c := int(r.NextIntN(6)), int(r.NextIntN(6)), int(r.NextIntN(6))
	x0, z0 := cx*16+2, cz*16+2
	room := &msPiece{opiece: opiece{b: box{x0, 50, z0, x0 + 7 + c, 54 + b, z0 + 7 + a}, o: oNone}, kind: msRoom}
	as := &msAssembler{r: r, pieces: []*msPiece{room}, room: room}
	p := float32(1.0)
	if m.mesa {
		p = 0.5
	}
	as.roomChildren(room, p)
	// 7. Height. The mesa rule (P141: j = -64 + 5 + span + 1 ...) is not complete in the notes; mesa
	// mineshafts take the normal height.
	boxes := make([]box, len(as.pieces))
	for i, pc := range as.pieces {
		boxes[i] = pc.b
	}
	dy := moveBelowSea(boundsOf(boxes), r)
	for _, pc := range as.pieces {
		pc.b = pc.b.shift(dy)
		for i := range pc.entrances {
			pc.entrances[i] = pc.entrances[i].shift(dy)
		}
	}
	m.pieces = as.pieces
	return m
}

// roomChildren is step 2.
func (as *msAssembler) roomChildren(room *msPiece, p float32) {
	r := as.r
	if r.NextFloat() > p {
		return
	}
	b := room.b
	hs := max(b[4]-b[1]+1-4, 1)
	xs, zs := b[3]-b[0]+1, b[5]-b[2]+1
	for _, side := range []orient{oN, oS, oW, oE} {
		n := xs
		if side == oW || side == oE {
			n = zs
		}
		for pos := 0; pos < n; pos += 4 {
			pos += int(r.NextIntN(int32(n)))
			if pos+3 > n {
				break
			}
			y := b[1] + int(r.NextIntN(int32(hs))) + 1
			var c *msPiece
			switch side {
			case oN:
				c = as.make(b[0]+pos, y, b[2]-1, oN, 0)
			case oS:
				c = as.make(b[0]+pos, y, b[5]+1, oS, 0)
			case oW:
				c = as.make(b[0]-1, y, b[2]+pos, oW, 0)
			case oE:
				c = as.make(b[3]+1, y, b[2]+pos, oE, 0)
			}
			if c == nil {
				continue
			}
			cb := c.b
			var e box
			switch side {
			case oN:
				e = box{cb[0], cb[1], b[2], cb[3], cb[4], b[2] + 1}
			case oS:
				e = box{cb[0], cb[1], b[5] - 1, cb[3], cb[4], b[5]}
			case oW:
				e = box{b[0], cb[1], cb[2], b[0] + 1, cb[4], cb[5]}
			case oE:
				e = box{b[3] - 1, cb[1], cb[2], b[3], cb[4], cb[5]}
			}
			room.entrances = append(room.entrances, e)
		}
	}
}

func (as *msAssembler) collides(b box) bool {
	for _, p := range as.pieces {
		if p.b.intersects(b) {
			return true
		}
	}
	return false
}

// make is the child factory (step 3); the made piece's children are added at once.
func (as *msAssembler) make(x, y, z int, o orient, depth int) *msPiece {
	if depth > 8 || abs(x-as.room.b[0]) > 80 || abs(z-as.room.b[2]) > 80 {
		return nil
	}
	r := as.r
	k := r.NextIntN(100)
	var p *msPiece
	switch {
	case k >= 80:
		top := 2
		if r.NextIntN(4) == 0 {
			top = 6
		}
		b := orientBox(x, y, z, -1, 0, 0, 5, top+1, 5, o)
		if as.collides(b) {
			return nil
		}
		p = &msPiece{opiece: opiece{b: b, o: oNone}, kind: msCrossing, dir: o}
	case k >= 70:
		b := orientBox(x, y, z, 0, -5, 0, 3, 8, 9, o)
		if as.collides(b) {
			return nil
		}
		p = &msPiece{opiece: opiece{b: b, o: o}, kind: msStairs}
	default:
		for n := int(r.NextIntN(3)) + 2; n >= 1; n-- {
			b := orientBox(x, y, z, 0, 0, 0, 3, 3, 5*n, o)
			if !as.collides(b) {
				p = &msPiece{opiece: opiece{b: b, o: o}, kind: msCorridor, sections: n}
				break
			}
		}
		if p == nil {
			return nil
		}
		p.rails = r.NextIntN(3) == 0
		p.spider = !p.rails && r.NextIntN(23) == 0
	}
	p.depth = depth + 1
	as.pieces = append(as.pieces, p)
	as.children(p)
	return p
}

func (as *msAssembler) children(p *msPiece) {
	r := as.r
	b, d := p.b, p.depth
	switch p.kind {
	case msCorridor: // Step 4.
		e := r.NextIntN(4)
		y := b[1] - 1 + int(r.NextIntN(3))
		switch p.o {
		case oN:
			switch {
			case e <= 1:
				as.make(b[0], y, b[2]-1, oN, d)
			case e == 2:
				as.make(b[0]-1, y, b[2], oW, d)
			default:
				as.make(b[3]+1, y, b[2], oE, d)
			}
		case oS:
			switch {
			case e <= 1:
				as.make(b[0], y, b[5]+1, oS, d)
			case e == 2:
				as.make(b[0]-1, y, b[5]-3, oW, d)
			default:
				as.make(b[3]+1, y, b[5]-3, oE, d)
			}
		case oW:
			switch {
			case e <= 1:
				as.make(b[0]-1, y, b[2], oW, d)
			case e == 2:
				as.make(b[0], y, b[2]-1, oN, d)
			default:
				as.make(b[0], y, b[5]+1, oS, d)
			}
		case oE:
			switch {
			case e <= 1:
				as.make(b[3]+1, y, b[2], oE, d)
			case e == 2:
				as.make(b[3]-3, y, b[2]-1, oN, d)
			default:
				as.make(b[3]-3, y, b[5]+1, oS, d)
			}
		}
		if d < 8 {
			if p.o == oW || p.o == oE {
				for x := b[0] + 3; x+3 <= b[3]; x += 5 {
					switch r.NextIntN(5) {
					case 0:
						as.make(x, b[1], b[2]-1, oN, d+1)
					case 1:
						as.make(x, b[1], b[5]+1, oS, d+1)
					}
				}
			} else {
				for z := b[2] + 3; z+3 <= b[5]; z += 5 {
					switch r.NextIntN(5) {
					case 0:
						as.make(b[0]-1, b[1], z, oW, d+1)
					case 1:
						as.make(b[3]+1, b[1], z, oE, d+1)
					}
				}
			}
		}
	case msCrossing: // Step 5.
		y := b[1]
		switch p.dir {
		case oN:
			as.make(b[0]+1, y, b[2]-1, oN, d)
			as.make(b[0]-1, y, b[2]+1, oW, d)
			as.make(b[3]+1, y, b[2]+1, oE, d)
		case oS:
			as.make(b[0]+1, y, b[5]+1, oS, d)
			as.make(b[0]-1, y, b[2]+1, oW, d)
			as.make(b[3]+1, y, b[2]+1, oE, d)
		case oW:
			as.make(b[0]+1, y, b[2]-1, oN, d)
			as.make(b[0]+1, y, b[5]+1, oS, d)
			as.make(b[0]-1, y, b[2]+1, oW, d)
		case oE:
			as.make(b[0]+1, y, b[2]-1, oN, d)
			as.make(b[0]+1, y, b[5]+1, oS, d)
			as.make(b[3]+1, y, b[2]+1, oE, d)
		}
		if b[4]-b[1]+1 > 3 {
			if r.NextBool() {
				as.make(b[0]+1, y+4, b[2]-1, oN, d)
			}
			if r.NextBool() {
				as.make(b[0]-1, y+4, b[2]+1, oW, d)
			}
			if r.NextBool() {
				as.make(b[3]+1, y+4, b[2]+1, oE, d)
			}
			if r.NextBool() {
				as.make(b[0]+1, y+4, b[5]+1, oS, d)
			}
		}
	case msStairs: // Step 6.
		switch p.o {
		case oN:
			as.make(b[0], b[1], b[2]-1, oN, d)
		case oS:
			as.make(b[0], b[1], b[5]+1, oS, d)
		case oW:
			as.make(b[0]-1, b[1], b[2], oW, d)
		case oE:
			as.make(b[3]+1, b[1], b[2], oE, d)
		}
	}
}

// msBlocks are the mineshaft's block states.
type msBlocks struct {
	air, web, rail, railEW, torchS, torchN, chain, spawner uint32
	planks, log, fence                                     [2]uint32 // normal, mesa
}

var (
	msOnce sync.Once
	msB    msBlocks
)

func mineshaftBlocks() *msBlocks {
	msOnce.Do(func() {
		msB = msBlocks{
			air: airRID(), web: ridNamed("web", nil),
			rail: ridNamed("rail", map[string]any{"rail_direction": int32(0)}), railEW: ridNamed("rail", map[string]any{"rail_direction": int32(1)}),
			chain:   ridNamed("iron_chain", map[string]any{"pillar_axis": "y"}),
			spawner: ridNamed("mob_spawner", nil),
			planks:  [2]uint32{ridNamed("oak_planks", nil), ridNamed("dark_oak_planks", nil)},
			log:     [2]uint32{ridNamed("oak_log", map[string]any{"pillar_axis": "y"}), ridNamed("dark_oak_log", map[string]any{"pillar_axis": "y"})},
			fence:   [2]uint32{ridNamed("oak_fence", nil), ridNamed("dark_oak_fence", nil)},
		}
	})
	return &msB
}

// torchRID is a wall torch facing f (pointing away from its wall). Bedrock's torch_facing_direction
// names the side the wall is on (Dragonfly's Torch.Facing), so it is the opposite word.
func torchRID(f facing) uint32 {
	return ridNamed("torch", map[string]any{"torch_facing_direction": [...]string{"south", "north", "east", "west"}[f]})
}

// minecartIntent is a chest minecart the mineshaft would spawn (P141 step 5). Entities in new chunks are
// not supported yet, so the cart is recorded, not spawned (docs/stage-2-known-gaps.md).
type minecartIntent struct {
	Pos       [3]int
	LootTable string
	Seed      int32
}

// placeMineshafts post-processes every mineshaft that reaches the chunk.
func (s *Structures) placeMineshafts(w *writer) {
	cb := w.chunkBox()
	for sx := w.cx - msReach; sx <= w.cx+msReach; sx++ {
		for sz := w.cz - msReach; sz <= w.cz+msReach; sz++ {
			if !s.mineshaftAt(sx, sz) {
				continue
			}
			m := s.mineshafts.get([2]int{sx, sz}, func() any { return s.mineshaftAtStart(sx, sz) }).(*mineshaft)
			for _, p := range m.pieces {
				if p.b.intersects(cb) {
					s.msPost(w, m, p)
				}
			}
		}
	}
}

// msInvalid is the invalid-location test (P141): the piece box grown by 1 and clipped to the chunk;
// deep_dark at its centre, or a liquid on one of its six faces.
func (s *Structures) msInvalid(w *writer, b box) bool {
	cb := w.chunkBox()
	g := box{max(b[0]-1, cb[0]), max(b[1]-1, cb[1]), max(b[2]-1, cb[2]), min(b[3]+1, cb[3]), min(b[4]+1, cb[4]), min(b[5]+1, cb[5])}
	if s.g.BiomeID((g[0]+g[3])/2, (g[1]+g[4])/2, (g[2]+g[5])/2) == 190 {
		return true
	}
	for x := g[0]; x <= g[3]; x++ {
		for z := g[2]; z <= g[5]; z++ {
			if w.liquidAt(x, g[1], z) || w.liquidAt(x, g[4], z) {
				return true
			}
		}
	}
	for x := g[0]; x <= g[3]; x++ {
		for y := g[1]; y <= g[4]; y++ {
			if w.liquidAt(x, y, g[2]) || w.liquidAt(x, y, g[5]) {
				return true
			}
		}
	}
	for z := g[2]; z <= g[5]; z++ {
		for y := g[1]; y <= g[4]; y++ {
			if w.liquidAt(g[0], y, z) || w.liquidAt(g[3], y, z) {
				return true
			}
		}
	}
	return false
}

func (s *Structures) msPost(w *writer, m *mineshaft, p *msPiece) {
	if s.msInvalid(w, p.b) {
		return
	}
	bl := mineshaftBlocks()
	t := 0
	if m.mesa {
		t = 1
	}
	c := &ctx{w: w, p: &p.opiece, r: w.rng}
	air := bl.air
	b := p.b
	switch p.kind {
	case msRoom:
		c.box(b[0], b[1]+1, b[2], b[3], min(b[1]+3, b[4]), b[5], air, air, false)
		for _, e := range p.entrances {
			c.box(e[0], e[4]-2, e[2], e[3], e[4], e[5], air, air, false)
		}
		// The upper half-ellipsoid (Java's generateUpperHalfSphere over y0+4..y1).
		y0 := b[1] + 4
		if y0 <= b[4] {
			fx, fy, fz := float32(b[3]-b[0]+1), float32(b[4]-y0+1), float32(b[5]-b[2]+1)
			cx, cz := float32(b[0])+fx/2, float32(b[2])+fz/2
			for y := y0; y <= b[4]; y++ {
				dy := float32(y-y0) / fy
				for x := b[0]; x <= b[3]; x++ {
					dx := (float32(x) - cx) / (fx * 0.5)
					for z := b[2]; z <= b[5]; z++ {
						dz := (float32(z) - cz) / (fz * 0.5)
						if dx*dx+dy*dy+dz*dz <= 1.05 {
							c.set(x, y, z, air)
						}
					}
				}
			}
		}
	case msCrossing:
		x0, y0, z0, x1, y1, z1 := b[0], b[1], b[2], b[3], b[4], b[5]
		if y1-y0+1 > 3 {
			c.box(x0+1, y0, z0, x1-1, y0+2, z1, air, air, false)
			c.box(x0, y0, z0+1, x1, y0+2, z1-1, air, air, false)
			c.box(x0+1, y1-2, z0, x1-1, y1, z1, air, air, false)
			c.box(x0, y1-2, z0+1, x1, y1, z1-1, air, air, false)
			c.box(x0+1, y0+3, z0+1, x1-1, y0+3, z1-1, air, air, false)
		} else {
			c.box(x0+1, y0, z0, x1-1, y1, z1, air, air, false)
			c.box(x0, y0, z0+1, x1, y1, z1-1, air, air, false)
		}
		for _, q := range [][2]int{{x0 + 1, z0 + 1}, {x0 + 1, z1 - 1}, {x1 - 1, z0 + 1}, {x1 - 1, z1 - 1}} {
			if c.get(q[0], y1+1, q[1]) != air {
				c.box(q[0], y0, q[1], q[0], y1, q[1], bl.planks[t], air, false)
			}
		}
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				c.planksAt(x, y0-1, z, bl.planks[t])
			}
		}
	case msStairs:
		c.box(0, 5, 0, 2, 7, 1, air, air, false)
		c.box(0, 0, 7, 2, 2, 8, air, air, false)
		for i := 0; i < 5; i++ {
			lo := 5 - i
			if i < 4 {
				lo--
			}
			c.box(0, lo, 2+i, 2, 7-i, 2+i, air, air, false)
		}
	case msCorridor:
		s.msCorridor(c, p, bl, t)
	}
}

// planksAt is setPlanksBlock: planks where the current block is not solid, in an interior cell.
func (c *ctx) planksAt(x, y, z int, planks uint32) {
	if c.interior(x, y, z) && !c.solid(x, y, z) {
		c.set(x, y, z, planks)
	}
}

func (s *Structures) msCorridor(c *ctx, p *msPiece, bl *msBlocks, t int) {
	air, r := bl.air, c.r
	L := p.sections*5 - 1
	// 1.
	c.box(0, 0, 0, 2, 1, L, air, air, false)
	c.maybeBox(0.8, 0, 2, 0, 2, 2, L, air, air, false, false)
	if p.spider {
		c.maybeBox(0.6, 0, 0, 0, 2, 1, L, bl.web, bl.web, false, true)
	}
	// 2. Sections.
	for sec := 0; sec < p.sections; sec++ {
		z := 2 + 5*sec
		if c.get(0, 3, z) != air && c.get(1, 3, z) != air && c.get(2, 3, z) != air {
			c.box(0, 0, z, 0, 1, z, bl.fence[t], air, false)
			c.box(2, 0, z, 2, 1, z, bl.fence[t], air, false)
			if r.NextIntN(4) == 0 {
				c.set(0, 2, z, bl.planks[t])
				c.set(2, 2, z, bl.planks[t])
			} else {
				c.box(0, 2, z, 2, 2, z, bl.planks[t], air, false)
				c.maybe(0.05, 1, 2, z-1, torchRID(c.p.turn(fS)))
				c.maybe(0.05, 1, 2, z+1, torchRID(c.p.turn(fN)))
			}
		}
		for _, q := range []struct {
			x, z int
			p    float32
		}{{0, z - 1, 0.1}, {2, z - 1, 0.1}, {0, z + 1, 0.1}, {2, z + 1, 0.1}, {0, z - 2, 0.05}, {2, z - 2, 0.05}, {0, z + 2, 0.05}, {2, z + 2, 0.05}} {
			if c.interior(q.x, 2, q.z) && r.NextFloat() < q.p && c.sturdyNeighbours(q.x, 2, q.z) >= 2 {
				c.set(q.x, 2, q.z, bl.web)
			}
		}
		if p.spider {
			zz := z - 1 + int(r.NextIntN(3))
			wx, wy, wz := c.p.world(1, 0, zz)
			if c.w.in(wx, wy, wz) && wy < maxY && c.interior(1, 0, zz) {
				c.w.set(wx, wy, wz, bl.spawner, false)
				c.w.bes = append(c.w.bes, spawnerEntity(wx, wy, wz, "minecraft:cave_spider"))
			}
		}
	}
	// 3. Floor, then the pillars or chains under the supports.
	for x := 0; x <= 2; x++ {
		for z := 0; z <= L; z++ {
			c.planksAt(x, -1, z, bl.planks[t])
		}
	}
	zs := []int{2}
	if p.sections > 1 {
		zs = append(zs, L-2)
	}
	for _, z := range zs {
		for _, x := range []int{0, 2} {
			if c.get(x, -1, z) == bl.planks[t] {
				c.pillarOrChain(x, -1, z, bl, t)
			}
		}
	}
	// 4. Rails.
	if p.rails {
		for z := 0; z <= L; z++ {
			if below := c.get(1, -1, z); below != air && c.solid(1, -1, z) {
				f, pr := r.NextFloat(), float32(0.9)
				if c.interior(1, 0, z) {
					pr = 0.7
				}
				if f < pr {
					c.set(1, 0, z, railRID(c.p, true))
				}
			}
		}
	}
	// 5. The chest minecarts, a pass after the blocks (Bedrock; which random it gets was not confirmed,
	// the chunk's stream continuing is assumed).
	for sec := 0; sec < p.sections; sec++ {
		z := 2 + 5*sec
		if r.NextIntN(100) == 0 {
			c.minecart(2, 0, z-1)
		}
		if r.NextIntN(100) == 0 {
			c.minecart(0, 0, z+1)
		}
	}
}

// railRID is a rail running along local z (north-south) or local x, turned with the piece.
func railRID(p *opiece, alongZ bool) uint32 {
	bl := mineshaftBlocks()
	if (p.o == oW || p.o == oE) == alongZ {
		return bl.railEW
	}
	return bl.rail
}

// minecart is the minecart helper (P141 step 5): inside the chunk, on air above a block that is not
// air: a rail (shape by nextBoolean) and a chest minecart with the mineshaft table and seed nextInt().
func (c *ctx) minecart(x, y, z int) {
	wx, wy, wz := c.p.world(x, y, z)
	if !c.w.in(wx, wy, wz) || c.get(x, y, z) != airRID() || c.get(x, y-1, z) == airRID() {
		return
	}
	c.set(x, y, z, railRID(c.p, c.r.NextBool()))
	c.w.carts = append(c.w.carts, minecartIntent{Pos: [3]int{wx, wy, wz}, LootTable: "loot_tables/chests/abandoned_mineshaft.json", Seed: c.r.NextInt()})
}

// sturdyNeighbours counts the solid blocks among the six neighbours inside the chunk.
func (c *ctx) sturdyNeighbours(x, y, z int) int {
	wx, wy, wz := c.p.world(x, y, z)
	n := 0
	for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if c.solidAt(wx+d[0], wy+d[1], wz+d[2]) {
			n++
		}
	}
	return n
}

// replaceable is Java's isReplaceableByStructures: air, a liquid, glow lichen, seagrass.
func (w *writer) replaceable(x, y, z int) bool {
	rid := w.c.Block(uint8(x&15), int16(y), uint8(z&15), 0)
	if rid == airRID() || w.liquidAt(x, y, z) {
		return true
	}
	n, _ := w.block(x, y, z).EncodeBlock()
	return n == "minecraft:glow_lichen" || n == "minecraft:seagrass"
}

func (w *writer) isLava(x, y, z int) bool {
	n, _ := w.block(x, y, z).EncodeBlock()
	return n == "minecraft:lava" || n == "minecraft:flowing_lava"
}

// pillarOrChain is "pillar down or chain up" (P141 step 3).
func (c *ctx) pillarOrChain(x, y, z int, bl *msBlocks, t int) {
	wx, wy, wz := c.p.world(x, y, z)
	if !c.w.in(wx, wy, wz) {
		return
	}
	down, up := true, true
	for d := 1; down || up; d++ {
		if down {
			yy := wy - d
			ok := yy >= minY && c.w.replaceable(wx, yy, wz) && !c.w.isLava(wx, yy, wz)
			if yy >= minY && !ok && c.solidAt(wx, yy, wz) {
				for k := yy + 1; k < wy; k++ {
					c.w.set(wx, k, wz, bl.log[t], false)
				}
				return
			}
			down = d <= 20 && ok && yy > minY+1
		}
		if up {
			yy := wy + d
			ok := yy <= maxY && c.w.replaceable(wx, yy, wz)
			if yy <= maxY && !ok && c.solidAt(wx, yy, wz) && !c.w.falling(wx, yy, wz) {
				c.w.set(wx, wy+1, wz, bl.fence[t], false)
				for k := wy + 2; k < yy; k++ {
					c.w.set(wx, k, wz, bl.chain, false)
				}
				return
			}
			up = d <= 50 && ok && yy < maxY
		}
	}
}

func (w *writer) falling(x, y, z int) bool {
	n, _ := w.block(x, y, z).EncodeBlock()
	switch n {
	case "minecraft:sand", "minecraft:red_sand", "minecraft:gravel", "minecraft:suspicious_sand", "minecraft:suspicious_gravel":
		return true
	}
	return false
}

// spawnerEntity is a mob spawner's block entity.
func spawnerEntity(x, y, z int, entity string) chunk.BlockEntity {
	return chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{"id": "MobSpawner", "EntityIdentifier": entity}}
}
