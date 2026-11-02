package worldgen

import (
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// blockNames maps every runtime ID to its block name and pillar axis, so these tests read blocks
// by what a client would call them and not through the generator's own table.
var blockNames = sync.OnceValue(func() []struct{ name, axis string } {
	world.DefaultBlockRegistry.Finalize()
	out := make([]struct{ name, axis string }, world.DefaultBlockRegistry.BlockCount())
	for i := range out {
		name, props, ok := world.DefaultBlockRegistry.RuntimeIDToState(uint32(i))
		if !ok {
			continue
		}
		out[i].name = strings.TrimPrefix(name, "minecraft:")
		if a, ok := props["pillar_axis"].(string); ok {
			out[i].axis = a
		}
	}
	return out
})

func nameOf(id uint32) string { return blockNames()[id].name }

func isLogID(id uint32) bool    { return strings.HasSuffix(nameOf(id), "_log") }
func isLeavesID(id uint32) bool { return strings.HasSuffix(nameOf(id), "_leaves") }

// isTrunkID is a log that stands upright. Sideways logs are branches.
func isTrunkID(id uint32) bool { return isLogID(id) && blockNames()[id].axis == "y" }

// isPlantName is a block placed by the decoration pass that needs support below it.
func isPlantName(n string) bool {
	switch n {
	case "short_grass", "fern", "tall_grass", "large_fern", "deadbush", "cactus", "reeds", "kelp", "seagrass", "waterlily",
		"dandelion", "poppy", "blue_orchid", "allium", "azure_bluet", "red_tulip", "orange_tulip", "white_tulip", "pink_tulip",
		"oxeye_daisy", "cornflower", "lily_of_the_valley", "sunflower", "lilac", "rose_bush", "peony":
		return true
	}
	return false
}

// world3 reads blocks by world position over a set of generated chunks.
type world3 map[[2]int32]*chunk.Chunk

func (w world3) at(x, y, z int) (uint32, bool) {
	c, ok := w[[2]int32{int32(x >> 4), int32(z >> 4)}]
	if !ok || y < minY || y > maxY {
		return 0, false
	}
	return c.Block(uint8(x&15), int16(y), uint8(z&15), 0), true
}

// biomeChunks finds up to n chunks whose corners and centre are all in one of the named biomes.
func biomeChunks(g *Generator, n int, names ...string) [][2]int32 {
	var out [][2]int32
	for x := -8192; x < 8192 && len(out) < n; x += 64 {
		for z := -8192; z < 8192 && len(out) < n; z += 64 {
			whole := true
			for _, o := range [5][2]int{{8, 8}, {1, 1}, {14, 1}, {1, 14}, {14, 14}} {
				if !slices.Contains(names, g.BiomeAt(x&^15+o[0], maxY, z&^15+o[1]).String()) {
					whole = false
					break
				}
			}
			if whole {
				out = append(out, [2]int32{int32(x >> 4), int32(z >> 4)})
			}
		}
	}
	return out
}

// generateAll generates chunks in parallel.
func generateAll(g *Generator, pos [][2]int32) world3 {
	w := make(world3, len(pos))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, p := range pos {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			c := generate(g, p[0], p[1])
			mu.Lock()
			w[p] = c
			mu.Unlock()
		}()
	}
	wg.Wait()
	return w
}

// blockAround gives a square of chunks around a point.
func blockAround(p [2]int32, radius int32) [][2]int32 {
	var out [][2]int32
	for x := p[0] - radius; x <= p[0]+radius; x++ {
		for z := p[1] - radius; z <= p[1]+radius; z++ {
			out = append(out, [2]int32{x, z})
		}
	}
	return out
}

