package structures

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
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

// TestNetherRuinedPortalsMatchBDS: the 9 Nether ruined portals of seed 100's measured Nether worlds
// (NOTES-leftovers L13). The study's model has chest position, y, air pocket, block pass and loot table
// 9/9 and the chest seed's draw 7/9: (-20,6)'s seed differs between BDS's own load orders (the strips
// run is the predicted one) and (-40,3) is 9 draws off. Each portal's box is scored against BDS with and
// without the structures; the Nether's ores and springs run after the portal in both (L13 4).
func TestNetherRuinedPortalsMatchBDS(t *testing.T) {
	bd, _ := installData(t)
	g, s := netherFor(t, 100)
	without := worldgenNether(bd, 100)
	pos, seeds, ys, pockets := 0, 0, 0, 0
	var lines []string
	var same0, same1, total int
	for _, c := range []struct {
		cx, cz int
		world  string
		runs   []string
		y      int  // L13's minY.
		pocket bool // L13's air pocket.
	}{
		{-16, -14, "bastion_100", []string{""}, 33, true},
		{13, 12, "fort_100", []string{""}, 43, false},
		{32, -39, "nw_100", []string{"big", "strips", "rstrips", "xrev"}, 30, false},
		{-40, 3, "l13/nA", []string{""}, 49, true},
		{-20, 6, "l13/nA_strips", []string{""}, 37, true},
		{-43, 35, "l13/nB", []string{""}, 54, true},
		{-43, 50, "l13/nB", []string{""}, 17, false},
		{3, -64, "l13/nC", []string{""}, 16, false},
		{4, -42, "l13/nC", []string{""}, 18, false},
	} {
		if _, ok := s.StartIn(NetherRuinedPortal, c.cx, c.cz); !ok {
			t.Errorf("(%d,%d) is not a Nether ruined portal start", c.cx, c.cz)
			continue
		}
		p := s.ruinedPortalAt(c.cx, c.cz)
		if p == nil || p.chest == nil {
			t.Errorf("(%d,%d): no portal or no chest", c.cx, c.cz)
			continue
		}
		b := p.box
		w := loadBDSRunsIn(t, c.world, c.runs, world.Nether, true, b[0]>>4-1, b[3]>>4+1, b[2]>>4-1, b[5]>>4+1)
		var theirs [3]int
		var seed any
		for q, d := range w.bes {
			if q[0] >= b[0]-1 && q[0] <= b[3]+1 && q[2] >= b[2]-1 && q[2] <= b[5]+1 && strings.Contains(fmt.Sprint(d["LootTable"]), "ruined_portal") {
				theirs, seed = q, d["LootTableSeed"]
			}
		}
		okPos := p.chest.Pos == theirs
		okSeed := okPos && fmt.Sprint(p.chest.Data["LootTableSeed"]) == fmt.Sprint(seed)
		if okPos {
			pos++
		}
		if okSeed {
			seeds++
		}
		if b[1] == c.y {
			ys++
		}
		if p.pocket == c.pocket {
			pockets++
		}
		var s0, s1, n int
		conf := map[[2]string]int{}
		for cx := b[0] >> 4; cx <= b[3]>>4; cx++ {
			for cz := b[2] >> 4; cz <= b[5]>>4; cz++ {
				if w.chunks[[2]int{cx, cz}] == nil {
					continue
				}
				a, tot, _ := boxScore(w.bdsWorld, generate(without, cx, cz).Chunk, cx, cz, []box{b})
				bb, _, cf := boxScore(w.bdsWorld, generate(g, cx, cz).Chunk, cx, cz, []box{b})
				for k, v := range cf {
					conf[k] += v
				}
				s0, s1, n = s0+a, s1+bb, n+tot
			}
		}
		same0, same1, total = same0+s0, same1+s1, total+n
		lines = append(lines, fmt.Sprintf("(%d,%d) %s: y %d (BDS %d), pocket %v (BDS %v), chest %v (BDS %v) seed match %v; box %.2f%% -> %.2f%% of %d cells; %s",
			c.cx, c.cz, c.world, b[1], c.y, p.pocket, c.pocket, p.chest.Pos, theirs, okSeed,
			100*float64(s0)/float64(max(n, 1)), 100*float64(s1)/float64(max(n, 1)), n, topConfusions(conf, 5)))
	}
	t.Logf("Nether ruined portals: chest position %d/9, seed %d/9, y %d/9, pocket %d/9; boxes %.2f%% -> %.2f%% of %d cells (study: 9/9, seed 7/9)\n%s",
		pos, seeds, ys, pockets, 100*float64(same0)/float64(max(total, 1)), 100*float64(same1)/float64(max(total, 1)), total, strings.Join(lines, "\n"))
	if pos < 9 || ys < 9 || pockets < 9 {
		t.Errorf("chest position %d/9, y %d/9, pocket %d/9: want 9/9", pos, ys, pockets)
	}
	if same1 < same0 {
		t.Errorf("the portals lower their boxes' score")
	}
}
