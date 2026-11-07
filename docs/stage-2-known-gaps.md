# Stage 2 known gaps

Found while testing play in the generated world (`internal/app/world_test.go`). Each is a limit we are living with, with the test that shows it.

## Behaviour

- **Leaf decay follows Java's distance 6 (fixed in fix round 1 of Task 5).** The earlier note here was wrong: it said leaves decayed partly because "the rest had not been marked yet". In Dragonfly a change beside a leaf marks it for a check, and each mark is itself a block change, so the mark floods the whole canopy within a few ticks; a marked leaf with no log within reach decays on its next random tick. The real cause was the reach: Dragonfly kept leaves within 4 steps of a log, but our trees are Java's, whose leaves reach 6. So any change beside an untouched tree made its outer leaves decay, and the test's "orphans" (no log within 4) included outer leaves of uncut neighbouring trees, which nothing ever marked. The fork now keeps leaves within 6 (`leafLogReach`, Java's rule). `TestLeavesDecay` at random tick speed 200, seed 1234: with every generated leaf within 40 blocks of spawn marked and no edits, 8439 of 8442 stayed after 15 s (at 4: 7967 of 8323); then, with a tree's logs cut, at least 98% of its leaves with no log within 6 must go within 60 s (measured 499 to 500 of 509). In one run 9 were still there after 40 s, all marked; a random tick by hand removed each, so the rule does not keep them, but why the server's random ticks had not reached them yet was not found.
- **The tick distance is held two chunks under the view distance, for each player.** A tick that reads a block of a chunk that is not loaded makes Dragonfly generate it on the tick. So the tick distance is Bedrock's default 4, at most the view distance minus 2 (`Config.TickDistance`), and the Dragonfly fork holds each player's tick reach, for blocks and entities, two under that player's own view distance. Entities beyond a player's tick reach do not move, and lightning, scheduled ticks (water, fire) and neighbour updates (falling sand) wait there until a player comes near, as on Bedrock (fix round 3 of Task 4c). `TestGenerationDoesNotStallTicks` and `TestSmallViewDistanceNeverGeneratesOnTick` assert no chunk is generated on the tick.
- **A geode can open beside underground water, as in Java.** Seed 17, chunk -8, 7: geode air at Y 7 sits under aquifer water at Y 8 (amethyst, calcite and basalt around it). Java's geode does the same: it checks only its 3 or 4 centre points for water, lava, ice or bedrock, and its blocks replace water (study notes, geode). This, not 4b's sealing, is why `TestOceanFloorHolds` could not stand; it was deleted in 4c with the reason given as 4b's sealing alone. Java schedules a fluid tick there so the water flows in; ours stays until a block update reaches it.
- **Snow does not melt; there are no items for snow layers and seagrass** (see worldgen NOTES).
- **The Nether and End are generated (Tasks 10 and 11); the Nether can be reached, the End not yet.** Dragonfly lights an obsidian frame with flint and steel (or fire) into a Nether portal, and its travel takes a player to the generated Nether. The End is generated (terrain, outer islands, spikes, chorus, natural gateways, the inactive exit portal) but needs an End portal to reach. Strongholds now generate their portal room (frames with the eyes they drew; the portal block only when all 12 have one) and Dragonfly has eye insertion and an End portal block that leads to the End, but filling a generated frame and travelling through it has not been tested. There is no dragon fight: no dragon, no end crystals on the spikes, no gateways from kills. The exit portal (inactive) and the arrival platform under the End's spawn are placed with their chunks, where Bedrock builds them when a player arrives; the platform is Java's 5 x 5 (Bedrock's not read). `TestDayCycleAdvances` lights a frame and checks the Overworld's portal destinations.

- **Villages: mostly BDS's now (ruling 42).** Built with their own depth-first placer as the study read it (P11.3, `internal/structures/village.go`). Measured: seed 22's village window, blocks in our piece boxes 96.2% same against 78.7% without villages (`TestVillageWindowMatchesBDS`); 5 of 5 BDS village chests at BDS's positions with BDS's loot seed (`TestVillageChestsMatchBDS`, P18.7: elements whose template file is missing keep their pool slots). Trees and piles of the decor pools and the villagers and golems are not placed.

- **Some structure blocks are static (Task 9).** Placed with their states and block entities, without behaviour yet: dispensers (no firing, contents not openable; their loot table and seed are kept), tripwire and tripwire hooks, sticky pistons, repeaters (Dragonfly states with no Go type), suspicious sand and gravel (no brushing; loot table and seed kept), flower pots (their plant is kept; not placeable by players), mob spawners (their mob is kept; they spawn nothing).
- **Structure mobs are recorded, not spawned (Task 9 round 10; mobs are stage 4).** Ocean monuments' three elder guardians (penthouse and the two wing rooms), and the piglins and hoglins of bastion mob pieces (`bastion/mobs/*`), and end cities' shulkers (Sentry markers) are kept as spawn intents in the chunk writer (`spawnIntent`); no entity appears. End city ships carry their elytra in an item frame. Fortress spawners hold `minecraft:blaze` and bastions their chests, as in BDS (fortress seed 100: 2/2 chests and 2/2 spawners; bastions seed 100: 21/21 chests with BDS's loot seed).
- **Bastion block rot is not applied (Task 9 round 10).** Bastion elements name block-rule lists (`bastion_generic_degradation`-style lists: cracked bricks, magma, gilded blackstone), but the study did not dump their contents. Seed 100's two hoglin stables score 96.6% and 96.5% of the cells in their piece boxes; most of the rest is cracked polished blackstone bricks, magma and gilded blackstone BDS put where the template has the plain block. The layout is right: 48 of 49 and 49 of 50 pieces with 50 cells or more score 0.8 or more on air / not air (the study's 49/50; the one below is the mostly-air start). Only hoglin stables were checked; the bridge, treasure and housing-units starts use the same placer and pools, untested.
- **Structures go in before decoration (fixed in Task 9 round 11, NOTES-structures-phase2 P21.1).** BDS writes a chunk's structures into its terrain before any decoration reaches the chunk; the Bedrock profile now does the same (`worldgen/structures_base.go`), so trees, fungi and plants grow over and into structures as in BDS (seed 100's fortress 99.58% -> 99.84% of its cells, seed 31's jungle temple 81.1% -> 98.2%). Overjava keeps the old order.
- **Ocean ruins stand where BDS has them, with other layouts (Task 9 round 11).** The study read the ruins only in part; with what was read, the start's draws do not reproduce BDS's ruins (0 of the 20 measured ruin chests at BDS's position, `TestOceanRuinsMatchBDS`). As villages under ruling 42, a ruin where BDS has one is kept; the unread parts are labelled in `oceanruin.go`. Their suspicious sand and gravel are not made.
- **Barrels, decorated pots and dispensers of trial chambers and camps keep their loot table and seed in the block entity, but only chests roll it** (the fork's loot filler is the chest's). Mansion decorative chests are empty: their template items use legacy item ids, not converted. Mansion evokers, vindicators and allays are spawn intents (mobs are stage 4).
- **Woodland mansions have no entrance staircase (clean-room rewrite, 2026-11-07).** BDS places two runs of `stone_stairs` from the start position after the cobblestone foundation; their cells were not decoded, so `cr_mansion.go` places none (17 `stone_stairs<-air` cells under seed 987654321's (105,-292) mansion, `TestMansionMatchesBDS` "below the mansion"). The foundation stops at y 2 and the start height is clamped to the sea (62 + 1), both as BDS.
- **Ancient city sculk feature elements use our own simple spreader** (BDS's is not stable between runs); their start test, catalyst and shriekers follow the study (T4). Nether ruined portals are not placed (their Nether placement was not read).
- **Trial chambers are placed but not exact (Task 9).** 6 of BDS's 13 merged volumes exact, the others a few blocks off in y or one side (NOTES-loot-trial-bastion T1's open misses at depth 5). Their encapsulate terrain adaptation, ignore_waterlogging and the processors are not applied; blocks Dragonfly lacks (trial spawners, vaults) are left out; every placement candidate is kept (the biome filter is not read).
- **Mineshaft chest minecarts are not spawned (Task 9).** New chunks carry no entities yet, so the cart's rail is placed and its loot table and seed are worked out (P14.1) but no minecart appears. Mineshaft cave spider spawners match BDS's count but not always its position along the corridor (the chunk order behind the "placed" mark is fitted: 1 of 4 and 3 of 7 at BDS's position). Strongholds now get the legacy terrain "bury" (P17.4) and mesa mineshafts Java 1.18's mesa height (P17.3); neither is measured on a BDS world.

## Fitted values (ruling 41)

Values the study fitted to BDS worlds instead of reading them from the code. Each stays, labelled in the code with its match, until the code is read; removing one would cost accuracy.
- The Nether's ore sizes (`nether_deco.go`): gold 10 and quartz 14 (99.2% to 99.8% recall and precision on seed 100's 1024 chunks), magma 28 (99.5%), gravel and blackstone 33 (99.8% to 100%).
- The Nether's soul fire block: soul soil only (253 of 253 BDS soul fires, one extra), where the study's round-2 text says both soul blocks.
- The Nether mushrooms' ground list (95% / 96%) and the solid test of the deltas, basalt columns and huge fungi (the study's delta box 99.82%).
- Earlier rounds' measured values (the dirt rule, the iron-bar and chorus states) match N of N and stay under ruling 39.

## Seeds

- **Bedrock's handling of negative seeds and seeds of 2^31 or more is not verified.** Every seed in `testSeeds` (0, negatives, past 32 bits, both int64 limits) generates, spawns on land, is kept in `overmind-seed.txt` and gives the same world after a restart. Whether Bedrock itself gives the same terrain for those seeds is untested.
- **The world seed is not sent to clients.** Dragonfly's start packet is not given our seed (`defaultGameData` sets none), so a Bedrock client does not show it.

## Tests

- **A test process can hold about 20 servers.** Dragonfly adds its creative items and recipes to global lists each time a server is made, so after that many, a joining bot's session is dropped after a second. Production makes one server per process. The tests keep to fewer, and `TestDefaultSpawnOnLandForEverySeed` uses the generator without a server. `testserver` refuses a server past its cap of 19 with this reason.

## Fixed on the way

- **Spawn inside a tree, flower or snow layer.** `DefaultSpawn` read heights only; seed 100's spawn was inside an oak trunk and seed 12345's inside a snow layer. It now checks the finished chunk (`spawnClear`).

## Bedrock profile, until Task 4e

The Bedrock profile runs Java's feature algorithms with Bedrock's file values where they differ. Task 4e replaces its placement with Bedrock's own, from the BDS study. Until then:

- **Underwater magma moved in the Bedrock profile too.** Task 4d (763e823) places underwater magma as Java does: Y from the bottom to 256, at least 2 under `OCEAN_FLOOR_WG`, so in flooded caves as well as on the sea floor. The Bedrock profile shares that placement and keeps only Bedrock's attempts (18 to 30, BEDROCK-NOTES row 9), so its magma moved into flooded caves as well. Where Bedrock itself puts magma is not known.
- **Bedrock's extra mangrove rate is not applied.** BEDROCK-NOTES row 9 gives Bedrock's mangrove trees as "25 + 2 + 2" a chunk; both profiles use Java's 25 (`trees_mangrove`).

## Bedrock comparisons (measured, not asserted)

The owner chose Java's algorithms for carvers, trees and plants (ruling 23), and Bedrock's placement is hidden in its program. So these are measured against the Bedrock reference worlds by `tools/seedcheck` and listed here, and do not fail a test (ruling 28). What is asserted is that ours match Java: `TestPlacementRatesMatchJava` (trees by biome, kelp, seagrass, cave air) and the golden stage tests. Bedrock's shipped data differences stay in the Bedrock profile.

Figures from `seedcheck` after fix round 2 of Task 4c, re-measured in fix round 3 (4 seeds, 1120 x 1120 blocks each, all 4900 chunks of each window, default profile). seedcheck exits 0.

**Finished height against Bedrock (ruling 30).** Our carvers are Java's, and Bedrock's carver positions are hidden, so the two worlds' carver holes never coincide. A grid point counts as a hole in a world if that world's finished top is more than 2 under our noise height there. Bedrock's holes are read from heights alone, so a terrain difference that leaves Bedrock lower counts as a hole too. Away from holes the gate is asserted; the total is measured.

| Seed | Holes ours / Bedrock / both | Points left out (of 1225) | Within 2 away from holes (gate: noise) | Within 2, all points |
|---|---|---|---|---|
| 100 | 7 / 30 / 0 | 37 | 99.92% (97.39) | 96.98% |
| 1 | 30 / 9 / 0 | 39 | 100.00% (99.27) | 96.82% |
| 12345 | 14 / 26 / 0 | 40 | 99.92% (97.80) | 96.65% |
| 987654321 | 5 / 8 / 0 | 13 | 100.00% (99.35) | 98.94% |

Task 4b scored 99.02% (seed 1) and 97.55% (seed 12345) over all points because its own carvers did not carve under water. Java's do: seed 1 is mostly sea, and 30 of its grid points are Java holes in the sea floor.

**Cave air**, % of blocks under the ground top minus 8, ours / Bedrock pooled over its 4 seeds. Against Java, `TestPlacementRatesMatchJava` asserts every band within 0.2 points; measured, the largest gap is 0.10 (Y -20 to 0: 10.03 against 9.93), the rest 0.07 or less.

| Y band | 100 | 1 | 12345 | 987654321 | Bedrock |
|---|---|---|---|---|---|
| -54 to -40 | 6.39 | 6.22 | 5.19 | 4.64 | 6.06 |
| -40 to -20 | 10.27 | 9.01 | 9.04 | 8.61 | 9.21 |
| -20 to 0 | 12.28 | 10.50 | 11.01 | 10.99 | 9.81 |
| 0 to 20 | 11.99 | 3.71 | 8.26 | 10.08 | 7.46 |
| 20 to 40 | 10.09 | 3.01 | 6.89 | 10.50 | 7.21 |
| 40 to 63 | 8.00 | 4.49 | 6.72 | 5.48 | 5.96 |
| 63 to 100 | 8.14 | 4.06 | 8.57 | 6.73 | 7.52 |
| 100 to 160 | 6.84 | none | 6.24 | 2.47 | 5.11 |

Java's carvers give more air than Bedrock's at Y -20 to 0 on every seed (10.5 to 12.3 against 9.81).

**Trees per chunk**, by the chunk's centre biome, ours / Bedrock (with our chunk count where it is under 100), after fix round 2 of Task 4c (unchanged in round 3). Bedrock's figures are over all 4 windows. All windows: ours 1.40 / 0.68 / 1.80 / 3.50 by seed, 1.85 pooled, against Bedrock's 2.217: **ours are about 17% below Bedrock.** The biomes below (forest, roofed forest, savanna, meadow, plains) outweigh those above. Against Java the same placement matches by biome, so this is Java's placement against Bedrock's.

- Close: birch forest 4.08 to 5.20 / 5.04; flower forest 3.97 to 4.09 / 4.06; mega taiga 4.07 / 4.72; badlands plateau 2.47 / 2.57 (49); grove 3.73 / 4.49.
- Ours above: jungle 10.53 / 10.03 (49), jungle edge 2.02 / 1.43, old growth birch forest 4.18 to 5.67 / 3.64, cherry grove 3.17 / 2.41 (29).
- Ours below: forest 4.27 to 5.21 / 5.92; roofed forest 5.00 to 5.56 / 7.44; savanna 0.66 / 1.02; savanna plateau 0.72 / 1.10; meadow 0.11 to 0.43 / 0.95; plains 0.12 to 0.20 / 0.31.
- Pale garden 4.51 / 6.50 (seed 12345, 45 chunks) after Task 4d's pale oaks; it was 0.49 before them. Java's pale garden is written from knowledge and unverified against a Java run.
- Grove was 0.21 before round 2. It was a bug: Java's grove trees grow on snow (`spruce_on_snow`, `pine_on_snow`), and ours used the sapling survival test, which fails on snow. Java's fixtures hold 14 grove trunks where ours had 0; now the one grove chunk there matches (2.00 / 2.00), and `TestGroveHasTrees` holds it.

**Against Java**, trees are asserted by biome (`TestPlacementRatesMatchJava`, with the fixtures), and all pass after round 2: jungle 9.59 / 9.73, roofed forest 4.58 / 4.64, bamboo jungle 2.88 / 2.75, forest 5.03 / 5.16. Each source now runs on top of what its earlier neighbours do (two levels), as Java's later chunk sees the earlier one's canopy. So where ours differ from Bedrock, it is Java's placement against Bedrock's.

**Kelp and seagrass per ocean chunk**, after fix round 2 of Task 4c (re-measured in round 3, unchanged): ours 108.0 to 109.6 kelp and 47.5 to 55.8 seagrass on the three seeds with real ocean (seed 100 has only 20 ocean chunks: 51.5 and 39.2), against Bedrock's seed 1 figures of 189 and 57. Against Java on the same chunks: kelp 111.3 / 112.9, seagrass 44.6 / 45.5. So Bedrock places about 1.7 times Java's kelp.

## Commit e1873aa's message

Commit e1873aa ("ores on the feature driver follow Mojang's algorithm") says every ore feature replays exactly. At that commit the emerald ore did not yet: it was fixed in the next commits (48b5e1f, 84c767f), and `TestGoldenFeatureTraces` has held every ore run exact since (6485 of 6485). History is not rewritten; this note corrects the record.
