package structures

import (
	"sync"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// The ocean monument, our own Go of NOTES-structures-phase2 P15.1 and P16.1 (cited P151, P161) with the
// per-block geometry of the study's P16-monument-geometry.txt (outside the repo; transcribed here in our
// own form under ruling 43). Validated in the study on seed 32's (-12,6) monument: 13,265 of 13,265
// non-water cells and 67 of 67 wet sponges. Bedrock's changes from the reference model, each marked
// BEDROCK below: the building origin (+8, +8), the direction draw, the room shuffle on a fresh random
// with bound k, no whole-box water fill, water boxes that never keep ice, the left middle wall's chunk
// test to z 54, and the double-y room's floor flag.
//
// Elder guardians are mobs (stage 4): their spawn positions become spawn intents, dropped, not spawned
// (docs/stage-2-known-gaps.md).

// Room faces, as the room graph indexes them (P161: down, up, north, south, west, east; the steps are
// jigsaw.go's faceStep).
const (
	omDown = iota
	omUp
	omNorth
	omSouth
	omWest
	omEast
)

// Room grid indices (P161: index = y*25 + z*5 + x).
func omIndex(x, y, z int) int { return y*25 + z*5 + x }

const (
	omLeftWing  = 1001
	omRightWing = 1002
	omRoof      = 1003
)

// omRoom is a room definition of the graph.
type omRoom struct {
	index   int
	conn    [6]*omRoom
	open    [6]bool
	claimed bool
	source  bool
	scan    int
}

func (r *omRoom) connect(f int, o *omRoom) {
	r.conn[f] = o
	o.conn[faceOpposite[f]] = r
}

func (r *omRoom) updateOpenings() {
	for i := range 6 {
		r.open[i] = r.conn[i] != nil
	}
}

func (r *omRoom) findSource(scan int) bool {
	if r.source {
		return true
	}
	r.scan = scan
	for i := range 6 {
		if c := r.conn[i]; c != nil && r.open[i] && c.scan != scan && c.findSource(scan) {
			return true
		}
	}
	return false
}

func (r *omRoom) special() bool { return r.index >= 75 }

func (r *omRoom) openings() int {
	n := 0
	for _, o := range r.open {
		if o {
			n++
		}
	}
	return n
}

// Monument piece kinds.
const (
	omBuilding = iota
	omEntry
	omCore
	omDoubleX
	omDoubleXY
	omDoubleY
	omDoubleYZ
	omDoubleZ
	omSimple
	omSimpleTop
	omWing
	omPenthouse
)

type omPiece struct {
	opiece
	kind   int
	room   *omRoom
	design int // Simple room's main design; a wing room's design.
}

type monument struct {
	building *omPiece
	children []*omPiece
}

// Monument constants (P151: y0 = sea level - 24; P161: the building is 58 x 23 x 58).
const (
	omSeaLevel = 63
	omY0       = omSeaLevel - 24
	omReach    = 2 // Chunks from the start the building reaches (x0 = cx*16 - 21 .. x0 + 57, rings to +-5).
)

// monumentSeeds are the two floats of the start seed (P151 step 1): nextFloat() twice of MT(levelSeed32).
func (s *Structures) monumentSeeds() (float32, float32) {
	s.omOnce.Do(func() {
		r := worldgen.NewBedrockRandom(s.seed32)
		s.omF1, s.omF2 = r.NextFloat(), r.NextFloat()
	})
	return s.omF1, s.omF2
}

// monumentStartSeed is P151 step 2: (int64)(f1 * cx) ^ (int64)(f2 * cz) ^ seed, low 32 bits, with float
// multiplies truncated toward zero.
func (s *Structures) monumentStartSeed(cx, cz int) uint32 {
	f1, f2 := s.monumentSeeds()
	return uint32(int64(f1*float32(cx))) ^ uint32(int64(f2*float32(cz))) ^ s.seed32
}

// monumentAtStart assembles the monument of start chunk (cx, cz) (P151 steps 2 to 6).
func (s *Structures) monumentAtStart(cx, cz int) *monument {
	seed := s.monumentStartSeed(cx, cz)
	r := worldgen.NewBedrockRandom(seed)
	// BEDROCK: 0 north, 1 south, 2 west, 3 east.
	o := [...]orient{oN, oS, oW, oE}[r.NextIntN(4)]
	x0, z0 := cx*16-21, cz*16-21 // BEDROCK: +8 in x and z.
	b := &omPiece{opiece: opiece{b: box{x0, omY0, z0, x0 + 57, omY0 + 22, z0 + 57}, o: o}, kind: omBuilding}
	m := &monument{building: b}

	// The room graph (P161, generateRoomGraph).
	var grid [75]*omRoom
	for x := range 5 {
		for z := range 4 {
			grid[omIndex(x, 0, z)] = &omRoom{index: omIndex(x, 0, z)}
		}
	}
	for x := range 5 {
		for z := range 4 {
			grid[omIndex(x, 1, z)] = &omRoom{index: omIndex(x, 1, z)}
		}
	}
	for x := 1; x <= 3; x++ {
		for z := range 2 {
			grid[omIndex(x, 2, z)] = &omRoom{index: omIndex(x, 2, z)}
		}
	}
	source := grid[omIndex(2, 0, 0)]
	for x := range 5 {
		for z := range 5 {
			for y := range 3 {
				room := grid[omIndex(x, y, z)]
				if room == nil {
					continue
				}
				for f := range 6 {
					st := faceStep[f]
					nx, ny, nz := x+st[0], y+st[1], z+st[2]
					if nx < 0 || nx >= 5 || nz < 0 || nz >= 5 || ny < 0 || ny >= 3 {
						continue
					}
					n := grid[omIndex(nx, ny, nz)]
					if n == nil {
						continue
					}
					if nz == z {
						room.connect(f, n)
					} else {
						room.connect(faceOpposite[f], n)
					}
				}
			}
		}
	}
	roof, left, right := &omRoom{index: omRoof}, &omRoom{index: omLeftWing}, &omRoom{index: omRightWing}
	grid[omIndex(2, 2, 0)].connect(omUp, roof)
	grid[omIndex(0, 1, 0)].connect(omSouth, left)
	grid[omIndex(4, 1, 0)].connect(omSouth, right)
	roof.claimed, left.claimed, right.claimed = true, true, true
	source.source = true
	core := grid[omIndex(int(r.NextIntN(4)), 0, 2)]
	core.claimed = true
	core.conn[omEast].claimed = true
	core.conn[omNorth].claimed = true
	core.conn[omEast].conn[omNorth].claimed = true
	core.conn[omUp].claimed = true
	core.conn[omEast].conn[omUp].claimed = true
	core.conn[omNorth].conn[omUp].claimed = true
	core.conn[omEast].conn[omNorth].conn[omUp].claimed = true
	var defs []*omRoom
	for _, room := range grid {
		if room != nil {
			room.updateOpenings()
			defs = append(defs, room)
		}
	}
	roof.updateOpenings()
	// BEDROCK shuffle: a fresh random on the start seed, bound k, R not advanced (P151 step 5).
	sr := worldgen.NewBedrockRandom(seed)
	for k := len(defs) - 1; k >= 1; k-- {
		j := int(sr.NextIntN(int32(k)))
		defs[k], defs[j] = defs[j], defs[k]
	}
	scan := 1
	for _, d := range defs {
		closed, tries := 0, 0
		for closed < 2 && tries < 5 {
			tries++
			f := int(r.NextIntN(6))
			if !d.open[f] {
				continue
			}
			of := faceOpposite[f]
			d.open[f] = false
			d.conn[f].open[of] = false
			ok := d.findSource(scan)
			scan++
			if ok {
				ok = d.conn[f].findSource(scan)
				scan++
			}
			if ok {
				closed++
			} else {
				d.open[f] = true
				d.conn[f].open[of] = true
			}
		}
	}
	defs = append(defs, roof, left, right)

	// The building's children (P161, MonumentBuilding).
	source.claimed = true
	m.children = append(m.children, m.room(omEntry, source, 1, 1, 1), m.room(omCore, core, 2, 2, 2))
	for _, d := range defs {
		if d.claimed || d.special() {
			continue
		}
		m.children = append(m.children, m.fit(d, r))
	}
	ox, oy, oz := b.world(9, 0, 22)
	for _, c := range m.children {
		c.b = box{c.b[0] + ox, c.b[1] + oy, c.b[2] + oz, c.b[3] + ox, c.b[4] + oy, c.b[5] + oz}
	}
	corners := func(x0, y0, z0, x1, y1, z1 int) box {
		ax, ay, az := b.world(x0, y0, z0)
		bx, by, bz := b.world(x1, y1, z1)
		return box{min(ax, bx), min(ay, by), min(az, bz), max(ax, bx), max(ay, by), max(az, bz)}
	}
	wr := r.NextInt()
	m.children = append(m.children,
		&omPiece{opiece: opiece{b: corners(1, 1, 1, 23, 8, 21), o: o}, kind: omWing, design: int(wr & 1)},
		&omPiece{opiece: opiece{b: corners(34, 1, 1, 56, 8, 21), o: o}, kind: omWing, design: int((wr + 1) & 1)},
		&omPiece{opiece: opiece{b: corners(22, 13, 22, 35, 17, 35), o: o}, kind: omPenthouse})
	return m
}

// fit runs the fitters in their order on a room definition (P151 step 6).
func (m *monument) fit(d *omRoom, r *worldgen.BedrockRandom) *omPiece {
	e := d.conn[omEast]
	n := d.conn[omNorth]
	switch {
	case d.open[omEast] && !e.claimed && d.open[omUp] && !d.conn[omUp].claimed && e.open[omUp] && !e.conn[omUp].claimed:
		d.claimed, e.claimed, d.conn[omUp].claimed, e.conn[omUp].claimed = true, true, true, true
		return m.room(omDoubleXY, d, 2, 2, 1)
	case d.open[omNorth] && !n.claimed && d.open[omUp] && !d.conn[omUp].claimed && n.open[omUp] && !n.conn[omUp].claimed:
		d.claimed, n.claimed, d.conn[omUp].claimed, n.conn[omUp].claimed = true, true, true, true
		return m.room(omDoubleYZ, d, 1, 2, 2)
	case d.open[omNorth] && !n.claimed:
		d.claimed, n.claimed = true, true
		return m.room(omDoubleZ, d, 1, 1, 2)
	case d.open[omEast] && !e.claimed:
		d.claimed, e.claimed = true, true
		return m.room(omDoubleX, d, 2, 1, 1)
	case d.open[omUp] && !d.conn[omUp].claimed:
		d.claimed, d.conn[omUp].claimed = true, true
		return m.room(omDoubleY, d, 1, 2, 1)
	case !d.open[omWest] && !d.open[omEast] && !d.open[omNorth] && !d.open[omSouth] && !d.open[omUp]:
		d.claimed = true
		return m.room(omSimpleTop, d, 1, 1, 1)
	}
	d.claimed = true
	p := m.room(omSimple, d, 1, 1, 1)
	p.design = int(r.NextIntN(3))
	return p
}

// room is a room piece's box before the building offset (P161 makeBoundingBox): w x h x d grid cells
// of 8 x 4 x 8 blocks at the room's grid position, turned by the building's direction.
func (m *monument) room(kind int, d *omRoom, w, h, dp int) *omPiece {
	o := m.building.o
	rx, ry, rz := d.index%5, d.index/25, d.index/5%5
	sx, sy, sz := w*8, h*4, dp*8
	if o == oW || o == oE {
		sx, sz = sz, sx
	}
	var mx, mz int
	switch o {
	case oN:
		mx, mz = rx*8, -(rz+dp)*8+1
	case oS:
		mx, mz = rx*8, rz*8
	case oW:
		mx, mz = -(rz+dp)*8+1, rx*8
	default:
		mx, mz = rz*8, rx*8
	}
	my := ry * 4
	return &omPiece{opiece: opiece{b: box{mx, my, mz, mx + sx - 1, my + sy - 1, mz + sz - 1}, o: o}, kind: kind, room: d}
}

// omBlocks are the monument's blocks (P161).
type omBlocks struct {
	gray, light, dark, lantern, gold, sponge, water, air uint32
}

var (
	omOnce sync.Once
	omB    omBlocks
)

func monumentBlocks() *omBlocks {
	omOnce.Do(func() {
		omB = omBlocks{gray: ridNamed("prismarine", nil), light: ridNamed("prismarine_bricks", nil), dark: ridNamed("dark_prismarine", nil),
			lantern: ridNamed("sea_lantern", nil), gold: ridNamed("gold_block", nil), sponge: ridNamed("wet_sponge", nil),
			water: waterRID(), air: airRID()}
	})
	return &omB
}

// om is a monument piece being post-processed.
type om struct {
	ctx
	bl *omBlocks
	p  *omPiece
}

func (c *om) fill(x0, y0, z0, x1, y1, z1 int, b uint32) { c.box(x0, y0, z0, x1, y1, z1, b, b, false) }

// water is WATERBOX (BEDROCK, P151 item 8): water below sea level, air at and above, whatever was there.
func (c *om) water(x0, y0, z0, x1, y1, z1 int) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				wx, wy, wz := c.p.world(x, y, z)
				if !c.w.in(wx, wy, wz) {
					continue
				}
				if wy < omSeaLevel {
					c.w.set(wx, wy, wz, c.bl.water, false)
				} else {
					c.w.set(wx, wy, wz, c.bl.air, false)
				}
			}
		}
	}
}

