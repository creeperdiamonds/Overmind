package worldgen

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
)

// surfaceName names the surface biome of a column. Y above every possible ground makes
// BiomeAt skip cave biomes.
func surfaceName(g *Generator, x, z int) string {
	return g.BiomeAt(x, maxY, z).String()
}

var requiredSurfaceBiomes = []string{
	(biome.Plains{}).String(), (biome.SunflowerPlains{}).String(), (biome.Forest{}).String(), (biome.FlowerForest{}).String(), (biome.BirchForest{}).String(), (biome.DarkForest{}).String(),
	(biome.Taiga{}).String(), (biome.SnowyTaiga{}).String(), (biome.SnowyPlains{}).String(), (biome.IceSpikes{}).String(), (biome.Desert{}).String(), (biome.Savanna{}).String(), (biome.Jungle{}).String(),
	(biome.Swamp{}).String(), (biome.Badlands{}).String(), (biome.Meadow{}).String(), (biome.Grove{}).String(), (biome.SnowySlopes{}).String(), (biome.JaggedPeaks{}).String(), (biome.FrozenPeaks{}).String(),
	(biome.StonyPeaks{}).String(), (biome.WindsweptHills{}).String(), (biome.Beach{}).String(), (biome.SnowyBeach{}).String(), (biome.StonyShore{}).String(), (biome.River{}).String(),
	(biome.FrozenRiver{}).String(), (biome.MushroomFields{}).String(),
	(biome.WarmOcean{}).String(), (biome.LukewarmOcean{}).String(), (biome.ColdOcean{}).String(), (biome.FrozenOcean{}).String(),
	(biome.DeepWarmOcean{}).String(), (biome.DeepLukewarmOcean{}).String(), (biome.DeepColdOcean{}).String(), (biome.DeepFrozenOcean{}).String(),
}

func TestEveryRequiredBiomeAppears(t *testing.T) {
	const span, step = 8192, 32
	seen := map[string]int{}
	for seed := int64(1); seed <= 5; seed++ {
		g := New(seed)
		for x := -span / 2; x < span/2; x += step {
			for z := -span / 2; z < span/2; z += step {
				seen[surfaceName(g, x, z)]++
			}
		}
	}
	for _, name := range requiredSurfaceBiomes {
		if seen[name] == 0 {
			t.Errorf("biome %s never appeared in 5 seeds of %dx%d", name, span, span)
		}
	}
	t.Logf("samples per biome over 5 seeds: %v", seen)
}

func TestCaveBiomesAppearUnderground(t *testing.T) {
	seen := map[string]bool{}
	for seed := int64(1); seed <= 5; seed++ {
		g := New(seed)
		for x := -4096; x < 4096; x += 32 {
			for z := -4096; z < 4096; z += 32 {
				seen[g.BiomeAt(x, 0, z).String()] = true
			}
		}
	}
	for _, name := range []string{(biome.DripstoneCaves{}).String(), (biome.LushCaves{}).String()} {
		if !seen[name] {
			t.Errorf("%s never appeared at Y 0", name)
		}
	}
}

func TestNoHardClimateJumps(t *testing.T) {
	cold := []string{(biome.SnowyPlains{}).String(), (biome.IceSpikes{}).String(), (biome.SnowyTaiga{}).String()}
	hot := []string{(biome.Desert{}).String(), (biome.Badlands{}).String()}
	const span, cell = 4096, 4 // Neighbouring biome cells are 4 blocks apart.
	const n = span / cell
	clash := func(a, b string) bool {
		return (slices.Contains(hot, a) && slices.Contains(cold, b)) || (slices.Contains(cold, a) && slices.Contains(hot, b))
	}
	for seed := int64(1); seed <= 5; seed++ {
		g := New(seed)
		grid := make([][n]string, n)
		for i := range grid {
			for j := range grid[i] {
				grid[i][j] = surfaceName(g, i*cell-span/2, j*cell-span/2)
			}
		}
		for i := range n - 1 {
			for j := range n - 1 {
				if clash(grid[i][j], grid[i+1][j]) || clash(grid[i][j], grid[i][j+1]) {
					t.Fatalf("seed %d at (%d,%d): %s is right beside a hot or cold opposite", seed, i*cell-span/2, j*cell-span/2, grid[i][j])
				}
			}
		}
	}
}

