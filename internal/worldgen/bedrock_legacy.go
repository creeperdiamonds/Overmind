package worldgen

import (
	"fmt"
	"math"
)

// Bedrock's own code features that stood in as Java's placements until Task 9 round 22, in our own Go
// from the study of BDS 1.26.60.6 (NOTES-leftovers L24 to L29, FEATS-pseudocode). Each replaces Java
// placements that ran at Java's positions in every Bedrock world (javaReplaced):
//   - the code prelude before the passes (L25): lava lakes and monster rooms on the chunk's Random;
//   - ice_spike_feature and ice_patch_feature (L26), grass_disc_feature and podzol_area_feature (L27);
//   - legacy:plains_flower_feature, meadow_flower_feature, eyeblossom_feature, firefly_bush_cluster_feature
//     and its water variant, legacy:swamp_foliage_feature (L28);
//   - legacy:springs_feature, desert_well_feature, legacy:emerald_ore_feature (L29).
// Not stored: the block entities these write in BDS (the room chest's LootTable "simple_dungeon" and
// LootTableSeed, the spawner's mob, the well's suspicious sand loot table and seed). Decoration writes
// block states only, so their draws are made and the values dropped (docs/stage-2-known-gaps.md).

// bedrockSeaLevel is the dimension's sea level (the short at +0xac, 63; FEATS-pseudocode).
const bedrockSeaLevel = 63

// ---------------------------------------------------------------- the prelude (L25)

// lavaLakeCfg is the prelude's LakeFeature (ctor 0x54f8820): lava, a stone border, no no-go blocks. Its
// place is T36's pool, the sulfur pool's code (bedrock_sulfurpool.go), drawing from the chunk Random.
var lavaLakeCfg = &sulfurPool{fluid: sLava, border: sStone}

// overworldPrelude is decorationPostProcessChunk's code before the passes (0x53019f0, L25), on the
// chunk's Random (seeded with the chunk seed; the caller reseeds it for the passes). In fresh worlds the
// level flag flag61c is set (MEAS: 22/22 first rooms, 0/13 with a water-lake draw first), so water lakes
// are skipped with no draw; the generator byte +0x130 is 0, so the lava lake and rooms run. There are no
// underground lava lakes.
func (d *decoration) overworldPrelude(r *bmt, cx, cz int) {
	ox, oz := cx*16, cz*16
	if r.nextIntN(8) == 0 {
		x := int(r.nextIntN(9))
		a := r.nextIntN(120)
		y := int(r.nextIntN(a + 8))
		z := int(r.nextIntN(9))
		// nextInt(10) is drawn only for y above sea level.
		if y >= bedrockSeaLevel-3 && (y <= bedrockSeaLevel || r.nextIntN(10) == 0) {
			lavaLakeCfg.place(d, r, pos{ox + x + 3, y, oz + z + 3})
		}
	}
	// 4 deep attempts (flag61c set and the dimension's minY below 0), then 10.
	for i := range 14 {
		x := int(r.nextIntN(16))
		var y int
		if i < 4 {
			y = int(r.nextIntN(int32(-(minY + 6)))) + minY + 6
		} else {
			y = int(r.nextIntN(320)) // Generator vt+0x1d0 = 0x140, no minY offset.
		}
		z := int(r.nextIntN(16))
		bMonsterRoom(d, r, pos{ox + 8 + x, y, oz + 8 + z})
	}
}

var (
	sCobble    = laterState("cobblestone")
	sSpawner   = laterState("spawner")
	bnChest    = internBName("chest")
	chestFaced = func() (m [6]bs) {
		for _, a := range horizontal {
			m[a] = stateOf(fmt.Sprintf("chest[facing=%s,type=single,waterlogged=false]", dirName(a)))
		}
		return m
	}()
)

