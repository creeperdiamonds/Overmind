package worldgen

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Bedrock's code-registered features (no file) and the lush-cave kinds, for the Bedrock profile, in our
// own Go from the study of BDS 1.26.60.6: re/phase4/feats (FEATS-pseudocode.md, p4_*.py), R5, T1 and
// re/phase6/LUSH-pseudocode.md. Block survival rules are the study's approximations by name.

// codeFeature gives a feature registered in code: ok false when the name is not one; an error when it
// is one not written yet.
func (fc *featCompiler) codeFeature(id string) (bfeat, bool, error) {
	if name, ok := strings.CutPrefix(id, "minecraft:legacy:"); ok {
		if f, ok, err := fc.legacyTreeFeature(name); ok {
			return f, true, err
		}
		switch name {
		case "forest_foliage_feature":
			return codeFeat(forestFoliage{n: -3}.place), true, nil
		case "flower_forest_foliage_feature":
			return codeFeat(forestFoliage{n: -1}.place), true, nil
		case "flower_forest_flower_feature":
			return codeFeat(flowerForestFlower), true, nil
		case "forest_flower_feature":
			return legacyFlower(1), true, nil
		case "overworld_flower_feature":
			return legacyFlower(2), true, nil
		case "swamp_flower_feature":
			return legacyFlower(4), true, nil
		case "forest_rock_feature":
			return codeFeat(forestRock), true, nil
		}
		return nil, true, unimpl("legacy feature %s", name)
	}
	if _, ok := fc.bd.features[id]; ok {
		return nil, false, nil
	}
	switch id {
	case "minecraft:pointed_dripstone_feature":
		return codeFeat(bedrockPointedDripstone), true, nil
	case "minecraft:dripstone_cluster_feature":
		return codeFeat(bedrockDripstoneCluster), true, nil
	case "minecraft:large_dripstone_feature":
		return codeFeat(bedrockLargeDripstone), true, nil
	case "minecraft:dirt_feature":
		// Measured, not read from the code (ruling 39): BDS's built-in dirt rule places like an ore of
		// count 33 over stone with no air discard, 144/144 first blobs matching (NOTES-bedrock-exact 4b).
		o := newBedrockOre(33, 0, []oreRule{{"dirt", "stone"}})
		fc.bd.lastOre = o
		return voidFeat(o.place), true, nil
	case "minecraft:sand_feature":
		return voidFeat(sandDisk{block: sSand, radius: 7}.place), true, nil
	case "minecraft:gravel_feature":
		return voidFeat(sandDisk{block: sGravel, radius: 6}.place), true, nil
	case "minecraft:clay_feature":
		return voidFeat(clayDisk), true, nil
	case "minecraft:huge_mushroom_feature":
		return codeFeat(bHugeMushroom{3}.place), true, nil
	case "minecraft:huge_red_mushroom_feature":
		return codeFeat(bHugeMushroom{1}.place), true, nil
	case "minecraft:huge_brown_mushroom_feature":
		return codeFeat(bHugeMushroom{0}.place), true, nil
	case "minecraft:leaf_litter_feature":
		return flowerBed("leaf_litter[facing=%s,segment_amount=%d]"), true, nil
	case "minecraft:wildflowers_feature":
		return flowerBed("wildflowers[facing=%s,flower_amount=%d]"), true, nil
	case "minecraft:pink_petals_feature":
		return flowerBed("pink_petals[facing=%s,flower_amount=%d]"), true, nil
	case "minecraft:brown_mushroom_feature":
		return smallMushroom(stateOf("brown_mushroom")), true, nil
	case "minecraft:red_mushroom_feature":
		return smallMushroom(stateOf("red_mushroom")), true, nil
	case "minecraft:kelp_feature":
		return codeFeat(bKelp), true, nil
	case "minecraft:seagrass_feature":
		return bSeagrassFeat{1, 0.6}, true, nil
	case "minecraft:shortest_seagrass_feature":
		return bSeagrassFeat{1, 0.3}, true, nil
	case "minecraft:shorter_seagrass_feature":
		return bSeagrassFeat{1, 0.4}, true, nil
	case "minecraft:taller_seagrass_feature":
		return bSeagrassFeat{1, 0.8}, true, nil
	case "minecraft:pickle_feature":
		return codeFeat(seaPickle), true, nil
	case "minecraft:reeds_feature":
		return codeFeat(reedsFeature), true, nil
	case "minecraft:pumpkin_feature":
		return centralSpiked{stateOf("pumpkin"), 96, false}, true, nil
	case "minecraft:sweet_berry_bush_feature":
		return centralSpiked{stateOf("sweet_berry_bush[age=3]"), 1, true}, true, nil
	case "minecraft:cactus_feature":
		return codeFeat(cactusFeature), true, nil
	case "minecraft:dead_bush_feature":
		return codeFeat(deadBushFeature), true, nil
	case "minecraft:melon_feature":
		return codeFeat(melonFeature), true, nil
	case "minecraft:waterlily_feature":
		return codeFeat(waterlilyFeature), true, nil
	case "minecraft:bamboo_feature":
		return codeFeat(bBamboo), true, nil
	case "minecraft:moss_vegetation_feature":
		return vegetationFeat{mossVegetation}, true, nil
	case "minecraft:pale_moss_vegetation_feature":
		return vegetationFeat{paleMossVegetation}, true, nil
	case "minecraft:dripleaf_feature":
		f := &dripleafFeat{}
		for i, n := range [4]string{"south", "west", "north", "east"} {
			c, err := fc.feature("minecraft:big_dripleaf_" + n + "_feature")
			if err != nil {
				return nil, true, err
			}
			f.big[i] = c
		}
		return f, true, nil
	case "minecraft:vines_single_face_feature":
		return codeFeat(vinesSingleFace), true, nil
	case "minecraft:azalea_root_system_feature":
		c, err := fc.feature("minecraft:azalea_tree_feature")
		if err != nil {
			return nil, true, err
		}
		return &trailToSurface{tree: c}, true, nil
	}
	return nil, false, nil
}

