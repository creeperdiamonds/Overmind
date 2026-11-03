package javafront

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
)

// Window 0 is the player's own inventory, its slots numbered as the Java client
// numbers them. minecraft-data has no slot layout; this is minecraft.wiki's
// "Inventory" page, and the stage 1 plan's.
const (
	slotArmour  = 5  // 5-8: helmet, chestplate, leggings, boots
	slotMain    = 9  // 9-35: Dragonfly's inventory slots 9-35
	slotHotbar  = 36 // 36-44: Dragonfly's inventory slots 0-8
	slotOffHand = 45
	windowSlots = 46 // 0 is the crafting result, 1-4 the crafting grid
	slotOutside = -999

	// cursorSlot is where Dragonfly keeps the item on the cursor, in the UI
	// inventory, as for a Bedrock client.
	cursorSlot = 0
)

// Equipment slots of entity_equipment. minecraft-data gives the field as a byte
// only; the numbers are minecraft.wiki's ("Set Equipment").
const (
	equipMainHand = 0
	equipOffHand  = 1
	equipBoots    = 2
	equipLeggings = 3
	equipChest    = 4
	equipHelmet   = 5
)

// Click modes and the buttons they use (window_click).
const (
	clickPickup  = 0 // button 0 left, 1 right; slot -999 drops the cursor
	clickShift   = 1
	clickSwap    = 2 // button 0-8 a hotbar slot, 40 the off hand
	clickClone   = 3 // creative middle click
	clickThrow   = 4 // button 0 one item, 1 the stack
	clickDrag    = 5 // dragging over slots (windows.go)
	clickCollect = 6 // double click: gather the cursor's item
	swapOffHand  = 40
)

// inventories are the player's inventories as HandleInventories gave them, and
// the window's state ID. World goroutine.
type inventories struct {
	inv, offHand, ui *inventory.Inventory
	armour           *inventory.Armour
	held             *uint32 // the hotbar slot held
	state            int32
	quiet            bool  // a click is being applied; the whole window follows
	win              int32 // the window a click is being applied in: 0, or the open block window
	lending          bool  // the off hand's item is in the main hand for a moment (withHand)
}

// HandleInventories is called each time the player is opened in a transaction.
// As a Bedrock session does, it shows changes to viewers, which see the held
// item and armour, and to the client, slot by slot.
func (c *conn) HandleInventories(tx *world.Tx, ctrl session.Controllable, inv, offHand, _, ui *inventory.Inventory, armour *inventory.Armour, heldSlot *uint32) {
	c.invs.inv, c.invs.offHand, c.invs.ui, c.invs.armour, c.invs.held = inv, offHand, ui, armour, heldSlot
	viewers := func(f func(world.Viewer)) {
		for _, v := range tx.Viewers(ctrl.Position()) {
			f(v)
		}
	}
	inv.SlotFunc(func(slot int, _, after item.Stack) {
		if slot == int(*heldSlot) && !c.invs.lending {
			viewers(func(v world.Viewer) { v.ViewEntityItems(ctrl) })
		}
		c.slotChanged(javaInventorySlot(slot), after)
	})
	offHand.SlotFunc(func(_ int, _, after item.Stack) {
		if c.invs.lending {
			return // withHand shows the hands once it is done
		}
		viewers(func(v world.Viewer) { v.ViewEntityItems(ctrl) })
		c.slotChanged(slotOffHand, after)
	})
	armour.Inventory().SlotFunc(func(slot int, before, after item.Stack) {
		c.slotChanged(slotArmour+slot, after)
		if !before.Comparable(after) || before.Empty() != after.Empty() {
			viewers(func(v world.Viewer) { v.ViewEntityArmour(ctrl) })
		}
	})
	ui.SlotFunc(func(slot int, _, after item.Stack) {
		if slot == cursorSlot && !c.invs.quiet {
			c.send(javaproto.SetCursor(javaItem(after)))
		}
	})
}

// javaInventorySlot is the window slot of a Dragonfly inventory slot.
func javaInventorySlot(slot int) int {
	if slot < 9 {
		return slotHotbar + slot
	}
	return slot
}

