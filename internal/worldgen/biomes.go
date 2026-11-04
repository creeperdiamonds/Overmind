package worldgen

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
)

// def is a Dragonfly biome.
type def struct {
	world.Biome
}

// Every biome the Java 26.1 Overworld generates has a Dragonfly biome. Two have other
// names there: Java's sparse jungle is JungleEdge and wooded badlands is
// WoodedBadlandsPlateau.
var (
	plains          = def{biome.Plains{}}
	sunflower       = def{biome.SunflowerPlains{}}
	forest          = def{biome.Forest{}}
	flowerForest    = def{biome.FlowerForest{}}
	birchForest     = def{biome.BirchForest{}}
	oldBirch        = def{biome.OldGrowthBirchForest{}}
	darkForest      = def{biome.DarkForest{}}
	paleGarden      = def{biome.PaleGarden{}}
	taiga           = def{biome.Taiga{}}
	oldSpruce       = def{biome.OldGrowthSpruceTaiga{}}
	oldPine         = def{biome.OldGrowthPineTaiga{}}
	snowyTaiga      = def{biome.SnowyTaiga{}}
	snowyPlains     = def{biome.SnowyPlains{}}
	iceSpikes       = def{biome.IceSpikes{}}
	desert          = def{biome.Desert{}}
	savanna         = def{biome.Savanna{}}
	savannaPlateau  = def{biome.SavannaPlateau{}}
	windSavanna     = def{biome.WindsweptSavanna{}}
	jungle          = def{biome.Jungle{}}
	sparseJungle    = def{biome.JungleEdge{}}
	bambooJungle    = def{biome.BambooJungle{}}
	swamp           = def{biome.Swamp{}}
	mangrove        = def{biome.MangroveSwamp{}}
	badlands        = def{biome.Badlands{}}
	erodedBadlands  = def{biome.ErodedBadlands{}}
	woodedBadlands  = def{biome.WoodedBadlandsPlateau{}}
	meadow          = def{biome.Meadow{}}
	cherryGrove     = def{biome.CherryGrove{}}
	grove           = def{biome.Grove{}}
	snowySlopes     = def{biome.SnowySlopes{}}
	jaggedPeaks     = def{biome.JaggedPeaks{}}
	frozenPeaks     = def{biome.FrozenPeaks{}}
	stonyPeaks      = def{biome.StonyPeaks{}}
	windsweptHills  = def{biome.WindsweptHills{}}
	windsweptForest = def{biome.WindsweptForest{}}
	gravellyHills   = def{biome.WindsweptGravellyHills{}}
	beach           = def{biome.Beach{}}
	snowyBeach      = def{biome.SnowyBeach{}}
	stonyShore      = def{biome.StonyShore{}}
	river           = def{biome.River{}}
	frozenRiver     = def{biome.FrozenRiver{}}
	mushroom        = def{biome.MushroomFields{}}

	dripstoneCaves = def{biome.DripstoneCaves{}}
	lushCaves      = def{biome.LushCaves{}}
	deepDark       = def{biome.DeepDark{}}
	sulfurCaves    = def{biome.SulfurCaves{}} // Bedrock's; no Java row names it.
	dappledForest  = def{biome.DappledForest{}} // Bedrock's variant of some plains rows.

	netherWastes   = def{biome.NetherWastes{}}
	soulSandValley = def{biome.SoulSandValley{}}
	crimsonForest  = def{biome.CrimsonForest{}}
	warpedForest   = def{biome.WarpedForest{}}
	basaltDeltas   = def{biome.BasaltDeltas{}}

	frozenOcean       = def{biome.FrozenOcean{}}
	coldOcean         = def{biome.ColdOcean{}}
	ocean             = def{biome.Ocean{}}
	lukewarmOcean     = def{biome.LukewarmOcean{}}
	warmOcean         = def{biome.WarmOcean{}}
	deepFrozenOcean   = def{biome.DeepFrozenOcean{}}
	deepColdOcean     = def{biome.DeepColdOcean{}}
	deepOcean         = def{biome.DeepOcean{}}
	deepLukewarmOcean = def{biome.DeepLukewarmOcean{}}
)

