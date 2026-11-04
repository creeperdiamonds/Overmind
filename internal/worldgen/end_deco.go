package worldgen

import (
	"fmt"
	"math"
)

// The End's decoration (Task 11), TheEndGenerator::decorationPostProcessChunk (0x5420370), in our own
// Go from the study of BDS 1.26.60.6 (NOTES-bedrock-end phase 8 "decoration", structures notes 5.2,
// 5.3 and phase 3 "End"). Per chunk, from the chunk's decoration MT:
//   - the obsidian spikes whose centre is in the chunk (no crystal and no bedrock cap: BDS adds those
//     in the dragon fight, not at worldgen);
//   - one MT word whose use was not found in the code (seen in the worlds: every island's words sit
//     after it);
//   - beyond 64 chunks of the centre: a small island (1 in 14 where the island value is below -20),
//     and where it is above 40, chorus plants and, 1 in 700, a gateway.
// The Dimension's data-driven passes run before all this in BDS; the vanilla data has no End rules.

// endSpike is one obsidian spike: centre, radius, height, and whether it is caged.
type endSpike struct {
	x, z, r, h int
	guarded    bool
}

// endSpikes is the ten spikes of a level seed (0x4b97660): heights 0..9 shuffled forward by an MT of
// the 32-bit level seed, spike i at angle i * pi/5 - pi on a circle of 42 through the 65536 sine table,
// truncated.
func endSpikes(seed int64) (out [10]endSpike) {
	r := newBMT(bedrockLevelSeed(seed))
	var h [10]int
	for i := range h {
		h[i] = i
	}
	for i := 1; i < 10; i++ {
		j := int(r.u32() % uint32(i+1))
		h[i], h[j] = h[j], h[i]
	}
	for i := range out {
		t := float32(float32(float32(float32(float32(i)*0.31415927)-3.1415927)*2) * 10430.378)
		x := int32(float32(bedrockSin[int32(t+16384)&0xffff] * 42))
		z := int32(float32(bedrockSin[int32(t)&0xffff] * 42))
		out[i] = endSpike{int(x), int(z), h[i]/3 + 2, h[i]*3 + 76, h[i] == 1 || h[i] == 2}
	}
	return out
}

var (
	sEndStone   = laterState("end_stone")
	sObsidian   = laterState("obsidian")
	sEndGateway = laterState("end_gateway")
	// sEndBedrock is the End's own bedrock, set to burn forever (BDS's gateways and exit portal).
	sEndBedrock = laterState("bedrock[infiniburn=true]")
	sChorusFlwr = laterState("chorus_flower[age=5]")
	sTorchNorth = laterState("wall_torch[facing=north]")
	sTorchEast  = laterState("wall_torch[facing=east]")
	sTorchSouth = laterState("wall_torch[facing=south]")
	sTorchWest  = laterState("wall_torch[facing=west]")
	// sChorus is chorus_plant by its connections, bits down, up, north, south, west, east.
	sChorus = func() (t [64]bs) {
		for m := range t {
			t[m] = laterState(chorusName(m))
		}
		return t
	}()
	// sIronBars is iron_bars by its connections, bits north, east, south, west.
	sIronBars = func() (t [16]bs) {
		for m := range t {
			t[m] = laterState(fmt.Sprintf("iron_bars[east=%v,north=%v,south=%v,west=%v]", m&2 != 0, m&1 != 0, m&4 != 0, m&8 != 0))
		}
		return t
	}()
)

func chorusName(m int) string {
	return fmt.Sprintf("chorus_plant[down=%v,east=%v,north=%v,south=%v,up=%v,west=%v]", m&1 != 0, m&32 != 0, m&4 != 0, m&8 != 0, m&2 != 0, m&16 != 0)
}

func isChorus(s bs) bool { return s == sChorusFlwr || s.block() == sChorus[0].block() }

