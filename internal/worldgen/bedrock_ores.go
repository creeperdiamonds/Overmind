package worldgen

import "math"

// Bedrock's underground pass for the Bedrock profile (Task 4e): the ore and rock rules and
// OreFeature::place, in our own Go from NOTES-bedrock-exact sections 3, 4, 4b, P1 and R9.
//
// The rule parameters below are transcribed from the vanilla behaviour pack's feature_rules and
// features of BDS 1.26.60.6 (newest pack's copy of each), as the study read them; the order is the
// study's model of the pass's rule map (section 3, tools/rule_order.json, 50 of 50 pair checks).
// Rules whose iterations are 0 or whose filter matches no biome are left out: each rule draws from its
// own seed, so they change nothing. The biome lists are the rules' tag filters worked out over the
// vanilla biome tags, in Bedrock's biome names.

// oreRule is one replace rule of an ore feature: the block it places and the blocks it may replace.
type oreRule struct{ places, mayReplace string }

// undergroundRow is a rule as transcribed.
type undergroundRow struct {
	ident      string
	iterations int
	num, den   int
	order      string
	axes       [3]bAxis
	count      int
	discard    float32
	replace    []oreRule
	biomes     string
}

// Biome lists of the filters. "all" is every Overworld biome of our table: the rules' filter is the
// tag overworld or overworld_generation, which every one of them has.
const (
	bMountainsOrHills = "snowy_slopes extreme_hills_mutated grove extreme_hills extreme_hills_plus_trees meadow jagged_peaks stony_peaks cherry_grove frozen_peaks"
	bMountains        = "snowy_slopes grove meadow jagged_peaks stony_peaks cherry_grove frozen_peaks"
	bMesa             = "mesa mesa_plateau_stone mesa_bryce"
	// The copper rule's filter is overworld and not dripstone_caves.
	bNotDripstone = "mushroom_island deep_frozen_ocean frozen_ocean deep_cold_ocean cold_ocean deep_ocean ocean deep_lukewarm_ocean lukewarm_ocean warm_ocean stone_beach swampland mangrove_swamp snowy_slopes ice_plains cold_beach extreme_hills_mutated grove extreme_hills cold_taiga extreme_hills_plus_trees taiga plains meadow beach forest redwood_taiga_mutated flower_forest birch_forest roofed_forest pale_garden savanna_plateau savanna jungle mesa desert mesa_plateau_stone jagged_peaks stony_peaks frozen_river river ice_plains_spikes mega_taiga sunflower_plains birch_forest_mutated jungle_edge bamboo_jungle mesa_bryce savanna_mutated cherry_grove frozen_peaks lush_caves deep_dark"
)

var (
	uni16    = bAxis{bUniform, 0, 16}
	stoneOre = func(ore string) []oreRule {
		return []oreRule{{ore + "_ore", "stone"}, {"deepslate_" + ore + "_ore", "deepslate"}}
	}
)