// bMonsterRoom is MonsterRoomFeature::place (0x5403280, L25): Java 1.12's dungeon with Bedrock's points.
// "Solid" is the material's solid bit (our fSolid, as the sulfur trail reads L11.1's flag).
func bMonsterRoom(d *decoration, r *bmt, p pos) bool {
	xr := int(r.nextIntN(2)) + 2
	zr := int(r.nextIntN(2)) + 2
	x0, x1, z0, z1 := p.x-xr-1, p.x+xr+1, p.z-zr-1, p.z+zr+1
	holes := 0
	for x := x0; x <= x1; x++ {
		for y := p.y - 1; y <= p.y+4; y++ {
			for z := z0; z <= z1; z++ {
				solid := d.block(x, y, z).has(fSolid)
				if (y == p.y-1 || y == p.y+4) && !solid {
					return false
				}
				if (x == x0 || x == x1 || z == z0 || z == z1) && y == p.y && d.block(x, y, z).isAir() && d.block(x, y+1, z).isAir() {
					holes++
				}
			}
		}
	}
	if holes < 1 || holes > 5 {
		return false
	}
	// x ascending, y from y+3 down to the floor (y-1), z ascending: the ceiling row (y+4) is tested but
	// not rewritten. L25 reads the loop from y+4; MEAS against BDS (seeds 100, 1, 987654321: the rooms'
	// cobblestone precision 64.4 / 62.6 / 64.5% from y+4, 100 / 100 / 100% from y+3, recall unchanged) says
	// the ceiling stays, so the loop starts at y+3 here (a measured correction of the reading, labelled).
	for x := x0; x <= x1; x++ {
		for y := p.y + 3; y >= p.y-1; y-- {
			for z := z0; z <= z1; z++ {
				q := pos{x, y, z}
				if x != x0 && x != x1 && z != z0 && z != z1 && y != p.y-1 {
					d.bput(q, sAir)
					continue
				}
				if y >= minY && !d.block(x, y-1, z).has(fSolid) {
					d.bput(q, sAir)
				} else if d.block(x, y, z).has(fSolid) {
					// nextInt(4) is drawn only for solid floor cells.
					if y == p.y-1 && r.nextIntN(4) != 0 {
						d.bput(q, sMossyCobble)
					} else {
						d.bput(q, sCobble)
					}
				}
			}
		}
	}
	// Two passes of up to 3 tries; dz is drawn before dx (Java draws x first). A pass ends on its chest.
	for range 2 {
		for range 3 {
			dz := int(r.nextIntN(int32(2*zr+1))) - zr
			dx := int(r.nextIntN(int32(2*xr+1))) - xr
			c := pos{p.x + dx, p.y, p.z + dz}
			if !d.at(c).isAir() {
				continue
			}
			n, wall := 0, dir(0)
			for _, a := range horizontal {
				if d.at(c.step(a)).has(fSolid) {
					n++
					wall = a
				}
			}
			if n != 1 {
				continue
			}
			// The chest faces away from its wall (MEAS, 30/30 chests of seed 100); a chest beside an
			// earlier chest takes that chest's facing (MEAS, the same 30; the pairing code is not read).
			face := wall.opposite()
			if w := d.at(c.step(wall)); bname(w) == bnChest {
				for f, s := range chestFaced {
					if s == w {
						face = dir(f)
					}
				}
			}
			d.bput(c, chestFaced[face])
			r.nextInt() // The chest's LootTableSeed (not stored, see the file's comment).
			break
		}
	}
	d.bput(p, sSpawner)
	r.nextIntN(4) // The mob: [skeleton, zombie, zombie, spider] (not stored).
	return true
}

// ---------------------------------------------------------------- ice spikes and patches (L26)

var (
	bnSnowBlock  = internBName("snow") // The snow block (0xff1a2b0); bname gives Bedrock's names.
	bnPackedIce  = internBName("packed_ice")
	bnSpikeOK    = bnameSetOf("grass_block dirt coarse_dirt snow ice") // And air.
	bnPatchRepl  = bnameSetOf("dirt coarse_dirt snow ice")
	bnPodzolRepl = bnameSetOf("dirt grass_block")
	bnDiscRepl   = bnameSetOf("dirt coarse_dirt grass_block mud")
)

