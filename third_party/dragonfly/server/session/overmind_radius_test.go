package session

import "testing"

// TestClampChunkRadius (Overmind fork): a client's radius is held between minChunkRadius and the
// server's maximum. A player's own chunk always ticks, and its blocks read the chunks beside it, so
// those must be loaded with a chunk to spare: a radius under 2 would let a tick load or generate one.
func TestClampChunkRadius(t *testing.T) {
	for _, c := range []struct{ asked, most, want int32 }{
		{-5, 6, 2}, {0, 6, 2}, {1, 6, 2}, {2, 6, 2}, {3, 6, 3}, {6, 6, 6}, {32, 6, 6},
	} {
		if got := clampChunkRadius(c.asked, c.most); got != c.want {
			t.Errorf("clampChunkRadius(%d, %d) = %d, want %d", c.asked, c.most, got, c.want)
		}
	}
}
