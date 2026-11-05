package structures

import (
	"fmt"
	"strings"
	"testing"
)

// TestTrialChambersVolumes compares the union of our trial chamber pieces' boxes with BDS's merged
// structure volumes in the four measured worlds (clipped to the scan), for the held assembly.
func TestTrialChambersVolumes(t *testing.T) {
	ev := loadEvidence(t)
	lo, hi := ev.Scanned[0]*16, ev.Scanned[1]*16+15
	n, exact := 0, 0
	for _, seed := range testSeeds {
		_, s := worldFor(t, seed)
		for _, b := range ev.Seeds[fmt.Sprint(seed)].Boxes {
			if !strings.Contains(b[0].(string), "trial_chambers") {
				continue
			}
			want := box{int(b[1].(float64)), int(b[2].(float64)), int(b[3].(float64)), int(b[4].(float64)), int(b[5].(float64)), int(b[6].(float64))}
			if !ev.interior(want[0]-16, want[2]-16) || !ev.interior(want[3]+16, want[5]+16) {
				continue
			}
			n++
			var got []box
			for _, st := range s.startsNear(TrialChambers, floorDiv((want[0]+want[3])/2, 16), floorDiv((want[2]+want[5])/2, 16), 9) {
				ps := s.trialChambers(st)
				if len(ps) == 0 {
					continue
				}
				u := ps[0].box
				for _, p := range ps {
					for i := range 3 {
						u[i], u[i+3] = min(u[i], p.box[i]), max(u[i+3], p.box[i+3])
					}
				}
				u[0], u[2], u[3], u[5] = max(u[0], lo), max(u[2], lo), min(u[3], hi), min(u[5], hi)
				if u.intersects(want) {
					got = append(got, u)
					if u == want {
						exact++
					}
				}
			}
			t.Logf("seed %d: BDS volume %v, ours %v", seed, want, got)
		}
	}
	t.Logf("trial chamber volumes: %d of %d exact", exact, n)
}
