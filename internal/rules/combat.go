package rules

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/go-gl/mathgl/mgl64"
)

// Combat is the part of an edition's rules for its players' attacks, in
// Player.
type Combat interface {
	// CanReachEntity reports whether a player with its eyes at eye may hit or
	// use an entity whose box (in the world) is box.
	CanReachEntity(eye mgl64.Vec3, box cube.BBox, creative bool) bool
	// Attack is the damage of an attack and whether it is critical, from what
	// Dragonfly worked out for it (Bedrock's damage for the held item, with
	// effects and enchantments), the item and how long ago the player last
	// attacked or changed item.
	Attack(held item.Stack, dmg float64, critical bool, since time.Duration) (float64, bool)
}

// CanReachEntity leaves the reach to Dragonfly.
func (Bedrock) CanReachEntity(mgl64.Vec3, cube.BBox, bool) bool { return true }

// Attack is Dragonfly's own: Bedrock has no attack cooldown.
func (Bedrock) Attack(_ item.Stack, dmg float64, critical bool, _ time.Duration) (float64, bool) {
	return dmg, critical
}

// Java's entity interaction range (player.entity_interaction_range): 3 blocks,
// 5 in creative. Source: minecraft.wiki, "Attribute". Measured from the eyes to
// the nearest point of the target's box, plus our latency allowance.
const (
	javaEntityReach         = 3.0
	javaEntityReachCreative = 5.0
)

// CanReachEntity is Java's reach.
func (Java) CanReachEntity(eye mgl64.Vec3, box cube.BBox, creative bool) bool {
	reach := javaEntityReach
	if creative {
		reach = javaEntityReachCreative
	}
	return distanceToBox(eye, box) <= reach+reachAllowance
}

// Attack applies Java's base damage for the item and its attack cooldown
// (minecraft.wiki, "Damage", Attack cooldown): the charge is (t + 0.5) / T,
// held to 0..1, where t is the ticks since the last attack or change of item
// and T is 20 / the item's attack speed; damage is multiplied by
// 0.2 + charge² × 0.8, and only an attack charged over 0.9 can be critical.
// Dragonfly's effects and enchantments stay in, scaled in proportion: the
// Bedrock amounts, not Java's.
func (Java) Attack(held item.Stack, dmg float64, critical bool, since time.Duration) (float64, bool) {
	base, speed := javaWeapon(held)
	if bedrock := held.AttackDamage(); bedrock > 0 {
		dmg *= base / bedrock
	}
	charge := AttackCharge(speed, since)
	return dmg * (0.2 + charge*charge*0.8), critical && charge > 0.9
}

// AttackCharge is Java's attack strength after since, for an attack speed.
func AttackCharge(speed float64, since time.Duration) float64 {
	t := float64(since) / float64(time.Second/20)
	return math.Max(0, math.Min(1, (math.Floor(t)+0.5)/(20/speed)))
}

// javaWeapon is Java's attack damage and attack speed for a held item: the
// values on each item's minecraft.wiki page (Java Edition): Sword oldid=3865794,
// Axe oldid=3865831, Pickaxe oldid=3868188, Shovel oldid=3865810, Hoe
// oldid=3865792. TestJavaWeapons checks all 35. Anything else hits
// as a hand, 1 damage at speed 4.
func javaWeapon(s item.Stack) (damage, speed float64) {
	damage, speed = javaTool(s)
	if damage <= 0 || speed <= 0 {
		return 1, 4
	}
	return damage, speed
}

// javaTool is the table behind javaWeapon; zero for a tier it does not list.
func javaTool(s item.Stack) (damage, speed float64) {
	tier := func(t item.ToolTier) string { return t.Name }
	switch it := s.Item().(type) {
	case item.Sword:
		return map[string]float64{"wooden": 4, "golden": 4, "stone": 5, "copper": 5, "iron": 6, "diamond": 7, "netherite": 8}[tier(it.Tier)], 1.6
	case item.Axe:
		d := map[string]float64{"wooden": 7, "golden": 7, "stone": 9, "copper": 9, "iron": 9, "diamond": 9, "netherite": 10}[tier(it.Tier)]
		sp := map[string]float64{"wooden": 0.8, "golden": 1, "stone": 0.8, "copper": 0.8, "iron": 0.9, "diamond": 1, "netherite": 1}[tier(it.Tier)]
		return d, sp
	case item.Pickaxe:
		return map[string]float64{"wooden": 2, "golden": 2, "stone": 3, "copper": 3, "iron": 4, "diamond": 5, "netherite": 6}[tier(it.Tier)], 1.2
	case item.Shovel:
		return map[string]float64{"wooden": 2.5, "golden": 2.5, "stone": 3.5, "copper": 3.5, "iron": 4.5, "diamond": 5.5, "netherite": 6.5}[tier(it.Tier)], 1
	case item.Hoe:
		return 1, map[string]float64{"wooden": 1, "golden": 1, "stone": 2, "copper": 2, "iron": 3, "diamond": 4, "netherite": 4}[tier(it.Tier)]
	}
	return 1, 4
}
