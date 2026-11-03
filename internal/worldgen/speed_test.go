package worldgen

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// sweepBench generates a square of chunks with workers, reporting the wall time per chunk and the
// shared cache's peak.
func sweepBench(b *testing.B, cache int64, workers int) {
	g := New(1)
	g.SetCacheBytes(cache)
	const side = 24
	for b.Loop() {
		var next atomic.Int64
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := next.Add(1) - 1
					if i >= side*side {
						return
					}
					c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
					g.GenerateChunk(world.ChunkPos{int32(i%side) + 40, int32(i/side) - 90}, c)
				}
			}()
		}
		wg.Wait()
	}
	b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(b.N*side*side), "ms/chunk")
	b.ReportMetric(float64(g.CacheStats().PeakBytes)/(1<<20), "cacheMB")
}

func BenchmarkSweepNoKeep(b *testing.B)        { sweepBench(b, 0, 1) }
func BenchmarkSweepNoKeepWorkers(b *testing.B) { sweepBench(b, 0, runtime.NumCPU()) }
func BenchmarkSweepCache(b *testing.B)         { sweepBench(b, DefaultCacheBytes, 1) }
func BenchmarkSweepCacheWorkers(b *testing.B)  { sweepBench(b, DefaultCacheBytes, runtime.NumCPU()) }
