package worldgen

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

// surfaceKind says which surface rule a biome uses. The rule set comes from studying
// Java 26.1: what lies on top, the filler under it and when water changes them. Band
// thresholds marked "own" are ours where the study gives only the idea.
//
// Dragonfly has no powder snow block, so grove and snowy slopes use plain snow where the
// game has powder snow bands.
type surfaceKind uint8

const (
	surfNone surfaceKind = iota
	surfGrass
	surfOldGrowth   // Grass with coarse dirt and podzol bands.
	surfSnow        // Snow block over dirt.
	surfGrove       // Snow block over dirt.
	surfSnowySlopes // Snow, stone where steep.
	surfJagged      // Snow, stone where steep.
	surfFrozenPeaks // Snow with packed ice and ice patches, packed ice where steep.
	surfStonyPeaks  // Stone with calcite strips.
	surfBeach       // Sand over sandstone. Warm oceans use it too.
	surfDesert      // Sand over deep sandstone.
	surfBadlands
	surfEroded // Badlands with hoodoos.
	surfWooded // Badlands with coarse dirt on high tops.
	surfMycelium
	surfWindswept   // Grass, stone where the surface noise is high.
	surfWindSavanna // Grass, coarse dirt and stone bands.
	surfGravelly    // Gravel, grass and stone bands.
	surfShore       // Stone, with strips of gravel.
	surfSwamp       // Grass with puddles at sea level.
	surfMangrove    // Mud with puddles at sea level.
	surfOcean       // Dirt floor in shallows, gravel deep.
	surfWarmFloor   // Sand floor.
	surfFrozenOcean // Like surfOcean, with icebergs.
)

// surfaceNoises holds the noises the surface rules read: the game's named noises, seeded as the game
// seeds them. The rules that read them stay our own design.
type surfaceNoises struct {
	depth, bands, gravel, calcite, ice, puddle, hoodoo, hoodooCap, berg, bergCap *normalNoise
	second, packed, powder, swamp, hoodooSurface, bergSurface                    *normalNoise
}

func newSurfaceNoises(w seedFactory) surfaceNoises {
	return surfaceNoises{
		depth:         newNamedNoise(w, "surface"),
		bands:         newNamedNoise(w, "clay_bands_offset"),
		gravel:        newNamedNoise(w, "gravel"),
		calcite:       newNamedNoise(w, "calcite"),
		ice:           newNamedNoise(w, "ice"),
		second:        newNamedNoise(w, "surface_secondary"),
		packed:        newNamedNoise(w, "packed_ice"),
		powder:        newNamedNoise(w, "powder_snow"),
		swamp:         newNamedNoise(w, "surface_swamp"),
		hoodooSurface: newNamedNoise(w, "badlands_surface"),
		bergSurface:   newNamedNoise(w, "iceberg_surface"),
		puddle:        newNamedNoise(w, "surface_swamp"),
		hoodoo:        newNamedNoise(w, "badlands_pillar"),
		hoodooCap:     newNamedNoise(w, "badlands_pillar_roof"),
		berg:          newNamedNoise(w, "iceberg_pillar"),
		bergCap:       newNamedNoise(w, "iceberg_pillar_roof"),
	}
}

// Heights used by the surface rules, from the study.
const (
	redSandTop     = 74  // Red sand up to about here, band pattern above.
	woodedCoarseY  = 97  // Wooded badlands turn to coarse dirt tops above this.
	orangeAboveY   = 256 // Badlands tops above this are orange terracotta.
	waterFillerMax = 6   // Within this much water, floors use filler.
	bandsDownToY   = 63  // Badlands bands continue down to here.
)

// Band colours, as indexes into blocks.terracotta.
const (
	bandPlain = iota
	bandOrange
	bandYellow
	bandBrown
	bandRed
	bandWhite
	bandLightGrey
	bandTableSize = 192
)

