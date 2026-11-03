package javafront

import (
	"slices"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
)

// Block windows. A block Dragonfly opens for a player (its Activate calls
// OpenBlockContainer) opens as the Java window of that kind. The window's own
// slots come first, then the player's main inventory (27) and hotbar (9), as
// minecraft.wiki's "Java Edition protocol/Inventory" (oldid=3201379) lays out
// each menu. Clicks go through the same click engine as window 0
// (inventory.go), by the window's slot map; the server's result is sent back
// as the whole window.
//
// The crafting grids are Dragonfly's own UI inventory slots, where a Bedrock
// client keeps its grid (28-31 the 2x2, 32-40 the 3x3), so closing a window
// or leaving puts what is in them back, as for Bedrock. The result is worked
// out by Java's recipes (java261.Craft): crafting is the player's own rule.

const (
	gridSmallOffset = 28 // Dragonfly's UI slots of the 2x2 grid, as for Bedrock
	gridLargeOffset = 32 // and of the 3x3 grid
	resultSlot      = 0  // the crafting result in window 0 and a crafting table
)

// windowKind is what a block window holds.
type windowKind int

const (
	kindContainer windowKind = iota // the block's own inventory
	kindCrafting                    // a crafting table: the 3x3 grid in the UI inventory
)

// window is the block window a client has open. World goroutine.
type window struct {
	id    int32
	kind  windowKind
	pos   cube.Pos
	inv   *inventory.Inventory // the block's slots; nil for a crafting table
	slots int                  // the window's own slots, before the player's inventory
	order []int                // the block's slot behind each window slot (nil: the same)
	block world.Block          // the block opened, to tell it the viewer left
	cook  int16                // a furnace's ticks per item, 0 for anything else
}

// containerSlot is the block's slot behind a window slot.
func (w *window) containerSlot(slot int) int {
	if w.order != nil {
		return w.order[slot]
	}
	return slot
}

// windowSlotOf is the window slot showing a block's slot, -1 if none.
func (w *window) windowSlotOf(slot int) int {
	if w.order == nil {
		return slot
	}
	return slices.Index(w.order, slot)
}

// brewingOrder: Java's brewing stand window has the bottles at 0-2, the
// ingredient at 3 and blaze powder at 4 (the Inventory page above);
// Dragonfly's brewer keeps the ingredient at 0, bottles at 1-3, fuel at 4.
var brewingOrder = []int{1, 2, 3, 0, 4}

// OpenBlockContainer opens the window of the block at pos: the Reporter
// method a block's Activate reaches through the player.
func (c *conn) OpenBlockContainer(pos cube.Pos, tx *world.Tx) {
	if c.win != nil && c.win.pos == pos {
		return
	}
	c.closeWindow(tx, false)
	e, ok := c.handle.Entity(tx)
	if !ok {
		return
	}
	p := e.(*player.Player)
	b := tx.Block(pos)
	w := &window{pos: pos, block: b}
	var menu, title string
	custom := ""
	switch v := b.(type) {
	case block.CraftingTable:
		w.kind, w.slots, menu, title = kindCrafting, 10, "crafting", "container.crafting"
	case block.EnderChest:
		v.AddViewer(tx, pos)
		w.inv, w.slots, menu, title = p.EnderChestInventory(), 27, "generic_9x3", "container.enderchest"
	case block.Container:
		v.AddViewer(c, tx, pos) // a chest may pair here
		v = tx.Block(pos).(block.Container)
		w.block = tx.Block(pos)
		w.inv = v.Inventory(tx, pos)
		w.slots = w.inv.Size()
		switch b := v.(type) {
		case block.Chest:
			menu, title, custom = "generic_9x3", "container.chest", b.CustomName
			if w.slots == 54 {
				menu, title = "generic_9x6", "container.chestDouble"
			}
		case block.Barrel:
			menu, title, custom = "generic_9x3", "container.barrel", b.CustomName
		case block.ShulkerBox:
			menu, title, custom = "shulker_box", "container.shulkerBox", b.CustomName
		case block.Hopper:
			menu, title, custom = "hopper", "container.hopper", b.CustomName
		case block.Furnace:
			menu, title, w.cook = "furnace", "container.furnace", 200
		case block.BlastFurnace:
			menu, title, w.cook = "blast_furnace", "container.blast_furnace", 100
		case block.Smoker:
			menu, title, w.cook = "smoker", "container.smoker", 100
		case block.BrewingStand:
			menu, title, w.order = "brewing_stand", "container.brewing", brewingOrder
		}
	}
	id := java261.MenuType(menu)
	if menu == "" || id < 0 {
		// A window Java has but this front end does not open yet (anvil,
		// enchanting, loom and the like): the client stays as it is.
		if ct, ok := w.block.(block.Container); ok {
			ct.RemoveViewer(c, tx, pos)
		}
		return
	}
	c.nextWin = c.nextWin%100 + 1 // the client takes 1 to 100, as vanilla cycles them
	w.id = c.nextWin
	c.win = w
	t := javaproto.Text{Translate: title}
	if custom != "" {
		t = javaproto.Text{Text: custom}
	}
	c.send(javaproto.OpenWindow(w.id, id, t))
	c.sendWindow(w.id)
	if w.cook > 0 {
		c.sendFurnace(smelterOf(w.block))
	}
}

