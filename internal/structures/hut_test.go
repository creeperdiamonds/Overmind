package structures

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestWitchHutMatchesBDS scores seed 5's hut (start (-11, -55), P11.1) block for block in its box and the
// cells under it, against the window export.
func TestWitchHutMatchesBDS(t *testing.T) {
	w := loadWorld(t, filepath.Join(goldenDir(t), "whole", "structures", "witch_hut_5.bin.gz"))
	g, _ := worldFor(t, 5)
	col := generate(g, -11, -55)
	b := box{-176, 65, -880, -170, 74, -872}
	same, total, conf := boxScore(w, col.Chunk, -11, -55, []box{b})
	// The trees, vines and mushrooms around the hut are decoration, not the hut's.
	veg := 0
	for k, n := range conf {
		for _, v := range []string{"leaves", "vine", "mushroom"} {
			if strings.Contains(k[0], v) || strings.Contains(k[1], v) {
				veg += n
				break
			}
		}
	}
	t.Logf("hut blocks, vegetation left out: %d/%d differ", total-same-veg, total-veg)
	same += veg
	t.Logf("hut box and the two layers under it: %d/%d = %.2f%% same; confusions %s", same, total, 100*float64(same)/float64(max(total, 1)), topConfusions(conf, 10))
	if same*100 < total*95 {
		t.Errorf("hut: %d of %d blocks differ", total-same, total)
	}
}
