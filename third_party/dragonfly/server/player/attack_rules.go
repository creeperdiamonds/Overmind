package player

import "github.com/df-mc/dragonfly/server/item"

// AttackRules, when the Reporter a player reports to has it, decides the damage
// of the player's attacks and whether they are critical: a front end whose
// players follow another edition's combat rules (Java's attack cooldown and
// weapon damage) implements it. A Reporter without it, such as the Bedrock
// session, keeps Dragonfly's own damage.
// Overmind fork addition, see third_party/dragonfly/NOTES.md.
type AttackRules interface {
	// AttackDamage is given Dragonfly's damage for an attack with held (with
	// effects and enchantments) and whether it would be critical, and returns
	// what the attack does.
	AttackDamage(held item.Stack, dmg float64, critical bool) (float64, bool)
}

// attackDamage applies the Reporter's AttackRules, if it has them.
func (p *Player) attackDamage(held item.Stack, dmg float64, critical bool) (float64, bool) {
	if r, ok := p.s.(AttackRules); ok {
		return r.AttackDamage(held, dmg, critical)
	}
	return dmg, critical
}
