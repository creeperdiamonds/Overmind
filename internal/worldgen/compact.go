package worldgen

import "unsafe"

// compactBase is a carvers-stage chunk as the shared cache keeps it (owner's ruling 31). A full base
// is 192 KB, and most of it is air above the ground or stone below it, so each 16-block section is
// kept as one value when it is all one block, or as a palette of its blocks with a byte for each
// cell. A section with more than 256 different blocks, which generation never makes, keeps its cells
// as they are. Workers expand a compact base into their own full buffer to read and write it.
type compactBase struct {
	cx, cz  int
	wgTop   [256]int16
	wgFloor [256]int16
	secs    [sections]compactSection
}

const (
	sections     = chunkCells / sectionCells
	sectionCells = 4096
)

// compactSection is one section: uniform, or a palette with byte indexes, or raw cells.
type compactSection struct {
	uniform bs
	palette []bs
	index   []uint8
	raw     []bs
}

// compact packs a full base.
func compact(b *baseChunk) *compactBase {
	c := &compactBase{cx: b.cx, cz: b.cz, wgTop: b.wgTop, wgFloor: b.wgFloor}
	var slot [1 << 16]int16 // A state's palette slot plus one, for the section being packed.
	for i := range c.secs {
		cells := b.b[i*sectionCells : (i+1)*sectionCells]
		sec := &c.secs[i]
		first, same := cells[0], true
		for _, s := range cells {
			if s != first {
				same = false
				break
			}
		}
		if same {
			sec.uniform = first
			continue
		}
		var palette []bs
		for _, s := range cells {
			if slot[s] == 0 {
				palette = append(palette, s)
				slot[s] = int16(len(palette))
			}
		}
		if len(palette) <= 256 {
			sec.palette = palette
			sec.index = make([]uint8, sectionCells)
			for j, s := range cells {
				sec.index[j] = uint8(slot[s] - 1)
			}
		} else {
			sec.raw = append([]bs(nil), cells...)
		}
		for _, s := range palette {
			slot[s] = 0
		}
	}
	return c
}

// expand writes the whole chunk into a full base.
func (c *compactBase) expand(b *baseChunk) {
	b.cx, b.cz, b.wgTop, b.wgFloor, b.whole = c.cx, c.cz, c.wgTop, c.wgFloor, true
	c.expandCells(&b.b)
}

// expandCells writes the blocks into a full cell array.
func (c *compactBase) expandCells(cells *[chunkCells]bs) {
	for i := range c.secs {
		sec := &c.secs[i]
		out := cells[i*sectionCells : (i+1)*sectionCells]
		switch {
		case sec.raw != nil:
			copy(out, sec.raw)
		case sec.index != nil:
			for j, k := range sec.index {
				out[j] = sec.palette[k]
			}
		default:
			for j := range out {
				out[j] = sec.uniform
			}
		}
	}
}

// bytes is what the compact base holds, for the cache's count.
func (c *compactBase) bytes() int64 {
	n := int64(unsafe.Sizeof(*c))
	for i := range c.secs {
		sec := &c.secs[i]
		n += int64(cap(sec.palette))*2 + int64(cap(sec.index)) + int64(cap(sec.raw))*2
	}
	return n
}
