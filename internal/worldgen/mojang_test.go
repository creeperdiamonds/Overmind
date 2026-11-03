package worldgen

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// worldStats holds the measures the study took from the game, worked out the same way:
// 3 seeds, 8192 x 8192 blocks, a 32-block grid, patches connected over 4 neighbours,
// height with noise caves but no carvers.
type worldStats struct {
	biomes                                                        int
	share                                                         map[string]float64
	patches, weightedPatch                                        float64
	patchOf                                                       map[string]float64
	landMedian, landP99, landP999                                 int
	land63to69, land170                                           float64
	step4Mean, step16Mean                                         float64
	step4P99, step16P99                                           int
	floorP10                                                      int
	std                                                           [5]float64
	contMedian, humid07, cont08, ocean, below63, river, farInland float64
}

const (
	statSpan  = 8192
	statStep  = 32
	statSeeds = 3
)

// studySeeds are the seeds the study's 8192 square measures are from. Biomes and terrain now follow
// the seed, so measuring on them compares like for like.
var studySeeds = [statSeeds]int64{1, 12345, 987654321}

func measureWorld() *worldStats {
	const n = statSpan/statStep + 1
	type sample struct {
		biome string
		h     int
		s4    int
		s16   int
		cl    Climate
	}
	all := make([]sample, 0, statSeeds*n*n)
	s := &worldStats{share: map[string]float64{}, patchOf: map[string]float64{}}
	areas := map[string][]int{}
	for _, seed := range studySeeds {
		g := New(seed)
		grid := make([]sample, n*n)
		rows := make(chan int)
		var wg sync.WaitGroup
		for range runtime.NumCPU() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range rows {
					for j := range n {
						x, z := -statSpan/2+i*statStep, -statSpan/2+j*statStep
						h := g.noiseHeight(x, z)
						grid[i*n+j] = sample{
							biome: g.BiomeAt(x, maxY, z).String(),
							h:     h,
							s4:    abs(g.noiseHeight(x+4, z) - h),
							s16:   abs(g.noiseHeight(x+16, z) - h),
							cl:    g.Climate(x, z),
						}
					}
				}
			}()
		}
		for i := range n {
			rows <- i
		}
		close(rows)
		wg.Wait()
		all = append(all, grid...)
		seen := make([]bool, n*n)
		for i := range n * n {
			if seen[i] {
				continue
			}
			area := 0
			stack := []int{i}
			seen[i] = true
			name := grid[i].biome
			for len(stack) > 0 {
				p := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				area++
				a, b := p/n, p%n
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					u, v := a+d[0], b+d[1]
					if u < 0 || v < 0 || u >= n || v >= n {
						continue
					}
					q := u*n + v
					if !seen[q] && grid[q].biome == name {
						seen[q] = true
						stack = append(stack, q)
					}
				}
			}
			areas[name] = append(areas[name], area)
		}
	}
	var patchCount, sumA, sumA2 float64
	for name, list := range areas {
		var a1, a2 float64
		for _, a := range list {
			a1 += float64(a)
			a2 += float64(a) * float64(a)
		}
		s.patchOf[name] = a2 / a1 * 4 // A grid cell of 32 x 32 blocks is 4 chunks.
		patchCount += float64(len(list))
		sumA += a1
		sumA2 += a2
	}
	s.patches = patchCount / statSeeds
	s.weightedPatch = sumA2 / sumA * 4
	s.biomes = len(areas)

	total := float64(len(all))
	var land, heights, s4, s16 []int
	var sums, squares [5]float64
	conts := make([]float64, 0, len(all))
	for _, v := range all {
		s.share[v.biome] += 100 / total
		heights = append(heights, v.h)
		vals := [5]float64{v.cl.Temperature, v.cl.Humidity, v.cl.Continentalness, v.cl.Erosion, v.cl.Weirdness}
		for i, c := range vals {
			sums[i] += c
			squares[i] += c * c
		}
		conts = append(conts, v.cl.Continentalness)
		if v.cl.Humidity >= 0.7 {
			s.humid07 += 100 / total
		}
		if v.cl.Continentalness >= 0.8 {
			s.cont08 += 100 / total
		}
		if v.cl.Continentalness < oceanBelow {
			s.ocean += 100 / total
		}
		if v.cl.Continentalness >= midBelow {
			s.farInland += 100 / total
		}
		if v.h < SeaLevel {
			s.below63 += 100 / total
			continue
		}
		land = append(land, v.h)
		s4 = append(s4, v.s4)
		s16 = append(s16, v.s16)
		if v.h <= 69 {
			s.land63to69++
		}
		if v.h >= 170 {
			s.land170++
		}
	}
	for i := range s.std {
		m := sums[i] / total
		s.std[i] = math.Sqrt(squares[i]/total - m*m)
	}
	sort.Float64s(conts)
	s.contMedian = conts[len(conts)/2]
	s.land63to69 *= 100 / float64(len(land))
	s.land170 *= 100 / float64(len(land))
	s.landMedian, s.landP99, s.landP999 = percentile(land, 0.5), percentile(land, 0.99), percentile(land, 0.999)
	s.floorP10 = percentile(heights, 0.1)
	s.step4Mean, s.step4P99 = mean(s4), percentile(s4, 0.99)
	s.step16Mean, s.step16P99 = mean(s16), percentile(s16, 0.99)
	s.river = s.share["river"] + s.share["frozen_river"]
	return s
}

