package worldgen

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Caves are carved out of finished terrain by 3D noise, as minecraft.wiki "Cave" describes
// noise caves: "cheese" caves are big open rooms, "spaghetti" caves are long winding
// tunnels. The wiki gives no noise settings or thresholds, so every wavelength and
// threshold below is our own tuning, set so that between Y -40 and 40 a few percent of
// the space is air (the test measures it). From the same page:
//   - noise caves reach down to Y -59 (caveMinY) and up to Y 256 (caveMaxY);
//   - below Y -54 the fluid in carved space is lava (lavaLevel), not air;
//   - aquifers split the world into 16x12x16 cells, offset 5 blocks on X and Z and 1 on Y,
//     and a flooded cell's water level is 40*floor(Y/40)+20+spread, with spread from -10 to
//     10 in steps of 3.
//
// Left out, on purpose: noodle caves, noise pillars, carver caves and canyons (this task
// asks for cheese and spaghetti only), lava aquifers (they would put lava above Y -54),
// the stone barriers around aquifers, the rule that picks the nearest of several aquifer
// cell centres (a block takes its own cell's level), and the erosion-and-depth rule that
// bans aquifers. Water can therefore touch lava at Y -54.
const (
	caveMinY = -59
	caveMaxY = 256
	// lavaLevel: carved space below this Y is lava.
	lavaLevel = -54

	// The noise is worked out every caveGrid blocks and blended between, like terrain.
	caveGrid = 4

	// Own tuning from here on.
	cheeseThreshold   = 0.52 // Cheese noise above this is open.
	spaghettiWidth    = 0.055
	oceanSeal         = 4    // Blocks of floor kept under ocean and river water.
	entranceDepth     = 8    // Caves stay this far under ground unless an entrance is here.
	entranceThreshold = 0.5  // Entrance noise above this lets caves reach the surface.
	aquiferShare      = 0.35 // Share of aquifer cells that hold water.
)

type caveNoises struct {
	cheese, spaghettiA, spaghettiB, entrance *fractal
}

func newCaveNoises(seed int64) caveNoises {
	sub := func(id uint64) uint64 { return mix64(uint64(seed) ^ mix64(id)) }
	return caveNoises{
		cheese:     newFractal(sub(11), 110, 70, 2),
		spaghettiA: newFractal(sub(12), 90, 60, 2),
		spaghettiB: newFractal(sub(13), 90, 60, 2),
		entrance:   newFractal(sub(14), 130, 130, 2),
	}
}

// caveSample holds the cave noises at one grid point.
type caveSample struct{ cheese, a, b float64 }

func (g *Generator) caveSampleAt(x, y, z int) caveSample {
	fx, fy, fz := float64(x), float64(y), float64(z)
	return caveSample{
		cheese: g.caves.cheese.at(fx, fy, fz),
		a:      g.caves.spaghettiA.at(fx, fy, fz),
		b:      g.caves.spaghettiB.at(fx, fy, fz),
	}
}

// open says whether the noise alone carves here: a cheese room, or a spaghetti tunnel
// where both tunnel noises are near zero.
func (s caveSample) open() bool {
	return s.cheese > cheeseThreshold || (math.Abs(s.a) < spaghettiWidth && math.Abs(s.b) < spaghettiWidth)
}

// caveCell holds the samples at the corners of one grid cell: x, then z, then y.
type caveCell [2][2][2]caveSample

// mayCarve is false only when no block of the cell can be open. A blended value stays
// between the corner values, so a cell whose corners are all too low, or all on one side
// of a tunnel, is skipped.
func (cc *caveCell) mayCarve() bool {
	top, loA, hiA, loB, hiB := math.Inf(-1), math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for x := range 2 {
		for z := range 2 {
			for y := range 2 {
				s := &cc[x][z][y]
				top = math.Max(top, s.cheese)
				loA, hiA = math.Min(loA, s.a), math.Max(hiA, s.a)
				loB, hiB = math.Min(loB, s.b), math.Max(hiB, s.b)
			}
		}
	}
	return top > cheeseThreshold ||
		(loA < spaghettiWidth && hiA > -spaghettiWidth && loB < spaghettiWidth && hiB > -spaghettiWidth)
}