// endSpike writes a spike: obsidian in the disc dx^2 + dz^2 <= r^2 + 1 from Y 0 to h-1 (exact on the
// BDS worlds), and for a guarded one Java's iron-bar cage on top, h .. h+3, 5 x 5 with a lid, with
// Java's connections (all 146 bars of the two cages of seed 100 match).
func (d *decoration) endSpike(sp endSpike) {
	for dx := -sp.r; dx <= sp.r; dx++ {
		for dz := -sp.r; dz <= sp.r; dz++ {
			if dx*dx+dz*dz > sp.r*sp.r+1 {
				continue
			}
			for y := 0; y < sp.h; y++ {
				d.set(sp.x+dx, y, sp.z+dz, sObsidian)
			}
		}
	}
	if !sp.guarded {
		return
	}
	for j := -2; j <= 2; j++ {
		for k := -2; k <= 2; k++ {
			for l := 0; l <= 3; l++ {
				xFace, zFace, lid := j == -2 || j == 2, k == -2 || k == 2, l == 3
				if !xFace && !zFace && !lid {
					continue
				}
				ns, we := xFace || lid, zFace || lid
				m := 0
				if ns && k != -2 {
					m |= 1 // north
				}
				if we && j != 2 {
					m |= 2 // east
				}
				if ns && k != 2 {
					m |= 4 // south
				}
				if we && j != -2 {
					m |= 8 // west
				}
				d.set(sp.x+j, sp.h+l, sp.z+k, sIronBars[m])
			}
		}
	}
}

// runSourceEnd decorates an End source chunk.
func (d *decoration) runSourceEnd(cx, cz int) {
	r := &d.chunkR
	r.setSeed(d.g.bseeds.chunk(cx, cz))
	for _, sp := range d.g.endN.spikes {
		if sp.x>>4 == cx && sp.z>>4 == cz {
			d.endSpike(sp)
		}
	}
	r.u32() // The word whose use was not found (see above).
	if int64(cx)*int64(cx)+int64(cz)*int64(cz) <= 4096 {
		return
	}
	n := d.g.endN.noise
	sim := n.islandSimplex(cx, cz)
	f := n.islandValue(cx, cz, 1, 1, sim)
	if f < -20 && r.u32()%14 == 0 {
		x := cx*16 + int(r.u32()&15) + 8
		y := 55 + int(r.u32()&15)
		z := cz*16 + int(r.u32()&15) + 8
		d.endIsland(r, x, y, z)
		if r.u32()&3 == 0 {
			d.endIsland(r, x, y, z)
		}
	}
	if f <= 40 {
		return
	}
	var plants []pos
	for range r.u32() % 5 {
		x := (cx*16 | 8) + int(r.u32()&15)
		z := (cz*16 | 8) + int(r.u32()&15)
		y := d.endHeightAt(x, z)
		if y > 0 && d.block(x, y, z) == sAir && d.block(x, y-1, z) == sEndStone {
			d.set(x, y, z, sChorus[0])
			plants = append(plants, pos{x, y, z})
			d.growChorus(r, x, y, z, x, z, 0, &plants)
		}
	}
	if r.u32()%700 == 0 {
		x := cx*16 + int(r.u32()&15) + 8
		z := cz*16 + int(r.u32()&15) + 8
		if y := d.endHeightAt(x, z); y > 0 {
			d.endGateway(x, y+3+int(r.u32()%7), z)
		}
	}
	d.connectChorus(plants)
}

// endHeightAt is the heightmap's first free Y of a column: one above its highest block.
func (d *decoration) endHeightAt(x, z int) int {
	for y := 255; y >= 0; y-- {
		if d.block(x, y, z) != sAir {
			return y + 1
		}
	}
	return 0
}

// endIsland is EndIslandFeature (0xd9b6560): discs of end stone, radius nextInt(3) + 4 shrinking by
// nextInt(2) + 0.5 per row down, while above 0.5; a disc takes |i|, |k| <= ceil(r) with i^2 + k^2 <=
// (r + 1)^2.
func (d *decoration) endIsland(r *bmt, x, y, z int) {
	f := float32(r.nextIntN(3)) + 4
	for j := 0; f > 0.5; j-- {
		lo, hi := int(math.Floor(float64(-f))), int(math.Ceil(float64(f)))
		r2 := (f + 1) * (f + 1)
		for i := lo; i <= hi; i++ {
			for k := lo; k <= hi; k++ {
				if float32(i*i+k*k) <= r2 {
					d.set(x+i, y+j, z+k, sEndStone)
				}
			}
		}
		f -= float32(r.nextIntN(2)) + 0.5
	}
}

