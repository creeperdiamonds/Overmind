package structures

import (
	"strconv"
	"sync"

	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// The woodland mansion (clean-room spec "mansion"): the start's region random lays out an 11 x 11
// floor plan by corridor walks, splits it into rooms on each floor, and places the install's mansion
// templates along it; each chunk writes the templates that reach it, then a cobblestone foundation.
// The templates themselves come from the install (Data.Template("mansion/...")), never from the repo.

// wmReach is how many chunks from its start a mansion can reach.
const wmReach = 6

// wmPiece is one placed template.
type wmPiece struct {
	name   string
	pos    [3]int
	rot    int
	mirror int // 0 none, 1 z flipped, 2 x flipped.
	box    box
	t      *Template
}

// mansion is one assembled mansion.
type mansion struct {
	pieces []*wmPiece
	draws  int // Draws of the start's random after the rotation; only TestCleanroomMansionAssembly reads it.
	all    box // The bounds of every piece box.
}

// Grid values and room flags.
const (
	wmRoom1x1    = 0x10000
	wmRoom1x2    = 0x20000
	wmRoom2x2    = 0x40000
	wmTypeMask   = 0xF0000
	wmOrigin     = 0x100000
	wmDoor       = 0x200000
	wmStairs     = 0x400000
	wmLinkFlag   = 0x800000
	wmIDMask     = 0xffff
	wmGridSize   = 11
	wmOutside    = 5
	wmMaxOutline = 4096 // A bound on an outline walk's steps.
)

// Grid directions, clockwise; wmUp is a room's door upward (none of the four sides).
const (
	wmN = iota
	wmE
	wmS
	wmW
	wmUp
	wmNone = -1
)

var wmDX, wmDY = [4]int{0, 1, 0, -1}, [4]int{-1, 0, 1, 0}

func wmCW(d int) int  { return (d + 1) & 3 }
func wmCCW(d int) int { return (d + 3) & 3 }

type wmGrid [wmGridSize][wmGridSize]int

func (g *wmGrid) get(x, y int) int {
	if x < 0 || y < 0 || x >= wmGridSize || y >= wmGridSize {
		return wmOutside
	}
	return g[y][x]
}

func (g *wmGrid) set(x, y, v int) {
	if x >= 0 && y >= 0 && x < wmGridSize && y < wmGridSize {
		g[y][x] = v
	}
}

func (g *wmGrid) fill(x0, y0, x1, y1, v int) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			g.set(x, y, v)
		}
	}
}

func wmHouse(v int) bool { return v >= 1 && v <= 4 }

func (g *wmGrid) house(x, y int) bool { return wmHouse(g.get(x, y)) }

// wmRand is the start's random with a count of its draws.
type wmRand struct {
	r *worldgen.BedrockRandom
	n int
}

func (r *wmRand) intn(n int) int { r.n++; return int(r.r.NextIntN(int32(n))) }
func (r *wmRand) flip() bool     { r.n++; return r.r.NextBool() }

// walk is a corridor walk on a grid.
func (g *wmGrid) walk(r *wmRand, x, y, h, depth int) {
	if depth <= 0 {
		return
	}
	g.set(x, y, 1)
	if g.get(x+wmDX[h], y+wmDY[h]) == 0 {
		g.set(x+wmDX[h], y+wmDY[h], 1)
	}
	for range 8 {
		dir := [4]int{wmN, wmS, wmW, wmE}[r.intn(4)]
		if dir == (h+2)&3 {
			continue
		}
		if dir == wmE && r.flip() {
			continue
		}
		a, b := x+wmDX[h], y+wmDY[h]
		if g.get(a+wmDX[dir], b+wmDY[dir]) == 0 && g.get(a+2*wmDX[dir], b+2*wmDY[dir]) == 0 {
			g.walk(r, a+wmDX[dir], b+wmDY[dir], dir, depth-1)
			break
		}
	}
	cw, ccw := wmCW(h), wmCCW(h)
	for _, o := range [][2]int{
		{wmDX[cw], wmDY[cw]}, {wmDX[ccw], wmDY[ccw]},
		{wmDX[h] + wmDX[cw], wmDY[h] + wmDY[cw]}, {wmDX[h] + wmDX[ccw], wmDY[h] + wmDY[ccw]},
		{2 * wmDX[h], 2 * wmDY[h]}, {2 * wmDX[cw], 2 * wmDY[cw]}, {2 * wmDX[ccw], 2 * wmDY[ccw]},
	} {
		if g.get(x+o[0], y+o[1]) == 0 {
			g.set(x+o[0], y+o[1], 2)
		}
	}
}

