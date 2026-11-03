package worldgen

import "math"

// Noise caves are part of the density. Everything from the sloped ground to the final value follows
// Mojang's algorithm exactly, per the owner's decision, in our own Go from the study's notes
// (NOTES-exact-seed.md section 8). The copper and iron veins are not from those notes: they are our
// own, on the game's vein noises, and they only choose which solid block appears.
type caveNoises struct {
	base3D                                      *blendedNoise
	jagged                                      *normalNoise
	cheese, layer, entrance                     *normalNoise
	rarity3d, spagA, spagB, thick3d             *normalNoise
	rarity2d, spag2d, elevation2d, thick2d      *normalNoise
	roughMod, rough                             *normalNoise
	pillar, pillarRare, pillarThick             *normalNoise
	noodleToggle, noodleThick, noodleA, noodleB *normalNoise
	veinToggle, veinA, veinB, veinGap           *normalNoise
	flood, spread, lavaCell, barrier            *normalNoise
}

func newCaveNoises(w seedFactory) caveNoises {
	terrain := w.named("minecraft:terrain")
	n := func(name string) *normalNoise { return newNamedNoise(w, name) }
	return caveNoises{
		base3D:       newBlendedNoise(&terrain),
		jagged:       n("jagged"),
		cheese:       n("cave_cheese"),
		layer:        n("cave_layer"),
		entrance:     n("cave_entrance"),
		rarity3d:     n("spaghetti_3d_rarity"),
		spagA:        n("spaghetti_3d_1"),
		spagB:        n("spaghetti_3d_2"),
		thick3d:      n("spaghetti_3d_thickness"),
		rarity2d:     n("spaghetti_2d_modulator"),
		spag2d:       n("spaghetti_2d"),
		elevation2d:  n("spaghetti_2d_elevation"),
		thick2d:      n("spaghetti_2d_thickness"),
		roughMod:     n("spaghetti_roughness_modulator"),
		rough:        n("spaghetti_roughness"),
		pillar:       n("pillar"),
		pillarRare:   n("pillar_rareness"),
		pillarThick:  n("pillar_thickness"),
		noodleToggle: n("noodle"),
		noodleThick:  n("noodle_thickness"),
		noodleA:      n("noodle_ridge_a"),
		noodleB:      n("noodle_ridge_b"),
		veinToggle:   n("ore_veininess"),
		veinA:        n("ore_vein_a"),
		veinB:        n("ore_vein_b"),
		veinGap:      n("ore_gap"),
		flood:        n("aquifer_fluid_level_floodedness"),
		spread:       n("aquifer_fluid_level_spread"),
		lavaCell:     n("aquifer_lava"),
		barrier:      n("aquifer_barrier"),
	}
}

// mapped puts a noise value onto a range, in the game's form.
func mapped(n, a, b float64) float64 {
	centre := float64((a + b) * 0.5)
	half := float64((b - a) * 0.5)
	return centre + float64(half*n)
}

// plain reads a noise at a block with its xz and y scales.
func plain(n *normalNoise, x, y, z int, sxz, sy float64) float64 {
	return n.at(float64(float64(x)*sxz), float64(float64(y)*sy), float64(float64(z)*sxz))
}

// scaledTunnel reads a tunnel noise stretched by a rarity.
func scaledTunnel(n *normalNoise, x, y, z int, r float64) float64 {
	return float64(r * math.Abs(n.at(float64(x)/r, float64(y)/r, float64(z)/r)))
}

func rarity3D(v float64) float64 {
	switch {
	case v < -0.5:
		return 0.75
	case v < 0:
		return 1.0
	case v < 0.5:
		return 1.5
	}
	return 2.0
}

func rarity2D(v float64) float64 {
	switch {
	case v < -0.75:
		return 0.5
	case v < -0.5:
		return 0.75
	case v < 0.5:
		return 1.0
	case v < 0.75:
		return 2.0
	}
	return 3.0
}

func (g *Generator) roughnessAt(x, y, z int) float64 {
	n := &g.caves
	return float64(mapped(plain(n.roughMod, x, y, z, 1, 1), 0, -0.1) * (math.Abs(plain(n.rough, x, y, z, 1, 1)) - 0.4))
}

