package structures

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/inventory"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// TestBoxesFollowRotation holds the element box and point rotation rules of N2 1.2.
func TestBoxesFollowRotation(t *testing.T) {
	e := &element{template: &Template{Size: [3]int{3, 2, 5}}}
	p := [3]int{10, 0, 20}
	want := [4]box{{10, 0, 20, 12, 1, 24}, {6, 0, 20, 10, 1, 22}, {8, 0, 16, 10, 1, 20}, {10, 0, 18, 14, 1, 20}}
	for rot, w := range want {
		if got := e.bbox(p, rot); got != w {
			t.Errorf("rot %d: box %v, want %v", rot, got, w)
		}
		// Every corner of the template, turned, lands in the box.
		for _, c := range [][3]int{{0, 0, 0}, {2, 1, 4}, {2, 0, 0}, {0, 1, 4}} {
			q := rotate(c, rot)
			if !w.inside([3]int{p[0] + q[0], q[1], p[2] + q[2]}) {
				t.Errorf("rot %d: corner %v turns to %v, outside %v", rot, c, q, w)
			}
		}
	}
	if f := rotateFace(2, 1); f != 5 { // North turns east (N2 1.2).
		t.Errorf("north turned once is %d, want east (5)", f)
	}
}

// TestStartsDoNotDependOnOrder asks the same chunks in two orders, from two Structures of one seed.
func TestStartsDoNotDependOnOrder(t *testing.T) {
	g := worldgen.NewWithData(100, worldgen.ProfileBedrock, nil)
	a, b := New(g, nil), New(g, nil)
	kinds := []Kind{BuriedTreasure, RuinedPortal, Mineshaft, TrialChambers, TrailRuins, Outpost, AncientCity}
	got := map[[3]int]bool{}
	for cx := -20; cx <= 20; cx++ {
		for cz := -20; cz <= 20; cz++ {
			for _, k := range kinds {
				_, ok := a.StartIn(k, cx, cz)
				got[[3]int{int(k), cx, cz}] = ok
			}
		}
	}
	for cx := 20; cx >= -20; cx-- {
		for cz := 20; cz >= -20; cz-- {
			for _, k := range kinds {
				if _, ok := b.StartIn(k, cx, cz); ok != got[[3]int{int(k), cx, cz}] {
					t.Fatalf("%v at (%d,%d) differs by order", k, cx, cz)
				}
			}
		}
	}
}

// TestNearestStrongholdIsNearest checks the eye of ender's answer against a scan of the grid
// strongholds (no install: villages, so ring strongholds, need the biome files).
func TestNearestStrongholdIsNearest(t *testing.T) {
	g := worldgen.NewWithData(1, worldgen.ProfileBedrock, nil)
	s := New(g, nil)
	for _, from := range []cube.Pos{{0, 64, 0}, {40000, 64, -12000}, {-25000, 64, 31000}} {
		at, ok := s.NearestStronghold(from)
		if !ok {
			t.Fatalf("no stronghold from %v", from)
		}
		cx, cz := floorDiv(at[0], 16), floorDiv(at[2], 16)
		if !s.strongholdAt(cx, cz) {
			t.Fatalf("from %v: %v is not a stronghold start", from, at)
		}
		pcx, pcz := floorDiv(from[0], 16), floorDiv(from[2], 16)
		d := (cx-pcx)*(cx-pcx) + (cz-pcz)*(cz-pcz)
		for i := -3; i <= 3; i++ {
			for j := -3; j <= 3; j++ {
				if c, ok := s.gridStronghold(pcx+i*gridCell, pcz+j*gridCell); ok {
					if e := (c[0]-pcx)*(c[0]-pcx) + (c[1]-pcz)*(c[1]-pcz); e < d {
						t.Fatalf("from %v: %v is nearer than %v", from, c, at)
					}
				}
			}
		}
	}
}

// TestChestKeepsItsLootTable round-trips the fork's chest fields through NBT, as a saved world does.
func TestChestKeepsItsLootTable(t *testing.T) {
	c := block.NewChest().DecodeNBT(map[string]any{"LootTable": treasureTable, "LootTableSeed": int32(-7)}).(block.Chest)
	if c.LootTable != treasureTable || c.LootTableSeed != -7 {
		t.Fatalf("decoded %q %d", c.LootTable, c.LootTableSeed)
	}
	m := c.EncodeNBT()
	if m["LootTable"] != treasureTable || m["LootTableSeed"] != int32(-7) {
		t.Fatalf("encoded %v", m)
	}
}

// TestLootFillsFromTheInstall rolls buried treasure's table with a measured loot seed: the same items
// every time, and the table's one-roll heart of the sea is there.
func TestLootFillsFromTheInstall(t *testing.T) {
	_, d := installData(t)
	fill := func() *inventory.Inventory {
		inv := inventory.New(27, nil)
		if !d.Fill(treasureTable, 1693768622, inv) {
			t.Fatal("no buried treasure table in the install")
		}
		return inv
	}
	a, b := fill(), fill()
	heart := false
	for i := range 27 {
		x, _ := a.Item(i)
		y, _ := b.Item(i)
		if !x.Equal(y) || x.Count() != y.Count() {
			t.Fatalf("slot %d differs between two rolls: %v %v", i, x, y)
		}
		if !x.Empty() {
			name, _ := x.Item().EncodeItem()
			heart = heart || name == "minecraft:heart_of_the_sea"
			t.Logf("slot %d: %s x%d", i, name, x.Count())
		}
	}
	if !heart {
		t.Error("buried treasure without a heart of the sea")
	}
	if len(d.Problems) > 0 {
		t.Logf("%d data problems, first: %s", len(d.Problems), d.Problems[0])
	}
}
