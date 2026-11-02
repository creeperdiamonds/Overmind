package worldgen

import (
	"math"
	"testing"
)

func TestPeaksAndValleysFormula(t *testing.T) {
	cases := map[float64]float64{0: -1, 1.0 / 3: 0, 2.0 / 3: 1, 1: 0, -2.0 / 3: 1}
	for weirdness, want := range cases {
		if got := peaksAndValleys(weirdness); math.Abs(got-want) > 1e-9 {
			t.Errorf("peaksAndValleys(%v) = %v, want %v", weirdness, got, want)
		}
	}
}

func TestSplineHitsKnotsAndClampsEnds(t *testing.T) {
	s := &spline{input: inputErosion, knots: []knot{{at: -1, value: 10}, {at: 0, value: 20}, {at: 1, value: 40}}}
	for _, c := range []struct{ at, want float64 }{{-5, 10}, {-1, 10}, {0, 20}, {-0.5, 15}, {1, 40}, {9, 40}} {
		if got := s.eval(splineInput{inputErosion: c.at}); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("eval(%v) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestSplineNests(t *testing.T) {
	inner := &spline{input: inputErosion, knots: []knot{{at: 0, value: 100}, {at: 1, value: 200}}}
	s := &spline{input: inputContinentalness, knots: []knot{{at: 0, value: 0}, {at: 1, child: inner}}}
	got := s.eval(splineInput{inputContinentalness: 1, inputErosion: 1})
	if got != 200 {
		t.Fatalf("nested eval = %v, want 200", got)
	}
}

// Larger continentalness raises terrain, higher erosion flattens it and high
// peaks-and-valleys raises it, in the height table from the study.
func TestHeightSplineDirections(t *testing.T) {
	h := func(c, e, pv float64) float64 {
		return heightSpline.eval(splineInput{inputContinentalness: c, inputErosion: e, inputPV: pv})
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
	if share > 0.25 {
		t.Fatalf("%.1f%% of mountain columns overhang; the ground should stay mostly solid", share*100)
	}
}

func TestClimateValuesInRanges(t *testing.T) {
	g := New(8)
	for x := -20000; x < 20000; x += 97 {
		c := g.Climate(x, x*3-1000)
		for name, v := range map[string]float64{"temperature": c.Temperature, "humidity": c.Humidity, "erosion": c.Erosion} {
			if v < -1 || v > 1 {
				t.Fatalf("%s %v outside -1..1", name, v)
			}
		}
		// Weirdness is not clamped; the largest measured is 1.33 in the game and 1.49 in ours.
		if math.Abs(c.Weirdness) > 1.6 {
			t.Fatalf("weirdness %v far outside the measured range", c.Weirdness)
		}
		if c.Continentalness < -1.2 || c.Continentalness > 1 {
			t.Fatalf("continentalness %v outside -1.2..1", c.Continentalness)
		}
	}
}

// The game's weirdness passes 1 on 0.25 to 0.42% of columns (4 seeds, 8192 square, 32-block grid),
// and PV goes on falling there. A clamp at 1 flattens those humps into plateaus.
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
		t.Errorf("largest |weirdness| %.3f, measured 1.33 in the game and 1.49 in ours", largest)
	}
}
