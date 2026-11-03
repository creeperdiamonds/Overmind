package javafront

import (
	"encoding/binary"
	"log/slog"
	"math/bits"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/overmind/overmind/internal/javaproto"
	"github.com/overmind/overmind/internal/tables/java261"
)

const (
	// directBlockBits and directBiomeBits are the global palette widths of
	// Java 26.1. TestDirectPaletteWidths checks them against the tables.
	directBlockBits = 15
	directBiomeBits = 7
	// maxIndirectBlockBits and maxIndirectBiomeBits are the widest indirect
	// palettes the client accepts.
	maxIndirectBlockBits = 8
	maxIndirectBiomeBits = 3
	minIndirectBlockBits = 4

	sectionBlocks = 4096
	sectionBiomes = 64

	// heightmapBits is ceil(log2(height+1)): 9 for the Overworld's 384 and
	// for the 256 of the Nether and the End.
	heightmapBits = 9
)

// Heightmap kinds the client reads (Heightmap.Types in the order of their IDs).
const (
	heightmapWorldSurface           = 1
	heightmapMotionBlocking         = 4
	heightmapMotionBlockingNoLeaves = 5
)

var (
	fluidOnce sync.Once
	fluidRIDs []bool

	biomeOnce sync.Once
	biomeIDs  map[int]int32
	plains    int32

	lightMu    sync.Mutex
	lightTails map[lightKey][]byte
)

// isFluid says whether a Dragonfly runtime ID is water or lava.
func isFluid(rid uint32) bool {
	fluidOnce.Do(func() {
		n := world.DefaultBlockRegistry.BlockCount()
		fluidRIDs = make([]bool, n)
		for i := 0; i < n; i++ {
			if b, ok := world.BlockByRuntimeID(uint32(i)); ok {
				_, fluidRIDs[i] = b.(world.Liquid)
			}
		}
	})
	return int(rid) < len(fluidRIDs) && fluidRIDs[rid]
}

// javaBiome maps a Dragonfly biome ID to Java's. A biome Java does not have
// becomes plains, logged once at the time the table is built.
func javaBiome(id uint32) int32 {
	biomeOnce.Do(func() {
		plains = java261.JavaBiome("plains")
		all := world.Biomes()
		biomeIDs = make(map[int]int32, len(all))
		for _, b := range all {
			j := java261.JavaBiome(b.String())
			if j < 0 {
				slog.Default().Debug("javafront: biome has no Java counterpart, using plains", "biome", b.String())
				j = plains
			}
			biomeIDs[b.EncodeBiome()] = j
		}
	})
	if j, ok := biomeIDs[int(id)]; ok {
		return j
	}
	return plains
}

// lightData is the light half of the chunk packet, the same for every chunk of
// a dimension: with sky light (the Overworld, the End), every section full sky
// light; without (the Nether), none. No block light either way. Built once per
// height and kind.
func lightData(sections int, sky bool) []byte {
	key := lightKey{sections, sky}
	lightMu.Lock()
	defer lightMu.Unlock()
	if b, ok := lightTails[key]; ok {
		return b
	}
	// Sections plus one above and one below.
	n := sections + 2
	w := javaproto.Writer{B: make([]byte, 0, n*2051+32)}
	if !sky {
		w.VarInt(0) // sky mask: none
		w.VarInt(0) // block mask: none
		w.VarInt(0) // empty sky mask
		w.VarInt(0) // empty block mask
		w.VarInt(0) // sky light arrays
		w.VarInt(0) // block light arrays
	} else {
		w.VarInt(1)
		w.Long(int64(1)<<n - 1) // sky mask: all of them
		w.VarInt(0)             // block mask: none
		w.VarInt(0)             // empty sky mask
		w.VarInt(0)             // empty block mask
		w.VarInt(int32(n))
		full := make([]byte, 2048)
		for i := range full {
			full[i] = 0xFF
		}
		for i := 0; i < n; i++ {
			w.Bytes(full)
		}
		w.VarInt(0) // block light arrays
	}
	if lightTails == nil {
		lightTails = map[lightKey][]byte{}
	}
	lightTails[key] = w.B
	return w.B
}

type lightKey struct {
	sections int
	sky      bool
}

