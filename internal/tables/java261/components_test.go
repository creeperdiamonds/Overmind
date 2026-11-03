package java261

import (
	"strconv"
	"testing"
)

// The component table holds protocol.json's SlotComponentType mapper: 110 types
// at the pinned commit, each ID once, numbered from 0.
func TestComponentTypes(t *testing.T) {
	cases := map[string]int32{"damage": 3, "minecraft:custom_name": 6, "enchantments": 13, "max_damage": 2}
	for name, want := range cases {
		if got := ComponentType(name); got != want {
			t.Errorf("ComponentType(%q) = %d, want %d", name, got, want)
		}
	}
	if got := ComponentType("no_such_component"); got != -1 {
		t.Errorf("an unknown component gave %d", got)
	}
	ls := lines("components")
	if len(ls) != 110 {
		t.Fatalf("%d component types, want 110", len(ls))
	}
	seen := map[int]bool{}
	for _, l := range ls {
		id, err := strconv.Atoi(fields(l)[1])
		if err != nil || seen[id] || id < 0 || id >= len(ls) {
			t.Fatalf("bad line %q", l)
		}
		seen[id] = true
	}
}
