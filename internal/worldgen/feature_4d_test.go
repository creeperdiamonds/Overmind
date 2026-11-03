package worldgen

import (
	"strings"
	"testing"
)

// The features of Task 4d that the trace fixtures hold no Java run of (coral, icebergs, cherry and
// meadow flowers, dry grass, firefly bushes, the mushroom island, mangroves, the pale garden, desert
// wells) are checked here for showing up where Java puts them, in both profiles, in Bedrock blocks,
// and for placing only states with a block (a state with none panics under test). Their parameters
// are held against Java's files by TestPlacementsFollowJava4d and TestMangrovesFollowJava, and their
// draw orders by the rule tests beside them; no traced chunk holds these biomes, so their exact
// shapes are not checked against a Java run. Each floor is about a quarter of what seed 7 places
// now (the profiles agree), so a feature that stops placing, or places far less, fails.
func TestUntracedFeaturesAppear(t *testing.T) {
	cases := []struct {
		name   string
		biomes []string
		want   func(n string, y int16) bool
		least  int
	}{
		{"coral", []string{"warm_ocean"}, func(n string, _ int16) bool { return strings.HasSuffix(n, "_coral_block") }, 450},
		{"coral fans", []string{"warm_ocean"}, func(n string, _ int16) bool { return strings.HasSuffix(n, "_coral_wall_fan") }, 300},
		{"icebergs", []string{"frozen_ocean", "deep_frozen_ocean"}, func(n string, y int16) bool { return n == "packed_ice" && y > SeaLevel }, 1600},
		{"cherry petals", []string{"cherry_grove"}, func(n string, _ int16) bool { return n == "pink_petals" }, 450},
		{"meadow flowers", []string{"meadow"}, func(n string, _ int16) bool {
			return n == "allium" || n == "cornflower" || n == "oxeye_daisy" || n == "azure_bluet"
		}, 50},
		{"wildflowers", []string{"meadow", "birch_forest"}, func(n string, _ int16) bool { return n == "wildflowers" }, 75},
		{"dry grass", []string{"desert"}, func(n string, _ int16) bool { return n == "short_dry_grass" || n == "tall_dry_grass" }, 24},
		{"firefly bushes", []string{"swampland"}, func(n string, _ int16) bool { return n == "firefly_bush" }, 1},
		{"mangrove roots", []string{"mangrove_swamp"}, func(n string, _ int16) bool { return n == "mangrove_roots" }, 650},
		{"mangrove trunks", []string{"mangrove_swamp"}, func(n string, _ int16) bool { return n == "mangrove_log" }, 400},
		{"mangrove propagules", []string{"mangrove_swamp"}, func(n string, _ int16) bool { return n == "mangrove_propagule" }, 100},
		{"pale oaks", []string{"pale_garden"}, func(n string, _ int16) bool { return n == "pale_oak_log" }, 1000},
		{"pale moss", []string{"pale_garden"}, func(n string, _ int16) bool { return n == "pale_moss_block" }, 1000},
		{"pale hanging moss", []string{"pale_garden"}, func(n string, _ int16) bool { return n == "pale_hanging_moss" }, 350},
		{"creaking hearts", []string{"pale_garden"}, func(n string, _ int16) bool { return n == "creaking_heart" }, 1},
		{"eyeblossoms", []string{"pale_garden"}, func(n string, _ int16) bool { return n == "closed_eyeblossom" }, 6},
		{"huge mushrooms", []string{"mushroom_island"}, func(n string, _ int16) bool { return strings.HasSuffix(n, "_mushroom_block") }, 240},
	}
	// Cases on the same biomes share one sweep, made once for each profile.
	type group struct {
		biomes []string
		cases  []int
	}
	var groups []*group
	byKey := map[string]*group{}
	for i, c := range cases {
		k := strings.Join(c.biomes, " ")
		if byKey[k] == nil {
			byKey[k] = &group{biomes: c.biomes}
			groups = append(groups, byKey[k])
		}
		byKey[k].cases = append(byKey[k].cases, i)
	}
	for _, prof := range []*Profile{ProfileOverjava, ProfileBedrock} {
		for _, gr := range groups {
			t.Run(prof.Name+"/"+strings.Join(gr.biomes, "+"), func(t *testing.T) {
				t.Parallel()
				g := NewWithProfile(7, prof)
				chunks := biomeChunks(g, 3, gr.biomes...)
				if len(chunks) == 0 {
					t.Fatalf("no %v chunk found", gr.biomes)
				}
				var pos [][2]int32
				for _, p := range chunks {
					pos = append(pos, blockAround(p, 1)...)
				}
				found := make([]int, len(gr.cases))
				for _, ch := range generateAll(g, pos) {
					for x := range uint8(16) {
						for z := range uint8(16) {
							for y := int16(minY); y <= maxY; y++ {
								n := nameOf(ch.Block(x, y, z, 0))
								for j, i := range gr.cases {
									if cases[i].want(n, y) {
										found[j]++
									}
								}
							}
						}
					}
				}
				for j, i := range gr.cases {
					c := cases[i]
					t.Logf("%s: %d in %d chunks", c.name, found[j], len(pos))
					if found[j] < c.least {
						t.Errorf("%s: %d blocks in %d chunks, want at least %d", c.name, found[j], len(pos), c.least)
					}
				}
			})
		}
	}
}

