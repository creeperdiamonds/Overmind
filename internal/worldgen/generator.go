// Package worldgen is Overmind's own vanilla-like Overworld generator.
package worldgen

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// World limits. The study of Java 26.1 gives Y -64 to 319 and sea level 63. Our water
// reaches and includes Y 63, while Java's top water block is Y 62.
const (
	minY     = -64
	maxY     = 319
	SeaLevel = 63
)

// Generator builds Overworld chunks from a seed. It holds only data that never changes
// after New, so any number of workers can share it.
type Generator struct {
	climate climateNoises
	terrain terrainNoises
	jagged  *layered
	surface surfaceNoises
	caves   caveNoises
	blocks  blocks
	// bandTable is the world's terracotta band pattern.
	bandTable [bandTableSize]uint8
	// Seeds for position-based chances.
	strataSeed, veinSeed, aquiferSeed, diskSeed, carveSeed, lakeSeed, featureSeed uint64
}

// New returns a Generator for a seed.
func New(seed int64) *Generator {
	sub := func(id uint64) uint64 { return mix64(uint64(seed) ^ mix64(id)) }
	return &Generator{
		climate: newClimateNoises(seed),
		terrain: newTerrainNoises(seed),
		// First octave -16 at xz scale 1500, from the study. The game lists 16 equal
		// amplitudes; finer than 6 octaves falls inside one grid cell.
		jagged:      newLayered(sub(6), 0, []float64{1, 1, 1, 1, 1, 1}, gameSpread(make16Ones())),
		surface:     newSurfaceNoises(seed),
		caves:       newCaveNoises(seed),
		blocks:      newBlocks(),
		bandTable:   newBandTable(sub(7)),
		strataSeed:  sub(20),
		veinSeed:    sub(21),
		aquiferSeed: sub(22),
		diskSeed:    sub(23),
		carveSeed:   sub(24),
		lakeSeed:    sub(25),
		featureSeed: sub(26),
	}
}

// chunkScratch holds working space for one GenerateChunk call. It goes back in a pool, and
// nothing in it is read before it is written, so no per-chunk data carries over.
type chunkScratch struct {
	shapes [cellsAcross + 1][cellsAcross + 1]columnShape
	nodes  [cellsAcross + 1][cellsAcross + 1][columnNodes]node
	aq     aquiferCache
	carved [16][16]carveMask
	lakes  []lake
}

const cellsAcross = 16 / cellXZ

var scratchPool = sync.Pool{New: func() any { return new(chunkScratch) }}

// GenerateChunk runs the passes in the game's order: noise (stone, caves, fluids, ore
// veins), biomes, surface, carvers, then decoration. Everything is worked out from the seed
// and the chunk position, with no look at neighbouring chunks.
func (g *Generator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	originX, originZ := int(pos[0])*16, int(pos[1])*16
	s := scratchPool.Get().(*chunkScratch)
	defer scratchPool.Put(s)
	s.aq.reset(originX, originZ)

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
	g.applyCarvers(c, s, originX, originZ, &noiseTop, &cols)
	g.applyLakes(c, s, originX, originZ, highest)
	g.placeGeodes(c, int(pos[0]), int(pos[1]))
	g.placeVeins(c, pos, highest, true)
	g.placeVeins(c, pos, highest, false)
	g.placeSprings(c, int(pos[0]), int(pos[1]))
}

