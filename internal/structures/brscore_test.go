package structures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// TestDataJigsawBlocksMatchBDS scores the trail ruin and the camps against the study's block exports
// (golden-bedrock/structures/br_jigsaw, P9.6): every block of every exported chunk, y0..y1, with and
// without structures, and inside BDS's structure volumes. There is no unstable mask for these chunks,
// so the figures are raw.
func TestDataJigsawBlocksMatchBDS(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(goldenDir(t), "structures", "br_jigsaw", "*.json"))
	if len(files) == 0 {
		t.Skip("no br_jigsaw exports")
	}
	type rec struct {
		Seed           int64 `json:"seed"`
		CX, CZ, Y0, Y1 int
		Structure      string   `json:"structure"`
		Boxes          [][]int  `json:"boxes"`
		Palette        []string `json:"palette"`
		Blocks         []int    `json:"blocks"`
	}
	type tally struct{ same, bare, total, inSame, inBare, inTotal int }
	per := map[string]*tally{}
	conf := map[[2]string]int{}
	gens := map[int64][2]*worldgen.Generator{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var r rec
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		gp, ok := gens[r.Seed]
		if !ok {
			g, _ := worldFor(t, r.Seed)
			bd, _ := installData(t)
			gp = [2]*worldgen.Generator{g, worldgen.NewWithData(r.Seed, worldgen.ProfileBedrock, bd)}
			gens[r.Seed] = gp
		}
		mu.Unlock()
		wg.Add(1)
		sem <- struct{}{}
		go func(r rec) {
			defer func() { <-sem; wg.Done() }()
			with := generate(gp[0], r.CX, r.CZ).Chunk
			bare := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			gp[1].GenerateChunk(world.ChunkPos{int32(r.CX), int32(r.CZ)}, bare)
			key := strings.TrimPrefix(r.Structure, "minecraft:")
			mu.Lock()
			defer mu.Unlock()
			tl := per[key]
			if tl == nil {
				tl = &tally{}
				per[key] = tl
			}
			for i, id := range r.Blocks {
				y := r.Y0 + i/256
				lz, lx := i/16%16, i%16
				x, z := r.CX*16+lx, r.CZ*16+lz
				real := strings.TrimPrefix(r.Palette[id], "minecraft:")
				if k := strings.IndexAny(real, "[{"); k >= 0 {
					real = real[:k]
				}
				a, b := nameAt(with, x, y, z), nameAt(bare, x, y, z)
				inside := false
				for _, bx := range r.Boxes {
					if len(bx) >= 6 && x >= bx[0] && x <= bx[3] && y >= bx[1] && y <= bx[4] && z >= bx[2] && z <= bx[5] {
						inside = true
					}
				}
				tl.total++
				if a == real {
					tl.same++
				} else if inside {
					conf[[2]string{real, a}]++
				}
				if b == real {
					tl.bare++
				}
				if inside {
					tl.inTotal++
					if a == real {
						tl.inSame++
					}
					if b == real {
						tl.inBare++
					}
				}
			}
		}(r)
	}
	wg.Wait()
	pct := func(a, b int) float64 { return 100 * float64(a) / float64(max(b, 1)) }
	for k, v := range per {
		t.Logf("%-32s all blocks: with structures %.3f%%, without %.3f%% (%d); inside BDS volumes: with %.2f%%, without %.2f%% (%d)",
			k, pct(v.same, v.total), pct(v.bare, v.total), v.total, pct(v.inSame, v.inTotal), pct(v.inBare, v.inTotal), v.inTotal)
	}
	type kv struct {
		k [2]string
		n int
	}
	var all []kv
	for k, n := range conf {
		all = append(all, kv{k, n})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].n > all[j].n })
	var top []string
	for i := 0; i < len(all) && i < 12; i++ {
		top = append(top, all[i].k[0]+"<-"+all[i].k[1]+":"+itoa(all[i].n))
	}
	t.Logf("inside volumes, top confusions (BDS<-ours): %s", strings.Join(top, ", "))
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