// wasWater says whether a cell held water when BDS post-processed the monument. BDS places its
// structures before the water plants, which our chunk already has: a seagrass or kelp cell was water.
func (c *om) wasWater(wx, wy, wz int) bool {
	if !c.w.in(wx, wy, wz) {
		return false
	}
	switch c.w.blockName(wx, wy, wz) {
	case "water", "flowing_water", "seagrass", "kelp":
		return true
	}
	return false
}

// replaceWater writes b where a cell is water now (P161 replace-water-with).
func (c *om) replaceWater(x0, y0, z0, x1, y1, z1 int, b uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				wx, wy, wz := c.p.world(x, y, z)
				if c.wasWater(wx, wy, wz) {
					c.w.set(wx, wy, wz, b, false)
				}
			}
		}
	}
}

// columnDown is column-down (P161): from the cell down, b while the cell is air or a liquid, to the
// dimension floor; nothing when the start is outside the chunk.
func (c *om) columnDown(x, y, z int, b uint32) {
	wx, wy, wz := c.p.world(x, y, z)
	if !c.w.in(wx, wy, wz) {
		return
	}
	for ; wy > c.w.lo+1; wy-- {
		if !c.w.replaceable(wx, wy, wz) && !c.wasWater(wx, wy, wz) {
			return
		}
		c.w.set(wx, wy, wz, b, false)
	}
}

// chunkIntersects is P161's chunkIntersects: the x/z rectangle of two local corners touches the chunk.
func (c *om) chunkIntersects(x0, z0, x1, z1 int) bool {
	ax, _, az := c.p.world(x0, 0, z0)
	bx, _, bz := c.p.world(x1, 0, z1)
	cb := c.w.chunkBox()
	return min(ax, bx) <= cb[3] && max(ax, bx) >= cb[0] && min(az, bz) <= cb[5] && max(az, bz) >= cb[2]
}

func (c *om) elder(x, y, z int) {
	wx, wy, wz := c.p.world(x, y, z)
	if c.w.in(wx, wy, wz) {
		c.w.spawns = append(c.w.spawns, spawnIntent{Pos: [3]int{wx, wy, wz}, Entity: "minecraft:elder_guardian"})
	}
}

