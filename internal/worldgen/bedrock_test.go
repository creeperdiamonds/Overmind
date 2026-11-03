package worldgen

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Figures measured from Bedrock worlds: BDS 1.26.60.6, seeds 100, 1, 12345 and 987654321, each
// 1120 x 1120 blocks, with features and carvers (BEDROCK-NOTES.md section 9, aggregate.json).
//
// Terrain and biomes now follow the same seed, so these tests measure ours on those same seeds and
// windows: like for like, which the earlier comparison on other seeds could not be. A tolerance is
// still never tighter than two standard errors of the mean of the 4 seeds, worked out from the
// by-seed figures below; on figures the seeds agree on, a floor of 10% of the figure (ores) or 1 point
// (air) stands in for what remains our own design, the carvers and the blob shape.

// seedTwoSE gives two standard errors of the mean of by-seed figures.
func seedTwoSE(bySeed []float64) float64 {
	var sum, sq float64
	for _, v := range bySeed {
		sum += v
	}
	n := float64(len(bySeed))
	avg := sum / n
	for _, v := range bySeed {
		sq += (v - avg) * (v - avg)
	}
	return 2 * math.Sqrt(sq/(n-1)/n)
}

// bedrockSample visits every step-th chunk of the 4 Bedrock windows (chunks -35 to 34), in parallel.
func bedrockSample(step int, visit func(g *Generator, c *chunk.Chunk, cx, cz int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, seed := range goldenSeeds {
		g := New(seed)
		for cx := -35; cx < 35; cx += step {
			for cz := -35; cz < 35; cz += step {
				wg.Add(1)
				sem <- struct{}{}
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					visit(g, generate(g, int32(cx), int32(cz)), cx, cz)
				}()
			}
		}
	}
	wg.Wait()
}

// groundTop is the study's column top: the highest ground block.
func groundTop(c *chunk.Chunk, ground map[uint32]bool, x, z uint8) int {
	for y := int16(maxY); y >= minY; y-- {
		if ground[c.Block(x, y, z, 0)] {
			return int(y)
		}
	}
	return minY - 1
}

// Air in full chunks, with carvers, by the study's method: blocks under the column's top minus 8,
// per band. These are the first real references for the carvers. Each band's tolerance comes from
// its by-seed figures, with a floor of 1 point. The 100 to 160 row has 3 seeds: seed 1 has no land
// up there.
func TestCaveAirMatchesBedrock(t *testing.T) {
	bands := []struct {
		lo, hi int
		pooled float64 // Weighted by blocks, so not always the mean of the seeds.
		bySeed []float64
	}{
		{-54, -40, 6.06, []float64{8.53, 5.26, 5.79, 4.65}},
		{-40, -20, 9.21, []float64{11.08, 8.15, 9.26, 8.35}},
		{-20, 0, 9.81, []float64{10.64, 9.56, 9.44, 9.59}},
		{0, 20, 7.46, []float64{10.22, 3.32, 7.09, 9.18}},
		{20, 40, 7.21, []float64{8.96, 2.92, 6.21, 9.88}},
		{40, 63, 5.96, []float64{7.3, 4.04, 5.84, 5.04}},
		{63, 100, 7.52, []float64{7.66, 4.38, 7.64, 6.83}},
		{100, 160, 5.11, []float64{5.67, 5.33, 2.14}},
	}
	air, total := make([]float64, len(bands)), make([]float64, len(bands))
	ground := GroundBlocks()
	var mu sync.Mutex
	bedrockSample(1, func(g *Generator, c *chunk.Chunk, _, _ int) {
		a, n := make([]float64, len(bands)), make([]float64, len(bands))
		for x := uint8(0); x < 16; x += 3 {
			for z := uint8(0); z < 16; z += 3 {
				top := groundTop(c, ground, x, z)
				for i, b := range bands {
					for y := b.lo; y < min(b.hi, top-8); y++ {
						n[i]++
						if c.Block(x, int16(y), z, 0) == g.blocks.air {
							a[i]++
						}
					}
				}
			}
		}
		mu.Lock()
		defer mu.Unlock()
		for i := range a {
			air[i] += a[i]
			total[i] += n[i]
		}
	})
	var s strings.Builder
	for i, b := range bands {
		tol := max(seedTwoSE(b.bySeed), 1)
		got := air[i] / total[i] * 100
		fmt.Fprintf(&s, "\nY %4d to %4d: air %5.2f, Bedrock %5.2f, tolerance %.2f, off %+.2f", b.lo, b.hi, got, b.pooled, tol, got-b.pooled)
		if math.Abs(got-b.pooled) > tol {
			t.Errorf("Y %d to %d: air %.2f%%, Bedrock %.2f%%, tolerance %.2f", b.lo, b.hi, got, b.pooled, tol)
		}
	}
	t.Log(s.String())
}

