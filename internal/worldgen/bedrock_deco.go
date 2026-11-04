package worldgen

import (
	"slices"
	"strings"
)

// Bedrock's decoration (Task 4e), for the Bedrock profile, in our own Go from the study of BDS
// 1.26.60.6 (NOTES-bedrock-exact sections 2, 3 and Q0).
//
// Order. BDS decorates chunks in four parity phases (Q0): of two neighbouring chunks, diagonals
// included, the one with the lower phase decorates first, and a chunk runs all its passes before the
// next. Here that is made a pure function of the seed and the chunk, as the server needs: a source of
// phase p is decorated on top of what its neighbours of a lower phase wrote (each of those decorated
// the same way, so the recursion ends at phase 0 within 7 x 7 chunks), and a chunk's final blocks are its
// carvers stage with the changes of its nine sources applied by phase, then z, then x. A later phase
// saw the earlier ones' blocks, so its writes stand. Two sources of one phase never touch each other
// but can both reach the chunk between them; BDS itself does not reproduce those blocks between runs
// (section 0), and here the later of the two, in z then x order, keeps a write only where it would
// have made it seeing the other's block (laterWins, as for Java's merge).

// decorationPhase is a chunk's parity phase (Q0 table): even x even z 0, odd x even z 1, odd odd 2,
// even x odd z 3.
func decorationPhase(cx, cz int) int {
	switch {
	case cx&1 == 0 && cz&1 == 0:
		return 0
	case cz&1 == 0:
		return 1
	case cx&1 != 0:
		return 2
	}
	return 3
}

// parityBefore orders sources: by phase, then z, then x.
func parityBefore(ax, az, bx, bz int) int {
	if pa, pb := decorationPhase(ax, az), decorationPhase(bx, bz); pa != pb {
		return pa - pb
	}
	if az != bz {
		return az - bz
	}
	return ax - bx
}

// cacheParity is the cache kind of a Bedrock-profile source result.
const cacheParity cacheKind = 7

// cachedParity gives what a source chunk's decoration did, in the Bedrock order.
func (g *Generator) cachedParity(s *chunkScratch, sx, sz int) *sourceResult {
	e := g.cache.get(cacheKey{cacheParity, int32(sx), int32(sz)}, func(e *cacheEntry) int64 {
		pre := g.parityPre(s, sx, sz)
		d := s.startDecoration(g, sx, sz)
		d.pre = pre
		e.src = s.runSourceChanges(d, sx, sz)
		d.pre = nil
		return int64(cap(e.src.changes))*changeBytes + resultBytes
	})
	return e.src
}

// paritySource is one source's changes in the order they are applied.
type paritySource struct {
	sx, sz  int
	changes []sourceChange
}

// paritySources gives the sources that write into chunk (cx, cz), decorate before phase limit and
// are neighbours of (nx, nz) or it, in the order they apply. limit 4 takes them all.
func (g *Generator) paritySources(s *chunkScratch, cx, cz, limit, nx, nz int) []paritySource {
	var out []paritySource
	for sx := cx - 1; sx <= cx+1; sx++ {
		for sz := cz - 1; sz <= cz+1; sz++ {
			if decorationPhase(sx, sz) < limit && absInt(sx-nx) <= 1 && absInt(sz-nz) <= 1 {
				out = append(out, paritySource{sx: sx, sz: sz})
			}
		}
	}
	slices.SortFunc(out, func(a, b paritySource) int { return parityBefore(a.sx, a.sz, b.sx, b.sz) })
	for i := range out {
		out[i].changes = g.cachedParity(s, out[i].sx, out[i].sz).changes
	}
	return out
}

// applyParity applies sources' changes to one chunk's cells, in their order. It calls touched for
// every cell it changes.
func applyParity(cells *[chunkCells]bs, cx, cz int, sources []paritySource, touched func(cell int32)) {
	type mark struct{ phase, src int8 }
	writer := map[int32]mark{}
	for i, src := range sources {
		want := int8((cx-src.sx+1)*3 + (cz - src.sz + 1))
		ph := int8(decorationPhase(src.sx, src.sz))
		for _, c := range src.changes {
			if c.chunk != want {
				continue
			}
			cur := cells[c.cell]
			if w, ok := writer[c.cell]; ok && w.phase == ph && int(w.src) != i && cur != c.old && !laterWins(c.old, cur, c.now) {
				continue // A same-phase source got there first, unseen.
			}
			if above := c.cell + 256; above < chunkCells && !c.now.tagged(tagLogs) && !sameSupport(cur, c.now) {
				// Ground under another source's plant or trunk keeps holding it: a later write that would
				// leave it floating is refused, as the Java merge does.
				if w, ok := writer[above]; ok && int(w.src) != i && standsOnBelow(cells[above]) {
					continue
				}
			}
			writer[c.cell] = mark{ph, int8(i)}
			cells[c.cell] = c.now
			if touched != nil {
				touched(c.cell)
			}
		}
	}
}

