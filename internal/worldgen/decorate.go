package worldgen

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Decoration: trees, then ice and snow, then plants. All of it runs after the terrain is final.
//
// Where the numbers come from. Tree rates per biome are Bedrock's, measured (BEDROCK-NOTES section 9,
// marked in treeRates), and so are kelp and seagrass per ocean chunk; the rates here are tuned
// until the tests measure them. Every tree shape is our own, made from the blocks Dragonfly offers,
// with no figure behind it. Plant rates, and the cold threshold and height drop, are our own too:
// the study did not measure them.

// treeKind is a tree shape and its wood.
type treeKind uint8

const (
	treeNone treeKind = iota
	treeOak
	treeBirch
	treeSpruce
	treePine
	treeDarkOak
	treeJungle
	treeAcacia
	treeCherry
	treePaleOak
	treeMangrove
	numTreeKinds
)

// treeRate is how many trees a chunk of a biome tries for, with the kinds it picks from.
type treeRate struct {
	rate  float64
	kinds []treeKind
}

// Attempts a chunk rolls. Every attempt uses the same draws whatever it finds, so a tree's place
// never depends on anything but its chunk's position.
const treeAttempts = 28

// treeRates by biome. A measured rate is Bedrock's figure scaled up for the attempts that fail on
// water and open ground, tuned by TestTreesMatchBedrock. "own" has no figure: Bedrock's windows
// did not hold those biomes, or held too little of them.
func treeRates(d def) treeRate {
	switch d {
	case jungle: // 10.03
		return treeRate{13.2, []treeKind{treeJungle, treeJungle, treeOak}}
	case darkForest: // 7.44
		return treeRate{11.2, []treeKind{treeDarkOak, treeDarkOak, treeDarkOak, treeOak}}
	case paleGarden: // 6.5
		return treeRate{7.6, []treeKind{treePaleOak}}
	case forest: // 5.92
		return treeRate{7.5, []treeKind{treeOak, treeOak, treeOak, treeBirch}}
	case taiga: // 5.18
		return treeRate{6.6, []treeKind{treeSpruce, treeSpruce, treePine}}
	case birchForest: // 5.04
		return treeRate{6.35, []treeKind{treeBirch}}
	case oldPine: // 4.72
		return treeRate{5.65, []treeKind{treePine, treeSpruce}}
	case grove: // 4.49
		return treeRate{4.8, []treeKind{treeSpruce}}
	case flowerForest: // 4.06
		return treeRate{5.15, []treeKind{treeOak, treeBirch}}
	case oldBirch: // 3.64
		return treeRate{4.75, []treeKind{treeBirch}}
	case woodedBadlands: // 2.57
		return treeRate{7.7, []treeKind{treeOak}}
	case cherryGrove: // 2.41
		return treeRate{2.4, []treeKind{treeCherry}}
	case oldSpruce: // 1.89
		return treeRate{2.4, []treeKind{treeSpruce}}
	case sparseJungle: // 1.43
		return treeRate{1.8, []treeKind{treeJungle, treeOak}}
	case savannaPlateau: // 1.1
		return treeRate{1.1, []treeKind{treeAcacia, treeAcacia, treeAcacia, treeOak}}
	case savanna: // 1.02
		return treeRate{1.23, []treeKind{treeAcacia, treeAcacia, treeAcacia, treeOak}}
	case meadow: // 0.95
		return treeRate{0.92, []treeKind{treeOak, treeBirch}}
	case plains: // 0.28 to 0.39 by seed.
		return treeRate{0.43, []treeKind{treeOak}}
	case sunflower: // 0.02 to 0.14 by seed.
		return treeRate{0.1, []treeKind{treeOak}}
	case snowyTaiga: // Own, like taiga.
		return treeRate{6.6, []treeKind{treeSpruce, treeSpruce, treePine}}
	case windsweptForest: // Own.
		return treeRate{4, []treeKind{treeSpruce, treeOak}}
	case bambooJungle: // Own; Dragonfly has bamboo, this generator does not place it yet.
		return treeRate{5, []treeKind{treeJungle}}
	case swamp: // Own.
		return treeRate{2, []treeKind{treeOak}}
	case mangrove: // Own.
		return treeRate{4, []treeKind{treeMangrove}}
	case windSavanna: // Own.
		return treeRate{0.5, []treeKind{treeAcacia}}
	case windsweptHills: // Own.
		return treeRate{0.3, []treeKind{treeSpruce, treeOak}}
	case snowyPlains: // Own.
		return treeRate{0.15, []treeKind{treeSpruce}}
	}
	return treeRate{}
}