// findColumns returns up to n block positions, spread out, whose surface biome has a name.
func findColumns(g *Generator, name string, n int) [][2]int {
	var out [][2]int
	for x := -4096; x < 4096 && len(out) < n; x += 32 {
		for z := -4096; z < 4096 && len(out) < n; z += 32 {
			if surfaceName(g, x, z) == name {
				out = append(out, [2]int{x, z})
			}
		}
	}
	return out
}

func TestSurfaceMatchesBiome(t *testing.T) {
	b := newBlocks()
	// A biome maps to the top blocks it may have. Underwater columns are checked apart.
	want := map[string][]uint32{
		(biome.Desert{}).String():         {b.sand},
		(biome.Badlands{}).String():       append([]uint32{b.redSand}, b.terracotta...),
		(biome.SnowyPlains{}).String():    {b.grass},
		(biome.IceSpikes{}).String():      {b.snow},
		(biome.SnowyTaiga{}).String():     {b.grass},
		(biome.Plains{}).String():         {b.grass},
		(biome.Forest{}).String():         {b.grass},
		(biome.Taiga{}).String():          {b.grass, b.podzol},
		(biome.MushroomFields{}).String(): {b.mycelium},
		(biome.Beach{}).String():          {b.sand},
		(biome.StonyShore{}).String():     {b.stone, b.gravel},
		(biome.JaggedPeaks{}).String():    {b.stone, b.snow},
		(biome.FrozenPeaks{}).String():    {b.stone, b.snow},
		(biome.StonyPeaks{}).String():     {b.stone},
		(biome.SnowySlopes{}).String():    {b.stone, b.snow},
	}
	for name, tops := range want {
		found := 0
		for seed := int64(1); seed <= 5 && found < 12; seed++ {
			g := New(seed)
			for _, p := range findColumns(g, name, 12) {
				c := generate(g, int32(p[0]>>4), int32(p[1]>>4))
				x, z := uint8(p[0]&15), uint8(p[1]&15)
				top := topSolid(c, x, z)
				if top < SeaLevel {
					continue // Floors are checked in TestOceanFloors.
				}
				found++
				if got := c.Block(x, int16(top), z, 0); !slices.Contains(tops, got) {
					t.Errorf("%s at (%d,%d) Y %d: top block id %d, want one of %v", name, p[0], p[1], top, got, tops)
				}
			}
		}
		if found == 0 {
			t.Errorf("no dry %s column found to check", name)
		}
	}
}

func TestOceanFloors(t *testing.T) {
	b := newBlocks()
	g := New(11)
	found := 0
	for _, name := range []string{(biome.WarmOcean{}).String(), (biome.ColdOcean{}).String(), (biome.DeepOcean{}).String(), (biome.FrozenOcean{}).String(), (biome.River{}).String()} {
		for seed := int64(1); seed <= 5; seed++ {
			for _, p := range findColumns(New(seed), name, 6) {
				g = New(seed)
				c := generate(g, int32(p[0]>>4), int32(p[1]>>4))
				x, z := uint8(p[0]&15), uint8(p[1]&15)
				top := topSolid(c, x, z)
				if top >= SeaLevel {
					continue
				}
				found++
				if got := c.Block(x, int16(top), z, 0); got != b.sand && got != b.gravel && got != b.clay {
					t.Errorf("%s floor at (%d,%d): block id %d, want sand, gravel or clay", name, p[0], p[1], got)
				}
			}
		}
	}
	if found < 20 {
		t.Errorf("only %d ocean floor columns checked", found)
	}
}

func TestBiomesStoredInChunk(t *testing.T) {
	var chunks [][3]int // seed, chunk x, chunk z
	for seed := int64(1); seed <= 3; seed++ {
		g := New(seed)
		for _, p := range [][2]int32{{0, 0}, {-37, 12}} {
			chunks = append(chunks, [3]int{int(seed), int(p[0]), int(p[1])})
		}
		// Chunks holding each cave biome.
		for _, name := range []string{(biome.DripstoneCaves{}).String(), (biome.LushCaves{}).String()} {
			n := 0
			for x := -4096; x < 4096 && n < 1; x += 16 {
				for z := -4096; z < 4096 && n < 1; z += 16 {
					for y := 0; y < 60; y += 4 {
						if g.BiomeAt(x, y, z).String() == name {
							chunks = append(chunks, [3]int{int(seed), x >> 4, z >> 4})
							n++
							break
						}
					}
				}
			}
		}
	}
	caveBlocks := 0
	for _, k := range chunks {
		g := New(int64(k[0]))
		c := generate(g, int32(k[1]), int32(k[2]))
		for x := 0; x < 16; x++ {
			for z := 0; z < 16; z++ {
				for y := minY; y <= maxY; y += 3 {
					want := g.BiomeAt(k[1]*16+x, y, k[2]*16+z)
					if got := c.Biome(uint8(x), int16(y), uint8(z)); got != uint32(want.EncodeBiome()) {
						t.Fatalf("seed %d chunk (%d,%d) block (%d,%d,%d): stored biome %d, BiomeAt %s (%d)",
							k[0], k[1], k[2], x, y, z, got, want, want.EncodeBiome())
					}
					if s := want.String(); s == (biome.DripstoneCaves{}).String() || s == (biome.LushCaves{}).String() {
						caveBlocks++
					}
				}
			}
		}
	}
	if caveBlocks == 0 {
		t.Error("no cave biome blocks were stored in any tested chunk")
	}
}

