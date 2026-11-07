package structures

import (
	"fmt"
	"sync"
	"testing"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// TestMineshaftStartsCoverTheSpan: msStarts names exactly the starts whose read span (msSpanLow ..
// msSpanHigh blocks from the start chunk's corner) meets a chunk.
func TestMineshaftStartsCoverTheSpan(t *testing.T) {
	for c := -40; c <= 40; c++ {
		lo, hi := msStarts(c)
		for sx := c - 12; sx <= c+12; sx++ {
			meets := sx*16+msSpanLow <= c*16+15 && sx*16+msSpanHigh >= c*16
			if in := sx >= lo && sx <= hi; in != meets {
				t.Fatalf("chunk %d: start %d listed %v, its span meets the chunk %v", c, sx, in, meets)
			}
			if meets && abs(sx-c) > msReach {
				t.Fatalf("chunk %d: start %d is past msReach", c, sx)
			}
		}
	}
}

// TestPiecesStayWithinReach assembles thousands of starts of every assembled structure, over many seeds
// and regions, and checks that every piece box lies within the structure's reach: the chunks around its
// start that a chunk looks at. A piece past the reach would be cut off at the reach's edge, where BDS
// writes it whole. Starts are the regions' candidate chunks (or any chunk, for mineshafts and
// strongholds) without the biome tests: the pieces depend only on the start's random and the terrain
// heights, so this samples the same layouts real starts get, more of them. It logs the farthest piece
// of each structure, the figure the measured reach constants come from.
func TestPiecesStayWithinReach(t *testing.T) {
	bd, d := installData(t)
	seeds := []int64{1, 2, 3, 100, 12345, 987654321}
	type job struct {
		seed   int64
		rx, rz int
		i      int
	}
	// run assembles one structure family over n x n regions of each seed (with mk the seed's
	// Structures), asking each job for its start chunk and boxes; it reports the farthest chunk any box
	// reaches from its start.
	run := func(t *testing.T, name string, reach, n int, mk func(seed int64) *Structures,
		boxes func(s *Structures, j job) (cx, cz int, bs []box, ok bool)) {
		var mu sync.Mutex
		worst, starts, pieces := 0, 0, 0
		hist := map[int]int{} // Starts by their farthest piece, in chunks.
		var worstAt string
		for _, seed := range seeds {
			s := mk(seed)
			jobs := make(chan job)
			var wg sync.WaitGroup
			for range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := range jobs {
						cx, cz, bs, ok := boxes(s, j)
						if !ok {
							continue
						}
						far, at := 0, ""
						for _, b := range bs {
							for _, v := range [4]int{cx - b[0]>>4, b[3]>>4 - cx, cz - b[2]>>4, b[5]>>4 - cz} {
								if v > far {
									far, at = v, fmt.Sprintf("seed %d start (%d,%d) box %v", seed, cx, cz, b)
								}
							}
						}
						mu.Lock()
						starts++
						pieces += len(bs)
						hist[far]++
						if far > worst {
							worst, worstAt = far, at
						}
						mu.Unlock()
					}
				}()
			}
			i := 0
			for rx := -n / 2; rx < n-n/2; rx++ {
				for rz := -n / 2; rz < n-n/2; rz++ {
					jobs <- job{seed, rx, rz, i}
					i++
				}
			}
			close(jobs)
			wg.Wait()
		}
		t.Logf("%s: %d starts, %d pieces; farthest piece %d chunks from its start (reach %d): %s; starts by farthest chunk %v",
			name, starts, pieces, worst, reach, worstAt, hist)
		if starts == 0 {
			t.Fatalf("%s: no starts assembled", name)
		}
		if worst > reach {
			t.Errorf("%s: a piece reaches %d chunks from its start, past the reach %d", name, worst, reach)
		}
	}
	overworld := func(seed int64) *Structures {
		g := worldgen.NewWithData(seed, worldgen.ProfileBedrock, bd)
		return New(g, d)
	}
	nether := func(seed int64) *Structures {
		return New(worldgen.NewWithData(seed, worldgen.ProfileBedrock, bd).Nether(), d)
	}
	end := func(seed int64) *Structures {
		return New(worldgen.NewWithData(seed, worldgen.ProfileBedrock, bd).End(), d)
	}
	candidate := func(s *Structures, k Kind, j job) Start {
		r := worldgen.NewBedrockRandom(0)
		cx, cz := spreads[k].candidate(s, j.rx, j.rz, r)
		return Start{Kind: k, CX: cx, CZ: cz, rseed: r.Seeded()}
	}
	jigsawBoxes := func(pieces []*piece) []box {
		var out []box
		for _, p := range pieces {
			out = append(out, p.box)
		}
		return out
	}
	villageKinds := []string{"default", "desert", "savanna", "taiga", "ice"}

	t.Run("village", func(t *testing.T) {
		run(t, "village", villageReach, 24, overworld, func(s *Structures, j job) (int, int, []box, bool) {
			st := candidate(s, Village, j)
			st.Piece = villageKinds[j.i%len(villageKinds)]
			ps := s.village(st)
			return st.CX, st.CZ, jigsawBoxes(ps), len(ps) > 0
		})
	})
	t.Run("ancient_city", func(t *testing.T) {
		run(t, "ancient city", ancientReach, 14, overworld, func(s *Structures, j job) (int, int, []box, bool) {
			st := candidate(s, AncientCity, j)
			ps := s.ancientCity(st)
			return st.CX, st.CZ, jigsawBoxes(ps), len(ps) > 0
		})
	})
	t.Run("trial_chambers", func(t *testing.T) {
		run(t, "trial chambers", trialReach, 8, overworld, func(s *Structures, j job) (int, int, []box, bool) {
			st := candidate(s, TrialChambers, j)
			ps := s.trialChambers(st)
			return st.CX, st.CZ, jigsawBoxes(ps), len(ps) > 0
		})
	})
	t.Run("trail_ruins", func(t *testing.T) {
		run(t, "trail ruins", dataReach, 14, overworld, func(s *Structures, j job) (int, int, []box, bool) {
			st := candidate(s, TrailRuins, j)
			st.Piece = "minecraft:trail_ruins"
			ps := s.dataJigsaw(st)
			return st.CX, st.CZ, jigsawBoxes(ps), len(ps) > 0
		})
	})
	t.Run("abandoned_camp", func(t *testing.T) {
		run(t, "abandoned camp", dataReach, 14, overworld, func(s *Structures, j job) (int, int, []box, bool) {
			st := candidate(s, AbandonedCamp, j)
			if len(s.campStructures) == 0 {
				return 0, 0, nil, false
			}
			st.Piece = s.campStructures[j.i%len(s.campStructures)]
			ps := s.dataJigsaw(st)
			return st.CX, st.CZ, jigsawBoxes(ps), len(ps) > 0
		})
	})
	// Mineshafts and strongholds may start in any chunk: j's region is taken as a chunk.
	t.Run("mineshaft", func(t *testing.T) {
		run(t, "mineshaft", msReach, 30, overworld, func(s *Structures, j job) (int, int, []box, bool) {
			cx, cz := j.rx*7, j.rz*7
			m := s.mineshaftAtStart(cx, cz)
			var bs []box
			for _, p := range m.pieces {
				bs = append(bs, p.b)
				if p.b[0] < cx*16+msSpanLow || p.b[3] > cx*16+msSpanHigh || p.b[2] < cz*16+msSpanLow || p.b[5] > cz*16+msSpanHigh {
					t.Errorf("seed %d mineshaft (%d,%d): piece %v outside the read span %d..%d", j.seed, cx, cz, p.b, msSpanLow, msSpanHigh)
				}
			}
			return cx, cz, bs, true
		})
	})
	t.Run("stronghold", func(t *testing.T) {
		run(t, "stronghold", strongholdReach, 20, overworld, func(s *Structures, j job) (int, int, []box, bool) {
			cx, cz := j.rx*11, j.rz*11
			var bs []box
			for _, p := range s.strongholdAtStart(cx, cz).pieces {
				bs = append(bs, p.b)
			}
			return cx, cz, bs, true
		})
	})
	t.Run("mansion", func(t *testing.T) {
		run(t, "mansion", wmReach, 6, overworld, func(s *Structures, j job) (int, int, []box, bool) {
			// mansionAt without its biome test.
			st := candidate(s, Mansion, j)
			m := s.assembleMansion(st)
			if m == nil {
				return st.CX, st.CZ, nil, false
			}
			var bs []box
			for _, p := range m.pieces {
				bs = append(bs, p.box)
			}
			return st.CX, st.CZ, bs, true
		})
	})
	t.Run("ocean_ruin", func(t *testing.T) {
		// Real starts (the biome test runs): a ruin's layout reads the biome.
		run(t, "ocean ruin", oceanRuinReach, 24, overworld, func(s *Structures, j job) (int, int, []box, bool) {
			st := candidate(s, OceanRuin, j)
			var bs []box
			for _, p := range s.oceanRuinAt(st.CX, st.CZ) {
				bs = append(bs, p.xzBox())
			}
			return st.CX, st.CZ, bs, len(bs) > 0
		})
	})
	t.Run("fortress", func(t *testing.T) {
		run(t, "fortress", fortressReach, 14, nether, func(s *Structures, j job) (int, int, []box, bool) {
			st := candidate(s, Fortress, j)
			var bs []box
			// The read span: a foot up to nfFar from the start piece's corner (cx*16+2) gets a piece of at most
			// 19 blocks; that piece's exit, past nfFar, an end filler of 8 (fortress.go).
			const span = nfFar + 18 + 1 + 7
			x0, z0 := st.CX*16+2, st.CZ*16+2
			for _, p := range s.fortressAtStart(st).pieces {
				bs = append(bs, p.b)
				if p.b[0] < x0-span || p.b[3] > x0+span || p.b[2] < z0-span || p.b[5] > z0+span {
					t.Errorf("seed %d fortress (%d,%d): piece %v outside the read span %d", j.seed, st.CX, st.CZ, p.b, span)
				}
			}
			return st.CX, st.CZ, bs, true
		})
	})
	t.Run("bastion", func(t *testing.T) {
		run(t, "bastion", bastionReach, 14, nether, func(s *Structures, j job) (int, int, []box, bool) {
			st := candidate(s, Bastion, j)
			ps := s.bastionAtStart(st)
			return st.CX, st.CZ, jigsawBoxes(ps), len(ps) > 0
		})
	})
	t.Run("end_city", func(t *testing.T) {
		// endCityAt without the height test: the layout does not depend on the start's height.
		run(t, "end city", endCityReach, 18, end, func(s *Structures, j job) (int, int, []box, bool) {
			r := worldgen.NewBedrockRandom(0)
			cx, cz := spreads[EndCity].candidate(s, j.rx, j.rz, r)
			c := s.assembleEndCity(cx, cz, endCityRotation(cx, cz), 70, r)
			if c == nil {
				return 0, 0, nil, false
			}
			var bs []box
			for _, p := range c.pieces {
				bs = append(bs, p.box)
			}
			return cx, cz, bs, true
		})
	})
}