// floor is generateDefaultFloor (P161).
func (c *om) floor(xo, zo int, open bool) {
	g, l := c.bl.gray, c.bl.light
	if !open {
		c.fill(xo, 0, zo, xo+7, 0, zo+7, g)
		return
	}
	c.fill(xo, 0, zo, xo+2, 0, zo+7, g)
	c.fill(xo+5, 0, zo, xo+7, 0, zo+7, g)
	c.fill(xo+3, 0, zo, xo+4, 0, zo+2, g)
	c.fill(xo+3, 0, zo+5, xo+4, 0, zo+7, g)
	c.fill(xo+3, 0, zo+2, xo+4, 0, zo+2, l)
	c.fill(xo+3, 0, zo+5, xo+4, 0, zo+5, l)
	c.fill(xo+2, 0, zo+3, xo+2, 0, zo+4, l)
	c.fill(xo+5, 0, zo+3, xo+5, 0, zo+4, l)
}

// placeMonuments post-processes every monument that reaches the chunk.
func (s *Structures) placeMonuments(w *writer) {
	sp := spreads[Monument]
	for rx := floorDiv(w.cx-omReach, sp.spacing); rx <= floorDiv(w.cx+omReach, sp.spacing); rx++ {
		for rz := floorDiv(w.cz-omReach, sp.spacing); rz <= floorDiv(w.cz+omReach, sp.spacing); rz++ {
			mx, mz := sp.candidate(s, rx, rz, worldgen.NewBedrockRandom(0))
			if abs(mx-w.cx) > omReach || abs(mz-w.cz) > omReach {
				continue
			}
			v := s.monuments.get([2]int{mx, mz}, func() any {
				if !s.monumentAt(mx, mz) {
					return (*monument)(nil)
				}
				return s.monumentAtStart(mx, mz)
			})
			if m := v.(*monument); m != nil {
				s.monumentPost(w, m)
			}
		}
	}
}

// monumentPost is the building's postProcess in one chunk (P161 order), then its children that touch
// the chunk, in child order.
func (s *Structures) monumentPost(w *writer, m *monument) {
	cb := w.chunkBox()
	if !m.building.b.intersects(cb) {
		return
	}
	c := &om{ctx: ctx{w: w, p: &m.building.opiece, r: w.rng}, bl: monumentBlocks(), p: m.building}
	c.building()
	for _, ch := range m.children {
		if ch.b.intersects(cb) {
			cc := &om{ctx: ctx{w: w, p: &ch.opiece, r: w.rng}, bl: c.bl, p: ch}
			cc.roomPost()
		}
	}
}

func (c *om) building() {
	g, l := c.bl.gray, c.bl.light
	// BEDROCK: no whole-box water fill first (P151 item 7).
	c.wing(false, 0)
	c.wing(true, 33)
	c.entranceArches()
	c.entranceWall()
	c.roofPiece()
	c.lowerWall()
	c.middleWall()
	c.upperWall()
	for px := range 7 {
		for pz := 0; pz < 7; {
			if pz == 0 && px == 3 {
				pz = 6
			}
			bx, bz := px*9, pz*9
			for w := range 4 {
				for d := range 4 {
					c.set(bx+w, 0, bz+d, l)
					c.columnDown(bx+w, -1, bz+d, l)
				}
			}
			if px != 0 && px != 6 {
				pz += 6
			} else {
				pz++
			}
		}
	}
	for i := range 5 {
		c.water(-1-i, i*2, -1-i, -1-i, 23, 58+i)
		c.water(58+i, i*2, -1-i, 58+i, 23, 58+i)
		c.water(-i, i*2, -1-i, 57+i, 23, -1-i)
		c.water(-i, i*2, 58+i, 57+i, 23, 58+i)
	}
	_ = g
}

func (c *om) wing(flipped bool, xo int) {
	g, l := c.bl.gray, c.bl.light
	if !c.chunkIntersects(xo, 0, xo+23, 20) {
		return
	}
	c.fill(xo, 0, 0, xo+24, 0, 20, g)
	c.water(xo, 1, 0, xo+24, 10, 20)
	for i := range 4 {
		c.fill(xo+i, i+1, i, xo+i, i+1, 20, l)
		c.fill(xo+i+7, i+5, i+7, xo+i+7, i+5, 20, l)
		c.fill(xo+17-i, i+5, i+7, xo+17-i, i+5, 20, l)
		c.fill(xo+24-i, i+1, i, xo+24-i, i+1, 20, l)
		c.fill(xo+i+1, i+1, i, xo+23-i, i+1, i, l)
		c.fill(xo+i+8, i+5, i+7, xo+16-i, i+5, i+7, l)
	}
	c.fill(xo+4, 4, 4, xo+6, 4, 20, g)
	c.fill(xo+7, 4, 4, xo+17, 4, 6, g)
	c.fill(xo+18, 4, 4, xo+20, 4, 20, g)
	c.fill(xo+11, 8, 11, xo+13, 8, 20, g)
	c.set(xo+12, 9, 12, l)
	c.set(xo+12, 9, 15, l)
	c.set(xo+12, 9, 18, l)
	leftPos, rightPos := xo+5, xo+19
	if flipped {
		leftPos, rightPos = xo+19, xo+5
	}
	for z := 20; z >= 5; z -= 3 {
		c.set(leftPos, 5, z, l)
	}
	for z := 19; z >= 7; z -= 3 {
		c.set(rightPos, 5, z, l)
	}
	for i := range 4 {
		pos := xo + 17 - i*3
		if flipped {
			pos = xo + 24 - (17 - i*3)
		}
		c.set(pos, 5, 5, l)
	}
	c.set(rightPos, 5, 5, l)
	c.fill(xo+11, 1, 12, xo+13, 7, 12, g)
	c.fill(xo+12, 1, 11, xo+12, 7, 13, g)
}

func (c *om) entranceArches() {
	g, l, lan := c.bl.gray, c.bl.light, c.bl.lantern
	if !c.chunkIntersects(22, 5, 35, 17) {
		return
	}
	c.water(25, 0, 0, 32, 8, 20)
	for i := range 4 {
		z := 5 + i*4
		c.fill(24, 2, z, 24, 4, z, l)
		c.fill(22, 4, z, 23, 4, z, l)
		c.set(25, 5, z, l)
		c.set(26, 6, z, l)
		c.set(26, 5, z, lan)
		c.fill(33, 2, z, 33, 4, z, l)
		c.fill(34, 4, z, 35, 4, z, l)
		c.set(32, 5, z, l)
		c.set(31, 6, z, l)
		c.set(31, 5, z, lan)
		c.fill(27, 6, z, 30, 6, z, g)
	}
}

