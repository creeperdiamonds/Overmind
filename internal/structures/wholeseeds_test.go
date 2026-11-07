package structures

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// TestWorldsWithStructuresMatchBDS scores the four whole-world exports (golden-bedrock/whole, chunks
// -10..9) with and without structures, on the stable mask: what the structures add to a whole world.
// It reports only; worldgen's TestBedrockWorldsMatchBDS holds the figures without structures.
func TestWorldsWithStructuresMatchBDS(t *testing.T) {
	if os.Getenv("OVERMIND_LONG") == "" && testing.Short() {
		t.Skip("short")
	}
	for _, seed := range testSeeds {
		w := loadWorld(t, filepath.Join(goldenDir(t), "whole", fmt.Sprintf("seed%d.bin.gz", seed)))
		g, s := worldFor(t, seed)
		bare := worldgen.NewWithData(seed, worldgen.ProfileBedrock, s.bd)
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		var same, sameBare, total int
		for c := range w.chunks {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				whole := []box{{c[0] * 16, minY, c[1] * 16, c[0]*16 + 15, maxY - 1, c[1]*16 + 15}}
				col := generate(g, c[0], c[1])
				sm, tt, _ := boxScore(w, col.Chunk, c[0], c[1], whole)
				plain := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
				bare.GenerateChunk(world.ChunkPos{int32(c[0]), int32(c[1])}, plain)
				sb, _, _ := boxScore(w, plain, c[0], c[1], whole)
				mu.Lock()
				same, sameBare, total = same+sm, sameBare+sb, total+tt
				mu.Unlock()
			}()
		}
		wg.Wait()
		t.Logf("seed %d whole world (%d chunks): with structures %.4f%%, without %.4f%%", seed, len(w.chunks),
			100*float64(same)/float64(max(total, 1)), 100*float64(sameBare)/float64(max(total, 1)))
	}
}
