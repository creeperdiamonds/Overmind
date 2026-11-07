package structures

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Pieces go into a chunk when it is made: each structure piece that reaches the chunk writes the part of
// it inside the chunk, as BDS post-processes a chunk with the chunk's box. In the Bedrock profile that is
// after the chunk's terrain and before its decoration (PlaceLogged, worldgen/structures_base.go, P21.1);
// PlaceChunk writes a finished chunk instead. A piece crossing chunks is rebuilt from its start for each
// of them (plan constraint); assembled starts are kept in small bounded caches, which change speed and
// never a result.
//
// The pieces post-processed in a chunk draw from the chunk's random, MT(the chunk's decoration seed),
// seeded afresh before each structure type (P17.2; the Nether and End reseed per start, which differs
// only where two starts of one type reach a chunk) and continued within the type, in placement order
// (N2 2.1); chests take their loot seed from it with nextInt(). The data-driven jigsaws draw from their
// own Xoroshiro per chunk instead (datajigsaw.go).

// writer is one chunk being written.
type writer struct {
	cx, cz int
	c      *chunk.Chunk        // Use chunk(): made on first use when mk is set.
	mk     func() *chunk.Chunk // Makes the chunk (PlaceLogged), so a chunk no piece reaches is never built.
	rng    *worldgen.BedrockRandom
	seed   uint32 // The chunk's post-process seed, which rng starts from for each structure type.
	bes    []chunk.BlockEntity
	carts  []minecartIntent
	// spawned are the spider corridors that placed their spawner in this chunk.
	spawned map[spiderKey]bool
	// spawns are the mobs the structures would spawn here (elder guardians, piglins, hoglins, shulkers,
	// illagers, allays), and carts the mineshafts' chest minecarts: worked out, then dropped with the
	// writer (PlaceChunk and PlaceLogged return block entities only), until entities in new chunks exist
	// (stage 4 needs a path out for them; docs/stage-2-known-gaps.md).
	spawns []spawnIntent
	// lo, hi are the dimension's block range: the Overworld's -64..319 or the Nether's 0..127.
	lo, hi int
	// trace, when set (tests), sees every block a structure writes.
	trace func(x, y, z int, rid uint32)
	// log, when set, hears every block written, with its second-layer water (PlaceLogged).
	log func(x, y, z int, rid uint32, water bool)
	// beAt indexes bes by position (bes[:beSeen]), so a block written over a structure block entity
	// drops it: BDS removes a block's entity when the block is replaced.
	beAt   map[[3]int]int
	beSeen int
}

// spawnIntent is a mob a structure would spawn at a block.
type spawnIntent struct {
	Pos    [3]int
	Entity string
}

func (w *writer) chunk() *chunk.Chunk {
	if w.c == nil {
		w.c = w.mk()
	}
	return w.c
}

func (w *writer) in(x, y, z int) bool {
	return x>>4 == w.cx && z>>4 == w.cz && y >= w.lo && y <= w.hi
}

// blockName is a cell's block name without "minecraft:".
func (w *writer) blockName(x, y, z int) string {
	return ridName(w.chunk().Block(uint8(x&15), int16(y), uint8(z&15), 0))
}

var ridNames sync.Map

// ridName is a runtime ID's block name without "minecraft:".
func ridName(rid uint32) string {
	if v, ok := ridNames.Load(rid); ok {
		return v.(string)
	}
	n := ""
	if b, ok := world.BlockByRuntimeID(rid); ok {
		n, _ = b.EncodeBlock()
		n = strings.TrimPrefix(n, "minecraft:")
	}
	ridNames.Store(rid, n)
	return n
}

func (w *writer) block(x, y, z int) world.Block {
	b, _ := world.BlockByRuntimeID(w.chunk().Block(uint8(x&15), int16(y), uint8(z&15), 0))
	return b
}

