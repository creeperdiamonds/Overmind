package worldgen

import (
	"math"
	"strings"
	"sync"
)

// Surface rules follow Mojang's algorithm, per the owner's decision (NOTES-exact-features.md section
// 5), in our own Go. The rule tree itself is written out as plain code in ruleAt: the game's tree is
// data; here it is a function of the context, tested block by block against the Java surface stage.

// noRule is what a rule returns when it places nothing.
const noRule bs = 0xFFFF

// Our indexes of the biome table's names.
var biomeIdx = func() map[string]uint8 {
	m := map[string]uint8{}
	for i, n := range biomeTableNames {
		m[n] = uint8(i)
	}
	return m
}()

var (
	bBadlands, bEroded, bWooded         = biomeIdx["badlands"], biomeIdx["eroded_badlands"], biomeIdx["wooded_badlands"]
	bFrozenOcean, bDeepFrozen           = biomeIdx["frozen_ocean"], biomeIdx["deep_frozen_ocean"]
	bFrozenPeaks, bSnowySlopes, bJagged = biomeIdx["frozen_peaks"], biomeIdx["snowy_slopes"], biomeIdx["jagged_peaks"]
	bGrove, bStonyPeaks, bStonyShore    = biomeIdx["grove"], biomeIdx["stony_peaks"], biomeIdx["stony_shore"]
	bWindswept, bWindSavanna, bGravelly = biomeIdx["windswept_hills"], biomeIdx["windswept_savanna"], biomeIdx["windswept_gravelly_hills"]
	bWarmOcean, bBeach, bSnowyBeach     = biomeIdx["warm_ocean"], biomeIdx["beach"], biomeIdx["snowy_beach"]
	bLukewarm, bDeepLukewarm            = biomeIdx["lukewarm_ocean"], biomeIdx["deep_lukewarm_ocean"]
	bDesert, bDripstone, bMangrove      = biomeIdx["desert"], biomeIdx["dripstone_caves"], biomeIdx["mangrove_swamp"]
	bSwamp, bOldPine, bOldSpruce        = biomeIdx["swamp"], biomeIdx["old_growth_pine_taiga"], biomeIdx["old_growth_spruce_taiga"]
	bIceSpikes, bMushroom               = biomeIdx["ice_spikes"], biomeIdx["mushroom_fields"]
)

// biomeClimate is a biome's base temperature and whether its temperature has the frozen modifier.
type biomeClimate struct {
	temp   float32
	frozen bool
}

// climateTable is every biome's climate, by biome table index.
type climateTable [len(biomeTableNames)]biomeClimate

// biomeClimates holds each biome's temperature: Java's values. Numbers are the game's, tested through
// the surface stage's ice and the freeze pass.
var biomeClimates = func() (t climateTable) {
	for name, v := range map[string]float32{
		"plains": 0.8, "sunflower_plains": 0.8, "snowy_plains": 0, "ice_spikes": 0, "desert": 2, "swamp": 0.8, "mangrove_swamp": 0.8,
		"forest": 0.7, "flower_forest": 0.7, "birch_forest": 0.6, "dark_forest": 0.7, "pale_garden": 0.7, "old_growth_birch_forest": 0.6,
		"old_growth_pine_taiga": 0.3, "old_growth_spruce_taiga": 0.25, "taiga": 0.25, "snowy_taiga": -0.5, "savanna": 2, "savanna_plateau": 2,
		"windswept_hills": 0.2, "windswept_gravelly_hills": 0.2, "windswept_forest": 0.2, "windswept_savanna": 2,
		"jungle": 0.95, "sparse_jungle": 0.95, "bamboo_jungle": 0.95, "badlands": 2, "eroded_badlands": 2, "wooded_badlands": 2,
		"meadow": 0.5, "cherry_grove": 0.5, "grove": -0.2, "snowy_slopes": -0.3, "frozen_peaks": -0.7, "jagged_peaks": -0.7, "stony_peaks": 1,
		"river": 0.5, "frozen_river": 0, "beach": 0.8, "snowy_beach": 0.05, "stony_shore": 0.2, "warm_ocean": 0.5, "lukewarm_ocean": 0.5,
		"deep_lukewarm_ocean": 0.5, "ocean": 0.5, "deep_ocean": 0.5, "cold_ocean": 0.5, "deep_cold_ocean": 0.5, "frozen_ocean": 0,
		"deep_frozen_ocean": 0.5, "mushroom_fields": 0.9, "dripstone_caves": 0.8, "lush_caves": 0.5, "deep_dark": 0.8,
	} {
		t[biomeIdx[name]] = biomeClimate{temp: v}
	}
	t[biomeIdx["frozen_ocean"]].frozen = true
	t[biomeIdx["deep_frozen_ocean"]].frozen = true
	return t
}()

