package worldgen

// The Nether's cave carver (Task 10), Bedrock's NetherCaveFeature (minecraft:nether_cave_carver_feature,
// pregeneration pass) in our own Go from the study of BDS 1.26.60.6 (NOTES-bedrock-nether N4 and R2-3):
// Java 1.12's MapGenCavesHell with Bedrock's MT and the 65536 sine table. Its configuration is the
// data file's (fill air, width modifier 0, skip chance 5, y scale 0.5, height limit 120, radius
// multipliers 1, floor level -0.7); the code reads it and so do we, as constants. It carves the
// chunk's netherrack after the surface, so surface blocks stay.

const (
	netherCaveSkip  = 5
	netherCaveFloor = float32(-0.7)
	netherCaveYMul  = float32(0.5)
)

// carveCaves carves a chunk (cx, cz): every source chunk within 8 runs its tunnels, each from an MT
// seeded with the source's decoration seed.
func (n *NetherGenerator) carveCaves(out *[netherHeight * 256]bs, cx, cz int, seeds bedrockSeeds) {
	var r bmt
	for sx := cx - 8; sx <= cx+8; sx++ {
		for sz := cz - 8; sz <= cz+8; sz++ {
			r.setSeed(seeds.chunk(sx, sz))
			netherCaveSource(&r, out, cx, cz, sx, sz)
		}
	}
}

func netherCaveSource(r *bmt, out *[netherHeight * 256]bs, cx, cz, sx, sz int) {
	n := r.nextIntN(r.nextIntN(r.nextIntN(10)+1) + 1)
	if g := r.nextIntN(netherCaveSkip); n <= 0 || g != 0 {
		return
	}
	for range n {
		z := int(r.nextIntN(16)) + sz*16
		y := int(r.nextIntN(128))
		x := int(r.nextIntN(16)) + sx*16
		p := [3]float32{float32(x), float32(y), float32(z)}
		tunnels := 1
		if r.nextIntN(4) == 0 {
			f := r.nextFloat()
			netherTunnel(r, out, cx, cz, p, float32(f*6)+1, 0, 0, netherCaveYMul, -1, -1)
			tunnels = int(r.nextIntN(4)) + 1
		}
		for range tunnels {
			yaw := float32(r.nextFloat()*bedrockPi) * 2
			f2 := r.nextFloat()
			pitch := float32(float32(f2+-0.5)+float32(f2+-0.5)) * 0.125
			f3 := r.nextFloat()
			f4 := r.nextFloat()
			width := float32(f4+float32(f3+f3)) * 2
			netherTunnel(r, out, cx, cz, p, width, yaw, pitch, netherCaveYMul, 0, 0)
		}
	}
}

func tableSin(v float32) float32 { return bedrockSin[int32(float32(v*10430.378))&0xffff] }
func tableCos(v float32) float32 {
	return bedrockSin[int32(float32(float32(v*10430.378)+16384))&0xffff]
}

func wordFloat(u uint32) float32 { return float32(float64(u) * 2.3283064365386963e-10) }

