package structures

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// structureScore compares the cells inside a structure's piece boxes, ours against a BDS world's runs,
// over every chunk the world has (as the study's figures count).
// It reports the cell figure, the chests (position and loot seed) and the spawners.
func structureScore(t *testing.T, name string, g *worldgen.Generator, w *bdsRuns, boxes []box) (same, total int) {
	all := boundsOf(boxes)
	conf := map[[2]string]int{}
	type chestKey [3]int
	ours, theirs := map[chestKey]int32{}, map[chestKey]int32{}
	oursSp, theirsSp := map[chestKey]string{}, map[chestKey]string{}
	for cx := all[0] >> 4; cx <= all[3]>>4; cx++ {
		for cz := all[2] >> 4; cz <= all[5]>>4; cz++ {
			if w.chunks[[2]int{cx, cz}] == nil {
				continue
			}
			var mine []box
			cb := box{cx * 16, minY, cz * 16, cx*16 + 15, maxY, cz*16 + 15}
			for _, b := range boxes {
				if b.intersects(cb) {
					mine = append(mine, b)
				}
			}
			if len(mine) == 0 {
				continue
			}
			col := generate(g, cx, cz)
			sm, tt, cf := boxScore(w.bdsWorld, col.Chunk, cx, cz, mine)
			same, total = same+sm, total+tt
			for k, v := range cf {
				conf[k] += v
			}
			inside := func(p [3]int) bool {
				if p[0]>>4 != cx || p[2]>>4 != cz {
					return false
				}
				for _, b := range mine {
					if b.inside(p) {
						return true
					}
				}
				return false
			}
			read := func(d map[string]any, chests map[chestKey]int32, sp map[chestKey]string, p [3]int) {
				switch d["id"] {
				case "Chest":
					v, _ := d["LootTableSeed"].(int32)
					chests[chestKey(p)] = v
				case "MobSpawner":
					v, _ := d["EntityIdentifier"].(string)
					sp[chestKey(p)] = v
				}
			}
			for _, b := range col.BlockEntities {
				if inside(b.Pos) {
					read(b.Data, ours, oursSp, b.Pos)
				}
			}
			for p, d := range w.bes {
				if inside(p) {
					read(d, theirs, theirsSp, p)
				}
			}
		}
	}
	chestOK, posOK := 0, 0
	var lines []string
	for p, s := range theirs {
		o, ok := ours[p]
		if ok {
			posOK++
		}
		if ok && o == s {
			chestOK++
		}
		lines = append(lines, fmt.Sprintf("%v bds %d ours %v(%v)", p, s, o, ok))
	}
	sort.Strings(lines)
	spOK := 0
	for p, e := range theirsSp {
		if oursSp[p] == e {
			spOK++
		}
	}
	t.Logf("%s: %d/%d = %.2f%% of the cells in the piece boxes; chests %d/%d at BDS's position, %d with its seed (ours %d); spawners %d/%d (ours %d); %s",
		name, same, total, 100*float64(same)/float64(max(total, 1)), posOK, len(theirs), chestOK, len(ours), spOK, len(theirsSp), len(oursSp), topConfusions(conf, 14))
	if len(lines) > 0 {
		t.Logf("%s chests: %s", name, strings.Join(lines, "; "))
	}
	return
}

func pieceBoxes[T interface{ boxOf() box }](ps []T) []box {
	out := make([]box, len(ps))
	for i, p := range ps {
		out[i] = p.boxOf()
	}
	return out
}

func (p *msPiece) boxOf() box { return p.b }
func (p *shPiece) boxOf() box { return p.b }

// TestMineshaftsMatchBDS: seed 5's mineshaft at (-12,-52) (the witch hut world) and seed 100's at
// (52,101) (the stronghold world), P14.1; the study's harness: 86.6% and 88.5% of cells.
func TestMineshaftsMatchBDS(t *testing.T) {
	for _, c := range []struct {
		seed   int64
		cx, cz int
		world  string
		runs   []string
	}{
		{5, -12, -52, "st_witch_hut_5", []string{"big", "strips", "rstrips", "xrev"}},
		{100, 52, 101, "sh_100", []string{"big", "strips", "rstrips"}},
	} {
		g, s := worldFor(t, c.seed)
		if !s.mineshaftAt(c.cx, c.cz) {
			t.Errorf("seed %d: (%d,%d) is not a mineshaft start", c.seed, c.cx, c.cz)
			continue
		}
		m := s.mineshaftAtStart(c.cx, c.cz)
		boxes := pieceBoxes(m.pieces)
		all := boundsOf(boxes)
		w := loadBDSRuns(t, c.world, c.runs, all[0]>>4-1, all[3]>>4+1, all[2]>>4-1, all[5]>>4+1)
		t.Logf("seed %d mineshaft (%d,%d): %d pieces, box %v, mesa %v", c.seed, c.cx, c.cz, len(m.pieces), all, m.mesa)
		structureScore(t, fmt.Sprintf("seed %d mineshaft", c.seed), g, w, boxes)
	}
}

// TestStrongholdMatchesBDS: seed 100's ring stronghold at (53,100), P14.4; the study's model: 149/149
// pieces, 99.33% of cells, 9/9 chest seeds.
func TestStrongholdMatchesBDS(t *testing.T) {
	g, s := worldFor(t, 100)
	if !s.strongholdAt(53, 100) {
		t.Fatal("(53,100) is not a stronghold start")
	}
	h := s.strongholdAtStart(53, 100)
	boxes := pieceBoxes(h.pieces)
	all := boundsOf(boxes)
	kinds := map[int]int{}
	for _, p := range h.pieces {
		kinds[p.kind]++
	}
	t.Logf("seed 100 stronghold: %d pieces (study 149), box %v (study y 10..48), kinds %v", len(h.pieces), all, kinds)
	w := loadBDSRuns(t, "sh_100", []string{"big", "strips", "rstrips"}, 46, 60, 93, 107)
	structureScore(t, "seed 100 stronghold", g, w, boxes)

	// The eye of ender leads to the start piece's corner, and the stronghold is there.
	at, ok := s.NearestStronghold(cube.Pos{860, 70, 1610})
	if !ok || at != (cube.Pos{850, 0, 1602}) {
		t.Fatalf("NearestStronghold = %v %v, want (850, 0, 1602)", at, ok)
	}
	col := generate(g, 53, 100)
	bricks := 0
	for y := minY; y <= maxY; y++ {
		if strings.HasSuffix(nameAt(col.Chunk, at[0]+1, y, at[2]+1), "stone_bricks") {
			bricks++
		}
	}
	if bricks == 0 {
		t.Fatalf("no stone bricks in the column at %v", at)
	}
}