// treeReach is the furthest a tree's blocks reach, on either axis, from its first trunk column. A
// dark oak's 2 x 2 trunk adds one more on the far side, so treeMargin is one over. Tunnels are cut
// for that many columns around a chunk (cutCarvers).
const (
	treeReach  = 5
	treeMargin = treeReach + 1
	carvedSize = 16 + 2*treeMargin
)

// decorator is the working state of one chunk's decoration.
type decorator struct {
	g           *Generator
	c           *chunk.Chunk
	s           *chunkScratch
	originX, oz int
	top         *[16][16]int16 // Terrain top before carvers and lakes.
	surfaces    [4][4]def      // This chunk's surface biomes.
	ground      groundSource   // The terrain outside this chunk.
	final       [16][16]int16  // Terrain top with everything.
	cover       [16][16]int16  // Highest block of any kind but water.
}

// groundSource is all decoration asks about terrain outside the chunk being generated, by column.
// top is what SurfaceHeight gives. untouched says carvers and lakes left the noise pass's top in
// place, and dry says no water lies directly on it. With the biome lookup in surfaceDefAt, this is
// the whole of decoration's tie to the noise, climate, terrain and cave layers, so they can be
// replaced under it.
type groundSource interface {
	ground(x, z int) (top int, untouched, dry bool)
}

// chunkGround answers for columns within treeMargin of a chunk, using the tunnels already cut for
// them in the chunk's scratch (cutCarvers), so no tunnel is walked per column as SurfaceHeight does.
type chunkGround struct {
	g         *Generator
	s         *chunkScratch
	originX   int
	originZ   int
	lakes     [3][3][]lake
	haveLakes [3][3]bool
}

func (c *chunkGround) ground(x, z int) (top int, untouched, dry bool) {
	w := c.g.newColumnWalk(x, z)
	h := w.top(nil, nil)
	dry = !w.topUnderWater
	mask := &c.s.carved[x-c.originX+treeMargin][z-c.originZ+treeMargin]
	lakes := c.lakesFor(x, z)
	if !mask.any() && len(lakes) == 0 {
		return h, true, dry
	}
	f := w.top(mask, lakes)
	return f, f == h, dry && !w.topUnderWater
}

// lakesFor lists the lakes that can reach a column, once per chunk asked about.
func (c *chunkGround) lakesFor(x, z int) []lake {
	sx, sz := x>>4, z>>4
	i, j := sx-c.originX>>4+1, sz-c.originZ>>4+1
	if !c.haveLakes[i][j] {
		c.haveLakes[i][j] = true
		c.lakes[i][j] = c.g.lakesNear(sx, sz, nil)
	}
	return c.lakes[i][j]
}

// allDefs lists every surface biome the tables can give.
func allDefs() []def {
	list := []def{plains, sunflower, forest, flowerForest, birchForest, oldBirch, darkForest, paleGarden, taiga, oldSpruce, oldPine,
		snowyTaiga, snowyPlains, iceSpikes, desert, savanna, savannaPlateau, windSavanna, jungle, sparseJungle, bambooJungle, swamp,
		mangrove, badlands, erodedBadlands, woodedBadlands, meadow, cherryGrove, grove, snowySlopes, jaggedPeaks, frozenPeaks, stonyPeaks,
		windsweptHills, windsweptForest, gravellyHills, beach, snowyBeach, stonyShore, river, frozenRiver, mushroom}
	list = append(list, oceans[:]...)
	return append(list, deepOceans[:]...)
}

var defsByBiome = func() map[int]def {
	m := map[int]def{}
	for _, d := range allDefs() {
		m[d.EncodeBiome()] = d
	}
	return m
}()

// surfaceDefAt is a column's surface biome, through BiomeAt alone. A biome with no entry gets no
// surface rule, and so no trees.
func (g *Generator) surfaceDefAt(x, z int) def {
	b := g.BiomeAt(x, maxY, z)
	if d, ok := defsByBiome[b.EncodeBiome()]; ok {
		return d
	}
	return def{b, surfNone}
}

// decorate places trees, ice, snow and plants. A tree is rolled from its origin chunk and written
// where it falls in this chunk, so each chunk works out its own and its neighbours' trees from the
// seed. Nothing here reads a neighbouring chunk or lasts past the call.
func (g *Generator) decorate(c *chunk.Chunk, s *chunkScratch, originX, originZ int, top *[16][16]int16, cols *[4][4]biomeColumn) {
	d := decorator{g: g, c: c, s: s, originX: originX, oz: originZ, top: top,
		ground: &chunkGround{g: g, s: s, originX: originX, originZ: originZ}}
	for i := range cols {
		for j := range cols[i] {
			d.surfaces[i][j] = cols[i][j].surface
		}
	}
	air, water := g.blocks.air, g.blocks.water
	for x := range 16 {
		for z := range 16 {
			y := int(top[x][z])
			for y >= minY {
				if id := c.Block(uint8(x), int16(y), uint8(z), 0); id != air && id != water {
					break
				}
				y--
			}
			d.final[x][z], d.cover[x][z] = int16(y), int16(y)
		}
	}
	d.placeTrees()
	d.freeze()
	d.placePlants()
}

