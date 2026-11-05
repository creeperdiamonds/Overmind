package structures

import (
	"strings"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Villages (N2 1.3, P9.2, P10.1): the legacy jigsaw placer on the start's region stream, with the
// tables of pools_village.go and the install's templates.
//   - start (Village_addPieces 0xd841ee0, read in the study's decompile): rotation nextInt(4), then
//     nextFloat() < 0.02 picks the abandoned town centres, then the element nextInt(pool size); maximum
//     depth 6;
//   - pieces start at the generator's height (VillagePiece: the chunk-cached generator heights; ours is
//     the noise stage's first free height) and terrain-matching pieces (streets, terminators) follow it
//     block by block (P9.1: y = height - 1 + local y);
//   - rigid pieces get the legacy beard (TA: VillagePiece mode 0);
//   - an abandoned element's tag rules point its jigsaws' target pools at the abandoned pools (P10.1);
//   - block rules as the ancient city's (P9.1); feature elements (trees, piles) and the actor rules
//     (villagers, golems: mobs come in a later stage) are not placed;
//   - the placer runs with Java's expansion hack (a street's box grows to hold its houses). The notes'
//     legacy placer (N2 1.6) has none; without it no house fits beside a street, so it is used,
//     unconfirmed. NOT VALIDATED: 0 of the 5 BDS village chests are matched (TestVillageChestsMatchBDS).

const (
	villageMaxDepth  = 6
	villageAbandoned = 0.02 // P9 / the decompile's constant at 0x1218054.
	// villageMaxDist is the placer's maximum distance. The argument was not traced in the decompile;
	// 80 is Java's value for villages and is used until it is read.
	villageMaxDist = 80
	villageReach   = 7 // Chunks a village's pieces can reach from its start.
)

// villagePoolType maps a biome's village_type to the pools' type name.
var villagePoolType = map[string]string{"default": "plains", "desert": "desert", "savanna": "savanna", "taiga": "taiga", "ice": "snowy"}

// villagePools builds the village pools once.
func (d *Data) villagePools() map[string]*pool {
	d.villageOnce.Do(func() {
		byName := map[string]*villageElement{}
		for i := range villageElements {
			byName[villageElements[i].name] = &villageElements[i]
		}
		els := map[string]*element{}
		d.village = map[string]*pool{"minecraft:empty": {}}
		for _, p := range villagePools {
			pl := &pool{fallback: p.fallback}
			for _, e := range p.entries {
				var el *element
				if e.name != "" {
					if el = els[e.name]; el == nil {
						ve := byName[e.name]
						switch {
						case ve == nil:
							d.problem("village element %s: not in the element table", e.name)
							continue
						case ve.path == "":
							el = &element{name: e.name} // A feature element: one jigsaw, placed as nothing.
						default:
							t := d.Template(ve.path)
							if t == nil {
								d.problem("village element %s: no template %s", e.name, ve.path)
								continue
							}
							el = &element{name: e.name, template: t, terrain: ve.terrain, bedrockLegacy: true,
								rules: villageRuleLists[ve.rules], abandonedTargets: strings.HasSuffix(ve.tags, "_abandoned_targets")}
						}
						els[e.name] = el
					}
				}
				for range e.weight {
					pl.elems = append(pl.elems, el)
				}
			}
			d.village[p.name] = pl
		}
	})
	return d.village
}

// abandonedPool is a tag rule's rewrite of a jigsaw's target pool (P10.1).
func abandonedPool(pool string) string {
	for _, typ := range villagePoolType {
		for _, kind := range []string{"streets", "houses", "decor"} {
			if pool == "minecraft:village/"+typ+"/"+kind {
				return "minecraft:village/abandoned/" + typ + "/" + kind
			}
		}
	}
	return pool
}

// village assembles a village from its start.
func (s *Structures) village(st Start) []*piece {
	typ := villagePoolType[st.Piece]
	if typ == "" {
		typ = "plains"
	}
	pools := s.data.villagePools()
	r := st.random()
	rot := int(r.NextIntN(4))
	name := "minecraft:village/" + typ + "/town_centers"
	if r.NextFloat() < villageAbandoned {
		name = "minecraft:village/abandoned/" + typ + "/town_centers"
	}
	centre := pools[name]
	if centre == nil || len(centre.elems) == 0 {
		return nil
	}
	start := centre.elems[r.NextIntN(int32(len(centre.elems)))]
	if start == nil || start.template == nil {
		return nil
	}
	return assembleWith(r, pools, start, st.CX*16, st.CZ*16, rot, "", villageMaxDepth, villageMaxDist, s.g.SurfaceWG, true)
}

// villagesNear calls f with the pieces of every village whose start is within reach chunks of (cx, cz).
func (s *Structures) villagesNear(cx, cz, reach int, f func([]*piece)) {
	sp := spreads[Village]
	for rx := floorDiv(cx-reach, sp.spacing); rx <= floorDiv(cx+reach, sp.spacing); rx++ {
		for rz := floorDiv(cz-reach, sp.spacing); rz <= floorDiv(cz+reach, sp.spacing); rz++ {
			st, ok := s.regionStart(Village, rx, rz)
			if ok && abs(st.CX-cx) <= reach && abs(st.CZ-cz) <= reach {
				f(s.assembled.get(st, s.village))
			}
		}
	}
}

// placeVillages writes the pieces of every village reaching the chunk.
func (s *Structures) placeVillages(w *writer) {
	chunkBox := box{w.cx * 16, minY, w.cz * 16, w.cx*16 + 15, maxY, w.cz*16 + 15}
	s.villagesNear(w.cx, w.cz, villageReach, func(pieces []*piece) {
		for _, p := range pieces {
			if p.elem.template == nil {
				continue
			}
			b := p.box
			if p.elem.terrain {
				b[1], b[4] = minY, maxY // Its blocks follow the ground.
			}
			if b.intersects(chunkBox) {
				s.placeTemplate(w, p)
			}
		}
	})
}

// villageAdjustments are the beard records of the rigid village pieces near a chunk (TA).
func (s *Structures) villageAdjustments(cx, cz int, into []worldgen.TerrainAdjustment) []worldgen.TerrainAdjustment {
	s.villagesNear(cx, cz, villageReach+1, func(pieces []*piece) {
		for _, p := range pieces {
			b := p.box
			if p.elem.terrain {
				continue
			}
			if (b[0]-12)>>4 <= cx && cx <= (b[3]+12)>>4 && (b[2]-12)>>4 <= cz && cz <= (b[5]+12)>>4 {
				into = append(into, worldgen.TerrainAdjustment{Mode: worldgen.AdaptBeard, Box: b, DeltaY: p.deltaY})
			}
		}
	})
	return into
}
