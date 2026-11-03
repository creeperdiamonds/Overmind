package worldgen

import (
	"math"
	"runtime"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// The underground tests name their blocks themselves rather than through the generator's
// own table, so a wrong table entry cannot hide a wrong world.
func rid(b world.Block) uint32 {
	registerPlantBlocks()
	world.DefaultBlockRegistry.Finalize()
	return world.BlockRuntimeID(b)
}

var (
	ridAir       = rid(block.Air{})
	ridWater     = rid(block.Water{Still: true, Depth: 8})
	ridLava      = rid(block.Lava{Still: true, Depth: 8})
	ridBedrock   = rid(block.Bedrock{})
	ridStone     = rid(block.Stone{})
	ridDeepslate = rid(block.Deepslate{})
)

// oreIDs lists each ore's stone and deepslate block, by the name the table uses.
func oreIDs() map[string][2]uint32 {
	pair := func(stone, deep world.Block) [2]uint32 { return [2]uint32{rid(stone), rid(deep)} }
	s, d := block.StoneOre(), block.DeepslateOre()
	return map[string][2]uint32{
		"coal":     pair(block.CoalOre{Type: s}, block.CoalOre{Type: d}),
		"iron":     pair(block.IronOre{Type: s}, block.IronOre{Type: d}),
		"copper":   pair(block.CopperOre{Type: s}, block.CopperOre{Type: d}),
		"gold":     pair(block.GoldOre{Type: s}, block.GoldOre{Type: d}),
		"redstone": pair(block.RedstoneOre{Type: s}, block.RedstoneOre{Type: d}),
		"lapis":    pair(block.LapisOre{Type: s}, block.LapisOre{Type: d}),
		"diamond":  pair(block.DiamondOre{Type: s}, block.DiamondOre{Type: d}),
		"emerald":  pair(block.EmeraldOre{Type: s}, block.EmeraldOre{Type: d}),
	}
}

// chunkGrid generates a square of chunks.
func chunkGrid(g *Generator, x0, z0, size int32) map[[2]int32]*chunk.Chunk {
	m := make(map[[2]int32]*chunk.Chunk, size*size)
	for x := x0; x < x0+size; x++ {
		for z := z0; z < z0+size; z++ {
			m[[2]int32{x, z}] = generate(g, x, z)
		}
	}
	return m
}

func TestBedrockFloor(t *testing.T) {
	g := New(11)
	var share [5]int // Bedrock count at Y -64 to -60.
	var total int
	for _, c := range chunkGrid(g, -3, -3, 6) {
		for x := range uint8(16) {
			for z := range uint8(16) {
				total++
				if c.Block(x, -64, z, 0) != ridBedrock {
					t.Fatalf("Y -64 at (%d,%d) is not bedrock", x, z)
				}
				for i := range share {
					if c.Block(x, int16(-64+i), z, 0) == ridBedrock {
						share[i]++
					}
				}
				for y := int16(-59); y <= -40; y++ {
					if c.Block(x, y, z, 0) == ridBedrock {
						t.Fatalf("bedrock at Y %d", y)
					}
				}
			}
		}
	}
	// Measured on Bedrock: 1, 1, .75, .5, .25 from Y -64 to Y -60, and none at Y -59.
	for i, want := range []float64{1, 1, 0.75, 0.5, 0.25} {
		got := float64(share[i]) / float64(total)
		if math.Abs(got-want) > 0.02 {
			t.Errorf("bedrock at Y %d: %.3f, measured on Bedrock %.2f", -64+i, got, want)
		}
		if want == 1 && got != 1 {
			t.Errorf("bedrock at Y %d: %.3f, measured solid", -64+i, got)
		}
	}
}

func TestDeepslateLine(t *testing.T) {
	g := New(12)
	count := func(y int16) (deep, stone int) {
		for _, c := range chunkGrid(g, 0, 0, 5) {
			for x := range uint8(16) {
				for z := range uint8(16) {
					switch c.Block(x, y, z, 0) {
					case ridDeepslate:
						deep++
					case ridStone:
						stone++
					}
				}
			}
		}
		return
	}
	if d, s := count(-10); float64(d) < 0.8*float64(d+s) {
		t.Errorf("Y -10: %d deepslate, %d stone, want mostly deepslate", d, s)
	}
	if d, _ := count(20); d != 0 {
		t.Errorf("Y 20: %d deepslate blocks, want none", d)
	}
	if d, s := count(12); d != 0 {
		t.Errorf("Y 12: %d deepslate blocks, want none (stone %d)", d, s)
	}
	d, s := count(4)
	if d == 0 || s == 0 {
		t.Errorf("Y 4 should blend: %d deepslate, %d stone", d, s)
	}
}

// airSteps counts air blocks between Y -40 and 40 and how many have air in the next
// block along x, inside one chunk and across the edge to a neighbour.
func airSteps(chunks map[[2]int32]*chunk.Chunk, size int32) (air, total, innerPairs, innerBoth, edgePairs, edgeBoth int) {
	for pos, c := range chunks {
		next := chunks[[2]int32{pos[0] + 1, pos[1]}]
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(-40); y <= 40; y++ {
					total++
					if c.Block(x, y, z, 0) != ridAir {
						continue
					}
					air++
					switch {
					case x < 15:
						innerPairs++
						if c.Block(x+1, y, z, 0) == ridAir {
							innerBoth++
						}
					case next != nil:
						edgePairs++
						if next.Block(0, y, z, 0) == ridAir {
							edgeBoth++
						}
					}
				}
			}
		}
	}
	return
}

