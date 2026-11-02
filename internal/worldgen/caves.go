package worldgen

import "math"

// Noise caves are part of the density, not cut afterwards. The build below comes from the
// study's notes (section 7); the octave lists, sample scales, rarity tables and thickness
// ranges from its addendum (A2, A3). The code is our own.
//
//	underground = max(min(cheese part, entrances, spaghetti2d + roughness), pillars above 0.03)
//
// Near the ground the density is min(sloped, 5 * entrances), which folds the ground into
// tunnel mouths rather than opening holes where a 2D noise is high.
type caveNoises struct {
	cheese, layer, entrance                     *layered
	rarity3d, spagA, spagB, thick3d             *layered
	rarity2d, spag2d, elevation2d, thick2d      *layered
	roughMod, rough                             *layered
	pillar, pillarRare, pillarThick             *layered
	noodleToggle, noodleThick, noodleA, noodleB *layered
	veinToggle, veinA, veinB, veinGap           *layered
	flood, spread, lavaCell, barrier            *layered
}

func newCaveNoises(seed int64) caveNoises {
	sub := func(id uint64) uint64 { return mix64(uint64(seed) ^ mix64(id)) }
	one := []float64{1}
	return caveNoises{
		cheese:      newGameLayered(sub(50), -8, []float64{0.5, 1, 2, 1, 2, 1, 0, 2, 0}),
		layer:       newGameLayered(sub(51), -8, one),
		entrance:    newGameLayered(sub(52), -7, []float64{0.4, 0.5, 1}),
		rarity3d:    newGameLayered(sub(53), -11, one),
		spagA:       newLayered(sub(54), -7, one, gameSpread(one)*tunnelNoiseScale),
		spagB:       newLayered(sub(55), -7, one, gameSpread(one)*tunnelNoiseScale),
		thick3d:     newGameLayered(sub(56), -8, one),
		rarity2d:    newGameLayered(sub(57), -11, one),
		spag2d:      newLayered(sub(58), -7, one, gameSpread(one)*tunnelNoiseScale),
		elevation2d: newGameLayered(sub(59), -8, one),
		thick2d:     newGameLayered(sub(60), -11, one),
		roughMod:    newGameLayered(sub(61), -8, one),
		rough:       newGameLayered(sub(62), -5, one),
		pillar:      newGameLayered(sub(63), -7, []float64{1, 1}),
		pillarRare:  newGameLayered(sub(64), -8, one),
		pillarThick: newGameLayered(sub(65), -8, one),
		// Fluid and vein octaves: addendum A3.
		noodleToggle: newGameLayered(sub(66), -8, one),
		noodleThick:  newGameLayered(sub(67), -8, one),
		noodleA:      newGameLayered(sub(68), -7, one),
		noodleB:      newGameLayered(sub(69), -7, one),
		veinToggle:   newGameLayered(sub(70), -8, one),
		veinA:        newGameLayered(sub(71), -7, one),
		veinB:        newGameLayered(sub(72), -7, one),
		veinGap:      newGameLayered(sub(73), -5, one),
		flood:        newGameLayered(sub(74), -7, one),
		spread:       newGameLayered(sub(75), -5, one),
		lavaCell:     newGameLayered(sub(76), -1, one),
		barrier:      newGameLayered(sub(77), -3, one),
	}
}

// Cave limits and scales from the study.
const (
	undergroundFrom = 1.5625 // Sloped density from which the full underground function applies.
	noodleMinY      = -60
	noodleOff       = 64.0 // Noodle value where the toggle says there is none.
	noodleScale     = 2.6666667
	cheeseYScale    = 2.0 / 3
	layerYScale     = 8.0
	pillarXZScale   = 25.0
	pillarYScale    = 0.3
)

// tunnelNoiseScale widens the spread of the tunnel noises, which narrows tunnels. The study
// gives their octaves and thickness but no spread, so this is measured and tuned by us against
// the air shares (TestCaveSharesMatchMeasuredFigures), on top of gameSpread.
const tunnelNoiseScale = 1.16