// walkDownAir is the legacy walk: down while the block is air, stopping at minY + 2.
func walkDownAir(d *decoration, p pos) pos {
	for d.at(p).isAir() && p.y > minY+2 {
		p.y--
	}
	return p
}

func spikeOK(s bs) bool { return s.isAir() || bnSpikeOK.has(bname(s)) }

// bIceSpike is IceSpikeFeature (0x516e210, L26 and FEATS-pseudocode).
func bIceSpike(d *decoration, r *bmt, p pos) bool {
	p = walkDownAir(d, p)
	if bname(d.at(p)) != bnSnowBlock {
		return false
	}
	p.y += int(r.nextIntN(4))
	h := int(r.nextIntN(4)) + 7
	w := h/4 + int(r.nextIntN(2))
	if w > 1 && r.nextIntN(60) == 0 {
		p.y += int(r.nextIntN(30)) + 10
	}
	for k := range h {
		f := float32(float32(1-float32(k)/float32(h)) * float32(w))
		l := int(math.Ceil(float64(f)))
		for i := -l; i <= l; i++ {
			for j := -l; j <= l; j++ {
				ai, aj := float32(absInt(i))-0.25, float32(absInt(j))-0.25
				if !(i == 0 && j == 0) && float32(aj*aj)+float32(ai*ai) > float32(f*f) {
					continue
				}
				if (absInt(i) == l || absInt(j) == l) && r.nextFloat() > 0.75 {
					continue
				}
				if q := p.add(i, k, j); spikeOK(d.at(q)) {
					d.bput(q, sPacked)
				}
				if k != 0 && l > 1 {
					if q := p.add(i, -k, j); spikeOK(d.at(q)) {
						d.bput(q, sPacked)
					}
				}
			}
		}
	}
	pw := min(w, 2) - 1
	for i := -pw; i <= pw; i++ {
		for j := -pw; j <= pw; j++ {
			q := p.add(i, -1, j)
			n := 50
			if absInt(i) == 1 && absInt(j) == 1 {
				n = int(r.nextIntN(5))
			}
			for q.y > bedrockSeaLevel-13 {
				if b := d.at(q); !spikeOK(b) && bname(b) != bnPackedIce {
					break
				}
				d.bput(q, sPacked)
				q.y--
				n--
				if n <= 0 {
					q.y -= int(r.nextIntN(5)) + 1
					n = int(r.nextIntN(5))
				}
			}
		}
	}
	return true
}

// bIcePatch is IcePatchFeature (0x516dc00, radius field 4).
func bIcePatch(d *decoration, r *bmt, p pos) bool {
	p = walkDownAir(d, p)
	if bname(d.at(p)) != bnSnowBlock {
		return false
	}
	rr := int(r.nextIntN(2)) + 2
	for x := p.x - rr; x <= p.x+rr; x++ {
		for z := p.z - rr; z <= p.z+rr; z++ {
			if (x-p.x)*(x-p.x)+(z-p.z)*(z-p.z) > rr*rr {
				continue
			}
			for y := p.y - 1; y <= p.y+1; y++ {
				if bnPatchRepl.has(bname(d.block(x, y, z))) {
					d.bput(pos{x, y, z}, sPacked)
				}
			}
		}
	}
	return true
}

// ---------------------------------------------------------------- grass disc, podzol area (L27)

// bGrassDisc is DiscFeature (0x5162750) as grass_disc_feature builds it: grass_block, radius 5. The
// origin is not tested; it always returns true.
func bGrassDisc(d *decoration, r *bmt, p pos) bool {
	rr := int(r.nextIntN(3)) + 2
	for x := p.x - rr; x <= p.x+rr; x++ {
		for z := p.z - rr; z <= p.z+rr; z++ {
			if (x-p.x)*(x-p.x)+(z-p.z)*(z-p.z) > rr*rr {
				continue
			}
			for y := p.y - 2; y <= p.y+2; y++ {
				if !bnDiscRepl.has(bname(d.block(x, y, z))) {
					continue
				}
				s := sGrass
				if !d.block(x, y+1, z).isAir() {
					s = sDirt
				}
				d.bput(pos{x, y, z}, s)
			}
		}
	}
	return true
}