// slotRef finds the Dragonfly inventory and slot behind a slot of the window
// a click is applied in (c.invs.win): window 0, whose 2x2 crafting grid is in
// the UI inventory, or the open block window (windows.go). A crafting result
// has none: it is worked out.
func (c *conn) slotRef(slot int) (*inventory.Inventory, int, bool) {
	switch {
	case c.invs.inv == nil:
		return nil, 0, false
	case c.invs.win != 0:
		return c.windowSlot(slot)
	case slot > resultSlot && slot < slotArmour:
		return c.invs.ui, gridSmallOffset + slot - 1, true
	case slot >= slotArmour && slot < slotMain:
		return c.invs.armour.Inventory(), slot - slotArmour, true
	case slot >= slotMain && slot < slotHotbar:
		return c.invs.inv, slot, true
	case slot >= slotHotbar && slot < slotOffHand:
		return c.invs.inv, slot - slotHotbar, true
	case slot == slotOffHand:
		return c.invs.offHand, 0, true
	}
	return nil, 0, false
}

// slotChanged shows the client one changed slot, unless a click is being
// applied: the whole window is sent after it.
func (c *conn) slotChanged(slot int, after item.Stack) {
	if c.invs.quiet {
		return
	}
	c.send(javaproto.SetSlot(0, c.nextState(), int16(slot), javaItem(after)))
}

// nextState moves the window's state ID on, as vanilla does: 15 bits.
func (c *conn) nextState() int32 {
	c.invs.state = (c.invs.state + 1) & 0x7FFF
	return c.invs.state
}

// sendInventory sends the whole window and the cursor.
func (c *conn) sendInventory() {
	if c.invs.inv == nil {
		return
	}
	saved := c.invs.win
	c.invs.win = 0
	defer func() { c.invs.win = saved }()
	items := make([]javaproto.Item, windowSlots)
	for s := range items {
		if inv, i, ok := c.slotRef(s); ok {
			st, _ := inv.Item(i)
			items[s] = javaItem(st)
		}
	}
	if r, ok := c.craftResult(); ok {
		items[resultSlot] = javaItem(r)
	}
	cursor, _ := c.invs.ui.Item(cursorSlot)
	c.send(javaproto.WindowItems(0, c.nextState(), items, javaItem(cursor)))
}

// SendHeldSlot selects the hotbar slot the server has.
func (c *conn) SendHeldSlot(slot int, _ session.Controllable, _ bool) {
	c.send(javaproto.HeldSlot(int32(slot)))
}

// click applies a window click to the player's inventory, by Java's rules for
// the survival inventory, and then sends the whole window: the client guessed
// the result, and the server's word is final.
func (c *conn) click(p *player.Player, k javaproto.Click) {
	c.invs.quiet = true
	defer func() {
		c.invs.quiet = false
		c.invs.win = 0
		c.sendWindow(k.Window)
	}()
	if c.invs.inv == nil || k.Window != 0 && (c.win == nil || k.Window != c.win.id) {
		return // a window that is not open: it is sent again as it is
	}
	if k.Window != 0 && !c.windowCurrent(p.Tx()) {
		// The chest paired or lost its pair since the window opened: close it
		// rather than click in a copy. The cursor goes back as on any close.
		c.closeWindow(p.Tx(), true)
		return
	}
	c.invs.win = k.Window
	before := c.furnaceOutput()
	defer c.smeltingXP(p, before) // runs before the window is sent
	slot := int(k.Slot)
	cursor, _ := c.invs.ui.Item(cursorSlot)
	setCursor := func(s item.Stack) { _ = c.invs.ui.SetItem(cursorSlot, s) }
	if k.Mode != clickDrag {
		c.drag = dragState{} // any other click ends a drag
	}
	_, _, crafts := c.grid()
	if crafts && slot == resultSlot && (k.Mode == clickPickup || k.Mode == clickShift) {
		c.takeResult(p, k.Mode == clickShift, cursor, setCursor)
		return
	}
	switch k.Mode {
	case clickDrag:
		c.dragClick(p, k, cursor, setCursor)
	case clickCollect:
		if k.Button == 0 {
			c.collect(cursor, setCursor, c.windowSize())
		}
	case clickPickup:
		if slot == slotOutside {
			if k.Button == 0 {
				setCursor(dropped(p, cursor, cursor.Count()))
			} else if k.Button == 1 {
				setCursor(dropped(p, cursor, 1))
			}
			return
		}
		if k.Button != 0 && k.Button != 1 {
			return
		}
		c.pickup(slot, k.Button == 0, cursor, setCursor)
	case clickShift:
		inv, i, ok := c.slotRef(slot)
		if !ok {
			return
		}
		st, _ := inv.Item(i)
		if st.Empty() {
			return
		}
		left := c.moveTo(st, c.shiftTargets(slot, st), slot)
		_ = inv.SetItem(i, left)
	case clickSwap:
		other := -1
		switch {
		case k.Button >= 0 && k.Button < 9:
			other = c.hotbarSlot(int(k.Button))
		case k.Button == swapOffHand && k.Window == 0:
			other = slotOffHand
		}
		c.swap(slot, other)
	case clickClone:
		inv, i, ok := c.slotRef(slot)
		if !ok || !p.GameMode().CreativeInventory() || !cursor.Empty() {
			return
		}
		st, _ := inv.Item(i)
		if !st.Empty() {
			setCursor(st.Grow(st.MaxCount() - st.Count()))
		}
	case clickThrow:
		inv, i, ok := c.slotRef(slot)
		if !ok || !cursor.Empty() {
			return
		}
		st, _ := inv.Item(i)
		n := st.Count()
		if k.Button == 0 {
			n = min(n, 1)
		}
		_ = inv.SetItem(i, dropped(p, st, n))
	}
}

