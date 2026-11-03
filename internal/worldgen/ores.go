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
	veinInfested
	veinGranite
	veinDiorite
	veinAndesite
	veinTuff
	veinDirt
	veinGravel
	veinClay
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
)

type distribution uint8

const (
	distUniform  distribution = iota
	distTriangle              // Most common in the middle, thinning to nothing at both ends.
)

// biomeRule limits a feature to some biomes.
type biomeRule uint8

const (
	biomeAny          biomeRule = iota
	biomeNotDripstone           // Anywhere but dripstone caves.
	biomeDripstone              // Dripstone caves only.
	biomeBadlands               // Any badlands surface biome.
	biomeMountain               // The mountain biomes listed in the study.
	biomeMountainsTag           // Bedrock's mountains tag: the same biomes without the windswept ones.
	biomeLush                   // Lush caves only.
)

// veinFeature is one row of the table below: a batch of blobs placed in each chunk.
// Attempts below one is a chance. exposed is the chance that a block touching air is
// dropped. A triangle's height range can run outside the world: the blob centre is drawn
// from the whole range, and those that land outside are lost, which is how the game thins
// out diamonds and redstone.
type veinFeature struct {
	block      veinBlock
	attempts   float64
	size       int
	minY, maxY int
	dist       distribution
	exposed    float64
	biome      biomeRule
	rock       bool // Patches of rock, dirt and gravel; placed before the ores, and may spill past the range.
	// replaces limits what a rock patch replaces. Zero is any kind of stone.
	replaces uint8
}

// veinFeatures is the table from the study of Java 26.1: attempts, size, air discard and height of
// each batch, with Bedrock's rules where the data files differ (BEDROCK-NOTES section 5): iron size
// 10, medium diamond size 4, the extra coal batch, infested stone size 8 at Y 0 to 64.
//
// What rock patches replace: section 5 row 8 reads the files as stone only for granite, diorite and
// andesite, and stone and deepslate for tuff and gravel (medium confidence, stone variants may match
// in a way the files do not show), against GAPS 18's "any stone kind and tuff" from Java. We follow
// the files, because TestDeepslateBlendMatchesBedrock measures the result and GAPS 18 does not fit.
// The files were not read for dirt; it takes stone only, like the rocks. Our own choice.
//
// Ores leave tuff alone. This is our own, tuned to counts, and not a Bedrock rule: the notes do not
// read the ores' replacement rules, and Java replaces tuff. With tuff replaced, every ore that lies mostly under Y 0 came out 7 to
// 14% over Bedrock's count, about tuff's share of the rock down there (6.8%).
var veinFeatures = [...]veinFeature{
	// Rock patches.
	{block: veinDirt, attempts: 7, size: 33, minY: 0, maxY: 160, rock: true, replaces: repStone},
	{block: veinGravel, attempts: 14, size: 33, minY: minY, maxY: maxY, rock: true, replaces: repStone | repDeepslate},
	{block: veinGranite, attempts: 2, size: 64, minY: 0, maxY: 60, rock: true, replaces: repStone},
	{block: veinGranite, attempts: 1.0 / 6, size: 64, minY: 64, maxY: 128, rock: true, replaces: repStone},
	{block: veinDiorite, attempts: 2, size: 64, minY: 0, maxY: 60, rock: true, replaces: repStone},
	{block: veinDiorite, attempts: 1.0 / 6, size: 64, minY: 64, maxY: 128, rock: true, replaces: repStone},
	{block: veinAndesite, attempts: 2, size: 64, minY: 0, maxY: 60, rock: true, replaces: repStone},
	{block: veinAndesite, attempts: 1.0 / 6, size: 64, minY: 64, maxY: 128, rock: true, replaces: repStone},
	{block: veinTuff, attempts: 2, size: 64, minY: minY, maxY: 0, rock: true, replaces: repStone | repDeepslate},
	{block: veinClay, attempts: 46, size: 33, minY: minY, maxY: 256, biome: biomeLush, rock: true},

	// Ores.
	{block: veinCoal, attempts: 30, size: 17, minY: 136, maxY: 320},
	{block: veinCoal, attempts: 20, size: 17, minY: 0, maxY: 192, dist: distTriangle, exposed: 0.5},
	{block: veinCoal, attempts: 20, size: 17, minY: 128, maxY: 256, exposed: 0.5, biome: biomeMountainsTag},
	{block: veinIron, attempts: 90, size: 10, minY: ironUpperBottom, maxY: 384, dist: distTriangle},
	{block: veinIron, attempts: 10, size: 10, minY: -24, maxY: 56, dist: distTriangle},
	{block: veinIron, attempts: 10, size: 4, minY: minY, maxY: 72},
	{block: veinCopper, attempts: 16, size: 10, minY: -16, maxY: 112, dist: distTriangle, biome: biomeNotDripstone},
	{block: veinCopper, attempts: 16, size: 20, minY: -16, maxY: 112, dist: distTriangle, biome: biomeDripstone},
	{block: veinGold, attempts: 4, size: 9, minY: -64, maxY: 32, dist: distTriangle, exposed: 0.5},
	{block: veinGold, attempts: 0.5, size: 9, minY: -64, maxY: -48, exposed: 0.5},
	{block: veinGold, attempts: 50, size: 9, minY: 32, maxY: 256, biome: biomeBadlands},
	{block: veinRedstone, attempts: 4, size: 8, minY: minY, maxY: 15},
	{block: veinRedstone, attempts: 8, size: 8, minY: -96, maxY: -32, dist: distTriangle},
	{block: veinLapis, attempts: 2, size: 7, minY: -32, maxY: 32, dist: distTriangle},
	{block: veinLapis, attempts: 4, size: 7, minY: minY, maxY: 64, exposed: 1},
	{block: veinDiamond, attempts: 7, size: 4, minY: -144, maxY: 16, dist: distTriangle, exposed: 0.5},
	{block: veinDiamond, attempts: 2, size: 4, minY: -64, maxY: -4, exposed: 0.5},
	{block: veinDiamond, attempts: 1.0 / 9, size: 12, minY: -144, maxY: 16, dist: distTriangle, exposed: 0.7},
	{block: veinDiamond, attempts: 4, size: 8, minY: -144, maxY: 16, dist: distTriangle, exposed: 1},
	{block: veinEmerald, attempts: 100, size: 3, minY: -16, maxY: 480, dist: distTriangle, biome: biomeMountain},
	{block: veinInfested, attempts: infestedAttempts, size: 8, minY: 0, maxY: 64, biome: biomeMountain, replaces: repStone | repDeepslate},
}

