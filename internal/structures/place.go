package structures

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Pieces go into a chunk when it is made, after its terrain and decoration: each structure piece that
// reaches the chunk writes the part of it inside the chunk, as BDS post-processes a chunk with the
// chunk's box. A piece crossing chunks is rebuilt from its start for each of them (plan constraint);
// assembled starts are kept in a small bounded cache, which changes speed and never a result.
//
// Every piece post-processed in a chunk draws from one random, MT(the chunk's decoration seed), fresh
// per chunk, in placement order (N2 2.1); chests take their loot seed from it with nextInt().

// writer is one chunk being written.
type writer struct {
	cx, cz int
	c      *chunk.Chunk
	rng    *worldgen.BedrockRandom
	bes    []chunk.BlockEntity
	carts  []minecartIntent
}

func (w *writer) in(x, y, z int) bool {
	return x>>4 == w.cx && z>>4 == w.cz && y >= minY && y <= maxY
}

func (w *writer) block(x, y, z int) world.Block {
	b, _ := world.BlockByRuntimeID(w.c.Block(uint8(x&15), int16(y), uint8(z&15), 0))
	return b
}

func (w *writer) set(x, y, z int, rid uint32, water bool) {
	lx, lz := uint8(x&15), uint8(z&15)
	w.c.SetBlock(lx, int16(y), lz, 0, rid)
	if water {
		w.c.SetBlock(lx, int16(y), lz, 1, waterRID())
	} else if w.c.Block(lx, int16(y), lz, 1) != airRID() {
		w.c.SetBlock(lx, int16(y), lz, 1, airRID())
	}
}

// height is the chunk's heightmap at a column: one above the highest block that is not air.
func (w *writer) height(x, z int) int {
	return int(w.c.HighestBlock(uint8(x&15), uint8(z&15))) + 1
}

// chestEntity is a chest's block entity with a loot table and seed.
func chestEntity(x, y, z int, table string, seed int32) chunk.BlockEntity {
	return chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{"id": "Chest", "LootTable": table, "LootTableSeed": seed}}
}

// chest puts a chest that will roll table with the next loot seed (N2 2.1).
func (w *writer) chest(x, y, z int, rid uint32, table string) {
	w.set(x, y, z, rid, false)
	w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{
		"id": "Chest", "LootTable": table, "LootTableSeed": w.rng.NextInt(),
	}})
}

var (
	ridOnce          sync.Once
	ridAir, ridWater uint32
)

func loadRIDs() {
	ridOnce.Do(func() {
		ridAir = world.BlockRuntimeID(block.Air{})
		ridWater = world.BlockRuntimeID(block.Water{Still: true, Depth: 8})
	})
}

func airRID() uint32   { loadRIDs(); return ridAir }
func waterRID() uint32 { loadRIDs(); return ridWater }

// PlaceChunk writes every structure piece of chunk (cx, cz) and returns the chests' block entities.
func (s *Structures) PlaceChunk(cx, cz int, c *chunk.Chunk) []chunk.BlockEntity {
	if s.data == nil {
		return nil
	}
	return s.place(cx, cz, c).bes
}

// place writes the chunk's pieces and returns the writer, with the chunk's block entities and the
// chest minecarts its mineshafts would spawn.
func (s *Structures) place(cx, cz int, c *chunk.Chunk) *writer {
	w := &writer{cx: cx, cz: cz, c: c, rng: worldgen.NewBedrockRandom(s.g.DecorationSeed(cx, cz))}
	if _, ok := s.StartIn(BuriedTreasure, cx, cz); ok {
		s.buriedTreasure(w)
	}
	if st, ok := s.StartIn(Scattered, cx, cz); ok {
		switch st.Piece {
		case "witch_hut":
			s.witchHut(w)
		case "jungle_temple":
			s.jungleTemple(w)
		}
	}
	s.placeAncientCities(w)
	s.placeVillages(w)
	s.placeShipwrecks(w)
	s.placeIgloos(w)
	s.placePyramids(w)
	s.placeDataJigsaws(w)
	// Strongholds, then mineshafts, after the others: in BDS's seed-100 chunks holding both, the
	// stronghold's chest seeds sit at the draw index of a stronghold-only stream (P14.3), and buried
	// treasure chests keep BDS's seeds only when they draw before the mineshafts (TestBuriedTreasure:
	// 26 of 27 this way, 23 with mineshafts first). The order was not read from the code.
	s.placeStrongholds(w)
	s.placeMineshafts(w)
	return w
}

