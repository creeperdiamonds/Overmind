package block_test

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// Overmind fork: lava runs 3 blocks, one every 30 game ticks, in the Overworld and the End, and 7 blocks,
// one every 10 game ticks, in the Nether (minecraft.wiki "Lava", oldid 3870569, both editions).
func TestLavaFlowByDimension(t *testing.T) {
	for _, c := range []struct {
		dim        world.Dimension
		reach, gap int
	}{
		{world.Overworld, 3, 30},
		{world.End, 3, 30},
		{world.Nether, 7, 10},
	} {
		t.Run(fmt.Sprint(c.dim), func(t *testing.T) {
			w := world.Config{Synchronous: true, Dim: c.dim, Entities: entity.DefaultRegistry}.New()
			defer w.Close()
			y := 10
			w.Do(func(tx *world.Tx) {
				for x := -12; x <= 12; x++ {
					for z := -12; z <= 12; z++ {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{0, y + 1, 0}, block.Lava{Still: true, Depth: 8}, nil)
			})
			lavaAt := func(x int) (ok bool) {
				w.Do(func(tx *world.Tx) { _, ok = tx.Block(cube.Pos{x, y + 1, 0}).(block.Lava) })
				return ok
			}
			// The tick at which lava first reaches each block east of the source.
			arrived := map[int]int{}
			for tick := 1; tick <= 400; tick++ {
				w.AdvanceTick()
				for x := 1; x <= c.reach+1; x++ {
					if _, seen := arrived[x]; !seen && lavaAt(x) {
						arrived[x] = tick
					}
				}
			}
			for x := 1; x <= c.reach; x++ {
				if _, ok := arrived[x]; !ok {
					t.Fatalf("lava did not reach %d blocks from its source (reached %v)", x, arrived)
				}
			}
			if at, ok := arrived[c.reach+1]; ok {
				t.Fatalf("lava reached %d blocks from its source, at tick %d; want %d at most", c.reach+1, at, c.reach)
			}
			for x := 2; x <= c.reach; x++ {
				if gap := arrived[x] - arrived[x-1]; gap != c.gap {
					t.Errorf("lava took %d ticks from block %d to %d, want %d (arrivals %v)", gap, x-1, x, c.gap, arrived)
				}
			}
		})
	}
}
