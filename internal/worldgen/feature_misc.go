package worldgen

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Lakes, rooms, disks, springs, geodes, magma, plants and freezing follow Mojang's algorithms from the
// study (sections 9, 11, 12). Parts the study marks unverified are inferred and tested on the traces.

func (d *decoration) safe(p pos, s bs) {
	if !d.at(p).tagged(tagNoFeature) {
		d.set(p.x, p.y, p.z, s)
	}
}

// lakeFeature is a lava lake.
func lakeFeature(fluid, barrier bs) feature {
	return func(d *decoration, pp pos) bool {
		if pp.y <= minY+4 {
			return false
		}
		r := &d.r
		o := pp.add(-8, -4, -8)
		var g [16][16][8]bool
		spots := r.nextInt(4) + 4
		for range spots {
			xr, yr, zr := float64(r.nextDouble()*6)+3, float64(r.nextDouble()*4)+2, float64(r.nextDouble()*6)+3
			xp := float64(r.nextDouble()*(16-xr-2)) + 1 + xr/2
			yp := float64(r.nextDouble()*(8-yr-4)) + 2 + yr/2
			zp := float64(r.nextDouble()*(16-zr-2)) + 1 + zr/2
			for x := 1; x < 15; x++ {
				for z := 1; z < 15; z++ {
					for y := 1; y < 7; y++ {
						a, b, c := (float64(x)-xp)/(xr/2), (float64(y)-yp)/(yr/2), (float64(z)-zp)/(zr/2)
						if float64(a*a)+float64(b*b)+float64(c*c) < 1 {
							g[x][z][y] = true
						}
					}
				}
			}
		}
		edge := func(x, z, y int) bool {
			if g[x][z][y] {
				return false
			}
			return (x < 15 && g[x+1][z][y]) || (x > 0 && g[x-1][z][y]) || (z < 15 && g[x][z+1][y]) || (z > 0 && g[x][z-1][y]) || (y < 7 && g[x][z][y+1]) || (y > 0 && g[x][z][y-1])
		}
		for x := range 16 {
			for z := range 16 {
				for y := range 8 {
					if edge(x, z, y) {
						b := d.at(o.add(x, y, z))
						if y >= 4 && b.has(fLiquid) {
							return false
						}
						if y < 4 && !b.has(fSolid) && b != fluid {
							return false
						}
					}
				}
			}
		}
		caveAir := stateOf("cave_air")
		for x := range 16 {
			for z := range 16 {
				for y := range 8 {
					if g[x][z][y] {
						p := o.add(x, y, z)
						if !d.at(p).tagged(tagNoFeature) {
							if y >= 4 {
								d.set(p.x, p.y, p.z, caveAir)
							} else {
								d.set(p.x, p.y, p.z, fluid)
							}
						}
					}
				}
			}
		}
		for x := range 16 {
			for z := range 16 {
				for y := range 8 {
					if edge(x, z, y) && (y < 4 || r.nextInt(2) != 0) {
						p := o.add(x, y, z)
						b := d.at(p)
						if b.has(fSolid) && !b.tagged(tagNoLavaPool) {
							d.set(p.x, p.y, p.z, barrier)
						}
					}
				}
			}
		}
		return true
	}
}

// roomFeature is a monster room. The chest's facing is inferred from the game's reorient rule.
func roomFeature(d *decoration, p pos) bool {
	r := &d.r
	xr, zr := r.nextInt(2)+2, r.nextInt(2)+2
	minX, maxX, minZ, maxZ := -xr-1, xr+1, -zr-1, zr+1
	holes := 0
	for dx := minX; dx <= maxX; dx++ {
		for dy := -1; dy <= 4; dy++ {
			for dz := minZ; dz <= maxZ; dz++ {
				q := p.add(dx, dy, dz)
				s := d.at(q).has(fSolid)
				if (dy == -1 || dy == 4) && !s {
					return false
				}
				if (dx == minX || dx == maxX || dz == minZ || dz == maxZ) && dy == 0 && d.at(q).isAir() && d.at(q.step(dUp)).isAir() {
					holes++
				}
			}
		}
	}
	if holes < 1 || holes > 5 {
		return false
	}
	caveAir := stateOf("cave_air")
	cobble, mossy := stateOf("cobblestone"), stateOf("mossy_cobblestone")
	chestB, spawner := stateOf("chest[facing=north,type=single,waterlogged=false]").block(), stateOf("spawner")
	for dx := minX; dx <= maxX; dx++ {
		for dy := 3; dy >= -1; dy-- {
			for dz := minZ; dz <= maxZ; dz++ {
				w := p.add(dx, dy, dz)
				st := d.at(w)
				if dx == minX || dx == maxX || dz == minZ || dz == maxZ || dy == -1 {
					if !d.at(w.step(dDown)).has(fSolid) {
						d.set(w.x, w.y, w.z, caveAir)
					} else if st.has(fSolid) && st.block() != chestB {
						if dy == -1 && r.nextInt(4) != 0 {
							d.safe(w, mossy)
						} else {
							d.safe(w, cobble)
						}
					}
				} else if st.block() != chestB && st != spawner {
					d.safe(w, caveAir)
				}
			}
		}
	}
	for range 2 {
		for range 3 {
			xc := p.x + r.nextInt(xr*2+1) - xr
			zc := p.z + r.nextInt(zr*2+1) - zr
			c := pos{xc, p.y, zc}
			if !d.at(c).isAir() {
				continue
			}
			n := 0
			for _, a := range horizontal {
				if d.at(c.step(a)).has(fSolid) {
					n++
				}
			}
			if n == 1 {
				d.safe(c, stateOf(fmt.Sprintf("chest[facing=%s,type=single,waterlogged=false]", dirName(chestFacing(d, c)))))
				r.nextLong()
				break
			}
		}
	}
	d.safe(p, spawner)
	r.nextInt(4)
	return true
}