// placeTrees rolls the trees of this chunk and its neighbours. Rolls come first and are cheap; a
// biome lookup is made only for a tree that can reach this chunk, and a column's height only after
// the biome says a tree can grow there.
func (d *decorator) placeTrees() {
	g := d.g
	cx, cz := d.originX>>4, d.oz>>4
	for sx := cx - 1; sx <= cx+1; sx++ {
		for sz := cz - 1; sz <= cz+1; sz++ {
			r := rng(posHash(g.treeSeed, sx, 0, sz))
			for range treeAttempts {
				// A dark oak's trunk stays in its chunk, so its origin keeps off the last row.
				x, z := sx*16+r.intn(16), sz*16+r.intn(16)
				u := r.float() * treeAttempts
				shape := r.next()
				if x < d.originX-treeReach || x > d.originX+15+treeReach || z < d.oz-treeReach || z > d.oz+15+treeReach {
					continue
				}
				if u >= maxTreeRate {
					continue
				}
				col := d.biomeAt(x, z)
				rate := treeRates(col)
				if u >= rate.rate {
					continue
				}
				kind := rate.kinds[shape%uint64(len(rate.kinds))]
				d.tryTree(kind, col.surface, x, z, shape>>8)
			}
		}
	}
}

// maxTreeRate is the highest rate in treeRates. Anything over it is skipped before a biome lookup.
const maxTreeRate = 13.2

// biomeAt gives a column's surface biome: the chunk's own table, or worked out from the climate.
func (d *decorator) biomeAt(x, z int) def {
	if lx, lz := x-d.originX, z-d.oz; lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
		return d.surfaces[lx>>2][lz>>2]
	}
	return d.g.surfaceDefAt(x, z)
}

// site says whether a tree can stand on a column, and how high the ground is. The ground must be
// dry land the carvers and lakes left alone, with soil on top. The answer is the same from any chunk:
// inside this chunk it reads the chunk's data, and outside it works out the column.
func (d *decorator) site(kind surfaceKind, x, z int) (int, bool) {
	var h int
	if lx, lz := x-d.originX, z-d.oz; lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
		h = int(d.final[lx][lz])
		if h != int(d.top[lx][lz]) || h < SeaLevel || d.c.Block(uint8(lx), int16(h+1), uint8(lz), 0) == d.g.blocks.water {
			return 0, false
		}
	} else {
		top, untouched, dry := d.ground.ground(x, z)
		if !untouched || !dry || top < SeaLevel {
			return 0, false
		}
		h = top
	}
	return h, d.g.treeSoil(kind, x, h, z)
}

// columnTop is the top of the terrain at any column this chunk has masks for.
func (d *decorator) columnTop(x, z int) int {
	if lx, lz := x-d.originX, z-d.oz; lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
		return int(d.final[lx][lz])
	}
	top, _, _ := d.ground.ground(x, z)
	return top
}

// treeSoil says whether a dry column's top block, by the surface rule alone, is one a tree grows
// from. Carvers and lakes were ruled out by the caller, so the rule decides.
func (g *Generator) treeSoil(kind surfaceKind, x, y, z int) bool {
	b := &g.blocks
	fx, fz := float64(x), float64(z)
	if kind == surfWooded {
		return y > woodedCoarseY && y <= orangeAboveY
	}
	s := surfaceColumn{g: g, x: 0, z: 0, wx: fx, wz: fz, noise: g.surface.depth.at(fx, 0, fz)}
	id, ok := s.top(kind, y, 0)
	if !ok {
		return false
	}
	switch id {
	case b.grass, b.dirt, b.podzol, b.coarseDirt, b.mud, b.snow:
		return true
	}
	return false
}

