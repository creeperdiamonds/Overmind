package structures

import (
	"path/filepath"
	"testing"
)

// The study's structure windows (golden-bedrock/whole/structures, 9 x 9 chunks of BDS worlds chosen to
// hold one structure each; N1 6 test seeds). We cannot build these pieces yet, so this checks starts
// only: every chunk where BDS has the structure's marker blocks must be within reach of a start we
// place, and every start we place in the window must have them within reach.
var structureWindows = []struct {
	file   string
	seed   int64
	kind   Kind
	piece  string // The scattered piece, when kind is Scattered.
	marker []string
	least  int // Marker blocks a structure's chunks have at least.
	reach  int // Chunks a structure's blocks reach from its start.
}{
	{"desert_pyramid_11", 11, Scattered, "desert_pyramid", []string{"tnt"}, 5, 2},
	{"jungle_temple_31", 31, Scattered, "jungle_temple", []string{"mossy_cobblestone", "chiseled_stone_bricks"}, 30, 2},
	{"witch_hut_5", 5, Scattered, "witch_hut", []string{"cauldron"}, 1, 2},
	{"igloo_10", 10, Scattered, "igloo", []string{"crafting_table"}, 1, 2},
	{"ocean_monument_32", 32, Monument, "", []string{"sea_lantern"}, 5, 4},
	{"woodland_mansion_8", 8, Mansion, "", []string{"dark_oak_planks", "birch_planks"}, 30, 4},
	{"pillager_outpost_15", 15, Outpost, "", []string{"birch_planks"}, 30, 2},
	{"village_22", 22, Village, "", []string{"bell"}, 1, 6},
}

func TestStructureWindowStarts(t *testing.T) {
	dir := goldenDir(t)
	for _, win := range structureWindows {
		t.Run(win.file, func(t *testing.T) {
			t.Parallel()
			w := loadWorld(t, filepath.Join(dir, "whole", "structures", win.file+".bin.gz"))
			_, s := worldFor(t, win.seed)
			marked := map[[2]int]int{}
			for k, c := range w.chunks {
				n := 0
				for cell, id := range c.ids {
					if c.masked(cell) {
						continue
					}
					name := w.names[id]
					for _, m := range win.marker {
						if name == m {
							n++
						}
					}
				}
				marked[k] = n
			}
			var starts []Start
			for cx := w.x0 - 2; cx < w.x1+2; cx++ {
				for cz := w.z0 - 2; cz < w.z1+2; cz++ {
					if st, ok := s.StartIn(win.kind, cx, cz); ok && (win.piece == "" || st.Piece == win.piece) {
						starts = append(starts, st)
					}
				}
			}
			t.Logf("starts we place in and around the window: %v", starts)
			for k, n := range marked {
				if n < win.least {
					continue
				}
				near := false
				for _, st := range starts {
					if abs(st.CX-k[0]) <= win.reach && abs(st.CZ-k[1]) <= win.reach {
						near = true
					}
				}
				if !near {
					t.Errorf("chunk %v has %d %v blocks in BDS and no start within 2 chunks", k, n, win.marker)
				}
			}
			for _, st := range starts {
				if st.CX < w.x0 || st.CX >= w.x1 || st.CZ < w.z0 || st.CZ >= w.z1 {
					continue
				}
				n := 0
				for dx := -win.reach; dx <= win.reach; dx++ {
					for dz := -win.reach; dz <= win.reach; dz++ {
						n += marked[[2]int{st.CX + dx, st.CZ + dz}]
					}
				}
				if n < win.least {
					t.Errorf("start %v: BDS has only %d %v blocks around it", st, n, win.marker)
				}
			}
		})
	}
}
