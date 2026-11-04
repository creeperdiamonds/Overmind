package worldgen

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

// End returns the generator of this world's End: Bedrock's for every profile, with or without data (its
// generation reads no data). Overjava's own Java End is not written (NOTES.md). End cities are not
// placed: their pieces are not read.
func (g *Generator) End() *Generator {
	n := g.dimension(dimEnd)
	n.endN = newEndGenerator(g.seed)
	return n
}

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

// The End's arrival platform. Java builds it when an entity arrives (ServerLevel.makeObsidianPlatform):
// 5 x 5 of obsidian under (100, 49, 0) with three rows of air over it. Bedrock's rule was not read, and
// nothing here moves a player to the End yet; it is placed with the chunks so the End's spawn stands on
// something (assumed, Java's).
const endPlatformX, endPlatformY, endPlatformZ = 100, 49, 0

// endPlatformChunk says whether the platform reaches a chunk.
func endPlatformChunk(cx, cz int) bool {
	return cx == (endPlatformX-2)>>4 && (cz == (endPlatformZ-2)>>4 || cz == (endPlatformZ+2)>>4)
}

// placeEndPlatform writes the platform's cells that fall in a chunk.
func placeEndPlatform(cells *[chunkCells]bs, cx, cz int) {
	for x := endPlatformX - 2; x <= endPlatformX+2; x++ {
		for z := endPlatformZ - 2; z <= endPlatformZ+2; z++ {
			if x>>4 != cx || z>>4 != cz {
				continue
			}
			for dy := -1; dy <= 2; dy++ {
				s := sAir
				if dy == -1 {
					s = sObsidian
				}
				cells[cellIndex(x&15, endPlatformY+dy, z&15)] = s
			}
		}
	}
}

// endPodiumChunk says whether the exit portal reaches a chunk: it spans X and Z -4..4.
func endPodiumChunk(cx, cz int) bool { return cx >= -1 && cx <= 0 && cz >= -1 && cz <= 0 }

// placeEndPodium writes the inactive exit portal into a chunk's cells. BDS builds it when the first
// player enters the End (EndPodiumFeature 0xd9b68a0, flag 0); with no dragon fight yet it is placed
// with the chunk. The lowest layer is from the BDS world (structures notes, phase 3 End): it replaces
// the top block, bedrock within 2.5 of the axis and end stone out to 3.5. The rest is Java's layout,
// which that layer matches, assumed: air above it out to 3.5 for 32 rows, a bedrock rim at the portal
// layer (air inside, the portal blocks come when the dragon dies), a four-high bedrock pillar and
// four torches on it.
func placeEndPodium(cells *[chunkCells]bs, cx, cz, top int) {
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