func lerpSample(a, b caveSample, t float64) caveSample {
	return caveSample{lerp(a.cheese, b.cheese, t), lerp(a.a, b.a, t), lerp(a.b, b.b, t)}
}

// blend interpolates the corners at a block's offset in the cell.
func (cc *caveCell) blend(dx, dz, dy int) caveSample {
	tx, tz, ty := float64(dx)/caveGrid, float64(dz)/caveGrid, float64(dy)/caveGrid
	return lerpSample(
		lerpSample(lerpSample(cc[0][0][0], cc[1][0][0], tx), lerpSample(cc[0][1][0], cc[1][1][0], tx), tz),
		lerpSample(lerpSample(cc[0][0][1], cc[1][0][1], tx), lerpSample(cc[0][1][1], cc[1][1][1], tx), tz),
		ty)
}

func (g *Generator) entranceAt(x, z int) float64 {
	return g.caves.entrance.at(float64(x), 0, float64(z))
}

// carves is the full rule for one block: the noise must be open, and the block must be
// allowed to open. top is the Y of the column's highest terrain block. Under ocean and
// river water the floor is kept, and near the surface caves only open where the entrance
// noise is high, so a few caves reach the sky.
func carves(s caveSample, y, top int, entrance float64) bool {
	if y < caveMinY || y > caveMaxY || !s.open() {
		return false
	}
	depth := top - y
	if top < SeaLevel && depth < oceanSeal {
		return false
	}
	return depth >= entranceDepth || entrance > entranceThreshold
}

// aquiferLevel gives the water level of the aquifer cell around a block, if the cell
// holds water. The level never goes above sea level (own tuning), so no pools hang in the
// air of the hills.
func (g *Generator) aquiferLevel(x, y, z int) (int, bool) {
	ax, ay, az := floorDiv(x+5, 16), floorDiv(y+1, 12), floorDiv(z+5, 16)
	h := posHash(g.aquiferSeed, ax, ay, az)
	if unit(h) >= aquiferShare {
		return 0, false
	}
	centre := ay*12 - 1 + 6
	spread := -10 + 3*int((h>>8)%7)
	return min(40*floorDiv(centre, 40)+20+spread, SeaLevel), true
}

