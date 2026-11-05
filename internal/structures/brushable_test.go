package structures

import (
	"fmt"
	"strings"
	"testing"
)

// TestTrailRuinsBrushablesMatchBDS: the trail ruin's suspicious gravel of seed 12345 (the scan's
// BrushableBlock entities) against ours: position, loot table and seed.
func TestTrailRuinsBrushablesMatchBDS(t *testing.T) {
	ev := loadEvidence(t)
	g, _ := worldFor(t, 12345)
	n, pos, seed := 0, 0, 0
	cols := map[[2]int][]map[string]any{}
	posOf := map[[2]int][][3]int{}
	for _, l := range ev.loot("12345") {
		if !strings.Contains(l.table, "trail_ruins_brushable") {
			continue
		}
		n++
		c := [2]int{floorDiv(l.x, 16), floorDiv(l.z, 16)}
		if _, ok := cols[c]; !ok {
			cols[c] = nil
			for _, be := range generate(g, c[0], c[1]).BlockEntities {
				cols[c] = append(cols[c], be.Data)
				posOf[c] = append(posOf[c], be.Pos)
			}
		}
		got := "none"
		for i, d := range cols[c] {
			if posOf[c][i] == [3]int{l.x, l.y, l.z} {
				got = fmt.Sprint(d)
				if d["LootTable"] == l.table {
					pos++
					if d["LootTableSeed"] == l.seed {
						seed++
					}
				}
			}
		}
		t.Logf("BDS (%d,%d,%d) %s %d; ours %s", l.x, l.y, l.z, l.table[12:], l.seed, got)
	}
	t.Logf("trail ruins brushables: %d of %d at BDS's position with its table, %d with its seed", pos, n, seed)
}
