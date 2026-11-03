package worldgen

import (
	"math"
	"strings"
)

// Ore blobs follow Mojang's algorithm from the study (section 8).

func fieldsOf(s string) []string { return strings.Fields(s) }

// stateBlockIndex finds a block's index by name, through its bare state.
func stateBlockIndex(name string) uint16 { return stateBlock[stateOf(name)] }

// oreTarget is one replacement rule: what it may replace, and with what.
type oreTarget struct {
	test  uint32 // Tag bits; zero means the block list.
	list  map[uint16]bool
	state bs
}

func (t *oreTarget) matches(s bs) bool {
	if t.test != 0 {
		return s.tagged(t.test)
	}
	return t.list[stateBlock[s]]
}

// oreSpec is one ore's shape.
type oreSpec struct {
	size    int
	discard float32
	targets []oreTarget
}

// oreScratch is an ore's working space. seen marks keys by stamp, so it needs no clearing; keys outside
// it, which the game's key formula allows, go in the map.
type oreScratch struct {
	stamp uint32
	seen  [1 << 15]uint32
	far   map[int]struct{}
	d     [64][4]float64
	// The Bedrock ore's spheres (bedrock_ores.go).
	bspheres, bactive []oreSphere
}

// see marks a key and says whether it was marked already in this blob.
func (o *oreScratch) see(key int) bool {
	if key >= 0 && key < len(o.seen) {
		if o.seen[key] == o.stamp {
			return true
		}
		o.seen[key] = o.stamp
		return false
	}
	if _, ok := o.far[key]; ok {
		return true
	}
	o.far[key] = struct{}{}
	return false
}

func (o oreSpec) place(d *decoration, p pos) bool {
	r := &d.r
	size := o.size
	ang := float32(r.nextFloat() * pi32)
	sp := float32(size) / 8.0
	maxR := ceilF32((float32(float32(float32(size)/16.0)*2.0) + 1.0) / 2.0)
	sn, cs := math.Sin(float64(ang)), math.Cos(float64(ang))
	x0, x1 := float64(p.x)+float64(sn*float64(sp)), float64(p.x)-float64(sn*float64(sp))
	z0, z1 := float64(p.z)+float64(cs*float64(sp)), float64(p.z)-float64(cs*float64(sp))
	y0 := float64(p.y + r.nextInt(3) - 2)
	y1 := float64(p.y + r.nextInt(3) - 2)
	csp := ceilF32(sp)
	xs, ys, zs := p.x-csp-maxR, p.y-2-maxR, p.z-csp-maxR
	dxz, dy := 2*(csp+maxR), 2*(2+maxR)

	pass := false
gate:
	for px := xs; px <= xs+dxz; px++ {
		for pz := zs; pz <= zs+dxz; pz++ {
			if ys <= d.height(hmOceanFloorWG, px, pz) {
				pass = true
				break gate
			}
		}
	}
	if !pass {
		return false
	}

	sc := &d.ore
	sc.stamp++
	if sc.stamp == 0 {
		clear(sc.seen[:])
		sc.stamp = 1
	}
	if sc.far == nil {
		sc.far = map[int]struct{}{}
	} else if len(sc.far) > 0 {
		clear(sc.far)
	}
	sp64 := sc.d[:size]
	for i := range size {
		t := float32(i) / float32(size)
		cx, cy, cz := lerp(x0, x1, float64(t)), lerp(y0, y1, float64(t)), lerp(z0, z1, float64(t))
		s := float64(float64(r.nextDouble()*float64(size)) / 16.0)
		rr := (float64(float64(tsin(float32(pi32*t))+1.0)*s) + 1.0) / 2.0
		sp64[i] = [4]float64{cx, cy, cz, rr}
	}
	for a := 0; a < size-1; a++ {
		if sp64[a][3] <= 0 {
			continue
		}
		for b := a + 1; b < size; b++ {
			if sp64[b][3] <= 0 {
				continue
			}
			dx, dyy, dz := sp64[a][0]-sp64[b][0], sp64[a][1]-sp64[b][1], sp64[a][2]-sp64[b][2]
			dr := sp64[a][3] - sp64[b][3]
			if float64(dr*dr) > float64(dx*dx)+float64(dyy*dyy)+float64(dz*dz) {
				if dr > 0 {
					sp64[b][3] = -1
				} else {
					sp64[a][3] = -1
				}
			}
		}
	}
	placed := false
	for i := range size {
		e := &sp64[i]
		rr := e[3]
		if rr < 0 {
			continue
		}
		xa, ya, za := max(floorI(e[0]-rr), xs), max(floorI(e[1]-rr), ys), max(floorI(e[2]-rr), zs)
		xb, yb, zb := max(floorI(e[0]+rr), xa), max(floorI(e[1]+rr), ya), max(floorI(e[2]+rr), za)
		for x := xa; x <= xb; x++ {
			xd := (float64(x) + 0.5 - e[0]) / rr
			xd2 := float64(xd * xd)
			if xd2 >= 1 {
				continue
			}
			for y := ya; y <= yb; y++ {
				yd := (float64(y) + 0.5 - e[1]) / rr
				yd2 := float64(yd * yd)
				if xd2+yd2 >= 1 {
					continue
				}
				for z := za; z <= zb; z++ {
					zd := (float64(z) + 0.5 - e[2]) / rr
					if xd2+yd2+float64(zd*zd) >= 1 || y < minY || y > maxY {
						continue
					}
					key := (x - xs) + (y-ys)*dxz + (z-zs)*dxz*dy
					if sc.see(key) {
						continue
					}
					if !d.canWrite(x, y, z) {
						continue
					}
					b := d.block(x, y, z)
					for ti := range o.targets {
						tg := &o.targets[ti]
						if !tg.matches(b) {
							continue
						}
						if o.keeps(d, x, y, z) {
							d.setOre(x, y, z, tg.state)
							placed = true
						}
						break
					}
				}
			}
		}
	}
	return placed
}

// keeps is the air discard rule. The chance is drawn only when a block passed its test.
func (o oreSpec) keeps(d *decoration, x, y, z int) bool {
	if o.discard <= 0 {
		return true
	}
	if o.discard < 1 && d.r.nextFloat() >= o.discard {
		return true
	}
	for _, v := range dirVec {
		if d.block(x+v.x, y+v.y, z+v.z).isAir() {
			return false
		}
	}
	return true
}

func ceilF32(f float32) int { return int(math.Ceil(float64(f))) }

func oreFeature(o oreSpec) feature {
	return func(d *decoration, p pos) bool { return o.place(d, p) }
}

// stoneThenDeep makes the usual two targets of an ore.
func stoneThenDeep(prof *Profile, stone, deep string) []oreTarget {
	dt := tagDeepslateOre
	if prof.oresSkipTuff {
		return []oreTarget{{test: tagStoneOre, state: stateOf(stone)}, {list: blockSet("deepslate"), state: stateOf(deep)}}
	}
	return []oreTarget{{test: tagStoneOre, state: stateOf(stone)}, {test: dt, state: stateOf(deep)}}
}
