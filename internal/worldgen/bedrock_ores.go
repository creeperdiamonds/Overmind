package worldgen

import "math"

// Bedrock's ore feature for the Bedrock profile (Task 4e): OreFeature::place, in our own Go from
// NOTES-bedrock-exact sections 4, 4b, P1, R9 and T2. The rules and their parameters are read from the
// install (bedrock_pack.go).

// oreRule is one replace rule of an ore feature: the block it places and the blocks it may replace.
type oreRule struct{ places, mayReplace string }

// bedrockOreTarget is a replace rule in our states.
type bedrockOreTarget struct {
	state bs
	may   map[uint16]bool
}

type bedrockOre struct {
	count   int
	discard float32
	targets []bedrockOreTarget
}

func newBedrockOre(count int, discard float32, rules []oreRule) *bedrockOre {
	o := &bedrockOre{count: count, discard: discard}
	for _, r := range rules {
		o.targets = append(o.targets, bedrockOreTarget{stateOf(r.places), blockSet(r.mayReplace)})
	}
	return o
}

// oreSphere is one sphere of a blob: its centre, squared radius and radius.
type oreSphere struct{ x, y, z, r2, h float32 }

const bedrockPi = float32(math.Pi)

// sinIndex reads the sine table at a float angle already scaled to table units.
func sinIndex(v float32) float32 { return bedrockSin[int32(v)&0xffff] }

// place is OreFeature::place (section 4).
func (o *bedrockOre) place(d *decoration, r *bmt, p pos) {
	active := o.spheres(r, p, &d.ore.bspheres, &d.ore.bactive)
	forBlobBlocks(active, func(x, y, z int) { o.placeBlock(d, r, x, y, z) })
}

// spheres draws a blob's spheres and drops those a later one holds (section 4). All arithmetic is
// single precision, in the order the notes give; each product is its own float32 so no platform
// fuses a multiply-add. The two slices are scratch.
func (o *bedrockOre) spheres(r *bmt, p pos, all, act *[]oreSphere) []oreSphere {
	countf := float32(o.count)
	inv := float32(1) / countf
	ang := float32(float32(r.nextFloat()*bedrockPi) * 10430.378)
	rr := float32(countf * 0.125)
	sx := float32(sinIndex(ang) * rr)
	cz := float32(rr * sinIndex(float32(ang+16384)))
	fx, fy, fz := float32(p.x), float32(p.y), float32(p.z)
	x0 := float32(sx+8) + fx
	z0 := float32(cz+8) + fz
	x1 := float32(8-sx) + fx
	z1 := fz + float32(8-cz)
	y0 := float32(float32(r.nextIntN(3))-2) + fy
	y1 := float32(float32(r.nextIntN(3))-2) + fy
	dx, dy, dz := x1-x0, y1-y0, z1-z0
	k := float32(inv * bedrockPi)
	spheres := (*all)[:0]
	for i := range o.count {
		fi := float32(i)
		t := float32(inv * fi)
		c := oreSphere{x: float32(t*dx) + x0, y: float32(t*dy) + y0, z: float32(float32(dz*fi)*inv) + z0}
		rnd := r.nextFloat()
		sv := sinIndex(float32(float32(fi*k) * 10430.378))
		a := float32(float32(sv+1) * rnd)
		a = float32(a * countf)
		a = float32(a * 0.0625)
		rad := float32(float32(a+1) * 0.5)
		c.r2 = float32(rad * rad)
		c.h = float32(math.Sqrt(float64(c.r2)))
		spheres = append(spheres, c)
	}
	active := (*act)[:0]
	for i, a := range spheres {
		inside := false
		for _, b := range spheres[i+1:] {
			if a.h > b.h {
				continue
			}
			ddz, ddx, ddy := b.z-a.z, b.x-a.x, b.y-a.y
			sum := float32(float32(ddz*ddz)+float32(ddx*ddx)) + float32(ddy*ddy)
			if float32(float32(math.Sqrt(float64(sum)))+a.h) < b.h {
				inside = true
				break
			}
		}
		if !inside {
			active = append(active, a)
		}
	}
	*all, *act = spheres, active
	return active
}

// forBlobBlocks visits every block inside a blob's spheres in Bedrock's order: x outer, z, then y.
func forBlobBlocks(active []oreSphere, visit func(x, y, z int)) {
	if len(active) == 0 {
		return
	}
	lo := [3]int{math.MaxInt32, math.MaxInt32, math.MaxInt32}
	hi := [3]int{math.MinInt32, math.MinInt32, math.MinInt32}
	for _, a := range active {
		for i, v := range [3]float32{a.x, a.y, a.z} {
			lo[i] = min(lo[i], int(math.Floor(float64(float32(v-a.h)))))
			hi[i] = max(hi[i], int(math.Floor(float64(float32(v+a.h)))))
		}
	}
	for bx := lo[0]; bx <= hi[0]; bx++ {
		px := float32(float32(bx) + 0.5)
		for bz := lo[2]; bz <= hi[2]; bz++ {
			pz := float32(float32(bz) + 0.5)
			for by := lo[1]; by <= hi[1]; by++ {
				py := float32(float32(by) + 0.5)
				for _, a := range active {
					ddz, ddx, ddy := pz-a.z, px-a.x, py-a.y
					d2 := float32(float32(ddz*ddz)+float32(ddx*ddx)) + float32(ddy*ddy)
					if d2 < a.r2 {
						visit(bx, by, bz)
						break
					}
				}
			}
		}
	}
}

// placeBlock is the replace step for one block of a blob (section 4).
func (o *bedrockOre) placeBlock(d *decoration, r *bmt, x, y, z int) {
	if !d.canWrite(x, y, z) {
		return
	}
	cur := d.block(x, y, z)
	for _, t := range o.targets {
		if !t.may[stateBlock[cur]] || cur == t.state {
			continue
		}
		if o.discard > 0 {
			kept := false
			if o.discard < 1 {
				kept = r.nextFloat() >= o.discard
			}
			if !kept && d.exposedToAir(x, y, z) {
				continue
			}
		}
		d.set(x, y, z, t.state)
		return
	}
}

// exposedToAir says whether one of the six neighbours is air. A neighbour outside the world, or in a
// chunk a source cannot read, is not.
func (d *decoration) exposedToAir(x, y, z int) bool {
	for _, v := range dirVec {
		nx, ny, nz := x+v.x, y+v.y, z+v.z
		if ny < minY || ny > maxY || d.chunkAt(nx>>4, nz>>4) == nil {
			continue
		}
		if d.block(nx, ny, nz).isAir() {
			return true
		}
	}
	return false
}