// newBandTable builds the world's terracotta band pattern: plain terracotta with runs of
// orange, yellow, brown, red and white, the white with light grey edges. The size of the
// table and its colours come from the study; the run lengths and counts are our own.
func newBandTable(seed uint64) (t [bandTableSize]uint8) {
	r := rng(seed)
	place := func(colour uint8, count, longest int, edged bool) {
		for range count {
			at := int(r.next() % bandTableSize)
			n := 1 + int(r.next()%uint64(longest))
			for i := range n {
				t[(at+i)%bandTableSize] = colour
			}
			if edged {
				t[(at+bandTableSize-1)%bandTableSize] = bandLightGrey
				t[(at+n)%bandTableSize] = bandLightGrey
			}
		}
	}
	place(bandOrange, 24, 2, false)
	place(bandYellow, 8, 2, false)
	place(bandBrown, 8, 3, false)
	place(bandRed, 8, 2, false)
	place(bandWhite, 6, 2, true)
	return t
}

// surfaceColumn is one column being covered.
type surfaceColumn struct {
	g        *Generator
	c        *chunk.Chunk
	x, z     uint8
	wx, wz   float64
	noise    float64 // Surface noise.
	depth    int     // Depth of the soil here.
	steep    bool
	bandBase int // Offset into the band table.
}

// columnExtension is what a column adds above its noise ground: an iceberg or a hoodoo.
type columnExtension struct {
	top     int // Highest Y of the extension, or lower than the ground for none.
	bottom  int // Iceberg only: its lowest Y.
	iceberg bool
}

// extensionAt works out the extension a surface kind gives a column. It is a function of
// position and kind only, so SurfaceHeight can ask without a chunk.
func (g *Generator) extensionAt(kind surfaceKind, x, z int) columnExtension {
	fx, fz := float64(x), float64(z)
	none := columnExtension{top: minY - 1}
	switch kind {
	case surfEroded:
		// Pillars from Y 64, capped by a noise at 24 or more blocks (from the study). The
		// shape of the height curve is our own.
		v := math.Abs(g.surface.hoodoo.at(fx, 0, fz)) * 2.75
		h := min(v*v*2.5, math.Ceil(math.Abs(g.surface.hoodooCap.at(fx, 0, fz))*50)+24)
		if h < 3 {
			return none
		}
		return columnExtension{top: 64 + int(h)}
	case surfFrozenOcean:
		// Icebergs of packed ice rising tens of blocks, with a submerged part reaching 7
		// blocks deeper than the height above (from the study). The curve is our own.
		v := math.Abs(g.surface.berg.at(fx, 0, fz))*3 - 1.8
		if v <= 0 {
			return none
		}
		h := min(v*v*14, math.Ceil(math.Abs(g.surface.bergCap.at(fx, 0, fz))*40)+8)
		if h < 1 {
			return none
		}
		return columnExtension{top: SeaLevel + int(h), bottom: SeaLevel - int(h) - 7, iceberg: true}
	}
	return none
}

// puddleY is the height a swamp puddle sits at: the water's top.
const puddleY = waterTop

// puddled says whether a swamp column has a puddle: positive noise makes the ground at
// puddleY water (from the study).
func (g *Generator) puddled(kind surfaceKind, x, z int) bool {
	return (kind == surfSwamp || kind == surfMangrove) && g.surface.puddle.at(float64(x), 0, float64(z)) > 0
}

// applyExtension writes a column's extension into the chunk and returns the new top.
func (g *Generator) applyExtension(c *chunk.Chunk, ext columnExtension, x, z uint8, top int) int {
	b := &g.blocks
	if ext.iceberg {
		for y := ext.top; y >= ext.bottom && y > top; y-- {
			id := b.packedIce
			if y == ext.top {
				id = b.snow
			}
			c.SetBlock(x, int16(y), z, 0, id)
		}
		return max(top, ext.top)
	}
	for y := top + 1; y <= ext.top; y++ {
		c.SetBlock(x, int16(y), z, 0, b.stone)
	}
	return max(top, ext.top)
}

