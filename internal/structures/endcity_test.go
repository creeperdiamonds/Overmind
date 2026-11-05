package structures

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestEndCityMatchesBDS: seed 100's (5,65) city (P181: 119/119 pieces, 100% of 35,417 cells; P182: 7/7
// chest loot seeds), world p15/end_100.
func TestEndCityMatchesBDS(t *testing.T) {
	g, s := endFor(t, 100)
	c := s.endCityAt(5, 65)
	if c == nil {
		t.Fatal("(5,65) is not an end city start")
	}
	var boxes []box
	for _, p := range c.pieces {
		b := p.box // The collision box is one longer on the far side in x and z (P181).
		switch p.rot {
		case 0:
			b[3], b[5] = b[3]-1, b[5]-1
		case 1:
			b[0], b[5] = b[0]+1, b[5]-1
		case 2:
			b[0], b[2] = b[0]+1, b[2]+1
		default:
			b[3], b[2] = b[3]-1, b[2]+1
		}
		boxes = append(boxes, b)
	}
	all := boundsOf(boxes)
	t.Logf("seed 100 end city: %d pieces (study 119), bounds %v", len(c.pieces), all)
	if len(c.pieces) != 119 {
		t.Errorf("%d pieces, want 119", len(c.pieces))
	}
	w := loadBDSRunsIn(t, "end_100", []string{""}, world.End, true, all[0]>>4-1, all[3]>>4+1, all[2]>>4-1, all[5]>>4+1)
	same, total := structureScore(t, "seed 100 end city", g, w, boxes)
	if total == 0 || same*100 < total*98 {
		t.Errorf("end city cells %d/%d below 98%%", same, total)
	}
}

// TestMarkerOrder: libc++'s map order gives the two-chest templates' markers in reverse NBT order (P182).
func TestMarkerOrder(t *testing.T) {
	keys := [][3]int{{5, 5, 7}, {7, 5, 7}}
	got := markerOrder(2, func(i int) [3]int { return keys[i] })
	if got[0] != 1 || got[1] != 0 {
		t.Errorf("order %v, want [1 0]", got)
	}
}