// countTrees counts trees the way the Bedrock study did: a log on a ground block, with columns
// joined over 4 neighbours inside one chunk counting as one tree. It reports each tree's first
// column.
func countTrees(c *chunk.Chunk, visit func(x, z uint8)) int {
	var cols, seen [16][16]bool
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := int16(minY + 1); y <= maxY; y++ {
				if isLogID(c.Block(x, y, z, 0)) {
					switch nameOf(c.Block(x, y-1, z, 0)) {
					case "dirt", "coarse_dirt", "grass_block", "podzol", "mycelium", "dirt_with_roots", "mud", "moss_block",
						"pale_moss_block", "sand", "red_sand", "snow", "gravel", "clay", "muddy_mangrove_roots":
						cols[x][z] = true
					}
				}
			}
		}
	}
	trees := 0
	for x := range 16 {
		for z := range 16 {
			if !cols[x][z] || seen[x][z] {
				continue
			}
			trees++
			if visit != nil {
				visit(uint8(x), uint8(z))
			}
			stack := [][2]int{{x, z}}
			seen[x][z] = true
			for len(stack) > 0 {
				p := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					u, v := p[0]+d[0], p[1]+d[1]
					if u >= 0 && u < 16 && v >= 0 && v < 16 && cols[u][v] && !seen[u][v] {
						seen[u][v] = true
						stack = append(stack, [2]int{u, v})
					}
				}
			}
		}
	}
	return trees
}

