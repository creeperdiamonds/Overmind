package worldgen

import "testing"

// mergeCase runs the merge on one column: a base, and the writes of an earlier and a later source.
func mergeCase(base map[int]bs, earlier, later map[int]bs) *[chunkCells]bs {
	final := new([chunkCells]bs)
	for y, s := range base {
		final[cellIndex(0, y, 0)] = s
	}
	var sources [9][]sourceChange
	var want [9]int8
	for i, writes := range []map[int]bs{earlier, later} {
		for y, now := range writes {
			c := int32(cellIndex(0, y, 0))
			sources[i] = append(sources[i], sourceChange{4, c, final[c], now})
		}
		want[i] = 4
	}
	for i := 2; i < 9; i++ {
		want[i] = 4
	}
	var m merger
	m.apply(final, &sources, &want)
	return final
}

func TestMergeKeepsDoublePlantsWhole(t *testing.T) {
	grass, short := stateOf("grass_block[snowy=false]"), stateOf("short_grass")
	lower, upper := stateOf("tall_grass[half=lower]"), stateOf("tall_grass[half=upper]")
	base := map[int]bs{70: grass}
	// Tall grass first: the later short grass finds it in its way.
	f := mergeCase(base, map[int]bs{71: lower, 72: upper}, map[int]bs{71: short})
	if f[cellIndex(0, 71, 0)] != lower || f[cellIndex(0, 72, 0)] != upper {
		t.Errorf("tall grass then short grass: %s / %s", f[cellIndex(0, 71, 0)].name(), f[cellIndex(0, 72, 0)].name())
	}
	// Short grass first: the later tall grass is refused whole, not left as a floating top.
	f = mergeCase(base, map[int]bs{71: short}, map[int]bs{71: lower, 72: upper})
	if f[cellIndex(0, 71, 0)] != short || !f[cellIndex(0, 72, 0)].isAir() {
		t.Errorf("short grass then tall grass: %s / %s", f[cellIndex(0, 71, 0)].name(), f[cellIndex(0, 72, 0)].name())
	}
}

func TestMergeFollowsReplaceRules(t *testing.T) {
	stone, granite, andesite, coal := stateOf("stone"), stateOf("granite"), stateOf("andesite"), stateOf("coal_ore")
	log, leaves := stateOf("oak_log[axis=y]"), stateOf("oak_leaves[distance=1,persistent=false,waterlogged=false]")
	litter := stateOf("leaf_litter[facing=north,segment_amount=1]")
	for _, c := range []struct {
		what            string
		base, a, b, out bs
	}{
		{"a rock blob over an earlier one: base stone both", stone, granite, andesite, andesite},
		{"an ore over an earlier ore: not base stone", stone, coal, andesite, coal},
		{"a trunk through earlier leaves", sAir, leaves, log, log},
		{"leaf litter under an earlier trunk", sAir, log, litter, log},
		{"a trunk over earlier leaf litter", sAir, litter, log, log},
	} {
		f := mergeCase(map[int]bs{50: c.base}, map[int]bs{50: c.a}, map[int]bs{50: c.b})
		if got := f[cellIndex(0, 50, 0)]; got != c.out {
			t.Errorf("%s: %s, want %s", c.what, got.name(), c.out.name())
		}
	}
}

func TestMergeKeepsPlantsOnTheirSoil(t *testing.T) {
	grass, gravel, short := stateOf("grass_block[snowy=false]"), stateOf("gravel"), stateOf("short_grass")
	lower, upper := stateOf("peony[half=lower]"), stateOf("peony[half=upper]")
	base := map[int]bs{62: grass}
	// A later disk would turn the grass under an earlier flower to gravel: refused, as the flower holds.
	f := mergeCase(base, map[int]bs{63: lower, 64: upper}, map[int]bs{62: gravel})
	if f[cellIndex(0, 62, 0)] != grass || f[cellIndex(0, 63, 0)] != lower {
		t.Errorf("flower then gravel: %s under %s", f[cellIndex(0, 62, 0)].name(), f[cellIndex(0, 63, 0)].name())
	}
	// A later flower on grass an earlier disk turned to gravel: refused, as Java's survival test would.
	f = mergeCase(base, map[int]bs{62: gravel}, map[int]bs{63: short})
	if f[cellIndex(0, 62, 0)] != gravel || !f[cellIndex(0, 63, 0)].isAir() {
		t.Errorf("gravel then grass plant: %s under %s", f[cellIndex(0, 62, 0)].name(), f[cellIndex(0, 63, 0)].name())
	}
	// Dirt for grass under a plant is the same soil.
	f = mergeCase(base, map[int]bs{63: short}, map[int]bs{62: sDirt})
	if f[cellIndex(0, 62, 0)] != sDirt {
		t.Errorf("grass plant then dirt: %s", f[cellIndex(0, 62, 0)].name())
	}
}

// TestAirIsStateZero: a new working space is all zero, which must read as air. A package-level table
// that makes states can run before states.go's and take zero first unless it asks for air.
func TestAirIsStateZero(t *testing.T) {
	if sAir != 0 || bs(0).name() != "air" {
		t.Fatalf("state zero is %q", bs(0).name())
	}
}
