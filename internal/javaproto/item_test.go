package javaproto

import (
	"bytes"
	"testing"
)

// A Slot is the count, then for an item its ID, the counts of components added
// and removed, and each added one as its type and data.
func TestSlotEncoding(t *testing.T) {
	if got := appendSlot(nil, Item{}); !bytes.Equal(got, []byte{0}) {
		t.Fatalf("empty slot: % X", got)
	}
	it := Item{ID: 300, Count: 5, Components: []Component{VarIntComponent(3, 100)}}
	want := []byte{5, 0xAC, 0x02, 1, 0, 3, 100}
	if got := appendSlot(nil, it); !bytes.Equal(got, want) {
		t.Fatalf("got % X, want % X", got, want)
	}
}

// Every equipment entry but the last has the top bit of its slot set.
func TestEquipmentTerminator(t *testing.T) {
	b := Equipment(7, []Equipped{{Slot: 0, Item: Item{}}, {Slot: 5, Item: Item{}}})
	r := NewReader(b)
	if r.VarInt() != PlayEquipment || r.VarInt() != 7 {
		t.Fatal("bad header")
	}
	if s := r.Byte(); s != 0x80 {
		t.Fatalf("first slot byte %X, want 80", s)
	}
	r.VarInt()
	if s := r.Byte(); s != 5 {
		t.Fatalf("last slot byte %X, want 05", s)
	}
	r.VarInt()
	if r.Done() != nil {
		t.Fatal("trailing bytes")
	}
}

// A click is read whole, the client's hashed guesses included.
func TestReadClick(t *testing.T) {
	w := &Writer{}
	w.VarInt(0)   // window
	w.VarInt(12)  // state
	w.Short(36)   // slot
	w.Byte(1)     // button
	w.VarInt(0)   // mode
	w.VarInt(1)   // one changed slot
	w.Short(36)   // its location
	w.Bool(true)  // with an item:
	w.VarInt(800) // id
	w.VarInt(3)   // count
	w.VarInt(1)   // one component hash
	w.VarInt(3)   // damage
	w.Int(-5)     // hash
	w.VarInt(0)   // nothing removed
	w.Bool(false) // empty cursor
	k, err := ReadClick(NewReader(w.B))
	if err != nil || k.State != 12 || k.Slot != 36 || k.Button != 1 || k.Mode != 0 {
		t.Fatalf("%+v, %v", k, err)
	}
	if _, err := ReadClick(NewReader(w.B[:len(w.B)-1])); err == nil {
		t.Fatal("a short click was read")
	}
}
