package worldgen

import "slices"

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
// written so far, and the profile's other placements, Java's algorithms, around them in Java's steps.
func (d *decoration) runSourceBedrock(cx, cz int, biomes *[2]uint64) {
	ox, oz := cx*16, cz*16
	underground, surface := false, false
	for _, pl := range d.g.profile.placements {
		if !underground && pl.step >= oreStep {
			underground = true
			d.bedrockPass(bedrockUnderground)
		}
		if !surface && pl.step >= vegetationStep {
			surface = true
			d.bedrockPass(bedrockSurfacePass)
		}
		if d.g.profile.bedrockReplaces[pl.name] || !placementInBiomes(pl, biomes) {
			continue
		}
		d.r.setSeed(d.decSeed + int64(pl.idx) + int64(10000*pl.step))
		d.run(pl.mods, pl.feat, pos{ox, minY, oz})
	}
	if !underground {
		d.bedrockPass(bedrockUnderground)
	}
	if !surface {
		d.bedrockPass(bedrockSurfacePass)
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
	// bAboveTopSolid is a y given as query.above_top_solid of the column drawn so far (bedrock_disks.go).
	bAboveTopSolid
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
	ident      string
	hash       uint32
	iterations int
	num, den   int      // scatter_chance as a fraction; den 0 for none.
	order      [3]int   // Axis indexes in coordinate_eval_order.
	axes       [3]bAxis // x, y, z.
	biomes     [2]uint64
	place      func(d *decoration, r *bmt, p pos)
}

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
// iterations from the last down, each position drawn from the first generator axis by axis, skipped
// outside the world or (in a chunk of several biomes) outside the rule's biome; the feature draws
// from the second.
func (d *decoration) scatter(rule *bedrockRule, b uint8, multi bool) {
	r1 := &d.br1
	if rule.den > 0 && rule.num != rule.den && int(r1.nextIntN(int32(rule.den))) >= rule.num {
		return
	}
	for i := rule.iterations - 1; i >= 0; i-- {
		p := [3]int{d.cx * 16, 0, d.cz * 16}
		for _, a := range rule.order {
			if rule.axes[a].kind == bAboveTopSolid {
				p[a] = d.aboveTopSolid(p[0], p[2])
				continue
			}
			p[a] += rule.axes[a].eval(r1)
		}
		if p[1] < minY || p[1] > maxY {
			continue
		}
		if multi && d.storedBiome(p[0], p[1], p[2]) != b {
			continue
		}
		rule.place(d, &d.br2, pos{p[0], p[1], p[2]})
	}
}

// biomeBits makes a biome set from Bedrock biome names, or every biome of the table for "all".
func biomeBits(names string) (set [2]uint64) {
	want := map[string]bool{}
	for _, n := range fieldsOf(names) {
		want[n] = true
	}
	for i := range biomeTableNames {
		if names == "all" || want[biomeRowDefs[i].String()] {
			set[i>>6] |= 1 << (i & 63)
			delete(want, biomeRowDefs[i].String())
		}
	}
	if names != "all" && len(want) > 0 {
		panic("worldgen: unknown Bedrock biome in a rule's list")
	}
	return set
}