// citiesNear calls f with the pieces of every ancient city whose start is within reach chunks of (cx, cz).
func (s *Structures) citiesNear(cx, cz, reach int, f func([]*piece)) {
	sp := spreads[AncientCity]
	for rx := floorDiv(cx-reach, sp.spacing); rx <= floorDiv(cx+reach, sp.spacing); rx++ {
		for rz := floorDiv(cz-reach, sp.spacing); rz <= floorDiv(cz+reach, sp.spacing); rz++ {
			st, ok := s.regionStart(AncientCity, rx, rz)
			if ok && abs(st.CX-cx) <= reach && abs(st.CZ-cz) <= reach {
				f(s.assembled.get(st, s.ancientCity))
			}
		}
	}
}

// InsideStructure says whether a block is inside a structure piece's box: the generator's geode asks
// it at its origin and places nothing inside one (NOTES-bedrock-exact T15). Ancient cities are the only
// structures with pieces so far, so only their pieces answer.
func (s *Structures) InsideStructure(x, y, z int) bool {
	if s.data == nil {
		return false
	}
	in := false
	s.citiesNear(x>>4, z>>4, ancientReach, func(pieces []*piece) {
		for _, p := range pieces {
			if p.box.inside([3]int{x, y, z}) {
				in = true
				return
			}
		}
	})
	return in
}

// placeAncientCities writes the pieces of every city that reaches the chunk.
func (s *Structures) placeAncientCities(w *writer) {
	chunkBox := box{w.cx * 16, minY, w.cz * 16, w.cx*16 + 15, maxY, w.cz*16 + 15}
	s.citiesNear(w.cx, w.cz, ancientReach, func(pieces []*piece) {
		for _, p := range pieces {
			if p.elem.template != nil && p.box.intersects(chunkBox) {
				s.placeTemplate(w, p)
			}
		}
	})
}

// TerrainAdjustments are the terrain adaptations of the pieces near chunk (cx, cz), for the generator's
// density (N2 phase 3 round 2, terrain adaptation): every rigid piece of an ancient city, beard and
// shave with widths 1 and 5, registered in the chunks its box grown by 12 blocks in x and z reaches.
// Feature elements are rigid pieces too, with their one-block box.
func (s *Structures) TerrainAdjustments(cx, cz int, into []worldgen.TerrainAdjustment) []worldgen.TerrainAdjustment {
	if s.data == nil {
		return into
	}
	into = s.dataAdjustments(cx, cz, into)
	into = s.villageAdjustments(cx, cz, into)
	s.citiesNear(cx, cz, ancientReach+1, func(pieces []*piece) {
		for _, p := range pieces {
			b := p.box
			if (b[0]-12)>>4 <= cx && cx <= (b[3]+12)>>4 && (b[2]-12)>>4 <= cz && cz <= (b[5]+12)>>4 {
				into = append(into, worldgen.TerrainAdjustment{Mode: worldgen.AdaptBeardAndShave, Box: b,
					DeltaY: p.deltaY, MinW: 1, MaxW: 5})
			}
		}
	})
	return into
}