// TestWorldMatchesMeasuredFigures compares our world with the figures measured from Java
// 26.1 by the study (GAPS.md, NOTES-how-it-works.md), on the same sampling: 3 seeds of
// 8192 x 8192 on a 32-block grid. The tolerances allow for what 3 seeds can vary by, which
// the study puts at about 1 point for small shares, and more for shares that follow
// temperature (wavelength 4096, so only about two wavelengths are in the sample).
func TestWorldMatchesMeasuredFigures(t *testing.T) {
	s := measureWorld()
	type row struct {
		name         string
		ours, mojang float64
		tol          float64
	}
	rows := []row{
		{"river + frozen river %", s.river, 6.8, 1.5},
		{"ocean biomes %", s.ocean, 27.8, 6},
		{"forest %", s.share["forest"], 12.1, 4},
		{"beach %", s.share["beach"] + s.share["cold_beach"], 2.0, 1},
		{"plains %", s.share["plains"], 10.0, 4},
		{"humidity 0.7+ %", s.humid07, 0.21, 0.4},
		{"continentalness 0.8+ %", s.cont08, 2.13, 1},
		{"distinct biomes", float64(s.biomes), 53, 8},
		{"patches per 8192 square", s.patches, 4012, 500},
		{"area-weighted patch (chunks)", s.weightedPatch, 1734, 400},
		{"plains patch (chunks)", s.patchOf["plains"], 1507, 500},
		{"savanna patch (chunks)", s.patchOf["savanna"], 2156, 700},
		{"jungle patch (chunks)", s.patchOf["jungle"], 583, 250},
		{"land median Y", float64(s.landMedian), 71, 4},
		{"land p99 Y", float64(s.landP99), 150, 15},
		{"land p99.9 Y", float64(s.landP999), 182, 20},
		{"land in Y 63-69 %", s.land63to69, 41, 8},
		{"land at Y 170+ %", s.land170, 0.25, 0.5},
		{"step over 4, mean", s.step4Mean, 1.34, 0.4},
		{"step over 4, p99", float64(s.step4P99), 11, 4},
		{"step over 16, mean", s.step16Mean, 4.36, 1.2},
		{"step over 16, p99", float64(s.step16P99), 30, 10},
		{"floor p10 Y", float64(s.floorP10), 45, 5},
		{"temperature std", s.std[0], 0.380, 0.08},
		{"humidity std", s.std[1], 0.252, 0.05},
		{"continentalness std", s.std[2], 0.373, 0.06},
		{"erosion std", s.std[3], 0.309, 0.05},
		{"weirdness std", s.std[4], 0.353, 0.05},
		{"continentalness median", s.contMedian, 0.04, 0.12},
		{"far inland %", s.farInland, 25, 7},
	}
	below, inland := measureBelowSea()
	t.Logf("below Y 63 %.1f%%; inland not river or swamp %.1f%%", below, inland)
	rows = append(rows,
		row{"below Y 63 %", below, 45, 3},
		row{"inland under Y 63 %", inland, inlandBelowSeaFigure, 2})
	var b strings.Builder
	fmt.Fprintf(&b, "\n%-30s %9s %9s %7s\n", "measure", "ours", "mojang", "tol")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-30s %9.2f %9.2f %7.2f\n", r.name, r.ours, r.mojang, r.tol)
	}
	names := make([]string, 0, len(s.share))
	for name := range s.share {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int { return int(s.share[b]*1000 - s.share[a]*1000) })
	for _, name := range names {
		fmt.Fprintf(&b, "%s:%.1f ", name, s.share[name])
	}
	t.Log(b.String())
	for _, r := range rows {
		if math.Abs(r.ours-r.mojang) > r.tol {
			t.Errorf("%s: ours %.2f, measured from the game %.2f, tolerance %.2f", r.name, r.ours, r.mojang, r.tol)
		}
	}
}

