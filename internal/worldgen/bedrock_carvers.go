package worldgen

import "math"

// Bedrock's carvers for the Bedrock profile (Task 4e), in our own Go from NOTES-bedrock-exact sections
// 5a, 5b and P2: two cave carvers of the pregeneration pass and the canyon of loadChunk, each walked
// from every source chunk within 8 chunks, from the decoration chunk seed of the source. A cut block is
// settled by the aquifer, as in Java.

// bedrockCarverReach is how far, in chunks, a source's carver can reach (sections 5a and 5b: cx-8..cx+8).
const bedrockCarverReach = 8

// caveCarver is one cave carver feature's configuration, read from its file (section 5a and P2):
// skip_carve_chance, height_limit, y_scale, the radius multipliers (rooms only), floor_level and
// width_modifier. The pregeneration pass's rules say which carvers run, in the pass's order.
type caveCarver struct {
	skip, heightLimit int32
	yScale            [2]float32
	roomH, roomV      [2]float32
	floor             [2]float32
	width             float32
}

// caveSegment is one step of a tunnel or a room: its centre in carver space (Y + 64), radii, floor,
// and the step and length that decide whether it can reach a chunk.
type caveSegment struct {
	x, y, z, rad, yrad, floor float32
	step, dist                int32
	thick                     float32
}

func sinf(v float32) float32 { return float32(math.Sin(float64(v))) }
func cosf(v float32) float32 { return float32(math.Cos(float64(v))) }

// lerpDraw is a + (b - a) * nextFloat, with no draw when a == b (section 5a).
func lerpDraw(r *bmt, a, b float32) float32 {
	if a == b {
		return a
	}
	return a + float32((b-a)*r.nextFloat())
}

// caveSource lists the segments one carver starts in a source chunk (section 5a, addFeature).
func (cc *caveCarver) caveSource(bs bedrockSeeds, sx, sz int, out []caveSegment) []caveSegment {
	r := newBMT(bs.chunk(sx, sz))
	n := r.nextIntN(r.nextIntN(r.nextIntN(40)+1) + 1)
	if g := r.nextIntN(cc.skip); n <= 0 || g != 0 {
		return out
	}
	for range n {
		z := r.nextIntN(16) + int32(sz)*16 // z is drawn first
		y := r.nextIntN(cc.heightLimit) + 8
		x := r.nextIntN(16) + int32(sx)*16
		hm := lerpDraw(r, cc.roomH[0], cc.roomH[1])
		vm := lerpDraw(r, cc.roomV[0], cc.roomV[1])
		fl := lerpDraw(r, cc.floor[0], cc.floor[1])
		tunnels := int32(1)
		if r.nextIntN(4) == 0 {
			f := r.nextFloat()
			t := newBMT(uint32(r.nextInt()))
			thick := float32(f*6) + 1
			ys := lerpDraw(t, cc.yScale[0], cc.yScale[1])
			dist := 112 - int32(t.u32()%28)
			step := dist >> 1
			rad := float32(sinf(float32(float32(step)*pi32)/float32(dist))*float32(cc.width+thick)) + 1.5
			out = append(out, caveSegment{float32(x), float32(y), float32(z), rad * hm, float32(ys*rad) * vm, fl, step, dist, thick})
			tunnels = r.nextIntN(4) + 1
		}
		for range tunnels {
			f := r.nextFloat()
			yaw := float32(f*pi32) + float32(f*pi32)
			g := r.nextFloat()
			pitch := float32((g+-0.5)+(g+-0.5)) * 0.125
			th := float32(r.nextFloat()*2) + r.nextFloat()
			if r.nextIntN(10) == 0 {
				h1 := r.nextFloat()
				h2 := r.nextFloat()
				th *= float32(float32(h2*h1)*3) + 1
			}
			dist := 112 - r.nextIntN(28)
			out = caveTunnel(r, out, float32(x), float32(y), float32(z), th, yaw, pitch, 1, 0, dist, fl, cc.width)
		}
	}
	return out
}

