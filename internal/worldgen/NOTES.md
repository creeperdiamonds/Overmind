# worldgen

Overmind's Overworld generator. A chunk depends only on the seed, the profile and its position, so chunks can be built in any order on any number of workers. GenerateChunk keeps nothing about a chunk afterwards. Pooled scratch holds values rewritten before they are read, and caches of preliminary surface levels, walk corner columns and quart biome columns, which are pure functions of the seed and a position.

## Whose algorithms these are

**By the owner's decision, the whole generator follows Mojang's algorithms:** seeding and random numbers, noise, climate, biome lookup, splines, density, cell interpolation, noise caves, aquifers, ore veins, surface rules, carvers, ores, trees, plants and the other features below. They were written here in our own Go, in our own structure and names, from a written study of the Java 26.1 generator (`NOTES-exact-seed.md`, `NOTES-exact-features.md`). No Mojang code or file is in this repo. Numeric tables and tree-of-rules data are our own Go with their source stated.

**Where parameters came from.** The feature, placement and surface-rule values were not read from the study's data folder, which this work was told not to open. They were written from knowledge of the game and corrected against the trace and stage fixtures. The fixtures, not the writing, are the evidence.

Bedrock's biomes, heights and noise caves match Java's on the same seed (BEDROCK-NOTES section 9), so reproducing Java reaches Bedrock for those layers.

**Golden tests** compare with fixtures from a real Java server. The fixtures are not in the repo. `OVERMIND_GOLDEN` points at the first set (`golden_test.go`), `OVERMIND_GOLDEN_FEATURES` at the stage and trace set (`golden_stage_test.go`, `golden_trace_test.go`), and `OVERMIND_GOLDEN_STEP` samples every Nth chunk. Tests skip clearly if unset and fail if the folder is empty. `tools/seedcheck` compares whole worlds with Bedrock's and exits 1 under the Java figures.

Explicit `float64()` and `float32()` conversions around products stop a platform fusing a multiply-add, which would change low bits. This has run on amd64 only. `math.Sin`, `Cos`, `Log` and `Pow` are Go's; no mismatch with Java's has been seen in the traces, but neither is specified to the last bit.

**Speed without changing a result.** Some reads are skipped where a proof says the answer cannot depend on them, all in `coreAt`, `cavesAt`, `classifyCell` and `quartColumnAt`. Surface rules work out noises only when a rule reads them.

**Ties in biome lookup.** Always a cold tree search. The game keeps a per-thread last result, which decides exact ties, and a world here must not depend on which worker asked. This is the one difference left in the surface and carvers stages (about 0.04% of quarts, 1.5e-6 of blocks): with the fixtures' stored biomes in place, both stages are block-exact on all 16384 chunks.

## How a chunk is made

1. `buildBase`: noise, surface, carvers, for a chunk and for each neighbour a feature reads. Exact against Java, as above.
2. `runSource`: each of the 9 source chunks around the target is decorated **alone** against those carvers stages, by the owner's ruling. The driver follows the study's seeds and step and index order, and writes through a change log that is rolled back after the source.
3. `keepChanges`: a source's changes that land in the target are applied in row-major source order, x then z. The same order as the fixtures, so it lands as near as a fixed order can.

Java itself has no single answer for step 3: two Java runs differ by 0.25 to 0.37% of blocks. See the task report for how far ours lands.

**What this costs.** A target reads up to 25 carvers stages, so without any kept data a chunk costs far more than the old generator. `Generator.SetReuseBases` lets a worker keep bases between chunks in its pooled scratch. They are pure functions of the seed and chunk, so this changes speed and never a result (`TestReuseBasesChangesNothing`), but it is per-chunk data kept after a chunk is made, so it is **off by default**, and the owner decides.

## Profiles

`Profile` holds what differs in data; the algorithms are the same. `ProfileBedrock` is the default: floor pattern 1, 1, .75, .5, .25, iron size 10, medium diamond size 4, infested size 8 at Y 0 to 64 with stone and deepslate only, an extra coal batch in the mountains (past Java's indexes, so Java's streams do not move), rock patches replacing what the data files say. Ores leaving tuff alone is our own tuning to counts, not a Bedrock rule. `ProfileOverjava` uses Java's values, and is what the golden tests run.

## Water

Carvers carve under water as the aquifer says, and the noise pass seals nothing, as the notes and fixtures have it. So generated water can hang over air. In the game it flows when a chunk first ticks; here it stays until a block update, as springs do. Lakes are lava only, as the study says the Overworld has.

## Files

- `random.go`: xoroshiro128++, the 48-bit generator, seed factories. `noise.go`, `noise_table.go`: octaves, stacks, normal noise, the terrain's blended noise. `climate.go`, `spline.go`, `terrain.go`.
- `caves.go`: density, cell blending, veins, `columnWalk`. `aquifer.go`. `biomes.go`, `biome_table.go`.
- `generator.go`: the pass order, `fillNoise`. `base.go`: a chunk's carvers stage and stage heightmaps. `surface_rules.go`: the rule tree, surface pass, simplex noises, temperature. `carve.go`: carvers, `SurfaceHeight`.
- `feature.go`: the driver, RNG, providers, modifiers, predicates. `feature_table.go`: the order, which biome has what, ores. `feature_ores.go`, `feature_trees.go`, `feature_misc.go`, `feature_defs.go`.
- `states.go`: our block states, flags and tags. `dfblocks.go`: states to Dragonfly blocks, `OwnBlock`. `plant_blocks.go`: `SnowLayer`, `Seagrass`. `profile.go`.

## Heights

- `SurfaceHeight` is the highest block at the carvers stage that is not air or water: the terrain before any feature. It builds that one column, cutting every tunnel that reaches it through `walkCarvers` and `carverReach` with one mask. `TestSurfaceHeightMatchesCarverStage` holds it to the chunk.
- `NoiseHeight` is the noise stage's highest solid block.

## Known gaps

- **Features not written, because the study does not write them up, and with no statistical version here:** iceberg, fossil, desert well, dripstone (cluster, large, pointed), sculk, coral (warm ocean vegetation), root systems (rooted azalea, mangrove roots), pale garden (moss patch, vegetation, flowers, creaking heart), lush cave vegetation, cave vines, spore blossom, moss and clay patches. Also not done: wildflowers, flower forest flowers, cherry and meadow flowers, dry grass, firefly bush, leaf litter patch, mushroom island vegetation, mangrove trees.
- **Inferred, not exact on every trace:** see the task report's list. Underwater magma's scan and filter are not settled.
- **Block mapping:** `OwnBlock` covers what Dragonfly lacks among placed blocks. Huge mushroom faces map to the nearest Bedrock pattern. Chests are empty, spawners have no mob, nests no bees. A state with no mapping becomes air.
- Stored biomes inside 8-block cells differ from Bedrock's at about 5% of off-grid quarts; Bedrock's rule there is not known. Dappled forest and sulfur caves are not generated, and neither are structures.
- A far read, more than one chunk past a source's 3 x 3 and outside the working set, reads as air (section 13's timing-dependent case).
- Negative seeds and seeds of 2^31 or more are untested against real Bedrock.
