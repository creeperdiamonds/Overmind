package javaproto

import (
	"encoding/binary"

	"github.com/creeperdiamonds/overmind/internal/javawire"
)

// Windows, block entities and signs. Layouts are minecraft-data's for 26.1.
var (
	PlayOpenWindow       = id("play", "toClient", "open_window")
	PlayCloseWindowOut   = id("play", "toClient", "close_window")
	PlayWindowProperty   = id("play", "toClient", "craft_progress_bar")
	PlayBlockEntityData  = id("play", "toClient", "tile_entity_data")
	PlayOpenSign         = id("play", "toClient", "open_sign_entity")
	PlayUpdateSign       = id("play", "toServer", "update_sign")
	PlayCraftRecipeAsk   = id("play", "toServer", "craft_recipe_request")
	tagByte, tagInt      = byte(1), byte(3)
	nbtCompoundEndMarker = byte(tagEnd)
)

// NBT builds the body of an NBT compound: named tags, in order. Root wraps it
// as the network's nameless compound.
type NBT struct{ B []byte }

func (n *NBT) name(typ byte, name string) {
	n.B = append(n.B, typ)
	n.B = appendNBTString(n.B, name)
}

// String adds a string tag.
func (n *NBT) String(name, v string) { n.name(tagString, name); n.B = appendNBTString(n.B, v) }

// Byte adds a byte tag.
func (n *NBT) Byte(name string, v byte) { n.name(tagByte, name); n.B = append(n.B, v) }

// Int adds an int tag.
func (n *NBT) Int(name string, v int32) {
	n.name(tagInt, name)
	n.B = binary.BigEndian.AppendUint32(n.B, uint32(v))
}

// Compound adds a compound tag built by f.
func (n *NBT) Compound(name string, f func(*NBT)) {
	n.name(tagCompound, name)
	var c NBT
	f(&c)
	n.B = append(append(n.B, c.B...), nbtCompoundEndMarker)
}

// Compounds adds a list of compounds, each built by its function.
func (n *NBT) Compounds(name string, fs []func(*NBT)) {
	n.name(tagList, name)
	if len(fs) == 0 {
		n.B = append(n.B, tagEnd, 0, 0, 0, 0)
		return
	}
	n.B = append(n.B, tagCompound)
	n.B = binary.BigEndian.AppendUint32(n.B, uint32(len(fs)))
	for _, f := range fs {
		var c NBT
		f(&c)
		n.B = append(append(n.B, c.B...), nbtCompoundEndMarker)
	}
}

// Texts adds a list of text components. A list holds one tag type, so plain
// strings stay strings only when every one is plain; otherwise all are
// compounds.
func (n *NBT) Texts(name string, ts []Text) {
	n.name(tagList, name)
	plain := true
	for _, t := range ts {
		if t.Translate != "" || t.Color != "" || len(t.With) > 0 {
			plain = false
		}
	}
	if plain {
		n.B = append(n.B, tagString)
		n.B = binary.BigEndian.AppendUint32(n.B, uint32(len(ts)))
		for _, t := range ts {
			n.B = appendNBTString(n.B, t.Text)
		}
		return
	}
	n.B = append(n.B, tagCompound)
	n.B = binary.BigEndian.AppendUint32(n.B, uint32(len(ts)))
	for _, t := range ts {
		n.B = appendTextBody(n.B, t)
	}
}

// Root is the compound as network NBT: a compound tag with no name.
func (n NBT) Root() []byte {
	b := make([]byte, 0, len(n.B)+2)
	b = append(b, tagCompound)
	b = append(b, n.B...)
	return append(b, nbtCompoundEndMarker)
}

// OpenWindow opens a window of a menu type (java261.MenuType) with a title.
func OpenWindow(window, menu int32, title Text) []byte {
	w := packet(PlayOpenWindow)
	w.VarInt(window)
	w.VarInt(menu)
	w.B = appendText(w.B, title)
	return w.B
}

// CloseWindow closes a window the client has open.
func CloseWindow(window int32) []byte {
	w := packet(PlayCloseWindowOut)
	w.VarInt(window)
	return w.B
}

// WindowProperty sets a property of a window, such as a furnace's progress.
func WindowProperty(window int32, property, value int16) []byte {
	w := packet(PlayWindowProperty)
	w.VarInt(window)
	w.Short(property)
	w.Short(value)
	return w.B
}

// BlockEntityData sets the data of a block entity of a type
// (java261.BlockEntityType); nbt is network NBT (NBT.Root).
func BlockEntityData(x, y, z int, typ int32, nbt []byte) []byte {
	w := packet(PlayBlockEntityData)
	w.Position(x, y, z)
	w.VarInt(typ)
	w.B = append(w.B, nbt...)
	return w.B
}

// AppendChunkBlockEntity writes one block entity of chunk data: x and z
// within the chunk packed in a byte, y, the type, and network NBT.
func AppendChunkBlockEntity(b []byte, x, y, z int, typ int32, nbt []byte) []byte {
	b = append(b, byte((x&15)<<4|z&15))
	b = binary.BigEndian.AppendUint16(b, uint16(int16(y)))
	b = javawire.AppendVarInt(b, typ)
	return append(b, nbt...)
}

// OpenSign opens the sign editor for one side of a sign.
func OpenSign(x, y, z int, front bool) []byte {
	w := packet(PlayOpenSign)
	w.Position(x, y, z)
	w.Bool(front)
	return w.B
}

// SignUpdate is a decoded update_sign: the four lines of one side.
type SignUpdate struct {
	X, Y, Z int
	Front   bool
	Lines   [4]string
}

// maxSignLine is the longest line update_sign carries (minecraft.wiki,
// Packets oldid=3868170: "String with max length 384").
const maxSignLine = 384

// ReadUpdateSign decodes update_sign.
func ReadUpdateSign(r *Reader) (SignUpdate, error) {
	var s SignUpdate
	s.X, s.Y, s.Z = r.Position()
	s.Front = r.Bool()
	for i := range s.Lines {
		s.Lines[i] = r.String(maxSignLine)
	}
	return s, r.Done()
}

// ReadCraftRecipeRequest decodes craft_recipe_request (the recipe book), which
// is read past: a grid filled by hand is enough.
func ReadCraftRecipeRequest(r *Reader) error {
	r.VarInt() // window
	r.VarInt() // recipe
	r.Bool()   // make all
	return r.Done()
}
