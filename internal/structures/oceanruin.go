package structures

import (
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Ocean ruins, our own Go from NOTES-portals-fossils-ruins O1 and NOTES-leftovers L1 (cited L1), which
// read the start and validated it: 17 of the 20 measured ruin chests at the predicted x, z with the
// right table, the 3 others explained by the deep oceans' large probability (L1.3).
//   - The start random (L1.1): the ruin's isFeatureChunk runs the monument test for x = cx-5 .. cx+4 and
//     z = cz-5 .. cz+4, which reseeds the same random with each monument region's seed; the last call is
//     (cx+4, cz+4), so the start draws from MT(the monument region seed of (floor((cx+4)/32),
//     floor((cz+4)/32))) after its 4 placement words.
//   - createStart (L1.2): rotation nextInt(4); origin (cx*16, cz*16) + ([0,15,15,0], [0,0,15,15])[rot];
//     big when nextFloat() <= the biome's large probability; the main piece; when big and nextFloat() <=
//     the cluster probability, 8 corners around the start chunk (x then z draws), 4 + nextInt(4) small
//     pieces at corners taken by nextInt(len) with their rotation nextInt(4), placed at the corner itself
//     when their 6 x 7 box misses the start chunk.
//   - a piece draws one word (L1.4): warm big_ruin_warm 4-7 / ruin_warm 1-8, cold big_ruin 1, 2, 3, 8 /
//     ruin4, ruin1-8 as three overlaid templates (brick, cracked 0.7, mossy 0.5 integrity; the plain
//     ruin4 alone).
//   - post-processed in every chunk its box reaches: Java's height scan (O1, without the ice test; not
//     re-checked in L1), the integrity draws on a fresh MT(the chunk's post-process seed) per template,
//     the archaeology replacement (L1.5), then the chest markers (L1.4) in Bedrock's map order: the marker
//     cell becomes water (sandstone in warm ruins; stone bricks in ruin3_brick, gravel in ruin2_brick and
//     the big brick ruins), the chest goes one below it facing east, waterlogged when the marker is at or
//     below the sea level field, its loot seed the chunk's random's nextInt().
// Labelled where used: the deep oceans' large probability (fitted), the cluster probability and the
// ocean's large one (Java's, which the measured starts fit), which biomes are warm (Java's split), and the
// archaeology's random (not traced). Drowned: Bedrock's templates have none.

const (
	oceanRuinReach   = 3 // Chunks a ruin's pieces reach from the start (the cluster corners lie within 24 blocks).
	ruinBigTable     = "loot_tables/chests/underwater_ruin_big.json"
	ruinSmallTable   = "loot_tables/chests/underwater_ruin_small.json"
	ruinCrackedInteg = 0.7
	ruinMossyInteg   = 0.5
	// ruinLargeChance is the large probability of every ocean but the deep ones: Java's 0.3, which the
	// measured ocean starts fit (L1.3: big at f 0.1373 and 0.0776, small at 0.3181); Bedrock's value is
	// copied from biome data whose source was not found.
	ruinLargeChance = 0.3
	// ruinDeepLargeChance is the deep oceans' large probability. Fitted (ruling 41): L1.3 bounds it to
	// [0.3366, 0.6209) from seed 1's three misses (deep_ocean, deep_ocean and deep_cold_ocean starts with f
	// 0.3181, 0.3366 and 0.3366 that BDS made big); 0.5 is inside. The other deep oceans are assumed to
	// share it. It may miss on unseen seeds.
	ruinDeepLargeChance = 0.5
	ruinClusterProb     = 0.9 // Java's; every measured big start that clusters fits it (L1.3), not read.
	// ruinArchaeologyLimit is the capped archaeology processor's limit, both types (L1.5, read).
	ruinArchaeologyLimit = 5
)

// ruinWarmBiomes are the oceans whose ruins are warm: Java's split (L1.3: the type is the biome's
// configuration, its source not found; every measured ruin is cold, in ocean, cold_ocean, deep_ocean or
// deep_cold_ocean, and no warm ruin was measured).
var ruinWarmBiomes = map[string]bool{"warm_ocean": true, "lukewarm_ocean": true, "deep_lukewarm_ocean": true, "deep_warm_ocean": true}

// ruinPiece is one ocean ruin template.
type ruinPiece struct {
	t         *Template
	name      string // The template's name under ruin/.
	origin    [3]int // x, z of the template origin; y is worked out per chunk.
	rot       int
	integrity float32
	big, warm bool
}

func (p *ruinPiece) xzBox() box {
	q := rotate([3]int{p.t.Size[0] - 1, 0, p.t.Size[2] - 1}, p.rot)
	return box{min(p.origin[0], p.origin[0]+q[0]), minY, min(p.origin[2], p.origin[2]+q[2]),
		max(p.origin[0], p.origin[0]+q[0]), maxY, max(p.origin[2], p.origin[2]+q[2])}
}

var (
	ruinColdBig   = [4]string{"big_ruin1", "big_ruin2", "big_ruin3", "big_ruin8"}
	ruinColdSmall = [9]string{"ruin4", "ruin1", "ruin2", "ruin3", "ruin4", "ruin5", "ruin6", "ruin7", "ruin8"}
	// ruinMainOrigin is the main piece's origin in the start chunk by rotation (L1.2, tables 0x13f0b5c and
	// 0x154b5b0).
	ruinMainOrigin = [4][2]int{{0, 0}, {15, 0}, {15, 15}, {0, 15}}
	// ruinClusterStep is a cluster piece's far corner from its own by rotation (L1.2).
	ruinClusterStep = [4][2]int{{5, 6}, {-6, 5}, {-5, -6}, {6, -5}}
	// ruinCorners are the 8 cluster corners around the start chunk's minimum corner: x offset and range,
	// z offset and range, each drawn x first (L1.2).
	ruinCorners = [8][4]int{{-15, 7, 17, 6}, {-15, 7, 1, 6}, {-15, 7, -12, 4}, {1, 6, 17, 6}, {1, 6, -12, 2},
		{17, 6, 19, 5}, {17, 6, 1, 6}, {17, 6, -12, 4}}
)

// ruinStartRandom is the random a ruin start draws from (L1.1): the monument region stream of chunk
// (cx+4, cz+4) after its 4 placement words.
func (s *Structures) ruinStartRandom(cx, cz int) *worldgen.BedrockRandom {
	r := worldgen.NewBedrockRandom(0)
	sp := spreads[Monument]
	sp.candidate(s, floorDiv(cx+4, sp.spacing), floorDiv(cz+4, sp.spacing), r)
	return r
}

// oceanRuinAt assembles a start's pieces, nil if the chunk is not one.
func (s *Structures) oceanRuinAt(cx, cz int) []*ruinPiece {
	if _, ok := s.StartIn(OceanRuin, cx, cz); !ok || s.data == nil {
		return nil
	}
	r := s.ruinStartRandom(cx, cz)
	x0, z0 := cx*16, cz*16
	biome := s.biomeName(s.g.BiomeID(x0+8, s.surface(cx, cz), z0+8))
	warm := ruinWarmBiomes[biome]
	large := float32(ruinLargeChance)
	if strings.HasPrefix(biome, "deep_") {
		large = ruinDeepLargeChance
	}
	var out []*ruinPiece
	add := func(x, z, rot int, big bool) {
		var names []string
		var integ []float32
		integrity := float32(0.8)
		if big {
			integrity = 0.9
		}
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
					names = []string{"ruin4"} // The plain template, one piece (L1.4).
				}
			}
			if names == nil {
				names = []string{base + "_brick", base + "_cracked", base + "_mossy"}
			}
			integ = []float32{integrity, ruinCrackedInteg, ruinMossyInteg}
		}
		for i, n := range names {
			t := s.data.Template("ruin/" + n)
			if t == nil {
				s.data.missingTemplate("ocean ruin", "ruin/"+n)
				continue
			}
			out = append(out, &ruinPiece{t: t, name: n, origin: [3]int{x, 0, z}, rot: rot, integrity: integ[i], big: big, warm: warm})
		}
	}
	rot := int(r.NextIntN(4))
	big := r.NextFloat() <= large
	o := ruinMainOrigin[rot]
	add(x0+o[0], z0+o[1], rot, big)
	if big && r.NextFloat() <= ruinClusterProb {
		n := func(k int) int { return int(r.NextIntN(int32(k))) }
		main := box{x0, minY, z0, x0 + 15, maxY, z0 + 15}
		var list [][2]int
		for _, c := range ruinCorners {
			x := x0 + c[0] + n(c[1])
			z := z0 + c[2] + n(c[3])
			list = append(list, [2]int{x, z})
		}
		count := 4 + n(4)
		for range count {
			if len(list) == 0 {
				break
			}
			j := n(len(list))
			q := list[j]
			list = append(list[:j], list[j+1:]...)
			pr := n(4)
			d := ruinClusterStep[pr]
			qb := box{min(q[0], q[0]+d[0]), minY, min(q[1], q[1]+d[1]), max(q[0], q[0]+d[0]), maxY, max(q[1], q[1]+d[1])}
			if !qb.intersects(main) {
				add(q[0], q[1], pr, false)
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
	// The archaeology candidates (L1.5): the cells written with the replacement's input block.
	input, output := "gravel", "suspicious_gravel"
	if p.warm {
		input, output = "sand", "suspicious_sand"
	}
	var cands [][3]int
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
		if res.name == input {
			cands = append(cands, [3]int{x, y, z})
		}
	}
	s.ruinArchaeology(w, p, cands, input, output)
	for _, i := range markerOrder(len(markers), func(i int) [3]int { return markers[i].pos }) {
		m := markers[i]
		if !strings.EqualFold(m.name, "chest") {
			continue
		}
		q := rotate(m.pos, p.rot)
		x, y, z := p.origin[0]+q[0], y0+m.pos[1], p.origin[2]+q[2]
		if !w.in(x, y, z) || !w.in(x, y-1, z) {
			continue
		}
		cell := ruinMarkerBlock(p)
		w.set(x, y, z, cell, false)
		table := ruinSmallTable
		if p.big {
			table = ruinBigTable
		}
		w.set(x, y-1, z, ruinChestRID(), y <= seaLevelField)
		w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{x, y - 1, z}, Data: map[string]any{"id": "Chest", "LootTable": table,
			"LootTableSeed": w.rng.NextInt()}})
	}
}

