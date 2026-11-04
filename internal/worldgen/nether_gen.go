package worldgen

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// NetherGenerator generates the Nether (Task 10): Bedrock's for every profile so far (Overjava's own
// Java Nether is not written; see NOTES.md). It holds only data that never changes after NewNether, so
// any number of workers can share it.
type NetherGenerator struct {
	seed     int64
	noise    *netherNoise
	biomes   *netherBiomes
	surfaces [5]netherSurfaceBiome // By Nether biome, from the data; none without it.
	adj      *bedrockSurface       // The adjustment noise, with the data.
}

var (
	sNetherrack = laterState("netherrack")
)

// NewNether returns the Nether generator of a world seed, with the feature data set by
// SetDefaultBedrockData.
func NewNether(seed int64) *NetherGenerator { return NewNetherWithData(seed, defaultBedrockData) }

// NewNetherWithData returns the Nether generator of a world seed and Bedrock's data (nil: plain
// netherrack surfaces).
func NewNetherWithData(seed int64, bd *BedrockData) *NetherGenerator {
	n := &NetherGenerator{seed: seed, noise: newNetherNoise(seed), biomes: newNetherBiomes(seed)}
	if bd != nil {
		n.surfaces = readNetherSurfaces(bd)
		root := xoroFromSeed(seed)
		n.adj = newBedrockSurface(seed, root.forkFactory(), bd)
	}
	return n
}

// biome is the Nether biome at a block column.
func (n *NetherGenerator) biome(x, z int) uint8 { return n.biomes.block(x, z, n.biomes.quart) }

// cells builds a chunk's blocks, index y<<8 | z<<4 | x for Y 0..127.
func (n *NetherGenerator) cells(cx, cz int, out *[netherHeight * 256]bs) {
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
}

// GenerateChunk writes a Nether chunk (Dragonfly's Nether range is Y 0..127).
func (n *NetherGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	var cells [netherHeight * 256]bs
	n.cells(int(pos[0]), int(pos[1]), &cells)
	last, lastID := bs(0), uint32(0)
	for i, s := range cells {
		if s == 0 {
			continue
		}
		if s != last {
			last, lastID = s, runtimeOf(s)
		}
		c.SetBlock(uint8(i&15), int16(i>>8), uint8(i>>4&15), 0, lastID)
	}
	n.storeBiomes(c, int(pos[0]), int(pos[1]))
}

// storeNetherBiomes writes the chunk's biomes: 2D, each quart's column from the biome source.
func (n *NetherGenerator) storeBiomes(c *chunk.Chunk, cx, cz int) {
	var ids [4][4]uint32
	for i := range 4 {
		for k := range 4 {
			ids[i][k] = uint32(biomeRowDefs[n.biomes.quart(cx*4+i, cz*4+k)].EncodeBiome())
		}
	}
	for y := 0; y < netherHeight; y += 16 {
		var section [4][4][4]uint32
		for i := range 4 {
			for j := range 4 {
				for k := range 4 {
					section[i][j][k] = ids[i][k]
				}
			}
		}
		c.SetBiomeSection(int16(y), section)
	}
}

// DefaultSpawn is a place in the Nether; players reach it through portals, which find their own.
func (n *NetherGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 64, 0} }