// chestFacing turns a chest away from its one solid neighbour.
func chestFacing(d *decoration, c pos) dir {
	found := -1
	for _, a := range horizontal {
		if d.at(c.step(a)).has(fSolidRender) {
			if found >= 0 {
				found = -1
				break
			}
			found = int(a)
		}
	}
	if found >= 0 {
		return dir(found).opposite()
	}
	f := dNorth
	if d.at(c.step(f)).has(fSolidRender) {
		f = f.opposite()
	}
	if d.at(c.step(f)).has(fSolidRender) {
		f = f.clockwise()
	}
	if d.at(c.step(f)).has(fSolidRender) {
		f = f.opposite()
	}
	return f
}

// diskFeature is a round patch: x fastest, as the study warns.
func diskFeature(radius intProvider, half int, target predicate, provider optionalState) feature {
	return func(d *decoration, p pos) bool {
		top, bot := p.y+half, p.y-half-1
		r := radius(&d.r)
		placed := false
		for z := p.z - r; z <= p.z+r; z++ {
			for x := p.x - r; x <= p.x+r; x++ {
				dx, dz := x-p.x, z-p.z
				if dx*dx+dz*dz > r*r {
					continue
				}
				for y := top; y > bot; y-- {
					q := pos{x, y, z}
					if target(d, q) {
						if s, ok := provider(d, q); ok {
							d.set(x, y, z, s)
							placed = true
						}
					}
				}
			}
		}
		return placed
	}
}

func always(s bs) optionalState {
	return func(*decoration, pos) (bs, bool) { return s, true }
}

// springFeature is section 9's spring: it changes one block and draws nothing.
func springFeature(state bs, valid map[uint16]bool, rock, hole int, needBelow bool) feature {
	return func(d *decoration, p pos) bool {
		in := func(q pos) bool { return valid[stateBlock[d.at(q)]] }
		if !in(p.step(dUp)) {
			return false
		}
		if needBelow && !in(p.step(dDown)) {
			return false
		}
		if s := d.at(p); !s.isAir() && !valid[stateBlock[s]] {
			return false
		}
		rocks, holes := 0, 0
		for _, a := range [5]dir{dWest, dEast, dNorth, dSouth, dDown} {
			q := p.step(a)
			if in(q) {
				rocks++
			}
			if d.at(q).isAir() {
				holes++
			}
		}
		if rocks == rock && holes == hole {
			d.set(p.x, p.y, p.z, state)
			return true
		}
		return false
	}
}

// legacyNormal is a normal noise built the study's way for geodes and noise providers: on the old
// 48-bit generator, with a forked positional octave.
func legacyNormal(seed int64, first int, amps []float64) *normalNoise {
	l := newLCG(seed)
	stack := func() octaveStack {
		fa := l.nextLong()
		n := len(amps)
		inF, valueF := pow2(first), pow2(n-1)/(pow2(n)-1)
		s := octaveStack{}
		for i, a := range amps {
			if a != 0 {
				name := "octave_" + strconv.Itoa(first+i)
				g := newLCG(int64(javaStringHash(name)) ^ fa)
				o := new(octave)
				o.initLegacy(&g)
				s.levels = append(s.levels, stackLevel{o, inF, a, valueF})
			}
			inF *= 2
			valueF /= 2
		}
		return s
	}
	n := &normalNoise{first: stack()}
	n.second = stack()
	lo, hi := len(amps), -1
	for i, a := range amps {
		if a != 0 {
			lo, hi = min(lo, i), max(hi, i)
		}
	}
	n.scale = 0.16666666666666666 / (0.1 * (1.0 + 1.0/float64(hi-lo+1)))
	return n
}

func javaStringHash(s string) int32 {
	var h int32
	for _, c := range s {
		h = h*31 + int32(c)
	}
	return h
}

func (o *octave) initLegacy(r *lcg) {
	o.xo = float64(r.nextDouble() * 256)
	o.yo = float64(r.nextDouble() * 256)
	o.zo = float64(r.nextDouble() * 256)
	for i := range o.perm {
		o.perm[i] = uint8(i)
	}
	for i := range 256 {
		j := i + int(r.nextInt(int32(256-i)))
		o.perm[i], o.perm[j] = o.perm[j], o.perm[i]
	}
}