// applySurface covers the ground of each column. top holds the highest solid Y of each
// column and is raised by extensions.
func (g *Generator) applySurface(c *chunk.Chunk, originX, originZ int, top *[16][16]int16, cols *[4][4]biomeColumn) {
	var slopes *slopeGrid
	for x := range cols {
		for z := range cols[x] {
			switch cols[x][z].surface.surface {
			case surfSnowySlopes, surfJagged, surfFrozenPeaks:
				if slopes == nil {
					slopes = g.newSlopeGrid(originX, originZ)
				}
			}
		}
	}
	for x := range 16 {
		for z := range 16 {
			col := &cols[x>>2][z>>2]
			kind := col.surface.surface
			if ext := g.extensionAt(kind, originX+x, originZ+z); ext.top >= minY {
				top[x][z] = int16(g.applyExtension(c, ext, uint8(x), uint8(z), int(top[x][z])))
			}
		}
	}
	for x := range 16 {
		for z := range 16 {
			if top[x][z] < minY {
				continue
			}
			col := &cols[x>>2][z>>2]
			kind := col.surface.surface
			wx, wz := originX+x, originZ+z
			fx, fz := float64(wx), float64(wz)
			n := g.surface.depth.at(fx, 0, fz)
			s := surfaceColumn{g: g, c: c, x: uint8(x), z: uint8(z), wx: fx, wz: fz, noise: n}
			// Surface depth, from the study: about 1 to 6 blocks.
			s.depth = max(int(n*2.75+3+unit(posHash(g.strataSeed^0x7f, wx, 0, wz))*0.25), 1)
			if slopes != nil {
				s.steep = slopes.steep(x, z)
			}
			s.walk(kind, int(top[x][z]), int(col.ground))
			if top[x][z] == puddleY && g.puddled(kind, wx, wz) {
				c.SetBlock(uint8(x), puddleY, uint8(z), 0, g.blocks.water)
				top[x][z]--
			}
		}
	}
}

// slopeGrid holds the terrain height at the grid points of a chunk and one cell around it. It
// is worked out from the position alone, so a slope reads the same from either side of a chunk
// edge, and it has no caves in it, so a cave mouth does not count as a cliff.
type slopeGrid struct {
	height [cellsAcross + 3][cellsAcross + 3]float64
}

func (g *Generator) newSlopeGrid(originX, originZ int) *slopeGrid {
	s := &slopeGrid{}
	for i := range s.height {
		for j := range s.height[i] {
			s.height[i][j] = g.gridHeight(originX+(i-1)*cellXZ, originZ+(j-1)*cellXZ)
		}
	}
	return s
}

// gridHeight is where the top of the terrain crosses from solid to air at a grid point, found
// by going down its density nodes.
func (g *Generator) gridHeight(x, z int) float64 {
	shape := g.shapeAt(x, z)
	above := -1.0
	for n := columnNodes - 1; n >= 0; n-- {
		y := minY + n*cellY
		if shape.certainlyAir(y) {
			continue
		}
		d := squeeze(float64(slides(g.slopedAt(&shape, x, y, z), y) * 0.64))
		if d > 0 {
			return float64(y) + cellY*d/(d-above)
		}
		above = d
	}
	return minY
}

// at blends the height at a block offset from the chunk's corner, -1 to 16.
func (s *slopeGrid) at(x, z int) float64 {
	bilerp := func(a, b, c, d, tx, tz float64) float64 { return lerp(lerp(a, b, tx), lerp(c, d, tx), tz) }
	i, j := floorDiv(x, cellXZ), floorDiv(z, cellXZ)
	tx, tz := float64(x-i*cellXZ)/cellXZ, float64(z-j*cellXZ)/cellXZ
	return bilerp(s.height[i+1][j+1], s.height[i+2][j+1], s.height[i+1][j+2], s.height[i+2][j+2], tx, tz)
}

// steepHeight is the height change across a block's neighbours that counts as steep (notes
// section 9).
const steepHeight = 4

func (s *slopeGrid) steep(x, z int) bool {
	return math.Abs(s.at(x+1, z)-s.at(x-1, z)) >= steepHeight || math.Abs(s.at(x, z+1)-s.at(x, z-1)) >= steepHeight
}