// parityPre is what a source sees before it runs, beyond the carvers stage: in each chunk of its
// 3 x 3, the writes of its neighbours of lower phases. Q0's rule orders neighbours only: a chunk two
// away is not ordered against it in BDS, and is left out here, which also bounds the recursion to the
// 7 x 7 chunks around a source.
func (g *Generator) parityPre(s *chunkScratch, sx, sz int) []sourceWrite {
	limit := decorationPhase(sx, sz)
	if limit == 0 {
		return nil
	}
	var pre []sourceWrite
	for kx := sx - 1; kx <= sx+1; kx++ {
		for kz := sz - 1; kz <= sz+1; kz++ {
			sources := g.paritySources(s, kx, kz, limit, sx, sz)
			if len(sources) == 0 {
				continue
			}
			buf := s.takeBuffer()
			g.cachedBase(s, kx, kz).expand(buf)
			base := buf.b // A copy, to tell a real change from a write taken back.
			seen := map[int32]bool{}
			var cells []int32
			applyParity(&buf.b, kx, kz, sources, func(cell int32) {
				if !seen[cell] {
					seen[cell] = true
					cells = append(cells, cell)
				}
			})
			slices.Sort(cells) // Bottom first, so heightmaps hear of ground before what stands on it.
			for _, cell := range cells {
				if now := buf.b[cell]; now != base[cell] {
					pre = append(pre, sourceWrite{pos{kx*16 + int(cell)&15, int(cell)>>8 + minY, kz*16 + int(cell)>>4&15}, now})
				}
			}
			s.giveBuffer(buf)
		}
	}
	return pre
}

// finalCellsParity is finalCells for the Bedrock order.
func (g *Generator) finalCellsParity(s *chunkScratch, cx, cz int) *[chunkCells]bs {
	sources := g.paritySources(s, cx, cz, 4, cx, cz)
	final := &s.final
	g.cachedBase(s, cx, cz).expandCells(final)
	applyParity(final, cx, cz, sources, nil)
	return final
}

// bedrockSeeds are what a generator keeps for Bedrock's seeding (section 2).
type bedrockSeeds struct {
	level  uint32
	xm, zm uint32
}

func newBedrockSeeds(seed int64) bedrockSeeds {
	s := bedrockLevelSeed(seed)
	xm, zm := bedrockSeedMultipliers(s)
	return bedrockSeeds{s, xm, zm}
}

func (b bedrockSeeds) chunk(cx, cz int) uint32 { return bedrockChunkSeed(b.level, b.xm, b.zm, cx, cz) }

// runSourceBedrock decorates a source in the Bedrock profile: Bedrock's own passes for the rules
// written so far, and the profile's other placements, Java's algorithms, around them in Java's steps:
// the first four passes at Java's ore step, the surface passes at its vegetation step.
func (d *decoration) runSourceBedrock(cx, cz int, biomes *[2]uint64) {
	ox, oz := cx*16, cz*16
	bd := d.g.bdata
	under, surf := false, false
	for _, pl := range d.g.profile.placements {
		if !under && pl.step >= oreStep {
			under = true
			for i := range 4 {
				d.bedrockPass(bd.passes[i])
			}
		}
		if !surf && pl.step >= vegetationStep {
			surf = true
			for i := 4; i < len(bd.passes); i++ {
				d.bedrockPass(bd.passes[i])
			}
		}
		if bd.javaReplaced(d.g.profile, pl) || !placementInBiomes(pl, biomes) {
			continue
		}
		d.r.setSeed(d.decSeed + int64(pl.idx) + int64(10000*pl.step))
		d.run(pl.mods, pl.feat, pos{ox, minY, oz})
	}
	if !under {
		for i := range 4 {
			d.bedrockPass(bd.passes[i])
		}
	}
	if !surf {
		for i := 4; i < len(bd.passes); i++ {
			d.bedrockPass(bd.passes[i])
		}
	}
	d.bedrockFreeze(cx, cz)
}