// geodeFeature is the amethyst geode.
func geodeFeature(d *decoration, pp pos) bool {
	r := &d.r
	amethyst, budding := stateOf("amethyst_block"), stateOf("budding_amethyst")
	calcite, basalt := sCalcite, stateOf("smooth_basalt")
	buds := [4]string{"small_amethyst_bud", "medium_amethyst_bud", "large_amethyst_bud", "amethyst_cluster"}
	bad := func(s bs) bool { return s.isAir() || s.tagged(tagGeodeBad) }
	n := r.between(3, 4)
	adj := float64(n) / 6.0
	iAir := 1.0 / math.Sqrt(1.7)
	iIn := 1.0 / math.Sqrt(2.2+adj)
	iMid := 1.0 / math.Sqrt(3.2+adj)
	iOut := 1.0 / math.Sqrt(4.2+adj)
	extra := 0.0
	if n > 3 {
		extra = adj
	}
	crackSz := 1.0 / math.Sqrt(2.0+r.nextDouble()/2.0+extra)
	crack := r.nextFloat() < 0.95
	badN := 0
	type pt struct {
		p   pos
		off int
	}
	pts := make([]pt, 0, 4)
	for range n {
		dx, dy, dz := r.between(4, 6), r.between(4, 6), r.between(4, 6)
		q := pp.add(dx, dy, dz)
		if bad(d.at(q)) {
			badN++
			if badN > 1 {
				return false
			}
		}
		pts = append(pts, pt{q, r.between(1, 2)})
	}
	var cracks []pos
	if crack {
		k := r.nextInt(4)
		o := n*2 + 1
		base := [4][2]int{{o, 0}, {0, o}, {o, o}, {0, 0}}[k]
		for _, y := range [3]int{7, 5, 1} {
			cracks = append(cracks, pp.add(base[0], y, base[1]))
		}
	}
	noise := d.g.geodeNoise()
	var places []pos
	dist := func(a, b pos) float64 {
		x, y, z := float64(a.x-b.x), float64(a.y-b.y), float64(a.z-b.z)
		return float64(x*x) + float64(y*y) + float64(z*z)
	}
	for z := pp.z - 16; z <= pp.z+16; z++ {
		for y := pp.y - 16; y <= pp.y+16; y++ {
			for x := pp.x - 16; x <= pp.x+16; x++ {
				p := pos{x, y, z}
				nv := float64(noise.at(float64(x), float64(y), float64(z)) * 0.05)
				shell, cr := 0.0, 0.0
				for _, q := range pts {
					shell += 1.0/math.Sqrt(dist(p, q.p)+float64(q.off)) + nv
				}
				for _, q := range cracks {
					cr += 1.0/math.Sqrt(dist(p, q)+2.0) + nv
				}
				if shell < iOut {
					continue
				}
				switch {
				case crack && cr >= crackSz && shell < iAir:
					d.safe(p, sAir)
				case shell >= iAir:
					d.safe(p, sAir)
				case shell >= iIn:
					alt := r.nextFloat() < 0.083
					if alt {
						d.safe(p, budding)
					} else {
						d.safe(p, amethyst)
					}
					if alt && r.nextFloat() < 0.35 {
						places = append(places, p)
					}
				case shell >= iMid:
					d.safe(p, calcite)
				default:
					d.safe(p, basalt)
				}
			}
		}
	}
	for _, p := range places {
		name := buds[r.nextInt(4)]
		for _, a := range allDirs {
			q := p.step(a)
			b := d.at(q)
			water := b.has(fWater) && b.has(fSource)
			if b.isAir() || water {
				d.safe(q, stateOf(fmt.Sprintf("%s[facing=%s,waterlogged=%v]", name, dirName(a), water)))
				break
			}
		}
	}
	return true
}

// magmaFeature is underwater magma. Its floor scan is inferred and tested on the traces.
func magmaFeature(search, radius int, chance float32) feature {
	magma := stateOf("magma_block")
	return func(d *decoration, o pos) bool {
		if d.at(o) != sWater {
			return false
		}
		q := o
		for i := 1; i < search && d.at(q) == sWater; i++ {
			q = q.step(dDown)
		}
		if d.at(q) == sWater {
			return false
		}
		f := q.y
		placed := false
		visible := func(p pos) bool { return !d.at(p).has(fSolidRender) }
		for z := o.z - radius; z <= o.z+radius; z++ {
			for y := f - radius; y <= f+radius; y++ {
				for x := o.x - radius; x <= o.x+radius; x++ {
					p := pos{x, y, z}
					if d.r.nextFloat() >= chance {
						continue
					}
					s := d.at(p)
					if s == sWater || s.isAir() || visible(p.step(dDown)) {
						continue
					}
					if visible(p.step(dNorth)) || visible(p.step(dEast)) || visible(p.step(dSouth)) || visible(p.step(dWest)) {
						continue
					}
					d.set(x, y, z, magma)
					placed = true
				}
			}
		}
		return placed
	}
}