// bedrockClimates is Java's table with Bedrock's values where its data files differ (BEDROCK-NOTES
// section 5 row 5): savanna 1.2, savanna plateau 1.0, meadow and cherry grove 0.3, lush caves 0.9,
// dripstone caves 0.2. The name mapping to Java's biomes is the study's.
var bedrockClimates = func() climateTable {
	t := biomeClimates
	for name, v := range map[string]float32{"savanna": 1.2, "savanna_plateau": 1.0, "meadow": 0.3, "cherry_grove": 0.3,
		"lush_caves": 0.9, "dripstone_caves": 0.2} {
		t[biomeIdx[name]].temp = v
	}
	return t
}()

// temperature is a biome's temperature at a block, from this table.
func (t *climateTable) temperature(b uint8, x, y, z int) float32 {
	c := t[b]
	adj := c.temp
	if c.frozen {
		fx, fz := float64(x), float64(z)
		// The study's notes leave out the game's factor of 7 on this noise (found in fix round 1 of task 4c).
		a := float64(fixedNoises().frozen.value(float64(fx*0.05), float64(fz*0.05)) * 7.0)
		bb := fixedNoises().info.value(float64(fx*0.2), float64(fz*0.2))
		if a+bb < 0.3 && fixedNoises().info.value(float64(fx*0.09), float64(fz*0.09)) < 0.8 {
			adj = 0.2
		}
	}
	if y > 80 {
		v := float32(float64(fixedNoises().temp.value(float64(float32(x)/8.0), float64(float32(z)/8.0)) * 8.0))
		return adj - float32(float32((v+float32(y))-80.0)*0.05)/40.0
	}
	return adj
}

// coldEnough says whether a biome freezes at a block under this generator's profile.
func (g *Generator) coldEnough(b uint8, x, y, z int) bool {
	return g.profile.climate.temperature(b, x, y, z) < 0.15
}

// quartColumn is one quart column's stored biomes, by table index. A pure function of the seed and
// the position, it lives in pooled scratch and changes speed and nothing else.
type quartColumn struct {
	owner  *Generator
	qx, qz int32
	ids    [(maxY - minY + 1) / 4]uint8
}

const quartSlots = 1 << 12

// quartColumnAt finds or works out a quart column.
func (s *chunkScratch) quartColumnAt(g *Generator, qx, qz int) *quartColumn {
	q := &s.quarts[(qx&63)|(qz&63)<<6]
	if q.owner != g || q.qx != int32(qx) || q.qz != int32(qz) {
		q.owner, q.qx, q.qz = g, int32(qx), int32(qz)
		col := g.biomeColumnAt(qx*4, qz*4)
		t := col.target
		n := col.sky
		for i := range q.ids {
			d := quantize(depthAt(minY+i*4, col.offset))
			t[biomeTargetDepth] = d
			n = nearestBiomeFrom(&t, n)
			id := uint8(biomeRows[n.row][0])
			q.ids[i] = id
			if d < 0 && n.bounds[biomeTargetDepth][0] == 0 {
				for j := i + 1; j < len(q.ids); j++ {
					q.ids[j] = id
				}
				break
			}
		}
	}
	return q
}

func (s *chunkScratch) quartBiome(g *Generator, qx, qy, qz int) uint8 {
	qy = min(max(qy-minY>>2, 0), (maxY-minY+1)/4-1)
	return s.quartColumnAt(g, qx, qz).ids[qy]
}

