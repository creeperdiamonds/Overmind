package javaproto

import (
	"encoding/hex"
	"math"
	"testing"
)

// Real server velocities from minecraft-protocol 1.68.0's own lpVec3 test
// (test/lpVec3Test.js): each must decode and encode back to the same bytes.
func TestLpVec3RoundTripsRealPayloads(t *testing.T) {
	for _, h := range []string{"f9ff7ffeebed", "59e7800cebed", "51e880011541", "09e98000d8fd"} {
		raw, _ := hex.DecodeString(h)
		r := NewReader(raw)
		x, y, z := r.ReadLpVec3()
		if r.Done() != nil {
			t.Fatalf("%s: not read whole", h)
		}
		if got := hex.EncodeToString(AppendLpVec3(nil, x, y, z)); got != h {
			t.Errorf("%s: encoded back as %s (from %v %v %v)", h, got, x, y, z)
		}
	}
	r := NewReader([]byte{0xf9, 0xff, 0x7f, 0xfe, 0xeb, 0xed})
	if _, y, _ := r.ReadLpVec3(); math.Abs(y+0.0784) > 0.001 {
		t.Errorf("y = %v, want about -0.0784 (gravity's pull in a tick)", y)
	}
}

func TestLpVec3(t *testing.T) {
	if b := AppendLpVec3(nil, 0, 0, 0); len(b) != 1 || b[0] != 0 {
		t.Errorf("zero is %x, want one zero byte", b)
	}
	if b := AppendLpVec3(nil, math.NaN(), 0, 0); len(b) != 1 {
		t.Errorf("NaN is %x, want zero", b)
	}
	for _, v := range [][3]float64{{0.1, 0.2, -0.1}, {-1.5, 0.42, 0.9}, {3, -3, 0}, {7.25, -0.5, 12}, {-100, 40, 1}} {
		b := AppendLpVec3(nil, v[0], v[1], v[2])
		want := 6
		if math.Ceil(math.Max(math.Abs(v[0]), math.Max(math.Abs(v[1]), math.Abs(v[2])))) > 3 {
			want = 7
		}
		if len(b) != want {
			t.Errorf("%v takes %d bytes, want %d", v, len(b), want)
		}
		r := NewReader(b)
		x, y, z := r.ReadLpVec3()
		scale := math.Ceil(math.Max(math.Abs(v[0]), math.Max(math.Abs(v[1]), math.Abs(v[2]))))
		for i, got := range []float64{x, y, z} {
			if math.Abs(got-v[i]) > scale/16000 {
				t.Errorf("%v came back as %v %v %v", v, x, y, z)
			}
		}
	}
}

func TestEntityMetadataEnds(t *testing.T) {
	var m Meta
	m.Byte(0, 0, 0x02)
	m.Float(9, 3, 20)
	b := EntityMetadata(7, m)
	if b[len(b)-1] != 0xFF {
		t.Fatalf("no end marker: %x", b)
	}
	r := NewReader(b)
	if r.VarInt() != PlayEntityMetadata || r.VarInt() != 7 {
		t.Fatal("bad header")
	}
	if r.Byte() != 0 || r.VarInt() != 0 || r.Byte() != 2 || r.Byte() != 9 || r.VarInt() != 3 || r.Float() != 20 || r.Byte() != 0xFF || r.Done() != nil {
		t.Fatal("entries not as written")
	}
}
