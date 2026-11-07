package worldgen

import (
	"log/slog"
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
// entities with the state each sits on. base is nil when no structure wrote in the chunk: the carvers
// stage is then looked up again (cachedBase), so this entry never keeps an uncharged copy alive.
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
		if b := g.cachedStructBase(s, cx, cz).base; b != nil {
			return b
		}
	}
	return g.cachedBase(s, cx, cz)
}

// cachedStructBase gives a chunk's structured base, from the cache. The carvers stage is expanded only
// once a structure first reads or writes the chunk; most chunks have no piece.
func (g *Generator) cachedStructBase(s *chunkScratch, cx, cz int) *structBase {
	e := g.cache.get(cacheKey{cacheStruct, g.dim, int32(cx), int32(cz)}, func(e *cacheEntry) int64 {
		var buf *baseChunk
		expand := func() {
			if buf == nil {
				buf = s.takeBuffer()
				g.cachedBase(s, cx, cz).expand(buf)
			}
		}
		defer func() {
			if buf != nil {
				s.giveBuffer(buf)
			}
		}()
		chunkOf := func() *chunk.Chunk {
			expand()
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
		var refused map[int32]bool
		bes := g.structs.(StructureLogger).PlaceLogged(cx, cz, chunkOf, func(x, y, z int, rid uint32, water bool) {
			expand()
			cell := cellIndex(x&15, y, z&15)
			st, ok := stateOfRuntime(rid, water)
			if !ok {
				if refused == nil {
					refused = map[int32]bool{}
				}
				refused[int32(cell)] = true
				return
			}
			buf.b[cell] = st
			wrote = true
		})
		sb := &structBase{}
		if wrote {
			buf.computeTops(0, 15, 0, 15)
			sb.base = compact(buf)
		}
		for _, be := range bes {
			cell := int32(cellIndex(be.Pos[0]&15, be.Pos[1], be.Pos[2]&15))
			if refused[cell] {
				continue
			}
			expand()
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
// generated before. ok is false when the state tables have no room left for a new block (only an
// install with far more structure blocks than vanilla's could fill them): the write is then refused,
// the cell keeps what it had, and the refusal is logged once.
func stateOfRuntime(rid uint32, water bool) (st bs, ok bool) {
	key := uint64(rid) << 1
	if water {
		key |= 1
	}
	if v, ok := runtimeStates.Load(key); ok {
		r := v.(runtimeState)
		return r.st, r.ok
	}
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if v, ok := runtimeStates.Load(key); ok {
		r := v.(runtimeState)
		return r.st, r.ok
	}
	st, ok = newRuntimeState(rid, water)
	runtimeStates.Store(key, runtimeState{st, ok})
	if !ok {
		tablesFullOnce.Do(func() {
			slog.Default().Warn("The generator's block state tables are full: structure blocks of new kinds are not written",
				"states", maxStates, "blocks", maxBlocks)
		})
	}
	return st, ok
}

type runtimeState struct {
	st bs
	ok bool
}

// Room kept in the state tables for the generator's own states, which code adds by name (a few at
// generation time, such as the cocoa facings); structure states stop short of it.
const (
	reservedStates = 256
	reservedBlocks = 32
)

var (
	runtimeStates  sync.Map
	runtimeMu      sync.Mutex
	tablesFullOnce sync.Once
	plainWater     = sync.OnceValue(func() uint32 { return world.BlockRuntimeID(block.Water{Still: true, Depth: 8}) })
	plainAir       = sync.OnceValue(func() uint32 { return world.BlockRuntimeID(block.Air{}) })
)

// stateRoom says whether a state named name (base[props]) can still be added for a structure block.
func stateRoom(name string) bool {
	stateLock.Lock()
	defer stateLock.Unlock()
	if _, ok := stateIndex[name]; ok {
		return true
	}
	base, _, _ := strings.Cut(name, "[")
	_, known := blockIndex[base]
	return structureRoom(len(stateIndex), len(blockIndex), known)
}

// structureRoom says whether tables holding states and blocks entries take one more structure state,
// of a block already listed (known) or not.
func structureRoom(states, blocks int, known bool) bool {
	return states < maxStates-reservedStates && (known || blocks < maxBlocks-reservedBlocks)
}

// openNames are parts of Bedrock names of blocks that do not stop motion (torches, rails, carpets ...).
var openNames = []string{"torch", "rail", "ladder", "vine", "web", "carpet", "redstone_wire", "lever", "button",
	"pressure_plate", "banner", "sign", "flower", "sapling", "tulip", "grass", "fern", "bush", "mushroom", "roots",
	"sprouts", "lichen", "frame", "tripwire", "skull", "head", "candle", "pot", "seagrass", "kelp", "coral_fan", "lantern"}

func newRuntimeState(rid uint32, water bool) (bs, bool) {
	if !water && rid == plainAir() {
		return 0, true
	}
	if !water && rid == plainWater() {
		return sWater, true
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
	if !stateRoom(name + "[" + props + "]") {
		return 0, false
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
	return st, true
}
