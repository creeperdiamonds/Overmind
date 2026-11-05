# structures

Overworld structures (plan Task 9), our own Go from the study of BDS 1.26.60.6: `NOTES-bedrock-structures.md` (N1) and `NOTES-structures-phase2.md` (N2), outside the repo. Every constant cites its section. Nothing of Mojang's is in the repo: templates, loot tables and the data-driven jigsaw JSON are read at start-up from the install in `BedrockDataDir` (`LoadData`); without it `internal/app` skips structures with one log line.

## How it fits

- `worldgen` exposes what structures need (`structures_api.go`: seeds, stored biomes, preliminary surface, Bedrock's MT and Java's LCG) and calls `PlaceChunk` after a chunk's own blocks are written (`SetStructures`, `GenerateColumn`). A chunk is still a pure function of the seed and its position: a piece that crosses chunks is rebuilt from its start in each (a 16-entry cache of assembled starts changes speed only).
- Chests get `LootTable` and `LootTableSeed` block entity fields (N2 2.1: the seed is `nextInt()` of MT(the chunk's decoration seed), one stream per chunk, drawn in placement order). The Dragonfly fork rolls the table on first open through `block.LootFiller` = `Data.Fill` (N2 2.2, 2.3).
- `NearestStronghold` (eye of ender): rings and 200-chunk grid (N1 2.5).

## State per structure

| Structure | Start (placement) | Pieces |
|---|---|---|
| Buried treasure | N1 2.2, checked | built (N2 2.1); y rule's "solid" flag and heightmap measured on 27 chests |
| Ancient city | N1 2.2, checked; BDS locate 28/28 | legacy jigsaw (N2 1.3-1.6, sculk pool with its empty element, N2 phase 3): 78 pieces, 15/15 chests with BDS's loot seed on seed 100. Terrain adaptation (beard and shave) in the generator's density (`worldgen/structures_terrain.go`, N2 phase 3 round 2). Block-rot processors and the sculk feature elements are not applied (their random is not read) |
| Village | N1 2.2 + village_type biomes, checked; BDS locate 12/12 | not built: element table (template, projection, processors) and terrain adaptation not decoded |
| Desert pyramid, jungle temple, witch hut, igloo | N1 2.2 (one scattered feature), sea-level field 63 assumed; windows agree | not built: pieces not decoded (N2 3) |
| Ocean monument, mansion | N1 2.2; BDS locate 28/28 each | not built: not decoded |
| Pillager outpost | N1 2.2, checked | not built: N2 3 has only its first draws |
| Shipwreck, ocean ruin, ruined portal | N1 2.2, checked | not built: not decoded (N2 3) |
| Mineshaft | N1 2.3, checked against cave spider spawners | not built: not decoded |
| Stronghold | N1 2.5 and N2 phase 3 round 2; `NearestStronghold` equals BDS locate from 24 of 28 origins | not built: not decoded |
| Trail ruins, abandoned camps | Java LCG sets (N1 2.4), checked; trail ruins: BDS locate 12/12 at our stubs, with the column biome test (fitted, see below) | built: br::worldgen jigsaw = Java's JigsawPlacement (N2 phase 8, `brjigsaw.go`), pools, structures and templates from the install; piece volumes equal BDS's for all 4 measured (3 camps, 1 trail ruin). Processors (archaeology, block rot) and terrain adaptation (bury, beard_thin: br::worldgen's beardifier) are not read, so not applied |
| Trial chambers | Java LCG set, every candidate kept; BDS locate 28/28 within 8 chunks | not built: pools are code, the study's table of them is not trusted yet (N2 phase 8) |
| Fossils | feature, not a structure start | not built |

Biome tests sample at "surf" (N1 2.2), the preliminary-surface cache, whose filling was not read. We use the noise stage's top solid block (`worldgen.NoiseHeight`), measured: it explains all the evidence and the eight structure windows; the generator's own preliminary level missed four windows. It places 3 buried treasure starts on seed 12345 that BDS lacks.

Trail ruins' biome test is fitted (ruling 41: 12 of 12 BDS locate answers on 4 worlds; the stub-only test misses 2): some biome of the stub's column, from the stub up to the WG surface, in BDS's own per-block model (`worldgen.BedrockBiomeID`), passes the JSON filter. The other structures keep the quart model: switched to the per-block model, they lose 6 scanned treasure chests and 14 locate answers.

"Checked" means every scanned BDS chest, spawner or structure volume in the four measured worlds lies within the study's distance of a start we place, with our biome tests (`TestStartsMatchBDS`).

## Data

- `Data.Template`: Java structure NBT (gzip, big-endian; `nbt.go` is our reader). Newest vanilla pack wins.
- `javablocks.go`: Java block states to Bedrock 1.26 names and states, then the nearest registered Dragonfly state. BDS's own table is code, not data; names are what the tests compare, states are best effort.
- Loot (`loot.go`): fill (N2 2.3) and rolls, tiers, entry pick, set_count, set_data, set_damage and the enchant functions' draws as read from the code (N2 phase 3). BDS picks the enchantment with a thread-local random, not the loot seed; ours picks one compatible enchantment at random (the choice rule was not read). Maps, potions, dyes, books and stew are not read: the item is made without them. Not validated: no BDS chest has been opened.
- Transcribed tables: a structure's pool table read from BDS's registration code lives in its own `pools_<structure>.go` (now `pools_ancient_city.go`), with a header naming its source; interim, while the owner decides whether such tables may be transcribed (review r6 I6).
- Loot limits: a table is refused when read if its rolls, tiers or bonus rolls pass 64, a count or level passes 1024, or a weight passes 2^20; one fill makes at most 4 entry picks per slot (loot is rolled on the tick).
- Terrain adaptation: `TerrainAdjustments` gives the generator each rigid city piece (feature elements included) whose box grown by 12 reaches the chunk; the generator adds the kernel term to every block's density after the squeeze and does not take its all-solid or all-sky cell shortcuts there.

## Tests

`OVERMIND_BEDROCK_DATA` (BDS behavior_packs) and `OVERMIND_GOLDEN_BEDROCK` (the study's golden-bedrock: `structures/evidence.json` built from the study's scans, `whole/*.bin.gz`). Without them the data tests skip.