// walk goes down a column from its top, covering each run of stone. Only runs near the
// nominal ground count, so deep cave floors stay bare.
func (s *surfaceColumn) walk(kind surfaceKind, top, ground int) {
	if kind == surfNone {
		return
	}
	b := &s.g.blocks
	limit := min(ground+s.depth-8, top-12)
	if kind == surfBadlands || kind == surfEroded || kind == surfWooded {
		limit = min(limit, bandsDownToY)
		s.bandBase = int(math.Round(s.g.surface.bands.at(s.wx, 0, s.wz) * 4))
	}
	run, water := 0, 0
	for y := max(top, SeaLevel); y >= limit && y >= minY; y-- {
		r := s.c.Block(s.x, int16(y), s.z, 0)
		switch {
		case r == b.air || r == b.lava:
			run, water = 0, 0
		case r == b.water:
			water++
			run = 0
		case r == b.stone || r == b.deepslate:
			if id, ok := s.rule(kind, y, run, water); ok {
				s.c.SetBlock(s.x, int16(y), s.z, 0, id)
			}
			run++
		}
	}
}

// rule says what a stone block at depth d in its run becomes, with water blocks above.
func (s *surfaceColumn) rule(kind surfaceKind, y, d, water int) (uint32, bool) {
	b := &s.g.blocks
	if kind == surfBadlands || kind == surfEroded || kind == surfWooded {
		return s.badlandsRule(kind, y, d, water)
	}
	if d >= s.depth {
		return s.below(kind, d)
	}
	if water > waterFillerMax {
		// Deep floors: sand in warm oceans, gravel elsewhere, stone under peaks.
		switch kind {
		case surfBeach, surfDesert, surfWarmFloor:
			return b.sand, true
		case surfJagged, surfFrozenPeaks, surfStonyPeaks, surfSnowySlopes, surfWindswept:
			return 0, false
		}
		return b.gravel, true
	}
	if d == 0 && water <= 1 {
		return s.top(kind, y, water)
	}
	return s.filler(kind)
}

// below is what lies under the soil: sandstone under sand, nothing otherwise.
func (s *surfaceColumn) below(kind surfaceKind, d int) (uint32, bool) {
	// Sandstone reaches up to 6 blocks under beaches and up to 30 under deserts (from the study).
	switch kind {
	case surfBeach:
		if d < s.depth+int((s.noise+1)*3) {
			return s.g.blocks.sandstone, true
		}
	case surfDesert:
		if d < s.depth+int((s.noise+1)*15) {
			return s.g.blocks.sandstone, true
		}
	}
	return 0, false
}

func (s *surfaceColumn) top(kind surfaceKind, y, water int) (uint32, bool) {
	b := &s.g.blocks
	n := s.noise
	switch kind {
	case surfBeach, surfDesert, surfWarmFloor:
		return b.sand, true
	case surfOldGrowth:
		if water == 0 {
			// Band thresholds from the study's description; the values are ours.
			switch {
			case n > 0.212:
				return b.coarseDirt, true
			case n > -0.115:
				return b.podzol, true
			}
			return b.grass, true
		}
	case surfSnow, surfGrove:
		return b.snow, true
	case surfSnowySlopes, surfJagged:
		if s.steep {
			return 0, false
		}
		return b.snow, true
	case surfFrozenPeaks:
		if s.steep {
			return b.packedIce, true
		}
		switch v := s.g.surface.ice.at(s.wx, 0, s.wz); {
		case v > 0.2:
			return b.packedIce, true
		case v > 0.1:
			return b.ice, true
		}
		return b.snow, true
	case surfStonyPeaks:
		if v := s.g.surface.calcite.at(s.wx, 0, s.wz); v > -0.0125 && v < 0.0125 {
			return b.calcite, true
		}
		return 0, false
	case surfMycelium:
		if water == 0 {
			return b.mycelium, true
		}
	case surfWindswept:
		if n > 0.12 {
			return 0, false // Above surface noise 0.12 windswept hills are stone (from the study).
		}
	case surfWindSavanna:
		switch {
		case n > 0.212:
			return 0, false
		case n > -0.06:
			return b.coarseDirt, true
		}
	case surfGravelly:
		switch {
		case n > 0.24:
			return 0, false
		case n > 0.12:
			return b.gravel, true
		case n > -0.12:
			if water == 0 {
				return b.grass, true
			}
			return b.dirt, true
		}
		return b.gravel, true
	case surfShore:
		return s.shore()
	case surfMangrove:
		return b.mud, true
	}
	if water > 0 {
		return b.dirt, true
	}
	return b.grass, true
}

