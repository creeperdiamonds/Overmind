package javafront_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// The Java window 0 slots, as the client numbers them: 5-8 armour, 9-35 the
// main inventory, 36-44 the hotbar, 45 the off hand.
const (
	wHelmet  = 5
	wMain    = 9
	wHotbar  = 36
	wOffHand = 45
)

// serverWindow is the Java player's inventory on the server, by window slot,
// as "name:count" (no minecraft: prefix).
func serverWindow(t *testing.T, srv *testserver.Server, name string) map[int]string {
	t.Helper()
	out := map[int]string{}
	put := func(slot int, s item.Stack) {
		if !s.Empty() {
			n, _ := s.Item().EncodeItem()
			out[slot] = fmt.Sprintf("%s:%d", strings.TrimPrefix(n, "minecraft:"), s.Count())
		}
	}
	onJava(t, srv, name, func(p *player.Player) {
		for i, s := range p.Inventory().Slots() {
			if i < 9 {
				put(wHotbar+i, s)
			} else {
				put(i, s)
			}
		}
		for i, s := range p.Armour().Slots() {
			put(wHelmet+i, s)
		}
		_, off := p.HeldItems()
		put(wOffHand, off)
	})
	return out
}

// botWindow is the same as the bot was told.
func botWindow(inv botInventory) map[int]string {
	out := map[int]string{}
	for i, s := range inv.Slots {
		if s != nil {
			out[i] = fmt.Sprintf("%s:%d", s.Name, s.Count)
		}
	}
	return out
}

func cursorOf(inv botInventory) string {
	if inv.Cursor == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d", inv.Cursor.Name, inv.Cursor.Count)
}

// takeDropped removes the dropped items in the Java player's world and returns
// how many items they held. Taking them at once keeps the player from picking
// them up again (they wait 2 s) and merging from mattering.
func takeDropped(t *testing.T, srv *testserver.Server, name string) int {
	t.Helper()
	n := 0
	onJava(t, srv, name, func(p *player.Player) {
		for e := range p.Tx().Entities() {
			if e.H().Type() != entity.ItemType {
				continue
			}
			n += e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item().Count()
			p.Tx().RemoveEntity(e)
		}
	})
	return n
}

