package javaproto

import (
	"encoding/binary"
	"unicode/utf8"
)

// Text is a chat component: literal text or a translation key with arguments,
// and an optional colour name ("yellow"). The client renders a translation in
// its own language.
type Text struct {
	Text      string
	Translate string
	With      []Text
	Color     string
	// Extra follows the component's own text, taking its colour unless it has
	// one of its own.
	Extra []Text
}

// NBT tag types used by text components.
const (
	tagEnd      = 0
	tagString   = 8
	tagList     = 9
	tagCompound = 10
)

// maxNBTString is the longest string NBT can hold, in bytes of modified UTF-8.
const maxNBTString = 65535

// appendText encodes a component as network NBT: a bare string tag for plain
// text, a compound otherwise.
func appendText(b []byte, t Text) []byte {
	if t.Translate == "" && t.Color == "" && len(t.With) == 0 && len(t.Extra) == 0 {
		b = append(b, tagString)
		return appendNBTString(b, t.Text)
	}
	b = append(b, tagCompound)
	return appendTextBody(b, t)
}

// appendTextBody writes the named tags of a compound component and its end tag.
func appendTextBody(b []byte, t Text) []byte {
	field := func(name, v string) {
		b = append(b, tagString)
		b = appendNBTString(b, name)
		b = appendNBTString(b, v)
	}
	if t.Translate != "" {
		field("translate", t.Translate)
	} else {
		field("text", t.Text)
	}
	if t.Color != "" {
		field("color", t.Color)
	}
	list := func(name string, ts []Text) {
		if len(ts) == 0 {
			return
		}
		b = append(b, tagList)
		b = appendNBTString(b, name)
		b = append(b, tagCompound)
		b = binary.BigEndian.AppendUint32(b, uint32(len(ts)))
		for _, w := range ts {
			b = appendTextBody(b, w)
		}
	}
	list("with", t.With)
	list("extra", t.Extra)
	return append(b, tagEnd)
}

// appendNBTString writes a string as NBT does: a 16-bit byte length, then
// modified UTF-8, which is what the game's reader expects. It differs from UTF-8
// in two ways: NUL is two bytes (C0 80), and a character outside the Basic
// Multilingual Plane, such as an emoji, is written as its two UTF-16 halves of
// three bytes each, not as four bytes. The game refuses four-byte sequences. A
// string too long for the length field is cut at a character boundary.
func appendNBTString(b []byte, s string) []byte {
	at := len(b)
	b = append(b, 0, 0)
	n := 0
	for _, r := range s {
		size := mutf8Len(r)
		if n+size > maxNBTString {
			break
		}
		n += size
		switch {
		case r == 0:
			b = append(b, 0xC0, 0x80)
		case r >= 0x10000:
			r -= 0x10000
			b = appendSurrogate(b, 0xD800+(r>>10))
			b = appendSurrogate(b, 0xDC00+(r&0x3FF))
		default:
			b = utf8.AppendRune(b, r)
		}
	}
	binary.BigEndian.PutUint16(b[at:], uint16(n))
	return b
}

// appendSurrogate writes one UTF-16 surrogate as a three-byte sequence.
func appendSurrogate(b []byte, u rune) []byte {
	return append(b, byte(0xE0|u>>12), byte(0x80|(u>>6)&0x3F), byte(0x80|u&0x3F))
}

func mutf8Len(r rune) int {
	switch {
	case r == 0:
		return 2
	case r >= 0x10000:
		return 6
	case r == utf8.RuneError:
		return 3
	}
	return utf8.RuneLen(r)
}
