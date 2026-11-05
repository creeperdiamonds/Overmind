package structures

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// TestVillageChestsMatchBDS: every village chest of the four measured worlds, our chest at BDS's position
// with BDS's loot seed.
func TestVillageChestsMatchBDS(t *testing.T) {
	ev := loadEvidence(t)
	n, ok := 0, 0
	for _, seed := range testSeeds {
		g, _ := worldFor(t, seed)
		for _, l := range ev.loot(fmt.Sprint(seed)) {
			if !strings.Contains(l.table, "chests/village/") {
				continue
			}
			n++
			col := generate(g, floorDiv(l.x, 16), floorDiv(l.z, 16))
			var got []string
			hit := false
			for _, be := range col.BlockEntities {
				got = append(got, fmt.Sprintf("%v %v %v", be.Pos, be.Data["LootTable"], be.Data["LootTableSeed"]))
				if be.Pos == [3]int{l.x, l.y, l.z} && be.Data["LootTable"] == l.table && be.Data["LootTableSeed"] == l.seed {
					hit = true
				}
			}
			if hit {
				ok++
			} else {
				t.Errorf("seed %d: BDS %s at (%d,%d,%d) seed %d; ours %v", seed, l.table, l.x, l.y, l.z, l.seed, got)
			}
		}
	}
	t.Logf("village chests: %d of %d exact (position, table, loot seed)", ok, n)
}

// TestVillageWindowMatchesBDS scores seed 22's village (start (-11, 6), the study's village window) in
// its piece boxes against the window export, with and without structures.
func TestVillageWindowMatchesBDS(t *testing.T) {
	w := loadWorld(t, filepath.Join(goldenDir(t), "whole", "structures", "village_22.bin.gz"))
	g, s := worldFor(t, 22)
	st, ok := s.StartIn(Village, -11, 6)
	if !ok {
		t.Fatal("no village start at (-11, 6)")
	}
	pieces := s.village(st)
	var boxes []box
	names := map[string]int{}
	for _, p := range pieces {
		if p.elem.template != nil {
			b := p.box
			if p.elem.terrain {
				b[1], b[4] = b[1]-8, b[4]+8
			}
			boxes = append(boxes, b)
			names[strings.SplitN(p.elem.name, "_", 2)[0]]++
		}
	}
	t.Logf("%d pieces (%s village), start %v %v", len(pieces), st.Piece, pieces[0].elem.name, pieces[0].box)
	bd, _ := installData(t)
	bare := worldgen.NewWithData(22, worldgen.ProfileBedrock, bd)
	var same, sameBare, total int
	conf := map[[2]string]int{}
	for c := range w.chunks {
		col := generate(g, c[0], c[1])
		sm, tt, cf := boxScore(w, col.Chunk, c[0], c[1], boxes)
		plain := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
		bare.GenerateChunk(world.ChunkPos{int32(c[0]), int32(c[1])}, plain)
		smb, _, _ := boxScore(w, plain, c[0], c[1], boxes)
		same, sameBare, total = same+sm, sameBare+smb, total+tt
		for k, v := range cf {
			conf[k] += v
		}
	}
	t.Logf("blocks in piece boxes (window, stable mask): with structures %d/%d = %.2f%%, without %.2f%%", same, total,
		100*float64(same)/float64(max(total, 1)), 100*float64(sameBare)/float64(max(total, 1)))
	t.Logf("  top confusions (BDS<-ours): %s", topConfusions(conf, 14))
}