// tryTree checks a tree's ground and writes it. A trunk goes straight up from the column's top
// block, and by definition nothing solid is above that, so only other trees can be in the way.
func (d *decorator) tryTree(kind treeKind, surface surfaceKind, x, z int, seed uint64) {
	r := rng(seed)
	if kind == treeDarkOak {
		// A 2 x 2 trunk. Keep it inside its origin chunk so one chunk's roll decides all four.
		x, z = min(x, x&^15+14), min(z, z&^15+14)
		var hs [4]int
		for i := range hs {
			h, ok := d.site(surface, x+i&1, z+i>>1)
			if !ok {
				return
			}
			hs[i] = h
		}
		base := max(hs[0], hs[1], hs[2], hs[3])
		if base-min(hs[0], hs[1], hs[2], hs[3]) > 2 {
			return
		}
		d.placeDarkOak(x, z, base, hs, &r)
		return
	}
	h, ok := d.site(surface, x, z)
	if !ok {
		return
	}
	d.groundUnder(x, h, z)
	switch kind {
	case treeSpruce:
		d.placeSpruce(x, z, h, &r)
	case treePine:
		d.placePine(x, z, h, &r)
	case treeAcacia:
		d.placeAcacia(x, z, h, &r)
	case treeJungle:
		d.placeRound(treeJungle, x, z, h, 7+r.intn(7), 2.7, 2.4, &r)
	case treeBirch:
		d.placeRound(treeBirch, x, z, h, 5+r.intn(3), 2.3, 2.2, &r)
	case treeCherry:
		d.placeRound(treeCherry, x, z, h, 5+r.intn(3), 3.4, 2.4, &r)
	case treePaleOak:
		d.placeRound(treePaleOak, x, z, h, 6+r.intn(3), 3.2, 2.4, &r)
	case treeMangrove:
		d.placeRound(treeMangrove, x, z, h, 5+r.intn(4), 2.7, 2.4, &r)
	default:
		d.placeRound(treeOak, x, z, h, 4+r.intn(3), 2.5, 2.2, &r)
	}
}

// set writes a block of a tree if it falls in this chunk. Logs go through leaves; leaves only go into
// air. Which block ends up where is then the same whichever tree comes first.
func (d *decorator) set(x, y, z int, id uint32, log bool) {
	lx, lz := x-d.originX, z-d.oz
	if lx < 0 || lx > 15 || lz < 0 || lz > 15 || y > maxY {
		return
	}
	b := &d.g.blocks
	r := d.c.Block(uint8(lx), int16(y), uint8(lz), 0)
	if r != b.air && !(log && b.isLeaves(r)) {
		return
	}
	d.c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, id)
	if int16(y) > d.cover[lx][lz] {
		d.cover[lx][lz] = int16(y)
	}
}

// groundUnder turns the block under a trunk into dirt. The block is in the trunk's own column.
func (d *decorator) groundUnder(x, y, z int) {
	if lx, lz := x-d.originX, z-d.oz; lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
		d.c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, d.g.blocks.dirt)
	}
}

// trunk writes a run of upright logs.
func (d *decorator) trunk(kind treeKind, x, z, from, to int) {
	id := d.g.blocks.logs[kind][1]
	for y := from; y <= to; y++ {
		d.set(x, y, z, id, true)
	}
}

// Own shapes. Every leaf stays within 4 steps of a log (leaves further away decay in Dragonfly), and
// every shape has leaves next to the top of each trunk. Corners are thinned with the tree's own rolls,
// so both sides of a chunk edge agree.

// placeRound is a straight trunk under a rounded crown: an ellipsoid, wide across and flat, whose
// middle sits at the trunk's second block from the top.
func (d *decorator) placeRound(kind treeKind, x, z, ground, height int, wide, tall float64, r *rng) {
	top := ground + height
	d.trunk(kind, x, z, ground+1, top)
	leaf := d.g.blocks.leaves[kind]
	seed := r.next()
	mid := top - 1
	span := int(math.Ceil(wide))
	for dy := -int(tall); dy <= int(tall)+1; dy++ {
		for dx := -span; dx <= span; dx++ {
			for dz := -span; dz <= span; dz++ {
				shell := float64(dx*dx+dz*dz)/(wide*wide) + float64(dy*dy)/(tall*tall)
				if shell > 1 {
					continue
				}
				if shell > 0.72 && unit(posHash(seed, dx, dy, dz)) < 0.45 {
					continue // Thin the outside.
				}
				d.set(x+dx, mid+dy, z+dz, leaf, false)
			}
		}
	}
	// The tip, always, so the top of the trunk is covered on every side.
	d.ring(x, top, z, 1, leaf)
	d.set(x, top+1, z, leaf, false)
}

// ring is a full diamond of leaves one layer thick.
func (d *decorator) ring(x, y, z, radius int, leaf uint32) {
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			if absInt(dx)+absInt(dz) <= radius+radius/2 {
				d.set(x+dx, y, z+dz, leaf, false)
			}
		}
	}
}

