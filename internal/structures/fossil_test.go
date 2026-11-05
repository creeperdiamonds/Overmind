package structures

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestFossilsMatchBDS: seed 100's five fossils (NOTES-portals-fossils-ruins F1: two coal ones, three
// deepslate diamond ones; 388 of 388 template cells in the study), against the measured world seed100:
// every bone block and every fossil ore cell (an ore touching a bone block) of the fossils' chunks.
func TestFossilsMatchBDS(t *testing.T) {
	g, _ := worldFor(t, 100)
	fossils := [][3]int{{-537, 47, 402}, {-540, 48, 416}, {-543, -45, 321}, {-524, -45, 259}, {-464, -54, 404}}
	chunks := map[[2]int]bool{}
	for _, f := range fossils {
		chunks[[2]int{f[0] >> 4, f[2] >> 4}] = true
	}
	x0, x1, z0, z1 := 1<<30, -1<<30, 1<<30, -1<<30
	for c := range chunks {
		x0, x1, z0, z1 = min(x0, c[0]), max(x1, c[0]), min(z0, c[1]), max(z1, c[1])
	}
	w := loadBDSRunsIn(t, "seed100", []string{""}, world.Overworld, true, x0, x1, z0, z1)
	fossilCell := func(name func(x, y, z int) string, x, y, z int) string {
		n := name(x, y, z)
		switch n {
		case "bone_block":
			return n
		case "coal_ore", "deepslate_diamond_ore":
			for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
				if name(x+d[0], y+d[1], z+d[2]) == "bone_block" {
					return n
				}
			}
		}
		return ""
	}
	both, theirs, ours := 0, 0, 0
	for c := range chunks {
		bc := w.chunks[c]
		if bc == nil {
			t.Fatalf("chunk %v is not in the world", c)
		}
		col := generate(g, c[0], c[1])
		real := func(x, y, z int) string {
			if x>>4 != c[0] || z>>4 != c[1] || y < minY || y >= maxY {
				return ""
			}
			return w.names[bc.ids[cellOf(x, y, z)]]
		}
		mine := func(x, y, z int) string {
			if x>>4 != c[0] || z>>4 != c[1] || y < minY || y >= maxY {
				return ""
			}
			return nameAt(col.Chunk, x, y, z)
		}
		for y := minY; y < maxY; y++ {
			for z := c[1] * 16; z < c[1]*16+16; z++ {
				for x := c[0] * 16; x < c[0]*16+16; x++ {
					r, m := fossilCell(real, x, y, z), fossilCell(mine, x, y, z)
					switch {
					case r != "" && r == m:
						both++
					case r != "":
						theirs++
					case m != "":
						ours++
					}
				}
			}
		}
	}
	t.Logf("seed 100 fossils: %d fossil cells exact, %d only BDS's, %d only ours (study: 388 of 388)", both, theirs, ours)
	if both == 0 || theirs+ours > both/20 {
		t.Errorf("fossil cells: %d exact, %d only BDS's, %d only ours", both, theirs, ours)
	}
}