// fillNoise is the noise pass. It works down each block column so that fluids can see what
// lies above them.
func (g *Generator) fillNoise(c *chunk.Chunk, s *chunkScratch, originX, originZ int, top *[16][16]int16) {
	for i := range cellsAcross + 1 {
		for j := range cellsAcross + 1 {
			x, z := originX+i*cellXZ, originZ+j*cellXZ
			s.shapes[i][j] = g.shapeAt(x, z)
			for n := range columnNodes {
				s.nodes[i][j][n] = g.nodeAt(&s.shapes[i][j], x, minY+n*cellY, z)
			}
		}
	}
	// Cell summaries: all corners solid and no noodle, or all corners air.
	var allSolid, allAir [cellsAcross][cellsAcross][columnNodes - 1]bool
	for i := range cellsAcross {
		for j := range cellsAcross {
			for l := range columnNodes - 1 {
				solid, air := true, true
				for a := range 2 {
					for b := range 2 {
						for k := range 2 {
							n := &s.nodes[i+a][j+b][l+k]
							solid = solid && n.density > 0 && n.toggle <= 0 && n.vein == 0
							air = air && n.density <= 0
						}
					}
				}
				allSolid[i][j][l], allAir[i][j][l] = solid, air
			}
		}
	}
	b := &g.blocks
	for lx := range 16 {
		for lz := range 16 {
			i, j := lx/cellXZ, lz/cellXZ
			dx, dz := lx-i*cellXZ, lz-j*cellXZ
			tx, tz := float64(dx)/cellXZ, float64(dz)/cellXZ
			wx, wz := originX+lx, originZ+lz
			ground := bilerp(s.shapes[i][j].ground, s.shapes[i+1][j].ground, s.shapes[i][j+1].ground, s.shapes[i+1][j+1].ground, tx, tz)
			colTop := minY - 1
			prevWater := false
			for l := columnNodes - 2; l >= 0; l-- {
				baseY := minY + l*cellY
				if allAir[i][j][l] && baseY > SeaLevel && float64(baseY) > ground-surfaceFluidMargin && !prevWater {
					continue // Open air, which is what a new chunk already holds.
				}
				solidCell := allSolid[i][j][l]
				var blend layerBlend
				if !solidCell {
					cn := cellNodes{}
					for a := range 2 {
						for k := range 2 {
							cn.lo[a][k], cn.hi[a][k] = &s.nodes[i+a][j+k][l], &s.nodes[i+a][j+k][l+1]
						}
					}
					blend = cn.blend(tx, tz)
				}
				for dy := cellY - 1; dy >= 0; dy-- {
					y := baseY + dy
					ty := float64(dy) / cellY
					if solidCell || blend.solidAt(ty) {
						vein := 0.0
						if !solidCell {
							vein = lerp(blend.v0, blend.v1, ty)
						}
						c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, g.solidBlockAt(wx, y, wz, vein))
						colTop = max(colTop, y)
						prevWater = false
						continue
					}
					kind := g.fillAt(&s.aq, wx, y, wz, ground, colTop < minY)
					switch kind {
					case fillAir:
						if prevWater {
							// Water never hangs over air. The game lets it flow later; a generated
							// block here would stay hanging, so the wall closes under it instead.
							c.SetBlock(uint8(lx), int16(y+1), uint8(lz), 0, g.stoneAt(wx, y+1, wz))
							colTop = max(colTop, y+1)
						}
						prevWater = false
					case fillWater:
						c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, b.water)
						prevWater = true
					case fillLava:
						c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, b.lava)
						prevWater = false
					case fillSolid:
						c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, g.stoneAt(wx, y, wz))
						colTop = max(colTop, y)
						prevWater = false
					}
				}
			}
			top[lx][lz] = int16(colTop)
		}
	}
}

// DefaultSpawn searches outward from the origin for dry inland ground with |weirdness| at
// least 0.16, which is what the study says the game's spawn search aims for.
func (g *Generator) DefaultSpawn(world.Dimension) cube.Pos {
	const step, reach = 32, 2048
	for r := 0; r <= reach; r += step {
		for a := -r; a <= r; a += step {
			for _, p := range [4][2]int{{a, -r}, {a, r}, {-r, a}, {r, a}} {
				cl := g.Climate(p[0], p[1])
				if cl.Continentalness < nearBelow || cl.Weirdness > -0.16 && cl.Weirdness < 0.16 {
					continue
				}
				if h := g.SurfaceHeight(p[0], p[1]); h >= SeaLevel {
					return cube.Pos{p[0], h + 1, p[1]}
				}
			}
		}
	}
	return cube.Pos{0, max(g.SurfaceHeight(0, 0), SeaLevel) + 1, 0}
}

// make16Ones is the jagged noise's octave list in the game.
func make16Ones() []float64 {
	v := make([]float64, 16)
	for i := range v {
		v[i] = 1
	}
	return v
}
