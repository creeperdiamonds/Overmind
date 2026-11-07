package structures

import (
	"strconv"
	"strings"
	"sync"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// The ocean monument (clean-room spec "monument"): one random assembles a building, a grid of rooms
// whose walls are opened and closed, and the rooms fitted into it; each chunk the building touches then
// runs the building's and each touching room's operation list (piecedata/monument_*.json).

// Faces of a grid room, in the order the closing draw names them.
const (
	omD = iota
	omU
	omN
	omS
	omW
	omE
)

// omStep is the grid step through each face: N is +gz and S is -gz.
var omStep = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, 1}, {0, 0, -1}, {-1, 0, 0}, {1, 0, 0}}

// omCell is one room of the grid, or one of the three rooms outside it (roof, two wings).
type omCell struct {
	g       [3]int
	link    [6]*omCell // The connected room through each face.
	open    [6]bool
	claimed bool
}

// omChild is one room piece of the building.
type omChild struct {
	kind   string
	b      box
	a      *omCell // The room's own cell (nil for the wing rooms and the penthouse).
	design int
}

// monument is one assembled monument.
type monument struct {
	building opiece // The building's box, with the monument's orientation.
	children []omChild
	grid     [75]*omCell // By index gy*25 + gz*5 + gx; nil where the grid has no room.
	// valid says the start passes the monument's biome tests (monumentAt); only a valid start is
	// assembled.
	valid bool
}

// monumentStartSeed is the start's seed: two world floats times the chunk coordinates, truncated, each
// one's low 32 bits XORed with the level seed.
func (s *Structures) monumentStartSeed(cx, cz int) uint32 {
	s.omOnce.Do(func() {
		r := worldgen.NewBedrockRandom(s.seed32)
		s.omF1 = r.NextFloat()
		s.omF2 = r.NextFloat()
	})
	a := s.omF1 * float32(cx)
	b := s.omF2 * float32(cz)
	return uint32(int64(a)) ^ uint32(int64(b)) ^ s.seed32
}

// monumentAtStart is the monument whose start chunk is (cx, cz), assembled once and cached. A candidate
// that fails the biome tests is not assembled (the assembly draws only from its own randoms, so skipping
// it changes nothing else).
func (s *Structures) monumentAtStart(cx, cz int) *monument {
	return s.monuments.get([2]int{cx, cz}, func() any {
		if !s.monumentAt(cx, cz) {
			return &monument{}
		}
		m := s.assembleMonument(cx, cz)
		m.valid = true
		return m
	}).(*monument)
}

func omExists(gx, gy, gz int) bool {
	switch gy {
	case 0, 1:
		return gx >= 0 && gx <= 4 && gz >= 0 && gz <= 3
	case 2:
		return gx >= 1 && gx <= 3 && gz >= 0 && gz <= 1
	}
	return false
}

func omIndex(gx, gy, gz int) int { return gy*25 + gz*5 + gx }

