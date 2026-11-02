package worldgen

import (
	"math"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// veinBlock names a block the vein pass places. Ores come first, then rock patches.
type veinBlock uint8

const (
	veinCoal veinBlock = iota
	veinIron
	veinCopper
	veinGold
	veinRedstone
	veinLapis
	veinDiamond
	veinEmerald
	veinGranite
	veinDiorite
	veinAndesite
	veinTuff
	numVeinBlocks
)

// Kinds of block a vein can replace, as bits of a mask.
const (
	repStone uint8 = 1 << iota
	repGranite
	repDiorite
	repAndesite
	repTuff
	repDeepslate

	repStoneLike = repStone | repGranite | repDiorite | repAndesite | repTuff | repDeepslate
)

type distribution uint8

const (
	distUniform  distribution = iota
	distTriangle              // Most common at peak, thinning to nothing at both ends.
)

// biomeRule limits a feature to some biomes.
type biomeRule uint8

const (
	biomeAny          biomeRule = iota
	biomeNotDripstone           // Anywhere but dripstone caves.
	biomeDripstone              // Dripstone caves only.
	biomeBadlands               // Badlands surface biome only.
	biomeMountain               // The mountain biomes the Emerald Ore page lists.
)

// veinFeature is one row of the tables below: a batch of blobs placed in each chunk.
// Attempts below one is a chance. maxSize is the largest blob, in blocks; each blob has
// a size from 1 to maxSize. exposed is the chance that a block touching air is dropped.
type veinFeature struct {
	block      veinBlock
	attempts   float64
	maxSize    int
	minY, maxY int
	dist       distribution
	peak       int
	exposed    float64
	replace    uint8
	biome      biomeRule
	rock       bool // Rock patches go in before the caves are carved, ores after.
}

// veinFeatures is the table. Numbers come from minecraft.wiki, read 2026-11-03: the
// "Natural generation" sections of the Coal Ore, Iron Ore, Copper Ore, Gold Ore, Redstone
// Ore, Lapis Lazuli Ore, Diamond Ore, Emerald Ore, Tuff, Granite, Diorite and Andesite
// pages (the "Ore" page itself holds only a summary of heights). The wiki states each
// attempt count, the largest blob ("blobs of 0-37"), the Y range and where an ore is
// "most common"; a triangle with that peak is our reading of "most common". Where the page
// gives no distribution (diamond) the ore is spread evenly, and where it gives no air rule
// (coal upper, iron, copper, redstone, emerald) nothing is dropped.
//
// Our own tuning, not on the wiki pages: the sizes of rock patches (the Granite, Diorite
// and Andesite pages give only Bedrock Edition numbers, "size 0-864", which are not
// block counts we can use, so patches here are up to 100 blocks), what size inside 0-N a
// blob gets (even from 1 to N), and the blob shape.
//
// Edition note: Granite, Diorite and Andesite use the Bedrock Edition numbers on their
// pages (2 attempts at Y 0 to 60, then a one in six chance at Y 64 to 128), because the
// Java sections give none. Diorite stays out of Y below 0, as the Diorite page's history
// says.
var veinFeatures = [...]veinFeature{
	// Coal Ore page.
	{block: veinCoal, attempts: 30, maxSize: 37, minY: 136, maxY: 320, replace: repStoneLike},
	{block: veinCoal, attempts: 20, maxSize: 37, minY: 0, maxY: 192, dist: distTriangle, peak: 96, exposed: 0.5, replace: repStoneLike},
	// Iron Ore page: upper, middle and small batches.
	{block: veinIron, attempts: 90, maxSize: 13, minY: 80, maxY: 384, dist: distTriangle, peak: 232, replace: repStoneLike},
	{block: veinIron, attempts: 10, maxSize: 13, minY: -24, maxY: 56, dist: distTriangle, peak: 16, replace: repStoneLike},
	{block: veinIron, attempts: 10, maxSize: 5, minY: -64, maxY: 72, replace: repStoneLike},
	// Copper Ore page: the second batch is the only one in dripstone caves, with larger blobs.
	{block: veinCopper, attempts: 16, maxSize: 16, minY: -16, maxY: 112, dist: distTriangle, peak: 48, replace: repStoneLike, biome: biomeNotDripstone},
	{block: veinCopper, attempts: 16, maxSize: 52, minY: -16, maxY: 112, dist: distTriangle, peak: 48, replace: repStoneLike, biome: biomeDripstone},
	// Gold Ore page: normal, deep, and the badlands batch that ignores the air rule.
	{block: veinGold, attempts: 4, maxSize: 13, minY: -64, maxY: 32, dist: distTriangle, peak: -16, exposed: 0.5, replace: repStoneLike},
	{block: veinGold, attempts: 0.5, maxSize: 13, minY: -64, maxY: -48, exposed: 0.5, replace: repStoneLike},
	{block: veinGold, attempts: 50, maxSize: 13, minY: 32, maxY: 256, replace: repStoneLike, biome: biomeBadlands},
	// Redstone Ore page: the second batch is more common the lower it goes.
	{block: veinRedstone, attempts: 4, maxSize: 10, minY: -64, maxY: 15, replace: repStoneLike},
	{block: veinRedstone, attempts: 8, maxSize: 10, minY: -63, maxY: -32, dist: distTriangle, peak: -63, replace: repStoneLike},
	// Lapis Lazuli Ore page: the second batch never touches air.
	{block: veinLapis, attempts: 2, maxSize: 10, minY: -32, maxY: 32, dist: distTriangle, peak: 0, replace: repStoneLike},
	{block: veinLapis, attempts: 4, maxSize: 10, minY: -64, maxY: 64, exposed: 1, replace: repStoneLike},
	// Diamond Ore page: four batches. The second is the rare large one, one chance in nine.
	{block: veinDiamond, attempts: 7, maxSize: 5, minY: -63, maxY: 16, exposed: 0.5, replace: repStoneLike},
	{block: veinDiamond, attempts: 1.0 / 9, maxSize: 23, minY: -63, maxY: 16, exposed: 0.7, replace: repStoneLike},
	{block: veinDiamond, attempts: 4, maxSize: 10, minY: -63, maxY: 16, exposed: 1, replace: repStoneLike},
	{block: veinDiamond, attempts: 2, maxSize: 10, minY: -63, maxY: -4, exposed: 0.5, replace: repStoneLike},
	// Emerald Ore page: scattered, only in mountain biomes, peak at Y 232.
	{block: veinEmerald, attempts: 100, maxSize: 3, minY: -16, maxY: 320, dist: distTriangle, peak: 232, replace: repStoneLike, biome: biomeMountain},

	// Rock patches. Tuff page: 2 attempts, Y -64 to 0. Granite, Diorite and Andesite pages
	// (Bedrock Edition numbers): 2 attempts at Y 0 to 60, and 1/6 at Y 64 to 128.
	{block: veinTuff, attempts: 2, maxSize: rockSize, minY: -64, maxY: 0, replace: repStone | repDeepslate, rock: true},
	{block: veinGranite, attempts: 2, maxSize: rockSize, minY: 0, maxY: 60, replace: repStone, rock: true},
	{block: veinGranite, attempts: 1.0 / 6, maxSize: rockSize, minY: 64, maxY: 128, replace: repStone, rock: true},
	{block: veinDiorite, attempts: 2, maxSize: rockSize, minY: 0, maxY: 60, replace: repStone, rock: true},
	{block: veinDiorite, attempts: 1.0 / 6, maxSize: rockSize, minY: 64, maxY: 128, replace: repStone, rock: true},
	{block: veinAndesite, attempts: 2, maxSize: rockSize, minY: 0, maxY: 60, replace: repStone, rock: true},
	{block: veinAndesite, attempts: 1.0 / 6, maxSize: rockSize, minY: 64, maxY: 128, replace: repStone, rock: true},
}

// rockSize is the largest rock patch in blocks. Own tuning, see above.
const rockSize = 100

// maxStretch bounds how much longer than its radius a blob can be along one axis: the
// longest stretch is 1.25 and the volume factor cbrt(1.25*0.75*0.75) is about 0.89.
const maxStretch = 1.25 / 0.88

// blobRadius is the radius of a round blob of each size, indexed by size. A blob's
// volume is about 4/3*pi*r^3, and the 1.15 makes up for the ragged edge.
var blobRadius = func() (r [rockSize + 1]float64) {
	for size := 1; size <= rockSize; size++ {
		r[size] = math.Cbrt(3*float64(size)/(4*math.Pi)) * 1.15
	}
	return
}()

// rng is a tiny seeded generator. Every blob has its own, started from the blob's origin
// chunk, so any chunk that a blob reaches rolls the same numbers.
type rng uint64

func (r *rng) next() uint64 {
	*r += 0x9E3779B97F4A7C15
	return mix64(uint64(*r))
}

// float returns a number from 0 up to but not including 1.
func (r *rng) float() float64 { return float64(r.next()>>11) / (1 << 53) }

// posHash mixes a seed with a block position.
func posHash(seed uint64, x, y, z int) uint64 {
	return mix64(seed ^ uint64(int64(x))*0x9E3779B97F4A7C15 ^ uint64(int64(y))*0xC2B2AE3D27D4EB4F ^ uint64(int64(z))*0x165667B19E3779F9)
}

// unit turns a hash into a number from 0 up to but not including 1.
func unit(h uint64) float64 { return float64(h>>11) / (1 << 53) }

// sampleY draws a height for a blob centre.
func (f *veinFeature) sampleY(r *rng) int {
	u := r.float()
	if f.dist == distUniform {
		return f.minY + int(u*float64(f.maxY-f.minY+1))
	}
	span, low, high := float64(f.maxY-f.minY), float64(f.peak-f.minY), float64(f.maxY-f.peak)
	if u < low/span {
		return f.minY + int(math.Round(math.Sqrt(u*span*low)))
	}
	return f.maxY - int(math.Round(math.Sqrt((1-u)*span*high)))
}

// veinKindOf says which replaceable kind a block is, or zero for anything else.
func (g *Generator) veinKindOf(id uint32) uint8 {
	b := &g.blocks
	switch id {
	case b.stone:
		return repStone
	case b.deepslate:
		return repDeepslate
	case b.granite:
		return repGranite
	case b.diorite:
		return repDiorite
	case b.andesite:
		return repAndesite
	case b.tuff:
		return repTuff
	}
	return 0
}

// placeVeins writes the rock patches (rocks true) or the ores (rocks false) into a chunk.
// A blob belongs to the chunk its centre is in and is rolled from that chunk's position
// alone, so each chunk rebuilds the blobs of itself and its eight neighbours and keeps
// only the blocks that fall inside itself. highest is the top of the chunk's terrain; a blob wholly above it is in air and skipped. Blobs are small enough that the neighbours
// cover everything that can reach in. Nothing is stored after the call.
func (g *Generator) placeVeins(c *chunk.Chunk, pos world.ChunkPos, highest int, rocks bool) {
	originX, originZ := int(pos[0])*16, int(pos[1])*16
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			cx, cz := int(pos[0])+dx, int(pos[1])+dz
			for i := range veinFeatures {
				f := &veinFeatures[i]
				if f.rock != rocks {
					continue
				}
				r := rng(posHash(g.veinSeed, cx, i, cz))
				n := int(f.attempts)
				if r.float() < f.attempts-float64(n) {
					n++
				}
				for range n {
					g.placeVein(c, f, &r, cx*16, cz*16, originX, originZ, highest)
				}
			}
		}
	}
}

