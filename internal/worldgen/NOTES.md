# worldgen

Overmind's Overworld generator. A chunk depends only on the seed and its position, so chunks can be built in any order on any number of workers. GenerateChunk keeps nothing about a chunk afterwards; pooled scratch memory holds only values rewritten before they are read, and caches of preliminary surface levels and walk corner columns, which are pure functions of the seed and a position.

## Whose algorithms these are

**By the owner's decision, these layers follow Mojang's algorithms, so that a seed gives Mojang's biomes, terrain shape and noise caves:** seeding and random numbers, noise, climate, biome lookup, splines, density, cell interpolation, noise caves, and the aquifer's terrain-level decision. They were written here in our own Go, in our own structure and names, from a written study of the Java 26.1 generator (`NOTES-exact-seed.md`). No Mojang code or file is in this repo. The numeric tables needed at run time are our own Go tables with their source stated: `noise_table.go`, `biome_table.go`, and the spline knots in `spline.go`.

Bedrock's biomes, heights and noise caves match Java's on the same seed (BEDROCK-NOTES section 9), so reproducing Java reaches Bedrock for those layers.

**Golden tests** (`golden_test.go`) compare every layer with fixtures from the Java generator, doubles by raw bits. The fixtures are not in the repo: set `OVERMIND_GOLDEN` to their folder, or the tests skip. `tools/seedcheck` compares whole worlds with Bedrock's.

Explicit `float64()` and `float32()` conversions around products stop a platform fusing a multiply-add, which would change low bits. This has run on amd64 only.

**Speed without changing a result.** Some reads are skipped where a proof says the answer cannot depend on them, all in `coreAt`, `cavesAt`, `classifyCell` and `heights`: the 3D noise under deep ground, where caves no longer read it; everything at the ends of the slides; a whole cell far above the ground and over the skip height; noodle parts where no corner's toggle is on; jagged noise where jaggedness is 0; biome lookups above the first one that lands, above ground, on a box whose depth starts at 0.

**Ties in biome lookup.** Always a cold tree search, which matches the fixtures' `biome_quart_rtree_cold`. The game also keeps a per-thread last result, which can change an exact tie, and a world here must not depend on which worker asked. The golden test checks that warm search too, with the same code given a remembered leaf.

## Our own designs on top

- **Carvers** (`carvers.go`): a tunnel is a formula, a line bent by sine waves, cut as ellipsoids. Chances, height ranges and shape ranges are from the study. `startScale` (4) is tuned to Bedrock's air by band in the same windows (`TestCaveAirMatchesBedrock`). Start heights are even over the study's range; an earlier lean to high starts is gone, because it only made up for the old noise caves.
- **Lakes**, sizes our own. **Geodes**, shape our own; a hollow never opens under aquifer water.
- **Ore blob shape** (`blobWidths`, `blobWiden`, `blobTip`), fitted to the block counts per blob in the study. The table of ores is from the study and Bedrock's data files.
- **Copper and iron veins:** our own rules on the game's vein noises. They only choose which solid block appears.
- **Surface rules:** our own, reading the game's named noises. Surface depth, bands and thresholds are ours where the study gives only the idea. A column's surface biome is its biome over the ground, on the 4-block grid.
- **Decoration** (`decorate.go`): tree shapes, plant rates and the cold threshold are our own; tree rates per biome and kelp and seagrass are tuned to Bedrock's. It reaches other layers only through `groundSource`, `surfaceDefAt` and `treeSoil`.
- **Bedrock floor:** measured on Bedrock, 1, 1, .75, .5, .25, none from Y -59.
- **Hanging water:** water with air straight under it in the noise pass gets stone instead (`setFluid`), and tunnels and geodes never open under water. The game lets such water flow later; here it would hang until a block update. This is the one way our noise pass differs from the game's, so golden tests read the raw stage (`rawKind`, `rawTops`). Water can still sit beside air sideways.
- **DefaultSpawn:** our own search, with the aims the study names.

## Bedrock rules

