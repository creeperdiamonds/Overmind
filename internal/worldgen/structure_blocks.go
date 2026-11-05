package worldgen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Blocks the structures need that Dragonfly has no type for (Task 9), our own types with their block
// entity data, registered like the other own blocks (dfblocks.go) under Bedrock 1.26's names and states:
//   - suspicious sand and suspicious gravel (Bedrock: brushed_progress 0..3, hanging), holding the loot
//     table and seed BDS stores on them (block entity BrushableBlock: LootTable, LootTableSeed); brushing
//     is not written yet, so they break like sand and gravel and drop nothing;
//   - the flower pot (Bedrock: update_bit), holding its plant (block entity FlowerPot: PlantBlock, a
//     block's name and states), as the witch hut's red mushroom.

// Brushable is suspicious sand or gravel.
type Brushable struct {
	Gravel   bool
	Progress int32 // brushed_progress, 0..3.
	Hanging  bool
	// LootTable and LootTableSeed are what brushing rolls (BDS's BrushableBlock entity).
	LootTable     string
	LootTableSeed int32
}

var brushableHash = block.NextHash()

func (b Brushable) name() string {
	if b.Gravel {
		return "minecraft:suspicious_gravel"
	}
	return "minecraft:suspicious_sand"
}

// EncodeBlock ...
func (b Brushable) EncodeBlock() (string, map[string]any) {
	return b.name(), map[string]any{"brushed_progress": b.Progress, "hanging": uint8(btoi(b.Hanging))}
}

// Hash ...
func (b Brushable) Hash() (uint64, uint64) {
	return brushableHash, uint64(btoi(b.Gravel)) | uint64(b.Progress)<<1 | uint64(btoi(b.Hanging))<<3
}

// Model ...
func (Brushable) Model() world.BlockModel { return model.Solid{} }

// BreakInfo ...
func (Brushable) BreakInfo() block.BreakInfo {
	return block.BreakInfo{Hardness: 0.25, Effective: func(item.Tool) bool { return true }, Harvestable: func(item.Tool) bool { return true },
		Drops: func(item.Tool, []item.Enchantment) []item.Stack { return nil }}
}

// EncodeNBT ...
func (b Brushable) EncodeNBT() map[string]any {
	m := map[string]any{"id": "BrushableBlock"}
	if b.LootTable != "" {
		m["LootTable"], m["LootTableSeed"] = b.LootTable, b.LootTableSeed
	}
	return m
}

// DecodeNBT ...
func (b Brushable) DecodeNBT(data map[string]any) any {
	b.LootTable, _ = data["LootTable"].(string)
	b.LootTableSeed, _ = data["LootTableSeed"].(int32)
	return b
}

// FlowerPot is a flower pot, with its plant (Bedrock name and states) or empty.
type FlowerPot struct {
	Plant       string
	PlantStates map[string]any
	Update      bool // update_bit.
}

var flowerPotHash = block.NextHash()

// EncodeBlock ...
func (f FlowerPot) EncodeBlock() (string, map[string]any) {
	return "minecraft:flower_pot", map[string]any{"update_bit": uint8(btoi(f.Update))}
}

// Hash ...
func (f FlowerPot) Hash() (uint64, uint64) { return flowerPotHash, uint64(btoi(f.Update)) }

// Model ...
func (FlowerPot) Model() world.BlockModel { return model.Empty{} }

// BreakInfo ...
func (FlowerPot) BreakInfo() block.BreakInfo {
	return block.BreakInfo{Effective: func(item.Tool) bool { return true }, Harvestable: func(item.Tool) bool { return true },
		Drops: func(item.Tool, []item.Enchantment) []item.Stack { return nil }}
}

// EncodeNBT ...
func (f FlowerPot) EncodeNBT() map[string]any {
	m := map[string]any{"id": "FlowerPot"}
	if f.Plant != "" {
		st := f.PlantStates
		if st == nil {
			st = map[string]any{}
		}
		m["PlantBlock"] = map[string]any{"name": f.Plant, "states": st}
	}
	return m
}

// DecodeNBT ...
func (f FlowerPot) DecodeNBT(data map[string]any) any {
	if p, ok := data["PlantBlock"].(map[string]any); ok {
		f.Plant, _ = p["name"].(string)
		f.PlantStates, _ = p["states"].(map[string]any)
	}
	return f
}

func registerStructureBlocks() {
	for _, g := range []bool{false, true} {
		for p := range int32(4) {
			for _, h := range []bool{false, true} {
				world.RegisterBlock(Brushable{Gravel: g, Progress: p, Hanging: h})
			}
		}
	}
	for _, u := range []bool{false, true} {
		world.RegisterBlock(FlowerPot{Update: u})
	}
}