// Biome size is the side of a square with the same area as a connected region of one
// biome, in blocks.
func TestBiomeSizeIsVanillaLike(t *testing.T) {
	const span, step = 4096, 16
	const n = span / step
	// Floor set from measured seeds 1-5 (94-173 blocks), to catch a return of the sliver problem (35-39 before the fix). Not a vanilla figure: no real-game measurement exists.
	const landMedianFloor = 90
	thin := map[string]bool{ // Rivers, beaches and shores are thin by nature.
		(biome.River{}).String(): true, (biome.FrozenRiver{}).String(): true,
		(biome.Beach{}).String(): true, (biome.SnowyBeach{}).String(): true, (biome.StonyShore{}).String(): true,
	}
	rivers := map[string]bool{(biome.River{}).String(): true, (biome.FrozenRiver{}).String(): true}
	for seed := int64(1); seed <= 5; seed++ {
		g := New(seed)
		var grid [n][n]world.Biome
		for i := range n {
			for j := range n {
				grid[i][j] = g.BiomeAt(i*step-span/2, maxY, j*step-span/2)
			}
		}
		var seen [n][n]bool
		var sizes, landSizes, areas []int
		var land, riverCells int
		for i := range n {
			for j := range n {
				name := grid[i][j].String()
				if !strings.Contains(name, "ocean") && name != (biome.MushroomFields{}).String() {
					land++
					if rivers[name] {
						riverCells++
					}
				}
				if seen[i][j] {
					continue
				}
				area := 0
				stack := [][2]int{{i, j}}
				seen[i][j] = true
				for len(stack) > 0 {
					p := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					area++
					for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
						a, b := p[0]+d[0], p[1]+d[1]
						if a < 0 || b < 0 || a >= n || b >= n || seen[a][b] || grid[a][b] != grid[i][j] {
							continue
						}
						seen[a][b] = true
						stack = append(stack, [2]int{a, b})
					}
				}
				size := int(math.Sqrt(float64(area)) * step)
				sizes = append(sizes, size)
				areas = append(areas, area)
				if !thin[name] && !strings.Contains(name, "ocean") && name != (biome.MushroomFields{}).String() {
					landSizes = append(landSizes, size)
				}
			}
		}
		// Weighted by area: the size of the region a random block is in.
		slices.Sort(areas)
		var total, acc int
		for _, a := range areas {
			total += a
		}
		weighted := 0
		for _, a := range areas {
			if acc += a; acc*2 >= total {
				weighted = int(math.Sqrt(float64(a)) * step)
				break
			}
		}
		riverShare := float64(riverCells) / float64(land)
		landMedian := percentile(landSizes, 0.5)
		t.Logf("seed %d: %d regions; by count median %d (all), %d (land without river, beach, shore); by area median %d; rivers %.1f%% of land",
			seed, len(areas), percentile(sizes, 0.5), landMedian, weighted, riverShare*100)
		if weighted < 150 || weighted > 1000 {
			t.Errorf("seed %d: area-weighted median biome region %d blocks across, want 150-1000", seed, weighted)
		}
		if riverShare >= 0.08 {
			t.Errorf("seed %d: rivers cover %.1f%% of land, want under 8%%", seed, riverShare*100)
		}
		if landMedian < landMedianFloor {
			t.Errorf("seed %d: by-count median of land regions without river, beach and shore is %d blocks across, want at least %d", seed, landMedian, landMedianFloor)
		}
	}
}