func TestCavesExist(t *testing.T) {
	g := New(13)
	const size = 12
	chunks := chunkGrid(g, 20, 20, size)
	air, total, ip, ib, ep, eb := airSteps(chunks, size)
	share := float64(air) / float64(total)
	inner, edge := float64(ib)/float64(ip), float64(eb)/float64(ep)
	t.Logf("air share Y -40..40: %.1f%%; next block also air: inside chunks %.3f, across edges %.3f", share*100, inner, edge)
	if share < 0.03 || share > 0.20 {
		t.Errorf("air share %.1f%%, want 3-20%%", share*100)
	}
	if edge < inner-0.08 {
		t.Errorf("caves break at chunk edges: air continues %.3f of the time across an edge, %.3f inside a chunk", edge, inner)
	}
}

// Some columns open to the sky, by noise caves alone (the share is asserted in
// TestNoiseCavesOpenToSky), and SurfaceHeight and the chunk must both show it. Every 2nd column.
func TestSomeCavesReachSurface(t *testing.T) {
	g := New(14)
	var land, open int
	var found [2]int
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for x := 0; x < 1024; x += 2 {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			for z := 0; z < 1024; z += 2 {
				top := g.highestSolid(x, z)
				if top <= SeaLevel {
					continue
				}
				opened := g.noiseHeight(x, z) < top
				mu.Lock()
				land++
				if opened {
					if open == 0 {
						found = [2]int{x, z}
					}
					open++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	t.Logf("land columns sampled %d, ground top removed by noise caves %d (%.2f%%)", land, open, float64(open)/float64(land)*100)
	if open == 0 {
		t.Fatal("no cave reaches the surface in 1024x1024")
	}
	// The chunk must agree: the terrain's top block is gone there.
	x, z := found[0], found[1]
	c := generate(g, int32(x>>4), int32(z>>4))
	if got := c.Block(uint8(x&15), int16(g.highestSolid(x, z)), uint8(z&15), 0); got != ridAir && got != ridWater {
		t.Errorf("column (%d,%d): noise height says open, chunk has block %d at terrain top", x, z, got)
	}
}

func TestSurfaceHeightMatchesChunkWithCaves(t *testing.T) {
	g := New(15)
	checked := 0
	for cx := 0; cx < 80 && checked < 4; cx++ {
		for cz := 0; cz < 80 && checked < 4; cz++ {
			// Cheap look first: is any sampled column of this chunk open to the sky?
			open := false
			for x := 0; x < 16 && !open; x += 2 {
				for z := 0; z < 16 && !open; z += 2 {
					wx, wz := cx*16+x, cz*16+z
					open = g.SurfaceHeight(wx, wz) < g.highestSolid(wx, wz)
				}
			}
			if !open {
				continue
			}
			checked++
			c := generate(g, int32(cx), int32(cz))
			for x := range 16 {
				for z := range 16 {
					wx, wz := cx*16+x, cz*16+z
					if got, want := g.SurfaceHeight(wx, wz), topSolid(c, uint8(x), uint8(z)); got != want {
						t.Fatalf("(%d,%d): SurfaceHeight %d, chunk top %d", wx, wz, got, want)
					}
				}
			}
		}
	}
	if checked < 4 {
		t.Fatalf("found only %d chunks with an open cave", checked)
	}
}

// The study measured lava aquifers at levels of -10 and under, and 0.0%% of carved space
// from Y -20 upward, so lava is allowed down low and only a trace of it higher up (springs
// and lakes). The old rule of no lava above Y -54 was a guess.
func TestLavaOnlyDeep(t *testing.T) {
	g := New(16)
	var deep, high, volume int
	for _, c := range chunkGrid(g, 0, 0, 8) {
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(minY); y <= maxY; y++ {
					if c.Block(x, y, z, 0) == ridLava {
						if y < 0 {
							deep++
						} else {
							high++
						}
					}
					volume++
				}
			}
		}
	}
	t.Logf("lava blocks in 64 chunks: %d under Y 0, %d from Y 0 up, of %d", deep, high, volume)
	if deep == 0 {
		t.Error("no lava under Y 0")
	}
	if float64(high)/float64(volume) > 0.001 {
		t.Errorf("%d lava blocks from Y 0 up, want only a trace", high)
	}
}

func TestOceanFloorHolds(t *testing.T) {
	g := New(17)
	var columns int
	for pos, c := range chunkGrid(g, -12, -12, 24) {
		wet := false
		for x := range uint8(16) {
			for z := range uint8(16) {
				if c.Block(x, waterTop, z, 0) != ridWater {
					continue
				}
				wet = true
				columns++
				for y := int16(waterTop); y > minY; y-- {
					if c.Block(x, y, z, 0) == ridWater && c.Block(x, y-1, z, 0) == ridAir {
						t.Fatalf("chunk %v (%d,%d): air at Y %d directly under water", pos, x, z, y-1)
					}
				}
			}
		}
		_ = wet
	}
	t.Logf("water columns checked: %d", columns)
	if columns < 20000 {
		t.Fatalf("only %d water columns in the sample, the test needs oceans", columns)
	}
}

func TestOreRanges(t *testing.T) {
	g := New(18)
	ids := oreIDs()
	byID := map[uint32]string{}
	for name, p := range ids {
		byID[p[0]], byID[p[1]] = name, name
	}
	low, high := map[string]int{}, map[string]int{}
	count := map[string]int{}
	for _, c := range chunkGrid(g, 0, 0, 8) {
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(minY); y <= maxY; y++ {
					r := c.Block(x, y, z, 0)
					name, ok := byID[r]
					if !ok {
						continue
					}
					if r == ids[name][1] && y >= 8 {
						t.Fatalf("deepslate %s ore at Y %d, want under 8", name, y)
					}
					if count[name] == 0 {
						low[name], high[name] = int(y), int(y)
					}
					low[name], high[name] = min(low[name], int(y)), max(high[name], int(y))
					count[name]++
				}
			}
		}
	}
	// The overall Y range of each ore across its table rows, from the study's ore table.
	limits := map[string][2]int{
		"coal": {0, 320}, "iron": {-64, 384}, "copper": {-16, 112}, "gold": {-64, 256},
		"redstone": {-64, 15}, "lapis": {-64, 64}, "diamond": {-64, 16}, "emerald": {-16, 320},
	}
	for name, lim := range limits {
		t.Logf("%-8s %6d blocks in 64 chunks, Y %d to %d", name, count[name], low[name], high[name])
		if count[name] > 0 && (low[name] < lim[0] || high[name] > lim[1]) {
			t.Errorf("%s found at Y %d to %d, table allows %d to %d", name, low[name], high[name], lim[0], lim[1])
		}
	}
	if count["diamond"] == 0 {
		t.Error("no diamonds in 64 chunks")
	}
	for _, name := range []string{"coal", "iron", "copper", "gold", "redstone", "lapis"} {
		if count[name] == 0 {
			t.Errorf("no %s ore in 64 chunks", name)
		}
	}
}

