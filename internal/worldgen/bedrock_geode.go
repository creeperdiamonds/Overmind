package worldgen

import (
	"encoding/json"
	"math"
)

// Bedrock's geode_feature (Task 4e round 6), in our own Go from the study of BDS 1.26.60.6
// (NOTES-bedrock-exact T15, T22; the study's sim p7geode.py). Every draw is the feature's Random's, in
// the code's order; the shell is a sum of fast inverse square roots, scanned column by column from the
// origin outwards with early stops, its blocks queued and written after the scan. The feature first
// asks whether its origin is inside a structure, and places nothing if so.

type geodeFeat struct {
	minPts, maxPts, minWall, maxWall, minOff, maxOff, invalid, maxR int
	crackOff                                                        int
	crackChance, baseCrack, noiseMul, potential, alternate         float32
	requireAlt                                                      bool
	filler, inner, alt, middle, outer                               bs
	buds                                                            []string // Java names of the inner placements, without facing.
}

func (fc *featCompiler) geode(v map[string]json.RawMessage) (bfeat, error) {
	f := &geodeFeat{minPts: 3, maxPts: 5, minWall: 4, maxWall: 7, minOff: 1, maxOff: 3, invalid: 1, maxR: 16, crackOff: 2,
		crackChance: 0.95, baseCrack: 2, noiseMul: 0.025, potential: 0.35, alternate: 0.083, requireAlt: true}
	num := func(k string, into any) { json.Unmarshal(v[k], into) }
	num("min_distribution_points", &f.minPts)
	num("max_distribution_points", &f.maxPts)
	num("min_outer_wall_distance", &f.minWall)
	num("max_outer_wall_distance", &f.maxWall)
	num("min_point_offset", &f.minOff)
	num("max_point_offset", &f.maxOff)
	num("invalid_blocks_threshold", &f.invalid)
	num("max_radius", &f.maxR)
	var co float32 = 2
	num("crack_point_offset", &co)
	f.crackOff = int(co)
	num("generate_crack_chance", &f.crackChance)
	num("base_crack_size", &f.baseCrack)
	num("noise_multiplier", &f.noiseMul)
	num("use_potential_placements_chance", &f.potential)
	num("use_alternate_layer0_chance", &f.alternate)
	num("placements_require_layer0_alternate", &f.requireAlt)
	if f.maxWall <= 0 || f.maxPts > 64 || f.maxR > maxDataRadius {
		return nil, unimpl("geode sizes")
	}
	for _, k := range []struct {
		key  string
		into *bs
	}{{"filler", &f.filler}, {"inner_layer", &f.inner}, {"alternate_inner_layer", &f.alt}, {"middle_layer", &f.middle}, {"outer_layer", &f.outer}} {
		s, _, err := blockState(v[k.key])
		if err != nil {
			return nil, err
		}
		*k.into = s
	}
	var pl []json.RawMessage
	json.Unmarshal(v["inner_placements"], &pl)
	for _, raw := range pl {
		d, err := parseBDesc(raw)
		if err != nil {
			return nil, err
		}
		if _, err := safeState(d.name + "[facing=up,waterlogged=false]"); err != nil {
			return nil, unimpl("geode placement %s", d.name)
		}
		f.buds = append(f.buds, d.name)
	}
	return f, nil
}

var (
	bnGeodeInvalid = bnameSetOf("air water flowing_water lava flowing_lava bedrock ice packed_ice blue_ice")
	bnGeodeCannot  = bnameSetOf("bedrock mob_spawner chest end_portal_frame reinforced_deepslate trial_spawner vault")
)

// bImproved is Java's ImprovedNoise made from Bedrock's MT (PerlinNoise_ctor's legacy branch): three
// offsets nextDouble*256 kept as floats, then a shuffle. It stays apart from legacyOctave (the Nether's
// and End's float32 sampler): the geode's noise is evaluated in double precision, and offsets drawn by
// nextDouble, so the two give different values.
type bImproved struct {
	xo, yo, zo float64
	p          [256]int32
}

func newBImproved(r *bmt) *bImproved {
	n := &bImproved{}
	n.xo = float64(float32(r.nextDouble() * 256))
	n.yo = float64(float32(r.nextDouble() * 256))
	n.zo = float64(float32(r.nextDouble() * 256))
	for i := range n.p {
		n.p[i] = int32(i)
	}
	for i := range 256 {
		j := int(r.nextIntN(int32(256-i))) + i
		n.p[i], n.p[j] = n.p[j], n.p[i]
	}
	return n
}

var improvedGrad = [16][3]float64{{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0}, {1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1}, {1, 1, 0}, {0, -1, 1}, {-1, 1, 0}, {0, -1, -1}}