// A tree crossing an edge must be whole, seen with the real neighbouring chunks. Every log above
// the first has a log or leaves connected to it. The top of every trunk has something on all four
// sides, across an edge too. Every leaf reaches a log through leaves within 4 steps, which is what
// keeps Dragonfly's leaves from decaying, so a tree cut off at an edge shows up as orphan leaves.
func TestTreesWholeAcrossEdges(t *testing.T) {
	g := New(41)
	var crossings, tops, edgeTops, leaves int
	seeds := biomeChunks(g, 8, "forest", "roofed_forest", "jungle", "savanna", "taiga", "birch_forest")
	seeds = append(seeds, biomeChunks(g, 3, "roofed_forest")...)
	seeds = append(seeds, biomeChunks(g, 3, "savanna")...)
	seeds = append(seeds, biomeChunks(g, 3, "jungle")...)
	if len(seeds) < 8 {
		t.Fatalf("only %d tree chunks found", len(seeds))
	}
	for _, p := range seeds {
		w := generateAll(g, blockAround(p, 2))
		// Only the middle 3 x 3 is checked, so that every neighbour read is a real chunk.
		for cx := p[0] - 1; cx <= p[0]+1; cx++ {
			for cz := p[1] - 1; cz <= p[1]+1; cz++ {
				c := w[[2]int32{cx, cz}]
				for lx := range 16 {
					for lz := range 16 {
						x, z := int(cx)*16+lx, int(cz)*16+lz
						for y := SeaLevel; y < 260; y++ {
							id := c.Block(uint8(lx), int16(y), uint8(lz), 0)
							switch {
							case isLeavesID(id):
								leaves++
								if !leafReachesLog(w, x, y, z) {
									t.Fatalf("chunk %v: leaves at %d %d %d reach no log within 4 steps", p, x, y, z)
								}
							case isLogID(id):
								below, _ := w.at(x, y-1, z)
								if isLogID(id) && !isLogID(below) && isTrunkID(id) && nameOf(below) != "air" && !isLeavesID(below) {
									continue // The first log of a trunk.
								}
								if !logConnected(w, x, y, z) {
									t.Fatalf("chunk %v: log at %d %d %d connects to no log or leaves", p, x, y, z)
								}
								edge := lx == 0 || lx == 15 || lz == 0 || lz == 15
								if edge && connectsAcrossEdge(w, x, y, z) {
									crossings++
								}
								above, _ := w.at(x, y+1, z)
								if isTrunkID(id) && !isLogID(above) {
									tops++
									if edge {
										edgeTops++
									}
									for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
										covered := false
										for dy := -1; dy <= 1; dy++ {
											if b, ok := w.at(x+d[0], y+dy, z+d[1]); ok && nameOf(b) != "air" {
												covered = true
											}
										}
										if !covered {
											t.Fatalf("chunk %v: trunk top at %d %d %d has nothing beside it towards %v", p, x, y, z, d)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d leaves, %d trunk tops (%d on chunk edges), %d edge logs joined across an edge", leaves, tops, edgeTops, crossings)
	if edgeTops < 30 || crossings < 100 {
		t.Fatalf("only %d trunk tops on edges and %d crossings; the test would prove little", edgeTops, crossings)
	}
}

func logConnected(w world3, x, y, z int) bool {
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				if b, ok := w.at(x+dx, y+dy, z+dz); ok && (isLogID(b) || isLeavesID(b)) {
					return true
				}
			}
		}
	}
	return false
}

func connectsAcrossEdge(w world3, x, y, z int) bool {
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if (x+dx)>>4 == x>>4 && (z+dz)>>4 == z>>4 {
				continue
			}
			for dy := -1; dy <= 1; dy++ {
				if b, ok := w.at(x+dx, y+dy, z+dz); ok && (isLogID(b) || isLeavesID(b)) {
					return true
				}
			}
		}
	}
	return false
}

// leafReachesLog is the rule leaves live by: a log within 4 steps, going through leaves.
func leafReachesLog(w world3, x, y, z int) bool {
	type node struct{ x, y, z, d int }
	seen := map[[3]int]bool{{x, y, z}: true}
	queue := []node{{x, y, z, 0}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
			u := [3]int{n.x + d[0], n.y + d[1], n.z + d[2]}
			if seen[u] {
				continue
			}
			seen[u] = true
			b, ok := w.at(u[0], u[1], u[2])
			if !ok {
				return true // Past the generated area, so nothing can be said.
			}
			if isLogID(b) {
				return true
			}
			if isLeavesID(b) && n.d < 3 {
				queue = append(queue, node{u[0], u[1], u[2], n.d + 1})
			}
		}
	}
	return false
}

// The same chunk whichever order chunks are made in, and on any number of workers, on chunks
// with trees crossing their edges.
func TestDecorationOrderIndependent(t *testing.T) {
	g := New(42)
	spots := biomeChunks(g, 3, "forest", "jungle", "roofed_forest")
	spots = append(spots, biomeChunks(g, 2, "snowy_plains", "taiga")...)
	spots = append(spots, biomeChunks(g, 2, "ocean", "frozen_ocean")...)
	for _, p := range spots {
		pos := blockAround(p, 1)
		want := map[[2]int32]*chunk.Chunk{}
		for _, q := range pos {
			want[q] = generate(New(42), q[0], q[1])
		}
		// Backwards, on one generator.
		h := New(42)
		for i := len(pos) - 1; i >= 0; i-- {
			if !generate(h, pos[i][0], pos[i][1]).Equals(want[pos[i]]) {
				t.Fatalf("chunk %v differs when made in reverse order", pos[i])
			}
		}
		// All at once, many times over.
		var wg sync.WaitGroup
		for k := range 36 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				q := pos[k%len(pos)]
				if !generate(g, q[0], q[1]).Equals(want[q]) {
					t.Errorf("chunk %v differs on a worker", q)
				}
			}()
		}
		wg.Wait()
	}
	if len(spots) < 4 {
		t.Fatalf("only %d chunks found", len(spots))
	}
}

func TestForestHasTreesDesertHasNone(t *testing.T) {
	var forest, forestTrees, desert int
	for _, seed := range []int64{1, 2, 3, 4} {
		g := New(seed)
		for _, c := range generateAll(g, biomeChunks(g, 40, "forest")) {
			forest++
			forestTrees += countTrees(c, nil)
		}
		for p, c := range generateAll(g, biomeChunks(g, 40, "desert")) {
			desert++
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(SeaLevel); y < 200; y++ {
						if id := c.Block(x, y, z, 0); isLogID(id) || isLeavesID(id) {
							t.Fatalf("seed %d chunk %v: %s in a desert", seed, p, nameOf(id))
						}
					}
				}
			}
		}
	}
	t.Logf("%d forest chunks, %.2f trees each; %d desert chunks", forest, float64(forestTrees)/float64(forest), desert)
	if forest < 40 || desert < 20 {
		t.Fatalf("only %d forest and %d desert chunks found", forest, desert)
	}
	if forestTrees < forest*2 {
		t.Errorf("%d trees in %d forest chunks", forestTrees, forest)
	}
}