// undergroundRows is the underground pass in the pass's order (section 3). Each row: identifier,
// iterations, scatter chance, coordinate order, x/y/z distributions, ore count, air-exposure discard,
// replace rules, biomes.
var undergroundRows = []undergroundRow{
	{"minecraft:overworld_underground_redstone_ore_lower_feature", 8, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, -96, -32}, uni16}, 8, 0, stoneOre("redstone"), "all"},
	{"minecraft:overworld_underground_lapis_ore_feature", 2, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, -32, 32}, uni16}, 7, 0, stoneOre("lapis"), "all"},
	{"minecraft:overworld_underground_iron_ore_small_feature", 10, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, -64, 72}, uni16}, 4, 0, stoneOre("iron"), "all"},
	{"minecraft:overworld_underground_iron_ore_middle_feature", 10, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, -24, 56}, uni16}, 10, 0, stoneOre("iron"), "all"},
	{"minecraft:overworld_underground_gravel_ore_feature", 14, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, -64, 320}, uni16}, 33, 0, []oreRule{{"gravel", "stone deepslate"}}, "all"},
	{"minecraft:overworld_underground_granite_upper_feature", 1, 1, 6, "zyx", [3]bAxis{uni16, {bUniform, 64, 128}, uni16}, 64, 0, []oreRule{{"granite", "stone"}}, "all"},
	{"minecraft:overworld_underground_redstone_ore_feature", 4, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, -64, 15}, uni16}, 8, 0, stoneOre("redstone"), "all"},
	{"minecraft:overworld_underground_emerald_ore_feature", 100, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, -16, 480}, uni16}, 3, 0, stoneOre("emerald"), bMountainsOrHills},
	// The dirt rule's feature has no file: it acts as an ore of 33 replacing stone (section 4b).
	{"minecraft:overworld_underground_dirt_feature", 7, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, 0, 160}, uni16}, 33, 0, []oreRule{{"dirt", "stone"}}, "all"},
	{"minecraft:overworld_underground_tuff_feature", 2, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, -64, 0}, uni16}, 64, 0, []oreRule{{"tuff", "stone deepslate"}}, "all"},
	{"minecraft:overworld_underground_granite_lower_feature", 2, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, 0, 60}, uni16}, 64, 0, []oreRule{{"granite", "stone"}}, "all"},
	{"minecraft:overworld_underground_diorite_upper_feature", 1, 1, 6, "zyx", [3]bAxis{uni16, {bUniform, 64, 128}, uni16}, 64, 0, []oreRule{{"diorite", "stone"}}, "all"},
	{"minecraft:overworld_underground_diorite_lower_feature", 2, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, 0, 60}, uni16}, 64, 0, []oreRule{{"diorite", "stone"}}, "all"},
	{"minecraft:overworld_underground_diamond_ore_large_feature", 1, 1, 9, "zyx", [3]bAxis{uni16, {bTriangle, -144, 16}, uni16}, 12, 0.7, stoneOre("diamond"), "all"},
	{"minecraft:overworld_underground_copper_ore_feature", 16, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, -16, 112}, uni16}, 10, 0, stoneOre("copper"), bNotDripstone},
	{"minecraft:overworld_underground_diamond_ore_buried_feature", 4, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, -144, 16}, uni16}, 8, 1, stoneOre("diamond"), "all"},
	{"minecraft:overworld_underground_lapis_ore_buried_feature", 4, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, -64, 64}, uni16}, 7, 1, stoneOre("lapis"), "all"},
	{"minecraft:overworld_underground_gold_ore_feature", 4, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, -64, 32}, uni16}, 9, 0.5, stoneOre("gold"), "all"},
	{"minecraft:overworld_underground_coal_ore_upper_feature", 30, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, 136, 320}, uni16}, 17, 0, stoneOre("coal"), "all"},
	{"minecraft:overworld_underground_andesite_upper_feature", 1, 1, 6, "zyx", [3]bAxis{uni16, {bUniform, 64, 128}, uni16}, 64, 0, []oreRule{{"andesite", "stone"}}, "all"},
	{"minecraft:mountains_underground_coal_ore_feature", 20, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, 128, 256}, uni16}, 17, 0.5, stoneOre("coal"), bMountains},
	{"minecraft:overworld_underground_andesite_lower_feature", 2, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, 0, 60}, uni16}, 64, 0, []oreRule{{"andesite", "stone"}}, "all"},
	{"minecraft:overworld_underground_coal_ore_lower_feature", 20, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, 0, 192}, uni16}, 17, 0.5, stoneOre("coal"), "all"},
	{"minecraft:mesa_underground_gold_ore_feature", 50, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, 32, 256}, uni16}, 9, 0, stoneOre("gold"), bMesa},
	{"minecraft:dripstone_caves_underground_copper_ore_feature", 16, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, -16, 112}, uni16}, 20, 0, stoneOre("copper"), "dripstone_caves"},
	{"minecraft:overworld_underground_diamond_ore_feature", 7, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, -144, 16}, uni16}, 4, 0.5, stoneOre("diamond"), "all"},
	{"minecraft:overworld_underground_gold_ore_lower_feature", 1, 1, 2, "zyx", [3]bAxis{uni16, {bUniform, -64, -48}, uni16}, 9, 0.5, stoneOre("gold"), "all"},
	{"minecraft:overworld_underground_iron_ore_upper_feature", 90, 0, 0, "zyx", [3]bAxis{uni16, {bTriangle, 80, 384}, uni16}, 10, 0, stoneOre("iron"), "all"},
	{"minecraft:lush_caves_underground_clay_ore_feature", 46, 0, 0, "xyz", [3]bAxis{uni16, {bUniform, -64, 256}, uni16}, 33, 0, []oreRule{{"clay", "stone"}}, "lush_caves"},
	{"minecraft:overworld_underground_diamond_ore_feature_square", 2, 0, 0, "zyx", [3]bAxis{uni16, {bUniform, -64, -4}, uni16}, 4, 0.5, stoneOre("diamond"), "all"},
}

