package structures

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestDataJigsawContainersMatchBDS: every loot container of the data-driven jigsaw structures (trial
// chambers' chests, barrels, pots and dispensers, abandoned camps' chests and barrels) that the study's
// scans of the four measured worlds hold, against ours: position, table and loot seed.
func TestDataJigsawContainersMatchBDS(t *testing.T) {
	ev := loadEvidence(t)
	for _, seed := range testSeeds {
		key := fmt.Sprint(seed)
		var want []lootEvidence
		for _, l := range ev.loot(key) {
			if (strings.Contains(l.table, "trial_chambers") || strings.Contains(l.table, "abandoned_camp")) && ev.interior(l.x, l.z) {
				want = append(want, l)
			}
		}
		if len(want) == 0 {
			continue
		}
		g, _ := worldFor(t, seed)
		chunks := map[[2]int]bool{}
		for _, l := range want {
			chunks[[2]int{l.x >> 4, l.z >> 4}] = true
		}
		got := map[[3]int]map[string]any{}
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
					got[[3]int(be.Pos)] = be.Data
				}
			}()
		}
		wg.Wait()
		type tally struct{ n, pos, table, seed int }
		kinds := map[string]*tally{}
		var miss []string
		for _, l := range want {
			k := l.kind + " " + l.table[strings.LastIndex(l.table, "/")+1:]
			if kinds[k] == nil {
				kinds[k] = &tally{}
			}
			tl := kinds[k]
			tl.n++
			d, ok := got[[3]int{l.x, l.y, l.z}]
			if !ok {
				if len(miss) < 6 {
					miss = append(miss, fmt.Sprintf("%s at %d,%d,%d", k, l.x, l.y, l.z))
				}
				continue
			}
			tl.pos++
			if d["LootTable"] == l.table {
				tl.table++
			}
			if s, _ := d["LootTableSeed"].(int32); s == l.seed {
				tl.seed++
			}
		}
		// Chunks whose containers are exactly BDS's (same positions, none extra): there the seeds test the
		// draw order alone.
		byChunk := map[[2]int][]lootEvidence{}
		for _, l := range want {
			byChunk[[2]int{l.x >> 4, l.z >> 4}] = append(byChunk[[2]int{l.x >> 4, l.z >> 4}], l)
		}
		ours := map[[2]int]int{}
		for p, d := range got {
			if tb, _ := d["LootTable"].(string); strings.Contains(tb, "trial_chambers") || strings.Contains(tb, "abandoned_camp") {
				ours[[2]int{p[0] >> 4, p[2] >> 4}]++
			}
		}
		sameChunks, sameSeeds, sameN := 0, 0, 0
		for c, ls := range byChunk {
			all := ours[c] == len(ls)
			for _, l := range ls {
				if _, ok := got[[3]int{l.x, l.y, l.z}]; !ok {
					all = false
				}
			}
			if !all {
				continue
			}
			sameChunks++
			for _, l := range ls {
				sameN++
				if s, _ := got[[3]int{l.x, l.y, l.z}]["LootTableSeed"].(int32); s == l.seed {
					sameSeeds++
				}
			}
		}
		total, exact := 0, 0
		for _, v := range kinds {
			total, exact = total+v.n, exact+v.seed
		}
		t.Logf("seed %d: %d of %d containers exact; %d chunks with BDS's container set: %d of %d seeds exact", seed, exact, total, sameChunks, sameSeeds, sameN)
		var lines []string
		for k, v := range kinds {
			lines = append(lines, fmt.Sprintf("%s %d/%d/%d of %d", k, v.pos, v.table, v.seed, v.n))
		}
		sort.Strings(lines)
		t.Logf("seed %d (at position / with table / with seed): %s; missing e.g. %v", seed, strings.Join(lines, "; "), miss)
	}
}