// pickup is a left (whole stack) or right (half, or one) click on a slot.
func (c *conn) pickup(slot int, left bool, cursor item.Stack, setCursor func(item.Stack)) {
	inv, i, ok := c.slotRef(slot)
	if !ok {
		return
	}
	have, _ := inv.Item(i)
	switch {
	case cursor.Empty() && have.Empty():
	case cursor.Empty():
		take := have.Count()
		if !left {
			take = (take + 1) / 2
		}
		if c.put(slot, inv, i, have.Grow(-take)) {
			setCursor(have.Grow(take - have.Count()))
		}
	case have.Empty() || have.Comparable(cursor):
		put := cursor.Count()
		if !left {
			put = 1
		}
		put = min(put, c.slotLimit(slot, cursor)-have.Count())
		if put <= 0 {
			return
		}
		next := cursor.Grow(put - cursor.Count())
		if !have.Empty() {
			next = have.Grow(put)
		}
		if c.put(slot, inv, i, next) {
			setCursor(cursor.Grow(-put))
		}
	default:
		// Different items: they change places, if the slot takes the cursor's.
		if cursor.Count() <= c.slotLimit(slot, cursor) && c.put(slot, inv, i, cursor) {
			setCursor(have)
		}
	}
}

// swap exchanges two window slots, if each takes the other's item.
func (c *conn) swap(a, b int) {
	ia, sa, okA := c.slotRef(a)
	ib, sb, okB := c.slotRef(b)
	if !okA || !okB || a == b {
		return
	}
	x, _ := ia.Item(sa)
	y, _ := ib.Item(sb)
	if x.Count() > c.slotLimit(b, x) || y.Count() > c.slotLimit(a, y) {
		return
	}
	if !c.put(b, ib, sb, x) {
		return
	}
	if !c.put(a, ia, sa, y) {
		_ = ib.SetItem(sb, y)
	}
}

// placed sets a slot and reports whether it took the stack. Dragonfly's SetItem
// refuses an item its slot does not take (armour, the off hand) by doing
// nothing, with no error.
func placed(inv *inventory.Inventory, slot int, s item.Stack) bool {
	_ = inv.SetItem(slot, s)
	got, _ := inv.Item(slot)
	return got.Equal(s)
}

// slotLimit is the most of an item a window slot holds: one in an armour slot.
func (c *conn) slotLimit(slot int, s item.Stack) int {
	if c.invs.win == 0 && slot >= slotArmour && slot < slotMain {
		return 1
	}
	return s.MaxCount()
}

// shiftTargets are the slots a shift-click moves an item to, in order, as the
// Java inventory does: armour to its slot if that is free, then the main
// inventory to the hotbar and the hotbar to the main inventory; from armour and
// the off hand, the inventory. Java also moves a shield to a free off hand;
// Dragonfly has no shield.
func (c *conn) shiftTargets(from int, s item.Stack) []int {
	if c.invs.win != 0 {
		return c.windowShiftTargets(from, s)
	}
	var out []int
	if from < slotMain || from == slotOffHand {
		for i := slotMain; i < slotOffHand; i++ {
			out = append(out, i)
		}
		return out
	}
	if a := armourSlot(s); a >= 0 {
		out = append(out, a)
	}
	lo, hi := slotHotbar, slotOffHand
	if from >= slotHotbar {
		lo, hi = slotMain, slotHotbar
	}
	for i := lo; i < hi; i++ {
		out = append(out, i)
	}
	return out
}