// blockBiome is the zoomed biome at a block. If all 8 quarts a block can draw from agree, the jitter
// cannot change the answer and is skipped.
func (s *chunkScratch) blockBiome(g *Generator, x, y, z int) uint8 {
	px, py, pz := (x-2)>>2, (y-2)>>2, (z-2)>>2
	first := s.quartBiome(g, px, py, pz)
	same := true
	for i := 1; i < 8 && same; i++ {
		same = s.quartBiome(g, px+i>>2&1, py+i>>1&1, pz+i&1) == first
	}
	if same {
		return first
	}
	qx, qy, qz := g.zoomQuart(x, y, z)
	return s.quartBiome(g, qx, qy, qz)
}

// surfaceFactories are the positional generators the rules draw from.
type surfaceFactories struct {
	world, bedrock, deepslate seedFactory
	bands                     [192]bs
}

func newSurfaceFactories(w seedFactory, deepslateName string) surfaceFactories {
	bed := w.named("minecraft:bedrock_floor")
	deep := w.named(deepslateName)
	f := surfaceFactories{world: w, bedrock: bed.forkFactory(), deepslate: deep.forkFactory()}
	r := w.named("minecraft:clay_bands")
	for i := range f.bands {
		f.bands[i] = sTerracotta
	}
	for i := 0; i < 192; i++ {
		i += r.nextInt(5) + 1
		if i < 192 {
			f.bands[i] = sTerrOrange
		}
	}
	bands := func(w int, st bs) {
		c := r.nextInt(10) + 6
		for range c {
			width := w + r.nextInt(3)
			start := r.nextInt(192)
			for p := 0; start+p < 192 && p < width; p++ {
				f.bands[start+p] = st
			}
		}
	}
	bands(1, sTerrYellow)
	bands(2, sTerrBrown)
	bands(1, sTerrRed)
	n := r.nextInt(7) + 9
	for i, at := 0, 0; i < n && at < 192; i++ {
		f.bands[at] = sTerrWhite
		if at-1 > 0 && r.next()&1 != 0 {
			f.bands[at-1] = sTerrLight
		}
		if at+1 < 192 && r.next()&1 != 0 {
			f.bands[at+1] = sTerrLight
		}
		at += r.nextInt(16) + 4
	}
	return f
}

// ruleContext is what the rules read about the block under test.
type ruleContext struct {
	g *Generator
	b *baseChunk
	s *chunkScratch

	x, z, lx, lz int
	depth        int
	minLevel     int
	surfaceN     float64
	haveSurfaceN bool
	secondary    float64
	haveSecond   bool

	y, above, below int
	waterTop        int
	haveBelow       bool
	biomeY          int
	biome           uint8
}

const noWaterTop = math.MinInt32

// startColumn sets the column's values: surface depth, secondary noise and the lowest surface level.
func (c *ruleContext) startColumn(x, z int) {
	g := c.g
	c.x, c.z, c.lx, c.lz = x, z, x&15, z&15
	c.haveSecond = false
	c.surfaceN = g.surface.depth.at(float64(x), 0, float64(z))
	c.haveSurfaceN = true
	r := g.sf.world.at(int32(x), 0, int32(z))
	c.depth = int(float64(float64(c.surfaceN*2.75)+3.0) + float64(r.nextDouble()*0.25))

	cx, cz := x>>4, z>>4
	var p [4]float64
	for i, o := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		p[i] = float64(g.preliminarySurface(&c.s.prelims, (cx+o[0])*16, (cz+o[1])*16))
	}
	fx, fz := float64(float32(x&15)/16), float64(float32(z&15)/16)
	v := lerp(lerp(p[0], p[1], fx), lerp(p[2], p[3], fx), fz)
	c.minLevel = int(math.Floor(v)) + c.depth - 8
}

func (c *ruleContext) second() float64 {
	if !c.haveSecond {
		c.secondary = c.g.surface.second.at(float64(c.x), 0, float64(c.z))
		c.haveSecond = true
	}
	return c.secondary
}

func (c *ruleContext) setY(above, y, waterTop int) {
	c.above, c.y, c.waterTop = above, y, waterTop
	c.haveBelow = false
	c.biomeY = minY - 100
}

// biomeHere is the zoomed biome at the block, worked out once.
func (c *ruleContext) biomeHere() uint8 {
	if c.biomeY != c.y {
		c.biomeY = c.y
		c.biome = c.s.blockBiome(c.g, c.x, c.y, c.z)
	}
	return c.biome
}

