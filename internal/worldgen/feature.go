package worldgen

import "math"

// Decoration follows Mojang's algorithms, per the owner's decision (NOTES-exact-features.md sections 7
// to 12), in our own Go and structure. By the owner's ruling, each source chunk is decorated alone
// against the carvers stage of itself and its neighbours, and a chunk's final blocks are its carvers
// stage with the changes of its nine sources applied in a fixed order. So a chunk is the same in any
// order and needs no real neighbours. Java itself has no single answer where sources overlap.

// featRand is the study's Feature RNG: bit draws over xoroshiro, with a Gaussian spare that survives
// reseeding within one chunk's decoration.
type featRand struct {
	x         xoro
	haveSpare bool
	spare     float64
}

func (r *featRand) setSeed(seed int64) { r.x = xoroFromSeed(seed) }

func (r *featRand) bits(n uint) int32 { return int32(r.x.next() >> (64 - n)) }

func (r *featRand) nextLong() int64 {
	hi := int64(r.bits(32))
	return hi<<32 + int64(r.bits(32))
}

func (r *featRand) nextBool() bool { return r.bits(1) != 0 }

func (r *featRand) nextFloat() float32 { return float32(r.bits(24)) * (1.0 / (1 << 24)) }

func (r *featRand) nextDouble() float64 {
	hi := int64(r.bits(26))
	return float64(hi<<27+int64(r.bits(27))) * (1.0 / (1 << 53))
}

func (r *featRand) nextInt(n int) int {
	b := int32(n)
	if b&(b-1) == 0 {
		return int((int64(b) * int64(r.bits(31))) >> 31)
	}
	for {
		s := r.bits(31)
		m := s % b
		if s-m+(b-1) >= 0 {
			return int(m)
		}
	}
}

func (r *featRand) between(a, b int) int { return r.nextInt(b-a+1) + a }

func (r *featRand) betweenSafe(a, b int) int {
	if a >= b {
		return a
	}
	return r.nextInt(b-a+1) + a
}

func (r *featRand) betweenF(a, b float32) float32 { return float32(r.nextFloat()*(b-a)) + a }

func (r *featRand) gaussian() float64 {
	if r.haveSpare {
		r.haveSpare = false
		return r.spare
	}
	for {
		x := float64(2.0*r.nextDouble()) - 1
		y := float64(2.0*r.nextDouble()) - 1
		s := float64(x*x) + float64(y*y)
		if s < 1 && s != 0 {
			m := math.Sqrt(float64(-2.0*math.Log(s)) / s)
			r.spare, r.haveSpare = float64(y*m), true
			return float64(x * m)
		}
	}
}

// intProvider samples an integer, drawing as its kind does.
type intProvider func(r *featRand) int

func constInt(v int) intProvider { return func(*featRand) int { return v } }

func uniInt(a, b int) intProvider {
	return func(r *featRand) int { return r.between(a, b) }
}

func biasedInt(a, b int) intProvider {
	return func(r *featRand) int { return a + r.nextInt(r.nextInt(b-a+1)+1) }
}

func clampedInt(p intProvider, a, b int) intProvider {
	return func(r *featRand) int { return min(max(p(r), a), b) }
}

func clampedNormal(mean, dev float32, a, b int) intProvider {
	return func(r *featRand) int {
		v := mean + float32(float32(r.gaussian())*dev)
		return int(min(max(v, float32(a)), float32(b)))
	}
}

// trapInt is the study's zero-plateau symmetric trapezoid: two draws and a subtraction.
func trapInt(m int) intProvider {
	return func(r *featRand) int { return r.nextInt(m+1) - r.nextInt(m+1) }
}

func trapezoidInt(lo, hi, plateau int) intProvider {
	return func(r *featRand) int {
		if plateau == 0 && hi == -lo {
			return r.nextInt(hi+1) - r.nextInt(hi+1)
		}
		rng := hi - lo
		if plateau == rng {
			return r.between(lo, hi)
		}
		s := (rng - plateau) / 2
		e := rng - s
		return lo + r.between(0, e) + r.between(0, s)
	}
}

type weightedInt struct {
	w int
	p intProvider
}