// smelterOf gives a smelter's durations, if the block has them.
func smelterOf(b world.Block) interface {
	Durations() (time.Duration, time.Duration, time.Duration)
} {
	d, _ := b.(interface {
		Durations() (time.Duration, time.Duration, time.Duration)
	})
	return d
}

// sendFurnace sends a furnace window's four properties: the fuel left and the
// fuel's whole burn time, the progress and the time an item takes, in ticks
// (minecraft.wiki Packets oldid=3868170, "Set Container Property": 0 fire
// icon, 1 maximum fuel burn time, 2 progress arrow, 3 maximum progress).
func (c *conn) sendFurnace(d interface {
	Durations() (time.Duration, time.Duration, time.Duration)
}) {
	if d == nil || c.win == nil {
		return
	}
	remaining, max, cook := d.Durations()
	c.furnaceProps(remaining, max, cook)
	c.send(javaproto.WindowProperty(c.win.id, 3, c.win.cook))
}

func (c *conn) furnaceProps(remaining, max, cook time.Duration) {
	ticks := func(d time.Duration) int16 { return int16(d / tick) }
	c.send(javaproto.WindowProperty(c.win.id, 0, ticks(remaining)))
	c.send(javaproto.WindowProperty(c.win.id, 1, ticks(max)))
	c.send(javaproto.WindowProperty(c.win.id, 2, ticks(cook)))
}

// ViewFurnaceUpdate shows a furnace's progress in its open window.
func (c *conn) ViewFurnaceUpdate(prevCook, cook, prevRemaining, remaining, prevMax, max time.Duration) {
	if !c.viewing() {
		return
	}
	if c.win == nil || c.win.cook == 0 {
		return
	}
	if prevCook != cook || prevRemaining != remaining || prevMax != max {
		c.furnaceProps(remaining, max, cook)
	}
}

// ViewSlotChange shows a change to the open block's slots, made by anyone:
// another player, a hopper, the furnace itself. A click being applied sends
// the whole window after.
func (c *conn) ViewSlotChange(slot int, it item.Stack) {
	if !c.viewing() {
		return
	}
	if c.win == nil || c.win.inv == nil || c.invs.quiet {
		return
	}
	if s := c.win.windowSlotOf(slot); s >= 0 {
		c.send(javaproto.SetSlot(c.win.id, c.nextState(), int16(s), javaItem(it)))
	}
}

// CloseContainer closes the window the server closes (a spectator, say).
func (c *conn) CloseContainer(tx *world.Tx) { c.closeWindow(tx, true) }

// closeWindow closes the open block window: the block is told its viewer has
// gone, and what is in the crafting grid and on the cursor goes back to the
// inventory. tell sends the client a close too, for a close the server made.
func (c *conn) closeWindow(tx *world.Tx, tell bool) {
	w := c.win
	if w == nil {
		return
	}
	c.win = nil
	c.drag = dragState{}
	switch b := w.block.(type) {
	case block.EnderChest:
		b.RemoveViewer(tx, w.pos)
	case block.Container:
		if _, still := tx.Block(w.pos).(block.Container); still {
			b.RemoveViewer(c, tx, w.pos)
		}
	}
	if e, ok := c.handle.Entity(tx); ok {
		e.(*player.Player).MoveItemsToInventory()
	}
	if tell {
		c.send(javaproto.CloseWindow(w.id))
	}
}

// closeWindowIn handles close_window from the client: its block window, or
// the inventory, whose grid and cursor go back as on Java.
func (c *conn) closeWindowIn(tx *world.Tx, p *player.Player, id int32) {
	if c.win != nil && id == c.win.id {
		c.closeWindow(tx, false)
		return
	}
	c.drag = dragState{}
	p.MoveItemsToInventory()
}