// caveTunnel walks one tunnel (section 5a, addTunnel), drawing from its own generator seeded by the
// parent's next int, and forking once at its branch point.
func caveTunnel(parent *bmt, out []caveSegment, x, y, z, thick, yaw, pitch, yScale float32, step, dist int32, floor, width float32) []caveSegment {
	r := newBMT(uint32(parent.nextInt()))
	if dist < 1 {
		dist = 112 - int32(r.u32()%28)
	}
	var bp int32
	if half := uint32(dist >> 1); half != 0 {
		bp = int32(r.u32() % half)
	}
	damp := float32(0.7)
	if r.u32()%6 == 0 {
		damp = 0.92
	}
	if dist-step <= 0 {
		return out
	}
	bp += dist >> 2
	var yawC, pitchC float32
	for {
		sv := sinf(float32(float32(step)*pi32) / float32(dist))
		cp := cosf(pitch)
		x += float32(cosf(yaw) * cp)
		y += sinf(pitch)
		z += float32(sinf(yaw) * cp)
		pn := float32(pitch * damp)
		pd := float32(pitchC * 0.9)
		yd := float32(0.75 * yawC)
		var f [6]float32
		for i := range f {
			f[i] = r.rawFloat()
		}
		yaw = float32(yawC*0.1) + yaw
		pitch = float32(pitchC*0.1) + pn
		yawC = float32(float32(f[5]*(f[3]-f[4]))*4.0) + yd
		pitchC = float32(float32(f[2]*(f[0]-f[1]))*2.0) + pd
		if thick > 1 && bp == step {
			t1 := float32(r.rawFloat()*0.5) + 0.5
			out = caveTunnel(r, out, x, y, z, t1, yaw+float32(-1.5707963705062866), pitch/3, 1, bp, dist, floor, width)
			t2 := float32(r.rawFloat()*0.5) + 0.5
			return caveTunnel(r, out, x, y, z, t2, yaw+float32(1.5707963705062866), pitch/3, 1, bp, dist, floor, width)
		}
		if r.u32()%4 != 0 {
			rad := float32(sv*float32(width+thick)) + 1.5 // no room multipliers on tunnels
			out = append(out, caveSegment{x, y, z, rad, float32(yScale * rad), floor, step, dist, thick})
		}
		if dist <= step+1 {
			return out
		}
		step++
	}
}

// bedrockCarve settles one cut block (section 5a and P2): only replaceable blocks (stone, deepslate,
// tuff, the iron vein's blocks, the carvable surface blocks; never vein granite or bedrock), by the
// aquifer; and where a grass or mycelium top was cut through, dirt left on top becomes grass.
func (g *Generator) bedrockCarve(b *baseChunk, s *chunkScratch, lx, wy, lz, wx, wz int, grass *bool) {
	st := b.b[cellIndex(lx, wy, lz)]
	if st == sGrass || st == sMycelium {
		*grass = true
	}
	if st.isAir() || !bedrockCarvable(st) {
		return
	}
	k := g.fluidAt(&s.aq, wx, wy, wz, 0)
	if k == fillSolid {
		return
	}
	b.set(lx, wy, lz, fluidState(k))
	if *grass && wy > minY && b.b[cellIndex(lx, wy-1, lz)] == sDirt {
		b.set(lx, wy-1, lz, sGrass)
	}
}

var (
	idxGranite  = stateBlockIndex("granite")
	idxTuff     = stateBlockIndex("tuff")
	idxDeepIron = stateBlockIndex("deepslate_iron_ore")
	idxRawIron  = stateBlockIndex("raw_iron_block")
)

// bedrockCarvable is the replaceable test as the study measured it (P2), not read from the code
// (ruling 39; the block test 0x514c6b0 compares globals not named yet): what our carvable tag holds
// except granite, plus tuff and the iron vein's blocks. Measured at predicted carve positions: tuff,
// deepslate iron ore and raw iron carved 100% of the time, granite 0%, stone 98.3%.
// Cinnabar and sulfur (the subsurface pass places them before the carvers) are left out, also
// measured: counting them carvable takes biome window extreme_hills_5 from 99.506% to 99.320% and
// flower_forest_20 from 99.317% to 99.306%. The code's list is not read.
func bedrockCarvable(st bs) bool {
	switch stateBlock[st] {
	case idxGranite:
		return false
	case idxTuff, idxDeepIron, idxRawIron:
		return true
	}
	return st.tagged(tagCarvable)
}