// jitter is two raw words masked and subtracted, first minus second.
func jitter(r *bmt, mask uint32) int { a := int(r.u32() & mask); return a - int(r.u32()&mask) }

// ---------------------------------------------------------------- flower beds, small mushrooms

// flowerBed is FlowerBedFeature (leaf litter, wildflowers, pink petals): 2 x nextInt(4) always, then
// placed on air over a solid block that is not a log or a bee nest.
func flowerBed(format string) bfeat {
	var states [4][4]bs
	for f := range 4 {
		for n := range 4 {
			states[f][n] = stateOf(fmt.Sprintf(format, cardinals[f], n+1))
		}
	}
	return codeFeat(func(d *decoration, r *bmt, p pos) bool {
		a, b := int(r.nextIntN(4)), int(r.nextIntN(4))
		if p.y < minY || p.y > maxY || !d.at(p).isAir() {
			return false
		}
		below := d.at(p.add(0, -1, 0))
		if !mushSolid(below) || below.tagged(tagLogs) || bname(below) == bnBeeNest {
			return false
		}
		return d.bput(p, states[a][b])
	})
}

var bnBeeNest = internBName("bee_nest")

func smallMushroom(s bs) bfeat {
	return codeFeat(func(d *decoration, r *bmt, p pos) bool {
		if p.y < minY || p.y > maxY || !d.at(p).isAir() {
			return false
		}
		if mushSolid(d.at(p.add(0, -1, 0))) {
			return d.bput(p, s)
		}
		return false
	})
}

// ---------------------------------------------------------------- legacy plants

// forestFoliage is LegacyForestFoliageFeature (0x53fc160): n = nextInt(5) + offset double plants, each
// tried five times at a random column and height (DoublePlant 0x5162ad0).
type forestFoliage struct{ n int }

var forestFoliageKinds = [3]string{"lilac", "rose_bush", "peony"}

func (f forestFoliage) place(d *decoration, r *bmt, p pos) bool {
	n := int(r.nextIntN(5)) + f.n
	for range max(0, n) {
		k := forestFoliageKinds[r.nextIntN(3)]
		lo, hi := stateOf(k+"[half=lower]"), stateOf(k+"[half=upper]")
		for range 5 {
			dx, dz := int(r.nextIntN(16)), int(r.nextIntN(16))
			h := d.bedrockHeight(p.x+dx, p.z+dz)
			if h+32 > 0 {
				yy := int(r.nextIntN(int32(h + 32)))
				doublePlant(d, r, pos{p.x + dx, yy, p.z + dz}, lo, hi)
			}
		}
	}
	return true
}

// doublePlant is the DoublePlant helper (0x5162ad0): nothing if the highest heightmap of the 15 x 15
// columns around is below y-4, else 64 tries of six raw words.
func doublePlant(d *decoration, r *bmt, p pos, lo, hi bs) bool {
	top := 0
	for dx := -7; dx <= 7; dx++ {
		for dz := -7; dz <= 7; dz++ {
			if d.chunkAt((p.x+dx)>>4, (p.z+dz)>>4) != nil {
				top = max(top, d.bedrockHeight(p.x+dx, p.z+dz))
			}
		}
	}
	if top < p.y-4 {
		return false
	}
	ok := false
	for range 64 {
		u1, u2, u3, u4, u5, u6 := r.u32(), r.u32(), r.u32(), r.u32(), r.u32(), r.u32()
		q := pos{p.x + int(u5&7) - int(u6&7), p.y + int(u3&3) - int(u4&3), p.z + int(u1&7) - int(u2&7)}
		if q.y < minY || q.y+1 > maxY {
			continue
		}
		if d.at(q).isAir() && bnSoil.has(bname(d.at(q.add(0, -1, 0)))) && d.at(q.add(0, 1, 0)).isAir() {
			d.bput(q, lo)
			d.bput(q.add(0, 1, 0), hi)
			ok = true
		}
	}
	return ok
}

// legacyFlower is LegacyFlowerFeature (0x53fbc30) for the kinds without noise; one try in worlds of
// 1.21.60 and later.
func legacyFlower(kind int) bfeat {
	dandelion, poppy, lily, orchid := stateOf("dandelion"), stateOf("poppy"), stateOf("lily_of_the_valley"), stateOf("blue_orchid")
	return codeFeat(func(d *decoration, r *bmt, p pos) bool {
		if p.y < minY || p.y > maxY || !d.at(p).isAir() {
			return false
		}
		var s bs
		switch kind {
		case 1:
			s = dandelion
			if r.nextBool() {
				s = lily
			}
		case 4:
			s = orchid
		default:
			s = poppy
			if r.nextIntN(3) == 0 {
				s = dandelion
			}
		}
		if d.at(p).isAir() && bnSoil.has(bname(d.at(p.add(0, -1, 0)))) {
			d.bput(p, s)
		}
		jitter(r, 7)
		jitter(r, 3)
		jitter(r, 7)
		return true
	})
}

// flowerForestFlowers is type 0's list (T19): the flower is picked by query.noise's octave.
var flowerForestFlowers = func() (l []bs) {
	for _, n := range []string{"dandelion", "poppy", "allium", "azure_bluet", "red_tulip", "orange_tulip", "white_tulip", "pink_tulip",
		"oxeye_daisy", "cornflower", "lily_of_the_valley"} {
		l = append(l, laterState(n))
	}
	return l
}()