// A Java bot picks items up, moves them by every kind of click and key, and
// the server's inventory is what Java's rules say, and what the client shows.
func TestJavaClicksMoveItems(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "Mover")
	waitOnGround(t, srv, "Mover")
	onJava(t, srv, "Mover", func(p *player.Player) {
		_ = p.Inventory().SetItem(0, item.NewStack(block.Dirt{}, 10))
		_ = p.Inventory().SetItem(1, item.NewStack(item.Totem{}, 1))
		_ = p.Inventory().SetItem(9, item.NewStack(item.Stick{}, 20))
		_ = p.Inventory().SetItem(10, item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
	})
	start := map[int]string{wHotbar: "dirt:10", wHotbar + 1: "totem_of_undying:1", wMain: "stick:20", wMain + 1: "iron_helmet:1"}
	require.Eventually(t, func() bool { return fmt.Sprint(botWindow(j.inventory())) == fmt.Sprint(start) }, 5*time.Second, 50*time.Millisecond,
		"the client was not shown the items: %v", botWindow(j.inventory()))

	steps := []struct {
		what   string
		do     func()
		window map[int]string
		cursor string
		drops  int
	}{
		{"pick up a stack", func() { j.click(wHotbar, 0, 0) },
			map[int]string{wHotbar + 1: "totem_of_undying:1", wMain: "stick:20", wMain + 1: "iron_helmet:1"}, "dirt:10", 0},
		{"place it", func() { j.click(20, 0, 0) },
			map[int]string{wHotbar + 1: "totem_of_undying:1", wMain: "stick:20", wMain + 1: "iron_helmet:1", 20: "dirt:10"}, "", 0},
		{"pick up half", func() { j.click(20, 1, 0) },
			map[int]string{wHotbar + 1: "totem_of_undying:1", wMain: "stick:20", wMain + 1: "iron_helmet:1", 20: "dirt:5"}, "dirt:5", 0},
		{"place one", func() { j.click(21, 1, 0) },
			map[int]string{wHotbar + 1: "totem_of_undying:1", wMain: "stick:20", wMain + 1: "iron_helmet:1", 20: "dirt:5", 21: "dirt:1"}, "dirt:4", 0},
		{"swap with another item", func() { j.click(wMain, 0, 0) },
			map[int]string{wHotbar + 1: "totem_of_undying:1", wMain: "dirt:4", wMain + 1: "iron_helmet:1", 20: "dirt:5", 21: "dirt:1"}, "stick:20", 0},
		{"drop the cursor outside the window", func() { j.click(-999, 0, 0) },
			map[int]string{wHotbar + 1: "totem_of_undying:1", wMain: "dirt:4", wMain + 1: "iron_helmet:1", 20: "dirt:5", 21: "dirt:1"}, "", 20},
		{"shift-click from the inventory to the hotbar", func() { j.click(wMain, 0, 1) },
			map[int]string{wHotbar: "dirt:4", wHotbar + 1: "totem_of_undying:1", wMain + 1: "iron_helmet:1", 20: "dirt:5", 21: "dirt:1"}, "", 0},
		{"shift-click armour on", func() { j.click(wMain+1, 0, 1) },
			map[int]string{wHotbar: "dirt:4", wHotbar + 1: "totem_of_undying:1", wHelmet: "iron_helmet:1", 20: "dirt:5", 21: "dirt:1"}, "", 0},
		{"swap with hotbar slot 3 by its key", func() { j.click(20, 2, 2) },
			map[int]string{wHotbar: "dirt:4", wHotbar + 1: "totem_of_undying:1", wHotbar + 2: "dirt:5", wHelmet: "iron_helmet:1", 21: "dirt:1"}, "", 0},
		{"throw one from a slot", func() { j.click(wHotbar+2, 0, 4) },
			map[int]string{wHotbar: "dirt:4", wHotbar + 1: "totem_of_undying:1", wHotbar + 2: "dirt:4", wHelmet: "iron_helmet:1", 21: "dirt:1"}, "", 1},
		{"throw a slot's stack", func() { j.click(21, 1, 4) },
			map[int]string{wHotbar: "dirt:4", wHotbar + 1: "totem_of_undying:1", wHotbar + 2: "dirt:4", wHelmet: "iron_helmet:1"}, "", 1},
		{"drop one held item", func() { j.hold(2); time.Sleep(200 * time.Millisecond); j.action(4) },
			map[int]string{wHotbar: "dirt:4", wHotbar + 1: "totem_of_undying:1", wHotbar + 2: "dirt:3", wHelmet: "iron_helmet:1"}, "", 1},
		{"drop the held stack", func() { j.action(3) },
			map[int]string{wHotbar: "dirt:4", wHotbar + 1: "totem_of_undying:1", wHelmet: "iron_helmet:1"}, "", 3},
		{"swap the totem into the off hand", func() { j.hold(1); time.Sleep(200 * time.Millisecond); j.action(6) },
			map[int]string{wHotbar: "dirt:4", wOffHand: "totem_of_undying:1", wHelmet: "iron_helmet:1"}, "", 0},
		{"take armour off", func() { j.click(wHelmet, 0, 1) },
			map[int]string{wHotbar: "dirt:4", wOffHand: "totem_of_undying:1", wMain: "iron_helmet:1"}, "", 0},
		{"an armour slot refuses what is not armour", func() { j.click(wHotbar, 0, 0); j.click(wHelmet, 0, 0) },
			map[int]string{wOffHand: "totem_of_undying:1", wMain: "iron_helmet:1"}, "dirt:4", 0},
		{"closing the window puts the cursor back", func() { j.closeWindow() },
			map[int]string{wHotbar: "dirt:4", wOffHand: "totem_of_undying:1", wMain: "iron_helmet:1"}, "", 0},
		// No click may leave one item in two places. These are the clicks where
		// Dragonfly refuses half of a two-slot change.
		{"put the helmet back on", func() { j.click(wMain, 0, 1) },
			map[int]string{wHotbar: "dirt:4", wOffHand: "totem_of_undying:1", wHelmet: "iron_helmet:1"}, "", 0},
		{"pick up half the dirt", func() { j.click(wHotbar, 1, 0) },
			map[int]string{wHotbar: "dirt:2", wOffHand: "totem_of_undying:1", wHelmet: "iron_helmet:1"}, "dirt:2", 0},
		{"set one down", func() { j.click(20, 1, 0) },
			map[int]string{wHotbar: "dirt:2", wOffHand: "totem_of_undying:1", wHelmet: "iron_helmet:1", 20: "dirt:1"}, "dirt:1", 0},
		{"one dirt clicked onto the worn helmet: the helmet is not picked up", func() { j.click(wHelmet, 0, 0) },
			map[int]string{wHotbar: "dirt:2", wOffHand: "totem_of_undying:1", wHelmet: "iron_helmet:1", 20: "dirt:1"}, "dirt:1", 0},
		{"put the cursor down", func() { j.click(20, 0, 0) },
			map[int]string{wHotbar: "dirt:2", wOffHand: "totem_of_undying:1", wHelmet: "iron_helmet:1", 20: "dirt:2"}, "", 0},
		{"leave one dirt in hotbar slot 1", func() { j.click(wHotbar, 1, 0); j.click(20, 0, 0) },
			map[int]string{wHotbar: "dirt:1", wOffHand: "totem_of_undying:1", wHelmet: "iron_helmet:1", 20: "dirt:3"}, "", 0},
		{"hotbar key 1 on the helmet: the dirt cannot be worn, so the helmet stays", func() { j.click(wHelmet, 0, 2) },
			map[int]string{wHotbar: "dirt:1", wOffHand: "totem_of_undying:1", wHelmet: "iron_helmet:1", 20: "dirt:3"}, "", 0},
	}
	for _, s := range steps {
		s.do()
		want := fmt.Sprint(s.window)
		require.Eventually(t, func() bool { return fmt.Sprint(serverWindow(t, srv, "Mover")) == want }, 5*time.Second, 50*time.Millisecond,
			"%s: the server has %v, want %v", s.what, serverWindow(t, srv, "Mover"), s.window)
		require.Eventually(t, func() bool {
			inv := j.inventory()
			return fmt.Sprint(botWindow(inv)) == want && cursorOf(inv) == s.cursor
		}, 5*time.Second, 50*time.Millisecond, "%s: the client shows %v with %q on the cursor", s.what, botWindow(j.inventory()), cursorOf(j.inventory()))
		require.Equal(t, s.drops, takeDropped(t, srv, "Mover"), "%s: items dropped", s.what)
	}
	require.False(t, j.ended())
}

