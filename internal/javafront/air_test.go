package javafront

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"

	"github.com/overmind/overmind/internal/tables/java261"
)

// A section's block count leaves out all three of Java's airs. No Dragonfly
// block maps to cave or void air today; the count must not depend on that.
func TestAirIsNotABlock(t *testing.T) {
	finalizeRegistry()
	for _, n := range []string{"minecraft:air", "minecraft:cave_air", "minecraft:void_air"} {
		s, ok := java261.DefaultState(n)
		if !ok || !isAir(int32(s)) {
			t.Errorf("%s (%d) is not counted as air", n, s)
		}
	}
	if stone, _ := java261.DefaultState("minecraft:stone"); isAir(int32(stone)) {
		t.Error("stone counted as air")
	}
	mapped := 0
	for rid := 0; rid < world.DefaultBlockRegistry.BlockCount(); rid++ {
		if s := int32(java261.JavaState(uint32(rid))); s != 0 && isAir(s) {
			mapped++
		}
	}
	t.Logf("%d runtime IDs map to cave or void air", mapped)
}
