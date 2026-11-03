package worldgen

import (
	"math"
	"slices"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

var _ world.Generator = (*Generator)(nil)

func generate(g *Generator, x, z int32) *chunk.Chunk {
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	g.GenerateChunk(world.ChunkPos{x, z}, c)
	return c
}

// topSolid returns the Y of the highest block of terrain in a column: not air or water, and not
// a tree, plant, snow layer or the ice on water, which the decoration pass lays over the terrain.
func topSolid(c *chunk.Chunk, x, z uint8) int {
	b := newBlocks()
	for y := int16(maxY); y >= minY; y-- {
		r := c.Block(x, y, z, 0)
		if r == b.air || r == b.water || isLogID(r) || isLeavesID(r) || isPlantName(nameOf(r)) || nameOf(r) == "snow_layer" {
			continue
		}
		if r == b.ice && y > minY {
			if under := c.Block(x, y-1, z, 0); under == b.water || isPlantName(nameOf(under)) {
				continue
			}
		}
		return int(y)
	}
	return minY - 1
}

// isDecorationID is a block only the decoration pass places.
func isDecorationID(r uint32) bool {
	return isLogID(r) || isLeavesID(r) || isPlantName(nameOf(r)) || nameOf(r) == "snow_layer"
}

func TestSameSeedSameChunk(t *testing.T) {
	a, b := New(42), New(42)
	for _, p := range [][2]int32{{0, 0}, {-3, 7}, {120, -80}} {
		if !generate(a, p[0], p[1]).Equals(generate(b, p[0], p[1])) {
			t.Fatalf("chunk %v differs between generators with one seed", p)
		}
	}
}

func TestDifferentSeedDiffers(t *testing.T) {
	if generate(New(1), 0, 0).Equals(generate(New(2), 0, 0)) {
		t.Fatal("seeds 1 and 2 gave the same chunk")
	}
}

// TestOrderIndependent makes chunk 5, 5 again after 100 scattered chunks, each built alone.
func TestOrderIndependent(t *testing.T) {
	skipUnlessLong(t)
	checkOrderIndependent(t, 100)
}

// TestOrderIndependentQuick is the same check, fixture free, after 12 of the chunks, for the default run.
func TestOrderIndependentQuick(t *testing.T) {
	checkOrderIndependent(t, 12)
}

func checkOrderIndependent(t *testing.T, n int32) {
	first := generate(New(7), 5, 5)
	g := New(7)
	for i := int32(0); i < n; i++ {
		generate(g, i*3-150, 40-i)
	}
	if !generate(g, 5, 5).Equals(first) {
		t.Fatalf("chunk (5,5) changed after %d other chunks were generated", n)
	}
}

func TestConcurrentSafe(t *testing.T) {
	g := New(9)
	want := make([]*chunk.Chunk, 8)
	for i := range want {
		want[i] = generate(New(9), int32(i), int32(-i))
	}
	var wg sync.WaitGroup
	for w := 0; w < 64; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			i := w % len(want)
			if !generate(g, int32(i), int32(-i)).Equals(want[i]) {
				t.Errorf("worker %d: chunk %d differs", w, i)
			}
		}()
	}
	wg.Wait()
}

// percentile returns the p-th percentile of steps, which it sorts.
func percentile(steps []int, p float64) int {
	slices.Sort(steps)
	return steps[int(float64(len(steps)-1)*p)]
}