// TestDesertWellShape places a desert well by hand on desert sand (one chunk in a thousand has one,
// too few to find by sweeping) and checks it cell by cell against DesertWellFeature: a 5 x 5 sandstone
// basin three deep, water in the five middle cells with sand under them, a rim with a slab at the
// middle of each side, four pillars and a roof of slabs round a sandstone middle. The two suspicious
// sand blocks go under two of the five middle cells, drawn in Java's order (middle, east, south,
// west, north): one in the sand layer, one in the sandstone under it. Everything else is left as it
// was. It refuses ground that is not sand, and ground with a hole two deep under its 5 x 5.
func TestDesertWellShape(t *testing.T) {
	g := NewWithProfile(7, ProfileOverjava)
	chunks := biomeChunks(g, 1, "desert")
	if len(chunks) == 0 {
		t.Fatal("no desert chunk")
	}
	cx, cz := int(chunks[0][0]), int(chunks[0][1])
	var well *placement
	for _, pl := range g.profile.placements {
		if pl.name == "desert_well" {
			well = pl
		}
	}
	if well == nil {
		t.Fatal("no desert_well placement")
	}
	d := testDecoration(t, g, cx, cz)
	x, z := cx*16+8, cz*16+8
	y := d.height(hmMotionBlocking, x, z)
	o := pos{x, y - 1, z}
	before := map[pos]bs{}
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			for dy := -3; dy <= 5; dy++ {
				before[o.add(dx, dy, dz)] = d.at(o.add(dx, dy, dz))
			}
		}
	}
	draws := d.r
	if !well.feat(d, pos{x, y, z}) {
		t.Fatalf("no well at %d %d %d on %s", x, y, z, d.block(x, y-1, z).name())
	}
	middle := [5]pos{o, o.step(dEast), o.step(dSouth), o.step(dWest), o.step(dNorth)}
	sus1, sus2 := middle[draws.nextInt(5)].add(0, -1, 0), middle[draws.nextInt(5)].add(0, -2, 0)
	if d.r != draws {
		t.Error("the well drew other than its two suspicious sand picks")
	}
	abs := func(v int) int { return max(v, -v) }
	want := func(dx, dy, dz int) bs {
		inPlus := abs(dx)+abs(dz) <= 1
		switch p := o.add(dx, dy, dz); {
		case p == sus1 || p == sus2:
			return sSuspicious
		case dy == 0 && inPlus:
			return sWater
		case dy == -1 && inPlus:
			return sSand
		case dy >= -2 && dy <= 0:
			return sSandst
		case dy == 1 && (abs(dx) == 2 || abs(dz) == 2):
			if dx == 0 || dz == 0 {
				return sSandSlab
			}
			return sSandst
		case dy >= 1 && dy <= 3 && abs(dx) == 1 && abs(dz) == 1:
			return sSandst
		case dy == 4 && abs(dx) <= 1 && abs(dz) <= 1:
			if dx == 0 && dz == 0 {
				return sSandst
			}
			return sSandSlab
		}
		return before[o.add(dx, dy, dz)]
	}
	sus := 0
	for p, old := range before {
		got, w := d.at(p), want(p.x-o.x, p.y-o.y, p.z-o.z)
		if got != w {
			t.Errorf("%+v (was %s): %s, want %s", pos{p.x - o.x, p.y - o.y, p.z - o.z}, old.name(), got.name(), w.name())
		}
		if got == sSuspicious {
			sus++
		}
		runtimeOf(got)
	}
	if sus != 2 {
		t.Errorf("%d suspicious sand, want 2", sus)
	}
	d.undo()

	// Refusals, high in the air where nothing else is: a lone sand block has air two deep under it,
	// and a stone floor is not sand. Neither writes anything.
	hi := pos{x, 250, z}
	for _, c := range []struct {
		what   string
		ground func()
	}{
		{"lone sand", func() { d.set(hi.x, hi.y, hi.z, sSand) }},
		{"stone floor", func() {
			for dx := -2; dx <= 2; dx++ {
				for dz := -2; dz <= 2; dz++ {
					for dy := -2; dy <= 0; dy++ {
						d.set(hi.x+dx, hi.y+dy, hi.z+dz, sStone)
					}
				}
			}
		}},
	} {
		c.ground()
		n := len(d.log)
		if well.feat(d, hi.add(0, 3, 0)) {
			t.Errorf("%s: a well was placed", c.what)
		}
		if len(d.log) != n {
			t.Errorf("%s: a refused well wrote %d blocks", c.what, len(d.log)-n)
		}
		d.undo()
	}
}