// flowerForestFlower is LegacyFlowerFeature type 0 (T19, place 0x53fbc30, block choice 0xd901ce0): the
// flower from Random(2345)'s simplex at (x, z) / 48 with no offsets, mapped to [0, 1) and clamped
// below 1; one try in 1.26 worlds, then the six jitter words.
func flowerForestFlower(d *decoration, r *bmt, p pos) bool {
	v := float32(float32(queryNoiseOctave.at2(float32(p.x)/48, float32(p.z)/48)*70+1) * 0.5)
	c := min(max(v, 0), 0.9999)
	s := flowerForestFlowers[int(float32(c*float32(len(flowerForestFlowers))))]
	ok := false
	if p.y >= minY && p.y < maxY && d.at(p).isAir() && bnSoil.has(bname(d.at(p.add(0, -1, 0)))) {
		ok = d.bput(p, s)
	}
	jitter(r, 7)
	jitter(r, 3)
	jitter(r, 7)
	return ok
}

var (
	sMossyCobble = laterState("mossy_cobblestone")
	bnBlobGround = bnameSetOf("grass_block stone dirt coarse_dirt")
)

// forestRock is BlockBlobFeature (0x4ba10a0): three mossy cobblestone blobs of radius 0.
func forestRock(d *decoration, r *bmt, p pos) bool {
	x, y, z := p.x, p.y, p.z
	for y > minY+3 {
		if bl := d.block(x, y-1, z); !bl.isAir() && bnBlobGround.has(bname(bl)) {
			break
		}
		y--
	}
	if y <= minY+3 {
		return false
	}
	cx, cy, cz := x, y, z
	for range 3 {
		xr, yr, zr := int(r.nextIntN(2)), int(r.nextIntN(2)), int(r.nextIntN(2))
		f := float32(float32(xr+yr+zr)*0.333) + 0.5
		f2 := f * f
		for X := cx - xr; X <= cx+xr; X++ {
			for Y := cy - yr; Y <= cy+yr; Y++ {
				for Z := cz - zr; Z <= cz+zr; Z++ {
					if float32((X-cx)*(X-cx)+(Y-cy)*(Y-cy)+(Z-cz)*(Z-cz)) <= f2 && Y >= minY && Y <= maxY {
						d.bput(pos{X, Y, Z}, sMossyCobble)
					}
				}
			}
		}
		cx += -1 + int(r.nextIntN(2))
		cy -= int(r.nextIntN(2))
		cz += -1 + int(r.nextIntN(2))
	}
	return true
}

// ---------------------------------------------------------------- ocean

var (
	sKelpTop   = laterState("kelp")
	sKelpPlant = laterState("kelp_plant")
	sSeagrass  = laterState("seagrass")
	sTallSeaLo = laterState("tall_seagrass[half=lower]")
	sTallSeaHi = laterState("tall_seagrass[half=upper]")
	sPickle    = laterState("sea_pickle")
	bnKelp     = internBName("kelp")
	bnPickle   = internBName("sea_pickle")
	bnNoKelpOn = bnameSetOf("magma soul_sand oak_fence spruce_fence birch_fence jungle_fence acacia_fence dark_oak_fence mangrove_fence cherry_fence pale_oak_fence bamboo_fence nether_brick_fence crimson_fence warped_fence")
)

// bKelp is KelpFeature::place (0x5170f40): one try in worlds of 1.21.60 and later. A try that
// finds no place ends before its jitter draws.
func bKelp(d *decoration, r *bmt, p pos) bool {
	x, z := p.x, p.z
	py := p.y
	if !bSolid(d.block(x, py-1, z)) {
		for {
			if bname(d.block(x, py, z)) == bnKelp || py <= minY {
				break
			}
			py--
			if bSolid(d.block(x, py-1, z)) {
				break
			}
		}
	}
	if !bSolid(d.block(x, py-1, z)) {
		return false
	}
	sx, sy, sz := x, py, z
	for sy <= maxY {
		b := d.block(sx, sy, sz)
		if b.isAir() || bWaterMat(b) {
			break
		}
		sy++
	}
	b := d.block(sx, sy, sz)
	if sy > maxY || b.isAir() || bWaterMat(d.block(sx, sy-1, sz)) || bname(b) == bnKelp {
		return true
	}
	n := 0
	for n < 26 && sy+n <= maxY && bWaterMat(d.block(sx, sy+n, sz)) {
		n++
	}
	if n < 3 {
		return true
	}
	h := 3
	if n >= 4 {
		h = int(r.nextIntN(int32(n-3))) + 3
	}
	below := d.block(sx, sy-1, sz)
	if bname(below) != bnKelp && !bnNoKelpOn.has(bname(below)) && bSolid(below) {
		for i := 0; i < h; i++ {
			if i > 0 && !bWaterMat(d.block(sx, sy+i, sz)) {
				break
			}
			s := sKelpPlant
			if i == h-1 || !bWaterMat(d.block(sx, sy+i+1, sz)) {
				s = sKelpTop
			}
			d.bput(pos{sx, sy + i, sz}, s)
		}
	}
	jitter(r, 7)
	jitter(r, 7)
	return true
}

// bSeagrassFeat is SeagrassFeature::place (0x4bc3930).
type bSeagrassFeat struct {
	count int
	tall  float32
}

func (f bSeagrassFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	x, z := p.x, p.z
	py := p.y
	for !bSolid(d.block(x, py, z)) && py > minY {
		py--
	}
	if !bSolid(d.block(x, py, z)) {
		return p, false
	}
	cold := 0.15 > d.bedrockTemperature(p)
	sx, sz := x, z
	for range f.count {
		sy := py
		for sy <= maxY {
			n := d.block(sx, sy, sz)
			if n.isAir() || bWaterMat(n) {
				break
			}
			sy++
		}
		if bWaterMat(d.block(sx, sy, sz)) && bSolid(d.block(sx, sy-1, sz)) {
			if fl := r.nextFloat(); f.tall > fl {
				if bWaterMat(d.block(sx, sy+1, sz)) && (!cold || bWaterMat(d.block(sx, sy+2, sz))) {
					d.bput(pos{sx, sy, sz}, sTallSeaLo)
					d.bput(pos{sx, sy + 1, sz}, sTallSeaHi)
				}
			} else if !cold || bWaterMat(d.block(sx, sy+1, sz)) {
				d.bput(pos{sx, sy, sz}, sSeagrass)
			}
		}
		dz := jitter(r, 7)
		dx := jitter(r, 7)
		sx, sz = x+dx, z+dz
	}
	return p, true
}

