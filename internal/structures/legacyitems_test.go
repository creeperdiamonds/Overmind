package structures

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestLegacyItemsConvert: the legacy forms the mansion templates use become Bedrock items a Dragonfly
// chest reads, with no install.
func TestLegacyItemsConvert(t *testing.T) {
	nbt := map[string]any{"Items": []any{
		map[string]any{"Count": byte(1), "Damage": int16(2), "Slot": byte(8), "id": "minecraft:red_flower"},
		map[string]any{"Count": byte(9), "Damage": int16(5), "Slot": byte(4), "id": "minecraft:sapling"},
		map[string]any{"Count": byte(1), "Damage": int16(0), "Slot": byte(10), "id": "minecraft:iron_axe",
			"tag": map[string]any{"ench": []any{map[string]any{"id": int16(32), "lvl": int16(1)}}}},
		map[string]any{"Count": byte(1), "Damage": int16(0), "Slot": byte(15), "id": "minecraft:ender_pearl"},
		map[string]any{"Count": byte(1), "Damage": int16(0), "Slot": byte(1), "id": "minecraft:no_such_item"},
	}}
	items, skipped := legacyItems(nbt)
	if len(skipped) != 1 || skipped[0] != "minecraft:no_such_item" {
		t.Errorf("skipped %v, want only the unknown item", skipped)
	}
	c := block.Chest{}.DecodeNBT(map[string]any{"id": "Chest", "Items": items}).(block.Chest)
	got := map[int]item.Stack{}
	for _, x := range c.EncodeNBT()["Items"].([]any) {
		m := x.(map[string]any)
		got[int(m["Slot"].(byte))] = item.ReadNBT(m, nil)
	}
	want := map[int]struct {
		name  string
		count int
	}{8: {"minecraft:allium", 1}, 4: {"minecraft:dark_oak_sapling", 9}, 10: {"minecraft:iron_axe", 1}, 15: {"minecraft:ender_pearl", 1}}
	if len(got) != len(want) {
		t.Fatalf("chest holds %d stacks, want %d", len(got), len(want))
	}
	for slot, w := range want {
		name, _ := got[slot].Item().EncodeItem()
		if name != w.name || got[slot].Count() != w.count {
			t.Errorf("slot %d: %s x%d, want %s x%d", slot, name, got[slot].Count(), w.name, w.count)
		}
	}
	if e, ok := got[10].Enchantment(enchantment.Efficiency); !ok || e.Level() != 1 {
		t.Errorf("the axe is not Efficiency I: %v", got[10].Enchantments())
	}
}

// TestMansionChestsKeepTheirItems: every item in the install's mansion templates converts, and seed
// 987654321's (105,-292) mansion places its decorative chests with them.
func TestMansionChestsKeepTheirItems(t *testing.T) {
	_, s := worldFor(t, 987654321)
	stacks := 0
	for k := range s.data.templates {
		if !strings.HasPrefix(k, "mansion/") {
			continue
		}
		tp := s.data.Template(k)
		for _, b := range tp.blocks {
			if n := tp.palette[b.state].name; n != "chest" && n != "trapped_chest" {
				continue
			}
			items, skipped := legacyItems(b.nbt)
			if len(skipped) > 0 {
				t.Errorf("%s %v: items not converted: %v", k, b.pos, skipped)
			}
			stacks += len(items)
		}
	}
	t.Logf("mansion templates: %d item stacks in decorative chests", stacks)
	if stacks == 0 {
		t.Fatal("no mansion template chest holds items")
	}

	m := s.mansionAt(105, -292)
	if m == nil {
		t.Fatal("(105,-292) is not a mansion start")
	}
	boxes := make([]box, len(m.pieces))
	for i, p := range m.pieces {
		boxes[i] = p.box
	}
	all := boundsOf(boxes)
	held := 0
	for cx := all[0] >> 4; cx <= all[3]>>4; cx++ {
		for cz := all[2] >> 4; cz <= all[5]>>4; cz++ {
			c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			for _, be := range s.PlaceChunk(cx, cz, c) {
				if items, _ := be.Data["Items"].([]any); len(items) > 0 {
					held += len(items)
				}
			}
		}
	}
	t.Logf("seed 987654321 mansion: %d item stacks in its decorative chests", held)
	if held == 0 {
		t.Error("the mansion's decorative chests hold nothing")
	}
}

// TestDecorativeTrappedChestDropsItsItems: the mansion's trapped chest is a stand-in (no Go type in
// Dragonfly), so it cannot be opened; broken, it drops the items it holds.
func TestDecorativeTrappedChestDropsItsItems(t *testing.T) {
	b, ok := world.BlockByName("minecraft:trapped_chest", map[string]any{"minecraft:cardinal_direction": "north"})
	if !ok {
		t.Fatal("no trapped chest state")
	}
	items, _ := legacyItems(map[string]any{"Items": []any{
		map[string]any{"Count": byte(1), "Damage": int16(0), "Slot": byte(11), "id": "minecraft:ender_pearl"},
		map[string]any{"Count": byte(1), "Damage": int16(0), "Slot": byte(15), "id": "minecraft:ender_pearl"},
	}})
	nb, ok := b.(world.NBTer)
	if !ok {
		t.Fatalf("%T keeps no block entity", b)
	}
	b = nb.DecodeNBT(map[string]any{"id": "Chest", "Items": items}).(world.Block)
	br, ok := b.(interface{ BreakInfo() block.BreakInfo })
	if !ok {
		t.Fatalf("%T does not break", b)
	}
	pearls := 0
	for _, st := range br.BreakInfo().Drops(item.ToolNone{}, nil) {
		if name, _ := st.Item().EncodeItem(); name == "minecraft:ender_pearl" {
			pearls += st.Count()
		}
	}
	if pearls != 2 {
		t.Errorf("broken, it dropped %d ender pearls, want 2", pearls)
	}
}