// armourSlot is the window slot an armour item is worn in, or -1.
func armourSlot(s item.Stack) int {
	switch it := s.Item().(type) {
	case item.HelmetType:
		if it.Helmet() {
			return slotArmour
		}
	}
	switch it := s.Item().(type) {
	case item.ChestplateType:
		if it.Chestplate() {
			return slotArmour + 1
		}
	}
	switch it := s.Item().(type) {
	case item.LeggingsType:
		if it.Leggings() {
			return slotArmour + 2
		}
	}
	switch it := s.Item().(type) {
	case item.BootsType:
		if it.Boots() {
			return slotArmour + 3
		}
	}
	return -1
}

// moveTo puts as much of s as fits into the slots given, first onto stacks of
// the same item, then into empty slots, and returns what is left. The slot it
// comes from is skipped.
func (c *conn) moveTo(s item.Stack, slots []int, from int) item.Stack {
	for pass := 0; pass < 2 && !s.Empty(); pass++ {
		for _, slot := range slots {
			if s.Empty() {
				break
			}
			inv, i, ok := c.slotRef(slot)
			if !ok || slot == from {
				continue
			}
			have, _ := inv.Item(i)
			room := c.slotLimit(slot, s) - have.Count()
			n := min(room, s.Count())
			if n <= 0 {
				continue
			}
			var next item.Stack
			switch {
			case pass == 0 && !have.Empty() && have.Comparable(s):
				next = have.Grow(n)
			case pass == 1 && have.Empty():
				next = s.Grow(n - s.Count())
			default:
				continue
			}
			if c.put(slot, inv, i, next) {
				s = s.Grow(-n)
			}
		}
	}
	return s
}

// dropHeld drops the held stack, or one item of it.
func (c *conn) dropHeld(p *player.Player, all bool) {
	if c.invs.inv == nil {
		return
	}
	slot := int(*c.invs.held)
	held, _ := c.invs.inv.Item(slot)
	n := 1
	if all {
		n = held.Count()
	}
	_ = c.invs.inv.SetItem(slot, dropped(p, held, n))
}

// swapHands swaps the held item and the off hand. Dragonfly's off hand takes
// only items Bedrock allows there, unlike Java's; anything else stays put, and
// the client is corrected.
func (c *conn) swapHands(p *player.Player) {
	main, off := p.HeldItems()
	if o, ok := main.Item().(item.OffHand); !main.Empty() && (!ok || !o.OffHand()) {
		c.sendInventory()
		return
	}
	p.SetHeldItems(off, main)
}

// dropped drops n of s from the player and returns the rest of s.
func dropped(p *player.Player, s item.Stack, n int) item.Stack {
	if s.Empty() || n <= 0 {
		return s
	}
	n = min(n, s.Count())
	return s.Grow(-p.Drop(s.Grow(n - s.Count())))
}

// setCreative puts an item a creative client chose in a window slot, or drops
// it for slot -1.
func (c *conn) setCreative(p *player.Player, ci javaproto.CreativeItem) {
	if !p.GameMode().CreativeInventory() {
		c.sendInventory() // as vanilla: refused, and the client corrected
		return
	}
	s := dragonflyItem(ci)
	if !s.Empty() && int(ci.Count) > s.MaxCount() {
		c.sendInventory() // more than a stack holds is not an item
		return
	}
	if ci.Slot == -1 {
		if !s.Empty() {
			p.Drop(s)
		}
		return
	}
	inv, i, ok := c.slotRef(int(ci.Slot))
	if !ok || s.Count() > c.slotLimit(int(ci.Slot), s) || !placed(inv, i, s) {
		c.sendInventory()
	}
}

// equipped is one piece of equipment for entity_equipment.
func equipped(slot byte, s item.Stack) javaproto.Equipped {
	return javaproto.Equipped{Slot: slot, Item: javaItem(s)}
}

// ViewEntityItems shows what a player holds, in both hands.
func (c *conn) ViewEntityItems(e world.Entity) {
	if !c.viewing() {
		return
	}
	s, ok := c.ents[e.H()]
	carrier, isCarrier := e.(item.Carrier)
	if !ok || !isCarrier {
		return
	}
	main, off := carrier.HeldItems()
	c.send(javaproto.Equipment(s.id, []javaproto.Equipped{equipped(equipMainHand, main), equipped(equipOffHand, off)}))
}

// ViewEntityArmour shows what a player wears.
func (c *conn) ViewEntityArmour(e world.Entity) {
	if !c.viewing() {
		return
	}
	s, ok := c.ents[e.H()]
	armoured, isArmoured := e.(interface{ Armour() *inventory.Armour })
	if !ok || !isArmoured || armoured.Armour() == nil {
		return
	}
	a := armoured.Armour()
	c.send(javaproto.Equipment(s.id, []javaproto.Equipped{
		equipped(equipHelmet, a.Helmet()), equipped(equipChest, a.Chestplate()),
		equipped(equipLeggings, a.Leggings()), equipped(equipBoots, a.Boots()),
	}))
}