// clean fills the free cells mostly surrounded by houses, pass after pass until none changes.
func (g *wmGrid) clean() {
	for changed := true; changed; {
		changed = false
		for y := range wmGridSize {
			for x := range wmGridSize {
				if g[y][x] != 0 {
					continue
				}
				n, d := 0, 0
				for i := range 4 {
					if g.house(x+wmDX[i], y+wmDY[i]) {
						n++
					}
				}
				for _, o := range [4][2]int{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
					if g.house(x+o[0], y+o[1]) {
						d++
					}
				}
				if n >= 3 || n == 2 && d <= 1 {
					g[y][x] = 2
					changed = true
				}
			}
		}
	}
}

// identify splits a grid's room cells (value 2) into rooms of a room grid.
func wmIdentify(r *wmRand, g, rg *wmGrid) {
	var list [][2]int
	for y := range wmGridSize {
		for x := range wmGridSize {
			if g[y][x] == 2 {
				list = append(list, [2]int{x, y})
			}
		}
	}
	for k := len(list) - 1; k >= 1; k-- {
		j := r.intn(k)
		list[k], list[j] = list[j], list[k]
	}
	usable := func(x, y int) bool { return rg.get(x, y) == 0 && g.get(x, y) == 2 }
	id := 10
	for _, c := range list {
		x, y := c[0], c[1]
		if rg.get(x, y) != 0 {
			continue
		}
		x0, y0, x1, y1, typ := x, y, x, y, wmRoom1x1
		switch {
		case usable(x+1, y) && usable(x, y+1) && usable(x+1, y+1):
			x1, y1, typ = x+1, y+1, wmRoom2x2
		case usable(x-1, y) && usable(x-1, y+1) && usable(x, y+1):
			x0, y1, typ = x-1, y+1, wmRoom2x2
		case usable(x-1, y-1) && usable(x, y-1) && usable(x-1, y):
			x0, y0, typ = x-1, y-1, wmRoom2x2
		case usable(x+1, y):
			x1, typ = x+1, wmRoom1x2
		case usable(x, y+1):
			y1, typ = y+1, wmRoom1x2
		case usable(x-1, y):
			x0, typ = x-1, wmRoom1x2
		case usable(x, y-1):
			y0, typ = y-1, wmRoom1x2
		}
		dx, dy := x1, y1
		if r.flip() {
			dx = x0
		}
		if r.flip() {
			dy = y0
		}
		touches := func() bool {
			for i := range 4 {
				if g.get(dx+wmDX[i], dy+wmDY[i]) == 1 {
					return true
				}
			}
			return false
		}
		flipX := func() {
			if dx == x0 {
				dx = x1
			} else {
				dx = x0
			}
		}
		flipY := func() {
			if dy == y0 {
				dy = y1
			} else {
				dy = y0
			}
		}
		door := true
		if !touches() {
			flipX()
			flipY()
			if !touches() {
				flipY()
				if !touches() {
					flipX()
					flipY()
					if !touches() {
						door, dx, dy = false, x0, y0
					}
				}
			}
		}
		for yy := y0; yy <= y1; yy++ {
			for xx := x0; xx <= x1; xx++ {
				rg.set(xx, yy, typ|id)
			}
		}
		v := typ | id | wmOrigin
		if door {
			v |= wmDoor
		}
		rg.set(dx, dy, v)
		id++
	}
}

// wmVec is a world offset.
type wmVec [3]int

func wmTurn(v wmVec, r int) wmVec {
	switch r & 3 {
	case 1:
		return wmVec{-v[2], v[1], v[0]}
	case 2:
		return wmVec{-v[0], v[1], -v[2]}
	case 3:
		return wmVec{v[2], v[1], -v[0]}
	}
	return v
}

// off is p + f F(r) + e E(r) + up blocks up.
func (p wmVec) off(f, e, up, r int) wmVec {
	F, E := wmTurn(wmVec{0, 0, 1}, r), wmTurn(wmVec{1, 0, 0}, r)
	return wmVec{p[0] + f*F[0] + e*E[0], p[1] + up, p[2] + f*F[2] + e*E[2]}
}

// wmMap maps a template position to the world for a piece: mirror, turn, then move to the origin.
func wmMap(p *wmPiece, q [3]int) [3]int {
	switch p.mirror {
	case 1:
		q[2] = -q[2]
	case 2:
		q[0] = -q[0]
	}
	v := wmTurn(wmVec(q), p.rot)
	return [3]int{p.pos[0] + v[0], p.pos[1] + v[1], p.pos[2] + v[2]}
}

// wmBuild is one mansion being assembled.
type wmBuild struct {
	s       *Structures
	r       *wmRand
	rot     int
	pieces  []*wmPiece
	missing string
}

