package structures

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// TestStructuredChunksDoNotDependOnOrder holds P21.1's claim that a chunk stays a pure function of the
// seed and its position with structures attached: structured bases are built, dropped and rebuilt at any
// time, and every decoration source reads its neighbours' structured bases. For chunks of a village, a
// ruined portal (its plan reads the terrain through GenerateBase), a mineshaft's spider corridor (the
// spawner mark replays the corridor's earlier chunks), trial chambers (the data-driven pass), a Nether
// fortress and seed 12345's (2, 28) ocean ruin cluster (piece heights read the terrain, overlaid templates
// on top of each other), each chunk is made alone on a fresh generator, and again after its 5 x 5 was made in reverse
// order by two racing workers on a generator with a 3 MB cache cap: both block layers and the block
// entities must be equal.
func TestStructuredChunksDoNotDependOnOrder(t *testing.T) {
	bd, d := installData(t)
	const seed = 100
	type target struct {
		name   string
		nether bool
		chunks [][2]int
		seed   int64 // 0: seed.
	}
	_, s := worldFor(t, seed)
	var targets []target
	pair := func(name string, nether bool, cx, cz int) {
		targets = append(targets, target{name, nether, [][2]int{{cx, cz}, {cx + 1, cz + 1}}, 0})
	}
	firstRegion := func(k Kind, ok func(Start) bool) (Start, bool) {
		for r := 0; r <= 3; r++ {
			for rx := -r; rx <= r; rx++ {
				for rz := -r; rz <= r; rz++ {
					if max(abs(rx), abs(rz)) != r {
						continue
					}
					if st, pass := s.regionStart(k, rx, rz); pass && ok(st) {
						return st, true
					}
				}
			}
		}
		return Start{}, false
	}
	if st, ok := firstRegion(Village, func(st Start) bool { return len(s.village(st)) > 1 }); ok {
		pair("village", false, st.CX, st.CZ)
	} else {
		t.Error("no village near the origin")
	}
	if st, ok := firstRegion(RuinedPortal, func(st Start) bool { return s.ruinedPortalAt(st.CX, st.CZ) != nil }); ok {
		pair("ruined portal", false, st.CX, st.CZ)
	} else {
		t.Error("no ruined portal near the origin")
	}
	if st, ok := firstRegion(TrialChambers, func(Start) bool { return true }); ok {
		pair("trial chambers", false, st.CX, st.CZ)
	} else {
		t.Error("no trial chambers near the origin")
	}
	// A spider corridor crossing chunks whose spawner is placed: the chunk holding it, and the corridor's
	// last chunk in corridorChunks' order, which replays the others for the "placed" mark.
	found := false
	sg, _ := worldFor(t, seed)
	for cx := -60; cx <= 60 && !found; cx++ {
		for cz := -60; cz <= 60 && !found; cz++ {
			if !s.mineshaftAt(cx, cz) {
				continue
			}
			for _, p := range s.mineshaftAtStart(cx, cz).pieces {
				cs := corridorChunks(p.b)
				if !p.spider || len(cs) < 2 {
					continue
				}
				for _, c := range cs {
					for _, be := range generate(sg, c[0], c[1]).BlockEntities {
						if be.Data["id"] == "MobSpawner" && be.Pos[0] >= p.b[0] && be.Pos[0] <= p.b[3] && be.Pos[2] >= p.b[2] && be.Pos[2] <= p.b[5] {
							found = true
						}
					}
					if found {
						last := cs[len(cs)-1]
						if last == c {
							last = cs[0]
						}
						targets = append(targets, target{"mineshaft spider corridor", false, [][2]int{c, last}, 0})
						break
					}
				}
				if found {
					break
				}
			}
		}
	}
	if !found {
		t.Error("no spider corridor crossing chunks near the origin")
	}
	_, ns := netherFor(t, seed)
	foundF := false
	for rx := -2; rx <= 2 && !foundF; rx++ {
		for rz := -2; rz <= 2 && !foundF; rz++ {
			if st, ok := ns.netherStart(rx, rz); ok && st.Kind == Fortress {
				pair("fortress", true, st.CX, st.CZ)
				foundF = true
			}
		}
	}
	if !foundF {
		t.Error("no fortress near the origin")
	}
	// The ruin2 pair of (2, 28)'s cluster (BDS's chests at y 33 and 34) is in chunk (2, 27).
	targets = append(targets, target{"ocean ruin", false, [][2]int{{2, 27}, {2, 28}}, 12345})

	mk := func(tg target) *worldgen.Generator {
		sd := tg.seed
		if sd == 0 {
			sd = seed
		}
		g := worldgen.NewWithData(sd, worldgen.ProfileBedrock, bd)
		if tg.nether {
			g = g.Nether()
		}
		g.SetStructures(New(g, d))
		return g
	}
	for _, tg := range targets {
		t.Run(tg.name, func(t *testing.T) {
			for _, k := range tg.chunks {
				want := generate(mk(tg), k[0], k[1])
				busy := mk(tg)
				busy.SetCacheBytes(3 << 20)
				var order [][2]int
				for dx := 2; dx >= -2; dx-- {
					for dz := 2; dz >= -2; dz-- {
						order = append(order, [2]int{k[0] + dx, k[1] + dz})
					}
				}
				jobs := make(chan [2]int)
				var mu sync.Mutex
				var raced *chunk.Column
				var wg sync.WaitGroup
				for range 2 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for c := range jobs {
							col := generate(busy, c[0], c[1])
							if c == k {
								mu.Lock()
								raced = col
								mu.Unlock()
							}
						}
					}()
				}
				for _, c := range order {
					jobs <- c
				}
				close(jobs)
				wg.Wait()
				again := generate(busy, k[0], k[1])
				for _, got := range []struct {
					how string
					col *chunk.Column
				}{{"inside its racing 5 x 5", raced}, {"after its 5 x 5", again}} {
					if diff := columnDiff(want, got.col); diff != "" {
						t.Errorf("%s chunk %v made %s differs from the chunk made alone: %s", tg.name, k, got.how, diff)
					}
				}
				t.Logf("%s chunk %v: %d block entities, equal", tg.name, k, len(want.BlockEntities))
			}
		})
	}
}

