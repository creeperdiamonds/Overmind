package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestCushionKeepsItsFields: a cushion read from Bedrock's actor fields writes them back (Overmind fork).
func TestCushionKeepsItsFields(t *testing.T) {
	if _, ok := DefaultRegistry.Lookup("minecraft:cushion"); !ok {
		t.Fatal("minecraft:cushion is not registered")
	}
	var data world.EntityData
	CushionType.DecodeNBT(map[string]any{"Variant": int32(12), "Rotation": []any{float32(-90), float32(0)}}, &data)
	if data.Rot[0] != -90 {
		t.Errorf("yaw %v, want -90", data.Rot[0])
	}
	if v := data.Data.(*CushionBehaviour).Variant(); v != 12 {
		t.Errorf("variant %d, want 12", v)
	}
	m := CushionType.EncodeNBT(&data)
	if m["Variant"] != int32(12) || m["Persistent"] != byte(1) || m["OnGround"] != byte(1) {
		t.Errorf("encoded %v", m)
	}
	if r := m["Rotation"].([]float32); r[0] != -90 || r[1] != 0 {
		t.Errorf("rotation %v", r)
	}
}
