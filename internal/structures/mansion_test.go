package structures

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestMansionMatchesBDS: seed 987654321's (105,-292) mansion, world p15/mansion_987654321 (one run).
// The study (P185, P207, P201): 490 pieces, 0.9975 air / not air of 88,608 cells, 0.9998 block names of
// 36,719 non-air cells, 6 of 6 chests with BDS's loot seed.
func TestMansionMatchesBDS(t *testing.T) {
	g, s := worldFor(t, 987654321)
	m := s.mansionAt(105, -292)
	if m == nil {
		t.Fatalf("(105,-292) is not a mansion start (problems %v)", s.data.Problems)
	}
	boxes := make([]box, len(m.pieces))
	for i, p := range m.pieces {
		boxes[i] = p.box
	}
	all := boundsOf(boxes)
	t.Logf("seed 987654321 mansion: %d pieces (study 490), %d draws (study 217), start y %d (study 76), bounds %v",
		len(m.pieces), m.draws, m.pieces[0].pos[1], all)
	if ref := os.Getenv("OVERMIND_MANSION_RECORDS"); ref != "" {
		compareMansionRecords(t, m, ref)
	}
	if len(m.pieces) != 490 {
		t.Errorf("%d pieces, want 490", len(m.pieces))
	}
	w := loadBDSRunsIn(t, "mansion_987654321", []string{""}, world.Overworld, true, all[0]>>4-1, all[3]>>4+1, all[2]>>4-1, all[5]>>4+1)
	same, total := structureScore(t, "seed 987654321 mansion", g, w, boxes)
	// Below the mansion (Java fills cobblestone under it; Bedrock's rule was not read).
	below := []box{{all[0], all[1] - 16, all[2], all[3], all[1] - 1, all[5]}}
	structureScore(t, "below the mansion (16 layers)", g, w, below)
	if total == 0 || same*1000 < total*990 {
		t.Errorf("mansion cells %d/%d below 99%%", same, total)
	}
}

// compareMansionRecords checks our pieces against the study harness's records ("T name x y z ROT MIRROR",
// origins on Java's +7, P164), a development aid outside the repo.
func compareMansionRecords(t *testing.T, m *mansion, path string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rots := map[string]int{"NONE": 0, "CLOCKWISE_90": 1, "CLOCKWISE_180": 2, "COUNTERCLOCKWISE_90": 3}
	mirrors := map[string]int{"NONE": 0, "LEFT_RIGHT": 1, "FRONT_BACK": 2}
	var want []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var name, rot, mir string
		var x, y, z int
		if n, _ := fmt.Sscanf(sc.Text(), "T %s %d %d %d %s %s", &name, &x, &y, &z, &rot, &mir); n == 6 {
			if strings.HasPrefix(name, "carpet_") || strings.HasPrefix(name, "indoors_") {
				name = strings.TrimSuffix(name, "_1") // BDS's first-floor files (P164).
			}
			want = append(want, fmt.Sprintf("%s %d %d %d %d %d", name, x+1, y, z+1, rots[rot], mirrors[mir]))
		}
	}
	bad := 0
	for i := range max(len(want), len(m.pieces)) {
		got, exp := "-", "-"
		if i < len(m.pieces) {
			p := m.pieces[i]
			got = fmt.Sprintf("%s %d %d %d %d %d", p.name, p.pos[0], p.pos[1], p.pos[2], p.rot, p.mirror)
		}
		if i < len(want) {
			exp = want[i]
		}
		if got != exp {
			if bad < 10 {
				t.Errorf("record %d: ours %s, study %s", i, got, exp)
			}
			bad++
		}
	}
	t.Logf("records: %d of %d differ", bad, len(want))
}
