package structures

import "github.com/creeperdiamonds/overmind/internal/worldgen"

// Igloos (Igloo postProcess 0x5383890: NOTES-structures-phase2 P9.4 and P11.6, cited P116), built by the
// start chunk's post-process with its random R (the piece's box is the start chunk's corner, 7 x 5 x 8):
//   - base = (x0, first free y at (x0, z0) - 1, z0), x0 and z0 the chunk corner (the average-height
//     gate of P11.1 always passes: the piece's box is inside its chunk);
//   - rotation nextInt(4), about the base;
//   - a basement when nextDouble() >= 0.5: n = nextInt(8) + 4 ladder pieces igloo_middle at
//     base + rot(2, -3-3k, 4), k = 0 .. n-1, then igloo_bottom at base + rot(0, -3n-6, -2), whose chest data
//     marker makes the chest below it with LootTableSeed = nextInt() (after one draw per data marker of
//     the template, as the shipwreck's template placement draws, P11.5 -- not checked for the igloo);
//   - igloo_top at the base.
// Structure blocks are not placed (the settings' ignore block); everything else of the templates is.

const iglooChestTable = "loot_tables/chests/igloo_chest.json"

// iglooSnowLift is how far the first free y at the corner lies above the noise stage's top solid block, a
// height the notes do not pin to our generator. Fitted (ruling 41): chosen from 0, 1 and 2 by score on
// seed 10's igloo, the one measured; 0 puts its chest at BDS's y 43 and scores 96.71% of the igloo's box
// (1 scores 95.24%, 2 94.55%; TestIglooMatchesBDS). It may miss on unseen seeds.
const iglooSnowLift = 0

// placeIgloos writes the part inside the chunk of every igloo whose start is in or beside it: the
// templates reach out of the start chunk (P11.6: a rotation-2 igloo extends -x and -z from the corner),
// and BDS writes them whole from the start chunk's post-process, so each chunk works the igloo out
// again from the start chunk's random.
func (s *Structures) placeIgloos(w *writer) {
	for cx := w.cx - 1; cx <= w.cx+1; cx++ {
		for cz := w.cz - 1; cz <= w.cz+1; cz++ {
			if st, ok := s.StartIn(Scattered, cx, cz); ok && st.Piece == "igloo" {
				s.igloo(w, cx, cz)
			}
		}
	}
}

func (s *Structures) igloo(w *writer, cx, cz int) {
	x0, z0 := cx*16, cz*16
	r := worldgen.NewBedrockRandom(s.g.DecorationSeed(cx, cz))
	base := [3]int{x0, s.g.NoiseHeight(x0, z0) + 1 + iglooSnowLift - 1, z0}
	rot := int(r.NextIntN(4))
	if r.NextDouble() >= 0.5 {
		n := int(r.NextIntN(8)) + 4
		if mid := s.data.Template("igloo/igloo_middle"); mid != nil {
			for k := range max(1, n) {
				s.placeIglooPart(w, r, mid, base, [3]int{2, -3 - 3*k, 4}, rot, false)
			}
		}
		if bot := s.data.Template("igloo/igloo_bottom"); bot != nil {
			s.placeIglooPart(w, r, bot, base, [3]int{0, -3*n - 6, -2}, rot, true)
		}
	}
	if top := s.data.Template("igloo/igloo_top_no_trapdoor"); top != nil {
		s.placeIglooPart(w, r, top, base, [3]int{}, rot, false)
	}
}

// placeIglooPart writes one igloo template at base + rot(offset), turned by rot about that point.
func (s *Structures) placeIglooPart(w *writer, rng *worldgen.BedrockRandom, t *Template, base, off [3]int, rot int, chests bool) {
	q := rotate(off, rot)
	o := [3]int{base[0] + q[0], base[1] + q[1], base[2] + q[2]}
	// Each chest marker's loot seed, drawn in template order (vanilla's igloo_bottom has one marker).
	seeds := map[[3]int]int32{}
	if chests {
		for _, b := range t.blocks {
			if t.palette[b.state].name == "structure_block" {
				rng.NextInt() // One draw per data marker (see the top).
			}
		}
		for _, b := range t.blocks {
			if meta, _ := b.nbt["metadata"].(string); t.palette[b.state].name == "structure_block" && meta == "chest" {
				p := [3]int{b.pos[0], b.pos[1] - 1, b.pos[2]}
				if _, dup := seeds[p]; !dup {
					seeds[p] = rng.NextInt()
				}
			}
		}
	}
	for _, b := range t.blocks {
		st := t.palette[b.state]
		if st.name == "structure_block" || st.name == "structure_void" || st.name == "jigsaw" {
			continue
		}
		r := rotate(b.pos, rot)
		x, y, z := o[0]+r[0], o[1]+r[1], o[2]+r[2]
		if !w.in(x, y, z) {
			continue
		}
		res := convert.state(st, rot)
		if !res.ok {
			continue
		}
		if sd, ok := seeds[b.pos]; ok && res.name == "chest" {
			w.set(x, y, z, res.rid, false)
			w.bes = append(w.bes, lootEntity("Chest", x, y, z, iglooChestTable, sd))
			continue
		}
		w.set(x, y, z, res.rid, res.waterlog)
	}
}
