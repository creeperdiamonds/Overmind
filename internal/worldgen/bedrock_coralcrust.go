package worldgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// The warm oceans' coral crust (Task 9 round 18): minecraft:coral_crust_feature, CoralCrustFeature
// (0x5151db0), in our own Go from NOTES-leftovers L12 (cited L12), which validated its layout on
// warm_ocean_37 (53 of 57 BDS templates at the predicted template, rotation, x and z; 62 of 66 at the
// predicted y; the helper's first colour on 952 of 977 cells; 94 leading markers cell-exact in the
// libc++ map order). Its rule is BDS's brarchive copy (L12.1): y = query.above_top_solid, which the
// reader takes from the newest pack. The templates are the install's structures/coralcrust/crust1..5 and
// outcropping1..6 (Java NBT, read on first use); nothing of them is in this repo.
//
// Labelled:
//   - ASSUMED: mayPlace (BlockType vt+0x58) is "the target is water" (L12.3 did not read it; the
//     study's simulation used that, as T40 did).
//   - The y walk's "water" (place's walk and its test at the found y) is the block itself being water,
//     as L12.2 writes it ("block(center.x, y, center.z) is water"): seagrass and kelp, whose water
//     Bedrock keeps in the second layer, are not (round 19). Round 18 took the study's simulation class
//     (water or waterlogged, L12.5), which put a crust group of three pieces at source chunk (-3,-3) of
//     warm_ocean_37 on a tall seagrass where BDS has seagrass and no crust; at source (3,3) BDS too has
//     seagrass at the centre and no crust (ours has water there: our seagrass differs, so that group
//     stays). Not read in the code: the test's own function (BDS evidence: 2 of 2 seagrass centres).
//   - ASSUMED: "water" in the helper is a water block or a waterlogged one (the study's class, L12.5).
//   - ASSUMED: a setBlock BDS refuses is one our decoration may not write (bput's false); L12.3 did not
//     read when setBlock gives false.
//   - The templates' dead coral is one colour per template for a whole BDS run and changes between runs
//     (L12.4): ours is always dead tube coral.
//   - Not read (L12.5 open 3): which neighbour chunks BDS's decoration region lets a crust write; ours
//     writes what our decoration may (a crust reaches about 25 blocks from its origin).

// crustOX, crustOZ are the centre offsets by crust template (a0) and rotation (L12.2, tables
// 0x1547d78..0x1547dc0 and the inline rot 3).
var (
	crustOX = [5][4]int{{6, -7, -7, 6}, {6, -6, -7, 6}, {4, -4, -4, 4}, {4, -4, -4, 4}, {3, -7, -2, 7}}
	crustOZ = [5][4]int{{6, 6, -7, -7}, {6, 6, -7, -7}, {4, 4, -4, -4}, {4, 4, -4, -4}, {6, 3, -7, -3}}
	// crustOut is the outcropping offsets from the centre by template and rotation (L12.2, 0x5152e20).
	crustOut = [6][4][3]int{
		{{-5, 0, -2}, {3, 0, -5}, {5, 0, 2}, {-2, 0, 5}},
		{{-7, -2, -3}, {3, -2, -6}, {6, -2, 3}, {-2, -2, 5}},
		{{-4, 0, -3}, {3, 0, -4}, {4, 0, 3}, {-3, 0, 4}},
		{{-3, 0, -3}, {3, 0, -3}, {3, 0, 3}, {-3, 0, 3}},
		{{-2, 0, -2}, {2, 0, -2}, {2, 0, 2}, {-2, 0, 2}},
		{{-5, 0, -5}, {6, 0, -6}, {5, 0, 5}, {-5, 0, 5}},
	}
	// crustWallSide is the side a wall fan is tried on, by the walk's face and a draw (0xf75f6e8).
	crustWallSide = map[int][3]int{2: {5, 3, 4}, 3: {5, 2, 4}, 4: {5, 3, 2}, 5: {2, 3, 4}}
	// crustSideStep is a side's step (0x18c8138): 2 -z, 3 +z, 4 -x, 5 +x; crustSideFacing its wall fan's
	// facing (coral_direction [2,3,0,1][side-2]: north, south, west, east).
	crustSideStep   = map[int][2]int{2: {0, -1}, 3: {0, 1}, 4: {-1, 0}, 5: {1, 0}}
	crustSideFacing = map[int]string{2: "north", 3: "south", 4: "west", 5: "east"}
	coralColours    = [5]string{"tube", "brain", "bubble", "fire", "horn"}
)