// ironUpperBottom and infestedAttempts are the Bedrock data files' values (BEDROCK-NOTES sections 4, 5).
const (
	ironUpperBottom  = 80
	infestedAttempts = 14
)

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
	// A triangle is the sum of two even draws.
	half := float64(f.maxY-f.minY) / 2
	return f.minY + int(math.Round(u*half+r.float()*half))
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
// only the blocks that fall inside itself. highest is the top of the chunk's terrain; a
// blob wholly above it is in air and skipped. No blob reaches past 16 blocks (see blobReach), so
// the neighbours cover everything that can reach in. Nothing is stored after the call.
func (g *Generator) placeVeins(c *chunk.Chunk, pos world.ChunkPos, highest int, rocks bool) {
	originX, originZ := int(pos[0])*16, int(pos[1])*16
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			cx, cz := int(pos[0])+dx, int(pos[1])+dz
			src := biomeSource{g: g, x: cx*16 + 8, z: cz*16 + 8}
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
					g.placeVein(c, f, &r, cx*16, cz*16, originX, originZ, highest, &src)
				}
			}
		}
	}
}

// biomeSource answers biome rules for a source chunk, looking the biome up at most once.
type biomeSource struct {
	g    *Generator
	x, z int
	have bool
	col  biomeColumn
	at   [(maxY - minY + 1) / 4]*biomeNode // Lookups so far, by quart.
}