// stoneBelow is the stone under this block until the next open space, reading the column as it is.
func (c *ruleContext) stoneBelow() int {
	if !c.haveBelow {
		c.haveBelow = true
		ceil := minY - 1
		for ly := c.y - 1; ly >= minY; ly-- {
			if s := c.b.b[cellIndex(c.lx, ly, c.lz)]; s.isAir() || s.has(fLiquid) {
				ceil = ly
				break
			}
		}
		c.below = c.y - (ceil + 1) + 1
	}
	return c.below
}

func (c *ruleContext) onFloor() bool    { return c.above <= 1 }
func (c *ruleContext) onCeiling() bool  { return c.stoneBelow() <= 1 }
func (c *ruleContext) underFloor() bool { return c.above <= 1+c.depth }

func (c *ruleContext) secondaryRange(r float64) int {
	return int(float64(((c.second() - (-1.0)) / 2.0) * r))
}

func (c *ruleContext) deepUnder() bool     { return c.above <= 1+c.depth+c.secondaryRange(6) }
func (c *ruleContext) veryDeepUnder() bool { return c.above <= 1+c.depth+c.secondaryRange(30) }

// waterAt is the water(offset, multiplier, addStone) condition.
func (c *ruleContext) waterAt(offset, mult int, addStone bool) bool {
	if c.waterTop == noWaterTop {
		return true
	}
	y := c.y
	if addStone {
		y += c.above
	}
	return y >= c.waterTop+offset+c.depth*mult
}

// yAbove is the y_above(anchor, multiplier, addStone) condition.
func (c *ruleContext) yAbove(anchor, mult int, addStone bool) bool {
	y := c.y
	if addStone {
		y += c.above
	}
	return y >= anchor+c.depth*mult
}

func (c *ruleContext) noiseIn(n *normalNoise, lo, hi float64) bool {
	v := n.at(float64(c.x), 0, float64(c.z))
	return v >= lo && v <= hi
}

// surfaceAbove is the surface noise over a mark given in surface-depth terms, as the game writes it.
func (c *ruleContext) surfaceAbove(v float64) bool { return c.surfaceN >= v/8.25 }

// steep looks only inside the chunk, and is deliberately lopsided (section 5).
func (c *ruleContext) steep() bool {
	h := func(x, z int) int { return int(c.b.wgTop[z<<4|x]) - 1 }
	x, z := c.lx, c.lz
	return h(x, min(z+1, 15)) >= h(x, max(z-1, 0))+4 || h(max(x-1, 0), z) >= h(min(x+1, 15), z)+4
}

func (c *ruleContext) band() bs {
	v := c.g.surface.bands.at(float64(c.x), 0, float64(c.z))
	off := int(math.Floor(float64(v*4.0) + 0.5))
	i := (c.y + off + 192) % 192
	if i < 0 {
		i += 192
	}
	return c.g.sf.bands[i]
}

// gradient is the vertical gradient condition.
func gradient(f seedFactory, x, y, z, trueBelow, falseAbove int) bool {
	if y <= trueBelow {
		return true
	}
	if y >= falseAbove {
		return false
	}
	r := f.at(int32(x), int32(y), int32(z))
	return float64(r.nextFloat()) < clampedMap(float64(y), float64(trueBelow), float64(falseAbove), 1, 0)
}

// ruleAt is the whole rule tree for a stone block.
func (c *ruleContext) ruleAt() bs {
	g := c.g
	if c.y < g.profile.bedrockFloorTop && g.bedrockAt(c.x, c.y, c.z) {
		return sBedrock
	}
	if g.profile.deepslateFirst && gradient(g.sf.deepslate, c.x, c.y, c.z, 0, 8) {
		return sDeepsl
	}
	if c.y >= c.minLevel {
		if s := c.surfaceAtBlock(); s != noRule {
			return s
		}
	}
	if gradient(g.sf.deepslate, c.x, c.y, c.z, 0, 8) {
		return sDeepsl
	}
	return noRule
}