// Every upright log either has a log below it or stands on dirt or grass. Sideways logs are
// branches and hang free.
func TestTreesStandOnSoil(t *testing.T) {
	var trunks int
	for _, seed := range []int64{5, 6} {
		g := New(seed)
		var pos [][2]int32
		for cx := int32(-200); cx < 200; cx += 9 {
			for cz := int32(-200); cz < 200; cz += 9 {
				pos = append(pos, [2]int32{cx, cz})
			}
		}
		for p, c := range generateAll(g, pos) {
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(minY + 1); y <= maxY; y++ {
						id := c.Block(x, y, z, 0)
						if !isTrunkID(id) {
							continue
						}
						below := c.Block(x, y-1, z, 0)
						if isLogID(below) {
							continue
						}
						trunks++
						if n := nameOf(below); n != "dirt" && n != "grass_block" {
							t.Fatalf("seed %d chunk %v (%d,%d,%d): trunk on %s", seed, p, x, y, z, n)
						}
					}
				}
			}
		}
	}
	t.Logf("%d trunks checked", trunks)
	if trunks < 1000 {
		t.Fatalf("only %d trunks", trunks)
	}
}

// Every plant sits on a valid block, over caves too: the sample holds all biomes and whatever the
// carvers opened under them. Plants on the outermost columns read the real neighbour for water.
func TestNoFloatingPlants(t *testing.T) {
	counts := map[string]int{}
	soil := []string{"grass_block", "dirt", "coarse_dirt", "podzol", "mycelium", "mud"}
	for _, seed := range []int64{7, 8, 9} {
		g := New(seed)
		var pos [][2]int32
		for cx := int32(-260); cx < 260; cx += 12 {
			for cz := int32(-260); cz < 260; cz += 12 {
				pos = append(pos, [2]int32{cx, cz})
			}
		}
		for p, c := range generateAll(g, pos) {
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(minY + 1); y <= maxY; y++ {
						id := c.Block(x, y, z, 0)
						n := nameOf(id)
						if !isPlantName(n) && n != "snow_layer" {
							continue
						}
						counts[n]++
						under := nameOf(c.Block(x, y-1, z, 0))
						ok := false
						switch n {
						case "cactus":
							ok = under == "sand" || under == "red_sand" || under == "cactus"
						case "deadbush":
							ok = under == "sand" || under == "red_sand" || strings.HasSuffix(under, "hardened_clay") || strings.HasSuffix(under, "terracotta") || slices.Contains(soil, under)
						case "reeds":
							ok = under == "reeds" || under == "sand" || under == "red_sand" || slices.Contains(soil, under)
						case "waterlily":
							ok = under == "water"
						case "kelp":
							ok = under == "kelp" || isGround(under)
							if nameOf(c.Block(x, y, z, 1)) != "water" {
								t.Fatalf("seed %d chunk %v: kelp at Y %d is not in water", seed, p, y)
							}
						case "seagrass":
							ok = isGround(under)
							if nameOf(c.Block(x, y, z, 1)) != "water" {
								t.Fatalf("seed %d chunk %v: seagrass at Y %d is not in water", seed, p, y)
							}
						case "snow_layer":
							ok = isGround(under) || isLogID(c.Block(x, y-1, z, 0)) || isLeavesID(c.Block(x, y-1, z, 0))
						case "tall_grass", "large_fern", "sunflower", "lilac", "rose_bush", "peony":
							// A double plant's lower half stands on soil; its upper half on its lower half.
							ok = under == n || slices.Contains(soil, under)
						default:
							ok = slices.Contains(soil, under)
						}
						if !ok {
							t.Fatalf("seed %d chunk %v (%d,%d,%d): %s on %s", seed, p, x, y, z, n, under)
						}
					}
				}
			}
		}
	}
	t.Logf("%v", counts)
	for _, n := range []string{"short_grass", "fern", "tall_grass", "large_fern", "dandelion", "poppy", "sunflower", "cactus", "reeds", "deadbush", "kelp", "seagrass", "waterlily", "snow_layer"} {
		if counts[n] == 0 {
			t.Errorf("no %s in the sample", n)
		}
	}
}

// isGround is any block that holds a plant up, by name.
func isGround(n string) bool {
	switch n {
	case "air", "water", "lava", "ice", "kelp", "seagrass", "snow_layer", "waterlily":
		return false
	}
	return !isPlantName(n)
}