func weightedInts(list ...weightedInt) intProvider {
	total := 0
	for _, e := range list {
		total += e.w
	}
	return func(r *featRand) int {
		k := r.nextInt(total)
		for _, e := range list {
			if k < e.w {
				return e.p(r)
			}
			k -= e.w
		}
		return 0
	}
}

// heightProvider samples a Y.
type heightProvider func(r *featRand) int

func uniHeight(a, b int) heightProvider {
	return func(r *featRand) int {
		if a > b {
			return a
		}
		return r.between(a, b)
	}
}

func trapHeight(a, b, plateau int) heightProvider {
	return func(r *featRand) int {
		if a > b {
			return a
		}
		rng := b - a
		if plateau >= rng {
			return r.between(a, b)
		}
		s := (rng - plateau) / 2
		e := rng - s
		return a + r.between(0, e) + r.between(0, s)
	}
}

func biasedHeight(a, b, inner int) heightProvider {
	return func(r *featRand) int {
		if b-a-inner+1 <= 0 {
			return a
		}
		lim := r.nextInt(b - a - inner + 1)
		return r.nextInt(lim+inner) + a
	}
}

func veryBiasedHeight(a, b, inner int) heightProvider {
	return func(r *featRand) int {
		if b-a-inner+1 <= 0 {
			return a
		}
		u := r.betweenSafe(a+inner, b)
		l := r.betweenSafe(a, u-1)
		return r.betweenSafe(a, l-1+inner)
	}
}

// Heightmap kinds. The first two are frozen at the end of carving.
const (
	hmWorldSurfaceWG = iota
	hmOceanFloorWG
	hmWorldSurface
	hmOceanFloor
	hmMotionBlocking
	hmMotionNoLeaves
)

// liveChunk is a chunk being read and written during one source's decoration: the cache's compact
// base expanded into one of the worker's buffers, given back when the working set moves.
type liveChunk struct {
	b        *baseChunk
	gen      uint32 // The source run its maps are valid for.
	primed   bool
	maps     [4][256]int16 // WorldSurface, OceanFloor, Motion, MotionNoLeaves.
	clean    [4][256]int16 // The same, for the carvers stage.
	hasClean bool
}

type changeEntry struct {
	chunk int32
	cell  int32
	old   bs
}

// liveSpan is how many chunks across the working set around a target is: the target's 5 x 5, which
// its nine sources write, and two more on each side for reads.
const liveSpan = 9

// sourceReach is how far from a source, in chunks, its features can read: its 3 x 3 and two more. A
// read past that is air. It is fixed by the source and never by the chunk being made, so a source's
// changes are the same for every chunk that needs them.
const sourceReach = 3

// decoration holds what a chunk's decoration uses, in the chunk's scratch.
type decoration struct {
	g              *Generator
	s              *chunkScratch
	cx, cz         int // The chunk being decorated now.
	baseCX, baseCZ int // The chunk at the middle of the working set.
	live           [liveSpan * liveSpan]liveChunk
	gen            uint32
	r              featRand
	// region is the source chunk's level random: Java's WorldGenRegion makes one per chunk it
	// decorates, from the seed and the chunk's corner, and blocks that place themselves draw from it
	// (a pale moss carpet's topper) instead of from the feature's random. Here a source is decorated
	// alone, so the source's run owns it from the start, as Java's region does.
	region xoro
	log    []changeEntry
	used   []int32
	// pre is what the source's earlier neighbours left, each run alone, written before the source runs
	// so its features see it; preLen is how much of the log those writes take.
	pre     []sourceWrite
	preLen  int
	ore     oreScratch
	decSeed int64
	// The Bedrock profile's two generators of a rule (section 2) and a chunk's biome list.
	br1, br2  bmt
	biomeList []uint8
	listOf    listKey // Whose list biomeList holds, in the Bedrock biome model.
	budget    int     // Scatter placements left to this source (bedrock_limits.go).
	menv      molangEnv // The Molang a rule and its features read.
	placed  bool
	// The chunk last read, a shortcut for runs of reads in one chunk.
	lastCX, lastCZ     int
	lastB              *baseChunk
	lastSrcX, lastSrcZ int // The source the shortcut was taken under, whose reach it obeyed.
}