// noodleGate: noodle and vein parts are worked out only where the density is above this, to
// save time; further into open air the blend cannot turn solid. Our own choice.
const noodleGate = -0.4

// mapUnit maps a noise value from -1..1 onto low..high, clamped.
func mapUnit(n, low, high float64) float64 {
	return lerp(low, high, clamp((n+1)/2, 0, 1))
}

// spaghettiRoughness is (modulator 0 to -0.1) * (|noise| - 0.4).
func (g *Generator) spaghettiRoughness(x, y, z float64) float64 {
	mod := mapUnit(g.caves.roughMod.at(x, y, z), 0, -0.1)
	return mod * (math.Abs(g.caves.rough.at(x, y, z)) - 0.4)
}

// spaghetti3d is max(a, b) + thickness: tunnels where two noises are both near zero. A rarity
// noise picks a stretch, and the tunnel noises are read at coordinates divided by it, so a
// bigger stretch is fatter and sparser. Thresholds, stretches and thickness: addendum A2.
func (g *Generator) spaghetti3d(x, y, z float64) float64 {
	r := 2.0
	switch n := g.caves.rarity3d.at(x*2, y, z*2); {
	case n < -0.5:
		r = 0.75
	case n < 0:
		r = 1
	case n < 0.5:
		r = 1.5
	}
	a := math.Abs(g.caves.spagA.at(x/r, y/r, z/r))
	b := math.Abs(g.caves.spagB.at(x/r, y/r, z/r))
	thick := mapUnit(g.caves.thick3d.at(x, y, z), -0.065, -0.088)
	return clamp(math.Max(a, b)+thick, -1, 1)
}

// spaghetti2d makes flat winding tunnels around an elevation that wanders across the world.
// Rarity table, thicknesses, elevation, gradient and the 0.083 weight: addendum A2.
func (g *Generator) spaghetti2d(x, y, z float64) float64 {
	r := 3.0
	switch n := g.caves.rarity2d.at(x*2, y, z*2); {
	case n < -0.75:
		r = 0.5
	case n < -0.5:
		r = 0.75
	case n < 0.5:
		r = 1
	case n < 0.75:
		r = 2
	}
	rare := math.Abs(g.caves.spag2d.at(x/r, y/r, z/r))
	thick := mapUnit(g.caves.thick2d.at(x*2, y, z*2), -0.6, -1.3)
	elevation := mapUnit(g.caves.elevation2d.at(x, 0, z), -8, 8)
	gradient := lerp(8, -40, (y-minY)/(maxY+1-minY))
	band := math.Abs(elevation+gradient) + thick
	return clamp(math.Max(rare+0.083*thick, band*band*band), -1, 1)
}

// entrances is min(cave entrance + 0.37 + gradient, roughness + spaghetti3d).
func (g *Generator) entrances(x, y, z, roughness float64) float64 {
	n := g.caves.entrance.at(x*0.75, y*0.5, z*0.75)
	gradient := 0.3 * clamp((30-y)/40, 0, 1)
	return math.Min(n+0.37+gradient, roughness+g.spaghetti3d(x, y, z))
}

// pillarsAt gives the thin columns inside large caverns, kept only above 0.03.
func (g *Generator) pillarsAt(x, y, z float64) float64 {
	thick := mapUnit(g.caves.pillarThick.at(x, y, z), 0, 1.1)
	rare := mapUnit(g.caves.pillarRare.at(x, y, z), 0, -2)
	n := g.caves.pillar.at(x*pillarXZScale, y*pillarYScale, z*pillarXZScale)
	v := (2*n + rare) * thick * thick * thick
	if v > 0.03 {
		return v
	}
	return math.Inf(-1)
}

// underground is the full cave function for a point well inside the ground.
func (g *Generator) underground(sloped, x, y, z float64) float64 {
	l := g.caves.layer.at(x, y*layerYScale, z)
	cheese := 4*l*l + clamp(0.27+g.caves.cheese.at(x, y*cheeseYScale, z), -1, 1) + clamp(1.5-0.64*sloped, 0, 0.5)
	rough := g.spaghettiRoughness(x, y, z)
	v := min(cheese, g.entrances(x, y, z, rough), g.spaghetti2d(x, y, z)+rough)
	return math.Max(v, g.pillarsAt(x, y, z))
}