func TestFrozenOceanHasIce(t *testing.T) {
	g := New(10)
	frozen := biomeChunks(g, 40, "frozen_ocean")
	warm := biomeChunks(g, 20, "ocean", "lukewarm_ocean", "warm_ocean")
	if len(frozen) < 10 || len(warm) < 10 {
		t.Fatalf("found %d frozen and %d warmer ocean chunks", len(frozen), len(warm))
	}
	var iced, wet int
	for _, c := range generateAll(g, frozen) {
		for x := range uint8(16) {
			for z := range uint8(16) {
				switch nameOf(c.Block(x, SeaLevel, z, 0)) {
				case "ice":
					iced++
					wet++
				case "water":
					wet++
				}
			}
		}
	}
	t.Logf("frozen ocean: %d of %d sea surface blocks are ice", iced, wet)
	if wet == 0 || iced*10 < wet*9 {
		t.Errorf("frozen ocean: only %d of %d surface blocks are ice", iced, wet)
	}
	for p, c := range generateAll(g, warm) {
		for x := range uint8(16) {
			for z := range uint8(16) {
				if nameOf(c.Block(x, SeaLevel, z, 0)) == "ice" {
					t.Fatalf("chunk %v: ice on a warmer ocean", p)
				}
			}
		}
	}
}

// Snow layers appear only where the biome's temperature, with the height drop, is cold; snowy
// plains and snowy taiga are nearly covered, since their surface is grass and relies on them; and
// the temperature read for Bedrock's warmer biomes holds at their usual heights.
func TestSnowOnlyWhereCold(t *testing.T) {
	var total, covered, columns int
	for _, seed := range []int64{11, 12} {
		g := New(seed)
		var pos [][2]int32
		for cx := int32(-300); cx < 300; cx += 10 {
			for cz := int32(-300); cz < 300; cz += 10 {
				pos = append(pos, [2]int32{cx, cz})
			}
		}
		for p, c := range generateAll(g, pos) {
			for x := range uint8(16) {
				for z := range uint8(16) {
					wx, wz := int(p[0])*16+int(x), int(p[1])*16+int(z)
					for y := int16(SeaLevel); y <= maxY; y++ {
						if nameOf(c.Block(x, y, z, 0)) != "snow_layer" {
							continue
						}
						total++
						b := g.BiomeAt(wx, maxY, wz)
						if !coldEnough(b, int(y)) {
							t.Fatalf("seed %d: snow at %d %d %d in %s (temperature %.2f)", seed, wx, y, wz, b.String(), b.Temperature())
						}
					}
				}
			}
		}
		for _, q := range biomeChunks(g, 40, "ice_plains", "cold_taiga") {
			c := generate(g, q[0], q[1])
			for x := range uint8(16) {
				for z := range uint8(16) {
					top := topSolid(c, x, z)
					if top < SeaLevel {
						continue
					}
					columns++
					for y := int16(top + 1); y < int16(top+24); y++ {
						if nameOf(c.Block(x, y, z, 0)) == "snow_layer" {
							covered++
							break
						}
					}
				}
			}
		}
	}
	t.Logf("%d snow layers; snowy plains and taiga: %d of %d land columns covered", total, covered, columns)
	if total == 0 || columns == 0 {
		t.Fatal("no snow in the sample")
	}
	if float64(covered) < 0.9*float64(columns) {
		t.Errorf("snowy plains and taiga: only %d of %d land columns have a snow layer", covered, columns)
	}
}

// coldEnough allows for the height drop generously: no biome warmer than the coldest non-snowy
// figure gets snow without being well above the ground of ordinary land.
func coldEnough(b world.Biome, y int) bool {
	t := b.Temperature()
	if b.String() == "deep_frozen_ocean" {
		t = 0 // Dragonfly reads 0.5 for it; the biome table puts it in the coldest row.
	}
	if t < 0.2 {
		return true
	}
	return y > 100 && t < 0.2+float64(y-100)*0.01
}

