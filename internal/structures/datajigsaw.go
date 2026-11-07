package structures

import (
	"slices"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Trail ruins and abandoned camps: data-driven jigsaw structures (N1 2.4 placement, BR assembly), from
// the install's structure, pool and template files. Trial chambers use the same assembly with their
// code-registered pools (trialchambers.go, pools_trial_chambers.go). All three are post-processed here,
// after the legacy types, on one Xoroshiro per chunk (P20.5).

// dataReach is how far (chunks) a data-driven structure's pieces can reach from its start chunk: its
// maximum horizontal distance (camps 116, trail ruins 80) plus a piece.
const dataReach = 11

// dataJigsaw assembles a data-driven start (Piece names the structure), cached like the ancient city.
func (s *Structures) dataJigsaw(st Start) []*piece {
	return s.assembled.get(st, func(st Start) []*piece {
		j := s.data.jigsaws[st.Piece]
		if j == nil {
			return nil
		}
		r := largeFeatureRandom(s.seed, st.CX, st.CZ)
		pieces, _ := assembleJava(r, s.data.dataPools(), j, st.CX, st.CZ, s.g.SurfaceWG)
		return pieces
	})
}

// stubOf is a data-driven start's generation stub: the start piece's box centre in x and z, its base
// (box bottom plus the ground delta of 1) in y; none of these structures names a start jigsaw.
func stubOf(pieces []*piece) [3]int {
	b := pieces[0].box
	return [3]int{(b[0] + b[3]) / 2, b[1] + 1, (b[2] + b[5]) / 2}
}

// trailRuinsBiome is the data-driven structures' biome test (P9.3, read from brJigsaw addPieces): the
// biomes of the whole column (minY to maxY, every quart) at the start piece's centre, taken before the
// heightmap projection with Bedrock's centre min + (max - min + 1) / 2; the start passes if any of them
// is one the structure's JSON filter allows.
func (s *Structures) trailRuinsBiome(st *Start) bool {
	const id = "minecraft:trail_ruins"
	if s.data == nil || s.data.jigsaws[id] == nil {
		return false
	}
	st.Piece = id
	pieces := s.dataJigsaw(*st)
	if len(pieces) == 0 {
		return false
	}
	b := pieces[0].box
	x, z := b[0]+(b[3]-b[0]+1)/2, b[2]+(b[5]-b[2]+1)/2
	return s.columnHasBiome(x, z, s.data.jigsaws[id].biomeFilters)
}

// columnHasBiome says whether any quart of a block column holds a biome the filter allows.
func (s *Structures) columnHasBiome(x, z int, filter []byte) bool {
	seen := map[int]bool{}
	for y := minY; y <= maxY; y += 4 {
		b := s.g.BiomeID(x, y, z) // The quart model; BDS per-block biomes give the same 12 of 12.
		if seen[b] {
			continue
		}
		seen[b] = true
		if s.bd.BiomeFilterMatches(filter, s.biomeName(b)) {
			return true
		}
	}
	return false
}

// dataAdjustments are the br beardifier's records near chunk (cx, cz) (P9.3, Java 1.21's Beardifier):
// every rigid piece of a trail ruin (bury) or abandoned camp (beard_thin) within 12 blocks of the chunk,
// with its ground level delta, and every junction of their pieces within 12 blocks.
func (s *Structures) dataAdjustments(cx, cz int, into []worldgen.TerrainAdjustment) []worldgen.TerrainAdjustment {
	minX, minZ := cx*16, cz*16
	for _, k := range []Kind{TrailRuins, AbandonedCamp} {
		sp := spreads[k]
		for rx := floorDiv(cx-dataReach, sp.spacing); rx <= floorDiv(cx+dataReach, sp.spacing); rx++ {
			for rz := floorDiv(cz-dataReach, sp.spacing); rz <= floorDiv(cz+dataReach, sp.spacing); rz++ {
				st, ok := s.regionStart(k, rx, rz)
				if !ok || abs(st.CX-cx) > dataReach || abs(st.CZ-cz) > dataReach {
					continue
				}
				j := s.data.jigsaws[st.Piece]
				if j == nil {
					continue
				}
				var mode uint8
				switch j.adaptation {
				case "bury":
					mode = worldgen.AdaptBury
				case "beard_thin":
					mode = worldgen.AdaptBeardThin
				default:
					continue // beard_box and encapsulate: no data-driven Overworld structure of ours uses them.
				}
				for _, p := range s.dataJigsaw(st) {
					b := p.box
					if !p.elem.terrain && b[0]-12 <= minX+15 && b[3]+12 >= minX && b[2]-12 <= minZ+15 && b[5]+12 >= minZ {
						into = append(into, worldgen.TerrainAdjustment{Mode: mode, Box: b, DeltaY: p.ground})
					}
					for _, jn := range p.junctions {
						if jn[0] > minX-12 && jn[2] > minZ-12 && jn[0] < minX+15+12 && jn[2] < minZ+15+12 {
							into = append(into, worldgen.TerrainAdjustment{Mode: worldgen.AdaptJunction, Box: [6]int{jn[0], jn[1], jn[2], jn[0], jn[1], jn[2]}})
						}
					}
				}
			}
		}
	}
	return into
}

// containerIDs are the block entity ids of the loot containers a data-driven template holds.
var containerIDs = map[string]string{"chest": "Chest", "trapped_chest": "Chest", "barrel": "Barrel", "decorated_pot": "DecoratedPot",
	"dispenser": "Dispenser", "dropper": "Dropper", "hopper": "Hopper", "shulker_box": "ShulkerBox"}

// decorationSteps are Java's GenerationStep.Decoration names, in order (11 steps, P20.5).
var decorationSteps = []string{"raw_generation", "lakes", "local_modifications", "underground_structures", "surface_structures",
	"strongholds", "underground_ores", "underground_decoration", "fluid_springs", "vegetal_decoration", "top_layer_modification"}

// stepIndex is a step name's index; an unknown or missing step is surface_structures, Java's default.
func stepIndex(name string) int {
	for i, n := range decorationSteps {
		if n == name {
			return i
		}
	}
	return 4
}

// trialStep is the trial chambers' step: registered in code, underground_structures as in Java (not read).
const trialStep = 3

// placeDataDriven is BDS's pass for the data-driven jigsaw structures (NOTES-structures-phase2 P20.5),
// after every legacy type: one Xoroshiro128++ per chunk, seeded with Java's decoration seed formula on
// the chunk coordinates (setSeed(level seed); a = nextLong() | 1, b = nextLong() | 1; setSeed((cx*a +
// cz*b) ^ level seed)), shared by every piece of every start, the starts taken step by step. Inside a step
// BDS takes them in its start cache's hash order (not emulated): here trail ruins, then camps, then trial
// chambers; no measured chunk holds two.
func (s *Structures) placeDataDriven(w *writer) {
	type start struct {
		step   int
		pieces []*piece
	}
	var starts []start
	for _, k := range []Kind{TrailRuins, AbandonedCamp} {
		sp := spreads[k]
		for rx := floorDiv(w.cx-dataReach, sp.spacing); rx <= floorDiv(w.cx+dataReach, sp.spacing); rx++ {
			for rz := floorDiv(w.cz-dataReach, sp.spacing); rz <= floorDiv(w.cz+dataReach, sp.spacing); rz++ {
				st, ok := s.regionStart(k, rx, rz)
				if !ok || abs(st.CX-w.cx) > dataReach || abs(st.CZ-w.cz) > dataReach {
					continue
				}
				if j := s.data.jigsaws[st.Piece]; j != nil {
					starts = append(starts, start{j.step, s.dataJigsaw(st)})
				}
			}
		}
	}
	sp := spreads[TrialChambers]
	for rx := floorDiv(w.cx-trialReach, sp.spacing); rx <= floorDiv(w.cx+trialReach, sp.spacing); rx++ {
		for rz := floorDiv(w.cz-trialReach, sp.spacing); rz <= floorDiv(w.cz+trialReach, sp.spacing); rz++ {
			st, ok := s.regionStart(TrialChambers, rx, rz)
			if ok && abs(st.CX-w.cx) <= trialReach && abs(st.CZ-w.cz) <= trialReach {
				starts = append(starts, start{trialStep, s.trialChambers(st)})
			}
		}
	}
	if len(starts) == 0 {
		return
	}
	slices.SortStableFunc(starts, func(a, b start) int { return a.step - b.step })
	xr := worldgen.NewXoroshiro(s.seed)
	a, b := xr.NextLong()|1, xr.NextLong()|1
	xr.SetSeed((int64(w.cx)*a + int64(w.cz)*b) ^ s.seed)
	cb := w.chunkBox()
	for _, st := range starts {
		for _, p := range st.pieces {
			if p.elem.template != nil && p.box.intersects(cb) {
				s.placeTemplateWith(w, p, xr)
			}
		}
	}
}
