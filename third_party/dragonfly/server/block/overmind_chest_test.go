package block_test

// Overmind fork: a generated chest's loot table is rolled once, and breaking the chest before it is
// opened drops the loot and one chest item and leaves air.

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type blastAt struct{ p mgl64.Vec3 }

func (b blastAt) Position() mgl64.Vec3 { return b.p }
func (b blastAt) Size() float64        { return 4 }

func TestBrokenLootChestDropsLootOnce(t *testing.T) {
	old := block.LootFiller
	defer func() { block.LootFiller = old }()
	rolls := 0
	block.LootFiller = func(table string, seed int32, inv *inventory.Inventory) bool {
		rolls++
		_ = inv.SetItem(0, item.NewStack(item.Diamond{}, 3))
		return true
	}
	for _, how := range []string{"hand", "explosion"} {
		t.Run(how, func(t *testing.T) {
			rolls = 0
			w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
			defer w.Close()
			pos := cube.Pos{0, 1, 0}
			w.Do(func(tx *world.Tx) {
				c := block.NewChest()
				c.LootTable, c.LootTableSeed = "loot_tables/chests/test.json", 7
				tx.SetBlock(pos, c, nil)
				b := tx.Block(pos).(block.Chest)
				switch how {
				case "hand":
					// What a player's break does: air first, then the handler, then the drops.
					tx.SetBlock(pos, nil, nil)
					b.BreakInfo().BreakHandler(pos, tx, nil)
					for _, d := range b.BreakInfo().Drops(item.ToolNone{}, nil) {
						tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos.Vec3Centre()}, d))
					}
				case "explosion":
					block.ExplosionConfig{ItemDropChance: 1}.Explode(tx, blastAt{pos.Vec3Centre()})
				}
				if _, ok := tx.Block(pos).(block.Air); !ok {
					t.Errorf("after the break the chest's position holds %#v, want air", tx.Block(pos))
				}
				var diamonds, chests int
				for e := range tx.Entities() {
					ent, ok := e.(*entity.Ent)
					if !ok {
						continue
					}
					ib, ok := ent.Behaviour().(*entity.ItemBehaviour)
					if !ok {
						continue
					}
					switch ib.Item().Item().(type) {
					case item.Diamond:
						diamonds += ib.Item().Count()
					case block.Chest:
						chests += ib.Item().Count()
					}
				}
				if rolls != 1 || diamonds != 3 || chests != 1 {
					t.Errorf("rolled %d times, dropped %d diamonds and %d chests; want 1, 3 and 1", rolls, diamonds, chests)
				}
			})
		})
	}
}
