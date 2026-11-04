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

// placeStructures is the hook GenerateChunk runs after the chunk's own blocks are written.
func (g *Generator) placeStructures(cx, cz int, c *chunk.Chunk, col *chunk.Column) {
	if g.structs == nil {
		return
	}
	bes := g.structs.PlaceChunk(cx, cz, c)
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
// tests read it (NOTES-bedrock-structures 2.1).
func (g *Generator) BiomeID(x, y, z int) int { return g.BiomeAt(x, y, z).EncodeBiome() }

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
