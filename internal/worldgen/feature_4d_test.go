package worldgen

import (
	"strings"
	"testing"
)

// The features of Task 4d that the trace fixtures hold no Java run of (coral, icebergs, cherry and
// meadow flowers, dry grass, firefly bushes, the mushroom island, mangroves, the pale garden, desert
// wells) are checked here for showing up where Java puts them, in Bedrock blocks, and for placing
// only states with a block (a state with none panics under test). Their exact shapes are not checked
// against Java: no traced chunk holds those biomes.
func TestUntracedFeaturesAppear(t *testing.T) {
	cases := []struct {
		name   string
		biomes []string
		want   func(n string, y int16) bool
		least  int
	}{
		{"coral", []string{"warm_ocean"}, func(n string, _ int16) bool { return strings.HasSuffix(n, "_coral_block") }, 50},
		{"coral fans", []string{"warm_ocean"}, func(n string, _ int16) bool { return strings.HasSuffix(n, "_coral_wall_fan") }, 1},
		{"icebergs", []string{"frozen_ocean", "deep_frozen_ocean"}, func(n string, y int16) bool { return n == "packed_ice" && y > SeaLevel }, 20},
		{"cherry petals", []string{"cherry_grove"}, func(n string, _ int16) bool { return n == "pink_petals" }, 10},
		{"meadow flowers", []string{"meadow"}, func(n string, _ int16) bool { return n == "allium" || n == "cornflower" || n == "oxeye_daisy" || n == "azure_bluet" }, 1},
		{"wildflowers", []string{"meadow", "birch_forest"}, func(n string, _ int16) bool { return n == "wildflowers" }, 1},
		{"dry grass", []string{"desert"}, func(n string, _ int16) bool { return n == "short_dry_grass" || n == "tall_dry_grass" }, 1},
		{"firefly bushes", []string{"swampland"}, func(n string, _ int16) bool { return n == "firefly_bush" }, 1},
		{"mangrove roots", []string{"mangrove_swamp"}, func(n string, _ int16) bool { return n == "mangrove_roots" }, 50},
		{"mangrove trunks", []string{"mangrove_swamp"}, func(n string, _ int16) bool { return n == "mangrove_log" }, 50},
		{"mangrove propagules", []string{"mangrove_swamp"}, func(n string, _ int16) bool { return n == "mangrove_propagule" }, 1},
		{"pale oaks", []string{"pale_garden"}, func(n string, _ int16) bool { return n == "pale_oak_log" }, 100},
		{"pale moss", []string{"pale_garden"}, func(n string, _ int16) bool { return n == "pale_moss_block" }, 10},
		{"pale hanging moss", []string{"pale_garden"}, func(n string, _ int16) bool { return n == "pale_hanging_moss" }, 10},
		{"creaking hearts", []string{"pale_garden"}, func(n string, _ int16) bool { return n == "creaking_heart" }, 1},
		{"eyeblossoms", []string{"pale_garden"}, func(n string, _ int16) bool { return n == "closed_eyeblossom" }, 1},
		{"huge mushrooms", []string{"mushroom_island"}, func(n string, _ int16) bool { return strings.HasSuffix(n, "_mushroom_block") }, 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g := New(7)
			chunks := biomeChunks(g, 3, c.biomes...)
			if len(chunks) == 0 {
				t.Fatalf("no %v chunk found", c.biomes)
			}
			var pos [][2]int32
			for _, p := range chunks {
				pos = append(pos, blockAround(p, 1)...)
			}
			found := 0
			for _, ch := range generateAll(g, pos) {
				for x := range uint8(16) {
					for z := range uint8(16) {
						for y := int16(minY); y <= maxY; y++ {
							if c.want(nameOf(ch.Block(x, y, z, 0)), y) {
								found++
							}
						}
					}
				}
			}
			t.Logf("%d in %d chunks", found, len(pos))
			if found < c.least {
				t.Errorf("%s: %d blocks in %d chunks, want at least %d", c.name, found, len(pos), c.least)
			}
		})
	}
}

// TestDesertWellShape places a desert well by hand on desert sand (one chunk in a thousand has one,
// too few to find by sweeping) and checks its shape and that its blocks map to Bedrock's.
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
	s := newScratch()
	d := s.startDecoration(g, cx, cz)
	d.cx, d.cz = cx, cz
	d.gen++
	d.r = featRand{}
	d.r.setSeed(1)
	x, z := cx*16+8, cz*16+8
	y := d.height(hmMotionBlocking, x, z)
	if !well.feat(d, pos{x, y, z}) {
		t.Fatalf("no well at %d %d %d on %s", x, y, z, d.block(x, y-1, z).name())
	}
	o := pos{x, y - 1, z}
	for _, c := range []struct {
		p    pos
		want bs
	}{{o, sWater}, {o.step(dNorth), sWater}, {o.add(2, 0, 2), sSandst}, {o.add(1, 2, 1), sSandst}, {o.add(0, 4, 0), sSandst}, {o.add(1, 4, 0), sSandSlab}, {o.add(2, 1, 0), sSandSlab}, {o.add(0, 1, 0), sAir}} {
		if got := d.at(c.p); got != c.want {
			t.Errorf("%v: %s, want %s", c.p, got.name(), c.want.name())
		}
	}
	sus := 0
	seen := map[[2]int32]bool{}
	for _, e := range d.log {
		if k := [2]int32{e.chunk, e.cell}; !seen[k] {
			seen[k] = true
			lv := &d.live[e.chunk]
			if s := lv.b.b[e.cell]; s == sSuspicious {
				sus++
			}
			runtimeOf(lv.b.b[e.cell])
		}
	}
	if sus == 0 || sus > 2 {
		t.Errorf("%d suspicious sand, want one or two", sus)
	}
	d.rollback()
}
