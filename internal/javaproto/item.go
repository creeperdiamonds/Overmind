package javaproto

import "github.com/creeperdiamonds/overmind/internal/javawire"

// Item and inventory packets. Layouts are minecraft-data's for 26.1.
var (
	PlayWindowItems = id("play", "toClient", "window_items")
	PlaySetSlot     = id("play", "toClient", "set_slot")
	PlaySetCursor   = id("play", "toClient", "set_cursor_item")
	PlayHeldSlotOut = id("play", "toClient", "held_item_slot")
	PlayEquipment   = id("play", "toClient", "entity_equipment")

	PlayHeldSlotIn   = id("play", "toServer", "held_item_slot")
	PlayCreativeSlot = id("play", "toServer", "set_creative_slot")
	PlayWindowClick  = id("play", "toServer", "window_click")
	PlayCloseWindow  = id("play", "toServer", "close_window")
)

// Item is one item stack as a slot carries it: the Java item ID, the count, and
// the components added to the item's defaults. A count of 0 is an empty slot.
type Item struct {
	ID, Count  int32
	Components []Component
}

// Component is one item component: its type ID (java261.ComponentType) and its
// data, already encoded.
type Component struct {
	Type int32
	Data []byte
}

// VarIntComponent is a component whose data is one VarInt, such as damage.
func VarIntComponent(typ, v int32) Component {
	return Component{typ, javawire.AppendVarInt(nil, v)}
}

// TextComponent is a component whose data is a text component, such as
// custom_name.
func TextComponent(typ int32, t Text) Component { return Component{typ, appendText(nil, t)} }

// Enchantment is one entry of an enchantments component: the enchantment's
// place in the enchantment registry, and its level.
type Enchantment struct{ ID, Level int32 }

// EnchantmentsComponent is the enchantments component: a VarInt count, then each
// enchantment as two VarInts.
func EnchantmentsComponent(typ int32, list []Enchantment) Component {
	b := javawire.AppendVarInt(nil, int32(len(list)))
	for _, e := range list {
		b = javawire.AppendVarInt(b, e.ID)
		b = javawire.AppendVarInt(b, e.Level)
	}
	return Component{typ, b}
}

// appendSlot writes a Slot: the count, and for a non-empty one the item ID, the
// numbers of components added and removed, then the added ones.
func appendSlot(b []byte, it Item) []byte {
	if it.Count <= 0 {
		return javawire.AppendVarInt(b, 0)
	}
	b = javawire.AppendVarInt(b, it.Count)
	b = javawire.AppendVarInt(b, it.ID)
	b = javawire.AppendVarInt(b, int32(len(it.Components)))
	b = javawire.AppendVarInt(b, 0)
	for _, c := range it.Components {
		b = javawire.AppendVarInt(b, c.Type)
		b = append(b, c.Data...)
	}
	return b
}

// WindowItems sets every slot of a window and the item on the cursor.
func WindowItems(window, state int32, items []Item, carried Item) []byte {
	w := packet(PlayWindowItems)
	w.VarInt(window)
	w.VarInt(state)
	w.VarInt(int32(len(items)))
	for _, it := range items {
		w.B = appendSlot(w.B, it)
	}
	w.B = appendSlot(w.B, carried)
	return w.B
}

// SetSlot sets one slot of a window.
func SetSlot(window, state int32, slot int16, it Item) []byte {
	w := packet(PlaySetSlot)
	w.VarInt(window)
	w.VarInt(state)
	w.Short(slot)
	w.B = appendSlot(w.B, it)
	return w.B
}

// SetCursor sets the item the client carries on its cursor.
func SetCursor(it Item) []byte {
	w := packet(PlaySetCursor)
	w.B = appendSlot(w.B, it)
	return w.B
}

// HeldSlot selects a hotbar slot, 0 to 8.
func HeldSlot(slot int32) []byte { w := packet(PlayHeldSlotOut); w.VarInt(slot); return w.B }

// Equipped is one piece of an entity's equipment and the slot it is in.
type Equipped struct {
	Slot byte
	Item Item
}

// Equipment shows what an entity holds and wears. Each slot byte has its top
// bit set when another entry follows.
func Equipment(entityID int32, list []Equipped) []byte {
	w := packet(PlayEquipment)
	w.VarInt(entityID)
	for i, e := range list {
		s := e.Slot & 0x7F
		if i < len(list)-1 {
			s |= 0x80
		}
		w.Byte(s)
		w.B = appendSlot(w.B, e.Item)
	}
	return w.B
}

// Short reads a big-endian int16.
func (r *Reader) Short() int16 { return int16(r.UShort()) }

// ReadHeldSlot decodes the serverbound held_item_slot.
func ReadHeldSlot(r *Reader) (int16, error) { s := r.Short(); return s, r.Done() }

// Click is a decoded window_click. The client's guesses at the result (the
// changed slots and cursor, as hashes) are read past: the server works out the
// result itself and sends it.
type Click struct {
	Window, State int32
	Slot          int16
	Button        int8
	Mode          int32
}

// ReadClick decodes window_click.
func ReadClick(r *Reader) (Click, error) {
	k := Click{Window: r.VarInt(), State: r.VarInt(), Slot: r.Short(), Button: int8(r.Byte()), Mode: r.VarInt()}
	n := r.VarInt()
	if n < 0 {
		r.fail()
	}
	for i := int32(0); i < n && r.Err() == nil; i++ {
		r.Short() // slot
		skipOptionalHashedSlot(r)
	}
	skipOptionalHashedSlot(r) // cursor
	return k, r.Done()
}

// skipOptionalHashedSlot reads past an optional HashedSlot: item ID, count,
// component types with their hashes, and removed component types.
func skipOptionalHashedSlot(r *Reader) {
	if !r.Bool() {
		return
	}
	r.VarInt() // item
	r.VarInt() // count
	for n := r.VarInt(); n > 0 && r.Err() == nil; n-- {
		r.VarInt() // type
		r.Int()    // hash
	}
	for n := r.VarInt(); n > 0 && r.Err() == nil; n-- {
		r.VarInt() // type
	}
}

// CreativeItem is the item a creative client puts in a slot. Its components
// come as type and bytes, each length-prefixed (minecraft-data's
// UntrustedSlotComponent), so ones the server does not read are skipped.
type CreativeItem struct {
	Slot       int16
	ID, Count  int32
	Components map[int32][]byte
}

// ReadCreativeSlot decodes set_creative_slot.
func ReadCreativeSlot(r *Reader) (CreativeItem, error) {
	c := CreativeItem{Slot: r.Short(), Count: r.VarInt()}
	if c.Count < 0 {
		r.fail()
	}
	if c.Count > 0 {
		c.ID = r.VarInt()
		added, removed := r.VarInt(), r.VarInt()
		if added < 0 || removed < 0 {
			r.fail()
		}
		for ; added > 0 && r.Err() == nil; added-- {
			typ := r.VarInt()
			data := r.Bytes(r.Len())
			if c.Components == nil {
				c.Components = map[int32][]byte{}
			}
			c.Components[typ] = data
		}
		for ; removed > 0 && r.Err() == nil; removed-- {
			r.VarInt()
		}
	}
	return c, r.Done()
}

// VarIntData reads a component's data that is one VarInt.
func VarIntData(b []byte) (int32, bool) {
	r := NewReader(b)
	v := r.VarInt()
	return v, r.Done() == nil
}