// bedrockAt is Java's floor, falling evenly over five layers. The Bedrock profile lays its own after
// the noise stage instead (writeBedrockFloor).
func (g *Generator) bedrockAt(x, y, z int) bool {
	return gradient(g.sf.bedrock, x, y, z, minY, minY+5)
}

func (c *ruleContext) grassOrDirt() bs {
	if c.waterAt(0, 0, false) {
		return sGrass
	}
	return sDirt
}

func (c *ruleContext) sandOrStone() bs {
	if c.onCeiling() {
		return sSandst
	}
	return sSand
}

func (c *ruleContext) gravelOrStone() bs {
	if c.onCeiling() {
		return sStone
	}
	return sGravel
}

// stoneLike is the part of the rules that top and under-floor share.
func (c *ruleContext) stoneLike(b uint8) bs {
	n := &c.g.surface
	switch b {
	case bStonyPeaks:
		if c.noiseIn(n.calcite, -0.0125, 0.0125) {
			return sCalcite
		}
		return sStone
	case bStonyShore:
		if c.noiseIn(n.gravel, -0.05, 0.05) {
			return c.gravelOrStone()
		}
		return sStone
	case bWindswept:
		if c.surfaceAbove(1.0) {
			return sStone
		}
		return noRule
	case bWarmOcean, bBeach, bSnowyBeach, bDesert:
		return c.sandOrStone()
	case bDripstone:
		return sStone
	}
	return noRule
}

// topLayer is the block on a floor with little water above.
func (c *ruleContext) topLayer(b uint8) bs {
	n := &c.g.surface
	dry := c.waterAt(0, 0, false)
	switch b {
	case bFrozenPeaks:
		if c.steep() || c.noiseIn(n.packed, 0, 0.2) {
			return sPacked
		}
		if c.noiseIn(n.ice, 0, 0.025) {
			return sIce
		}
		if dry {
			return sSnowBl
		}
	case bSnowySlopes:
		if c.steep() {
			return sStone
		}
		if dry {
			if c.noiseIn(n.powder, 0.35, 0.6) {
				return sPowder
			}
			return sSnowBl
		}
	case bJagged:
		if c.steep() {
			return sStone
		}
		if dry {
			return sSnowBl
		}
	case bGrove:
		if dry {
			if c.noiseIn(n.powder, 0.35, 0.6) {
				return sPowder
			}
			return sSnowBl
		}
	}
	if s := c.stoneLike(b); s != noRule {
		return s
	}
	switch b {
	case bWindSavanna:
		if c.surfaceAbove(1.75) {
			return sStone
		}
		if c.surfaceAbove(-0.5) {
			return sCoarse
		}
	case bGravelly:
		switch {
		case c.surfaceAbove(2.0):
			return c.gravelOrStone()
		case c.surfaceAbove(1.0):
			return sStone
		case c.surfaceAbove(-1.0):
			return c.grassOrDirt()
		}
		return c.gravelOrStone()
	case bOldPine, bOldSpruce:
		if c.surfaceAbove(1.75) {
			return sCoarse
		}
		if c.surfaceAbove(-0.95) {
			return sPodzol
		}
	case bIceSpikes:
		if dry {
			return sSnowBl
		}
	case bMangrove:
		return sMud
	case bMushroom:
		return sMycelium
	}
	return c.grassOrDirt()
}

// fillerLayer is the block under the top layer.
func (c *ruleContext) fillerLayer(b uint8) bs {
	n := &c.g.surface
	dry := c.waterAt(0, 0, false)
	switch b {
	case bFrozenPeaks:
		if c.steep() || c.noiseIn(n.packed, -0.5, 0.2) {
			return sPacked
		}
		if c.noiseIn(n.ice, -0.0625, 0.025) {
			return sIce
		}
		if dry {
			return sSnowBl
		}
	case bSnowySlopes:
		if c.steep() {
			return sStone
		}
		if dry {
			if c.noiseIn(n.powder, 0.45, 0.58) {
				return sPowder
			}
			return sSnowBl
		}
	case bJagged:
		return sStone
	case bGrove:
		if dry && c.noiseIn(n.powder, 0.45, 0.58) {
			return sPowder
		}
		return sDirt
	}
	if s := c.stoneLike(b); s != noRule {
		return s
	}
	switch b {
	case bWindSavanna:
		if c.surfaceAbove(1.75) {
			return sStone
		}
	case bGravelly:
		switch {
		case c.surfaceAbove(2.0):
			return c.gravelOrStone()
		case c.surfaceAbove(1.0):
			return sStone
		case c.surfaceAbove(-1.0):
			return sDirt
		}
		return c.gravelOrStone()
	case bMangrove:
		return sMud
	}
	return sDirt
}