// bPodzolArea is podzol_area_feature (0x540bc40): dirt or grass under a disc of radius 1..4 turns to
// podzol. Bedrock 1.26's coarse dirt is its own block, so "dirt in any state" is dirt alone.
func bPodzolArea(d *decoration, r *bmt, p pos) bool {
	rr := int(r.nextIntN(4)) + 1
	n := 0
	for x := p.x - rr; x <= p.x+rr; x++ {
		for z := p.z - rr; z <= p.z+rr; z++ {
			if (x-p.x)*(x-p.x)+(z-p.z)*(z-p.z) > rr*rr {
				continue
			}
			if q := (pos{x, p.y - 1, z}); bnPodzolRepl.has(bname(d.at(q))) && d.bput(q, sPodzol) {
				n++
			}
		}
	}
	return n > 0
}

// ---------------------------------------------------------------- flowers (L28)

var (
	plainsTulips = [4]bs{laterState("red_tulip"), laterState("orange_tulip"), laterState("white_tulip"), laterState("pink_tulip")}
	plainsOthers = [4]bs{laterState("poppy"), laterState("azure_bluet"), laterState("oxeye_daisy"), laterState("cornflower")}
	sDandelion   = laterState("dandelion")
)

// plainsFlower is LegacyFlowerFeature type 3 (0x53fbc30, block choice 0xd901ce0): the noise at
// (x, z) / 200 picks tulips below -0.8, else a dandelion one time in four, else one of four flowers.
// The origin must be air (no draws otherwise); one try (iterations 1), canSurvive approximated as soil
// below as for the other legacy flowers; then the six jitter words. It returns that the origin was air.
func plainsFlower(d *decoration, r *bmt, p pos) bool {
	if p.y < minY || p.y > maxY || !d.at(p).isAir() {
		return false
	}
	var s bs
	if n := queryNoise(float32(p.x)/200, float32(p.z)/200); float64(n) < -0.8 {
		s = plainsTulips[r.nextIntN(4)]
	} else if r.nextIntN(4) == 0 {
		s = sDandelion
	} else {
		s = plainsOthers[r.nextIntN(4)]
	}
	if bnSoil.has(bname(d.at(p.add(0, -1, 0)))) {
		d.bput(p, s)
	}
	jitter(r, 7)
	jitter(r, 3)
	jitter(r, 7)
	return true
}

var meadowList = func() (l [8]bs) {
	for i, n := range []string{"allium", "poppy", "cornflower", "oxeye_daisy", "azure_bluet", "dandelion", "short_grass", "tall_grass[half=lower]"} {
		l[i] = laterState(n)
	}
	return l
}()

var sTallGrassHi = laterState("tall_grass[half=upper]")

// meadowU is MeadowFlower's mapped noise: (PSN(x s, z s) + 1) / 2. The products are taken in float32
// (ASSUMED: L28 does not say float or double).
func meadowU(x, z int, s float32) float32 {
	return float32(queryNoise(float32(float32(x)*s), float32(float32(z)*s))+1) / 2
}

// meadowFlower is MeadowFlower (0x5402a80, L28): no draws; the flower from two noises; placed on air
// where it survives; it always returns false. tall_grass is placed as both halves where the double
// plant may go (air above, ASSUMED: L28 left its half handling untested).
func meadowFlower(d *decoration, r *bmt, p pos) bool {
	if p.y < minY || p.y >= maxY {
		return false
	}
	pick := func(w float32) bs {
		if w > 0.99999988 {
			return meadowList[7]
		}
		return meadowList[int(float32(max(w, 0)*8))]
	}
	v := meadowU(p.x, p.z, 0.0005)
	k := 1
	switch {
	case v < 0:
	case v > 1:
		k = 4
	default:
		k = int(float32(3*v + 1))
	}
	var s bs
	if k == 1 {
		s = pick(meadowU(p.x, p.z, 0.0005))
	} else {
		u := min(max(meadowU(p.x, p.z, 0.1), 0), 0.99999988)
		i := int(float32(u * float32(k)))
		s = pick(meadowU(p.x+i*234349, p.z, 0.0005))
	}
	if !d.at(p).isAir() {
		return false
	}
	name := bname(s)
	if !bedrockSurvives(d, name, p) || !bedrockMayPlace(d, name, p) {
		return false
	}
	d.bput(p, s)
	if s == meadowList[7] {
		d.bput(p.add(0, 1, 0), sTallGrassHi)
	}
	return false
}

