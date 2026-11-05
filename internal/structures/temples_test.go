package structures

import (
	"path/filepath"
	"strings"
	"testing"
)

// templeScore scores a scattered piece's box against a structure window, chunk by chunk, leaving out
// vegetation (vines, leaves) that decoration grows into the piece.
func templeScore(t *testing.T, file string, seed int64, chunks [][2]int, b box) {
	w := loadWorld(t, filepath.Join(goldenDir(t), "whole", "structures", file))
	g, _ := worldFor(t, seed)
	var same, total, veg int
	conf := map[[2]string]int{}
	for _, c := range chunks {
		col := generate(g, c[0], c[1])
		sm, tt, cf := boxScore(w, col.Chunk, c[0], c[1], []box{b})
		same, total = same+sm, total+tt
		for k, v := range cf {
			conf[k] += v
			if strings.Contains(k[0], "vine") || strings.Contains(k[0], "leaves") || strings.Contains(k[1], "leaves") {
				veg += v
			}
		}
	}
	t.Logf("%s: %d/%d = %.2f%% same in the piece box (%d vegetation cells differ); %s", file, same, total,
		100*float64(same)/float64(max(total, 1)), veg, topConfusions(conf, 12))
}

// TestTemplesMatchBDS: seed 11's desert pyramid (start (8,-16)) and seed 31's jungle temple (start (2,12)),
// as P13.3 / P13.4 describe them (the study's harness: 99.95% and 97.31%).
func TestTemplesMatchBDS(t *testing.T) {
	templeScore(t, "desert_pyramid_11.bin.gz", 11, [][2]int{{8, -16}, {9, -16}, {8, -15}, {9, -15}}, box{128, 90, -256, 148, 130, -236})
	templeScore(t, "jungle_temple_31.bin.gz", 31, [][2]int{{2, 12}}, box{32, 60, 192, 43, 90, 206})
}
