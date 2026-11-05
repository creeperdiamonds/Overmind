package structures

import "github.com/creeperdiamonds/overmind/internal/worldgen"

// Trial chambers: the br::worldgen jigsaw (brjigsaw.go, Java's placer) with the code-registered pools of
// pools_trial_chambers.go and the registration's settings (NOTES-loot-trial-bastion T1, cited TC1, read
// from 0xd848460): start pool trial_chambers/chamber/end, no start jigsaw, maximum depth 20, maximum
// distance 116 horizontally and vertically, dimension padding 10 at the bottom and top, start height
// uniform(-40, -20) drawn first, no heightmap projection, no expansion hack, and the pool aliases of
// the spawner contents (trialAliases). An element whose template the install lacks
// (hallway/rubble_chamber_fallback, rubble_chamber_thin_fallback) stays in its pool and never attaches.
// Validated in the study: seed 100's (15,-23) start 234 of 236 pieces, 6 of BDS's 13 merged volumes exact.
//
// Not applied: the "encapsulate" terrain adaptation and ignore_waterlogging (their effects were not
// read), the pieces' processors, trial spawners and vaults where Dragonfly has no block (docs/stage-2-known-gaps.md).

const (
	trialMaxDepth = 20  // TC1 +0x98.
	trialMaxDist  = 116 // TC1 +0x9c, +0xa0.
	trialPadding  = 10  // TC1 +0xa4, +0xa8.
	trialReach    = 9   // Chunks from the start a piece can reach: 116 blocks plus a piece.
)

const tc = "minecraft:trial_chambers/"

// trialAliasTargets are TC1's alias bindings in resolution order: a random group of three (ranged and
// slow ranged together), then two random aliases.
var (
	trialGroups = [3][2]string{
		{tc + "spawner/ranged/skeleton", tc + "spawner/slow_ranged/skeleton"},
		{tc + "spawner/ranged/stray", tc + "spawner/slow_ranged/stray"},
		{tc + "spawner/ranged/poison_skeleton", tc + "spawner/slow_ranged/poison_skeleton"},
	}
	trialMelee      = []string{tc + "spawner/melee/zombie", tc + "spawner/melee/husk", tc + "spawner/melee/spider"}
	trialSmallMelee = []string{tc + "spawner/small_melee/slime", tc + "spawner/small_melee/cave_spider",
		tc + "spawner/small_melee/silverfish", tc + "spawner/small_melee/baby_zombie"}
)

func (d *Data) trialPools() map[string]*pool {
	d.trialOnce.Do(func() {
		d.trial = map[string]*pool{"minecraft:empty": {}}
		els := map[string]*element{}
		elementOf := func(path string) *element {
			if el := els[path]; el != nil {
				return el
			}
			el := &element{name: path, missing: true} // TC1: a missing template never attaches.
			if t := d.Template(path); t != nil {
				el = &element{name: path, template: t}
			}
			els[path] = el
			return el
		}
		for _, p := range trialChamberPools {
			pl := &pool{fallback: p.fallback}
			for _, e := range p.entries {
				var el *element
				if e.name != "" {
					el = elementOf(e.name)
				}
				for range e.weight {
					pl.elems = append(pl.elems, el)
				}
			}
			d.trial[p.name] = pl
		}
		// The alias targets' pools (TC1, 0x51ab4e0): one per target, fallback minecraft:empty, its one
		// element the target's template.
		var targets []string
		for _, g := range trialGroups {
			targets = append(targets, g[0], g[1])
		}
		targets = append(append(targets, trialMelee...), trialSmallMelee...)
		for _, name := range targets {
			d.trial[name] = &pool{fallback: "minecraft:empty", elems: []*element{elementOf(name[len("minecraft:"):])}}
		}
	})
	return d.trial
}

// trialAliases is Java's PoolAliasLookup.create for one start (TC1): a Java random forked positionally
// at (cx*16, startY, cz*16) from RandomSource.create(levelSeed), the bindings resolved in order.
func trialAliases(seed int64, cx, cz, startY int) map[string]string {
	f := worldgen.NewJavaRandom(seed).NextLong()
	x, y, z := cx*16, startY, cz*16
	l := int64(int32(x)*3129871) ^ int64(z)*116129781 ^ int64(y)
	l = l*l*42317861 + l*11
	r := worldgen.NewJavaRandom((l >> 16) ^ f)
	g := trialGroups[r.NextInt(3)]
	return map[string]string{
		tc + "spawner/contents/ranged":      g[0],
		tc + "spawner/contents/slow_ranged": g[1],
		tc + "spawner/contents/melee":       trialMelee[r.NextInt(int32(len(trialMelee)))],
		tc + "spawner/contents/small_melee": trialSmallMelee[r.NextInt(int32(len(trialSmallMelee)))],
	}
}

// trialChambers assembles a start.
func (s *Structures) trialChambers(st Start) []*piece {
	return s.assembled.get(st, func(st Start) []*piece {
		r := largeFeatureRandom(s.seed, st.CX, st.CZ)
		y := -40 + int(r.NextInt(21)) // uniform(-40, -20), Java's UniformHeight.
		j := &jsonJigsaw{startPool: tc + "chamber/end", maxDepth: trialMaxDepth, maxH: trialMaxDist, maxV: trialMaxDist,
			padBottom: trialPadding, padTop: trialPadding, startY: y, aliases: trialAliases(s.seed, st.CX, st.CZ, y)}
		pieces, _ := assembleJava(r, s.data.trialPools(), j, st.CX, st.CZ, s.g.SurfaceWG)
		return pieces
	})
}

// placeTrialChambers writes the trial chamber pieces that reach the chunk.
func (s *Structures) placeTrialChambers(w *writer) {
	cb := w.chunkBox()
	sp := spreads[TrialChambers]
	for rx := floorDiv(w.cx-trialReach, sp.spacing); rx <= floorDiv(w.cx+trialReach, sp.spacing); rx++ {
		for rz := floorDiv(w.cz-trialReach, sp.spacing); rz <= floorDiv(w.cz+trialReach, sp.spacing); rz++ {
			st, ok := s.regionStart(TrialChambers, rx, rz)
			if !ok || abs(st.CX-w.cx) > trialReach || abs(st.CZ-w.cz) > trialReach {
				continue
			}
			for _, p := range s.trialChambers(st) {
				if p.elem.template != nil && p.box.intersects(cb) {
					s.placeTemplate(w, p)
				}
			}
		}
	}
}
