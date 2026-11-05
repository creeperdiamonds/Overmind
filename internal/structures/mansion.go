package structures

import (
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Woodland mansions, our own Go from NOTES-structures-phase2 P15.3, P18.5, P18.6 and P20.1 to P20.6
// (cited P153, P185, P186, P201..P206). Validated in the study on seed 987654321's (105,-292) mansion:
// 0.9975 air / not air of 88,608 cells, 0.9998 block names of 36,719 non-air cells, 6 of 6 chest loot
// seeds. The pieces are the install's mansion templates, old-format Java NBT whose legacy names are
// rewritten here (P207). Evokers, vindicators and allays are mobs (stage 4): recorded as spawn intents.
//
// One random runs through the whole start: the region stream after its four placement words, then the
// rotation, the grid's corridor walk, the three room shuffles and the placer's door and room draws.

const mansionReach = 6 // Chunks from the start a piece reaches (the grid is 11 cells of 8 blocks).

// Base grid cells (P186).
const (
	mFree     = 0
	mCorridor = 1
	mRoom     = 2
	mEntrance = 3
	mBlocked  = 5
)

// Room grid flags (P186 step 4, P202 step 5); the low 16 bits are the room id.
const (
	mr1x1    = 0x10000
	mr1x2    = 0x20000
	mr2x2    = 0x40000
	mrType   = 0xf0000
	mrOrigin = 0x100000
	mrDoor   = 0x200000
	mrStairs = 0x400000
	mrLink   = 0x800000
)

// mGrid is an 11 x 11 grid, indexed [x][y]; cells outside read mBlocked.
type mGrid [11][11]int32

func (g *mGrid) get(x, y int) int32 {
	if x < 0 || y < 0 || x > 10 || y > 10 {
		return mBlocked
	}
	return g[x][y]
}

func (g *mGrid) set(x, y int, v int32) {
	if x >= 0 && y >= 0 && x <= 10 && y <= 10 {
		g[x][y] = v
	}
}

func (g *mGrid) fill(x0, y0, x1, y1 int, v int32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			g.set(x, y, v)
		}
	}
}

func (g *mGrid) setIf(x, y int, old, v int32) {
	if g.get(x, y) == old {
		g.set(x, y, v)
	}
}

func (g *mGrid) house(x, y int) bool { v := g.get(x, y); return v >= 1 && v <= 4 }

// mDir is a grid direction, clockwise from north; mUp and mNoDoor are the two non-wall doors.
type mDir int

const (
	mN mDir = iota
	mE
	mS
	mW
	mUp
	mNoDoor
)

