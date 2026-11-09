package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Overmind fork: the cushion, Bedrock's minecraft:cushion actor (1.26), which Dragonfly lacks. Overmind's
// abandoned camps place them with their chunks (internal/structures template_entities.go). A minimal
// type: it stays where it is, keeps its colour (Variant, the dye index, sent to clients as the variant
// metadata) and rotation, and saves the fields BDS stores for it (Persistent, OnGround). Not written:
// Bedrock's survival check (CushionSurvivalComponent), sitting, and breaking it into its item.

// CushionType is a world.EntityType implementation for cushions.
var CushionType cushionType

type cushionType struct{}

// CushionBehaviour is a cushion's behaviour: stationary, with its colour.
type CushionBehaviour struct {
	*StationaryBehaviour
	variant int32
}

// Variant is the cushion's colour as a dye index (the dye item order, black 0 to white 15).
func (c *CushionBehaviour) Variant() int32 { return c.variant }

func (cushionType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return Open(tx, handle, data)
}

func (cushionType) EncodeEntity() string { return "minecraft:cushion" }

// BBox is ASSUMED (Bedrock's collision box for the cushion was not read): a low cushion a block wide.
func (cushionType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.4375, 0, -0.4375, 0.4375, 0.25, 0.4375)
}

func (cushionType) DecodeNBT(m map[string]any, data *world.EntityData) {
	v, _ := m["Variant"].(int32)
	data.Data = &CushionBehaviour{StationaryBehaviour: StationaryBehaviourConfig{}.New(), variant: v}
	// Bedrock keeps the rotation as Rotation [yaw, pitch]; Dragonfly's own fields are Yaw and Pitch.
	if r, ok := m["Rotation"].([]float32); ok && len(r) == 2 {
		data.Rot = cube.Rotation{float64(r[0]), float64(r[1])}
	} else if r, ok := m["Rotation"].([]any); ok && len(r) == 2 {
		y, _ := r[0].(float32)
		p, _ := r[1].(float32)
		data.Rot = cube.Rotation{float64(y), float64(p)}
	}
}

func (cushionType) EncodeNBT(data *world.EntityData) map[string]any {
	var v int32
	if b, ok := data.Data.(*CushionBehaviour); ok {
		v = b.variant
	}
	return map[string]any{
		"Variant":     v,
		"Rotation":    []float32{float32(data.Rot[0]), float32(data.Rot[1])},
		"Persistent":  byte(1),
		"OnGround":    byte(1),
		"definitions": []string{"+minecraft:cushion"},
	}
}
