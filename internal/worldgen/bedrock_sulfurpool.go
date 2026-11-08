package worldgen

// The sulfur caves' pool (Task 9 round 16): minecraft:sulfur_pool, a feature registered in code, in our
// own Go from the study of BDS 1.26.60.6. NOTES-bedrock-exact T36 read its place function (0x53f8a10)
// with every draw in order; T42 named its blocks from the registration: the fluid is water, the border
// sulfur, and a pool may not touch a sulfur_spike. The shape is Java's lake in float math. The vanilla
// rule places it through sulfur_pool_with_potent_sulfur_snap_to_surface_feature: snapped into a sulfur
// floor, then potent sulfur searched for under it.
//
// Not read, so assumed (NOTES-leftovers L5b):
//   - the "dirt to grass" step's sky test (0xcf5d4e0): taken as "at or above the chunk heightmap";
//     its block list (vector 0xff14198) is dirt and coarse dirt, as FEATS-pseudocode's SpringFeature
//     names the same vector.
//   - the border's position check (block virtual +0x260 must give back the same position): taken to
//     pass for every block, as it does for a one-cell block.

// sulfurPool is the pool's configuration (T42: the registration's fields +0x20, +0x48, +0x70).
type sulfurPool struct {
	fluid, border bs
	noGo          bnameSet // A pool whose edge holds one of these is not placed.
}

var (
	sSulfur        = laterState("sulfur")
	bnPoolDirtList = bnameSetOf("dirt coarse_dirt") // Vector 0xff14198 (FEATS-pseudocode, SpringFeature).
	sulfurPoolCfg  = &sulfurPool{fluid: sWater, border: sSulfur, noGo: bnameSetOf("sulfur_spike")}
)

// poolGrid is the pool's 16 x 16 x 8 cells, x then z then y.
type poolGrid [16 * 16 * 8]bool

func (g *poolGrid) at(x, z, y int) bool { return g[x<<7|z<<3|y] }

// edge says whether a cell outside the pool touches it on one of its six sides.
func (g *poolGrid) edge(x, z, y int) bool {
	if g.at(x, z, y) {
		return false
	}
	return x < 15 && g.at(x+1, z, y) || x > 0 && g.at(x-1, z, y) || z < 15 && g.at(x, z+1, y) || z > 0 && g.at(x, z-1, y) ||
		y < 7 && g.at(x, z, y+1) || y > 0 && g.at(x, z, y-1)
}

// place is SulfurPool's place (T36).
func (f *sulfurPool) place(d *decoration, r *bmt, p pos) bool {
	if p.y <= minY+4 {
		return false
	}
	// The corner column walks down through air, but not below y 1 (no draws).
	o := pos{p.x - 8, p.y, p.z - 8}
	for o.y > minY {
		if !d.at(o).isAir() || o.y < 1 {
			break
		}
		o.y--
	}
	o.y -= 4
	var g poolGrid
	n := int(r.nextIntN(4)) + 4
	for range n {
		sx := float32(r.nextFloat()*6) + 3
		sy := float32(r.nextFloat()*4) + 2
		sz := float32(r.nextFloat()*6) + 3
		hx, hy, hz := float32(sx*0.5), float32(sy*0.5), float32(sz*0.5)
		cx := float32(float32((16-sx)-2)*r.nextFloat()) + 1 + hx
		cy := float32(float32((8-sy)-4)*r.nextFloat()) + 2 + hy
		cz := float32(float32((16-sz)-2)*r.nextFloat()) + 1 + hz
		var ty [7]float32
		for y := 1; y <= 6; y++ {
			v := (float32(y) - cy) / hy
			ty[y] = float32(v * v)
		}
		for x := 1; x <= 14; x++ {
			vx := (float32(x) - cx) / hx
			tx := float32(vx * vx)
			for z := 1; z <= 14; z++ {
				vz := (float32(z) - cz) / hz
				txz := float32(vz*vz) + tx
				for y := 1; y <= 6; y++ {
					if txz+ty[y] < 1 {
						g[x<<7|z<<3|y] = true
					}
				}
			}
		}
	}
	fluidName := bname(f.fluid)
	for x := range 16 {
		for z := range 16 {
			for y := range 8 {
				if !g.edge(x, z, y) {
					continue
				}
				b := d.at(o.add(x, y, z))
				if y >= 4 && b.has(fLiquid) {
					return false
				}
				if y < 4 && !b.has(fSolid) && bname(b) != fluidName {
					return false
				}
				if f.noGo.has(bname(b)) {
					return false
				}
			}
		}
	}
	for x := range 16 {
		for z := range 16 {
			for y := range 8 {
				if g.at(x, z, y) {
					s := sAir
					if y < 4 {
						s = f.fluid
					}
					d.bput(o.add(x, y, z), s)
				}
			}
		}
	}
	for x := range 16 {
		for z := range 16 {
			for y := 4; y < 8; y++ {
				if !g.at(x, z, y) {
					continue
				}
				q := o.add(x, y, z)
				below := q.add(0, -1, 0)
				// The sky test (assumed): the cell is at or above the chunk heightmap. Asked only when the
				// block below is on the list, so the heightmap is not read for every pool.
				if bnPoolDirtList.has(bname(d.at(below))) && q.y >= d.bedrockHeight(q.x, q.z) {
					d.bput(below, sGrass)
				}
			}
		}
	}
	if f.border.isAir() {
		return true
	}
	for x := range 16 {
		for z := range 16 {
			for y := range 8 {
				if !g.edge(x, z, y) {
					continue
				}
				if y >= 4 && r.nextIntN(2) == 0 {
					continue // The draw comes before the solid test.
				}
				q := o.add(x, y, z)
				// The position check is assumed to pass (see the file's comment).
				if d.at(q).has(fSolid) {
					d.bput(q, f.border)
				}
			}
		}
	}
	return true
}