var mStep = [4][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

func (d mDir) cw() mDir  { return (d + 1) & 3 }
func (d mDir) ccw() mDir { return (d + 3) & 3 }
func (d mDir) opp() mDir { return (d + 2) & 3 }

// mRandDir is Bedrock's direction table for the corridor walk's nextInt(4) (P185: north, south, west,
// east; Java's order differs).
var mRandDir = [4]mDir{mN, mS, mW, mE}

// mPiece is one placed template (P202): name, origin, rotation (0 none .. 3 ccw90), mirror (0 none, 1
// LEFT_RIGHT = z flipped, 2 FRONT_BACK = x flipped), and its box.
type mPiece struct {
	name   string
	t      *Template
	pos    [3]int
	rot    int
	mirror int
	box    box
}

// local maps a template position to the world: mirror first, then the rotation about the origin (P164).
func (p *mPiece) local(q [3]int) [3]int {
	switch p.mirror {
	case 1:
		q[2] = -q[2]
	case 2:
		q[0] = -q[0]
	}
	q = rotate(q, p.rot)
	return [3]int{p.pos[0] + q[0], p.pos[1] + q[1], p.pos[2] + q[2]}
}

type mansion struct {
	pieces []*mPiece
	draws  int // Words the start drew after the rotation (the study counts these plus the rotation and the 4 placement words: 217 on seed 987654321).
	bounds box
}

type mBuilder struct {
	s     *Structures
	r     *worldgen.BedrockRandom
	draws int
	base  mGrid
	third mGrid
	rooms [3]mGrid
	rot   int
	o     [3]int
	out   []*mPiece
	ok    bool
}

func (b *mBuilder) intN(n int32) int32 { b.draws++; return b.r.NextIntN(n) }
func (b *mBuilder) boolean() bool      { b.draws++; return b.r.NextBool() }

// mansionAt is the mansion of a start chunk, nil when the chunk is not one.
func (s *Structures) mansionAt(cx, cz int) *mansion {
	st, ok := s.StartIn(Mansion, cx, cz)
	if !ok || s.data == nil {
		return nil
	}
	b := &mBuilder{s: s, r: st.random(), ok: true}
	b.rot = int(b.r.NextIntN(4)) // P153 item 2.
	b.o = [3]int{cx*16 + 8, s.mansionHeight(cx, cz, b.rot), cz*16 + 8}
	b.grid()
	b.place()
	if !b.ok {
		return nil
	}
	boxes := make([]box, len(b.out))
	for i, p := range b.out {
		boxes[i] = p.box
	}
	return &mansion{pieces: b.out, draws: b.draws, bounds: boundsOf(boxes)}
}

// mansionHeight is the start height (P203 item 3): the lowest of four columns' ground, one above the top
// solid block at the noise stage, plus one.
func (s *Structures) mansionHeight(cx, cz, rot int) int {
	dx, dz := [4]int{5, -5, -5, 5}[rot], [4]int{5, 5, -5, -5}[rot]
	h := maxY
	for _, c := range [4][2]int{{7, 7}, {7, 7 + dz}, {7 + dx, 7}, {7 + dx, 7 + dz}} {
		h = min(h, s.g.NoiseHeight(cx*16+c[0], cz*16+c[1])+1)
	}
	return max(h, minY-1) + 1
}

// grid builds the base grid and the three floors' room grids (P186 with P185's direction table and
// P202's stairs flag).
func (b *mBuilder) grid() {
	g := &b.base
	g.fill(7, 4, 8, 5, mEntrance)
	g.fill(6, 4, 6, 5, mRoom)
	g.fill(9, 2, 10, 7, mBlocked)
	g.fill(8, 2, 8, 3, mCorridor)
	g.fill(8, 6, 8, 7, mCorridor)
	g.set(6, 3, mCorridor)
	g.set(6, 6, mCorridor)
	g.fill(0, 0, 11, 1, mBlocked)
	g.fill(0, 9, 11, 11, mBlocked)
	b.corridor(g, 7, 2, mW, 6)
	b.corridor(g, 7, 7, mW, 6)
	b.corridor(g, 5, 3, mW, 3)
	b.corridor(g, 5, 6, mW, 3)
	for cleanEdges(g) {
	}
	b.identify(g, &b.rooms[0])
	b.identify(g, &b.rooms[1])
	b.rooms[0].fill(8, 4, 8, 5, mrLink)
	b.rooms[1].fill(8, 4, 8, 5, mrLink)

	// The third floor starts over a 1x2 room of the second floor that has a door.
	t, f1 := &b.third, &b.rooms[1]
	var cand [][2]int
	for y := range 11 {
		for x := range 11 {
			if v := f1.get(x, y); v&mrType == mr1x2 && v&mrDoor != 0 {
				cand = append(cand, [2]int{x, y})
			}
		}
	}
	if len(cand) == 0 {
		t.fill(0, 0, 10, 10, mBlocked)
	} else {
		p := cand[b.intN(int32(len(cand)))]
		v := f1.get(p[0], p[1])
		f1.set(p[0], p[1], v|mrStairs)
		end := p
		for d := mN; d <= mW; d++ {
			q := [2]int{p[0] + mStep[d][0], p[1] + mStep[d][1]}
			if f1.get(q[0], q[1])&0xffff == v&0xffff {
				end = q
				break
			}
		}
		for y := range 11 {
			for x := range 11 {
				if !g.house(x, y) {
					t.set(x, y, mBlocked)
				}
			}
		}
		t.set(p[0], p[1], mEntrance)
		t.set(end[0], end[1], mEntrance)
		b.rooms[2].set(end[0], end[1], mrLink)
		var dirs []mDir
		for d := mN; d <= mW; d++ {
			if t.get(end[0]+mStep[d][0], end[1]+mStep[d][1]) == mFree {
				dirs = append(dirs, d)
			}
		}
		if len(dirs) == 0 {
			t.fill(0, 0, 10, 10, mBlocked)
			f1.set(p[0], p[1], v)
		} else {
			d := dirs[b.intN(int32(len(dirs)))]
			b.corridor(t, end[0]+mStep[d][0], end[1]+mStep[d][1], d, 4)
			for cleanEdges(t) {
			}
		}
	}
	b.identify(t, &b.rooms[2])
}

// corridor is the recursive corridor walk (P186 step 2).
func (b *mBuilder) corridor(g *mGrid, x, y int, h mDir, depth int) {
	if depth <= 0 {
		return
	}
	hx, hy := mStep[h][0], mStep[h][1]
	g.set(x, y, mCorridor)
	g.setIf(x+hx, y+hy, mFree, mCorridor)
	for range 8 {
		k := b.intN(4)
		d := mRandDir[k]
		if d == h.opp() {
			continue
		}
		if d == mE && b.boolean() {
			continue
		}
		a, c := x+hx+mStep[d][0], y+hy+mStep[d][1]
		if g.get(a, c) == mFree && g.get(a+mStep[d][0], c+mStep[d][1]) == mFree {
			b.corridor(g, a, c, d, depth-1)
			break
		}
	}
	cw, ccw := mStep[h.cw()], mStep[h.ccw()]
	g.setIf(x+cw[0], y+cw[1], mFree, mRoom)
	g.setIf(x+ccw[0], y+ccw[1], mFree, mRoom)
	g.setIf(x+hx+cw[0], y+hy+cw[1], mFree, mRoom)
	g.setIf(x+hx+ccw[0], y+hy+ccw[1], mFree, mRoom)
	g.setIf(x+2*hx, y+2*hy, mFree, mRoom)
	g.setIf(x+2*cw[0], y+2*cw[1], mFree, mRoom)
	g.setIf(x+2*ccw[0], y+2*ccw[1], mFree, mRoom)
}

// cleanEdges is one pass of P186 step 3; it says whether a cell changed.
func cleanEdges(g *mGrid) bool {
	changed := false
	for y := range 11 {
		for x := range 11 {
			if g.get(x, y) != mFree {
				continue
			}
			n := 0
			for _, s := range mStep {
				if g.house(x+s[0], y+s[1]) {
					n++
				}
			}
			if n >= 3 {
				g.set(x, y, mRoom)
				changed = true
			} else if n == 2 {
				d := 0
				for _, s := range [4][2]int{{1, 1}, {-1, 1}, {1, -1}, {-1, -1}} {
					if g.house(x+s[0], y+s[1]) {
						d++
					}
				}
				if d <= 1 {
					g.set(x, y, mRoom)
					changed = true
				}
			}
		}
	}
	return changed
}

// identify groups a grid's room cells into rooms (P186 step 5, Bedrock's shuffle of P153 item 4).
func (b *mBuilder) identify(from, rooms *mGrid) {
	var cells [][2]int
	for y := range 11 {
		for x := range 11 {
			if from.get(x, y) == mRoom {
				cells = append(cells, [2]int{x, y})
			}
		}
	}
	for k := len(cells) - 1; k >= 1; k-- {
		j := b.intN(int32(k))
		cells[k], cells[j] = cells[j], cells[k]
	}
	free := func(x, y int) bool { return rooms.get(x, y) == 0 && from.get(x, y) == mRoom }
	touches := func(x, y int) bool {
		for _, s := range mStep {
			if from.get(x+s[0], y+s[1]) == mCorridor {
				return true
			}
		}
		return false
	}
	id := int32(10)
	for _, c := range cells {
		x, y := c[0], c[1]
		if rooms.get(x, y) != 0 {
			continue
		}
		x0, y0, x1, y1, typ := x, y, x, y, int32(mr1x1)
		switch {
		case free(x+1, y) && free(x, y+1) && free(x+1, y+1):
			x1, y1, typ = x+1, y+1, mr2x2
		case free(x-1, y) && free(x, y+1) && free(x-1, y+1):
			x0, y1, typ = x-1, y+1, mr2x2
		case free(x-1, y) && free(x, y-1) && free(x-1, y-1):
			x0, y0, typ = x-1, y-1, mr2x2
		case free(x+1, y):
			x1, typ = x+1, mr1x2
		case free(x, y+1):
			y1, typ = y+1, mr1x2
		case free(x-1, y):
			x0, typ = x-1, mr1x2
		case free(x, y-1):
			y0, typ = y-1, mr1x2
		}
		other := func(v, a, c int) int {
			if v == a {
				return c
			}
			return a
		}
		dx, dy := x1, y1
		if b.boolean() {
			dx = x0
		}
		if b.boolean() {
			dy = y0
		}
		door := int32(mrDoor)
		if !touches(dx, dy) {
			dx, dy = other(dx, x0, x1), other(dy, y0, y1)
			if !touches(dx, dy) {
				dy = other(dy, y0, y1)
				if !touches(dx, dy) {
					dx, dy = other(dx, x0, x1), other(dy, y0, y1)
					if !touches(dx, dy) {
						door, dx, dy = 0, x0, y0
					}
				}
			}
		}
		for yy := y0; yy <= y1; yy++ {
			for xx := x0; xx <= x1; xx++ {
				if xx == dx && yy == dy {
					rooms.set(xx, yy, mrOrigin|door|typ|id)
				} else {
					rooms.set(xx, yy, typ|id)
				}
			}
		}
		id++
	}
}

// World steps of the placer (P202): F is "south" and E "east" turned by a rotation.
func mFwd(r int) [3]int  { return rotate([3]int{0, 0, 1}, r) }
func mEast(r int) [3]int { return rotate([3]int{1, 0, 0}, r) }

// mv is p + a*F(r) + b*E(r) + up.
func mv(p [3]int, r, a, b, up int) [3]int {
	f, e := mFwd(r), mEast(r)
	return [3]int{p[0] + a*f[0] + b*e[0], p[1] + up, p[2] + a*f[2] + b*e[2]}
}

// anchor is a grid cell's anchor A(base, x, y) of P202 (sx = 8, sy = 5).
func (b *mBuilder) anchor(base [3]int, x, y int) [3]int {
	return mv(base, b.rot, 8+8*(y-5), 8*(x-8), 0)
}

// piece adds a template piece to the list.
func (b *mBuilder) piece(name string, pos [3]int, rot, mirror int) {
	p := &mPiece{name: name, pos: pos, rot: rot & 3, mirror: mirror}
	p.t = b.s.data.Template("mansion/" + name)
	if p.t == nil {
		b.ok = false
		b.s.data.problem("mansion template mansion/%s is missing", name)
		return
	}
	s := p.t.Size
	c0, c1 := p.local([3]int{}), p.local([3]int{s[0] - 1, s[1] - 1, s[2] - 1})
	p.box = box{min(c0[0], c1[0]), min(c0[1], c1[1]), min(c0[2], c1[2]), max(c0[0], c1[0]), max(c0[1], c1[1]), max(c0[2], c1[2])}
	b.out = append(b.out, p)
}

// mCursor is a wall walk's cursor (P202 step 2).
type mCursor struct {
	pos  [3]int
	rot  int
	kind string
}

func (b *mBuilder) wall(c *mCursor) {
	b.piece(c.kind, mv(c.pos, c.rot, 0, 7, 0), c.rot, 0)
	c.pos = mv(c.pos, c.rot, 8, 0, 0)
}

func (b *mBuilder) turn(c *mCursor) {
	c.pos = mv(c.pos, c.rot, -1, 0, 0)
	b.piece("wall_corner", c.pos, c.rot, 0)
	c.pos = mv(c.pos, c.rot, -7, 6, 0)
	c.rot = (c.rot + 1) & 3
}

func (b *mBuilder) inner(c *mCursor) {
	c.pos = mv(c.pos, c.rot, 6, 8, 0)
	c.rot = (c.rot + 3) & 3
}

// walk traces the outer walls of a grid's house cells (P202 step 2).
func (b *mBuilder) walk(c *mCursor, g *mGrid, x0, y0, x1, y1 int) {
	gx, gy, d := x0, y0, mS
	done := func() bool { return gx == x1 && gy == y1 && d == mS }
	for range 4 * 11 * 11 * 4 { // A closed outline is far shorter; the bound only guards a loop.
		nx, ny := gx+mStep[d][0], gy+mStep[d][1]
		switch {
		case !g.house(nx, ny):
			b.turn(c)
			d = d.cw()
			if !done() {
				b.wall(c)
			}
		case g.house(nx+mStep[d.ccw()][0], ny+mStep[d.ccw()][1]):
			b.inner(c)
			gx, gy, d = nx, ny, d.ccw()
		default:
			gx, gy = nx, ny
			if !done() {
				b.wall(c)
			}
		}
		if done() {
			return
		}
	}
}

// place is the placer (P202): entrance, outer walls of the two floors and the third, roofs, then each
// floor's corridors and rooms, into one list in this order.
func (b *mBuilder) place() {
	r, o := b.rot, b.o
	b.piece("entrance", mv(o, r, 0, -9, 0), r, 0)
	c1 := &mCursor{mv(o, r, 16, 0, 0), r, "wall_flat"}
	c2 := &mCursor{mv(o, r, 16, 0, 8), r, "wall_window"}
	b.walk(c1, &b.base, 8, 5, 8, 4)
	b.walk(c2, &b.base, 8, 5, 8, 4)

	c3 := &mCursor{[3]int{c1.pos[0], c1.pos[1] + 19, c1.pos[2]}, r, "wall_window"}
third:
	for y := range 11 {
		for x := 10; x >= 0; x-- {
			if b.third.house(x, y) {
				c3.pos = mv(c3.pos, r, 8+8*(y-5), 8*(x-8), 0)
				b.wall(c3)
				b.walk(c3, &b.third, x, y, x, y)
				break third
			}
		}
	}

	b.roof([3]int{o[0], o[1] + 16, o[2]}, &b.base, &b.third)
	b.roof([3]int{o[0], o[1] + 27, o[2]}, &b.third, nil)

	for f := range 3 {
		fo := [3]int{o[0], o[1] + [3]int{0, 8, 19}[f], o[2]}
		g := &b.base
		if f == 2 {
			g = &b.third
		}
		b.corridors(f, fo, g, &b.rooms[f])
		b.roomsOf(f, fo, g, &b.rooms[f])
	}
}

// roof is P202 step 4's three passes over grid g, with u the floor above (nil for none).
func (b *mBuilder) roof(base [3]int, g, u *mGrid) {
	r := b.rot
	hu := func(x, y int) bool { return u != nil && u.house(x, y) }
	for y := range 11 {
		for x := range 11 {
			if !g.house(x, y) || hu(x, y) {
				continue
			}
			c := b.anchor(base, x, y)
			b.piece("roof", [3]int{c[0], c[1] + 3, c[2]}, r, 0)
			if !g.house(x+1, y) {
				b.piece("roof_front", mv(c, r, 0, 6, 0), r, 0)
			}
			if !g.house(x-1, y) {
				b.piece("roof_front", mv(c, r, 7, 0, 0), r+2, 0)
			}
			if !g.house(x, y-1) {
				b.piece("roof_front", mv(c, r, 0, -1, 0), r+3, 0)
			}
			if !g.house(x, y+1) {
				b.piece("roof_front", mv(c, r, 6, 6, 0), r+1, 0)
			}
		}
	}
	if u != nil {
		for y := range 11 {
			for x := range 11 {
				if !g.house(x, y) || !u.house(x, y) {
					continue
				}
				c := b.anchor(base, x, y)
				oe, ow, on, os := !g.house(x+1, y), !g.house(x-1, y), !g.house(x, y-1), !g.house(x, y+1)
				if oe {
					b.piece("small_wall", mv(c, r, 0, 7, 0), r, 0)
				}
				if ow {
					b.piece("small_wall", mv(c, r, 6, -1, 0), r+2, 0)
				}
				if on {
					b.piece("small_wall", mv(c, r, -1, 0, 0), r+3, 0)
				}
				if os {
					b.piece("small_wall", mv(c, r, 7, 6, 0), r+1, 0)
				}
				if oe && on {
					b.piece("small_wall_corner", mv(c, r, -2, 7, 0), r, 0)
				}
				if oe && os {
					b.piece("small_wall_corner", mv(c, r, 7, 8, 0), r+1, 0)
				}
				if ow && on {
					b.piece("small_wall_corner", mv(c, r, -1, -2, 0), r+3, 0)
				}
				if ow && os {
					b.piece("small_wall_corner", mv(c, r, 8, -1, 0), r+2, 0)
				}
			}
		}
	}
	for y := range 11 {
		for x := range 11 {
			if !g.house(x, y) || hu(x, y) {
				continue
			}
			c := b.anchor(base, x, y)
			on, os := !g.house(x, y-1), !g.house(x, y+1)
			if !g.house(x+1, y) {
				q := mv(c, r, 0, 6, 0)
				if os {
					b.piece("roof_corner", mv(q, r, 6, 0, 0), r, 0)
				} else if g.house(x+1, y+1) {
					b.piece("roof_inner_corner", mv(q, r, 5, 0, 0), r, 0)
				}
				if on {
					b.piece("roof_corner", q, r+3, 0)
				} else if g.house(x+1, y-1) {
					b.piece("roof_inner_corner", mv(c, r, -2, 9, 0), r+1, 0)
				}
			}
			if !g.house(x-1, y) {
				q := c
				if os {
					b.piece("roof_corner", mv(q, r, 6, 0, 0), r+1, 0)
				} else if g.house(x-1, y+1) {
					b.piece("roof_inner_corner", mv(q, r, 8, -3, 0), r+3, 0)
				}
				if on {
					b.piece("roof_corner", q, r+2, 0)
				} else if g.house(x-1, y-1) {
					b.piece("roof_inner_corner", mv(q, r, 1, 0, 0), r+2, 0)
				}
			}
		}
	}
}

// floorName is a first-floor name or its upper-floor variant (P202: BDS's first-floor files lack _1).
func floorName(name string, f int) string {
	if f == 0 {
		return name
	}
	return name + "_2"
}

// corridors is P202 step 5a.
func (b *mBuilder) corridors(f int, fo [3]int, g, rg *mGrid) {
	r := b.rot
	link := func(x, y int) bool { return g.get(x, y) == mCorridor || rg.get(x, y)&mrLink != 0 }
	for y := range 11 {
		for x := range 11 {
			if g.get(x, y) != mCorridor {
				continue
			}
			d := b.anchor(fo, x, y)
			b.piece("corridor_floor", d, r, 0)
			if link(x, y-1) {
				b.piece("carpet_north", mv(d, r, 0, 1, 1), r, 0)
			}
			if link(x+1, y) {
				b.piece("carpet_east", mv(d, r, 1, 5, 1), r, 0)
			}
			if link(x, y+1) {
				b.piece(floorName("carpet_south", f), mv(d, r, 5, -1, 0), r, 0)
			}
			if link(x-1, y) {
				b.piece(floorName("carpet_west", f), mv(d, r, -1, -1, 0), r, 0)
			}
		}
	}
}

func (b *mBuilder) numbered(prefix string, n int32) string {
	return prefix + strconv.Itoa(1+int(b.intN(n)))
}

// mRoomPlace is a room template's origin offset (along F, along E), turn and mirror for a door and the
// direction the room continues in (P202 step 5b's tables).
type mRoomPlace struct {
	kind       byte // 's' side, 'f' front, 'x' secret; 0 for a 2x2 room.
	f, e, turn int
	mirror     int
}

var mansion1x2 = map[[2]mDir]mRoomPlace{
	{mE, mS}: {'s', 0, 1, 0, 0}, {mE, mN}: {'s', 6, 1, 0, 1}, {mW, mN}: {'s', 6, 7, 2, 0}, {mW, mS}: {'s', 0, 7, 0, 2},
	{mS, mE}: {'s', 0, 1, 1, 1}, {mS, mW}: {'s', 0, 7, 1, 0}, {mN, mW}: {'s', 6, 7, 1, 2}, {mN, mE}: {'s', 6, 1, 3, 0},
	{mS, mN}: {'f', -8, 1, 0, 0}, {mN, mS}: {'f', 14, 7, 2, 0}, {mW, mE}: {'f', 0, 15, 1, 0}, {mE, mW}: {'f', 6, -7, 3, 0},
	{mUp, mE}: {'x', 0, 15, 1, 0}, {mUp, mS}: {'x', 0, 1, 0, 0},
}

var mansion2x2 = map[[2]mDir]mRoomPlace{
	{mE, mS}: {0, 0, -7, 0, 0}, {mE, mN}: {0, 6, -7, 0, 1}, {mN, mE}: {0, 14, 1, 3, 0}, {mN, mW}: {0, 14, 7, 3, 1},
	{mS, mW}: {0, -8, 7, 1, 0}, {mS, mE}: {0, -8, 1, 1, 1}, {mW, mN}: {0, 6, 15, 2, 0}, {mW, mS}: {0, 0, 15, 0, 2},
}

// roomsOf is P202 step 5b: each room cell's walls and doors, then its room template.
func (b *mBuilder) roomsOf(f int, fo [3]int, g, rg *mGrid) {
	r := b.rot
	for y := range 11 {
		for x := range 11 {
			gv := g.get(x, y)
			if gv != mRoom && !(f == 2 && gv == mEntrance) {
				continue
			}
			data := rg.get(x, y)
			id := data & 0xffff
			start := f == 2 && gv == mEntrance && data&mrLink != 0
			door := mNoDoor
			var ds []mDir
			if data&mrDoor != 0 {
				for d := mN; d <= mW; d++ {
					if g.get(x+mStep[d][0], y+mStep[d][1]) == mCorridor {
						ds = append(ds, d)
					}
				}
			}
			if len(ds) > 0 {
				door = ds[b.intN(int32(len(ds)))]
			} else if data&mrOrigin != 0 {
				door = mUp
			}
			same := func(d mDir) bool { return rg.get(x+mStep[d][0], y+mStep[d][1])&0xffff == id }
			p := mv(b.anchor(fo, x, y), r, 0, -1, 0)
			name := func(side mDir) string {
				if door == side {
					return floorName("indoors_door", f)
				}
				return floorName("indoors_wall", f)
			}
			if g.house(x-1, y) && !same(mW) {
				b.piece(name(mW), p, r, 0)
			}
			if g.get(x+1, y) == mCorridor && !start {
				b.piece(name(mE), mv(p, r, 0, 8, 0), r, 0)
			}
			if g.house(x, y+1) && !same(mS) {
				b.piece(name(mS), mv(p, r, 7, 7, 0), r+1, 0)
			}
			if g.get(x, y-1) == mCorridor && !start {
				b.piece(name(mN), mv(p, r, -1, 7, 0), r+1, 0)
			}
			stairs := data&mrStairs != 0
			switch data & mrType {
			case mr1x1:
				var nm string
				if f == 0 {
					nm = b.numbered("1x1_a", 5)
				} else {
					nm = b.numbered("1x1_b", 5)
				}
				turn, a, e := 0, 0, 1
				switch door {
				case mE:
				case mN:
					turn, a, e = 3, 6, 1
				case mW:
					turn, a, e = 2, 6, 7
				case mS:
					turn, a, e = 1, 0, 7
				default:
					nm = b.numbered("1x1_as", 4)
				}
				b.piece(nm, mv(p, r, a, e, 0), r+turn, 0)
			case mr1x2:
				if door == mNoDoor {
					break
				}
				dir := mNoDoor
				for d := mN; d <= mW; d++ {
					if same(d) {
						dir = d
						break
					}
				}
				pl, ok := mansion1x2[[2]mDir{door, dir}]
				if !ok {
					break
				}
				var nm string
				switch pl.kind {
				case 's':
					switch {
					case f == 0:
						nm = b.numbered("1x2_a", 9)
					case stairs:
						nm = "1x2_c_stairs"
					default:
						nm = b.numbered("1x2_c", 4)
					}
				case 'f':
					switch {
					case f == 0:
						nm = b.numbered("1x2_b", 5)
					case stairs:
						nm = "1x2_d_stairs"
					default:
						nm = b.numbered("1x2_d", 5)
					}
				default:
					if f == 0 {
						nm = b.numbered("1x2_s", 2)
					} else {
						nm = b.numbered("1x2_se", 1)
					}
				}
				b.piece(nm, mv(p, r, pl.f, pl.e, 0), r+pl.turn, pl.mirror)
			case mr2x2:
				switch door {
				case mN, mE, mS, mW:
					dir := door.cw()
					if !same(dir) {
						dir = dir.opp()
					}
					pl := mansion2x2[[2]mDir{door, dir}] // Any other pair: at P, unturned.
					var nm string
					if f == 0 {
						nm = b.numbered("2x2_a", 4)
					} else {
						nm = b.numbered("2x2_b", 5)
					}
					b.piece(nm, mv(p, r, pl.f, pl.e, 0), r+pl.turn, pl.mirror)
				case mUp:
					b.piece("2x2_s1", mv(p, r, 0, 1, 0), r, 0)
				}
			}
		}
	}
}

// placeMansions writes the mansion pieces that reach the chunk.
func (s *Structures) placeMansions(w *writer) {
	sp := spreads[Mansion]
	cb := w.chunkBox()
	for rx := floorDiv(w.cx-mansionReach, sp.spacing); rx <= floorDiv(w.cx+mansionReach, sp.spacing); rx++ {
		for rz := floorDiv(w.cz-mansionReach, sp.spacing); rz <= floorDiv(w.cz+mansionReach, sp.spacing); rz++ {
			x, z := sp.candidate(s, rx, rz, worldgen.NewBedrockRandom(0))
			if abs(x-w.cx) > mansionReach || abs(z-w.cz) > mansionReach {
				continue
			}
			m, _ := s.mansions.get([2]int{x, z}, func() any { return s.mansionAt(x, z) }).(*mansion)
			if m == nil {
				continue
			}
			for _, p := range m.pieces {
				if p.box.intersects(cb) {
					s.mansionPost(w, p)
				}
			}
			if m.bounds.intersects(cb) {
				mansionFoundation(w, m)
			}
		}
	}
}

// mansionFoundation fills cobblestone under the mansion, measured on seed 987654321's world (BDS has it
// under every column; Bedrock's code for it was not read, Java's afterPlace rule is used): in each
// column whose cell at the mansion's lowest y is not air and inside a piece, every air or liquid cell
// below, down to the first other block.
func mansionFoundation(w *writer, m *mansion) {
	y0 := m.bounds[1]
	for x := max(m.bounds[0], w.cx*16); x <= min(m.bounds[3], w.cx*16+15); x++ {
		for z := max(m.bounds[2], w.cz*16); z <= min(m.bounds[5], w.cz*16+15); z++ {
			if w.blockName(x, y0, z) == "air" {
				continue
			}
			inside := false
			for _, p := range m.pieces {
				if p.box.inside([3]int{x, y0, z}) {
					inside = true
					break
				}
			}
			if !inside {
				continue
			}
			for y := y0 - 1; y > minY; y-- {
				if w.blockName(x, y, z) != "air" && !w.liquidAt(x, y, z) {
					break
				}
				w.set(x, y, z, cobbleRID(), false)
			}
		}
	}
}

var cobbleRID = sync.OnceValue(func() uint32 { return ridNamed("cobblestone", nil) })

// mansionPost is one piece in one chunk (P201): the template's blocks (structure blocks skipped, air
// placed; each chest placed draws a word, P191), then its data markers in Bedrock's map order.
func (s *Structures) mansionPost(w *writer, p *mPiece) {
	t := p.t
	type marker struct {
		pos  [3]int
		name string
	}
	var markers []marker
	for _, bl := range t.blocks {
		st := t.palette[bl.state]
		if st.name == "structure_block" {
			md, _ := bl.nbt["metadata"].(string)
			markers = append(markers, marker{bl.pos, md})
			continue
		}
		q := p.local(bl.pos)
		if !w.in(q[0], q[1], q[2]) {
			continue
		}
		st = legacyJavaState(st)
		st.props = mirrorProps(st.props, p.mirror)
		res := convert.state(st, p.rot)
		if !res.ok {
			continue
		}
		w.set(q[0], q[1], q[2], res.rid, res.waterlog)
		if res.name == "chest" || res.name == "trapped_chest" {
			// A decorative chest: its word is drawn, and it is saved without a loot table. Its template
			// Items (legacy Java item ids) are not carried; see NOTES.md.
			w.rng.U32()
			w.bes = append(w.bes, chunk.BlockEntity{Pos: q, Data: map[string]any{"id": "Chest"}})
		}
	}
	for _, i := range markerOrder(len(markers), func(i int) [3]int { return markers[i].pos }) {
		m := markers[i]
		q := p.local(m.pos)
		in := w.in(q[0], q[1], q[2])
		switch {
		case m.name == "Mage":
			if in {
				w.spawns = append(w.spawns, spawnIntent{Pos: q, Entity: "minecraft:evocation_illager"})
			}
		case m.name == "Warrior":
			if in {
				w.spawns = append(w.spawns, spawnIntent{Pos: q, Entity: "minecraft:vindicator"})
			}
		case m.name == "Group of Allays":
			if in {
				w.set(q[0], q[1], q[2], airRID(), false)
			}
			n := int(w.rng.NextIntN(3)) // Drawn in every chunk the piece reaches (P201: no box test).
			if in {
				for range n + 1 {
					w.spawns = append(w.spawns, spawnIntent{Pos: q, Entity: "minecraft:allay"})
				}
			}
		case strings.HasPrefix(m.name, "Chest"):
			idx := 0
			switch m.name {
			case "ChestSouth":
				idx = 2
			case "ChestWest":
				idx = 3
			case "ChestEast":
				idx = 1
			}
			if p.mirror == 1 && idx%2 == 0 || p.mirror == 2 && idx%2 == 1 {
				idx += 2
			}
			facing := [4]string{"north", "east", "south", "west"}[(idx+p.rot)&3]
			if in && w.blockName(q[0], q[1], q[2]) != "chest" {
				w.chest(q[0], q[1], q[2], ridNamed("chest", map[string]any{"minecraft:cardinal_direction": facing}),
					"loot_tables/chests/woodland_mansion.json")
			}
		}
	}
}

// mirrorProps mirrors a Java state's properties: 1 LEFT_RIGHT swaps north and south, 2 FRONT_BACK east
// and west (Java's template transform, P164).
func mirrorProps(p map[string]string, mirror int) map[string]string {
	if mirror == 0 {
		return p
	}
	a, c := "north", "south"
	if mirror == 2 {
		a, c = "east", "west"
	}
	swap := func(v string) string {
		switch v {
		case a:
			return c
		case c:
			return a
		}
		return v
	}
	out := make(map[string]string, len(p))
	for k, v := range p {
		switch k {
		case "facing":
			v = swap(v)
		case "hinge":
			if v == "left" {
				v = "right"
			} else if v == "right" {
				v = "left"
			}
		case "rotation":
			n, _ := strconv.Atoi(v)
			if mirror == 1 {
				n = (8 - n) & 15
			} else {
				n = (16 - n) & 15
			}
			v = strconv.Itoa(n)
		}
		out[swap(k)] = v
	}
	return out
}

// legacyJavaState rewrites a pre-1.13 Java state (the mansion templates' palettes: a name plus variant,
// color or type properties) as the modern Java state, which javablocks.go then turns into Bedrock's
// (P207's mapping, in our own table).
func legacyJavaState(st tState) tState {
	p := st.props
	rest := func(drop ...string) map[string]string {
		out := map[string]string{}
	next:
		for k, v := range p {
			for _, d := range drop {
				if k == d {
					continue next
				}
			}
			out[k] = v
		}
		return out
	}
	colour := func() string {
		if c := p["color"]; c == "silver" {
			return "light_gray"
		} else if c != "" {
			return c
		}
		return "white"
	}
	switch st.name {
	case "planks":
		return tState{p["variant"] + "_planks", rest("variant")}
	case "log", "log2":
		return tState{p["variant"] + "_log", rest("variant")}
	case "leaves", "leaves2":
		q := rest("variant", "check_decay", "decayable")
		q["persistent"] = strconv.FormatBool(p["decayable"] == "false")
		return tState{p["variant"] + "_leaves", q}
	case "wool":
		return tState{colour() + "_wool", rest("color")}
	case "carpet":
		return tState{colour() + "_carpet", rest("color")}
	case "wooden_slab":
		q := rest("variant", "half")
		q["type"] = p["half"]
		return tState{p["variant"] + "_slab", q}
	case "stone_slab", "double_stone_slab":
		if _, ok := p["variant"]; !ok {
			return st
		}
		q := rest("variant", "half", "seamless")
		q["type"] = p["half"]
		if st.name == "double_stone_slab" {
			q["type"] = "double"
		}
		v := p["variant"] // cobblestone, brick, sandstone, quartz, stone_brick, nether_brick keep their name.
		if v == "smoothstone" || v == "stone" {
			v = "smooth_stone"
		}
		return tState{v + "_slab", q}
	case "stone":
		switch v := p["variant"]; v {
		case "smooth_andesite", "smooth_diorite", "smooth_granite":
			return tState{"polished_" + strings.TrimPrefix(v, "smooth_"), rest("variant")}
		case "andesite", "diorite", "granite":
			return tState{v, rest("variant")}
		}
		return tState{"stone", rest("variant")}
	case "dirt":
		if p["variant"] == "coarse_dirt" {
			return tState{"coarse_dirt", rest("variant", "snowy")}
		}
		return tState{"dirt", rest("variant", "snowy")}
	case "sapling":
		return tState{p["type"] + "_sapling", rest("type", "stage")}
	case "fence":
		return tState{"oak_fence", p}
	case "monster_egg":
		switch v := p["variant"]; v {
		case "cobblestone":
			return tState{"infested_cobblestone", rest("variant")}
		case "stone_brick":
			return tState{"infested_stone_bricks", rest("variant")}
		}
		return tState{"infested_stone", rest("variant")}
	case "stone_stairs":
		return tState{"cobblestone_stairs", p} // Java 1.12's stone_stairs is cobblestone.
	case "torch":
		if f := p["facing"]; f != "" && f != "up" {
			return tState{"wall_torch", p}
		}
		return tState{"torch", rest("facing")}
	case "anvil":
		switch p["damage"] {
		case "1":
			return tState{"chipped_anvil", rest("damage")}
		case "2":
			return tState{"damaged_anvil", rest("damage")}
		}
		return tState{"anvil", rest("damage")}
	case "cobblestone_wall":
		q := rest()
		for _, s := range []string{"north", "east", "south", "west"} {
			switch p[s] {
			case "false", "none", "":
				q[s] = "none"
			case "true":
				q[s] = "low"
			}
		}
		return tState{st.name, q}
	case "flower_pot":
		return tState{"flower_pot", rest("contents", "legacy_data")}
	}
	return st
}