// placeSpruce is a cone with a stepped edge: rings that widen downward in saw teeth.
func (d *decorator) placeSpruce(x, z, ground int, r *rng) {
	height := 6 + r.intn(5)
	top := ground + height
	d.trunk(treeSpruce, x, z, ground+1, top)
	leaf := d.g.blocks.leaves[treeSpruce]
	d.set(x, top+1, z, leaf, false)
	bare := 1 + r.intn(2)
	radius := 1
	for y := top; y > ground+bare; y-- {
		d.ring(x, y, z, radius, leaf)
		radius++
		if radius > 3 || (top-y)%4 == 3 {
			radius = 1 + (top-y)/4
		}
	}
}

// placePine is a tall bare pole with a short crown at the top.
func (d *decorator) placePine(x, z, ground int, r *rng) {
	height := 8 + r.intn(6)
	top := ground + height
	d.trunk(treePine, x, z, ground+1, top)
	leaf := d.g.blocks.leaves[treePine]
	d.set(x, top+1, z, leaf, false)
	d.ring(x, top, z, 1, leaf)
	d.ring(x, top-1, z, 2, leaf)
	d.ring(x, top-2, z, 1, leaf)
}

// placeAcacia is a short trunk with a bend: sideways logs, more trunk, then a flat crown.
func (d *decorator) placeAcacia(x, z, ground int, r *rng) {
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	dir := dirs[r.intn(4)]
	lower := 2 + r.intn(3)
	reach := 1 + r.intn(2)
	bent := ground + lower
	// Past the columns worked out around this chunk, the tree has nothing in this chunk at all: its
	// crown reaches 3 from the bend and its first column is further away yet.
	lx, lz := x+dir[0]*reach-d.originX, z+dir[1]*reach-d.oz
	if lx < -treeMargin || lx > 15+treeMargin || lz < -treeMargin || lz > 15+treeMargin {
		return
	}
	// Ground in the way of the bend: it grows straight, so the upper trunk never rests on the ground.
	for i := 1; i <= reach; i++ {
		if d.columnTop(x+dir[0]*i, z+dir[1]*i) >= bent {
			reach = 0
			break
		}
	}
	d.trunk(treeAcacia, x, z, ground+1, bent-1)
	axis := 0
	if dir[1] != 0 {
		axis = 2
	}
	side := d.g.blocks.logs[treeAcacia][axis]
	for i := 0; i <= reach; i++ {
		if reach == 0 {
			side = d.g.blocks.logs[treeAcacia][1]
		}
		d.set(x+dir[0]*i, bent, z+dir[1]*i, side, true)
	}
	px, pz := x+dir[0]*reach, z+dir[1]*reach
	top := bent + 1 + r.intn(2)
	d.trunk(treeAcacia, px, pz, bent+1, top)
	leaf := d.g.blocks.leaves[treeAcacia]
	// The flat crown leans no further than treeReach from the first column: reach is at most 2.
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			if absInt(dx)+absInt(dz) <= 3 {
				d.set(px+dx, top+1, pz+dz, leaf, false)
			}
		}
	}
	d.ring(px, top, pz, 1, leaf)
	d.ring(px, top+2, pz, 1, leaf)
}

// placeDarkOak is a 2 x 2 trunk under a wide, low crown. hs are the four columns' grounds.
func (d *decorator) placeDarkOak(x, z, base int, hs [4]int, r *rng) {
	height := 5 + r.intn(3)
	top := base + height
	for i := range hs {
		cx, cz := x+i&1, z+i>>1
		d.groundUnder(cx, hs[i], cz)
		d.trunk(treeDarkOak, cx, cz, hs[i]+1, top)
	}
	leaf := d.g.blocks.leaves[treeDarkOak]
	seed := r.next()
	for y := top - 2; y <= top+1; y++ {
		wide := 3.9
		if y == top+1 {
			wide = 2.6
		}
		for dx := -4; dx <= 5; dx++ {
			for dz := -4; dz <= 5; dz++ {
				ex, ez := math.Abs(float64(dx)-0.5), math.Abs(float64(dz)-0.5)
				shell := (ex*ex + ez*ez) / (wide * wide)
				if shell > 1 || (shell > 0.7 && unit(posHash(seed, dx, y-top, dz)) < 0.35) {
					continue
				}
				d.set(x+dx, y, z+dz, leaf, false)
			}
		}
	}
}

// Own numbers for cold, with nothing measured behind them. The notes say only that freezing
// reads the biome's temperature with a height drop. The threshold is halfway between the warmest
// biome whose name and table row say snow (snowy beach, 0.05 in Dragonfly) and the coldest that
// does not (windswept and stony shore, 0.2). The drop is a choice that puts the snow line on
// windswept hills near Y 130, and leaves meadow and cherry grove bare up to about Y 178.
const (
	snowBelow     = 0.125
	snowDropStart = 90
	snowDropRate  = 1.0 / 500
)

