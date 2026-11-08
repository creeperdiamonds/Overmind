package structures

import (
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Overworld ruined portals, our own Go from NOTES-portals-fossils-ruins R1 (cited R1). Validated in the
// study on the 14 portal chests of the four measured worlds: template, rotation, mirror and chest x/z
// 14/14, the chest's draw index 10/14. Everything is decided in the start chunk's post-process on its
// random R (the chunk's MT, seeded afresh for the type), and the start chunk writes the whole portal: the
// template, the netherrack around it and the block pass over its box. Here the portal is worked out once
// from the terrain (structures go in before decoration, P21.1) and each chunk takes its part.
//
// Nether ruined portals (NOTES-leftovers L13, cited L13; 9 of 9 measured portals of seed 100 at BDS's
// chest position and y with its air pocket, block pass and loot table, the chest seed's draw index 7 of
// 9) take R1's nether branch with L13's corrections: y = 15 + nextInt(85); the air pocket is
// (y < 32) || (f1 >= .5 && nextFloat() >= .5) (L13 1); the netherrack spread's height is the floor of
// the topmost cave, scanning down from y 127 (L13 2); the chest's seed is drawn when the template is
// placed, before the drip and the block pass (L13 3; the Overworld's already is); no mossiness, the
// blackstone block pass. The Nether's ore and spring features overwrite the portal (L13 4): they are
// decoration, which runs on the structured base (worldgen/structures_base.go).

const portalReach = 3 // Chunks from the start chunk a portal's blocks reach (box plus the spread's 14).

// rpPlan is a worked-out portal: its final blocks and its chest.
type rpPlan struct {
	blocks map[[3]int]uint32
	chest  *chunk.BlockEntity
	// box is the template's box (one block larger than the template, R1); pocket says template air was
	// written. Tests read them.
	box    box
	pocket bool
}

// portalSpread is R1's netherrack chance table by distance.
var portalSpread = [14]float64{1, 1, 1, 1, 1, 1, 1, .9, .9, .8, .7, .6, .4, .2}

// terrainView reads the terrain (the carvers stage, as structures see it) of the chunks around a start,
// with the structure's own writes on top. In the Nether it reads the generator's base cells (Y 0..127).
type terrainView struct {
	s      *Structures
	chunks map[[2]int]*chunk.Chunk
	cells  map[[2]int]worldgen.BaseCells
	over   map[[3]int]uint32
	lo, hi int
}

func (s *Structures) newTerrainView() *terrainView {
	v := &terrainView{s: s, chunks: map[[2]int]*chunk.Chunk{}, over: map[[3]int]uint32{}, lo: minY, hi: maxY}
	if s.nether {
		v.cells, v.lo, v.hi = map[[2]int]worldgen.BaseCells{}, netherMinY, netherMaxY
	}
	return v
}

func (v *terrainView) chunkAt(x, z int) *chunk.Chunk {
	k := [2]int{x >> 4, z >> 4}
	c, ok := v.chunks[k]
	if !ok {
		c = chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
		v.s.g.GenerateBase(k[0], k[1], c)
		v.chunks[k] = c
	}
	return c
}

func (v *terrainView) rid(x, y, z int) uint32 {
	if r, ok := v.over[[3]int{x, y, z}]; ok {
		return r
	}
	if y < v.lo || y > v.hi {
		return airRID()
	}
	if v.cells != nil {
		k := [2]int{x >> 4, z >> 4}
		c, ok := v.cells[k]
		if !ok {
			c = v.s.g.BaseCells(k[0], k[1])
			v.cells[k] = c
		}
		return c.RuntimeID(x, y, z)
	}
	return v.chunkAt(x, z).Block(uint8(x&15), int16(y), uint8(z&15), 0)
}

func (v *terrainView) name(x, y, z int) string { return ridName(v.rid(x, y, z)) }

func (v *terrainView) set(x, y, z int, rid uint32) {
	if y >= v.lo && y <= v.hi && rid != noBlock {
		v.over[[3]int{x, y, z}] = rid
	}
}

// height is the first free y above a column (R1's vt+0x168): above the highest block that is not air, or
// with water false not air nor water either.
func (v *terrainView) height(x, z int, water bool) int {
	for y := maxY; y >= minY; y-- {
		n := v.name(x, y, z)
		if n == "air" || !water && (n == "water" || n == "flowing_water") {
			continue
		}
		return y + 1
	}
	return minY
}

// netherHeight is the Nether's vt+0x168 (L13 2, READ and MEASURED: the chunk scan with
// iteratePastInitialBlocking): from y 127 down past the roof's blocks, then past air, to the first
// block that is not air; the height is one above it, the floor of the topmost cave. Lava counts as a
// block (ASSUMED; L13: lava as a block or as free space gave the same on the 9 portals).
func (v *terrainView) netherHeight(x, z int) int {
	y := netherMaxY
	for y > netherMinY && (y == netherMaxY || v.name(x, y, z) != "air") {
		y--
	}
	for y > netherMinY && v.name(x, y, z) == "air" {
		y--
	}
	return y + 1
}

// solidAt is a block's "solid material" (R1's flag 4), as a full Dragonfly cube.
func (v *terrainView) solidAt(x, y, z int) bool {
	b, ok := world.BlockByRuntimeID(v.rid(x, y, z))
	return ok && fullCube(world.BlockRuntimeID(b))
}

func (s *Structures) biomeTag(id int, tag string) bool {
	return s.bd != nil && s.bd.BiomeHasTag(s.biomeName(id), tag)
}

// portalKind is the ruined portal's placement in this dimension.
func (s *Structures) portalKind() Kind {
	if s.nether {
		return NetherRuinedPortal
	}
	return RuinedPortal
}

// ruinedPortalAt works out the portal of a start chunk, nil if it is not one or places nothing.
func (s *Structures) ruinedPortalAt(cx, cz int) *rpPlan {
	if _, ok := s.StartIn(s.portalKind(), cx, cz); !ok || s.data == nil {
		return nil
	}
	v := s.newTerrainView()
	r := worldgen.NewBedrockRandom(s.g.DecorationSeed(cx, cz))
	ax, az := cx*16, cz*16
	// R1's biome flags come from tags no Nether biome has: in the Nether all are false.
	var floor, desert, cold, jungle, swamp bool
	surf := 0
	if !s.nether {
		surf = s.surface(cx, cz)
		b0 := s.g.BiomeID(ax+8, surf, az+8)
		floor = s.biomeTag(b0, "ocean") || s.biomeTag(b0, "swamp")
		desert, cold = s.biomeTag(b0, "desert"), s.biomeTag(b0, "ice")
		jungle, swamp = s.biomeTag(b0, "jungle"), s.biomeTag(b0, "swamp")
	}

	f1 := r.NextFloat()
	surface := f1 >= 0.5
	rot := int(r.NextIntN(4))
	mirror := r.NextFloat() >= 0.5
	var name string
	if r.NextFloat() < 0.05 {
		name = "giant_portal_" + strconv.Itoa(1+int(r.NextIntN(3)))
	} else {
		name = "portal_" + strconv.Itoa(1+int(r.NextIntN(10)))
	}
	t := s.data.Template("ruined_portal/" + name)
	if t == nil {
		s.data.missingTemplate("ruined portal", "ruined_portal/"+name)
		return nil
	}
	// ASSUMED: no placeholder-box test in the Nether. R1 reads the box from the generator's preliminary
	// height, not read for the Nether's generator; each Nether candidate measured (9 of 9, L13) had its
	// portal.
	if !s.nether {
		h0 := v.height(ax, az, !floor)
		if h0 <= surf-50 || h0-1 > surf+50 { // The placeholder box, y h-50..h+50 (R1).
			return nil
		}
	}
	sx, sy, sz := t.Size[0], t.Size[1], t.Size[2]
	if mirror {
		sz = -sz
	}
	q := rotate([3]int{sx, 0, sz}, rot)
	x0, x1 := min(ax, ax+q[0]), max(ax, ax+q[0])
	z0, z1 := min(az, az+q[2]), max(az, az+q[2])
	sy = t.Size[1]
	ccx, ccz := x0+(x1-x0+1)>>1, z0+(z1-z0+1)>>1
	var y, hc int
	if !s.nether {
		hc = v.height(ccx, ccz, !floor)
	}
	low := minY + 15 // The descent's floor: the dimension's minimum + 15 (R1).
	switch {
	case s.nether:
		low = netherMinY + 15
		y = low + int(r.NextIntN(85)) // R1: 15..99.
	case surface && desert:
		y = hc - sy + int(r.NextIntN(6))
	case surface:
		y = hc - 1
	default:
		mn := minY + 15
		if cb := s.g.BiomeID(ccx, hc, ccz); s.biomeTag(cb, "mountain") || s.biomeTag(cb, "extreme_hills") {
			mn = seaLevelField + 7
		}
		mx := hc - 2 - sy
		y = mx
		if mx > mn {
			y = mn + int(r.NextIntN(int32(mx-mn+1)))
		}
	}
	for y > low {
		n := 0
		for _, c := range [4][2]int{{x0, z0}, {x0, z1}, {x1, z0}, {x1, z1}} {
			bn := v.name(c[0], y-1, c[1])
			if bn != "air" && ((bn == "water" || bn == "flowing_water") && !floor || v.solidAt(c[0], y-1, c[1])) {
				n++
			}
			if n >= 3 {
				break
			}
		}
		if n >= 3 {
			break
		}
		y--
	}
	y0, y1 := y, y+sy

	netherrack, magma := ridNamed("netherrack", nil), ridNamed("magma", nil)
	rackOrMagma := func() uint32 {
		if r.NextFloat() < 0.05 {
			return magma
		}
		return netherrack
	}
	drip := func(x, y, z int) {
		v.set(x, y, z, rackOrMagma())
		for range 8 {
			if r.NextFloat() >= 0.5 {
				break
			}
			y--
			v.set(x, y, z, rackOrMagma())
		}
	}
	off := int(r.NextIntN(int32(max(1, 8-((x1-x0+1)+(z1-z0+1))/4))))
	buried := !surface || desert
	for x := ccx - 14; x <= ccx+14; x++ {
		for z := ccz - 14; z <= ccz+14; z++ {
			d := abs(x-ccx) + abs(z-ccz) + off
			if d >= 14 {
				continue
			}
			if r.NextDouble() >= portalSpread[max(0, d)] {
				continue
			}
			var y2 int
			if s.nether {
				y2 = v.netherHeight(x, z) - 1
			} else {
				y2 = v.height(x, z, !floor) - 1
			}
			if buried {
				y2 = min(y0, y2)
			}
			if abs(y0-y2) > 3 {
				continue
			}
			v.set(x, y2, z, rackOrMagma())
			drip(x, y2-1, z)
		}
	}
	// L13 1 (READ, MEASURED 9/9): in the Nether a portal under y 32 never has a pocket and draws nothing;
	// a surface portal draws its float in both dimensions.
	ignoreAir := s.nether && y0 < netherSeaLevel || surface && (desert || r.NextFloat() >= 0.5)

	plan := &rpPlan{box: box{x0, y0, z0, x1, y1, z1}, pocket: !ignoreAir}
	for _, b := range t.blocks {
		st := t.palette[b.state]
		if st.name == "structure_block" || st.name == "jigsaw" || ignoreAir && st.name == "air" {
			continue
		}
		p := b.pos
		if mirror {
			p[2] = -p[2]
		}
		p = rotate(p, rot)
		x, yy, z := ax+p[0], y0+b.pos[1], az+p[2]
		props := mirrorProps(st.props, map[bool]int{false: 0, true: 1}[mirror])
		res := convert.state(tState{st.name, props}, rot)
		if !res.ok {
			continue
		}
		v.set(x, yy, z, res.rid)
		if res.name == "chest" {
			plan.chest = &chunk.BlockEntity{Pos: [3]int{x, yy, z}, Data: map[string]any{"id": "Chest",
				"LootTable": "loot_tables/chests/ruined_portal.json", "LootTableSeed": r.NextInt()}}
		}
	}
	for x := x0 + 1; x <= x1-1; x++ {
		for z := z0 + 1; z <= z1-1; z++ {
			if v.name(x, y0, z) == "netherrack" {
				drip(x, y0-1, z)
			}
		}
	}

	if s.nether {
		netherPortalPass(v, r, x0, y0, z0, x1, y1, z1)
		plan.blocks = v.over
		return plan
	}
	m := float32(0.2)
	switch {
	case portalSubmerged(v, x0, y0, z0, x1, y1, z1):
		m = 0.8
	case desert:
		m = 0
	case jungle:
		m = 0.8
	case swamp:
		m = 0.5
	}
	stairs := func(name string) uint32 {
		return ridNamed(name, map[string]any{"weirdo_direction": int32(r.NextIntN(4)), "upside_down_bit": r.NextFloat() < 0.5})
	}
	lava := func(n string) bool { return n == "lava" || n == "flowing_lava" }
	for x := x0; x <= x1; x++ {
		for yy := y0; yy <= y1; yy++ {
			for z := z0; z <= z1; z++ {
				n := v.name(x, yy, z)
				switch {
				case portalBricks[n]:
					if r.NextFloat() >= 0.5 {
						a := [2]uint32{ridNamed("cracked_stone_bricks", nil), stairs("stone_brick_stairs")}
						b := [2]uint32{ridNamed("mossy_stone_bricks", nil), stairs("mossy_stone_brick_stairs")}
						f, i := r.NextFloat(), r.NextIntN(2)
						if f < m {
							v.set(x, yy, z, b[i])
						} else {
							v.set(x, yy, z, a[i])
						}
					}
				case n == "stone_brick_stairs":
					if r.NextFloat() >= 0.5 {
						st, _ := world.BlockByRuntimeID(v.rid(x, yy, z))
						_, props := st.EncodeBlock()
						a := [2]uint32{ridNamed("normal_stone_slab", nil), ridNamed("stone_brick_slab", nil)}
						b := [2]uint32{ridNamed("mossy_stone_brick_stairs", props), ridNamed("mossy_stone_brick_slab", nil)}
						f2, i := r.NextFloat(), r.NextIntN(2)
						if f2 < m {
							v.set(x, yy, z, b[i])
						} else {
							v.set(x, yy, z, a[i])
						}
					}
				case portalSlabs[n]:
					if r.NextFloat() < m {
						v.set(x, yy, z, ridNamed("mossy_stone_brick_slab", nil))
					}
				case portalWalls[n]:
					if r.NextFloat() < m {
						v.set(x, yy, z, ridNamed("mossy_stone_brick_wall", nil))
					}
				case n == "netherrack":
					if !cold && r.NextFloat() < 0.1 {
						v.set(x, yy, z, magma)
					}
				case n == "obsidian":
					if r.NextFloat() < 0.2 {
						v.set(x, yy, z, ridNamed("crying_obsidian", nil))
					}
				}
				switch n = v.name(x, yy, z); {
				case lava(n):
					switch {
					case floor:
						v.set(x, yy, z, magma)
					case cold:
						v.set(x, yy, z, netherrack)
					case r.NextFloat() < 0.2:
						v.set(x, yy, z, magma)
					}
				case n == "gold_block":
					if r.NextFloat() < 0.3 && !lava(v.name(x-1, yy, z)) && !lava(v.name(x+1, yy, z)) && !lava(v.name(x, yy, z-1)) &&
						!lava(v.name(x, yy, z+1)) && !lava(v.name(x, yy+1, z)) {
						v.set(x, yy, z, airRID())
					}
				}
			}
		}
	}
	plan.blocks = v.over
	return plan
}

// netherPortalPass is a Nether portal's block pass over its whole box (R1's nether branches, checked on
// L13's 9 portals with 0 violations): no mossiness, the stone bricks to blackstone, then the lava and
// gold rules of both dimensions. A stairs' two draws come before the choice (L13).
func netherPortalPass(v *terrainView, r *worldgen.BedrockRandom, x0, y0, z0, x1, y1, z1 int) {
	magma := ridNamed("magma", nil)
	stairs := func(name string) uint32 {
		return ridNamed(name, map[string]any{"weirdo_direction": int32(r.NextIntN(4)), "upside_down_bit": r.NextFloat() < 0.5})
	}
	pbbSlab := ridNamed("polished_blackstone_brick_slab", nil)
	lava := func(n string) bool { return n == "lava" || n == "flowing_lava" }
	for x := x0; x <= x1; x++ {
		for yy := y0; yy <= y1; yy++ {
			for z := z0; z <= z1; z++ {
				n := v.name(x, yy, z)
				switch {
				case n == "stone":
					st := stairs("polished_blackstone_stairs")
					v.set(x, yy, z, [2]uint32{ridNamed("polished_blackstone", nil), st}[r.NextIntN(2)])
				case n == "chiseled_stone_bricks":
					v.set(x, yy, z, ridNamed("chiseled_polished_blackstone", nil))
				case portalBricks[n]:
					st := stairs("polished_blackstone_brick_stairs")
					v.set(x, yy, z, [3]uint32{ridNamed("polished_blackstone_bricks", nil), ridNamed("cracked_polished_blackstone_bricks", nil), st}[r.NextIntN(3)])
				case n == "stone_brick_stairs":
					b, _ := world.BlockByRuntimeID(v.rid(x, yy, z))
					_, props := b.EncodeBlock()
					if r.NextFloat() >= 0.5 {
						v.set(x, yy, z, pbbSlab)
					} else {
						v.set(x, yy, z, ridNamed("polished_blackstone_brick_stairs", props))
					}
				case portalSlabs[n]:
					v.set(x, yy, z, pbbSlab)
				case portalWalls[n]:
					v.set(x, yy, z, ridNamed("polished_blackstone_brick_wall", nil))
				case n == "iron_bars":
					v.set(x, yy, z, ridNamed("iron_chain", nil)) // R1 E; no sampled template has bars (L13).
				case n == "netherrack":
					if r.NextFloat() < 0.1 { // No Nether biome is cold.
						v.set(x, yy, z, magma)
					}
				case n == "obsidian":
					if r.NextFloat() < 0.2 {
						v.set(x, yy, z, ridNamed("crying_obsidian", nil))
					}
				}
				switch n = v.name(x, yy, z); {
				case lava(n):
					if r.NextFloat() < 0.2 { // Neither ocean floor nor cold in the Nether.
						v.set(x, yy, z, magma)
					}
				case n == "gold_block":
					if r.NextFloat() < 0.3 && !lava(v.name(x-1, yy, z)) && !lava(v.name(x+1, yy, z)) && !lava(v.name(x, yy, z-1)) &&
						!lava(v.name(x, yy, z+1)) && !lava(v.name(x, yy+1, z)) {
						v.set(x, yy, z, airRID())
					}
				}
			}
		}
	}
}

// portalSubmerged says whether any of the box's 8 corners is water (R1, the 1.26.60 branch).
func portalSubmerged(v *terrainView, x0, y0, z0, x1, y1, z1 int) bool {
	for _, x := range [2]int{x0, x1} {
		for _, y := range [2]int{y0, y1} {
			for _, z := range [2]int{z0, z1} {
				if n := v.name(x, y, z); n == "water" || n == "flowing_water" {
					return true
				}
			}
		}
	}
	return false
}

// The block pass's sets (R1 A, C, D), by Bedrock name.
var (
	portalBricks = nameSet("stone_bricks mossy_stone_bricks cracked_stone_bricks chiseled_stone_bricks stone smooth_stone_double_slab " +
		"sandstone_double_slab petrified_oak_double_slab cobblestone_double_slab brick_double_slab stone_brick_double_slab " +
		"quartz_double_slab nether_brick_double_slab oak_double_slab")
	// normal_stone_slab is our conversion of the templates' plain stone slab: MEASURED (round 18, the
	// Nether portal (13,12) of seed 100's fort_100 world) BDS's pass turns it to a blackstone slab, so BDS
	// takes it for one of R1's slabs (its smooth stone slab, assumed: BDS's Java-name mapping of
	// "stone_slab" was not read). The list is one for both dimensions in R1.
	portalSlabs = nameSet("smooth_stone_slab normal_stone_slab sandstone_slab petrified_oak_slab cobblestone_slab brick_slab stone_brick_slab " +
		"quartz_slab nether_brick_slab oak_slab")
	portalWalls = nameSet("granite_wall diorite_wall andesite_wall sandstone_wall brick_wall stone_brick_wall mossy_stone_brick_wall " +
		"nether_brick_wall end_stone_brick_wall prismarine_wall red_sandstone_wall red_nether_brick_wall")
)

func nameSet(names string) map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields(names) {
		m[n] = true
	}
	return m
}

// placeRuinedPortals writes the part inside the chunk of every portal whose start chunk is near.
func (s *Structures) placeRuinedPortals(w *writer) {
	for cx := w.cx - portalReach; cx <= w.cx+portalReach; cx++ {
		for cz := w.cz - portalReach; cz <= w.cz+portalReach; cz++ {
			if _, ok := s.StartIn(s.portalKind(), cx, cz); !ok {
				continue
			}
			p, _ := s.portals.get([2]int{cx, cz}, func() any { return s.ruinedPortalAt(cx, cz) }).(*rpPlan)
			if p == nil {
				continue
			}
			for pos, rid := range p.blocks {
				if w.in(pos[0], pos[1], pos[2]) {
					w.set(pos[0], pos[1], pos[2], rid, false)
				}
			}
			if c := p.chest; c != nil && w.in(c.Pos[0], c.Pos[1], c.Pos[2]) {
				w.bes = append(w.bes, *c)
			}
		}
	}
}
