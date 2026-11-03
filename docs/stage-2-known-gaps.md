# Stage 2 known gaps

Found while testing play in the generated world (`internal/app/world_test.go`). Each is a limit we are living with, with the test that shows it.

## Behaviour

- **Leaf decay follows Java's distance 6 (fixed in fix round 1 of Task 5).** The earlier note here was wrong: it said leaves decayed partly because "the rest had not been marked yet". In Dragonfly a change beside a leaf marks it for a check, and each mark is itself a block change, so the mark floods the whole canopy within a few ticks; a marked leaf with no log within reach decays on its next random tick. The real cause was the reach: Dragonfly kept leaves within 4 steps of a log, but our trees are Java's, whose leaves reach 6. So any change beside an untouched tree made its outer leaves decay, and the test's "orphans" (no log within 4) included outer leaves of uncut neighbouring trees, which nothing ever marked. The fork now keeps leaves within 6 (`leafLogReach`, Java's rule). `TestLeavesDecay` at random tick speed 200, seed 1234: with every generated leaf within 40 blocks of spawn marked and no edits, 8439 of 8442 stayed after 15 s (at 4: 7967 of 8323); then, with a tree's logs cut, at least 98% of its leaves with no log within 6 must go within 60 s (measured 499 to 500 of 509). In one run 9 were still there after 40 s, all marked; a random tick by hand removed each, so the rule does not keep them, but why the server's random ticks had not reached them yet was not found.
- **The tick distance is held two chunks under the view distance, for each player.** A tick that reads a block of a chunk that is not loaded makes Dragonfly generate it on the tick. So the tick distance is Bedrock's default 4, at most the view distance minus 2 (`Config.TickDistance`), and the Dragonfly fork holds each player's tick reach, for blocks and entities, two under that player's own view distance. Entities beyond a player's tick reach do not move, and lightning, scheduled ticks (water, fire) and neighbour updates (falling sand) wait there until a player comes near, as on Bedrock (fix round 3 of Task 4c). `TestGenerationDoesNotStallTicks` and `TestSmallViewDistanceNeverGeneratesOnTick` assert no chunk is generated on the tick.
- **Snow does not melt; there are no items for snow layers and seagrass** (see worldgen NOTES).
- **Nether and End are flat floors** and nothing sends players there.

## Seeds

- **Bedrock's handling of negative seeds and seeds of 2^31 or more is not verified.** Every seed in `testSeeds` (0, negatives, past 32 bits, both int64 limits) generates, spawns on land, is kept in `overmind-seed.txt` and gives the same world after a restart. Whether Bedrock itself gives the same terrain for those seeds is untested.
- **The world seed is not sent to clients.** Dragonfly's start packet is not given our seed (`defaultGameData` sets none), so a Bedrock client does not show it.

## Tests

- **A test process can hold about 20 servers.** Dragonfly adds its creative items and recipes to global lists each time a server is made, so after that many, a joining bot's session is dropped after a second. Production makes one server per process. The tests keep to fewer, and `TestBotSpawnsOnLand` uses the generator without a server.

## Fixed on the way

- **Spawn inside a tree, flower or snow layer.** `DefaultSpawn` read heights only; seed 100's spawn was inside an oak trunk and seed 12345's inside a snow layer. It now checks the finished chunk (`spawnClear`).

## Bedrock comparisons (measured, not asserted)

The owner chose Java's algorithms for carvers, trees and plants (ruling 23), and Bedrock's placement is hidden in its program. So these are measured against the Bedrock reference worlds by `tools/seedcheck` and listed here, and do not fail a test (ruling 28). What is asserted is that ours match Java: `TestPlacementRatesMatchJava` (trees by biome, kelp, seagrass, cave air) and the golden stage tests. Bedrock's shipped data differences stay in the Bedrock profile.

Figures from `seedcheck` after fix round 1 of Task 4c (4 seeds, 1120 x 1120 blocks each, all 4900 chunks of each window, default profile).

**Finished height against Bedrock (ruling 30).** Our carvers are Java's, and Bedrock's carver positions are hidden, so the two worlds' carver holes never coincide. A grid point counts as a hole in a world if that world's finished top is more than 2 under our noise height there. Bedrock's holes are read from heights alone, so a terrain difference that leaves Bedrock lower counts as a hole too. Away from holes the gate is asserted; the total is measured.