// startTarget makes the working set ready for a chunk.
func (d *decoration) startTarget(g *Generator, s *chunkScratch, cx, cz int) {
	d.g, d.s, d.baseCX, d.baseCZ = g, s, cx, cz
	d.cx, d.cz = cx, cz
	d.lastB = nil
	for _, i := range d.used {
		d.s.giveBuffer(d.live[i].b)
		d.live[i].b = nil
	}
	d.used = d.used[:0]
}

// chunkAt gives a chunk of the working set, or nil past the reach of the source being decorated.
func (d *decoration) chunkAt(cx, cz int) *liveChunk {
	if absInt(cx-d.cx) > sourceReach || absInt(cz-d.cz) > sourceReach {
		return nil
	}
	dx, dz := cx-d.baseCX+liveSpan/2, cz-d.baseCZ+liveSpan/2
	if dx < 0 || dx >= liveSpan || dz < 0 || dz >= liveSpan {
		return nil
	}
	i := dx*liveSpan + dz
	l := &d.live[i]
	if l.b == nil {
		cb := d.g.cachedBase(d.s, cx, cz)
		l.b = d.s.takeBuffer()
		cb.expand(l.b)
		l.hasClean = false
		d.used = append(d.used, int32(i))
	}
	return l
}

// mapsReady readies a chunk's final heightmaps for this source run, primed from the carvers stage plus
// nothing, or from ore writes tracked so far.
func (d *decoration) mapsReady(l *liveChunk) {
	if l.gen == d.gen {
		return
	}
	l.gen, l.primed = d.gen, false
	if !l.hasClean {
		l.hasClean = true
		l.b.finalMaps(&l.clean)
	}
	l.maps = l.clean
}

func (b *baseChunk) finalMaps(m *[4][256]int16) {
	for i := range 256 {
		x, z := i&15, i>>4
		top := int(b.wgTop[i])
		m[0][i] = int16(top)
		m[1][i] = b.wgFloor[i]
		var mo, ml int16 = noTop, noTop
		for y := top - 1; y >= minY; y-- {
			s := b.b[cellIndex(x, y, z)]
			if s.has(fMotion) || s.hasFluid() {
				if mo == noTop {
					mo = int16(y + 1)
				}
				if !s.tagged(tagLeaves) {
					ml = int16(y + 1)
					break
				}
			}
		}
		m[2][i], m[3][i] = mo, ml
	}
}

// matchesMap says whether a state counts for a final heightmap.
func matchesMap(k int, s bs) bool {
	switch k {
	case 0:
		return !s.isAir()
	case 1:
		return s.has(fMotion)
	case 2:
		return s.has(fMotion) || s.hasFluid()
	}
	return (s.has(fMotion) || s.hasFluid()) && !s.tagged(tagLeaves)
}

func (l *liveChunk) updateMaps(x, y, z int, s bs) {
	i := z<<4 | x
	for k := range l.maps {
		t := int(l.maps[k][i])
		if y <= t-2 {
			continue
		}
		if matchesMap(k, s) {
			if y >= t {
				l.maps[k][i] = int16(y + 1)
			}
		} else if y == t-1 {
			v := int16(noTop)
			for q := y - 1; q >= minY; q-- {
				if matchesMap(k, l.b.b[cellIndex(x, q, z)]) {
					v = int16(q + 1)
					break
				}
			}
			l.maps[k][i] = v
		}
	}
}

// block reads a block. Outside the world it is air; outside the working set it is air too, the one
// read a real server could answer differently (section 13, far reads).
func (d *decoration) block(x, y, z int) bs {
	if y < minY || y > maxY {
		return sAir
	}
	cx, cz := x>>4, z>>4
	if cx == d.lastCX && cz == d.lastCZ && d.lastB != nil && d.lastSrcX == d.cx && d.lastSrcZ == d.cz {
		return d.lastB.b[cellIndex(x&15, y, z&15)]
	}
	l := d.chunkAt(cx, cz)
	if l == nil {
		return sAir
	}
	d.lastCX, d.lastCZ, d.lastB = cx, cz, l.b
	d.lastSrcX, d.lastSrcZ = d.cx, d.cz
	return l.b.b[cellIndex(x&15, y, z&15)]
}