func (w *writer) set(x, y, z int, rid uint32, water bool) {
	if len(w.bes) > 0 {
		w.dropEntityAt([3]int{x, y, z}, rid)
	}
	if w.trace != nil {
		w.trace(x, y, z, rid)
	}
	if w.log != nil {
		w.log(x, y, z, rid, water)
	}
	lx, lz := uint8(x&15), uint8(z&15)
	w.chunk().SetBlock(lx, int16(y), lz, 0, rid)
	if water {
		w.chunk().SetBlock(lx, int16(y), lz, 1, waterRID())
	} else if w.chunk().Block(lx, int16(y), lz, 1) != airRID() {
		w.chunk().SetBlock(lx, int16(y), lz, 1, airRID())
	}
}

// dropEntityAt drops the block entity at p, if one was made there, when rid writes another block over
// its own (the same block written again keeps it). Every structure writes a block before appending its
// entity, so an entity outlives only its own block.
func (w *writer) dropEntityAt(p [3]int, rid uint32) {
	if w.beAt == nil {
		w.beAt = map[[3]int]int{}
	}
	for ; w.beSeen < len(w.bes); w.beSeen++ {
		if w.bes[w.beSeen].Data != nil {
			w.beAt[w.bes[w.beSeen].Pos] = w.beSeen
		}
	}
	if i, ok := w.beAt[p]; ok && w.chunk().Block(uint8(p[0]&15), int16(p[1]), uint8(p[2]&15), 0) != rid {
		w.bes[i].Data = nil
		delete(w.beAt, p)
	}
}

// entities are the block entities left standing.
func (w *writer) entities() []chunk.BlockEntity {
	out := w.bes[:0:0]
	for _, be := range w.bes {
		if be.Data != nil {
			out = append(out, be)
		}
	}
	return out
}

// height is the chunk's heightmap at a column: one above the highest block that is not air.
func (w *writer) height(x, z int) int {
	return int(w.chunk().HighestBlock(uint8(x&15), uint8(z&15))) + 1
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
	return s.place(cx, cz, c, nil).entities()
}

// PlaceLogged is PlaceChunk reporting every block it writes: the generator places structures into a
// chunk's terrain before decorating it (NOTES-structures-phase2 P21.1, worldgen/structures_base.go).
// The chunk is made only when a piece first reads or writes it.
func (s *Structures) PlaceLogged(cx, cz int, chunkOf func() *chunk.Chunk, write func(x, y, z int, rid uint32, water bool)) []chunk.BlockEntity {
	if s.data == nil {
		return nil
	}
	return s.placeWith(cx, cz, nil, chunkOf, write).entities()
}

// place writes the chunk's pieces and returns the writer, with the chunk's block entities and the
// chest minecarts its mineshafts would spawn.
func (s *Structures) place(cx, cz int, c *chunk.Chunk, log func(x, y, z int, rid uint32, water bool)) *writer {
	return s.placeWith(cx, cz, c, nil, log)
}

func (s *Structures) placeWith(cx, cz int, c *chunk.Chunk, mk func() *chunk.Chunk, log func(x, y, z int, rid uint32, water bool)) *writer {
	w := &writer{cx: cx, cz: cz, c: c, mk: mk, rng: worldgen.NewBedrockRandom(s.g.DecorationSeed(cx, cz)), seed: s.g.DecorationSeed(cx, cz), spawned: map[spiderKey]bool{},
		lo: minY, hi: maxY, trace: s.trace, log: log}
	if s.nether {
		w.lo, w.hi = netherMinY, netherMaxY
		s.placeNether(w)
		return w
	}
	if s.end {
		w.lo, w.hi = endMinY, endMaxY
		s.placeEndCities(w)
		return w
	}
	// Each structure type post-processes with the chunk's random seeded afresh (P17.2), in the
	// registry's order (P18.4: village, stronghold, the scattered feature, mineshaft, mansion, buried
	// treasure, outpost, monument, ocean ruin, shipwreck, ruined portal, ancient city), which decides
	// only which blocks win where two types overlap. The data-driven jigsaws are not in that list (their
	// pass was not read): last.
	scattered := func(w *writer) {
		if st, ok := s.StartIn(Scattered, cx, cz); ok {
			switch st.Piece {
			case "witch_hut":
				s.witchHut(w)
			case "jungle_temple":
				s.jungleTemple(w)
			}
		}
		s.placeIgloos(w)
		s.placePyramids(w)
	}
	treasure := func(w *writer) {
		if _, ok := s.StartIn(BuriedTreasure, cx, cz); ok {
			s.buriedTreasure(w)
		}
	}
	for _, place := range []func(*writer){s.placeVillages, s.placeStrongholds, scattered, s.placeMineshafts, s.placeMansions, treasure,
		s.placeMonuments, s.placeOceanRuins, s.placeShipwrecks, s.placeRuinedPortals, s.placeAncientCities} {
		w.reseed()
		place(w)
	}
	s.placeDataDriven(w)
	return w
}

