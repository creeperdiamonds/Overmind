package worldgen

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"testing"
)

// TestNBTListsAllocateAsTheyRead: a list claiming more elements than the bytes left can hold is refused
// at once, and nested lists that each claim as many as the bytes allow cost memory in proportion to the
// bytes read, not to the counts (a hostile template). A plain nested list still reads.
func TestNBTListsAllocateAsTheyRead(t *testing.T) {
	root := func(body []byte) []byte {
		var b bytes.Buffer
		b.Write([]byte{10, 0, 0, 9, 0, 1, 'l'}) // Root compound "", then list "l".
		b.Write(body)
		b.WriteByte(0)
		return b.Bytes()
	}
	u32 := func(n uint32) []byte { return binary.BigEndian.AppendUint32(nil, n) }

	if _, err := ReadJavaNBT(root(append([]byte{9}, u32(1<<30)...))); err == nil {
		t.Error("a list of 2^30 lists in a few bytes was read")
	}
	// 60 nested lists, each claiming about as many elements as the bytes left could hold (the old
	// reader reserved room for all of them at every level: about 19 MB here).
	var body []byte
	const depth = 60
	for i := range depth {
		body = append(body, 9)
		body = append(body, u32(uint32((depth-i)*5+20000))...)
	}
	body = append(body, make([]byte, 20000)...)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := ReadJavaNBT(root(append([]byte{9}, body...)))
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Error("truncated nested lists were read")
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 1<<20 {
		t.Errorf("nested lists of %d bytes allocated %d bytes", len(body), alloc)
	}

	// [[1, 2], []] as a list of int lists.
	ok := append([]byte{9}, u32(2)...)
	ok = append(ok, 3)
	ok = append(ok, u32(2)...)
	ok = append(ok, u32(1)...)
	ok = append(ok, u32(2)...)
	ok = append(ok, 0)
	ok = append(ok, u32(0)...)
	m, err := ReadJavaNBT(root(ok))
	if err != nil {
		t.Fatal(err)
	}
	l, _ := m["l"].([]any)
	if len(l) != 2 || len(l[0].([]any)) != 2 || l[0].([]any)[1] != int32(2) || len(l[1].([]any)) != 0 {
		t.Fatalf("read %v", m)
	}
}
