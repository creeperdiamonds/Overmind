package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Placement comparisons (owner's ruling 28). Our carvers, trees, kelp and seagrass follow Java's
// algorithms, and Bedrock's placement is hidden in its program, so these figures are printed beside
// Bedrock's and never fail the run. The worldgen tests assert that our rates match Java's.
// docs/stage-2-known-gaps.md lists the gaps.

// Bedrock's figures, BEDROCK-NOTES section 9 (aggregate.json): cave air by band, as the percentage
// of blocks under a column's ground top minus 8, pooled over the 4 seeds.
var bedrockAir = []struct {
	lo, hi int
	pct    float64
}{{-54, -40, 6.06}, {-40, -20, 9.21}, {-20, 0, 9.81}, {0, 20, 7.46}, {20, 40, 7.21}, {40, 63, 5.96}, {63, 100, 7.52}, {100, 160, 5.11}}

// Trees per chunk by the chunk's centre biome, Bedrock names, over the 4 windows.
var bedrockTrees = map[string]float64{
	"jungle": 10.03, "roofed_forest": 7.44, "pale_garden": 6.5, "forest": 5.92, "taiga": 5.18, "birch_forest": 5.04,
	"mega_taiga": 4.72, "grove": 4.49, "flower_forest": 4.06, "birch_forest_mutated": 3.64, "mesa_plateau_stone": 2.57,
	"cherry_grove": 2.41, "redwood_taiga_mutated": 1.89, "jungle_edge": 1.43, "savanna_plateau": 1.1, "savanna": 1.02, "meadow": 0.95,
	"plains": 0.31, "sunflower_plains": 0.07, "river": 0.11, "desert": 0,
}

// Bedrock's trees per chunk over all 4 windows, and kelp and seagrass blocks per ocean chunk on seed 1,
// whose window is 76% ocean, deep ocean, cold ocean and deep cold ocean.
const bedrockTreesAll, bedrockKelp, bedrockSeagrass = 2.217, 189.0, 57.0

var oceanBiomes = []string{"ocean", "deep_ocean", "cold_ocean", "deep_cold_ocean"}

// treeBase is measure.py's LOG_BASE: the blocks a counted trunk stands on.
var treeBase = map[string]bool{}

func init() {
	for _, n := range strings.Fields(`dirt coarse_dirt grass_block podzol mycelium dirt_with_roots mud moss_block pale_moss_block
		sand red_sand snow gravel clay muddy_mangrove_roots`) {
		treeBase[n] = true
	}
}

// blockInfo is what the tallies need of a runtime ID.
type blockInfo struct {
	name           string
	air, log, kelp bool
	seagrass       bool
}

func blockInfos() []blockInfo {
	out := make([]blockInfo, world.DefaultBlockRegistry.BlockCount())
	for i := range out {
		name, _, ok := world.DefaultBlockRegistry.RuntimeIDToState(uint32(i))
		if !ok {
			continue
		}
		n := strings.TrimPrefix(name, "minecraft:")
		out[i] = blockInfo{name: n, air: n == "air", log: strings.HasSuffix(n, "_log"), kelp: n == "kelp", seagrass: n == "seagrass"}
	}
	return out
}

// tally holds one seed's placement figures, merged from the chunks.
type tally struct {
	air, below          []float64
	trees, chunks       map[string]float64
	allTrees, allChunks float64
	kelp, seagrass      float64
	oceanChunks         float64
}

func newTally() *tally {
	return &tally{air: make([]float64, len(bedrockAir)), below: make([]float64, len(bedrockAir)),
		trees: map[string]float64{}, chunks: map[string]float64{}}
}

func (t *tally) add(o *tally) {
	for i := range t.air {
		t.air[i] += o.air[i]
		t.below[i] += o.below[i]
	}
	for k, v := range o.trees {
		t.trees[k] += v
	}
	for k, v := range o.chunks {
		t.chunks[k] += v
	}
	t.allTrees += o.allTrees
	t.allChunks += o.allChunks
	t.kelp += o.kelp
	t.seagrass += o.seagrass
	t.oceanChunks += o.oceanChunks
}

// chunkTally measures one finished chunk. tops are its columns' ground tops, by x*16+z; centre is the
// biome at its middle.
func chunkTally(c *chunk.Chunk, info []blockInfo, tops *[256]int, centre string) *tally {
	t := newTally()
	var cols [16][16]bool
	ocean := slices.Contains(oceanBiomes, centre)
	for x := range 16 {
		for z := range 16 {
			top := tops[x*16+z]
			for i, b := range bedrockAir {
				for y := b.lo; y < min(b.hi, top-8); y++ {
					t.below[i]++
					if info[c.Block(uint8(x), int16(y), uint8(z), 0)].air {
						t.air[i]++
					}
				}
			}
			for y := int16(minY + 1); y <= maxY; y++ {
				in := info[c.Block(uint8(x), y, uint8(z), 0)]
				if in.log && treeBase[info[c.Block(uint8(x), y-1, uint8(z), 0)].name] {
					cols[x][z] = true
				}
				if ocean && y <= 63 {
					if in.kelp {
						t.kelp++
					}
					if in.seagrass {
						t.seagrass++
					}
				}
			}
		}
	}
	// Trunk columns touching at a side are one tree, as measure.py counts them.
	var seen [16][16]bool
	n := 0.0
	for x := range 16 {
		for z := range 16 {
			if !cols[x][z] || seen[x][z] {
				continue
			}
			n++
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
	t.trees[centre] += n
	t.chunks[centre]++
	t.allTrees, t.allChunks = n, 1
	if ocean {
		t.oceanChunks = 1
	}
	return t
}

// report prints the figures beside Bedrock's.
func (t *tally) report(seed int64) []string {
	var out []string
	var air []string
	for i, b := range bedrockAir {
		if t.below[i] == 0 {
			continue
		}
		air = append(air, fmt.Sprintf("Y %d..%d %.2f/%.2f", b.lo, b.hi, 100*t.air[i]/t.below[i], b.pct))
	}
	out = append(out, "cave air %, ours/Bedrock pooled: "+strings.Join(air, ", "))
	out = append(out, fmt.Sprintf("trees a chunk: %.2f over %0.f chunks (Bedrock all 4 windows %.3f)", t.allTrees/max(t.allChunks, 1), t.allChunks, bedrockTreesAll))
	names := make([]string, 0, len(bedrockTrees))
	for k := range bedrockTrees {
		if t.chunks[k] >= 10 {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	var tr []string
	for _, k := range names {
		tr = append(tr, fmt.Sprintf("%s %.2f/%.2f (%0.f)", k, t.trees[k]/t.chunks[k], bedrockTrees[k], t.chunks[k]))
	}
	if len(tr) > 0 {
		out = append(out, "trees by centre biome, ours/Bedrock (our chunks): "+strings.Join(tr, ", "))
	}
	if t.oceanChunks > 0 {
		note := ""
		if seed != 1 {
			note = " (Bedrock's figures are seed 1's)"
		}
		out = append(out, fmt.Sprintf("per ocean chunk over %0.f: kelp %.1f/%.0f, seagrass %.1f/%.0f%s", t.oceanChunks, t.kelp/t.oceanChunks, bedrockKelp, t.seagrass/t.oceanChunks, bedrockSeagrass, note))
	}
	return out
}
