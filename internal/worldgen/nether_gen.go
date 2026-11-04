package worldgen

import (
	"github.com/df-mc/dragonfly/server/world/chunk"
)

var sNetherrack = laterState("netherrack")

// netherGen is what a world's Nether keeps (Task 10): its noises, biome source, surface builders and the
// seeds its cave carver starts from. It never changes once made, so any number of workers can share it.
type netherGen struct {
	seed     int64
	noise    *netherNoise
	biomes   *netherBiomes
	surfaces [5]netherSurfaceBiome // By Nether biome: the install's, or the built-in copy without one.
	adj      *bedrockSurface       // The adjustment noise.
	seeds    bedrockSeeds          // The decoration seeds the cave carver starts from.
}

// newNetherGen makes a world's Nether. Everything it builds is code, so it needs no data: with the
// install's biome files the surfaces are read from them, without them the same builders are built in
// (netherSurfacesBuiltIn, the values the study read from those files).
func newNetherGen(seed int64, bd *BedrockData) *netherGen {
	n := &netherGen{seed: seed, noise: newNetherNoise(seed), biomes: newNetherBiomes(seed), seeds: newBedrockSeeds(seed)}
	n.adj = &bedrockSurface{adjust: n.noise.adjust}
	if bd != nil {
		n.surfaces = readNetherSurfaces(seed, bd)
	} else {
		n.surfaces = netherSurfacesBuiltIn(seed)
	}
	return n
}

// biome is the Nether biome at a block column.
func (n *netherGen) biome(x, z int) uint8 { return n.biomes.block(x, z, n.biomes.quart) }

// cells builds a chunk's base, index y<<8 | z<<4 | x for Y 0..127: terrain, bedrock rows, surface,
// caves.
func (n *netherGen) cells(cx, cz int, out *[netherHeight * 256]bs) {
	clear(out[:])
	n.noise.fill(cx, cz, out, sNetherrack, sLava)
	r := netherBedrock(cx, cz, out, sBedrock)
	depth := n.noise.depthNoise(cx, cz)
	for lx := range 16 {
		for lz := range 16 {
			x, z := cx*16+lx, cz*16+lz
			b := n.biome(x, z)
			n.netherColumn(out, r, x, z, &n.surfaces[b-bNetherWastes], depth[lz*16+lx])
		}
	}
	n.carveCaves(out, cx, cz, n.seeds)
}

// storeBiomes writes the chunk's biomes: 2D, each quart's column from the biome source.
func (n *netherGen) storeBiomes(c *chunk.Chunk, cx, cz int) {
	var section [4][4][4]uint32
	for i := range 4 {
		for k := range 4 {
			id := uint32(biomeRowDefs[n.biomes.quart(cx*4+i, cz*4+k)].EncodeBiome())
			for j := range 4 {
				section[i][j][k] = id
			}
		}
	}
	for y := 0; y < netherHeight; y += 16 {
		c.SetBiomeSection(int16(y), section)
	}
}

// storeUniformBiome writes one biome over a chunk's rows from Y 0.
func storeUniformBiome(c *chunk.Chunk, id uint32, height int) {
	var section [4][4][4]uint32
	for i := range 4 {
		for j := range 4 {
			for k := range 4 {
				section[i][j][k] = id
			}
		}
	}
	for y := 0; y < height; y += 16 {
		c.SetBiomeSection(int16(y), section)
	}
}

// Nether returns the generator of this world's Nether: the same seed, profile, data and shared cache,
// the Nether's terrain, biomes, surface and caves, and its decoration: the code's (springs, fire,
// glowstone, mushrooms, ores) always, and the install's rules (fungi, roots, vines, deltas, basalt
// columns, soul sand) only with an install read, so not in Overjava, which reads none. Overjava's own
// Java Nether is not written (NOTES.md). Nether structures are not placed yet.
func (g *Generator) Nether() *Generator {
	n := g.dimension(dimNether)
	n.netherN = newNetherGen(g.seed, g.bdata)
	return n
}

// buildNetherBase builds a Nether chunk's base in the Overworld's cell layout: the Nether's Y 0..127 in
// place, air above and below. Only whole chunks are built.
func (g *Generator) buildNetherBase(b *baseChunk) {
	var cells [netherHeight * 256]bs
	g.netherN.cells(b.cx, b.cz, &cells)
	for i, s := range cells {
		b.b[cellIndex(i&15, i>>8, i>>4&15)] = s
	}
	b.computeTops(0, 15, 0, 15)
}

// writeRows writes a Nether or End chunk's Y 0..height-1 from the cell layout.
func writeRows(c *chunk.Chunk, cells *[chunkCells]bs, height int) {
	last, lastID := bs(0), uint32(0)
	for y := range height {
		for i := range 256 {
			s := cells[cellIndex(i&15, y, i>>4)]
			if s == 0 {
				continue
			}
			if s != last {
				last, lastID = s, runtimeOf(s)
			}
			c.SetBlock(uint8(i&15), int16(y), uint8(i>>4), 0, lastID)
		}
	}
}
