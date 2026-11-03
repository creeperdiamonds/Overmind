package java261

import "testing"

func TestMetadata(t *testing.T) {
	for _, c := range []struct {
		entity, key string
		want        int32
	}{
		{"player", "shared_flags", 0},
		{"minecraft:player", "pose", 6},
		{"player", "living_entity_flags", 8},
		{"player", "health", 9},
		{"player", "player_mode_customisation", 16},
		{"item", "item", 8},
		{"item", "health", -1},
		{"nothing", "pose", -1},
	} {
		if got := MetadataIndex(c.entity, c.key); got != c.want {
			t.Errorf("MetadataIndex(%q, %q) = %d, want %d", c.entity, c.key, got, c.want)
		}
	}
	for name, want := range map[string]int32{"byte": 0, "float": 3, "item_stack": 7, "pose": 20, "humanoid_arm": 42, "nothing": -1} {
		if got := MetadataType(name); got != want {
			t.Errorf("MetadataType(%q) = %d, want %d", name, got, want)
		}
	}
}
