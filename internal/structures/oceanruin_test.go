package structures

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestOceanRuinsMatchBDS: the ocean ruin chests of the measured worlds (seeds 1 and 12345 hold them):
// position (and x/z only) and loot seed.
func TestOceanRuinsMatchBDS(t *testing.T) {
	ev := loadEvidence(t)
	for _, seed := range []int64{1, 12345} {
		var want []lootEvidence
		for _, l := range ev.loot(fmt.Sprint(seed)) {
			if strings.Contains(l.table, "underwater_ruin") && ev.interior(l.x, l.z) {
				want = append(want, l)
			}
		}
		g, _ := worldFor(t, seed)
		chunks := map[[2]int]bool{}
		for _, l := range want {
			chunks[[2]int{l.x >> 4, l.z >> 4}] = true
		}
		got := map[[3]int]map[string]any{}
		gotXZ := map[[2]int]bool{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for c := range chunks {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				col := generate(g, c[0], c[1])
				mu.Lock()
				defer mu.Unlock()
				for _, be := range col.BlockEntities {
					if tb, _ := be.Data["LootTable"].(string); strings.Contains(tb, "underwater_ruin") {
						got[[3]int(be.Pos)] = be.Data
						gotXZ[[2]int{be.Pos[0], be.Pos[2]}] = true
					}
				}
			}()
		}
		wg.Wait()
		xz, pos, table, sd := 0, 0, 0, 0
		for _, l := range want {
			if gotXZ[[2]int{l.x, l.z}] {
				xz++
			}
			d, ok := got[[3]int{l.x, l.y, l.z}]
			if !ok {
				continue
			}
			pos++
			if d["LootTable"] == l.table {
				table++
			}
			if d["LootTableSeed"] == l.seed {
				sd++
			}
		}
		t.Logf("seed %d ocean ruin chests: %d in BDS; ours in those chunks %d; %d at BDS's x/z, %d at its position, %d with its table, %d with its seed",
			seed, len(want), len(got), xz, pos, table, sd)
	}
}
