package javafront

import "testing"

// A brewing stand's window shows the bottles first and the ingredient fourth;
// Dragonfly keeps the ingredient first.
func TestBrewingStandSlots(t *testing.T) {
	w := &window{order: brewingOrder}
	for java, df := range map[int]int{0: 1, 1: 2, 2: 3, 3: 0, 4: 4} {
		if got := w.containerSlot(java); got != df {
			t.Errorf("window slot %d is Dragonfly's %d, want %d", java, got, df)
		}
		if back := w.windowSlotOf(df); back != java {
			t.Errorf("Dragonfly's slot %d shows in window slot %d, want %d", df, back, java)
		}
	}
	plain := &window{}
	if plain.containerSlot(7) != 7 || plain.windowSlotOf(7) != 7 {
		t.Error("a window without an order changed a slot")
	}
}