// javaReplaced says whether Bedrock's passes take a Java placement's place: the profile's ores and
// disks, and every plant and tree of Java's vegetation step but glow lichen, unless the Bedrock rules
// that would place them are not written yet (javaKeep).
func (bd *BedrockData) javaReplaced(p *Profile, pl *placement) bool {
	if bd.javaKeep[pl.name] {
		return false
	}
	if p.bedrockReplaces[pl.name] {
		return true
	}
	switch pl.name {
	case "glow_lichen":
		return false
	case "forest_rock", "ore_infested", "freeze_top_layer":
		return true
	}
	return pl.step == vegetationStep
}

// bedrockJavaFallback names the Java placements kept while a Bedrock rule is not written, by a part of
// the rule's identifier.
var bedrockJavaFallback = []struct{ rule, java string }{
	{"jungle_surface_trees", "trees_jungle"},
	{"jungle_edge_surface_trees", "trees_sparse_jungle"},
	{"bamboo_jungle_surface_trees", "bamboo_vegetation"},
	{"bamboo_jungle_before_surface_bamboo", "bamboo"},
	{"swamp_surface_swamp_foliage", "trees_swamp"},
	{"mangrove_swamp_", "trees_mangrove"},
	{"pale_garden_pale_oak_tree", "pale_garden_vegetation"},
	{"swamp_surface_waterlily", "patch_waterlily"},
	{"plains_surface_flowers", "flower_plains"},
	{"flower_forest_surface_flowers", "flower_flower_forest flower_forest_flowers"},
	{"meadow_flowers_feature", "flower_meadow"},
	{"redwood_taiga_mutated_surface_trees", "trees_old_growth_spruce_taiga"},
	{"taiga_surface_mushrooms", "brown_mushroom_taiga red_mushroom_taiga brown_mushroom_old_growth red_mushroom_old_growth"},
	{"swamp_surface_mushrooms", "brown_mushroom_swamp red_mushroom_swamp"},
	{"warm_ocean_after_surface_coral_feature", "warm_ocean_vegetation"},
	{"firefly_bush", "patch_firefly_bush_swamp patch_firefly_bush_near_water_swamp patch_firefly_bush_near_water"},
	{"jungle_after_surface_vines", "vines"},
	{"extreme_hills_after_surface_emerald_ore", "ore_emerald"},
}

// keepJava fills javaKeep from the rules of a pass that were skipped.
func (bd *BedrockData) keepJava(id string) {
	for _, f := range bedrockJavaFallback {
		if strings.Contains(id, f.rule) {
			for _, n := range strings.Fields(f.java) {
				bd.javaKeep[n] = true
			}
		}
	}
}

// vegetationStep is Java's step of plants and trees, before which Bedrock's surface pass goes.
const vegetationStep = 9

// oreStep is Java's step of ores, where Bedrock's underground pass goes among the Java placements.
const oreStep = 6

// chunkBiomeList is the biomes of a chunk as BDS lists them for the passes (section P1, function
// 0xcfae6b0): sub-chunks bottom up, each in its storage order, first appearance only. Our stored
// biomes are by quart, so a sub-chunk's order is quart y, then z, then x.
func (d *decoration) chunkBiomeList(cx, cz int) []uint8 {
	list := d.biomeList[:0]
	var seen [2]uint64
	for q := range (maxY - minY + 1) / 4 {
		for qz := range 4 {
			for qx := range 4 {
				id := d.s.quartColumnAt(d.g, cx*4+qx, cz*4+qz).ids[q]
				if seen[id>>6]&(1<<(id&63)) == 0 {
					seen[id>>6] |= 1 << (id & 63)
					list = append(list, id)
				}
			}
		}
	}
	d.biomeList = list
	return list
}

// storedBiome is the biome stored at a block, which the scatter's biome test reads (section 3).
func (d *decoration) storedBiome(x, y, z int) uint8 {
	return d.s.quartColumnAt(d.g, x>>2, z>>2).ids[(y-minY)>>2]
}