func (g *Generator) entrancesAt(x, y, z int, roughness float64) float64 {
	n := &g.caves
	r := rarity3D(plain(n.rarity3d, x, y, z, 2, 1))
	a := scaledTunnel(n.spagA, x, y, z, r)
	b := scaledTunnel(n.spagB, x, y, z, r)
	sp := clamp(max(a, b)+mapped(plain(n.thick3d, x, y, z, 1, 1), -0.065, -0.088), -1, 1)
	mouth := (plain(n.entrance, x, y, z, 0.75, 0.5) + 0.37) + yGrad(y, -10, 30, 0.3, 0)
	return min(mouth, roughness+sp)
}

// thickness2D is the thickness of the flat tunnels.
func (g *Generator) thickness2D(x, y, z int) float64 {
	return mapped(plain(g.caves.thick2d, x, y, z, 2, 1), -0.6, -1.3)
}

func (g *Generator) spaghetti2DAt(x, y, z int) float64 {
	n := &g.caves
	th := g.thickness2D(x, y, z)
	r := rarity2D(plain(n.rarity2d, x, y, z, 2, 1))
	flat := scaledTunnel(n.spag2d, x, y, z, r) + float64(0.083*th)
	band := math.Abs(mapped(plain(n.elevation2d, x, y, z, 1, 0), -8, 8)+yGrad(y, -64, 320, 8, -40)) + th
	return clamp(max(flat, cubed(band)), -1, 1)
}

// pillarsAt is the raw pillar value; cutPillars drops it where it is too weak to make a pillar.
func (g *Generator) pillarsAt(x, y, z int) float64 {
	n := &g.caves
	pil := float64(float64(plain(n.pillar, x, y, z, 25, 0.3)*2.0)+mapped(plain(n.pillarRare, x, y, z, 1, 1), 0, -2)) *
		cubed(mapped(plain(n.pillarThick, x, y, z, 1, 1), 0, 1.1))
	return float64(pil)
}

func cutPillars(pil float64) float64 {
	if pil >= -1000000 && pil < 0.03 {
		return -1000000
	}
	return pil
}

// slopedKeepNoise: from this sloped value the caves read no sloped term at all, so where even the
// lowest possible value is above it the 3D noise can be left out with no change to the result. The
// exact limit is 2.34375; this keeps clear of rounding at it.
const slopedKeepNoise = 2.35

// coreAt is the density before squeeze and noodle caves: the function the game blends between cell
// corners. Parts that cannot change the result are not read.
func (g *Generator) coreAt(s *columnShape, x, y, z int) float64 {
	var caves float64
	switch {
	case y >= 256 || y <= -64:
		// A slide is at its end, so what it fades is multiplied by zero.
	default:
		caves = g.cavesAt(s, x, y, z)
	}
	return slides(caves, y)
}

func (g *Generator) cavesAt(s *columnShape, x, y, z int) float64 {
	ramp := s.ramp(y)
	var fill float64 // clamp(1.5 - 0.64*sloped, 0, 0.5)
	if ramp-base3DLimit < slopedKeepNoise {
		sloped := ramp + g.caves.base3D.at(x, y, z)
		if sloped < 1.5625 {
			return min(sloped, float64(5.0*g.entrancesAt(x, y, z, g.roughnessAt(x, y, z))))
		}
		fill = clamp(1.5+float64(-0.64*sloped), 0, 0.5)
	}
	n := &g.caves
	roughness := g.roughnessAt(x, y, z)
	ent := g.entrancesAt(x, y, z, roughness)
	l := plain(n.layer, x, y, z, 1, 8)
	layer := float64(4.0 * float64(l*l))
	base := layer + (clamp(0.27+plain(n.cheese, x, y, z, 1, 0.6666666666666666), -1, 1) + fill)
	under := min(min(base, ent), g.spaghetti2DAt(x, y, z)+roughness)
	return max(under, cutPillars(g.pillarsAt(x, y, z)))
}

