package worldgen

import (
	"context"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/go-gl/mathgl/mgl64"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// blockNames maps every runtime ID to its block name and pillar axis, so these tests read blocks
// by what a client would call them and not through the generator's own table.
var blockNames = sync.OnceValue(func() []struct{ name, axis, seagrass string } {
	registerOwnBlocks()
	world.DefaultBlockRegistry.Finalize()
	out := make([]struct{ name, axis, seagrass string }, world.DefaultBlockRegistry.BlockCount())
	for i := range out {
		name, props, ok := world.DefaultBlockRegistry.RuntimeIDToState(uint32(i))
		if !ok {
			continue
		}
		out[i].name = strings.TrimPrefix(name, "minecraft:")
		if a, ok := props["pillar_axis"].(string); ok {
			out[i].axis = a
		}
		if k, ok := props["sea_grass_type"].(string); ok {
			out[i].seagrass = k
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
	inTiles(pos, func(p [2]int32) {
		c := generate(g, p[0], p[1])
		mu.Lock()
		w[p] = c
		mu.Unlock()
	})
	return w
}

// inTiles runs f on every position, in parallel, a tile of nearby chunks per worker in row order, so a
// worker's kept bases serve its next chunks. Tests turn base reuse on; it changes no result.
func inTiles(pos [][2]int32, f func(p [2]int32)) {
	const tile = 10
	tiles := map[[2]int32][][2]int32{}
	var keys [][2]int32
	for _, p := range pos {
		k := [2]int32{p[0] / tile, p[1] / tile}
		if p[0] < 0 {
			k[0] = (p[0] - tile + 1) / tile
		}
		if p[1] < 0 {
			k[1] = (p[1] - tile + 1) / tile
		}
		if _, ok := tiles[k]; !ok {
			keys = append(keys, k)
		}
		tiles[k] = append(tiles[k], p)
	}
	jobs := make(chan [][2]int32)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for list := range jobs {
				slices.SortFunc(list, func(a, b [2]int32) int {
					if a[1] != b[1] {
						return int(a[1] - b[1])
					}
					return int(a[0] - b[0])
				})
				for _, p := range list {
					f(p)
				}
			}
		}()
	}
	for _, k := range keys {
		jobs <- tiles[k]
	}
	close(jobs)
	wg.Wait()
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
// the first has a log or leaves connected to it. The top of every trunk, where it does not bend on,
// has something on all four sides, across an edge too. Every leaf reaches a log through leaves within 6 steps, Java's rule, so a
// tree cut off at an edge shows up as orphan leaves.
func TestTreesWholeAcrossEdges(t *testing.T) {
	g := New(41)
	var crossings, tops, edgeTops, leaves, cutLeaves, bareTops int
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
								if !leafReachesLog(w, x, y, z) && !javaDecays(g, x, y, z) {
									if !cutOnlyByNeighbours(g, x, y, z) {
										t.Fatalf("chunk %v: leaves at %d %d %d reach no log within 6 steps", p, x, y, z)
									}
									cutLeaves++ // Allowed: cut only by a neighbour level the finished world lacks.
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
								// A column that ends where the trunk bends on, one step over and up (Java's
								// dark oak and acacia trunks shift), is not the tree's top.
								bends := false
								for dx := -1; dx <= 1; dx++ {
									for dz := -1; dz <= 1; dz++ {
										if b, ok := w.at(x+dx, y+1, z+dz); ok && isLogID(b) {
											bends = true
										}
									}
								}
								if isTrunkID(id) && !isLogID(above) && !bends {
									tops++
									if edge {
										edgeTops++
									}
									bare := false
									for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
										covered := false
										for dy := -1; dy <= 1; dy++ {
											if b, ok := w.at(x+d[0], y+dy, z+d[1]); ok && nameOf(b) != "air" {
												covered = true
											}
										}
										if !covered {
											if !neighbourStoodBeside(g, pos{x, y, z}, d) {
												t.Fatalf("chunk %v: trunk top at %d %d %d has nothing beside it towards %v", p, x, y, z, d)
											}
											bare = true // Allowed: what stood there belongs to a neighbour level the world lacks.
										}
									}
									if bare {
										bareTops++
									}
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d leaves, %d trunk tops (%d on chunk edges), %d edge logs joined across an edge; allowed: %d leaves cut only by a missing neighbour level, %d trunk tops bare only beside one", leaves, tops, edgeTops, crossings, cutLeaves, bareTops)
	// Bounds from fix round 2's measure, 63 of 52,987 leaves and 2 of 964 tops, with a small margin:
	// the allowance covers what two levels of neighbours leave out, and must stay that rare.
	if cutLeaves > 80 || bareTops > 4 {
		t.Errorf("%d leaves cut and %d bare trunk tops allowed; at most 80 and 4", cutLeaves, bareTops)
	}
	if edgeTops < 30 || crossings < 100 {
		t.Fatalf("only %d trunk tops on edges and %d crossings; the test would prove little", edgeTops, crossings)
	}
}

// javaDecays says whether the generator's own state for a leaf has distance 7: Java's tree feature found
// no log within 6 through its leaves when it placed it, so in the game it decays. Dragonfly's blocks
// keep no distance, so the generator's state is read.
func javaDecays(g *Generator, x, y, z int) bool {
	f := g.finalCells(newScratch(), x>>4, z>>4)
	return propOf(f[cellIndex(x&15, y, z&15)].name(), "distance") == "7"
}

// neighbourStoodBeside says whether, in the view of the source that placed a trunk top, a block that an
// earlier neighbour left stood beside it towards d (within a block up or down), one the source did not
// place itself. The source ran on top of that neighbour's level before (cachedLevel); the finished
// world has the neighbour's next level, where it can be gone.
func neighbourStoodBeside(g *Generator, top pos, d [2]int) bool {
	s := newScratch()
	cx, cz := top.x>>4, top.z>>4
	for sx := cx - 1; sx <= cx+1; sx++ {
		for sz := cz - 1; sz <= cz+1; sz++ {
			placed := false
			for _, c := range g.cachedLevel(s, sx, sz, sourceDepth).changes {
				q := pos{(sx-1+int(c.chunk)/3)*16 + int(c.cell)&15, int(c.cell)>>8 + minY, (sz-1+int(c.chunk)%3)*16 + int(c.cell)>>4&15}
				if q == top && c.now.tagged(tagLogs) {
					placed = true
				}
			}
			if !placed {
				continue
			}
			for ex := sx - 1; ex <= sx; ex++ {
				for ez := sz - 1; ez <= sz+1; ez++ {
					if ex == sx && ez >= sz {
						continue
					}
					for _, c := range g.cachedLevel(s, ex, ez, sourceDepth-1).changes {
						q := pos{(ex-1+int(c.chunk)/3)*16 + int(c.cell)&15, int(c.cell)>>8 + minY, (ez-1+int(c.chunk)%3)*16 + int(c.cell)>>4&15}
						if q.x == top.x+d[0] && q.z == top.z+d[1] && absInt(q.y-top.y) <= 1 && !c.now.isAir() {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// cutOnlyByNeighbours says whether a leaf lost its way to a log only through blocks its own source did
// not place. A source runs on top of what its earlier neighbours did at the level before (cachedLevel),
// and its leaf distances count their leaves, as Java's do; those neighbours' blocks in the finished
// world are from the next level and can differ. Leaves the source's own tree cut off still fail.
func cutOnlyByNeighbours(g *Generator, x, y, z int) bool {
	s := newScratch()
	cx, cz := x>>4, z>>4
	finals := map[[2]int]*[chunkCells]bs{}
	final := func(q pos) bs {
		k := [2]int{q.x >> 4, q.z >> 4}
		if finals[k] == nil {
			f := *g.finalCells(newScratch(), k[0], k[1])
			finals[k] = &f
		}
		return finals[k][cellIndex(q.x&15, q.y, q.z&15)]
	}
	near := func(q pos) bool { return absInt(q.x-x) <= 7 && absInt(q.z-z) <= 7 && absInt(q.y-y) <= 7 }
	collect := func(sx, sz, level int, into map[pos]bs) {
		for _, c := range g.cachedLevel(s, sx, sz, level).changes {
			q := pos{(sx-1+int(c.chunk)/3)*16 + int(c.cell)&15, int(c.cell)>>8 + minY, (sz-1+int(c.chunk)%3)*16 + int(c.cell)>>4&15}
			if near(q) {
				into[q] = c.now
			}
		}
	}
	for sx := cx - 1; sx <= cx+1; sx++ {
		for sz := cz - 1; sz <= cz+1; sz++ {
			own := map[pos]bs{}
			collect(sx, sz, sourceDepth, own)
			if !own[pos{x, y, z}].tagged(tagLeaves) {
				continue
			}
			seen := map[pos]bs{} // What the source saw of its earlier neighbours.
			for ex := sx - 1; ex <= sx; ex++ {
				for ez := sz - 1; ez <= sz+1; ez++ {
					if ex != sx || ez < sz {
						collect(ex, ez, sourceDepth-1, seen)
					}
				}
			}
			view := func(q pos) (bs, bool, bool) { // Block, known, placed by the source itself.
				if st, ok := own[q]; ok {
					return st, true, true
				}
				st, ok := seen[q]
				return st, ok, false
			}
			type node struct {
				p   pos
				d   int
				cut bool
			}
			start := pos{x, y, z}
			visited := map[pos]bool{start: true}
			queue := []node{{start, 0, false}}
			for len(queue) > 0 {
				n := queue[0]
				queue = queue[1:]
				for _, a := range allDirs {
					q := n.p.step(a)
					st, ok, mine := view(q)
					if visited[q] || !ok {
						continue
					}
					visited[q] = true
					differs := final(q) != st
					if differs && mine {
						continue // The source's own block is gone: not this allowance.
					}
					cut := n.cut || differs
					if st.tagged(tagLogs) {
						if cut {
							return true
						}
						continue
					}
					if st.tagged(tagLeaves) && n.d < 5 {
						queue = append(queue, node{q, n.d + 1, cut})
					}
				}
			}
		}
	}
	return false
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

// leafReachesLog is Java's rule for leaves that stay: a log within 6 steps, going through leaves
// (a leaf's distance runs 1 to 7, and 7 decays). Java's foliage placers make leaves out to 6, so a
// tree cut off at an edge shows up as leaves with no log that near. The search also passes through
// huge mushroom blocks: in Java a tree places no leaf where one stands and a mushroom placed later
// overwrites leaves, so in a dark forest a cap can cut leaves off in any order, and those leaves decay.
// What it may not pass is air, which is what a missing neighbour leaves.
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
			if (isLeavesID(b) || strings.HasSuffix(nameOf(b), "mushroom_block") || nameOf(b) == "mushroom_stem") && n.d < 5 {
				queue = append(queue, node{u[0], u[1], u[2], n.d + 1})
			}
		}
	}
	return false
}

// The same chunk whichever order chunks are made in, and on any number of workers, on chunks
// with trees crossing their edges: 3 forest, jungle or roofed forest spots, 2 snowy and 2 ocean.
func TestDecorationOrderIndependent(t *testing.T) {
	skipUnlessLong(t)
	checkDecorationOrderIndependent(t, 3, 2)
}

// TestDecorationOrderIndependentQuick is the same check, fixture free, on one forest, jungle or roofed
// forest spot, whose trees cross chunk edges, for the default run.
func TestDecorationOrderIndependentQuick(t *testing.T) {
	checkDecorationOrderIndependent(t, 1, 0)
}

// checkDecorationOrderIndependent makes the 3 x 3 chunks around wooded spots of seed 42, and around
// others snowy and others ocean spots, in order, backwards and on many workers at once.
func checkDecorationOrderIndependent(t *testing.T, wooded, others int) {
	g := New(42)
	spots := biomeChunks(g, wooded, "forest", "jungle", "roofed_forest")
	spots = append(spots, biomeChunks(g, others, "snowy_plains", "taiga")...)
	spots = append(spots, biomeChunks(g, others, "ocean", "frozen_ocean")...)
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
	if len(spots) < wooded+2*others {
		t.Fatalf("only %d chunks found", len(spots))
	}
}

// TestForestHasTreesDesertHasNone counts trees in 40 forest chunks and looks for logs and leaves in
// 40 desert chunks, of each of seeds 1 to 4.
func TestForestHasTreesDesertHasNone(t *testing.T) {
	skipUnlessLong(t)
	checkForestAndDesert(t, []int64{1, 2, 3, 4}, 40)
}

// TestForestHasTreesDesertHasNoneQuick is the same check, fixture free, on 20 chunks of each of seeds
// 1 and 2, for the default run.
func TestForestHasTreesDesertHasNoneQuick(t *testing.T) {
	checkForestAndDesert(t, []int64{1, 2}, 20)
}

func checkForestAndDesert(t *testing.T, seeds []int64, n int) {
	var forest, forestTrees, desert int
	for _, seed := range seeds {
		g := New(seed)
		for _, c := range generateAll(g, biomeChunks(g, n, "forest")) {
			forest++
			forestTrees += countTrees(c, nil)
		}
		for p, c := range generateAll(g, biomeChunks(g, n, "desert")) {
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
	if forest < n || desert < n/2 {
		t.Fatalf("only %d forest and %d desert chunks found", forest, desert)
	}
	if forestTrees < forest*2 {
		t.Errorf("%d trees in %d forest chunks", forestTrees, forest)
	}
}

// Every log belongs to a tree that stands on soil, the game's dirt tag (Java's trunk placers leave
// coarse dirt, podzol, moss and mud alone and turn grass to dirt): through logs touching at a face,
// edge or corner it reaches an upright log on soil. Java's trunk placers bend (acacia) and branch (mega jungle)
// with upright logs over air, so a log over air is fine if it is joined to a grounded trunk; a trunk
// cut from its base by another source's block is not. Neighbouring chunks are generated as needed.
func TestTreesStandOnSoil(t *testing.T) {
	skipUnlessLong(t)
	checkTreesStandOnSoil(t, []int64{5, 6}, func(*Generator) [][2]int32 { return spread(-200, 200, 9) }, 1000)
}

// TestTreesStandOnSoilQuick is the same check on a few dense blocks of chunks, for the default run:
// forests and jungles, where sources overlap most, and savanna, whose acacias bend.
func TestTreesStandOnSoilQuick(t *testing.T) {
	checkTreesStandOnSoil(t, []int64{41}, func(g *Generator) [][2]int32 {
		var pos [][2]int32
		for _, b := range [][]string{{"jungle"}, {"roofed_forest"}, {"forest"}, {"savanna"}} {
			for _, c := range biomeChunks(g, 1, b...) {
				pos = append(pos, blockAround(c, 2)...)
			}
		}
		return pos
	}, 300)
}

// spread is every step-th chunk of a square, from lo to hi on both axes.
func spread(lo, hi, step int32) [][2]int32 {
	var pos [][2]int32
	for cx := lo; cx < hi; cx += step {
		for cz := lo; cz < hi; cz += step {
			pos = append(pos, [2]int32{cx, cz})
		}
	}
	return pos
}

func checkTreesStandOnSoil(t *testing.T, seeds []int64, chunks func(*Generator) [][2]int32, minTrunks int) {
	var trunks, joined int
	for _, seed := range seeds {
		g := New(seed)
		pos := chunks(g)
		w := generateAll(g, pos)
		extra := world3{}
		at := func(x, y, z int) uint32 {
			if id, ok := w.at(x, y, z); ok {
				return id
			}
			k := [2]int32{int32(x >> 4), int32(z >> 4)}
			if extra[k] == nil {
				extra[k] = generate(g, k[0], k[1])
			}
			id, _ := extra.at(x, y, z)
			return id
		}
		for p, c := range w {
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(minY + 1); y <= maxY; y++ {
						id := c.Block(x, y, z, 0)
						if !isTrunkID(id) || isLogID(c.Block(x, y-1, z, 0)) {
							continue
						}
						trunks++
						if isTreeSoil(nameOf(c.Block(x, y-1, z, 0))) {
							continue
						}
						if !logGrounded(at, int(p[0])*16+int(x), int(y), int(p[1])*16+int(z)) {
							t.Fatalf("seed %d chunk %v (%d,%d,%d): a log on %s joins no trunk on soil", seed, p, x, y, z, nameOf(c.Block(x, y-1, z, 0)))
						}
						joined++
					}
				}
			}
		}
	}
	t.Logf("%d trunk bottoms checked, %d of them bends or branches joined to a grounded trunk", trunks, joined)
	if trunks < minTrunks {
		t.Fatalf("only %d trunks", trunks)
	}
}

// isTreeSoil is the game's dirt tag, by Bedrock name.
func isTreeSoil(n string) bool {
	switch n {
	case "dirt", "grass_block", "coarse_dirt", "podzol", "mycelium", "dirt_with_roots", "moss_block", "pale_moss_block", "mud", "muddy_mangrove_roots":
		return true
	}
	return false
}

// logGrounded searches the logs joined to one, out to 48 blocks (a mega jungle tree is up to about
// 40 tall), for an upright log on soil.
func logGrounded(at func(x, y, z int) uint32, x, y, z int) bool {
	start := [3]int{x, y, z}
	seen, step := map[[3]int]bool{start: true}, [][3]int{start}
	for len(step) > 0 {
		var next [][3]int
		for _, p := range step {
			id := at(p[0], p[1], p[2])
			if isTrunkID(id) {
				if isTreeSoil(nameOf(at(p[0], p[1]-1, p[2]))) {
					return true
				}
			}
			for dx := -1; dx <= 1; dx++ {
				for dy := -1; dy <= 1; dy++ {
					for dz := -1; dz <= 1; dz++ {
						q := [3]int{p[0] + dx, p[1] + dy, p[2] + dz}
						if seen[q] || absInt(q[0]-x) > 48 || absInt(q[2]-z) > 48 || absInt(q[1]-y) > 48 || !isLogID(at(q[0], q[1], q[2])) {
							continue
						}
						seen[q] = true
						next = append(next, q)
					}
				}
			}
		}
		step = next
	}
	return false
}

// Every plant sits on a valid block, over caves too: the sample holds all biomes and whatever the
// carvers opened under them. Plants on the outermost columns read the real neighbour for water.
func TestNoFloatingPlants(t *testing.T) {
	skipUnlessLong(t)
	counts := checkNoFloatingPlants(t, []int64{7, 8, 9}, func(*Generator) [][2]int32 { return spread(-260, 260, 12) })
	for _, n := range []string{"short_grass", "fern", "tall_grass", "large_fern", "dandelion", "poppy", "sunflower", "cactus", "reeds", "deadbush", "kelp", "seagrass", "waterlily", "snow_layer"} {
		if counts[n] == 0 {
			t.Errorf("no %s in the sample", n)
		}
	}
}

// TestNoFloatingPlantsQuick is the same check on a few dense blocks of chunks, for the default run:
// where plants of neighbouring sources overlap (forests, plains and taiga with double plants, jungle,
// swamp), under water (ocean) and on sand (desert).
func TestNoFloatingPlantsQuick(t *testing.T) {
	counts := checkNoFloatingPlants(t, []int64{7}, func(g *Generator) [][2]int32 {
		var pos [][2]int32
		for _, b := range [][]string{{"forest"}, {"flower_forest"}, {"plains", "sunflower_plains"}, {"taiga", "old_growth_spruce_taiga"},
			{"jungle"}, {"swampland"}, {"ocean", "lukewarm_ocean"}, {"desert"}} {
			for _, c := range biomeChunks(g, 1, b...) {
				pos = append(pos, blockAround(c, 2)...)
			}
		}
		return pos
	})
	if counts["short_grass"] == 0 || counts["seagrass"] == 0 {
		t.Fatalf("the sample holds no grass or seagrass: %v", counts)
	}
}

func checkNoFloatingPlants(t *testing.T, seeds []int64, chunks func(*Generator) [][2]int32) map[string]int {
	counts := map[string]int{}
	soil := []string{"grass_block", "dirt", "coarse_dirt", "podzol", "mycelium", "mud"}
	for _, seed := range seeds {
		g := New(seed)
		pos := chunks(g)
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
							// Java's lily pad needs water under it, which a waterlogged block holds too.
							ok = under == "water" || nameOf(c.Block(x, y-1, z, 1)) == "water"
						case "kelp":
							ok = under == "kelp" || isGround(under)
							if nameOf(c.Block(x, y, z, 1)) != "water" {
								t.Fatalf("seed %d chunk %v (%d,%d,%d): kelp is not in water", seed, p, x, y, z)
							}
						case "seagrass":
							// Tall seagrass is Bedrock's seagrass in two halves: the top stands on the bottom.
							ok = isGround(under)
							if blockNames()[id].seagrass == "double_top" {
								ok = blockNames()[c.Block(x, y-1, z, 0)].seagrass == "double_bot"
							}
							if nameOf(c.Block(x, y, z, 1)) != "water" {
								t.Fatalf("seed %d chunk %v (%d,%d,%d): seagrass is not in water", seed, p, x, y, z)
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
	return counts
}

// isGround is any block that holds a plant up, by name.
func isGround(n string) bool {
	switch n {
	case "air", "water", "lava", "ice", "kelp", "seagrass", "snow_layer", "waterlily":
		return false
	}
	return !isPlantName(n)
}

// TestFrozenOceanHasIce: the freeze pass follows Java's rule column by column. Open sea at Y 62 becomes
// ice where the biome there is colder than 0.15, and stays water where it is not, as frozen oceans'
// warm patches (the frozen temperature modifier) and deep frozen oceans (0.5) are. No Java fixture
// holds a frozen ocean, so the share is logged, not asserted; both outcomes must occur.
// TestFrozenOceanHasIce holds Java's freeze rule on 40 frozen ocean chunks of seed 10, and looks for
// ice on 20 warmer ocean chunks.
func TestFrozenOceanHasIce(t *testing.T) {
	skipUnlessLong(t)
	checkFrozenOcean(t, 40, 20)
}

// TestFrozenOceanHasIceQuick is the same check, fixture free, on 20 frozen and 10 warmer ocean chunks,
// for the default run.
func TestFrozenOceanHasIceQuick(t *testing.T) {
	checkFrozenOcean(t, 20, 10)
}

func checkFrozenOcean(t *testing.T, frozenChunks, warmChunks int) {
	g := New(10)
	frozen := biomeChunks(g, frozenChunks, "frozen_ocean", "deep_frozen_ocean")
	warm := biomeChunks(g, warmChunks, "ocean", "lukewarm_ocean", "warm_ocean")
	if len(frozen) < 10 || len(warm) < 10 {
		t.Fatalf("found %d frozen and %d warmer ocean chunks", len(frozen), len(warm))
	}
	s := newScratch()
	var iced, open int
	for _, p := range frozen {
		cx, cz := int(p[0]), int(p[1])
		base := carversStage(g, s, cx, cz)
		final := g.finalCells(s, cx, cz)
		for i := range 256 {
			x, z := i&15, i>>4
			c := cellIndex(x, waterTop, z)
			if base.b[c] != sWater || int(base.wgTop[i]) != waterTop+1 {
				continue // Not open sea at the carvers stage: under an iceberg, ice from the surface, or land.
			}
			wx, wz := cx*16+x, cz*16+z
			// Java's freeze reads the biome at the open block above and the temperature at the water.
			cold := g.coldEnough(s.blockBiome(g, wx, waterTop+1, wz), wx, waterTop, wz)
			switch got := final[c]; {
			case got == sIce:
				iced++
				if !cold {
					t.Fatalf("(%d,%d): ice where the biome is warm enough not to freeze", wx, wz)
				}
			case got == sWater:
				open++
				if cold {
					t.Fatalf("(%d,%d): open water where the biome is cold enough to freeze", wx, wz)
				}
			}
		}
	}
	t.Logf("frozen ocean: %d of %d open sea blocks froze (%.1f%%)", iced, iced+open, 100*float64(iced)/float64(max(iced+open, 1)))
	if iced == 0 || open == 0 {
		t.Errorf("frozen ocean: %d iced and %d open; the sample shows only one side of the rule", iced, open)
	}
	for p, c := range generateAll(g, warm) {
		for x := range uint8(16) {
			for z := range uint8(16) {
				if nameOf(c.Block(x, waterTop, z, 0)) == "ice" {
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
	skipUnlessLong(t)
	checkSnowOnlyWhereCold(t, []int64{11, 12}, 300, 10, 40, 120, 60)
}

// TestSnowOnlyWhereColdQuick is the same check, fixture free, on a sparser sample for the default run.
func TestSnowOnlyWhereColdQuick(t *testing.T) {
	checkSnowOnlyWhereCold(t, []int64{11}, 300, 60, 8, 24, 12)
}

// checkSnowOnlyWhereCold samples every step-th chunk within reach of the origin on each seed, snowy
// plains and taiga chunks per seed, and warm chunks of seed 11, needing at least minWarm of them.
func checkSnowOnlyWhereCold(t *testing.T, seeds []int64, reach, step int32, snowy, warmN, minWarm int) {
	var total, covered, columns int
	for _, seed := range seeds {
		g := New(seed)
		var pos [][2]int32
		for cx := -reach; cx < reach; cx += step {
			for cz := -reach; cz < reach; cz += step {
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
						// The layer rests on the block below, so that is the height the pass asks about.
						sc := newScratch()
						if b := sc.blockBiome(g, wx, int(y), wz); !g.coldEnough(b, wx, int(y), wz) {
							t.Fatalf("seed %d: snow at %d %d %d in %s (temperature %.2f)", seed, wx, y, wz, biomeTableNames[b], g.profile.climate.temperature(b, wx, int(y), wz))
						}
					}
				}
			}
		}
		for _, q := range biomeChunks(g, snowy, "ice_plains", "cold_taiga") {
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
	// Warm biomes, at whatever height they reach, have none.
	g := New(11)
	warm := biomeChunks(g, warmN, "plains", "forest", "desert", "savanna", "savanna_plateau", "jungle", "swampland", "beach", "river")
	if len(warm) < minWarm {
		t.Fatalf("only %d warm chunks", len(warm))
	}
	for p, c := range generateAll(g, warm) {
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(SeaLevel); y < 300; y++ {
					if nameOf(c.Block(x, y, z, 0)) == "snow_layer" {
						t.Fatalf("chunk %v: snow at Y %d in a warm biome", p, y)
					}
				}
			}
		}
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

// Every block the generator places can be mined in survival. Bedrock can't, as in the game, and the
// fluids and air are not blocks to mine. A survey over biomes, by world grid.
func TestPlacedBlocksAreBreakable(t *testing.T) {
	seen := map[uint32]bool{}
	var mu sync.Mutex
	bedrockSample(24, func(g *Generator, c *chunk.Chunk, _, _ int) {
		local := map[uint32]bool{}
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(minY); y <= maxY; y++ {
					local[c.Block(x, y, z, 0)] = true
				}
			}
		}
		mu.Lock()
		defer mu.Unlock()
		for id := range local {
			seen[id] = true
		}
	})
	for id := range seen {
		b, ok := world.DefaultBlockRegistry.BlockByRuntimeID(id)
		if !ok {
			t.Fatalf("no block for id %d", id)
		}
		switch nameOf(id) {
		case "air", "water", "flowing_water", "lava", "flowing_lava", "bedrock":
			continue
		}
		if _, ok := b.(block.Breakable); !ok {
			t.Errorf("%s cannot be mined", nameOf(id))
		}
	}
	for _, n := range []string{"snow_layer", "seagrass"} {
		found := false
		for id := range seen {
			found = found || nameOf(id) == n
		}
		if !found {
			t.Errorf("the survey holds no %s", n)
		}
	}
	t.Logf("%d kinds of block surveyed", len(seen))
}

// In a real server world, with a survival player: a block goes into snow and into seagrass, and
// both can be mined.
func TestSnowAndSeagrassInARealWorld(t *testing.T) {
	w := world.Config{}.New()
	defer w.Close()
	_, err := world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		tx.SetBlock(cube.Pos{0, 70, 0}, block.Stone{}, nil)
		h := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 71, 2.5}}.New(player.Type, player.Config{Name: "t", Position: mgl64.Vec3{0.5, 71, 2.5}})
		p := tx.AddEntity(h).(*player.Player)
		p.SetGameMode(world.GameModeSurvival)
		for _, target := range []struct {
			name string
			b    world.Block
		}{{"snow_layer", SnowLayer{}}, {"seagrass", Seagrass{}}} {
			pos := cube.Pos{0, 71, 0}
			tx.SetBlock(pos, target.b, nil)
			if tx.Block(pos) != target.b {
				t.Fatalf("%s did not stay in the world", target.name)
			}
			// Placing on it, clicking its top, replaces it.
			p.SetHeldItems(item.NewStack(block.Stone{}, 4), item.Stack{})
			p.UseItemOnBlock(pos, cube.FaceUp, mgl64.Vec3{0.5, 0.1, 0.5})
			if _, ok := tx.Block(pos).(block.Stone); !ok {
				t.Errorf("placing on %s left %T", target.name, tx.Block(pos))
			}
			tx.SetBlock(pos, target.b, nil)
			p.BreakBlock(pos)
			if _, ok := tx.Block(pos).(block.Air); !ok {
				t.Errorf("mining %s left %T", target.name, tx.Block(pos))
			}
		}
		// Taking the floor away takes the snow with it.
		tx.SetBlock(cube.Pos{0, 71, 0}, SnowLayer{}, nil)
		tx.SetBlock(cube.Pos{0, 70, 0}, nil, nil)
		if _, ok := tx.Block(cube.Pos{0, 71, 0}).(SnowLayer); ok {
			SnowLayer{}.NeighbourUpdateTick(cube.Pos{0, 71, 0}, cube.Pos{0, 70, 0}, tx)
			if _, ok := tx.Block(cube.Pos{0, 71, 0}).(SnowLayer); ok {
				t.Error("snow stayed with nothing under it")
			}
		}
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestGroveHasTrees: Java's grove grows spruces and pines on snow (spruce_on_snow, pine_on_snow: a
// scan up through powder snow, then snow block or powder snow below), where a sapling's survival test
// would refuse them. Java's fixtures hold 14 grove trunks over 953 grove columns of seed 987654321.
func TestGroveHasTrees(t *testing.T) {
	g := New(987654321)
	spots := biomeChunks(g, 12, "grove")
	if len(spots) < 6 {
		t.Fatalf("only %d grove chunks", len(spots))
	}
	trunks := 0
	for _, c := range generateAll(g, spots) {
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(minY + 1); y <= maxY; y++ {
					if isTrunkID(c.Block(x, y, z, 0)) && !isLogID(c.Block(x, y-1, z, 0)) {
						trunks++
					}
				}
			}
		}
	}
	t.Logf("%d trunks in %d grove chunks", trunks, len(spots))
	if trunks < len(spots) {
		t.Errorf("%d trunks in %d grove chunks", trunks, len(spots))
	}
}
