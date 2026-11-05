package structures

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Strongholds (N1 2.5): three on rings around the origin, each on the first village chunk near its
// ring point, and one in each 200 x 200 chunk cell with a chance of a quarter.

const (
	ringAttempts = 1000
	ringCount    = 3
	sinScale     = 10430.378 // 65536 / 2pi, the sine table's index scale (N1 2.5).
	ringTurn     = 1.8849558 // 0.6 pi, after a stronghold (N1 2.5).
	ringMiss     = 0.7853982 // pi / 4, after a miss (N1 2.5).
	gridCell     = 200
)

// WarmStrongholds works out the ring strongholds now, so the first eye of ender thrown, on the world's
// tick, does not have to. The app calls it on its own goroutine when the structures attach.
func (s *Structures) WarmStrongholds() { s.rings() }

// rings works out the three ring strongholds once.
func (s *Structures) rings() [][2]int {
	s.ringOnce.Do(func() {
		r := worldgen.NewBedrockRandom(s.seed32)
		angle := r.NextFloat() * float32(2*math.Pi)
		dist := int(r.NextIntN(16)) + 40
		for range ringAttempts {
			if len(s.ringChunks) == ringCount {
				break
			}
			i := int(float32(angle*sinScale + 16384))
			x := int(math.Floor(float64(float32(dist) * worldgen.BedrockSin(i))))
			z := int(math.Floor(float64(float32(dist) * worldgen.BedrockSin(int(float32(angle*sinScale))))))
			found := false
		search:
			for cx := x - 8; cx <= x+7; cx++ {
				for cz := z - 8; cz <= z+7; cz++ {
					if s.ringVillage(cx, cz) {
						s.ringChunks = append(s.ringChunks, [2]int{cx, cz})
						found = true
						break search
					}
				}
			}
			if found {
				dist += 8
				angle += ringTurn
			} else {
				dist += 4
				angle += ringMiss
			}
		}
	})
	return s.ringChunks
}

// gridStronghold is the grid stronghold of the 200-chunk cell holding chunk (cx, cz), if the cell has one
// (N1 2.5).
func (s *Structures) gridStronghold(cx, cz int) ([2]int, bool) {
	base := func(c int) int { return floorDiv(c, gridCell) * gridCell }
	gx, gz := base(cx)+gridCell/2, base(cz)+gridCell/2
	if gx*gx+gz*gz < 100 {
		return [2]int{}, false
	}
	r := worldgen.NewBedrockRandom(s.seed32 + uint32(int32(gx))*uint32(0x100000000-0x645414af) +
		uint32(int32(gz))*uint32(0x100000000-0x41975147) + 0x5d534e7)
	px := base(gx) + int(r.NextIntN(100)) + 50
	pz := base(gz) + int(r.NextIntN(100)) + 50
	ok := float64(r.U32())*(1.0/4294967296.0) < 0.2499999925494194 // N2 phase 3 round 2 (out/sh.c).
	return [2]int{px, pz}, ok && px*px+pz*pz >= 100
}

// strongholdAt says whether chunk (cx, cz) is a stronghold start (N1 2.5: either source).
func (s *Structures) strongholdAt(cx, cz int) bool {
	for _, c := range s.rings() {
		if c == [2]int{cx, cz} {
			return true
		}
	}
	p, ok := s.gridStronghold(cx, cz)
	return ok && p == [2]int{cx, cz}
}

// NearestStronghold is the stronghold start nearest pos, as block (cx*16+2, 0, cz*16+2) of its start
// chunk, the block BDS's locate names (N2 phase 3 round 2): where an eye of ender leads. That block is
// the corner of the stronghold's start piece, the spiral stairs down (strongholdpieces.go), so the eye
// leads to a stronghold that is really there.
func (s *Structures) NearestStronghold(pos cube.Pos) (cube.Pos, bool) {
	if s.nether || s.end {
		return cube.Pos{}, false
	}
	pcx, pcz := floorDiv(pos[0], 16), floorDiv(pos[2], 16)
	best, bestD, found := [2]int{}, int64(math.MaxInt64), false
	try := func(c [2]int) {
		dx, dz := int64(c[0]-pcx), int64(c[1]-pcz)
		if d := dx*dx + dz*dz; d < bestD {
			best, bestD, found = c, d, true
		}
	}
	for _, c := range s.rings() {
		try(c)
	}
	// Grid strongholds: the cells around the player's, widening until no nearer cell can hold one.
	gcx, gcz := floorDiv(pcx, gridCell), floorDiv(pcz, gridCell)
	for ring := 0; ring < 8; ring++ {
		if found {
			near := int64(max(ring-1, 0)) * gridCell
			if near*near > bestD {
				break
			}
		}
		for i := -ring; i <= ring; i++ {
			for j := -ring; j <= ring; j++ {
				if max(abs(i), abs(j)) != ring {
					continue
				}
				if c, ok := s.gridStronghold((gcx+i)*gridCell, (gcz+j)*gridCell); ok {
					try(c)
				}
			}
		}
	}
	if !found {
		return cube.Pos{}, false
	}
	return cube.Pos{best[0]*16 + 2, 0, best[1]*16 + 2}, true
}

// ringVillage is VillageFeature::isFeatureChunk as the ring walk calls it (N1 2.5, P9.5).
func (s *Structures) ringVillage(cx, cz int) bool {
	sp := spreads[Village]
	r := worldgen.NewBedrockRandom(0)
	x, z := sp.candidate(s, floorDiv(cx, sp.spacing), floorDiv(cz, sp.spacing), r)
	if x != cx || z != cz {
		return false
	}
	st := Start{Kind: Village, CX: cx, CZ: cz, rseed: r.Seeded(), ring: true}
	return s.passes(&st)
}