func (c *om) entranceWall() {
	g, l, d := c.bl.gray, c.bl.light, c.bl.dark
	if !c.chunkIntersects(15, 20, 42, 21) {
		return
	}
	c.fill(15, 0, 21, 42, 0, 21, g)
	c.water(26, 1, 21, 31, 3, 21)
	c.fill(21, 12, 21, 36, 12, 21, g)
	c.fill(17, 11, 21, 40, 11, 21, g)
	c.fill(16, 10, 21, 41, 10, 21, g)
	c.fill(15, 7, 21, 42, 9, 21, g)
	c.fill(16, 6, 21, 41, 6, 21, g)
	c.fill(17, 5, 21, 40, 5, 21, g)
	c.fill(21, 4, 21, 36, 4, 21, g)
	c.fill(22, 3, 21, 26, 3, 21, g)
	c.fill(31, 3, 21, 35, 3, 21, g)
	c.fill(23, 2, 21, 25, 2, 21, g)
	c.fill(32, 2, 21, 34, 2, 21, g)
	c.fill(28, 4, 20, 29, 4, 21, l)
	c.set(27, 3, 21, l)
	c.set(30, 3, 21, l)
	c.set(26, 2, 21, l)
	c.set(31, 2, 21, l)
	c.set(25, 1, 21, l)
	c.set(32, 1, 21, l)
	for i := range 7 {
		c.set(28-i, 6+i, 21, d)
		c.set(29+i, 6+i, 21, d)
	}
	for i := range 4 {
		c.set(28-i, 9+i, 21, d)
		c.set(29+i, 9+i, 21, d)
	}
	c.set(28, 12, 21, d)
	c.set(29, 12, 21, d)
	for i := range 3 {
		c.set(22-i*2, 8, 21, d)
		c.set(22-i*2, 9, 21, d)
		c.set(35+i*2, 8, 21, d)
		c.set(35+i*2, 9, 21, d)
	}
	c.water(15, 13, 21, 42, 15, 21)
	c.water(15, 1, 21, 15, 6, 21)
	c.water(16, 1, 21, 16, 5, 21)
	c.water(17, 1, 21, 20, 4, 21)
	c.water(21, 1, 21, 21, 3, 21)
	c.water(22, 1, 21, 22, 2, 21)
	c.water(23, 1, 21, 24, 1, 21)
	c.water(42, 1, 21, 42, 6, 21)
	c.water(41, 1, 21, 41, 5, 21)
	c.water(37, 1, 21, 40, 4, 21)
	c.water(36, 1, 21, 36, 3, 21)
	c.water(33, 1, 21, 34, 1, 21)
	c.water(35, 1, 21, 35, 2, 21)
}

func (c *om) roofPiece() {
	g, l, lan := c.bl.gray, c.bl.light, c.bl.lantern
	if !c.chunkIntersects(21, 21, 36, 36) {
		return
	}
	c.fill(21, 0, 22, 36, 0, 36, g)
	c.water(21, 1, 22, 36, 23, 36)
	for i := range 4 {
		c.fill(21+i, 13+i, 21+i, 36-i, 13+i, 21+i, l)
		c.fill(21+i, 13+i, 36-i, 36-i, 13+i, 36-i, l)
		c.fill(21+i, 13+i, 22+i, 21+i, 13+i, 35-i, l)
		c.fill(36-i, 13+i, 22+i, 36-i, 13+i, 35-i, l)
	}
	c.fill(25, 16, 25, 32, 16, 32, g)
	c.fill(25, 17, 25, 25, 19, 25, l)
	c.fill(32, 17, 25, 32, 19, 25, l)
	c.fill(25, 17, 32, 25, 19, 32, l)
	c.fill(32, 17, 32, 32, 19, 32, l)
	c.set(26, 20, 26, l)
	c.set(27, 21, 27, l)
	c.set(27, 20, 27, lan)
	c.set(26, 20, 31, l)
	c.set(27, 21, 30, l)
	c.set(27, 20, 30, lan)
	c.set(31, 20, 31, l)
	c.set(30, 21, 30, l)
	c.set(30, 20, 30, lan)
	c.set(31, 20, 26, l)
	c.set(30, 21, 27, l)
	c.set(30, 20, 27, lan)
	c.fill(28, 21, 27, 29, 21, 27, g)
	c.fill(27, 21, 28, 27, 21, 29, g)
	c.fill(28, 21, 30, 29, 21, 30, g)
	c.fill(30, 21, 28, 30, 21, 29, g)
}

func (c *om) lowerWall() {
	g, l := c.bl.gray, c.bl.light
	if c.chunkIntersects(0, 21, 6, 58) {
		c.fill(0, 0, 21, 6, 0, 57, g)
		c.water(0, 1, 21, 6, 7, 57)
		c.fill(4, 4, 21, 6, 4, 53, g)
		for i := range 4 {
			c.fill(i, i+1, 21, i, i+1, 57-i, l)
		}
		for z := 23; z < 53; z += 3 {
			c.set(5, 5, z, l)
		}
		c.set(5, 5, 52, l)
		for i := range 4 {
			c.fill(i, i+1, 21, i, i+1, 57-i, l)
		}
		c.fill(4, 1, 52, 6, 3, 52, g)
		c.fill(5, 1, 51, 5, 3, 53, g)
	}
	if c.chunkIntersects(51, 21, 58, 58) {
		c.fill(51, 0, 21, 57, 0, 57, g)
		c.water(51, 1, 21, 57, 7, 57)
		c.fill(51, 4, 21, 53, 4, 53, g)
		for i := range 4 {
			c.fill(57-i, i+1, 21, 57-i, i+1, 57-i, l)
		}
		for z := 23; z < 53; z += 3 {
			c.set(52, 5, z, l)
		}
		c.set(52, 5, 52, l)
		c.fill(51, 1, 52, 53, 3, 52, g)
		c.fill(52, 1, 51, 52, 3, 53, g)
	}
	if c.chunkIntersects(0, 51, 57, 57) {
		c.fill(7, 0, 51, 50, 0, 57, g)
		c.water(7, 1, 51, 50, 10, 57)
		for i := range 4 {
			c.fill(i+1, i+1, 57-i, 56-i, i+1, 57-i, l)
		}
	}
}

func (c *om) middleWall() {
	g, l := c.bl.gray, c.bl.light
	if c.chunkIntersects(7, 21, 13, 54) { // BEDROCK: to z 54 (P151 item 9).
		c.fill(7, 0, 21, 13, 0, 50, g)
		c.water(7, 1, 21, 13, 10, 50)
		c.fill(11, 8, 21, 13, 8, 53, g)
		for i := range 4 {
			c.fill(i+7, i+5, 21, i+7, i+5, 54, l)
		}
		for z := 21; z <= 45; z += 3 {
			c.set(12, 9, z, l)
		}
	}
	if c.chunkIntersects(44, 21, 50, 54) {
		c.fill(44, 0, 21, 50, 0, 50, g)
		c.water(44, 1, 21, 50, 10, 50)
		c.fill(44, 8, 21, 46, 8, 53, g)
		for i := range 4 {
			c.fill(50-i, i+5, 21, 50-i, i+5, 54, l)
		}
		for z := 21; z <= 45; z += 3 {
			c.set(45, 9, z, l)
		}
	}
	if c.chunkIntersects(8, 44, 49, 54) {
		c.fill(14, 0, 44, 43, 0, 50, g)
		c.water(14, 1, 44, 43, 10, 50)
		for x := 12; x <= 45; x += 3 {
			c.set(x, 9, 45, l)
			c.set(x, 9, 52, l)
			if x == 12 || x == 18 || x == 24 || x == 33 || x == 39 || x == 45 {
				c.set(x, 9, 47, l)
				c.set(x, 9, 50, l)
				c.set(x, 10, 45, l)
				c.set(x, 10, 46, l)
				c.set(x, 10, 51, l)
				c.set(x, 10, 52, l)
				c.set(x, 11, 47, l)
				c.set(x, 11, 50, l)
				c.set(x, 12, 48, l)
				c.set(x, 12, 49, l)
			}
		}
		for i := range 3 {
			c.fill(8+i, 5+i, 54, 49-i, 5+i, 54, g)
		}
		c.fill(11, 8, 54, 46, 8, 54, l)
		c.fill(14, 8, 44, 43, 8, 53, g)
	}
}

