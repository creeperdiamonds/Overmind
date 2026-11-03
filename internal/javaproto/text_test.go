package javaproto

import (
	"bytes"
	"strings"
	"testing"
)

// The NBT string the game reads is modified UTF-8. These byte strings follow the
// format's definition: NUL as C0 80, and a character past U+FFFF as its two
// UTF-16 halves, three bytes each.
func TestNBTStringIsModifiedUTF8(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"hi", []byte{0, 2, 'h', 'i'}},
		{"é✓", []byte{0, 5, 0xC3, 0xA9, 0xE2, 0x9C, 0x93}},
		{"a\x00b", []byte{0, 4, 'a', 0xC0, 0x80, 'b'}},
		// U+1F600 is D83D DE00 in UTF-16.
		{"😀", []byte{0, 6, 0xED, 0xA0, 0xBD, 0xED, 0xB8, 0x80}},
	}
	for _, c := range cases {
		if got := appendNBTString(nil, c.in); !bytes.Equal(got, c.want) {
			t.Errorf("%q: got % X, want % X", c.in, got, c.want)
		}
	}
}

func TestNBTStringIsCutToFit(t *testing.T) {
	got := appendNBTString(nil, strings.Repeat("😀", 20000)) // 120000 bytes encoded
	n := int(got[0])<<8 | int(got[1])
	if n != len(got)-2 || n > maxNBTString || n%6 != 0 {
		t.Fatalf("length field %d, body %d bytes", n, len(got)-2)
	}
}

func TestTextComponentShapes(t *testing.T) {
	plain := appendText(nil, Text{Text: "x"})
	if !bytes.Equal(plain, []byte{tagString, 0, 1, 'x'}) {
		t.Fatalf("plain text: % X", plain)
	}
	tr := appendText(nil, Text{Translate: "multiplayer.player.joined", Color: "yellow", With: []Text{{Text: "Bob"}}})
	want := []byte{tagCompound}
	str := func(name, v string) {
		want = append(want, tagString)
		want = appendNBTString(want, name)
		want = appendNBTString(want, v)
	}
	str("translate", "multiplayer.player.joined")
	str("color", "yellow")
	want = append(want, tagList)
	want = appendNBTString(want, "with")
	want = append(want, tagCompound, 0, 0, 0, 1)
	str("text", "Bob")
	want = append(want, tagEnd, tagEnd)
	if !bytes.Equal(tr, want) {
		t.Fatalf("translation:\n got % X\nwant % X", tr, want)
	}
}
