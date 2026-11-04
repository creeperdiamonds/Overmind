package worldgen

import (
	"math"
	"sync"
)

// Terrain adaptation of legacy jigsaw pieces (Task 9): Bedrock adds a "beard" term to the overworld
// density of every block near a piece, so ancient cities stand in carved space and villages on filled
// ground. Our own Go from NOTES-structures-phase2, section "2026-11-04 (phase 3, round 2): terrain
// adaptation of legacy jigsaw pieces" (cited as TA below). The structures package says which pieces are
// near a chunk (TerrainAdjuster); this file turns them into density.

// Adaptation modes (TA "Which pieces").
const (
	AdaptBeard         uint8 = iota // Villages: kernel * 0.8 around the box base.
	AdaptBeardAndShave              // Ancient cities, bastions: carves the box, fills under it.
)

// TerrainAdjustment is one piece's adaptation: its box (inclusive, after placement), the base lift
// deltaY, and the widths of beard-and-shave (TA: 1.0 and 5.0 for cities and bastions).
type TerrainAdjustment struct {
	Mode       uint8
	Box        [6]int // Min x, y, z, max x, y, z.
	DeltaY     int
	MinW, MaxW float32
}

// TerrainAdjuster is a StructurePlacer that also adapts terrain: the adjustments of every piece whose
// box, grown by 12 blocks in x and z, reaches chunk (cx, cz) (TA: 0xd817f80).
type TerrainAdjuster interface {
	TerrainAdjustments(cx, cz int, into []TerrainAdjustment) []TerrainAdjustment
}

// adaptDesc is a description record (TA "Description record").
type adaptDesc struct {
	mode                      uint8
	minX, minY, minZ          int // Bounds: the box grown by 2 in x and z.
	maxX, maxY, maxZ          int
	baseY, xCenter, zCenter   int
	maxRadius                 float32
	minW, maxW                float32
	reach0X, reach0Y, reach0Z int // Where the term can be non-zero (kernel reach), for skipping.
	reach1X, reach1Y, reach1Z int
}

func newAdaptDesc(a TerrainAdjustment) adaptDesc {
	b := a.Box
	d := adaptDesc{mode: a.Mode, minX: b[0] - 2, minY: b[1], minZ: b[2] - 2, maxX: b[3] + 2, maxY: b[4], maxZ: b[5] + 2,
		baseY: b[1] + a.DeltaY, minW: a.MinW, maxW: a.MaxW}
	d.xCenter = (b[3]-b[0]+5)/2 + b[0] - 2
	d.zCenter = (b[5]-b[2]+5)/2 + b[2] - 2
	d.maxRadius = float32(int32(max(float32(b[3]-b[0]+5)*0.5, float32(b[5]-b[2]+5)*0.5)))
	// The kernel is zero outside -12..11 on each axis.
	d.reach0X, d.reach1X = d.minX-12, d.maxX+12
	d.reach0Z, d.reach1Z = d.minZ-12, d.maxZ+12
	d.reach0Y, d.reach1Y = d.baseY-12, max(d.maxY, d.baseY)+11
	return d
}

// beardKernel is the 24^3 kernel (TA "Kernel"), float with the fast inverse square root.
var beardKernel = sync.OnceValue(func() *[24 * 24 * 24]float32 {
	var k [24 * 24 * 24]float32
	for dz := -12; dz < 12; dz++ {
		for dx := -12; dx < 12; dx++ {
			for dy := -12; dy < 12; dy++ {
				yp := float32(float64(dy) + 0.5)
				d2 := float32(dx*dx + dz*dz)
				e := float32(math.Pow(2.7182817, float64(-(yp*yp*0.0625 + d2*0.0625))))
				s := yp*yp*0.5 + d2*0.5
				i := math.Float32frombits(0x5f3759df - math.Float32bits(s)>>1)
				i = i * (1.5 - s*0.5*i*i)
				k[(dz+12)*576+(dx+12)*24+(dy+12)] = -yp * i * 0.5 * e
			}
		}
	}
	return &k
})

func kernelAt(k *[24 * 24 * 24]float32, dx, dy, dz int) float32 {
	if dx < -12 || dx > 11 || dy < -12 || dy > 11 || dz < -12 || dz > 11 {
		return 0
	}
	return k[(dz+12)*576+(dx+12)*24+(dy+12)]
}

// terrainAdapt is a chunk's descriptions, nil when no piece is near.
type terrainAdapt []adaptDesc

// terrainAdaptation gathers chunk (cx, cz)'s descriptions into the scratch.
func (g *Generator) terrainAdaptation(s *chunkScratch, cx, cz int) terrainAdapt {
	ta, ok := g.structs.(TerrainAdjuster)
	if !ok {
		return nil
	}
	s.adjustIn = ta.TerrainAdjustments(cx, cz, s.adjustIn[:0])
	if len(s.adjustIn) == 0 {
		return nil
	}
	s.adapt = s.adapt[:0]
	for _, a := range s.adjustIn {
		s.adapt = append(s.adapt, newAdaptDesc(a))
	}
	return s.adapt
}

// touchesCell says whether any description can change a block of the noise cell at (x, y, z), whose
// shortcuts (all solid, all sky) then do not hold.
func (t terrainAdapt) touchesCell(x, y, z int) bool {
	for i := range t {
		d := &t[i]
		if x+cellXZ-1 >= d.reach0X && x <= d.reach1X && z+cellXZ-1 >= d.reach0Z && z <= d.reach1Z && y+cellY-1 >= d.reach0Y && y <= d.reach1Y {
			return true
		}
	}
	return false
}

// at is the contribution added to a block's density (TA "Contribution").
func (t terrainAdapt) at(x, y, z int) float64 {
	if t == nil {
		return 0
	}
	k := beardKernel()
	var sum float32
	for i := range t {
		d := &t[i]
		if x < d.reach0X || x > d.reach1X || z < d.reach0Z || z > d.reach1Z || y < d.reach0Y || y > d.reach1Y {
			continue
		}
		dx := max(x-d.maxX, d.minX-x, 0)
		dz := max(z-d.maxZ, d.minZ-z, 0)
		switch d.mode {
		case AdaptBeardAndShave:
			yc := max(d.baseY, y)
			yc = min(yc, d.maxY)
			fx, fy, fz := float32(x-d.xCenter), float32(y-yc), float32(z-d.zCenter)
			dist := float32(math.Sqrt(float64(fz*fz + fx*fx + fy*fy)))
			w := d.minW
			if y > d.baseY {
				w = d.maxW
			}
			r := w + d.maxRadius
			if r > 0 && dist <= r {
				sum += kernelAt(k, dx, y-yc, dz) * (1.5 - dist/r)
			}
		case AdaptBeard:
			sum += kernelAt(k, dx, y-d.baseY, dz) * 0.8
		}
	}
	return float64(sum)
}