// oreBands are the bands of aggregate.json: 16 blocks up to Y 64, then wider.
var oreBands = []int{-64, -48, -32, -16, 0, 16, 32, 48, 64, 96, 128, 192, 320}

// oreFigures is each ore's total, its total by seed and its blocks per chunk by band.
var oreFigures = []struct {
	name   string
	total  float64
	bySeed []float64
	byBand []float64
}{
	{"coal", 107.75, []float64{142.303, 45.193, 148.939, 94.57}, []float64{0, 0, 0, 0.09, 5.19, 13.2, 20.36, 20.82, 29.66, 12.01, 6.43, 0}},
	{"iron", 97.23, []float64{97.238, 88.59, 107.808, 95.302}, []float64{2.82, 4.3, 6.18, 15.14, 24.6, 23.38, 12.13, 2.35, 1.36, 3.07, 1.89, 0}},
	{"copper", 107.85, []float64{143.902, 55.532, 82.736, 149.221}, []float64{0, 0, 0.09, 6.23, 15.73, 23.89, 29.39, 19.51, 12.35, 0.66, 0, 0}},
	{"gold", 24.29, []float64{26.856, 23.452, 23.467, 23.381}, []float64{3.45, 3.8, 5.98, 5.7, 3.34, 1.06, 0.25, 0.3, 0.34, 0.06, 0, 0}},
	{"redstone", 34.42, []float64{33.78, 34.477, 34.598, 34.818}, []float64{14.53, 8.76, 3.93, 3.99, 3.21, 0, 0, 0, 0, 0, 0, 0}},
	{"lapis", 22.34, []float64{22.383, 21.546, 22.58, 22.837}, []float64{1.7, 2.01, 3.24, 5.13, 4.81, 2.67, 1.64, 1.14, 0, 0, 0, 0}},
	{"diamond", 16.89, []float64{16.702, 16.95, 17.002, 16.902}, []float64{5.25, 5.03, 3.77, 2.29, 0.55, 0, 0, 0, 0, 0, 0, 0}},
	{"emerald", 0.19, []float64{0.171, 0, 0.412, 0.162}, []float64{0, 0, 0, 0, 0.01, 0.01, 0.01, 0.01, 0.03, 0.05, 0.07, 0}},
}

