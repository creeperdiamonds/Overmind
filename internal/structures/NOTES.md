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
| Ancient city | N1 2.2, checked | legacy jigsaw (N2 1.3-1.6), same 74 pieces as the study's model; first 44 match BDS. Degradation processors, the sculk feature elements and terrain adaptation are not read, so the city sits in solid deepslate |
| Village | N1 2.2 + village_type biomes, checked | not built: element table (template, projection, processors) and terrain adaptation not decoded |
| Desert pyramid, jungle temple, witch hut, igloo | N1 2.2 (one scattered feature), sea-level field 63 assumed; windows agree | not built: pieces not decoded (N2 3) |
| Ocean monument, mansion | N1 2.2 | not built: not decoded |
| Pillager outpost | N1 2.2, checked | not built: N2 3 has only its first draws |
| Shipwreck, ocean ruin, ruined portal | N1 2.2, checked | not built: not decoded (N2 3) |
| Mineshaft | N1 2.3, checked against cave spider spawners | not built: not decoded |
| Stronghold | N1 2.5 (rings and grid), not checked against a world | not built: not decoded |
| Trail ruins, trial chambers, abandoned camps | Java LCG sets (N1 2.4), checked | not built: the br::worldgen placer was not read (N2 1). Trial chambers keep every candidate (biome filter not read); camps and trail ruins test their JSON biome filter at the chunk centre, at surf (position not read) |
| Fossils | feature, not a structure start | not built |

Biome tests sample at "surf" (N1 2.2), the preliminary-surface cache, whose filling was not read. We use the noise stage's top solid block (`worldgen.NoiseHeight`), measured: it explains all the evidence and the eight structure windows; the generator's own preliminary level missed four windows. It places 3 buried treasure starts on seed 12345 that BDS lacks.

"Checked" means every scanned BDS chest, spawner or structure volume in the four measured worlds lies within the study's distance of a start we place, with our biome tests (`TestStartsMatchBDS`).

## Data

- `Data.Template`: Java structure NBT (gzip, big-endian; `nbt.go` is our reader). Newest vanilla pack wins.
- `javablocks.go`: Java block states to Bedrock 1.26 names and states, then the nearest registered Dragonfly state. BDS's own table is code, not data; names are what the tests compare, states are best effort.
- Loot (`loot.go`): fill as read (N2 2.3). getRandomItems (N2 2.5) was not decoded: pools, rolls, weighted pick, `set_count`, `set_data`, `set_damage` follow Java's LootPool shape, unconfirmed; enchanting, maps, potions, dyes, book and stew functions are not applied.

## Tests

`OVERMIND_BEDROCK_DATA` (BDS behavior_packs) and `OVERMIND_GOLDEN_BEDROCK` (the study's golden-bedrock: `structures/evidence.json` built from the study's scans, `whole/*.bin.gz`). Without them the data tests skip.