// crustTemplate is a coral crust template: its blocks in NBT order (markers left out) and its data
// markers in NBT order.
type crustTemplate struct {
	blocks  []crustBlock
	markers [][3]int
}

type crustBlock struct {
	pos [3]int
	s   bs
}

// crustStates are the crust's blocks: the live coral blocks (tube to horn), their floor fans of both
// directions, wall fans by facing, and the list of the ten coral blocks.
type crustStates struct {
	coral         [5]bs
	fans          [2][5]bs
	wall          map[string][5]bs
	coral10, live bnameSet // The ten coral blocks (0xff0c210); the five live ones (0xff12208).
}

var crustOnce = sync.OnceValue(func() *crustStates {
	c := &crustStates{wall: map[string][5]bs{}}
	var names, live []string
	for i, k := range coralColours {
		c.coral[i] = stateOf(k + "_coral_block")
		c.fans[0][i] = stateOf(k + "_coral_fan[waterlogged=true]")
		c.fans[1][i] = stateOf(k + "_coral_fan[coral_fan_direction=1,waterlogged=true]")
		names = append(names, k+"_coral_block", "dead_"+k+"_coral_block")
		live = append(live, k+"_coral_block")
	}
	for _, f := range cardinals {
		var w [5]bs
		for i, k := range coralColours {
			w[i] = stateOf(k + "_coral_wall_fan[facing=" + f + ",waterlogged=true]")
		}
		c.wall[f] = w
	}
	c.coral10, c.live = bnameSetOf(strings.Join(names, " ")), bnameSetOf(strings.Join(live, " "))
	return c
})

// crustData is the eleven templates, read once (nil if one is missing: the feature then places none).
type crustData struct {
	once  sync.Once
	crust [5]*crustTemplate
	out   [6]*crustTemplate
	err   error
}

func (bd *BedrockData) crustTemplates() (*crustData, error) {
	cd := &bd.crust
	cd.once.Do(func() {
		for i := range cd.crust {
			if cd.crust[i], cd.err = bd.readCrust("crust" + strconv.Itoa(i+1)); cd.err != nil {
				return
			}
		}
		for i := range cd.out {
			if cd.out[i], cd.err = bd.readCrust("outcropping" + strconv.Itoa(i+1)); cd.err != nil {
				return
			}
		}
	})
	return cd, cd.err
}

// readCrust reads a template from the newest pack that has it: stone's variants are andesite, diorite
// and granite, sand is sand, the dead coral block dead tube coral (see the file's comment), and a
// structure block in data mode a marker (its metadata ignored, L12.3); other structure blocks place
// nothing.
func (bd *BedrockData) readCrust(name string) (*crustTemplate, error) {
	packs, err := VanillaPacks(bd.Dir)
	if err != nil {
		return nil, err
	}
	for _, pack := range packs {
		file := filepath.Join(pack, "structures", "coralcrust", name+".nbt")
		if _, err := os.Stat(file); err != nil {
			continue
		}
		jt, err := ReadJavaTemplate(file)
		if err != nil {
			return nil, err
		}
		t := &crustTemplate{}
		for _, b := range jt.Blocks {
			st := jt.Palette[b.State]
			var s bs
			switch st.Name {
			case "structure_block":
				if mode, _ := b.NBT["mode"].(string); strings.EqualFold(mode, "data") {
					t.markers = append(t.markers, b.Pos)
				}
				continue
			case "stone":
				switch v := st.Props["variant"]; v {
				case "andesite", "diorite", "granite":
					s = stateOf(v)
				default:
					return nil, fmt.Errorf("%s: stone variant %q", file, v)
				}
			case "sand":
				s = stateOf("sand")
			case "coral_block":
				s = stateOf("dead_tube_coral_block")
			default:
				return nil, fmt.Errorf("%s: block %s", file, st.Name)
			}
			t.blocks = append(t.blocks, crustBlock{b.Pos, s})
		}
		return t, nil
	}
	return nil, fmt.Errorf("no template file for coralcrust/%s", name)
}