// canWrite is the step's write rule: only the source's 3 x 3.
func (d *decoration) canWrite(x, y, z int) bool {
	if y < minY || y > maxY {
		return false
	}
	return absInt(x>>4-d.cx) <= 1 && absInt(z>>4-d.cz) <= 1
}

func (d *decoration) write(l *liveChunk, x, y, z int, s bs) {
	cell := cellIndex(x&15, y, z&15)
	dx, dz := (x>>4)-d.baseCX+liveSpan/2, (z>>4)-d.baseCZ+liveSpan/2
	slot := dx*liveSpan + dz
	d.log = append(d.log, changeEntry{int32(slot), int32(cell), l.b.b[cell]})
	l.b.b[cell] = s
}

// set is a block write with heightmap updates.
func (d *decoration) set(x, y, z int, s bs) {
	if !d.canWrite(x, y, z) {
		return
	}
	l := d.chunkAt(x>>4, z>>4)
	d.mapsReady(l)
	l.primed = true
	d.write(l, x, y, z, s)
	l.updateMaps(x&15, y, z&15, s)
}

// setOre is an ore's direct write: a primed heightmap does not hear of it (section 4).
func (d *decoration) setOre(x, y, z int, s bs) {
	if !d.canWrite(x, y, z) {
		return
	}
	l := d.chunkAt(x>>4, z>>4)
	d.mapsReady(l)
	d.write(l, x, y, z, s)
	if !l.primed {
		l.updateMaps(x&15, y, z&15, s)
	}
}

// height is a heightmap's first free Y.
func (d *decoration) height(kind, x, z int) int {
	l := d.chunkAt(x>>4, z>>4)
	if l == nil {
		return minY
	}
	i := (z&15)<<4 | x&15
	switch kind {
	case hmWorldSurfaceWG:
		return int(l.b.wgTop[i])
	case hmOceanFloorWG:
		return int(l.b.wgFloor[i])
	}
	d.mapsReady(l)
	l.primed = true
	return int(l.maps[kind-hmWorldSurface][i])
}

func (d *decoration) biomeAt(x, y, z int) uint8 { return d.s.blockBiome(d.g, x, y, z) }

// pos is a block position.
type pos struct{ x, y, z int }

func (p pos) add(x, y, z int) pos { return pos{p.x + x, p.y + y, p.z + z} }

// Directions, in the study's order: down, up, north, south, west, east.
type dir uint8

const (
	dDown dir = iota
	dUp
	dNorth
	dSouth
	dWest
	dEast
)

var dirVec = [6]pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

var (
	horizontal = [4]dir{dNorth, dEast, dSouth, dWest}
	allDirs    = [6]dir{dDown, dUp, dNorth, dSouth, dWest, dEast}
)

func (a dir) opposite() dir {
	return [6]dir{dUp, dDown, dSouth, dNorth, dEast, dWest}[a]
}

func (a dir) clockwise() dir {
	return [6]dir{dDown, dUp, dEast, dWest, dNorth, dSouth}[a]
}

func (p pos) step(a dir) pos { v := dirVec[a]; return pos{p.x + v.x, p.y + v.y, p.z + v.z} }

func (p pos) stepN(a dir, n int) pos {
	v := dirVec[a]
	return pos{p.x + v.x*n, p.y + v.y*n, p.z + v.z*n}
}

func (d *decoration) at(p pos) bs { return d.block(p.x, p.y, p.z) }

// placement is one placed feature: its modifiers and what it places.
type placement struct {
	name      string
	step, idx int
	mods      []modifier
	feat      feature
	tracked   feature   // feat, also noting that it placed, for nested placed features.
	biomes    [2]uint64 // Biome table indexes that list this feature.
}

// feature places at a position and says whether it placed.
type feature func(d *decoration, p pos) bool

// modifier is one step of a placed feature's pipeline. It calls d.run for each position it produces,
// so a position's feature is placed before the next position is drawn (section 7).
type modifier interface {
	apply(d *decoration, p pos, rest []modifier, f feature)
}

func (d *decoration) run(mods []modifier, f feature, p pos) {
	if len(mods) == 0 {
		f(d, p)
		return
	}
	mods[0].apply(d, p, mods[1:], f)
}