// surfaceAtBlock is the rules above the preliminary surface.
func (c *ruleContext) surfaceAtBlock() bs {
	n := &c.g.surface
	b := c.biomeHere()
	onFloor := c.onFloor()
	if onFloor {
		switch b {
		case bWooded:
			if c.yAbove(97, 2, false) {
				if c.noiseIn(n.depth, -0.909, -0.5454) || c.noiseIn(n.depth, -0.1818, 0.1818) || c.noiseIn(n.depth, 0.5454, 0.909) {
					return sCoarse
				}
				return c.grassOrDirt()
			}
		case bSwamp:
			if c.yAbove(62, 0, false) && !c.yAbove(63, 0, false) && c.noiseIn(n.swamp, 0, math.MaxFloat64) {
				return sWater
			}
		case bMangrove:
			if c.yAbove(60, 0, false) && !c.yAbove(63, 0, false) && c.noiseIn(n.swamp, 0, math.MaxFloat64) {
				return sWater
			}
		}
	}
	waterStone := c.waterAt(-6, -1, true)
	if b == bBadlands || b == bEroded || b == bWooded {
		if onFloor {
			if c.yAbove(256, 0, false) {
				return sTerrOrange
			}
			if c.yAbove(74, 1, true) {
				if c.noiseIn(n.depth, -0.909, -0.5454) || c.noiseIn(n.depth, -0.1818, 0.1818) || c.noiseIn(n.depth, 0.5454, 0.909) {
					return sTerracotta
				}
				return c.band()
			}
			if c.waterAt(-1, 0, false) {
				if c.onCeiling() {
					return sRedSands
				}
				return sRedSand
			}
			if c.depth > 0 {
				return sTerrOrange
			}
			if waterStone {
				return sTerrWhite
			}
			return c.gravelOrStone()
		}
		if c.yAbove(63, -1, true) {
			if c.yAbove(63, 0, false) && !c.yAbove(74, 1, true) {
				return sTerrOrange
			}
			return c.band()
		}
		if c.underFloor() && waterStone {
			return sTerrWhite
		}
		return noRule
	}
	frozen := b == bFrozenOcean || b == bDeepFrozen
	if onFloor && c.waterAt(-1, 0, false) {
		if frozen && c.depth <= 0 {
			if c.waterAt(0, 0, false) {
				return sAir
			}
			if c.g.coldEnough(c.biomeHere(), c.x, c.y, c.z) {
				return sIce
			}
			return sWater
		}
		return c.topLayer(b)
	}
	if waterStone {
		if onFloor && frozen && c.depth <= 0 {
			return sWater
		}
		if c.underFloor() {
			return c.fillerLayer(b)
		}
		if (b == bWarmOcean || b == bBeach || b == bSnowyBeach) && c.deepUnder() {
			return sSandst
		}
		if b == bDesert && c.veryDeepUnder() {
			return sSandst
		}
		return noRule
	}
	if onFloor {
		switch b {
		case bFrozenPeaks, bJagged:
			return sStone
		case bWarmOcean, bLukewarm, bDeepLukewarm:
			return c.sandOrStone()
		}
		return c.gravelOrStone()
	}
	return noRule
}

// applySurfaceRules is the surface pass over the columns given, in the game's x then z order.
func (g *Generator) applySurfaceRules(b *baseChunk, s *chunkScratch, x0, x1, z0, z1 int) {
	c := &s.rule
	c.g, c.b, c.s = g, b, s
	for lx := x0; lx <= x1; lx++ {
		for lz := z0; lz <= z1; lz++ {
			g.surfaceRulesColumn(b, s, lx, lz)
		}
	}
}

