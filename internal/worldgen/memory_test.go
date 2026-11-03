package worldgen

import (
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestSweepHeap measures the live heap above an idle baseline after a 192-chunk sweep (16 x 12) on
// OVERMIND_MEASURE_WORKERS workers (ruling 32), just after it ends and once the cache has emptied.
// It is a measurement, run one worker count a process, since pooled scratch outlives a run: set
// OVERMIND_MEASURE_WORKERS=1, 4, ... to run it.
func TestSweepHeap(t *testing.T) {
	n, err := strconv.Atoi(os.Getenv("OVERMIND_MEASURE_WORKERS"))
	if err != nil || n < 1 {
		t.Skip("set OVERMIND_MEASURE_WORKERS to a worker count to measure")
	}
	live := func() float64 {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return float64(m.HeapAlloc) / (1 << 20)
	}
	g := New(100)
	g.SurfaceHeight(0, 0)
	base := live()

	jobs := make(chan [2]int32)
	var wg sync.WaitGroup
	start := time.Now()
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				// The chunk is dropped at once, as Dragonfly's worker hands it on and the column is
				// counted elsewhere: this measures the generator's own memory.
				g.GenerateChunk(world.ChunkPos{p[0], p[1]}, chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()))
			}
		}()
	}
	for x := int32(0); x < 16; x++ {
		for z := int32(0); z < 12; z++ {
			jobs <- [2]int32{x, z}
		}
	}
	close(jobs)
	wg.Wait()
	took := time.Since(start)
	after := live()
	cache := float64(g.CacheStats().Bytes) / (1 << 20)
	time.Sleep(cacheIdleDelay + 500*time.Millisecond)
	idle := live()
	runtime.GC() // the second GC frees the pool's victim scratch too
	idle2 := live()
	t.Logf("%d workers: sweep %.1f s; heap above baseline %.1f MB just after (cache %.1f MB, the rest scratch and garbage kept by the pool), %.1f MB once the cache emptied, %.1f MB after the pool's second GC",
		n, took.Seconds(), after-base, cache, idle-base, idle2-base)
}