var (
	sClosedEye  = laterState("closed_eyeblossom")
	bnClosedEye = internBName("closed_eyeblossom")
	sFirefly    = laterState("firefly_bush")
	bnFirefly   = internBName("firefly_bush")
)

// eyeblossom is EyeblossomFeature (0x51631d0, L28): no draws; air required; at generation time 0 the
// closed eyeblossom (MEAS: only closed ones in pale_garden_33). mayPlace (vt+0x220) is taken as the
// bush family's ground rule (ASSUMED).
func eyeblossom(d *decoration, r *bmt, p pos) bool {
	if p.y < minY || p.y > maxY || !d.at(p).isAir() || !bedrockSurvives(d, bnClosedEye, p) {
		return false
	}
	return d.bput(p, sClosedEye)
}

// fireflyCluster is FireflyBushClusterFeature (0x5163380, L28); water is the flag at +0x20 (0 for
// firefly_bush_cluster_feature; 1 for the _water variant, ASSUMED).
type fireflyCluster struct{ water bool }

// fireflyMayPlace is the bush's mayPlace, taken as air with the bush family's ground below (ASSUMED).
func fireflyMayPlace(d *decoration, q pos) bool {
	return q.y >= minY && q.y <= maxY && d.at(q).isAir() && bedrockSurvives(d, bnFirefly, q)
}

func (f fireflyCluster) place(d *decoration, r *bmt, p pos) (pos, bool) {
	for range 20 {
		x := p.x + int(r.nextIntN(9)) - 4
		z := p.z + int(r.nextIntN(9)) - 4
		q := pos{x, d.bedrockHeight(x, z), z}
		if !fireflyMayPlace(d, q) {
			continue
		}
		if f.water {
			// A water block below one of the four side cells (ASSUMED reading of L28's test).
			wet := false
			for _, a := range horizontal {
				if bWaterMat(d.at(q.step(a).add(0, -1, 0))) {
					wet = true
				}
			}
			if !wet {
				continue
			}
		}
		d.bput(q, sFirefly)
		left := int(r.nextIntN(3)) + 1
		for t := 0; t < 20 && left > 0; t++ {
			x2 := q.x + int(r.nextIntN(9)) - 4
			z2 := q.z + int(r.nextIntN(9)) - 4
			q2 := pos{x2, d.bedrockHeight(x2, z2), z2}
			if fireflyMayPlace(d, q2) {
				d.bput(q2, sFirefly)
				left--
			}
		}
		return p, true
	}
	return p, false
}

// swampFoliage is LegacySwampFoliage (0x53fdd00, L28): seagrass, then swamp trees (one in twenty a huge
// mushroom) on the rule's Random, from the chunk corner. The features are the data's, by name (ctor
// 0x53fd7b0).
type swampFoliage struct{ seagrass, grass, mushroom, tree bfeat }

// swampGrassAroundTrees says whether the tall-grass-around-tree scatter runs after each tree. L28: it
// runs unless a level flag (byte +0x564 == 1 and int +0x560 == 0) is set, and which way fresh worlds go
// is not read. ASSUMED to run. It makes no difference to blocks or draws with the 1.26.60 install: the
// newest pack's scatter_tall_grass_around_tree_feature has 0 iterations (MEAS: swampland_17 is the same
// to the block either way).
const swampGrassAroundTrees = true

