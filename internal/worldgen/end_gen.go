package worldgen

import (
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// The End as a dimension of Generator (Task 11), as the Nether is: the same seed, profile, data and
// shared cache, the End's terrain as the base, its decoration (decorationPostProcessChunk 0x5420370:
// spikes, small islands, chorus, gateways) run per source chunk in the same parity machinery, and the
// exit portal added as a player's arrival adds it in BDS.

// endGenerator is what a world's End keeps; it never changes after it is made.
type endGenerator struct {
	noise  *endNoise
	spikes [10]endSpike
}

func newEndGenerator(seed int64) *endGenerator {
	return &endGenerator{noise: newEndNoise(seed), spikes: endSpikes(seed)}
}

const dimEnd = 2

// End returns the generator of this world's End. It is Bedrock's for every profile so far (Overjava's
// Java End is not written; see NOTES.md).
func (g *Generator) End() *Generator {
	n := NewWithData(g.seed, g.profile, g.bdata)
	n.structs = nil // End cities are not placed: their pieces are not read.
	n.cache = g.cache
	n.dim = dimEnd
	n.endN = newEndGenerator(g.seed)
	return n
}

// parityDeco says whether decoration runs in Bedrock's parity order: the Bedrock profile with data, and
// the End always (its decoration is Bedrock's code, with no data).
func (g *Generator) parityDeco() bool { return g.dim == dimEnd || g.bedrockDeco() }

// buildEndBase builds an End chunk's base in the Overworld's cell layout: end stone in Y 0..127, air
// elsewhere. Only whole chunks are built.
func (g *Generator) buildEndBase(b *baseChunk) {
	var cells [endHeight * 256]bs
	g.endN.noise.fill(b.cx, b.cz, &cells, sEndStone)
	for i, s := range cells {
		if s != 0 {
			b.b[cellIndex(i&15, i>>8, i>>4&15)] = s
		}
	}
	b.computeTops(0, 15, 0, 15)
}

// writeEndCells writes an End chunk's Y 0..255 from the cell layout, with the End biome throughout.
func writeEndCells(c *chunk.Chunk, cells *[chunkCells]bs) {
	last, lastID := bs(0), uint32(0)
	for y := range 256 {
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
	id := uint32(biome.End{}.EncodeBiome())
	var section [4][4][4]uint32
	for i := range 4 {
		for j := range 4 {
			for k := range 4 {
				section[i][j][k] = id
			}
		}
	}
	for y := 0; y < 256; y += 16 {
		c.SetBiomeSection(int16(y), section)
	}
}

// endPodiumTop is the Y of the highest block at (0, 0), on which the exit portal stands (BDS records
// ExitPortalLocation = top + 1 when a player first arrives). Decoration never reaches that column, so
// the terrain's is the world's.
func (g *Generator) endPodiumTop(s *chunkScratch) int {
	buf := s.takeBuffer()
	defer s.giveBuffer(buf)
	g.cachedBase(s, 0, 0).expandCells(&buf.b)
	for y := 255; y > 0; y-- {
		if buf.b[cellIndex(0, y, 0)] != sAir {
			return y
		}
	}
	return 0
}

// placeEndPodium writes the inactive exit portal into a chunk's cells. BDS builds it when the first
// player enters the End (EndPodiumFeature 0xd9b68a0, flag 0); with no dragon fight yet it is placed
// with the chunk. The lowest layer is from the BDS world (structures notes, phase 3 End): it replaces
// the top block, bedrock within 2.5 of the axis and end stone out to 3.5. The rest is Java's layout,
// which that layer matches, assumed: air above it out to 3.5 for 32 rows, a bedrock rim at the portal
// layer (air inside, the portal blocks come when the dragon dies), a four-high bedrock pillar and
// four torches on it.
func placeEndPodium(cells *[chunkCells]bs, cx, cz, top int) {
	if cx < -1 || cx > 0 || cz < -1 || cz > 0 {
		return
	}
	py := top + 1
	set := func(x, y, z int, s bs) {
		if x>>4 == cx && z>>4 == cz && y >= 0 && y < 256 {
			cells[cellIndex(x&15, y, z&15)] = s
		}
	}
	for x := -4; x <= 4; x++ {
		for z := -4; z <= 4; z++ {
			d := x*x + z*z
			if 4*d > 49 { // Beyond 3.5.
				continue
			}
			inner := 4*d <= 25 // Within 2.5.
			for y := py - 1; y <= py+32; y++ {
				switch {
				case y < py && inner:
					set(x, y, z, sEndBedrock)
				case y < py:
					set(x, y, z, sEndStone)
				case y > py:
					set(x, y, z, sAir)
				case !inner:
					set(x, y, z, sEndBedrock)
				default:
					set(x, y, z, sAir)
				}
			}
		}
	}
	for y := py; y < py+4; y++ {
		set(0, y, 0, sEndBedrock)
	}
	set(0, py+2, -1, sTorchNorth)
	set(1, py+2, 0, sTorchEast)
	set(0, py+2, 1, sTorchSouth)
	set(-1, py+2, 0, sTorchWest)
}
