package structures

import (
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Ocean ruins, our own Go from NOTES-portals-fossils-ruins O1 and NOTES-structures-phase2 phase 9's
// start (cited O1). Read in part in the study; what was not read is labelled where it is used:
//   - the start (region random after its placement draws): rotation nextInt(4); big when nextFloat() <=
//     the large probability; the main piece; when big and nextFloat() <= the cluster probability, 8
//     candidate corners (x then z draws), n = 4 + nextInt(4) small pieces at corners taken by nextInt(len)
//     with their own rotation nextInt(4), kept when their box misses the main ruin's;
//   - a piece: warm (big_ruin_warm 4-7 / ruin_warm 1-8) or cold (big_ruin 1, 2, 3, 8 / ruin4, ruin1-8, as
//     three overlaid templates, brick, cracked at 0.7 and mossy at 0.5 integrity);
//   - post-processed in every chunk its box reaches: Java's height scan (O1, without the ice test), the
//     integrity draws on a fresh MT(the chunk's post-process seed) per template, one word per template
//     block that is not air nor a structure block, then the chest markers, each with the chunk's random's
//     nextInt() as its loot seed (the 20 seed-1 chests of phase 2 are draws 0..2 of their chunk's stream).
// Unread, labelled: the large and cluster probabilities (Java's 0.3 and 0.9 are used), the eighth cluster
// corner's z (taken as the third's), the per-rotation origin tables (the main piece takes the
// shipwreck's shift: the BDS ruin chests that any template explains fit it), the cluster box table, the
// warm or cold rule (the biome's warm or lukewarm tag at the start), the
// archaeology capped processor (suspicious sand and gravel are not made), the marker handler's special
// cases for ruin2_brick, ruin3_brick and the big brick ruins. Drowned: Bedrock's templates have none.
// The start's draws do not reproduce BDS's ruins (TestOceanRuinsMatchBDS: no chest at BDS's position;
// rotation, size and template of the measured ruins fit neither the region stream nor the start
// chunk's post-process stream as read): the ruins stand where BDS has one, with other layouts, as the
// villages did (ruling 42), listed in the known gaps.

const (
	oceanRuinReach   = 3 // Chunks a ruin's pieces reach from the start (the cluster corners lie within 24 blocks).
	ruinLargeChance  = 0.3
	ruinClusterProb  = 0.9
	ruinBigTable     = "loot_tables/chests/underwater_ruin_big.json"
	ruinSmallTable   = "loot_tables/chests/underwater_ruin_small.json"
	ruinCrackedInteg = 0.7
	ruinMossyInteg   = 0.5
)

// ruinPiece is one ocean ruin template.
type ruinPiece struct {
	t         *Template
	origin    [3]int // x, z of the template origin; y is worked out per chunk.
	rot       int
	integrity float32
	big       bool
}

func (p *ruinPiece) xzBox() box {
	q := rotate([3]int{p.t.Size[0] - 1, 0, p.t.Size[2] - 1}, p.rot)
	return box{min(p.origin[0], p.origin[0]+q[0]), minY, min(p.origin[2], p.origin[2]+q[2]),
		max(p.origin[0], p.origin[0]+q[0]), maxY, max(p.origin[2], p.origin[2]+q[2])}
}

var (
	ruinColdBig   = [4]string{"big_ruin1", "big_ruin2", "big_ruin3", "big_ruin8"}
	ruinColdSmall = [9]string{"ruin4", "ruin1", "ruin2", "ruin3", "ruin4", "ruin5", "ruin6", "ruin7", "ruin8"}
)

// oceanRuinAt assembles a start's pieces, nil if the chunk is not one.
func (s *Structures) oceanRuinAt(cx, cz int) []*ruinPiece {
	st, ok := s.StartIn(OceanRuin, cx, cz)
	if !ok || s.data == nil {
		return nil
	}
	r := st.random()
	x0, z0 := cx*16, cz*16
	b := s.g.BiomeID(x0+8, s.surface(cx, cz), z0+8)
	warm := s.biomeTag(b, "warm") || s.biomeTag(b, "lukewarm")
	var out []*ruinPiece
	add := func(x, z, rot int, big bool, integrity float32) []*ruinPiece {
		var names []string
		var integ []float32
		if warm {
			if big {
				names = []string{"big_ruin_warm" + strconv.Itoa(4+int(r.NextIntN(4)))}
			} else {
				names = []string{"ruin_warm" + strconv.Itoa(1+int(r.NextIntN(8)))}
			}
			integ = []float32{integrity}
		} else {
			var base string
			if big {
				base = ruinColdBig[r.NextIntN(4)]
			} else {
				i := r.NextIntN(9)
				base = ruinColdSmall[i]
				if i == 0 {
					names = []string{"ruin4", "ruin4", "ruin4"} // Bedrock's extra plain entry (O1).
				}
			}
			if names == nil {
				names = []string{base + "_brick", base + "_cracked", base + "_mossy"}
			}
			integ = []float32{integrity, ruinCrackedInteg, ruinMossyInteg}
		}
		var ps []*ruinPiece
		for i, n := range names {
			t := s.data.Template("ruin/" + n)
			if t == nil {
				s.data.missingTemplate("ocean ruin", "ruin/"+n)
				continue
			}
			ps = append(ps, &ruinPiece{t: t, origin: [3]int{x, 0, z}, rot: rot, integrity: integ[i], big: big})
		}
		out = append(out, ps...)
		return ps
	}
	rot := int(r.NextIntN(4))
	big := r.NextFloat() <= ruinLargeChance
	integrity := float32(0.8)
	if big {
		integrity = 0.9
	}
	sh := shipwreckShift[rot] // Measured: every placed BDS ruin chest fits the shipwreck's per-rotation shift.
	main := add(x0+sh[0], z0+sh[1], rot, big, integrity)
	if big && r.NextFloat() <= ruinClusterProb && len(main) > 0 {
		mb := main[0].xzBox()
		n := func(k int32) int { return int(r.NextIntN(k)) }
		var list [][2]int
		for _, c := range [8][4]int{{-15, 7, 17, 6}, {-15, 7, 1, 6}, {-15, 7, -12, 4}, {1, 6, 17, 6}, {1, 6, -12, 2},
			{17, 6, 19, 5}, {17, 6, 1, 6}, {17, 6, -12, 4}} {
			x := x0 + c[0] + n(int32(c[1]))
			z := z0 + c[2] + n(int32(c[3]))
			list = append(list, [2]int{x, z})
		}
		count := 4 + n(4)
		for range count {
			if len(list) == 0 {
				break
			}
			j := n(int32(len(list)))
			q := list[j]
			list = append(list[:j], list[j+1:]...)
			pr := int(r.NextIntN(4))
			d := rotate([3]int{5, 0, 6}, pr)
			qb := box{min(q[0], q[0]+d[0]), minY, min(q[1], q[1]+d[2]), max(q[0], q[0]+d[0]), maxY, max(q[1], q[1]+d[2])}
			if !qb.intersects(mb) {
				add(q[0], q[1], pr, false, 0.8)
			}
		}
	}
	return out
}

// ruinHeight is the piece's y in a chunk (O1's height scan on the terrain): the template origin's ocean
// floor, lowered to the lowest column under the footprint plus one when most columns lie 3 or more below.
func (s *Structures) ruinHeight(p *ruinPiece) int {
	py := s.g.NoiseHeight(p.origin[0], p.origin[2]) + 1
	q := rotate([3]int{p.t.Size[0] - 1, 0, p.t.Size[2] - 1}, p.rot)
	x0, x1 := min(p.origin[0], p.origin[0]+q[0]), max(p.origin[0], p.origin[0]+q[0])
	z0, z1 := min(p.origin[2], p.origin[2]+q[2]), max(p.origin[2], p.origin[2]+q[2])
	j, l := 512, 0
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			y := max(min(py-1, s.g.NoiseHeight(x, z)), minY+1)
			j = min(j, y)
			if y < py-3 {
				l++
			}
		}
	}
	ny := py
	if j-py < -3 && l > abs(q[0])-2 {
		ny = j + 1
	}
	return ny - 1
}