func (c *om) upperWall() {
	g, l := c.bl.gray, c.bl.light
	if c.chunkIntersects(14, 21, 20, 43) {
		c.fill(14, 0, 21, 20, 0, 43, g)
		c.water(14, 1, 22, 20, 14, 43)
		c.fill(18, 12, 22, 20, 12, 39, g)
		c.fill(18, 12, 21, 20, 12, 21, l)
		for i := range 4 {
			c.fill(i+14, i+9, 21, i+14, i+9, 43-i, l)
		}
		for z := 23; z <= 39; z += 3 {
			c.set(19, 13, z, l)
		}
	}
	if c.chunkIntersects(37, 21, 43, 43) {
		c.fill(37, 0, 21, 43, 0, 43, g)
		c.water(37, 1, 22, 43, 14, 43)
		c.fill(37, 12, 22, 39, 12, 39, g)
		c.fill(37, 12, 21, 39, 12, 21, l)
		for i := range 4 {
			c.fill(43-i, i+9, 21, 43-i, i+9, 43-i, l)
		}
		for z := 23; z <= 39; z += 3 {
			c.set(38, 13, z, l)
		}
	}
	if c.chunkIntersects(15, 37, 42, 43) {
		c.fill(21, 0, 37, 36, 0, 43, g)
		c.water(21, 1, 37, 36, 14, 43)
		c.fill(21, 12, 37, 36, 12, 39, g)
		for i := range 4 {
			c.fill(15+i, i+9, 43-i, 42-i, i+9, 43-i, l)
		}
		for x := 21; x <= 36; x += 3 {
			c.set(x, 13, 38, l)
		}
	}
}

// roomPost is a child piece's postProcess (P161).
func (c *om) roomPost() {
	switch c.p.kind {
	case omEntry:
		c.entry()
	case omCore:
		c.core()
	case omDoubleX:
		c.doubleX()
	case omDoubleXY:
		c.doubleXY()
	case omDoubleY:
		c.doubleY()
	case omDoubleYZ:
		c.doubleYZ()
	case omDoubleZ:
		c.doubleZ()
	case omSimple:
		c.simple()
	case omSimpleTop:
		c.simpleTop()
	case omWing:
		c.wingRoom()
	case omPenthouse:
		c.penthouse()
	}
}

func (c *om) entry() {
	l, rd := c.bl.light, c.p.room
	c.fill(0, 3, 0, 2, 3, 7, l)
	c.fill(5, 3, 0, 7, 3, 7, l)
	c.fill(0, 2, 0, 1, 2, 7, l)
	c.fill(6, 2, 0, 7, 2, 7, l)
	c.fill(0, 1, 0, 0, 1, 7, l)
	c.fill(7, 1, 0, 7, 1, 7, l)
	c.fill(0, 1, 7, 7, 3, 7, l)
	c.fill(1, 1, 0, 2, 3, 0, l)
	c.fill(5, 1, 0, 6, 3, 0, l)
	if rd.open[omNorth] {
		c.water(3, 1, 7, 4, 2, 7)
	}
	if rd.open[omWest] {
		c.water(0, 1, 3, 1, 2, 4)
	}
	if rd.open[omEast] {
		c.water(6, 1, 3, 7, 2, 4)
	}
}

func (c *om) core() {
	g, l, d, lan := c.bl.gray, c.bl.light, c.bl.dark, c.bl.lantern
	c.replaceWater(1, 8, 0, 14, 8, 14, g)
	c.fill(0, 7, 0, 0, 7, 15, l)
	c.fill(15, 7, 0, 15, 7, 15, l)
	c.fill(1, 7, 0, 15, 7, 0, l)
	c.fill(1, 7, 15, 14, 7, 15, l)
	for y := 1; y <= 6; y++ {
		b := l
		if y == 2 || y == 6 {
			b = g
		}
		for x := 0; x <= 15; x += 15 {
			c.fill(x, y, 0, x, y, 1, b)
			c.fill(x, y, 6, x, y, 9, b)
			c.fill(x, y, 14, x, y, 15, b)
		}
		c.fill(1, y, 0, 1, y, 0, b)
		c.fill(6, y, 0, 9, y, 0, b)
		c.fill(14, y, 0, 14, y, 0, b)
		c.fill(1, y, 15, 14, y, 15, b)
	}
	c.fill(6, 3, 6, 9, 6, 9, d)
	c.fill(7, 4, 7, 8, 5, 8, c.bl.gold)
	for y := 3; y <= 6; y += 3 {
		for x := 6; x <= 9; x += 3 {
			c.set(x, y, 6, lan)
			c.set(x, y, 9, lan)
		}
	}
	for _, q := range [][6]int{
		{5, 1, 6, 5, 2, 6}, {5, 1, 9, 5, 2, 9}, {10, 1, 6, 10, 2, 6}, {10, 1, 9, 10, 2, 9},
		{6, 1, 5, 6, 2, 5}, {9, 1, 5, 9, 2, 5}, {6, 1, 10, 6, 2, 10}, {9, 1, 10, 9, 2, 10},
		{5, 2, 5, 5, 6, 5}, {5, 2, 10, 5, 6, 10}, {10, 2, 5, 10, 6, 5}, {10, 2, 10, 10, 6, 10},
		{5, 7, 1, 5, 7, 6}, {10, 7, 1, 10, 7, 6}, {5, 7, 9, 5, 7, 14}, {10, 7, 9, 10, 7, 14},
		{1, 7, 5, 6, 7, 5}, {1, 7, 10, 6, 7, 10}, {9, 7, 5, 14, 7, 5}, {9, 7, 10, 14, 7, 10},
		{2, 1, 2, 2, 1, 3}, {3, 1, 2, 3, 1, 2}, {13, 1, 2, 13, 1, 3}, {12, 1, 2, 12, 1, 2},
		{2, 1, 12, 2, 1, 13}, {3, 1, 13, 3, 1, 13}, {13, 1, 12, 13, 1, 13}, {12, 1, 13, 12, 1, 13},
	} {
		c.fill(q[0], q[1], q[2], q[3], q[4], q[5], l)
	}
}

// doorways are the water boxes of a one-floor pair of rooms: the openings of the first (a) and the
// second (b) definition, at floor height y.
func (c *om) doubleXDoors(west, east *omRoom, y int) {
	if west.open[omSouth] {
		c.water(3, y, 0, 4, y+1, 0)
	}
	if west.open[omNorth] {
		c.water(3, y, 7, 4, y+1, 7)
	}
	if west.open[omWest] {
		c.water(0, y, 3, 0, y+1, 4)
	}
	if east.open[omSouth] {
		c.water(11, y, 0, 12, y+1, 0)
	}
	if east.open[omNorth] {
		c.water(11, y, 7, 12, y+1, 7)
	}
	if east.open[omEast] {
		c.water(15, y, 3, 15, y+1, 4)
	}
}

func (c *om) doubleX() {
	g, l, lan := c.bl.gray, c.bl.light, c.bl.lantern
	west := c.p.room
	east := west.conn[omEast]
	if west.index/25 > 0 {
		c.floor(8, 0, east.open[omDown])
		c.floor(0, 0, west.open[omDown])
	}
	if west.conn[omUp] == nil {
		c.replaceWater(1, 4, 1, 7, 4, 6, g)
	}
	if east.conn[omUp] == nil {
		c.replaceWater(8, 4, 1, 14, 4, 6, g)
	}
	for _, row := range []struct {
		y int
		b uint32
	}{{3, l}, {2, g}, {1, l}} {
		c.fill(0, row.y, 0, 0, row.y, 7, row.b)
		c.fill(15, row.y, 0, 15, row.y, 7, row.b)
		c.fill(1, row.y, 0, 15, row.y, 0, row.b)
		c.fill(1, row.y, 7, 14, row.y, 7, row.b)
	}
	c.fill(5, 1, 0, 10, 1, 4, l)
	c.fill(6, 2, 0, 9, 2, 3, g)
	c.fill(5, 3, 0, 10, 3, 4, l)
	c.set(6, 2, 3, lan)
	c.set(9, 2, 3, lan)
	c.doubleXDoors(west, east, 1)
}