// Ore blocks per chunk against Bedrock's totals, and per Y band. Ores in ordinary stone depend on
// how much land a window holds, which is why coal and copper get a wide tolerance from their
// by-seed figures. A band up to Y 64 gets 2 blocks or 35% of its figure, whichever is more, because
// the by-band figures have no by-seed split to work a tolerance from. Bands above Y 64 are logged
// only: they hold only land, and the windows' land stands higher than the world's (median 80
// against the 8192 study's 71, p99 164 against 150), so their figures do not carry over.
func TestOresMatchBedrock(t *testing.T) {
	type counts [12]float64
	totals := map[string]*counts{}
	byID := map[uint32]string{}
	for name, p := range oreIDs() {
		byID[p[0]], byID[p[1]] = name, name
		totals[name] = &counts{}
	}
	var chunks float64
	var mu sync.Mutex
	bedrockSample(2, func(g *Generator, c *chunk.Chunk, _, _ int) {
		local := map[string]*counts{}
		for name := range totals {
			local[name] = &counts{}
		}
		for x := range uint8(16) {
			for z := range uint8(16) {
				band := 0
				for y := minY; y <= maxY; y++ {
					for y >= oreBands[band+1] {
						band++
					}
					if name, ok := byID[c.Block(x, int16(y), z, 0)]; ok {
						local[name][band]++
					}
				}
			}
		}
		mu.Lock()
		defer mu.Unlock()
		chunks++
		for name, l := range local {
			for i := range l {
				totals[name][i] += l[i]
			}
		}
	})
	var s strings.Builder
	for _, f := range oreFigures {
		var sum float64
		for i := range totals[f.name] {
			sum += totals[f.name][i] / chunks
		}
		tol := max(seedTwoSE(f.bySeed), f.total*0.10)
		fmt.Fprintf(&s, "\n%-8s %7.2f a chunk, Bedrock %7.2f, tolerance %6.2f, off %+.2f; bands:", f.name, sum, f.total, tol, sum-f.total)
		if math.Abs(sum-f.total) > tol {
			t.Errorf("%s: %.2f blocks a chunk, Bedrock %.2f, tolerance %.2f", f.name, sum, f.total, tol)
		}
		for i, want := range f.byBand {
			got := totals[f.name][i] / chunks
			fmt.Fprintf(&s, " %.2f/%.2f", got, want)
			if oreBands[i] < 64 && math.Abs(got-want) > max(2, want*0.35) {
				t.Errorf("%s at Y %d to %d: %.2f blocks a chunk, Bedrock %.2f", f.name, oreBands[i], oreBands[i+1], got, want)
			}
		}
	}
	t.Log(s.String())
}

// Rock patches per chunk, from the same worlds. They follow how much land and how deep the windows
// are, and the notes give no by-seed split, so the tolerance is 30%. Two rows are logged only. Dirt
// (700) mixes patches with the soil under every surface, so it does not measure the patch rule.
// Infested stone has its own test.
func TestRockPatchesMatchBedrock(t *testing.T) {
	b := newBlocks()
	rows := []struct {
		name string
		id   uint32
		want float64
	}{
		{"granite", b.granite, 935.78}, {"diorite", b.diorite, 883.36}, {"andesite", b.andesite, 837.69},
		{"tuff", b.tuff, 1111.46}, {"gravel", b.gravel, 544.62},
	}
	var dirt float64
	counts := make([]float64, len(rows))
	var chunks, infested float64
	var mu sync.Mutex
	bedrockSample(2, func(g *Generator, c *chunk.Chunk, _, _ int) {
		l := make([]float64, len(rows))
		var inf, d float64
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(minY); y <= maxY; y++ {
					r := c.Block(x, y, z, 0)
					if r == b.veins[veinInfested][0] || r == b.veins[veinInfested][1] {
						inf++
					}
					if r == b.dirt {
						d++
					}
					for i := range rows {
						if rows[i].id == r {
							l[i]++
						}
					}
				}
			}
		}
		mu.Lock()
		defer mu.Unlock()
		chunks++
		infested += inf
		dirt += d
		for i := range l {
			counts[i] += l[i]
		}
	})
	for i, r := range rows {
		got := counts[i] / chunks
		t.Logf("%-8s %7.1f a chunk, Bedrock %7.1f (%+.0f%%)", r.name, got, r.want, (got/r.want-1)*100)
		if math.Abs(got-r.want) > r.want*0.3 {
			t.Errorf("%s: %.1f blocks a chunk, Bedrock %.1f, tolerance 30%%", r.name, got, r.want)
		}
	}
	t.Logf("dirt %.1f a chunk, Bedrock 700.5; infested %.2f a chunk, Bedrock 1.23", dirt/chunks, infested/chunks)
}

