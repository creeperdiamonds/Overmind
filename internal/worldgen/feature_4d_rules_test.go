package worldgen

import (
	"math"
	"testing"
)

// Rules of Task 4d's untraced features, checked against Java 26.1's source and data files rather
// than against a Java run, since no traced chunk holds these biomes.

// TestIcebergDrawsLikeJava replays the iceberg's shape draws in IcebergFeature's order: an ellipse
// draws its height above water with nextInt(6), a round iceberg with nextInt(15) and maybe a tall
// extra, never both. Both the values and the generator's state after must agree.
func TestIcebergDrawsLikeJava(t *testing.T) {
	ellipses, round := 0, 0
	for seed := range int64(4000) {
		var ours, java featRand
		ours.setSeed(seed)
		java.setSeed(seed)
		got := icebergShape(&ours)

		var want icebergDims
		want.snowy = java.nextDouble() > 0.7
		want.angle = java.nextDouble() * 2 * math.Pi
		want.ea = 11 - java.nextInt(5)
		want.ec = 3 + java.nextInt(3)
		want.ellipse = java.nextDouble() > 0.7
		if want.ellipse {
			want.over = java.nextInt(6) + 6
			ellipses++
		} else {
			want.over = java.nextInt(15) + 3
			round++
		}
		if !want.ellipse && java.nextDouble() > 0.9 {
			want.over += java.nextInt(19) + 7
		}
		want.under = min(want.over+java.nextInt(11), 18)
		want.width = min(want.over+java.nextInt(7)-java.nextInt(5), 11)

		if got != want {
			t.Fatalf("seed %d: shape %+v, Java draws %+v", seed, got, want)
		}
		if ours.x != java.x {
			t.Fatalf("seed %d: the generator is not where Java's is after the shape draws", seed)
		}
	}
	if ellipses == 0 || round == 0 {
		t.Fatalf("seeds gave %d ellipses and %d round icebergs, want both kinds", ellipses, round)
	}
}
