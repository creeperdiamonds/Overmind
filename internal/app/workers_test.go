package app

import (
	"runtime"
	"testing"
)

// TestChunkWorkersDefault checks ruling 32: generation workers default to min(4, processors), since
// each busy worker holds its own scratch on top of the shared cache, and the setting wins when given.
func TestChunkWorkersDefault(t *testing.T) {
	if got, want := chunkWorkers(0), min(4, runtime.NumCPU()); got != want {
		t.Errorf("chunkWorkers(0) = %d, want min(4, %d processors) = %d", got, runtime.NumCPU(), want)
	}
	for _, set := range []int{1, 3, 16} {
		if got := chunkWorkers(set); got != set {
			t.Errorf("chunkWorkers(%d) = %d, want the setting", set, got)
		}
	}
}