// node holds what the block loop blends at one grid point.
type node struct {
	density             float64
	toggle, a, b, thick float64 // Noodle cave parts; toggle at or under 0 means none here.
	vein                float64 // Ore vein toggle, in the bands where veins can be.
}

// nodeAt works out a grid point.
func (g *Generator) nodeAt(s *columnShape, xi, yi, zi int) node {
	x, y, z := float64(xi), float64(yi), float64(zi)
	sloped := g.slopedDensity(s, x, y, z)
	v := sloped
	switch {
	case sloped <= 0:
	case sloped < undergroundFrom:
		v = math.Min(sloped, 5*g.entrances(x, y, z, g.spaghettiRoughness(x, y, z)))
	default:
		v = g.underground(sloped, x, y, z)
	}
	n := node{density: finishDensity(v, y), thick: noodleOff}
	if n.density > noodleGate {
		if yi >= noodleMinY {
			// The toggle noise below 0 turns noodles on (addendum A2).
			if t := -g.caves.noodleToggle.at(x, y, z); t > 0 {
				n.toggle = t
				n.thick = mapUnit(g.caves.noodleThick.at(x, y, z), -0.05, -0.1)
				n.a = g.caves.noodleA.at(x*noodleScale, y*noodleScale, z*noodleScale)
				n.b = g.caves.noodleB.at(x*noodleScale, y*noodleScale, z*noodleScale)
			}
		}
		if yi >= veinIronBottom-cellY && yi <= veinCopperTop+cellY {
			n.vein = g.caves.veinToggle.at(x*1.5, y*1.5, z*1.5)
		}
	}
	return n
}

// layerBlend is a column's values at the bottom and top of one grid cell, blended on x and
// z, so that a value at any height in the cell takes one more lerp.
type layerBlend struct {
	d0, d1   float64
	noodle   bool
	t0, t1   float64
	a0, a1   float64
	b0, b1   float64
	th0, th1 float64
	v0, v1   float64
}

// cellNodes holds the corners of a cell: [x][z], lower then upper.
type cellNodes struct {
	lo, hi [2][2]*node
}

func bilerp(a, b, c, d, tx, tz float64) float64 {
	return lerp(lerp(a, b, tx), lerp(c, d, tx), tz)
}

// blend fills a layerBlend for a block column inside the cell.
func (c *cellNodes) blend(tx, tz float64) layerBlend {
	lo, hi := &c.lo, &c.hi
	l := layerBlend{
		d0: bilerp(lo[0][0].density, lo[1][0].density, lo[0][1].density, lo[1][1].density, tx, tz),
		d1: bilerp(hi[0][0].density, hi[1][0].density, hi[0][1].density, hi[1][1].density, tx, tz),
		v0: bilerp(lo[0][0].vein, lo[1][0].vein, lo[0][1].vein, lo[1][1].vein, tx, tz),
		v1: bilerp(hi[0][0].vein, hi[1][0].vein, hi[0][1].vein, hi[1][1].vein, tx, tz),
	}
	for i := range 2 {
		for j := range 2 {
			if lo[i][j].toggle > 0 || hi[i][j].toggle > 0 {
				l.noodle = true
			}
		}
	}
	if l.noodle {
		l.t0 = bilerp(lo[0][0].toggle, lo[1][0].toggle, lo[0][1].toggle, lo[1][1].toggle, tx, tz)
		l.t1 = bilerp(hi[0][0].toggle, hi[1][0].toggle, hi[0][1].toggle, hi[1][1].toggle, tx, tz)
		l.a0 = bilerp(lo[0][0].a, lo[1][0].a, lo[0][1].a, lo[1][1].a, tx, tz)
		l.a1 = bilerp(hi[0][0].a, hi[1][0].a, hi[0][1].a, hi[1][1].a, tx, tz)
		l.b0 = bilerp(lo[0][0].b, lo[1][0].b, lo[0][1].b, lo[1][1].b, tx, tz)
		l.b1 = bilerp(hi[0][0].b, hi[1][0].b, hi[0][1].b, hi[1][1].b, tx, tz)
		l.th0 = bilerp(lo[0][0].thick, lo[1][0].thick, lo[0][1].thick, lo[1][1].thick, tx, tz)
		l.th1 = bilerp(hi[0][0].thick, hi[1][0].thick, hi[0][1].thick, hi[1][1].thick, tx, tz)
	}
	return l
}