| Seed | Holes ours / Bedrock / both | Points left out (of 1225) | Within 2 away from holes (gate: noise) | Within 2, all points |
|---|---|---|---|---|
| 100 | 7 / 30 / 0 | 37 | 99.92% (97.39) | 96.98% |
| 1 | 30 / 9 / 0 | 39 | 100.00% (99.27) | 96.82% |
| 12345 | 14 / 26 / 0 | 40 | 99.92% (97.80) | 96.65% |
| 987654321 | 5 / 8 / 0 | 13 | 100.00% (99.35) | 98.94% |

Task 4b scored 99.02% (seed 1) and 97.55% (seed 12345) over all points because its own carvers did not carve under water. Java's do: seed 1 is mostly sea, and 30 of its grid points are Java holes in the sea floor.

**Cave air**, % of blocks under the ground top minus 8, ours / Bedrock pooled over its 4 seeds. Ours matches Java within 0.1 point in every band (`TestPlacementRatesMatchJava`).

| Y band | 100 | 1 | 12345 | 987654321 | Bedrock |
|---|---|---|---|---|---|
| -54 to -40 | 6.39 | 6.22 | 5.19 | 4.64 | 6.06 |
| -40 to -20 | 10.27 | 9.01 | 9.04 | 8.61 | 9.21 |
| -20 to 0 | 12.28 | 10.50 | 11.01 | 10.99 | 9.81 |
| 0 to 20 | 11.98 | 3.71 | 8.26 | 10.08 | 7.46 |
| 20 to 40 | 10.09 | 3.01 | 6.89 | 10.50 | 7.21 |
| 40 to 63 | 8.00 | 4.49 | 6.72 | 5.48 | 5.96 |
| 63 to 100 | 8.15 | 4.06 | 8.57 | 6.73 | 7.52 |
| 100 to 160 | 6.84 | none | 6.24 | 2.47 | 5.11 |

Java's carvers give more air than Bedrock's at Y -20 to 0 on every seed (10.5 to 12.3 against 9.81).

**Trees per chunk**, by the chunk's centre biome, ours / Bedrock (with our chunk count where it is under 100), after fix round 2 of Task 4c. Bedrock's figures are over all 4 windows. All windows: ours 1.40 / 0.68 / 1.80 / 3.50 by seed, Bedrock 2.217 pooled.

- Close: birch forest 4.08 to 5.20 / 5.04; flower forest 3.97 to 4.09 / 4.06; mega taiga 4.07 / 4.72; badlands plateau 2.47 / 2.57 (49); grove 3.73 / 4.49.
- Ours above: jungle 10.53 / 10.03 (49), jungle edge 2.02 / 1.43, old growth birch forest 4.18 to 5.67 / 3.64, cherry grove 3.17 / 2.41 (29).
- Ours below: forest 4.27 to 5.21 / 5.92; roofed forest 5.00 to 5.56 / 7.44; savanna 0.66 / 1.02; savanna plateau 0.72 / 1.10; meadow 0.11 to 0.43 / 0.95; plains 0.12 to 0.20 / 0.31.
- Pale garden 0.49 / 6.50: pale oak trees are a Task 4d gap.
- Grove was 0.21 before round 2. It was a bug: Java's grove trees grow on snow (`spruce_on_snow`, `pine_on_snow`), and ours used the sapling survival test, which fails on snow. Java's fixtures hold 14 grove trunks where ours had 0; now the one grove chunk there matches (2.00 / 2.00), and `TestGroveHasTrees` holds it.

**Against Java**, trees are asserted by biome (`TestPlacementRatesMatchJava`, with the fixtures), and all pass after round 2: jungle 9.59 / 9.73, roofed forest 4.58 / 4.64, bamboo jungle 2.88 / 2.75, forest 5.03 / 5.16. Each source now runs on top of what its earlier neighbours do (two levels), as Java's later chunk sees the earlier one's canopy. So where ours differ from Bedrock, it is Java's placement against Bedrock's.

**Kelp and seagrass per ocean chunk**: ours 108.0 to 109.6 kelp and 47.5 to 55.8 seagrass, against Bedrock's seed 1 figures of 189 and 57. Against Java on the same chunks: kelp 111.3 / 112.9, seagrass 44.6 / 45.5. So Bedrock places about 1.7 times Java's kelp.

## Commit e1873aa's message

Commit e1873aa ("ores on the feature driver follow Mojang's algorithm") says every ore feature replays exactly. At that commit the emerald ore did not yet: it was fixed in the next commits (48b5e1f, 84c767f), and `TestGoldenFeatureTraces` has held every ore run exact since (6485 of 6485). History is not rewritten; this note corrects the record.
