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
// Four windows of 4900 chunks are a small sample: seed 1 is 82% sea and seed 100 is 13%. So a
// tolerance here is never tighter than two standard errors of the mean of the 4 seeds, worked out
// from the by-seed figures below. On figures the seeds agree on, a floor of 10% of the figure
// (ores) or 1 point (air) stands in for what those windows cannot show.

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

// bedrockSample visits chunks spread over an area of statSeeds worlds, in parallel.
func bedrockSample(step int, visit func(g *Generator, c *chunk.Chunk, cx, cz int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for seed := int64(1); seed <= statSeeds; seed++ {
		g := New(seed)
		for cx := -statSpan / 32; cx < statSpan/32; cx += step {
			for cz := -statSpan / 32; cz < statSpan/32; cz += step {
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

// groundIDs is the study's idea of ground: not air, water, lava, plants, logs or leaves. The list
// follows the study's measuring tool, for the blocks we place.
func groundIDs() map[uint32]bool {
	b := newBlocks()
	m := map[uint32]bool{}
	for _, id := range []uint32{b.stone, b.granite, b.diorite, b.andesite, b.deepslate, b.tuff, b.calcite, b.dirt, b.coarseDirt,
		b.grass, b.podzol, b.mycelium, b.mud, b.clay, b.sand, b.redSand, b.sandstone, b.redSandstone, b.gravel, b.bedrock,
		b.packedIce, b.snow, b.amethyst, b.smoothBasalt, b.rawIron, b.rawCopper} {
		m[id] = true
	}
	for _, id := range b.terracotta {
		m[id] = true
	}
	for _, p := range b.veins {
		m[p[0]], m[p[1]] = true, true
	}
	return m
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
	ground := groundIDs()
	var mu sync.Mutex
	bedrockSample(8, func(g *Generator, c *chunk.Chunk, _, _ int) {
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
	bedrockSample(16, func(g *Generator, c *chunk.Chunk, _, _ int) {
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
// Infested stone (1.23) rests on the few mountain chunks in the windows, none on seed 1.
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
	bedrockSample(16, func(g *Generator, c *chunk.Chunk, _, _ int) {
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
	bedrockSample(16, func(g *Generator, c *chunk.Chunk, _, _ int) {
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
