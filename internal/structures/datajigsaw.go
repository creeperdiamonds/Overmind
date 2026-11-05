package structures

// Trail ruins and abandoned camps: data-driven jigsaw structures (N1 2.4 placement, BR assembly), from
// the install's structure, pool and template files. Trial chambers place their starts but are not
// assembled: their pools are registered in code and the study's table of them is not yet trusted (BR:
// the old extract drops the empty entries).

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

// trailRuinsBiome is the trail ruins' biome test. BDS's locate answers are Java's stubs exactly (BR
// trail ruins section), but its biome test is not Java's stub test: fitted, 12 of 12 BDS locate answers
// on 4 worlds pass and the stub-only test fails 2 (ruling 41). The test as fitted: some quart of the
// stub's column, from the stub up to the WG surface, is in a biome the structure's JSON filter allows
// (the notes' ColumnBiomes hypothesis, not read from code).
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
	stub := stubOf(pieces)
	filter := s.data.jigsaws[id].biomeFilters
	top := s.g.SurfaceWG(stub[0], stub[2])
	seen := map[int]bool{}
	for y := stub[1]; y <= max(top, stub[1]); y += 4 {
		b := s.g.BedrockBiomeID(stub[0], y, stub[2])
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

// placeDataJigsaws writes the pieces of every trail ruin and abandoned camp that reaches the chunk. No
// processor is applied (their per-block random was not read), and their terrain adaptation (bury,
// beard_thin) is br::worldgen's own beardifier, which was not read either.
func (s *Structures) placeDataJigsaws(w *writer) {
	chunkBox := box{w.cx * 16, minY, w.cz * 16, w.cx*16 + 15, maxY, w.cz*16 + 15}
	for _, k := range []Kind{TrailRuins, AbandonedCamp} {
		sp := spreads[k]
		for rx := floorDiv(w.cx-dataReach, sp.spacing); rx <= floorDiv(w.cx+dataReach, sp.spacing); rx++ {
			for rz := floorDiv(w.cz-dataReach, sp.spacing); rz <= floorDiv(w.cz+dataReach, sp.spacing); rz++ {
				st, ok := s.regionStart(k, rx, rz)
				if !ok || abs(st.CX-w.cx) > dataReach || abs(st.CZ-w.cz) > dataReach {
					continue
				}
				for _, p := range s.dataJigsaw(st) {
					if p.elem.template != nil && p.box.intersects(chunkBox) {
						s.placeTemplate(w, p)
					}
				}
			}
		}
	}
}