// biomeAt is the column's biome at a height, looked up at most once per quart.
func (s *biomeSource) biomeAt(y int) def {
	col := s.column()
	i := (y - minY) >> 2
	if s.at[i] == nil {
		t := col.target
		t[biomeTargetDepth] = quantize(depthAt((y>>2)*4, col.offset))
		s.at[i] = nearestBiomeFrom(&t, col.sky)
	}
	return s.at[i].def()
}

func (s *biomeSource) column() *biomeColumn {
	if !s.have {
		s.col, s.have = s.g.biomeColumnAt(s.x, s.z), true
	}
	return &s.col
}

// Our own blob shape, tuned to the block counts in the notes (section 10, addendum A5). A blob
// is a spindle: a solid of revolution around a short line through its centre, widest in the
// middle and coming to a point at both ends. The line's length (size/4) and tilt (2 blocks
// each way on Y) and the swelling mid-way are from the notes' description. The widths are
// fitted by us, one table entry per size, by measuring TestBlobSizesMatchMeasuredFigures.
var blobWidths = [...]struct {
	size   int
	radius float64 // Radius at the middle.
}{
	{3, 0.25}, {4, 0.54}, {7, 0.82}, {8, 0.86}, {9, 0.915}, {10, 1.0}, {12, 1.136},
	{17, 1.44}, {20, 1.68}, {33, 2.62}, {64, 4.86},
}

// blobRadius looks up a size's middle radius in the table, blending between entries.
func blobRadius(size int) float64 {
	t := blobWidths[:]
	for i := 1; i < len(t); i++ {
		if size <= t[i].size {
			return lerp(t[i-1].radius, t[i].radius, float64(size-t[i-1].size)/float64(t[i].size-t[i-1].size))
		}
	}
	return t[len(t)-1].radius
}

// blobWiden is the largest a blob's roll can widen it, and blobTip how far its spindle runs
// past the ends of its line. Both own.
const (
	blobWiden = 1.06
	blobTip   = 1.0
)

// blobReach is how far a blob of a size can reach from its centre on any axis: half its line,
// its tip and its widest radius. A blob's centre lies in its source chunk, so as long as this
// is at most 16, the eight neighbours of a chunk hold every blob that can reach it.
func blobReach(size int) int {
	return int(math.Ceil(float64(size)/8+blobTip+blobRadius(size)*blobWiden)) + 1
}

