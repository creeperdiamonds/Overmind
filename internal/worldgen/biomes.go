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

var (
	plains         = def{biome.Plains{}, surfGrass}
	sunflower      = def{biome.SunflowerPlains{}, surfGrass}
	forest         = def{biome.Forest{}, surfGrass}
	flowerForest   = def{biome.FlowerForest{}, surfGrass}
	birchForest    = def{biome.BirchForest{}, surfGrass}
	darkForest     = def{biome.DarkForest{}, surfGrass}
	taiga          = def{biome.Taiga{}, surfPodzol}
	snowyTaiga     = def{biome.SnowyTaiga{}, surfGrass}
	snowyPlains    = def{biome.SnowyPlains{}, surfGrass}
	iceSpikes      = def{biome.IceSpikes{}, surfSnow}
	desert         = def{biome.Desert{}, surfSand}
	savanna        = def{biome.Savanna{}, surfGrass}
	jungle         = def{biome.Jungle{}, surfGrass}
	swamp          = def{biome.Swamp{}, surfGrass}
	badlands       = def{biome.Badlands{}, surfBadlands}
	meadow         = def{biome.Meadow{}, surfGrass}
	grove          = def{biome.Grove{}, surfSnow}
	snowySlopes    = def{biome.SnowySlopes{}, surfSnowOnStone}
	jaggedPeaks    = def{biome.JaggedPeaks{}, surfSnowOnStone}
	frozenPeaks    = def{biome.FrozenPeaks{}, surfSnowOnStone}
	stonyPeaks     = def{biome.StonyPeaks{}, surfStone}
	windsweptHills = def{biome.WindsweptHills{}, surfWindswept}
	beach          = def{biome.Beach{}, surfSand}
	snowyBeach     = def{biome.SnowyBeach{}, surfSand}
	stonyShore     = def{biome.StonyShore{}, surfShore}
	river          = def{biome.River{}, surfSeabed}
	frozenRiver    = def{biome.FrozenRiver{}, surfColdSeabed}
	mushroom       = def{biome.MushroomFields{}, surfMycelium}

	dripstoneCaves = def{biome.DripstoneCaves{}, surfNone}
	lushCaves      = def{biome.LushCaves{}, surfNone}

	// Oceans by temperature level 0 to 4. Level 2 is the plain ocean.
	oceans = [5]def{
		{biome.FrozenOcean{}, surfColdSeabed}, {biome.ColdOcean{}, surfColdSeabed},
		{biome.Ocean{}, surfColdSeabed}, {biome.LukewarmOcean{}, surfSeabed}, {biome.WarmOcean{}, surfSandSeabed},
	}
	deepOceans = [5]def{
		{biome.DeepFrozenOcean{}, surfColdSeabed}, {biome.DeepColdOcean{}, surfColdSeabed},
		{biome.DeepOcean{}, surfColdSeabed}, {biome.DeepLukewarmOcean{}, surfSeabed}, {biome.DeepWarmOcean{}, surfSandSeabed},
	}
)

// Band edges. Source: minecraft.wiki "World generation", the Overworld biome parameters:
// five temperature and five humidity levels, seven erosion levels, five peaks-and-valleys
// bands. A value's level is how many edges are at or below it.
var (
	temperatureEdges = []float64{-0.45, -0.15, 0.2, 0.55}
	humidityEdges    = []float64{-0.35, -0.1, 0.1, 0.3}
	erosionEdges     = []float64{-0.78, -0.375, -0.2225, 0.05, 0.45, 0.55}
	pvEdges          = []float64{-0.85, -0.2, 0.2, 0.7}
)

// Continentalness bands, from the same wiki page: mushroom fields below -1.05, deep
// ocean to -0.455, ocean to -0.19, coast to -0.11, near-inland to 0.03, mid-inland to
// 0.3, far-inland above.
// beachNarrowing is how far, in continentalness, the beach edge can lie below the
// coast edge. Own tuning.
const beachNarrowing = 0.045

const (
	mushroomBelow = -1.05
	deepBelow     = -0.455
	oceanBelow    = -0.19
	coastBelow    = -0.11
	midInlandTo   = 0.3
)