// bedrockTemperature is the base temperature of the biome at a block, from the pack's climate.
func (d *decoration) bedrockTemperature(p pos) float32 {
	if p.y < minY {
		p.y = minY
	}
	if p.y > maxY {
		p.y = maxY
	}
	return d.g.bdata.surfaceBiomes[d.storedBiome(p.x, p.y, p.z)].temp
}

// seaPickle is SeaPickleFeature::place (0x4bc34a0).
func seaPickle(d *decoration, r *bmt, p pos) bool {
	x, z := p.x, p.z
	py := p.y
	for !bSolid(d.block(x, py, z)) {
		if bname(d.block(x, py, z)) == bnPickle || py <= minY {
			break
		}
		py--
	}
	if !bSolid(d.block(x, py, z)) {
		return false
	}
	for range 4 {
		dz, dx := int(r.nextIntN(6)), int(r.nextIntN(6))
		r.nextIntN(4)
		sx, sz := x+dx, z+dz
		sy := py
		for sy <= maxY {
			n := d.block(sx, sy, sz)
			if n.isAir() || bWaterMat(n) {
				break
			}
			sy++
		}
		n := d.block(sx, sy, sz)
		if n.isAir() || bname(n) == bnPickle || !bSolid(d.block(sx, sy-1, sz)) {
			continue
		}
		d.bput(pos{sx, sy, sz}, sPickle)
	}
	return true
}

// ---------------------------------------------------------------- land plants (p4_land)

var (
	sSugarCane    = laterState("sugar_cane")
	sCactus       = laterState("cactus")
	sCactusFlower = laterState("cactus_flower")
	sDeadBush     = laterState("dead_bush")
	sMelon        = laterState("melon")
	sLilyPad      = laterState("lily_pad")
	bnReeds       = internBName("reeds")
	bnCactus      = internBName("cactus")
	bnGrassBl     = internBName("grass_block")
	bnSands       = bnameSetOf(bedrockTagMembers["sand"])
	bnLandRepl    = bnameSetOf("air short_grass tall_grass fern large_fern deadbush snow_layer vine water flowing_water lava flowing_lava seagrass glow_lichen short_dry_grass tall_dry_grass bush leaf_litter fire")
	bnLava        = internBName("lava")
)

func reedsOK(d *decoration, q pos) bool {
	b := bname(d.at(q.add(0, -1, 0)))
	if b == bnReeds {
		return true
	}
	if !bnSoil.has(b) && !bnSands.has(b) {
		return false
	}
	for _, o := range [4]pos{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}} {
		if n := d.at(q.add(o.x, -1, o.z)); bWaterMat(n) || bnameString(n) == "frosted_ice" {
			return true
		}
	}
	return false
}

// reedsFeature is ReedsFeature::place (0x540bee0).
func reedsFeature(d *decoration, r *bmt, p pos) bool {
	for range 20 {
		px := p.x + jitter(r, 7)
		pz := p.z + jitter(r, 7)
		q := pos{px, p.y, pz}
		if !d.at(q).isAir() {
			continue
		}
		if !(bWaterMat(d.at(q.add(-1, -1, 0))) || bWaterMat(d.at(q.add(1, -1, 0))) || bWaterMat(d.at(q.add(0, -1, -1))) || bWaterMat(d.at(q.add(0, -1, 1)))) {
			continue
		}
		h := int(r.nextIntN(r.nextIntN(3)+1)) + 2
		for i := range h {
			c := q.add(0, i, 0)
			if c.y > maxY || !d.at(c).isAir() || !reedsOK(d, c) {
				break
			}
			d.bput(c, sSugarCane)
		}
	}
	return true
}

func genericMayPlace(d *decoration, q pos) bool {
	return q.y >= minY && q.y <= maxY && bnLandRepl.has(bname(d.at(q)))
}

// centralSpiked is CentralSpikedFeature (0x5151110): pumpkins and sweet berries.
type centralSpiked struct {
	s     bs
	count int
	berry bool
}

func (f centralSpiked) place(d *decoration, r *bmt, p pos) (pos, bool) {
	q := p
	for range f.count {
		if q.y >= minY && q.y <= maxY && d.at(q).isAir() && bname(d.at(q.add(0, -1, 0))) == bnGrassBl && genericMayPlace(d, q) {
			if f.berry {
				r.nextIntN(8)
			}
			d.bput(q, f.s)
		}
		dx := jitter(r, 7)
		dz := jitter(r, 7)
		dy := jitter(r, 3)
		q = p.add(dx, dy, dz)
	}
	return p, true
}

func cactusOK(d *decoration, q pos) bool {
	b := bname(d.at(q.add(0, -1, 0)))
	if b != bnCactus && !bnSands.has(b) {
		return false
	}
	for _, o := range [4]pos{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}} {
		if bSolid(d.at(q.add(o.x, 0, o.z))) {
			return false
		}
	}
	a := d.at(q.add(0, 1, 0))
	return !bWaterMat(a) && bname(a) != bnLava
}