// surfaceRulesColumn runs the rule tree down one column. s.rule must be readied for the chunk.
func (g *Generator) surfaceRulesColumn(b *baseChunk, s *chunkScratch, lx, lz int) {
	c := &s.rule
	ox, oz := b.cx*16, b.cz*16
	{
		{
			wx, wz := ox+lx, oz+lz
			start := int(b.wgTop[lz<<4|lx])
			sb := s.blockBiome(g, wx, start, wz)
			if sb == bEroded {
				g.erodedExtension(b, lx, lz, wx, wz, start)
			}
			h := int(b.wgTop[lz<<4|lx])
			c.startColumn(wx, wz)
			above, water := 0, noWaterTop
			for y := h; y >= minY; y-- {
				st := b.b[cellIndex(lx, y, lz)]
				switch {
				case st == sAir:
					above, water = 0, noWaterTop
				case st.has(fLiquid):
					if water == noWaterTop {
						water = y + 1
					}
				default:
					above++
					if st == sStone {
						c.setY(above, y, water)
						if r := c.ruleAt(); r != noRule {
							b.set(lx, y, lz, r)
						}
					}
				}
			}
			if sb == bFrozenOcean || sb == bDeepFrozen {
				g.frozenExtension(b, c, sb, lx, lz, wx, wz, start)
			}
		}
	}
}

// erodedExtension raises a badlands hoodoo before the main loop.
func (g *Generator) erodedExtension(b *baseChunk, lx, lz, x, z, start int) {
	n := &g.surface
	fx, fz := float64(x), float64(z)
	buf := min(math.Abs(float64(n.hoodooSurface.at(fx, 0, fz)*8.25)), float64(n.hoodoo.at(float64(fx*0.2), 0, float64(fz*0.2))*15.0))
	if buf <= 0 {
		return
	}
	roof := math.Abs(float64(n.hoodooCap.at(float64(fx*0.75), 0, float64(fz*0.75)) * 1.5))
	top := 64.0 + min(float64(float64(buf*buf)*2.5), float64(math.Ceil(float64(roof*50.0)))+24.0)
	ty := int(math.Floor(top))
	if start > ty {
		return
	}
	for y := ty; y >= minY; y-- {
		st := b.b[cellIndex(lx, y, lz)]
		if st == sStone {
			break
		}
		if st == sWater {
			return
		}
	}
	for y := ty; y >= minY && b.b[cellIndex(lx, y, lz)].isAir(); y-- {
		b.set(lx, y, lz, sStone)
	}
}

// frozenExtension raises an iceberg after the main loop.
func (g *Generator) frozenExtension(b *baseChunk, c *ruleContext, sb uint8, lx, lz, x, z, start int) {
	n := &g.surface
	fx, fz := float64(x), float64(z)
	ice := min(math.Abs(float64(n.bergSurface.at(fx, 0, fz)*8.25)), float64(n.berg.at(float64(fx*1.28), 0, float64(fz*1.28))*15.0))
	if ice <= 1.8 {
		return
	}
	roof := math.Abs(float64(n.bergCap.at(float64(fx*1.17), 0, float64(fz*1.17)) * 1.5))
	top := min(float64(float64(ice*ice)*1.2), float64(math.Ceil(float64(roof*40.0)))+14.0)
	if g.profile.climate.temperature(sb, x, 63, z) > 0.1 {
		top -= 2.0
	}
	var bot float64
	if top > 2.0 {
		bot = 63 - top - 7.0
		top += 63
	} else {
		top, bot = 0, 0
	}
	r := g.sf.world.at(int32(x), 0, int32(z))
	maxSnow := 2 + r.nextInt(4)
	minSnowY := 63 + 18 + r.nextInt(10)
	snow := 0
	itop, ibot := int(top), int(bot)
	for y := max(start, itop+1); y >= c.minLevel && y >= minY; y-- {
		st := b.b[cellIndex(lx, y, lz)]
		place := (st.isAir() && y < itop && r.nextDouble() > 0.01) ||
			(st == sWater && y > ibot && y < 63 && ibot != 0 && r.nextDouble() > 0.15)
		if place {
			if snow <= maxSnow && y > minSnowY {
				b.set(lx, y, lz, sSnowBl)
				snow++
			} else {
				b.set(lx, y, lz, sPacked)
			}
		}
	}
}