// checkWindow closes a block window whose block is gone or changed kind, or
// whose player died (the client closes its screens for the death screen).
func (c *conn) checkWindow(tx *world.Tx, p *player.Player) {
	if c.win == nil {
		return
	}
	now := tx.Block(c.win.pos)
	if now == nil || blockName(now) != blockName(c.win.block) || p.Dead() || !c.windowCurrent(tx) {
		c.closeWindow(tx, true)
	}
}

// windowCurrent reports whether the open window still shows the block's own
// inventory. Dragonfly gives a chest a new inventory when it pairs or its pair
// goes (Chest.pair and unpair clone both halves), and a window on the old one
// would move items out of a copy: duplicated. Found by the batch C review.
func (c *conn) windowCurrent(tx *world.Tx) bool {
	w := c.win
	_, ok := w.block.(block.Container)
	if !ok {
		return true // a crafting table, an ender chest (the player's own)
	}
	now, ok := tx.Block(w.pos).(block.Container)
	if !ok {
		return false
	}
	inv := now.Inventory(tx, w.pos)
	return inv == w.inv && inv.Size() == w.slots
}

func blockName(b world.Block) string {
	n, _ := b.EncodeBlock()
	return strings.TrimPrefix(strings.TrimPrefix(n, "minecraft:"), "lit_")
}

// sendWindow sends every slot of a window (0, or the open block window) and
// the cursor.
func (c *conn) sendWindow(id int32) {
	if id == 0 || c.win == nil || id != c.win.id {
		c.sendInventory()
		return
	}
	saved := c.invs.win
	c.invs.win = id
	defer func() { c.invs.win = saved }()
	items := make([]javaproto.Item, c.win.slots+36)
	for s := range items {
		if inv, i, ok := c.slotRef(s); ok {
			st, _ := inv.Item(i)
			items[s] = javaItem(st)
		}
	}
	if c.win.kind == kindCrafting {
		if r, ok := c.craftResult(); ok {
			items[resultSlot] = javaItem(r)
		}
	}
	cursor, _ := c.invs.ui.Item(cursorSlot)
	c.send(javaproto.WindowItems(id, c.nextState(), items, javaItem(cursor)))
}

// windowSlot is the inventory and slot behind a slot of the open block window.
func (c *conn) windowSlot(slot int) (*inventory.Inventory, int, bool) {
	w := c.win
	switch {
	case w == nil || slot < 0:
		return nil, 0, false
	case slot < w.slots:
		if w.kind == kindCrafting {
			if slot == resultSlot {
				return nil, 0, false
			}
			return c.invs.ui, gridLargeOffset + slot - 1, true
		}
		return w.inv, w.containerSlot(slot), true
	case slot < w.slots+27:
		return c.invs.inv, slotMain + slot - w.slots, true
	case slot < w.slots+36:
		return c.invs.inv, slot - w.slots - 27, true
	}
	return nil, 0, false
}

// windowShiftTargets are the slots a shift-click moves to in a block window,
// as Java's menus do: from the block's slots to the player's inventory, the
// hotbar's last slot first; from the inventory into the block's slots (a
// furnace's input if it smelts, else its fuel if it burns), or for a crafting
// table between the main inventory and the hotbar.
func (c *conn) windowShiftTargets(from int, s item.Stack) []int {
	w := c.win
	var out []int
	if from < w.slots {
		for i := w.slots + 35; i >= w.slots; i-- {
			out = append(out, i)
		}
		return out
	}
	switch {
	case w.kind == kindCrafting:
	case w.cook > 0:
		if _, ok := s.Item().(item.Smeltable); ok {
			return []int{0}
		}
		if burns(s) {
			return []int{1}
		}
	default:
		for i := 0; i < w.slots; i++ {
			out = append(out, i)
		}
		return out
	}
	lo, hi := w.slots+27, w.slots+36 // main inventory to the hotbar
	if from >= w.slots+27 {
		lo, hi = w.slots, w.slots+27
	}
	for i := lo; i < hi; i++ {
		out = append(out, i)
	}
	return out
}

// --- crafting ---

// grid is the crafting grid of the window a click is applied in: its first
// UI slot and its width; false for a window without one.
func (c *conn) grid() (offset, size int, ok bool) {
	if c.invs.win == 0 {
		return gridSmallOffset, 2, true
	}
	if c.win != nil && c.win.kind == kindCrafting {
		return gridLargeOffset, 3, true
	}
	return 0, 0, false
}