// solidAt says whether the density and noodle caves leave a block solid at ty in the cell.
func (l *layerBlend) solidAt(ty float64) bool {
	if lerp(l.d0, l.d1, ty) <= 0 {
		return false
	}
	if !l.noodle || lerp(l.t0, l.t1, ty) <= 0 {
		return true
	}
	return lerp(l.th0, l.th1, ty)+1.5*math.Max(math.Abs(lerp(l.a0, l.a1, ty)), math.Abs(lerp(l.b0, l.b1, ty))) >= 0
}

// Block kinds the noise pass produces.
type fillKind uint8

const (
	fillAir fillKind = iota
	fillWater
	fillLava
	fillSolid
)

// surfaceFluidMargin: blocks within this many of the nominal ground, or above it, take the
// global fluid without asking the aquifer. Own choice: rivers and shallows are too thin for
// the aquifer's coarse surface sampling to see.
const surfaceFluidMargin = 4

// openSkyFluidDepth: a column with nothing solid above takes the global fluid down to this
// far under the nominal ground, so a dip in the land is never dry below sea level. Own.
const openSkyFluidDepth = 16

// fillAt says what a block that is not solid in the density becomes, by the aquifer's
// rules. ground is the column's nominal ground Y, open that nothing solid lies above.
func (g *Generator) fillAt(a *aquiferCache, x, y, z int, ground float64, open bool) fillKind {
	if y < lavaLevel {
		return fillLava // Open space below Y -54 is lava: the study measured no air there.
	}
	if fy := float64(y); fy > ground-surfaceFluidMargin || (open && fy > ground-openSkyFluidDepth) {
		return globalFluid(y).kindAt(y)
	}
	return g.aquiferAt(a, x, y, z)
}

