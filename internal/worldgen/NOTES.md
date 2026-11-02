# worldgen

Our own Overworld generator. A chunk depends only on the seed and its position, so chunks can be built in any order on any number of workers. GenerateChunk keeps nothing afterwards except pooled scratch memory, which is always rewritten before it is read.

## Where the numbers come from

The owner had the Java 26.1 generator studied (decompiled and measured with a probe) and asked for corrections from that study. No Mojang code or data file is in the repo, and the code here works only from the study's written notes and measurements. "Notes" means `NOTES-how-it-works.md`; "addendum" means its numeric section (A1 to A6). Every number is one of three kinds.

**From the notes or addendum:** octave lists and sample scales (section 2, A3); all height, factor and jaggedness knots and slopes (A1); rarity thresholds, cave thicknesses and cave formulas (section 7, A2); biome tables (section 4); the aquifer grid, candidates, similarity, flood thresholds, random levels and lava cells (section 5); the ore table (section 10); carver chances, ranges and shape ranges (section 8); the ground offset, where `ground + 0.02 - y` follows from the offset constant and gradient (section 3).

**Measured and tuned by us, against the study's figures:**

- `blobWidths`, `blobWiden`, `blobTip`: the blob shape, fitted to the block counts (section 10, A5).
- `tunnelNoiseScale` (1.16): the spread of the tunnel noises on top of `gameSpread`, tuned to the air shares (section 7). The study gives no spread for them.
- `warpSpread` (0.284): the domain-warp noises' spread, from `gameSpread`: their natural spread times the ratio for three octaves. Temperature and weirdness, the two three-octave climate noises, measure 0.652 and 0.648 against the table's 0.65. An earlier tuned 0.312 was only there to pass a chunk edge test that compared unlike things.
- `spreadRatios`: the share of its natural spread that a noise keeps, by its number of octaves. The points for 2, 3, 5 and 9 octaves are sampled from our own noises against the five measured climate spreads (`TestSpreadRatios`); the point for one octave (0.83) has no measurement and is tuned to the air shares.
- `startScale` (3.6), `caveHeightBias` (0.5): the number of starts in a cave system (an exponential draw capped at the notes' 15), and how far their heights lean to the top of the notes' range. Tuned to Bedrock's air by band with carvers. Tried at bias and scale: 1 and 2.2 (Y 40 to 63 too low), 0.75 and 2.9, 0.6 and 3.6 (Y -20 to 0 too high), 0.5 and 4.4 (Y -20 to 0 too high).
- `wallThreshold`, `wallRoughness`, `wallFade`, `wallFullGap`: the aquifer wall, against the water and air shares.
- `baseSpread`: set so the 3D ground noise measures std 0.22.
- `densityEase`: of the curves tried, the one that passed the chunk edge test nearest the inland figure.
- `tunnelRadius`, tunnel length rolls, `canyonCore`, `canyonMiddleWidth`: the carvers' shape. Tuned earlier to an own bound, when no carver figure existed, and left as they were; the Bedrock air figures are now met by `startScale` and `caveHeightBias`.

**Our own design or choice, with no figure behind it:**
- Canyon thickness: even over the notes' 0 to 6 (the notes say a trapezoid with a plateau of 2).

- The carver layout: a tunnel is a formula, a line bent by sine waves, cut as a chain of ellipsoids around it, running both ways from its start. It replaces an earlier step-by-step walk.
- `wallStrength`: when a border between two aquifers becomes stone. The notes describe what it depends on, in words. It replaces an earlier pressure formula.
- The blob's spindle shape. The line's length and tilt and the swelling are from the notes; the rest replaces an earlier sphere chain.
- `finishDensity`'s tanh. The notes name a squeeze with no formula.
- Octave limits, since finer ones fall inside a grid cell: limit noises 6 of 16, selector 4 of 8, jagged 6 of 16. The selector's sharpness of 5 (`base3D`).
- Clamps on temperature, humidity, continentalness and erosion (`Climate`, `shapeClimate`): past them nothing reads a different value. Weirdness has none, because the game's passes 1 on 0.25 to 0.42% of columns and reaches 1.33 (ours reaches 1.49).
- Ores are clipped to their feature's height range and rock patches are not (`placeVein`).
- Tunnel bends, tilt (`caveTilt`), `minTunnelHeight`, room height and `canyonLedge`'s step size.
- Surface band thresholds, band table run lengths, iceberg and hoodoo height curves, disk radii, geode shape, lake sizes.
- Aquifer: the 13 surface sample offsets, `surfaceFluidMargin`, `openSkyFluidDepth`, and the "near the sea" and "far above ground" margins in `computeStatus`.
- Hanging water: water with air straight under it in the noise pass gets stone instead, and tunnels never open under water. The game lets such water flow later; here it would hang until something updated it. This covers straight down only. **Water can still sit beside air sideways, for example at the edge of an aquifer, and it stays until a block update, when it will flow.**

**Bedrock.** The server follows Bedrock where the editions differ. BDS 1.26.60.6 was measured on 4 seeds of 1120 square (`BEDROCK-NOTES.md` section 9, `aggregate.json`). Terrain, biomes and noise caves match Java on the same seed, so the Java figures still stand for shape. What Bedrock changes:

- **Measured:** the bedrock floor, 1, 1, .75, .5, .25, then none from Y -59.
- **From its data files (section 5):** medium diamond size 4, iron upper and middle size 10, infested stone size 8 at Y 0 to 64, an extra coal batch in the mountains tag (20 attempts, Y 128 to 256; the notes say the tag leaves out the windswept biomes, the rest is our inference from the emerald set). Its temperatures for savanna, savanna plateau, meadow and cherry grove are already what Dragonfly's biomes return.
- **Rock patches, decided by measurement.** Section 5 reads granite, diorite and andesite as replacing stone only, and tuff and gravel as stone and deepslate, medium confidence, against GAPS 18's "any stone kind and tuff". We follow section 5. With it, deepslate's share at Y 1 to 7 comes to within 0.01 of Bedrock at every height (0.896 against 0.902 at Y 1, 0.439 against 0.446 at Y 5); with GAPS 18 it was up to 0.07 under. Dirt was not read; stone only is our choice.
- **Ores leave tuff alone: our own, tuned to counts, not a Bedrock rule.** The notes did not read the ores' replacement rules, and Java does replace tuff. With tuff replaced, ores lying mostly under Y 0 came out 7 to 14% over Bedrock's counts, about tuff's share of the rock there. Without it they are within 6%; diamond is still the highest, 17.9 against 16.9 (+6%). If a later reading of the rules disagrees, the rule wins and blob sizes need another look.
- **Ores above Y 64 are compared by density.** Raw counts there (coal Y 128 to 192: 1.0 against 6.43) follow how much high land the windows hold. Per 1000 blocks under the ground, on Bedrock's height profile, they match (`TestOreDensityMatchesBedrock`).
- **`ironUpperBottom` 76 is tuned, the files say 80.** With 80, iron was 2.25 against 2.98 per 1000 at Y 96 to 128, with coal and copper there about right. The cause is unknown.
- **Infested stone: `infestedAttempts` 4.4 is tuned, the files say 14.** 14 gave 65.8 blocks a mountain chunk against Bedrock's 20.6. The cause is unknown.
- **Carvers now have real references.** `caveHeightBias` (0.5) and `startScale` (3.6) are tuned to Bedrock's air by band (`TestCaveAirMatchesBedrock`).

Not added, because the notes give no rule to place them by, only that they occur: dappled forest (one patch on one of 4 seeds, 0.32% of the pooled windows, in place of plains) and sulfur caves (seen, share not measured). Dragonfly has both biomes. Bedrock's steep-slope rule (section 5 row 7) is also not followed: the notes say to check it against a world first.

## Files

- `noise.go`: seeded gradient noise, `layered` (two octave stacks, the second read at 1.01813 times the coordinates), and the plain `fractal` used by the 3D ground noise.
- `climate.go`: the five climate values and the domain warp. Spreads are the measured ones.
- `terrain.go`: Hermite splines, the height, factor and jaggedness tables, and the sloped density `4*quarterNeg(depth*factor) + noise`. `highestSolid` is the top before caves.
- `caves.go`: cheese, spaghetti, entrances, pillars and noodle caves as density terms, ore veins (copper and iron) in the noise pass, and `columnWalk`, which works out a column without a chunk.
- `aquifer.go`: 12 jittered candidate centres of which the nearest 4 count (the nearest decides the fluid; the other 3 can raise a wall against it), flood thresholds, random levels, stone walls, lava aquifers, the dry deep dark zone.
- `carvers.go`: worm caves, canyons and lava lakes. Every tunnel is recomputed from its source chunk's seed, up to 8 chunks away, and its size constants are checked at compile time to fit that reach. `SurfaceHeight` is here.
- `biomes.go`: the Middle, Plateau and Shattered tables and the slice tables, plus cave biomes as nearest-box by climate and depth. Biomes are stored 4 x 4 x 4 blocks.
- `surface.go`: surface rules by water depth, badlands bands, hoodoos, icebergs, puddles, sand, clay and gravel disks.
- `ores.go`: the ore table, rock, dirt, gravel and clay patches, infested stone, geodes and springs. A blob is a spindle rolled from its origin chunk.
- `strata.go`: deepslate and bedrock chances.
- `decorate.go`: trees, ice, snow and plants. It reaches other layers only through `surfaceDefAt` (BiomeAt) and `groundSource`, so it survives a rewrite of noise, climate, terrain and noise caves. `treeSoil` also reads the surface rules.
- `generator.go`: the pass order: noise, biomes, surface, disks, carvers, lakes, geodes, patches, ores, springs, decoration. The game does biomes before noise; ours can do them after, because density reads no stored biome.

## Decoration

A tree is rolled from its origin chunk and written where it falls in the chunk being made, with no look at a real neighbour. Inside the chunk it reads the chunk's data. For a neighbouring column it uses `chunkGround`: tunnels are cut once for the chunk plus 6 columns around it, so a column costs about 46 microseconds against `SurfaceHeight`'s 220, and equals it exactly (`TestChunkGroundMatchesSurfaceHeight`). Only trees that can reach the chunk are asked about.

**Measured:** tree rates per biome and kelp and seagrass per ocean chunk are tuned to Bedrock's (the kelp and seagrass figures rest on one seed). **Own, labelled:** every tree shape; which tree kinds each biome uses; rates of snowy taiga, windswept forest and hills, windswept savanna, bamboo jungle, swamp, mangrove and snowy plains (no Bedrock figure); all land plant rates; the cold threshold 0.125 and height drop (Y 90, 1/500 a block), because the notes give the idea and no numbers; deep frozen ocean counting as cold though Dragonfly reads 0.5.

**Our own block types, `plant_blocks.go`:** `SnowLayer` and `Seagrass`. Dragonfly lists their states but has no Go type, which would leave them solid, unbreakable and irreplaceable. Ours can be mined and replaced, and registered once before the registry is finalised. Hardness and drops are our own. Snow does not melt, and there are no items for them.

**Open gap, soil depth:** dirt in the top 4 blocks of the ground is 1.76 a land column against about 2.3 on Bedrock seed 100, the only raw world here. Dirt patches under that match (15.7 against 15.95 per 1000). The old total of 486 against 700 a chunk is the soil plus land share, on one world's split; the soil figure is logged, not asserted, because one seed cannot set it and surface depth follows the surface noise being rewritten.

**Plants seen in Bedrock worlds with no Go type in Dragonfly, not placed:** bush, firefly bush, wildflowers, short and tall dry grass, cactus flower, azalea and flowering azalea, red and brown mushroom, sweet berry bush, cave vines, pale hanging moss, pale moss carpet, plus moss block, pale moss block, powder snow and glow lichen. Also lacking: leaf litter, mangrove propagule.

**Known gaps:** no 2 x 2 giant spruce or giant jungle trees, no vines, bamboo, mushrooms, melons, pumpkins, azaleas or moss, though Dragonfly has some of them. No trees in rivers (Bedrock 0.11 a chunk). A column's snow and biome follow the 4-block grid. Cane and cactus keep off the outermost columns, so none grows on chunk edges.

## Rivers

There is no river noise any more; this reverses the earlier choice of a separate 1600-block noise. A river is the weirdness valley, |weirdness| under 0.05, wherever the slice table says so. Its bed comes from the height spline at PV near -1. With weirdness back at its 512 and 256 wavelengths the valley is a thin winding band, and rivers cover about 7% of the sample, close to the measured 6.8%.

## Heights

- `SurfaceHeight` matches the generated chunk exactly, including carvers and lakes, but it walks every tunnel that could reach the column (under a millisecond). Use it for a few columns.
- `NoiseHeight` skips carvers and lakes and takes tens of microseconds. The map tool and the statistics use it, and it is what the study measured.
- `highestSolid` has no caves at all.

## Known differences from the game

- Water reaches Y 63 inclusive; Java's top water block is Y 62.
- Biome borders follow the 4-block storage grid. The game adds a jittered lookup at block level; the warp already roughens the edges, and `BiomeAt` must match what is stored.
- Cave-share figures: every band in `TestCaveSharesMatchMeasuredFigures` is the recount of the study's `mojang-cols2.csv` (60,000 columns, all biomes, blocks from the band's bottom to 8 under the column's top solid block, which is the test's own filter), not the notes' section 7 table. The notes' Y 100 to 160 figure of 7.8% rested on 1,359 blocks; the recount has 65,497 there (4.41%). Ours there is about 5.0. Worst band against the recount: Y 0 to 20, air 7.1 against 5.76 (1.3 over, tolerance 1.5); Y -54 to -40 and Y -20 to 0 are about 0.9 over.
- Caves open to the sky (noise caves only): 3.88% pooled over 12 seeds against 4.7 (limit 1 point, so 3.7); the seeds range from 1.8% to 6.5%.
- Columns under Y 63 are 45.9% over 12 seeds, measured 45%. Our first 3 seeds alone give 48.8%: groups of 3 seeds range from 42% to 50%, so the test row uses 12 seeds. Feeding the game's climate values from the probe grid into our spline gives 45.7%, so the spline itself matches.
- **Open gap:** inland columns under Y 63 (continentalness -0.11 or more, not river or swamp) are 15.05% (15.2% before the spread change), against 14% in the brief and 12.8% in a recount of the probe grid. The test passes at 14 give or take 2, but the figure is not met. With no noise our figure is 13.2%, so the extra comes from 3D noise and cave mouths near sea level.
- Stepping over a grid cell edge gives more large height steps than stepping inside a cell, from blending. Chunk edges are cell edges too, so `TestNoSeamAtChunkEdges` compares them with the other cell edges in the same chunks (columns 3/4, 7/8, 11/12), by per-chunk difference with a margin of 3 standard errors. Over 504 chunks of 3 seeds, the excess is under one standard error at both spreads tried; a 3-block ground shift between chunks fails it on every seed.
- Monster rooms are left out, because they need spawners and this stage has no mobs.
- Dragonfly has no powder snow or budding amethyst block; plain snow and plain amethyst stand in. Dragonfly has `DeepWarmOcean`, but Java has none, so the hottest deep ocean is `WarmOcean`.
- Springs stay single source blocks until something next to them changes.
- A vein's air-exposure rule only sees air inside the chunk.