func (s *surfaceColumn) filler(kind surfaceKind) (uint32, bool) {
	b := &s.g.blocks
	switch kind {
	case surfBeach, surfDesert, surfWarmFloor:
		return b.sand, true
	case surfSnowySlopes, surfJagged, surfFrozenPeaks, surfStonyPeaks:
		return 0, false
	case surfWindswept, surfWindSavanna:
		if s.noise > 0.12 {
			return 0, false
		}
	case surfGravelly:
		if s.noise > 0.24 {
			return 0, false
		}
		if s.noise > 0.12 || s.noise <= -0.12 {
			return b.gravel, true
		}
	case surfShore:
		return s.shore()
	case surfMangrove:
		return b.mud, true
	}
	return b.dirt, true
}

// shore is a stony shore: bare stone with strips of gravel, a narrow band on a first-octave -8
// noise (notes via GAPS.md).
func (s *surfaceColumn) shore() (uint32, bool) {
	if v := s.g.surface.gravel.at(s.wx, 0, s.wz); v > -0.05 && v < 0.05 {
		return s.g.blocks.gravel, true
	}
	return 0, false
}

// badlandsRule: red sand low down, terracotta bands above and below it, all the way down
// to Y 63. From the study.
func (s *surfaceColumn) badlandsRule(kind surfaceKind, y, d, water int) (uint32, bool) {
	b := &s.g.blocks
	if d == 0 && water <= 1 {
		switch {
		case y > orangeAboveY:
			return b.terracotta[bandOrange], true
		case kind == surfWooded && y > woodedCoarseY:
			return b.coarseDirt, true
		case y <= redSandTop+int(s.noise*3):
			return b.redSand, true
		}
	}
	if d < s.depth && y <= redSandTop {
		return b.terracotta[bandPlain], true
	}
	if y < bandsDownToY {
		return 0, false
	}
	return b.terracotta[s.g.bandTable[(y+s.bandBase+bandTableSize)%bandTableSize]], true
}

// disk is one round patch of floor material under water. Sand, clay and gravel disks are
// the way river beds and shallows get their patches: 3 sand, 1 clay and 1 gravel per chunk
// (rates from the study; the radii are our own).
var diskKinds = [...]struct {
	count, minR, maxR, thick int
	block                    func(b *blocks) uint32
}{
	{3, 2, 6, 2, func(b *blocks) uint32 { return b.sand }},
	{1, 1, 3, 1, func(b *blocks) uint32 { return b.clay }},
	{1, 2, 5, 2, func(b *blocks) uint32 { return b.gravel }},
}

// placeDisks lays the disks on each column's underwater floor. A disk is rolled from its
// origin chunk alone, and only the column's own floor is touched, so chunks agree.
func (g *Generator) placeDisks(c *chunk.Chunk, cx, cz int, top *[16][16]int16) {
	b := &g.blocks
	for sx := cx - 1; sx <= cx+1; sx++ {
		for sz := cz - 1; sz <= cz+1; sz++ {
			r := rng(posHash(g.diskSeed, sx, 0, sz))
			for k := range diskKinds {
				kind := &diskKinds[k]
				for range kind.count {
					px, pz := sx*16+int(r.next()%16), sz*16+int(r.next()%16)
					radius := kind.minR + int(r.next()%uint64(kind.maxR-kind.minR+1))
					for x := max(px-radius, cx*16); x <= min(px+radius, cx*16+15); x++ {
						for z := max(pz-radius, cz*16); z <= min(pz+radius, cz*16+15); z++ {
							if (x-px)*(x-px)+(z-pz)*(z-pz) > radius*radius {
								continue
							}
							lx, lz := uint8(x-cx*16), uint8(z-cz*16)
							y := int(top[lx][lz])
							if y >= SeaLevel || y < minY || c.Block(lx, int16(y+1), lz, 0) != b.water {
								continue
							}
							for d := range kind.thick {
								switch c.Block(lx, int16(y-d), lz, 0) {
								case b.dirt, b.sand, b.gravel, b.clay, b.stone, b.grass, b.mud:
									c.SetBlock(lx, int16(y-d), lz, 0, kind.block(b))
								}
							}
						}
					}
				}
			}
		}
	}
}
