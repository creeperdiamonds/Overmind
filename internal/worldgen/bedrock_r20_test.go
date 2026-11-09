package worldgen

import (
	"path/filepath"
	"testing"
)

// TestRound20BlocksMatchBDS measures the blocks of Task 9 round 20 (NOTES-leftovers L17, L18, L20, L21) in
// the study's windows (golden-bedrock/whole/windows) on their stable masks: for each block BDS's count,
// ours and how many agree (recall and precision), and in the frozen windows the open sea columns (water
// at Y 62 with only air or a snow layer above). Nothing is held: the figures measure the rules.
func TestRound20BlocksMatchBDS(t *testing.T) {
	dir := goldenBedrockDir(t)
	testBedrockData(t)
	windows := []struct {
		name   string
		blocks []string
	}{
		{"frozen_ocean_4", []string{"ice", "water", "magma"}},
		{"deep_frozen_ocean_4", []string{"ice", "water", "magma"}},
		{"cold_beach_25", []string{"ice", "water", "magma"}},
		{"frozen_river_2", []string{"ice", "water", "magma"}},
		{"flower_forest_20", []string{"bee_nest"}},
		{"cherry_grove_35", []string{"bee_nest"}},
		{"meadow_987654321", []string{"bee_nest"}},
		{"pale_garden_33", []string{"creaking_heart"}},
		{"jungle_22", []string{"cocoa"}},
		{"mangrove_swamp_20", []string{"bee_nest", "mud", "water", "stone", "dirt", "grass_block", "clay", "magma"}},
		{"swampland_17", []string{"grass_block", "dirt", "gravel", "water", "stone", "clay", "magma"}},
		{"cold_ocean_34", []string{"magma"}},
		{"lukewarm_ocean_31", []string{"magma"}},
		{"warm_ocean_37", []string{"magma"}},
	}
	for _, win := range windows {
		t.Run(win.name, func(t *testing.T) {
			f := filepath.Join(dir, "whole", "windows", win.name+".bin.gz")
			w := loadBDSFile(t, f)
			defer bdsWorlds.Delete(f)
			var pos [][2]int32
			for k := range w.chunks {
				pos = append(pos, [2]int32{int32(k[0]), int32(k[1])})
			}
			got := generateNames(NewWithProfile(w.seed, ProfileBedrock), pos)
			type tally struct{ bds, ours, both int }
			all := make([]tally, len(win.blocks))
			var openBDS, openOurs, openBoth, cols int
			for _, p := range pos {
				k := [2]int{int(p[0]), int(p[1])}
				bc, ours := w.chunks[k], got[k]
				for cell := range chunkCells - 256 { // Y 319 holds the study's loading marker.
					if bc.masked(cell) {
						continue
					}
					real, mine := w.names[bc.ids[cell]], ours.names[cell]
					for i, n := range win.blocks {
						a, b := real == n, mine == n
						if a {
							all[i].bds++
						}
						if b {
							all[i].ours++
						}
						if a && b {
							all[i].both++
						}
					}
				}
				// Open sea at Y 62: water there, only air or snow layers above.
				for i := range 256 {
					x, z := i&15, i>>4
					open := func(name func(cell int) string) bool {
						if name(cellIndex(x, 62, z)) != "water" {
							return false
						}
						for y := 63; y < maxY; y++ {
							if n := name(cellIndex(x, y, z)); n != "air" && n != "snow_layer" {
								return false
							}
						}
						return true
					}
					if bc.masked(cellIndex(x, 62, z)) {
						continue
					}
					cols++
					a := open(func(c int) string { return w.names[bc.ids[c]] })
					b := open(func(c int) string { return ours.names[c] })
					if a {
						openBDS++
					}
					if b {
						openOurs++
					}
					if a && b {
						openBoth++
					}
				}
			}
			t.Logf("%s: open sea at Y 62: BDS %d ours %d both %d of %d columns", win.name, openBDS, openOurs, openBoth, cols)
			for i, n := range win.blocks {
				c := all[i]
				t.Logf("  %-15s BDS %8d ours %8d both %8d: recall %6.2f%%, precision %6.2f%%", n, c.bds, c.ours, c.both,
					100*float64(c.both)/float64(max(c.bds, 1)), 100*float64(c.both)/float64(max(c.ours, 1)))
			}
		})
	}
}
