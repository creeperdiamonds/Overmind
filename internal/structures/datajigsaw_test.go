package structures

import (
	"fmt"
	"strings"
	"testing"
)

// TestTrailRuinsStubsMatchLocate: BDS's locate names the trail ruins' generation stub (BR: "every BDS
// locate answer is Java's generation stub"), so a start of ours must have its stub's x and z there.
func TestTrailRuinsStubsMatchLocate(t *testing.T) {
	n, ok := 0, 0
	worlds := map[int64]*Structures{}
	for _, l := range loadLocate(t) {
		if l.kind != "trail_ruins" || l.none {
			continue
		}
		s := worlds[l.seed]
		if s == nil {
			_, s = worldFor(t, l.seed)
			worlds[l.seed] = s
		}
		n++
		var got []string
		hit := false
		for _, st := range s.startsNear(TrailRuins, floorDiv(l.x, 16), floorDiv(l.z, 16), 8) {
			ps := s.dataJigsaw(st)
			if len(ps) == 0 {
				continue
			}
			stub := stubOf(ps)
			got = append(got, fmt.Sprint(stub))
			if stub[0] == l.x && stub[2] == l.z {
				hit = true
			}
		}
		if hit {
			ok++
		} else {
			t.Errorf("seed %d: BDS locates trail ruins at (%d,%d); our stubs near it %v", l.seed, l.x, l.z, got)
		}
	}
	t.Logf("trail ruins: %d of %d locate answers are our stubs", ok, n)
}

// TestDataJigsawVolumesMatchBDS compares the union of our pieces' boxes with BDS's structure volumes
// (chunk tag 0x77, merged per structure by the study) for the trail ruins and abandoned camps in the
// four measured worlds.
func TestDataJigsawVolumesMatchBDS(t *testing.T) {
	ev := loadEvidence(t)
	for _, seed := range testSeeds {
		_, s := worldFor(t, seed)
		for _, b := range ev.Seeds[fmt.Sprint(seed)].Boxes {
			name := b[0].(string)
			k := AbandonedCamp
			switch {
			case strings.Contains(name, "trail_ruins"):
				k = TrailRuins
			case !strings.Contains(name, "abandoned_camp"):
				continue
			}
			want := box{int(b[1].(float64)), int(b[2].(float64)), int(b[3].(float64)), int(b[4].(float64)), int(b[5].(float64)), int(b[6].(float64))}
			// BDS's volumes stop at the scanned area (chunks -38..37); ours are clipped the same way.
			lo, hi := ev.Scanned[0]*16, ev.Scanned[1]*16+15
			clip := func(b box) box {
				b[0], b[2] = max(b[0], lo), max(b[2], lo)
				b[3], b[5] = min(b[3], hi), min(b[5], hi)
				return b
			}
			var best box
			found := false
			for _, st := range s.startsNear(k, floorDiv((want[0]+want[3])/2, 16), floorDiv((want[2]+want[5])/2, 16), 8) {
				ps := s.dataJigsaw(st)
				if len(ps) == 0 {
					continue
				}
				u := ps[0].box
				for _, p := range ps {
					for i := range 3 {
						u[i], u[i+3] = min(u[i], p.box[i]), max(u[i+3], p.box[i+3])
					}
				}
				if u = clip(u); u.intersects(want) {
					best, found = u, true
				}
			}
			switch {
			case !found:
				t.Errorf("seed %d %s: BDS volume %v, no structure of ours there", seed, name, want)
			case best != want:
				t.Errorf("seed %d %s: BDS volume %v, ours %v", seed, name, want, best)
			default:
				t.Logf("seed %d %s: volume %v exact", seed, name, want)
			}
		}
	}
}
