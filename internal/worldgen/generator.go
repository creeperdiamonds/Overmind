// Package worldgen is Overmind's Overworld generator. Its seeding, noise, climate, biome lookup,
// splines, density, noise caves and aquifers follow Mojang's algorithms, per the owner's decision, so
// that a seed gives the same biomes and terrain shape as Mojang's. Carvers, ores and decoration are
// our own designs on top. See NOTES.md.
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
	zoomSeed   int64
	climate    climateNoises
	caves      caveNoises
	aquiferFac seedFactory
	surface    surfaceNoises
	blocks     blocks
	// bandTable is the world's terracotta band pattern.
	bandTable [bandTableSize]uint8
	// Seeds for our own position-based chances.
	strataSeed, veinSeed, diskSeed, carveSeed, lakeSeed, featureSeed, treeSeed uint64
}

// New returns a Generator for a seed.
func New(seed int64) *Generator {
	sub := func(id uint64) uint64 { return mix64(uint64(seed) ^ mix64(id)) }
	root := xoroFromSeed(seed)
	w := root.forkFactory()
	aquifer := w.named("minecraft:aquifer")
	return &Generator{
		zoomSeed:    biomeZoomSeed(seed),
		climate:     newClimateNoises(w),
		caves:       newCaveNoises(w),
		aquiferFac:  aquifer.forkFactory(),
		surface:     newSurfaceNoises(w),
		blocks:      newBlocks(),
		bandTable:   newBandTable(sub(7)),
		strataSeed:  sub(20),
		veinSeed:    sub(21),
		diskSeed:    sub(23),
		carveSeed:   sub(24),
		lakeSeed:    sub(25),
		featureSeed: sub(26),
		treeSeed:    sub(27),
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
	carved      [carvedSize][carvedSize]carveMask // The chunk and treeMargin columns around it.
	lakes       []lake
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

var scratchPool = sync.Pool{New: func() any { return new(chunkScratch) }}

// GenerateChunk runs noise (stone, caves, fluids, ore veins), then biomes, surface, carvers,
// underground features, and trees, snow and plants. The game does biomes before noise; ours can do
// them after, because density never reads a stored biome. Everything is worked out from the seed and
// the chunk position, with no look at neighbouring chunks.
func (g *Generator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	originX, originZ := int(pos[0])*16, int(pos[1])*16
	s := scratchPool.Get().(*chunkScratch)
	defer scratchPool.Put(s)
	s.startChunk(g, originX, originZ)

	var top [16][16]int16
	g.fillNoise(c, s, originX, originZ, &top)
	noiseTop := top

	var cols [4][4]biomeColumn
	g.fillBiomes(c, originX, originZ, &cols)
	g.applySurface(c, originX, originZ, &top, &cols)
	g.placeDisks(c, int(pos[0]), int(pos[1]), &top)
	highest := minY
	for x := range top {
		for z := range top[x] {
			highest = max(highest, int(top[x][z]))
		}
	}
	g.applyCarvers(c, s, originX, originZ, &noiseTop)
	g.applyLakes(c, s, originX, originZ, highest)
	g.placeGeodes(c, int(pos[0]), int(pos[1]))
	g.placeVeins(c, pos, highest, true)
	g.placeVeins(c, pos, highest, false)
	g.placeSprings(c, int(pos[0]), int(pos[1]))
	g.decorate(c, s, originX, originZ, &top, &cols)
}

// Kinds of noise cell, worked out once for all the block columns that share it.
const (
	cellUnknown uint8 = iota
	cellSky           // No corner can be solid and it is over the skip height: blocks take the global fluid.
	cellSolid         // Every corner is solid, with no noodle or vein possible.
	cellMixed
)

// fillNoise is the noise pass. A cell's corners are read only if some block in it needs its density.
// It works down each block column so that fluids can see what lies above them.
func (g *Generator) fillNoise(c *chunk.Chunk, s *chunkScratch, originX, originZ int, top *[16][16]int16) {
	for i := range cellsAcross + 1 {
		for j := range cellsAcross + 1 {
			s.shapes[i][j] = g.shapeAt(originX+i*cellXZ, originZ+j*cellXZ)
			for n := range columnNodes {
				s.nodes[i][j][n].haveCore, s.nodes[i][j][n].haveNoodle, s.nodes[i][j][n].haveVein = false, false, false
			}
		}
	}
	skip := g.skipAbove(&s.aq)
	var kinds [cellsAcross][cellsAcross][columnNodes - 1]uint8
	var noodles [cellsAcross][cellsAcross][columnNodes - 1]bool
	var veins [cellsAcross][cellsAcross][columnNodes - 1]bool
	for lx := range 16 {
		i, dx := lx/cellXZ, lx%cellXZ
		fx := float64(dx) / 4.0
		wx := originX + lx
		for lz := range 16 {
			j, dz := lz/cellXZ, lz%cellXZ
			fz := float64(dz) / 4.0
			wz := originZ + lz
			colTop := minY - 1
			prevWater := false
			for l := columnNodes - 2; l >= 0; l-- {
				baseY := minY + l*cellY
				kind := kinds[i][j][l]
				if kind == cellUnknown {
					kind, noodles[i][j][l], veins[i][j][l] = g.classifyCell(s, originX, originZ, i, j, l, skip)
					kinds[i][j][l] = kind
				}
				switch kind {
				case cellSky:
					if baseY >= seaFluidTop && !prevWater {
						continue // Open air, which is what a new chunk already holds.
					}
					for dy := cellY - 1; dy >= 0; dy-- {
						y := baseY + dy
						colTop, prevWater = g.setFluid(c, globalFluid(y).at(y), uint8(lx), y, uint8(lz), wx, wz, colTop, prevWater)
					}
				case cellSolid:
					for dy := cellY - 1; dy >= 0; dy-- {
						y := baseY + dy
						c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, g.solidBlockAt(wx, y, wz, 0))
					}
					colTop = max(colTop, baseY+cellY-1)
					prevWater = false
				default:
					corners := s.cellAt(i, j, l)
					blend := corners.blend(fx, fz, noodles[i][j][l], veins[i][j][l])
					for dy := cellY - 1; dy >= 0; dy-- {
						y := baseY + dy
						fy := float64(dy) / 8.0
						f := blend.final(fy)
						if f > 0 {
							c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, g.solidBlockAt(wx, y, wz, blend.veinAt(fy)))
							colTop = max(colTop, y)
							prevWater = false
							continue
						}
						colTop, prevWater = g.setFluid(c, g.fluidAt(&s.aq, wx, y, wz, f), uint8(lx), y, uint8(lz), wx, wz, colTop, prevWater)
					}
				}
			}
			top[lx][lz] = int16(colTop)
		}
	}
}

// setFluid writes a block that the density left open. Water never hangs over air: the game lets it
// flow later; a generated block here would stay hanging, so the wall closes under it instead. This
// sealing is our own and is not in the game's noise stage.
func (g *Generator) setFluid(c *chunk.Chunk, kind fillKind, lx uint8, y int, lz uint8, wx, wz, colTop int, prevWater bool) (int, bool) {
	b := &g.blocks
	switch kind {
	case fillAir:
		if prevWater {
			c.SetBlock(lx, int16(y+1), lz, 0, g.stoneAt(wx, y+1, wz))
			colTop = max(colTop, y+1)
		}
		return colTop, false
	case fillWater:
		c.SetBlock(lx, int16(y), lz, 0, b.water)
		return colTop, true
	case fillLava:
		c.SetBlock(lx, int16(y), lz, 0, b.lava)
		return colTop, false
	}
	c.SetBlock(lx, int16(y), lz, 0, g.stoneAt(wx, y, wz))
	return max(colTop, y), false
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