// coldAt says whether a biome freezes at a height. Temperatures are Dragonfly's, which are Bedrock's
// where the editions differ (TestBedrockClimateValues). Deep frozen ocean reads 0.5 there and
// frozen ocean 0; the biome table puts both in the coldest row, so both count as cold. Our own choice.
func coldAt(b def, y int) bool {
	t := b.Temperature()
	if b.surface == surfFrozenOcean {
		t = 0
	}
	return t-float64(max(y-snowDropStart, 0))*snowDropRate < snowBelow
}

// freeze turns the top of open water to ice and lays snow on top of everything else, where it is
// cold enough. Column by column, from the chunk's own data.
func (d *decorator) freeze() {
	b := &d.g.blocks
	for x := range 16 {
		for z := range 16 {
			col := d.surfaces[x>>2][z>>2]
			lx, lz := uint8(x), uint8(z)
			y := int(d.cover[x][z])
			if y >= maxY || y < minY {
				continue
			}
			above := d.c.Block(lx, int16(y+1), lz, 0)
			if above == b.water {
				w := y + 1
				for w < maxY && d.c.Block(lx, int16(w+1), lz, 0) == b.water {
					w++
				}
				if coldAt(col, w) {
					d.c.SetBlock(lx, int16(w), lz, 0, b.ice)
					d.cover[x][z] = int16(w)
				}
				continue
			}
			if !coldAt(col, y+1) {
				continue
			}
			if r := d.c.Block(lx, int16(y), lz, 0); r != b.lava && r != b.ice {
				d.c.SetBlock(lx, int16(y+1), lz, 0, b.snowLayer)
				d.cover[x][z] = int16(y + 1)
			}
		}
	}
}

// plantRow is a batch of one plant in a biome: attempts per chunk, and which block.
type plantRow struct {
	attempts int
	plant    func(b *blocks, u uint64) (lower, upper uint32)
}

// Plants, by name. A double plant has two halves; a single one has no upper half.
func plantOne(pick func(b *blocks) uint32) func(*blocks, uint64) (uint32, uint32) {
	return func(b *blocks, _ uint64) (uint32, uint32) { return pick(b), 0 }
}

var (
	shortGrassPlant = plantOne(func(b *blocks) uint32 { return b.shortGrass })
	fernPlant       = plantOne(func(b *blocks) uint32 { return b.fern })
	tallGrassPlant  = func(b *blocks, _ uint64) (uint32, uint32) { return b.tallGrass[0], b.tallGrass[1] }
	largeFernPlant  = func(b *blocks, _ uint64) (uint32, uint32) { return b.largeFern[0], b.largeFern[1] }
	sunflowerPlant  = func(b *blocks, _ uint64) (uint32, uint32) { return b.sunflower[0], b.sunflower[1] }
	tallFlowerPlant = func(b *blocks, u uint64) (uint32, uint32) {
		p := b.tallFlowers[u%uint64(len(b.tallFlowers))]
		return p[0], p[1]
	}
	orchidPlant  = plantOne(func(b *blocks) uint32 { return b.blueOrchid })
	commonFlower = func(b *blocks, u uint64) (uint32, uint32) { return b.commonFlowers[u%uint64(len(b.commonFlowers))], 0 }
	anyFlower    = func(b *blocks, u uint64) (uint32, uint32) { return b.flowers[u%uint64(len(b.flowers))], 0 }
)

// plantRows are the land plants of a biome. All counts are our own: the study did not measure
// plants (other than kelp and seagrass), and Bedrock's lists only tell which blocks occur.
func plantRows(d def) []plantRow {
	switch d {
	case plains, sunflower, meadow:
		rows := []plantRow{{36, shortGrassPlant}, {3, tallGrassPlant}, {4, commonFlower}}
		if d == sunflower {
			rows = append(rows, plantRow{6, sunflowerPlant})
		}
		if d == meadow {
			rows = append(rows, plantRow{8, anyFlower})
		}
		return rows
	case forest, birchForest, oldBirch, darkForest, paleGarden, cherryGrove, windsweptForest:
		return []plantRow{{10, shortGrassPlant}, {1, tallGrassPlant}, {2, commonFlower}}
	case flowerForest:
		return []plantRow{{10, shortGrassPlant}, {24, anyFlower}, {3, tallFlowerPlant}}
	case taiga, oldSpruce, oldPine:
		return []plantRow{{10, shortGrassPlant}, {8, fernPlant}, {2, largeFernPlant}}
	case jungle, sparseJungle, bambooJungle:
		return []plantRow{{16, shortGrassPlant}, {6, fernPlant}, {2, largeFernPlant}, {2, tallGrassPlant}}
	case savanna, savannaPlateau, windSavanna:
		return []plantRow{{28, shortGrassPlant}, {4, tallGrassPlant}}
	case swamp, mangrove:
		return []plantRow{{6, shortGrassPlant}, {1, orchidPlant}}
	case river, windsweptHills, gravellyHills:
		return []plantRow{{4, shortGrassPlant}}
	}
	return nil
}

