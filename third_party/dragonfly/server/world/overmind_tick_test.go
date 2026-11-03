package world_test

// Overmind fork: tests that a tick never waits for a chunk that a chunk worker is still making.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/go-gl/mathgl/mgl64"
)

// heldGenerator is a flat world whose chunk held is not finished until release is closed, as a
// chunk is that waits behind many others on busy workers.
type heldGenerator struct {
	world.Generator
	held    world.ChunkPos
	release chan struct{}
	once    sync.Once
}

func (g *heldGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	if pos == g.held {
		<-g.release
	}
	g.Generator.GenerateChunk(pos, c)
}

func (g *heldGenerator) free() { g.once.Do(func() { close(g.release) }) }

// loadBounded moves the loader and waits until every chunk in want has been shown to it, failing,
// not hanging, if a world transaction stalls on the way.
func loadBounded(t *testing.T, w *world.World, l *world.Loader, v *chunkViewer, pos mgl64.Vec3, want ...world.ChunkPos) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		err := w.Do(func(tx *world.Tx) {
			l.Move(tx, pos)
			l.Load(tx, 256)
		}).Wait(ctx)
		cancel()
		if err != nil {
			t.Fatalf("a world transaction waited past 2 s while chunks loaded")
		}
		all := true
		for _, c := range want {
			all = all && v.has(c)
		}
		if all {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("chunks %v never loaded", want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestTickNeverWaitsForAChunkBeingMade holds chunk 5,0 in its worker while chunk 4,0, inside the
// loader's tick reach of 4, is loaded, and puts flowing water and grass on 4,0's edge with 5,0. Their
// ticks read the next block, in 5,0. A tick that read it would wait for the worker; the world must
// keep answering instead, and the water must still flow on once the chunk is made.
func TestTickNeverWaitsForAChunkBeingMade(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	gen := &heldGenerator{
		Generator: generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}}),
		held:      world.ChunkPos{5, 0},
		release:   make(chan struct{}),
	}
	w := world.Config{Entities: entity.DefaultRegistry, Generator: gen, ChunkLoadWorkers: 4, RandomTickSpeed: 300, ChunkUnloadInterval: time.Hour}.New()
	t.Cleanup(func() { gen.free(); _ = w.Close() })
	w.SetTickRange(reachRange)
	v := &chunkViewer{chunks: map[world.ChunkPos]bool{}}
	l := world.NewLoader(reachRadius, w, v)
	t.Cleanup(func() { _ = w.Do(func(tx *world.Tx) { l.Close(tx) }).Wait(context.Background()) })
	t.Cleanup(gen.free) // runs first, so a stalled tick lets the loader close
	loadBounded(t, w, l, v, mgl64.Vec3{8, -60, 8}, world.ChunkPos{4, 0}, world.ChunkPos{4, 1}, world.ChunkPos{4, -1}, world.ChunkPos{3, 0})
	if v.has(gen.held) {
		t.Fatal("the held chunk loaded")
	}

	const ground = -60 // first air block above the flat grass
	edge := cube.Pos{79, ground, 8}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := w.Do(func(tx *world.Tx) {
		for z := 0; z < 16; z++ {
			tx.SetBlock(cube.Pos{79, ground - 1, z}, block.Dirt{}, nil) // grass spreads onto it on random ticks
		}
		tx.SetLiquid(edge, block.Water{Still: true, Depth: 8})
	}).Wait(ctx); err != nil {
		t.Fatalf("setting up: %v", err)
	}

	worst := time.Duration(0)
	for start := time.Now(); time.Since(start) < 3*time.Second; {
		a := time.Now()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		err := w.Do(func(*world.Tx) {}).Wait(ctx)
		cancel()
		if err != nil {
			t.Fatalf("a world transaction waited past 2 s while chunk %v was being made", gen.held)
		}
		worst = max(worst, time.Since(a))
		time.Sleep(20 * time.Millisecond)
	}
	t.Logf("worst empty-transaction wait with chunk %v held: %v", gen.held, worst)
	if worst > 500*time.Millisecond {
		t.Errorf("a world transaction waited %v while chunk %v was being made", worst, gen.held)
	}

	// Once the chunk is made, the water flows across into it.
	gen.free()
	loadBounded(t, w, l, v, mgl64.Vec3{8, -60, 8}, gen.held)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var wet bool
		_ = w.Do(func(tx *world.Tx) { _, wet = tx.Liquid(edge.Add(cube.Pos{1, 0, 0})) }).Wait(t.Context())
		if wet {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the water never flowed into the chunk once it was made")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