// biomeDefs maps the parameter table's names to our biomes. The table has no deep warm ocean, though
// Dragonfly lists one.
var biomeDefs = map[string]def{
	"plains": plains, "sunflower_plains": sunflower, "forest": forest, "flower_forest": flowerForest,
	"birch_forest": birchForest, "old_growth_birch_forest": oldBirch, "dark_forest": darkForest, "pale_garden": paleGarden,
	"taiga": taiga, "old_growth_spruce_taiga": oldSpruce, "old_growth_pine_taiga": oldPine, "snowy_taiga": snowyTaiga,
	"snowy_plains": snowyPlains, "ice_spikes": iceSpikes, "desert": desert, "savanna": savanna,
	"savanna_plateau": savannaPlateau, "windswept_savanna": windSavanna, "jungle": jungle, "sparse_jungle": sparseJungle,
	"bamboo_jungle": bambooJungle, "swamp": swamp, "mangrove_swamp": mangrove, "badlands": badlands,
	"eroded_badlands": erodedBadlands, "wooded_badlands": woodedBadlands, "meadow": meadow, "cherry_grove": cherryGrove,
	"grove": grove, "snowy_slopes": snowySlopes, "jagged_peaks": jaggedPeaks, "frozen_peaks": frozenPeaks,
	"stony_peaks": stonyPeaks, "windswept_hills": windsweptHills, "windswept_forest": windsweptForest,
	"windswept_gravelly_hills": gravellyHills, "beach": beach, "snowy_beach": snowyBeach, "stony_shore": stonyShore,
	"river": river, "frozen_river": frozenRiver, "mushroom_fields": mushroom, "dripstone_caves": dripstoneCaves,
	"lush_caves": lushCaves, "deep_dark": deepDark, "sulfur_caves": sulfurCaves, "dappled_forest": dappledForest, "nether_wastes": netherWastes, "soul_sand_valley": soulSandValley,
	"crimson_forest": crimsonForest, "warped_forest": warpedForest, "basalt_deltas": basaltDeltas, "frozen_ocean": frozenOcean, "cold_ocean": coldOcean, "ocean": ocean,
	"lukewarm_ocean": lukewarmOcean, "warm_ocean": warmOcean, "deep_frozen_ocean": deepFrozenOcean,
	"deep_cold_ocean": deepColdOcean, "deep_ocean": deepOcean, "deep_lukewarm_ocean": deepLukewarmOcean,
}

// Biome lookup follows Mojang's algorithm, per the owner's decision: the nearest box of a parameter
// table, found with an R-tree built in the game's order (NOTES-exact-seed.md section 12). The table is
// biome_table.go.

// biomeTarget is a climate point, quantized. The seventh dimension of the game's tree, a box's
// offset, is 0 on every row of the table and for every target.
type biomeTarget [6]int64

func quantize(v float64) int64 { return int64(float32(float32(v) * 10000.0)) }

// biomeDims counts the offset dimension: its center is always 0, yet the build's cyclic sort keys and
// its choice among dimensions still go through it.
const biomeDims = 7

// biomeNode is a leaf (one table row) or a group of nodes. Bounds fit 16 bits, which keeps the
// tree small, since every generator shares it.
type biomeNode struct {
	bounds [6][2]int16
	kids   []*biomeNode
	row    int32
	id     uint8 // The leaf's biome, an index into biomeTableNames.
}

func (n *biomeNode) center(d int) int64 {
	if d == 6 {
		return 0
	}
	return (int64(n.bounds[d][0]) + int64(n.bounds[d][1])) / 2
}

func (n *biomeNode) def() def { return biomeRowDefs[n.id] }

// biomeRowDefs gives a def by index into biomeTableNames.
var biomeRowDefs = func() (out [len(biomeTableNames)]def) {
	for i, name := range biomeTableNames {
		out[i] = biomeDefs[name]
	}
	return out
}()

// dist is the squared distance from a target to a node's box. It stops early once past limit, since
// its only use is to compare with limit.
func (n *biomeNode) dist(t *biomeTarget, limit int64) int64 {
	var sum int64
	for d := range t {
		v, b := t[d], &n.bounds[d]
		var e int64
		if hi := int64(b[1]); v > hi {
			e = v - hi
		} else if lo := int64(b[0]); v < lo {
			e = lo - v
		}
		sum += e * e
		if sum >= limit {
			return sum
		}
	}
	return sum
}

// search is the game's descent: a child is opened only if it could beat the best so far, so at equal
// distance the first leaf reached stays.
func (n *biomeNode) search(t *biomeTarget, best *biomeNode, bestD int64) (*biomeNode, int64) {
	for _, k := range n.kids {
		d := k.dist(t, bestD)
		if bestD <= d {
			continue
		}
		if k.kids == nil {
			best, bestD = k, d
		} else {
			best, bestD = k.search(t, best, bestD)
		}
	}
	return best, bestD
}