func (n *bImproved) noise(x, y, z float64) float64 {
	d, e, f := x+n.xo, y+n.yo, z+n.zo
	i, j, k := math.Floor(d), math.Floor(e), math.Floor(f)
	g, h, l := d-i, e-j, f-k
	ii, jj, kk := int32(i), int32(j), int32(k)
	pp := func(a int32) int32 { return n.p[a&255] }
	a0, a1 := pp(ii), pp(ii+1)
	n0, o0, q0, r0 := pp(a0+jj), pp(a0+jj+1), pp(a1+jj), pp(a1+jj+1)
	gd := func(idx int32, x, y, z float64) float64 {
		gr := improvedGrad[idx&15]
		return gr[0]*x + gr[1]*y + gr[2]*z
	}
	c000, c100 := gd(pp(n0+kk), g, h, l), gd(pp(q0+kk), g-1, h, l)
	c010, c110 := gd(pp(o0+kk), g, h-1, l), gd(pp(r0+kk), g-1, h-1, l)
	c001, c101 := gd(pp(n0+kk+1), g, h, l-1), gd(pp(q0+kk+1), g-1, h, l-1)
	c011, c111 := gd(pp(o0+kk+1), g, h-1, l-1), gd(pp(r0+kk+1), g-1, h-1, l-1)
	sm := func(t float64) float64 { return t * t * t * (t*(t*6-15) + 10) }
	lp := func(t, a, b float64) float64 { return a + t*(b-a) }
	u, v, w := sm(g), sm(h), sm(l)
	return lp(w, lp(v, lp(u, c000, c100), lp(u, c010, c110)), lp(v, lp(u, c001, c101), lp(u, c011, c111)))
}

// geodeNoise is NormalNoise(R, -4, [1]) as the geode builds it from its own Random: two PerlinNoises of
// one used octave each; building one makes the octave-4 noise and drops it, consumes 262 words for each
// of octaves 3 to 1, and makes octave 0.
type geodeNoise struct{ a, b *bImproved }

func newGeodeNoise(r *bmt) geodeNoise {
	one := func() *bImproved {
		newBImproved(r) // Octave 4, past the amplitudes: made and dropped.
		for range 3 {
			for range 262 {
				r.u32()
			}
		}
		return newBImproved(r)
	}
	a := one()
	return geodeNoise{a, one()}
}

func (g geodeNoise) value(x, y, z float64) float64 {
	wrap := func(d float64) float64 { return d - math.Floor(d/3.3554432e7+0.5)*3.3554432e7 }
	at := func(n *bImproved, x, y, z float64) float64 {
		const f = 1.0 / 16
		return n.noise(wrap(x*f), wrap(y*f), wrap(z*f))
	}
	const m = 1.0181268882175227
	return (at(g.a, x, y, z) + at(g.b, x*m, y*m, z*m)) * ((1.0 / 6.0) / (0.1 * (1.0 + 1.0)))
}

// fastInvSqrt is the code's inverse square root: the 0x5f3759df guess and one Newton step, in float32.
func fastInvSqrt(v float32) float32 {
	y := math.Float32frombits(0x5f3759df - math.Float32bits(v)>>1)
	return float32(float32(float32(float32(v*-0.5)*y)*y)+1.5) * y
}

type geodePoint struct{ x, y, z, off int }

