package worldgen

import (
	"math"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// def pairs a Dragonfly biome with the surface it gets.
type def struct {
	world.Biome
	surface surfaceKind
}

// Every biome the Java 26.1 Overworld generates has a Dragonfly biome. Two have other
// names there: Java's sparse jungle is JungleEdge and wooded badlands is
// WoodedBadlandsPlateau.
var (
	plains          = def{biome.Plains{}, surfGrass}
	sunflower       = def{biome.SunflowerPlains{}, surfGrass}
	forest          = def{biome.Forest{}, surfGrass}
	flowerForest    = def{biome.FlowerForest{}, surfGrass}
	birchForest     = def{biome.BirchForest{}, surfGrass}
	oldBirch        = def{biome.OldGrowthBirchForest{}, surfGrass}
	darkForest      = def{biome.DarkForest{}, surfGrass}
	paleGarden      = def{biome.PaleGarden{}, surfGrass}
	taiga           = def{biome.Taiga{}, surfGrass}
	oldSpruce       = def{biome.OldGrowthSpruceTaiga{}, surfOldGrowth}
	oldPine         = def{biome.OldGrowthPineTaiga{}, surfOldGrowth}
	snowyTaiga      = def{biome.SnowyTaiga{}, surfGrass}
	snowyPlains     = def{biome.SnowyPlains{}, surfGrass}
	iceSpikes       = def{biome.IceSpikes{}, surfSnow}
	desert          = def{biome.Desert{}, surfDesert}
	savanna         = def{biome.Savanna{}, surfGrass}
	savannaPlateau  = def{biome.SavannaPlateau{}, surfGrass}
	windSavanna     = def{biome.WindsweptSavanna{}, surfWindSavanna}
	jungle          = def{biome.Jungle{}, surfGrass}
	sparseJungle    = def{biome.JungleEdge{}, surfGrass}
	bambooJungle    = def{biome.BambooJungle{}, surfGrass}
	swamp           = def{biome.Swamp{}, surfSwamp}
	mangrove        = def{biome.MangroveSwamp{}, surfMangrove}
	badlands        = def{biome.Badlands{}, surfBadlands}
	erodedBadlands  = def{biome.ErodedBadlands{}, surfEroded}
	woodedBadlands  = def{biome.WoodedBadlandsPlateau{}, surfWooded}
	meadow          = def{biome.Meadow{}, surfGrass}
	cherryGrove     = def{biome.CherryGrove{}, surfGrass}
	grove           = def{biome.Grove{}, surfGrove}
	snowySlopes     = def{biome.SnowySlopes{}, surfSnowySlopes}
	jaggedPeaks     = def{biome.JaggedPeaks{}, surfJagged}
	frozenPeaks     = def{biome.FrozenPeaks{}, surfFrozenPeaks}
	stonyPeaks      = def{biome.StonyPeaks{}, surfStonyPeaks}
	windsweptHills  = def{biome.WindsweptHills{}, surfWindswept}
	windsweptForest = def{biome.WindsweptForest{}, surfWindswept}
	gravellyHills   = def{biome.WindsweptGravellyHills{}, surfGravelly}
	beach           = def{biome.Beach{}, surfBeach}
	snowyBeach      = def{biome.SnowyBeach{}, surfBeach}
	stonyShore      = def{biome.StonyShore{}, surfShore}
	river           = def{biome.River{}, surfGrass}
	frozenRiver     = def{biome.FrozenRiver{}, surfGrass}
	mushroom        = def{biome.MushroomFields{}, surfMycelium}

	dripstoneCaves = def{biome.DripstoneCaves{}, surfNone}
	lushCaves      = def{biome.LushCaves{}, surfNone}
	deepDark       = def{biome.DeepDark{}, surfNone}

	// Oceans by temperature level 0 to 4. Level 2 is the plain ocean.
	oceans = [5]def{
		{biome.FrozenOcean{}, surfFrozenOcean}, {biome.ColdOcean{}, surfOcean},
		{biome.Ocean{}, surfOcean}, {biome.LukewarmOcean{}, surfWarmFloor}, {biome.WarmOcean{}, surfBeach},
	}
	// Java has no deep warm ocean: its hottest deep row is the plain warm ocean. Dragonfly
	// does list DeepWarmOcean, but we follow what was measured from Java.
	deepOceans = [5]def{
		{biome.DeepFrozenOcean{}, surfFrozenOcean}, {biome.DeepColdOcean{}, surfOcean},
		{biome.DeepOcean{}, surfOcean}, {biome.DeepLukewarmOcean{}, surfWarmFloor}, {biome.WarmOcean{}, surfBeach},
	}
)

// Band edges and the tables below come from studying the Java 26.1 generator (see
// NOTES.md). A value's level is how many edges are at or below it.
var (
	temperatureEdges = []float64{-0.45, -0.15, 0.2, 0.55}
	humidityEdges    = []float64{-0.35, -0.1, 0.1, 0.3}
	erosionEdges     = []float64{-0.78, -0.375, -0.2225, 0.05, 0.45, 0.55}
)

// Continentalness bands: mushroom fields, deep ocean, ocean, coast, near, mid, far inland.
const (
	mushroomBelow = -1.05
	deepBelow     = -0.455
	oceanBelow    = -0.19
	coastBelow    = -0.11
	nearBelow     = 0.03
	midBelow      = 0.3
)

// Cave biome boxes: dripstone needs continentalness 0.8 up, lush humidity 0.7 up, both in
// the depth range below. Deep dark needs erosion under -0.375 at the depth point 1.1.
const (
	dripstoneContinentalness = 0.8
	lushHumidity             = 0.7
	deepDarkErosion          = -0.375
	caveDepthMin             = 0.2
	caveDepthMax             = 0.9
	deepDarkDepth            = 1.1
)

func level(v float64, edges []float64) int {
	n := 0
	for _, e := range edges {
		if v >= e {
			n++
		}
	}
	return n
}

// Weirdness slices, by |weirdness|.
const (
	sliceValley = iota
	sliceLow
	sliceMid
	sliceHigh
	slicePeak
)

func weirdnessSlice(w float64) int {
	switch w = math.Abs(w); {
	case w < 0.05:
		return sliceValley
	case w < 0.26666667:
		return sliceLow
	case w < 0.4:
		return sliceMid
	case w < 0.56666667:
		return sliceHigh
	case w < 0.76666667:
		return slicePeak
	case w < 0.93333333:
		return sliceHigh
	}
	return sliceMid
}

// pick returns b when weird is set (weirdness 0 or higher), otherwise a.
func pick(weird bool, a, b def) def {
	if weird {
		return b
	}
	return a
}

// middle is the biome of ordinary ground. Rows are temperature, columns humidity.
func middle(t, h int, weird bool) def {
	switch t {
	case 0:
		switch h {
		case 0:
			return pick(weird, snowyPlains, iceSpikes)
		case 1:
			return snowyPlains
		case 2:
			return pick(weird, snowyPlains, snowyTaiga)
		case 3:
			return snowyTaiga
		}
		return taiga
	case 1:
		switch h {
		case 0, 1:
			return plains
		case 2:
			return forest
		case 3:
			return taiga
		}
		return pick(weird, oldSpruce, oldPine)
	case 2:
		switch h {
		case 0:
			return pick(weird, flowerForest, sunflower)
		case 1:
			return plains
		case 2:
			return forest
		case 3:
			return pick(weird, birchForest, oldBirch)
		}
		return darkForest
	case 3:
		switch h {
		case 0, 1:
			return savanna
		case 2:
			return pick(weird, forest, plains)
		case 3:
			return pick(weird, jungle, sparseJungle)
		}
		return pick(weird, jungle, bambooJungle)
	}
	return desert
}

// plateau is the biome of flat high ground.
func plateau(t, h int, weird bool) def {
	switch t {
	case 0:
		switch h {
		case 0:
			return pick(weird, snowyPlains, iceSpikes)
		case 1, 2:
			return snowyPlains
		}
		return snowyTaiga
	case 1:
		switch h {
		case 0:
			return pick(weird, meadow, cherryGrove)
		case 1:
			return meadow
		case 2:
			return pick(weird, forest, meadow)
		case 3:
			return pick(weird, taiga, meadow)
		}
		return pick(weird, oldSpruce, oldPine)
	case 2:
		switch h {
		case 0, 1:
			return pick(weird, meadow, cherryGrove)
		case 2:
			return pick(weird, meadow, forest)
		case 3:
			return pick(weird, meadow, birchForest)
		}
		return paleGarden
	case 3:
		switch h {
		case 0, 1:
			return savannaPlateau
		case 2, 3:
			return forest
		}
		return jungle
	}
	return badlandsPick(h, weird)
}

// badlandsPick is the badlands biome by humidity.
func badlandsPick(h int, weird bool) def {
	switch h {
	case 0, 1:
		return pick(weird, badlands, erodedBadlands)
	case 2:
		return badlands
	}
	return woodedBadlands
}

// shattered is broken windswept ground. Temperature 3 and 4 fall back to middle.
func shattered(t, h int, weird bool) def {
	switch t {
	case 0, 1:
		switch h {
		case 0, 1:
			return gravellyHills
		case 2:
			return windsweptHills
		}
		return windsweptForest
	case 2:
		if h <= 2 {
			return windsweptHills
		}
		return windsweptForest
	}
	return middle(t, h, weird)
}

// peak is the biome of the highest ground.
func peak(t, h int, weird bool) def {
	switch {
	case t <= 2:
		return pick(weird, jaggedPeaks, frozenPeaks)
	case t == 3:
		return stonyPeaks
	}
	return badlandsPick(h, weird)
}

// slope is the biome of high steep ground.
func slope(t, h int, weird bool) def {
	switch {
	case t > 2:
		return plateau(t, h, weird)
	case h <= 1:
		return snowySlopes
	}
	return grove
}

// middleOrBadlands is the middle biome, or badlands when hottest.
func middleOrBadlands(t, h int, weird bool) def {
	if t == 4 {
		return badlandsPick(h, weird)
	}
	return middle(t, h, weird)
}

// middleBadlandsOrSlope also gives a slope when coldest.
func middleBadlandsOrSlope(t, h int, weird bool) def {
	if t == 0 {
		return slope(t, h, weird)
	}
	return middleOrBadlands(t, h, weird)
}

// savannaSwap gives windswept savanna when warm, not wettest and weirdness is 0 or
// higher, otherwise the fallback.
func savannaSwap(t, h int, weird bool, fallback def) def {
	if t >= 2 && h < 4 && weird {
		return windSavanna
	}
	return fallback
}

func riverFor(t int) def {
	if t == 0 {
		return frozenRiver
	}
	return river
}

func beachFor(t int) def {
	switch t {
	case 0:
		return snowyBeach
	case 4:
		return desert
	}
	return beach
}

// wetlandFor is the wettest erosion level inland: swamp when mild, mangrove when hot.
func wetlandFor(t, h int, weird bool) def {
	switch t {
	case 1, 2:
		return swamp
	case 3, 4:
		return mangrove
	}
	return middle(t, h, weird)
}

// Inland bands.
const (
	bandCoast = iota
	bandNear
	bandMid
	bandFar
)

// surfaceBiome picks the surface biome for a climate sample. Rivers are not a separate
// system: a river is the valley slice of weirdness, wherever this table says so.
func surfaceBiome(cl Climate) def {
	t, h := level(cl.Temperature, temperatureEdges), level(cl.Humidity, humidityEdges)
	c := cl.Continentalness
	switch {
	case c < mushroomBelow:
		return mushroom
	case c < deepBelow:
		return deepOceans[t]
	case c < oceanBelow:
		return oceans[t]
	}
	band := bandFar
	switch {
	case c < coastBelow:
		band = bandCoast
	case c < nearBelow:
		band = bandNear
	case c < midBelow:
		band = bandMid
	}
	e := level(cl.Erosion, erosionEdges)
	weird := cl.Weirdness >= 0
	switch weirdnessSlice(cl.Weirdness) {
	case slicePeak:
		return peaksSlice(t, h, e, band, weird)
	case sliceHigh:
		return highSlice(t, h, e, band, weird)
	case sliceMid:
		return midSlice(t, h, e, band, weird)
	case sliceLow:
		return lowSlice(t, h, e, band, weird)
	}
	return valleySlice(t, h, e, band, weird)
}

func peaksSlice(t, h, e, band int, weird bool) def {
	inner := band <= bandNear // Coast or near inland.
	switch e {
	case 0:
		return peak(t, h, weird)
	case 1:
		if inner {
			return middleBadlandsOrSlope(t, h, weird)
		}
		return peak(t, h, weird)
	case 2:
		if inner {
			return middle(t, h, weird)
		}
		return plateau(t, h, weird)
	case 3:
		switch band {
		case bandCoast, bandNear:
			return middle(t, h, weird)
		case bandMid:
			return middleOrBadlands(t, h, weird)
		}
		return plateau(t, h, weird)
	case 5:
		if inner {
			return savannaSwap(t, h, weird, shattered(t, h, weird))
		}
		return shattered(t, h, weird)
	}
	return middle(t, h, weird)
}

func highSlice(t, h, e, band int, weird bool) def {
	inner := band <= bandNear
	switch e {
	case 0:
		switch band {
		case bandCoast:
			return middle(t, h, weird)
		case bandNear:
			return slope(t, h, weird)
		}
		return peak(t, h, weird)
	case 1:
		switch band {
		case bandCoast:
			return middle(t, h, weird)
		case bandNear:
			return middleBadlandsOrSlope(t, h, weird)
		}
		return slope(t, h, weird)
	case 2, 3:
		return peaksSlice(t, h, e, band, weird)
	case 5:
		if inner {
			return savannaSwap(t, h, weird, middle(t, h, weird))
		}
		return shattered(t, h, weird)
	}
	return middle(t, h, weird)
}

func midSlice(t, h, e, band int, weird bool) def {
	switch e {
	case 0:
		if band == bandCoast {
			return stonyShore
		}
		return slope(t, h, weird)
	case 1:
		switch band {
		case bandCoast:
			return stonyShore
		case bandNear, bandMid:
			return middleBadlandsOrSlope(t, h, weird)
		}
		if t == 0 {
			return slope(t, h, weird)
		}
		return plateau(t, h, weird)
	case 2:
		switch band {
		case bandCoast:
			return stonyShore
		case bandNear:
			return middle(t, h, weird)
		case bandMid:
			return middleOrBadlands(t, h, weird)
		}
		return plateau(t, h, weird)
	case 3:
		if band <= bandNear {
			return middle(t, h, weird)
		}
		return middleOrBadlands(t, h, weird)
	case 4:
		if band == bandCoast && !weird {
			return beachFor(t)
		}
		return middle(t, h, weird)
	case 5:
		return coastSwap(t, h, band, weird, shattered(t, h, weird))
	}
	if band == bandCoast {
		if !weird {
			return beachFor(t)
		}
		return middle(t, h, weird)
	}
	return wetlandFor(t, h, weird)
}

// coastSwap is erosion level 5 in the mid and low slices: a beach at negative weirdness
// on the coast, the savanna swap on the coast and near inland, and inland otherwise.
func coastSwap(t, h, band int, weird bool, inland def) def {
	switch band {
	case bandCoast:
		if !weird {
			return beachFor(t)
		}
		return savannaSwap(t, h, weird, middle(t, h, weird))
	case bandNear:
		return savannaSwap(t, h, weird, middle(t, h, weird))
	}
	return inland
}

func lowSlice(t, h, e, band int, weird bool) def {
	switch e {
	case 0, 1:
		switch band {
		case bandCoast:
			return stonyShore
		case bandNear:
			return middleOrBadlands(t, h, weird)
		}
		return middleBadlandsOrSlope(t, h, weird)
	case 2:
		switch band {
		case bandCoast:
			return stonyShore
		case bandNear:
			return middle(t, h, weird)
		}
		return middleOrBadlands(t, h, weird)
	case 3:
		switch band {
		case bandCoast:
			return beachFor(t)
		case bandNear:
			return middle(t, h, weird)
		}
		return middleOrBadlands(t, h, weird)
	case 4:
		if band == bandCoast {
			return beachFor(t)
		}
		return middle(t, h, weird)
	case 5:
		return coastSwap(t, h, band, weird, middle(t, h, weird))
	}
	if band == bandCoast {
		return beachFor(t)
	}
	return wetlandFor(t, h, weird)
}

func valleySlice(t, h, e, band int, weird bool) def {
	switch e {
	case 0, 1:
		switch band {
		case bandCoast:
			if weird {
				return riverFor(t)
			}
			return stonyShore
		case bandNear:
			return riverFor(t)
		}
		return middleOrBadlands(t, h, weird)
	case 6:
		if band == bandCoast {
			return riverFor(t)
		}
		if t == 0 {
			return frozenRiver
		}
		return wetlandFor(t, h, weird)
	}
	return riverFor(t)
}

// biomeColumn is what one 4x4 block column holds: its surface biome, and what is needed
// to tell whether a cave biome is nearer at a given height.
type biomeColumn struct {
	surface def
	ground  float64 // Nominal ground Y, from the offset spline.
	drip    float64 // Climate distance to the dripstone box.
	lush    float64 // Climate distance to the lush box.
	dark    float64 // Climate distance to the deep dark box.
}

// biomeColumnAt works out a column from its climate. No 3D noise is involved: depth is
// the distance under the nominal ground, as in the game.
func (g *Generator) biomeColumnAt(cx, cz int) biomeColumn {
	cl := g.Climate(cx, cz)
	return biomeColumn{
		surface: surfaceBiome(cl),
		ground:  nominalGround(cl.Continentalness, cl.Erosion, cl.Weirdness),
		drip:    max(dripstoneContinentalness-cl.Continentalness, 0),
		lush:    max(lushHumidity-cl.Humidity, 0),
		dark:    max(cl.Erosion-deepDarkErosion, 0),
	}
}

// at returns the biome at height cy. The game takes the nearest parameter box, measuring
// squared distance over climate and depth. Surface boxes always match on climate and sit
// at depth 0 and depth 1, so only depth counts for them. This is the same rule cut down
// to the boxes that can win here.
func (c *biomeColumn) at(cy int) def {
	depth := (c.ground - float64(cy)) / 128
	if depth <= 0 {
		return c.surface
	}
	best := min(depth*depth, (depth-1)*(depth-1))
	out := c.surface
	bandOut := max(caveDepthMin-depth, depth-caveDepthMax, 0)
	if v := c.drip*c.drip + bandOut*bandOut; v < best {
		best, out = v, dripstoneCaves
	}
	if v := c.lush*c.lush + bandOut*bandOut; v < best {
		best, out = v, lushCaves
	}
	if d := depth - deepDarkDepth; c.dark*c.dark+d*d < best {
		out = deepDark
	}
	return out
}

// cellCentre returns the centre of the 4-block cell holding v. Biomes are stored per
// 4x4x4 cell, and BiomeAt answers for the cell, so it matches the stored chunk.
func cellCentre(v int) int { return (v>>2)*4 + 2 }

// BiomeAt returns the biome at a block. Biomes change on a 4-block grid, as they are
// stored, so every block in a cell gets the same answer. A y at or above the nominal
// ground gives the surface biome.
func (g *Generator) BiomeAt(x, y, z int) world.Biome {
	col := g.biomeColumnAt(cellCentre(x), cellCentre(z))
	return col.at(cellCentre(y)).Biome
}

// fillBiomes stores a chunk's biomes, one 4x4x4 cell at a time. It fills cols so the
// surface pass uses the same biomes.
func (g *Generator) fillBiomes(c *chunk.Chunk, originX, originZ int, cols *[4][4]biomeColumn) {
	for i := range cols {
		for j := range cols[i] {
			cols[i][j] = g.biomeColumnAt(originX+i*4+2, originZ+j*4+2)
		}
	}
	for y := minY; y <= maxY; y += 16 {
		var section [4][4][4]uint32
		for i := range section {
			for k := range section[i][0] {
				for j := range section[i] {
					section[i][j][k] = uint32(cols[i][k].at(y + j*4 + 2).EncodeBiome())
				}
			}
		}
		c.SetBiomeSection(int16(y), section)
	}
}