// placeVein rolls one blob from its origin chunk and writes the part inside the chunk at
// originX, originZ. The rolls are the same whichever chunk asks.
func (g *Generator) placeVein(c *chunk.Chunk, f *veinFeature, r *rng, blobX, blobZ, originX, originZ, highest int) {
	px, pz := blobX+int(r.next()%16), blobZ+int(r.next()%16)
	py := f.sampleY(r)
	size := 1 + int(r.next()%uint64(f.maxSize))
	a1, a2, a3 := 0.75+0.5*r.float(), 0.75+0.5*r.float(), 0.75+0.5*r.float()
	key := r.next()

	// A blob of this many blocks has this radius; the three stretches keep its volume.
	// No axis is longer than maxStretch times the radius, which gives a quick reject
	// before the exact stretch is worked out.
	radius := blobRadius[size]
	reach := int(math.Ceil(radius * maxStretch))
	if px+reach < originX || px-reach >= originX+16 || pz+reach < originZ || pz-reach >= originZ+16 ||
		py+reach < f.minY || py-reach > f.maxY || py-reach > highest {
		return
	}
	norm := math.Cbrt(a1 * a2 * a3)
	rx, ry, rz := radius*a1/norm, radius*a2/norm, radius*a3/norm
	reach = int(math.Ceil(max(rx, ry, rz)))

	if px+reach < originX || px-reach >= originX+16 || pz+reach < originZ || pz-reach >= originZ+16 ||
		py+reach < f.minY || py-reach > f.maxY || py-reach > highest {
		return
	}
	if f.biome != biomeAny && !g.veinBiome(f.biome, px, py, pz) {
		return
	}

	b := &g.blocks
	for x := max(px-reach, originX); x <= min(px+reach, originX+15); x++ {
		for z := max(pz-reach, originZ); z <= min(pz+reach, originZ+15); z++ {
			for y := max(py-reach, f.minY, minY); y <= min(py+reach, f.maxY, maxY); y++ {
				ex, ey, ez := float64(x-px)/rx, float64(y-py)/ry, float64(z-pz)/rz
				d := ex*ex + ey*ey + ez*ez
				if d > 1 || (d > 0.2 && unit(posHash(key, x, y, z)) < d*0.6) {
					continue // Ragged edge: the outer blocks are left out more often.
				}
				lx, lz := uint8(x-originX), uint8(z-originZ)
				kind := g.veinKindOf(c.Block(lx, int16(y), lz, 0))
				if kind&f.replace == 0 {
					continue
				}
				if f.exposed > 0 && g.touchesAir(c, lx, int16(y), lz) && unit(posHash(g.veinSeed, x, y, z)) < f.exposed {
					continue
				}
				// Deepslate and tuff take the deepslate variant of an ore.
				variant := 0
				if kind&(repDeepslate|repTuff) != 0 {
					variant = 1
				}
				c.SetBlock(lx, int16(y), lz, 0, b.veins[f.block][variant])
			}
		}
	}
}

