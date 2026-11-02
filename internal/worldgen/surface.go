package worldgen

import "github.com/df-mc/dragonfly/server/world/chunk"

// surfaceKind says which blocks cover the ground of a biome. The materials per biome
// follow the biome pages on minecraft.wiki; depths of each layer are our own tuning,
// since the wiki gives no fixed numbers for them.
type surfaceKind uint8

const (
	surfNone        surfaceKind = iota
	surfGrass                   // Grass over dirt.
	surfPodzol                  // Grass over dirt, with podzol patches.
	surfSnow                    // Snow block over dirt.
	surfSnowOnStone             // A snow block over bare stone, for peaks and slopes.
	surfStone                   // Bare stone.
	surfSand                    // Sand over sandstone.
	surfBadlands                // Red sand over terracotta bands.
	surfGravel                  // Gravel over stone.
	surfMycelium                // Mycelium over dirt.
	surfWindswept               // Grass low down, bare stone high up.
	surfSeabed                  // Sand, with gravel and clay patches.
	surfColdSeabed              // Gravel, with sand and clay patches.
)

// Layer depths in blocks, our own tuning.
const (
	soilDepth       = 4   // Top block plus filler.
	sandDepth       = 4   // Sand before sandstone starts.
	sandstoneDepth  = 4   // Sandstone before stone starts.
	seabedDepth     = 4   // Sand, gravel or clay before stone.
	badlandsDepth   = 24  // Terracotta bands run this deep.
	windsweptStoneY = 110 // Windswept hills turn to bare stone above this.
	podzolThreshold = 0.3
	gravelThreshold = 0.35
	clayThreshold   = -0.4
)

// The patch noise is 3D, so one fractal gives independent patterns on different Y planes.
const (
	patchPlaneSeabed = 0
	patchPlanePodzol = 1000
	patchPlaneBands  = 2000
)

// badlandsBands is the order of terracotta colours going down, as indexes into
// blocks.terracotta. Own tuning: thin stripes of mixed colours.
var badlandsBands = [...]int{0, 1, 0, 2, 0, 3, 0, 4, 5, 0, 1, 6}

// surfaceInfo lets a column be written without allocating.
type surfaceInfo struct {
	g      *Generator
	c      *chunk.Chunk
	x, z   uint8 // Position in the chunk.
	wx, wz float64
	top    int
}

// applySurface replaces the top of each column's stone with the biome's surface blocks.
// top holds the highest solid Y of each column.
func (g *Generator) applySurface(c *chunk.Chunk, originX, originZ int, top *[16][16]int16, cols *[4][4]biomeColumn) {
	for x := range 16 {
		for z := range 16 {
			if top[x][z] < minY {
				continue
			}
			s := surfaceInfo{g: g, c: c, x: uint8(x), z: uint8(z), top: int(top[x][z])}
			s.wx, s.wz = float64(originX+x), float64(originZ+z)
			s.apply(cols[x>>2][z>>2].surface.surface)
		}
	}
}

// layer puts block id into count blocks going down from y, over stone only.
func (s *surfaceInfo) layer(y, count int, id uint32) int {
	for ; count > 0 && y >= minY; count-- {
		if s.c.Block(s.x, int16(y), s.z, 0) != s.g.blocks.stone {
			break
		}
		s.c.SetBlock(s.x, int16(y), s.z, 0, id)
		y--
	}
	return y
}

func (s *surfaceInfo) apply(kind surfaceKind) {
	b := &s.g.blocks
	y := s.top
	switch {
	case kind == surfNone || kind == surfStone:
		return
	case kind == surfSeabed || kind == surfColdSeabed || y < SeaLevel:
		s.seabed(kind == surfColdSeabed)
		return
	}
	switch kind {
	case surfGrass:
		y = s.layer(y, 1, b.grass)
		s.layer(y, soilDepth-1, b.dirt)
	case surfPodzol:
		top := b.grass
		if s.g.patch.at(s.wx, patchPlanePodzol, s.wz) > podzolThreshold {
			top = b.podzol
		}
		y = s.layer(y, 1, top)
		s.layer(y, soilDepth-1, b.dirt)
	case surfSnow:
		y = s.layer(y, 1, b.snow)
		s.layer(y, soilDepth-1, b.dirt)
	case surfSnowOnStone:
		s.layer(y, 1, b.snow)
	case surfSand:
		y = s.layer(y, sandDepth, b.sand)
		s.layer(y, sandstoneDepth, b.sandstone)
	case surfBadlands:
		y = s.layer(y, 1, b.redSand)
		shift := int(s.g.patch.at(s.wx, patchPlaneBands, s.wz) * 6)
		for d := 0; d < badlandsDepth && y >= minY; d, y = d+1, y-1 {
			band := badlandsBands[((y+shift)%len(badlandsBands)+len(badlandsBands))%len(badlandsBands)]
			if s.layer(y, 1, b.terracotta[band]) == y {
				break
			}
		}
	case surfGravel:
		s.layer(y, 3, b.gravel)
	case surfMycelium:
		y = s.layer(y, 1, b.mycelium)
		s.layer(y, soilDepth-1, b.dirt)
	case surfWindswept:
		if y <= windsweptStoneY {
			y = s.layer(y, 1, b.grass)
			s.layer(y, soilDepth-1, b.dirt)
		}
	}
}

// seabed lays sand, gravel or clay. Cold floors favour gravel.
func (s *surfaceInfo) seabed(cold bool) {
	b := &s.g.blocks
	p := s.g.patch.at(s.wx, patchPlaneSeabed, s.wz)
	id := b.sand
	switch {
	case p < clayThreshold:
		id = b.clay
	case cold && p > gravelThreshold-0.55, !cold && p > gravelThreshold:
		id = b.gravel
	}
	s.layer(s.top, seabedDepth, id)
}
