package structures

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestNetherStartsMatchLocate: seed 100's Nether starts that BDS's locate gave (P15.2) and the study's
// worlds hold (N1 6), each of the right kind.
func TestNetherStartsMatchLocate(t *testing.T) {
	_, s := netherFor(t, 100)
	for _, c := range []struct {
		k      Kind
		cx, cz int
	}{
		{Fortress, 4, 19}, {Fortress, 70, -24}, {Fortress, -6, 82},
		{Bastion, -13, -20}, {Bastion, 84, 19}, {Bastion, 22, 64}, {Bastion, 9, -9}, {Bastion, 4, 34},
	} {
		if _, ok := s.StartIn(c.k, c.cx, c.cz); !ok {
			sp := spreads[c.k]
			st, bio := s.netherStart(floorDiv(c.cx, sp.spacing), floorDiv(c.cz, sp.spacing))
			t.Errorf("%v at (%d,%d) is not a start: region gives %v (%d,%d), biome test %v", c.k, c.cx, c.cz, st.Kind, st.CX, st.CZ, bio)
		}
	}
}

// TestFortressMatchesBDS: seed 100's (4,19) fortress (P15.2: 133/133 pieces, 99.57% of 41,103 cells, 2/2
// chest loot seeds, 2/2 blaze spawners).
func TestFortressMatchesBDS(t *testing.T) {
	g, s := netherFor(t, 100)
	st, ok := s.StartIn(Fortress, 4, 19)
	if !ok {
		t.Fatal("(4,19) is not a fortress start")
	}
	f := s.fortressAtStart(st)
	boxes := pieceBoxes(f.pieces)
	all := boundsOf(boxes)
	t.Logf("seed 100 fortress: %d pieces (study 133), box %v (study x 7..190, y 48..79, z 189..390)", len(f.pieces), all)
	if len(f.pieces) != 133 {
		t.Errorf("%d pieces, want 133", len(f.pieces))
	}
	w := loadBDSRunsIn(t, "fort_100", []string{""}, world.Nether, true, all[0]>>4-1, all[3]>>4+1, all[2]>>4-1, all[5]>>4+1)
	same, total := structureScore(t, "seed 100 fortress", g, w, boxes)
	if total == 0 || same*100 < total*99 {
		t.Errorf("fortress cells %d/%d below 99%%", same, total)
	}
}

func (p *nfPiece) boxOf() box { return p.b }

// TestBastionsMatchBDS: seed 100's two hoglin stables, (-13,-20) and (9,-9) (P16.3: 49 of 50 pieces with
// 50 cells or more score 0.8 or more on air / not air; the start's air_base 0.73).
func TestBastionsMatchBDS(t *testing.T) {
	g, s := netherFor(t, 100)
	for _, c := range []struct {
		cx, cz int
		world  string
		runs   []string
	}{
		{-13, -20, "bastion_100", []string{""}},
		{9, -9, "n100", []string{"big", "strips", "rstrips", "xrev"}},
	} {
		st, ok := s.StartIn(Bastion, c.cx, c.cz)
		if !ok {
			t.Errorf("(%d,%d) is not a bastion start", c.cx, c.cz)
			continue
		}
		pieces := s.bastionAtStart(st)
		var boxes []box
		for _, p := range pieces {
			boxes = append(boxes, p.box)
		}
		all := boundsOf(boxes)
		w := loadBDSRunsIn(t, c.world, c.runs, world.Nether, true, all[0]>>4-1, all[3]>>4+1, all[2]>>4-1, all[5]>>4+1)
		good, scored, mobs := 0, 0, 0
		var low []string
		for i, p := range pieces {
			if p.elem.mob != "" {
				mobs++
				continue
			}
			agree, n := 0, 0
			for _, b := range p.elem.template.blocks {
				q := rotate(b.pos, p.rot)
				x, y, z := p.pos[0]+q[0], p.pos[1]+q[1], p.pos[2]+q[2]
				bc := w.chunks[[2]int{x >> 4, z >> 4}]
				if bc == nil || y < 0 || y > 127 {
					continue
				}
				cell := cellOf(x, y, z)
				if bc.masked(cell) {
					continue
				}
				res := convert.state(p.elem.template.palette[b.state], p.rot)
				if !res.ok || res.name == "structure_void" || res.name == "jigsaw" {
					continue
				}
				n++
				if (res.name == "air") == (w.names[bc.ids[cell]] == "air") {
					agree++
				}
			}
			if n >= 50 {
				scored++
				if agree*5 >= n*4 {
					good++
				} else {
					low = append(low, fmt.Sprintf("#%d %s %.2f", i, p.elem.name, float64(agree)/float64(n)))
				}
			}
		}
		t.Logf("bastion (%d,%d): %d pieces (%d mobs), %d of %d pieces with 50+ cells score >= 0.8 (study 49/50); low: %v",
			c.cx, c.cz, len(pieces), mobs, good, scored, low)
		structureScore(t, fmt.Sprintf("bastion (%d,%d)", c.cx, c.cz), g, w, boxes)
		if good*50 < scored*45 {
			t.Errorf("bastion (%d,%d): only %d of %d pieces score 0.8", c.cx, c.cz, good, scored)
		}
	}
}

