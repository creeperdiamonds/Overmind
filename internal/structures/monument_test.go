package structures

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestMonumentStart checks P151's start on seed 32 (-12,6): start seed 0xFFFFFFD6, direction west.
func TestMonumentStart(t *testing.T) {
	_, s := worldFor(t, 32)
	if _, ok := s.StartIn(Monument, -12, 6); !ok {
		t.Fatal("(-12,6) is not a monument start on seed 32")
	}
	if seed := s.monumentStartSeed(-12, 6); seed != 0xFFFFFFD6 {
		t.Fatalf("start seed %#x, want 0xFFFFFFD6", seed)
	}
	m := s.monumentAtStart(-12, 6)
	if m.building.o != oW {
		t.Fatalf("direction %d, want west", m.building.o)
	}
	t.Logf("%d children, building box %v", len(m.children), m.building.b)
}

// TestMonumentMatchesBDS scores seed 32's (-12,6) monument against BDS (P151: 13,265 of 13,265 non-water
// cells, 67 of 67 wet sponges): every cell the monument leaves holding something other than water or
// air, on the four runs' stable cells.
func TestMonumentMatchesBDS(t *testing.T) {
	g, s := worldFor(t, 32)
	m := s.monumentAtStart(-12, 6)
	b := m.building.b
	w := loadBDSRunsIn(t, "st_ocean_monument_32", []string{"big", "strips", "rstrips", "xrev"}, world.Overworld, true, b[0]>>4-1, b[3]>>4+1, b[2]>>4-1, b[5]>>4+1)
	// Structures are written into a chunk's terrain when a neighbour's decoration first needs it (P21.1),
	// so the writes are gathered over the whole run.
	var mu sync.Mutex
	written := map[[3]int]uint32{}
	s.trace = func(x, y, z int, rid uint32) { mu.Lock(); written[[3]int{x, y, z}] = rid; mu.Unlock() }
	defer func() { s.trace = nil }()
	same, total, spongeOurs, spongeBoth := 0, 0, 0, 0
	conf := map[[2]string]int{}
	cols := map[[2]int]*chunk.Column{}
	for cx := (b[0] - 5) >> 4; cx <= (b[3]+5)>>4; cx++ {
		for cz := (b[2] - 5) >> 4; cz <= (b[5]+5)>>4; cz++ {
			if w.chunks[[2]int{cx, cz}] != nil {
				cols[[2]int{cx, cz}] = generate(g, cx, cz)
			}
		}
	}
	for cx := (b[0] - 5) >> 4; cx <= (b[3]+5)>>4; cx++ {
		for cz := (b[2] - 5) >> 4; cz <= (b[5]+5)>>4; cz++ {
			bc := w.chunks[[2]int{cx, cz}]
			if bc == nil {
				continue
			}
			col := cols[[2]int{cx, cz}]
			for p, rid := range written {
				if p[0]>>4 != cx || p[2]>>4 != cz {
					continue
				}
				n := ridName(rid)
				if n == "water" || n == "air" {
					continue
				}
				cell := cellOf(p[0], p[1], p[2])
				if bc.masked(cell) {
					continue
				}
				mine, real := nameAt(col.Chunk, p[0], p[1], p[2]), w.names[bc.ids[cell]]
				total++
				if mine == real {
					same++
				} else {
					conf[[2]string{real, mine}]++
				}
				if mine == "wet_sponge" {
					spongeOurs++
					if real == mine {
						spongeBoth++
					}
				}
			}
		}
	}
	spongeBDS := 0
	for k, c := range w.chunks {
		for y := b[1]; y <= b[4]; y++ {
			for i := range 256 {
				x, z := k[0]*16+i&15, k[1]*16+i>>4
				if x < b[0] || x > b[3] || z < b[2] || z > b[5] {
					continue
				}
				if w.names[c.ids[cellOf(x, y, z)]] == "wet_sponge" {
					spongeBDS++
				}
			}
		}
	}
	t.Logf("seed 32 monument: %d/%d = %.2f%% of the non-water cells it writes (study 13,265/13,265); wet sponges %d of BDS's %d at BDS's place (ours %d); %s",
		same, total, 100*float64(same)/float64(max(total, 1)), spongeBoth, spongeBDS, spongeOurs, topConfusions(conf, 12))
	if total == 0 || same*1000 < total*995 {
		t.Errorf("monument cells %d/%d below 99.5%%", same, total)
	}
}
