package worldgen

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Java fidelity of the placement rates that seedcheck compares with Bedrock (owner's ruling 28):
// trees per chunk by the chunk's centre biome, kelp and seagrass per ocean chunk, and cave air by Y
// band, counted the same way on Java's finished chunks (the fixtures' features stage) and on ours,
// over the same chunks. The Bedrock figures are measured comparisons; these are asserted.

// rateTally is one world's counts.
type rateTally struct {
	trees, chunks         map[string]float64
	kelp, seagrass, ocean float64
	air, below            [8]float64
}

var rateBands = [8][2]int{{-54, -40}, {-40, -20}, {-20, 0}, {0, 20}, {20, 40}, {40, 63}, {63, 100}, {100, 160}}

// Blocks a counted trunk stands on, as the Bedrock study's measure counts, by Java name.
var rateTreeBase = func() map[uint16]bool {
	m := map[uint16]bool{}
	for _, n := range strings.Fields("dirt coarse_dirt grass_block podzol mycelium rooted_dirt mud moss_block pale_moss_block sand red_sand snow_block gravel clay muddy_mangrove_roots") {
		m[stateBlockIndex(n)] = true
	}
	return m
}()

var (
	rateKelp     = map[uint16]bool{stateBlockIndex("kelp"): true, stateBlockIndex("kelp_plant"): true}
	rateSeagrass = map[uint16]bool{stateBlockIndex("seagrass"): true, stateBlockIndex("tall_seagrass"): true}
)

// rateGround is the column top's block: solid ground, not a tree or a plant.
func rateGround(s bs) bool {
	return s.has(fSolid) && !s.tagged(tagLogs) && !s.tagged(tagLeaves) && !s.isAir()
}

func (r *rateTally) addChunk(cells []bs, centre string) {
	if r.trees == nil {
		r.trees, r.chunks = map[string]float64{}, map[string]float64{}
	}
	ocean := slices.Contains([]string{"ocean", "deep_ocean", "cold_ocean", "deep_cold_ocean"}, centre)
	var cols [16][16]bool
	for x := range 16 {
		for z := range 16 {
			top := minY - 1
			for y := maxY; y >= minY; y-- {
				if rateGround(cells[cellIndex(x, y, z)]) {
					top = y
					break
				}
			}
			for i, b := range rateBands {
				for y := b[0]; y < min(b[1], top-8); y++ {
					r.below[i]++
					if cells[cellIndex(x, y, z)].isAir() {
						r.air[i]++
					}
				}
			}
			for y := minY + 1; y <= maxY; y++ {
				s := cells[cellIndex(x, y, z)]
				if s.tagged(tagLogs) && rateTreeBase[cells[cellIndex(x, y-1, z)].block()] {
					cols[x][z] = true
				}
				if ocean && y < SeaLevel {
					if rateKelp[s.block()] {
						r.kelp++
					}
					if rateSeagrass[s.block()] {
						r.seagrass++
					}
				}
			}
		}
	}
	var seen [16][16]bool
	for x := range 16 {
		for z := range 16 {
			if !cols[x][z] || seen[x][z] {
				continue
			}
			r.trees[centre]++
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
	r.chunks[centre]++
	if ocean {
		r.ocean++
	}
}

// TestPlacementRatesMatchJava: on the same chunks, our trees per chunk by centre biome, kelp and
// seagrass per ocean chunk and cave air by band land on Java's. Each source's features replay Java's
// exactly or nearly (TestGoldenFeatureTraces); what differs is the order of overlapping sources,
// where two Java runs differ by 0.25 to 0.37% of blocks, and the features not yet written. So a rate
// may be off by 10% of Java's figure or 0.2 a chunk, whichever is more, and air by 0.2 points.
// Biomes with under 20 chunks are logged only.
func TestPlacementRatesMatchJava(t *testing.T) {
	dir := goldenFeaturesDir(t)
	var java, ours rateTally
	var mu sync.Mutex
	for _, seed := range goldenSeeds {
		g := NewWithProfile(seed, ProfileOverjava)
		g.SetCacheBytes(128 << 20)
		var pos [][2]int32
		for cx := goldenMinCX; cx <= goldenMaxCX; cx++ {
			for cz := goldenMinCZ; cz <= goldenMaxCZ; cz++ {
				if longTests() || (cx%2 == 0 && cz%2 == 0) {
					pos = append(pos, [2]int32{int32(cx), int32(cz)})
				}
			}
		}
		inTiles(pos, func(p [2]int32) {
			cx, cz := int(p[0]), int(p[1])
			want, err := readStage(chunkFile(dir, seed, cx, cz), "features")
			if err != nil || len(want) != chunkCells {
				t.Errorf("chunk %d,%d: %v", cx, cz, err)
				return
			}
			s := scratchPool.Get().(*chunkScratch)
			defer scratchPool.Put(s)
			got := g.finalCells(s, cx, cz)
			centre := g.BiomeAt(cx*16+8, maxY, cz*16+8).String()
			mu.Lock()
			defer mu.Unlock()
			java.addChunk(want, centre)
			ours.addChunk(got[:], centre)
		})
	}
	var b strings.Builder
	names := make([]string, 0, len(java.chunks))
	for k := range java.chunks {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		n := java.chunks[k]
		j, o := java.trees[k]/n, ours.trees[k]/n
		fmt.Fprintf(&b, "\n%-24s trees %6.2f, Java %6.2f over %4.0f chunks", k, o, j, n)
		if n >= 20 && math.Abs(o-j) > max(0.1*j, 0.2) {
			t.Errorf("%s: %.2f trees a chunk, Java %.2f", k, o, j)
		}
	}
	if java.ocean > 0 {
		jk, ok := java.kelp/java.ocean, ours.kelp/java.ocean
		js, os := java.seagrass/java.ocean, ours.seagrass/java.ocean
		fmt.Fprintf(&b, "\nper ocean chunk over %.0f: kelp %.1f, Java %.1f; seagrass %.1f, Java %.1f", java.ocean, ok, jk, os, js)
		if math.Abs(ok-jk) > max(0.1*jk, 0.2) || math.Abs(os-js) > max(0.1*js, 0.2) {
			t.Errorf("per ocean chunk: kelp %.1f (Java %.1f), seagrass %.1f (Java %.1f)", ok, jk, os, js)
		}
	} else {
		t.Error("no ocean chunks in the sample")
	}
	for i, band := range rateBands {
		if java.below[i] == 0 {
			continue
		}
		j, o := 100*java.air[i]/java.below[i], 100*ours.air[i]/ours.below[i]
		fmt.Fprintf(&b, "\nY %4d to %4d: air %.2f%%, Java %.2f%%", band[0], band[1], o, j)
		if math.Abs(o-j) > 0.2 {
			t.Errorf("Y %d to %d: air %.2f%%, Java %.2f%%", band[0], band[1], o, j)
		}
	}
	t.Log(b.String())
}
