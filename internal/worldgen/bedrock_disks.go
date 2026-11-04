package worldgen

// Bedrock's sand, gravel and clay disks for the Bedrock profile (Task 4e), in our own Go from
// NOTES-bedrock-exact R5. The features are registered in code (no file): sand_feature = SandFeature
// (sand, radius 7), gravel_feature = SandFeature(gravel, radius 6), clay_feature = ClayFeature(radius 4).
// Their rules are read from the install; y is query.above_top_solid of the column.

// aboveTopSolid is query.above_top_solid: one above the highest block that is not air, a fluid, leaves
// or a plant that can be replaced (as the study's simulator reads it).
func (d *decoration) aboveTopSolid(x, z int) int {
	l := d.chunkAt(x>>4, z>>4)
	if l == nil {
		return minY
	}
	for y := maxY; y >= minY; y-- {
		s := l.b.b[cellIndex(x&15, y, z&15)]
		if s.isAir() || s.hasFluid() || s.tagged(tagLeaves) || s.has(fReplaceable) && !s.has(fMotion) {
			continue
		}
		return y + 1
	}
	return minY
}

// sandDisk is SandFeature (R5).
type sandDisk struct {
	block  bs
	radius int32
}

var diskReplaces = blockSet("dirt coarse_dirt grass_block")

func (f sandDisk) place(d *decoration, r *bmt, p pos) {
	if !d.at(p).has(fWater) {
		return
	}
	n := int(r.nextIntN(f.radius-2)) + 2
	for x := p.x - n; x <= p.x+n; x++ {
		for z := p.z - n; z <= p.z+n; z++ {
			if (x-p.x)*(x-p.x)+(z-p.z)*(z-p.z) > n*n {
				continue
			}
			for y := p.y - 2; y <= p.y+2; y++ {
				if diskReplaces[stateBlock[d.block(x, y, z)]] && d.canWrite(x, y, z) {
					d.set(x, y, z, f.block)
				}
			}
		}
	}
}

var (
	clayReplaces = blockSet("dirt coarse_dirt mud")
	clayState    = stateOf("clay")
)

// clayDisk is ClayFeature with radius 4 (R5).
func clayDisk(d *decoration, r *bmt, p pos) {
	if at := d.at(p); !at.has(fWater) && at != clayState {
		return
	}
	n := int(r.nextIntN(4-2)) + 2
	for x := p.x - n; x <= p.x+n; x++ {
		for z := p.z - n; z <= p.z+n; z++ {
			if (x-p.x)*(x-p.x)+(z-p.z)*(z-p.z) > n*n {
				continue
			}
			for y := p.y - 1; y <= p.y+1; y++ {
				if clayReplaces[stateBlock[d.block(x, y, z)]] && d.canWrite(x, y, z) {
					d.set(x, y, z, clayState)
				}
			}
		}
	}
}
