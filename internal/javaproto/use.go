package javaproto

// Using items: on a block (block_place, Java's "use item on") and in the air
// (use_item). Layouts are minecraft-data's for 26.1.
var (
	PlayBlockPlace = id("play", "toServer", "block_place")
	PlayUseItem    = id("play", "toServer", "use_item")
)

// Hands, as block_place and use_item number them.
const (
	HandMain = 0
	HandOff  = 1
)

// BlockPlace is a decoded block_place: the client used the item in a hand on a
// face of a block, at a point on that face (each 0 to 1 inside the block).
type BlockPlace struct {
	Hand                      int32
	X, Y, Z                   int
	Face                      int32
	CursorX, CursorY, CursorZ float32
	InsideBlock               bool
	WorldBorderHit            bool
	Sequence                  int32
}

// ReadBlockPlace decodes block_place.
func ReadBlockPlace(r *Reader) (BlockPlace, error) {
	b := BlockPlace{Hand: r.VarInt()}
	b.X, b.Y, b.Z = r.Position()
	b.Face = r.VarInt()
	b.CursorX, b.CursorY, b.CursorZ = r.Float(), r.Float(), r.Float()
	b.InsideBlock, b.WorldBorderHit = r.Bool(), r.Bool()
	b.Sequence = r.VarInt()
	return b, r.Done()
}

// UseItem is a decoded use_item: the client used the item in a hand in the air,
// looking this way.
type UseItem struct {
	Hand       int32
	Sequence   int32
	Yaw, Pitch float32
}

// ReadUseItem decodes use_item.
func ReadUseItem(r *Reader) (UseItem, error) {
	u := UseItem{Hand: r.VarInt(), Sequence: r.VarInt()}
	u.Yaw, u.Pitch = r.Float(), r.Float()
	return u, r.Done()
}
