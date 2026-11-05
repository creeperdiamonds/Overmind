package worldgen

import (
	"encoding/json"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// What internal/structures (Task 9) reads from the generator, and the hook it places pieces through.
// Structures live in their own package; this file only exposes the generator's seeds, biomes and
// random numbers, so that the two share one implementation of each.

// StructurePlacer places structure pieces into a chunk once its terrain and decoration are done.
// It is a pure function of the seed and the chunk position, safe on any number of workers.
type StructurePlacer interface {
	// PlaceChunk writes the parts of every structure piece inside chunk (cx, cz) and returns the
	// block entities it made (chests with a loot table and seed).
	PlaceChunk(cx, cz int, c *chunk.Chunk) []chunk.BlockEntity
	// NearestStronghold is the stronghold an eye of ender thrown at pos leads to.
	NearestStronghold(pos cube.Pos) (cube.Pos, bool)
}

// SetStructures attaches the structures of this generator's world; nil places none. Set it before the
// first chunk is made.
func (g *Generator) SetStructures(p StructurePlacer) { g.structs = p }

// GenerateColumn is GenerateChunk with the column's block entities filled in too (the Dragonfly fork
// calls it when a generator has it).
func (g *Generator) GenerateColumn(pos world.ChunkPos, col *chunk.Column) {
	g.generateInto(int(pos[0]), int(pos[1]), col.Chunk, col)
}

// GenerateBase is an Overworld chunk as GenerateChunk makes it before any structure piece is written.
// Structures replay a neighbour chunk's post-process on it (a mineshaft corridor's spawner mark).
func (g *Generator) GenerateBase(cx, cz int, c *chunk.Chunk) {
	g.cache.begin()
	defer g.cache.end()
	s := scratchPool.Get().(*chunkScratch)
	defer scratchPool.Put(s)
	if g.structuresFirst() {
		// The Bedrock order: a chunk's structures see its terrain only (P21.1).
		g.cachedBase(s, cx, cz).expandCells(&s.final)
		writeCells(c, &s.final)
	} else {
		writeCells(c, g.finalCells(s, cx, cz))
	}
	g.storeBiomes(c, s, cx, cz)
}

// placeStructures is the hook GenerateChunk runs after the chunk's own blocks are written.
// In the Bedrock order the blocks are already in the chunk's cells (structures_base.go): only the
// block entities whose block the decoration left are added.
func (g *Generator) placeStructures(s *chunkScratch, cx, cz int, final *[chunkCells]bs, c *chunk.Chunk, col *chunk.Column) {
	if g.structs == nil {
		return
	}
	var bes []chunk.BlockEntity
	if g.structuresFirst() {
		bes = g.structureEntities(s, cx, cz, final)
	} else {
		bes = g.structs.PlaceChunk(cx, cz, c)
	}
	if col != nil {
		col.BlockEntities = append(col.BlockEntities, bes...)
	}
}

// Seed is the world seed.
func (g *Generator) Seed() int64 { return g.seed }

// BedrockLevelSeed is the 32-bit level seed Bedrock's structure placement draws from (section P3).
func (g *Generator) BedrockLevelSeed() uint32 { return g.bseeds.level }

// DecorationSeed is chunk (cx, cz)'s decoration seed (NOTES-bedrock-exact section 2), which structure
// post-processing reseeds its random with (NOTES-structures-phase2 2.1).
func (g *Generator) DecorationSeed(cx, cz int) uint32 { return g.bseeds.chunk(cx, cz) }

// BiomeID is Bedrock's numeric id of the biome stored at a block (its quart), as the structure biome
// tests read it (NOTES-bedrock-structures 2.1). The Nether's biomes are 2D: the quart's stored biome.
func (g *Generator) BiomeID(x, y, z int) int {
	if g.dim == dimNether {
		return biomeRowDefs[g.netherN.biomes.quart(x>>2, z>>2)].EncodeBiome()
	}
	return g.BiomeAt(x, y, z).EncodeBiome()
}

// IsEnd says whether this is a world's End generator.
func (g *Generator) IsEnd() bool { return g.dim == dimEnd }

// EndTerrainHeights are the End's terrain heights (first solid block from the top + 1, 0 for void) at
// columns (local x, z) of chunk (cx, cz), from the terrain alone, as the end city's start test reads them
// (NOTES-bedrock-end phase 8).
func (g *Generator) EndTerrainHeights(cx, cz int, cols [][2]int) []int {
	var cells [endHeight * 256]bs
	g.endN.noise.fill(cx, cz, &cells, sEndStone)
	out := make([]int, len(cols))
	for i, c := range cols {
		for y := endHeight - 1; y >= 0; y-- {
			if cells[y<<8|(c[1]&15)<<4|c[0]&15] != 0 {
				out[i] = y + 1
				break
			}
		}
	}
	return out
}

// IsNether says whether this is a world's Nether generator.
func (g *Generator) IsNether() bool { return g.dim == dimNether }

// PreliminarySurface is the generator's preliminary surface level at the quart holding block (x, z), the
// level its surface builder uses. The stronghold ring's village test reads it (NOTES-structures-phase2
// P9.5).
func (g *Generator) PreliminarySurface(x, z int) int {
	s := scratchPool.Get().(*chunkScratch)
	defer scratchPool.Put(s)
	s.prelims.use(g)
	return g.preliminarySurface(&s.prelims, x, z)
}

// SurfaceWG is Java's getFirstFreeHeight on the WORLD_SURFACE_WG heightmap at a block column: one above
// the highest block that is not air at the noise stage (water counts). The data-driven jigsaw
// structures project their starts and terrain-matching pieces with it (NOTES-structures-phase2 phase 8).
func (g *Generator) SurfaceWG(x, z int) int {
	s := scratchPool.Get().(*chunkScratch)
	defer scratchPool.Put(s)
	s.startColumnOnly(g)
	_, nonAir := g.startWalk(s, x, z).rawTops()
	return nonAir + 1
}

// Data is the Bedrock feature data this generator reads, nil when no install was read.
func (g *Generator) Data() *BedrockData { return g.bdata }

// BiomeComponent is a component of a biome's pack file ("minecraft:village_type"), nil if the biome or
// the component is missing. Names are without "minecraft:".
func (bd *BedrockData) BiomeComponent(biome, component string) json.RawMessage {
	if b, ok := bd.biomes[biome]; ok {
		return b.components[component]
	}
	return nil
}

// BiomeNames lists the biomes the data has files for.
func (bd *BedrockData) BiomeNames() []string {
	out := make([]string, 0, len(bd.biomes))
	for n := range bd.biomes {
		out = append(out, n)
	}
	return out
}

// BiomeFilterMatches runs a pack biome filter (feature rules' and jigsaw structures' biome_filters) on
// a biome by name.
func (bd *BedrockData) BiomeFilterMatches(raw json.RawMessage, biome string) bool {
	b, ok := bd.biomes[biome]
	if !ok {
		return false
	}
	m, ok := biomeFilter(raw, b.tags)
	return ok && m
}

// PacksDir finds the behaviour packs folder of an install, as LoadBedrockData does.
func PacksDir(dir string) (string, error) {
	p, _, err := packsDir(dir)
	return p, err
}

// PackVersion orders vanilla pack folder names: "vanilla" first, then by version.
func PackVersion(name string) []int { return packVersion(name) }

// IsVanillaPack says whether a pack folder is one of the vanilla packs.
func IsVanillaPack(name string) bool { return vanillaPack.MatchString(name) }

// ReadBrarchive reads a pack archive's files as name and text pairs.
func ReadBrarchive(path string) ([][2]string, error) { return readArchiveIfAny(path) }

// JSONText strips the comments a pack's JSON may carry.
func JSONText(text string) []byte { return jsonText(text) }

// BedrockRandom is Bedrock's Random (MT19937 with Bedrock's method semantics; NOTES-bedrock-exact
// section 1, NOTES-structures-phase2 1.1).
type BedrockRandom struct{ r bmt }

// NewBedrockRandom seeds a Bedrock random.
func NewBedrockRandom(seed uint32) *BedrockRandom {
	b := &BedrockRandom{}
	b.r.setSeed(seed)
	return b
}

func (b *BedrockRandom) SetSeed(seed uint32)    { b.r.setSeed(seed) }
func (b *BedrockRandom) U32() uint32            { return b.r.u32() }
func (b *BedrockRandom) NextInt() int32         { return b.r.nextInt() }
func (b *BedrockRandom) NextIntN(n int32) int32 { return b.r.nextIntN(n) }
func (b *BedrockRandom) NextBool() bool         { return b.r.nextBool() }
func (b *BedrockRandom) NextFloat() float32     { return b.r.nextFloat() }
func (b *BedrockRandom) NextDouble() float64    { return b.r.nextDouble() }
func (b *BedrockRandom) Seeded() uint32         { return b.r.seed }
func (b *BedrockRandom) Consume(n int) {
	for range n {
		b.r.u32()
	}
}

// BedrockSin is Bedrock's 65536-entry sine table (NOTES-bedrock-exact section 4).
func BedrockSin(i int) float32 { return bedrockSin[i&0xffff] }

// JavaRandom is java.util.Random's 48-bit generator, which Bedrock's data-driven jigsaw sets place
// with (NOTES-bedrock-structures 2.4).
type JavaRandom struct{ r lcg }

// NewJavaRandom seeds a Java random.
func NewJavaRandom(seed int64) *JavaRandom { return &JavaRandom{newLCG(seed)} }

func (j *JavaRandom) NextInt(n int32) int32 { return j.r.nextInt(n) }
func (j *JavaRandom) NextLong() int64       { return j.r.nextLong() }
func (j *JavaRandom) NextFloat() float32    { return j.r.nextFloat() }
func (j *JavaRandom) NextDouble() float64   { return j.r.nextDouble() }

// Xoroshiro is Java's Xoroshiro128++ seeded from a 64-bit seed (RandomSupport's upgrade to 128 bits),
// which the desert pyramid's archaeology uses on Bedrock too (NOTES-structures-phase2 P11.2).
type Xoroshiro struct{ r xoro }

// NewXoroshiro seeds a Xoroshiro128++.
func NewXoroshiro(seed int64) *Xoroshiro { return &Xoroshiro{xoroFromSeed(seed)} }

// NextIntN is Java's bounded nextInt (Lemire's method).
func (x *Xoroshiro) NextIntN(n int32) int32 { return int32(x.r.nextInt(uint32(n))) }

// NextInt is Java's nextInt(): the low 32 bits of a draw.
func (x *Xoroshiro) NextInt() int32 { return int32(uint32(x.r.next())) }

// NextLong is Java's nextLong(): one 64-bit draw.
func (x *Xoroshiro) NextLong() int64 { return int64(x.r.next()) }

// SetSeed reseeds it as Java's setSeed does (RandomSupport's upgrade to 128 bits).
func (x *Xoroshiro) SetSeed(seed int64) { x.r = xoroFromSeed(seed) }