// cactusFeature is CactusFeature::place (0x514a300), with the flower of worlds of 1.21.70 and later.
func cactusFeature(d *decoration, r *bmt, p pos) bool {
	for range 10 {
		dz, dy, dx := jitter(r, 7), jitter(r, 3), jitter(r, 7)
		q := p.add(dx, dy, dz)
		if q.y < minY || q.y > maxY || !d.at(q).isAir() {
			continue
		}
		n := int(r.nextIntN(r.nextIntN(3) + 1))
		placed := 0
		for i := 0; i <= n; i++ {
			c := q.add(0, i, 0)
			if c.y > maxY || !cactusOK(d, c) {
				break
			}
			d.bput(c, sCactus)
			placed = i + 1
		}
		if placed != 0 && r.nextIntN(3) == 0 {
			if c := q.add(0, placed, 0); c.y <= maxY && d.at(c).isAir() {
				d.bput(c, sCactusFlower)
			}
		}
	}
	return true
}

// deadBushFeature is DeadBushFeature::place (0x515daa0).
func deadBushFeature(d *decoration, r *bmt, p pos) bool {
	py := p.y
	for py > minY {
		m := d.block(p.x, py, p.z)
		py--
		if !(m.isAir() || bLeaves(m)) {
			break
		}
	}
	for range 4 {
		dz, dy, dx := jitter(r, 7), jitter(r, 3), jitter(r, 7)
		q := pos{p.x + dx, py + dy, p.z + dz}
		if q.y < minY || q.y > maxY || !d.at(q).isAir() {
			continue
		}
		b := d.at(q.add(0, -1, 0))
		bn := bname(b)
		if bnSands.has(bn) || b.tagged(tagTerracotta) || bnSoil.has(bn) {
			d.bput(q, sDeadBush)
		}
	}
	return true
}

// melonFeature is MelonFeature::place (0x5402ff0).
func melonFeature(d *decoration, r *bmt, p pos) bool {
	for range 64 {
		dz, dy, dx := jitter(r, 7), jitter(r, 3), jitter(r, 7)
		q := p.add(dx, dy, dz)
		if genericMayPlace(d, q) && bname(d.at(q.add(0, -1, 0))) == bnGrassBl {
			d.bput(q, sMelon)
		}
	}
	return true
}

// waterlilyFeature is WaterlilyFeature::place (0x4befe60): no draws.
func waterlilyFeature(d *decoration, r *bmt, p pos) bool {
	if p.y < minY || p.y > maxY || !d.at(p).isAir() || !bWaterMat(d.at(p.add(0, -1, 0))) {
		return false
	}
	if p.y < d.height(hmWorldSurface, p.x, p.z) {
		return false
	}
	return d.bput(p, sLilyPad)
}

var (
	sBambooStalk = laterState("bamboo[age=0,leaves=none,stage=0]")
	sBambooLarge = laterState("bamboo[age=0,leaves=large,stage=0]")
	sBambooSmall = laterState("bamboo[age=0,leaves=small,stage=0]")
	bnBambooSoil = bnameSetOf("grass_block podzol dirt dirt_with_roots")
)

// bBamboo is BambooFeature (0x4b9f1b0).
func bBamboo(d *decoration, r *bmt, p pos) bool {
	if p.y < minY || p.y > maxY || !d.at(p).isAir() || !bnBambooSoil.has(bname(d.at(p.add(0, -1, 0)))) {
		return false
	}
	h := uint32(p.x*0x2fc20f) ^ uint32(p.z*0x6ebfff5)
	h = h * (h*0x285b825 + 0xb)
	n := int32((h>>24)&0xff)%5 | 8
	k := int(r.nextIntN(n))
	cy := p.y
	if k+4 >= 0 {
		for range max(k+4, 0) + 1 {
			if cy > maxY || !d.block(p.x, cy, p.z).isAir() {
				break
			}
			d.bput(pos{p.x, cy, p.z}, sBambooStalk)
			cy++
		}
	}
	if cy-p.y >= 3 {
		d.bput(pos{p.x, cy, p.z}, sBambooLarge)
		d.bput(pos{p.x, cy - 1, p.z}, sBambooLarge)
		d.bput(pos{p.x, cy - 2, p.z}, sBambooSmall)
	}
	return true
}

// ---------------------------------------------------------------- lush caves (T1, LUSH-pseudocode)

// snapFeat is SnapToSurfaceFeature (0xd8e7930, scan 0xd8fe840).
type snapFeat struct {
	f                              bfeat
	ceiling                        bool
	randomH, randomV               bool
	rng                            int
	allowAir, allowNonAir, allowUW bool
	surface                        *bmatch
}

func (fc *featCompiler) snap(v map[string]json.RawMessage) (bfeat, error) {
	f := &snapFeat{allowAir: true}
	var s string
	json.Unmarshal(v["surface"], &s)
	switch s {
	case "", "floor":
	case "ceiling":
		f.ceiling = true
	case "random_horizontal":
		f.randomH = true // T14: a horizontal surface, floor or ceiling by nextBoolean.
	case "random_vertical":
		f.randomV = true // T14: a wall, the four sides shuffled by three raw words.
	default:
		return nil, unimpl("snap surface %s", s)
	}
	json.Unmarshal(v["vertical_search_range"], &f.rng)
	if raw, ok := v["allow_air_placement"]; ok {
		json.Unmarshal(raw, &f.allowAir)
	}
	json.Unmarshal(v["allow_non_air_placement"], &f.allowNonAir)
	json.Unmarshal(v["allow_underwater_placement"], &f.allowUW)
	var err error
	if f.surface, err = optMatch(v["allowed_surface_blocks"]); err != nil {
		return nil, err
	}
	if f.f, err = fc.child(v["feature_to_snap"]); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *snapFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	if f.randomH {
		c := *f
		c.randomH, c.ceiling = false, r.nextBool()
		return c.place(d, r, p)
	}
	if f.randomV {
		return f.placeWall(d, r, p)
	}
	if p.y < minY || p.y > maxY {
		return p, false
	}
	b0 := d.at(p)
	open := b0.isAir() || bWaterMat(b0)
	up := f.ceiling == open
	dy := -1
	if up {
		dy = 1
	}
	allowedOpen := func(s bs) bool { return f.allowAir && s.isAir() || f.allowUW && bWaterMat(s) }
	allowed := func(s bs) bool {
		if open {
			return allowedOpen(s)
		}
		return f.allowNonAir && !s.isAir() && !bWaterMat(s)
	}
	target := func(s bs) bool {
		switch {
		case f.surface != nil:
			return f.surface.has(s)
		case open:
			return bSolid(s)
		}
		return allowedOpen(s)
	}
	if !allowed(b0) {
		return p, false
	}
	bio := d.storedBiome(p.x, p.y, p.z)
	k, py := 0, p.y+dy
	for k < f.rng-1 {
		if py < minY || py > maxY {
			break
		}
		n := d.block(p.x, py, p.z)
		if !allowed(n) || d.storedBiome(p.x, py, p.z) != bio {
			break
		}
		py += dy
		k++
	}
	if py < minY || py > maxY || !target(d.block(p.x, py, p.z)) {
		return p, false
	}
	off := k
	if !open {
		off++
	}
	return f.f.place(d, r, pos{p.x, p.y + dy*off, p.z})
}

