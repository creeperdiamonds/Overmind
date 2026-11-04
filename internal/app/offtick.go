package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// Chunk generation belongs on the chunk workers, never on the world's tick (owner's ruling 26): a chunk
// costs tens of milliseconds and the tick has 50. Dragonfly runs every transaction and tick of a world
// on one goroutine, and a block read there of a chunk not yet loaded generates the chunk on the spot.
// The tick distance and spawn settings in Start keep that from happening; offTick watches for it.

// generationsOnTick counts chunks generated on a world's tick goroutine since the process started.
var generationsOnTick atomic.Int64

// GenerationsOnTick is how many chunks have been generated on a world's tick, which should be none.
func GenerationsOnTick() int64 { return generationsOnTick.Load() }

// offTick passes generation through to the generator, counting and logging any call made on the
// world's tick goroutine. Finding the goroutine costs about a microsecond, against a chunk's tens of
// milliseconds.
type offTick struct {
	g    world.Generator
	tick atomic.Uint64 // The tick goroutine's id, once known.
}

func (o *offTick) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	if id := o.tick.Load(); id != 0 && goroutineID() == id {
		if generationsOnTick.Add(1) == 1 {
			slog.Default().Warn("A chunk was generated on the world's tick", "X", pos[0], "Z", pos[1])
		}
	}
	o.g.GenerateChunk(pos, c)
}

// GenerateColumn passes a column generator (structure chests) through, with the same tick check.
func (o *offTick) GenerateColumn(pos world.ChunkPos, col *chunk.Column) {
	cg, ok := o.g.(world.ColumnGenerator)
	if !ok {
		o.GenerateChunk(pos, col.Chunk)
		return
	}
	if id := o.tick.Load(); id != 0 && goroutineID() == id {
		if generationsOnTick.Add(1) == 1 {
			slog.Default().Warn("A chunk was generated on the world's tick", "X", pos[0], "Z", pos[1])
		}
	}
	cg.GenerateColumn(pos, col)
}

func (o *offTick) DefaultSpawn(dim world.Dimension) cube.Pos { return o.g.DefaultSpawn(dim) }

// NearestStronghold passes the generator's strongholds through, for eyes of ender.
func (o *offTick) NearestStronghold(pos cube.Pos) (cube.Pos, bool) {
	if l, ok := o.g.(world.StrongholdLocator); ok {
		return l.NearestStronghold(pos)
	}
	return cube.Pos{}, false
}

// watch learns the goroutine of a world's transactions.
func (o *offTick) watch(w *world.World) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = w.Do(func(*world.Tx) { o.tick.Store(goroutineID()) }).Wait(ctx)
}

// goroutineID reads the running goroutine's id from the first line of its stack, "goroutine N [".
func goroutineID() uint64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i > 0 {
		b = b[:i]
	}
	id, _ := strconv.ParseUint(string(b), 10, 64)
	return id
}

// preloadSpawn loads the chunks around the world spawn, out to the tick distance and two more, through
// a loader, which hands them all to the chunk workers at once, and waits for them, so the first
// player's arrival does not generate them on the tick. They stay loaded until Dragonfly closes unused
// chunks.
func preloadSpawn(w *world.World, spawnRadius int) error {
	spawn := w.Spawn()
	centre := mgl64.Vec3{float64(spawn[0]), 0, float64(spawn[2])}
	l := world.NewLoader(spawnRadius, w, world.NopViewer{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer w.Do(func(tx *world.Tx) { l.Close(tx) })
	cx, cz := int32(spawn[0]>>4), int32(spawn[2]>>4)
	for {
		missing := 0
		if err := w.Do(func(tx *world.Tx) {
			l.Move(tx, centre)
			l.ChangeRadius(tx, spawnRadius)
			l.Load(tx, 1<<12)
		}).Wait(ctx); err != nil {
			return err
		}
		for x := -spawnRadius; x <= spawnRadius; x++ {
			for z := -spawnRadius; z <= spawnRadius; z++ {
				if x*x+z*z > spawnRadius*spawnRadius {
					continue
				}
				if _, ok := l.Chunk(world.ChunkPos{cx + int32(x), cz + int32(z)}); !ok {
					missing++
				}
			}
		}
		if missing == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%d spawn chunks still loading", missing)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
