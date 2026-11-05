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
			// The chunk's chests with a buried treasure table (other block entities, such as a mineshaft's
			// spawner in the same chunk, are not candidates).
			var got []string
			found := false
			for _, be := range col.BlockEntities {
				if be.Data["LootTable"] != l.table {
					continue
				}
				if be.Pos == [3]int{l.x, l.y, l.z} && be.Data["LootTableSeed"] == l.seed {
					found = true
					break
				}
				got = append(got, fmt.Sprintf("%v seed %v", be.Pos, be.Data["LootTableSeed"]))
			}
			if found {
				exact++
				continue
			}
			wrong = append(wrong, fmt.Sprintf("seed %d BDS (%d,%d,%d) seed %d, ours %v", seed, l.x, l.y, l.z, l.seed, got))
		}
	}
	t.Logf("buried treasure: %d of %d chests exact (position, table and loot seed)", exact, n)
	for _, w := range wrong {
		t.Error(w)
	}
}