// netherTunnel walks one tunnel (a room when start is -1), carving where it passes the chunk.
func netherTunnel(parent *bmt, out *[netherHeight * 256]bs, cx, cz int, p [3]float32, width, yaw, pitch, yMul float32, start, dist int) {
	t := newBMT(uint32(parent.nextInt()))
	if dist < 1 {
		dist = 112 - int(t.u32()%28)
	}
	half := dist >> 1
	room := start == -1
	if room {
		start = half
	}
	bp := 0
	if half != 0 {
		bp = int(t.u32() % uint32(half))
	}
	damp := float32(0.7)
	if t.u32()%6 == 0 {
		damp = 0.92
	}
	if start >= dist {
		return
	}
	bp += dist >> 2
	X, Y, Z := p[0], p[1], p[2]
	var ych, pch float32
	ccx, ccz := float32(cx*16+8), float32(cz*16+8)
	fd := float32(dist)
	for {
		sv := bedrockSin[int32(float32(float32(float32(float32(start)*bedrockPi)/fd)*10430.378))&0xffff]
		rad := sv * (0 + width)
		Y += tableSin(pitch)
		cp := tableCos(pitch)
		X += cp * tableCos(yaw)
		Z += cp * tableSin(yaw)
		pitchD := damp * pitch
		pchD, ychD := pch*0.9, 0.75*ych
		var f [6]float32
		for i := range f {
			f[i] = wordFloat(t.u32())
		}
		rad += 1.5
		yawN := ych*0.1 + yaw
		pitchN := pch*0.1 + pitchD
		ych = float32(f[5]*(f[3]-f[4]))*4 + ychD
		pch = float32(f[2]*(f[0]-f[1]))*2 + pchD
		yaw, pitch = yawN, pitchN
		carve := true
		if !room {
			if width > 1 && start == bp {
				q := [3]float32{X, Y, Z}
				t1 := wordFloat(t.u32())*0.5 + 0.5
				netherTunnel(t, out, cx, cz, q, t1, yaw+-1.5707964, pitch/3, 1, bp, dist)
				t2 := wordFloat(t.u32())*0.5 + 0.5
				netherTunnel(t, out, cx, cz, q, t2, yaw+1.5707964, pitch/3, 1, bp, dist)
				return
			}
			carve = t.u32()%4 != 0
		}
		if carve {
			dx, dz, rem := X-ccx, Z-ccz, float32(dist-start)
			lim := (2 + width) + 16
			if (dx*dx+dz*dz)-rem*rem > lim*lim {
				return
			}
			r2 := rad + rad
			if !(r2+(ccx+16) < X || r2+(ccz+16) < Z || X < (ccx-16)-r2 || Z < (ccz-16)-r2) {
				if netherCarve(out, cx, cz, X, Y, Z, rad, yMul*rad) && room {
					return
				}
			}
		}
		start++
		if start >= dist {
			return
		}
	}
}

// netherCarve carves one step's ellipsoid into the chunk, unless lava touches its box's shell. It
// reports whether it carved (or had nothing in the chunk to carve).
func netherCarve(out *[netherHeight * 256]bs, cx, cz int, X, Y, Z, rad, yrad float32) bool {
	x0 := max(int(ffloor(X-rad))-cx*16-1, 0)
	x1 := min(int(ffloor(rad+X))-cx*16, 15)
	lo := max(int(ffloor(Y-yrad)), 2)
	hi := min(int(ffloor(yrad+Y)), 119)
	z0 := max(int(ffloor(Z-rad))-cz*16-1, 0)
	z1 := min(int(ffloor(rad+Z))-cz*16, 15)
	if x0 > x1 || z0 > z1 {
		return true
	}
	lavaAt := func(x, y, z int) bool {
		return y >= 0 && y < netherHeight && out[y<<8|z<<4|x].has(fLava)
	}
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			if x == x0 || x == x1 || z == z0 || z == z1 {
				for y := hi + 2; y >= lo-2; y-- {
					if lavaAt(x, y, z) {
						return false
					}
				}
			} else if lavaAt(x, hi+2, z) || lavaAt(x, lo-2, z) {
				return false
			}
		}
	}
	for x := x0; x <= x1; x++ {
		ddx := (float32(float32(cx*16+x)+0.5) - X) / rad
		ddx *= ddx
		for z := z0; z <= z1; z++ {
			ddz := (float32(float32(cz*16+z)+0.5) - Z) / rad
			for y := hi; y >= lo-1; y-- {
				ddy := (float32(float32(y)+0.5) - Y) / yrad
				if netherCaveFloor < ddy && (ddy*ddy+ddx)+ddz*ddz < 1 {
					if i := (y+1)<<8 | z<<4 | x; out[i] == sNetherrack {
						out[i] = sAir
					}
				}
			}
		}
	}
	return true
}
