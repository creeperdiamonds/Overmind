package structures

import (
	"strings"
)

// Bastion remnants: the legacy jigsaw placer (N2 1.3 to 1.6, jigsaw.go) with the pools of
// pools_bastion.go and P16.3's three element rules, which closed the study's model on seed 100's two
// hoglin stables (49 of 49 slots, 49 of 50 pieces scoring 0.8 or more):
//  1. an element whose template the install lacks (hoglin_stable small_stables inner_4, outer_4) keeps
//     its weight slots, draws its rotation shuffle on every try and never attaches;
//  2. a mob element (bastion/mobs/*.mcstructure, 1 x 3 x 1) has one jigsaw at (0,0,0), front down, its
//     top the stored rotation turned with the element; melee_piglin_always is named
//     minecraft:melee_connector, the others minecraft:mob_connector;
//  3. bastion_mobs_empty is the real 1 x 1 x 1 template bastion/mobs/empty, not an empty element.
// Mob pieces enter the piece list and the queue and draw nothing when processed. Piglins and hoglins are
// mobs (stage 4): their pieces record a spawn, nothing is spawned.
//
// Start (N2 1.3, P10.2): rot = nextInt(4), kind = nextInt(4) (bridge, treasure, hoglin stable, units),
// the element nextInt(pool size), at (cx*16, cz*16), height the Nether's sea level + 1, maximum depth 15,
// maximum distance 80, no start jigsaw. The block-rule lists the elements name are not applied: their
// contents were not dumped (NOTES.md).

const (
	bastionMaxDepth = 15 // N2 1.3 (0xf).
	bastionMaxDist  = 80 // N2 1.3 (0x50).
	bastionReach    = 7  // Chunks from the start a piece can reach: 80 blocks around the start piece.
	// mobJigsawTop is the mob jigsaws' stored top as a face index, turned with the element (P16.3
	// finding 2: rotation 3, the study's model's face 3).
	mobJigsawTop = 3
)

var bastionStarts = [4]string{"minecraft:bastion/bridge/start", "minecraft:bastion/treasure/starters",
	"minecraft:bastion/hoglin_stable/origin", "minecraft:bastion/units/air_base"}

// bastionPools builds the bastion's pools from the install's templates, once.
func (d *Data) bastionPools() map[string]*pool {
	d.bastionOnce.Do(func() {
		els := map[string]*element{}
		d.bastion = map[string]*pool{"minecraft:empty": {}}
		elementOf := func(name string) *element {
			if el, ok := els[name]; ok {
				return el
			}
			def, ok := bastionElements[name]
			if !ok {
				d.problem("bastion element %s: not in the element table", name)
				return nil
			}
			el := &element{name: name, bedrockLegacy: true}
			switch path := def[0]; {
			case strings.HasPrefix(path, "bastion/mobs/") && path != "bastion/mobs/empty":
				el.mob = strings.TrimPrefix(path, "bastion/mobs/")
				if !d.mcstructures[path] {
					el.missing = true
					break
				}
				jname := "minecraft:mob_connector"
				if el.mob == "melee_piglin_always" {
					jname = "minecraft:melee_connector"
				}
				el.template = &Template{Path: path, Size: [3]int{1, 3, 1}, jigsaws: []jigsaw{{front: 0, top: mobJigsawTop,
					name: jname, target: "minecraft:empty", pool: "minecraft:empty"}}}
			default:
				if el.template = d.Template(path); el.template == nil {
					el.missing = true // P16.3 rule 1.
				}
			}
			els[name] = el
			return el
		}
		for _, p := range bastionPoolDefs {
			pl := &pool{fallback: p.fallback}
			for _, e := range p.entries {
				el := elementOf(e.name)
				if el == nil {
					continue
				}
				for range e.weight {
					pl.elems = append(pl.elems, el)
				}
			}
			d.bastion[p.name] = pl
		}
	})
	return d.bastion
}

// bastionAtStart assembles a bastion from its start.
func (s *Structures) bastionAtStart(st Start) []*piece {
	pools := s.data.bastionPools()
	r := st.random()
	rot := int(r.NextIntN(4))
	start := pools[bastionStarts[r.NextIntN(4)]]
	if start == nil || len(start.elems) == 0 {
		return nil
	}
	el := start.elems[r.NextIntN(int32(len(start.elems)))]
	if el == nil || el.template == nil {
		return nil
	}
	return assemble(r, pools, el, st.CX*16, st.CZ*16, rot, "", bastionMaxDepth, bastionMaxDist,
		func(int, int) int { return netherSeaLevel + 1 })
}

// placeBastion writes the bastion's pieces that reach the chunk, in piece order.
func (s *Structures) placeBastion(w *writer, st Start) {
	pieces := s.assembled.get(st, s.bastionAtStart)
	cb := w.chunkBox()
	for _, p := range pieces {
		if !p.box.intersects(cb) {
			continue
		}
		if p.elem.mob != "" {
			if w.in(p.pos[0], p.pos[1], p.pos[2]) {
				w.spawns = append(w.spawns, spawnIntent{Pos: p.pos, Entity: "minecraft:" + p.elem.mob})
			}
			continue
		}
		if p.elem.template != nil {
			s.placeTemplate(w, p)
		}
	}
}