// encodeChunk builds a whole Chunk Data and Update Light packet. It reads the
// chunk, so it must run on the world's goroutine.
// blockEntities are the chunk's block entities: some states need them (paired
// chests), and Java draws some blocks from them (blockentities.go).
func encodeChunk(x, z int32, c *chunk.Chunk, blockEntities map[cube.Pos]world.Block, dim javaDimension) []byte {
	subs := c.Sub()
	w := javaproto.Writer{B: make([]byte, 0, 2048)}
	w.VarInt(javaproto.PlayChunk)
	w.Int(x)
	w.Int(z)

	writeHeightmaps(&w, c)

	extras := sectionExtras(x, z, c, blockEntities)
	var data javaproto.Writer
	data.B = make([]byte, 0, 256*dim.sections)
	var palette []int32
	for i, sub := range subs {
		minY := int16(c.Range().Min()) + int16(i)<<4
		palette = writeSection(&data, sub, palette, extras[i])
		writeBiomes(&data, c, minY)
	}
	// Java's dimension may be taller than Bedrock's: the Nether is 256 blocks
	// on Java and 128 on Bedrock, with air above its roof on both. The rest is
	// empty sections, with the top section's biomes.
	for i := len(subs); i < dim.sections; i++ {
		data.Short(0) // blocks
		data.Short(0) // fluids
		data.Byte(0)  // single value: air
		data.VarInt(0)
		writeBiomes(&data, c, int16(c.Range().Max())&^15)
	}
	w.Bytes(data.B)
	appendChunkBlockEntities(&w, blockEntities)

	light := lightData(max(len(subs), dim.sections), dim.skyLight)
	out := make([]byte, 0, len(w.B)+len(light))
	out = append(out, w.B...)
	return append(out, light...)
}

// writeHeightmaps writes the three maps the client uses. For now all three say
// the same thing: the highest non-air block. Leaves and non-blocking plants
// therefore count as motion blocking, which only affects rain and spawning
// hints on the client.
func writeHeightmaps(w *javaproto.Writer, c *chunk.Chunk) {
	min := c.Range().Min()
	longs := packedLongs(256, heightmapBits)
	packed := make([]int64, longs)
	perLong := 64 / heightmapBits
	for zz := uint8(0); zz < 16; zz++ {
		for xx := uint8(0); xx < 16; xx++ {
			i := int(zz)*16 + int(xx)
			height := int64(c.HighestBlock(xx, zz)) + 1 - int64(min)
			if height < 0 {
				height = 0
			}
			packed[i/perLong] |= height << ((i % perLong) * heightmapBits)
		}
	}
	w.VarInt(3)
	for _, kind := range []int32{heightmapWorldSurface, heightmapMotionBlocking, heightmapMotionBlockingNoLeaves} {
		w.VarInt(kind)
		w.VarInt(int32(longs))
		for _, l := range packed {
			w.Long(l)
		}
	}
}

// packedLongs is how many longs hold n entries of b bits, none crossing a long.
func packedLongs(n, b int) int {
	per := 64 / b
	return (n + per - 1) / per
}

// writeSection writes the counts and the block paletted container of one
// section. palette is scratch space, returned for reuse. extras are states
// worked out with a block entity, by index in the section (y<<8|z<<4|x).
func writeSection(w *javaproto.Writer, sub *chunk.SubChunk, palette []int32, extras map[int]int32) []int32 {
	layers := sub.Layers()
	if len(layers) == 0 {
		w.Short(0) // blocks
		w.Short(0) // fluids
		w.Byte(0)  // single value
		w.VarInt(0)
		return palette
	}
	if len(extras) > 0 || len(layers) > 1 && holdsWater(layers[1]) {
		return writeMixedSection(w, layers, palette, extras)
	}
	storage := layers[0]
	src := storage.Palette()
	n := src.Len()

	// Dragonfly's palette to Java states; two Bedrock states may be one Java
	// state, so count and write only what is really distinct.
	palette = palette[:0]
	mapped := make([]uint16, n)
	airIdx, fluid := make([]bool, n), make([]bool, n)
	for i := 0; i < n; i++ {
		rid := src.Value(uint16(i))
		state := int32(java261.JavaState(rid))
		airIdx[i] = isAir(state)
		fluid[i] = isFluid(rid)
		found := -1
		for k, s := range palette {
			if s == state {
				found = k
				break
			}
		}
		if found < 0 {
			found = len(palette)
			palette = append(palette, state)
		}
		mapped[i] = uint16(found)
	}

	var blocks, fluids int
	if n == 1 {
		if !airIdx[0] {
			blocks = sectionBlocks
		}
		if fluid[0] {
			fluids = sectionBlocks
		}
	} else {
		for y := byte(0); y < 16; y++ {
			for z := byte(0); z < 16; z++ {
				for x := byte(0); x < 16; x++ {
					i := storage.PaletteIndex(x, y, z)
					if !airIdx[i] {
						blocks++
					}
					if fluid[i] {
						fluids++
					}
				}
			}
		}
	}
	w.Short(int16(blocks))
	w.Short(int16(fluids))

	if len(palette) == 1 {
		w.Byte(0)
		w.VarInt(palette[0])
		return palette
	}
	bitsPer := max(bits.Len(uint(len(palette)-1)), minIndirectBlockBits)
	direct := bitsPer > maxIndirectBlockBits
	if direct {
		bitsPer = directBlockBits
	}
	w.Byte(byte(bitsPer))
	if !direct {
		w.VarInt(int32(len(palette)))
		for _, s := range palette {
			w.VarInt(s)
		}
	}
	writePacked(w, sectionBlocks, bitsPer, func(i int) uint64 {
		x, z, y := byte(i&15), byte(i>>4&15), byte(i>>8&15)
		k := mapped[storage.PaletteIndex(x, y, z)]
		if direct {
			return uint64(palette[k])
		}
		return uint64(k)
	})
	return palette
}