// caveFluid is what fills carved space at a block: lava deep down, water in a flooded
// aquifer, and air otherwise.
func (g *Generator) caveFluid(x, y, z int) uint32 {
	if y < lavaLevel {
		return g.blocks.lava
	}
	if level, ok := g.aquiferLevel(x, y, z); ok && y < level {
		return g.blocks.water
	}
	return g.blocks.air
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// carveCaves opens the caves in a finished chunk. top holds each column's highest solid
// terrain block. Only the noise at the chunk's own grid points is used, and the noise is
// a function of world position, so caves meet exactly at chunk edges.
func (g *Generator) carveCaves(c *chunk.Chunk, originX, originZ, highest int, top *[16][16]int16) {
	const across = 16/caveGrid + 1
	highest = min(highest, caveMaxY)
	if highest < caveMinY {
		return
	}
	// Grid layers 1 to last hold the corners of every cell that reaches a block under highest.
	last := (highest-minY)/caveGrid + 1
	var grid [across][across][(maxY-minY+1)/caveGrid + 1]caveSample
	for i := range grid {
		for j := range grid[i] {
			for l := 1; l <= last; l++ {
				grid[i][j][l] = g.caveSampleAt(originX+i*caveGrid, minY+l*caveGrid, originZ+j*caveGrid)
			}
		}
	}
	var entrance [16][16]float64
	for x := range entrance {
		for z := range entrance[x] {
			entrance[x][z] = g.entranceAt(originX+x, originZ+z)
		}
	}

	b := &g.blocks
	var wet [16][16]bool // Columns that got aquifer water.
	for i := range across - 1 {
		for j := range across - 1 {
			for l := 1; l < last; l++ {
				cc := caveCell{
					{{grid[i][j][l], grid[i][j][l+1]}, {grid[i][j+1][l], grid[i][j+1][l+1]}},
					{{grid[i+1][j][l], grid[i+1][j][l+1]}, {grid[i+1][j+1][l], grid[i+1][j+1][l+1]}},
				}
				if !cc.mayCarve() {
					continue
				}
				baseY := minY + l*caveGrid
				for dx := range caveGrid {
					for dz := range caveGrid {
						x, z := i*caveGrid+dx, j*caveGrid+dz
						for dy := range caveGrid {
							y := baseY + dy
							if y > int(top[x][z]) {
								break
							}
							if !carves(cc.blend(dx, dz, dy), y, int(top[x][z]), entrance[x][z]) {
								continue
							}
							switch c.Block(uint8(x), int16(y), uint8(z), 0) {
							case b.air, b.water, b.lava, b.bedrock:
								continue
							}
							fluid := g.caveFluid(originX+x, y, originZ+z)
							wet[x][z] = wet[x][z] || fluid == b.water
							c.SetBlock(uint8(x), int16(y), uint8(z), 0, fluid)
						}
					}
				}
			}
		}
	}
	for x := range wet {
		for z := range wet[x] {
			if wet[x][z] {
				g.drainFloating(c, x, z, int(top[x][z]))
			}
		}
	}
}

// drainFloating turns aquifer water that has air under it into air. Aquifer cells are
// decided one at a time, so a flooded cell can sit on a dry one; this keeps water from
// hanging in the air (a stand-in for the stone barriers the wiki describes). Working from
// the bottom up, the water above a gap drains too.
func (g *Generator) drainFloating(c *chunk.Chunk, x, z, top int) {
	b := &g.blocks
	for y := caveMinY + 1; y <= top; y++ {
		if c.Block(uint8(x), int16(y), uint8(z), 0) == b.water && c.Block(uint8(x), int16(y-1), uint8(z), 0) == b.air {
			c.SetBlock(uint8(x), int16(y), uint8(z), 0, b.air)
		}
	}
}

// SurfaceHeight returns the Y of the highest solid block in a column, with caves taken
// into account: it gives the same answer as the generated chunk, without needing the
// chunk to exist. Where a cave opens the top of the ground, this is lower than the
// terrain (terrainHeight). Later passes that stand things on the ground should use it.
func (g *Generator) SurfaceHeight(x, z int) int {
	top := g.terrainHeight(x, z)
	if top < caveMinY {
		return top
	}
	entrance := g.entranceAt(x, z)
	// Most columns end here: the top block can only be carved under an entrance, and
	// never under ocean or river water.
	if entrance <= entranceThreshold || top < SeaLevel {
		return top
	}
	gridX, gridZ := x&^(caveGrid-1), z&^(caveGrid-1)
	var cc caveCell
	cellY := math.MinInt
	return g.highestSolid(x, z, func(y int) bool {
		if y < caveMinY || y > caveMaxY {
			return false
		}
		if base := floorDiv(y, caveGrid) * caveGrid; base != cellY {
			cellY = base
			for i := range 2 {
				for j := range 2 {
					for k := range 2 {
						cc[i][j][k] = g.caveSampleAt(gridX+i*caveGrid, base+k*caveGrid, gridZ+j*caveGrid)
					}
				}
			}
		}
		return carves(cc.blend(x-gridX, z-gridZ, y-cellY), y, top, entrance)
	})
}

// noiseHeight is the top of the ground with noise caves but without carvers.
func (g *Generator) noiseHeight(x, z int) int { return g.SurfaceHeight(x, z) }