// inlandBelowSeaFigure is the target share of inland columns (continentalness -0.11 or more,
// not river or swamp biomes) with top solid under Y 63: about 14% as set for this fix. A recount
// of the study's raw probe grid (mojang-grid.csv) gives 12.8%.
const inlandBelowSeaFigure = 14.0

// measureBelowSea gives the share of all columns under Y 63, and the inland share above, on the
// study's seeds and sampling grid.
func measureBelowSea() (below, inland float64) {
	const n = statSpan/statStep + 1
	var total, under, inlandTotal, inlandUnder atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, seed := range studySeeds {
		g := New(seed)
		for i := range n {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				for j := range n {
					x, z := -statSpan/2+i*statStep, -statSpan/2+j*statStep
					h := g.noiseHeight(x, z)
					total.Add(1)
					if h < SeaLevel {
						under.Add(1)
					}
					if g.Climate(x, z).Continentalness < -0.11 {
						continue
					}
					switch g.BiomeAt(x, maxY, z).String() {
					case "river", "frozen_river", "swampland", "mangrove_swamp":
						continue
					}
					inlandTotal.Add(1)
					if h < SeaLevel {
						inlandUnder.Add(1)
					}
				}
			}()
		}
	}
	wg.Wait()
	return float64(under.Load()) / float64(total.Load()) * 100, float64(inlandUnder.Load()) / float64(inlandTotal.Load()) * 100
}

// TestCaveSharesMatchMeasuredFigures measures blocks at least 8 under the column top, with
// noise caves and fluids only (no carvers), on the study's bands, statSeeds seeds over 8192
// blocks. Air must be within 1.5 points and each fluid within 1. Every figure is one recount of
// the study's probe output probe-out/mojang-cols2.csv (60,000 columns of 384 blocks, all biomes,
// ocean included): per column, blocks from the band's bottom up to 8 under that column's top
// solid block, the same filter as below. It replaces the notes' section 7 table, whose
// Y 100 to 160 row (7.8%) rested on 1,359 blocks; the recount has 65,497 there. Applied to all
// bands alike: the notes' figures differ by up to 1 point (for example 7.7 against 8.72 at
// Y -20 to 0).
func TestCaveSharesMatchMeasuredFigures(t *testing.T) {
	const airTol, fluidTol = 1.5, 1.0
	bands := []struct {
		lo, hi           int
		air, water, lava float64
	}{
		{-64, -54, 0.00, 0.00, 0.39},
		{-54, -40, 3.88, 0.02, 0.01},
		{-40, -20, 7.22, 0.39, 0.21},
		{-20, 0, 8.72, 0.44, 0.01},
		{0, 20, 5.76, 3.05, 0.00},
		{20, 40, 5.11, 1.81, 0.00},
		{40, 63, 3.43, 1.63, 0.00},
		{63, 100, 5.10, 0.06, 0.00},
		{100, 160, 4.41, 0.02, 0.00},
	}
	var air, water, lava, total [9]float64
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, seed := range studySeeds {
		g := New(seed)
		for cx := -statSpan / 32; cx < statSpan/32; cx += 8 {
			for cz := -statSpan / 32; cz < statSpan/32; cz += 8 {
				wg.Add(1)
				sem <- struct{}{}
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
					s := scratchPool.Get().(*chunkScratch)
					defer scratchPool.Put(s)
					g.buildBase(&s.base, s, cx, cz, stageNoise, 0, 15, 0, 15)
					g.writeChunk(c, &s.base)
					var top [16][16]int16
					for tx := range 16 {
						for tz := range 16 {
							top[tx][tz] = s.base.wgFloor[tz<<4|tx] - 1
						}
					}
					var a, w, l, n [9]float64
					for x := uint8(0); x < 16; x += 5 {
						for z := uint8(0); z < 16; z += 5 {
							for i, b := range bands {
								for y := b.lo; y < min(b.hi, int(top[x][z])-8); y++ {
									n[i]++
									switch c.Block(x, int16(y), z, 0) {
									case g.blocks.air:
										a[i]++
									case g.blocks.water:
										w[i]++
									case g.blocks.lava:
										l[i]++
									}
								}
							}
						}
					}
					mu.Lock()
					for i := range n {
						air[i] += a[i]
						water[i] += w[i]
						lava[i] += l[i]
						total[i] += n[i]
					}
					mu.Unlock()
				}()
			}
		}
	}
	wg.Wait()
	var b strings.Builder
	fmt.Fprintf(&b, "\n%-14s %14s %14s %14s\n", "band", "air ours/game", "water", "lava")
	for i, r := range bands {
		pa, pw, pl := air[i]/total[i]*100, water[i]/total[i]*100, lava[i]/total[i]*100
		fmt.Fprintf(&b, "%4d to %4d   %6.1f /%5.1f  %6.1f /%5.1f  %6.1f /%5.1f\n", r.lo, r.hi, pa, r.air, pw, r.water, pl, r.lava)
		if math.Abs(pa-r.air) > airTol || math.Abs(pw-r.water) > fluidTol || math.Abs(pl-r.lava) > fluidTol {
			t.Errorf("Y %d to %d: air %.2f water %.2f lava %.2f; measured from the game %.2f %.2f %.2f", r.lo, r.hi, pa, pw, pl, r.air, r.water, r.lava)
		}
		if r.lo == -64 && pa != 0 {
			t.Errorf("Y -64 to -54: %.2f%% air, the study measured none; open space there is lava", pa)
		}
	}
	t.Log(b.String())
}