func (c *om) doubleXY() {
	g, l, lan := c.bl.gray, c.bl.light, c.bl.lantern
	west := c.p.room
	east := west.conn[omEast]
	westUp, eastUp := west.conn[omUp], east.conn[omUp]
	if west.index/25 > 0 {
		c.floor(8, 0, east.open[omDown])
		c.floor(0, 0, west.open[omDown])
	}
	if westUp.conn[omUp] == nil {
		c.replaceWater(1, 8, 1, 7, 8, 6, g)
	}
	if eastUp.conn[omUp] == nil {
		c.replaceWater(8, 8, 1, 14, 8, 6, g)
	}
	for y := 1; y <= 7; y++ {
		b := l
		if y == 2 || y == 6 {
			b = g
		}
		c.fill(0, y, 0, 0, y, 7, b)
		c.fill(15, y, 0, 15, y, 7, b)
		c.fill(1, y, 0, 15, y, 0, b)
		c.fill(1, y, 7, 14, y, 7, b)
	}
	for _, q := range [][6]int{
		{2, 1, 3, 2, 7, 4}, {3, 1, 2, 4, 7, 2}, {3, 1, 5, 4, 7, 5}, {13, 1, 3, 13, 7, 4}, {11, 1, 2, 12, 7, 2},
		{11, 1, 5, 12, 7, 5}, {5, 1, 3, 5, 3, 4}, {10, 1, 3, 10, 3, 4}, {5, 7, 2, 10, 7, 5}, {5, 5, 2, 5, 7, 2},
		{10, 5, 2, 10, 7, 2}, {5, 5, 5, 5, 7, 5}, {10, 5, 5, 10, 7, 5},
	} {
		c.fill(q[0], q[1], q[2], q[3], q[4], q[5], l)
	}
	c.set(6, 6, 2, l)
	c.set(9, 6, 2, l)
	c.set(6, 6, 5, l)
	c.set(9, 6, 5, l)
	c.fill(5, 4, 3, 6, 4, 4, l)
	c.fill(9, 4, 3, 10, 4, 4, l)
	c.set(5, 4, 2, lan)
	c.set(5, 4, 5, lan)
	c.set(10, 4, 2, lan)
	c.set(10, 4, 5, lan)
	c.doubleXDoors(west, east, 1)
	c.doubleXDoors(westUp, eastUp, 5)
}

func (c *om) doubleY() {
	g, l := c.bl.gray, c.bl.light
	rd := c.p.room
	if rd.index/25 > 0 {
		c.floor(0, 0, rd.open[omSouth]) // BEDROCK: the SOUTH bit (P151 item 10).
	}
	above := rd.conn[omUp]
	if above.conn[omUp] == nil {
		c.replaceWater(1, 8, 1, 6, 8, 6, g)
	}
	for _, q := range [][6]int{
		{0, 4, 0, 0, 4, 7}, {7, 4, 0, 7, 4, 7}, {1, 4, 0, 6, 4, 0}, {1, 4, 7, 6, 4, 7}, {2, 4, 1, 2, 4, 2}, {1, 4, 2, 1, 4, 2},
		{5, 4, 1, 5, 4, 2}, {6, 4, 2, 6, 4, 2}, {2, 4, 5, 2, 4, 6}, {1, 4, 5, 1, 4, 5}, {5, 4, 5, 5, 4, 6}, {6, 4, 5, 6, 4, 5},
	} {
		c.fill(q[0], q[1], q[2], q[3], q[4], q[5], l)
	}
	d := rd
	for y := 1; y <= 5; y += 4 {
		if d.open[omSouth] {
			c.fill(2, y, 0, 2, y+2, 0, l)
			c.fill(5, y, 0, 5, y+2, 0, l)
			c.fill(3, y+2, 0, 4, y+2, 0, l)
		} else {
			c.fill(0, y, 0, 7, y+2, 0, l)
			c.fill(0, y+1, 0, 7, y+1, 0, g)
		}
		if d.open[omNorth] {
			c.fill(2, y, 7, 2, y+2, 7, l)
			c.fill(5, y, 7, 5, y+2, 7, l)
			c.fill(3, y+2, 7, 4, y+2, 7, l)
		} else {
			c.fill(0, y, 7, 7, y+2, 7, l)
			c.fill(0, y+1, 7, 7, y+1, 7, g)
		}
		if d.open[omWest] {
			c.fill(0, y, 2, 0, y+2, 2, l)
			c.fill(0, y, 5, 0, y+2, 5, l)
			c.fill(0, y+2, 3, 0, y+2, 4, l)
		} else {
			c.fill(0, y, 0, 0, y+2, 7, l)
			c.fill(0, y+1, 0, 0, y+1, 7, g)
		}
		if d.open[omEast] {
			c.fill(7, y, 2, 7, y+2, 2, l)
			c.fill(7, y, 5, 7, y+2, 5, l)
			c.fill(7, y+2, 3, 7, y+2, 4, l)
		} else {
			c.fill(7, y, 0, 7, y+2, 7, l)
			c.fill(7, y+1, 0, 7, y+1, 7, g)
		}
		d = above
	}
}

func (c *om) doubleYZ() {
	g, l, dk, lan := c.bl.gray, c.bl.light, c.bl.dark, c.bl.lantern
	south := c.p.room
	north := south.conn[omNorth]
	northUp, southUp := north.conn[omUp], south.conn[omUp]
	if south.index/25 > 0 {
		c.floor(0, 8, north.open[omDown])
		c.floor(0, 0, south.open[omDown])
	}
	if southUp.conn[omUp] == nil {
		c.replaceWater(1, 8, 1, 6, 8, 7, g)
	}
	if northUp.conn[omUp] == nil {
		c.replaceWater(1, 8, 8, 6, 8, 14, g)
	}
	for y := 1; y <= 7; y++ {
		b := l
		if y == 2 || y == 6 {
			b = g
		}
		c.fill(0, y, 0, 0, y, 15, b)
		c.fill(7, y, 0, 7, y, 15, b)
		c.fill(1, y, 0, 6, y, 0, b)
		c.fill(1, y, 15, 6, y, 15, b)
	}
	for y := 1; y <= 7; y++ {
		b := dk
		if y == 2 || y == 6 {
			b = lan
		}
		c.fill(3, y, 7, 4, y, 8, b)
	}
	if south.open[omSouth] {
		c.water(3, 1, 0, 4, 2, 0)
	}
	if south.open[omEast] {
		c.water(7, 1, 3, 7, 2, 4)
	}
	if south.open[omWest] {
		c.water(0, 1, 3, 0, 2, 4)
	}
	if north.open[omNorth] {
		c.water(3, 1, 15, 4, 2, 15)
	}
	if north.open[omWest] {
		c.water(0, 1, 11, 0, 2, 12)
	}
	if north.open[omEast] {
		c.water(7, 1, 11, 7, 2, 12)
	}
	if southUp.open[omSouth] {
		c.water(3, 5, 0, 4, 6, 0)
	}
	if southUp.open[omEast] {
		c.water(7, 5, 3, 7, 6, 4)
		c.fill(5, 4, 2, 6, 4, 5, l)
		c.fill(6, 1, 2, 6, 3, 2, l)
		c.fill(6, 1, 5, 6, 3, 5, l)
	}
	if southUp.open[omWest] {
		c.water(0, 5, 3, 0, 6, 4)
		c.fill(1, 4, 2, 2, 4, 5, l)
		c.fill(1, 1, 2, 1, 3, 2, l)
		c.fill(1, 1, 5, 1, 3, 5, l)
	}
	if northUp.open[omNorth] {
		c.water(3, 5, 15, 4, 6, 15)
	}
	if northUp.open[omWest] {
		c.water(0, 5, 11, 0, 6, 12)
		c.fill(1, 4, 10, 2, 4, 13, l)
		c.fill(1, 1, 10, 1, 3, 10, l)
		c.fill(1, 1, 13, 1, 3, 13, l)
	}
	if northUp.open[omEast] {
		c.water(7, 5, 11, 7, 6, 12)
		c.fill(5, 4, 10, 6, 4, 13, l)
		c.fill(6, 1, 10, 6, 3, 10, l)
		c.fill(6, 1, 13, 6, 3, 13, l)
	}
}