// Own rates of the plants with their own rules, per chunk: cactus and dead bush per dry chunk, cane
// beside water, lily pads per swamp chunk.
const (
	cactusAttempts   = 3
	deadBushAttempts = 3
	caneAttempts     = 12
	lilyAttempts     = 5
	// Kelp and seagrass attempts are tuned to Bedrock's per ocean chunk (TestKelpAndSeagrassMatchBedrock).
	kelpAttempts     = 44
	seagrassAttempts = 76
)

// placePlants writes the plants, from the chunk's own data: a plant never crosses a chunk edge, so
// nothing from the neighbours can reach here. Only the column's own blocks, and for cane and
// cactus its in-chunk sides, are read.
func (d *decorator) placePlants() {
	g := d.g
	r := rng(posHash(g.treeSeed^0x51, d.originX>>4, 0, d.oz>>4))
	column := func() (x, z int) { return r.intn(16), r.intn(16) }
	// Rows go by biome cell, so a chunk across a biome border gets its share of each.
	for i := range d.surfaces {
		for j := range d.surfaces[i] {
			col := d.surfaces[i][j]
			for _, row := range plantRows(col) {
				// A 16th of the chunk's rate for each of its 16 cells, with the remainder as a chance.
				n := row.attempts / 16
				if r.float() < float64(row.attempts%16)/16 {
					n++
				}
				for range n {
					u := r.next()
					d.landPlant(i*4+int(u%4), j*4+int((u>>2)%4), row, u>>4)
				}
			}
		}
	}
	for range cactusAttempts {
		x, z := column()
		h := 1 + r.intn(3)
		if d.surfaces[x>>2][z>>2].surface == surfDesert {
			d.cactus(x, z, h)
		}
	}
	for range deadBushAttempts {
		x, z := column()
		switch d.surfaces[x>>2][z>>2].surface {
		case surfDesert, surfBadlands, surfEroded, surfWooded:
			d.deadBush(x, z)
		}
	}
	for range caneAttempts {
		x, z := column()
		d.cane(x, z, 1+r.intn(3))
	}
	for range lilyAttempts {
		x, z := column()
		if k := d.surfaces[x>>2][z>>2].surface; k == surfSwamp || k == surfMangrove {
			d.lily(x, z)
		}
	}
	for range kelpAttempts {
		x, z := column()
		h := 1 + r.intn(9)
		d.kelp(x, z, h)
	}
	for range seagrassAttempts {
		x, z := column()
		d.seagrass(x, z)
	}
}

// openTop gives a column's top and whether a land plant could stand on it: air above, nothing placed
// there yet, and soil to grow from.
func (d *decorator) openTop(x, z int) (int, bool) {
	y := int(d.cover[x][z])
	if y < SeaLevel || y >= maxY-3 || y != int(d.final[x][z]) {
		return y, false
	}
	return y, d.c.Block(uint8(x), int16(y+1), uint8(z), 0) == d.g.blocks.air
}

// landPlant places one or two blocks on grass, dirt or podzol.
func (d *decorator) landPlant(x, z int, row plantRow, u uint64) {
	y, ok := d.openTop(x, z)
	if !ok {
		return
	}
	b := &d.g.blocks
	switch d.c.Block(uint8(x), int16(y), uint8(z), 0) {
	case b.grass, b.dirt, b.podzol, b.mud:
	default:
		return
	}
	lower, upper := row.plant(b, u)
	d.c.SetBlock(uint8(x), int16(y+1), uint8(z), 0, lower)
	d.cover[x][z] = int16(y + 1)
	if upper != 0 {
		d.c.SetBlock(uint8(x), int16(y+2), uint8(z), 0, upper)
		d.cover[x][z] = int16(y + 2)
	}
}

