package block_test

// Overmind fork: generated barrels and decorated pots keep a loot table and seed (as chests do), roll
// it once when their contents are first used, and keep it through their NBT until then.

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// fakeLoot puts 3 diamonds in the container's first slot and counts its rolls.
func fakeLoot(t *testing.T) *int {
	old := block.LootFiller
	t.Cleanup(func() { block.LootFiller = old })
	rolls := new(int)
	block.LootFiller = func(table string, seed int32, inv *inventory.Inventory) bool {
		if table != "loot_tables/test.json" || seed != 7 {
			t.Errorf("rolled table %q seed %d, want loot_tables/test.json and 7", table, seed)
		}
		*rolls++
		_ = inv.SetItem(0, item.NewStack(item.Diamond{}, 3))
		return true
	}
	return rolls
}

// droppedDiamonds counts the diamonds lying in the world as items.
func droppedDiamonds(tx *world.Tx) int {
	n := 0
	for e := range tx.Entities() {
		if ent, ok := e.(*entity.Ent); ok {
			if ib, ok := ent.Behaviour().(*entity.ItemBehaviour); ok {
				if _, ok := ib.Item().Item().(item.Diamond); ok {
					n += ib.Item().Count()
				}
			}
		}
	}
	return n
}

func TestBarrelLootRollsOnce(t *testing.T) {
	rolls := fakeLoot(t)
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	w.Do(func(tx *world.Tx) {
		// A generated barrel arrives as a block with its block entity data.
		b := block.Barrel{Facing: cube.FaceUp}.DecodeNBT(map[string]any{"id": "Barrel", "LootTable": "loot_tables/test.json", "LootTableSeed": int32(7)}).(block.Barrel)
		if b.LootTable == "" || b.EncodeNBT()["LootTable"] != "loot_tables/test.json" || b.EncodeNBT()["LootTableSeed"] != int32(7) {
			t.Fatalf("the barrel's NBT does not keep its loot table: %v", b.EncodeNBT())
		}
		tx.SetBlock(pos, b, nil)
		inv := tx.Block(pos).(block.Barrel).Inventory(tx, pos)
		if st, _ := inv.Item(0); st.Count() != 3 || *rolls != 1 {
			t.Fatalf("after the first open: slot 0 %v, %d rolls; want 3 diamonds, 1 roll", st, *rolls)
		}
		if now := tx.Block(pos).(block.Barrel); now.LootTable != "" || now.EncodeNBT()["LootTable"] != nil {
			t.Errorf("the opened barrel still holds its table %q", now.LootTable)
		}
		tx.Block(pos).(block.Barrel).Inventory(tx, pos)
		if *rolls != 1 {
			t.Errorf("rolled %d times after a second open, want 1", *rolls)
		}
	})
	// Broken before it is opened: the loot drops once and air stays.
	*rolls = 0
	w.Do(func(tx *world.Tx) {
		b := block.Barrel{}.DecodeNBT(map[string]any{"LootTable": "loot_tables/test.json", "LootTableSeed": int32(7)}).(block.Barrel)
		p := cube.Pos{4, 1, 0}
		tx.SetBlock(p, b, nil)
		got := tx.Block(p).(block.Barrel)
		tx.SetBlock(p, nil, nil)
		got.BreakInfo().BreakHandler(p, tx, nil)
		if _, ok := tx.Block(p).(block.Air); !ok {
			t.Errorf("after the break the barrel's position holds %#v, want air", tx.Block(p))
		}
		if d := droppedDiamonds(tx); *rolls != 1 || d != 3 {
			t.Errorf("broken unopened barrel: %d rolls, %d diamonds dropped; want 1 and 3", *rolls, d)
		}
	})
}

func TestDecoratedPotLootRollsOnce(t *testing.T) {
	rolls := fakeLoot(t)
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	w.Do(func(tx *world.Tx) {
		p := block.DecoratedPot{}.DecodeNBT(map[string]any{"id": "DecoratedPot", "LootTable": "loot_tables/test.json", "LootTableSeed": int32(7)}).(block.DecoratedPot)
		if p.EncodeNBT()["LootTable"] != "loot_tables/test.json" || p.EncodeNBT()["LootTableSeed"] != int32(7) {
			t.Fatalf("the pot's NBT does not keep its loot table: %v", p.EncodeNBT())
		}
		// Broken: the rolled item drops.
		pos := cube.Pos{0, 1, 0}
		tx.SetBlock(pos, p, nil)
		tx.SetBlock(pos, nil, nil)
		p.BreakInfo().BreakHandler(pos, tx, nil)
		if d := droppedDiamonds(tx); *rolls != 1 || d != 3 {
			t.Errorf("broken pot: %d rolls, %d diamonds dropped; want 1 and 3", *rolls, d)
		}
		// Hit by a projectile: the item drops with the pot's sides.
		*rolls = 0
		pos = cube.Pos{4, 1, 0}
		tx.SetBlock(pos, p, nil)
		p.ProjectileHit(pos, tx, nil, cube.FaceUp)
		if d := droppedDiamonds(tx); *rolls != 1 || d != 6 {
			t.Errorf("pot hit by a projectile: %d rolls, %d diamonds dropped in all; want 1 and 6", *rolls, d)
		}
	})
}
