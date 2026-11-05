package structures

import (
	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// The Nether's structures: fortresses and bastions share one region placement (NOTES-bedrock-structures
// 2.2, cited N1: NetherStructureFeatureHelper, spacing 30, separation 4, salt 30084232, linear). The
// region random goes on into the start: after the two placement draws, k = nextInt(6) picks a fortress
// when k < 2, else a bastion, and the start's pieces draw from the same random (P15.2, P10.2). The
// biome test is containsOnly(cx*16, 1, cz*16, 2, list) with the fortress's or the bastion's list (N1
// 2.2); the Nether's biomes are 2D.
//
// Nether structures are placed through the same hook as the Overworld's (worldgen.SetStructures on the
// Nether generator), and each chunk is still made from the seed and its position alone.

var (
	fortressBiomes = idSet(8, 178, 179, 180, 181) // N1 2.2: nether wastes, crimson, warped, soul sand valley, basalt deltas.
	bastionBiomes  = idSet(8, 178, 179, 180)      // N1 2.2: no basalt deltas.
)

const (
	netherMinY = 0
	netherMaxY = 127
	// netherSeaLevel is the Nether's dimension field +0xac (sea level 32): a bastion's start height is
	// one above it (N2 1.3).
	netherSeaLevel = 32
)

// netherStart is region (rx, rz)'s Nether start: its kind, and whether its biome test passes.
func (s *Structures) netherStart(rx, rz int) (Start, bool) {
	sp := spreads[Fortress]
	r := worldgen.NewBedrockRandom(0)
	cx, cz := sp.candidate(s, rx, rz, r)
	st := Start{Kind: Bastion, CX: cx, CZ: cz, rseed: r.Seeded()}
	list := bastionBiomes
	if r.NextIntN(6) < 2 {
		st.Kind, list = Fortress, fortressBiomes
	}
	return st, s.containsOnly(cx*16, 1, cz*16, 2, list)
}

// netherStartsNear calls f with every Nether start whose candidate is within reach chunks of (cx, cz).
func (s *Structures) netherStartsNear(cx, cz, reach int, f func(Start)) {
	sp := spreads[Fortress]
	for rx := floorDiv(cx-reach, sp.spacing); rx <= floorDiv(cx+reach, sp.spacing); rx++ {
		for rz := floorDiv(cz-reach, sp.spacing); rz <= floorDiv(cz+reach, sp.spacing); rz++ {
			st, ok := s.netherStart(rx, rz)
			if ok && abs(st.CX-cx) <= reach && abs(st.CZ-cz) <= reach {
				f(st)
			}
		}
	}
}

// placeNether writes the Nether structure pieces that reach the chunk.
func (s *Structures) placeNether(w *writer) {
	s.netherStartsNear(w.cx, w.cz, max(fortressReach, bastionReach), func(st Start) {
		switch st.Kind {
		case Fortress:
			if abs(st.CX-w.cx) <= fortressReach && abs(st.CZ-w.cz) <= fortressReach {
				w.reseed() // P17.2: a fresh post-process random per structure type.
				s.placeFortress(w, st)
			}
		case Bastion:
			if abs(st.CX-w.cx) <= bastionReach && abs(st.CZ-w.cz) <= bastionReach {
				w.reseed()
				s.placeBastion(w, st)
			}
		}
	})
}
