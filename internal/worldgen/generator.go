// Package worldgen is Overmind's Overworld generator. Per the owner's decision it follows Mojang's
// algorithms throughout, in our own Go from written study notes, so that a seed gives the game's
// biomes, terrain, caves, ores and decorations. See NOTES.md.
package worldgen

import (
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

// Generator builds Overworld chunks from a seed. It holds only data that never changes
// after New, so any number of workers can share it.
type Generator struct {
	seed       int64
	reuseBases bool
	keptLock   sync.Mutex
	kept       []*chunkScratch // Working spaces kept with reuse on, one per worker that has asked.
	zoomSeed   int64
	climate    climateNoises
	caves      caveNoises
	aquiferFac seedFactory
	surface    surfaceNoises
	blocks     blocks
	sf         surfaceFactories
	oreFac     seedFactory
	profile    *Profile
}

// defaultReuseBases is what a new generator starts with. Tests turn it on, for speed: they generate
// many chunks, and TestReuseBasesChangesNothing holds it to changing no result.
var defaultReuseBases = false

// New returns a Generator for a seed, with the default profile, Bedrock's.
func New(seed int64) *Generator { return NewWithProfile(seed, ProfileBedrock) }

// NewWithProfile returns a Generator for a seed and a placement profile.
func NewWithProfile(seed int64, profile *Profile) *Generator {
	root := xoroFromSeed(seed)
	w := root.forkFactory()
	aquifer := w.named("minecraft:aquifer")
	ore := w.named("minecraft:ore")
	return &Generator{
		reuseBases: defaultReuseBases,
		seed:       seed,
		sf:         newSurfaceFactories(w),
		oreFac:     ore.forkFactory(),
		profile:    profile,
		zoomSeed:   biomeZoomSeed(seed),
		climate:    newClimateNoises(w),
		caves:      newCaveNoises(w),
		aquiferFac: aquifer.forkFactory(),
		surface:    newSurfaceNoises(w),
		blocks:     newBlocks(),
	}
}

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
	rule        ruleContext
	base        baseChunk
	masks       [256]carveMask
	deco        decoration
	bases       [keptSpan * keptSpan]*baseChunk
	final       [chunkCells]bs
	sources     [keptSpan * keptSpan]sourceResult
	firstOld    map[int64]bs
	busy        bool
	lastCX      int
	lastCZ      int
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

var scratchPool = sync.Pool{New: func() any { return &chunkScratch{firstOld: map[int64]bs{}} }}

// GenerateChunk builds a chunk: noise, surface and carvers for it and its neighbours, then the changes
// of the nine source chunks that can reach it, each decorated alone, merged in row-major order. It
// reads no real neighbour and keeps nothing about a chunk after it returns, unless the generator was
// told to reuse bases (SetReuseBases), which changes speed and never a result.
func (g *Generator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	cx, cz := int(pos[0]), int(pos[1])
	s := g.getScratch(cx, cz)
	defer g.putScratch(s)
	if !g.reuseBases {
		s.forgetBases()
	}
	writeCells(c, g.finalCells(s, cx, cz))
	g.storeBiomes(c, s, cx, cz)
}

// finalCells makes a chunk's finished blocks, in the scratch.
func (g *Generator) finalCells(s *chunkScratch, cx, cz int) *[chunkCells]bs {
	d := s.startDecoration(g, cx, cz)
	here := d.chunkAt(cx, cz)
	final := &s.final
	*final = here.b.b
	for sx := cx - 1; sx <= cx+1; sx++ {
		for sz := cz - 1; sz <= cz+1; sz++ {
			m := s.sourceChanges(g, d, sx, sz)
			want := int8((cx-sx+1)*3 + (cz - sz + 1))
			for _, c := range m.changes {
				if c.chunk == want {
					final[c.cell] = c.now
				}
			}
		}
	}
	return final
}

// sourceResult is what one source's decoration changed, by chunk of its 3 x 3 (x*3+z) and cell. It
// is a pure function of the seed, profile and source chunk.
type sourceResult struct {
	owner   *Generator
	sx, sz  int
	ok      bool
	changes []sourceChange
}

type sourceChange struct {
	chunk int8
	cell  int32
	now   bs
}

// sourceChanges runs a source alone, or finds its result kept from an earlier chunk when bases are
// reused, and rolls the working set back.
func (s *chunkScratch) sourceChanges(g *Generator, d *decoration, sx, sz int) *sourceResult {
	m := &s.sources[floorMod(sx, keptSpan)*keptSpan+floorMod(sz, keptSpan)]
	if g.reuseBases && m.ok && m.owner == g && m.sx == sx && m.sz == sz {
		return m
	}
	d.runSource(sx, sz)
	m.owner, m.sx, m.sz, m.ok = g, sx, sz, g.reuseBases
	m.changes = m.changes[:0]
	first := s.firstOld
	clear(first)
	for _, e := range d.log {
		k := int64(e.chunk)<<32 | int64(e.cell)
		if _, seen := first[k]; !seen {
			first[k] = e.old
		}
	}
	for k, old := range first {
		lv := &d.live[k>>32]
		cell := int32(k)
		now := lv.b.b[cell]
		if now == old {
			continue
		}
		dx, dz := lv.b.cx-sx+1, lv.b.cz-sz+1
		m.changes = append(m.changes, sourceChange{int8(dx*3 + dz), cell, now})
	}
	d.rollback()
	d.log = d.log[:0]
	return m
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
	return cube.Pos{0, max(g.SurfaceHeight(0, 0), SeaLevel) + 1, 0}
}

// forgetBases marks every kept base as unusable.
func (s *chunkScratch) forgetBases() {
	for _, b := range s.bases {
		if b != nil {
			b.whole = false
		}
	}
}

// baseFor gives a chunk's carvers stage, in a slot by its position, built on first use.
func (s *chunkScratch) baseFor(g *Generator, _, cx, cz int) *baseChunk {
	slot := floorMod(cx, keptSpan)*keptSpan + floorMod(cz, keptSpan)
	b := s.bases[slot]
	if b == nil {
		b = new(baseChunk)
		s.bases[slot] = b
	}
	if b.owner != g || b.cx != cx || b.cz != cz || !b.whole {
		g.buildBase(b, s, cx, cz, stageCarvers, 0, 15, 0, 15)
		b.whole = true
	}
	return b
}

func floorMod(a, b int) int { return a - floorDiv(a, b)*b }

// getScratch takes a chunk's working space. With reuse on, a generator keeps its own spaces, which the
// garbage collector does not empty as it does a sync.Pool, and hands out the free one that last made
// the nearest chunk, so its kept bases serve.
func (g *Generator) getScratch(cx, cz int) *chunkScratch {
	if !g.reuseBases {
		return scratchPool.Get().(*chunkScratch)
	}
	g.keptLock.Lock()
	defer g.keptLock.Unlock()
	best, bestD := -1, 1<<62
	for i, k := range g.kept {
		if k.busy {
			continue
		}
		if d := absInt(k.lastCX-cx) + absInt(k.lastCZ-cz); d < bestD {
			best, bestD = i, d
		}
	}
	if best < 0 {
		s := &chunkScratch{firstOld: map[int64]bs{}}
		g.kept = append(g.kept, s)
		best = len(g.kept) - 1
	}
	s := g.kept[best]
	s.busy, s.lastCX, s.lastCZ = true, cx, cz
	return s
}

func (g *Generator) putScratch(s *chunkScratch) {
	if !g.reuseBases {
		scratchPool.Put(s)
		return
	}
	g.keptLock.Lock()
	s.busy = false
	g.keptLock.Unlock()
}

// SetReuseBases lets workers keep neighbour bases and source results between chunks, in pooled
// scratch. Both are pure functions of the seed, profile and chunk, so this changes speed and never a
// result; but it is per-chunk data kept after a chunk is made, so it is off unless the owner asks.
func (g *Generator) SetReuseBases(on bool) { g.reuseBases = on }

// startDecoration readies the working set around a chunk.
func (s *chunkScratch) startDecoration(g *Generator, cx, cz int) *decoration {
	s.deco.startTarget(g, s, cx, cz)
	return &s.deco
}

// keptSpan is how many chunks across the kept bases and source results span, by position: enough for
// a worker sweeping rows a few dozen chunks wide to find the row above. Without SetReuseBases only the
// 25 a chunk needs are ever built.
const keptSpan = 12