// Ore vein bands, from the study.
const (
	veinCopperBottom = 0
	veinCopperTop    = 50
	veinIronBottom   = -60
	veinIronTop      = -8
)

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
	if t < 0.4+0.2*clamp(1-margin/20, 0, 1) {
		return 0
	}
	if unit(posHash(g.veinSeed^1, x, y, z)) >= 0.7 {
		return 0
	}
	fx, fy, fz := float64(x), float64(y), float64(z)
	a := g.caves.veinA.at(fx*4, fy*4, fz*4)
	b := g.caves.veinB.at(fx*4, fy*4, fz*4)
	if math.Max(math.Abs(a), math.Abs(b)) >= 0.08 {
		return 0
	}
	ids := &g.blocks
	rich := lerp(0.1, 0.3, clamp((t-0.4)/0.2, 0, 1))
	if unit(posHash(g.veinSeed^2, x, y, z)) < rich && g.caves.veinGap.at(fx, fy, fz) > -0.3 {
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

// columnWalk works out one column from the top down without a chunk, by the same rules as
// the chunk pass. Grid points are worked out only when reached.
type columnWalk struct {
	g            *Generator
	x, z         int
	gridX, gridZ int
	tx, tz       float64
	shapes       [2][2]columnShape
	ground       float64
	kind         surfaceKind
	ext          columnExtension
	puddle       bool
	nodes        [2][2][columnNodes]node
	have         [columnNodes]bool
	aq           aquiferCache
}

func (g *Generator) newColumnWalk(x, z int) *columnWalk {
	w := &columnWalk{g: g, x: x, z: z, gridX: x &^ (cellXZ - 1), gridZ: z &^ (cellXZ - 1)}
	w.tx, w.tz = float64(x-w.gridX)/cellXZ, float64(z-w.gridZ)/cellXZ
	for i := range 2 {
		for j := range 2 {
			w.shapes[i][j] = g.shapeAt(w.gridX+i*cellXZ, w.gridZ+j*cellXZ)
		}
	}
	w.ground = blendGround(&w.shapes[0][0], &w.shapes[1][0], &w.shapes[0][1], &w.shapes[1][1], w.tx, w.tz)
	col := g.biomeColumnAt(cellCentre(x), cellCentre(z))
	w.kind = col.surface.surface
	w.ext = g.extensionAt(w.kind, x, z)
	w.puddle = g.puddled(w.kind, x, z)
	return w
}

// layerBlendAt works out the grid points of a cell layer if needed, and blends them.
func (w *columnWalk) layerBlendAt(layer int) layerBlend {
	for _, n := range [2]int{layer, layer + 1} {
		if w.have[n] {
			continue
		}
		w.have[n] = true
		for i := range 2 {
			for j := range 2 {
				w.nodes[i][j][n] = w.g.nodeAt(&w.shapes[i][j], w.gridX+i*cellXZ, minY+n*cellY, w.gridZ+j*cellXZ)
			}
		}
	}
	var c cellNodes
	for i := range 2 {
		for j := range 2 {
			c.lo[i][j], c.hi[i][j] = &w.nodes[i][j][layer], &w.nodes[i][j][layer+1]
		}
	}
	return c.blend(w.tx, w.tz)
}

// layerSkyFast says whether a whole cell layer is above sea level and too far above every
// corner's ground for the 3D noise to reach, so it is air with no noise at all.
func (w *columnWalk) layerSkyFast(layer int) bool {
	yLow := float64(minY + layer*cellY)
	if yLow <= SeaLevel {
		return false
	}
	for i := range 2 {
		for j := range 2 {
			s := &w.shapes[i][j]
			if s.ramp(yLow) >= -maxBase {
				return false
			}
		}
	}
	return true
}

// top finds the highest block that is not air or water, by the order of the chunk pass:
// noise, sealing of hanging water, extensions, puddles, carvers, lakes. carved may be nil.
func (w *columnWalk) top(carved *carveMask, lakes []lake) int {
	g := w.g
	noiseSolid := false // A solid noise block has been met above.
	prevWater := false  // The block above is water in the noise pass.
	aboveWater := false // The block above is water in the final column.
	// final decides a block's final kind from what the noise pass left there.
	final := func(y int, k fillKind) fillKind {
		if !noiseSolid && y <= w.ext.top && (!w.ext.iceberg || y >= w.ext.bottom) && k != fillSolid {
			k = fillSolid // An extension block; it is not carved.
		} else if k == fillSolid {
			first := !noiseSolid
			noiseSolid = true
			if first && y == SeaLevel && w.puddle && w.ext.top < minY {
				return fillWater
			}
			if carved != nil && carved.has(y) && y > minY {
				f := g.carveFill(&w.aq, w.x, y, w.z, w.ground)
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
		if w.layerSkyFast(layer) && w.ext.top < minY+layer*cellY {
			continue
		}
		blend := w.layerBlendAt(layer)
		for dy := cellY - 1; dy >= 0; dy-- {
			y := minY + layer*cellY + dy
			kind := fillSolid
			if !blend.solidAt(float64(dy) / cellY) {
				kind = g.fillAt(&w.aq, w.x, y, w.z, w.ground, !noiseSolid)
			}
			if kind == fillAir && prevWater {
				// The water above is sealed with stone, as in the chunk pass.
				if f := final(y+1, fillSolid); f != fillAir && f != fillWater {
					return y + 1
				}
			}
			prevWater = kind == fillWater
			f := final(y, kind)
			if f != fillAir && f != fillWater {
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
	return g.newColumnWalk(x, z).top(nil, nil)
}

// NoiseHeight is noiseHeight for tools that draw maps.
func (g *Generator) NoiseHeight(x, z int) int { return g.noiseHeight(x, z) }

// carveMask marks the blocks of one column that tunnels pass through.
type carveMask [(maxY - minY + 1) / 64]uint64

func (m *carveMask) has(y int) bool {
	i := y - minY
	return m[i>>6]&(1<<(i&63)) != 0
}