// craftResult is what the grid makes by Java's recipes, as a Dragonfly item.
func (c *conn) craftResult() (item.Stack, bool) {
	offset, size, ok := c.grid()
	if !ok || c.invs.ui == nil {
		return item.Stack{}, false
	}
	ids := make([]int32, size*size)
	for i := range ids {
		ids[i] = -1
		st, _ := c.invs.ui.Item(offset + i)
		if st.Empty() {
			continue
		}
		name, meta := st.Item().EncodeItem()
		id, ok := java261.JavaItem(name, meta)
		if !ok {
			return item.Stack{}, false // nothing of Java's: no recipe has it
		}
		ids[i] = id
	}
	r, ok := java261.Craft(ids, size)
	if !ok {
		return item.Stack{}, false
	}
	name, meta, ok := java261.BedrockItem(r.Result)
	if !ok {
		return item.Stack{}, false
	}
	it, ok := world.ItemByName(name, meta)
	if !ok {
		return item.Stack{}, false
	}
	return item.NewStack(it, r.Count), true
}

// craft takes the result once: one of each item in the grid is used up.
func (c *conn) craftOnce(p *player.Player) {
	offset, size, _ := c.grid()
	for i := 0; i < size*size; i++ {
		st, _ := c.invs.ui.Item(offset + i)
		if st.Empty() {
			continue
		}
		left := st.Grow(-1)
		_ = c.invs.ui.SetItem(offset+i, left)
		// What the ingredient leaves (a milk bucket's bucket) stays in the
		// grid on Java; where the cell still holds more, it goes to the
		// inventory, or drops.
		rest, ok := craftRemainder(st)
		if !ok {
			continue
		}
		if left.Empty() {
			_ = c.invs.ui.SetItem(offset+i, rest)
		} else if n, _ := c.invs.inv.AddItem(rest); n < rest.Count() {
			p.Drop(rest.Grow(-n))
		}
	}
}

// craftRemainder is what a Dragonfly item leaves in the grid by Java's rules
// (java261.CraftRemainder).
func craftRemainder(s item.Stack) (item.Stack, bool) {
	name, meta := s.Item().EncodeItem()
	id, ok := java261.JavaItem(name, meta)
	if !ok {
		return item.Stack{}, false
	}
	rest := java261.CraftRemainder(java261.JavaItemName(id))
	if rest == "" {
		return item.Stack{}, false
	}
	bn, bm, ok := java261.BedrockItem(java261.JavaItemID(rest))
	if !ok {
		return item.Stack{}, false
	}
	it, ok := world.ItemByName(bn, bm)
	if !ok {
		return item.Stack{}, false
	}
	return item.NewStack(it, 1), true
}

// takeResult is a click on the result slot. A plain click crafts once onto
// the cursor, if the cursor is empty or holds the same item with room for it;
// a shift-click crafts as long as the grid makes the same thing and the
// inventory has room, as Java does.
func (c *conn) takeResult(p *player.Player, shift bool, cursor item.Stack, setCursor func(item.Stack)) {
	for n := 0; n < 64; n++ {
		r, ok := c.craftResult()
		if !ok {
			return
		}
		if !shift {
			if !cursor.Empty() && (!cursor.Comparable(r) || cursor.Count()+r.Count() > cursor.MaxCount()) {
				return
			}
			c.craftOnce(p)
			if cursor.Empty() {
				setCursor(r)
			} else {
				setCursor(cursor.Grow(r.Count()))
			}
			return
		}
		// The inventory must take the result and what the craft leaves
		// that does not stay in the grid (a bottle from a cell of several
		// honey bottles) together: checking the result alone let the
		// remainder take the last free slot and the result be lost.
		if !roomFor(c.invs.inv, append(c.leftovers(), r)...) {
			return
		}
		c.craftOnce(p)
		if n, _ := c.invs.inv.AddItem(r); n < r.Count() {
			p.Drop(r.Grow(-n)) // never happens after roomFor; never lost if it does
		}
	}
}

// leftovers is what one craft from the grid as it is would send to the
// inventory: the remainder of each cell that holds more than one item.
func (c *conn) leftovers() []item.Stack {
	offset, size, _ := c.grid()
	var out []item.Stack
	for i := 0; i < size*size; i++ {
		st, _ := c.invs.ui.Item(offset + i)
		if st.Count() < 2 {
			continue
		}
		if rest, ok := craftRemainder(st); ok {
			out = append(out, rest)
		}
	}
	return out
}

