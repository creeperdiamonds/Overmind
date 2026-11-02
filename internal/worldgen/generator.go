// Package worldgen is Overmind's own vanilla-like Overworld generator.
package worldgen

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// World limits, from minecraft.wiki "Noise settings": min_y -64, height 384, sea level 63.
const (
	minY     = -64
	maxY     = 319
	SeaLevel = 63
)

// Generator builds Overworld chunks from a seed. It holds only data that never changes
// after New, so any number of workers can share it.
type Generator struct {
	climate  climateNoises
	overhang *fractal
	patch    *fractal // Patches of surface material.
	blocks   blocks
}

// New returns a Generator for a seed.
func New(seed int64) *Generator {
	return &Generator{
		climate: newClimateNoises(seed),
		// Own tuning; the wiki publishes no noise settings. Taller than wide would
		// make spires, so wide bumps with a shorter vertical span make ledges.
		overhang: newFractal(mix64(uint64(seed)^mix64(6)), 96, 64, 4),
		// Own tuning: patches a few dozen blocks across.
		patch:  newFractal(mix64(uint64(seed)^mix64(7)), 40, 40, 3),
		blocks: newBlocks(),
	}
}

// GenerateChunk fills a chunk with stone, then water up to sea level, leaving air above.
// It then stores biomes and covers the ground with each biome's surface blocks.
func (g *Generator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	const cellsAcross = 16 / cellXZ
	originX, originZ := int(pos[0])*16, int(pos[1])*16

	// The grid is on the stack, so nothing outlives this call.
	var top [16][16]int16
	for i := range top {
		for j := range top[i] {
			top[i][j] = minY - 1
		}
	}
	var grid [cellsAcross + 1][cellsAcross + 1][columnNodes]float64
	for i := range grid {
		for j := range grid[i] {
			g.fillColumn(originX+i*cellXZ, originZ+j*cellXZ, &grid[i][j])
		}
	}

	for i := range cellsAcross {
		for j := range cellsAcross {
			for layer := range columnNodes - 1 {
				corners := cell{
					{{grid[i][j][layer], grid[i][j][layer+1]}, {grid[i][j+1][layer], grid[i][j+1][layer+1]}},
					{{grid[i+1][j][layer], grid[i+1][j][layer+1]}, {grid[i+1][j+1][layer], grid[i+1][j+1][layer+1]}},
				}
				g.fillCell(c, &top, &corners, i*cellXZ, j*cellXZ, minY+layer*cellY)
			}
		}
	}

	var cols [4][4]biomeColumn
	g.fillBiomes(c, originX, originZ, &cols)
	g.applySurface(c, originX, originZ, &top, &cols)
}

// fillCell writes one grid cell into the chunk, and raises top to the highest stone.
func (g *Generator) fillCell(c *chunk.Chunk, top *[16][16]int16, corners *cell, baseX, baseZ, baseY int) {
	same, solid := corners.uniform()
	if same && !solid && baseY > SeaLevel {
		return // Open air is what a new chunk already holds.
	}
	for dx := range cellXZ {
		for dz := range cellXZ {
			for dy := range cellY {
				y := baseY + dy
				rid := g.blocks.water
				switch {
				case same && solid, !same && corners.solidAt(dx, dz, dy):
					rid = g.blocks.stone
					top[baseX+dx][baseZ+dz] = max(top[baseX+dx][baseZ+dz], int16(y))
				case y > SeaLevel:
					continue
				}
				c.SetBlock(uint8(baseX+dx), int16(y), uint8(baseZ+dz), 0, rid)
			}
		}
	}
}

// DefaultSpawn returns a placeholder above the origin column. Task 5 finds real dry land.
func (g *Generator) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{0, max(g.SurfaceHeight(0, 0), SeaLevel) + 1, 0}
}