// bedrockUnderground is the pass built from the rows.
var bedrockUnderground = func() []*bedrockRule {
	var out []*bedrockRule
	for _, row := range undergroundRows {
		f := newBedrockOre(row.count, row.discard, row.replace)
		out = append(out, &bedrockRule{ident: row.ident, hash: uint32(fnv1Hash(row.ident)), iterations: row.iterations,
			num: row.num, den: row.den, order: evalOrder(row.order), axes: row.axes, biomes: biomeBits(row.biomes), place: f.place})
	}
	return out
}()

// bedrockOreTarget is a replace rule in our states.
type bedrockOreTarget struct {
	state bs
	may   map[uint16]bool
}

type bedrockOre struct {
	count   int
	discard float32
	targets []bedrockOreTarget
}

func newBedrockOre(count int, discard float32, rules []oreRule) *bedrockOre {
	o := &bedrockOre{count: count, discard: discard}
	for _, r := range rules {
		o.targets = append(o.targets, bedrockOreTarget{stateOf(r.places), blockSet(r.mayReplace)})
	}
	return o
}

// oreSphere is one sphere of a blob: its centre, squared radius and radius.
type oreSphere struct{ x, y, z, r2, h float32 }

const bedrockPi = float32(math.Pi)

// sinIndex reads the sine table at a float angle already scaled to table units.
func sinIndex(v float32) float32 { return bedrockSin[int32(v)&0xffff] }

// place is OreFeature::place (section 4).
func (o *bedrockOre) place(d *decoration, r *bmt, p pos) {
	active := o.spheres(r, p, &d.ore.bspheres, &d.ore.bactive)
	forBlobBlocks(active, func(x, y, z int) { o.placeBlock(d, r, x, y, z) })
}

