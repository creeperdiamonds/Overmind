package worldgen

import (
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

// topSolid returns the Y of the highest non-air, non-water block in a column.
func topSolid(c *chunk.Chunk, x, z uint8) int {
	b := newBlocks()
	for y := int16(maxY); y >= minY; y-- {
		if r := c.Block(x, y, z, 0); r != b.air && r != b.water {
			return int(y)
		}
	}
	return minY - 1
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

func TestOrderIndependent(t *testing.T) {
	first := generate(New(7), 5, 5)
	g := New(7)
	for i := int32(0); i < 100; i++ {
		generate(g, i*3-150, 40-i)
	}
	if !generate(g, 5, 5).Equals(first) {
		t.Fatal("chunk (5,5) changed after 100 other chunks were generated")
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

// The brief's "height step across an edge" is read as the 99th percentile of edge
// steps: a smooth surface has as many large edge steps as large inner ones, so the
// single largest edge step always beats any inner percentile.
func TestNoSeamAtChunkEdges(t *testing.T) {
	g := New(1234)
	const across, down = 8, 7 // 56 chunks a patch, 224 in all.
	var inner, edge []int
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
		for cx := range across {
			for cz := range down {
				for i := range 16 {
					// A chunk edge is also a grid cell edge, where blended density has a kink, so
					// compare it with the cell edges inside a chunk.
					for j := 3; j < 15; j += 4 {
						inner = append(inner,
							abs(h[cx][cz][j][i]-h[cx][cz][j+1][i]),
							abs(h[cx][cz][i][j]-h[cx][cz][i][j+1]))
					}
					if cx+1 < across {
						edge = append(edge, abs(h[cx][cz][15][i]-h[cx+1][cz][0][i]))
					}
					if cz+1 < down {
						edge = append(edge, abs(h[cx][cz][i][15]-h[cx][cz+1][i][0]))
					}
				}
			}
		}
	}
	in99, ed99 := percentile(inner, 0.99), percentile(edge, 0.99)
	inMean, edMean := mean(inner), mean(edge)
	t.Logf("steps: p99 inner %d, edge %d; mean inner %.3f, edge %.3f; max inner %d, edge %d (%d inner, %d edge)",
		in99, ed99, inMean, edMean, inner[len(inner)-1], edge[len(edge)-1], len(inner), len(edge))
	if ed99 > in99 {
		t.Errorf("edge step p99 %d is above inner step p99 %d", ed99, in99)
	}
	// Whole-block percentiles can miss a small seam; the mean cannot.
	if edMean > inMean*1.25 {
		t.Errorf("mean edge step %.3f is over 1.25 times mean inner step %.3f", edMean, inMean)
	}
}

func TestTerrainStatistics(t *testing.T) {
	const span, step = 4096, 32
	for seed := int64(1); seed <= 5; seed++ {
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
}

// Later tasks place trees on SurfaceHeight, so it must equal the real chunk.
func TestSurfaceHeightMatchesChunk(t *testing.T) {
	g := New(77)
	for _, p := range [][2]int32{{0, 0}, {-1, -1}, {64, -33}, {-200, 150}, {500, 500}, {90, 300}} {
		c := generate(g, p[0], p[1])
		for x := range 16 {
			for z := range 16 {
				got := g.SurfaceHeight(int(p[0])*16+x, int(p[1])*16+z)
				if want := topSolid(c, uint8(x), uint8(z)); got != want {
					t.Fatalf("chunk %v (%d,%d): SurfaceHeight %d, chunk top %d", p, x, z, got, want)
				}
			}
		}
	}
}

func TestBlocksFollowFillRules(t *testing.T) {
	g, b := New(3), newBlocks()
	// Surface blocks replace stone near the top of a column, and the underground adds
	// deepslate, bedrock, rock patches, ores and (below Y -54) lava.
	solids := append([]uint32{b.stone, b.grass, b.dirt, b.podzol, b.mycelium, b.sand, b.redSand,
		b.sandstone, b.redSandstone, b.gravel, b.clay, b.snow, b.deepslate, b.bedrock, b.lava,
		b.coarseDirt, b.packedIce, b.ice, b.calcite, b.mud, b.rawIron, b.rawCopper}, b.terracotta...)
	for _, pair := range b.veins {
		solids = append(solids, pair[0], pair[1])
	}
	for _, p := range [][2]int32{{0, 0}, {40, 40}, {-90, 15}, {300, -120}} {
		c := generate(g, p[0], p[1])
		for x := uint8(0); x < 16; x++ {
			for z := uint8(0); z < 16; z++ {
				for y := int16(minY); y <= maxY; y++ {
					switch r := c.Block(x, y, z, 0); {
					case r == b.water && y > SeaLevel && g.terrainHeight(int(p[0])*16+int(x), int(p[1])*16+int(z)) <= int(y):
						// Under the ground a raised aquifer may hold water: the study measured 0.1%
						// of blocks at Y 63 to 100.
						t.Fatalf("water at Y %d above the terrain", y)
					case r == b.air && y <= SeaLevel && g.terrainHeight(int(p[0])*16+int(x), int(p[1])*16+int(z)) <= int(y):
						t.Fatalf("air at Y %d, below sea level and not in a cave", y)
					case r != b.air && r != b.water && !slices.Contains(solids, r):
						t.Fatalf("unexpected block %d at Y %d", r, y)
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

func BenchmarkGenerateChunk(b *testing.B) {
	g := New(1)
	var i int32
	for b.Loop() {
		generate(g, i*7, -i*5)
		i++
	}
	b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(b.N), "ms/op")
}