// openNoiseCaves measures land columns whose cave air joins the sky, by the study's method
// (addendum A4): noise caves only, no carvers; random 32 x 32 windows; air flooded from the top
// of the window over 6 directions inside it; land is a top solid block at Y 63 or above. Every
// column of each window counts.
func openNoiseCaves(g *Generator, seed int64, windows int) (land, open int) {
	const height = maxY - minY + 1
	index := func(x, y, z int) int { return (x*32+z)*height + y }
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for w := range windows {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			h := mix64(uint64(seed) ^ mix64(uint64(w)*31+99))
			cx, cz := int(h%512)-256, int((h>>20)%512)-256
			var chunks [2][2]*chunk.Chunk
			var tops [2][2][16][16]int16
			for a := range 2 {
				for b := range 2 {
					chunks[a][b] = chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
					s := new(chunkScratch)
					s.startChunk(g, (cx+a)*16, (cz+b)*16)
					noiseOnly(g, chunks[a][b], s, (cx+a)*16, (cz+b)*16, &tops[a][b])
				}
			}
			air := make([]bool, 32*32*height)
			seen := make([]bool, 32*32*height)
			var stack []int
			for x := range 32 {
				for z := range 32 {
					for y := range height {
						air[index(x, y, z)] = chunks[x/16][z/16].Block(uint8(x%16), int16(y+minY), uint8(z%16), 0) == g.blocks.air
					}
					i := index(x, height-1, z)
					seen[i] = true
					stack = append(stack, i)
				}
			}
			for len(stack) > 0 {
				p := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				y, q := p%height, p/height
				x, z := q/32, q%32
				for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
					u, v, k := x+d[0], y+d[1], z+d[2]
					if u < 0 || u >= 32 || k < 0 || k >= 32 || v < 0 || v >= height {
						continue
					}
					if i := index(u, v, k); air[i] && !seen[i] {
						seen[i] = true
						stack = append(stack, i)
					}
				}
			}
			l, o := 0, 0
			for x := range 32 {
				for z := range 32 {
					top := int(tops[x/16][z/16][x%16][z%16])
					if top < SeaLevel {
						continue
					}
					l++
					for y := top - 1; y >= minY; y-- {
						if seen[index(x, y-minY, z)] {
							o++
							break
						}
					}
				}
			}
			mu.Lock()
			land += l
			open += o
			mu.Unlock()
		}()
	}
	wg.Wait()
	return land, open
}

// TestNoiseCavesOpenToSky asserts the measured figure: 4.7% of land columns (addendum A4), which
// the game itself spread over 3.5 to 6.6 per seed. Because seeds vary that much, ours is pooled over
// 12 seeds and must be within 1 point of 4.7. Carvers are left out, as they were there.
func TestNoiseCavesOpenToSky(t *testing.T) {
	const seeds = 12
	var land, open int
	for seed := int64(1); seed <= seeds; seed++ {
		l, o := openNoiseCaves(New(seed), seed, 60)
		t.Logf("seed %d: %d land columns, %.2f%% open to the sky", seed, l, float64(o)/float64(l)*100)
		land += l
		open += o
	}
	share := float64(open) / float64(land) * 100
	t.Logf("%d seeds: %d land columns, %.2f%% open; measured from the game 4.7%%", seeds, land, share)
	if math.Abs(share-4.7) > 1 {
		t.Errorf("%.2f%% of land columns open to the sky over %d seeds, measured 4.7%% give or take 1", share, seeds)
	}
}

// noiseOnly writes a chunk's noise stage and each column's highest solid block.
func noiseOnly(g *Generator, c *chunk.Chunk, s *chunkScratch, originX, originZ int, top *[16][16]int16) {
	g.buildBase(&s.base, s, originX>>4, originZ>>4, stageNoise, 0, 15, 0, 15)
	g.writeChunk(c, &s.base)
	for x := range 16 {
		for z := range 16 {
			top[x][z] = s.base.wgFloor[z<<4|x] - 1
		}
	}
}