// roomFor reports whether an inventory can take all the stacks at once,
// filling stacks of the same item first and then empty slots, as AddItem does.
func roomFor(inv *inventory.Inventory, stacks ...item.Stack) bool {
	slots := inv.Slots()
	for _, s := range stacks {
		n := s.Count()
		for i, have := range slots {
			if n > 0 && !have.Empty() && have.Comparable(s) {
				add := min(n, max(0, have.MaxCount()-have.Count()))
				slots[i], n = have.Grow(add), n-add
			}
		}
		for i, have := range slots {
			if n > 0 && have.Empty() {
				add := min(n, s.MaxCount())
				slots[i], n = s.Grow(add-s.Count()), n-add
			}
		}
		if n > 0 {
			return false
		}
	}
	return true
}

// --- dragging and collecting ---

// dragState is a drag the client is painting (window_click mode 5).
type dragState struct {
	button int8 // 0 left, 4 right, 8 middle; -1 none
	active bool
	slots  []int
}

// drag applies window_click mode 5, which comes as a start (button 0, 4 or 8,
// slot -999), a packet per slot painted (1, 5 or 9) and an end (2, 6 or 10),
// minecraft.wiki Packets oldid=3868170 ("Click Container"). At the end a left
// drag shares the cursor evenly between the slots, a right drag puts one in
// each, and a middle drag (creative only) a whole stack in each. A packet out
// of order resets the drag.
func (c *conn) dragClick(p *player.Player, k javaproto.Click, cursor item.Stack, setCursor func(item.Stack)) {
	kind, stage := k.Button&^3, k.Button&3
	switch {
	case stage == 0 && k.Slot == slotOutside && (kind == 0 || kind == 4 || kind == 8):
		c.drag = dragState{button: kind, active: true}
		return
	case !c.drag.active || kind != c.drag.button:
		c.drag = dragState{}
		return
	case stage == 1:
		if !slices.Contains(c.drag.slots, int(k.Slot)) {
			c.drag.slots = append(c.drag.slots, int(k.Slot))
		}
		return
	case stage != 2 || k.Slot != slotOutside:
		c.drag = dragState{}
		return
	}
	d := c.drag
	c.drag = dragState{}
	if cursor.Empty() || len(d.slots) == 0 || d.button == 8 && !p.GameMode().CreativeInventory() {
		return
	}
	var each int
	switch d.button {
	case 0:
		each = cursor.Count() / len(d.slots)
	case 4:
		each = 1
	case 8:
		each = cursor.MaxCount()
	}
	left := cursor.Count()
	for _, slot := range d.slots {
		inv, i, ok := c.slotRef(slot)
		if !ok || (d.button != 8 && left <= 0) {
			continue
		}
		have, _ := inv.Item(i)
		if !have.Empty() && !have.Comparable(cursor) {
			continue
		}
		n := min(each, c.slotLimit(slot, cursor)-have.Count())
		if d.button != 8 {
			n = min(n, left)
		}
		if n <= 0 {
			continue
		}
		next := cursor.Grow(n - cursor.Count())
		if !have.Empty() {
			next = have.Grow(n)
		}
		if c.put(slot, inv, i, next) && d.button != 8 {
			left -= n
		}
	}
	if d.button != 8 {
		setCursor(cursor.Grow(left - cursor.Count()))
	}
}

// collect applies a double click (mode 6, button 0): the cursor gathers its
// own item from the window's slots up to a full stack, stacks that are not
// full first, then full ones, in slot order.
func (c *conn) collect(cursor item.Stack, setCursor func(item.Stack), slots int) {
	if cursor.Empty() {
		return
	}
	for pass := 0; pass < 2 && cursor.Count() < cursor.MaxCount(); pass++ {
		for s := 0; s < slots && cursor.Count() < cursor.MaxCount(); s++ {
			inv, i, ok := c.slotRef(s)
			if !ok {
				continue
			}
			have, _ := inv.Item(i)
			if have.Empty() || !have.Comparable(cursor) || (pass == 0) == (have.Count() == have.MaxCount()) {
				continue
			}
			n := min(have.Count(), cursor.MaxCount()-cursor.Count())
			if placed(inv, i, have.Grow(-n)) {
				cursor = cursor.Grow(n)
			}
		}
	}
	setCursor(cursor)
}

// --- signs ---

