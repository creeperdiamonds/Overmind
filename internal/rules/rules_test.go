package rules

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

func TestJavaBlockReach(t *testing.T) {
	eye := mgl64.Vec3{0.5, 1.62, 0.5}
	cases := []struct {
		pos      cube.Pos
		creative bool
		want     bool
	}{
		{cube.Pos{0, -1, 0}, false, true},  // under the feet
		{cube.Pos{5, 1, 0}, false, true},   // 4.5 to its near face
		{cube.Pos{6, 1, 0}, false, true},   // 5.5: just inside 4.5 and the allowance
		{cube.Pos{6, 2, 0}, false, false},  // 5.51
		{cube.Pos{6, 2, 0}, true, true},    // creative reaches 5 and the allowance
		{cube.Pos{7, 1, 0}, true, false},   // 6.5
		{cube.Pos{-7, 1, 0}, false, false}, // the other way: 6.5 to its near face
	}
	for _, c := range cases {
		if got := (Java{}).CanReachBlock(eye, c.pos, c.creative); got != c.want {
			t.Errorf("Java reach to %v (creative %v) = %v, want %v", c.pos, c.creative, got, c.want)
		}
	}
	if !(Bedrock{}).CanReachBlock(eye, cube.Pos{7, 1, 0}, false) {
		t.Error("Bedrock rules must leave the reach to Dragonfly")
	}
}

func TestDistanceToBox(t *testing.T) {
	b := cube.Box(0, 0, 0, 1, 1, 1)
	for _, c := range []struct {
		p    mgl64.Vec3
		want float64
	}{
		{mgl64.Vec3{0.5, 0.5, 0.5}, 0},
		{mgl64.Vec3{3, 0.5, 0.5}, 2},
		{mgl64.Vec3{4, 5, 0.5}, 5},
	} {
		if got := distanceToBox(c.p, b); got != c.want {
			t.Errorf("distance from %v = %v, want %v", c.p, got, c.want)
		}
	}
}