func (c *om) doubleZ() {
	g, l, lan := c.bl.gray, c.bl.light, c.bl.lantern
	south := c.p.room
	north := south.conn[omNorth]
	if south.index/25 > 0 {
		c.floor(0, 8, north.open[omDown])
		c.floor(0, 0, south.open[omDown])
	}
	if south.conn[omUp] == nil {
		c.replaceWater(1, 4, 1, 6, 4, 7, g)
	}
	if north.conn[omUp] == nil {
		c.replaceWater(1, 4, 8, 6, 4, 14, g)
	}
	for _, row := range []struct {
		y int
		b uint32
	}{{3, l}, {2, g}, {1, l}} {
		c.fill(0, row.y, 0, 0, row.y, 15, row.b)
		c.fill(7, row.y, 0, 7, row.y, 15, row.b)
		c.fill(1, row.y, 0, 7, row.y, 0, row.b)
		c.fill(1, row.y, 15, 6, row.y, 15, row.b)
	}
	for _, q := range [][6]int{
		{1, 1, 1, 1, 1, 2}, {6, 1, 1, 6, 1, 2}, {1, 3, 1, 1, 3, 2}, {6, 3, 1, 6, 3, 2}, {1, 1, 13, 1, 1, 14},
		{6, 1, 13, 6, 1, 14}, {1, 3, 13, 1, 3, 14}, {6, 3, 13, 6, 3, 14}, {2, 1, 6, 2, 3, 6}, {5, 1, 6, 5, 3, 6},
		{2, 1, 9, 2, 3, 9}, {5, 1, 9, 5, 3, 9}, {3, 2, 6, 4, 2, 6}, {3, 2, 9, 4, 2, 9}, {2, 2, 7, 2, 2, 8},
		{5, 2, 7, 5, 2, 8},
	} {
		c.fill(q[0], q[1], q[2], q[3], q[4], q[5], l)
	}
	c.set(2, 2, 5, lan)
	c.set(5, 2, 5, lan)
	c.set(2, 2, 10, lan)
	c.set(5, 2, 10, lan)
	c.set(2, 3, 5, l)
	c.set(5, 3, 5, l)
	c.set(2, 3, 10, l)
	c.set(5, 3, 10, l)
	if south.open[omSouth] {
		c.water(3, 1, 0, 4, 2, 0)
	}
	if south.open[omEast] {
		c.water(7, 1, 3, 7, 2, 4)
	}
	if south.open[omWest] {
		c.water(0, 1, 3, 0, 2, 4)
	}
	if north.open[omNorth] {
		c.water(3, 1, 15, 4, 2, 15)
	}
	if north.open[omWest] {
		c.water(0, 1, 11, 0, 2, 12)
	}
	if north.open[omEast] {
		c.water(7, 1, 11, 7, 2, 12)
	}
}

// simple is the simple room. Its centre pillar's nextBoolean is drawn from the chunk's post-process
// random, as the reference model draws it (P161; TestMonumentMatchesBDS checks it).
func (c *om) simple() {
	g, l, d, lan := c.bl.gray, c.bl.light, c.bl.dark, c.bl.lantern
	rd := c.p.room
	if rd.index/25 > 0 {
		c.floor(0, 0, rd.open[omDown])
	}
	if rd.conn[omUp] == nil {
		c.replaceWater(1, 4, 1, 6, 4, 6, g)
	}
	pillar := c.p.design != 0 && c.r.NextBool() && !rd.open[omDown] && !rd.open[omUp] && rd.openings() > 1
	switch c.p.design {
	case 0:
		for _, q := range []struct {
			box [6]int
			b   uint32
		}{
			{[6]int{0, 1, 0, 2, 1, 2}, l}, {[6]int{0, 3, 0, 2, 3, 2}, l}, {[6]int{0, 2, 0, 0, 2, 2}, g}, {[6]int{1, 2, 0, 2, 2, 0}, g},
			{[6]int{1, 2, 1, 1, 2, 1}, lan},
			{[6]int{5, 1, 0, 7, 1, 2}, l}, {[6]int{5, 3, 0, 7, 3, 2}, l}, {[6]int{7, 2, 0, 7, 2, 2}, g}, {[6]int{5, 2, 0, 6, 2, 0}, g},
			{[6]int{6, 2, 1, 6, 2, 1}, lan},
			{[6]int{0, 1, 5, 2, 1, 7}, l}, {[6]int{0, 3, 5, 2, 3, 7}, l}, {[6]int{0, 2, 5, 0, 2, 7}, g}, {[6]int{1, 2, 7, 2, 2, 7}, g},
			{[6]int{1, 2, 6, 1, 2, 6}, lan},
			{[6]int{5, 1, 5, 7, 1, 7}, l}, {[6]int{5, 3, 5, 7, 3, 7}, l}, {[6]int{7, 2, 5, 7, 2, 7}, g}, {[6]int{5, 2, 7, 6, 2, 7}, g},
			{[6]int{6, 2, 6, 6, 2, 6}, lan},
		} {
			c.fill(q.box[0], q.box[1], q.box[2], q.box[3], q.box[4], q.box[5], q.b)
		}
		if rd.open[omSouth] {
			c.fill(3, 3, 0, 4, 3, 0, l)
		} else {
			c.fill(3, 3, 0, 4, 3, 1, l)
			c.fill(3, 2, 0, 4, 2, 0, g)
			c.fill(3, 1, 0, 4, 1, 1, l)
		}
		if rd.open[omNorth] {
			c.fill(3, 3, 7, 4, 3, 7, l)
		} else {
			c.fill(3, 3, 6, 4, 3, 7, l)
			c.fill(3, 2, 7, 4, 2, 7, g)
			c.fill(3, 1, 6, 4, 1, 7, l)
		}
		if rd.open[omWest] {
			c.fill(0, 3, 3, 0, 3, 4, l)
		} else {
			c.fill(0, 3, 3, 1, 3, 4, l)
			c.fill(0, 2, 3, 0, 2, 4, g)
			c.fill(0, 1, 3, 1, 1, 4, l)
		}
		if rd.open[omEast] {
			c.fill(7, 3, 3, 7, 3, 4, l)
		} else {
			c.fill(6, 3, 3, 7, 3, 4, l)
			c.fill(7, 2, 3, 7, 2, 4, g)
			c.fill(6, 1, 3, 7, 1, 4, l)
		}
	case 1:
		c.fill(2, 1, 2, 2, 3, 2, l)
		c.fill(2, 1, 5, 2, 3, 5, l)
		c.fill(5, 1, 5, 5, 3, 5, l)
		c.fill(5, 1, 2, 5, 3, 2, l)
		c.set(2, 2, 2, lan)
		c.set(2, 2, 5, lan)
		c.set(5, 2, 5, lan)
		c.set(5, 2, 2, lan)
		for _, q := range [][6]int{
			{0, 1, 0, 1, 3, 0}, {0, 1, 1, 0, 3, 1}, {0, 1, 7, 1, 3, 7}, {0, 1, 6, 0, 3, 6}, {6, 1, 7, 7, 3, 7},
			{7, 1, 6, 7, 3, 6}, {6, 1, 0, 7, 3, 0}, {7, 1, 1, 7, 3, 1},
		} {
			c.fill(q[0], q[1], q[2], q[3], q[4], q[5], l)
		}
		for _, q := range [][3]int{{1, 2, 0}, {0, 2, 1}, {1, 2, 7}, {0, 2, 6}, {6, 2, 7}, {7, 2, 6}, {6, 2, 0}, {7, 2, 1}} {
			c.set(q[0], q[1], q[2], g)
		}
		if !rd.open[omSouth] {
			c.fill(1, 3, 0, 6, 3, 0, l)
			c.fill(1, 2, 0, 6, 2, 0, g)
			c.fill(1, 1, 0, 6, 1, 0, l)
		}
		if !rd.open[omNorth] {
			c.fill(1, 3, 7, 6, 3, 7, l)
			c.fill(1, 2, 7, 6, 2, 7, g)
			c.fill(1, 1, 7, 6, 1, 7, l)
		}
		if !rd.open[omWest] {
			c.fill(0, 3, 1, 0, 3, 6, l)
			c.fill(0, 2, 1, 0, 2, 6, g)
			c.fill(0, 1, 1, 0, 1, 6, l)
		}
		if !rd.open[omEast] {
			c.fill(7, 3, 1, 7, 3, 6, l)
			c.fill(7, 2, 1, 7, 2, 6, g)
			c.fill(7, 1, 1, 7, 1, 6, l)
		}
	case 2:
		c.ringWalls()
		if rd.open[omSouth] {
			c.water(3, 1, 0, 4, 2, 0)
		}
		if rd.open[omNorth] {
			c.water(3, 1, 7, 4, 2, 7)
		}
		if rd.open[omWest] {
			c.water(0, 1, 3, 0, 2, 4)
		}
		if rd.open[omEast] {
			c.water(7, 1, 3, 7, 2, 4)
		}
	}
	if pillar {
		c.fill(3, 1, 3, 4, 1, 4, l)
		c.fill(3, 2, 3, 4, 2, 4, g)
		c.fill(3, 3, 3, 4, 3, 4, l)
	}
	_ = d
}

