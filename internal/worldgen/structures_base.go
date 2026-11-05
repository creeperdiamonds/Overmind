package worldgen

import (
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Structures before decoration (NOTES-structures-phase2 P21.1): BDS post-processes a chunk's structures
// in their own stage once its 3 x 3 is generated, and decorates a chunk only after all 8 neighbours have
// placed theirs. Per cell the order is terrain, that chunk's structures, then the decorations of its 3 x 3
// in parity order: features always write over structure blocks and see them (heightmaps, trees meeting
// walls). Here a chunk's structures are written into its carvers stage, the "structured base", and every
// decoration source and the chunk's final blocks start from structured bases. The structures read only
// the seed and the terrain, so this stays a pure function of the seed and the chunk.

// StructureLogger is a StructurePlacer that reports each block it writes into a chunk (the Bedrock
// order needs them as the generator's own states).
type StructureLogger interface {
	// chunkOf makes the chunk with the terrain, when a piece first needs it.
	PlaceLogged(cx, cz int, chunkOf func() *chunk.Chunk, write func(x, y, z int, rid uint32, water bool)) []chunk.BlockEntity
}

const cacheStruct cacheKind = 8

// structBase is a chunk's carvers stage with its structures written in, and the structures' block
// entities with the state each sits on.
type structBase struct {
	base *compactBase
	bes  []structBE
}

type structBE struct {
	be   chunk.BlockEntity
	cell int32
	st   bs
}

// structuresFirst says whether this generator places structures before decorating: the Bedrock order
// (every dimension of the Bedrock profile, P21.1 item 3) with a placer that reports its writes.
func (g *Generator) structuresFirst() bool {
	if g.structs == nil || !g.parityDeco() {
		return false
	}
	_, ok := g.structs.(StructureLogger)
	return ok
}

// decoBase is the base a decoration source and the final chunk start from: the structured base in the
// Bedrock order, else the carvers stage.
func (g *Generator) decoBase(s *chunkScratch, cx, cz int) *compactBase {
	if g.structuresFirst() {
		return g.cachedStructBase(s, cx, cz).base
	}
	return g.cachedBase(s, cx, cz)
}

// cachedStructBase gives a chunk's structured base, from the cache.
func (g *Generator) cachedStructBase(s *chunkScratch, cx, cz int) *structBase {
	e := g.cache.get(cacheKey{cacheStruct, g.dim, int32(cx), int32(cz)}, func(e *cacheEntry) int64 {
		cb := g.cachedBase(s, cx, cz)
		buf := s.takeBuffer()
		defer s.giveBuffer(buf)
		cb.expand(buf)
		chunkOf := func() *chunk.Chunk {
			c := chunk.New(world.DefaultBlockRegistry, g.dimRange())
			switch g.dim {
			case dimNether:
				writeRows(c, &buf.b, netherHeight)
			case dimEnd:
				writeRows(c, &buf.b, 256)
			default:
				writeCells(c, &buf.b)
			}
			return c
		}
		wrote := false
		bes := g.structs.(StructureLogger).PlaceLogged(cx, cz, chunkOf, func(x, y, z int, rid uint32, water bool) {
			buf.b[cellIndex(x&15, y, z&15)] = stateOfRuntime(rid, water)
			wrote = true
		})
		sb := &structBase{base: cb}
		if wrote {
			buf.computeTops(0, 15, 0, 15)
			sb.base = compact(buf)
		}
		for _, be := range bes {
			cell := int32(cellIndex(be.Pos[0]&15, be.Pos[1], be.Pos[2]&15))
			sb.bes = append(sb.bes, structBE{be, cell, buf.b[cell]})
		}
		e.sb = sb
		n := int64(len(sb.bes)) * 128
		if wrote {
			n += sb.base.bytes()
		}
		return n
	})
	return e.sb
}

// structureEntities are a chunk's structure block entities whose block the decoration left in place.
func (g *Generator) structureEntities(s *chunkScratch, cx, cz int, final *[chunkCells]bs) []chunk.BlockEntity {
	var out []chunk.BlockEntity
	for _, b := range g.cachedStructBase(s, cx, cz).bes {
		if final[b.cell] == b.st {
			out = append(out, b.be)
		}
	}
	return out
}

// dimRange is the dimension's block range.
func (g *Generator) dimRange() cube.Range {
	switch g.dim {
	case dimNether:
		return world.Nether.Range()
	case dimEnd:
		return world.End.Range()
	}
	return world.Overworld.Range()
}

// stateOfRuntime is the generator state for a block a structure wrote. Air and still water are the
// generator's own; any other block gets a state of its own (its Bedrock name with the runtime ID), which
// shares the block index and tags of the generator's block of that name, and flags from the
// generator's tables or else Dragonfly's model. Its runtime ID is the one written, so the final chunk
// holds exactly the structure's block. It depends only on the runtime ID, never on what was
// generated before.
func stateOfRuntime(rid uint32, water bool) bs {
	key := uint64(rid) << 1
	if water {
		key |= 1
	}
	if v, ok := runtimeStates.Load(key); ok {
		return v.(bs)
	}
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if v, ok := runtimeStates.Load(key); ok {
		return v.(bs)
	}
	st := newRuntimeState(rid, water)
	runtimeStates.Store(key, st)
	return st
}

var (
	runtimeStates sync.Map
	runtimeMu     sync.Mutex
	plainWater    = sync.OnceValue(func() uint32 { return world.BlockRuntimeID(block.Water{Still: true, Depth: 8}) })
	plainAir      = sync.OnceValue(func() uint32 { return world.BlockRuntimeID(block.Air{}) })
)

// openNames are parts of Bedrock names of blocks that do not stop motion (torches, rails, carpets ...).
var openNames = []string{"torch", "rail", "ladder", "vine", "web", "carpet", "redstone_wire", "lever", "button",
	"pressure_plate", "banner", "sign", "flower", "sapling", "tulip", "grass", "fern", "bush", "mushroom", "roots",
	"sprouts", "lichen", "frame", "tripwire", "skull", "head", "candle", "pot", "seagrass", "kelp", "coral_fan", "lantern"}

func newRuntimeState(rid uint32, water bool) bs {
	if !water && rid == plainAir() {
		return 0
	}
	if !water && rid == plainWater() {
		return sWater
	}
	b, _ := world.BlockByRuntimeID(rid)
	name := "unknown"
	if b != nil {
		name, _ = b.EncodeBlock()
		name = strings.TrimPrefix(name, "minecraft:")
	}
	props := "~rid=" + strconv.FormatUint(uint64(rid), 10)
	if water {
		props += ",waterlogged=true"
	}
	st := stateOf(name + "[" + props + "]")
	stateLock.Lock()
	defer stateLock.Unlock()
	fl, listed := blockFlags[name]
	switch {
	case name == "air":
		fl = fAir | fReplaceable
	case listed:
	case b == nil:
		fl = fCube
	default:
		if _, ok := b.(world.Liquid); ok {
			fl = fLiquid | fReplaceable
			if strings.Contains(name, "water") {
				fl |= fWater
			} else {
				fl |= fLava
			}
		} else if _, ok := b.Model().(model.Solid); ok {
			fl = fCube
		} else {
			fl = fMotion | fSolid
			for _, o := range openNames {
				if strings.Contains(name, o) {
					fl = 0
					break
				}
			}
		}
	}
	if water {
		fl |= fWater | fSource
	}
	stateFlag[st] = fl
	dfLock.Lock()
	dfRT[st], dfDone[st] = rid, true
	dfLock.Unlock()
	bnameOf[st].Store(uint32(internBName(name)) + 1)
	return st
}