// holdsWater reports whether a layer's palette has water in it.
func holdsWater(s *chunk.PalettedStorage) bool {
	p := s.Palette()
	for i := 0; i < p.Len(); i++ {
		if isWater(p.Value(uint16(i))) {
			return true
		}
	}
	return false
}

// writeMixedSection is writeSection for a section whose Java states need more
// than the first layer's runtime IDs: water on the second layer (waterlogged
// blocks) or block entities. Each block's state is worked out by stateOf.
func writeMixedSection(w *javaproto.Writer, layers []*chunk.PalettedStorage, palette []int32, extras map[int]int32) []int32 {
	first := layers[0]
	var second *chunk.PalettedStorage
	if len(layers) > 1 {
		second = layers[1]
	}
	var states [sectionBlocks]int32
	index := map[int32]uint16{}
	palette = palette[:0]
	blocks, fluids := 0, 0
	for i := range states {
		x, z, y := byte(i&15), byte(i>>4&15), byte(i>>8&15)
		s, ok := extras[i]
		fluid := false
		if !ok {
			liquid := airRID()
			if second != nil {
				liquid = second.Palette().Value(second.PaletteIndex(x, y, z))
			}
			s, fluid = stateOf(cube.Pos{}, first.Palette().Value(first.PaletteIndex(x, y, z)), liquid, nil)
		} else if v, isW := java261.Property(uint16(s), "waterlogged"); isW && v == "true" {
			fluid = true
		}
		states[i] = s
		if !isAir(s) {
			blocks++
		}
		if fluid {
			fluids++
		}
		if _, seen := index[s]; !seen {
			index[s] = uint16(len(palette))
			palette = append(palette, s)
		}
	}
	w.Short(int16(blocks))
	w.Short(int16(fluids))
	if len(palette) == 1 {
		w.Byte(0)
		w.VarInt(palette[0])
		return palette
	}
	bitsPer := max(bits.Len(uint(len(palette)-1)), minIndirectBlockBits)
	direct := bitsPer > maxIndirectBlockBits
	if direct {
		bitsPer = directBlockBits
	}
	w.Byte(byte(bitsPer))
	if !direct {
		w.VarInt(int32(len(palette)))
		for _, s := range palette {
			w.VarInt(s)
		}
	}
	writePacked(w, sectionBlocks, bitsPer, func(i int) uint64 {
		if direct {
			return uint64(states[i])
		}
		return uint64(index[states[i]])
	})
	return palette
}

// writeBiomes writes one section's biome container, sampling the 4x4x4 cells.
func writeBiomes(w *javaproto.Writer, c *chunk.Chunk, minY int16) {
	var cells [sectionBiomes]int32
	var palette [1 << maxIndirectBiomeBits]int32
	used := 0
	direct := false
	for i := range cells {
		x, z, y := uint8(i&3)*4, uint8(i>>2&3)*4, int16(i>>4&3)*4
		id := javaBiome(c.Biome(x, minY+y, z))
		cells[i] = id
		if direct {
			continue
		}
		found := false
		for _, p := range palette[:used] {
			if p == id {
				found = true
				break
			}
		}
		if !found {
			if used == len(palette) {
				direct = true
				continue
			}
			palette[used] = id
			used++
		}
	}
	if !direct && used == 1 {
		w.Byte(0)
		w.VarInt(palette[0])
		return
	}
	if direct {
		w.Byte(directBiomeBits)
		writePacked(w, sectionBiomes, directBiomeBits, func(i int) uint64 { return uint64(cells[i]) })
		return
	}
	bitsPer := bits.Len(uint(used - 1))
	w.Byte(byte(bitsPer))
	w.VarInt(int32(used))
	for _, p := range palette[:used] {
		w.VarInt(p)
	}
	writePacked(w, sectionBiomes, bitsPer, func(i int) uint64 {
		for k, p := range palette[:used] {
			if p == cells[i] {
				return uint64(k)
			}
		}
		return 0
	})
}

// writePacked writes n entries of b bits into longs, lowest bits first. No
// entry crosses a long, and the array has no length prefix (since 1.21.5).
func writePacked(w *javaproto.Writer, n, b int, entry func(i int) uint64) {
	per := 64 / b
	for i := 0; i < n; i += per {
		var l uint64
		for k := 0; k < per && i+k < n; k++ {
			l |= entry(i+k) << (k * b)
		}
		w.B = binary.BigEndian.AppendUint64(w.B, l)
	}
}
