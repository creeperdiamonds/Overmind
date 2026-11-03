package world_test

// Overmind fork: tests that a tick changes nothing past a loader's tick reach, as on Bedrock, where
// chunks beyond the simulation distance stay loaded but do not tick.

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

// chunkViewer records the chunks a loader has been shown.
type chunkViewer struct {
	world.NopViewer
	mu     sync.Mutex
	chunks map[world.ChunkPos]bool
}

func (v *chunkViewer) ViewChunk(pos world.ChunkPos, _ world.Dimension, _ map[cube.Pos]world.Block, _ *chunk.Chunk) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.chunks[pos] = true
}

func (v *chunkViewer) has(pos world.ChunkPos) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.chunks[pos]
}

// Reach of these tests: tick range 4, loader radius 6, so min(4, 6-2) = 4 chunks.
const reachRadius, reachRange = 6, 4

// reachWorld opens a flat grass world with one loader at chunk 0,0 that has loaded chunk 5,0, which
// lies past its reach of 4 chunks but inside its view of 6.
func reachWorld(t *testing.T) (*world.World, *world.Loader, *chunkViewer) {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	gen := generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}})
	w := world.Config{Entities: entity.DefaultRegistry, Generator: gen, ChunkUnloadInterval: time.Hour}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetTickRange(reachRange)
	v := &chunkViewer{chunks: map[world.ChunkPos]bool{}}
	l := world.NewLoader(reachRadius, w, v)
	t.Cleanup(func() { _ = w.Do(func(tx *world.Tx) { l.Close(tx) }).Wait(context.Background()) })
	loadAround(t, w, l, v, mgl64.Vec3{8, -60, 8}, world.ChunkPos{5, 0})
	return w, l, v
}

// loadAround moves the loader and waits until chunk want has been shown to it.
func loadAround(t *testing.T, w *world.World, l *world.Loader, v *chunkViewer, pos mgl64.Vec3, want world.ChunkPos) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !v.has(want) {
		if time.Now().After(deadline) {
			t.Fatalf("chunk %v never loaded", want)
		}
		_ = w.Do(func(tx *world.Tx) {
			l.Move(tx, pos)
			l.Load(tx, 256)
		}).Wait(t.Context())
		time.Sleep(20 * time.Millisecond)
	}
	// Let the remaining chunks of the ring arrive too.
	for range 20 {
		_ = w.Do(func(tx *world.Tx) { l.Load(tx, 256) }).Wait(t.Context())
		time.Sleep(10 * time.Millisecond)
	}
}

func inReach(centre, pos world.ChunkPos) bool {
	dx, dz := pos[0]-centre[0], pos[1]-centre[1]
	return dx*dx+dz*dz <= reachRange*reachRange
}

func chunkOf(v mgl64.Vec3) world.ChunkPos {
	p := cube.PosFromVec3(v)
	return world.ChunkPos{int32(p[0] >> 4), int32(p[2] >> 4)}
}

