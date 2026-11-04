package world

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Generator handles the generating of newly created chunks. Worlds have one generator which is used to
// generate chunks when the provider of the world cannot find a chunk at a given chunk position.
type Generator interface {
	// GenerateChunk generates a chunk at a chunk position passed. The generator sets blocks in the chunk that
	// is passed to the method. With more than one chunk load worker, GenerateChunk is called concurrently.
	GenerateChunk(pos ChunkPos, chunk *chunk.Chunk)
	// DefaultSpawn returns the default spawn position for worlds using this generator in the dimension passed.
	DefaultSpawn(dim Dimension) cube.Pos
}

// ColumnGenerator is a Generator that also fills a new column's block entities, such as the chests of
// generated structures (Overmind fork). The world calls GenerateColumn instead of GenerateChunk.
type ColumnGenerator interface {
	GenerateColumn(pos ChunkPos, col *chunk.Column)
}

// NopGenerator is the default generator a world. It places no blocks in the world which results in a void
// world.
type NopGenerator struct{}

// GenerateChunk ...
func (NopGenerator) GenerateChunk(ChunkPos, *chunk.Chunk) {}

// DefaultSpawn ...
func (NopGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{} }

// lockedGenerator wraps a Generator, serialising GenerateChunk calls for
// generators that are not safe for concurrent use.
type lockedGenerator struct {
	mu sync.Mutex
	g  Generator
}

func (l *lockedGenerator) GenerateChunk(pos ChunkPos, c *chunk.Chunk) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.g.GenerateChunk(pos, c)
}

// GenerateColumn passes a ColumnGenerator through (Overmind fork).
func (l *lockedGenerator) GenerateColumn(pos ChunkPos, col *chunk.Column) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if cg, ok := l.g.(ColumnGenerator); ok {
		cg.GenerateColumn(pos, col)
		return
	}
	l.g.GenerateChunk(pos, col.Chunk)
}

func (l *lockedGenerator) DefaultSpawn(dim Dimension) cube.Pos {
	return l.g.DefaultSpawn(dim)
}
