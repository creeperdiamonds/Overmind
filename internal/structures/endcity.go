package structures

import (
	"strings"

	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// End cities (Task 11), our own Go of NOTES-structures-phase2 P18.1 to P18.3 (cited P181..P183) with the
// start of NOTES-bedrock-end phase 8. Validated in the study on seed 100's (5,65) city: 119 of 119 pieces,
// 100% of 35,417 cells, 7 of 7 chest loot seeds. The pieces are the install's endcity templates.
// Shulkers are mobs (stage 4): their Sentry markers become spawn intents, which are dropped (place.go).
//
// Bedrock's changes from the reference model: the pieces continue the region placement's random after
// its four nextInt(9) words (a local MT only gives the rotation), and the collision box is one block
// longer in x and z (P181).

const (
	// endCityReach is how far (chunks) a piece reaches from its start. The placer has no distance limit,
	// only its depth (8), so this is measured, not read: of 19,200 starts over 12 seeds the farthest piece
	// was 10 chunks out (20 starts; 114 at 9), and 2 more are kept as a margin (TestPiecesStayWithinReach
	// checks 1,944 starts).
	endCityReach   = 12
	endCityMinY    = 60 // P183: below this no city.
	endCityMinDist = 4096
	endMinY        = 0
	endMaxY        = 255
)

// ecPiece is one end city piece.
type ecPiece struct {
	t         *Template
	name      string
	pos       [3]int
	rot       int
	overwrite bool
	tag       int32
	box       box // The collision box (P181 item 2), also the chunk test.
}

type endCity struct {
	pieces []*ecPiece
}

type ecAssembler struct {
	s    *Structures
	r    *worldgen.BedrockRandom
	ship bool
	ok   bool // Every template was found.
}

// endCityAt is the city of a start chunk, nil when the chunk is not one (P183 start).
func (s *Structures) endCityAt(cx, cz int) *endCity {
	sp := spreads[EndCity]
	r := worldgen.NewBedrockRandom(0)
	x, z := sp.candidate(s, floorDiv(cx, sp.spacing), floorDiv(cz, sp.spacing), r)
	if x != cx || z != cz || int64(cx)*int64(cx)+int64(cz)*int64(cz) <= endCityMinDist {
		return nil
	}
	rot := endCityRotation(cx, cz)
	o1, o2 := [4]int{12, 12, 2, 2}[rot], [4]int{12, 2, 2, 12}[rot]
	hs := s.g.EndTerrainHeights(cx, cz, [][2]int{{7, 7}, {7, o1}, {o2, 7}, {o2, o1}})
	h := min(hs[0], hs[1], hs[2], hs[3])
	if h < endCityMinY {
		return nil
	}
	return s.assembleEndCity(cx, cz, rot, h, r)
}

// assembleEndCity builds a city at start chunk (cx, cz) and height h on the region random r (left after
// the placement draws), nil when a template is missing.
func (s *Structures) assembleEndCity(cx, cz, rot, h int, r *worldgen.BedrockRandom) *endCity {
	as := &ecAssembler{s: s, r: r, ok: true}
	base := as.piece("base_floor", [3]int{cx*16 + 8, h, cz*16 + 8}, rot, true)
	second := as.child(base, "second_floor", [3]int{-1, 0, -1}, rot, false)
	third := as.child(second, "third_floor", [3]int{-1, 4, -1}, rot, false)
	roof := as.child(third, "third_roof", [3]int{-1, 8, -1}, rot, true)
	list := []*ecPiece{base, second, third, roof}
	as.add(as.tower, 1, roof, [3]int{}, &list)
	if !as.ok {
		return nil
	}
	return &endCity{pieces: list}
}

// endCityRotation is a start's rotation, from a local MT (P181).
func endCityRotation(cx, cz int) int {
	return int(worldgen.NewBedrockRandom(uint32(int32(cz))*10387313+uint32(int32(cx))).U32() & 3)
}

// piece is a template piece at a position (P183) with its collision box (P181 item 2).
func (as *ecAssembler) piece(name string, pos [3]int, rot int, overwrite bool) *ecPiece {
	t := as.s.data.Template("endcity/" + name)
	p := &ecPiece{t: t, name: name, pos: pos, rot: rot & 3, overwrite: overwrite}
	if t == nil {
		as.ok = false
		as.s.data.missingTemplate("end city", "endcity/"+name)
		p.box = box{pos[0], pos[1], pos[2], pos[0], pos[1], pos[2]}
		return p
	}
	X, Y, Z := t.Size[0], t.Size[1], t.Size[2]
	if p.rot&1 == 1 {
		X, Z = Z, X
	}
	x, y, z := pos[0], pos[1], pos[2]
	switch p.rot {
	case 0:
		p.box = box{x, y, z, x + X, y + Y - 1, z + Z}
	case 1:
		p.box = box{x - X, y, z, x, y + Y - 1, z + Z}
	case 2:
		p.box = box{x - X, y, z - Z, x, y + Y - 1, z}
	default:
		p.box = box{x, y, z - Z, x + X, y + Y - 1, z}
	}
	return p
}

// child is a piece at offset o from a parent: the parent's origin plus o turned by the parent's
// rotation (P183).
func (as *ecAssembler) child(parent *ecPiece, name string, o [3]int, rot int, overwrite bool) *ecPiece {
	q := rotate(o, parent.rot)
	return as.piece(name, [3]int{parent.pos[0] + q[0], parent.pos[1] + q[1], parent.pos[2] + q[2]}, rot, overwrite)
}

type ecGen func(depth int, parent *ecPiece, off [3]int, out *[]*ecPiece) bool

// add is recursiveChildren (P181 item 3, P183): the generator's children, one tag for all of them, and
// the first intersecting piece of the list deciding (another tag than the parent's drops them all).
func (as *ecAssembler) add(gen ecGen, depth int, parent *ecPiece, off [3]int, list *[]*ecPiece) bool {
	if depth > 8 {
		return false
	}
	var children []*ecPiece
	if !gen(depth, parent, off, &children) {
		return false
	}
	tag := as.r.NextInt()
	for _, c := range children {
		c.tag = tag
	}
	for _, c := range children {
		for _, p := range *list {
			if p.box.intersects(c.box) {
				if p.tag != parent.tag {
					return false
				}
				break
			}
		}
	}
	*list = append(*list, children...)
	return true
}

func (as *ecAssembler) tower(depth int, parent *ecPiece, _ [3]int, out *[]*ecPiece) bool {
	r := as.r
	rot := parent.rot
	ox, oz := int(r.NextIntN(2)), int(r.NextIntN(2))
	p := as.child(parent, "tower_base", [3]int{3 + ox, -3, 3 + oz}, rot, true)
	*out = append(*out, p)
	p = as.child(p, "tower_piece", [3]int{0, 7, 0}, rot, true)
	*out = append(*out, p)
	var floor *ecPiece
	if r.NextIntN(3) == 0 {
		floor = p
	}
	h := 1 + int(r.NextIntN(3))
	for i := range h {
		p = as.child(p, "tower_piece", [3]int{0, 4, 0}, rot, true)
		*out = append(*out, p)
		if i < h-1 && r.NextBool() {
			floor = p
		}
	}
	if floor != nil {
		for _, b := range [4]struct {
			rot int
			o   [3]int
		}{{0, [3]int{1, -1, 0}}, {1, [3]int{6, -1, 1}}, {3, [3]int{0, -1, 5}}, {2, [3]int{5, -1, 6}}} {
			if r.NextBool() {
				end := as.child(floor, "bridge_end", b.o, rot+b.rot, true)
				*out = append(*out, end)
				as.add(as.bridge, depth+1, end, [3]int{}, out)
			}
		}
	} else if depth != 7 {
		return as.add(as.fat, depth+1, p, [3]int{}, out)
	}
	*out = append(*out, as.child(p, "tower_top", [3]int{-1, 4, -1}, rot, true))
	return true
}

func (as *ecAssembler) fat(depth int, parent *ecPiece, _ [3]int, out *[]*ecPiece) bool {
	r := as.r
	rot := parent.rot
	p := as.child(parent, "fat_tower_base", [3]int{-3, 4, -3}, rot, true)
	*out = append(*out, p)
	p = as.child(p, "fat_tower_middle", [3]int{0, 4, 0}, rot, true)
	*out = append(*out, p)
	for i := 0; i < 2 && r.NextIntN(3) != 0; i++ {
		p = as.child(p, "fat_tower_middle", [3]int{0, 8, 0}, rot, true)
		*out = append(*out, p)
		for _, b := range [4]struct {
			rot int
			o   [3]int
		}{{0, [3]int{4, -1, 0}}, {1, [3]int{12, -1, 4}}, {3, [3]int{0, -1, 8}}, {2, [3]int{8, -1, 12}}} {
			if r.NextBool() {
				end := as.child(p, "bridge_end", b.o, rot+b.rot, true)
				*out = append(*out, end)
				as.add(as.bridge, depth+1, end, [3]int{}, out)
			}
		}
	}
	*out = append(*out, as.child(p, "fat_tower_top", [3]int{-2, 8, -2}, rot, true))
	return true
}

func (as *ecAssembler) bridge(depth int, parent *ecPiece, _ [3]int, out *[]*ecPiece) bool {
	r := as.r
	rot := parent.rot
	n := int(r.NextIntN(4)) + 1
	p := as.child(parent, "bridge_piece", [3]int{0, 0, -4}, rot, true)
	p.tag = -1
	*out = append(*out, p)
	dy := 0
	for range n {
		if r.NextBool() {
			p = as.child(p, "bridge_piece", [3]int{0, dy, -4}, rot, true)
			dy = 0
		} else {
			if r.NextBool() {
				p = as.child(p, "bridge_steep_stairs", [3]int{0, dy, -4}, rot, true)
			} else {
				p = as.child(p, "bridge_gentle_stairs", [3]int{0, dy, -8}, rot, true)
			}
			dy = 4
		}
		*out = append(*out, p)
	}
	if !as.ship && r.NextIntN(int32(10-depth)) == 0 {
		x := -8 + int(r.NextIntN(8))
		z := -70 + int(r.NextIntN(10))
		*out = append(*out, as.child(p, "ship", [3]int{x, dy, z}, rot, true))
		as.ship = true
	} else if !as.add(as.house, depth+1, p, [3]int{-3, dy + 1, -11}, out) {
		return false
	}
	*out = append(*out, as.child(p, "bridge_end", [3]int{4, dy, 0}, rot+2, true))
	return true
}

func (as *ecAssembler) house(depth int, parent *ecPiece, off [3]int, out *[]*ecPiece) bool {
	if depth > 8 {
		return false
	}
	rot := parent.rot
	p := as.child(parent, "base_floor", off, rot, true)
	*out = append(*out, p)
	switch as.r.NextIntN(3) {
	case 0:
		*out = append(*out, as.child(p, "base_roof", [3]int{-1, 4, -1}, rot, true))
	case 1:
		p = as.child(p, "second_floor_2", [3]int{-1, 0, -1}, rot, false)
		*out = append(*out, p)
		p = as.child(p, "second_roof", [3]int{-1, 8, -1}, rot, false)
		*out = append(*out, p)
		as.add(as.tower, depth+1, p, [3]int{}, out)
	default:
		p = as.child(p, "second_floor_2", [3]int{-1, 0, -1}, rot, false)
		*out = append(*out, p)
		p = as.child(p, "third_floor_c", [3]int{-1, 4, -1}, rot, false)
		*out = append(*out, p)
		p = as.child(p, "third_roof", [3]int{-1, 8, -1}, rot, true)
		*out = append(*out, p)
		as.add(as.tower, depth+1, p, [3]int{}, out)
	}
	return true
}

// placeEndCities writes the end city pieces that reach the chunk.
func (s *Structures) placeEndCities(w *writer) {
	sp := spreads[EndCity]
	cb := w.chunkBox()
	for rx := floorDiv(w.cx-endCityReach, sp.spacing); rx <= floorDiv(w.cx+endCityReach, sp.spacing); rx++ {
		for rz := floorDiv(w.cz-endCityReach, sp.spacing); rz <= floorDiv(w.cz+endCityReach, sp.spacing); rz++ {
			x, z := sp.candidate(s, rx, rz, worldgen.NewBedrockRandom(0))
			if abs(x-w.cx) > endCityReach || abs(z-w.cz) > endCityReach {
				continue
			}
			c := s.endCities.get([2]int{x, z}, func() any { return s.endCityAt(x, z) }).(*endCity)
			if c == nil {
				continue
			}
			w.reseed() // P17.2: the end city type's own post-process random.
			for _, p := range c.pieces {
				if p.t != nil && p.box.intersects(cb) {
					s.ecPost(w, p)
				}
			}
		}
	}
}

// ecPost is one piece in one chunk (P182): the template (each chest placed in the chunk draws a word),
// then its data markers in the order of Bedrock's unordered_map of template positions.
func (s *Structures) ecPost(w *writer, p *ecPiece) {
	t := p.t
	type marker struct {
		pos  [3]int
		name string
	}
	var markers []marker
	for _, b := range t.blocks {
		st := t.palette[b.state]
		if st.name == "structure_block" {
			md, _ := b.nbt["metadata"].(string)
			markers = append(markers, marker{b.pos, md})
			continue
		}
		if !p.overwrite && st.name == "air" {
			continue
		}
		switch st.name { // Template names Bedrock maps otherwise (P182).
		case "stained_glass":
			st = tState{name: "magenta_stained_glass", props: st.props}
		case "skull":
			st = tState{name: "dragon_head", props: st.props}
		}
		q := rotate(b.pos, p.rot)
		x, y, z := p.pos[0]+q[0], p.pos[1]+q[1], p.pos[2]+q[2]
		if !w.in(x, y, z) {
			continue
		}
		res := convert.state(st, p.rot)
		if !res.ok {
			continue
		}
		w.set(x, y, z, res.rid, res.waterlog)
		if res.name == "chest" {
			w.rng.U32() // Its first loot seed, which the Chest marker replaces (P182 step 1).
		}
	}
	order := markerOrder(len(markers), func(i int) [3]int { return markers[i].pos })
	for _, i := range order {
		m := markers[i]
		q := rotate(m.pos, p.rot)
		x, y, z := p.pos[0]+q[0], p.pos[1]+q[1], p.pos[2]+q[2]
		switch {
		case strings.HasPrefix(m.name, "Chest"):
			if w.in(x, y-1, z) {
				w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{x, y - 1, z}, Data: map[string]any{
					"id": "Chest", "LootTable": "loot_tables/chests/end_city_treasure.json", "LootTableSeed": w.rng.NextInt()}})
			}
		case strings.HasPrefix(m.name, "Sentry"):
			if w.in(x, y, z) {
				w.spawns = append(w.spawns, spawnIntent{Pos: [3]int{x, y, z}, Entity: "minecraft:shulker"})
			}
		case strings.HasPrefix(m.name, "Elytra"):
			if w.in(x, y, z) {
				w.set(x, y, z, ridNamed("frame", map[string]any{"facing_direction": int32([4]int{3, 4, 2, 5}[p.rot])}), false)
				w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{
					"id": "ItemFrame", "Item": map[string]any{"Name": "minecraft:elytra", "Count": byte(1), "Damage": int16(0)}}})
			}
		}
	}
}