// TestNetherWindowsWithStructures scores whole Nether chunks against BDS, with and without the
// fortresses and bastions: the 9 x 9 windows of seed 100's Nether world around its fortress and bastion
// starts (N1 6), and the fortress world's chunks. Y 127 is left out (the study's loading marker).
func TestNetherWindowsWithStructures(t *testing.T) {
	bd, _ := installData(t)
	with, _ := netherFor(t, 100)
	without := worldgenNether(bd, 100)
	for _, c := range []struct {
		name   string
		world  string
		runs   []string
		cx, cz int
		r      int
	}{
		{"fortress window (-24,4)", "n100", []string{"big", "strips", "rstrips", "xrev"}, -24, 4, 4},
		{"fortress window (10,-44)", "n100", []string{"big", "strips", "rstrips", "xrev"}, 10, -44, 4},
		{"bastion window (-13,-20)", "n100", []string{"big", "strips", "rstrips", "xrev"}, -13, -20, 4},
		{"bastion window (4,34)", "n100", []string{"big", "strips", "rstrips", "xrev"}, 4, 34, 4},
		{"bastion (9,-9)", "n100", []string{"big", "strips", "rstrips", "xrev"}, 9, -9, 4},
		{"fortress world (4,19)", "fort_100", []string{""}, 4, 19, 8},
	} {
		w := loadBDSRunsIn(t, c.world, c.runs, world.Nether, true, c.cx-c.r, c.cx+c.r, c.cz-c.r, c.cz+c.r)
		var s0, s1, n int
		for cx := c.cx - c.r; cx <= c.cx+c.r; cx++ {
			for cz := c.cz - c.r; cz <= c.cz+c.r; cz++ {
				if w.chunks[[2]int{cx, cz}] == nil {
					continue
				}
				b := []box{{cx * 16, 0, cz * 16, cx*16 + 15, 126, cz*16 + 15}}
				a, tot, _ := boxScore(w.bdsWorld, generate(without, cx, cz).Chunk, cx, cz, b)
				bb, _, _ := boxScore(w.bdsWorld, generate(with, cx, cz).Chunk, cx, cz, b)
				s0, s1, n = s0+a, s1+bb, n+tot
			}
		}
		t.Logf("%s: %.4f%% without structures, %.4f%% with (%d cells)", c.name, 100*float64(s0)/float64(max(n, 1)), 100*float64(s1)/float64(max(n, 1)), n)
		if s1 < s0 {
			t.Errorf("%s: structures lower the score", c.name)
		}
	}
}