// Veins go only into stone-type blocks: never into air, water, lava, bedrock or the surface.
func TestOreCornerBlocksReplaced(t *testing.T) {
	g := New(19)
	pos := world.ChunkPos{3, -2}
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	// Stone from the bottom to Y 100 with pockets of air, water and lava through it.
	pocket := func(x, y, z int) uint32 {
		switch ((x/3)+(y/3)+(z/3))%4 + 0 {
		case 0:
			return ridAir
		case 1:
			return ridWater
		case 2:
			return ridLava
		}
		return ridStone
	}
	want := map[[3]int]uint32{}
	for x := range 16 {
		for z := range 16 {
			for y := minY; y <= 100; y++ {
				id := pocket(x, y, z)
				if y > 0 && y < 8 {
					id = ridStone
				}
				want[[3]int{x, y, z}] = id
				c.SetBlock(uint8(x), int16(y), uint8(z), 0, id)
			}
		}
	}
	g.placeVeins(c, pos, 100, true)
	g.placeVeins(c, pos, 100, false)
	ids := oreIDs()
	ore := map[uint32]bool{}
	for _, p := range ids {
		ore[p[0]], ore[p[1]] = true, true
	}
	var changed int
	for k, was := range want {
		now := c.Block(uint8(k[0]), int16(k[1]), uint8(k[2]), 0)
		if now == was {
			continue
		}
		changed++
		if was != ridStone {
			t.Fatalf("block %v was %d and became %d: only stone may be replaced", k, was, now)
		}
	}
	t.Logf("%d stone blocks replaced by veins", changed)
	if changed == 0 {
		t.Fatal("no veins were placed")
	}
}