// omReaches says whether a walk through open faces leads from c to the source room.
func omReaches(c, source *omCell) bool {
	seen := map[*omCell]bool{c: true}
	queue := []*omCell{c}
	for len(queue) > 0 {
		r := queue[0]
		queue = queue[1:]
		if r == source {
			return true
		}
		for f, n := range r.link {
			if n != nil && r.open[f] && !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	return false
}

func (s *Structures) assembleMonument(cx, cz int) *monument {
	seed := s.monumentStartSeed(cx, cz)
	r := worldgen.NewBedrockRandom(seed)
	o := [4]orient{oN, oS, oW, oE}[r.NextIntN(4)]
	x0, z0 := cx*16-21, cz*16-21
	m := &monument{building: opiece{box{x0, 39, z0, x0 + 57, 61, z0 + 57}, o}}

	// The grid and its connections.
	var grid [75]*omCell
	var order []*omCell
	for i := range grid {
		gx, gz, gy := i%5, i/5%5, i/25
		if omExists(gx, gy, gz) {
			grid[i] = &omCell{g: [3]int{gx, gy, gz}}
			order = append(order, grid[i])
		}
	}
	at := func(gx, gy, gz int) *omCell {
		if !omExists(gx, gy, gz) {
			return nil
		}
		return grid[omIndex(gx, gy, gz)]
	}
	for _, c := range order {
		for f, st := range omStep {
			c.link[f] = at(c.g[0]+st[0], c.g[1]+st[1], c.g[2]+st[2])
		}
	}
	join := func(c *omCell, f int, extra *omCell) {
		c.link[f] = extra
		extra.link[f^1] = c
	}
	roof := &omCell{g: [3]int{-1, -1, -1}, claimed: true}
	left := &omCell{g: [3]int{-2, -1, -1}, claimed: true}
	right := &omCell{g: [3]int{-3, -1, -1}, claimed: true}
	join(at(2, 2, 0), omU, roof)
	join(at(0, 1, 0), omS, left)
	join(at(4, 1, 0), omS, right)
	source := at(2, 0, 0)

	k := int(r.NextIntN(4))
	core := at(k, 0, 2)
	for _, c := range []*omCell{core, at(k+1, 0, 2), at(k, 0, 3), at(k+1, 0, 3)} {
		c.claimed = true
		c.link[omU].claimed = true
	}
	for _, c := range append(order[:len(order):len(order)], roof) {
		for f, n := range c.link {
			c.open[f] = n != nil
		}
	}

	// The shuffled list, from a second random with the same seed.
	shuffled := append([]*omCell(nil), order...)
	sh := worldgen.NewBedrockRandom(seed)
	for i := len(shuffled) - 1; i >= 1; i-- {
		j := sh.NextIntN(int32(i))
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	// Closing faces.
	for _, d := range shuffled {
		closed := 0
		for tries := 0; closed < 2 && tries < 5; tries++ {
			f := int(r.NextIntN(6))
			if !d.open[f] {
				continue
			}
			n := d.link[f]
			d.open[f], n.open[f^1] = false, false
			if omReaches(d, source) && omReaches(n, source) {
				closed++
			} else {
				d.open[f], n.open[f^1] = true, true // Reopened on both sides (a wing's side too: no room reads it).
			}
		}
	}
	source.claimed = true

	m.grid = grid

	// Room pieces.
	bw := [3]int{}
	bw[0], bw[1], bw[2] = m.building.world(9, 0, 22)
	room := func(kind string, a *omCell, w, h, d, design int) omChild {
		gx, gy, gz := a.g[0], a.g[1], a.g[2]
		sx, sy, sz := 8*w, 4*h, 8*d
		if o == oW || o == oE {
			sx, sz = sz, sx
		}
		var mx, my, mz int
		switch o {
		case oN:
			mx, my, mz = gx*8, gy*4, -(gz+d)*8+1
		case oS:
			mx, my, mz = gx*8, gy*4, gz*8
		case oW:
			mx, my, mz = -(gz+d)*8+1, gy*4, gx*8
		case oE:
			mx, my, mz = gz*8, gy*4, gx*8
		}
		x, y, z := bw[0]+mx, bw[1]+my, bw[2]+mz
		return omChild{kind: kind, b: box{x, y, z, x + sx - 1, y + sy - 1, z + sz - 1}, a: a, design: design}
	}
	m.children = append(m.children, room("entry", source, 1, 1, 1, 0), room("core", core, 2, 2, 2, 0))
	free := func(c *omCell, f int) bool { return c.open[f] && c.link[f] != nil && !c.link[f].claimed }
	claim := func(cs ...*omCell) {
		for _, c := range cs {
			c.claimed = true
		}
	}
	for _, d := range shuffled {
		if d.claimed {
			continue
		}
		E, N, U := d.link[omE], d.link[omN], d.link[omU]
		switch {
		case free(d, omE) && free(d, omU) && free(E, omU):
			claim(d, E, U, E.link[omU])
			m.children = append(m.children, room("double_xy", d, 2, 2, 1, 0))
		case free(d, omN) && free(d, omU) && free(N, omU):
			claim(d, N, U, N.link[omU])
			m.children = append(m.children, room("double_yz", d, 1, 2, 2, 0))
		case free(d, omN):
			claim(d, N)
			m.children = append(m.children, room("double_z", d, 1, 1, 2, 0))
		case free(d, omE):
			claim(d, E)
			m.children = append(m.children, room("double_x", d, 2, 1, 1, 0))
		case free(d, omU):
			claim(d, U)
			m.children = append(m.children, room("double_y", d, 1, 2, 1, 0))
		case !d.open[omW] && !d.open[omE] && !d.open[omN] && !d.open[omS] && !d.open[omU]:
			claim(d)
			m.children = append(m.children, room("simple_top", d, 1, 1, 1, 0))
		default:
			claim(d)
			m.children = append(m.children, room("simple", d, 1, 1, 1, int(r.NextIntN(3))))
		}
	}
	wing := int(r.NextInt())
	bd := &m.building
	m.children = append(m.children,
		omChild{kind: "wing_room", b: bd.span([3]int{1, 1, 1}, [3]int{23, 8, 21}), design: wing & 1},
		omChild{kind: "wing_room", b: bd.span([3]int{34, 1, 1}, [3]int{56, 8, 21}), design: (wing + 1) & 1},
		omChild{kind: "penthouse", b: bd.span([3]int{22, 13, 22}, [3]int{35, 17, 35})})
	return m
}

// Operation lists.

type omOpKind uint8

const (
	omFill omOpKind = iota
	omWater
	omReplaceWater
	omColumn
	omElder
	omSponges
)

// omCond is one condition label of an operation.
type omCond struct {
	neg    bool
	what   uint8 // omTouch, omDesign, omPillar, omOpen, omNoneAbove, omRaised.
	role   uint8 // omRoleA ... omRoleNU.
	face   int
	design int
	rect   [4]int // touch: building-local x0, z0, x1, z1.
}

const (
	omTouch = iota
	omDesign
	omPillar
	omOpen
	omNoneAbove
	omRaised
)

const (
	omRoleA = iota
	omRoleE
	omRoleN
	omRoleAU
	omRoleEU
	omRoleNU
)

type omOp struct {
	kind omOpKind
	b    [6]int
	rid  uint32
	when []omCond
}

var (
	omOnceTables sync.Once
	omBuilding   []omOp
	omRooms      map[string][]omOp
)

type omRawOp struct {
	Op    string   `json:"op"`
	Box   [6]int   `json:"box"`
	Block string   `json:"block"`
	When  []string `json:"when"`
}

func omParseOps(raw []omRawOp) []omOp {
	kinds := map[string]omOpKind{"fill": omFill, "water": omWater, "replace_water": omReplaceWater,
		"column_down": omColumn, "elder": omElder, "sponge_field": omSponges}
	faces := map[string]int{"D": omD, "U": omU, "N": omN, "S": omS, "W": omW, "E": omE}
	roles := map[string]uint8{"A": omRoleA, "E": omRoleE, "N": omRoleN, "AU": omRoleAU, "EU": omRoleEU, "NU": omRoleNU}
	out := make([]omOp, 0, len(raw))
	for _, r := range raw {
		k, ok := kinds[r.Op]
		if !ok {
			panic("monument table: unknown op " + r.Op)
		}
		op := omOp{kind: k, b: r.Box}
		if r.Block != "" {
			op.rid = mustState(r.Block)
		}
		for _, l := range r.When {
			c := omCond{}
			if strings.HasPrefix(l, "!") {
				c.neg, l = true, l[1:]
			}
			switch {
			case strings.HasPrefix(l, "touch:"):
				c.what = omTouch
				parts := strings.Split(l[len("touch:"):], ",")
				for i := range 4 {
					c.rect[i], _ = strconv.Atoi(parts[i])
				}
			case strings.HasPrefix(l, "design="):
				c.what = omDesign
				c.design, _ = strconv.Atoi(l[len("design="):])
			case l == "pillar":
				c.what = omPillar
			default:
				role, rest, _ := strings.Cut(l, ".")
				rr, ok := roles[role]
				if !ok {
					panic("monument table: unknown label " + l)
				}
				c.role = rr
				switch {
				case rest == "noneAbove":
					c.what = omNoneAbove
				case rest == "raised":
					c.what = omRaised
				case strings.HasPrefix(rest, "open."):
					c.what = omOpen
					f, ok := faces[rest[len("open."):]]
					if !ok {
						panic("monument table: unknown face in " + l)
					}
					c.face = f
				default:
					panic("monument table: unknown label " + l)
				}
			}
			op.when = append(op.when, c)
		}
		out = append(out, op)
	}
	return out
}

func omTables() {
	omOnceTables.Do(func() {
		var b []omRawOp
		readTable("monument_building.json", &b)
		omBuilding = omParseOps(b)
		var rooms map[string][]omRawOp
		readTable("monument_rooms.json", &rooms)
		omRooms = map[string][]omOp{}
		for k, v := range rooms {
			omRooms[k] = omParseOps(v)
		}
	})
}

// omRun is one piece being run in one chunk.
type omRun struct {
	w      *writer
	f      opiece
	roles  [6]*omCell
	design int
	pillar bool
}

func (c omCond) holds(r *omRun) bool {
	v := false
	switch c.what {
	case omTouch:
		a := r.f.span([3]int{c.rect[0], 0, c.rect[1]}, [3]int{c.rect[2], 0, c.rect[3]})
		v = a[0] <= r.w.cx*16+15 && a[3] >= r.w.cx*16 && a[2] <= r.w.cz*16+15 && a[5] >= r.w.cz*16
	case omDesign:
		v = r.design == c.design
	case omPillar:
		v = r.pillar
	default:
		x := r.roles[c.role]
		switch c.what {
		case omOpen:
			v = x != nil && x.open[c.face]
		case omNoneAbove:
			v = x == nil || x.link[omU] == nil
		case omRaised:
			v = x != nil && x.g[1] > 0
		}
	}
	return v != c.neg
}

func (r *omRun) run(ops []omOp) {
	w := r.w
	for i := range ops {
		op := &ops[i]
		ok := true
		for _, c := range op.when {
			if !c.holds(r) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		switch op.kind {
		case omFill, omWater, omReplaceWater:
			cb, in := w.clip(r.f.span([3]int{op.b[0], op.b[1], op.b[2]}, [3]int{op.b[3], op.b[4], op.b[5]}))
			if !in {
				continue
			}
			for x := cb[0]; x <= cb[3]; x++ {
				for y := cb[1]; y <= cb[4]; y++ {
					for z := cb[2]; z <= cb[5]; z++ {
						switch op.kind {
						case omFill:
							w.set(x, y, z, op.rid, false)
						case omWater:
							if y < seaLevelField {
								w.set(x, y, z, waterRID(), false)
							} else {
								w.set(x, y, z, airRID(), false)
							}
						case omReplaceWater:
							if omWaterLike(w.blockName(x, y, z)) {
								w.set(x, y, z, op.rid, false)
							}
						}
					}
				}
			}
		case omColumn:
			x, y, z := r.f.world(op.b[0], op.b[1], op.b[2])
			if !w.in(x, y, z) {
				continue
			}
			for ; y >= w.lo+2; y-- {
				if !w.replaceable(x, y, z) && !omWaterLike(w.blockName(x, y, z)) {
					break
				}
				w.set(x, y, z, op.rid, false)
			}
		case omElder:
			x, y, z := r.f.world(op.b[0], op.b[1], op.b[2])
			if w.in(x, y, z) {
				w.spawns = append(w.spawns, spawnIntent{Pos: [3]int{x, y, z}, Entity: "minecraft:elder_guardian"})
			}
		case omSponges:
			for lx := op.b[0]; lx <= op.b[3]; lx++ {
				for lz := op.b[2]; lz <= op.b[5]; lz++ {
					if w.rng.NextIntN(3) == 0 {
						continue
					}
					low := w.rng.NextIntN(4) == 0
					for _, ly := range []int{op.b[4], op.b[1]} {
						if ly == op.b[1] && !low {
							continue
						}
						x, y, z := r.f.world(lx, ly, lz)
						if w.in(x, y, z) {
							w.set(x, y, z, op.rid, false)
						}
					}
				}
			}
		}
	}
}

func omWaterLike(name string) bool {
	switch name {
	case "water", "flowing_water", "seagrass", "kelp":
		return true
	}
	return false
}

// placeMonuments writes every monument that reaches the chunk: the building's list, then each room
// whose box touches the chunk, in child order.
func (s *Structures) placeMonuments(w *writer) {
	sp := spreads[Monument]
	const reach = 2
	for rx := floorDiv(w.cx-reach, sp.spacing); rx <= floorDiv(w.cx+reach, sp.spacing); rx++ {
		for rz := floorDiv(w.cz-reach, sp.spacing); rz <= floorDiv(w.cz+reach, sp.spacing); rz++ {
			cx, cz := sp.candidate(s, rx, rz, worldgen.NewBedrockRandom(0))
			if abs(cx-w.cx) > reach || abs(cz-w.cz) > reach {
				continue
			}
			if m := s.monumentAtStart(cx, cz); m.valid {
				s.monumentChunk(w, m)
			}
		}
	}
}

func (s *Structures) monumentChunk(w *writer, m *monument) {
	cb := w.chunkBox()
	if !m.building.b.intersects(cb) {
		return
	}
	omTables()
	(&omRun{w: w, f: m.building}).run(omBuilding)
	for i := range m.children {
		c := &m.children[i]
		if !c.b.intersects(cb) {
			continue
		}
		r := &omRun{w: w, f: opiece{c.b, m.building.o}, design: c.design}
		if a := c.a; a != nil {
			r.roles[omRoleA] = a
			r.roles[omRoleE], r.roles[omRoleN], r.roles[omRoleAU] = a.link[omE], a.link[omN], a.link[omU]
			if e := a.link[omE]; e != nil {
				r.roles[omRoleEU] = e.link[omU]
			}
			if n := a.link[omN]; n != nil {
				r.roles[omRoleNU] = n.link[omU]
			}
		}
		if c.kind == "simple" && c.design != 0 {
			draw := w.rng.NextBool()
			open := 0
			for _, v := range c.a.open {
				if v {
					open++
				}
			}
			r.pillar = draw && !c.a.open[omD] && !c.a.open[omU] && open > 1
		}
		r.run(omRooms[c.kind])
	}
}