// spheres draws a blob's spheres and drops those a later one holds (section 4). All arithmetic is
// single precision, in the order the notes give; each product is its own float32 so no platform
// fuses a multiply-add. The two slices are scratch.
func (o *bedrockOre) spheres(r *bmt, p pos, all, act *[]oreSphere) []oreSphere {
	countf := float32(o.count)
	inv := float32(1) / countf
	ang := float32(float32(r.nextFloat()*bedrockPi) * 10430.378)
	rr := float32(countf * 0.125)
	sx := float32(sinIndex(ang) * rr)
	cz := float32(rr * sinIndex(float32(ang+16384)))
	fx, fy, fz := float32(p.x), float32(p.y), float32(p.z)
	x0 := float32(sx+8) + fx
	z0 := float32(cz+8) + fz
	x1 := float32(8-sx) + fx
	z1 := fz + float32(8-cz)
	y0 := float32(float32(r.nextIntN(3))-2) + fy
	y1 := float32(float32(r.nextIntN(3))-2) + fy
	dx, dy, dz := x1-x0, y1-y0, z1-z0
	k := float32(inv * bedrockPi)
	spheres := (*all)[:0]
	for i := range o.count {
		fi := float32(i)
		t := float32(inv * fi)
		c := oreSphere{x: float32(t*dx) + x0, y: float32(t*dy) + y0, z: float32(float32(dz*fi)*inv) + z0}
		rnd := r.nextFloat()
		sv := sinIndex(float32(float32(fi*k) * 10430.378))
		a := float32(float32(sv+1) * rnd)
		a = float32(a * countf)
		a = float32(a * 0.0625)
		rad := float32(float32(a+1) * 0.5)
		c.r2 = float32(rad * rad)
		c.h = float32(math.Sqrt(float64(c.r2)))
		spheres = append(spheres, c)
	}
	active := (*act)[:0]
	for i, a := range spheres {
		inside := false
		for _, b := range spheres[i+1:] {
			if a.h > b.h {
				continue
			}
			ddz, ddx, ddy := b.z-a.z, b.x-a.x, b.y-a.y
			sum := float32(float32(ddz*ddz)+float32(ddx*ddx)) + float32(ddy*ddy)
			if float32(float32(math.Sqrt(float64(sum)))+a.h) < b.h {
				inside = true
				break
			}
		}
		if !inside {
			active = append(active, a)
		}
	}
	*all, *act = spheres, active
	return active
}

// forBlobBlocks visits every block inside a blob's spheres in Bedrock's order: x outer, z, then y.
func forBlobBlocks(active []oreSphere, visit func(x, y, z int)) {
	if len(active) == 0 {
		return
	}
	lo := [3]int{math.MaxInt32, math.MaxInt32, math.MaxInt32}
	hi := [3]int{math.MinInt32, math.MinInt32, math.MinInt32}
	for _, a := range active {
		for i, v := range [3]float32{a.x, a.y, a.z} {
			lo[i] = min(lo[i], int(math.Floor(float64(float32(v-a.h)))))
			hi[i] = max(hi[i], int(math.Floor(float64(float32(v+a.h)))))
		}
	}
	for bx := lo[0]; bx <= hi[0]; bx++ {
		px := float32(float32(bx) + 0.5)
		for bz := lo[2]; bz <= hi[2]; bz++ {
			pz := float32(float32(bz) + 0.5)
			for by := lo[1]; by <= hi[1]; by++ {
				py := float32(float32(by) + 0.5)
				for _, a := range active {
					ddz, ddx, ddy := pz-a.z, px-a.x, py-a.y
					d2 := float32(float32(ddz*ddz)+float32(ddx*ddx)) + float32(ddy*ddy)
					if d2 < a.r2 {
						visit(bx, by, bz)
						break
					}
				}
			}
		}
	}
}

// placeBlock is the replace step for one block of a blob (section 4).
func (o *bedrockOre) placeBlock(d *decoration, r *bmt, x, y, z int) {
	if !d.canWrite(x, y, z) {
		return
	}
	cur := d.block(x, y, z)
	for _, t := range o.targets {
		if !t.may[stateBlock[cur]] || cur == t.state {
			continue
		}
		if o.discard > 0 {
			kept := false
			if o.discard < 1 {
				kept = r.nextFloat() >= o.discard
			}
			if !kept && d.exposedToAir(x, y, z) {
				continue
			}
		}
		d.set(x, y, z, t.state)
		return
	}
}

// exposedToAir says whether one of the six neighbours is air. A neighbour outside the world, or in a
// chunk a source cannot read, is not.
func (d *decoration) exposedToAir(x, y, z int) bool {
	for _, v := range dirVec {
		nx, ny, nz := x+v.x, y+v.y, z+v.z
		if ny < minY || ny > maxY || d.chunkAt(nx>>4, nz>>4) == nil {
			continue
		}
		if d.block(nx, ny, nz).isAir() {
			return true
		}
	}
	return false
}
