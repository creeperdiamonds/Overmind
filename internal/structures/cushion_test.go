package structures

import (
	"fmt"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// TestCushionsMatchBDS: the abandoned camps' cushions (NOTES-leftovers L33) against the measured worlds
// seed100 and seed1 (3 cushions each): every minecraft:cushion actor BDS stored in the export area
// (chunks -35..34) against ours from the camp starts in that area, by position, yaw and Variant.
func TestCushionsMatchBDS(t *testing.T) {
	for _, c := range []struct {
		seed  int64
		world string
	}{{100, "seed100"}, {1, "seed1"}} {
		t.Run(c.world, func(t *testing.T) {
			dir := bdsWorldDir(t, c.world)
			g, s := worldFor(t, c.seed)
			const lo, hi = -35, 34
			theirs := map[string]bool{}
			tmp := t.TempDir()
			copyTree(t, dir, tmp)
			db, err := mcdb.Open(tmp)
			if err != nil {
				t.Fatal(err)
			}
			for cx := lo; cx <= hi; cx++ {
				for cz := lo; cz <= hi; cz++ {
					col, err := db.LoadColumn(world.ChunkPos{int32(cx), int32(cz)}, world.Overworld)
					if err != nil || col == nil {
						continue
					}
					for _, e := range col.Entities {
						if e.Data["identifier"] == "minecraft:cushion" {
							theirs[cushionKey(e)] = true
						}
					}
				}
			}
			db.Close()
			ours := map[string]bool{}
			seen := map[[2]int]bool{}
			sp := spreads[AbandonedCamp]
			for rx := floorDiv(lo, sp.spacing); rx <= floorDiv(hi, sp.spacing); rx++ {
				for rz := floorDiv(lo, sp.spacing); rz <= floorDiv(hi, sp.spacing); rz++ {
					st, ok := s.regionStart(AbandonedCamp, rx, rz)
					if !ok {
						continue
					}
					for cx := max(lo, st.CX-dataReach); cx <= min(hi, st.CX+dataReach); cx++ {
						for cz := max(lo, st.CZ-dataReach); cz <= min(hi, st.CZ+dataReach); cz++ {
							if seen[[2]int{cx, cz}] {
								continue
							}
							seen[[2]int{cx, cz}] = true
							col := generate(g, cx, cz)
							for _, e := range col.Entities {
								ours[cushionKey(e)] = true
							}
						}
					}
				}
			}
			both := 0
			for k := range theirs {
				if ours[k] {
					both++
				} else {
					t.Errorf("BDS's cushion %s is not ours", k)
				}
			}
			for k := range ours {
				if !theirs[k] {
					t.Errorf("our cushion %s is not BDS's", k)
				}
			}
			keys := slices.Sorted(func(f func(string) bool) {
				for k := range theirs {
					if !f(k) {
						return
					}
				}
			})
			t.Logf("%s: BDS %d cushions, ours %d, %d the same (position, yaw, Variant): %v", c.world, len(theirs), len(ours), both, keys)
		})
	}
}

// cushionKey is a cushion's position, yaw and Variant.
func cushionKey(e chunk.Entity) string {
	f := func(k string) []float64 {
		var out []float64
		switch l := e.Data[k].(type) {
		case []float32:
			for _, v := range l {
				out = append(out, float64(v))
			}
		case []any:
			for _, v := range l {
				if x, ok := v.(float32); ok {
					out = append(out, float64(x))
				}
			}
		}
		return out
	}
	return fmt.Sprintf("%v yaw %v variant %v", f("Pos"), f("Rotation")[:1], e.Data["Variant"])
}

// TestCushionsLiveInTheWorld: seed 100's camp at (220, 92, -194) through a Dragonfly world with a leveldb
// provider: its two cushions exist as minecraft:cushion entities once the chunk is generated, keep their
// colour and yaw, and come back from the saved world after a restart.
func TestCushionsLiveInTheWorld(t *testing.T) {
	g, _ := worldFor(t, 100)
	dir := t.TempDir()
	want := map[string]bool{"[220.5 92.9375 -194.5] yaw [-180] variant 2": true, "[222.5 92.9375 -194.5] yaw [-90] variant 2": true}
	for run := range 2 {
		db, err := mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		w := world.Config{Provider: db, Generator: g, Entities: entity.DefaultRegistry, Synchronous: true}.New()
		got := map[string]bool{}
		<-w.Do(func(tx *world.Tx) {
			tx.Block(cube.Pos{220, 92, -194})
			for e := range tx.EntitiesWithin(cube.Box(200, 80, -210, 240, 110, -180)) {
				if e.H().Type() != entity.CushionType {
					continue
				}
				v := e.(*entity.Ent).Behaviour().(*entity.CushionBehaviour).Variant()
				p, r := e.Position(), e.Rotation()
				got[fmt.Sprintf("%v yaw [%v] variant %d", []float64{p[0], p[1], p[2]}, r[0], v)] = true
			}
		}).Done()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("run %d: cushions %v, want %v", run, got, want)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
