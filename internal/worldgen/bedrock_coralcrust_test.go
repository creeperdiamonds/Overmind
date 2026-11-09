package worldgen

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// crustFamily groups the blocks the coral crust places, colour left out (the templates' dead coral
// colour changes between BDS runs, NOTES-leftovers L12.4).
func crustFamily(n string) string {
	switch {
	case strings.HasPrefix(n, "dead_") && strings.HasSuffix(n, "_coral_block"):
		return "dead coral block"
	case strings.HasSuffix(n, "_coral_block"):
		return "coral block"
	case strings.HasSuffix(n, "_coral_wall_fan"):
		return "coral wall fan"
	case strings.HasSuffix(n, "_coral_fan"):
		return "coral fan"
	}
	return ""
}

// TestCoralCrustMatchesBDS scores the coral crust (NOTES-leftovers L12) on the warm_ocean_37 window,
// without and with it: the window's stable cells, and over every cell (the unstable ones too: the dead
// coral's colour is what moves between BDS runs) the placement of each crust family with its colour
// left out (BDS's count, ours, both).
func TestCoralCrustMatchesBDS(t *testing.T) {
	bd := testBedrockData(t)
	w := loadBDSFile(t, filepath.Join(goldenBedrockDir(t), "whole", "windows", "warm_ocean_37.bin.gz"))
	defer bdsWorlds.Delete(filepath.Join(goldenBedrockDir(t), "whole", "windows", "warm_ocean_37.bin.gz"))
	gens := [2]*Generator{NewWithData(w.seed, ProfileBedrock, dataWithout(t, bd, "minecraft:coral_crust_feature")),
		NewWithData(w.seed, ProfileBedrock, bd)}
	type tally struct{ bds, ours, both int }
	fams := []string{"dead coral block", "coral block", "coral fan", "coral wall fan"}
	var per [2]map[string]*tally
	for i := range per {
		per[i] = map[string]*tally{}
		for _, f := range fams {
			per[i][f] = &tally{}
		}
	}
	var same [2]int
	var cells int
	names := blockNames()
	type traced struct {
		cx, cz int
		p      crustPiece
		cells  []pos
	}
	var pieces []traced
	seen := map[[5]int]bool{}
	crustTrace = func(cx, cz int, p crustPiece, c []pos) {
		k := [5]int{p.corner.x, p.corner.y, p.corner.z, p.rot, len(p.t.blocks)}
		if !seen[k] {
			seen[k] = true
			pieces = append(pieces, traced{cx, cz, p, c})
		}
	}
	defer func() { crustTrace = nil }()
	for k, c := range w.chunks {
		for i, g := range gens {
			ch := generate(g, int32(k[0]), int32(k[1]))
			for y := minY; y < maxY; y++ { // Y 319 holds the study's loading marker.
				for z := range 16 {
					for x := range 16 {
						cell := cellIndex(x, y, z)
						ours, real := names[ch.Block(uint8(x), int16(y), uint8(z), 0)].name, w.names[c.ids[cell]]
						if !c.masked(cell) {
							if i == 0 {
								cells++
							}
							if ours == real {
								same[i]++
							}
						}
						fo, fr := crustFamily(ours), crustFamily(real)
						if fr != "" {
							per[i][fr].bds++
						}
						if fo != "" {
							per[i][fo].ours++
						}
						if fo != "" && fo == fr {
							per[i][fo].both++
						}
					}
				}
			}
		}
	}
	hist := map[string]int{}
	for _, tp := range pieces {
		n, ok := 0, 0
		for _, q := range tp.cells {
			c := w.chunks[[2]int{q.x >> 4, q.z >> 4}]
			if c == nil {
				continue
			}
			n++
			if crustFamily(w.names[c.ids[cellIndex(q.x&15, q.y, q.z&15)]]) == "dead coral block" ||
				!strings.Contains(w.names[c.ids[cellIndex(q.x&15, q.y, q.z&15)]], "water") {
				ok++
			}
		}
		b := "none"
		switch {
		case n == 0:
		case ok*10 >= n*8:
			b = ">=80%"
		case ok*10 >= n*2:
			b = "20-80%"
		default:
			b = "<20%"
		}
		hist[b]++
		if b == "<20%" || b == "20-80%" {
			t.Logf("  piece source (%d,%d) corner %v rot %d blocks %d: %d of %d cells not water in BDS", tp.cx, tp.cz, tp.p.corner, tp.p.rot, len(tp.p.t.blocks), ok, n)
		}
	}
	t.Logf("crust pieces by the share of their cells BDS has crust (or rock) at: %v", hist)
	var lines []string
	for _, f := range fams {
		a, b := per[0][f], per[1][f]
		lines = append(lines, fmt.Sprintf("  %-17s BDS %6d; without: ours %6d both %6d; with: ours %6d both %6d (recall %.2f%%, precision %.2f%%)",
			f, b.bds, a.ours, a.both, b.ours, b.both, 100*float64(b.both)/float64(max(b.bds, 1)), 100*float64(b.both)/float64(max(b.ours, 1))))
	}
	t.Logf("warm_ocean_37 (%d chunks): %.4f%% -> %.4f%% of %d stable cells\n%s", len(w.chunks),
		100*float64(same[0])/float64(max(cells, 1)), 100*float64(same[1])/float64(max(cells, 1)), cells, strings.Join(lines, "\n"))
	if per[1]["dead coral block"].both == 0 {
		t.Errorf("no dead coral block of ours where BDS has one")
	}
}
