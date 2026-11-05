package structures

import (
	"math"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Where structures start: our own Go from the study of BDS 1.26.60.6, NOTES-bedrock-structures.md
// (cited as N1 below) section 2 and NOTES-structures-phase2.md (N2). Every Overworld structure is
// placed here; which of them also build pieces is in NOTES.md.

// Kind is a structure feature.
type Kind uint8

const (
	Village   Kind = iota
	Scattered      // Desert pyramid, jungle temple, witch hut, igloo: one feature, the piece chosen by biome (N1 2.2).
	Monument
	Mansion
	Outpost
	BuriedTreasure
	Shipwreck
	OceanRuin
	RuinedPortal
	AncientCity
	TrailRuins
	TrialChambers
	AbandonedCamp
	Mineshaft
	Stronghold
	Fortress // The Nether's: fortress and bastion share one placement (nether.go).
	Bastion
	EndCity // The End's (endcity.go).
	numKinds
)

var kindNames = [numKinds]string{"village", "scattered", "monument", "mansion", "pillager_outpost", "buried_treasure", "shipwreck",
	"ocean_ruin", "ruined_portal", "ancient_city", "trail_ruins", "trial_chambers", "abandoned_camp", "mineshaft", "stronghold",
	"fortress", "bastion_remnant", "end_city"}

func (k Kind) String() string { return kindNames[k] }

// spread is a region placement: one candidate chunk per spacing x spacing region (N1 2.1, 2.4).
type spread struct {
	spacing, separation int
	salt                uint32
	triangular          bool
	java                bool // Java's LCG with a 64-bit seed (N1 2.4) instead of Bedrock's MT.
}

// spreads are the region placements, N1 2.2 (MT) and 2.4 (Java LCG) tables. Shipwrecks and ocean ruins
// use the default mode (24/4 and 20/8 linear), the one fresh 1.26.60 worlds match (N1 2.2 "flag mode", 2.6).
var spreads = [numKinds]spread{
	Village:        {34, 8, 10387312, true, false},
	Scattered:      {32, 8, 14357617, false, false},
	Monument:       {32, 5, 10387313, true, false},
	Mansion:        {80, 20, 10387319, true, false},
	Outpost:        {80, 24, 165745296, true, false},
	BuriedTreasure: {4, 2, 16842397, true, false},
	Shipwreck:      {24, 4, 165745295, false, false},
	OceanRuin:      {20, 8, 14357621, false, false},
	RuinedPortal:   {40, 15, 40552231, false, false},
	AncientCity:    {24, 8, 20083232, true, false},
	TrailRuins:     {34, 8, 83469867, false, true},
	TrialChambers:  {34, 12, 94251327, false, true},
	AbandonedCamp:  {37, 8, 91231127, false, true},
	Fortress:       {30, 4, 30084232, false, false}, // N1 2.2 (NetherStructureFeatureHelper).
	Bastion:        {30, 4, 30084232, false, false},
	EndCity:        {20, 11, 10387313, true, false}, // NOTES-bedrock-end phase 8.
}

// Region seed multipliers: Java's -0x66c60af8 and -0xea9a42b truncated to 32 bits (N1 2.1).
const (
	regionMulX = 0x9939F508
	regionMulZ = 0xF1565BD5
	// Java's RandomSpreadStructurePlacement multipliers (N1 2.4).
	javaMulX = 341873128712
	javaMulZ = 132897987541
)

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// regionRandom seeds Bedrock's random for a region (N1 2.1).
func regionRandom(r *worldgen.BedrockRandom, seed32 uint32, rx, rz int, salt uint32) {
	r.SetSeed(uint32(int32(rx))*regionMulX + uint32(int32(rz))*regionMulZ + seed32 + salt)
}

// candidate is a region's candidate chunk, with the region random left after the placement draws when
// it is Bedrock's (the start's pieces continue that stream, N1 2.1 and 3.1).
func (sp spread) candidate(s *Structures, rx, rz int, r *worldgen.BedrockRandom) (cx, cz int) {
	d := int32(sp.spacing - sp.separation)
	var x, z int32
	if sp.java {
		j := worldgen.NewJavaRandom(int64(rx)*javaMulX + int64(rz)*javaMulZ + s.seed + int64(sp.salt))
		if sp.triangular {
			x = (j.NextInt(d) + j.NextInt(d)) / 2
			z = (j.NextInt(d) + j.NextInt(d)) / 2
		} else {
			x, z = j.NextInt(d), j.NextInt(d)
		}
	} else {
		regionRandom(r, s.seed32, rx, rz, sp.salt)
		if sp.triangular { // 4 draws, the x pair first (N1 2.1).
			x = (r.NextIntN(d) + r.NextIntN(d)) / 2
			z = (r.NextIntN(d) + r.NextIntN(d)) / 2
		} else {
			x, z = r.NextIntN(d), r.NextIntN(d)
		}
	}
	return rx*sp.spacing + int(x), rz*sp.spacing + int(z)
}

// Start is a structure start found by placement.
type Start struct {
	Kind   Kind
	CX, CZ int
	// Piece names the variant: the scattered piece, the village type, the abandoned camp's structure.
	Piece string
	ring  bool // Tested for the stronghold ring walk (P9.5).
	// rseed is the region random's seed; the pieces continue it after the placement draws (N1 3.1).
	rseed uint32
}

// random gives the start's random as the pieces receive it: the region stream after its 2 (linear) or
// 4 (triangular) placement draws (N1 2.1, 3.1).
func (st Start) random() *worldgen.BedrockRandom {
	r := worldgen.NewBedrockRandom(st.rseed)
	if spreads[st.Kind].triangular {
		r.Consume(4)
	} else if st.Kind == Fortress || st.Kind == Bastion {
		r.Consume(3) // The two placement draws and the fortress-or-bastion draw (P15.2).
	} else {
		r.Consume(2)
	}
	return r
}

// biome sets of N1 2.2, as Bedrock numeric biome ids.
var (
	scatteredBiomes = map[int]string{2: "desert_pyramid", 17: "desert_pyramid", 21: "jungle_temple", 22: "jungle_temple",
		6: "witch_hut", 134: "witch_hut", 12: "igloo", 30: "igloo", 184: "igloo"}
	monumentDeep    = idSet(24, 45, 43, 41, 47)
	monumentAround  = idSet(0, 24, 44, 45, 46, 47, 42, 43, 40, 41, 11, 7)
	mansionBiomes   = idSet(0xc1, 0x1d, 0x9d, 0xbc, 0xbb, 0xc2, 0xc4) // N1 2.2 gives them in hex; 187, 188 and 196 are marked unsure there.
	outpostBiomes   = idSet(1, 129, 35, 12, 5, 19, 30, 31, 2, 186, 183, 182, 189, 184, 192, 185)
	treasureBiomes  = idSet(16, 26, 25, 15)
	shipwreckBiomes = idSet(16, 26, 15, 0, 24, 44, 45, 42, 43, 46, 47, 40) // 15 marked unsure in N1 2.2.
	oceanRuinBiomes = idSet(0, 24, 44, 45, 46, 47, 42, 43, 40, 41)
	deepDark        = idSet(190)
)

func idSet(ids ...int) map[int]bool {
	m := map[int]bool{}
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// surface is the "surf" height of N1 2.2: preliminarySurface.get(cx*4, cz*4), the chunk's corner quart
// (N1 2.1). What fills that cache was not read. Measured instead (TestStartsMatchBDS,
// TestStructureWindowStarts): the noise stage's top solid block at the corner (worldgen.NoiseHeight)
// explains every scanned chest, spawner and volume and all eight structure windows; the generator's
// preliminary level, which the surface builder uses, missed one treasure and four windows. It also
// places 3 buried treasure starts on seed 12345 that BDS has no chest for (TestTreasureStartsHaveChests).
func (s *Structures) surface(cx, cz int) int { return s.g.NoiseHeight(cx*16, cz*16) }

// containsOnly is BiomeSource3d::containsOnly (N1 2.1): every sample, 4 blocks apart from x-r, z-r and
// max(y-r, minY), must be in the set.
func (s *Structures) containsOnly(x, y, z, r int, allowed map[int]bool) bool {
	y0, y1 := max(y-r, minY), min(y+r, maxY)
	if y0 > y1 {
		return true
	}
	for X := x - r; X <= x+r; X += 4 {
		for Z := z - r; Z <= z+r; Z += 4 {
			for Y := y0; Y <= y1; Y += 4 {
				if !allowed[s.g.BiomeID(X, Y, Z)] {
					return false
				}
			}
		}
	}
	return true
}

const (
	minY = -64
	maxY = 319
	// seaLevelField is the dimension field +0xac the scattered features test and place at (N1 2.2,
	// 3.1). Its value is not in the notes; 63 is the Overworld's sea level, consistent with the four
	// scattered windows (TestStructureWindowStarts).
	seaLevelField = 63
)

// regionStart is the start of an MT or Java region feature in region (rx, rz), if it has one.
func (s *Structures) regionStart(k Kind, rx, rz int) (Start, bool) {
	sp := spreads[k]
	r := worldgen.NewBedrockRandom(0)
	cx, cz := sp.candidate(s, rx, rz, r)
	st := Start{Kind: k, CX: cx, CZ: cz, rseed: r.Seeded()}
	return st, s.passes(&st)
}

// StartIn is the start whose candidate is chunk (cx, cz), if that chunk is a start of kind k.
func (s *Structures) StartIn(k Kind, cx, cz int) (Start, bool) {
	switch k {
	case Mineshaft:
		return Start{Kind: k, CX: cx, CZ: cz}, s.mineshaftAt(cx, cz)
	case Stronghold:
		return Start{Kind: k, CX: cx, CZ: cz}, s.strongholdAt(cx, cz)
	case Fortress, Bastion:
		sp := spreads[k]
		st, ok := s.netherStart(floorDiv(cx, sp.spacing), floorDiv(cz, sp.spacing))
		return st, ok && st.Kind == k && st.CX == cx && st.CZ == cz
	}
	sp := spreads[k]
	r := worldgen.NewBedrockRandom(0)
	x, z := sp.candidate(s, floorDiv(cx, sp.spacing), floorDiv(cz, sp.spacing), r)
	if x != cx || z != cz {
		return Start{}, false
	}
	st := Start{Kind: k, CX: cx, CZ: cz, rseed: r.Seeded()}
	return st, s.passes(&st)
}

// passes runs a candidate's biome and other tests (N1 2.2), and fills in its variant.
func (s *Structures) passes(st *Start) bool {
	cx, cz := st.CX, st.CZ
	bx, bz := cx*16+8, cz*16+8
	switch st.Kind {
	case Village:
		y := s.surface(cx, cz)
		if st.ring {
			// The stronghold ring's village test reads the generator's preliminary surface (P9.5: 11 of 11
			// located ring strongholds; with the noise height seed 1's second ring stronghold moves).
			y = s.g.PreliminarySurface(cx*16, cz*16)
		}
		t, ok := s.villageType(bx, y, bz)
		if !ok || !s.containsOnly(bx, y, bz, 2, s.villageBiomes) {
			return false
		}
		st.Piece = t
		return true
	case Scattered:
		p, ok := scatteredBiomes[s.g.BiomeID(bx, s.surface(cx, cz), bz)]
		if !ok || s.minChunkSurface(cx, cz) < seaLevelField {
			return false
		}
		st.Piece = p
		return true
	case Monument:
		return s.monumentAt(cx, cz)
	case Mansion:
		return s.containsOnly(bx, s.surface(cx, cz), bz, 32, mansionBiomes)
	case Outpost:
		return s.containsOnly(bx, s.surface(cx, cz), bz, 0, outpostBiomes)
	case BuriedTreasure:
		return s.containsOnly(bx, s.surface(cx, cz), bz, 3, treasureBiomes)
	case Shipwreck:
		y := s.surface(cx, cz)
		if !s.containsOnly(bx, y, bz, 0, shipwreckBiomes) {
			return false
		}
		centre := s.g.BiomeID(bx, y, bz)
		r := 20
		if centre == 15 || centre == 16 || centre == 26 {
			r = 10
		}
		return s.containsOnly(bx, y, bz, r, idSet(centre)) && !s.monumentNear(cx, cz)
	case OceanRuin:
		return s.containsOnly(bx, s.surface(cx, cz), bz, 0, oceanRuinBiomes) && !s.monumentNear(cx, cz)
	case RuinedPortal:
		return true // No biome test in isFeatureChunk (N1 2.2).
	case AncientCity:
		return s.containsOnly(cx*16, -27, cz*16, 0, deepDark)
	case TrailRuins:
		return s.trailRuinsBiome(st)
	case AbandonedCamp:
		return s.jigsawBiome(st, s.campStructures)
	case TrialChambers:
		// The structure set is in code (N1 2.4) and its biome filter was not read: every candidate is
		// kept. See NOTES.md.
		return true
	}
	return false
}

// minChunkSurface is the lowest surface (as surface measures it) over a chunk's 4 x 4 quart corners
// (N1 2.2 "min preliminary surface over the chunk").
func (s *Structures) minChunkSurface(cx, cz int) int {
	m := math.MaxInt
	for qx := range 4 {
		for qz := range 4 {
			m = min(m, s.g.NoiseHeight(cx*16+qx*4, cz*16+qz*4))
		}
	}
	return m
}

// monumentAt is the monument's two biome tests at a candidate (N1 2.2).
func (s *Structures) monumentAt(cx, cz int) bool {
	bx, bz, y := cx*16+8, cz*16+8, s.surface(cx, cz)
	return s.containsOnly(bx, y, bz, 16, monumentDeep) && s.containsOnly(bx, y, bz, 29, monumentAround)
}

// monumentNear says whether a monument starts in chunks cx-5..cx+4 x cz-5..cz+4, which rules out a
// shipwreck or ocean ruin (N1 2.2).
func (s *Structures) monumentNear(cx, cz int) bool {
	sp := spreads[Monument]
	for rx := floorDiv(cx-5, sp.spacing); rx <= floorDiv(cx+4, sp.spacing); rx++ {
		for rz := floorDiv(cz-5, sp.spacing); rz <= floorDiv(cz+4, sp.spacing); rz++ {
			r := worldgen.NewBedrockRandom(0)
			mx, mz := sp.candidate(s, rx, rz, r)
			if mx >= cx-5 && mx <= cx+4 && mz >= cz-5 && mz <= cz+4 && s.monumentAt(mx, mz) {
				return true
			}
		}
	}
	return false
}

// mineshaftAt is the mineshaft's per-chunk test (N1 2.3).
func (s *Structures) mineshaftAt(cx, cz int) bool {
	r := worldgen.NewBedrockRandom(uint32(s.mineB)*uint32(int32(cz)) ^ uint32(s.mineA)*uint32(int32(cx)) ^ s.seed32)
	r.NextInt()
	if !(r.NextFloat() < 0.004) {
		return false
	}
	return int(r.NextIntN(80)) < max(abs(cx), abs(cz))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// jigsawBiome is the biome filter of a data-driven jigsaw structure (N1 2.4: the JSON biome_filters),
// tested at the candidate chunk's centre at surf. With several structures in the set
// (abandoned camps, one per biome), the first whose filter passes is the start's; their filters
// exclude one another. Which position the filter samples was not read; see NOTES.md.
func (s *Structures) jigsawBiome(st *Start, structures []string) bool {
	bx, bz := st.CX*16+8, st.CZ*16+8
	name := s.biomeName(s.g.BiomeID(bx, s.surface(st.CX, st.CZ), bz))
	for _, id := range structures {
		if f, ok := s.jigsawFilters[id]; ok && s.bd.BiomeFilterMatches(f, name) {
			st.Piece = id
			return true
		}
	}
	return false
}

// villageType is the village_type component of the biome at the start (N2 1.3), if the biome has one.
func (s *Structures) villageType(x, y, z int) (string, bool) {
	t, ok := s.villageTypes[s.g.BiomeID(x, y, z)]
	return t, ok
}
