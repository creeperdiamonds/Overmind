package structures

import (
	"fmt"
	"strings"
	"sync"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// The Nether fortress (clean-room spec "fortress"): the start's region random goes on to grow a tree of
// pieces from two weighted lists, then moves it to a random height; each piece writes its final cells
// (piecedata/fortress_pieces.json), its chest or spawner and its support columns, and an end filler writes
// rows from its own random.

// fortressReach is how many chunks from its start a fortress piece can reach.
const fortressReach = 9

// nfFar is how far (blocks, per axis) from the start piece's corner a piece may still open onto more than
// an end filler.
const nfFar = 112

// Piece kinds.
const (
	nfBridgeStraight = iota
	nfBridgeCrossing
	nfRoomCrossing
	nfStairsRoom
	nfMonsterThrone
	nfCastleEntrance
	nfCorridor
	nfCorridorCrossing
	nfRightTurn
	nfLeftTurn
	nfCorridorStairs
	nfTBalcony
	nfStalkRoom
	nfEndFiller
	nfKinds
)

var nfKindNames = [nfKinds]string{"bridge_straight", "bridge_crossing", "room_crossing", "stairs_room", "monster_throne",
	"castle_entrance", "castle_corridor", "castle_corridor_crossing", "castle_right_turn", "castle_left_turn",
	"castle_corridor_stairs", "castle_t_balcony", "castle_stalk_room", "bridge_end_filler"}

// nfPiece is one placed fortress piece.
type nfPiece struct {
	kind   int
	b      box
	o      orient
	depth  int
	chest  bool
	filler uint32 // The end filler's own seed.
}

// fortress is one assembled fortress, its pieces in list order.
type fortress struct {
	pieces []*nfPiece
}

// nfRecord is one entry of a weight list.
type nfRecord struct {
	kind, weight, max int // max 0: no limit.
	again             bool
	count             int
}

func nfBridgeList() []*nfRecord {
	return []*nfRecord{{nfBridgeStraight, 30, 0, true, 0}, {nfBridgeCrossing, 10, 4, false, 0}, {nfRoomCrossing, 10, 4, false, 0},
		{nfStairsRoom, 10, 3, false, 0}, {nfMonsterThrone, 5, 2, false, 0}, {nfCastleEntrance, 5, 1, false, 0}}
}

func nfCastleList() []*nfRecord {
	return []*nfRecord{{nfCorridor, 25, 0, true, 0}, {nfCorridorCrossing, 15, 5, false, 0}, {nfRightTurn, 5, 10, false, 0},
		{nfLeftTurn, 5, 10, false, 0}, {nfCorridorStairs, 10, 3, true, 0}, {nfTBalcony, 7, 2, false, 0}, {nfStalkRoom, 5, 2, false, 0}}
}

// nfShape is a kind's table: its foot box and final cells.
type nfShape struct {
	foot    [6]int
	cells   []nfCell
	column  [][2]int
	chest   *[3]int
	spawner *[3]int
}

type nfCell struct {
	x, y, z int
	rid     uint32
	stairs  int // Local facing index of a stairs block (0 east, 1 west, 2 south, 3 north), -1 otherwise.
}

var (
	nfOnce   sync.Once
	nfShapes [nfKinds]nfShape
	nfChests [nfKinds]nfShape // The "with chest" entries of the two turns.
	// The blocks the code writes itself, worked out once with the table: the stairs by weirdo_direction
	// (0 east, 1 west, 2 south, 3 north), the bricks, the chest and the spawner.
	nfStairs                 [4]uint32
	nfBrick, nfChest, nfMobs uint32
)

type nfRaw struct {
	Kind    string     `json:"kind"`
	Variant string     `json:"variant"`
	Foot    [6]int     `json:"foot_box"`
	Min     [3]int     `json:"min"`
	Palette []string   `json:"palette"`
	Layers  [][]string `json:"layers"`
	Column  [][2]int   `json:"column_down"`
	Chest   []int      `json:"chest"`
	Spawner []int      `json:"spawner"`
}

// nfKeys are the layers' palette keys, in palette order; "." is a cell the piece leaves alone.
const nfKeys = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// nfTables loads the fortress table. Like every embedded table, one that does not load panics.
func nfTables() {
	nfOnce.Do(func() {
		for d := range nfStairs {
			nfStairs[d] = mustState(fmt.Sprintf("nether_brick_stairs[minecraft:corner=none,upside_down_bit=0,weirdo_direction=%d]", d))
		}
		nfBrick = mustState("nether_brick")
		nfChest = mustState("chest[minecraft:cardinal_direction=south]")
		nfMobs = mustState("mob_spawner")
		var raw []nfRaw
		readTable("fortress_pieces.json", &raw)
		byName := map[string]int{}
		for k, n := range nfKindNames {
			byName[n] = k
		}
		for _, r := range raw {
			k, ok := byName[r.Kind]
			if !ok {
				panic("fortress table: unknown kind " + r.Kind)
			}
			sh := nfShape{foot: r.Foot, column: r.Column}
			type pal struct {
				rid    uint32
				stairs int
			}
			var p []pal
			for _, n := range r.Palette {
				name, facing, isStairs := strings.Cut(n, " facing=")
				if isStairs {
					// Written turned by the piece (nfWrite), always nether brick stairs.
					d, ok := weirdo[facing]
					if !ok || name != "nether_brick_stairs" {
						panic(fmt.Sprintf("fortress table: %s: stairs %q", r.Kind, n))
					}
					p = append(p, pal{noBlock, int(d)})
					continue
				}
				rid := paletteState(n)
				if rid == noBlock {
					panic(fmt.Sprintf("fortress table: %s: block %q is not registered", r.Kind, n))
				}
				p = append(p, pal{rid, -1})
			}
			for y, layer := range r.Layers {
				for z, row := range layer {
					for x, ch := range row {
						if ch == '.' {
							continue
						}
						i := strings.IndexRune(nfKeys, ch)
						if i < 0 || i >= len(p) {
							panic(fmt.Sprintf("fortress table: %s: layer key %q is not in its palette of %d", r.Kind, ch, len(p)))
						}
						e := p[i]
						sh.cells = append(sh.cells, nfCell{x + r.Min[0], y + r.Min[1], z + r.Min[2], e.rid, e.stairs})
					}
				}
			}
			if len(r.Chest) == 3 {
				sh.chest = &[3]int{r.Chest[0], r.Chest[1], r.Chest[2]}
			}
			if len(r.Spawner) == 3 {
				sh.spawner = &[3]int{r.Spawner[0], r.Spawner[1], r.Spawner[2]}
			}
			if r.Variant == "with chest" {
				nfChests[k] = sh
			} else {
				nfShapes[k] = sh
			}
		}
		nfShapes[nfEndFiller] = nfShape{foot: [6]int{-1, -3, 0, 5, 10, 8}}
	})
}

// nfBuild is one fortress being assembled.
type nfBuild struct {
	r       *worldgen.BedrockRandom
	pieces  []*nfPiece
	pending []*nfPiece
	start   box
	last    *nfRecord
	lists   [2][]*nfRecord // nfBridges, nfCastles; a record is removed when it reaches its max.
}

// The two weight lists.
const (
	nfBridges = iota
	nfCastles
)

// fortressAtStart is the fortress of a start, assembled once and cached.
func (s *Structures) fortressAtStart(st Start) *fortress {
	return s.fortresses.get([2]int{st.CX, st.CZ}, func() any { return assembleFortress(st) }).(*fortress)
}

func assembleFortress(st Start) *fortress {
	nfTables()
	a := &nfBuild{r: st.random(), lists: [2][]*nfRecord{nfBridgeList(), nfCastleList()}}
	o := [4]orient{oS, oW, oN, oE}[a.r.NextIntN(4)]
	x, z := st.CX*16, st.CZ*16
	start := &nfPiece{kind: nfBridgeCrossing, b: box{x + 2, 64, z + 2, x + 20, 73, z + 20}, o: o}
	a.start = start.b
	a.pieces = []*nfPiece{start}
	a.exits(start)
	for len(a.pending) > 0 {
		i := int(a.r.NextIntN(int32(len(a.pending))))
		p := a.pending[i]
		a.pending = append(a.pending[:i], a.pending[i+1:]...)
		a.exits(p)
	}
	boxes := make([]box, len(a.pieces))
	for i, p := range a.pieces {
		boxes[i] = p.b
	}
	all := boundsOf(boxes)
	k := 48
	if j := 23 - (all[4] - all[1] + 1); j > 1 {
		k += int(a.r.NextIntN(int32(j)))
	}
	dy := k - all[1]
	for _, p := range a.pieces {
		p.b[1] += dy
		p.b[4] += dy
	}
	return &fortress{pieces: a.pieces}
}

// exits makes a piece's children.
func (a *nfBuild) exits(p *nfPiece) {
	switch p.kind {
	case nfBridgeCrossing:
		a.forward(p, 8, 3, nfBridges)
		a.left(p, 3, 8, nfBridges)
		a.right(p, 3, 8, nfBridges)
	case nfBridgeStraight:
		a.forward(p, 1, 3, nfBridges)
	case nfRoomCrossing:
		a.forward(p, 2, 0, nfBridges)
		a.left(p, 0, 2, nfBridges)
		a.right(p, 0, 2, nfBridges)
	case nfStairsRoom:
		a.right(p, 6, 2, nfBridges)
	case nfCastleEntrance:
		a.forward(p, 5, 3, nfCastles)
	case nfCorridor, nfCorridorStairs:
		a.forward(p, 1, 0, nfCastles)
	case nfCorridorCrossing:
		a.forward(p, 1, 0, nfCastles)
		a.left(p, 0, 1, nfCastles)
		a.right(p, 0, 1, nfCastles)
	case nfLeftTurn:
		a.left(p, 0, 1, nfCastles)
	case nfRightTurn:
		a.right(p, 0, 1, nfCastles)
	case nfStalkRoom:
		a.forward(p, 5, 3, nfCastles)
		a.forward(p, 5, 11, nfCastles)
	case nfTBalcony:
		zo := 1
		if p.o == oW || p.o == oN {
			zo = 5
		}
		list := nfCastles
		if a.r.NextIntN(8) == 0 {
			list = nfBridges
		}
		a.left(p, 0, zo, list)
		list = nfCastles
		if a.r.NextIntN(8) == 0 {
			list = nfBridges
		}
		a.right(p, 0, zo, list)
	}
}

func (a *nfBuild) forward(p *nfPiece, xo, yo int, l int) {
	b := p.b
	switch p.o {
	case oN:
		a.child(b[0]+xo, b[1]+yo, b[2]-1, oN, p.depth, l)
	case oS:
		a.child(b[0]+xo, b[1]+yo, b[5]+1, oS, p.depth, l)
	case oW:
		a.child(b[0]-1, b[1]+yo, b[2]+xo, oW, p.depth, l)
	case oE:
		a.child(b[3]+1, b[1]+yo, b[2]+xo, oE, p.depth, l)
	}
}

func (a *nfBuild) left(p *nfPiece, yo, zo int, l int) {
	b := p.b
	if p.o == oN || p.o == oS {
		a.child(b[0]-1, b[1]+yo, b[2]+zo, oW, p.depth, l)
	} else {
		a.child(b[0]+zo, b[1]+yo, b[2]-1, oN, p.depth, l)
	}
}

func (a *nfBuild) right(p *nfPiece, yo, zo int, l int) {
	b := p.b
	if p.o == oN || p.o == oS {
		a.child(b[3]+1, b[1]+yo, b[2]+zo, oE, p.depth, l)
	} else {
		a.child(b[0]+zo, b[1]+yo, b[5]+1, oS, p.depth, l)
	}
}

// child adds a piece at a foot: from the list when the foot is near the start, else an end filler.
func (a *nfBuild) child(x, y, z int, o orient, depth int, l int) {
	var p *nfPiece
	if abs(x-a.start[0]) <= nfFar && abs(z-a.start[2]) <= nfFar {
		p = a.choose(x, y, z, o, depth+1, &a.lists[l])
	} else {
		p = a.create(nfEndFiller, x, y, z, o, depth)
	}
	if p != nil {
		a.pieces = append(a.pieces, p)
		a.pending = append(a.pending, p)
	}
}

// create makes a piece of a kind at a foot, or nil when its box is refused.
func (a *nfBuild) create(kind, x, y, z int, o orient, depth int) *nfPiece {
	f := nfShapes[kind].foot // Offsets across, up and along, then the size.
	b := orientBox(x, y, z, f[0], f[1], f[2], f[3], f[4], f[5], o)
	if b[1] <= 10 {
		return nil
	}
	for _, q := range a.pieces {
		if q.b.intersects(b) {
			return nil
		}
	}
	p := &nfPiece{kind: kind, b: b, o: o, depth: depth}
	switch kind {
	case nfLeftTurn, nfRightTurn:
		p.chest = a.r.NextIntN(3) == 0
	case nfEndFiller:
		p.filler = uint32(a.r.NextInt())
	}
	return p
}

// choose picks a piece from a weight list.
func (a *nfBuild) choose(x, y, z int, o orient, depth int, list *[]*nfRecord) *nfPiece {
	total, avail := 0, false
	for _, r := range *list {
		total += r.weight
		if r.max > 0 && r.count < r.max {
			avail = true
		}
	}
	if avail && total > 0 && depth <= 30 {
	tries:
		for range 5 {
			v := int(a.r.NextIntN(int32(total)))
			for i, r := range *list {
				v -= r.weight
				if v >= 0 {
					continue
				}
				if r.max > 0 && r.count >= r.max || r == a.last && !r.again {
					continue tries
				}
				p := a.create(r.kind, x, y, z, o, depth)
				if p == nil {
					continue
				}
				r.count++
				a.last = r
				if r.max > 0 && r.count >= r.max {
					*list = append((*list)[:i:i], (*list)[i+1:]...)
				}
				return p
			}
		}
	}
	return a.create(nfEndFiller, x, y, z, o, depth)
}

// placeFortress writes a fortress's pieces that reach the chunk, in list order.
func (s *Structures) placeFortress(w *writer, st Start) {
	f := s.fortressAtStart(st)
	cb := w.chunkBox()
	for _, p := range f.pieces {
		if p.b.intersects(cb) {
			nfWrite(w, p)
		}
	}
}

// nfStairsTurn turns a local stairs facing (0 east, 1 west, 2 south, 3 north) by a piece's orientation.
var nfStairsTurn = [4][4]int32{
	oS: {0, 1, 2, 3},
	oN: {0, 1, 3, 2},
	oW: {2, 3, 1, 0}, // East becomes south, west north, south west, north east.
	oE: {2, 3, 0, 1}, // East becomes south, west north, south east, north west.
}

func nfWrite(w *writer, p *nfPiece) {
	if p.kind == nfEndFiller {
		nfFiller(w, p)
		return
	}
	sh := &nfShapes[p.kind]
	if p.chest {
		sh = &nfChests[p.kind]
	}
	f := &opiece{p.b, p.o}
	for _, c := range sh.cells {
		x, y, z := f.world(c.x, c.y, c.z)
		if !w.in(x, y, z) {
			continue
		}
		rid := c.rid
		if c.stairs >= 0 {
			rid = nfStairs[nfStairsTurn[p.o][c.stairs]]
		}
		w.set(x, y, z, rid, false)
	}
	if c := sh.spawner; c != nil {
		x, y, z := f.world(c[0], c[1], c[2])
		if w.in(x, y, z) {
			w.set(x, y, z, nfMobs, false)
			w.bes = append(w.bes, spawnerEntity(x, y, z, "minecraft:blaze"))
		}
	}
	if c := sh.chest; c != nil {
		x, y, z := f.world(c[0], c[1], c[2])
		if w.in(x, y, z) && w.blockName(x, y, z) != "chest" {
			w.chest(x, y, z, nfChest, "loot_tables/chests/nether_bridge.json")
		}
	}
	for _, c := range sh.column {
		x, y, z := f.world(c[0], -1, c[1])
		if !w.in(x, y, z) {
			continue
		}
		for ; y >= w.lo+2 && w.replaceable(x, y, z); y-- {
			w.set(x, y, z, nfBrick, false)
		}
	}
}

// nfFiller is the bridge end filler: rows of nether bricks of random lengths from the piece's own seed.
func nfFiller(w *writer, p *nfPiece) {
	q := worldgen.NewBedrockRandom(p.filler)
	f := &opiece{p.b, p.o}
	row := func(x, y, z1 int) {
		for z := 0; z <= z1; z++ {
			wx, wy, wz := f.world(x, y, z)
			if w.in(wx, wy, wz) {
				w.set(wx, wy, wz, nfBrick, false)
			}
		}
	}
	for x := range 5 {
		for _, y := range []int{3, 4} {
			row(x, y, int(q.NextIntN(8)))
		}
	}
	row(0, 5, int(q.NextIntN(8)))
	row(4, 5, int(q.NextIntN(8)))
	for x := range 5 {
		row(x, 2, int(q.NextIntN(5)))
	}
	for x := range 5 {
		for _, y := range []int{0, 1} {
			row(x, y, int(q.NextIntN(3)))
		}
	}
}
