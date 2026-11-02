package worldgen

import (
	"math"
	"reflect"
	"testing"
)

// sampleSpread is the standard deviation of a noise over points far apart, so that every octave
// is sampled in many cells.
func sampleSpread(l *layered) float64 {
	const samples = 40000
	var sum, squares float64
	for k := range samples {
		h := posHash(5, k, 0, 0)
		s := l.at((unit(h)-0.5)*4e5, (unit(mix64(h))-0.5)*4e4, (unit(mix64(h+1))-0.5)*4e5)
		sum += s
		squares += s * s
	}
	mean := sum / samples
	return math.Sqrt(squares/samples - mean*mean)
}

// TestSpreadRatios measures spreadRatios: the five climate noises have spreads measured in the
// game (notes section 2); our own noises of the same octave lists, unscaled, are sampled for
// their natural spread, and the share between the two must match the table at that span.
func TestSpreadRatios(t *testing.T) {
	for _, c := range []struct {
		name  string
		first int
		amps  []float64
		span  int
		game  float64
	}{
		{"temperature", -10, []float64{1.5, 0, 1}, 3, 0.380},
		{"humidity", -8, []float64{1, 1}, 2, 0.252},
		{"continentalness", -9, []float64{1, 1, 2, 2, 2, 1, 1, 1, 1}, 9, 0.373},
		{"erosion", -9, []float64{1, 1, 0, 1, 1}, 5, 0.309},
		{"weirdness", -7, []float64{1, 2, 1}, 3, 0.353},
	} {
		natural := sampleSpread(newLayered(5, c.first, c.amps, naturalSpread(c.amps)))
		ratio := c.game / natural
		t.Logf("%-16s natural %.3f, game %.3f, ratio %.3f, table %.3f", c.name, natural, c.game, ratio, spreadRatio(c.span))
		if math.Abs(ratio-spreadRatio(c.span)) > 0.03 {
			t.Errorf("%s: measured ratio %.3f, table says %.3f", c.name, ratio, spreadRatio(c.span))
		}
	}
}

// TestEveryNoiseSpread samples every noise of the cave set. Each must measure a spread in the
// range the game's noises have (the five measured ones run 0.25 to 0.38; entrance, whose
// amplitudes are small, is lower).
func TestEveryNoiseSpread(t *testing.T) {
	n := newCaveNoises(7)
	v := reflect.ValueOf(&n).Elem()
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		spread := sampleSpread((*layered)(v.Field(i).UnsafePointer()))
		t.Logf("%-13s spread %.3f", name, spread)
		if spread < 0.12 || spread > 0.45 {
			t.Errorf("%s measures spread %.3f", name, spread)
		}
	}
}