- **From its data files:** medium diamond size 4, iron upper and middle size 10, infested stone size 8 at Y 0 to 64 with 14 attempts, replacing only stone and deepslate, an extra coal batch in the mountains tag, `ironUpperBottom` 80.
- **Both old tunings are gone.** Iron at Y 96 to 128 now measures 2.85 per 1000 against 2.98 with the file's 80: its gap came from the old terrain. Infested stone with the file's 14 attempts was 3 times too high, with two found causes: the rule was read at the surface instead of the blob's own height (under mountains much of Y 0 to 64 is dripstone caves or deep dark; raw seed 100 holds 5% of its infested blocks in those though they are 40% of that rock), and it replaced every stone kind where the file says stone and deepslate. By seed it now gives 0.99, 0, 2.58, 1.11 a chunk against 1.05, 0, 2.79, 1.10.
- **Emeralds and the mountains coal batch read the blob's height too:** same cause. Emeralds were 0.46 a chunk against 0.19 by the surface, and are 0.17 now. The study's addendum A5 emerald figures assumed no biome change with height and are no longer a target.
- **Rock patches:** granite, diorite and andesite replace stone only, tuff and gravel stone and deepslate (section 5, medium confidence). Dirt takes stone only, our choice. **Ores leave tuff alone:** our own, tuned to counts, not a Bedrock rule.

Not added, because no rule places them: dappled forest (the one biome mismatch of size on seed 1) and sulfur caves. Bedrock's steep-slope rule (section 5 row 7) is not followed.

## Files

- `random.go`: xoroshiro128++, seed factories. `noise.go`: octaves, stacks, normal noise, the terrain's blended noise. `noise_table.go`: the named noises.
- `climate.go`: the climate values and warp. `spline.go`: offset, factor, jaggedness. `terrain.go`: column shape, slides, squeeze.
- `caves.go`: the density's cave functions, cell blending in the game's x, y, z order, vein blocks, `columnWalk`.
- `aquifer.go`: preliminary surface, centres, status, pressure. `biomes.go` and `biome_table.go`: the parameter table, its R-tree, block-level zoom.
- `generator.go`: the pass order, and `fillNoise`. The game does biomes before noise; ours after, as density reads no stored biome.
- `carvers.go`, `surface.go`, `ores.go`, `strata.go`, `decorate.go`, `plant_blocks.go`, `blocks.go`: as above.

## Decoration

A tree is rolled from its origin chunk and written where it falls in the chunk being made, with no look at a real neighbour. For a neighbouring column it uses `chunkGround`, with tunnels cut once for the chunk plus 6 columns around it, and equals `SurfaceHeight` exactly (`TestChunkGroundMatchesSurfaceHeight`).

**Our own block types, `plant_blocks.go`:** `SnowLayer` and `Seagrass`. Dragonfly lists their states but has no Go type. Hardness and drops are our own. Snow does not melt, and there are no items for them.

**Open gap, soil depth:** the soil figure is logged, not asserted; it is one seed's figure, and surface depth is our own rule.

**Plants seen in Bedrock worlds with no Go type in Dragonfly, not placed:** bush, firefly bush, wildflowers, short and tall dry grass, cactus flower, azalea and flowering azalea, red and brown mushroom, sweet berry bush, cave vines, pale hanging moss, pale moss carpet, plus moss block, pale moss block, powder snow and glow lichen. Also lacking: leaf litter, mangrove propagule.

**Known gaps:** no 2 x 2 giant spruce or giant jungle trees, no vines, bamboo, mushrooms, melons, pumpkins, azaleas or moss. No trees in rivers (Bedrock 0.11 a chunk). A column's snow and biome follow the 4-block grid. Cane and cactus keep off the outermost columns.

## Heights

- `SurfaceHeight` matches the generated chunk exactly, including carvers and lakes, but walks every tunnel that could reach the column. Use it for a few columns.
- `NoiseHeight` skips carvers and lakes. It includes our surface extensions and sealing; the exact noise stage is `rawTops`.

## Known differences from Bedrock

- **Stored biomes inside 8-block cells.** At X and Z multiples of 8 we match Bedrock's stored biomes 99.8%; at the other quarts about 95%, all near biome borders. Neither the block-level zoom nor a copy of a neighbouring quart explains it. Bedrock's rule there is not known. The same-seed target, on the study's 32-block grid, is met.
- **Finished heights** include our carvers where Bedrock has its own, so they match a little less than the noise stage. See the seedcheck table in the task report.
- Structures are not generated, so their density term is left out.
- Monster rooms are left out, because they need spawners and this stage has no mobs.
- Dragonfly has no powder snow or budding amethyst block; plain snow and plain amethyst stand in.
- Springs stay single source blocks until something next to them changes.
- A vein's air-exposure rule only sees air inside the chunk.
- Negative seeds and seeds of 2^31 or more are untested against real Bedrock.