// Trees per chunk against Bedrock, measured the same way (BEDROCK-NOTES section 9): trees by the
// biome of their column, over chunks by their centre biome. Rare biomes rest on few Bedrock chunks,
// so a biome's tolerance is 30% or 0.3 trees, whichever is more, and biomes with under 60 of our
// chunks are logged only. Overall, the 2.217 is a mix of 4 small windows (0.84 to 4.06 by seed), so
// our rates are weighted by those windows' biome shares and must land within 15%.
func TestTreesMatchBedrock(t *testing.T) {
	figures := map[string]float64{
		"jungle": 10.03, "roofed_forest": 7.44, "pale_garden": 6.5, "forest": 5.92, "taiga": 5.18, "birch_forest": 5.04,
		"mega_taiga": 4.72, "grove": 4.49, "flower_forest": 4.06, "birch_forest_mutated": 3.64, "mesa_plateau_stone": 2.57,
		"cherry_grove": 2.41, "redwood_taiga_mutated": 1.89, "jungle_edge": 1.43, "savanna_plateau": 1.1, "savanna": 1.02, "meadow": 0.95,
		"plains": 0.31, "sunflower_plains": 0.07, "river": 0.11, "desert": 0,
	}
	shares := map[string]float64{
		"ocean": 13.96, "plains": 12.26, "forest": 12.22, "roofed_forest": 8.01, "deep_ocean": 6.68, "river": 5.97, "cold_ocean": 4.4,
		"birch_forest_mutated": 3.98, "savanna": 3.71, "birch_forest": 3.35, "deep_cold_ocean": 2.8, "flower_forest": 2.64,
		"mega_taiga": 2.13, "grove": 2.08, "savanna_plateau": 2.06, "mesa": 1.94, "stone_beach": 1.72, "meadow": 1.59, "frozen_peaks": 1.33,
		"beach": 1.07, "jungle_edge": 1.03, "desert": 1.01, "sunflower_plains": 0.74, "mesa_bryce": 0.58, "stony_peaks": 0.5,
		"dappled_forest": 0.32, "taiga": 0.29, "jungle": 0.27, "mesa_plateau_stone": 0.26, "jagged_peaks": 0.24, "pale_garden": 0.23,
		"redwood_taiga_mutated": 0.18, "cherry_grove": 0.15, "lush_caves": 0.1, "dripstone_caves": 0.1, "snowy_slopes": 0.08,
	}
	trees, chunks := map[string]float64{}, map[string]float64{}
	var allTrees, allChunks float64
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for seed := int64(1); seed <= statSeeds; seed++ {
		g := New(seed)
		for cx := -statSpan / 32; cx < statSpan/32; cx += 6 {
			for cz := -statSpan / 32; cz < statSpan/32; cz += 6 {
				wg.Add(1)
				sem <- struct{}{}
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					c := generate(g, int32(cx), int32(cz))
					local := map[string]float64{}
					n := countTrees(c, func(x, z uint8) {
						local[g.BiomeAt(cx*16+int(x), maxY, cz*16+int(z)).String()]++
					})
					centre := g.BiomeAt(cx*16+8, maxY, cz*16+8).String()
					mu.Lock()
					defer mu.Unlock()
					for k, v := range local {
						trees[k] += v
					}
					chunks[centre]++
					allTrees += float64(n)
					allChunks++
				}()
			}
		}
	}
	wg.Wait()
	names := make([]string, 0, len(figures))
	for k := range figures {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		want := figures[k]
		if chunks[k] < 60 {
			t.Logf("%-24s %6.2f a chunk over %4.0f chunks, Bedrock %5.2f: too few to judge", k, trees[k]/max(chunks[k], 1), chunks[k], want)
			continue
		}
		got := trees[k] / chunks[k]
		t.Logf("%-24s %6.2f a chunk over %4.0f chunks, Bedrock %5.2f", k, got, chunks[k], want)
		if math.Abs(got-want) > max(want*0.3, 0.3) {
			t.Errorf("%s: %.2f trees a chunk, Bedrock %.2f", k, got, want)
		}
	}
	var weighted, sum, theirs float64
	for k, s := range shares {
		sum += s
		theirs += s * figures[k]
		if chunks[k] > 0 {
			weighted += s * trees[k] / chunks[k]
		}
	}
	weighted /= sum
	t.Logf("overall: ours %.2f a chunk on our own mix; %.2f on the Bedrock windows' mix, where Bedrock's rates give %.2f; Bedrock measured 2.217", allTrees/allChunks, weighted, theirs/sum)
	if math.Abs(weighted-2.217) > 2.217*0.15 {
		t.Errorf("trees on the Bedrock windows' mix: %.2f a chunk, Bedrock 2.217, tolerance 15%%", weighted)
	}
}

