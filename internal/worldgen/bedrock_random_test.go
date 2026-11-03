package worldgen

import "testing"

// TestBedrockMTIsTextbook checks the Mersenne Twister against its published outputs (MT19937's
// reference: seed 5489 gives 3499211612 first, and the 10000th output is 4123659995).
func TestBedrockMTIsTextbook(t *testing.T) {
	r := newBMT(5489)
	if v := r.u32(); v != 3499211612 {
		t.Fatalf("first output %d, want 3499211612", v)
	}
	for range 9998 {
		r.u32()
	}
	if v := r.u32(); v != 4123659995 {
		t.Fatalf("10000th output %d, want 4123659995", v)
	}
}

// TestBedrockFloorIsPerChunk checks the floor rule of NOTES-bedrock-exact R1: 2 to 5 layers in every
// column, no holes, independent of the seed, and chunk (0, 0) drawn from MT(0).
func TestBedrockFloorIsPerChunk(t *testing.T) {
	g1, g2 := NewWithProfile(1, ProfileBedrock), NewWithProfile(12345, ProfileBedrock)
	s := newScratch()
	var a, b baseChunk
	for _, p := range [][2]int{{0, 0}, {3, -7}} {
		g1.buildBase(&a, s, p[0], p[1], stageNoise, 0, 15, 0, 15)
		g2.buildBase(&b, s, p[0], p[1], stageNoise, 0, 15, 0, 15)
		r := newBMT(uint32(int32(p[0])*floorMulX + int32(p[1])*floorMulZ))
		for x := range 16 {
			for z := range 16 {
				n := int(r.u32()%4) + 2
				for y := minY; y < minY+6; y++ {
					want := y < minY+n
					if got := a.b[cellIndex(x, y, z)] == sBedrock; got != want {
						t.Fatalf("chunk %v column %d,%d y %d: bedrock %v, want %v", p, x, z, y, got, want)
					}
					if (a.b[cellIndex(x, y, z)] == sBedrock) != (b.b[cellIndex(x, y, z)] == sBedrock) {
						t.Fatalf("chunk %v: the floor depends on the seed", p)
					}
				}
			}
		}
	}
}
