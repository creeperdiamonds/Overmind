package worldgen

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

// TestStructureStatesStopShortOfTheTables: a structure block of a new kind is refused, not a panic, once
// the state or block table is down to the generator's reserve; a block already listed still takes a new
// state while states remain.
func TestStructureStatesStopShortOfTheTables(t *testing.T) {
	for _, c := range []struct {
		states, blocks int
		known, want    bool
	}{
		{100, 100, false, true},
		{maxStates - reservedStates - 1, 100, false, true},
		{maxStates - reservedStates, 100, true, false},
		{maxStates, maxBlocks, true, false},
		{100, maxBlocks - reservedBlocks, false, false},
		{100, maxBlocks - reservedBlocks, true, true},
	} {
		if got := structureRoom(c.states, c.blocks, c.known); got != c.want {
			t.Errorf("states %d blocks %d known %v: room %v, want %v", c.states, c.blocks, c.known, got, c.want)
		}
	}
	// A real structure block still gets its state today.
	st, ok := stateOfRuntime(world.BlockRuntimeID(block.NewChest()), false)
	if !ok || st == 0 {
		t.Fatalf("a chest's runtime state was refused (%d, %v)", st, ok)
	}
}