// Scatter distributions (section 3 table).
type bAxisKind uint8

const (
	bSingle bAxisKind = iota
	bUniform
	bGaussian
	bInverseGaussian
	bFixedGrid
	bJitteredGrid
	bTriangle
)

// bAxis is one coordinate's distribution and extent.
type bAxis struct {
	kind   bAxisKind
	lo, hi int
}

// eval draws a value (section 3: ScatterParams::CoordinateRange::eval).
func (a bAxis) eval(r *bmt) int {
	lo, hi := a.lo, a.hi
	if a.kind == bSingle || hi <= lo {
		return lo
	}
	switch a.kind {
	case bUniform:
		return lo + int(r.nextIntN(int32(hi-lo)))
	case bGaussian:
		h := uint32((hi - lo) / 2)
		if hi-lo+1 > 2 {
			u1, u2 := r.u32(), r.u32()
			return lo + int(h) + int(u1%h) - int(u2%h)
		}
		return lo + int(h)
	case bInverseGaussian:
		if hi-lo+1 > 2 {
			h := uint32((hi - lo) / 2)
			u1, u2 := r.u32(), r.u32()
			dd := int(u1%h) - int(u2%h)
			if dd < 0 {
				return lo - dd
			}
			if dd > 0 {
				return hi - dd
			}
		}
		if r.nextBool() {
			return hi
		}
		return lo
	case bTriangle:
		rg := hi - lo
		return lo + int(r.nextIntN(int32(rg/2+1))) + int(r.nextIntN(int32(rg-rg/2+1)))
	}
	panic("worldgen: grid distributions are not written yet")
}

// bedrockRule is one feature rule (section 3): its identifier's hash, scatter and feature.
type bedrockRule struct {
	ident  string
	hash   uint32
	sc     *bScatter
	biomes [2]uint64
	feat   bfeat
}

// iterations is the rule's iteration count when it is a plain number.
func (r *bedrockRule) iterations() int { return r.sc.iterations }

// evalOrder turns "zyx" and the like into axis indexes.
func evalOrder(s string) (o [3]int) {
	for i := range 3 {
		o[i] = int(s[i] - 'x')
	}
	return o
}

// bedrockPass runs one pass over a source: for each biome of the chunk, each of the pass's rules that
// biome lists, in the pass's order (section 3).
func (d *decoration) bedrockPass(rules []*bedrockRule) {
	cs := d.g.bseeds.chunk(d.cx, d.cz)
	list := d.chunkBiomeList(d.cx, d.cz)
	multi := len(list) > 1
	for i := range list {
		b := list[i]
		for _, rule := range rules {
			if rule.biomes[b>>6]&(1<<(b&63)) == 0 {
				continue
			}
			rs := bedrockRuleSeed(cs, rule.hash)
			d.br1.setSeed(rs)
			d.br2.setSeed(rs)
			d.scatter(rule, b, multi)
		}
	}
}

// scatter is ScatterParams::prepare and the placement loop (section 3): the chance, then the
// iterations from the last down, each position drawn from the first generator axis by axis from the
// chunk corner (y from 0), skipped (in a chunk of several biomes) outside the world or the rule's
// biome; the feature draws from the second. Molang sees the chunk corner as variable.originx/z (T3).
func (d *decoration) scatter(rule *bedrockRule, b uint8, multi bool) {
	r1 := &d.br1
	e := &d.menv
	*e = molangEnv{d: d, r1: r1, originX: float32(d.cx * 16), originZ: float32(d.cz * 16), worldX: float32(d.cx * 16),
		worldZ: float32(d.cz * 16), biome: b}
	sc := rule.sc
	if !sc.chance(r1, e) {
		return
	}
	n := sc.count(e)
	for i := n - 1; i >= 0; i-- {
		e.worldX, e.worldY, e.worldZ = float32(d.cx*16), 0, float32(d.cz*16)
		p := sc.position(r1, e, [3]int{d.cx * 16, 0, d.cz * 16})
		// Only a chunk of several biomes tests the height, with the biome (T2): in a one-biome chunk a
		// position outside the world still places, and its feature still draws.
		if multi && (p[1] < minY || p[1] > maxY || d.storedBiome(p[0], p[1], p[2]) != b) {
			continue
		}
		rule.feat.place(d, &d.br2, pos{p[0], p[1], p[2]})
	}
}
