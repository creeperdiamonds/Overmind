package structures

import (
	"fmt"
	"strings"
	"testing"
)

// TestBuriedTreasureMatchesBDS generates the chunk of every buried treasure chest in the four measured
// worlds and wants our chest at BDS's position with BDS's loot seed (N2 2.1: 27 of 27 seeds found by
// the study).
func TestBuriedTreasureMatchesBDS(t *testing.T) {
	ev := loadEvidence(t)
	n, exact := 0, 0
	var wrong []string
	for _, seed := range testSeeds {
		g, _ := worldFor(t, seed)
		for _, l := range ev.loot(fmt.Sprint(seed)) {
			if !strings.Contains(l.table, "buriedtreasure") {
				continue
			}
			n++
			col := generate(g, floorDiv(l.x, 16), floorDiv(l.z, 16))
			got := "none"
			for _, be := range col.BlockEntities {
				got = fmt.Sprintf("%v seed %v", be.Pos, be.Data["LootTableSeed"])
				if be.Pos == [3]int{l.x, l.y, l.z} && be.Data["LootTableSeed"] == l.seed && be.Data["LootTable"] == l.table {
					exact++
					got = ""
				}
			}
			if got != "" {
				wrong = append(wrong, fmt.Sprintf("seed %d BDS (%d,%d,%d) seed %d, ours %s", seed, l.x, l.y, l.z, l.seed, got))
			}
		}
	}
	t.Logf("buried treasure: %d of %d chests exact (position, table and loot seed)", exact, n)
	for _, w := range wrong {
		t.Error(w)
	}
}
