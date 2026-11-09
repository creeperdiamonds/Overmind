package worldgen

import (
	"fmt"
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
	st := g.CacheStats()
	b.ReportMetric(float64(st.PeakBytes)/(1<<20), "cacheMB")
	var hits, misses uint64
	for k := range st.Hits {
		hits, misses = hits+st.Hits[k], misses+st.Misses[k]
	}
	b.ReportMetric(100*float64(hits)/float64(max(hits+misses, 1)), "hit%")
	b.Logf("cache by kind (0 bases, 1.. source levels): hits %v misses %v", st.Hits, st.Misses)
}

func BenchmarkSweepCache(b *testing.B)        { sweepBench(b, DefaultCacheBytes, 1) }
func BenchmarkSweepCacheWorkers(b *testing.B) { sweepBench(b, DefaultCacheBytes, runtime.NumCPU()) }

// BenchmarkSulfurArea generates the study's sulfur caves window (extreme_hills_5: seed 5, chunks x -15..-7,
// z -24..-16, NOTES-bedrock-exact T42) on one worker with a fresh generator each round, so the sulfur
// features' cost shows per chunk. With OVERMIND_BEDROCK_DATA the Bedrock profile runs BDS's rules.
func BenchmarkSulfurArea(b *testing.B) {
	for b.Loop() {
		g := New(5)
		for cz := int32(-24); cz <= -16; cz++ {
			for cx := int32(-15); cx <= -7; cx++ {
				c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
				g.GenerateChunk(world.ChunkPos{cx, cz}, c)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(b.N*81), "ms/chunk")
}

// BenchmarkMangroveArea generates the study's mangrove swamp window (mangrove_swamp_20: seed 20, chunks
// x -34..-26, z 28..36, NOTES-bedrock-exact T17) the same way, so the mangrove trees' cost shows per chunk.
func BenchmarkMangroveArea(b *testing.B) {
	for b.Loop() {
		g := New(20)
		for cz := int32(28); cz <= 36; cz++ {
			for cx := int32(-34); cx <= -26; cx++ {
				c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
				g.GenerateChunk(world.ChunkPos{cx, cz}, c)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(b.N*81), "ms/chunk")
}

// BenchmarkCoralArea generates the study's warm ocean window (warm_ocean_37: seed 37, chunks -4..4 x
// -4..4, NOTES-leftovers L12) the same way, so the coral crust's cost shows per chunk.
func BenchmarkCoralArea(b *testing.B) {
	for b.Loop() {
		g := New(37)
		for cz := int32(-4); cz <= 4; cz++ {
			for cx := int32(-4); cx <= 4; cx++ {
				c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
				g.GenerateChunk(world.ChunkPos{cx, cz}, c)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(b.N*81), "ms/chunk")
}

// Sweeps at other caps, to find where the cache stops rebuilding bases (ruling 31).
func BenchmarkSweepCap(b *testing.B) {
	for _, mb := range []int64{26, 28, 30, 32} {
		b.Run(fmt.Sprintf("%dMB", mb), func(b *testing.B) { sweepBench(b, mb<<20, 1) })
	}
}