// placeTemplate writes a rigid template piece's blocks inside the chunk, in template order. Jigsaw
// blocks become their final state; structure voids leave the world alone. The ancient city's
// degradation processors are not applied (their random was not read, N2 1.8).
func (s *Structures) placeTemplate(w *writer, p *piece) {
	t := p.elem.template
	var rr *worldgen.BedrockRandom
	if len(p.elem.rules) > 0 {
		rr = worldgen.NewBedrockRandom(s.g.DecorationSeed(w.cx, w.cz)) // P9.1: fresh per element and chunk.
	}
	processed, procLoot := s.processedNames(p)
	var heights map[[2]int]int
	for bi, b := range t.blocks {
		q := rotate(b.pos, p.rot)
		x, y, z := p.pos[0]+q[0], p.pos[1]+q[1], p.pos[2]+q[2]
		if p.elem.terrain && p.elem.bedrockLegacy {
			// Terrain matching: each column follows the ground (P9.1: y = height - 1 + local y).
			if x>>4 != w.cx || z>>4 != w.cz {
				continue
			}
			if heights == nil {
				heights = map[[2]int]int{}
			}
			h, ok := heights[[2]int{x, z}]
			if !ok {
				h = s.g.SurfaceWG(x, z)
				heights[[2]int{x, z}] = h
			}
			y = h - 1 + b.pos[1]
		}
		if !w.in(x, y, z) {
			continue
		}
		st := t.palette[b.state]
		if st.name == "jigsaw" {
			if p.elem.bedrockLegacy {
				continue // LegacyStructureTemplate::place skips jigsaw blocks (P9.1 flag +0xc).
			}
			fs, _ := b.nbt["final_state"].(string)
			st = parseJavaState(fs)
		}
		res := convert.state(st, p.rot)
		if !res.ok {
			continue
		}
		if rr != nil {
			res.name, res.rid = w.applyRules(p.elem.rules, rr, res.name, res.rid, x, y, z)
		}
		if processed != nil && processed[bi] != res.name {
			if nr, ok := ridOfName(processed[bi]); ok {
				res.name, res.rid = processed[bi], nr
			}
		}
		if procLoot != nil && procLoot[bi] != nil && (res.name == "suspicious_gravel" || res.name == "suspicious_sand") {
			w.set(x, y, z, res.rid, false)
			w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{"id": "BrushableBlock",
				"LootTable": procLoot[bi].table, "LootTableSeed": int32(procLoot[bi].seed)}})
			continue
		}
		if p.elem.legacy && res.name == "air" {
			continue // A legacy element places no air (Java's LegacySinglePoolElement).
		}
		if lt, ok := b.nbt["LootTable"].(string); ok && lt != "" && (res.name == "chest" || res.name == "trapped_chest") {
			w.chest(x, y, z, res.rid, bedrockLootPath(lt))
			continue
		}
		w.set(x, y, z, res.rid, res.waterlog)
	}
}

// parseJavaState reads "minecraft:name[k=v,...]".
func parseJavaState(s string) tState {
	if s == "" {
		s = "minecraft:air"
	}
	name, props, _ := strings.Cut(s, "[")
	st := tState{name: strings.TrimPrefix(name, "minecraft:"), props: map[string]string{}}
	for _, kv := range strings.Split(strings.TrimSuffix(props, "]"), ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			st.props[k] = v
		}
	}
	return st
}

// bedrockLootPath turns a template's Java loot table name into the path Bedrock chests store (N1 1.2):
// "minecraft:chests/ancient_city" is "loot_tables/chests/ancient_city.json".
func bedrockLootPath(java string) string {
	return "loot_tables/" + strings.TrimPrefix(java, "minecraft:") + ".json"
}

// keyCache keeps recently assembled code-built structures (mineshafts, strongholds) by start chunk,
// bounded like assemblyCache.
type keyCache struct {
	mu    sync.Mutex
	items map[[2]int]any
	order [][2]int
}

func (c *keyCache) get(k [2]int, build func() any) any {
	c.mu.Lock()
	if v, ok := c.items[k]; ok {
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	v := build()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[[2]int]any{}
	}
	if old, ok := c.items[k]; ok {
		return old
	}
	if len(c.order) >= assemblyCacheSize*4 {
		delete(c.items, c.order[0])
		c.order = c.order[1:]
	}
	c.items[k] = v
	c.order = append(c.order, k)
	return v
}

// assemblyCache keeps recently assembled starts, bounded (plan: a small cache inside the 30 MB cap).
type assemblyCache struct {
	mu    sync.Mutex
	items map[Start][]*piece
	order []Start
}

const assemblyCacheSize = 16

func (c *assemblyCache) get(st Start, build func(Start) []*piece) []*piece {
	c.mu.Lock()
	if p, ok := c.items[st]; ok {
		c.mu.Unlock()
		return p
	}
	c.mu.Unlock()
	p := build(st)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[Start][]*piece{}
	}
	if _, ok := c.items[st]; !ok {
		if len(c.order) >= assemblyCacheSize {
			delete(c.items, c.order[0])
			c.order = c.order[1:]
		}
		c.items[st] = p
		c.order = append(c.order, st)
	}
	return c.items[st]
}