// placeVein rolls one blob from its origin chunk and writes the part inside the chunk at
// originX, originZ. The rolls are the same whichever chunk asks. See blobWidths for the shape.
// Only blocks whose middle lies inside the shape are placed, so very small blobs often miss
// every block; that is how a size 3 blob places nothing about two times in three, as the
// study's addendum A5 measured.
func (g *Generator) placeVein(c *chunk.Chunk, f *veinFeature, r *rng, blobX, blobZ, originX, originZ, highest int, src *biomeSource) {
	px, pz := blobX+int(r.next()%16), blobZ+int(r.next()%16)
	py := f.sampleY(r)
	key := r.next()

	reach := blobReach(f.size)
	if px+reach < originX || px-reach >= originX+16 || pz+reach < originZ || pz-reach >= originZ+16 ||
		py+reach < minY || py-reach > maxY || py-reach > highest {
		return
	}
	if !f.rock && (py < minY || py > maxY) {
		return
	}
	if f.biome != biomeAny && !veinBiome(f.biome, src, py) {
		return
	}

	b := &g.blocks
	// Ores stay inside their feature's range and rock patches may spill past it. Our own
	// choice; the notes only give the range for the blob's centre.
	clipLow, clipHigh := minY, maxY
	if !f.rock {
		clipLow, clipHigh = max(f.minY, minY), min(f.maxY, maxY)
	}
	blob := rng(key)
	size := float64(f.size)
	// The middle of the blob lies anywhere inside its centre block.
	mx, my, mz := float64(px)+blob.float(), float64(py)+blob.float(), float64(pz)+blob.float()
	// Half the line, from the middle to one end.
	angle := blob.float() * 2 * math.Pi
	hx, hz := math.Cos(angle)*size/8, math.Sin(angle)*size/8
	hy := (2*blob.float() - 1) * 2
	lineSq := hx*hx + hy*hy + hz*hz
	// The spindle's half length runs past the line's end.
	halfLen := math.Sqrt(lineSq) + blobTip
	ux, uy, uz := hx/(halfLen-blobTip), hy/(halfLen-blobTip), hz/(halfLen-blobTip)
	radius := blobRadius(f.size) * (blobWiden - 0.12*blob.float())
	grainSeed := blob.next()
	span := halfLen + radius
	// Every block of the spindle lies within this squared distance of the middle, so most of the box
	// can be passed over without working out its place along the line.
	outer := halfLen*halfLen + radius*radius
	for x := max(int(math.Floor(mx-span)), originX); x <= min(int(math.Ceil(mx+span)), originX+15); x++ {
		dx := float64(x) + 0.5 - mx
		for z := max(int(math.Floor(mz-span)), originZ); z <= min(int(math.Ceil(mz+span)), originZ+15); z++ {
			dz := float64(z) + 0.5 - mz
			flat := dx*dx + dz*dz
			if flat >= outer {
				continue
			}
			for y := max(int(math.Floor(my-span)), clipLow); y <= min(int(math.Ceil(my+span)), clipHigh); y++ {
				dy := float64(y) + 0.5 - my
				whole := flat + dy*dy
				if whole >= outer {
					continue
				}
				along := dx*ux + dy*uy + dz*uz
				sideSq := whole - along*along
				axial := along / halfLen
				width := radius * (1 - axial*axial)
				if axial*axial >= 1 || sideSq >= width*width {
					continue
				}
				// A rough edge: blocks near the shell are dropped by chance.
				if f.size > 4 && sideSq > 0.6*width*width && unit(posHash(grainSeed, x, y, z)) < 0.2 {
					continue
				}
				lx, lz := uint8(x-originX), uint8(z-originZ)
				kind := g.veinKindOf(c.Block(lx, int16(y), lz, 0))
				if kind == 0 || (f.replaces != 0 && kind&f.replaces == 0) || (!f.rock && kind == repTuff) {
					continue
				}
				if f.exposed > 0 && g.touchesAir(c, lx, int16(y), lz) && unit(posHash(g.veinSeed, x, y, z)) < f.exposed {
					continue
				}
				// Deepslate takes the deepslate variant of an ore.
				variant := 0
				if kind == repDeepslate {
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

// veinBiome checks a biome rule for a blob, in its source chunk's middle column.
//
// The mountain rules read the biome at the blob's own height, not the surface: under a mountain the
// lower rock is often dripstone caves or deep dark, and Bedrock's raw seed 100 holds only 5% of its
// infested blocks in those two, though they are 40% of Y 0 to 64 under its mountain biomes.
func veinBiome(rule biomeRule, src *biomeSource, y int) bool {
	col := src.column()
	switch rule {
	case biomeDripstone:
		return src.biomeAt(y) == dripstoneCaves
	case biomeNotDripstone:
		return src.biomeAt(y) != dripstoneCaves
	case biomeLush:
		return src.biomeAt(y) == lushCaves
	case biomeBadlands:
		switch col.surface {
		case badlands, erodedBadlands, woodedBadlands:
			return true
		}
	case biomeMountain:
		return mountainBiome(src.biomeAt(y))
	case biomeMountainsTag:
		// The data files give the tag to these, among those the Overworld generates.
		switch src.biomeAt(y) {
		case meadow, cherryGrove, grove, snowySlopes, jaggedPeaks, frozenPeaks, stonyPeaks:
			return true
		}
	}
	return false
}

func mountainBiome(d def) bool {
	switch d {
	case meadow, cherryGrove, grove, snowySlopes, jaggedPeaks, frozenPeaks, stonyPeaks, windsweptHills, windsweptForest, gravellyHills:
		return true
	}
	return false
}

// Decoration rates from the study: an amethyst geode in 1 of 24 chunks at Y -58 to 30, 25
// water and 20 lava springs per chunk. Geode size and layers are our own, from the look
// of the feature: Dragonfly has no budding amethyst, so the shell is plain amethyst.
const (
	geodeChance      = 1.0 / 24
	geodeBottom      = -58
	geodeTop         = 30
	geodeMarginAbove = 10
	waterSprings     = 25
	waterSpringTop   = 192
	lavaSprings      = 20
)

// placeGeodes writes the geodes that reach a chunk. Only rock is replaced, and a geode stays
// well under the nominal ground, so it never opens to the sky or the sea.
func (g *Generator) placeGeodes(c *chunk.Chunk, cx, cz int) {
	b := &g.blocks
	for sx := cx - 1; sx <= cx+1; sx++ {
		for sz := cz - 1; sz <= cz+1; sz++ {
			h := posHash(g.featureSeed, sx, 0, sz)
			if unit(h) >= geodeChance {
				continue
			}
			r := rng(h)
			px, pz := sx*16+r.intn(16), sz*16+r.intn(16)
			py := geodeBottom + r.intn(geodeTop-geodeBottom+1)
			radius := 4 + 3*r.float()
			if float64(py)+radius+geodeMarginAbove >= nominalGround(g.surfaceShape(px, pz).offset) {
				continue
			}
			ox, oz := cx*16, cz*16
			reach := int(math.Ceil(radius))
			for x := max(px-reach, ox); x <= min(px+reach, ox+15); x++ {
				for z := max(pz-reach, oz); z <= min(pz+reach, oz+15); z++ {
					for y := max(py-reach, minY+1); y <= min(py+reach, maxY); y++ {
						dx, dy, dz := float64(x-px), float64(y-py), float64(z-pz)
						d := math.Sqrt(dx*dx+dy*dy+dz*dz) / radius
						if d >= 1 {
							continue
						}
						lx, lz := uint8(x-ox), uint8(z-oz)
						if g.veinKindOf(c.Block(lx, int16(y), lz, 0)) == 0 {
							continue
						}
						switch {
						case d < 0.55:
							// Our own rule: a hollow never opens under an aquifer's water, which would
							// hang over it until a block update.
							if c.Block(lx, int16(y+1), lz, 0) == b.water {
								c.SetBlock(lx, int16(y), lz, 0, b.amethyst)
							} else {
								c.SetBlock(lx, int16(y), lz, 0, b.air)
							}
						case d < 0.7:
							c.SetBlock(lx, int16(y), lz, 0, b.amethyst)
						case d < 0.84:
							c.SetBlock(lx, int16(y), lz, 0, b.calcite)
						default:
							c.SetBlock(lx, int16(y), lz, 0, b.smoothBasalt)
						}
					}
				}
			}
		}
	}
}

// placeSprings sets single fluid sources in rock walls: rock above, below and on three
// sides, air on the fourth. Dragonfly has no way to start a generated source flowing, so
// a spring stays a single block until something next to it changes. Only positions inside
// the chunk are used, so the check needs no neighbours.
func (g *Generator) placeSprings(c *chunk.Chunk, cx, cz int) {
	b := &g.blocks
	r := rng(posHash(g.featureSeed^1, cx, 0, cz))
	try := func(y int, id uint32) {
		x, z := uint8(1+r.intn(14)), uint8(1+r.intn(14))
		if y <= minY+1 || y >= maxY {
			return
		}
		rock := func(x uint8, y int, z uint8) bool { return g.veinKindOf(c.Block(x, int16(y), z, 0)) != 0 }
		if !rock(x, y, z) || !rock(x, y+1, z) || !rock(x, y-1, z) {
			return
		}
		open, solid := 0, 0
		for _, n := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			nx, nz := uint8(int(x)+n[0]), uint8(int(z)+n[1])
			switch {
			case c.Block(nx, int16(y), nz, 0) == b.air:
				open++
			case rock(nx, y, nz):
				solid++
			}
		}
		if open == 1 && solid == 3 {
			c.SetBlock(x, int16(y), z, 0, id)
		}
	}
	for range waterSprings {
		try(minY+r.intn(waterSpringTop-minY+1), b.water)
	}
	for range lavaSprings {
		// Heavily biased low, by nesting even draws.
		try(minY+r.intn(r.intn(r.intn(maxY-minY)+1)+1), b.lava)
	}
}