// The measured share of deepslate in deepslate and stone at Y 1 to 7. A straight line would give
// 0.875 down to 0.125. Rock patches that replace only stone would lift it, which is what the data
// files suggest (BEDROCK-NOTES section 5 row 8), so this test bears on that choice.
func TestDeepslateBlendMatchesBedrock(t *testing.T) {
	want := []float64{0.9021, 0.8008, 0.6914, 0.5736, 0.4462, 0.3093, 0.1613}
	var deep, stone [7]float64
	var mu sync.Mutex
	bedrockSample(2, func(g *Generator, c *chunk.Chunk, _, _ int) {
		var d, s [7]float64
		for x := range uint8(16) {
			for z := range uint8(16) {
				for i := range want {
					switch c.Block(x, int16(i+1), z, 0) {
					case ridDeepslate:
						d[i]++
					case ridStone:
						s[i]++
					}
				}
			}
		}
		mu.Lock()
		defer mu.Unlock()
		for i := range d {
			deep[i] += d[i]
			stone[i] += s[i]
		}
	})
	for i, w := range want {
		got := deep[i] / (deep[i] + stone[i])
		t.Logf("Y %d: deepslate %.3f, Bedrock %.3f", i+1, got, w)
		if math.Abs(got-w) > 0.02 {
			t.Errorf("Y %d: deepslate share %.3f, Bedrock %.3f, tolerance 0.02", i+1, got, w)
		}
	}
}

// Bedrock's climate values where they differ from Java's (BEDROCK-NOTES section 5 row 5). Snow
// reads temperature from Dragonfly's biomes, so these pin what it reads.
func TestBedrockClimateValues(t *testing.T) {
	for _, c := range []struct {
		b    world.Biome
		want float64
	}{{biome.Savanna{}, 1.2}, {biome.SavannaPlateau{}, 1.0}, {biome.Meadow{}, 0.3}, {biome.CherryGrove{}, 0.3}} {
		if got := c.b.Temperature(); got != c.want {
			t.Errorf("%s temperature %v, Bedrock %v", c.b.String(), got, c.want)
		}
	}
}

// bedrockVolume is the blocks under the ground top per chunk at each Y from 64 to 191, the mean of
// the 4 Bedrock worlds' height maps (seedN-maps.npz).
var bedrockVolume = [128]float64{141.9, 136.6, 130.6, 124.5, 118.2, 111.8, 105.3, 99.4, 94.5, 92.0, 89.7, 87.5, 85.4, 83.2, 80.9, 78.5, 76.1, 74.7, 73.4, 72.2, 71.1, 69.9, 68.4, 66.5, 64.6, 63.6, 62.5, 61.4, 59.6, 57.3, 55.2, 52.9, 50.7, 49.6, 48.4, 47.2, 45.9, 44.6, 43.1, 41.4, 39.6, 38.6, 37.6, 36.5, 35.5, 34.4, 33.0, 31.4, 29.9, 29.1, 28.2, 27.3, 26.3, 25.2, 23.9, 22.7, 21.5, 20.7, 20.0, 19.2, 18.4, 17.6, 16.8, 15.9, 14.9, 14.4, 14.0, 13.5, 13.0, 12.5, 12.0, 11.4, 10.9, 10.5, 10.2, 9.9, 9.5, 9.2, 8.8, 8.4, 8.0, 7.8, 7.5, 7.2, 6.9, 6.6, 6.2, 5.8, 5.4, 5.2, 4.9, 4.7, 4.4, 4.1, 3.7, 3.4, 3.1, 2.8, 2.6, 2.4, 2.2, 1.9, 1.7, 1.5, 1.2, 1.1, 1.0, 0.9, 0.8, 0.7, 0.6, 0.5, 0.5, 0.4, 0.4, 0.3, 0.3, 0.3, 0.2, 0.2, 0.2, 0.2, 0.2, 0.1, 0.1, 0.1, 0.1, 0.1}