// cactus stands on sand with nothing solid beside it. It keeps inside the chunk, so a neighbour
// cannot put a block next to it.
func (d *decorator) cactus(x, z, height int) {
	y, ok := d.openTop(x, z)
	b := &d.g.blocks
	if !ok || x < 1 || x > 14 || z < 1 || z > 14 || d.c.Block(uint8(x), int16(y), uint8(z), 0) != b.sand {
		return
	}
	for _, n := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		for dy := 1; dy <= height; dy++ {
			if d.c.Block(uint8(x+n[0]), int16(y+dy), uint8(z+n[1]), 0) != b.air {
				return
			}
		}
	}
	for dy := 1; dy <= height; dy++ {
		d.c.SetBlock(uint8(x), int16(y+dy), uint8(z), 0, b.cactus)
	}
	d.cover[x][z] = int16(y + height)
}

func (d *decorator) deadBush(x, z int) {
	y, ok := d.openTop(x, z)
	if !ok {
		return
	}
	b := &d.g.blocks
	r := d.c.Block(uint8(x), int16(y), uint8(z), 0)
	if r != b.sand && r != b.redSand && !b.isTerracotta(r) {
		return
	}
	d.c.SetBlock(uint8(x), int16(y+1), uint8(z), 0, b.deadBush)
	d.cover[x][z] = int16(y + 1)
}

// cane needs water beside its base block, read inside the chunk only.
func (d *decorator) cane(x, z, height int) {
	y, ok := d.openTop(x, z)
	b := &d.g.blocks
	if !ok || y != SeaLevel && y != SeaLevel+1 || x < 1 || x > 14 || z < 1 || z > 14 {
		return
	}
	switch d.c.Block(uint8(x), int16(y), uint8(z), 0) {
	case b.grass, b.dirt, b.sand, b.redSand, b.podzol, b.mud:
	default:
		return
	}
	beside := false
	for _, n := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		beside = beside || d.c.Block(uint8(x+n[0]), int16(y), uint8(z+n[1]), 0) == b.water
	}
	if !beside {
		return
	}
	for dy := 1; dy <= height; dy++ {
		d.c.SetBlock(uint8(x), int16(y+dy), uint8(z), 0, b.sugarCane)
	}
	d.cover[x][z] = int16(y + height)
}

// waterAbove reports a column whose top is under open water up to sea level, and how deep.
func (d *decorator) waterAbove(x, z int) (floor, depth int, ok bool) {
	floor = int(d.cover[x][z])
	if floor >= SeaLevel || floor < minY {
		return 0, 0, false
	}
	b := &d.g.blocks
	for y := floor + 1; y <= SeaLevel; y++ {
		if d.c.Block(uint8(x), int16(y), uint8(z), 0) != b.water {
			return 0, 0, false
		}
		depth++
	}
	return floor, depth, true
}

// lily sits on the water's surface.
func (d *decorator) lily(x, z int) {
	_, depth, ok := d.waterAbove(x, z)
	b := &d.g.blocks
	if !ok || depth < 1 || d.c.Block(uint8(x), SeaLevel+1, uint8(z), 0) != b.air {
		return
	}
	d.c.SetBlock(uint8(x), SeaLevel+1, uint8(z), 0, b.lilyPad)
}

// kelpGrows says whether a biome holds kelp and seagrass: oceans that are not frozen or warm and,
// for seagrass, rivers too. Our own choice; the Bedrock counts are not split by biome.
func kelpGrows(k surfaceKind) bool { return k == surfOcean || k == surfWarmFloor }

// waterPlant stands on the floor of a column, with its water in the block's second layer, as
// Dragonfly keeps it.
func (d *decorator) waterPlant(x, y, z int, id uint32) {
	b := &d.g.blocks
	d.c.SetBlock(uint8(x), int16(y), uint8(z), 1, b.water)
	d.c.SetBlock(uint8(x), int16(y), uint8(z), 0, id)
}

func (d *decorator) kelp(x, z, height int) {
	if !kelpGrows(d.surfaces[x>>2][z>>2].surface) {
		return
	}
	floor, depth, ok := d.waterAbove(x, z)
	if !ok || depth < 3 || !d.g.blocks.holdsWaterPlant(d.c.Block(uint8(x), int16(floor), uint8(z), 0)) {
		return
	}
	height = min(height, depth-2)
	for dy := 1; dy <= height; dy++ {
		d.waterPlant(x, floor+dy, z, d.g.blocks.kelp)
	}
	d.cover[x][z] = int16(floor + height)
}

func (d *decorator) seagrass(x, z int) {
	col := d.surfaces[x>>2][z>>2]
	if !kelpGrows(col.surface) && col != river {
		return
	}
	floor, depth, ok := d.waterAbove(x, z)
	if !ok || depth < 2 || !d.g.blocks.holdsWaterPlant(d.c.Block(uint8(x), int16(floor), uint8(z), 0)) {
		return
	}
	d.waterPlant(x, floor+1, z, d.g.blocks.seagrass)
	d.cover[x][z] = int16(floor + 1)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
