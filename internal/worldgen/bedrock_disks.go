package worldgen

// Bedrock's sand, gravel and clay disks for the Bedrock profile (Task 4e), in our own Go from
// NOTES-bedrock-exact R5. The features are registered in code (no JSON): sand_feature = SandFeature
// (sand, radius 7), gravel_feature = SandFeature(gravel, radius 6), clay_feature = ClayFeature(radius 4).
// The rules are the vanilla behaviour pack's surface_pass rules of BDS 1.26.60.6 (the clay rule is in
// the server's own definitions only): x and z uniform over the chunk, y = query.above_top_solid of the
// column, no coordinate order given (the default, x then z then y). The sand rule's identifier really
// has the doubled prefix, and its seed hashes it so.

// bedrockSurfacePass is the part of the surface pass written so far, in the pass's order (section 3's
// model puts gravel before sand; the clay rule is not in the model, and goes last).
var bedrockSurfacePass = []*bedrockRule{
	diskRule("minecraft:overworld_surface_gravel_feature", 1, "not_swamp", sandDisk{block: sGravel, radius: 6}.place),
	diskRule("minecraft:minecraft:overworld_surface_sand_feature", 3, "not_swamp", sandDisk{block: sSand, radius: 7}.place),
	diskRule("minecraft:overworld_surface_clay_feature", 1, "all", clayDisk),
}

// diskRule makes a disk rule. y is worked out from the column after x and z (aboveTopSolidAxis).
func diskRule(ident string, iterations int, biomes string, place func(d *decoration, r *bmt, p pos)) *bedrockRule {
	set := biomeBits("all")
	if biomes == "not_swamp" {
		for _, n := range []string{"swampland", "mangrove_swamp"} {
			for i := range biomeTableNames {
				if biomeRowDefs[i].String() == n {
					set[i>>6] &^= 1 << (i & 63)
				}
			}
		}
	}
	return &bedrockRule{ident: ident, hash: uint32(fnv1Hash(ident)), iterations: iterations, order: evalOrder("xzy"),
		axes: [3]bAxis{uni16, {kind: bAboveTopSolid}, uni16}, biomes: set, place: place}
}

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
