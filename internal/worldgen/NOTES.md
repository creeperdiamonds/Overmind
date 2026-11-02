# worldgen

Our own Overworld generator. A chunk depends only on the seed and its position, so chunks can be built in any order on any number of workers. GenerateChunk keeps nothing afterwards except pooled scratch memory, which is always rewritten before it is read.

## Where the numbers come from

The owner had the Java 26.1 generator studied (decompiled and measured with a probe) and asked for corrections from that study. Numeric parameters in this package come from the study's written notes unless a comment says otherwise: octave lists, spline tables, biome tables, cave and aquifer rules, ore tables, carver chances. The code and structure are our own and no Mojang code or data file is in the repo. Earlier versions of this package cited minecraft.wiki and own tuning; those claims no longer apply.

Our own choices, where the notes give an idea but no number, are marked in comments:

- `pvRow`: the height tables' Y values are from the study, but their slopes were not transcribed. Ours are worked out from the table so a row never overshoots; the valley slope is doubled against the measured 45% of columns under Y 63.
- `factorTable`, `jaggedRow`: how sharply a dip is entered, and jaggedness's mid value.
- `gameSpread`: the spread of each noise other than the five climate ones is a formula we fitted to the five measured spreads.
- `base3D`: limit noises stop at 6 octaves and the jagged noise at 6, since finer ones fall inside a grid cell.
- `spaghettiTrim` and `entranceTrim`: small shifts against the measured air shares and the 5% limit on land open to the sky.
- Surface band thresholds, band table run lengths, iceberg and hoodoo height curves, disk radii, geode shape, lake sizes.
- Aquifer: the barrier pressure formula, the 13 surface sample offsets, `surfaceFluidMargin` and `openSkyFluidDepth`.
- Sealing: water with air under it in the noise pass gets stone instead. The game lets such water flow later; here it would hang until something updated it.

The study reads Java only. Bedrock may differ on any of this, and that is unmeasured.

## Files

- `noise.go`: seeded gradient noise, `layered` (two octave stacks, the second read at 1.01813 times the coordinates), and the plain `fractal` used by the 3D ground noise.
- `climate.go`: the five climate values and the domain warp. Spreads are the measured ones.
- `terrain.go`: Hermite splines, the height, factor and jaggedness tables, and the sloped density `4*quarterNeg(depth*factor) + noise`. `terrainHeight` is the top before caves.
- `caves.go`: cheese, spaghetti, entrances, pillars and noodle caves as density terms, ore veins (copper and iron) in the noise pass, and `columnWalk`, which works out a column without a chunk.
- `aquifer.go`: nearest of 12 jittered centres, flood thresholds, random levels, barrier stone, lava aquifers, the dry deep dark zone.
- `carvers.go`: worm caves, canyons and lava lakes. Every tunnel is recomputed from its source chunk's seed, up to 8 chunks away. `SurfaceHeight` is here.
- `biomes.go`: the Middle, Plateau and Shattered tables and the slice tables, plus cave biomes as nearest-box by climate and depth. Biomes are stored 4 x 4 x 4 blocks.
- `surface.go`: surface rules by water depth, badlands bands, hoodoos, icebergs, puddles, sand, clay and gravel disks.
- `ores.go`: the ore table, rock, dirt, gravel and clay patches, infested stone, geodes and springs. A blob is a chain of spheres rolled from its origin chunk.
- `strata.go`: deepslate and bedrock chances.
- `generator.go`: the pass order: noise, biomes, surface, disks, carvers, lakes, geodes, patches, ores, springs.

## Rivers

There is no river noise any more; this reverses the earlier choice of a separate 1600-block noise. A river is the weirdness valley, |weirdness| under 0.05, wherever the slice table says so. Its bed comes from the height spline at PV near -1. With weirdness back at its 512 and 256 wavelengths the valley is a thin winding band, and rivers cover about 7% of the sample, close to the measured 6.8%.

## Heights

- `SurfaceHeight` matches the generated chunk exactly, including carvers and lakes, but it walks every tunnel that could reach the column (under a millisecond). Use it for a few columns.
- `NoiseHeight` skips carvers and lakes and takes tens of microseconds. The map tool and the statistics use it, and it is what the study measured.
- `terrainHeight` has no caves at all.

## Known differences from the game

- Water reaches Y 63 inclusive; Java's top water block is Y 62.
- Biome borders follow the 4-block storage grid. The game adds a jittered lookup at block level; the warp already roughens the edges, and `BiomeAt` must match what is stored.
- Air at Y 100 to 160 is about half the measured share; the study does not show why (see `TestCaveSharesMatchMeasuredFigures`).
- Columns under Y 63 are about 50%, measured 45%.
- Monster rooms are left out, because they need spawners and this stage has no mobs. Snow layers, ice and plants are a later task.
- Dragonfly has no powder snow or budding amethyst block; plain snow and plain amethyst stand in. Dragonfly has `DeepWarmOcean`, but Java has none, so the hottest deep ocean is `WarmOcean`.
- Springs stay single source blocks until something next to them changes.
- A vein's air-exposure rule only sees air inside the chunk.

Worlds match across machines of the same CPU type. Go may round floating-point sums differently on ARM, so a seed could differ slightly there.