// Ore density above Y 64: ore blocks per 1000 blocks under the ground top. A plain count there says
// more about how much land a window holds than about ore, so both worlds divide by their own
// volume. Bedrock's volumes come from the 4 worlds' height maps, and its counts from each seed's
// per_chunk_by_y. A seed with under 50 blocks a chunk in a band is left out. Within a band ore
// density changes with Y (iron rises fast from Y 96) and the land thins out upward at a different
// rate in each world, so ours is measured per Y and weighted by Bedrock's volume at that Y. The
// tolerance is two standard errors of the seeds' densities, with a floor of 20%.
func TestOreDensityMatchesBedrock(t *testing.T) {
	rows := []struct {
		ore    string
		band   int // Index into oreBands.
		bySeed []float64
	}{
		{"coal", 8, []float64{10.99, 6.30, 12.03, 9.45}},
		{"coal", 9, []float64{11.13, 12.15, 12.36}},
		{"coal", 10, []float64{18.97, 21.54, 23.40}},
		{"iron", 8, []float64{0.50, 0.25, 0.57, 0.42}},
		{"iron", 9, []float64{2.64, 3.33, 2.97}},
		{"iron", 10, []float64{5.63, 6.42, 5.40}},
		{"copper", 8, []float64{5.12, 2.75, 3.41, 5.61}},
		{"copper", 9, []float64{0.72, 0.51, 0.98}},
	}
	byID := map[uint32]string{}
	for name, p := range oreIDs() {
		byID[p[0]], byID[p[1]] = name, name
	}
	ground := GroundBlocks()
	blocks := map[string]*[128]float64{"coal": {}, "iron": {}, "copper": {}}
	var volume [128]float64
	var mu sync.Mutex
	bedrockSample(1, func(g *Generator, c *chunk.Chunk, _, _ int) {
		local := map[string]*[128]float64{"coal": {}, "iron": {}, "copper": {}}
		var vol [128]float64
		for x := range uint8(16) {
			for z := range uint8(16) {
				top := groundTop(c, ground, x, z)
				for y := 64; y <= top && y < 192; y++ {
					vol[y-64]++
					if name, ok := byID[c.Block(x, int16(y), z, 0)]; ok && local[name] != nil {
						local[name][y-64]++
					}
				}
			}
		}
		mu.Lock()
		defer mu.Unlock()
		for name, l := range local {
			for i := range l {
				blocks[name][i] += l[i]
			}
		}
		for i := range vol {
			volume[i] += vol[i]
		}
	})
	for _, r := range rows {
		var weighted, weight, plainOre, plainVol float64
		for y := oreBands[r.band]; y < oreBands[r.band+1]; y++ {
			i := y - 64
			plainOre += blocks[r.ore][i]
			plainVol += volume[i]
			if volume[i] > 0 {
				weighted += blocks[r.ore][i] / volume[i] * bedrockVolume[i]
				weight += bedrockVolume[i]
			}
		}
		got := weighted / weight * 1000
		var sum float64
		for _, v := range r.bySeed {
			sum += v
		}
		want := sum / float64(len(r.bySeed))
		tol := max(seedTwoSE(r.bySeed), want*0.2)
		t.Logf("%-6s Y %d to %d: %.2f per 1000 under the ground (%.2f on our own land), Bedrock %.2f, tolerance %.2f", r.ore, oreBands[r.band], oreBands[r.band+1], got, plainOre/plainVol*1000, want, tol)
		if math.Abs(got-want) > tol {
			t.Errorf("%s at Y %d to %d: %.2f per 1000, Bedrock %.2f, tolerance %.2f", r.ore, oreBands[r.band], oreBands[r.band+1], got, want, tol)
		}
	}
}

