package block

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// PotDecoration represents an item that can be used as a decoration on a pot.
type PotDecoration interface {
	world.Item
	PotDecoration() bool
}

// DecoratedPot is a decoration block that can be crafted from up to four pottery sherds, and bricks on the sides where
// no pattern should be displayed.
type DecoratedPot struct {
	transparent
	sourceWaterDisplacer

	// Item is the item being stored in the decorated pot.
	Item item.Stack
	// Facing is the direction the pot is facing. The first decoration will be facing opposite of this direction.
	Facing cube.Direction
	// Decorations are the four decorations displayed on the sides of the pot. If a decoration is a brick or nil,
	// the side will appear to be empty.
	Decorations [4]PotDecoration
	// LootTable and LootTableSeed are the loot a generated pot rolls into its item the first time the item
	// is used: inserted into, taken by a hopper, dropped when broken or hit (Overmind fork). Empty once rolled.
	LootTable     string
	LootTableSeed int32
}

// unpackLoot rolls the pot's loot table into its item and clears the table (Overmind fork, as a chest's
// unpackLoot): the pot holds one stack, so the roll fills a container of one slot. The caller writes the
// pot back or breaks it.
func (p DecoratedPot) unpackLoot() DecoratedPot {
	if p.LootTable == "" || LootFiller == nil {
		return p
	}
	inv := inventory.New(1, nil)
	_ = inv.SetItem(0, p.Item)
	LootFiller(p.LootTable, p.LootTableSeed, inv)
	p.LootTable, p.LootTableSeed = "", 0
	p.Item, _ = inv.Item(0)
	return p
}

func (DecoratedPot) ContainerSize() int { return 1 }

// SideClosed ...
func (p DecoratedPot) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// ProjectileHit ...
func (p DecoratedPot) ProjectileHit(pos cube.Pos, tx *world.Tx, _ world.Entity, _ cube.Face) {
	p = p.unpackLoot() // Overmind fork: the break handler below drops the rolled item.
	for _, d := range p.Decorations {
		if d == nil {
			dropItem(tx, item.NewStack(item.Brick{}, 1), pos.Vec3Centre())
			continue
		}
		dropItem(tx, item.NewStack(d, 1), pos.Vec3Centre())
	}
	breakBlockNoDrops(p, pos, tx)
}

// Pick ...
func (p DecoratedPot) Pick() item.Stack {
	return item.NewStack(DecoratedPot{Decorations: p.Decorations}, 1)
}

// ExtractItem ...
func (p DecoratedPot) ExtractItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	p = p.unpackLoot() // Overmind fork.
	if p.Item.Empty() {
		return false
	}
	if _, err := h.inventory.AddItem(p.Item.Grow(-p.Item.Count() + 1)); err != nil {
		return false
	}
	p.Item = p.Item.Grow(-1)
	tx.SetBlock(pos, p, nil)
	return true
}

// InsertItem ...
func (p DecoratedPot) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	if p.LootTable != "" { // Overmind fork: roll the loot before anything goes in.
		p = p.unpackLoot()
		tx.SetBlock(pos, p, nil)
	}
	for sourceSlot, sourceStack := range h.inventory.Slots() {
		if !sourceStack.Empty() && sourceStack.Comparable(p.Item) && p.Item.Count() < p.Item.MaxCount() {
			if p.Item.Empty() {
				p.Item = sourceStack.Grow(-sourceStack.Count() + 1)
			} else {
				p.Item = p.Item.Grow(1)
			}
			_ = h.inventory.SetItem(sourceSlot, sourceStack.Grow(-1))
			tx.SetBlock(pos, p, nil)
			return true
		}
	}
	return false
}