// place evaluates a nested placed feature and says whether anything placed.
func (d *decoration) place(pl *placement, p pos) bool {
	saved := d.placed
	d.placed = false
	d.run(pl.mods, pl.tracked, p)
	ok := d.placed
	d.placed = saved
	return ok
}

type countMod struct{ n intProvider }

func (m countMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	for range m.n(&d.r) {
		d.run(rest, f, p)
	}
}

// dataCountMod is a count from the Bedrock data's rule of that name when the data is read, else the
// profile's value.
type dataCountMod struct {
	name     string
	fallback [2]int
}

func (m dataCountMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	lo, hi := m.fallback[0], m.fallback[1]
	if bd := d.g.bdata; bd != nil {
		if c, ok := bd.counts[m.name]; ok {
			lo, hi = c[0], c[1]
		}
	}
	for range d.r.between(lo, hi) {
		d.run(rest, f, p)
	}
}

type inSquareMod struct{}

func (inSquareMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	x := p.x + d.r.nextInt(16)
	z := p.z + d.r.nextInt(16)
	d.run(rest, f, pos{x, p.y, z})
}

type heightRangeMod struct{ h heightProvider }

func (m heightRangeMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	d.run(rest, f, pos{p.x, m.h(&d.r), p.z})
}

type heightmapMod struct{ kind int }

func (m heightmapMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	if h := d.height(m.kind, p.x, p.z); h > minY {
		d.run(rest, f, pos{p.x, h, p.z})
	}
}

type biomeMod struct{ self *placement }

func (m biomeMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	if biomeHasFeature(d.biomeAt(p.x, p.y, p.z), m.self) {
		d.run(rest, f, p)
	}
}

type rarityMod struct{ chance int }

func (m rarityMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	if d.r.nextFloat() < 1.0/float32(m.chance) {
		d.run(rest, f, p)
	}
}

type noiseCountMod struct {
	ratio, factor, offset float64
}

func (m noiseCountMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	v := fixedNoises().info.value(float64(p.x)/m.factor, float64(p.z)/m.factor)
	n := int(math.Ceil(float64((v + m.offset) * m.ratio)))
	for range max(n, 0) {
		d.run(rest, f, p)
	}
}

type noiseThresholdMod struct {
	level        float64
	below, above int
}

func (m noiseThresholdMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	v := fixedNoises().info.value(float64(p.x)/200.0, float64(p.z)/200.0)
	n := m.above
	if v < m.level {
		n = m.below
	}
	for range n {
		d.run(rest, f, p)
	}
}

type waterDepthMod struct{ limit int }

func (m waterDepthMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	if d.height(hmWorldSurface, p.x, p.z)-d.height(hmOceanFloor, p.x, p.z) <= m.limit {
		d.run(rest, f, p)
	}
}

type surfaceThresholdMod struct{ kind, lo, hi int }

func (m surfaceThresholdMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	h := d.height(m.kind, p.x, p.z)
	if h+m.lo <= p.y && p.y <= h+m.hi {
		d.run(rest, f, p)
	}
}

type filterMod struct{ test predicate }

func (m filterMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	if m.test(d, p) {
		d.run(rest, f, p)
	}
}

type scanMod struct {
	dir             dir
	target, allowed predicate
	steps           int
}

func (m scanMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	if !m.allowed(d, p) {
		return
	}
	q := p
	for range m.steps {
		if m.target(d, q) {
			d.run(rest, f, q)
			return
		}
		q = q.step(m.dir)
		if q.y < minY || q.y > maxY {
			return
		}
		if !m.allowed(d, q) {
			break
		}
	}
	if m.target(d, q) {
		d.run(rest, f, q)
	}
}

type offsetMod struct{ xz, y intProvider }

func (m offsetMod) apply(d *decoration, p pos, rest []modifier, f feature) {
	x := p.x + m.xz(&d.r)
	y := p.y + m.y(&d.r)
	z := p.z + m.xz(&d.r)
	d.run(rest, f, pos{x, y, z})
}

// predicate is a block predicate at a position.
type predicate func(d *decoration, p pos) bool

func allOf(list ...predicate) predicate {
	return func(d *decoration, p pos) bool {
		for _, t := range list {
			if !t(d, p) {
				return false
			}
		}
		return true
	}
}