// Dirt under the soil layer, separating patches from soil: blocks at least 4 under the ground top, per
// 1000 blocks of Y 0 to 160 so deep. Bedrock's figure is from the one raw world kept here, seed 100:
// 342.7 such dirt a chunk over 21,485 blocks, so 15.95. One seed, so 20%. Soil, the rest, is logged:
// seed 100 has 516 a chunk over a window 87% land, about 2.3 a land column.
func TestDirtPatchesMatchBedrock(t *testing.T) {
	ground := GroundBlocks()
	var deep, soil, volume, land float64
	var mu sync.Mutex
	bedrockSample(2, func(g *Generator, c *chunk.Chunk, _, _ int) {
		var d, s, v, l float64
		for x := range uint8(16) {
			for z := range uint8(16) {
				top := groundTop(c, ground, x, z)
				if top >= SeaLevel {
					l++
				}
				v += float64(max(min(top-4, 160), 0))
				for y := minY; y <= top; y++ {
					if c.Block(x, int16(y), z, 0) != g.blocks.dirt {
						continue
					}
					if y < top-4 {
						d++
					} else {
						s++
					}
				}
			}
		}
		mu.Lock()
		defer mu.Unlock()
		deep += d
		soil += s
		volume += v
		land += l
	})
	got := deep / volume * 1000
	t.Logf("dirt under the soil layer %.2f per 1000, Bedrock seed 100 15.95; soil %.2f a land column, Bedrock seed 100 about 2.3", got, soil/land)
	if math.Abs(got-15.95) > 15.95*0.2 {
		t.Errorf("dirt under the soil layer %.2f per 1000, Bedrock 15.95, tolerance 20%%", got)
	}
}

// Rare mountain ores per chunk in the same 4 windows, by seed (aggregate.json; infested is stone plus
// deepslate). This replaces two tests. One asked for 20.6 infested blocks per chunk of a mountain
// biome on other seeds, a figure made by dividing Bedrock's count by the mountain share at the surface.
// The other asked for the study's addendum A5 emerald figures, which assumed a whole region in one
// biome and no biome change with height. Real counts in the same windows need neither assumption, and
// both biome rules read the blob's own height to meet them. Tolerance 30% a seed, the wider of the
// old two, plus 0.01 so a seed with none can hold a stray blob's edge at most.
func TestMountainOresMatchBedrock(t *testing.T) {
	b := newBlocks()
	emerald := oreIDs()["emerald"]
	rows := []struct {
		name string
		ids  [2]uint32
		want map[int64]float64
	}{
		{"infested", b.veins[veinInfested], map[int64]float64{100: 1.05, 1: 0, 12345: 2.79, 987654321: 1.10}},
		{"emerald", emerald, map[int64]float64{100: 0.171, 1: 0, 12345: 0.412, 987654321: 0.162}},
	}
	for _, seed := range goldenSeeds {
		g := New(seed)
		var mu sync.Mutex
		counts := make([]float64, len(rows))
		var wg sync.WaitGroup
		sem := make(chan struct{}, runtime.NumCPU())
		for cx := -35; cx < 35; cx++ {
			for cz := -35; cz < 35; cz++ {
				wg.Add(1)
				sem <- struct{}{}
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					c := generate(g, int32(cx), int32(cz))
					n := make([]float64, len(rows))
					for x := range uint8(16) {
						for z := range uint8(16) {
							for y := int16(-16); y <= maxY; y++ {
								r := c.Block(x, y, z, 0)
								for i := range rows {
									if r == rows[i].ids[0] || r == rows[i].ids[1] {
										n[i]++
									}
								}
							}
						}
					}
					mu.Lock()
					for i := range n {
						counts[i] += n[i]
					}
					mu.Unlock()
				}()
			}
		}
		wg.Wait()
		for i, r := range rows {
			got := counts[i] / (70 * 70)
			t.Logf("seed %d: %s %.3f a chunk, Bedrock %.3f", seed, r.name, got, r.want[seed])
			if math.Abs(got-r.want[seed]) > r.want[seed]*0.3+0.01 {
				t.Errorf("seed %d: %s %.3f a chunk, Bedrock %.3f, tolerance 30%%", seed, r.name, got, r.want[seed])
			}
		}
	}
}