// canSurvive is the plant rules the study did not write out, inferred and tested on the traces.
func canSurvive(d *decoration, s bs, p pos) bool {
	base := blockName[stateBlock[s]]
	below := d.at(p.step(dDown))
	switch base {
	case "dead_bush", "short_dry_grass", "tall_dry_grass":
		return below.tagged(tagDirtLike) || below.tagged(tagSand) || below.tagged(tagTerracotta)
	case "cactus":
		for _, a := range horizontal {
			if n := d.at(p.step(a)); n.has(fSolid) || n.has(fLiquid) {
				return false
			}
		}
		bn := blockName[stateBlock[below]]
		return (bn == "cactus" || below.tagged(tagSand)) && !d.at(p.step(dUp)).has(fLiquid)
	case "sugar_cane":
		if blockName[stateBlock[below]] == "sugar_cane" {
			return true
		}
		if !below.tagged(tagDirtLike) && !below.tagged(tagSand) {
			return false
		}
		q := p.step(dDown)
		for _, a := range horizontal {
			if n := d.at(q.step(a)); n.has(fWater) || blockName[stateBlock[n]] == "frosted_ice" {
				return true
			}
		}
		return false
	case "lily_pad":
		return below == sWater || below == sIce
	case "brown_mushroom", "red_mushroom":
		return below == sMycelium || below == sPodzol
	case "seagrass", "tall_seagrass":
		return below.has(fSturdyUp) && blockName[stateBlock[below]] != "magma_block"
	case "kelp", "kelp_plant":
		bn := blockName[stateBlock[below]]
		return bn == "kelp" || bn == "kelp_plant" || (below.has(fSturdyUp) && bn != "magma_block")
	case "sea_pickle":
		return below.has(fSturdyUp)
	case "snow":
		bn := blockName[stateBlock[below]]
		if bn == "ice" || bn == "packed_ice" {
			return false
		}
		return below.has(fSturdyUp) || below.tagged(tagLeaves)
	case "bamboo":
		return below.tagged(tagBambooSoil)
	case "pumpkin", "melon":
		return true
	}
	return below.tagged(tagDirtLike)
}

func wouldSurvive(name string) predicate {
	s := stateOf(name)
	return func(d *decoration, p pos) bool { return canSurvive(d, s, p) }
}

// doublePlant is a state pair written as lower and upper.
func isDouble(s bs) bool { return strings.Contains(s.name(), "half=lower") }

// simpleBlock is the simple_block feature.
func simpleBlock(pv stateProvider) feature {
	return func(d *decoration, p pos) bool {
		s := pv(d, p)
		if !canSurvive(d, s, p) {
			return false
		}
		if isDouble(s) {
			up := p.step(dUp)
			if !d.at(up).isAir() {
				return false
			}
			d.set(p.x, p.y, p.z, s)
			d.set(up.x, up.y, up.z, withProp(s, "half", "upper"))
			return true
		}
		d.set(p.x, p.y, p.z, s)
		return true
	}
}

// selector is a random_selector: the first entry whose chance lands, or the default.
type selEntry struct {
	chance float32
	pl     *placement
}

func randomSelector(def *placement, list ...selEntry) feature {
	return func(d *decoration, p pos) bool {
		for _, e := range list {
			if d.r.nextFloat() < e.chance {
				return d.place(e.pl, p)
			}
		}
		return d.place(def, p)
	}
}

func simpleSelector(list ...*placement) feature {
	return func(d *decoration, p pos) bool { return d.place(list[d.r.nextInt(len(list))], p) }
}

func boolSelector(a, b *placement) feature {
	return func(d *decoration, p pos) bool {
		if d.r.nextBool() {
			return d.place(a, p)
		}
		return d.place(b, p)
	}
}

// blockColumn is the block_column feature, with layers of height and provider.
type columnLayer struct {
	h  intProvider
	pv stateProvider
}

func blockColumn(a dir, allowed predicate, tipFirst bool, layers ...columnLayer) feature {
	return func(d *decoration, o pos) bool {
		hs := make([]int, len(layers))
		total := 0
		for i, l := range layers {
			hs[i] = l.h(&d.r)
			total += hs[i]
		}
		if total == 0 {
			return false
		}
		n := o.step(a)
		for y := 0; y < total-1+1; y++ {
			if y == total-1 {
				break
			}
			if !allowed(d, n) {
				rem := total - (y + 1)
				if tipFirst {
					for i := 0; i < len(hs) && rem > 0; i++ {
						t := min(hs[i], rem)
						rem -= t
						hs[i] -= t
					}
				} else {
					for i := len(hs) - 1; i >= 0 && rem > 0; i-- {
						t := min(hs[i], rem)
						rem -= t
						hs[i] -= t
					}
				}
				break
			}
			n = n.step(a)
		}
		p := o
		for i, l := range layers {
			for range hs[i] {
				d.set(p.x, p.y, p.z, l.pv(d, p))
				p = p.step(a)
			}
		}
		return true
	}
}

