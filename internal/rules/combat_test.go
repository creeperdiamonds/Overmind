package rules

import (
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/go-gl/mathgl/mgl64"
)

const tick = time.Second / 20

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Java's attack strength: (t + 0.5) / T, T = 20 / speed, held to 0..1.
func TestAttackCharge(t *testing.T) {
	for _, c := range []struct {
		speed float64
		since time.Duration
		want  float64
	}{
		{4, 0, 0.1},           // a hand, at once: 0.5 / 5
		{4, 2 * tick, 0.5},    // 2.5 / 5
		{4, 5 * tick, 1},      // 5.5 / 5, held to 1
		{1.6, 6 * tick, 0.52}, // a sword: 6.5 / 12.5
		{1.6, time.Second, 1},
		{4, 2*tick + tick/2, 0.5}, // whole ticks only
	} {
		if got := AttackCharge(c.speed, c.since); !near(got, c.want) {
			t.Errorf("AttackCharge(%v, %v) = %v, want %v", c.speed, c.since, got, c.want)
		}
	}
}

// javaWeapons is every tool kind and tier with Java's damage and attack speed,
// from minecraft.wiki's Java Edition tables: Sword oldid=3865794, Axe
// oldid=3865831, Pickaxe oldid=3868188, Shovel oldid=3865810, Hoe
// oldid=3865792 (27-29 October 2026). It is written out here, apart from
// javaTool, so a wrong value in either fails.
var javaWeapons = []struct {
	kind          string
	tier          item.ToolTier
	damage, speed float64
}{
	{"sword", item.ToolTierWood, 4, 1.6}, {"sword", item.ToolTierGold, 4, 1.6}, {"sword", item.ToolTierStone, 5, 1.6},
	{"sword", item.ToolTierCopper, 5, 1.6}, {"sword", item.ToolTierIron, 6, 1.6}, {"sword", item.ToolTierDiamond, 7, 1.6},
	{"sword", item.ToolTierNetherite, 8, 1.6},
	{"axe", item.ToolTierWood, 7, 0.8}, {"axe", item.ToolTierGold, 7, 1}, {"axe", item.ToolTierStone, 9, 0.8},
	{"axe", item.ToolTierCopper, 9, 0.8}, {"axe", item.ToolTierIron, 9, 0.9}, {"axe", item.ToolTierDiamond, 9, 1},
	{"axe", item.ToolTierNetherite, 10, 1},
	{"pickaxe", item.ToolTierWood, 2, 1.2}, {"pickaxe", item.ToolTierGold, 2, 1.2}, {"pickaxe", item.ToolTierStone, 3, 1.2},
	{"pickaxe", item.ToolTierCopper, 3, 1.2}, {"pickaxe", item.ToolTierIron, 4, 1.2}, {"pickaxe", item.ToolTierDiamond, 5, 1.2},
	{"pickaxe", item.ToolTierNetherite, 6, 1.2},
	{"shovel", item.ToolTierWood, 2.5, 1}, {"shovel", item.ToolTierGold, 2.5, 1}, {"shovel", item.ToolTierStone, 3.5, 1},
	{"shovel", item.ToolTierCopper, 3.5, 1}, {"shovel", item.ToolTierIron, 4.5, 1}, {"shovel", item.ToolTierDiamond, 5.5, 1},
	{"shovel", item.ToolTierNetherite, 6.5, 1},
	{"hoe", item.ToolTierWood, 1, 1}, {"hoe", item.ToolTierGold, 1, 1}, {"hoe", item.ToolTierStone, 1, 2},
	{"hoe", item.ToolTierCopper, 1, 2}, {"hoe", item.ToolTierIron, 1, 3}, {"hoe", item.ToolTierDiamond, 1, 4},
	{"hoe", item.ToolTierNetherite, 1, 4},
}

func tool(kind string, tier item.ToolTier) item.Stack {
	switch kind {
	case "sword":
		return item.NewStack(item.Sword{Tier: tier}, 1)
	case "axe":
		return item.NewStack(item.Axe{Tier: tier}, 1)
	case "pickaxe":
		return item.NewStack(item.Pickaxe{Tier: tier}, 1)
	case "shovel":
		return item.NewStack(item.Shovel{Tier: tier}, 1)
	}
	return item.NewStack(item.Hoe{Tier: tier}, 1)
}

func TestJavaWeapons(t *testing.T) {
	if len(javaWeapons) != 35 {
		t.Fatalf("%d weapons in the table, want 5 kinds of 7 tiers", len(javaWeapons))
	}
	for _, c := range javaWeapons {
		if d, s := javaWeapon(tool(c.kind, c.tier)); d != c.damage || s != c.speed {
			t.Errorf("%s %s: damage %v speed %v, want %v %v", c.tier.Name, c.kind, d, s, c.damage, c.speed)
		}
	}
	for _, s := range []item.Stack{{}, item.NewStack(item.Stick{}, 1)} {
		if d, sp := javaWeapon(s); d != 1 || sp != 4 {
			t.Errorf("%v hits as %v at %v, want a hand: 1 at 4", s, d, sp)
		}
	}
}

func TestJavaAttack(t *testing.T) {
	hand := item.Stack{}
	// Full charge: Java's base for the item, in place of Bedrock's.
	if d, crit := (Java{}).Attack(hand, 1, true, time.Second); !near(d, 1) || !crit {
		t.Errorf("a charged punch: %v %v", d, crit)
	}
	sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1) // Bedrock 8 in Dragonfly, Java 7
	if d, _ := (Java{}).Attack(sword, sword.AttackDamage(), false, time.Second); !near(d, 7) {
		t.Errorf("a charged diamond sword hits for %v, want 7", d)
	}
	// Effects and enchantments Dragonfly added scale in proportion.
	if d, _ := (Java{}).Attack(sword, 2*sword.AttackDamage(), false, time.Second); !near(d, 14) {
		t.Errorf("a doubled diamond sword hits for %v, want 14", d)
	}
	// At once: 0.2 + 0.1² × 0.8, and never critical.
	if d, crit := (Java{}).Attack(hand, 1, true, 0); !near(d, 0.208) || crit {
		t.Errorf("an uncharged punch: %v %v, want 0.208 and no critical", d, crit)
	}
	// Charged just over 0.9 may be critical; 0.9 itself may not.
	if _, crit := (Java{}).Attack(hand, 1, true, 4*tick); crit { // 4.5 / 5 = 0.9
		t.Error("an attack charged to 0.9 was critical")
	}
	if d, crit := (Bedrock{}).Attack(sword, 8, true, 0); d != 8 || !crit {
		t.Errorf("Bedrock changed an attack: %v %v", d, crit)
	}
}

func TestJavaEntityReach(t *testing.T) {
	eye := mgl64.Vec3{0.5, 1.62, 0.5}
	box := func(x float64) cube.BBox { return cube.Box(x-0.3, 0, 0.2, x+0.3, 1.8, 0.8) }
	for _, c := range []struct {
		x        float64
		creative bool
		want     bool
	}{
		{2, false, true},   // 1.2 to its side
		{4.8, false, true}, // 4.0: 3 and the allowance
		{5, false, false},  // 4.2
		{5, true, true},
		{7, true, false}, // 6.2
	} {
		if got := (Java{}).CanReachEntity(eye, box(c.x), c.creative); got != c.want {
			t.Errorf("reach to x %v (creative %v) = %v, want %v", c.x, c.creative, got, c.want)
		}
	}
	if !(Bedrock{}).CanReachEntity(eye, box(7), false) {
		t.Error("Bedrock rules must leave the reach to Dragonfly")
	}
}