// placeWall is the random_vertical search (T14): the faces north, east, south, west shuffled with three
// raw words, then the search along each until one finds its surface.
func (f *snapFeat) placeWall(d *decoration, r *bmt, p pos) (pos, bool) {
	a := [4]int{2, 5, 3, 4}
	if r.u32()&1 == 0 {
		a[0], a[1] = a[1], a[0]
	}
	if j := r.u32() % 3; j != 2 {
		a[j], a[2] = a[2], a[j]
	}
	if j := r.u32() & 3; j != 3 {
		a[j], a[3] = a[3], a[j]
	}
	if p.y < minY || p.y > maxY {
		return p, false
	}
	b0 := d.at(p)
	open := b0.isAir() || bWaterMat(b0)
	allowedOpen := func(s bs) bool { return f.allowAir && s.isAir() || f.allowUW && bWaterMat(s) }
	allowed := func(s bs) bool {
		if open {
			return allowedOpen(s)
		}
		return f.allowNonAir && !s.isAir() && !bWaterMat(s)
	}
	target := func(s bs) bool {
		switch {
		case f.surface != nil:
			return f.surface.has(s)
		case open:
			return bSolid(s)
		}
		return allowedOpen(s)
	}
	if !allowed(b0) {
		return p, false
	}
	bio := d.storedBiome(p.x, p.y, p.z)
	for _, fi := range a {
		v := facingVec[fi]
		k, q := 0, p.add(v.x, v.y, v.z)
		for k < f.rng-1 {
			if q.y < minY || q.y > maxY || !allowed(d.at(q)) || d.storedBiome(q.x, q.y, q.z) != bio {
				break
			}
			q = q.add(v.x, v.y, v.z)
			k++
		}
		if q.y < minY || q.y > maxY || !target(d.at(q)) {
			continue
		}
		off := k
		if !open {
			off++
		}
		return f.f.place(d, r, pos{p.x + v.x*off, p.y + v.y*off, p.z + v.z*off})
	}
	return p, false
}

// vegetationPatchFeat is VegetationPatchFeature::place (0xd8f3860).
type vegetationPatchFeat struct {
	ceiling               bool
	radius, depth         bRange
	edge, deep, vegChance float32
	vr                    int
	ground                bs
	replace               *bmatch
	waterlogged           bool
	veg                   bfeat
}