// touchesAir reports whether a block has air next to it inside the chunk. Neighbours in
// other chunks are not looked at (they do not exist yet), so they count as not air.
func (g *Generator) touchesAir(c *chunk.Chunk, x uint8, y int16, z uint8) bool {
	air := g.blocks.air
	return (x > 0 && c.Block(x-1, y, z, 0) == air) || (x < 15 && c.Block(x+1, y, z, 0) == air) ||
		(z > 0 && c.Block(x, y, z-1, 0) == air) || (z < 15 && c.Block(x, y, z+1, 0) == air) ||
		(y > minY && c.Block(x, y-1, z, 0) == air) || (y < maxY && c.Block(x, y+1, z, 0) == air)
}

// veinBiome checks a biome rule at a blob centre.
func (g *Generator) veinBiome(rule biomeRule, x, y, z int) bool {
	col := g.biomeColumnAt(cellCentre(x), cellCentre(z))
	switch rule {
	case biomeDripstone:
		return col.at(cellCentre(y)) == dripstoneCaves
	case biomeNotDripstone:
		return col.at(cellCentre(y)) != dripstoneCaves
	case biomeBadlands:
		return col.surface == badlands
	case biomeMountain:
		switch col.surface {
		case meadow, grove, snowySlopes, jaggedPeaks, frozenPeaks, stonyPeaks, windsweptHills:
			return true
		}
	}
	return false
}