// ruinMarkerBlock is the block a chest marker's cell becomes (L1.4): water, sandstone in a warm ruin,
// stone bricks in ruin3_brick, gravel in ruin2_brick and the big cold brick ruins.
func ruinMarkerBlock(p *ruinPiece) uint32 {
	switch {
	case p.warm:
		return ridNamed("sandstone", nil)
	case p.name == "ruin3_brick":
		return ridNamed("stone_bricks", nil)
	case p.name == "ruin2_brick" || strings.HasPrefix(p.name, "big_ruin") && strings.HasSuffix(p.name, "_brick"):
		return ridNamed("gravel", nil)
	}
	return waterRID()
}

var ruinChestRID = sync.OnceValue(func() uint32 {
	return ridNamed("chest", map[string]any{"minecraft:cardinal_direction": "east"})
})

// ruinArchaeology is the capped archaeology replacement of one piece in one chunk (L1.5, read in part):
// the candidates shuffled forward (for i = 1 .., j = nextInt(i + 1), swap when j != i), then the first 5
// still holding the input block become the suspicious block, with the ruin's brushable loot table. Its
// random was not traced: ours is a fresh MT(the chunk's post-process seed) per piece, which also gives
// each block's loot seed (nextInt()), so the chunk's own stream, which the chest seeds come from, is not
// touched (the measured chest seeds are its first words).
func (s *Structures) ruinArchaeology(w *writer, p *ruinPiece, cands [][3]int, input, output string) {
	if len(cands) == 0 {
		return
	}
	r := worldgen.NewBedrockRandom(w.seed)
	for i := 1; i < len(cands); i++ {
		if j := int(r.NextIntN(int32(i + 1))); j != i {
			cands[i], cands[j] = cands[j], cands[i]
		}
	}
	table := "loot_tables/entities/cold_ocean_ruins_brushable_block.json"
	if p.warm {
		table = "loot_tables/entities/warm_ocean_ruins_brushable_block.json"
	}
	rid := ridNamed(output, map[string]any{"brushed_progress": int32(0), "hanging": true})
	n := 0
	for _, c := range cands {
		if n == ruinArchaeologyLimit {
			break
		}
		if w.blockName(c[0], c[1], c[2]) != input {
			continue
		}
		w.set(c[0], c[1], c[2], rid, false)
		w.bes = append(w.bes, chunk.BlockEntity{Pos: c, Data: map[string]any{"id": "BrushableBlock", "LootTable": table,
			"LootTableSeed": r.NextInt()}})
		n++
	}
}