// seagrassFeature, kelpFeature and picklesFeature read the ocean floor.
func seagrassFeature(prob float64) feature {
	short := stateOf("seagrass")
	tallL, tallU := stateOf("tall_seagrass[half=lower]"), stateOf("tall_seagrass[half=upper]")
	return func(d *decoration, p pos) bool {
		r := &d.r
		x := r.nextInt(8) - r.nextInt(8)
		z := r.nextInt(8) - r.nextInt(8)
		y := d.height(hmOceanFloor, p.x+x, p.z+z)
		g := pos{p.x + x, y, p.z + z}
		if d.at(g) != sWater {
			return false
		}
		tall := r.nextDouble() < prob
		s := short
		if tall {
			s = tallL
		}
		if !canSurvive(d, s, g) {
			return false
		}
		if tall {
			if d.at(g.step(dUp)) != sWater {
				return false
			}
			d.set(g.x, g.y, g.z, tallL)
			d.set(g.x, g.y+1, g.z, tallU)
		} else {
			d.set(g.x, g.y, g.z, short)
		}
		return true
	}
}

func kelpFeature(d *decoration, p pos) bool {
	r := &d.r
	y := d.height(hmOceanFloor, p.x, p.z)
	k := pos{p.x, y, p.z}
	if d.at(k) != sWater {
		return false
	}
	plant := stateOf("kelp_plant")
	h := 1 + r.nextInt(10)
	placed := false
	age := func() bs { return stateOf("kelp[age=" + strconv.Itoa(r.nextInt(4)+20) + "]") }
	for i := 0; i <= h; i++ {
		if d.at(k) == sWater && d.at(k.step(dUp)) == sWater && canSurvive(d, plant, k) {
			if i == h {
				d.set(k.x, k.y, k.z, age())
				placed = true
			} else {
				d.set(k.x, k.y, k.z, plant)
			}
		} else if i > 0 {
			b := k.step(dDown)
			kb := stateOf("kelp[age=25]")
			if canSurvive(d, kb, b) && blockName[stateBlock[d.at(b.step(dDown))]] != "kelp" {
				d.set(b.x, b.y, b.z, age())
				placed = true
			}
			break
		}
		k = k.step(dUp)
	}
	return placed
}

func picklesFeature(count int) feature {
	return func(d *decoration, p pos) bool {
		r := &d.r
		placed := false
		for range count {
			x := r.nextInt(8) - r.nextInt(8)
			z := r.nextInt(8) - r.nextInt(8)
			y := d.height(hmOceanFloor, p.x+x, p.z+z)
			g := pos{p.x + x, y, p.z + z}
			n := r.nextInt(4) + 1
			s := stateOf(fmt.Sprintf("sea_pickle[pickles=%d,waterlogged=true]", n))
			if d.at(g) == sWater && canSurvive(d, s, g) {
				d.set(g.x, g.y, g.z, s)
				placed = true
			}
		}
		return placed
	}
}

// vinesFeature hangs one vine. Which neighbours count is inferred from the game's rule and the traces.
func vinesFeature(d *decoration, p pos) bool {
	if !d.at(p).isAir() {
		return false
	}
	for _, a := range allDirs {
		if a == dDown {
			continue
		}
		n := d.at(p.step(a))
		ok := n.has(fSturdySide)
		if a == dUp {
			ok = n.has(fSturdyDown)
		}
		bn := blockName[stateBlock[n]]
		if !ok && !n.tagged(tagLeaves) {
			continue
		}
		if bn == "shulker_box" || bn == "glass" {
			continue
		}
		d.set(p.x, p.y, p.z, vineState(dirName(a)))
		return true
	}
	return false
}