// yFn is the block Y passed through the game's rounding.
func yFn(y int) float64 { return yGrad(y, -4064, 4062, -4064, 4062) }

func noodleInRange(y int) bool {
	v := yFn(y)
	return v >= -60 && v < 321
}

// noodleToggleAt is outside the band -1, where it says no noodle.
func (g *Generator) noodleToggleAt(x, y, z int) float64 {
	if !noodleInRange(y) {
		return -1
	}
	return plain(g.caves.noodleToggle, x, y, z, 1, 1)
}

func (g *Generator) noodleParts(x, y, z int) (thick, a, b float64) {
	if !noodleInRange(y) {
		return 0, 0, 0
	}
	const ridge = 2.6666666666666665
	n := &g.caves
	return mapped(plain(n.noodleThick, x, y, z, 1, 1), -0.05, -0.1), plain(n.noodleA, x, y, z, ridge, ridge), plain(n.noodleB, x, y, z, ridge, ridge)
}

func noodleValue(toggle, thick, a, b float64) float64 {
	if toggle >= -1000000 && toggle < 0 {
		return 64
	}
	return thick + float64(1.5*max(math.Abs(a), math.Abs(b)))
}

// node holds what the block loop blends at one grid point.
type node struct {
	core, toggle float64
	thick, a, b  float64
	vein         float64
	haveCore     bool
	haveNoodle   bool
	haveVein     bool
}

func (n *node) needCore(g *Generator, s *columnShape, x, y, z int) {
	if !n.haveCore {
		n.core, n.toggle, n.haveCore = g.coreAt(s, x, y, z), g.noodleToggleAt(x, y, z), true
	}
}

func (n *node) needNoodle(g *Generator, x, y, z int) {
	if !n.haveNoodle {
		n.thick, n.a, n.b = g.noodleParts(x, y, z)
		n.haveNoodle = true
	}
}

func (n *node) needVein(g *Generator, x, y, z int) {
	if !n.haveVein {
		n.vein, n.haveVein = plain(g.caves.veinToggle, x, y, z, 1.5, 1.5), true
	}
}

// edges holds one value of a cell blended on x, at the four edges that run along x. Blending x first,
// then y, then z is the game's order, and changing it would change the low bits.
type edges struct{ y0z0, y1z0, y0z1, y1z1 float64 }

func (e *edges) at(fy, fz float64) float64 {
	return lerp(lerp(e.y0z0, e.y1z0, fy), lerp(e.y0z1, e.y1z1, fy), fz)
}

// cellCorners points at a cell's corners: x, y, z.
type cellCorners [2][2][2]*node

func (c *cellCorners) blendX(fx float64, read func(*node) float64) edges {
	return edges{
		lerp(read(c[0][0][0]), read(c[1][0][0]), fx),
		lerp(read(c[0][1][0]), read(c[1][1][0]), fx),
		lerp(read(c[0][0][1]), read(c[1][0][1]), fx),
		lerp(read(c[0][1][1]), read(c[1][1][1]), fx),
	}
}

func readCore(n *node) float64   { return n.core }
func readToggle(n *node) float64 { return n.toggle }
func readThick(n *node) float64  { return n.thick }
func readA(n *node) float64      { return n.a }
func readB(n *node) float64      { return n.b }
func readVein(n *node) float64   { return n.vein }

// noodleLive says whether a cell can hold a noodle cave. If every corner's toggle is under zero then
// so is every blend of them.
func (c *cellCorners) noodleLive() bool {
	for i := range c {
		for j := range c[i] {
			for k := range c[i][j] {
				if !(c[i][j][k].toggle < 0) {
					return true
				}
			}
		}
	}
	return false
}

// columnBlend is a block column's blended values inside one cell.
type columnBlend struct {
	core, toggle, thick, a, b, vein edges
	noodle, veins                   bool
	fz                              float64
}

func (c *cellCorners) blend(fx, fz float64, noodle, veins bool) columnBlend {
	b := columnBlend{core: c.blendX(fx, readCore), noodle: noodle, veins: veins, fz: fz}
	if noodle {
		b.toggle, b.thick = c.blendX(fx, readToggle), c.blendX(fx, readThick)
		b.a, b.b = c.blendX(fx, readA), c.blendX(fx, readB)
	}
	if veins {
		b.vein = c.blendX(fx, readVein)
	}
	return b
}

