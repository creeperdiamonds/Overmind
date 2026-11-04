package worldgen

import "math"

// Bedrock's stored 3D biomes (Task 4e round 5), for the Bedrock profile with the data read, in our own
// Go from the study of BDS 1.26.60.6 (NOTES-bedrock-exact T11, read from BiomeSource3d's fill code).
//
// BDS does not store one table answer per 4 x 4 x 4 quart as Java does. It looks the table up on an
// 8-block grid: sample (gx, gy, gz) sits at block (8gx, 8gy, 8gz) and takes the climate of the quart
// holding its column, quantized as BDS does (bedrockQuantize), with depth from its own Y. Each sample
// column is searched bottom up from Y -72, every search starting from the last one's leaf (Java's
// search with a candidate, kept on exact ties), so a sample is a pure function of its column. A block
// takes the biome of its 8-block cell's corners if all eight agree, and else the corner nearest to it
// after each corner is moved by a jitter drawn from the corner and the level seed (bedrockJitter).
//
// Sections above the one holding the chunk's highest block are stored as copies of it, so a biome read
// there is the top real section's at the same height within the section. In BDS's saved chunks the
// highest block is the decorated one (trees reach a section higher in about 2% of chunks); decoration
// here reads them with the carvers stage's highest block, which it knows before it runs.
//
// Decoration (the scatter's biome test, the chunk's biome list, snap to surface, freeze), the surface
// pass (the biome at a column's top) and the subsurface pass read these biomes. The biomes stored in
// the chunk for the client, and Java's placements in this profile, keep the quart model.

// bedrockQuantize is BDS's quantization of a climate value: the float32 value's double product with
// 10000, truncated (T11). Java's is the float32 product.
func bedrockQuantize(v float32) int64 { return int64(float64(v) * 10000.0) }

const (
	bSampleLow  = (minY - 8) >> 3         // The lowest sample row, Y -72.
	bSampleRows = (maxY+1-minY)>>3 + 2 // Y -72 to 320.
	bSampleSlots = 1 << 10
)

// bSampleColumn is one sample column's biomes, bottom up. It lives in pooled scratch.
type bSampleColumn struct {
	owner  *Generator
	gx, gz int32
	ids    [bSampleRows]uint8
}

// bedrockSamples gives a sample column, worked out on first use.
func (s *chunkScratch) bedrockSamples(g *Generator, gx, gz int) *bSampleColumn {
	c := &s.bsamples[(gx&31)|(gz&31)<<5]
	if c.owner == g && c.gx == int32(gx) && c.gz == int32(gz) {
		return c
	}
	c.owner, c.gx, c.gz = g, int32(gx), int32(gz)
	qx, qz := (gx*8)>>2, (gz*8)>>2
	cl := g.Climate(qx*4, qz*4)
	in := coordsOf(cl.Continentalness, cl.Erosion, cl.Weirdness)
	off := float32(offsetFrom(&in))
	t := biomeTarget{bedrockQuantize(float32(cl.Temperature)), bedrockQuantize(float32(cl.Humidity)),
		bedrockQuantize(float32(cl.Continentalness)), bedrockQuantize(float32(cl.Erosion)), 0, bedrockQuantize(float32(cl.Weirdness))}
	var last *biomeNode
	for k := range c.ids {
		y := (bSampleLow + k) * 8
		// Depth as BDS sums it, rounded to float32 at each step in its order: y * -1/128 + 1, + offset.
		t[biomeTargetDepth] = bedrockQuantize(float32(float32(float32(float32(y)*-0.0078125)+1.0) + off))
		d := int64(math.MaxInt64)
		if last != nil {
			d = last.dist(&t, math.MaxInt64)
		}
		last, _ = g.biomeTree.search(&t, last, d)
		c.ids[k] = last.id
	}
	return c
}

// bedrockJitter is a grid corner's offset (T11): a hash of the corner, mixed with the level seed, then
// three steps of a 48-bit LCG, each giving one axis in [-0.45, 0.45).
func bedrockJitter(seed uint32, gx, gy, gz int) (j [3]float32) {
	h := int64(int32(uint32(int32(gy))^uint32(int32(gx)*3129871))) ^ int64(gz)*116129781
	u := uint64((h*42317861+11)*h) >> 16
	r := u ^ (uint64(seed) ^ 0x5DEECE66D)
	for i := range j {
		r = r*0x5DEECE66D + 0xB
		j[i] = float32(float32(float32(float32((r>>24)&0xFFFFFF)*float32(1.0/16777216))+-0.5) * 0.9)
	}
	return j
}

// bCell is one 8-block cell: its eight corners' biomes, corner c at (c>>2&1, c>>1&1, c&1).
type bCell struct {
	gx, gy, gz int
	ids        [8]uint8
	uniform    bool
	jit        [8][3]float32
	jitReady   bool
}

func (s *chunkScratch) bedrockCell(g *Generator, gx, gy, gz int, c *bCell) {
	c.gx, c.gy, c.gz, c.jitReady = gx, gy, gz, false
	c.uniform = true
	for i := range 8 {
		col := s.bedrockSamples(g, gx+i>>2&1, gz+i&1)
		c.ids[i] = col.ids[gy+i>>1&1-bSampleLow]
		c.uniform = c.uniform && c.ids[i] == c.ids[0]
	}
}

