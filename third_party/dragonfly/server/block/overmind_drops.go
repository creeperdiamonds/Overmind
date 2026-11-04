package block

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Overmind fork: a block whose drops differ by the world's edition rules (world.Mechanics), which a
// BreakInfo's Drops cannot see.

// WorldDropper is a block whose drops depend on the world it breaks in.
type WorldDropper interface {
	DropsIn(tx *world.Tx, t item.Tool, e []item.Enchantment) []item.Stack
}

// DropsAt is what b drops when broken in tx with the tool t and enchantments e: its DropsIn if it is a
// WorldDropper, its BreakInfo's Drops otherwise.
func DropsAt(b Breakable, tx *world.Tx, t item.Tool, e []item.Enchantment) []item.Stack {
	if d, ok := b.(WorldDropper); ok {
		return d.DropsIn(tx, t, e)
	}
	return b.BreakInfo().Drops(t, e)
}