// reseed starts the chunk's post-process random again, as BDS does before each structure type (P17.2).
func (w *writer) reseed() { w.rng.SetSeed(w.seed) }

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
// it at its origin and places nothing inside one (NOTES-bedrock-exact T15, T22). Only ancient city
// pieces answer: which structures BDS's query counts is not read (T22 explains seed 100's extra geode by
// the city; T15's seed 987654321 one overlaps a trial chamber), so the others are not asked yet.
func (s *Structures) InsideStructure(x, y, z int) bool {
	if s.data == nil || s.nether || s.end {
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
			switch {
			case p.elem.template != nil && p.box.intersects(chunkBox):
				s.placeTemplate(w, p)
			case p.elem.template == nil && p.elem.name == sculkCityFeature && w.in(p.pos[0], p.pos[1], p.pos[2]):
				s.sculkElement(w, p.pos)
			}
		}
	})
}

// TerrainAdjustments are the terrain adaptations of the pieces near chunk (cx, cz), for the generator's
// density (N2 phase 3 round 2, terrain adaptation), each registered in the chunks its box grown by 12
// blocks in x and z reaches: the data-driven structures' bury and beard_thin records and junctions
// (dataAdjustments), the rigid village pieces' beards, the strongholds', and every rigid piece of an
// ancient city, beard and shave with widths 1 and 5 (feature elements are rigid pieces too, with their
// one-block box).
func (s *Structures) TerrainAdjustments(cx, cz int, into []worldgen.TerrainAdjustment) []worldgen.TerrainAdjustment {
	if s.data == nil || s.nether || s.end {
		return into
	}
	into = s.dataAdjustments(cx, cz, into)
	into = s.villageAdjustments(cx, cz, into)
	into = s.strongholdAdjustments(cx, cz, into)
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

// placeTemplate writes a template piece's blocks inside the chunk, in template order. Jigsaw blocks
// become their final state (a legacy element skips them); structure voids leave the world alone. The
// element's block rules (the ancient city's and village's degradation lists, blockrules.go) run on the
// chunk's rule random; a data-driven element's processors are worked out per piece (brprocess.go).
func (s *Structures) placeTemplate(w *writer, p *piece) { s.placeTemplateWith(w, p, nil) }

// placeTemplateWith is placeTemplate; with xr (a data-driven piece, P20.5) every loot container it
// places takes its seed from xr instead of the chunk's MT.
func (s *Structures) placeTemplateWith(w *writer, p *piece, xr *worldgen.Xoroshiro) {
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
			res.name, res.rid = w.applyRules(p.elem.rules, rr, res.name, res.rid, x, y, z, p.pos)
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
		lt, _ := b.nbt["LootTable"].(string)
		if id, ok := containerIDs[res.name]; ok && xr != nil && b.nbt != nil {
			// Every container block with NBT draws a word, as Java's template placement gives any
			// randomizable container a seed (nextLong()), and one with a table keeps its low word: every
			// container of the four measured worlds' trial chambers and camps (1,519) has its seed in its
			// chunk's stream (TestDataJigsawContainersMatchBDS).
			seed := int32(xr.NextLong())
			w.set(x, y, z, res.rid, res.waterlog)
			d := map[string]any{"id": id}
			if lt != "" {
				d["LootTable"], d["LootTableSeed"] = bedrockLootPath(lt), seed
			}
			w.bes = append(w.bes, chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: d})
			continue
		}
		if lt != "" && xr == nil && (res.name == "chest" || res.name == "trapped_chest") {
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