// carveCaveSegment cuts one segment into the columns x0..x1, z0..z1 of chunk b (section 5a,
// carveEllipsoidVolume), with the y quirk: the test is at yb-1, the cut at yb.
func (g *Generator) carveCaveSegment(b *baseChunk, s *chunkScratch, sg *caveSegment, lim int32, x0, x1, z0, z1 int) {
	ox, oz := int32(b.cx*16), int32(b.cz*16)
	mx, mz := float32(ox+8), float32(oz+8)
	dx, dz := sg.x-mx, sg.z-mz
	t := sg.thick + 2 + 16
	left := float32((sg.dist - sg.step) * (sg.dist - sg.step))
	if float32(float32(dx*dx)+float32(dz*dz))-left > float32(t*t) {
		return
	}
	ext := sg.rad + sg.rad + 16
	fx, fz := float32(math.Floor(float64(sg.x))), float32(math.Floor(float64(sg.z)))
	if !(mx-ext <= fx && fx <= mx+ext) || !(mz-ext <= fz && fz <= ext+mz) {
		return
	}
	cx0 := max(0, int32(math.Floor(float64(sg.x-sg.rad)))-ox-1)
	cx1 := min(15, int32(math.Floor(float64(sg.x+sg.rad)))-ox) + 1
	cz0 := max(0, int32(math.Floor(float64(sg.z-sg.rad)))-oz-1)
	cz1 := min(15, int32(math.Floor(float64(sg.z+sg.rad)))-oz) + 1
	fy1 := int32(math.Floor(float64(sg.yrad + sg.y)))
	y1 := lim
	if fy1 < lim {
		y1 = max(fy1, 0) + 1
	}
	y0 := min(max(1, int32(math.Floor(float64(sg.y-sg.yrad)))-1), y1)
	for lx := max(cx0, int32(x0)); lx < min(cx1, int32(x1)+1); lx++ {
		X := ox + lx
		ddx := float32(float32(X)+0.5-sg.x) / sg.rad
		ddx = float32(ddx * ddx)
		for lz := max(cz0, int32(z0)); lz < min(cz1, int32(z1)+1); lz++ {
			Z := oz + lz
			ddz := float32(float32(Z)+0.5-sg.z) / sg.rad
			ddz = float32(ddz * ddz)
			if ddx+ddz >= 1 {
				continue
			}
			grass := false
			for yb := y1; yb > y0; yb-- {
				ddy := float32(float32(yb-1)+0.5-sg.y) / sg.yrad
				if !(ddy > sg.floor && float32(float32(ddy*ddy)+ddx)+ddz < 1) {
					continue
				}
				wy := int(yb) - 64
				if wy < minY || wy > maxY {
					continue
				}
				g.bedrockCarve(b, s, int(lx), wy, int(lz), int(X), int(Z), &grass)
			}
		}
	}
}

// canyonSegment is one step of a canyon with its wall table.
type canyonSegment struct {
	x, y, z, hr, vr float32
	rs              *[320]float32
}

// canyonChance and the rest are the new-terrain canyon configuration (section P2: base 64, height 312,
// chance 100).
const (
	canyonChance = 100
	canyonHeight = 312
)

// canyonSource walks the canyon of a source chunk for target chunk (cx, cz), adding the segments that
// reach it (section P2's canyon read).
func canyonSource(bsd bedrockSeeds, sx, sz, cx, cz int, out []canyonSegment) []canyonSegment {
	r := newBMT(bsd.chunk(sx, sz))
	if r.nextIntN(canyonChance) != 0 {
		return out
	}
	x := r.nextIntN(16)
	y := r.nextIntN(58) + 64 + 10
	r.u32()
	r.u32()
	z := r.nextIntN(16)
	yaw := float32(r.nextFloat()*pi32) * 2
	pitch := float32(r.nextFloat()+-0.5) * 0.25
	f1 := r.nextFloat()
	f2 := r.nextFloat()
	thick := float32(f1*3) + float32(f2*3)
	const yScale = float32(4)
	r.nextFloat() // the doubling chance, used only by a configuration flag the new terrain does not set
	fx, fy, fz := float32(x+int32(sx)*16), float32(y), float32(z+int32(sz)*16)
	seed := uint32(r.nextInt())
	t1, t2 := newBMT(seed), newBMT(seed)
	dist := int32(120 - t1.u32()%30)
	rs := &[320]float32{}
	a := t1.nextFloat()
	b := t1.nextFloat()
	c := float32(b*a) + 1
	rs[0] = c * c
	for i := 1; i < 320; i++ {
		t := t1
		if i >= 128 {
			t = t2
		}
		if t.nextIntN(3) == 0 {
			a := t.nextFloat()
			b := t.nextFloat()
			c = float32(b*a) + 1
		}
		rs[i] = c * c
	}
	mx, mz := float32(cx*16|8), float32(cz*16|8)
	lim := float32(thick+2+16) * float32(thick+2+16)
	var yawC, pitchC float32
	rem := dist
	for step := int32(1); ; step++ {
		sv := sinf(float32(float32(step-1)*pi32) / float32(dist))
		rad := float32(sv*thick) + 1.5
		ua, ub := t1.u32(), t1.u32()
		cp := cosf(pitch)
		fx = float32(cosf(yaw)*cp) + fx
		fy = sinf(pitch) + fy
		fz = float32(sinf(yaw)*cp) + fz
		var f [6]float32
		for i := range f {
			f[i] = t1.rawFloat()
		}
		nyawC := float32(float32(f[5]*(f[3]-f[4]))*4) + float32(yawC*0.5)
		npitchC := float32(float32(f[2]*(f[0]-f[1]))*2) + float32(pitchC*0.8)
		nyaw := float32(yawC*0.05) + yaw
		npitch := float32(pitchC*0.05) + float32(pitch*0.7)
		if t1.u32()&3 != 0 {
			ddx, ddz := fx-mx, fz-mz
			if float32(float32(ddx*ddx)+float32(ddz*ddz))-float32(float32(rem)*float32(rem)) > lim {
				return out
			}
			hr := float32(float32(float32(float64(ua)*(1.0/4294967296.0))*0.25)+0.75) * rad
			if !(fx < -hr+-hr+(mx-16) || hr+hr+(mx+16) < fx || hr+hr+(mz+16) < fz || fz < -hr+-hr+(mz-16)) {
				vr := float32(float32(float32(float32(float64(ub)*(1.0/4294967296.0))*0.25)+0.75)*rad) * yScale
				out = append(out, canyonSegment{fx, fy, fz, hr, vr, rs})
			}
		}
		yawC, pitchC, yaw, pitch = nyawC, npitchC, nyaw, npitch
		if dist <= step {
			return out
		}
		rem--
	}
}