// TestTickCreatesNoEntityOutsideReach sets up every tick-driven spawner on both sides of the reach:
// lightning (a bolt past reach would never tick, so never close), sand left floating (a neighbour
// update makes it a falling block) and water flowing onto a flower (a scheduled liquid tick breaks
// it into an item). Inside the reach each must spawn its entity; outside, none may.
func TestTickCreatesNoEntityOutsideReach(t *testing.T) {
	defer world.SetLightningChance(40)()
	w, l, v := reachWorld(t)
	w.StartRaining(time.Hour)
	w.StartThundering(time.Hour)

	const ground = -60    // first air block above the flat grass
	setups := []int{1, 5} // chunk X of each setup: 1 is in reach, 5 is past it
	_ = w.Do(func(tx *world.Tx) {
		for _, cx := range setups {
			x := cx*16 + 8
			// Floating sand: placing air beside it sends it a neighbour update.
			tx.SetBlock(cube.Pos{x, ground + 10, 4}, block.Sand{}, nil)
			tx.SetBlock(cube.Pos{x, ground + 11, 4}, block.Air{}, nil)
			// Water beside a flower.
			tx.SetBlock(cube.Pos{x + 1, ground, 8}, block.Flower{}, nil)
			tx.SetLiquid(cube.Pos{x, ground, 8}, block.Water{Still: true, Depth: 8})
		}
	}).Wait(t.Context())

	seen := map[string]int{}
	outside := map[string]int{}
	for range 100 {
		_ = w.Do(func(tx *world.Tx) {
			for e := range tx.Entities() {
				name := e.H().Type().EncodeEntity()
				if inReach(world.ChunkPos{0, 0}, chunkOf(e.Position())) {
					seen[name]++
				} else {
					outside[name]++
				}
			}
		}).Wait(t.Context())
		time.Sleep(25 * time.Millisecond)
	}
	t.Logf("entity sightings in reach %v, outside %v", seen, outside)
	for _, name := range []string{"minecraft:lightning_bolt", "minecraft:falling_block", "minecraft:item"} {
		if seen[name] == 0 {
			t.Errorf("no %s inside the reach: the setup did not work", name)
		}
	}
	if len(outside) != 0 {
		t.Errorf("the tick created entities outside the reach: %v", outside)
	}

	// The waiting work is not lost: with the loader beside the far setup, its sand falls and its
	// flower is washed away.
	w.StopThundering()
	w.StopRaining()
	loadAround(t, w, l, v, mgl64.Vec3{88, ground, 8}, world.ChunkPos{9, 0})
	far := map[string]int{}
	for range 60 {
		_ = w.Do(func(tx *world.Tx) {
			for e := range tx.Entities() {
				if chunkOf(e.Position()) == (world.ChunkPos{5, 0}) {
					far[e.H().Type().EncodeEntity()]++
				}
			}
		}).Wait(t.Context())
		time.Sleep(25 * time.Millisecond)
	}
	t.Logf("entity sightings in chunk 5,0 once near: %v", far)
	for _, name := range []string{"minecraft:falling_block", "minecraft:item"} {
		if far[name] == 0 {
			t.Errorf("no %s in chunk 5,0 once a loader came near: the waiting tick was lost", name)
		}
	}
}

// TestEntitiesFreezeOutsideReachAndResume drops one falling block inside the reach and one past it.
// The first falls; the second stays put until the loader moves next to it, then falls.
func TestEntitiesFreezeOutsideReachAndResume(t *testing.T) {
	w, l, v := reachWorld(t)
	near, far := mgl64.Vec3{24.5, -30, 8.5}, mgl64.Vec3{88.5, -30, 8.5}
	var hNear, hFar *world.EntityHandle
	_ = w.Do(func(tx *world.Tx) {
		hNear = entity.NewFallingBlock(world.EntitySpawnOpts{Position: near}, block.Sand{})
		hFar = entity.NewFallingBlock(world.EntitySpawnOpts{Position: far}, block.Sand{})
		tx.AddEntity(hNear)
		tx.AddEntity(hFar)
	}).Wait(t.Context())

	// y gives an entity's height, or false once it has landed and closed.
	y := func(h *world.EntityHandle) (float64, bool) {
		var out float64
		var ok bool
		_ = w.Do(func(tx *world.Tx) {
			for e := range tx.Entities() {
				if e.H() == h {
					out, ok = e.Position()[1], true
				}
			}
		}).Wait(t.Context())
		return out, ok
	}

	time.Sleep(1500 * time.Millisecond)
	if got, ok := y(hNear); ok && got >= near[1] {
		t.Errorf("the falling block inside the reach did not move: Y %v", got)
	}
	if got, ok := y(hFar); !ok || got != far[1] {
		t.Fatalf("the falling block outside the reach moved or vanished: Y %v, present %v", got, ok)
	}

	loadAround(t, w, l, v, far, world.ChunkPos{9, 0})
	time.Sleep(1500 * time.Millisecond)
	if got, ok := y(hFar); ok && got >= far[1] {
		t.Errorf("the falling block did not resume once a loader came near: Y %v", got)
	}
}