// Kelp and seagrass per ocean chunk, from Bedrock's seed 1, whose window is 76% ocean, deep ocean,
// cold ocean and deep cold ocean and has almost nothing else under water: 143.51 kelp and 43.51
// seagrass a chunk over the window gives 189 and 57 per ocean chunk. Seed 12345 agrees on kelp (50.33
// over 28% ocean). One seed is thin evidence, so the tolerance is 30%.
func TestKelpAndSeagrassMatchBedrock(t *testing.T) {
	g := New(13)
	var pos [][2]int32
	pos = append(pos, biomeChunks(g, 100, "ocean", "deep_ocean", "cold_ocean", "deep_cold_ocean")...)
	if len(pos) < 50 {
		t.Fatalf("only %d ocean chunks", len(pos))
	}
	var kelp, seagrass float64
	for _, c := range generateAll(g, pos) {
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(minY); y <= SeaLevel; y++ {
					switch nameOf(c.Block(x, y, z, 0)) {
					case "kelp":
						kelp++
					case "seagrass":
						seagrass++
					}
				}
			}
		}
	}
	n := float64(len(pos))
	t.Logf("%.0f ocean chunks: kelp %.1f a chunk, Bedrock 189; seagrass %.1f, Bedrock 57", n, kelp/n, seagrass/n)
	if math.Abs(kelp/n-189) > 189*0.3 {
		t.Errorf("kelp %.1f a chunk, Bedrock 189", kelp/n)
	}
	if math.Abs(seagrass/n-57) > 57*0.3 {
		t.Errorf("seagrass %.1f a chunk, Bedrock 57", seagrass/n)
	}
}

// The cheap answer for columns around a chunk must equal SurfaceHeight, on every margin column.
func TestChunkGroundMatchesSurfaceHeight(t *testing.T) {
	g := New(14)
	var checked, opened int
	var spots [][2]int
	for i := range 80 {
		spots = append(spots, [2]int{i*23 - 900, 700 - i*31})
	}
	for _, p := range spots {
		ox, oz := p[0]*16, p[1]*16
		s := new(chunkScratch)
		g.cutCarvers(s, ox, oz)
		cg := chunkGround{g: g, s: s, originX: ox, originZ: oz}
		for x := ox - treeMargin; x <= ox+15+treeMargin; x++ {
			for z := oz - treeMargin; z <= oz+15+treeMargin; z++ {
				if x >= ox && x < ox+16 && z >= oz && z < oz+16 {
					continue
				}
				top, untouched, _ := cg.ground(x, z)
				if want := g.SurfaceHeight(x, z); top != want {
					t.Fatalf("(%d,%d) from chunk %v: ground %d, SurfaceHeight %d", x, z, p, top, want)
				}
				checked++
				if !untouched {
					opened++
				}
			}
		}
	}
	t.Logf("%d columns agree, %d of them opened by carvers or lakes", checked, opened)
	if opened == 0 {
		t.Fatal("no column opened by a carver or lake; the test would prove little")
	}
}

// Decoration reaches biomes only through BiomeAt, so every biome the tables give must map back.
func TestSurfaceDefAtMatchesTables(t *testing.T) {
	g := New(15)
	for x := -4096; x < 4096; x += 37 {
		for z := -4096; z < 4096; z += 41 {
			if got, want := g.surfaceDefAt(x, z), g.biomeColumnAt(cellCentre(x), cellCentre(z)).surface; got != want {
				// Hottest deep ocean and warm ocean share a biome and a surface, so this is exact.
				t.Fatalf("(%d,%d): %v, want %v", x, z, got, want)
			}
		}
	}
}

func BenchmarkChunkGround(b *testing.B) {
	g := New(1)
	s := new(chunkScratch)
	g.cutCarvers(s, 0, 0)
	cg := chunkGround{g: g, s: s}
	i := 0
	for b.Loop() {
		cg.ground(-1-i%5, i%16)
		i++
	}
}

// How long the heaviest chunks take: those with the most neighbouring trees to work out.
func BenchmarkGenerateTreeChunk(b *testing.B) {
	for _, name := range []string{"forest", "roofed_forest", "jungle", "ocean"} {
		b.Run(name, func(b *testing.B) {
			g := New(1)
			spots := biomeChunks(g, 200, name)
			if len(spots) == 0 {
				b.Skip("none found")
			}
			i := 0
			for b.Loop() {
				generate(g, spots[i%len(spots)][0], spots[i%len(spots)][1])
				i++
			}
		})
	}
}