// carveCanyonSegment cuts one canyon step into the columns x0..x1, z0..z1 of chunk b.
func (g *Generator) carveCanyonSegment(b *baseChunk, s *chunkScratch, sg *canyonSegment, x0, x1, z0, z1 int) {
	ox, oz := int32(b.cx*16), int32(b.cz*16)
	cx0 := max(0, int32(math.Floor(float64(sg.x-sg.hr))-float64(ox)+-1))
	cx1 := min(16, int32(math.Floor(float64(sg.x+sg.hr))-float64(ox)+1))
	fy0 := int32(math.Floor(float64(sg.y-sg.vr)) + -1)
	fy1 := int32(math.Floor(float64(sg.y+sg.vr)) + 1)
	y1 := max(int32(1), fy1)
	if canyonHeight < fy1 {
		y1 = canyonHeight
	}
	y0 := max(int32(1), fy0)
	if y1 < fy0 {
		y0 = y1
	}
	cz0 := max(0, int32(math.Floor(float64(sg.z-sg.hr))-float64(oz)+-1))
	cz1 := min(16, int32(math.Floor(float64(sg.z+sg.hr))-float64(oz)+1))
	for lx := max(cx0, int32(x0)); lx < min(cx1, int32(x1)+1); lx++ {
		ddx := float32(float32(ox+lx)+0.5-sg.x) / sg.hr
		dx2 := float32(ddx * ddx)
		for lz := max(cz0, int32(z0)); lz < min(cz1, int32(z1)+1); lz++ {
			ddz := float32(float32(oz+lz)+0.5-sg.z) / sg.hr
			d := float32(ddz*ddz) + dx2
			if d >= 1 {
				continue
			}
			grass := false
			for yb := y1; yb > y0; yb-- {
				if yb-2 < 0 || yb-2 >= 320 {
					continue
				}
				dy := float32(float32(yb-1)+0.5-sg.y) / sg.vr
				if !(float32(sg.rs[yb-2]*d)+float32(dy*dy)/6.0 < 1) {
					continue
				}
				wy := int(yb) - 64
				if wy < minY || wy > maxY {
					continue
				}
				g.bedrockCarve(b, s, int(lx), wy, int(lz), int(ox+lx), int(oz+lz), &grass)
			}
		}
	}
}

// applyBedrockCarvers is the Bedrock profile's carver pass over the columns given: both cave carvers,
// source by source (x outer, z inner; the pregeneration pass's rule order puts the extra carver
// first), then the canyon of every source.
func (g *Generator) applyBedrockCarvers(b *baseChunk, s *chunkScratch, x0, x1, z0, z1 int) {
	segs := s.caveSegs[:0]
	for sx := b.cx - bedrockCarverReach; sx <= b.cx+bedrockCarverReach; sx++ {
		for sz := b.cz - bedrockCarverReach; sz <= b.cz+bedrockCarverReach; sz++ {
			for i := range g.bdata.carvers {
				cc := &g.bdata.carvers[i]
				segs = cc.caveSource(g.bseeds, sx, sz, segs[:0])
				lim := min(cc.heightLimit, int32(maxY-minY+1-2))
				for k := range segs {
					g.carveCaveSegment(b, s, &segs[k], lim, x0, x1, z0, z1)
				}
			}
		}
	}
	s.caveSegs = segs
	cs := s.canyonSegs[:0]
	for sx := b.cx - bedrockCarverReach; sx <= b.cx+bedrockCarverReach; sx++ {
		for sz := b.cz - bedrockCarverReach; sz <= b.cz+bedrockCarverReach; sz++ {
			cs = canyonSource(g.bseeds, sx, sz, b.cx, b.cz, cs[:0])
			for k := range cs {
				g.carveCanyonSegment(b, s, &cs[k], x0, x1, z0, z1)
			}
		}
	}
	s.canyonSegs = cs
}
