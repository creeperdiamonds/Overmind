package worldgen

import (
	"slices"
	"testing"
)

// TestNetherChunkShape checks a Nether chunk without any data: the bedrock floor and roof rows as the
// code lays them (2 to 5 rows of floor, 1 to 5 of roof), netherrack, the lava sea below Y 32 only, and
// the same chunk twice.
func TestNetherChunkShape(t *testing.T) {
	n := newNetherGen(100, nil)
	var a, b [netherHeight * 256]bs
	lava, rack := 0, 0
	for cx := -2; cx < 2; cx++ {
		for cz := -2; cz < 2; cz++ {
			n.cells(cx, cz, &a)
			n.cells(cx, cz, &b)
			if a != b {
				t.Fatalf("chunk %d,%d differs between two builds", cx, cz)
			}
			for x := range 16 {
				for z := range 16 {
					floor, roof := 0, 0
					for y := 0; y < 6 && a[y<<8|z<<4|x] == sBedrock; y++ {
						floor++
					}
					for y := netherHeight - 1; y > 121 && a[y<<8|z<<4|x] == sBedrock; y-- {
						roof++
					}
					if floor < 2 || floor > 5 || roof < 1 || roof > 5 {
						t.Fatalf("column %d,%d of chunk %d,%d: %d floor and %d roof rows", x, z, cx, cz, floor, roof)
					}
					for y := range netherHeight {
						switch a[y<<8|z<<4|x] {
						case sLava:
							lava++
							if y >= netherLava {
								t.Fatalf("lava at Y %d, above the sea", y)
							}
						case sNetherrack:
							rack++
						}
					}
				}
			}
		}
	}
	if lava == 0 || rack == 0 {
		t.Errorf("%d lava and %d netherrack blocks in 16 chunks", lava, rack)
	}
}

// TestNetherVineStates: weeping and twisting vines resolve to Bedrock blocks with their age.
func TestNetherVineStates(t *testing.T) {
	for _, n := range []string{"weeping_vines[age=0]", "weeping_vines[age=25]", "twisting_vines[age=7]", "weeping_vines_plant", "twisting_vines_plant"} {
		s, err := safeState(n)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		name, props := dragonflyBlock(s.name()).EncodeBlock()
		if name != "minecraft:weeping_vines" && name != "minecraft:twisting_vines" || len(props) != 1 {
			t.Errorf("%s: %s %v", n, name, props)
		}
	}
}

// TestNetherWithoutData generates Overjava's Nether, which reads no install: the code's parts are all
// there (surfaces of the biomes there, caves, glowstone, quartz, gold, magma, fire, mushrooms), and nothing
// the install's rules place (fungi, roots, huge fungi, deltas' basalt columns).
func TestNetherWithoutData(t *testing.T) {
	g := NewWithProfile(100, ProfileOverjava).Nether()
	if g.bdata != nil {
		t.Fatal("Overjava's Nether has data")
	}
	sc := newScratch()
	count := map[string]int{}
	for cx := -4; cx < 4; cx++ {
		for cz := -4; cz < 4; cz++ {
			cells := g.finalCells(sc, cx, cz)
			for y := range netherHeight {
				for i := range 256 {
					count[blockName[stateBlock[cells[cellIndex(i&15, y, i>>4)]]]]++
				}
			}
		}
	}
	for _, want := range []string{"soul_sand", "soul_soil", "crimson_nylium", "glowstone", "nether_quartz_ore", "nether_gold_ore", "magma_block", "fire", "gravel", "blackstone"} {
		if count[want] == 0 {
			t.Errorf("no %s in 64 chunks of Overjava's Nether: %v", want, count)
		}
	}
	for _, not := range []string{"crimson_fungus", "crimson_roots", "crimson_stem", "weeping_vines"} {
		if count[not] != 0 {
			t.Errorf("%d %s without data", count[not], not)
		}
	}
	// Caves: open cells inside the netherrack mass, which the terrain alone leaves solid more often.
	n := newNetherGen(100, nil)
	var with, without [netherHeight * 256]bs
	n.cells(0, 0, &with)
	n.seeds = bedrockSeeds{} // Any other seeds give other caves.
	n.cells(0, 0, &without)
	if with == without {
		t.Error("the cave carver changed nothing")
	}
}

// TestNetherSurfacesBuiltInMatchData holds the built-in Nether surface builders to the install's biome
// files.
func TestNetherSurfacesBuiltInMatchData(t *testing.T) {
	bd := testBedrockData(t)
	got, want := netherSurfacesBuiltIn(100), readNetherSurfaces(100, bd)
	for i := range got {
		g, w := got[i], want[i]
		if g.ok != w.ok || g.mats != w.mats || g.seaDepth != w.seaDepth || len(g.adj) != len(w.adj) || (g.capped == nil) != (w.capped == nil) {
			t.Errorf("biome %d: built in %+v, data %+v", i, g, w)
			continue
		}
		for k := range g.adj {
			if g.adj[k] != w.adj[k] {
				t.Errorf("biome %d adjustment %d: built in %+v, data %+v", i, k, g.adj[k], w.adj[k])
			}
		}
		if g.capped != nil && (!slices.Equal(g.capped.floor, w.capped.floor) || !slices.Equal(g.capped.ceil, w.capped.ceil) || g.capped.beach != w.capped.beach) {
			t.Errorf("biome %d capped: built in %v %v %v, data %v %v %v", i, g.capped.floor, g.capped.ceil, g.capped.beach, w.capped.floor, w.capped.ceil, w.capped.beach)
		}
	}
}
