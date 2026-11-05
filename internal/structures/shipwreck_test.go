package structures

import (
	"fmt"
	"strings"
	"testing"
)

// TestShipwrecksMatchBDS: every shipwreck chest of the four measured worlds against ours: position (x, z
// as P9.6 validated, then y), table and loot seed. Reported; it fails only when a chest has no chest of
// ours at its x and z with its table.
func TestShipwrecksMatchBDS(t *testing.T) {
	ev := loadEvidence(t)
	n, xz, xyz, seed := 0, 0, 0, 0
	for _, sd := range testSeeds {
		g, _ := worldFor(t, sd)
		for _, l := range ev.loot(fmt.Sprint(sd)) {
			if !strings.Contains(l.table, "shipwreck") {
				continue
			}
			n++
			col := generate(g, floorDiv(l.x, 16), floorDiv(l.z, 16))
			found := false
			for _, be := range col.BlockEntities {
				if be.Pos[0] != l.x || be.Pos[2] != l.z || be.Data["LootTable"] != l.table {
					continue
				}
				found = true
				xz++
				if be.Pos[1] == l.y {
					xyz++
				}
				if be.Data["LootTableSeed"] == l.seed {
					seed++
				}
				t.Logf("seed %d %s: BDS (%d,%d,%d) %d, ours y %d seed %v", sd, l.table, l.x, l.y, l.z, l.seed, be.Pos[1], be.Data["LootTableSeed"])
			}
			if !found {
				t.Errorf("seed %d %s at (%d,%d,%d): no chest of ours at its x, z", sd, l.table, l.x, l.y, l.z)
			}
		}
	}
	t.Logf("shipwreck chests: %d of %d at BDS's x, z with its table; %d also at its y; %d with its loot seed", xz, n, xyz, seed)
}