func (f *geodeFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	if d.g.structureAt(p.x, p.y, p.z) {
		return p, false // The origin is inside a structure (T15): the code places nothing.
	}
	draw := func(lo, hi int) int {
		if lo < hi {
			return lo + int(r.nextIntN(int32(hi-lo)))
		}
		return lo
	}
	n := draw(f.minPts, f.maxPts)
	dd := float32(float32(n) / float32(f.maxWall))
	noise := newGeodeNoise(r)
	innerT := 1 / float32(math.Sqrt(float64(float32(2.2)+dd)))
	midT := 1 / float32(math.Sqrt(float64(float32(3.2)+dd)))
	outT := 1 / float32(math.Sqrt(float64(float32(4.2)+dd)))
	fillT := math.Float32frombits(0x3f4457d1)
	var dn float32
	if n >= 4 {
		dn = dd
	}
	crackT := 1 / float32(math.Sqrt(float64(float32(dn+float32(r.nextFloat()*0.5))+f.baseCrack)))
	crack := r.nextFloat() < f.crackChance
	pts := make([]geodePoint, 0, n)
	bad := 0
	for range n {
		ox, oy, oz := draw(f.minWall, f.maxWall), draw(f.minWall, f.maxWall), draw(f.minWall, f.maxWall)
		q := pos{p.x + ox, p.y + oy, p.z + oz}
		if q.y < minY || q.y > maxY || bnGeodeInvalid.has(bname(d.block(q.x, q.y, q.z))) {
			if f.invalid <= bad {
				return p, false
			}
			bad++
		}
		pts = append(pts, geodePoint{q.x, q.y, q.z, draw(f.minOff, f.maxOff)})
	}
	var cps []pos
	if crack {
		s := n*2 + 1
		switch r.nextIntN(4) {
		case 0:
			cps = []pos{{p.x + s, p.y + 7, p.z}, {p.x + s, p.y + 5, p.z}, {p.x + s, p.y + 1, p.z}}
		case 1:
			cps = []pos{{p.x, p.y + 7, p.z + s}, {p.x, p.y + 5, p.z + s}, {p.x, p.y + 1, p.z + s}}
		case 2:
			cps = []pos{{p.x + s, p.y + 7, p.z + s}, {p.x + s, p.y + 5, p.z + s}, {p.x + s, p.y + 1, p.z + s}}
		default:
			cps = []pos{{p.x, p.y + 7, p.z}, {p.x, p.y + 5, p.z}, {p.x, p.y + 1, p.z}}
		}
	}
	type write struct {
		q pos
		s bs
	}
	var writes []write
	var potential []pos
	result := false
	npts, ncp := float32(len(pts)), float32(len(cps))
	R := f.maxR
	inWorld := func(q pos) bool { return q.y >= minY && q.y <= maxY && d.canWrite(q.x, q.y, q.z) }
	type scan struct{ found, seen bool }
	column := func(cx, cy int, st *scan) bool {
		had := false
		for cz := p.z - R; cz <= p.z+R; cz++ {
			nv := float32(float32(noise.value(float64(cx), float64(cy), float64(cz))) * f.noiseMul)
			var u float32
			for _, pt := range pts {
				d2 := (cx-pt.x)*(cx-pt.x) + (cy-pt.y)*(cy-pt.y) + (cz-pt.z)*(cz-pt.z)
				u = u + fastInvSqrt(float32(float32(pt.off)+float32(d2)))
			}
			u = float32(npts*nv) + u
			if !(outT <= u) {
				continue
			}
			done := false
			if len(cps) > 0 {
				var cv float32
				for _, c := range cps {
					d2 := (cx-c.x)*(cx-c.x) + (cy-c.y)*(cy-c.y) + (cz-c.z)*(cz-c.z)
					cv = cv + fastInvSqrt(float32(float32(d2)+float32(f.crackOff)))
				}
				if crack && !(float32(ncp*nv)+cv < crackT) && u < fillT {
					q := pos{cx, cy, cz}
					if inWorld(q) && !bnGeodeCannot.has(bname(d.at(q))) {
						d.set(q.x, q.y, q.z, sAir)
						result = true
					}
					done = true
				}
			}
			if !done {
				q := pos{cx, cy, cz}
				switch {
				case fillT <= u:
					writes = append(writes, write{q, f.filler})
				case u < innerT:
					if midT <= u {
						writes = append(writes, write{q, f.middle})
					} else {
						writes = append(writes, write{q, f.outer})
					}
				default:
					if f.alternate <= r.nextFloat() {
						writes = append(writes, write{q, f.inner})
						if !f.requireAlt && r.nextFloat() < f.potential {
							potential = append(potential, q)
						}
					} else {
						writes = append(writes, write{q, f.alt})
						if r.nextFloat() < f.potential {
							potential = append(potential, q)
						}
					}
				}
			}
			if result {
				st.seen, had = true, true
			}
		}
		if !had && !st.seen {
			return false
		}
		st.found = true
		return !had && st.seen
	}
	// The scan: x from the origin up, then down; for each x, y from the origin up, then down.
	anyf := false
	slice := func(xx int) bool { // Says whether to stop the x scan.
		st := scan{}
		for yy := p.y; ; {
			ret := column(xx, yy, &st)
			cond := yy < p.y+R
			yy++
			if ret || !cond {
				break
			}
		}
		up := st.seen
		st.seen = false
		for yy := p.y; yy > p.y-R; {
			yy--
			if column(xx, yy, &st) {
				break
			}
		}
		anyf = anyf || up || st.seen
		return !st.found && anyf
	}
	for xx := p.x; ; xx++ {
		if slice(xx) || xx == p.x+R {
			break
		}
	}
	for xx := p.x; xx > p.x-R; {
		xx--
		if slice(xx) {
			break
		}
	}
	for _, w := range writes {
		if inWorld(w.q) && !bnGeodeCannot.has(bname(d.at(w.q))) {
			d.set(w.q.x, w.q.y, w.q.z, w.s)
			result = true
		}
	}
	if len(f.buds) > 0 {
		dirs := [6]struct {
			o      pos
			facing string
		}{{pos{0, 1, 0}, "up"}, {pos{0, -1, 0}, "down"}, {pos{0, 0, -1}, "north"}, {pos{0, 0, 1}, "south"}, {pos{-1, 0, 0}, "west"}, {pos{1, 0, 0}, "east"}}
		for _, q := range potential {
			bud := f.buds[r.nextIntN(int32(len(f.buds)))]
			for _, dr := range dirs {
				nb := q.add(dr.o.x, dr.o.y, dr.o.z)
				if !inWorld(nb) {
					continue
				}
				cur := d.at(nb)
				if cur.isAir() || bWaterMat(cur) {
					wl := "false"
					if bWaterMat(cur) {
						wl = "true"
					}
					d.set(nb.x, nb.y, nb.z, stateOf(bud+"[facing="+dr.facing+",waterlogged="+wl+"]"))
					break
				}
			}
		}
	}
	return p, result
}

// structureAt says whether a block is inside a structure piece, for features that refuse to start in
// one (the geode). Structures that can answer implement it; without structures nothing is.
func (g *Generator) structureAt(x, y, z int) bool {
	if q, ok := g.structs.(interface{ InsideStructure(x, y, z int) bool }); ok {
		return q.InsideStructure(x, y, z)
	}
	return false
}