// at is the biome of a block of the cell, given its place in it (0..7 on each axis).
func (c *bCell) at(seed uint32, lx, ly, lz int) uint8 {
	if c.uniform {
		return c.ids[0]
	}
	if !c.jitReady {
		for i := range 8 {
			c.jit[i] = bedrockJitter(seed, c.gx+i>>2&1, c.gy+i>>1&1, c.gz+i&1)
		}
		c.jitReady = true
	}
	fx, fy, fz := float32(lx)*0.125, float32(ly)*0.125, float32(lz)*0.125
	best, bi := float32(math.MaxFloat32), 0
	for i := range 8 {
		bx, by, bz := float32(i>>2&1), float32(i>>1&1), float32(i&1)
		dx := float32(float32(fx-bx) + c.jit[i][0])
		dy := float32(float32(fy-by) + c.jit[i][1])
		dz := float32(float32(fz-bz) + c.jit[i][2])
		d := float32(float32(float32(dz*dz)+float32(dx*dx)) + float32(dy*dy))
		if d < best {
			best, bi = d, i
		}
	}
	return c.ids[bi]
}

// bedrockBiome is the biome BDS computes for a block, before the copy rule for the sky sections.
func (s *chunkScratch) bedrockBiome(g *Generator, x, y, z int) uint8 {
	y = min(max(y, minY), maxY)
	var c bCell
	s.bedrockCell(g, x>>3, y>>3, z>>3, &c)
	return c.at(g.bseeds.level, x&7, y&7, z&7)
}

// bedrockTopSection is the index of the highest section with a generated block (the highest real
// section), from the carvers stage's heights.
func bedrockTopSection(tops *[256]int16) int {
	top := int16(minY)
	for _, t := range tops {
		top = max(top, t-1)
	}
	return (int(top) - minY) >> 4
}

// buildTopSection is the top real section a base build uses for the copy rule: from the stage's own
// heights in a whole-chunk build; a part-chunk build (SurfaceHeight's one column) knows no whole-chunk
// top and reads every section as computed. When the chunk's highest column tops out at a section
// boundary, that column's biome can then differ from the whole build's: its surface materials can, and
// so can its height where one of the two is a biome that keeps Java's rules (eroded badlands spires,
// frozen ocean icebergs). Rare, and only for SurfaceHeight.
func buildTopSection(b *baseChunk, x0, x1, z0, z1 int) int {
	if x0 == 0 && x1 == 15 && z0 == 0 && z1 == 15 {
		return bedrockTopSection(&b.wgTop)
	}
	return 1 << 10
}

// storedY is the height a stored biome at y is read from: the same height in the top real section for
// the copies above it.
func storedY(y, topSection int) int {
	if (y-minY)>>4 > topSection {
		return minY + topSection<<4 + (y-minY)&15
	}
	return y
}

// bedrockTopSectionOf gives a chunk's highest real section, remembered in scratch.
func (s *chunkScratch) bedrockTopSectionOf(g *Generator, cx, cz int) int {
	e := &s.btops[(cx&7)|(cz&7)<<3]
	if e.owner != g || e.cx != int32(cx) || e.cz != int32(cz) {
		e.owner, e.cx, e.cz = g, int32(cx), int32(cz)
		e.top = int16(bedrockTopSection(&g.cachedBase(s, cx, cz).wgTop))
	}
	return int(e.top)
}

type bTopEntry struct {
	owner  *Generator
	cx, cz int32
	top    int16
}

// bedrockStoredBiome is the biome a stored chunk holds at a block.
func (s *chunkScratch) bedrockStoredBiome(g *Generator, x, y, z int) uint8 {
	return s.bedrockBiome(g, x, storedY(y, s.bedrockTopSectionOf(g, x>>4, z>>4)), z)
}

// bedrockBiomeList is a chunk's biomes as BDS lists them for the passes (P1): the real sections bottom
// up, each scanned y, z, x, first appearance only.
func (s *chunkScratch) bedrockBiomeList(g *Generator, cx, cz, topSection int, list []uint8) []uint8 {
	list = list[:0]
	var seen [4]uint64
	add := func(id uint8) {
		if seen[id>>6]&(1<<(id&63)) == 0 {
			seen[id>>6] |= 1 << (id & 63)
			list = append(list, id)
		}
	}
	var cells [8]bCell // by (ly, lz, lx) halves: index ly<<2 | lz<<1 | lx
	for sec := 0; sec <= topSection; sec++ {
		y0 := minY + sec*16
		uniform := true
		for i := range cells {
			s.bedrockCell(g, cx*2+i&1, y0>>3+i>>2&1, cz*2+i>>1&1, &cells[i])
			uniform = uniform && cells[i].uniform
		}
		if uniform {
			// In y, z, x order the cells first appear by their index.
			for i := range cells {
				add(cells[i].ids[0])
			}
			continue
		}
		for ly := range 16 {
			for lz := range 16 {
				for lx := range 16 {
					c := &cells[(ly>>3)<<2|(lz>>3)<<1|lx>>3]
					add(c.at(g.bseeds.level, lx&7, ly&7, lz&7))
				}
			}
		}
	}
	return list
}