// columnDiff describes the first difference between two columns' blocks (both layers) and block
// entities, "" when they are equal.
func columnDiff(a, b *chunk.Column) string {
	r := a.Chunk.Range()
	for y := r.Min(); y <= r.Max(); y++ {
		for x := range uint8(16) {
			for z := range uint8(16) {
				for layer := range uint8(2) {
					if p, q := a.Chunk.Block(x, int16(y), z, layer), b.Chunk.Block(x, int16(y), z, layer); p != q {
						return fmt.Sprintf("layer %d at (%d,%d,%d): %d against %d", layer, x, y, z, p, q)
					}
				}
			}
		}
	}
	sorted := func(bes []chunk.BlockEntity) []chunk.BlockEntity {
		out := slices.Clone(bes)
		slices.SortFunc(out, func(p, q chunk.BlockEntity) int {
			return slices.Compare(p.Pos[:], q.Pos[:])
		})
		return out
	}
	if p, q := sorted(a.BlockEntities), sorted(b.BlockEntities); !reflect.DeepEqual(p, q) {
		return fmt.Sprintf("block entities %v against %v", p, q)
	}
	return ""
}

// corridorChunks are the chunks a corridor's box covers, in the order spiderPlacedBefore walks them
// (x then z, each from low to high): the last one replays all the others for the "placed" mark.
func corridorChunks(b box) [][2]int {
	var out [][2]int
	for ax := b[0] >> 4; ax <= b[3]>>4; ax++ {
		for az := b[2] >> 4; az <= b[5]>>4; az++ {
			out = append(out, [2]int{ax, az})
		}
	}
	return out
}