// spikeFeature is the ice spike.
func spikeFeature(state bs, on, canReplace map[uint16]bool) feature {
	return func(d *decoration, o pos) bool {
		r := &d.r
		pp := o
		for d.at(pp).isAir() && pp.y > minY+2 {
			pp = pp.step(dDown)
		}
		if !on[stateBlock[d.at(pp)]] {
			return false
		}
		pp = pp.add(0, r.nextInt(4), 0)
		h := r.nextInt(4) + 7
		w := h/4 + r.nextInt(2)
		if w > 1 && r.nextInt(60) == 0 {
			pp = pp.add(0, 10+r.nextInt(30), 0)
		}
		repl := func(q pos) {
			s := d.at(q)
			if s.isAir() || canReplace[stateBlock[s]] {
				d.set(q.x, q.y, q.z, state)
			}
		}
		for yo := range h {
			sc := float32(float32(1.0-float32(yo)/float32(h)) * float32(w))
			nw := ceilF32(sc)
			for xo := -nw; xo <= nw; xo++ {
				dx := float32(absInt(xo)) - 0.25
				for zo := -nw; zo <= nw; zo++ {
					dz := float32(absInt(zo)) - 0.25
					if ((xo == 0 && zo == 0) || !(float32(dx*dx)+float32(dz*dz) > float32(sc*sc))) &&
						((xo != -nw && xo != nw && zo != -nw && zo != nw) || !(r.nextFloat() > 0.75)) {
						repl(pp.add(xo, yo, zo))
						if yo != 0 && nw > 1 {
							repl(pp.add(xo, -yo, zo))
						}
					}
				}
			}
		}
		pw := min(max(w-1, 0), 1)
		for xo := -pw; xo <= pw; xo++ {
			for zo := -pw; zo <= pw; zo++ {
				c := pp.add(xo, -1, zo)
				run := 50
				if absInt(xo) == 1 && absInt(zo) == 1 {
					run = r.nextInt(5)
				}
				for c.y > 50 {
					s := d.at(c)
					if !s.isAir() && !canReplace[stateBlock[s]] && s != state {
						break
					}
					d.set(c.x, c.y, c.z, state)
					c = c.step(dDown)
					run--
					if run <= 0 {
						c = c.add(0, -(r.nextInt(5) + 1), 0)
						run = r.nextInt(5)
					}
				}
			}
		}
		return true
	}
}

// blobFeature is forest_rock.
func blobFeature(state bs, on predicate) feature {
	return func(d *decoration, o pos) bool {
		r := &d.r
		p := o
		for p.y > minY+3 && !on(d, p.step(dDown)) {
			p = p.step(dDown)
		}
		if p.y <= minY+3 {
			return false
		}
		for range 3 {
			xr, yr, zr := r.nextInt(2), r.nextInt(2), r.nextInt(2)
			tr := float32(float32(xr+yr+zr)*0.333) + 0.5
			for z := p.z - zr; z <= p.z+zr; z++ {
				for y := p.y - yr; y <= p.y+yr; y++ {
					for x := p.x - xr; x <= p.x+xr; x++ {
						dx, dy, dz := x-p.x, y-p.y, z-p.z
						if float64(dx*dx+dy*dy+dz*dz) <= float64(float32(tr*tr)) {
							d.set(x, y, z, state)
						}
					}
				}
			}
			p = p.add(-1+r.nextInt(2), -r.nextInt(2), -1+r.nextInt(2))
		}
		return true
	}
}

// bambooFeature is section 11's bamboo.
func bambooFeature(prob float32) feature {
	stalk := stateOf("bamboo[age=1,leaves=none,stage=0]")
	large1 := stateOf("bamboo[age=1,leaves=large,stage=1]")
	large0 := stateOf("bamboo[age=1,leaves=large,stage=0]")
	small := stateOf("bamboo[age=1,leaves=small,stage=0]")
	return func(d *decoration, o pos) bool {
		r := &d.r
		p := o
		if !d.at(p).isAir() || !canSurvive(d, stalk, p) {
			return false
		}
		h := r.nextInt(12) + 5
		if r.nextFloat() < prob {
			rr := r.nextInt(4) + 1
			for xx := o.x - rr; xx <= o.x+rr; xx++ {
				for zz := o.z - rr; zz <= o.z+rr; zz++ {
					dx, dz := xx-o.x, zz-o.z
					if dx*dx+dz*dz <= rr*rr {
						q := pos{xx, d.height(hmWorldSurface, xx, zz) - 1, zz}
						if d.at(q).tagged(tagPodzolSwap) {
							d.set(q.x, q.y, q.z, sPodzol)
						}
					}
				}
			}
		}
		for i := 0; i < h && d.at(p).isAir(); i++ {
			d.set(p.x, p.y, p.z, stalk)
			p = p.step(dUp)
		}
		if p.y-o.y >= 3 {
			d.set(p.x, p.y, p.z, large1)
			p = p.step(dDown)
			d.set(p.x, p.y, p.z, large0)
			p = p.step(dDown)
			d.set(p.x, p.y, p.z, small)
		}
		return true
	}
}

func mushroomState(name string, up, down, north, south, east, west bool) bs {
	return stateOf(fmt.Sprintf("%s[down=%v,east=%v,north=%v,south=%v,up=%v,west=%v]", name, down, east, north, south, up, west))
}