// The creative inventory works only for a player whose game mode has one.
func TestCreativeInventoryNeedsCreative(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "Maker")
	waitOnGround(t, srv, "Maker")

	j.creative(wHotbar, "diamond", 3)
	time.Sleep(500 * time.Millisecond)
	require.Empty(t, serverWindow(t, srv, "Maker"), "a survival player made items")
	require.Eventually(t, func() bool { return j.inventory().Slots[wHotbar] == nil }, 5*time.Second, 50*time.Millisecond, "the client was not corrected")

	onJava(t, srv, "Maker", func(p *player.Player) { p.SetGameMode(world.GameModeCreative) })
	j.creative(wHotbar, "diamond", 3)
	require.Eventually(t, func() bool { return serverWindow(t, srv, "Maker")[wHotbar] == "diamond:3" }, 5*time.Second, 50*time.Millisecond)
	j.creative(-1, "emerald", 65)
	time.Sleep(500 * time.Millisecond)
	require.Zero(t, takeDropped(t, srv, "Maker"), "a creative drop of more than a stack made items")
	j.creative(-1, "emerald", 2)
	require.Eventually(t, func() bool { return takeDropped(t, srv, "Maker") == 2 }, 5*time.Second, 50*time.Millisecond, "a creative drop made no item")
}

// componentData finds a component the bot read from a slot.
func componentData(it *botItem, typ string) (json.RawMessage, bool) {
	for _, c := range it.Components {
		if c.Type == typ {
			return c.Data, true
		}
	}
	return nil, false
}