// coralCrust is the feature; templates are read on its first placement.
type coralCrust struct{ bd *BedrockData }

func (fc *featCompiler) coralCrustFeature() (bfeat, error) {
	if _, err := fc.bd.crustTemplates(); err != nil {
		return nil, err
	}
	return codeFeat((&coralCrust{fc.bd}).place), nil
}

// crustWater is the "water" of the helper (ASSUMED, see the file's comment).
func crustWater(s bs) bool { return bWaterMat(s) || s.has(fWater) }

// crustWalkWater is the "water" of place's y walk and its test at the found y: the block itself is water,
// so seagrass and kelp (whose water is in the second layer) are not (round 19; see the file's comment).
func crustWalkWater(s bs) bool { return bWaterMat(s) }

type crustPiece struct {
	t      *crustTemplate
	rot    int
	corner pos
}

// place is CoralCrustFeature::place (L12.2) on the rule's second random.
func (f *coralCrust) place(d *decoration, r *bmt, o pos) bool {
	cd, err := f.bd.crustTemplates()
	if err != nil {
		return false
	}
	cs := crustOnce()
	a0, r0 := int(r.nextIntN(5)), int(r.nextIntN(4))
	cx, cz := o.x+crustOX[a0][r0], o.z+crustOZ[a0][r0]
	y := o.y
	for ; y < 54; y++ { // The dimension's sea level 63 - 9.
		if crustWalkWater(d.block(cx, y, cz)) {
			break
		}
	}
	if y >= 54 || !bSolid(d.block(cx, y-1, cz)) || !crustWalkWater(d.block(cx, y, cz)) || cs.live.has(bname(d.block(cx, y-1, cz))) {
		return false
	}
	outcrop := func() {
		i := int(r.nextIntN(6))
		q := crustOut[i][r0]
		f.placePiece(d, r, cs, crustPiece{cd.out[i], r0, pos{cx + q[0], y + q[1], cz + q[2]}})
	}
	if r.nextIntN(4) == 0 {
		outcrop()
		return true
	}
	pieces := []crustPiece{{cd.crust[a0], r0, pos{o.x, y, o.z}}}
	n := int(r.nextIntN(5))
	for range n + 4 {
		a, rr := int(r.nextIntN(5)), int(r.nextIntN(4))
		d1, s1 := int(r.nextIntN(9)), r.nextIntN(2)
		dx := -3 - d1
		if s1 != 0 {
			dx = d1 + 3
		}
		d2, s2 := int(r.nextIntN(9)), r.nextIntN(2)
		dz := -3 - d2
		if s2 != 0 {
			dz = d2 + 3
		}
		if bSolid(d.block(cx+dx+crustOX[a][rr], y-1, cz+dz+crustOZ[a][rr])) {
			pieces = append(pieces, crustPiece{cd.crust[a], rr, pos{cx + dx, y, cz + dz}})
		}
	}
	if len(pieces) == 1 {
		outcrop() // The first crust is not placed.
		return true
	}
	for _, p := range pieces {
		f.placePiece(d, r, cs, p)
	}
	return true
}

// placePiece writes a template (NBT order, integrity 1, markers not written) and runs the helper.
func (f *coralCrust) placePiece(d *decoration, r *bmt, cs *crustStates, p crustPiece) {
	var trace []pos
	for _, b := range p.t.blocks {
		u, w := turn(b.pos[0], b.pos[2], p.rot)
		q := pos{p.corner.x + u, p.corner.y + b.pos[1], p.corner.z + w}
		if d.bput(q, b.s) && crustTrace != nil {
			trace = append(trace, q)
		}
	}
	if crustTrace != nil {
		crustTrace(d.cx, d.cz, p, trace)
	}
	f.helper(d, r, cs, p)
}

// crustMarkerHash is std::hash<BlockPos> (0xd8689b3): boost's hash_combine over the sign-extended
// coordinates, 64 bits.
func crustMarkerHash(p pos) uint64 {
	h := uint64(int64(p.x)) + 0x9e3779b9
	h ^= uint64(int64(p.y)) + 0x9e3779b9 + h<<6 + h>>2
	h ^= uint64(int64(p.z)) + 0x9e3779b9 + h<<6 + h>>2
	return h
}