func (b *wmBuild) add(name string, pos wmVec, rot, mirror int) {
	if b.missing != "" {
		return
	}
	t := b.s.data.Template("mansion/" + name)
	if t == nil {
		b.missing = name
		return
	}
	p := &wmPiece{name: name, pos: pos, rot: rot & 3, mirror: mirror, t: t}
	a := wmMap(p, [3]int{})
	c := wmMap(p, [3]int{t.Size[0] - 1, t.Size[1] - 1, t.Size[2] - 1})
	p.box = box{min(a[0], c[0]), min(a[1], c[1]), min(a[2], c[2]), max(a[0], c[0]), max(a[1], c[1]), max(a[2], c[2])}
	b.pieces = append(b.pieces, p)
}

// anchor is A(B, x, y): a grid cell's corner from a base.
func (b *wmBuild) anchor(base wmVec, x, y int) wmVec {
	return base.off(8+8*(y-5), 8*(x-8), 0, b.rot)
}

// wmCursor walks the outer walls.
type wmCursor struct {
	pos  wmVec
	rot  int
	tmpl string
}

func (b *wmBuild) wall(c *wmCursor) {
	b.add(c.tmpl, c.pos.off(0, 7, 0, c.rot), c.rot, 0)
	c.pos = c.pos.off(8, 0, 0, c.rot)
}

func (b *wmBuild) turn(c *wmCursor) {
	c.pos = c.pos.off(-1, 0, 0, c.rot)
	b.add("wall_corner", c.pos, c.rot, 0)
	c.pos = c.pos.off(-7, 6, 0, c.rot)
	c.rot = (c.rot + 1) & 3
}

func (b *wmBuild) inner(c *wmCursor) {
	c.pos = c.pos.off(6, 8, 0, c.rot)
	c.rot = (c.rot + 3) & 3
}