func anyOf(list ...predicate) predicate {
	return func(d *decoration, p pos) bool {
		for _, t := range list {
			if t(d, p) {
				return true
			}
		}
		return false
	}
}

func notP(t predicate) predicate {
	return func(d *decoration, p pos) bool { return !t(d, p) }
}

func offsetP(x, y, z int, t predicate) predicate {
	return func(d *decoration, p pos) bool { return t(d, p.add(x, y, z)) }
}

func blockSet(names string) map[uint16]bool {
	m := map[uint16]bool{}
	for _, n := range fieldsOf(names) {
		m[stateBlockIndex(n)] = true
	}
	return m
}

func matchBlocks(names string) predicate {
	m := blockSet(names)
	return func(d *decoration, p pos) bool { return m[stateBlock[d.at(p)]] }
}

func matchTag(tag uint32) predicate {
	return func(d *decoration, p pos) bool { return d.at(p).tagged(tag) }
}

func emptyP(d *decoration, p pos) bool {
	s := d.at(p)
	return s.isAir()
}

func replaceableP(d *decoration, p pos) bool { return d.at(p).has(fReplaceable) }

func solidP(d *decoration, p pos) bool { return d.at(p).has(fSolid) }

func waterFluidP(d *decoration, p pos) bool {
	s := d.at(p)
	return s.has(fWater) && s.has(fSource)
}

func sturdyUpBelow(d *decoration, p pos) bool { return d.at(p.add(0, -1, 0)).has(fSturdyUp) }

// runSource decorates one source chunk alone, on the working set, leaving its changes in d.log.
func (d *decoration) runSource(cx, cz int) {
	d.cx, d.cz = cx, cz
	d.gen++
	d.log = d.log[:0]
	for _, w := range d.pre {
		d.preset(w)
	}
	d.preLen = len(d.log)
	d.r = featRand{}
	d.r.setSeed(d.g.seed)
	a, b := d.r.nextLong()|1, d.r.nextLong()|1
	ox, oz := cx*16, cz*16
	d.region = d.g.regionFac.at(int32(ox), 0, int32(oz))
	d.decSeed = int64(ox)*a + int64(oz)*b ^ d.g.seed
	l := d.chunkAt(cx, cz)
	d.mapsReady(l)
	l.primed = true
	biomes := d.biomesAround(cx, cz)
	if d.g.bedrockDeco() {
		d.runSourceBedrock(cx, cz, &biomes)
		return
	}
	for _, pl := range d.g.profile.placements {
		if !placementInBiomes(pl, &biomes) {
			continue
		}
		d.r.setSeed(d.decSeed + int64(pl.idx) + int64(10000*pl.step))
		d.run(pl.mods, pl.feat, pos{ox, minY, oz})
	}
}

// sourceWrite is a block another source left, by world position.
type sourceWrite struct {
	p pos
	s bs
}

// preset writes a neighbour's block into the working set before a source runs, where the source can
// read it, with the heightmaps hearing of it as they would of any placed block.
func (d *decoration) preset(w sourceWrite) {
	l := d.chunkAt(w.p.x>>4, w.p.z>>4)
	if l == nil || w.p.y < minY || w.p.y > maxY {
		return
	}
	d.mapsReady(l)
	l.primed = true
	d.write(l, w.p.x, w.p.y, w.p.z, w.s)
	l.updateMaps(w.p.x&15, w.p.y, w.p.z&15, w.s)
}

// rollback undoes a source's writes, so the working set is the carvers stage again.
func (d *decoration) rollback() {
	for i := len(d.log) - 1; i >= 0; i-- {
		e := d.log[i]
		d.live[e.chunk].b.b[e.cell] = e.old
	}
	d.lastB = nil
}

// biomesAround is the set of biomes in the quarts of the 3 x 3 chunks around a source, as bits.
func (d *decoration) biomesAround(cx, cz int) (set [2]uint64) {
	for qx := (cx - 1) * 4; qx < (cx+2)*4; qx++ {
		for qz := (cz - 1) * 4; qz < (cz+2)*4; qz++ {
			q := d.s.quartColumnAt(d.g, qx, qz)
			prev := -1
			for _, id := range q.ids {
				if int(id) != prev {
					set[id>>6] |= 1 << (id & 63)
					prev = int(id)
				}
			}
		}
	}
	return set
}
