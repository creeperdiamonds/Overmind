package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Overmind fork: the plants of 1.21.5 and later are registered with their items, break at once by hand
// and drop as in the game.
func TestOvermindPlantsBreak(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	hand := item.NewStack(item.Stick{}, 1)
	plants := []world.Block{block.Bush{}, block.FireflyBush{}, block.ShortDryGrass{}, block.TallDryGrass{}, block.CactusFlower{},
		block.LeafLitter{AdditionalCount: 2, Facing: cube.East}, block.Wildflowers{AdditionalCount: 3, Facing: cube.West}}
	for _, b := range plants {
		name, _ := b.EncodeBlock()
		if _, ok := world.BlockByRuntimeID(world.BlockRuntimeID(b)); !ok {
			t.Errorf("%s is not registered", name)
		}
		if _, ok := world.ItemByName(name, 0); !ok {
			t.Errorf("%s has no item", name)
		}
		if d := block.BreakDuration(b, hand, block.BreakContext{}); d != 0 {
			t.Errorf("%s takes %v by hand", name, d)
		}
	}
	drops := func(b world.Block, tool item.Tool) int {
		n := 0
		for _, s := range b.(block.Breakable).BreakInfo().Drops(tool, nil) {
			n += s.Count()
		}
		return n
	}
	if n := drops(block.Bush{}, item.ToolNone{}); n != 0 {
		t.Errorf("bush by hand drops %d", n)
	}
	if n := drops(block.Bush{}, item.Shears{}); n != 1 {
		t.Errorf("bush to shears drops %d", n)
	}
	if n := drops(block.FireflyBush{}, item.ToolNone{}); n != 1 {
		t.Errorf("firefly bush drops %d", n)
	}
	if n := drops(block.LeafLitter{AdditionalCount: 2}, item.ToolNone{}); n != 3 {
		t.Errorf("three leaf litter pieces drop %d", n)
	}
	if !(block.Bush{}).ReplaceableBy(block.Stone{}) || !(block.LeafLitter{}).ReplaceableBy(block.Stone{}) {
		t.Errorf("bush and leaf litter are replaceable")
	}
}