// helper is the post-place helper 0x5152f60 (L12.3): live coral blobs and walks at the markers in the
// libc++ unordered_map's order, then floor fans and wall fans.
func (f *coralCrust) helper(d *decoration, r *bmt, cs *crustStates, p crustPiece) {
	m := &libcxxOrder{}
	at := map[string]pos{}
	for _, mk := range p.t.markers {
		u, w := turn(mk[0], mk[2], p.rot)
		q := pos{p.corner.x + u, p.corner.y + mk[1], p.corner.z + w}
		k := fmt.Sprint(q.x, q.y, q.z)
		at[k] = q
		m.insert(k, crustMarkerHash(q))
	}
	c := int(r.nextIntN(5))
	coral := cs.coral[c]
	isCoral := func(q pos) bool { return cs.coral10.has(bname(d.at(q))) }
	mayPlace := func(q pos) bool { return crustWater(d.at(q)) } // ASSUMED (vt+0x58 not read).
	type face struct {
		q pos
		f int
	}
	var la []pos
	var lb []face
	for _, k := range m.list {
		q := at[k]
		if !bSolid(d.at(q.add(0, -1, 0))) {
			continue
		}
		if bSolid(d.at(q)) {
			q.y++
		}
		if r.nextIntN(4) == 0 {
			if !mayPlace(q) {
				continue
			}
			for _, s := range [4][2]int{{1, 0}, {0, -1}, {-1, 0}, {0, 1}} {
				if n := q.add(s[0], 0, s[1]); !isCoral(n) {
					d.bput(n, coral)
				}
			}
			d.bput(q, coral)
			if !d.bput(q.add(0, 1, 0), coral) {
				continue
			}
			if r.nextIntN(4) == 0 {
				la = append(la, q.add(0, 2, 0))
			}
			continue
		}
		n := int(r.nextIntN(3))
		if !crustWater(d.at(q)) {
			continue
		}
		d.bput(q, coral)
		dir := r.nextIntN(2)
		w := q
		for range n + 1 {
			s := r.nextIntN(2)
			var fc int
			switch {
			case dir == 0 && s == 0:
				w.x, fc = w.x+1, 4
			case dir == 0:
				w.x, fc = w.x-1, 5
			case s == 0:
				w.z, fc = w.z-1, 3
			default:
				w.z, fc = w.z+1, 2
			}
			if !mayPlace(w) || isCoral(w) || !d.bput(w, coral) {
				break
			}
			if r.nextIntN(4) == 0 {
				la = append(la, w.add(0, 1, 0))
			}
			if r.nextIntN(2) == 0 {
				lb = append(lb, face{w, fc})
			}
			if r.nextIntN(2) == 0 {
				lb = append(lb, face{w, fc})
			}
			if r.nextIntN(3) == 0 {
				lb = append(lb, face{w, fc})
			}
			dir ^= 1
		}
	}
	for _, q := range la {
		// tube coral's canSurvive (T48) taken as a solid block under it, as the study's simulation.
		if below := d.at(q.add(0, -1, 0)); crustWater(d.at(q)) && !crustWater(below) && bSolid(below) {
			i, dir := r.nextIntN(5), r.nextIntN(2)
			d.bput(q, cs.fans[dir][i])
		}
	}
	for _, e := range lb {
		isC := isCoral(e.q) || bname(d.at(e.q)) == bnStone
		side := crustWallSide[e.f][r.nextIntN(3)] // Drawn whatever follows.
		st := crustSideStep[side]
		q := e.q.add(st[0], 0, st[1])
		if crustWater(d.at(q)) && isC && mayPlace(q) {
			u := min(int(r.nextIntN(2))+2*int(r.nextIntN(3)), 4)
			d.bput(q, cs.wall[crustSideFacing[side]][u])
		}
	}
}

var bnStone = internBName("stone")

// crustTrace, when set (tests only), sees each crust piece placed by source chunk with the cells it wrote.
var crustTrace func(cx, cz int, p crustPiece, cells []pos)