// outline walks the outside of a grid's houses from (x0, y0) until it is back at (x1, y1) heading S.
func (b *wmBuild) outline(c *wmCursor, g *wmGrid, x0, y0, x1, y1 int) {
	gx, gy, d := x0, y0, wmS
	for range wmMaxOutline {
		nx, ny := gx+wmDX[d], gy+wmDY[d]
		done := func() bool { return gx == x1 && gy == y1 && d == wmS }
		switch {
		case !g.house(nx, ny):
			b.turn(c)
			d = wmCW(d)
			if !done() {
				b.wall(c)
			}
		case g.house(nx+wmDX[wmCCW(d)], ny+wmDY[wmCCW(d)]):
			b.inner(c)
			gx, gy, d = nx, ny, wmCCW(d)
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

// roofs places one roof level over grid g; u is the grid of the level above (nil for none).
func (b *wmBuild) roofs(base wmVec, g, u *wmGrid) {
	rot := b.rot
	upper := func(x, y int) bool { return u != nil && u.house(x, y) }
	for y := range wmGridSize {
		for x := range wmGridSize {
			if !g.house(x, y) || upper(x, y) {
				continue
			}
			C := b.anchor(base, x, y)
			b.add("roof", C.off(0, 0, 3, rot), rot, 0)
			if !g.house(x+1, y) {
				b.add("roof_front", C.off(0, 6, 0, rot), rot, 0)
			}
			if !g.house(x-1, y) {
				b.add("roof_front", C.off(7, 0, 0, rot), rot+2, 0)
			}
			if !g.house(x, y-1) {
				b.add("roof_front", C.off(0, -1, 0, rot), rot+3, 0)
			}
			if !g.house(x, y+1) {
				b.add("roof_front", C.off(6, 6, 0, rot), rot+1, 0)
			}
		}
	}
	if u != nil {
		for y := range wmGridSize {
			for x := range wmGridSize {
				if !g.house(x, y) || !upper(x, y) {
					continue
				}
				C := b.anchor(base, x, y)
				e, w, n, s := !g.house(x+1, y), !g.house(x-1, y), !g.house(x, y-1), !g.house(x, y+1)
				if e {
					b.add("small_wall", C.off(0, 7, 0, rot), rot, 0)
				}
				if w {
					b.add("small_wall", C.off(6, -1, 0, rot), rot+2, 0)
				}
				if n {
					b.add("small_wall", C.off(-1, 0, 0, rot), rot+3, 0)
				}
				if s {
					b.add("small_wall", C.off(7, 6, 0, rot), rot+1, 0)
				}
				if e && n {
					b.add("small_wall_corner", C.off(-2, 7, 0, rot), rot, 0)
				}
				if e && s {
					b.add("small_wall_corner", C.off(7, 8, 0, rot), rot+1, 0)
				}
				if w && n {
					b.add("small_wall_corner", C.off(-1, -2, 0, rot), rot+3, 0)
				}
				if w && s {
					b.add("small_wall_corner", C.off(8, -1, 0, rot), rot+2, 0)
				}
			}
		}
	}
	for y := range wmGridSize {
		for x := range wmGridSize {
			if !g.house(x, y) || upper(x, y) {
				continue
			}
			C := b.anchor(base, x, y)
			n, s := !g.house(x, y-1), !g.house(x, y+1)
			if !g.house(x+1, y) {
				Q := C.off(0, 6, 0, rot)
				if s {
					b.add("roof_corner", Q.off(6, 0, 0, rot), rot, 0)
				} else if g.house(x+1, y+1) {
					b.add("roof_inner_corner", Q.off(5, 0, 0, rot), rot, 0)
				}
				if n {
					b.add("roof_corner", Q, rot+3, 0)
				} else if g.house(x+1, y-1) {
					b.add("roof_inner_corner", C.off(-2, 9, 0, rot), rot+1, 0)
				}
			}
			if !g.house(x-1, y) {
				Q := C
				if s {
					b.add("roof_corner", Q.off(6, 0, 0, rot), rot+1, 0)
				} else if g.house(x-1, y+1) {
					b.add("roof_inner_corner", Q.off(8, -3, 0, rot), rot+3, 0)
				}
				if n {
					b.add("roof_corner", Q, rot+2, 0)
				} else if g.house(x-1, y-1) {
					b.add("roof_inner_corner", Q.off(1, 0, 0, rot), rot+2, 0)
				}
			}
		}
	}
}

// wmPlace is a room template's placement relative to P: f F + e E, a turn added to the rotation, a mirror.
type wmPlace struct {
	f, e, turn, mirror int
	kind               string // 1x2 rooms: side, front or secret.
}

var wm1x2 = map[[2]int]wmPlace{
	{wmE, wmS}: {0, 1, 0, 0, "side"}, {wmE, wmN}: {6, 1, 0, 1, "side"},
	{wmW, wmN}: {6, 7, 2, 0, "side"}, {wmW, wmS}: {0, 7, 0, 2, "side"},
	{wmS, wmE}: {0, 1, 1, 1, "side"}, {wmS, wmW}: {0, 7, 1, 0, "side"},
	{wmN, wmW}: {6, 7, 1, 2, "side"}, {wmN, wmE}: {6, 1, 3, 0, "side"},
	{wmS, wmN}: {-8, 1, 0, 0, "front"}, {wmN, wmS}: {14, 7, 2, 0, "front"},
	{wmW, wmE}: {0, 15, 1, 0, "front"}, {wmE, wmW}: {6, -7, 3, 0, "front"},
	{wmUp, wmE}: {0, 15, 1, 0, "secret"}, {wmUp, wmS}: {0, 1, 0, 0, "secret"},
}

var wm2x2 = map[[2]int]wmPlace{
	{wmE, wmS}: {0, -7, 0, 0, ""}, {wmE, wmN}: {6, -7, 0, 1, ""},
	{wmN, wmE}: {14, 1, 3, 0, ""}, {wmN, wmW}: {14, 7, 3, 1, ""},
	{wmS, wmW}: {-8, 7, 1, 0, ""}, {wmS, wmE}: {-8, 1, 1, 1, ""},
	{wmW, wmN}: {6, 15, 2, 0, ""}, {wmW, wmS}: {0, 15, 0, 2, ""},
}

// floor places one floor's corridors, then its rooms.
func (b *wmBuild) floor(f int, fo wmVec, g, rg *wmGrid) {
	rot := b.rot
	sfx := ""
	if f > 0 {
		sfx = "_2"
	}
	link := func(x, y int) bool { return g.get(x, y) == 1 || rg.get(x, y)&wmLinkFlag != 0 }
	for y := range wmGridSize {
		for x := range wmGridSize {
			if g[y][x] != 1 {
				continue
			}
			D := b.anchor(fo, x, y)
			b.add("corridor_floor", D, rot, 0)
			if link(x, y-1) {
				b.add("carpet_north", D.off(0, 1, 1, rot), rot, 0)
			}
			if link(x+1, y) {
				b.add("carpet_east", D.off(1, 5, 1, rot), rot, 0)
			}
			if link(x, y+1) {
				b.add("carpet_south"+sfx, D.off(5, -1, 0, rot), rot, 0)
			}
			if link(x-1, y) {
				b.add("carpet_west"+sfx, D.off(-1, -1, 0, rot), rot, 0)
			}
		}
	}
	draw := func(name string, n int) string { return name + strconv.Itoa(1+b.r.intn(n)) }
	for y := range wmGridSize {
		for x := range wmGridSize {
			gv := g[y][x]
			if gv != 2 && !(f == 2 && gv == 3) {
				continue
			}
			data := rg.get(x, y)
			id := data & wmIDMask
			start := f == 2 && gv == 3 && data&wmLinkFlag != 0
			door := wmNone
			if data&wmDoor != 0 {
				var sides []int
				for d := range 4 {
					if g.get(x+wmDX[d], y+wmDY[d]) == 1 {
						sides = append(sides, d)
					}
				}
				if len(sides) > 0 {
					door = sides[b.r.intn(len(sides))]
				}
			}
			if door == wmNone && data&wmOrigin != 0 {
				door = wmUp
			}
			same := func(d int) bool { return rg.get(x+wmDX[d], y+wmDY[d])&wmIDMask == id }
			P := b.anchor(fo, x, y).off(0, -1, 0, rot)
			wall := func(side int) string {
				if door == side {
					return "indoors_door" + sfx
				}
				return "indoors_wall" + sfx
			}
			if g.house(x-1, y) && !same(wmW) {
				b.add(wall(wmW), P, rot, 0)
			}
			if g.get(x+1, y) == 1 && !start {
				b.add(wall(wmE), P.off(0, 8, 0, rot), rot, 0)
			}
			if g.house(x, y+1) && !same(wmS) {
				b.add(wall(wmS), P.off(7, 7, 0, rot), rot+1, 0)
			}
			if g.get(x, y-1) == 1 && !start {
				b.add(wall(wmN), P.off(-1, 7, 0, rot), rot+1, 0)
			}
			switch data & wmTypeMask {
			case wmRoom1x1:
				name := "1x1_b"
				if f == 0 {
					name = "1x1_a"
				}
				name = draw(name, 5)
				turn, pf, pe := 0, 0, 1
				switch door {
				case wmE:
				case wmN:
					turn, pf, pe = 3, 6, 1
				case wmW:
					turn, pf, pe = 2, 6, 7
				case wmS:
					turn, pf, pe = 1, 0, 7
				default:
					name = draw("1x1_as", 4)
				}
				b.add(name, P.off(pf, pe, 0, rot), rot+turn, 0)
			case wmRoom1x2:
				if door == wmNone {
					break
				}
				dir := wmNone
				for d := range 4 {
					if same(d) {
						dir = d
						break
					}
				}
				pl, ok := wm1x2[[2]int{door, dir}]
				if !ok {
					break
				}
				stairs := data&wmStairs != 0
				var name string
				switch {
				case pl.kind == "side" && f == 0:
					name = draw("1x2_a", 9)
				case pl.kind == "side" && stairs:
					name = "1x2_c_stairs"
				case pl.kind == "side":
					name = draw("1x2_c", 4)
				case pl.kind == "front" && f == 0:
					name = draw("1x2_b", 5)
				case pl.kind == "front" && stairs:
					name = "1x2_d_stairs"
				case pl.kind == "front":
					name = draw("1x2_d", 5)
				case f == 0:
					name = draw("1x2_s", 2)
				default:
					name = draw("1x2_se", 1)
				}
				b.add(name, P.off(pl.f, pl.e, 0, rot), rot+pl.turn, pl.mirror)
			case wmRoom2x2:
				switch door {
				case wmNone:
				case wmUp:
					b.add("2x2_s1", P.off(0, 1, 0, rot), rot, 0)
				default:
					dir := wmCW(door)
					if !same(dir) {
						dir = (dir + 2) & 3
					}
					pl := wm2x2[[2]int{door, dir}] // Every pair is listed; a missing one would be P, rot, none.
					var name string
					if f == 0 {
						name = draw("2x2_a", 4)
					} else {
						name = draw("2x2_b", 5)
					}
					b.add(name, P.off(pl.f, pl.e, 0, rot), rot+pl.turn, pl.mirror)
				}
			}
		}
	}
}

// mansionAt is the mansion whose start chunk is (cx, cz), nil when the chunk is not a start, when there
// is no install data, or when a template is missing (noted in the data's problems).
func (s *Structures) mansionAt(cx, cz int) *mansion {
	if s.data == nil {
		return nil
	}
	return s.mansions.get([2]int{cx, cz}, func() any {
		st, ok := s.StartIn(Mansion, cx, cz)
		if !ok {
			return (*mansion)(nil)
		}
		return s.assembleMansion(st)
	}).(*mansion)
}

func (s *Structures) assembleMansion(st Start) *mansion {
	rr := st.random()
	rot := int(rr.NextIntN(4))
	r := &wmRand{r: rr}

	// Start height: the lowest of four noise heights around the chunk's middle, never below the sea.
	dx, dz := [4]int{5, -5, -5, 5}[rot], [4]int{5, 5, -5, -5}[rot]
	minH := 1 << 30
	for _, c := range [4][2]int{{7, 7}, {7, 7 + dz}, {7 + dx, 7}, {7 + dx, 7 + dz}} {
		minH = min(minH, s.g.NoiseHeight(st.CX*16+c[0], st.CZ*16+c[1])+1)
	}
	O := wmVec{st.CX*16 + 8, max(minH, seaLevelField-1) + 1, st.CZ*16 + 8}

	// The base grid and its corridors.
	var base wmGrid
	base.fill(7, 4, 8, 5, 3)
	base.fill(6, 4, 6, 5, 2)
	base.fill(9, 2, 10, 7, 5)
	base.fill(8, 2, 8, 3, 1)
	base.fill(8, 6, 8, 7, 1)
	base.set(6, 3, 1)
	base.set(6, 6, 1)
	base.fill(0, 0, 10, 1, 5)
	base.fill(0, 9, 10, 10, 5)
	base.walk(r, 7, 2, wmW, 6)
	base.walk(r, 7, 7, wmW, 6)
	base.walk(r, 5, 3, wmW, 3)
	base.walk(r, 5, 6, wmW, 3)
	base.clean()

	// Room grids.
	var f0, f1, f2, third wmGrid
	wmIdentify(r, &base, &f0)
	wmIdentify(r, &base, &f1)
	for _, g := range []*wmGrid{&f0, &f1} {
		g.set(8, 4, wmLinkFlag)
		g.set(8, 5, wmLinkFlag)
	}
	var cands [][2]int
	for y := range wmGridSize {
		for x := range wmGridSize {
			if v := f1[y][x]; v&wmTypeMask == wmRoom1x2 && v&wmDoor != 0 {
				cands = append(cands, [2]int{x, y})
			}
		}
	}
	allBlocked := func() {
		for y := range wmGridSize {
			for x := range wmGridSize {
				third[y][x] = wmOutside
			}
		}
	}
	if len(cands) == 0 {
		allBlocked()
	} else {
		p := cands[r.intn(len(cands))]
		v := f1.get(p[0], p[1])
		f1.set(p[0], p[1], v|wmStairs)
		end := p
		for d := range 4 {
			q := [2]int{p[0] + wmDX[d], p[1] + wmDY[d]}
			if f1.get(q[0], q[1])&wmIDMask == v&wmIDMask {
				end = q
				break
			}
		}
		for y := range wmGridSize {
			for x := range wmGridSize {
				if !base.house(x, y) {
					third[y][x] = wmOutside
				}
			}
		}
		third.set(p[0], p[1], 3)
		third.set(end[0], end[1], 3)
		f2.set(end[0], end[1], wmLinkFlag)
		var dirs []int
		for d := range 4 {
			if third.get(end[0]+wmDX[d], end[1]+wmDY[d]) == 0 {
				dirs = append(dirs, d)
			}
		}
		if len(dirs) == 0 {
			allBlocked()
			f1.set(p[0], p[1], v)
		} else {
			d := dirs[r.intn(len(dirs))]
			third.walk(r, end[0]+wmDX[d], end[1]+wmDY[d], d, 4)
			third.clean()
		}
	}
	wmIdentify(r, &third, &f2)

	// Templates.
	b := &wmBuild{s: s, r: r, rot: rot}
	b.add("entrance", O.off(0, -9, 0, rot), rot, 0)
	c1 := &wmCursor{O.off(16, 0, 0, rot), rot, "wall_flat"}
	c2 := &wmCursor{O.off(16, 0, 8, rot), rot, "wall_window"}
	b.outline(c1, &base, 8, 5, 8, 4)
	b.outline(c2, &base, 8, 5, 8, 4)
	c3 := &wmCursor{c1.pos.off(0, 0, 19, rot), rot, "wall_window"}
third:
	for y := range wmGridSize {
		for x := wmGridSize - 1; x >= 0; x-- {
			if third.house(x, y) {
				c3.pos = c3.pos.off(8+8*(y-5), 8*(x-8), 0, rot)
				b.wall(c3)
				b.outline(c3, &third, x, y, x, y)
				break third
			}
		}
	}
	b.roofs(O.off(0, 0, 16, rot), &base, &third)
	b.roofs(O.off(0, 0, 27, rot), &third, nil)
	b.floor(0, O, &base, &f0)
	b.floor(1, O.off(0, 0, 8, rot), &base, &f1)
	b.floor(2, O.off(0, 0, 19, rot), &third, &f2)
	if b.missing != "" {
		s.data.missingTemplate("mansion", "mansion/"+b.missing) // Logged once per template; the mansion is not placed.
		return nil
	}
	m := &mansion{pieces: b.pieces, draws: r.n}
	boxes := make([]box, len(m.pieces))
	for i, p := range m.pieces {
		boxes[i] = p.box
	}
	m.all = boundsOf(boxes)
	return m
}

// placeMansions writes every mansion that reaches the chunk: its pieces in list order, then its
// foundation.
func (s *Structures) placeMansions(w *writer) {
	if s.data == nil {
		return
	}
	sp := spreads[Mansion]
	for rx := floorDiv(w.cx-wmReach, sp.spacing); rx <= floorDiv(w.cx+wmReach, sp.spacing); rx++ {
		for rz := floorDiv(w.cz-wmReach, sp.spacing); rz <= floorDiv(w.cz+wmReach, sp.spacing); rz++ {
			cx, cz := sp.candidate(s, rx, rz, worldgen.NewBedrockRandom(0))
			if abs(cx-w.cx) > wmReach || abs(cz-w.cz) > wmReach {
				continue
			}
			m := s.mansionAt(cx, cz)
			if m == nil {
				continue
			}
			cb := w.chunkBox()
			for _, p := range m.pieces {
				if p.box.intersects(cb) {
					wmWrite(w, p)
				}
			}
			m.foundation(w)
		}
	}
}

// wmWrite is one piece in one chunk: the template's blocks, then its data markers.
func wmWrite(w *writer, p *wmPiece) {
	t := p.t
	type marker struct {
		pos  [3]int
		name string
	}
	var markers []marker
	states := make([]*convResult, len(t.palette))
	for _, b := range t.blocks {
		st := t.palette[b.state]
		if st.name == "structure_block" {
			md, _ := b.nbt["metadata"].(string)
			markers = append(markers, marker{b.pos, md})
			continue
		}
		q := wmMap(p, b.pos)
		if !w.in(q[0], q[1], q[2]) {
			continue
		}
		res := states[b.state]
		if res == nil {
			ls := wmLegacy(st)
			r := convert.state(tState{ls.name, mirrorProps(ls.props, p.mirror)}, p.rot)
			res = &r
			states[b.state] = res
		}
		if !res.ok {
			continue
		}
		w.set(q[0], q[1], q[2], res.rid, res.waterlog)
		if res.name == "chest" || res.name == "trapped_chest" {
			w.rng.U32()
			w.bes = append(w.bes, chunk.BlockEntity{Pos: q, Data: map[string]any{"id": "Chest"}})
		}
	}
	for _, i := range markerOrder(len(markers), func(i int) [3]int { return markers[i].pos }) {
		m := markers[i]
		q := wmMap(p, m.pos)
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
			n := int(w.rng.NextIntN(3))
			if in {
				for range n + 1 {
					w.spawns = append(w.spawns, spawnIntent{Pos: q, Entity: "minecraft:allay"})
				}
			}
		case len(m.name) >= 5 && m.name[:5] == "Chest":
			index := wmChestIndex(m.name)
			if p.mirror == 1 && index%2 == 0 || p.mirror == 2 && index%2 == 1 {
				index += 2
			}
			if in && w.blockName(q[0], q[1], q[2]) != "chest" {
				w.chest(q[0], q[1], q[2], wmChests()[(index+p.rot)&3], "loot_tables/chests/woodland_mansion.json")
			}
		}
	}
}

// wmChestIndex is a chest marker's facing as an index of wmChests (0 north, 1 east, 2 south, 3 west):
// "ChestEast", "ChestSouth" and "ChestWest" name theirs, any other chest marker faces north.
func wmChestIndex(marker string) int {
	switch marker {
	case "ChestEast":
		return 1
	case "ChestSouth":
		return 2
	case "ChestWest":
		return 3
	}
	return 0
}

// wmChests are the chest states facing north, east, south and west.
var wmChests = sync.OnceValue(func() (out [4]uint32) {
	for i, f := range [4]string{"north", "east", "south", "west"} {
		out[i] = mustState("chest[minecraft:cardinal_direction=" + f + "]")
	}
	return out
})

// foundation fills cobblestone down from under the mansion's lowest layer, in the chunk, through air
// and liquids, to y 2 at the lowest.
func (m *mansion) foundation(w *writer) {
	cb := w.chunkBox()
	if !m.all.intersects(cb) {
		return
	}
	y0 := m.all[1]
	var under []box
	for _, p := range m.pieces {
		if p.box.intersects(cb) && p.box[1] <= y0 && y0 <= p.box[4] {
			under = append(under, p.box)
		}
	}
	if len(under) == 0 || !w.in(cb[0], y0, cb[2]) {
		return
	}
	cobble := mustState("cobblestone")
	for x := max(m.all[0], cb[0]); x <= min(m.all[3], cb[3]); x++ {
		for z := max(m.all[2], cb[2]); z <= min(m.all[5], cb[5]); z++ {
			if w.blockName(x, y0, z) == "air" {
				continue
			}
			inside := false
			for _, b := range under {
				if b.inside([3]int{x, y0, z}) {
					inside = true
					break
				}
			}
			if !inside {
				continue
			}
			for y := y0 - 1; y >= 2 && y >= w.lo; y-- {
				if w.mainAt(x, y, z) != airRID() && !w.liquidAt(x, y, z) {
					break
				}
				w.set(x, y, z, cobble, false)
			}
		}
	}
}

// wmLegacy rewrites a template's pre-1.13 Java state as the modern one.
func wmLegacy(st tState) tState {
	p := make(map[string]string, len(st.props))
	for k, v := range st.props {
		p[k] = v
	}
	name := st.name
	take := func(k string) string { v := p[k]; delete(p, k); return v }
	color := func() string {
		c := take("color")
		switch c {
		case "":
			c = "white"
		case "silver":
			c = "light_gray"
		}
		return c
	}
	switch name {
	case "planks":
		if v := take("variant"); v != "" {
			name = v + "_planks"
		}
	case "log", "log2":
		if v := take("variant"); v != "" {
			name = v + "_log"
		}
	case "leaves", "leaves2":
		if v := take("variant"); v != "" {
			name = v + "_leaves"
		}
		take("check_decay")
		p["persistent"] = strconv.FormatBool(take("decayable") == "false")
	case "wool":
		name = color() + "_wool"
	case "carpet":
		name = color() + "_carpet"
	case "wooden_slab":
		v := take("variant")
		p["type"] = take("half")
		name = v + "_slab"
	case "stone_slab", "double_stone_slab":
		if v, ok := p["variant"]; ok {
			if v == "smoothstone" || v == "stone" {
				v = "smooth_stone"
			}
			t := p["half"]
			if name == "double_stone_slab" {
				t = "double"
			}
			delete(p, "variant")
			delete(p, "half")
			delete(p, "seamless")
			p["type"] = t
			name = v + "_slab"
		}
	case "stone":
		switch v := take("variant"); v {
		case "smooth_andesite", "smooth_diorite", "smooth_granite":
			name = "polished_" + v[len("smooth_"):]
		case "andesite", "diorite", "granite":
			name = v
		}
	case "dirt":
		if take("variant") == "coarse_dirt" {
			name = "coarse_dirt"
		}
		take("snowy")
	case "sapling":
		name = take("type") + "_sapling"
		take("stage")
	case "fence":
		name = "oak_fence"
	case "monster_egg":
		switch take("variant") {
		case "cobblestone":
			name = "infested_cobblestone"
		case "stone_brick":
			name = "infested_stone_bricks"
		default:
			name = "infested_stone"
		}
	case "stone_stairs":
		name = "cobblestone_stairs"
	case "torch":
		if f, ok := p["facing"]; ok && f != "up" {
			name = "wall_torch"
		} else {
			delete(p, "facing")
		}
	case "anvil":
		switch take("damage") {
		case "1":
			name = "chipped_anvil"
		case "2":
			name = "damaged_anvil"
		}
	case "cobblestone_wall":
		for _, side := range []string{"north", "east", "south", "west"} {
			switch p[side] {
			case "true":
				p[side] = "low"
			case "false", "none", "":
				p[side] = "none"
			} // Other values (low, tall) are kept.
		}
	case "flower_pot":
		take("contents")
		take("legacy_data")
	}
	return tState{name: name, props: p}
}

// mirrorProps mirrors a Java state's properties: mirror 1 swaps north and south, mirror 2 east and
// west, in the facing's value and in property names; a door's hinge swaps sides; a rotation (0..15)
// is reflected. Mirror 0 returns the properties as they are.
func mirrorProps(props map[string]string, mirror int) map[string]string {
	var swap map[string]string
	switch mirror {
	case 1:
		swap = map[string]string{"north": "south", "south": "north"}
	case 2:
		swap = map[string]string{"east": "west", "west": "east"}
	default:
		return props
	}
	out := make(map[string]string, len(props))
	for k, v := range props {
		switch k {
		case "facing":
			if s, ok := swap[v]; ok {
				v = s
			}
		case "hinge":
			switch v {
			case "left":
				v = "right"
			case "right":
				v = "left"
			}
		case "rotation":
			if n, err := strconv.Atoi(v); err == nil {
				if mirror == 1 {
					v = strconv.Itoa(((8-n)%16 + 16) % 16)
				} else {
					v = strconv.Itoa(((16-n)%16 + 16) % 16)
				}
			}
		}
		if s, ok := swap[k]; ok {
			k = s
		}
		out[k] = v
	}
	return out
}