// final is the density of a block: squeeze, min and the noodle choice come after blending.
func (b *columnBlend) final(fy float64) float64 {
	v := squeeze(float64(b.core.at(fy, b.fz) * 0.64))
	if b.noodle {
		v = min(v, noodleValue(b.toggle.at(fy, b.fz), b.thick.at(fy, b.fz), b.a.at(fy, b.fz), b.b.at(fy, b.fz)))
	} else {
		v = min(v, 64)
	}
	return v + 0.0
}

func (b *columnBlend) veinAt(fy float64) float64 {
	if !b.veins {
		return 0
	}
	return b.vein.at(fy, b.fz)
}

// Block kinds the noise pass produces.
type fillKind uint8

const (
	fillAir fillKind = iota
	fillWater
	fillLava
	fillSolid
)

// Ore vein bands, from the study.
const (
	veinCopperBottom = 0
	veinCopperTop    = 50
	veinIronBottom   = -60
	veinIronTop      = -8
	veinStrength     = 0.4 // A blended toggle under this makes no vein block.
)

// cellHasVeins says whether a cell at a layer's base Y can hold a vein.
func cellHasVeins(baseY int) bool {
	return baseY+cellY > veinIronBottom && baseY <= veinCopperTop
}

// veinBlockAt gives the block an ore vein makes of solid rock, or zero for none. Rules and
// numbers from the study: copper in granite, iron in tuff.
func (g *Generator) veinBlockAt(x, y, z int, toggle float64) uint32 {
	copperVein := toggle > 0
	lo, hi := veinIronBottom, veinIronTop
	if copperVein {
		lo, hi = veinCopperBottom, veinCopperTop
	}
	if y < lo || y > hi {
		return 0
	}
	t := math.Abs(toggle)
	margin := float64(min(y-lo, hi-y))
	if t < veinStrength+0.2*clamp(1-margin/20, 0, 1) {
		return 0
	}
	if unit(posHash(g.veinSeed^1, x, y, z)) >= 0.7 {
		return 0
	}
	a := plain(g.caves.veinA, x, y, z, 4, 4)
	b := plain(g.caves.veinB, x, y, z, 4, 4)
	if max(math.Abs(a), math.Abs(b)) >= 0.08 {
		return 0
	}
	ids := &g.blocks
	rich := lerp(0.1, 0.3, clamp((t-0.4)/0.2, 0, 1))
	if unit(posHash(g.veinSeed^2, x, y, z)) < rich && plain(g.caves.veinGap, x, y, z, 1, 1) > -0.3 {
		h := unit(posHash(g.veinSeed^3, x, y, z))
		switch {
		case !copperVein && h < 0.02:
			return ids.rawIron
		case !copperVein:
			return ids.veins[veinIron][1]
		case h < 0.02:
			return ids.rawCopper
		}
		if g.stoneAt(x, y, z) == ids.deepslate {
			return ids.veins[veinCopper][1]
		}
		return ids.veins[veinCopper][0]
	}
	if copperVein {
		return ids.granite
	}
	return ids.tuff
}

