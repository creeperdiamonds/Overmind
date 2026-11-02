# worldgen

Our own Overworld generator. A chunk depends only on the seed and its position, so chunks can be built in any order on any number of workers. GenerateChunk keeps nothing afterwards except pooled scratch memory, which is always rewritten before it is read.

## Where the numbers come from

The owner had the Java 26.1 generator studied (decompiled and measured with a probe) and asked for corrections from that study. No Mojang code or data file is in the repo, and the code here works only from the study's written notes and measurements. "Notes" means `NOTES-how-it-works.md`; "addendum" means its numeric section (A1 to A6). Every number is one of three kinds.

**From the notes or addendum:** octave lists and sample scales (section 2, A3); all height, factor and jaggedness knots and slopes (A1); rarity thresholds, cave thicknesses and cave formulas (section 7, A2); biome tables (section 4); the aquifer grid, candidates, similarity, flood thresholds, random levels and lava cells (section 5); the ore table (section 10); carver chances, ranges and shape ranges (section 8); the ground offset, where `ground + 0.02 - y` follows from the offset constant and gradient (section 3).

**Measured and tuned by us, against the study's figures:**

- `blobWidths`, `blobWiden`, `blobTip`: the blob shape, fitted to the block counts (section 10, A5).
- `tunnelNoiseScale`: the spread of the tunnel noises, tuned to the air shares (section 7). The study gives no spread for them.
- `wallThreshold`, `wallRoughness`, `wallFade`, `wallFullGap`: the aquifer wall, against the water and air shares.
- `baseSpread`: set so the 3D ground noise measures std 0.22.
- `densityEase`: of the curves tried, the one that passed the chunk edge test nearest the inland figure.
- `tunnelRadius`, tunnel length rolls, `canyonCore`, `canyonMiddleWidth`, and how deep the start count nests (the notes say only "nested"): the carvers. Carver outputs could not be measured (A6), so these are tuned to an own target: carvers add 0.3 to 1.5 points of air per band up to Y 100 (`TestCarversAddLittleAir`).

**Our own design or choice, with no figure behind it:**

- The carver layout: a tunnel is a formula, a line bent by sine waves, cut as a chain of ellipsoids around it, running both ways from its start. It replaces an earlier step-by-step walk.
- `wallStrength`: when a border between two aquifers becomes stone. The notes describe what it depends on, in words. It replaces an earlier pressure formula.
- The blob's spindle shape. The line's length and tilt and the swelling are from the notes; the rest replaces an earlier sphere chain.
- `finishDensity`'s tanh. The notes name a squeeze with no formula.
- `gameSpread`: the spread of each noise other than the five climate ones is a formula fitted to the five measured spreads.
- Octave limits, since finer ones fall inside a grid cell: limit noises 6 of 16, selector 4 of 8, jagged 6 of 16. The selector's sharpness of 5 (`base3D`).
- Clamps on climate values (`shapeClimate`).
- Ores are clipped to their feature's height range and rock patches are not (`placeVein`).
- Tunnel bends, tilt (`caveTilt`), `minTunnelHeight`, room height and `canyonLedge`'s step size.
- Surface band thresholds, band table run lengths, iceberg and hoodoo height curves, disk radii, geode shape, lake sizes.
- Aquifer: the 13 surface sample offsets, `surfaceFluidMargin`, `openSkyFluidDepth`, and the "near the sea" and "far above ground" margins in `computeStatus`.
- Hanging water: water with air straight under it in the noise pass gets stone instead, and tunnels never open under water. The game lets such water flow later; here it would hang until something updated it. This covers straight down only. **Water can still sit beside air sideways, for example at the edge of an aquifer, and it stays until a block update, when it will flow.**

The study reads Java only. Bedrock may differ on any of this, and that is unmeasured.

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
- `generator.go`: the pass order: noise, biomes, surface, disks, carvers, lakes, geodes, patches, ores, springs. The game does biomes before noise; ours can do them after, because density reads no stored biome.

## Rivers

There is no river noise any more; this reverses the earlier choice of a separate 1600-block noise. A river is the weirdness valley, |weirdness| under 0.05, wherever the slice table says so. Its bed comes from the height spline at PV near -1. With weirdness back at its 512 and 256 wavelengths the valley is a thin winding band, and rivers cover about 7% of the sample, close to the measured 6.8%.

## Heights

- `SurfaceHeight` matches the generated chunk exactly, including carvers and lakes, but it walks every tunnel that could reach the column (under a millisecond). Use it for a few columns.
- `NoiseHeight` skips carvers and lakes and takes tens of microseconds. The map tool and the statistics use it, and it is what the study measured.
- `highestSolid` has no caves at all.

## Known differences from the game

- Water reaches Y 63 inclusive; Java's top water block is Y 62.
- Biome borders follow the 4-block storage grid. The game adds a jittered lookup at block level; the warp already roughens the edges, and `BiomeAt` must match what is stored.
- Air at Y 100 to 160 is about 4.8% against 7.8% in the notes. That 7.8% rests on only 1,359 blocks. Recounting the study's larger probe output (`mojang-cols2.csv`, 60,000 columns, 65,497 blocks in that band) gives 4.41%, so the test asserts 4.41, and we are close to it. The same recount moves other bands a little too (for example Y 63 to 100 is 5.10, not 4.2).
- Columns under Y 63 are 46.0% over 12 seeds, measured 45%. Our first 3 seeds alone give 48.8%: groups of 3 seeds range from 42% to 50%, so the test row uses 12 seeds. Feeding the game's climate values from the probe grid into our spline gives 45.7%, so the spline itself matches.
- Inland columns under Y 63 (continentalness -0.11 or more, not river or swamp) are 15.2%. The target was about 14%, and a recount of the probe grid gives 12.8%. With no noise our figure is 13.2%, so the extra comes from 3D noise and cave mouths near sea level.
- Stepping over a grid cell edge gives more large height steps than stepping inside a cell (over 4 blocks: about 0.9% at cell edges, 0.6% inside, from blending). Chunk edges are cell edges too, so they read about the same as other cell edges and sit close to the limit in `TestNoSeamAtChunkEdges`.
- Monster rooms are left out, because they need spawners and this stage has no mobs. Snow layers, ice and plants are a later task.
- Dragonfly has no powder snow or budding amethyst block; plain snow and plain amethyst stand in. Dragonfly has `DeepWarmOcean`, but Java has none, so the hottest deep ocean is `WarmOcean`.
- Springs stay single source blocks until something next to them changes.
- A vein's air-exposure rule only sees air inside the chunk.

Worlds match across machines of the same CPU type. Go may round floating-point sums differently on ARM, so a seed could differ slightly there.
