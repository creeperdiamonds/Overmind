package worldgen

import "sync"

// SeaAnemoneFeature (NOTES-leftovers L5, place 0x540cca0, read; not simulated or scored there): the coral
// plants and floor fans of warm oceans, placed by the bds60 rule warm_ocean_after_surface_sea_anemone_feature
// (20 iterations, y 62). On the rule's feature random:
//   - from the position, step down while the block below is not solid and y is above the floor (the sea
//     level field 63 - 15 = 48), with no draws;
//   - the block must be water, the block below solid, y above the floor, and a coral plant must survive
//     there (T48's test on the block below); else nothing is drawn;
//   - k = nextInt(4): k != 0 (3 in 4) a coral plant nextInt(5); k == 0 a floor fan nextInt(5) with
//     coral_fan_direction nextInt(2).
// Labelled: the plant list's order is assumed tube, brain, bubble, fire, horn like the fan list T52 read;
// "solid" is our bSolid (isSolidBlockingBlock), the region test's own body (vt+0x1b0) not traced; the
// plant's survive test is taken as the solid block below already required.

const anemoneFloor = 63 - 15

var anemoneStates = sync.OnceValue(func() (out struct {
	plants, fans [5]bs
	fans1        [5]bs
}) { for i, k := range [5]string{"tube", "brain", "bubble", "fire", "horn"} {
	out.plants[i] = stateOf(k + "_coral[waterlogged=true]")
	out.fans[i] = stateOf(k + "_coral_fan[waterlogged=true]")
	out.fans1[i] = stateOf(k + "_coral_fan[coral_fan_direction=1,waterlogged=true]")
}; return out })

func seaAnemone(d *decoration, r *bmt, p pos) bool {
	for !bSolid(d.block(p.x, p.y-1, p.z)) && p.y > anemoneFloor {
		p.y--
	}
	if !bWaterMat(d.block(p.x, p.y, p.z)) || !bSolid(d.block(p.x, p.y-1, p.z)) || p.y <= anemoneFloor {
		return false
	}
	st := anemoneStates()
	if r.nextIntN(4) != 0 {
		d.set(p.x, p.y, p.z, st.plants[r.nextIntN(5)])
		return true
	}
	i := r.nextIntN(5)
	if r.nextIntN(2) == 0 {
		d.set(p.x, p.y, p.z, st.fans[i])
	} else {
		d.set(p.x, p.y, p.z, st.fans1[i])
	}
	return true
}