// wobble ...
func (p DecoratedPot) wobble(pos cube.Pos, tx *world.Tx, success bool) {
	for _, v := range tx.Viewers(pos.Vec3Centre()) {
		v.ViewBlockAction(pos, DecoratedPotWobbleAction{DecoratedPot: p, Success: success})
	}

	if success {
		tx.AddParticle(pos.Vec3Middle().Add(mgl64.Vec3{0, 1.2}), particle.DustPlume{})
		tx.PlaySound(pos.Vec3Centre(), sound.DecoratedPotInserted{Progress: float64(p.Item.Count()) / float64(p.Item.MaxCount())})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.DecoratedPotInsertFailed{})
	}
}

// Activate ...
func (p DecoratedPot) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if p.LootTable != "" { // Overmind fork: roll the loot before the held item is compared with it.
		p = p.unpackLoot()
		tx.SetBlock(pos, p, nil)
	}
	held, _ := u.HeldItems()
	if held.Empty() || !p.Item.Comparable(held) || p.Item.Count() == p.Item.MaxCount() {
		p.wobble(pos, tx, false)
		return false
	}

	if p.Item.Empty() {
		p.Item = held.Grow(-held.Count() + 1)
	} else {
		p.Item = p.Item.Grow(1)
	}
	tx.SetBlock(pos, p, nil)
	p.wobble(pos, tx, true)
	ctx.SubtractFromCount(1)
	return true
}

// BreakInfo ...
func (p DecoratedPot) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(DecoratedPot{Decorations: p.Decorations})).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		p := p.unpackLoot() // Overmind fork.
		if !p.Item.Empty() {
			dropItem(tx, p.Item, pos.Vec3Centre())
		}
	})
}

// EncodeItem ...
func (p DecoratedPot) EncodeItem() (name string, meta int16) {
	return "minecraft:decorated_pot", 0
}

// EncodeBlock ...
func (p DecoratedPot) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:decorated_pot", map[string]any{"direction": int32(horizontalDirection(p.Facing))}
}

// Model ...
func (p DecoratedPot) Model() world.BlockModel {
	return model.DecoratedPot{}
}

// UseOnBlock ...
func (p DecoratedPot) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, p)
	if !used {
		return
	}
	p.Facing = user.Rotation().Direction().Opposite()

	place(tx, pos, p, user, ctx)
	return placed(ctx)
}

// EncodeNBT ...
func (p DecoratedPot) EncodeNBT() map[string]any {
	var sherds []any
	for _, decoration := range p.Decorations {
		if decoration == nil {
			sherds = append(sherds, "minecraft:brick")
		} else {
			name, _ := decoration.EncodeItem()
			sherds = append(sherds, name)
		}
	}

	m := map[string]any{
		"id":     "DecoratedPot",
		"sherds": sherds,
	}
	if !p.Item.Empty() {
		m["item"] = item.WriteNBT(p.Item, true)
	}
	if p.LootTable != "" { // Overmind fork.
		m["LootTable"], m["LootTableSeed"] = p.LootTable, p.LootTableSeed
	}
	return m
}

// DecodeNBT ...
func (p DecoratedPot) DecodeNBT(data map[string]any) any {
	p.Item = item.MapNBT(data, "item")
	p.LootTable = nbtconv.String(data, "LootTable") // Overmind fork.
	p.LootTableSeed = nbtconv.Int32(data, "LootTableSeed")
	p.Decorations = [4]PotDecoration{}
	if sherds := nbtconv.Slice(data, "sherds"); sherds != nil {
		for i, name := range sherds {
			if i >= len(p.Decorations) {
				break
			}
			it, ok := world.ItemByName(name.(string), 0)
			if !ok {
				panic(fmt.Errorf("unknown item %s", name))
			}
			decoration, ok := it.(PotDecoration)
			if !ok {
				panic(fmt.Errorf("item %s is not a pot decoration", name))
			}
			p.Decorations[i] = decoration
		}
	}
	return p
}

// allDecoratedPots ...
func allDecoratedPots() (pots []world.Block) {
	for _, f := range cube.Directions() {
		pots = append(pots, DecoratedPot{Facing: f})
	}
	return
}
