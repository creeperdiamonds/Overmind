package structures

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestRuinedPortalsMatchBDS: the ruined portal chests of the four measured worlds (R1: 14 overworld
// chests; the study's model has template, rotation, mirror and x/z 14/14, the seed's draw 10/14), and
// the blocks of seed 100's (3,7) portal against the whole-world export.
func TestRuinedPortalsMatchBDS(t *testing.T) {
	ev := loadEvidence(t)
	pos, seeds, n := 0, 0, 0
	var lines []string
	for _, seed := range testSeeds {
		_, s := worldFor(t, seed)
		for _, l := range ev.loot(fmt.Sprint(seed)) {
			if !strings.Contains(l.table, "ruined_portal") {
				continue
			}
			n++
			var got *rpPlan
			for cx := l.x>>4 - portalReach; cx <= l.x>>4+portalReach && got == nil; cx++ {
				for cz := l.z>>4 - portalReach; cz <= l.z>>4+portalReach; cz++ {
					if p := s.ruinedPortalAt(cx, cz); p != nil && p.chest != nil && abs(p.chest.Pos[0]-l.x) < 4 && abs(p.chest.Pos[2]-l.z) < 4 {
						got = p
						break
					}
				}
			}
			line := fmt.Sprintf("seed %d %v: ", seed, [3]int{l.x, l.y, l.z})
			if got == nil {
				lines = append(lines, line+"no portal of ours")
				continue
			}
			c := got.chest
			ok := c.Pos == [3]int{l.x, l.y, l.z}
			if ok {
				pos++
			}
			if ok && c.Data["LootTableSeed"] == l.seed {
				seeds++
			}
			lines = append(lines, fmt.Sprintf("%sours %v seed match %v", line, c.Pos, c.Data["LootTableSeed"] == l.seed))
		}
	}
	t.Logf("ruined portal chests: %d of %d at BDS's position, %d with its seed (study: x/z 14/14, seed draw 10/14)\n%s", pos, n, seeds, strings.Join(lines, "\n"))

	g, s := worldFor(t, 100)
	p := s.ruinedPortalAt(3, 7)
	if p == nil {
		t.Fatal("seed 100: (3,7) is not a ruined portal start")
	}
	var bb box
	first := true
	for q := range p.blocks {
		if first {
			bb, first = box{q[0], q[1], q[2], q[0], q[1], q[2]}, false
		}
		bb = box{min(bb[0], q[0]), min(bb[1], q[1]), min(bb[2], q[2]), max(bb[3], q[0]), max(bb[4], q[1]), max(bb[5], q[2])}
	}
	w := loadWorld(t, filepath.Join(goldenDir(t), "whole", "seed100.bin.gz"))
	same, total := 0, 0
	conf := map[[2]string]int{}
	for cx := bb[0] >> 4; cx <= bb[3]>>4; cx++ {
		for cz := bb[2] >> 4; cz <= bb[5]>>4; cz++ {
			if w.chunks[[2]int{cx, cz}] == nil {
				continue
			}
			col := generate(g, cx, cz)
			sm, tt, cf := boxScore(w, col.Chunk, cx, cz, []box{bb})
			same, total = same+sm, total+tt
			for k, v := range cf {
				conf[k] += v
			}
		}
	}
	t.Logf("seed 100 portal (3,7): %d blocks written, box %v: %d/%d = %.2f%% same; %s", len(p.blocks), bb, same, total,
		100*float64(same)/float64(max(total, 1)), topConfusions(conf, 12))
}