// markerOrder is the iteration order of libc++'s unordered_map<BlockPos, string> after inserting n keys
// in order (P182 step 2): the hash of the template position, buckets 2, 5, 11, 23, ..., a key into an
// empty bucket going to the front of the list, into a used one to the front of that bucket's run, and
// the list regrouped bucket by bucket on growth.
func markerOrder(n int, key func(int) [3]int) []int {
	hash := func(p [3]int) uint64 {
		h := uint64(int64(p[0])) + 0x9e3779b9
		h = (uint64(int64(p[1])) + 0x9e3779b9 + h<<6 + h>>2) ^ h
		h = (uint64(int64(p[2])) + 0x9e3779b9 + h<<6 + h>>2) ^ h
		return h
	}
	bucketOf := func(h uint64, bc int) int {
		if bc&(bc-1) == 0 {
			return int(h & uint64(bc-1))
		}
		return int(h % uint64(bc))
	}
	nextPrime := func(v int) int {
		for ; ; v++ {
			prime := v >= 2
			for d := 2; d*d <= v && prime; d++ {
				prime = v%d != 0
			}
			if prime {
				return v
			}
		}
	}
	var list []int // Keys in list order.
	hashes := make([]uint64, n)
	bc := 0
	for i := range n {
		hashes[i] = hash(key(i))
		if len(list)+1 > bc {
			if bc == 0 {
				bc = 2
			} else {
				bc = nextPrime(max(2*bc+1, len(list)+1))
			}
			// Regroup (libc++'s __rehash): walk the list; a node in the same bucket as the one before it
			// stays; one whose bucket already has a run moves to that run's front; else it starts a run.
			var out []int
			starts := map[int]int{} // Bucket to its run's index in out.
			for _, k := range list {
				b := bucketOf(hashes[k], bc)
				if len(out) > 0 && bucketOf(hashes[out[len(out)-1]], bc) == b {
					out = append(out, k)
				} else if at, ok := starts[b]; ok {
					out = append(out[:at], append([]int{k}, out[at:]...)...)
					for bb, a := range starts {
						if a > at || a == at && bb != b {
							starts[bb] = a + 1
						}
					}
				} else {
					starts[b] = len(out)
					out = append(out, k)
				}
			}
			list = out
		}
		b := bucketOf(hashes[i], bc)
		at := -1
		for j, k := range list {
			if bucketOf(hashes[k], bc) == b {
				at = j
				break
			}
		}
		if at < 0 {
			list = append([]int{i}, list...)
		} else {
			list = append(list[:at], append([]int{i}, list[at:]...)...)
		}
	}
	return list
}