// chorusDirs is Bedrock's direction order for nextInt(4): north, east, south, west.
var chorusDirs = [4][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

// chorusAround says whether the four horizontal neighbours are air, but the one on side skip.
func (d *decoration) chorusAround(x, y, z, skip int) bool {
	for i, dir := range chorusDirs {
		if i != skip && d.block(x+dir[0], y, z+dir[1]) != sAir {
			return false
		}
	}
	return true
}

// growChorus is ChorusFlowerBlock's generatePlant (0xcb5a0a0), Java's growTreeRecursive with Bedrock's
// direction order: a trunk of nextInt(4) + 1 (one more at the root), each cell with four free sides;
// up to 4 levels of nextInt(4) branches (one more at the root) within 8 of the origin; a flower of age
// 5 on top where no branch grew.
func (d *decoration) growChorus(r *bmt, x, y, z, ox, oz, it int, plants *[]pos) {
	n := int(r.nextIntN(4)) + 1
	if it == 0 {
		n++
	}
	for j := 1; j <= n; j++ {
		if !d.chorusAround(x, y+j, z, -1) {
			return
		}
		d.set(x, y+j, z, sChorus[0])
		*plants = append(*plants, pos{x, y + j, z})
	}
	grew := false
	if it < 4 {
		l := int(r.nextIntN(4))
		if it == 0 {
			l++
		}
		for k := 1; k <= l; k++ {
			dir := int(r.nextIntN(4))
			bx, by, bz := x+chorusDirs[dir][0], y+n, z+chorusDirs[dir][1]
			if absInt(bx-ox) >= 8 || absInt(bz-oz) >= 8 {
				continue
			}
			if d.block(bx, by, bz) != sAir || d.block(bx, by-1, bz) != sAir || !d.chorusAround(bx, by, bz, (dir+2)%4) {
				continue
			}
			d.set(bx, by, bz, sChorus[0])
			*plants = append(*plants, pos{bx, by, bz})
			d.growChorus(r, bx, by, bz, ox, oz, it+1, plants)
			grew = true
			if k == l {
				return
			}
		}
		if grew {
			return
		}
	}
	d.set(x, y+n, z, sChorusFlwr)
}

// connectChorus gives the chorus plants a source grew their connections: to every neighbour that is a
// chorus plant or flower, in all six directions. End stone under a plant does not count (BDS: every
// one of the 474 plants standing on end stone in the outer world has no down connection; all other
// plants there match this rule).
func (d *decoration) connectChorus(plants []pos) {
	for _, p := range plants {
		if !isChorus(d.block(p.x, p.y, p.z)) || d.block(p.x, p.y, p.z) == sChorusFlwr {
			continue
		}
		m := 0
		for i, o := range [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			if isChorus(d.block(p.x+o[0], p.y+o[1], p.z+o[2])) {
				m |= 1 << i
			}
		}
		d.set(p.x, p.y, p.z, sChorus[m])
	}
}

// endGateway is EndGatewayFeature (0xd9b5c70), Java's shape: the gateway at the centre, air in its
// row, bedrock in a cross one above and below and a block two above and below, air elsewhere in the
// 3 x 5 x 3. Its exit is set when a player uses it.
func (d *decoration) endGateway(x, y, z int) {
	for dx := -1; dx <= 1; dx++ {
		for dy := -2; dy <= 2; dy++ {
			for dz := -1; dz <= 1; dz++ {
				onX, onZ, far := dx == 0, dz == 0, dy == 2 || dy == -2
				s := sAir
				switch {
				case onX && onZ && dy == 0:
					s = sEndGateway
				case dy == 0:
				case far && onX && onZ:
					s = sEndBedrock
				case (onX || onZ) && !far:
					s = sEndBedrock
				}
				d.set(x+dx, y+dy, z+dz, s)
			}
		}
	}
}