func (f *swampFoliage) place(d *decoration, r *bmt, p pos) (pos, bool) {
	var trees int
	if r.nextIntN(10) != 0 {
		trees = max(int(r.nextIntN(10)), 7) - 5
	} else {
		trees = int(float32(2 * r.nextFloat()))
	}
	for range 10 {
		x := p.x + int(r.nextIntN(16))
		z := p.z + int(r.nextIntN(16))
		f.seagrass.place(d, r, pos{x, 54, z})
	}
	for range trees {
		t := f.tree
		if r.nextIntN(20) == 0 {
			t = f.mushroom
		}
		z := p.z + int(r.nextIntN(14)) + 1 // z first.
		x := p.x + int(r.nextIntN(14)) + 1
		q := pos{x, d.bedrockHeight(x, z), z}
		t.place(d, r, q)
		if swampGrassAroundTrees {
			f.grass.place(d, r, q)
		}
	}
	return p, true
}

// ---------------------------------------------------------------- springs, desert well, emerald (L29)

// bnSpringHost is SpringFeature's host test (0x4bc8360) in a world of 1.18 or later: stone, or deepslate,
// tuff, calcite, snow, powder snow, packed ice, dirt or coarse dirt. The flag +0x18 that would refuse
// the others is taken as clear (ASSUMED).
var bnSpringHost = bnameSetOf("stone deepslate tuff calcite snow powder_snow packed_ice dirt coarse_dirt")

func springHost(s bs) bool { return bnSpringHost.has(bname(s)) }

// bSpring is SpringFeature::place (0x4bc7ed0, no draws). The liquid's tick is not run: BDS lets it flow
// during generation, which is not reproduced here.
func bSpring(d *decoration, p pos, liquid bs) {
	if p.y <= minY || p.y >= maxY || !springHost(d.at(p.add(0, 1, 0))) || !springHost(d.at(p.add(0, -1, 0))) {
		return
	}
	if c := d.at(p); !c.isAir() && !springHost(c) {
		return
	}
	rock, hole := 0, 0
	for _, o := range [5][3]int{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, -1, 0}} {
		switch b := d.block(p.x+o[0], p.y+o[1], p.z+o[2]); {
		case springHost(b):
			rock++
		case b.isAir():
			hole++
		}
	}
	if rock == 4 && hole == 1 {
		d.bput(p, liquid)
	}
}

// legacySprings is legacy:springs_feature (0x53fd420) in a world of 1.18 or later (newMode, MEAS): 25
// water springs, then 20 lava springs skipped (after their draws) in the biomes of noLava.
type legacySprings struct{ noLava [2]uint64 }

func (f *legacySprings) place(d *decoration, r *bmt, p pos) (pos, bool) {
	for range 25 {
		dz := int(r.nextIntN(16))
		y := int(r.nextIntN(256)) - 64
		dx := int(r.nextIntN(16))
		bSpring(d, pos{p.x + dx, y, p.z + dz}, sWater)
	}
	for range 20 {
		dz := int(r.nextIntN(16))
		y := int(r.nextIntN(r.nextIntN(r.nextIntN(240)))) - 56 // nextInt(0) draws nothing.
		dx := int(r.nextIntN(16))
		q := pos{p.x + dx, y, p.z + dz}
		if b := d.storedBiome(q.x, q.y, q.z); f.noLava[b>>6]&(1<<(b&63)) != 0 {
			continue
		}
		bSpring(d, q, sLava)
	}
	return p, true
}

// newLegacySprings builds the springs with their lava exclusions: frozen oceans, ice plains spikes
// (ice_plains + mutated) and the deep dark (id 190, our deep_dark).
func (fc *featCompiler) newLegacySprings() bfeat {
	bd := fc.bd
	and := func(a, b [2]uint64) [2]uint64 { return [2]uint64{a[0] & b[0], a[1] & b[1]} }
	frozen := and(bd.biomesWithTag("frozen"), bd.biomesWithTag("ocean"))
	spikes := and(bd.biomesWithTag("ice_plains"), bd.biomesWithTag("mutated"))
	f := &legacySprings{noLava: [2]uint64{frozen[0] | spikes[0], frozen[1] | spikes[1]}}
	if i, ok := biomeIdx["deep_dark"]; ok {
		f.noLava[i>>6] |= 1 << (i & 63)
	}
	return f
}