// placeOceanRuins post-processes every ruin piece that reaches the chunk.
func (s *Structures) placeOceanRuins(w *writer) {
	cb := w.chunkBox()
	for cx := w.cx - oceanRuinReach; cx <= w.cx+oceanRuinReach; cx++ {
		for cz := w.cz - oceanRuinReach; cz <= w.cz+oceanRuinReach; cz++ {
			if _, ok := s.StartIn(OceanRuin, cx, cz); !ok {
				continue
			}
			ps, _ := s.oceanRuins.get([2]int{cx, cz}, func() any { return s.oceanRuinAt(cx, cz) }).([]*ruinPiece)
			for _, p := range ps {
				if p.xzBox().intersects(cb) {
					s.ruinPost(w, p)
				}
			}
		}
	}
}

// ruinPost is one piece in one chunk.
func (s *Structures) ruinPost(w *writer, p *ruinPiece) {
	y0 := s.ruinHeight(p)
	l := worldgen.NewBedrockRandom(w.seed)
	type marker struct {
		pos  [3]int
		name string
	}
	var markers []marker
	for _, b := range p.t.blocks {
		st := p.t.palette[b.state]
		if st.name == "structure_block" {
			md, _ := b.nbt["metadata"].(string)
			markers = append(markers, marker{b.pos, md})
			continue
		}
		if st.name == "air" || st.name == "structure_void" {
			continue
		}
		if float64(l.U32())*(1.0/4294967296.0) > float64(p.integrity) {
			continue
		}
		q := rotate(b.pos, p.rot)
		x, y, z := p.origin[0]+q[0], y0+b.pos[1], p.origin[2]+q[2]
		if !w.in(x, y, z) {
			continue
		}
		res := convert.state(st, p.rot)
		if !res.ok {
			continue
		}
		w.set(x, y, z, res.rid, res.waterlog || y < seaLevelField && w.liquidAt(x, y, z))
	}
	for _, i := range markerOrder(len(markers), func(i int) [3]int { return markers[i].pos }) {
		m := markers[i]
		if !strings.EqualFold(m.name, "chest") {
			continue
		}
		q := rotate(m.pos, p.rot)
		x, y, z := p.origin[0]+q[0], y0+m.pos[1], p.origin[2]+q[2]
		if !w.in(x, y, z) {
			continue
		}
		table := ruinSmallTable
		if p.big {
			table = ruinBigTable
		}
		w.set(x, y, z, ridNamed("chest", nil), y < seaLevelField)
		w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{"id": "Chest", "LootTable": table,
			"LootTableSeed": w.rng.NextInt()}})
	}
}