// Ore blobs are rolled from their origin chunk, so one that crosses a chunk edge must join up.
func TestVeinsCrossChunkEdges(t *testing.T) {
	g := New(21)
	ore := map[uint32]bool{}
	for _, p := range oreIDs() {
		ore[p[0]], ore[p[1]] = true, true
	}
	chunks := chunkGrid(g, 0, 0, 10)
	var inner, innerOre, edge, edgeOre int
	for pos, c := range chunks {
		next := chunks[[2]int32{pos[0] + 1, pos[1]}]
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(-60); y < 60; y++ {
					if !ore[c.Block(x, y, z, 0)] {
						continue
					}
					switch {
					case x < 15:
						inner++
						if ore[c.Block(x+1, y, z, 0)] {
							innerOre++
						}
					case next != nil:
						edge++
						if ore[next.Block(0, y, z, 0)] {
							edgeOre++
						}
					}
				}
			}
		}
	}
	in, ed := float64(innerOre)/float64(inner), float64(edgeOre)/float64(edge)
	t.Logf("ore next to ore: inside chunks %.3f (%d), across edges %.3f (%d)", in, inner, ed, edge)
	if edge < 100 || ed < in*0.7 {
		t.Errorf("veins break at chunk edges: %.3f across edges against %.3f inside", ed, in)
	}
}

// countBlob rolls one blob of a size and counts its blocks across the chunks it can touch.
func countBlob(g *Generator, size int, seed uint64) int {
	f := veinFeature{block: veinDirt, attempts: 1, size: size, minY: 0, maxY: 0, rock: true}
	src := biomeSource{g: g}
	reach := int16(blobReach(size))
	total := 0
	for a := range 3 {
		for b := range 3 {
			c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := -reach; y <= reach; y++ {
						c.SetBlock(x, y, z, 0, ridStone)
					}
				}
			}
			r := rng(seed)
			g.placeVein(c, &f, &r, 16, 16, a*16, b*16, 100, &src)
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := -reach; y <= reach; y++ {
						if c.Block(x, y, z, 0) != ridStone {
							total++
						}
					}
				}
			}
		}
	}
	return total
}

// Blob sizes from the study (simulated geometry, notes section 10 and addendum A5). Means must
// be within 20%. Size 3 also has its share of empty blobs: 64.7% place nothing.
func TestBlobSizesMatchMeasuredFigures(t *testing.T) {
	g := New(31)
	for _, c := range []struct {
		size, runs int
		want       float64
	}{{3, 1500, 0.46}, {4, 400, 2.1}, {7, 300, 5.0}, {8, 300, 5.7}, {9, 300, 6.7}, {10, 200, 8.4}, {12, 200, 11.5}, {17, 200, 22}, {20, 100, 33}, {33, 60, 113}, {64, 30, 679}} {
		if reach := blobReach(c.size); reach > 16 {
			t.Errorf("size %d reaches %d blocks, past the neighbouring chunks", c.size, reach)
		}
		var total, empty int
		for i := range c.runs {
			n := countBlob(g, c.size, posHash(77, i, c.size, 0))
			total += n
			if n == 0 {
				empty++
			}
		}
		got := float64(total) / float64(c.runs)
		t.Logf("size %d: %.2f blocks a blob, measured %.2f; %.0f%% empty", c.size, got, c.want, float64(empty)/float64(c.runs)*100)
		if math.Abs(got-c.want) > c.want*0.2 {
			t.Errorf("size %d: %.2f blocks a blob, measured %.2f", c.size, got, c.want)
		}
		if c.size == 3 {
			if share := float64(empty) / float64(c.runs); math.Abs(share-0.647) > 0.05 {
				t.Errorf("size 3: %.1f%% of blobs empty, measured 64.7%%", share*100)
			}
		}
	}
}