func unionBounds(list []*biomeNode) (b [6][2]int16) {
	b = list[0].bounds
	for _, n := range list[1:] {
		for d := range b {
			b[d][0], b[d][1] = min(b[d][0], n.bounds[d][0]), max(b[d][1], n.bounds[d][1])
		}
	}
	return b
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// buildBiomeTree builds the tree in the game's order. Sorts are stable, and one choice of dimension
// starts from the order the last one left.
func buildBiomeTree(list []*biomeNode) *biomeNode {
	if len(list) == 1 {
		return list[0]
	}
	if len(list) <= 6 {
		sum := func(n *biomeNode) int64 {
			var s int64
			for d := range biomeDims {
				s += abs64(n.center(d))
			}
			return s
		}
		slices.SortStableFunc(list, func(a, b *biomeNode) int { return cmpInt64(sum(a), sum(b)) })
		return &biomeNode{bounds: unionBounds(list), kids: list}
	}
	size := int(math.Pow(6, math.Floor(math.Log(float64(len(list))-0.01)/math.Log(6))))
	var bestBuckets []*biomeNode
	bestDim := 0
	bestCost := int64(math.MaxInt64)
	for d := range biomeDims {
		slices.SortStableFunc(list, func(a, b *biomeNode) int {
			for i := range biomeDims {
				if c := cmpInt64(a.center((d+i)%biomeDims), b.center((d+i)%biomeDims)); c != 0 {
					return c
				}
			}
			return 0
		})
		var buckets []*biomeNode
		var cost int64
		for at := 0; at < len(list); at += size {
			kids := slices.Clone(list[at:min(at+size, len(list))])
			b := &biomeNode{bounds: unionBounds(kids), kids: kids}
			buckets = append(buckets, b)
			for e := range b.bounds {
				cost += abs64(int64(b.bounds[e][1]) - int64(b.bounds[e][0]))
			}
		}
		if bestCost > cost {
			bestCost, bestDim, bestBuckets = cost, d, buckets
		}
	}
	slices.SortStableFunc(bestBuckets, func(a, b *biomeNode) int {
		for i := range biomeDims {
			e := (bestDim + i) % biomeDims
			if c := cmpInt64(abs64(a.center(e)), abs64(b.center(e))); c != 0 {
				return c
			}
		}
		return 0
	})
	kids := make([]*biomeNode, len(bestBuckets))
	for i, b := range bestBuckets {
		kids[i] = buildBiomeTree(b.kids)
	}
	return &biomeNode{bounds: unionBounds(kids), kids: kids}
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// biomeTree is shared by every generator: it depends on no seed. It is built the first time it is
// needed, and a process holds one however many worlds it has.
var biomeTree = sync.OnceValue(func() *biomeNode { return buildBiomeTree(biomeLeaves()) })

// biomeLeaves is a leaf for each row of the table, in its order.
func biomeLeaves() []*biomeNode {
	list := make([]*biomeNode, len(biomeRows))
	for i, row := range biomeRows {
		n := &biomeNode{row: int32(i), id: row[0]}
		for d := range n.bounds {
			n.bounds[d] = biomeIntervals[row[d+1]]
		}
		list[i] = n
	}
	return list
}

// nearestBiome finds a target's biome from scratch, with no remembered result. The game remembers a
// thread's last leaf, which can change the answer at an exact tie (about 0.05% of lookups); a world
// here must not depend on which worker asked or what it asked last, so this is always the cold search.
func nearestBiome(tree *biomeNode, t *biomeTarget) *biomeNode {
	n, _ := tree.search(t, nil, math.MaxInt64)
	return n
}

// nearestBiomeFrom is nearestBiome with a leaf known to be close, which prunes most of the tree. The
// answer is the same: the search's answer is the first nearest leaf in tree order, and a limit just
// over a leaf's distance can never prune a subtree holding a nearest leaf.
func nearestBiomeFrom(tree *biomeNode, t *biomeTarget, near *biomeNode) *biomeNode {
	n, _ := tree.search(t, nil, near.dist(t, math.MaxInt64)+1)
	return n
}

// biomeColumn is one quart column's climate, from which any height's biome follows.
type biomeColumn struct {
	target  biomeTarget // The depth entry is filled in per lookup.
	offset  float64
	ground  float64 // Nominal ground Y, from the offset spline.
	surface def     // The biome over the ground, which surface rules and decoration read.
	sky     *biomeNode
	tree    *biomeNode // The generator's table.
}

const biomeTargetDepth = 4

// biomeColumnAt works out the column of the quart holding a block.
func (g *Generator) biomeColumnAt(x, z int) biomeColumn {
	bx, bz := (x>>2)*4, (z>>2)*4
	cl := g.Climate(bx, bz)
	in := coordsOf(cl.Continentalness, cl.Erosion, cl.Weirdness)
	c := biomeColumn{offset: offsetFrom(&in), tree: g.biomeTree}
	c.ground = nominalGround(c.offset)
	c.target = biomeTarget{quantize(cl.Temperature), quantize(cl.Humidity), quantize(cl.Continentalness), quantize(cl.Erosion), 0, quantize(cl.Weirdness)}
	t := c.target
	t[biomeTargetDepth] = quantize(depthAt((maxY>>2)*4, c.offset))
	c.sky = nearestBiome(c.tree, &t)
	c.surface = c.sky.def()
	return c
}

// at returns the biome at a height. Depth is taken at the quart's own Y.
func (c *biomeColumn) at(y int) def {
	t := c.target
	t[biomeTargetDepth] = quantize(depthAt((y>>2)*4, c.offset))
	return nearestBiomeFrom(c.tree, &t, c.sky).def()
}

// BiomeAt returns the biome at a block, by its 4 x 4 x 4 quart, as chunks store it.
func (g *Generator) BiomeAt(x, y, z int) world.Biome {
	col := g.biomeColumnAt(x, z)
	if y>>2 == maxY>>2 {
		return col.surface.Biome
	}
	return col.at(y).Biome
}

// biomeZoomSeed is the world seed's hash as the game's block-level lookup uses it.
func biomeZoomSeed(seed int64) int64 {
	var in [8]byte
	binary.LittleEndian.PutUint64(in[:], uint64(seed))
	d := sha256.Sum256(in[:])
	return int64(binary.LittleEndian.Uint64(d[:8]))
}

func zoomLCG(r, c int64) int64 { return r*(r*6364136223846793005+1442695040888963407) + c }

func zoomFiddle(r int64) float64 {
	m := (r >> 24) % 1024
	if m < 0 {
		m += 1024
	}
	return float64((float64(m)/1024.0 - 0.5) * 0.9)
}

// BlockBiomeAt is the game's block-level biome: a jittered choice among the 8 quarts around a block.
// Chunks store quarts, and surface rules read quarts, so this is for callers that want the game's
// answer for one block.
func (g *Generator) BlockBiomeAt(x, y, z int) world.Biome {
	qx, qy, qz := g.zoomQuart(x, y, z)
	col := g.biomeColumnAt(qx*4, qz*4)
	return col.at(qy * 4).Biome
}

// zoomQuart picks the quart a block's biome comes from.
func (g *Generator) zoomQuart(x, y, z int) (qx, qy, qz int) {
	ax, ay, az := x-2, y-2, z-2
	px, py, pz := ax>>2, ay>>2, az>>2
	fx, fy, fz := float64(ax&3)/4.0, float64(ay&3)/4.0, float64(az&3)/4.0
	best := -1
	bestV := math.Inf(1)
	for i := range 8 {
		b2, b1, b0 := (i>>2)&1, (i>>1)&1, i&1
		cx, cy, cz := int64(px+b2), int64(py+b1), int64(pz+b0)
		dx, dy, dz := fx-float64(b2), fy-float64(b1), fz-float64(b0)
		r := g.zoomSeed
		for _, c := range [6]int64{cx, cy, cz, cx, cy, cz} {
			r = zoomLCG(r, c)
		}
		jx := zoomFiddle(r)
		r = zoomLCG(r, g.zoomSeed)
		jy := zoomFiddle(r)
		r = zoomLCG(r, g.zoomSeed)
		jz := zoomFiddle(r)
		sz, sy, sx := dz+jz, dy+jy, dx+jx
		v := float64(sz*sz) + float64(sy*sy) + float64(sx*sx)
		if v < bestV {
			best, bestV = i, v
		}
	}
	return px + (best>>2)&1, py + (best>>1)&1, pz + best&1
}