// solidBlockAt gives the block of a solid position in the noise pass.
func (g *Generator) solidBlockAt(x, y, z int, vein float64) uint32 {
	if y == minY || g.isBedrock(x, y, z) {
		return g.blocks.bedrock
	}
	if vein != 0 {
		if id := g.veinBlockAt(x, y, z, vein); id != 0 {
			return id
		}
	}
	return g.stoneAt(x, y, z)
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// columnWalk works out one column from the top down without a chunk, by the same rules as the chunk
// pass. Grid points are worked out only when reached. It lives in a chunk's scratch.
type columnWalk struct {
	g            *Generator
	x, z         int
	gridX, gridZ int
	fx, fz       float64
	corners      [2][2]*walkCorner
	aq           *aquiferCache
	kind         surfaceKind
	ext          columnExtension
	puddle       bool
	// topUnderWater says whether the top found by the last call of top has water directly above.
	topUnderWater bool
}

// walkCorner is one grid column a walk has read. Neighbouring walks share them, and an entry is a pure
// function of the generator and its position.
type walkCorner struct {
	owner *Generator
	x, z  int
	shape columnShape
	nodes [columnNodes]node
}

const walkCornerSlots = 1 << 5

// cornerAt finds or starts a grid column in the scratch's small direct-mapped store. A walk's four
// corners are neighbours, so they fall in four different slots.
func (s *chunkScratch) cornerAt(g *Generator, x, z int) *walkCorner {
	gx, gz := x>>2, z>>2
	k := &s.walkCorners[(gx&7)|(gz&3)<<3]
	if k.owner != g || k.x != x || k.z != z {
		k.owner, k.x, k.z = g, x, z
		k.shape = g.shapeAt(x, z)
		for i := range k.nodes {
			k.nodes[i].haveCore, k.nodes[i].haveNoodle, k.nodes[i].haveVein = false, false, false
		}
	}
	return k
}

// startWalk prepares s.walk for a column. Its aquifer cache follows the chunk the column is in.
func (g *Generator) startWalk(s *chunkScratch, x, z int) *columnWalk {
	if !s.walkAqReady || s.walkAq.chunkX != x>>4 || s.walkAq.chunkZ != z>>4 {
		s.walkAq.reset(x, z, &s.prelims)
		s.walkAqReady = true
	}
	w := &s.walk
	w.g, w.x, w.z = g, x, z
	w.gridX, w.gridZ = x&^(cellXZ-1), z&^(cellXZ-1)
	w.fx, w.fz = float64(x-w.gridX)/4.0, float64(z-w.gridZ)/4.0
	w.aq = &s.walkAq
	for i := range 2 {
		for j := range 2 {
			w.corners[i][j] = s.cornerAt(g, w.gridX+i*cellXZ, w.gridZ+j*cellXZ)
		}
	}
	col := g.biomeColumnAt(x, z)
	w.kind = col.surface.surface
	w.ext = g.extensionAt(w.kind, x, z)
	w.puddle = g.puddled(w.kind, x, z)
	return w
}

// layerBlendAt works out the grid points of a cell layer if needed, and blends them.
func (w *columnWalk) layerBlendAt(layer int) columnBlend {
	g := w.g
	var c cellCorners
	for i := range 2 {
		for j := range 2 {
			k := w.corners[i][j]
			for up := range 2 {
				n := &k.nodes[layer+up]
				n.needCore(g, &k.shape, w.gridX+i*cellXZ, minY+(layer+up)*cellY, w.gridZ+j*cellXZ)
				c[i][up][j] = n
			}
		}
	}
	noodle := c.noodleLive()
	if noodle {
		for i := range 2 {
			for k := range 2 {
				for j := range 2 {
					c[i][k][j].needNoodle(g, w.gridX+i*cellXZ, minY+(layer+k)*cellY, w.gridZ+j*cellXZ)
				}
			}
		}
	}
	return c.blend(w.fx, w.fz, noodle, false)
}

// layerCertainAir says whether no block of a whole cell layer can be solid or need its density,
// without reading any 3D noise: every corner is far above the ground and the layer is over the
// chunk's skip height.
func (w *columnWalk) layerCertainAir(layer int) bool {
	yLow := minY + layer*cellY
	if yLow <= w.g.skipAbove(w.aq) {
		return false
	}
	for i := range 2 {
		for j := range 2 {
			if s := &w.corners[i][j].shape; !s.certainlyAir(yLow) || !s.certainlyAir(yLow+cellY) {
				return false
			}
		}
	}
	return true
}

// rawKind is the noise stage's answer for a block: density and aquifer, nothing later.
func (w *columnWalk) rawKind(blend *columnBlend, y, dy int) fillKind {
	f := blend.final(float64(dy) / 8.0)
	if f > 0 {
		return fillSolid
	}
	return w.g.fluidAt(w.aq, w.x, y, w.z, f)
}

// rawTops are the highest solid block and the highest block that is not air, at the noise stage.
func (w *columnWalk) rawTops() (solid, nonAir int) {
	solid, nonAir = minY-1, minY-1
	for layer := columnNodes - 2; layer >= 0; layer-- {
		baseY := minY + layer*cellY
		if w.layerCertainAir(layer) {
			if nonAir < minY && baseY < seaFluidTop {
				nonAir = min(baseY+cellY-1, seaFluidTop-1)
			}
			continue
		}
		blend := w.layerBlendAt(layer)
		for dy := cellY - 1; dy >= 0; dy-- {
			y := baseY + dy
			k := w.rawKind(&blend, y, dy)
			if k != fillAir && nonAir < minY {
				nonAir = y
			}
			if k == fillSolid {
				return y, nonAir
			}
		}
	}
	return solid, nonAir
}

// top finds the highest block that is not air or water, by the order of the chunk pass:
// noise, sealing of hanging water, extensions, puddles, carvers, lakes. carved may be nil.
func (w *columnWalk) top(carved *carveMask, lakes []lake) int {
	g := w.g
	noiseSolid := false // A solid noise block has been met above.
	prevWater := false  // The block above is water in the noise pass.
	aboveWater := false // The block above is water in the final column.
	w.topUnderWater = false
	// final decides a block's final kind from what the noise pass left there.
	final := func(y int, k fillKind) fillKind {
		if !noiseSolid && y <= w.ext.top && (!w.ext.iceberg || y >= w.ext.bottom) && k != fillSolid {
			k = fillSolid // An extension block; it is not carved.
		} else if k == fillSolid {
			first := !noiseSolid
			noiseSolid = true
			if first && y == puddleY && w.puddle && w.ext.top < minY {
				return fillWater
			}
			if carved != nil && carved.has(y) && y > minY {
				f := g.carveFill(w.aq, w.x, y, w.z)
				if f != fillSolid && !(f == fillAir && aboveWater) {
					return f
				}
			}
		}
		if k == fillSolid {
			switch lakeAt(lakes, w.x, y, w.z) {
			case lakeAir:
				if !aboveWater {
					return fillAir
				}
			case lakeLava:
				return fillLava
			}
		}
		return k
	}
	for layer := columnNodes - 2; layer >= 0; layer-- {
		baseY := minY + layer*cellY
		if w.ext.top < baseY && baseY >= seaFluidTop && w.layerCertainAir(layer) {
			prevWater = false
			continue
		}
		var blend columnBlend
		certain := w.layerCertainAir(layer)
		if !certain {
			blend = w.layerBlendAt(layer)
		}
		for dy := cellY - 1; dy >= 0; dy-- {
			y := baseY + dy
			var kind fillKind
			if certain {
				kind = globalFluid(y).at(y)
			} else {
				kind = w.rawKind(&blend, y, dy)
			}
			if kind == fillAir && prevWater {
				// The water above is sealed with stone, as in the chunk pass.
				if f := final(y+1, fillSolid); f != fillAir && f != fillWater {
					w.topUnderWater = aboveWater
					return y + 1
				}
			}
			prevWater = kind == fillWater
			f := final(y, kind)
			if f != fillAir && f != fillWater {
				w.topUnderWater = aboveWater
				return y
			}
			aboveWater = f == fillWater
		}
	}
	return minY - 1
}

// noiseHeight is the top of the ground with noise caves but without carvers or lakes. It is
// what the study measured, and much cheaper than SurfaceHeight because no tunnels are walked.
func (g *Generator) noiseHeight(x, z int) int {
	s := scratchPool.Get().(*chunkScratch)
	defer scratchPool.Put(s)
	s.startColumnOnly(g)
	return g.startWalk(s, x, z).top(nil, nil)
}

// NoiseHeight is noiseHeight for tools that draw maps.
func (g *Generator) NoiseHeight(x, z int) int { return g.noiseHeight(x, z) }

// carveMask marks the blocks of one column that tunnels pass through.
type carveMask [(maxY - minY + 1) / 64]uint64

func (m *carveMask) has(y int) bool {
	i := y - minY
	return m[i>>6]&(1<<(i&63)) != 0
}
