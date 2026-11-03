package javafront

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
)

// Java draws chests, signs, beds, banners, skulls, shulker boxes and other
// blocks with a block entity, so chunks and block changes carry one for each,
// by the block entity type's ID (java261.BlockEntityType, from azalea)
// and with the data Java reads for it. Blocks Java draws from their state
// alone (furnaces, barrels, hoppers) get an empty one, as the vanilla server
// sends them.

// javaBlockEntity is the Java block entity of a Dragonfly block: its type and
// network NBT, false if Java has none for it.
func javaBlockEntity(b world.Block) (typ int32, nbt []byte, ok bool) {
	name := ""
	var data javaproto.NBT
	switch v := b.(type) {
	case block.Chest:
		name = "chest"
		if v.CustomName != "" {
			data.String("CustomName", v.CustomName)
		}
	case block.EnderChest:
		name = "ender_chest"
	case block.Sign:
		name = "sign"
		signNBT(&data, v)
	case block.Bed:
		name = "bed"
	case block.Banner:
		name = "banner" // its colour is in the block; patterns are not sent yet
	case block.Skull:
		name = "skull"
	case block.ShulkerBox:
		name = "shulker_box"
	case block.Furnace:
		name = "furnace"
	case block.BlastFurnace:
		name = "blast_furnace"
	case block.Smoker:
		name = "smoker"
	case block.Barrel:
		name = "barrel"
	case block.Hopper:
		name = "hopper"
	case block.BrewingStand:
		name = "brewing_stand"
	case block.Beacon:
		name = "beacon"
	case block.Lectern:
		name = "lectern"
	case block.Campfire:
		name = "campfire"
	case block.EnchantingTable:
		name = "enchanting_table"
	case block.Jukebox:
		name = "jukebox"
	case block.DecoratedPot:
		name = "decorated_pot"
	case block.EndPortal:
		name = "end_portal"
	default:
		return 0, nil, false
	}
	typ = java261.BlockEntityType(name)
	if typ < 0 {
		return 0, nil, false
	}
	return typ, data.Root(), true
}

// signNBT is a sign's text as Java keeps it: is_waxed, and front_text and
// back_text, each with has_glowing_text, a dye colour and four messages
// (minecraft.wiki "Sign" oldid=3849680 for the outer tags; the inner ones as
// prismarine-block 1.23.0's blockEntity.js writes them for 1.21.5 and later:
// messages as plain strings).
func signNBT(n *javaproto.NBT, s block.Sign) {
	var waxed byte
	if s.Waxed {
		waxed = 1
	}
	n.Byte("is_waxed", waxed)
	side := func(name string, t block.SignText) {
		n.Compound(name, func(c *javaproto.NBT) {
			var glow byte
			if t.Glowing {
				glow = 1
			}
			c.Byte("has_glowing_text", glow)
			c.String("color", signColour(t))
			c.Texts("messages", signLines(t.Text))
		})
	}
	side("front_text", s.Front)
	side("back_text", s.Back)
}

// signLines splits a sign's text into Java's four lines; Dragonfly keeps one
// string with line breaks, as Bedrock does. Lines past the fourth are left
// out, as Java has no room for them.
func signLines(text string) []javaproto.Text {
	lines := strings.SplitN(text, "\n", 5)
	out := make([]javaproto.Text, 4)
	for i := range out {
		if i < len(lines) {
			out[i] = javaproto.Text{Text: lines[i]}
		}
	}
	return out
}

// signColour is the dye colour of a side of a sign, black unless dyed.
func signColour(t block.SignText) string {
	for _, c := range item.Colours() {
		if c.SignRGBA() == t.BaseColour {
			return c.String()
		}
	}
	return "black"
}

// appendChunkBlockEntities writes the chunk's block entities Java draws.
func appendChunkBlockEntities(w *javaproto.Writer, blockEntities map[cube.Pos]world.Block) {
	var body []byte
	n := 0
	for pos, b := range blockEntities {
		if typ, nbt, ok := javaBlockEntity(b); ok {
			body = javaproto.AppendChunkBlockEntity(body, pos[0], pos[1], pos[2], typ, nbt)
			n++
		}
	}
	w.VarInt(int32(n))
	w.B = append(w.B, body...)
}

// editBlockEntity is block_entity_data for an edited block Java draws from a
// block entity, read from the loader's chunk (the update's own block without
// it); false for any other block.
func (c *conn) editBlockEntity(pos cube.Pos) ([]byte, bool) {
	b := c.edits[pos]
	if c.loader != nil {
		if col, ok := c.loader.Chunk(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}); ok {
			b = col.BlockEntities[pos]
		}
	}
	if b == nil {
		return nil, false
	}
	typ, nbt, ok := javaBlockEntity(b)
	if !ok {
		return nil, false
	}
	return javaproto.BlockEntityData(pos[0], pos[1], pos[2], typ, nbt), true
}