// Durability, names and enchantments travel as item components, and an item
// Java does not have shows as a named barrier.
func TestItemComponentsReachJava(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "Looker")
	waitOnGround(t, srv, "Looker")

	var missing world.Item
	for _, it := range world.Items() {
		name, meta := it.EncodeItem()
		if _, ok := java261.JavaItem(name, meta); !ok {
			missing = it
			break
		}
	}
	require.NotNil(t, missing, "every Dragonfly item maps to Java")
	missingName, _ := missing.EncodeItem()

	sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).Damage(100).WithCustomName("Blade").
		WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 3))
	onJava(t, srv, "Looker", func(p *player.Player) {
		_ = p.Inventory().SetItem(0, sword)
		_ = p.Inventory().SetItem(1, item.NewStack(missing, 1))
	})
	require.Eventually(t, func() bool {
		s := j.inventory().Slots
		return s[wHotbar] != nil && s[wHotbar+1] != nil
	}, 5*time.Second, 50*time.Millisecond)
	inv := j.inventory()
	got := inv.Slots[wHotbar]
	require.Equal(t, "diamond_sword", got.Name)
	damage, ok := componentData(got, "damage")
	require.True(t, ok, "no damage component: %+v", got.Components)
	require.JSONEq(t, "100", string(damage))
	maxDamage, ok := componentData(got, "max_damage")
	require.True(t, ok)
	require.JSONEq(t, fmt.Sprint(sword.MaxDurability()), string(maxDamage))
	name, ok := componentData(got, "custom_name")
	require.True(t, ok)
	require.Contains(t, string(name), "Blade")
	ench, ok := componentData(got, "enchantments")
	require.True(t, ok, "no enchantments component: %+v", got.Components)
	require.Contains(t, string(ench), `"level":3`, "enchantments: %s", ench)

	barrier := inv.Slots[wHotbar+1]
	require.Equal(t, "barrier", barrier.Name, "%s should show as a barrier", missingName)
	label, ok := componentData(barrier, "custom_name")
	require.True(t, ok)
	require.Contains(t, string(label), missingName)
}

// Each edition sees what players of the other hold and wear.
func TestHeldItemsAndArmourSeenBothWays(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BHolder")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JHolder")
	waitOnGround(t, srv, "JHolder")
	watch := startBot(t, srv, "JWatch")
	waitOnGround(t, srv, "JWatch")
	require.Eventually(t, func() bool { _, ok := b.Player("JHolder"); return ok }, 20*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { _, ok := j.players()["BHolder"]; return ok }, 20*time.Second, 50*time.Millisecond)

	// Java to Bedrock, and to another Java player: the client picks the slot.
	onJava(t, srv, "JHolder", func(p *player.Player) {
		_ = p.Inventory().SetItem(2, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1))
		p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
	})
	j.hold(2)
	require.Eventually(t, func() bool {
		s, _ := b.Player("JHolder")
		return s.MainHand == "minecraft:diamond_sword" && s.Helmet == "minecraft:iron_helmet"
	}, 10*time.Second, 50*time.Millisecond, "Bedrock sees %+v", func() bedrock.Seen { s, _ := b.Player("JHolder"); return s }())
	require.Eventually(t, func() bool {
		e := watch.equipment("JHolder")
		return e["0"] != nil && e["0"].Name == "diamond_sword" && e["5"] != nil && e["5"].Name == "iron_helmet"
	}, 10*time.Second, 50*time.Millisecond, "the other Java player sees %v", watch.equipment("JHolder"))
	var held int
	onJava(t, srv, "JHolder", func(p *player.Player) { held = p.Data().HeldSlot })
	require.Equal(t, 2, held, "the server did not take the held slot")

	// Bedrock to Java.
	onBedrock(t, srv, "BHolder", func(p *player.Player) {
		_ = p.Inventory().SetItem(0, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1))
		p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.NewStack(item.Totem{}, 1))
		p.Armour().SetBoots(item.NewStack(item.Boots{Tier: item.ArmourTierDiamond{}}, 1))
	})
	require.Eventually(t, func() bool {
		e := j.equipment("BHolder")
		return e["0"] != nil && e["0"].Name == "iron_sword" && e["1"] != nil && e["1"].Name == "totem_of_undying" && e["2"] != nil && e["2"].Name == "diamond_boots"
	}, 10*time.Second, 50*time.Millisecond, "Java sees %v", j.equipment("BHolder"))
}