// Item components Dragonfly items carry over to Java.
var (
	componentDamage       = java261.ComponentType("damage")
	componentMaxDamage    = java261.ComponentType("max_damage")
	componentCustomName   = java261.ComponentType("custom_name")
	componentEnchantments = java261.ComponentType("enchantments")
	barrierItem, _        = java261.JavaItem("minecraft:barrier", 0)
)

// javaItem is a Dragonfly stack as a Java slot: the Java item by the item
// tables, with durability, a custom name and enchantments as components. An
// item Java has no counterpart for shows as a barrier named after it.
func javaItem(s item.Stack) javaproto.Item {
	if s.Empty() {
		return javaproto.Item{}
	}
	name, meta := s.Item().EncodeItem()
	out := javaproto.Item{Count: int32(s.Count())}
	id, ok := java261.JavaItem(name, meta)
	if !ok {
		id = barrierItem
		if s.CustomName() == "" {
			out.Components = append(out.Components, javaproto.TextComponent(componentCustomName, javaproto.Text{Text: name}))
		}
	}
	out.ID = id
	if max := s.MaxDurability(); max > 0 {
		// Dragonfly's durability, which may differ from Java's own for the item.
		out.Components = append(out.Components, javaproto.VarIntComponent(componentMaxDamage, int32(max)))
		if d := max - s.Durability(); d > 0 {
			out.Components = append(out.Components, javaproto.VarIntComponent(componentDamage, int32(d)))
		}
	}
	if n := s.CustomName(); n != "" {
		out.Components = append(out.Components, javaproto.TextComponent(componentCustomName, javaproto.Text{Text: n}))
	}
	var ench []javaproto.Enchantment
	for _, e := range s.Enchantments() {
		if id, ok := javaEnchantment(e.Type()); ok {
			ench = append(ench, javaproto.Enchantment{ID: id, Level: int32(e.Level())})
		}
	}
	if len(ench) > 0 {
		out.Components = append(out.Components, javaproto.EnchantmentsComponent(componentEnchantments, ench))
	}
	return out
}

// dragonflyItem is the item a creative client chose, by the item tables, with
// its damage. Other components are not read yet. An item Dragonfly does not
// have gives an empty stack.
func dragonflyItem(ci javaproto.CreativeItem) item.Stack {
	name, meta, ok := java261.BedrockItem(ci.ID)
	if !ok || ci.Count <= 0 {
		return item.Stack{}
	}
	it, ok := world.ItemByName(name, meta)
	if !ok {
		return item.Stack{}
	}
	s := item.NewStack(it, int(ci.Count))
	if d, ok := javaproto.VarIntData(ci.Components[componentDamage]); ok && s.MaxDurability() > 0 {
		s = s.WithDurability(s.MaxDurability() - int(d))
	}
	return s
}

// enchantmentIDs is the enchantment registry the client was sent, by key.
var enchantmentIDs = sync.OnceValue(func() map[string]int32 {
	m := map[string]int32{}
	for _, r := range java261.Registries() {
		if r.ID == "minecraft:enchantment" {
			for i, e := range r.Entries {
				m[e] = int32(i)
			}
		}
	}
	return m
})

// javaEnchantment finds a Dragonfly enchantment in Java's registry by its name:
// "Fire Aspect" is minecraft:fire_aspect, and a curse is named the other way
// round ("Curse of Vanishing" is minecraft:vanishing_curse).
func javaEnchantment(t item.EnchantmentType) (int32, bool) {
	key := strings.ReplaceAll(strings.ToLower(t.Name()), " ", "_")
	if rest, ok := strings.CutPrefix(key, "curse_of_"); ok {
		key = rest + "_curse"
	}
	id, ok := enchantmentIDs()["minecraft:"+key]
	return id, ok
}

// hotbarSlot is the slot of a hotbar slot (0-8) in the window a click is
// applied in.
func (c *conn) hotbarSlot(i int) int {
	if c.invs.win != 0 && c.win != nil {
		return c.win.slots + 27 + i
	}
	return slotHotbar + i
}

// windowSize is how many slots the window a click is applied in has.
func (c *conn) windowSize() int {
	if c.invs.win != 0 && c.win != nil {
		return c.win.slots + 36
	}
	return windowSlots
}
