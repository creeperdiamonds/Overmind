package worldgen

// A baseChunk holds one chunk's blocks up to the carvers stage. It is a pure function of the seed and
// the chunk, and is what every feature reads (section 13 of NOTES-exact-features.md, as ruled by the
// owner: a source chunk is decorated alone against these).

const (
	chunkCells = 256 * (maxY - minY + 1)
	noTop      = minY // A first-free height with no block under it.
)

func cellIndex(lx, y, lz int) int { return (y-minY)<<8 | lz<<4 | lx }

// baseChunk is a chunk being built. First-free heights follow the study's heightmaps (section 4).
type baseChunk struct {
	cx, cz  int
	owner   *Generator
	b       [chunkCells]bs
	wgTop   [256]int16 // WORLD_SURFACE_WG: above the highest block that is not air.
	wgFloor [256]int16 // OCEAN_FLOOR_WG: above the highest block that blocks motion.
	whole   bool       // All columns were built, to the carvers stage.
}

// computeTops works the two stage heightmaps out from the blocks, for the columns given.
func (b *baseChunk) computeTops(x0, x1, z0, z1 int) {
	for z := z0; z <= z1; z++ {
		for x := x0; x <= x1; x++ {
			top, floor := int16(noTop), int16(noTop)
			y := maxY
			for ; y >= minY; y-- {
				s := b.b[cellIndex(x, y, z)]
				if !s.isAir() {
					top = int16(y + 1)
					break
				}
			}
			for ; y >= minY; y-- {
				if b.b[cellIndex(x, y, z)].has(fMotion) {
					floor = int16(y + 1)
					break
				}
			}
			b.wgTop[z<<4|x], b.wgFloor[z<<4|x] = top, floor
		}
	}
}

// set writes a block before the carvers stage is over, keeping the stage's heightmaps by the study's
// update rule.
func (b *baseChunk) set(x, y, z int, s bs) {
	b.b[cellIndex(x, y, z)] = s
	i := z<<4 | x
	b.wgTop[i] = b.updated(b.wgTop[i], x, y, z, s, fAir, true)
	b.wgFloor[i] = b.updated(b.wgFloor[i], x, y, z, s, fMotion, false)
}

// updated applies a write at y to one first-free height. A heightmap matches states with the flag, or
// without it if invert is set.
func (b *baseChunk) updated(top int16, x, y, z int, s bs, flag uint16, invert bool) int16 {
	t := int(top)
	if y <= t-2 {
		return top
	}
	if s.has(flag) != invert {
		if y >= t {
			return int16(y + 1)
		}
		return top
	}
	if y == t-1 {
		for q := y - 1; q >= minY; q-- {
			if b.b[cellIndex(x, q, z)].has(flag) != invert {
				return int16(q + 1)
			}
		}
		return noTop
	}
	return top
}

// buildBase makes a chunk's base up to a stage, for the columns given. Whole chunks are what features
// read; SurfaceHeight asks for one column.
func (g *Generator) buildBase(b *baseChunk, s *chunkScratch, cx, cz int, stage baseStage, x0, x1, z0, z1 int) {
	originX, originZ := cx*16, cz*16
	b.cx, b.cz, b.owner, b.whole = cx, cz, g, false
	clear(b.b[:])
	if g.dim == dimNether {
		g.buildNetherBase(b)
		return
	}
	if g.dim == dimEnd {
		g.buildEndBase(b)
		return
	}
	s.startChunk(g, originX, originZ)
	g.fillNoise(b, s, x0, x1, z0, z1)
	var floor *bmt
	if g.profile.floorMT {
		if g.bsurf != nil {
			g.bedrockDeepslate(b, x0, x1, z0, z1)
		}
		floor = writeBedrockFloor(b, x0, x1, z0, z1)
	}
	b.computeTops(x0, x1, z0, z1)
	if stage == stageNoise {
		return
	}
	if g.bsurf != nil {
		g.applyBedrockSurface(b, s, floor, x0, x1, z0, z1)
		g.applySubsurface(b, s, x0, x1, z0, z1)
	} else {
		g.applySurfaceRules(b, s, x0, x1, z0, z1)
	}
	if stage == stageSurface {
		return
	}
	if g.profile.carversBedrock && g.bdata != nil {
		g.applyBedrockCarvers(b, s, x0, x1, z0, z1)
		return
	}
	g.applyJavaCarvers(b, s, x0, x1, z0, z1)
}

// baseStage names how far buildBase goes.
type baseStage uint8

const (
	stageNoise baseStage = iota
	stageSurface
	stageCarvers
)