func (fc *featCompiler) vegetationPatch(v map[string]json.RawMessage) (bfeat, error) {
	f := &vegetationPatchFeat{depth: bRange{1, 1, false}}
	var s string
	json.Unmarshal(v["surface"], &s)
	f.ceiling = s == "ceiling"
	var err error
	if f.radius, err = parseBRange(v["horizontal_radius"]); err != nil {
		return nil, err
	}
	if raw, ok := v["depth"]; ok {
		if f.depth, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	json.Unmarshal(v["extra_edge_column_chance"], &f.edge)
	json.Unmarshal(v["extra_deep_block_chance"], &f.deep)
	json.Unmarshal(v["vegetation_chance"], &f.vegChance)
	json.Unmarshal(v["vertical_range"], &f.vr)
	json.Unmarshal(v["waterlogged"], &f.waterlogged)
	if f.ground, _, err = blockState(v["ground_block"]); err != nil {
		return nil, err
	}
	if f.replace, err = parseBMatch(v["replaceable_blocks"]); err != nil {
		return nil, err
	}
	if raw, ok := v["vegetation_feature"]; ok {
		if f.veg, err = fc.child(raw); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func (f *vegetationPatchFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	sd := -1
	if f.ceiling {
		sd = 1
	}
	xr, zr := f.radius.excl(r), f.radius.excl(r)
	surfaces := make([]pos, 0, 64)
	gb := f.ground.block()
	for dx := -xr - 1; dx <= xr+1; dx++ {
		xe := dx == -xr-1 || dx == xr+1
		for dz := -zr - 1; dz <= zr+1; dz++ {
			ze := dz == -zr-1 || dz == zr+1
			if xe && ze {
				continue
			}
			if (xe || ze) && !(f.edge != 0 && r.nextFloat() <= f.edge) {
				continue
			}
			px, py, pz := p.x+dx, p.y, p.z+dz
			for i := 0; i < f.vr && d.block(px, py, pz).isAir() && py >= minY && py <= maxY; i++ {
				py += sd
			}
			for i := 0; i < f.vr; i++ {
				if py < minY || py > maxY || d.block(px, py, pz).isAir() {
					break
				}
				py -= sd
			}
			if py < minY || py > maxY || !d.block(px, py, pz).isAir() {
				continue
			}
			sy := py + sd
			if !bSolid(d.block(px, sy, pz)) {
				continue
			}
			extra := 0
			if f.deep > 0 && r.nextFloat() < f.deep {
				extra = 1
			}
			tot := f.depth.excl(r) + extra
			ok := true
			for j := range tot {
				q := pos{px, sy + j*sd, pz}
				b := d.at(q)
				if b.block() == gb {
					continue
				}
				if q.y < minY || q.y > maxY || !f.replace.has(b) {
					if j == 0 {
						ok = false
					}
					break
				}
				d.bput(q, f.ground)
			}
			if ok {
				surfaces = append(surfaces, pos{px, sy, pz})
			}
		}
	}
	list := surfaces
	if f.waterlogged {
		var wl []pos
		for _, q := range surfaces {
			if bSolid(d.at(q.add(0, 0, -1))) && bSolid(d.at(q.add(1, 0, 0))) && bSolid(d.at(q.add(0, 0, 1))) &&
				bSolid(d.at(q.add(-1, 0, 0))) && bSolid(d.at(q.add(0, -1, 0))) {
				wl = append(wl, q)
			}
		}
		for _, q := range wl {
			d.bput(q, sWater)
		}
		list = wl
	}
	if len(list) > 0 && f.vegChance > 0 {
		for _, q := range list {
			if r.nextFloat() < f.vegChance && f.veg != nil {
				vq := q
				if !f.waterlogged {
					vq = q.add(0, -sd, 0)
				}
				f.veg.place(d, r, vq)
			}
		}
	}
	if len(list) > 0 {
		return list[len(list)-1], true
	}
	return p, false
}

// growingPlantFeat is GrowingPlantFeature::place (0xd9b9130).
type growingPlantFeat struct {
	heights []bRange
	hw      []float32
	age     bRange
	hasAge  bool
	up      bool
	water   bool
	body    []bs
	bw      []float32
	head    [][26]bs // by age
	hdw     []float32
}

func (fc *featCompiler) growingPlant(v map[string]json.RawMessage) (bfeat, error) {
	f := &growingPlantFeat{}
	var hd [][2]json.RawMessage
	if err := json.Unmarshal(v["height_distribution"], &hd); err != nil {
		return nil, err
	}
	for _, e := range hd {
		rg, err := parseBRange(e[0])
		if err != nil {
			return nil, err
		}
		var w float32
		json.Unmarshal(e[1], &w)
		f.heights, f.hw = append(f.heights, rg), append(f.hw, w)
	}
	if raw, ok := v["age"]; ok {
		f.hasAge = true
		var err error
		if f.age, err = parseBRange(raw); err != nil {
			return nil, err
		}
	}
	var dirn string
	json.Unmarshal(v["growth_direction"], &dirn)
	f.up = strings.EqualFold(dirn, "up")
	json.Unmarshal(v["allow_water"], &f.water)
	read := func(raw json.RawMessage, head bool) error {
		var list [][2]json.RawMessage
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
		for _, e := range list {
			b, err := parseBDesc(e[0])
			if err != nil {
				return err
			}
			var w float32
			json.Unmarshal(e[1], &w)
			if !head {
				s, err := javaStateFor(b)
				if err != nil {
					return unimpl("%v", err)
				}
				f.body, f.bw = append(f.body, s), append(f.bw, w)
				continue
			}
			var ages [26]bs
			for a := range 26 {
				var s bs
				var err error
				switch b.name {
				case "cave_vines":
					s, err = safeState(fmt.Sprintf("cave_vines[age=%d,berries=false]", a))
				case "cave_vines_head_with_berries":
					s, err = safeState(fmt.Sprintf("cave_vines[age=%d,berries=true]", a))
				default:
					s, err = javaStateFor(b)
				}
				if err != nil {
					return unimpl("%v", err)
				}
				ages[a] = s
			}
			f.head, f.hdw = append(f.head, ages), append(f.hdw, w)
		}
		return nil
	}
	if err := read(v["body_blocks"], false); err != nil {
		return nil, err
	}
	if err := read(v["head_blocks"], true); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *growingPlantFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	k := weightedPick(r, f.hw)
	if k < 0 {
		return p, false
	}
	h := f.heights[k].excl(r)
	age := 0
	if f.hasAge {
		age = min(max(f.age.excl(r), 0), 25)
	}
	if h < 1 {
		return p, false
	}
	dy := -1
	if f.up {
		dy = 1
	}
	open := func(s bs) bool { return s.isAir() || f.water && bWaterMat(s) }
	for i := range h {
		q := p.add(0, dy*i, 0)
		if q.y < minY || q.y > maxY || !open(d.at(q)) {
			continue
		}
		nx := q.add(0, dy, 0)
		if i == h-1 || nx.y < minY || nx.y > maxY || !d.at(nx).isAir() {
			if j := weightedPick(r, f.hdw); j >= 0 {
				d.bput(q, f.head[j][age])
			}
			return q, true
		}
		if j := weightedPick(r, f.bw); j >= 0 {
			d.bput(q, f.body[j])
		}
	}
	return p, false
}

// vegetationFeat is VegetationFeature (0x4bef330): the weighted pick first, then air, a sturdy block
// below and the plant's own rule; double plants get their upper half.
type vegetationFeat struct{ list []vegEntry }

type vegEntry struct {
	w      float32
	lo, hi bs
	kind   int // 0 grass family, 1 azalea, 2 carpet
}

var (
	mossVegetation = []vegEntry{
		{4, laterState("flowering_azalea"), 0, 1}, {7, laterState("azalea"), 0, 1},
		{25, laterState("moss_carpet"), 0, 2}, {50, laterState("short_grass"), 0, 0},
		{10, laterState("tall_grass[half=lower]"), laterState("tall_grass[half=upper]"), 0},
	}
	paleMossVegetation = []vegEntry{
		{25, laterState("pale_moss_carpet[bottom=true,east=none,north=none,south=none,west=none]"), 0, 2},
		{25, laterState("short_grass"), 0, 0},
		{10, laterState("tall_grass[half=lower]"), laterState("tall_grass[half=upper]"), 0},
	}
	bnClay = internBName("clay")
)

func (f vegetationFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	var w [8]float32
	for i, e := range f.list {
		w[i] = e.w
	}
	k := weightedPick(r, w[:len(f.list)])
	if k < 0 || p.y < minY || p.y+1 > maxY || !d.at(p).isAir() {
		return p, false
	}
	below := d.at(p.add(0, -1, 0))
	if !bSolid(below) {
		return p, false
	}
	e := f.list[k]
	bn := bname(below)
	switch e.kind {
	case 1:
		if !bnSoil.has(bn) && bn != bnClay {
			return p, false
		}
	case 2:
		if below.isAir() {
			return p, false
		}
	default:
		if !bnSoil.has(bn) || e.hi != 0 && !d.at(p.add(0, 1, 0)).isAir() {
			return p, false
		}
	}
	if e.hi != 0 {
		d.bput(p.add(0, 1, 0), e.hi)
	}
	d.bput(p, e.lo)
	return p, true
}

// dripleafFeat is DripleafFeature (0x5162d20).
type dripleafFeat struct{ big [4]bfeat }

var (
	sSmallDripLo [4]bs
	sSmallDripHi [4]bs
	bnDripGround = bnameSetOf("clay moss_block")
)

func init() {
	for i := range 4 {
		f := bedrockDirection(i)
		sSmallDripLo[i] = laterState("small_dripleaf[facing=" + f + ",half=lower,waterlogged=false]")
		sSmallDripHi[i] = laterState("small_dripleaf[facing=" + f + ",half=upper,waterlogged=false]")
	}
}

func (f *dripleafFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	if r.nextFloat() < 0.2 {
		k := int(r.nextIntN(4))
		if p.y < minY || p.y+1 > maxY {
			return p, false
		}
		cur, below, up := d.at(p), d.at(p.add(0, -1, 0)), d.at(p.add(0, 1, 0))
		ok := (cur.isAir() || bWaterMat(cur)) && (bnDripGround.has(bname(below)) || bWaterMat(cur) && bnSoil.has(bname(below))) &&
			(up.isAir() || bWaterMat(up))
		if !ok {
			return p, false
		}
		d.bput(p, sSmallDripLo[k])
		d.bput(p.add(0, 1, 0), sSmallDripHi[k])
		return p, true
	}
	return f.big[r.nextIntN(4)].place(d, r, p)
}

// vinesSingleFace is VinesSingleFaceFeature (0x4befc60): no draws; a vine on the first solid side of
// -z, +x, +z, -x.
func vinesSingleFace(d *decoration, r *bmt, p pos) bool {
	if p.y < minY || p.y > maxY || !d.at(p).isAir() {
		return false
	}
	for i, o := range [4]pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}} {
		if n := d.at(p.add(o.x, 0, o.z)); bSolid(n) && n.name() != "bedrock" {
			return d.bput(p, vineState([4]string{"north", "east", "south", "west"}[i]))
		}
	}
	return false
}

// trailToSurface is TrailToSurfaceFeature (0x4bc84d0), azalea_root_system_feature.
type trailToSurface struct{ tree bfeat }

var (
	sRootedDirt   = laterState("rooted_dirt")
	sHangingRoots = laterState("hanging_roots[waterlogged=false]")
	bnRootRepl    = bnameSetOf("clay moss_block sand gravel dirt podzol dirt_with_roots grass_block mycelium stone deepslate tuff cave_vines cave_vines_head_with_berries moss_carpet short_grass fern")
)

func matSolid(s bs) bool { return bSolid(s) || bLeaves(s) }

func (f *trailToSurface) place(d *decoration, r *bmt, p pos) (pos, bool) {
	top := d.height(hmWorldSurface, p.x, p.z)
	x, z := p.x, p.z
	py := p.y
	get := func(y int) bs { return d.block(x, y, z) }
	for range 184 {
		if matSolid(get(py-1)) && bPassable(get(py)) {
			b0 := get(py)
			wat := bWaterMat(b0)
			if wat || b0.isAir() {
				b1 := get(py + 1)
				goOn := true
				if !bWaterMat(b1) {
					if !b1.isAir() {
						goOn = false
					} else {
						wat = false
					}
				}
				if goOn {
					b2 := get(py + 2)
					if bWaterMat(b2) {
						goOn = !wat
					} else if !b2.isAir() {
						goOn = false
					}
				}
				if goOn {
					off := 0
					if bWaterMat(get(py - 1)) {
						if bnRootRepl.has(bname(get(py - 2))) {
							off = -1
						} else if bWaterMat(get(py-2)) && bnRootRepl.has(bname(get(py-3))) {
							off = -2
						}
					}
					if _, ok := f.tree.place(d, r, pos{x, py + off, z}); ok {
						for yy := p.y + 1; yy <= py; yy++ {
							for range 20 {
								a, b, c, e := int(r.nextIntN(3)), int(r.nextIntN(3)), int(r.nextIntN(3)), int(r.nextIntN(3))
								q := pos{x + a - b, yy, z + c - e}
								if bnRootRepl.has(bname(d.at(q))) {
									d.bput(q, sRootedDirt)
								}
							}
						}
						h := p
						for range 20 {
							a, b, c, e := int(r.nextIntN(3)), int(r.nextIntN(3)), int(r.nextIntN(3)), int(r.nextIntN(3))
							g, k := int(r.nextIntN(2)), int(r.nextIntN(2))
							h = h.add(a-b, g-k, c-e)
							if h.y >= minY && h.y < maxY && d.at(h).isAir() && matSolid(d.at(h.add(0, 1, 0))) {
								d.bput(h, sHangingRoots)
							}
						}
						return p, true
					}
				}
			}
		}
		if top <= py {
			return p, false
		}
		py++
	}
	return p, false
}
