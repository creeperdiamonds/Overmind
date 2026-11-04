package structures

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
)

// evidenceKind says which structure a scanned block entity belongs to, and how far (chunks) from its
// start it may be: the distances the study measured (N1 2.6; N2 1.9 for the ancient city's extent).
func evidenceKind(table string) (Kind, int, bool) {
	switch {
	case strings.Contains(table, "buriedtreasure"):
		return BuriedTreasure, 0, true
	case strings.Contains(table, "ancient_city"):
		return AncientCity, 8, true
	case strings.Contains(table, "chests/village/"):
		return Village, 5, true
	case strings.Contains(table, "pillager_outpost"):
		return Outpost, 1, true
	case strings.Contains(table, "shipwreck"):
		return Shipwreck, 1, true
	case strings.Contains(table, "underwater_ruin"), strings.Contains(table, "cold_ocean_ruins"):
		return OceanRuin, 2, true
	case strings.Contains(table, "ruined_portal"):
		return RuinedPortal, 1, true
	case strings.Contains(table, "abandoned_camp"):
		return AbandonedCamp, 2, true
	case strings.Contains(table, "trial_chambers"):
		return TrialChambers, 8, true
	case strings.Contains(table, "trail_ruins"):
		return TrailRuins, 3, true
	}
	return 0, 0, false
}

// startsNear lists the starts of a kind within r chunks of (cx, cz).
func (s *Structures) startsNear(k Kind, cx, cz, r int) []Start {
	var out []Start
	for x := cx - r; x <= cx+r; x++ {
		for z := cz - r; z <= cz+r; z++ {
			if st, ok := s.StartIn(k, x, z); ok {
				out = append(out, st)
			}
		}
	}
	return out
}

// TestStartsMatchBDS checks structure starts against every structure block entity, spawner and volume
// the study scanned in the four measured worlds: each must be within its measured distance of a start
// we place (with our biome tests). Buried treasure must be exact: the chest at the start chunk's middle.
func TestStartsMatchBDS(t *testing.T) {
	ev := loadEvidence(t)
	for _, seed := range testSeeds {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Parallel()
			_, s := worldFor(t, seed)
			key := fmt.Sprint(seed)
			type tally struct{ n, ok int }
			got := map[string]*tally{}
			var mu sync.Mutex
			var misses []string
			check := func(name string, k Kind, x, z, r int) {
				cx, cz := floorDiv(x, 16), floorDiv(z, 16)
				ok := false
				if k == BuriedTreasure {
					_, ok = s.StartIn(k, cx, cz)
					ok = ok && x == cx*16+8 && z == cz*16+8
				} else {
					ok = len(s.startsNear(k, cx, cz, r)) > 0
				}
				mu.Lock()
				defer mu.Unlock()
				if got[name] == nil {
					got[name] = &tally{}
				}
				got[name].n++
				if ok {
					got[name].ok++
				} else if len(misses) < 20 {
					misses = append(misses, fmt.Sprintf("%s at (%d,%d) chunk (%d,%d)", name, x, z, cx, cz))
				}
			}
			for _, l := range ev.loot(key) {
				if k, r, ok := evidenceKind(l.table); ok && ev.interior(l.x, l.z) {
					check(k.String(), k, l.x, l.z, r)
				}
			}
			for _, sp := range ev.Seeds[key].Spawners {
				if sp[3].(string) == "minecraft:cave_spider" && ev.interior(int(sp[0].(float64)), int(sp[2].(float64))) {
					check("mineshaft (cave spider spawner)", Mineshaft, int(sp[0].(float64)), int(sp[2].(float64)), 5)
				}
			}
			for _, b := range ev.Seeds[key].Boxes {
				name := b[0].(string)
				x0, z0, x1, z1 := int(b[1].(float64)), int(b[3].(float64)), int(b[4].(float64)), int(b[6].(float64))
				if !ev.interior(x0-16, z0-16) || !ev.interior(x1+16, z1+16) {
					continue // Reaching the scan's undecorated edge: cut there, its start may lie outside.
				}
				k := TrialChambers
				switch {
				case strings.Contains(name, "abandoned_camp"):
					k = AbandonedCamp
				case strings.Contains(name, "trail_ruins"):
					k = TrailRuins
				}
				// A volume holds or touches its start chunk (N1 2.6).
				hit := false
				for _, st := range s.startsNear(k, floorDiv((x0+x1)/2, 16), floorDiv((z0+z1)/2, 16), 9) {
					if st.CX >= floorDiv(x0, 16)-1 && st.CX <= floorDiv(x1, 16)+1 && st.CZ >= floorDiv(z0, 16)-1 && st.CZ <= floorDiv(z1, 16)+1 {
						hit = true
					}
				}
				n := k.String() + " volume"
				if got[n] == nil {
					got[n] = &tally{}
				}
				got[n].n++
				if hit {
					got[n].ok++
				} else {
					misses = append(misses, fmt.Sprintf("%s %v", name, b[1:]))
				}
			}
			names := make([]string, 0, len(got))
			for n := range got {
				names = append(names, n)
			}
			slices.Sort(names)
			for _, n := range names {
				t.Logf("%-32s %4d/%-4d near a start", n, got[n].ok, got[n].n)
				if got[n].ok != got[n].n {
					t.Errorf("%s: %d of %d not near any start we place", n, got[n].n-got[n].ok, got[n].n)
				}
			}
			for _, m := range misses {
				t.Log("  miss:", m)
			}
		})
	}
}

// TestTreasureStartsHaveChests counts the buried treasure starts we place inside the scanned area that
// BDS has no chest for, a start BDS rejects. It reports and does not fail: a missing chest can also be
// a chunk BDS did not decorate.
func TestTreasureStartsHaveChests(t *testing.T) {
	ev := loadEvidence(t)
	for _, seed := range testSeeds {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Parallel()
			_, s := worldFor(t, seed)
			chests := map[[2]int]bool{}
			lo, hi := ev.Scanned[0]+1, ev.Scanned[1]-1
			for _, l := range ev.loot(fmt.Sprint(seed)) {
				if strings.Contains(l.table, "buriedtreasure") {
					chests[[2]int{floorDiv(l.x, 16), floorDiv(l.z, 16)}] = true
				}
			}
			var extra [][2]int
			n := 0
			for cx := lo; cx <= hi; cx++ {
				for cz := lo; cz <= hi; cz++ {
					if _, ok := s.StartIn(BuriedTreasure, cx, cz); ok {
						n++
						if !chests[[2]int{cx, cz}] {
							extra = append(extra, [2]int{cx, cz})
						}
					}
				}
			}
			t.Logf("seed %d: %d treasure starts in the scanned area, %d chests, %d starts without a chest %v", seed, n, len(chests), len(extra), extra)
		})
	}
}