// hugeMushroom is section 11's huge mushroom.
func hugeMushroom(red bool, fr int) feature {
	cap := "brown_mushroom_block"
	if red {
		cap = "red_mushroom_block"
	}
	stem := mushroomState("mushroom_stem", false, false, true, true, true, true)
	return func(d *decoration, o pos) bool {
		r := &d.r
		h := r.nextInt(3) + 4
		if r.nextInt(12) == 0 {
			h *= 2
		}
		if o.y < minY+1 || o.y+h+1 > maxY+1 {
			return false
		}
		bl := d.at(o.step(dDown))
		if !bl.tagged(tagDirtLike) {
			return false
		}
		radius := func(dy int) int {
			if red {
				if dy >= h-3 && dy <= h {
					return fr
				}
				return 0
			}
			if dy <= 3 {
				return 0
			}
			return fr
		}
		for dy := 0; dy <= h; dy++ {
			rr := radius(dy)
			for dx := -rr; dx <= rr; dx++ {
				for dz := -rr; dz <= rr; dz++ {
					s := d.at(o.add(dx, dy, dz))
					if !s.isAir() && !s.tagged(tagLeaves) {
						return false
					}
				}
			}
		}
		put := func(p pos, st bs) {
			s := d.at(p)
			if s.isAir() || s.has(fReplaceable) {
				d.set(p.x, p.y, p.z, st)
			}
		}
		if red {
			for dy := h - 3; dy <= h; dy++ {
				rr := fr
				if dy >= h {
					rr = fr - 1
				}
				c := fr - 2
				for dx := -rr; dx <= rr; dx++ {
					for dz := -rr; dz <= rr; dz++ {
						xe, ze := absInt(dx) == rr, absInt(dz) == rr
						if dy >= h || xe != ze {
							put(o.add(dx, dy, dz), mushroomState(cap, dy >= h, false, dz < -c, dz > c, dx > c, dx < -c))
						}
					}
				}
			}
		} else {
			for dx := -fr; dx <= fr; dx++ {
				for dz := -fr; dz <= fr; dz++ {
					xe, ze := absInt(dx) == fr, absInt(dz) == fr
					if !(xe && ze) {
						west := dx == -fr || (ze && dx == 1-fr)
						east := dx == fr || (ze && dx == fr-1)
						north := dz == -fr || (xe && dz == 1-fr)
						south := dz == fr || (xe && dz == fr-1)
						put(o.add(dx, h, dz), mushroomState(cap, true, false, north, south, east, west))
					}
				}
			}
		}
		for dy := range h {
			put(o.add(0, dy, 0), stem)
		}
		return true
	}
}

// fallenTree is section 11's fallen tree.
func fallenTree(logName string, length intProvider, stump []treeDecorator, logDeco []treeDecorator) feature {
	return func(d *decoration, o pos) bool {
		r := &d.r
		t := &treeRun{d: d, cfg: &treeCfg{}}
		t.trunkSet.reset()
		t.leafSet.reset()
		t.decoSet.reset()
		t.rootSet.reset()
		d.set(o.x, o.y, o.z, logState(logName, 'y'))
		for _, dec := range stump {
			dec(t, []pos{o}, nil, nil)
		}
		a := pickHorizontal(r)
		l := length(r) - 2
		s := o.stepN(a, 2+r.nextInt(2)).step(dUp)
		valid := func(p pos) bool {
			st := d.at(p)
			return st.isAir() || st.has(fReplaceable)
		}
		for range 6 {
			if valid(s) && d.at(s.step(dDown)).has(fSturdyUp) {
				break
			}
			s = s.step(dDown)
		}
		gap := 0
		q := s
		for range l {
			if !valid(q) {
				return true
			}
			if !d.at(q.step(dDown)).has(fSturdyUp) {
				gap++
				if gap > 2 {
					return true
				}
			} else {
				gap = 0
			}
			q = q.step(a)
		}
		var fallen posSet
		fallen.reset()
		axis := byte('x')
		if a == dNorth || a == dSouth {
			axis = 'z'
		}
		q = s
		for range l {
			d.set(q.x, q.y, q.z, logState(logName, axis))
			fallen.add(q)
			q = q.step(a)
		}
		logs := sortedByY(&fallen)
		for _, dec := range logDeco {
			dec(t, logs, nil, nil)
		}
		return true
	}
}