// Cave biome ranges from the same wiki page: dripstone caves need continentalness 0.8
// to 1, lush caves humidity 0.7 to 1, and both need depth 0.2 to 0.9. Depth grows by
// 1/128 for each block below the surface.
const (
	dripstoneContinentalness = 0.8
	lushHumidity             = 0.7
	caveDepthMin             = 0.2
	caveDepthMax             = 0.9
	depthPerBlock            = 1.0 / 128
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

// Peaks-and-valleys bands.
const (
	pvValley = iota
	pvLow
	pvMid
	pvHigh
	pvPeaks
)

// The tables below pick a biome from temperature level (0 cold to 4 hot), humidity
// level (0 dry to 4 wet) and the sign of weirdness. The wiki's tables name the biomes
// per cell, but they are images that could not be read cell by cell, so the layout is
// our own tuning, built from the wiki's rules: snow only when cold, desert and badlands
// only when hot, jungle hot and wet, and weirdness picking a rarer variant.

// middle is the biome of ordinary ground.
func middle(t, h int, weird bool) def {
	pick := func(a, b def) def {
		if weird {
			return b
		}
		return a
	}
	switch t {
	case 0:
		switch h {
		case 0:
			return pick(snowyPlains, iceSpikes)
		case 1:
			return snowyPlains
		case 2:
			return pick(snowyPlains, snowyTaiga)
		}
		return snowyTaiga
	case 1:
		switch h {
		case 0, 1:
			return plains
		case 2:
			return forest
		}
		return taiga
	case 2:
		switch h {
		case 0:
			return pick(flowerForest, sunflower)
		case 1:
			return plains
		case 2:
			return pick(forest, plains)
		case 3:
			return birchForest
		}
		return darkForest
	case 3:
		if h >= 3 {
			return jungle
		}
		return savanna
	}
	return desert
}

// plateau is the biome of flat high ground.
func plateau(t, h int, weird bool) def {
	switch {
	case t == 4, t == 3 && (h == 2 || h == 3):
		return badlands
	case t >= 1 && t <= 2 && h <= 2:
		return meadow
	}
	return middle(t, h, weird)
}

// peak is the biome of the highest ground.
func peak(t int, weird bool) def {
	switch {
	case t <= 1 && weird:
		return frozenPeaks
	case t <= 1:
		return jaggedPeaks
	case t <= 3:
		return stonyPeaks
	}
	return badlands
}

// slope is the biome of high steep ground.
func slope(t, h int, weird bool) def {
	switch t {
	case 0:
		return snowySlopes
	case 1:
		return grove
	case 2:
		return meadow
	}
	return plateau(t, h, weird)
}

// shattered is the broken, windswept ground.
func shattered(t int) def {
	if t == 4 {
		return badlands
	}
	return windsweptHills
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

// surfaceBiome picks the biome from the sky down to the cave zone for a climate sample.
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
	e := level(cl.Erosion, erosionEdges)
	pv := level(peaksAndValleys(cl.Weirdness), pvEdges)
	weird := cl.Weirdness > 0

	river := math.Abs(cl.River) < riverHalfWidth
	// The beach reaches inland by a varying amount, so its width is not the same
	// everywhere. Own tuning.
	coastEdge := coastBelow - beachNarrowing*(1-clamp((cl.Weirdness+1)/2, 0, 1))
	if c < coastEdge {
		switch {
		case river:
			return riverFor(t)
		case e <= 2:
			return stonyShore
		}
		return beachFor(t)
	}
	switch {
	case river && c < midInlandTo:
		return riverFor(t)
	case e == 6 && (pv == pvLow || pv == pvMid) && c < midInlandTo && h >= 2 && t >= 1 && t <= 3:
		return swamp
	}
	switch pv {
	case pvPeaks:
		switch {
		case e <= 2:
			return peak(t, weird)
		case e <= 4:
			return plateau(t, h, weird)
		case e == 5:
			return shattered(t)
		}
	case pvHigh:
		switch {
		case e <= 1:
			return slope(t, h, weird)
		case e <= 3:
			return plateau(t, h, weird)
		case e == 5:
			return shattered(t)
		}
	case pvMid:
		switch {
		case e <= 1:
			return plateau(t, h, weird)
		case e == 5:
			return shattered(t)
		}
	}
	return middle(t, h, weird)
}

// biomeColumn is what one 4x4 block column holds: the surface biome, and a cave biome
// between two heights when the climate allows one.
type biomeColumn struct {
	surface    def
	cave       def
	hasCave    bool
	caveTop    int // Highest cell centre Y inside the cave band.
	caveBottom int
}

// biomeColumnAt works out the column whose cell centre is at cx, cz.
func (g *Generator) biomeColumnAt(cx, cz int) biomeColumn {
	cl := g.Climate(cx, cz)
	col := biomeColumn{surface: surfaceBiome(cl)}
	dripstone := cl.Continentalness >= dripstoneContinentalness
	lush := cl.Humidity >= lushHumidity
	if !dripstone && !lush {
		return col
	}
	// With both, pick whichever sits deeper inside its range. The wiki says the closest
	// biome wins; this reading of "closest" is our own.
	col.hasCave = true
	col.cave = lushCaves
	if dripstone && (!lush || (cl.Continentalness-dripstoneContinentalness)/0.2 > (cl.Humidity-lushHumidity)/0.3) {
		col.cave = dripstoneCaves
	}
	ground := g.terrainHeight(cx, cz)
	col.caveTop = ground - int(math.Ceil(caveDepthMin/depthPerBlock))
	col.caveBottom = ground - int(math.Floor(caveDepthMax/depthPerBlock))
	return col
}

// at returns the biome in the cell whose centre is at height cy.
func (c *biomeColumn) at(cy int) def {
	if c.hasCave && cy <= c.caveTop && cy >= c.caveBottom {
		return c.cave
	}
	return c.surface
}

// cellCentre returns the centre of the 4-block cell holding v. Biomes are stored per
// 4x4x4 cell, and BiomeAt answers for the cell, so it matches the stored chunk.
func cellCentre(v int) int { return (v>>2)*4 + 2 }

// BiomeAt returns the biome at a block. Biomes change on a 4-block grid, as in the
// game, so every block in a cell gets the same answer. Cave biomes lie only where the
// depth is between 0.2 and 0.9, so a y above the ground gives the surface biome.
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
