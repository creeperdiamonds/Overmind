package structures

import (
	"strings"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Villages (N2 1.3, P9.2, P10.1, P11.3): their own depth-first placer on the start's region stream, with
// the tables of pools_village.go and the install's templates.
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
//   - the placer (P11.3, VillPlacer 0xd866720 / 0xd866c20 / 0xd867090) is depth first: a child's whole
//     subtree is built before its parent's next jigsaw; a child inside its parent's box must fit the
//     parent in x and z only and miss, in x and z, the parent's other inside children; a child outside
//     must miss, in x and z, every piece placed so far; no maximum distance, no expansion hack; at the
//     last depth only the fallback pool is tried.

const (
	villageMaxDepth  = 6
	villageAbandoned = 0.02 // P9 / the decompile's constant at 0x1218054.
	// villageReach is how far (chunks) a village's pieces reach from its start. The placer has no distance
	// limit, only its depth, so this is measured, not read: of 30,000 starts over 12 seeds (region
	// candidates, every village type) the farthest piece was 8 chunks out (40 starts; 1,300 at 7, none at
	// 9), and 2 more are kept as a margin. TestPiecesStayWithinReach checks it on 3,456 starts.
	villageReach = 10
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
							if t := d.Template(ve.path); t == nil {
								// BDS keeps an element whose template file is absent (plains_acessory_1's
								// misspelt path, taiga_lamp): valid, so it keeps its weight slots and draws its
								// rotation shuffle, but never attaches (P18.7; as the bastion's, P16.3).
								el = &element{name: e.name, missing: true}
							} else {
								el = &element{name: e.name, template: t, terrain: ve.terrain, bedrockLegacy: true,
									rules: villageRuleLists[ve.rules], abandonedTargets: strings.HasSuffix(ve.tags, "_abandoned_targets")}
							}
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
	return assembleVillage(r, pools, start, st.CX*16, st.CZ*16, rot, s.g.SurfaceWG)
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

// xzOverlap says whether two boxes overlap in x and z (P11.3's tests ignore y).
func xzOverlap(a, b box) bool {
	return a[0] <= b[3] && b[0] <= a[3] && a[2] <= b[5] && b[2] <= a[5]
}

type villagePlacer struct {
	r      *worldgen.BedrockRandom
	pools  map[string]*pool
	height func(x, z int) int
	pieces []*piece
}

// assembleVillage is the village start (0xd866720, P11.3) and its depth-first placer.
func assembleVillage(r *worldgen.BedrockRandom, pools map[string]*pool, start *element, x, z, rot int, height func(x, z int) int) []*piece {
	b := start.bbox([3]int{x, 0, z}, rot)
	cx, cz := b[0]+(b[3]-b[0]+1)/2, b[2]+(b[5]-b[2]+1)/2
	dy := height(cx, cz) - 1 - b[1]
	p := &piece{elem: start, pos: [3]int{x, dy, z}, rot: rot, box: b.shift(dy), deltaY: 1}
	vp := &villagePlacer{r: r, pools: pools, height: height, pieces: []*piece{p}}
	vp.run(p)
	return vp.pieces
}

// run is VillPlacer_run (0xd866c20).
func (v *villagePlacer) run(p *piece) {
	if p.depth > villageMaxDepth {
		return
	}
	js := p.elem.jigsaws(p.pos, p.rot)
	shuffleJigsaws(v.r, js)
	var kids []box
	for i := range js {
		sj := &js[i]
		name := sj.pool
		if p.elem.abandonedTargets {
			name = abandonedPool(name)
		}
		pl := v.pools[name]
		if pl == nil || len(pl.elems) == 0 {
			continue
		}
		fb := v.pools[pl.fallback]
		if fb != nil && len(fb.elems) == 0 {
			fb = nil
		}
		if p.depth == villageMaxDepth {
			if fb != nil {
				v.tryPool(p, sj, fb, &kids)
			}
			continue
		}
		if !v.tryPool(p, sj, pl, &kids) && fb != nil {
			v.tryPool(p, sj, fb, &kids)
		}
	}
}

// tryPool is 0xd867090 (P11.3).
func (v *villagePlacer) tryPool(p *piece, sj *jigsaw, pl *pool, kids *[]box) bool {
	st := faceStep[sj.front]
	target := [3]int{sj.pos[0] + st[0], sj.pos[1] + st[1], sj.pos[2] + st[2]}
	inside := p.box.inside(target)
	h := v.height(sj.pos[0], sj.pos[2])
	idx := make([]int, len(pl.elems))
	for i := range idx {
		idx[i] = i
	}
	shuffleInts(v.r, idx)
	for _, i := range idx {
		e := pl.elems[i]
		if e == nil {
			return true
		}
		rots := []int{1, 2, 3, 0}
		shuffleInts(v.r, rots)
		for _, rot := range rots {
			tjs := e.jigsaws([3]int{}, rot)
			shuffleJigsaws(v.r, tjs)
			for k := range tjs {
				tj := &tjs[k]
				if !canAttach(sj, tj) {
					continue
				}
				base := p.box[1]
				if p.elem.terrain || e.terrain {
					base = h - 1
				}
				raw := [3]int{target[0] - tj.pos[0], target[1] - tj.pos[1], target[2] - tj.pos[2]}
				tb := e.bbox(raw, rot)
				newMin := (sj.pos[1] - (p.box[1] + tj.pos[1])) + base + st[1]
				sh := newMin - tb[1]
				tb = tb.shift(sh)
				raw[1] += sh
				ok := true
				if inside {
					ok = tb[0] >= p.box[0] && tb[3] <= p.box[3] && tb[2] >= p.box[2] && tb[5] <= p.box[5]
					for _, k := range *kids {
						if ok && xzOverlap(k, tb) {
							ok = false
						}
					}
				} else {
					for _, q := range v.pieces {
						if ok && xzOverlap(q.box, tb) {
							ok = false
						}
					}
				}
				if !ok {
					continue
				}
				if inside {
					*kids = append(*kids, tb)
				}
				child := &piece{elem: e, pos: raw, rot: rot, depth: p.depth + 1, box: tb, deltaY: tj.pos[1]}
				v.pieces = append(v.pieces, child)
				v.run(child)
				return true
			}
		}
	}
	return false
}
