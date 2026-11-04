package world_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
)

// Overmind fork: the weather check of a chunk tick (overmind_weather.go, block/overmind_weather.go).

const floorY = 70

// weatherWorld is a synchronous Overworld with every chunk checking a column each tick, raining or not,
// a stone floor over chunk 0, 0 at floorY and the biome b over it.
func weatherWorld(t *testing.T, java, raining bool, b world.Biome) *world.World {
	t.Helper()
	t.Cleanup(world.SetPrecipitationChance(1))
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: -1,
		Mechanics: world.Mechanics{JavaWeather: java}}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.Do(func(tx *world.Tx) {
		for x := range 16 {
			for z := range 16 {
				for y := floorY - 2; y <= floorY+10; y++ {
					tx.SetBiome(cube.Pos{x, y, z}, b)
				}
				tx.SetBlock(cube.Pos{x, floorY, z}, block.Stone{}, nil)
			}
		}
	})
	if raining {
		w.StartRaining(time.Hour)
	} else {
		w.StopRaining()
	}
	return w
}

func runTicks(w *world.World, n int) {
	for range n {
		w.AdvanceTick()
	}
}

// snowDepths counts the floor's columns by the layers of snow on them.
func snowDepths(w *world.World) map[int]int {
	depths := map[int]int{}
	w.Do(func(tx *world.Tx) {
		for x := range 16 {
			for z := range 16 {
				n := 0
				if s, ok := tx.Block(cube.Pos{x, floorY + 1, z}).(block.SnowLayer); ok {
					n = s.Height + 1
				}
				depths[n]++
			}
		}
	})
	return depths
}

// Snowfall builds snow to the biome's height on Bedrock, one layer on Java, and none where it rains.
func TestSnowfallBuildsLayers(t *testing.T) {
	for _, c := range []struct {
		biome world.Biome
		java  bool
		depth int
	}{
		{biome.SnowyPlains{}, false, 8},
		{biome.IceSpikes{}, false, 8},
		{biome.SnowyTaiga{}, false, 4},
		{biome.Grove{}, false, 2},
		{biome.FrozenRiver{}, false, 2},
		{biome.Taiga{}, false, 0}, // snows only above Y 120 or so
		{biome.Plains{}, false, 0},
		{biome.SnowyPlains{}, true, 1},
		{biome.Plains{}, true, 0},
	} {
		t.Run(fmt.Sprintf("%v java %v", c.biome, c.java), func(t *testing.T) {
			w := weatherWorld(t, c.java, true, c.biome)
			// 6000 checks over 256 columns: about 23 for each column.
			runTicks(w, 6000)
			depths := snowDepths(w)
			if depths[c.depth] < 250 {
				t.Errorf("%d of 256 columns have %d layers of snow, want nearly all: %v", depths[c.depth], c.depth, depths)
			}
		})
	}
	t.Run("no snow without rain", func(t *testing.T) {
		w := weatherWorld(t, false, false, biome.SnowyPlains{})
		runTicks(w, 3000)
		if d := snowDepths(w); d[0] != 256 {
			t.Errorf("snow lay with no snowfall: %v", d)
		}
	})
}

// Snowfall lies on what each edition lets snow lie on. Java 26.1 keeps it off ice, packed ice and barriers
// (the block tag cannot_support_snow_layer, minecraft.wiki "Block tag (Java Edition)" oldid 3877595: packed ice is in it
// until Java 26.4), and lets it lie on soul sand and mud (support_override_snow_layer; the fork has no honey block). Bedrock
// keeps it off ice only ("Snow": "a solid block that is not ice"), and Overmind off blue ice too, as before.
func TestSnowfallFloors(t *testing.T) {
	for _, c := range []struct {
		floor world.Block
		java  bool
		snow  bool
	}{
		{block.Ice{}, true, false},
		{block.PackedIce{}, true, false},
		{block.Barrier{}, true, false},
		{block.BlueIce{}, true, true},
		{block.SoulSand{}, true, true},
		{block.Mud{}, true, true},
		{block.Stone{}, true, true},
		{block.Ice{}, false, false},
		{block.BlueIce{}, false, false},
		{block.PackedIce{}, false, true},
		{block.Stone{}, false, true},
	} {
		t.Run(fmt.Sprintf("%T java %v", c.floor, c.java), func(t *testing.T) {
			w := weatherWorld(t, c.java, true, biome.SnowyPlains{})
			w.Do(func(tx *world.Tx) {
				for x := range 16 {
					for z := range 16 {
						tx.SetBlock(cube.Pos{x, floorY, z}, c.floor, nil)
					}
				}
			})
			runTicks(w, 2000)
			if d := snowDepths(w); c.snow && d[0] > 6 || !c.snow && d[0] != 256 {
				t.Errorf("snow on %T (java %v): want snow %v, got %v", c.floor, c.java, c.snow, d)
			}
		})
	}
}

// waterPool lays water sources over the floor, inside a ring of the floor's stone raised a block, and
// returns the sources.
func waterPool(w *world.World, edge world.Block) []cube.Pos {
	var water []cube.Pos
	w.Do(func(tx *world.Tx) {
		for x := 2; x <= 13; x++ {
			for z := 2; z <= 13; z++ {
				p := cube.Pos{x, floorY + 1, z}
				if x == 2 || x == 13 || z == 2 || z == 13 {
					tx.SetBlock(p, edge, nil)
					if _, leaves := edge.(block.Leaves); leaves {
						tx.SetLiquid(p, block.Water{Still: true, Depth: 8})
					}
					continue
				}
				tx.SetBlock(p, block.Water{Still: true, Depth: 8}, nil)
				water = append(water, p)
			}
		}
	})
	return water
}

func iceCount(w *world.World, at []cube.Pos) (n int) {
	w.Do(func(tx *world.Tx) {
		for _, p := range at {
			if _, ok := tx.Block(p).(block.Ice); ok {
				n++
			}
		}
	})
	return n
}

// Water freezes in a cold biome from its edge inwards, in any weather, and never in a warm one. A ring of
// waterlogged leaves lets it freeze on Bedrock and not on Java.
func TestWaterFreezes(t *testing.T) {
	waterlogged := block.Leaves{Type: block.OakLeaves(), Persistent: true}
	for _, c := range []struct {
		name   string
		biome  world.Biome
		java   bool
		edge   world.Block
		freeze bool
	}{
		{"cold", biome.SnowyPlains{}, false, block.Stone{}, true},
		{"cold java", biome.SnowyPlains{}, true, block.Stone{}, true},
		{"warm", biome.Plains{}, false, block.Stone{}, false},
		{"waterlogged edge", biome.SnowyPlains{}, false, waterlogged, true},
		{"waterlogged edge java", biome.SnowyPlains{}, true, waterlogged, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := weatherWorld(t, c.java, false, c.biome)
			water := waterPool(w, c.edge)
			runTicks(w, 6000)
			n := iceCount(w, water)
			if c.freeze && n < len(water)*9/10 {
				t.Errorf("%d of %d water sources froze, want nearly all", n, len(water))
			}
			if !c.freeze && n != 0 {
				t.Errorf("%d of %d water sources froze, want none", n, len(water))
			}
		})
	}
}