var bnWellGround = bnameSetOf("sand red_sand suspicious_sand")

// bDesertWell is DesertWellFeature (0x515eb90, FEATS-pseudocode and L29): the layout, then (worlds of
// 1.20 and later) two suspicious sand blocks: one of the centre and its four sides at y-1, then one at
// y-2, each with a LootTableSeed drawn after it (ASSUMED order; the seeds are not stored).
func bDesertWell(d *decoration, r *bmt, p pos) bool {
	o := walkDownAir(d, p)
	if !bnWellGround.has(bname(d.at(o))) {
		return false
	}
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			if d.at(o.add(x, -1, z)).isAir() && d.at(o.add(x, -2, z)).isAir() {
				return false
			}
		}
	}
	put := func(x, y, z int, s bs) { d.bput(o.add(x, y, z), s) }
	for y := -2; y <= 0; y++ {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				put(x, y, z, sSandst)
			}
		}
	}
	middle := [5][2]int{{0, 0}, {-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	for _, m := range middle {
		put(m[0], 0, m[1], sWater)
	}
	for _, m := range middle {
		put(m[0], -1, m[1], sSand)
	}
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			if x == -2 || x == 2 || z == -2 || z == 2 {
				put(x, 1, z, sSandst)
			}
		}
	}
	put(2, 1, 0, sSandSlab)
	put(-2, 1, 0, sSandSlab)
	put(0, 1, 2, sSandSlab)
	put(0, 1, -2, sSandSlab)
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			if x == 0 && z == 0 {
				put(x, 4, z, sSandst)
			} else {
				put(x, 4, z, sSandSlab)
			}
		}
	}
	for y := 1; y <= 3; y++ {
		put(-1, y, -1, sSandst)
		put(1, y, -1, sSandst)
		put(-1, y, 1, sSandst)
		put(1, y, 1, sSandst)
	}
	// L = [c, c-x, c+x, c-z, c+z], as middle.
	a := middle[r.nextIntN(5)]
	put(a[0], -1, a[1], sSuspicious)
	r.nextInt()
	b := middle[r.nextIntN(5)]
	put(b[0], -2, b[1], sSuspicious)
	r.nextInt()
	return true
}

var (
	sEmerald   = laterState("emerald_ore")
	sDEmerald  = laterState("deepslate_emerald_ore")
	bnDeepslat = internBName("deepslate")
)

// legacyEmerald is LegacyEmeraldOre (0x53fb950, L29). The vanilla rule has 0 iterations, so it never
// runs (MEAS: 0 of 463 predicted cells hold emerald); written for a pack that runs it.
func legacyEmerald(d *decoration, r *bmt, p pos) bool {
	n := int(r.nextIntN(6)) + 3
	for range n {
		dx := int(r.nextIntN(16))
		dy := int(r.nextIntN(28)) + 4
		dz := int(r.nextIntN(16))
		q := pos{p.x + dx, dy, p.z + dz}
		switch bname(d.at(q)) {
		case bnStone:
			d.bput(q, sEmerald)
		case bnDeepslat:
			d.bput(q, sDEmerald)
		}
	}
	return true
}

// swampFoliageFeature builds legacy:swamp_foliage_feature from the data's features by name.
func (fc *featCompiler) swampFoliageFeature() (bfeat, error) {
	f := &swampFoliage{}
	for _, c := range []struct {
		id  string
		dst *bfeat
	}{{"minecraft:seagrass_feature", &f.seagrass}, {"minecraft:scatter_tall_grass_around_tree_feature", &f.grass},
		{"minecraft:huge_mushroom_feature", &f.mushroom}, {"minecraft:swamp_tree_feature", &f.tree}} {
		g, err := fc.feature(c.id)
		if err != nil {
			return nil, err
		}
		*c.dst = g
	}
	return f, nil
}