## How one seed compares

The same seed gives a different world from the game's by design, and single seeds differ a lot. Climate-zone differences are not distinguishable from noise with 4 Java seeds, with one open weak signal (coldest-band region size 8.1% vs 14.7). Pooled over 60 of our seeds against the 4 Java seeds the probes cover (8192 square, 32-block grid): land 47.3% against 47.4%, land p99 149 against 148, p99.9 184 against 181, Y 150 or more 0.47% against 0.45%. Each Java figure sits inside the 5 to 95% range of our groups of 4 seeds. Our seed 100 has the second lowest temperature spread of the 61 (0.23; range 0.23 to 0.48), so it lacks hot and cold regions, and its height tail is also on the high side of ours.

- **Open:** cold regions may be a little less coherent than the game's. The area-weighted size of the coldest band's regions is 8.1% of the map against 14.7, which only 0.6% of our groups of 4 reach; pooling cold and hot it is 6.6 against 9.4 (7%). This is one of about 20 figures compared and rests on 4 Java seeds, so nothing is changed for it. More Java seeds would settle it.
- Rings around climate extremes are in the game too. Nested band rings around weirdness extremes: 403 per seed against 389 (560 while weirdness was clamped at 1, where plateaus counted as extremes); around humidity extremes 16.5 against 13.8.

Worlds match across machines of the same CPU type. Go may round floating-point sums differently on ARM, so a seed could differ slightly there.