// OpenSign opens the sign editor, for a sign the player placed or clicked.
func (c *conn) OpenSign(pos cube.Pos, front bool) {
	c.sign = &openSign{pos: pos, front: front}
	// Whatever changes the block from now on (broken, replaced, edited by
	// someone else) ends this open: see ViewBlockUpdate.
	c.send(javaproto.OpenSign(pos[0], pos[1], pos[2], front))
}

// openSign is the side of a sign the client was asked to edit.
type openSign struct {
	pos   cube.Pos
	front bool
}

// editSign applies update_sign, for the sign and side the server opened only:
// the four lines become the side's text through Dragonfly's EditSign, so its
// rules and handler apply, as for a Bedrock player. Characters Java does not
// let a player type are dropped.
func (c *conn) editSign(p *player.Player, u javaproto.SignUpdate) {
	open := c.sign
	c.sign = nil
	pos := cube.Pos{u.X, u.Y, u.Z}
	if open == nil || open.pos != pos || open.front != u.Front {
		return
	}
	s, ok := p.Tx().Block(pos).(block.Sign)
	if !ok || !c.rules.CanReachBlock(entity.EyePosition(p), pos, p.GameMode().CreativeInventory()) {
		return
	}
	// Nothing is cut here: javaproto.ReadUpdateSign already refused a line
	// over update_sign's 384 characters (minecraft.wiki Packets
	// oldid=3868170: "String with max length 384"), counted by character, as
	// a malformed packet, which disconnects. Dropping characters below keeps
	// whole characters, so Bedrock readers get valid text.
	lines := make([]string, 0, 4)
	for _, l := range u.Lines {
		lines = append(lines, strings.Map(func(r rune) rune {
			if allowedInChat(string(r)) {
				return r
			}
			return -1
		}, l))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	text := strings.Join(lines, "\n")
	front, back := s.Front.Text, s.Back.Text
	if u.Front {
		front = text
	} else {
		back = text
	}
	_ = p.EditSign(pos, front, back)
}

// put sets a window slot if the slot takes the stack, and reports whether it
// did. A furnace's output slot only gives (a stack may only get smaller), and
// its fuel slot takes only fuel or one empty bucket (minecraft.wiki "Furnace"
// oldid=3823010: "It is no longer possible to put items into the output slot",
// "Furnaces now allow only fuel or a single empty bucket in the fuel slot").
// Taking the output releases the experience the furnace kept (the same page:
// "released the next time a player takes the output").
func (c *conn) put(slot int, inv *inventory.Inventory, i int, s item.Stack) bool {
	have, _ := inv.Item(i)
	if !c.mayPlace(slot, have, s) {
		return false
	}
	return placed(inv, i, s)
}

func (c *conn) mayPlace(slot int, have, next item.Stack) bool {
	if c.invs.win == 0 || c.win == nil || c.win.cook == 0 || next.Empty() {
		return true
	}
	smaller := !have.Empty() && next.Comparable(have) && next.Count() <= have.Count()
	switch slot {
	case 2:
		return smaller
	case 1:
		b, bucket := next.Item().(item.Bucket)
		return smaller || burns(next) || bucket && b.Empty() && next.Count() == 1
	}
	return true
}

// burns reports whether a stack is fuel: an item.Fuel with a burn time. A
// water or milk bucket and an empty bucket are item.Fuel too (Bucket has
// FuelInfo for lava), with no burn time, so they are not.
func burns(s item.Stack) bool {
	f, ok := s.Item().(item.Fuel)
	return ok && f.FuelInfo().Duration > 0
}

// furnaceOutput is how many items the open furnace's output holds, -1 when no
// furnace is open.
func (c *conn) furnaceOutput() int {
	if c.win == nil || c.win.cook == 0 {
		return -1
	}
	st, _ := c.win.inv.Item(2)
	return st.Count()
}

// smeltingXP gives a player who took a furnace's output the experience the
// furnace kept for it, as orbs at its eyes, as Dragonfly does for Bedrock
// (session's collectRewards).
func (c *conn) smeltingXP(p *player.Player, before int) {
	if before < 0 || c.win == nil || c.furnaceOutput() >= before {
		return
	}
	f, ok := p.Tx().Block(c.win.pos).(interface{ ResetExperience() int })
	if !ok {
		return
	}
	for _, o := range entity.NewExperienceOrbs(entity.EyePosition(p), f.ResetExperience()) {
		p.Tx().AddEntity(o)
	}
}
