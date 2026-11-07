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
		gotXZ := map[[2]int]int{} // x, z to our chest's y.
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
						gotXZ[[2]int{be.Pos[0], be.Pos[2]}] = be.Pos[1]
					}
				}
			}()
		}
		wg.Wait()
		xz, pos, table, sd := 0, 0, 0, 0
		for _, l := range want {
			y, ok := gotXZ[[2]int{l.x, l.z}]
			if ok {
				xz++
			}
			d, ok := got[[3]int{l.x, l.y, l.z}]
			if !ok {
				t.Logf("seed %d: BDS chest (%d,%d,%d) %s; ours at y %d", seed, l.x, l.y, l.z, l.table[len("loot_tables/chests/"):], y)
				continue
			}
			if d["LootTableSeed"] != l.seed {
				t.Logf("seed %d: BDS chest (%d,%d,%d) seed %d; ours %v", seed, l.x, l.y, l.z, l.seed, d["LootTableSeed"])
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

// TestOceanRuinArchaeology counts the suspicious gravel our cold ruin at seed 12345's start (2, 28) makes
// in its 7 x 7 chunks, against the 65 the study found in BDS's world there (NOTES-leftovers L1.5; the
// limit, 5 per piece and chunk, is read, the shuffle's random is not). Logged, not asserted.
func TestOceanRuinArchaeology(t *testing.T) {
	g, _ := worldFor(t, 12345)
	n, bes := 0, 0
	for cx := -1; cx <= 5; cx++ {
		for cz := 25; cz <= 31; cz++ {
			col := generate(g, cx, cz)
			for _, be := range col.BlockEntities {
				if tb, _ := be.Data["LootTable"].(string); strings.Contains(tb, "ocean_ruins_brushable") {
					bes++
				}
			}
			r := col.Chunk.Range()
			for y := r.Min(); y <= 70; y++ {
				for x := range uint8(16) {
					for z := range uint8(16) {
						if ridName(col.Chunk.Block(x, int16(y), z, 0)) == "suspicious_gravel" {
							n++
						}
					}
				}
			}
		}
	}
	t.Logf("seed 12345 ruin (2,28), 7 x 7 chunks: %d suspicious gravel (%d with their loot table); BDS 65", n, bes)
	if n == 0 {
		t.Error("no suspicious gravel around a cold ruin")
	}
}
