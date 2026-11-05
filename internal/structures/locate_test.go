package structures

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// BDS `locate structure` answers from 7 origins on the four measured seeds (golden-bedrock/structures/
// locate.txt, the study's p3/loc_all.txt; N2 phase 3 round 2). Every structure BDS names must be a start
// we place at that chunk, and an eye of ender from each origin must lead where BDS's locate does.

type located struct {
	seed   int64
	kind   string
	ox, oz int
	x, z   int
	none   bool
}

var locLine = regexp.MustCompile(`^### (\S+) (-?\d+) (-?\d+) -> .*?(?:is at block (-?\d+), \(y\?\), (-?\d+)|(No valid))`)

func loadLocate(t *testing.T) []located {
	f, err := os.Open(filepath.Join(goldenDir(t), "structures", "locate.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []located
	var seed int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if s, ok := strings.CutPrefix(l, "SEED "); ok {
			seed, _ = strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			continue
		}
		m := locLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		e := located{seed: seed, kind: m[1], none: m[6] != ""}
		e.ox, _ = strconv.Atoi(m[2])
		e.oz, _ = strconv.Atoi(m[3])
		e.x, _ = strconv.Atoi(m[4])
		e.z, _ = strconv.Atoi(m[5])
		out = append(out, e)
	}
	return out
}

var locateKinds = map[string]Kind{"ancient_city": AncientCity, "buried_treasure": BuriedTreasure, "mansion": Mansion,
	"monument": Monument, "pillager_outpost": Outpost, "ruined_portal": RuinedPortal, "village": Village,
	"shipwreck": Shipwreck, "trail_ruins": TrailRuins, "trial_chambers": TrialChambers, "mineshaft": Mineshaft}

func TestStartsMatchLocate(t *testing.T) {
	locs := loadLocate(t)
	worlds := map[int64]*Structures{}
	type tally struct{ n, ok int }
	got := map[string]*tally{}
	for _, l := range locs {
		k, ok := locateKinds[l.kind]
		if !ok || l.none {
			continue
		}
		s := worlds[l.seed]
		if s == nil {
			_, s = worldFor(t, l.seed)
			worlds[l.seed] = s
		}
		if got[l.kind] == nil {
			got[l.kind] = &tally{}
		}
		got[l.kind].n++
		cx, cz := floorDiv(l.x, 16), floorDiv(l.z, 16)
		found := false
		if k == TrialChambers || k == TrailRuins {
			// The data-driven sets' locate names a piece position, not the start chunk's middle: the
			// start must be within the measured reach (N1 2.6).
			found = len(s.startsNear(k, cx, cz, 8)) > 0
		} else {
			_, found = s.StartIn(k, cx, cz)
		}
		if found {
			got[l.kind].ok++
		} else {
			t.Errorf("seed %d: BDS locates %s at (%d,%d), chunk (%d,%d): no start of ours there", l.seed, l.kind, l.x, l.z, cx, cz)
		}
	}
	for k, v := range got {
		t.Logf("%-16s %3d/%-3d located answers are starts of ours", k, v.ok, v.n)
	}
}

// TestStrongholdsMatchLocate: every stronghold BDS's locate names (block (cx*16+2, cz*16+2) of its start
// chunk) must be a stronghold start of ours. Which one locate picks is not plain nearest (N2 phase 8:
// neither Euclidean nor Chebyshev chunk distance explains every answer, its search order was not read),
// so the eye of ender's choice is only reported against it.
func TestStrongholdsMatchLocate(t *testing.T) {
	n, ok, same := 0, 0, 0
	worlds := map[int64]*Structures{}
	seen := map[[3]int]bool{}
	for _, l := range loadLocate(t) {
		if l.kind != "stronghold" || l.none {
			continue
		}
		s := worlds[l.seed]
		if s == nil {
			_, s = worldFor(t, l.seed)
			worlds[l.seed] = s
		}
		n++
		if at, found := s.NearestStronghold(cube.Pos{l.ox, 64, l.oz}); found && at[0] == l.x && at[2] == l.z {
			same++
		}
		key := [3]int{int(l.seed), l.x, l.z}
		if seen[key] {
			continue
		}
		seen[key] = true
		if s.strongholdAt(floorDiv(l.x, 16), floorDiv(l.z, 16)) {
			ok++
		} else {
			t.Errorf("seed %d: BDS locates a stronghold at (%d,%d), chunk (%d,%d): not a start of ours (rings %v)",
				l.seed, l.x, l.z, floorDiv(l.x, 16), floorDiv(l.z, 16), s.rings())
		}
	}
	t.Logf("%d of %d distinct located strongholds are starts of ours; the eye of ender picks locate's answer from %d of %d origins", ok, len(seen), same, n)
}
