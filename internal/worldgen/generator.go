// Package worldgen is Overmind's Overworld generator. Per the owner's decision it follows Mojang's
// algorithms throughout, in our own Go from written study notes, so that a seed gives the game's
// biomes, terrain, caves, ores and decorations. See NOTES.md.
package worldgen

import (
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// World limits: Y -64 to 319 and sea level 63, so the top water block is Y 62.
const (
	minY     = -64
	maxY     = 319
	SeaLevel = 63
	waterTop = SeaLevel - 1
)

// Generator builds Overworld chunks from a seed. Apart from its shared cache, which locks, it holds
// only data that never changes after New, so any number of workers can share it.
type Generator struct {
	seed       int64
	cache      *sharedCache
	zoomSeed   int64
	climate    climateNoises
	caves      caveNoises
	aquiferFac seedFactory
	surface    surfaceNoises
	blocks     blocks
	sf         surfaceFactories
	oreFac     seedFactory
	// regionFac seeds each source chunk's level random, Java's WorldGenRegion random (feature.go).
	regionFac seedFactory
	profile   *Profile
	bsurf     *bedrockSurface // Bedrock's surface builder, for a profile that uses it.
	// climateT is the biome temperatures: the profile's table, with the pack's values when the data is read.
	climateT  *climateTable
	bseeds    bedrockSeeds // Bedrock's decoration seeds.
	bdata     *BedrockData // Bedrock's feature data, nil when no install was read.
	geodeOnce sync.Once
	geode     *normalNoise
	// biomeTree is the biome table's search tree: Java's, or Bedrock's with its own cave biome.
	biomeTree *biomeNode
	// bedrockBiomes says decoration, the surface pass and the subsurface pass read Bedrock's stored 3D
	// biomes (bedrock_biomes.go) rather than quarts.
	bedrockBiomes bool
}

// New returns a Generator for a seed, with the default profile, Bedrock's.
func New(seed int64) *Generator { return NewWithProfile(seed, ProfileBedrock) }

// NewWithProfile returns a Generator for a seed and a placement profile, with the feature data set
// by SetDefaultBedrockData.
func NewWithProfile(seed int64, profile *Profile) *Generator {
	return NewWithData(seed, profile, defaultBedrockData)
}

// NewWithData returns a Generator for a seed, a profile and Bedrock's feature data. The Bedrock
// profile follows Bedrock's own features only with data; with nil it uses the built-in, Java-based
// features, which are approximate. Overjava never reads it.
func NewWithData(seed int64, profile *Profile, bd *BedrockData) *Generator {
	root := xoroFromSeed(seed)
	w := root.forkFactory()
	aquifer := w.named("minecraft:aquifer")
	ore := w.named("minecraft:ore")
	region := w.named("minecraft:worldgen_region_random")
	g := &Generator{
		cache:      newSharedCache(DefaultCacheBytes),
		seed:       seed,
		sf:         newSurfaceFactories(w, profile.deepslateName),
		oreFac:     ore.forkFactory(),
		regionFac:  region.forkFactory(),
		profile:    profile,
		zoomSeed:   biomeZoomSeed(seed),
		climate:    newClimateNoises(w),
		caves:      newCaveNoises(w),
		aquiferFac: aquifer.forkFactory(),
		surface:    newSurfaceNoises(w),
		blocks:     newBlocks(),
		bseeds:     newBedrockSeeds(seed),
	}
	if !profile.bedrockDeco && !profile.surfaceBedrock && !profile.carversBedrock {
		bd = nil
	}
	g.bdata = bd
	g.climateT = profile.climate
	if bd != nil {
		t := *profile.climate
		for i := range t {
			if bd.climateSet[i] {
				t[i].temp = bd.climate[i]
			}
		}
		g.climateT = &t
	}
	g.biomeTree = biomeTree()
	if profile.surfaceBedrock && bd != nil {
		g.bsurf = newBedrockSurface(seed, w, bd)
		g.bedrockBiomes = true
		g.biomeTree = bedrockBiomeTrees()[0]
		if bd.biomes[sulfurCaves.String()] != nil {
			g.biomeTree = bedrockBiomeTrees()[1]
		}
	}
	return g
}

// bedrockDeco says whether decoration follows Bedrock's order and passes: the profile asks for it and
// the data is there.
func (g *Generator) bedrockDeco() bool { return g.profile.bedrockDeco && g.bdata != nil }

// chunkScratch holds working space for one GenerateChunk call. It goes back in a pool. What can be
// read again later, preliminary levels and walk corners, is a pure function of the generator and a
// position and is checked against both, so it changes speed and never a result.
type chunkScratch struct {
	shapes      [cellsAcross + 1][cellsAcross + 1]columnShape
	nodes       [cellsAcross + 1][cellsAcross + 1][columnNodes]node
	prelims     prelimCache
	aq          aquiferCache
	walkAq      aquiferCache
	walkAqReady bool
	walk        columnWalk
	walkCorners [walkCornerSlots]walkCorner
	quarts      [quartSlots]quartColumn
	bsamples    [bSampleSlots]bSampleColumn // Bedrock's biome samples (bedrock_biomes.go).
	btops       [64]bTopEntry
	blist       []uint8
	rule        ruleContext
	base        baseChunk
	masks       [256]carveMask
	caveSegs    []caveSegment // The Bedrock carvers' segments of one source (bedrock_carvers.go).
	canyonSegs  []canyonSegment
	deco        decoration
	copies      []*baseChunk // Spare full buffers, which the working set's chunks are expanded into.
	final       [chunkCells]bs
	changes     []sourceChange
	changeKeys  []int64 // Each change's working-set key, slot and cell.
	merge       merger
	firstOld    map[int64]bs
}

const cellsAcross = 16 / cellXZ

// startChunk makes the scratch ready for a chunk.
func (s *chunkScratch) startChunk(g *Generator, originX, originZ int) {
	s.prelims.use(g)
	s.walkAqReady = false
	s.aq.reset(originX, originZ, &s.prelims)
}

// startColumnOnly makes the scratch ready for a column asked about outside any chunk.
func (s *chunkScratch) startColumnOnly(g *Generator) {
	s.prelims.use(g)
	s.walkAqReady = false
}

var scratchPool = sync.Pool{New: func() any { return newScratch() }}

func newScratch() *chunkScratch {
	return &chunkScratch{firstOld: map[int64]bs{}}
}

// GenerateChunk builds a chunk: noise, surface and carvers for it and its neighbours, then the changes
// of the nine source chunks that can reach it, each decorated alone, merged in row-major order. It
// reads no real neighbour. Bases and source results go in the shared cache, which empties once no
// chunk is being asked for.
func (g *Generator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	cx, cz := int(pos[0]), int(pos[1])
	g.cache.begin()
	defer g.cache.end()
	s := scratchPool.Get().(*chunkScratch)
	defer scratchPool.Put(s)
	writeCells(c, g.finalCells(s, cx, cz))
	g.storeBiomes(c, s, cx, cz)
}

// finalCells makes a chunk's finished blocks, in the scratch.
func (g *Generator) finalCells(s *chunkScratch, cx, cz int) *[chunkCells]bs {
	if g.bedrockDeco() {
		return g.finalCellsParity(s, cx, cz)
	}
	final := &s.final
	g.cachedBase(s, cx, cz).expandCells(final)
	var sources [9][]sourceChange
	var want [9]int8
	for i := range 9 {
		sx, sz := cx-1+i/3, cz-1+i%3
		sources[i] = g.cachedSource(s, sx, sz).changes
		want[i] = int8((cx-sx+1)*3 + (cz - sz + 1))
	}
	s.merge.apply(final, &sources, &want)
	return final
}

// merger applies the changes of a chunk's nine sources to its carvers stage, in row-major source
// order. Each source ran alone; in Java the one that runs second sees the other's blocks. So:
//   - where an earlier source already changed a cell, a later write is kept only if the later
//     feature, seeing that block, would have made it too (laterWins);
//   - a later plant is refused if an earlier source changed the block under it to one it would not
//     stand on (sameSupport), as Java's survival test would refuse it, and so is what the same
//     source stacked on it;
//   - a later write is refused if it would turn the block under an earlier source's plant or trunk
//     into one that would not hold it, which in Java leaves it floating;
//   - a trunk always keeps the ground it was grown on: where an earlier source changed the block under
//     a later trunk, that change is taken back. Refusing the trunk instead would leave its leaves,
//     in other chunks, with no log;
//   - a double plant whose other half was refused is refused whole.
//
// Every decision reads only the changes at that cell or in its column, so a chunk and its neighbours
// agree on blocks that straddle their edge.
type merger struct {
	halves  []mergedHalf
	trunks  []int32
	history []mergedWrite
	writer  [chunkCells]int8 // Which source changed a cell, counted from 1; 0 if none did.
	refused [chunkCells]int8 // Which source last had a write refused there.
}

// mergedWrite is a kept write, with the block and writer before it.
type mergedWrite struct {
	cell       int32
	prev       bs
	prevWriter int8
}

// mergedHalf is a double plant half a source wrote, and the block before it.
type mergedHalf struct {
	cell      int32
	prev, now bs
}

func (m *merger) apply(final *[chunkCells]bs, sources *[9][]sourceChange, want *[9]int8) {
	m.halves, m.trunks, m.history = m.halves[:0], m.trunks[:0], m.history[:0]
	clear(m.writer[:])
	clear(m.refused[:])
	base := *final
	for i, changes := range sources {
		for _, c := range changes {
			if c.chunk != want[i] {
				continue
			}
			cur := final[c.cell]
			self := int8(i + 1)
			if !m.keeps(final, &base, c, self) {
				m.refused[c.cell] = self
				continue
			}
			if c.now.tagged(tagLogs) {
				m.trunks = append(m.trunks, c.cell)
			}
			m.history = append(m.history, mergedWrite{c.cell, cur, m.writer[c.cell]})
			m.writer[c.cell] = self
			if c.now.half() != 0 {
				m.halves = append(m.halves, mergedHalf{c.cell, cur, c.now})
			}
			final[c.cell] = c.now
		}
	}
	for _, t := range m.trunks {
		m.keepTrunkGround(final, &base, t)
	}
	// A refused half leaves its partner alone; take the partner back too, until none is left alone.
	for again := true; again; {
		again = false
		for _, h := range m.halves {
			if final[h.cell] != h.now || pairedHalf(final, h.cell) {
				continue
			}
			final[h.cell] = h.prev
			again = true
		}
	}
}

// laterWins says whether a write of now, by a feature that saw old there, would be made over cur, the
// block an earlier source left. A feature replaces what it tests for, so it replaces cur if cur passes
// the same test old did: the same block, or both base stone (the ore and rock rule, the game's
// base_stone_overworld) or both air. Leaves also replace leaves and the plants trees grow through.
// Anything else the later feature would have found in its way.
//
// A log also replaces any plant. In Java a tree finding a plant in its trunk's way is not placed at
// all, but a tree reaches into other chunks, and one column cannot take back the rest of it, so the
// trunk goes through and the plant goes.
func laterWins(old, cur, now bs) bool {
	switch {
	case cur == old, cur == now, cur.isAir() && old.isAir(), cur.tagged(tagBaseStone) && old.tagged(tagBaseStone):
		// cur == now: an earlier source already left what this one writes, as overlapping moss
		// patches do. The block is the same either way, and what stands on it stays this source's.
		return true
	case now.tagged(tagLogs):
		return cur.isAir() || cur.tagged(tagLeaves) || cur.tagged(tagTreeSwap) || standsOnBelow(cur) && !cur.tagged(tagLogs)
	case now.tagged(tagLeaves):
		return cur.isAir() || cur.tagged(tagLeaves) || cur.tagged(tagTreeSwap)
	}
	return false
}

// keeps decides one write of source self against what the earlier sources left.
func (m *merger) keeps(final, base *[chunkCells]bs, c sourceChange, self int8) bool {
	cur := final[c.cell]
	if cur != c.old && !laterWins(c.old, cur, c.now) {
		return false
	}
	if below := c.cell - 256; below >= 0 && standsOnBelow(c.now) && !c.now.tagged(tagLogs) {
		if m.refused[below] == self && m.writer[below] != self {
			return false // What it stood on, from the same source, was refused.
		}
		if m.byOther(below, self) && !sameSupport(base[below], final[below]) {
			return false
		}
	}
	// A trunk goes through a plant above it too (laterWins); anything else keeps the plant's ground.
	if above := c.cell + 256; above < chunkCells && !c.now.tagged(tagLogs) && m.byOther(above, self) && standsOnBelow(final[above]) && !sameSupport(cur, c.now) {
		return false
	}
	return true
}

// keepTrunkGround takes back other sources' changes under a trunk's lowest log, newest first, until
// the block there is one of the same kind as before any source ran.
func (m *merger) keepTrunkGround(final, base *[chunkCells]bs, trunk int32) {
	below := trunk - 256
	if below < 0 || m.writer[below] == m.writer[trunk] || final[below].tagged(tagLogs) {
		return
	}
	for i := len(m.history) - 1; i >= 0 && !sameSupport(base[below], final[below]); i-- {
		if h := m.history[i]; h.cell == below && m.writer[below] != m.writer[trunk] {
			final[below], m.writer[below] = h.prev, h.prevWriter
		}
	}
}

// byOther says whether a source other than self changed a cell.
func (m *merger) byOther(cell int32, self int8) bool { w := m.writer[cell]; return w != 0 && w != self }

// standsOnBelow says whether a block needs the block under it: a plant, a snow layer or a log. Blocks
// that hang from a side or a ceiling do not.
func standsOnBelow(s bs) bool {
	if s.tagged(tagLogs) {
		return true
	}
	return !s.isAir() && !s.has(fMotion) && !s.has(fLiquid) && !s.tagged(tagHangs)
}

// sameSupport says whether a block that stood on a holds on b too: the same block, or both of the
// dirt family, both sand, both base stone.
func sameSupport(a, b bs) bool {
	return a == b || a.tagged(tagDirtLike) && b.tagged(tagDirtLike) || a.tagged(tagSand) && b.tagged(tagSand) ||
		a.tagged(tagBaseStone) && b.tagged(tagBaseStone)
}

// pairedHalf says whether the double plant half at a cell has its other half beside it.
func pairedHalf(final *[chunkCells]bs, cell int32) bool {
	s := final[cell]
	other := cell + 256
	if s.half() > 0 {
		other = cell - 256
	}
	if other < 0 || other >= chunkCells {
		return false
	}
	o := final[other]
	return o.block() == s.block() && o.half() == -s.half()
}

// sourceChange is one block a source's decoration changed: the chunk of its 3 x 3 (x*3+z), the
// cell, the block before and after, and the feature call (unit) that last wrote it.
type sourceChange struct {
	chunk    int8
	cell     int32
	old, now bs // What the source found there, and what it left.
}

// sourceResult is what one source's decoration did, a pure function of the seed, profile, source and
// level (cachedLevel).
type sourceResult struct {
	changes []sourceChange
}

// runSourceChanges runs a source alone on the working set, lists what it changed, and rolls the
// working set back.
func (s *chunkScratch) runSourceChanges(d *decoration, sx, sz int) *sourceResult {
	d.runSource(sx, sz)
	s.changes = s.changes[:0]
	first := s.firstOld
	clear(first)
	// The source's own writes, after what its earlier neighbours left (d.preLen).
	for _, e := range d.log[d.preLen:] {
		// A cell of the working set as one key: its slot in the high half, its cell in the low.
		k := int64(e.chunk)<<32 | int64(e.cell)
		if _, seen := first[k]; !seen {
			first[k] = e.old
		}
	}
	res := &sourceResult{}
	s.changeKeys = s.changeKeys[:0]
	for k, old := range first {
		lv := &d.live[k>>32]
		cell := int32(k)
		now := lv.b.b[cell]
		if now == old {
			continue
		}
		dx, dz := lv.b.cx-sx+1, lv.b.cz-sz+1
		s.changes = append(s.changes, sourceChange{int8(dx*3 + dz), cell, old, now})
		s.changeKeys = append(s.changeKeys, k)
	}
	// A plant or trunk may stand on ground an earlier neighbour left at the level before (a moss
	// patch), which the finished world may lack. Such ground goes with the source's own changes, so
	// what stands on it keeps it: in Java the later feature saw it there.
	type ground struct {
		k    int64
		at   int
		seen bs
	}
	var grounds []ground
	for i, c := range s.changes {
		if d.g.bedrockDeco() {
			break // In the Bedrock order a source's ground is what the finished world has.
		}
		if !standsOnBelow(c.now) || c.cell < 256 {
			continue
		}
		slot := s.changeKeys[i] >> 32
		below := slot<<32 | int64(c.cell-256)
		if _, own := first[below]; !own {
			grounds = append(grounds, ground{below, i, d.live[slot].b.b[c.cell-256]})
		}
	}
	d.rollback()
	d.log = d.log[:0]
	for _, gr := range grounds {
		base := d.live[gr.k>>32].b.b[int32(gr.k)]
		if base != gr.seen {
			c := s.changes[gr.at]
			s.changes = append(s.changes, sourceChange{c.chunk, c.cell - 256, base, gr.seen})
		}
	}
	// By cell, so a column's blocks come bottom first: the merge decides a block after the one it
	// stands on.
	slices.SortFunc(s.changes, func(a, b sourceChange) int {
		if a.chunk != b.chunk {
			return int(a.chunk) - int(b.chunk)
		}
		return int(a.cell - b.cell)
	})
	res.changes = append([]sourceChange(nil), s.changes...)
	return res
}

// storeBiomes writes the chunk's quart biomes, from the same lookups the passes read.
func (g *Generator) storeBiomes(c *chunk.Chunk, s *chunkScratch, cx, cz int) {
	for y := minY; y <= maxY; y += 16 {
		var section [4][4][4]uint32
		base := (y - minY) / 4
		for i := range section {
			for k := range section[i][0] {
				q := s.quartColumnAt(g, cx*4+i, cz*4+k)
				for j := range section[i] {
					section[i][j][k] = uint32(biomeRowDefs[q.ids[base+j]].EncodeBiome())
				}
			}
		}
		c.SetBiomeSection(int16(y), section)
	}
}

// Kinds of noise cell, worked out once for all the block columns that share it.
const (
	cellUnknown uint8 = iota
	cellSky           // No corner can be solid and it is over the skip height: blocks take the global fluid.
	cellSolid         // Every corner is solid, with no noodle or vein possible.
	cellMixed
)

// fillNoise is the noise pass, for the columns x0 to x1, z0 to z1 of a chunk. A cell's corners are read
// only if some block in it needs its density. It writes what the study says the game's noise stage
// holds: stone, veins, water and lava, with nothing sealed.
func (g *Generator) fillNoise(b *baseChunk, s *chunkScratch, x0, x1, z0, z1 int) {
	originX, originZ := b.cx*16, b.cz*16
	for i := range cellsAcross + 1 {
		for j := range cellsAcross + 1 {
			s.shapes[i][j] = g.shapeAt(originX+i*cellXZ, originZ+j*cellXZ)
			for n := range columnNodes {
				nd := &s.nodes[i][j][n]
				nd.haveCore, nd.haveNoodle, nd.haveVein, nd.haveRidge = false, false, false, false
			}
		}
	}
	skip := g.skipAbove(&s.aq)
	var kinds [cellsAcross][cellsAcross][columnNodes - 1]uint8
	var noodles [cellsAcross][cellsAcross][columnNodes - 1]bool
	var veins [cellsAcross][cellsAcross][columnNodes - 1]bool
	for lx := x0; lx <= x1; lx++ {
		i, dx := lx/cellXZ, lx%cellXZ
		fx := float64(dx) / 4.0
		wx := originX + lx
		for lz := z0; lz <= z1; lz++ {
			j, dz := lz/cellXZ, lz%cellXZ
			fz := float64(dz) / 4.0
			wz := originZ + lz
			for l := columnNodes - 2; l >= 0; l-- {
				baseY := minY + l*cellY
				kind := kinds[i][j][l]
				if kind == cellUnknown {
					kind, noodles[i][j][l], veins[i][j][l] = g.classifyCell(s, originX, originZ, i, j, l, skip)
					kinds[i][j][l] = kind
				}
				switch kind {
				case cellSky:
					if baseY >= seaFluidTop {
						continue // Open air, which is what a new chunk already holds.
					}
					for dy := cellY - 1; dy >= 0; dy-- {
						y := baseY + dy
						if k := globalFluid(y).at(y); k != fillAir {
							b.b[cellIndex(lx, y, lz)] = fluidState(k)
						}
					}
				case cellSolid:
					for dy := cellY - 1; dy >= 0; dy-- {
						b.b[cellIndex(lx, baseY+dy, lz)] = sStone
					}
				default:
					corners := s.cellAt(i, j, l)
					blend := corners.blend(fx, fz, noodles[i][j][l], veins[i][j][l])
					for dy := cellY - 1; dy >= 0; dy-- {
						y := baseY + dy
						fy := float64(dy) / 8.0
						f := blend.final(fy)
						var k fillKind = fillSolid
						if !(f > 0) {
							k = g.fluidAt(&s.aq, wx, y, wz, f)
						}
						st := fluidState(k)
						if k == fillSolid {
							st = sStone
							if veins[i][j][l] {
								st = g.veinState(&corners, &blend, originX+i*cellXZ, baseY, originZ+j*cellXZ, wx, y, wz, fx, fy, fz)
							}
						}
						b.b[cellIndex(lx, y, lz)] = st
					}
				}
			}
		}
	}
}

// fluidState is the block of an open space.
func fluidState(k fillKind) bs {
	switch k {
	case fillWater:
		return sWater
	case fillLava:
		return sLava
	}
	return sAir
}

// cellAt gives the corners of a cell. classifyCell has read them.
func (s *chunkScratch) cellAt(i, j, l int) cellCorners {
	var c cellCorners
	for a := range 2 {
		for k := range 2 {
			for b := range 2 {
				c[a][k][b] = &s.nodes[i+a][j+b][l+k]
			}
		}
	}
	return c
}

// classifyCell decides how the blocks of a cell are worked out, reading only what it must. Blends of
// positive values are positive and blends of values at or under zero stay there, so a cell whose
// corners agree needs no block-level density.
func (g *Generator) classifyCell(s *chunkScratch, originX, originZ, i, j, l, skip int) (kind uint8, noodle, veins bool) {
	baseY := minY + l*cellY
	if baseY > skip {
		sky := true
		for a := range 2 {
			for b := range 2 {
				sh := &s.shapes[i+a][j+b]
				sky = sky && sh.certainlyAir(baseY) && sh.certainlyAir(baseY+cellY)
			}
		}
		if sky {
			return cellSky, false, false
		}
	}
	corners := s.cellAt(i, j, l)
	allSolid := true
	for a := range 2 {
		for k := range 2 {
			for b := range 2 {
				x, y, z := originX+(i+a)*cellXZ, baseY+k*cellY, originZ+(j+b)*cellXZ
				n := corners[a][k][b]
				n.needCore(g, &s.shapes[i+a][j+b], x, y, z)
				allSolid = allSolid && n.core > 0
			}
		}
	}
	noodle = corners.noodleLive()
	if noodle {
		for a := range 2 {
			for k := range 2 {
				for b := range 2 {
					corners[a][k][b].needNoodle(g, originX+(i+a)*cellXZ, baseY+k*cellY, originZ+(j+b)*cellXZ)
				}
			}
		}
	}
	if cellHasVeins(baseY) {
		for a := range 2 {
			for k := range 2 {
				for b := range 2 {
					n := corners[a][k][b]
					n.needVein(g, originX+(i+a)*cellXZ, baseY+k*cellY, originZ+(j+b)*cellXZ)
					veins = veins || absF(n.vein) >= veinStrength
				}
			}
		}
	}
	if allSolid && !noodle && !veins {
		return cellSolid, false, false
	}
	return cellMixed, noodle, veins
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// spawnMinContinentalness and spawnMinWeirdness are what DefaultSpawn asks of a place: dry inland
// ground away from a river valley, as the study says the game's spawn search aims for.
const (
	spawnMinContinentalness = 0.03
	spawnMinWeirdness       = 0.16
)

// spawnClear says whether a player can stand at (x, y, z): the block under the feet is ground, and
// the feet and head are air in the finished chunk, so not inside a tree, plant or snow layer.
func (g *Generator) spawnClear(x, y, z int) bool {
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	g.GenerateChunk(world.ChunkPos{int32(x >> 4), int32(z >> 4)}, c)
	lx, lz := uint8(x&15), uint8(z&15)
	ground := c.Block(lx, int16(y-1), lz, 0)
	return ground != g.blocks.air && ground != g.blocks.water && ground != g.blocks.lava &&
		c.Block(lx, int16(y), lz, 0) == g.blocks.air && c.Block(lx, int16(y+1), lz, 0) == g.blocks.air
}

// DefaultSpawn searches outward from the origin for such ground, with room to stand on it. A
// candidate costs one chunk, and only those that pass the cheap tests get that far.
func (g *Generator) DefaultSpawn(world.Dimension) cube.Pos {
	const step, reach = 32, 2048
	for r := 0; r <= reach; r += step {
		for a := -r; a <= r; a += step {
			for _, p := range [4][2]int{{a, -r}, {a, r}, {-r, a}, {r, a}} {
				cl := g.Climate(p[0], p[1])
				if cl.Continentalness < spawnMinContinentalness || cl.Weirdness > -spawnMinWeirdness && cl.Weirdness < spawnMinWeirdness {
					continue
				}
				if h := g.SurfaceHeight(p[0], p[1]); h >= SeaLevel && g.spawnClear(p[0], h+1, p[1]) {
					return cube.Pos{p[0], h + 1, p[1]}
				}
			}
		}
	}
	return g.fallbackSpawn(0, 0)
}

// fallbackSpawn is the spawn used when the search finds no land: just above the highest block of the
// finished column at x, z, which may be water or leaves, so a player there has room and nothing
// over their head.
func (g *Generator) fallbackSpawn(x, z int) cube.Pos {
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	g.GenerateChunk(world.ChunkPos{int32(x >> 4), int32(z >> 4)}, c)
	lx, lz := uint8(x&15), uint8(z&15)
	r := world.Overworld.Range()
	for y := r.Max() - 2; y > r.Min(); y-- {
		if c.Block(lx, int16(y), lz, 0) != g.blocks.air {
			return cube.Pos{x, y + 1, z}
		}
	}
	return cube.Pos{x, max(g.SurfaceHeight(x, z), SeaLevel) + 1, z}
}

// takeBuffer gives the worker a full base buffer from its spares.
func (s *chunkScratch) takeBuffer() *baseChunk {
	if n := len(s.copies); n > 0 {
		b := s.copies[n-1]
		s.copies = s.copies[:n-1]
		return b
	}
	return new(baseChunk)
}

// giveBuffer returns a buffer to the spares.
func (s *chunkScratch) giveBuffer(b *baseChunk) { s.copies = append(s.copies, b) }

// startDecoration readies the working set around a chunk.
func (s *chunkScratch) startDecoration(g *Generator, cx, cz int) *decoration {
	s.deco.startTarget(g, s, cx, cz)
	s.deco.listOf = listKey{}
	return &s.deco
}