// blueIceFeature is section 11's blue ice.
func blueIceFeature(d *decoration, p pos) bool {
	if p.y > 62 {
		return false
	}
	if d.at(p) != sWater && d.at(p.step(dDown)) != sWater {
		return false
	}
	blue := stateOf("blue_ice")
	found := false
	for _, a := range [5]dir{dUp, dNorth, dSouth, dWest, dEast} {
		if d.at(p.step(a)) == sPacked {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	d.set(p.x, p.y, p.z, blue)
	r := &d.r
	for range 200 {
		yo := r.nextInt(5) - r.nextInt(6)
		xz := 3
		if yo < 2 {
			xz += yo / 2
		}
		if xz >= 1 {
			q := p.add(r.nextInt(xz)-r.nextInt(xz), yo, r.nextInt(xz)-r.nextInt(xz))
			s := d.at(q)
			if s.isAir() || s == sWater || s == sPacked || s == sIce {
				for _, a := range allDirs {
					if d.at(q.step(a)) == blue {
						d.set(q.x, q.y, q.z, blue)
						break
					}
				}
			}
		}
	}
	return true
}

// freezeFeature is section 12's freeze.
func freezeFeature(d *decoration, o pos) bool {
	snow := stateOf("snow[layers=1]")
	for dx := range 16 {
		for dz := range 16 {
			x, z := o.x+dx, o.z+dz
			y := d.height(hmMotionBlocking, x, z)
			top := pos{x, y, z}
			b := top.step(dDown)
			biome := d.biomeAt(x, y, z)
			if biomeTemperature(biome, b.x, b.y, b.z) < 0.15 {
				if s := d.at(b); s == sWater || (s.has(fLiquid) && s.has(fWater)) {
					d.set(b.x, b.y, b.z, sIce)
				}
			}
			if biomeTemperature(biome, x, y, z) < 0.15 {
				if s := d.at(top); s.isAir() && canSurvive(d, snow, top) {
					d.set(x, y, z, snow)
					if bb := d.at(b); strings.Contains(bb.name(), "snowy=false") {
						d.set(b.x, b.y, b.z, withProp(bb, "snowy", "true"))
					}
				}
			}
		}
	}
	return true
}

// Multiface growth (glow lichen). The spreader is inferred from the game's rule and tested on traces.

func lichenName(down, up, north, south, west, east, water bool) string {
	return fmt.Sprintf("glow_lichen[down=%v,east=%v,north=%v,south=%v,up=%v,waterlogged=%v,west=%v]", down, east, north, south, up, water, west)
}

func hasFace(s bs, a dir) bool {
	return strings.Contains(s.name(), dirName(a)+"=true")
}

func lichenWith(old bs, a dir) bs {
	f := [6]bool{}
	water := old.has(fWater) && old.has(fSource)
	if blockName[stateBlock[old]] == "glow_lichen" {
		for _, q := range allDirs {
			f[q] = hasFace(old, q)
		}
	}
	f[a] = true
	return stateOf(lichenName(f[dDown], f[dUp], f[dNorth], f[dSouth], f[dWest], f[dEast], water))
}

func sturdyFace(s bs, face dir) bool {
	switch face {
	case dUp:
		return s.has(fSturdyUp)
	case dDown:
		return s.has(fSturdyDown)
	}
	return s.has(fSturdySide)
}

func shuffledDirs(r *featRand, list []dir) []dir {
	out := append([]dir{}, list...)
	for i := len(out); i > 1; i-- {
		j := r.nextInt(i)
		out[i-1], out[j] = out[j], out[i-1]
	}
	return out
}

func (a dir) axis() int { return [6]int{1, 1, 2, 2, 0, 0}[a] }

// lichenSpread is the spreader run once from a placed lichen's face.
func lichenSpread(d *decoration, p pos, face dir) {
	r := &d.r
	all := allDirs[:]
	canInto := func(q pos, face dir) bool {
		st := d.at(q)
		isL := blockName[stateBlock[st]] == "glow_lichen"
		if !st.isAir() && !isL && !(st.has(fWater) && st.has(fSource) && st.has(fLiquid)) {
			return false
		}
		if isL && hasFace(st, face) {
			return false
		}
		return sturdyFace(d.at(q.step(face)), face.opposite())
	}
	for _, sd := range shuffledDirs(r, all) {
		if face.axis() == sd.axis() {
			continue
		}
		type try struct {
			q pos
			f dir
		}
		for _, t := range [3]try{{p, sd}, {p.step(sd), face}, {p.step(sd).step(face), sd.opposite()}} {
			if canInto(t.q, t.f) {
				d.set(t.q.x, t.q.y, t.q.z, lichenWith(d.at(t.q), t.f))
				return
			}
		}
	}
}

// lichenFeature is multiface_growth for glow lichen.
func lichenFeature(on map[uint16]bool, search int, chance float32) feature {
	placeable := []dir{dUp, dNorth, dEast, dSouth, dWest}
	return func(d *decoration, o pos) bool {
		r := &d.r
		start := d.at(o)
		if !start.isAir() && !(start.has(fWater) && start.has(fLiquid) && start.has(fSource)) {
			return false
		}
		put := func(q pos, old bs, dirs []dir) bool {
			for _, a := range dirs {
				if on[stateBlock[d.at(q.step(a))]] {
					st := lichenWith(old, a)
					if blockName[stateBlock[old]] == "glow_lichen" && hasFace(old, a) {
						return false
					}
					d.set(q.x, q.y, q.z, st)
					if r.nextFloat() < chance {
						lichenSpread(d, q, a)
					}
					return true
				}
			}
			return false
		}
		dirs := shuffledDirs(r, placeable)
		if put(o, start, dirs) {
			return true
		}
		for _, sd := range dirs {
			q := o
			var pd []dir
			for _, x := range placeable {
				if x != sd.opposite() {
					pd = append(pd, x)
				}
			}
			pd = shuffledDirs(r, pd)
			for range search {
				q = o.step(sd)
				b := d.at(q)
				if !b.isAir() && !(b.has(fWater) && b.has(fLiquid)) && blockName[stateBlock[b]] != "glow_lichen" {
					break
				}
				if put(q, b, pd) {
					return true
				}
			}
		}
		return false
	}
}
