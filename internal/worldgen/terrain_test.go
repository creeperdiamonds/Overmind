package worldgen

import (
	"math"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// Continentalness edges of the measured figures: under oceanBelow is ocean, at midBelow or over is
// far inland.
const (
	oceanBelow = -0.19
	midBelow   = 0.3
)

// highestSolid is the top of the terrain alone in a column, before caves and fluids: the blended
// sloped density by the same order as the real pass. Tests use it to tell terrain from caves.
func (g *Generator) highestSolid(x, z int) int {
	gridX, gridZ := x&^(cellXZ-1), z&^(cellXZ-1)
	fx, fz := float64(x-gridX)/4.0, float64(z-gridZ)/4.0
	var shapes [2][2]columnShape
	for i := range 2 {
		for j := range 2 {
			shapes[i][j] = g.shapeAt(gridX+i*cellXZ, gridZ+j*cellXZ)
		}
	}
	at := func(i, j, y int) float64 {
		if shapes[i][j].certainlyAir(y) {
			return -1
		}
		return slides(g.slopedAt(&shapes[i][j], gridX+i*cellXZ, y, gridZ+j*cellXZ), y)
	}
	for layer := columnNodes - 2; layer >= 0; layer-- {
		y0 := minY + layer*cellY
		var c [2][2][2]float64
		any := false
		for i := range 2 {
			for k := range 2 {
				for j := range 2 {
					c[i][k][j] = at(i, j, y0+k*cellY)
					any = any || c[i][k][j] > 0
				}
			}
		}
		if !any {
			continue
		}
		e := edges{lerp(c[0][0][0], c[1][0][0], fx), lerp(c[0][1][0], c[1][1][0], fx), lerp(c[0][0][1], c[1][0][1], fx), lerp(c[0][1][1], c[1][1][1], fx)}
		for dy := cellY - 1; dy >= 0; dy-- {
			if e.at(float64(dy)/8.0, fz) > 0 {
				return y0 + dy
			}
		}
	}
	return minY - 1
}

func TestPeaksAndValleysFormula(t *testing.T) {
	cases := map[float64]float64{0: -1, 1.0 / 3: 0, 2.0 / 3: 1, 1: 0, -2.0 / 3: 1}
	for weirdness, want := range cases {
		if got := peaksAndValleys(weirdness); math.Abs(got-want) > 1e-9 {
			t.Errorf("peaksAndValleys(%v) = %v, want %v", weirdness, got, want)
		}
	}
}

// Past its ends a spline carries on along the end slope, and with no slope it holds its end value.
func TestSplineEnds(t *testing.T) {
	s := onCoord(coordE).c(-1, 10).c(0, 20).cd(1, 40, 2).s
	for _, c := range []struct{ at, want float32 }{{-5, 10}, {-1, 10}, {0, 20}, {1, 40}, {2, 42}} {
		in := splineCoords{coordE: c.at}
		if got := s.eval(&in); got != c.want {
			t.Errorf("eval(%v) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestSplineNests(t *testing.T) {
	inner := onCoord(coordE).c(0, 100).c(1, 200).s
	s := onCoord(coordC).c(0, 0).n(1, inner).s
	in := splineCoords{coordC: 1, coordE: 1}
	if got := s.eval(&in); got != 200 {
		t.Fatalf("nested eval = %v, want 200", got)
	}
}

// Larger continentalness raises terrain, higher erosion flattens it and high peaks-and-valleys raises
// it, in the offset spline.
func TestOffsetSplineDirections(t *testing.T) {
	h := func(c, e, pv float32) float64 {
		in := splineCoords{coordC: c, coordE: e, coordPV: pv}
		return nominalGround(offsetFrom(&in))
	}
	if h(-0.8, 0, 0) >= h(-0.3, 0, 0) || h(-0.3, 0, 0) >= SeaLevel || h(0.5, 0, 0) <= h(0, 0, 0) {
		t.Error("height does not rise with continentalness across the sea level")
	}
	if h(0.5, -0.8, 0.8) <= h(0.5, 0.8, 0.8) {
		t.Error("high erosion does not flatten terrain")
	}
	if h(0.5, -0.3, 1) <= h(0.5, -0.3, -0.5) {
		t.Error("high peaks-and-valleys does not raise terrain")
	}
	if h(0, 0.3, -1) > SeaLevel {
		t.Error("valleys on low inland ground should reach the water")
	}
}

// A column with open air under solid ground can only come from the 3D density. The patch
// is found by searching for high ground, since the map of a seed changes with the generator.
func TestMountainsHaveOverhangs(t *testing.T) {
	g, b := New(1), newBlocks()
	var overhung, total, deepest int
	var fromX, fromZ int32
search:
	for x := -4096; x < 4096; x += 64 {
		for z := -4096; z < 4096; z += 64 {
			if g.highestSolid(x, z) > 150 {
				fromX, fromZ = int32(x>>4)-15, int32(z>>4)-15
				break search
			}
		}
	}
	for cx := fromX; cx < fromX+30; cx++ {
		for cz := fromZ; cz < fromZ+30; cz++ {
			c := generate(g, cx, cz)
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					total++
					top := topSolid(c, x, z)
					gap := 0
					for y := int16(top); y > SeaLevel; y-- {
						if c.Block(x, y, z, 0) == b.air {
							gap++
						}
					}
					if gap > 0 {
						overhung++
						deepest = max(deepest, gap)
					}
				}
			}
		}
	}
	share := float64(overhung) / float64(total)
	t.Logf("%d of %d columns overhang (%.1f%%), most air under the top %d", overhung, total, share*100, deepest)
	if overhung == 0 {
		t.Fatal("no overhangs in a mountain range")
	}
	// The bound was 25%, our own guess. The noise stage alone, which matches the game bit for bit, has
	// 27.2% in this patch, so that bound was wrong; carvers add the rest. With the game's carvers the
	// patch measures 36.2%; the bound was set to 40% and then reset from that figure.
	if share > 0.38 {
		t.Fatalf("%.1f%% of mountain columns overhang; the ground should stay mostly solid", share*100)
	}
}

// Climate values are not clamped: the golden fixtures hold temperature to -1.21 and continentalness
// to 1.21, so the old clamps at +-1 are gone. This bounds them where the noise itself bounds them.
func TestClimateValuesInRanges(t *testing.T) {
	g := New(8)
	for x := -20000; x < 20000; x += 97 {
		c := g.Climate(x, x*3-1000)
		for name, v := range map[string]float64{"temperature": c.Temperature, "humidity": c.Humidity, "erosion": c.Erosion, "continentalness": c.Continentalness, "weirdness": c.Weirdness} {
			if math.Abs(v) > 2 {
				t.Fatalf("%s %v outside the range the game's noises reach", name, v)
			}
		}
	}
}

// The game's weirdness passes 1 on 0.25 to 0.42% of columns (4 seeds, 8192 square, 32-block grid),
// and PV goes on falling there.
func TestWeirdnessIsNotClamped(t *testing.T) {
	var over, total int
	var largest float64
	for _, seed := range []int64{1, 2, 3, 4, 5, 6} {
		g := New(seed)
		for x := -4096; x <= 4096; x += 32 {
			for z := -4096; z <= 4096; z += 32 {
				w := math.Abs(g.Climate(x, z).Weirdness)
				largest = max(largest, w)
				total++
				if w > 1 {
					over++
				}
			}
		}
	}
	share := float64(over) / float64(total) * 100
	t.Logf("|weirdness| over 1 on %.3f%% of columns, largest %.3f", share, largest)
	if share < 0.15 || share > 0.6 {
		t.Errorf("|weirdness| over 1 on %.3f%% of columns, measured 0.25 to 0.42%%", share)
	}
	if largest > 1.6 {
		t.Errorf("largest |weirdness| %.3f, measured 1.33 in the game", largest)
	}
}

func TestNoiseIsDeterministicAndSeeded(t *testing.T) {
	make := func(seed int64) *normalNoise {
		r := xoroFromSeed(seed)
		return newNamedNoise(r.forkFactory(), "erosion")
	}
	a, b, c := make(1), make(1), make(2)
	var differs bool
	for i := range 200 {
		x, z := float64(i)*13.7-900, float64(i)*-9.1+40
		if a.at(x, 5, z) != b.at(x, 5, z) {
			t.Fatal("same seed gave different noise")
		}
		differs = differs || a.at(x, 5, z) != c.at(x, 5, z)
	}
	if !differs {
		t.Fatal("different seeds gave the same noise")
	}
}

func TestNoiseIsContinuous(t *testing.T) {
	r := xoroFromSeed(4)
	n := newNamedNoise(r.forkFactory(), "erosion")
	prev := n.at(-500, 0, 0)
	for x := -500.0; x < 500; x += 0.25 {
		v := n.at(x, 0, 0)
		if math.Abs(v-prev) > 0.2 {
			t.Fatalf("jump of %.3f at x=%.2f", v-prev, x)
		}
		prev = v
	}
}

// TestGroundBlocksFollowTheMeasure: GroundBlocks is the Bedrock study's measure.py rule, by Bedrock
// block name, so a height read with it compares with the reference worlds: the listed full blocks,
// every ore and terracotta, and nothing that grows, flows or lies on top.
func TestGroundBlocksFollowTheMeasure(t *testing.T) {
	ground := GroundBlocks()
	byName := map[string][]uint32{}
	for _, b := range world.Blocks() {
		name, _ := b.EncodeBlock()
		byName[strings.TrimPrefix(name, "minecraft:")] = append(byName[strings.TrimPrefix(name, "minecraft:")], world.BlockRuntimeID(b))
	}
	check := func(name string, want bool) {
		ids := byName[name]
		if len(ids) == 0 {
			t.Errorf("no block is named %s", name)
		}
		for _, id := range ids {
			if ground[id] != want {
				t.Errorf("%s: ground %v, the measure says %v", name, ground[id], want)
				return
			}
		}
	}
	for _, name := range []string{"stone", "grass_block", "powder_snow", "moss_block", "budding_amethyst", "magma",
		"coal_ore", "deepslate_diamond_ore", "blue_ice", "obsidian", "sculk", "infested_stone", "dirt_with_roots",
		"packed_ice", "snow", "orange_terracotta", "hardened_clay", "dripstone_block", "raw_iron_block", "sandstone"} {
		check(name, true)
	}
	for _, name := range []string{"air", "water", "lava", "oak_log", "oak_leaves", "short_grass", "snow_layer", "ice",
		"kelp", "seagrass", "moss_carpet", "glow_lichen", "pointed_dripstone", "cobblestone", "mossy_cobblestone"} {
		check(name, false)
	}
}