func mean(steps []int) float64 {
	var sum int
	for _, s := range steps {
		sum += s
	}
	return float64(sum) / float64(len(steps))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// seamSteps is one chunk's height steps across its chunk edges and across the other cell edges
// inside it (columns 3/4, 7/8, 11/12), both ways.
type seamSteps struct {
	edge, cell []int
}

// measureSeams returns the steps of each chunk of four patches, for chunks with a neighbour
// on both the +X and +Z sides.
func measureSeams(g *Generator) []seamSteps {
	const across, down = 8, 7
	var out []seamSteps
	for _, origin := range [][2]int32{{0, 0}, {300, -200}, {-500, 400}, {700, 700}} {
		var h [across][down][16][16]int
		for cx := range across {
			for cz := range down {
				c := generate(g, origin[0]+int32(cx), origin[1]+int32(cz))
				for x := range 16 {
					for z := range 16 {
						h[cx][cz][x][z] = topSolid(c, uint8(x), uint8(z))
					}
				}
			}
		}
		for cx := range across - 1 {
			for cz := range down - 1 {
				var s seamSteps
				here := &h[cx][cz]
				for i := range 16 {
					s.edge = append(s.edge, abs(here[15][i]-h[cx+1][cz][0][i]), abs(here[i][15]-h[cx][cz+1][i][0]))
					for _, j := range []int{3, 7, 11} {
						s.cell = append(s.cell, abs(here[j][i]-here[j+1][i]), abs(here[i][j]-here[i][j+1]))
					}
				}
				out = append(out, s)
			}
		}
	}
	return out
}

// seamExcess is how much more a chunk edge steps than the other cell edges of the same chunks:
// the mean of the per-chunk differences of a measure, and the standard error of that mean. The
// chunk is the unit because neighbouring steps are far from independent.
func seamExcess(chunks []seamSteps, measure func([]int) float64) (diff, stdErr float64) {
	var sum, squares float64
	for _, c := range chunks {
		d := measure(c.edge) - measure(c.cell)
		sum += d
		squares += d * d
	}
	n := float64(len(chunks))
	diff = sum / n
	return diff, math.Sqrt(max(squares/n-diff*diff, 0) / n)
}

// Every chunk edge is also a cell edge, and cell edges step more than the inside of a cell
// because of blending, so the fair question is whether a chunk edge steps more than the other
// cell edges. It must not, beyond three standard errors of the sampling. Seeds 1234, 5 and 6.
func TestNoSeamAtChunkEdges(t *testing.T) {
	skipUnlessLong(t)
	checkNoSeams(t, 1234, 5, 6)
}

// TestNoSeamAtChunkEdgesQuick is the same check, fixture free, on seed 1234 alone, for the default
// run. Fewer chunks give a wider standard error, so it is the same test with less power.
func TestNoSeamAtChunkEdgesQuick(t *testing.T) {
	checkNoSeams(t, 1234)
}

func checkNoSeams(t *testing.T, seeds ...int64) {
	over4 := func(steps []int) float64 {
		var n int
		for _, s := range steps {
			if s > 4 {
				n++
			}
		}
		return float64(n) / float64(len(steps))
	}
	for _, seed := range seeds {
		chunks := measureSeams(New(seed))
		rate, rateErr := seamExcess(chunks, over4)
		meanDiff, meanErr := seamExcess(chunks, mean)
		t.Logf("seed %d: %d chunks, edge minus cell: rate over 4 %+.4f (se %.4f), mean %+.4f (se %.4f)", seed, len(chunks), rate, rateErr, meanDiff, meanErr)
		if rate > 3*rateErr {
			t.Errorf("seed %d: chunk edges step over 4 more often than other cell edges by %.4f, se %.4f", seed, rate, rateErr)
		}
		if meanDiff > 3*meanErr {
			t.Errorf("seed %d: mean chunk edge step exceeds other cell edges by %.4f, se %.4f", seed, meanDiff, meanErr)
		}
	}
}

func TestTerrainStatistics(t *testing.T) {
	const span, step = 4096, 32
	// The seeds run side by side: each is a single-threaded sweep.
	var wg sync.WaitGroup
	defer wg.Wait()
	for seed := int64(1); seed <= 5; seed++ {
		wg.Go(func() { terrainStatistics(t, seed, span, step) })
	}
}

func terrainStatistics(t *testing.T, seed int64, span, step int) {
	g := New(seed)
	var ocean, total, high, low int
	low = maxY
	for x := -span / 2; x < span/2; x += step {
		for z := -span / 2; z < span/2; z += step {
			h := g.noiseHeight(x, z)
			total++
			if h < SeaLevel {
				ocean++
			}
			high, low = max(high, h), min(low, h)
		}
	}
	share := float64(ocean) / float64(total)
	t.Logf("seed %d: ocean %.1f%%, highest %d, lowest %d", seed, share*100, high, low)
	if share < 0.25 || share > 0.75 {
		t.Errorf("seed %d: ocean %.1f%% of area, want 25-75%%", seed, share*100)
	}
	if high <= 120 {
		t.Errorf("seed %d: highest land %d, want above 120", seed, high)
	}
	if high >= 319 {
		t.Errorf("seed %d: peak %d, want under 319", seed, high)
	}
	if low >= 45 {
		t.Errorf("seed %d: deepest ocean floor %d, want below 45", seed, low)
	}
}

// carversStage is a chunk's carvers stage, expanded from the cache.
func carversStage(g *Generator, s *chunkScratch, cx, cz int) *baseChunk {
	b := new(baseChunk)
	g.cachedBase(s, cx, cz).expand(b)
	return b
}

// carverTop is the highest block of a whole chunk's carvers stage that is not air or water.
func carverTop(b *baseChunk, x, z int) int {
	for y := maxY; y >= minY; y-- {
		if st := b.b[cellIndex(x, y, z)]; !st.isAir() && st != sWater {
			return y
		}
	}
	return minY - 1
}

// SurfaceHeight is the terrain before any feature: the carvers stage. Built for one column, with every
// tunnel that reaches it, it must give the whole chunk's carvers stage. (TestSurfaceHeightMatchesCarverStage
// checks the same against Java's fixtures.)
func TestSurfaceHeightMatchesChunk(t *testing.T) {
	g := New(77)
	s := newScratch()
	for _, p := range [][2]int{{0, 0}, {-1, -1}, {64, -33}, {-200, 150}, {500, 500}, {90, 300}} {
		b := carversStage(g, s, p[0], p[1])
		for x := range 16 {
			for z := range 16 {
				got := g.SurfaceHeight(p[0]*16+x, p[1]*16+z)
				if want := carverTop(b, x, z); got != want {
					t.Fatalf("chunk %v (%d,%d): SurfaceHeight %d, carvers stage top %d", p, x, z, got, want)
				}
			}
		}
	}
}

func TestBlocksFollowFillRules(t *testing.T) {
	g, b := New(3), newBlocks()
	// The blocks are the noise, surface and carver blocks and whatever Java's features place (ores,
	// rock, glow lichen, geodes, rooms and the rest), so there is no closed list any more. A placed
	// state Dragonfly has no block for panics under test (unknownState), so none turns into air unseen.
	for _, p := range [][2]int32{{0, 0}, {40, 40}, {-90, 15}, {300, -120}} {
		c := generate(g, p[0], p[1])
		for x := uint8(0); x < 16; x++ {
			for z := uint8(0); z < 16; z++ {
				terrainTop := g.SurfaceHeight(int(p[0])*16+int(x), int(p[1])*16+int(z)) // The carvers stage: terrain before features.
				for y := int16(minY); y <= maxY; y++ {
					if r := c.Block(x, y, z, 0); int(y) <= terrainTop && isDecorationID(r) {
						t.Fatalf("%s at Y %d, in or under the terrain top %d", nameOf(r), y, terrainTop)
					}
					switch r := c.Block(x, y, z, 0); {
					case r == b.water && y > SeaLevel && g.highestSolid(int(p[0])*16+int(x), int(p[1])*16+int(z)) <= int(y):
						// Under the ground a raised aquifer may hold water: the study measured 0.1%
						// of blocks at Y 63 to 100.
						t.Fatalf("water at Y %d above the terrain", y)
					case r == b.air && y <= SeaLevel && g.highestSolid(int(p[0])*16+int(x), int(p[1])*16+int(z)) <= int(y):
						t.Fatalf("air at Y %d, below sea level and not in a cave", y)
					}
				}
			}
		}
	}
}

func TestDefaultSpawnIsAboveTerrain(t *testing.T) {
	g := New(5)
	pos := g.DefaultSpawn(world.Overworld)
	if pos.Y() <= g.SurfaceHeight(pos.X(), pos.Z()) || pos.Y() <= SeaLevel {
		t.Fatalf("spawn %v is inside terrain or water", pos)
	}
}

// TestDefaultSpawnFallbackHasRoom checks the spawn used when the search finds no land: in the finished
// chunk a player there stands on a block (ground, water or leaves) with air for feet and head, not
// inside a tree, snow layer or plant, and nothing over their head.
func TestDefaultSpawnFallbackHasRoom(t *testing.T) {
	// Each spot is a lone chunk with all its sources to build, so the spots run side by side.
	var wg sync.WaitGroup
	defer wg.Wait()
	for _, seed := range []int64{0, 1, 5, 7, 100, 12345, -2147483648} {
		g := New(seed)
		for _, xz := range [][2]int{{0, 0}, {8, 8}, {100, -40}, {-333, 517}} {
			wg.Go(func() { fallbackSpawnHasRoom(t, g, seed, xz) })
		}
	}
}

func fallbackSpawnHasRoom(t *testing.T, g *Generator, seed int64, xz [2]int) {
	p := g.fallbackSpawn(xz[0], xz[1])
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	g.GenerateChunk(world.ChunkPos{int32(p.X() >> 4), int32(p.Z() >> 4)}, c)
	lx, lz := uint8(p.X()&15), uint8(p.Z()&15)
	at := func(y int) uint32 { return c.Block(lx, int16(y), lz, 0) }
	if at(p.Y()-1) == g.blocks.air {
		t.Errorf("seed %d at %v: nothing to stand on", seed, p)
	}
	if at(p.Y()) != g.blocks.air || at(p.Y()+1) != g.blocks.air {
		t.Errorf("seed %d at %v: no room to stand (%v, %v)", seed, p, nameOf(at(p.Y())), nameOf(at(p.Y()+1)))
	}
	for y := p.Y() + 2; y < world.Overworld.Range().Max(); y++ {
		if b := at(y); b != g.blocks.air {
			t.Errorf("seed %d at %v: %v above at Y %d", seed, p, nameOf(b), y)
			break
		}
	}
}

func BenchmarkGenerateChunk(b *testing.B) {
	g := New(1)
	var i int32
	for b.Loop() {
		generate(g, i*7, -i*5)
		i++
	}
	b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(b.N), "ms/op")
}

func BenchmarkSurfaceHeight(b *testing.B) {
	g := New(1)
	var i int
	for b.Loop() {
		g.SurfaceHeight(i*7, -i*5)
		i++
	}
}

func BenchmarkNoiseHeight(b *testing.B) {
	g := New(1)
	var i int
	for b.Loop() {
		g.NoiseHeight(i*7, -i*5)
		i++
	}
}
