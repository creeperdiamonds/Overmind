package worldgen

import (
	"math"
	"testing"
)

func TestNoiseIsDeterministicAndSeeded(t *testing.T) {
	a, b, c := newFractal(1, 100, 100, 3), newFractal(1, 100, 100, 3), newFractal(2, 100, 100, 3)
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

// Band edges assume values spread across -1 to 1, so the normalising
// gain has to hold for every octave count the generator uses.
func TestNoiseSpreadFitsMinusOneToOne(t *testing.T) {
	for _, octaves := range []int{1, 3, 4, 6} {
		f := newFractal(99, 64, 64, octaves)
		const n = 200000
		var sum, squares float64
		var outside int
		for i := range n {
			v := f.at(float64(i)*7.3, float64(i%911)*3.1, float64(i)*-11.9)
			sum += v
			squares += v * v
			if math.Abs(v) > 1 {
				outside++
			}
		}
		mean := sum / n
		spread := math.Sqrt(squares/n - mean*mean)
		t.Logf("%d octaves: mean %.3f, spread %.3f, outside +-1 %.2f%%", octaves, mean, spread, 100*float64(outside)/n)
		if math.Abs(mean) > 0.05 || spread < 0.25 || spread > 0.42 {
			t.Errorf("%d octaves: mean %.3f, spread %.3f", octaves, mean, spread)
		}
		if float64(outside)/n > 0.02 {
			t.Errorf("%d octaves: %d of %d outside +-1", octaves, outside, n)
		}
	}
}

func TestNoiseIsContinuous(t *testing.T) {
	f := newFractal(4, 32, 32, 3)
	prev := f.at(-500, 0, 0)
	for x := -500.0; x < 500; x += 0.25 {
		v := f.at(x, 0, 0)
		if math.Abs(v-prev) > 0.2 {
			t.Fatalf("jump of %.3f at x=%.2f", v-prev, x)
		}
		prev = v
	}
}