// Fixed simplex noises, the same for every seed (section 12).
type simplexNoise struct {
	perm [512]uint8
}

func newSimplex(r *lcg) *simplexNoise {
	n := &simplexNoise{}
	r.nextDouble()
	r.nextDouble()
	r.nextDouble()
	var p [256]uint16
	for i := range p {
		p[i] = uint16(i)
	}
	for i := range 256 {
		j := int(r.nextInt(int32(256 - i)))
		p[i], p[i+j] = p[i+j], p[i]
	}
	for i := range n.perm {
		n.perm[i] = uint8(p[i&255])
	}
	return n
}

var (
	simplexF2 = 0.5 * (math.Sqrt(3.0) - 1.0)
	simplexG2 = (3.0 - math.Sqrt(3.0)) / 6.0
)

var simplexGrad = [12][2]float64{{1, 1}, {-1, 1}, {1, -1}, {-1, -1}, {1, 0}, {-1, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}, {0, 1}, {0, -1}}

func simplexCorner(g int, x, y float64) float64 {
	t := 0.5 - float64(x*x) - float64(y*y)
	if t < 0 {
		return 0
	}
	t = float64(t * t)
	return float64(t*t) * (float64(simplexGrad[g][0]*x) + float64(simplexGrad[g][1]*y))
}

func (n *simplexNoise) get(xin, yin float64) float64 {
	s := float64((xin + yin) * simplexF2)
	i, j := int(math.Floor(xin+s)), int(math.Floor(yin+s))
	t := float64(float64(i+j) * simplexG2)
	x0, y0 := xin-(float64(i)-t), yin-(float64(j)-t)
	i1, j1 := 0, 1
	if x0 > y0 {
		i1, j1 = 1, 0
	}
	x1, y1 := x0-float64(i1)+simplexG2, y0-float64(j1)+simplexG2
	x2, y2 := x0-1.0+float64(2.0*simplexG2), y0-1.0+float64(2.0*simplexG2)
	ii, jj := i&255, j&255
	p := func(k int) int { return int(n.perm[k&255]) }
	gi0, gi1, gi2 := p(ii+p(jj))%12, p(ii+i1+p(jj+j1))%12, p(ii+1+p(jj+1))%12
	return float64(70.0 * (simplexCorner(gi0, x0, y0) + simplexCorner(gi1, x1, y1) + simplexCorner(gi2, x2, y2)))
}

// simplexStack is the study's stack of simplex levels for a sorted set of octaves.
type simplexStack struct {
	levels      []*simplexNoise
	inF, valueF float64
}

func newSimplexStack(seed int64, octaves []int) *simplexStack {
	r := newLCG(seed)
	has := func(o int) bool {
		for _, v := range octaves {
			if v == o {
				return true
			}
		}
		return false
	}
	low, high := -octaves[0], octaves[len(octaves)-1]
	n := low + high + 1
	st := &simplexStack{levels: make([]*simplexNoise, n)}
	zero := newSimplex(&r)
	if high >= 0 && high < n && has(0) {
		st.levels[high] = zero
	}
	for i := high + 1; i < n; i++ {
		if has(high - i) {
			st.levels[i] = newSimplex(&r)
		} else {
			for range 262 {
				r.bits(32)
			}
		}
	}
	st.inF = math.Pow(2, float64(high))
	st.valueF = 1.0 / (math.Pow(2, float64(n)) - 1.0)
	return st
}

func (s *simplexStack) value(x, z float64) float64 {
	v := 0.0
	f, a := s.inF, s.valueF
	for _, l := range s.levels {
		if l != nil {
			v += float64(l.get(float64(x*f), float64(z*f)) * a)
		}
		f /= 2
		a *= 2
	}
	return v
}

type fixedSimplex struct{ temp, frozen, info *simplexStack }

var fixedNoises = sync.OnceValue(func() *fixedSimplex {
	return &fixedSimplex{
		temp:   newSimplexStack(1234, []int{0}),
		frozen: newSimplexStack(3456, []int{-2, -1, 0}),
		info:   newSimplexStack(2345, []int{0}),
	}
})

// stateBase strips a state's properties from its name.
func stateBase(name string) string {
	base, _, _ := strings.Cut(name, "[")
	return base
}