// ringWalls are the walls of the simple room's design 2 and of the simple top room (P161).
func (c *om) ringWalls() {
	l, d := c.bl.light, c.bl.dark
	for _, row := range []struct {
		y int
		b uint32
	}{{1, l}, {2, d}, {3, l}} {
		c.fill(0, row.y, 0, 0, row.y, 7, row.b)
		c.fill(7, row.y, 0, 7, row.y, 7, row.b)
		c.fill(1, row.y, 0, 6, row.y, 0, row.b)
		c.fill(1, row.y, 7, 6, row.y, 7, row.b)
	}
	c.fill(0, 1, 3, 0, 2, 4, d)
	c.fill(7, 1, 3, 7, 2, 4, d)
	c.fill(3, 1, 0, 4, 2, 0, d)
	c.fill(3, 1, 7, 4, 2, 7, d)
}

func (c *om) simpleTop() {
	rd := c.p.room
	if rd.index/25 > 0 {
		c.floor(0, 0, rd.open[omDown])
	}
	if rd.conn[omUp] == nil {
		c.replaceWater(1, 4, 1, 6, 4, 6, c.bl.gray)
	}
	for x := 1; x <= 6; x++ {
		for z := 1; z <= 6; z++ {
			if c.r.NextIntN(3) != 0 {
				y0 := 3
				if c.r.NextIntN(4) == 0 {
					y0 = 2
				}
				c.fill(x, y0, z, x, 3, z, c.bl.sponge)
			}
		}
	}
	c.ringWalls()
	if rd.open[omSouth] {
		c.water(3, 1, 0, 4, 2, 0)
	}
}

func (c *om) wingRoom() {
	l, d, lan := c.bl.light, c.bl.dark, c.bl.lantern
	if c.p.design == 0 {
		for i := range 4 {
			c.fill(10-i, 3-i, 20-i, 12+i, 3-i, 20, l)
		}
		for _, q := range [][6]int{
			{7, 0, 6, 15, 0, 16}, {6, 0, 6, 6, 3, 20}, {16, 0, 6, 16, 3, 20}, {7, 1, 7, 7, 1, 20}, {15, 1, 7, 15, 1, 20},
			{7, 1, 6, 9, 3, 6}, {13, 1, 6, 15, 3, 6}, {8, 1, 7, 9, 1, 7}, {13, 1, 7, 14, 1, 7}, {9, 0, 5, 13, 0, 5},
		} {
			c.fill(q[0], q[1], q[2], q[3], q[4], q[5], l)
		}
		c.fill(10, 0, 7, 12, 0, 7, d)
		c.fill(8, 0, 10, 8, 0, 12, d)
		c.fill(14, 0, 10, 14, 0, 12, d)
		for z := 18; z >= 7; z -= 3 {
			c.set(6, 3, z, lan)
			c.set(16, 3, z, lan)
		}
		for _, q := range [][3]int{{10, 0, 10}, {12, 0, 10}, {10, 0, 12}, {12, 0, 12}, {8, 3, 6}, {14, 3, 6}} {
			c.set(q[0], q[1], q[2], lan)
		}
		for _, q := range [][2]int{{4, 4}, {18, 4}, {4, 18}, {18, 18}} {
			c.set(q[0], 2, q[1], l)
			c.set(q[0], 1, q[1], lan)
			c.set(q[0], 0, q[1], l)
		}
		c.set(9, 7, 20, l)
		c.set(13, 7, 20, l)
		c.fill(6, 0, 21, 7, 4, 21, l)
		c.fill(15, 0, 21, 16, 4, 21, l)
		c.elder(11, 2, 16)
		return
	}
	c.fill(9, 3, 18, 13, 3, 20, l)
	c.fill(9, 0, 18, 9, 2, 18, l)
	c.fill(13, 0, 18, 13, 2, 18, l)
	for _, x := range []int{9, 13} {
		c.set(x, 6, 20, l)
		c.set(x, 5, 20, lan)
		c.set(x, 4, 20, l)
	}
	c.fill(7, 3, 7, 15, 3, 14, l)
	for _, x := range []int{10, 12} {
		c.fill(x, 0, 10, x, 6, 10, l)
		c.fill(x, 0, 12, x, 6, 12, l)
		c.set(x, 0, 10, lan)
		c.set(x, 0, 12, lan)
		c.set(x, 4, 10, lan)
		c.set(x, 4, 12, lan)
	}
	for _, x := range []int{8, 14} {
		c.fill(x, 0, 7, x, 2, 7, l)
		c.fill(x, 0, 14, x, 2, 14, l)
	}
	c.fill(8, 3, 8, 8, 3, 13, d)
	c.fill(14, 3, 8, 14, 3, 13, d)
	c.elder(11, 5, 13)
}

func (c *om) penthouse() {
	g, l, d, lan := c.bl.gray, c.bl.light, c.bl.dark, c.bl.lantern
	c.fill(2, -1, 2, 11, -1, 11, l)
	c.fill(0, -1, 0, 1, -1, 11, g)
	c.fill(12, -1, 0, 13, -1, 11, g)
	c.fill(2, -1, 0, 11, -1, 1, g)
	c.fill(2, -1, 12, 11, -1, 13, g)
	c.fill(0, 0, 0, 0, 0, 13, l)
	c.fill(13, 0, 0, 13, 0, 13, l)
	c.fill(1, 0, 0, 12, 0, 0, l)
	c.fill(1, 0, 13, 12, 0, 13, l)
	for i := 2; i <= 11; i += 3 {
		c.set(0, 0, i, lan)
		c.set(13, 0, i, lan)
		c.set(i, 0, 0, lan)
	}
	c.fill(2, 0, 3, 4, 0, 9, l)
	c.fill(9, 0, 3, 11, 0, 9, l)
	c.fill(4, 0, 9, 9, 0, 11, l)
	c.set(5, 0, 8, l)
	c.set(8, 0, 8, l)
	c.set(10, 0, 10, l)
	c.set(3, 0, 10, l)
	c.fill(3, 0, 3, 3, 0, 7, d)
	c.fill(10, 0, 3, 10, 0, 7, d)
	c.fill(6, 0, 10, 7, 0, 10, d)
	for _, x := range []int{3, 10} {
		for z := 2; z <= 8; z += 3 {
			c.fill(x, 0, z, x, 2, z, l)
		}
	}
	c.fill(5, 0, 10, 5, 2, 10, l)
	c.fill(8, 0, 10, 8, 2, 10, l)
	c.fill(6, -1, 7, 7, -1, 8, d)
	c.water(6, -1, 3, 7, -1, 4)
	c.elder(6, 1, 6)
}
