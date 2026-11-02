# Stage 2: World Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A vanilla-like Overworld on our Dragonfly server that Bedrock players can join, meeting the spec's production-ready world criteria.

**Architecture:** One deterministic generator package implements Dragonfly's `world.Generator`. Each chunk is built from only the seed and its coordinates, in passes: climate, terrain shape, surface, caves, ores, decoration. Nothing is cached between chunks and no neighbouring chunk needs to exist, so generation is safe on many workers and keeps memory low. Saving, backups and tick measurement sit in `internal/app` around Dragonfly's own world and LevelDB storage.

**Tech Stack:** Go; Dragonfly v0.11.6 (pinned commit `51dd9c1`); module `github.com/overmind/overmind`.

**Spec:** `docs/superpowers/specs/2026-11-02-overmind-restart-design.md`

The owner said on 2026-11-03: "Just do the world." So the Java translation (`2026-11-02-stage-1-translation-layer.md`, Tasks 2-10) is paused, not dropped, and this plan is tested with Bedrock bots only.

## Global Constraints

- No Mojang code or files in the repo, and the server never needs them to run. Studying decompiled code outside the repo is allowed (owner, 2026-11-03): learn how it works, then write our own code, with no copied code. Each table of values says where it came from. Don't write values from memory.
- No structures, mobs, Nether or End in this plan.
- Y range -64 to 319, sea level 63.
- Same seed and position give the same chunk, in any order, on any number of workers.
- Do not keep per-chunk data after `GenerateChunk` returns, other than Dragonfly's own storage.
- Memory is whole-process. Targets are in the spec's Quarry.ts table: 50 MB idle, 161 MB peak at 1 player, 1,093 MB peak at 10, 1,209 MB peak at 30. Report honestly whichever way they land.
- Every folder has a short `NOTES.md`. No duplicated code.
- Don't change Dragonfly unless a task must, and say why (`replace` in `go.mod`).
- Commit only in this repo on `stage-1`. Nothing is pushed. Commit messages end with: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`

## Review Focus

1. A player walks across chunk borders. No cliffs, cut trees or broken caves appear at the seams. (Tasks 1, 3, 4)
2. A seed puts spawn in the ocean. The player still spawns on dry land. (Task 5)
3. A power cut hits during a save. The world reopens with no corruption and at most a few seconds lost. (Task 6)
4. The disk fills or a backup folder is unwritable. The backup fails with a logged error, and the game keeps running. (Task 6)
5. Many players join in the same moment. The server does not crash. (Task 7)

---

## File Structure

```text
internal/worldgen/
  generator.go   Generator: GenerateChunk, DefaultSpawn
  noise.go       seeded gradient noise, octaves
  climate.go     temperature, humidity, continentalness, erosion, weirdness
  terrain.go     height and 3D density
  biomes.go      climate to biome table
  surface.go     top blocks per biome
  caves.go       cave carving, aquifers, lava
  ores.go        ore table and veins
  decorate.go    trees, plants, snow and ice, across chunk edges
  blocks.go      block runtime IDs, looked up once
internal/app/saving.go, backup.go, ticklog.go
tools/worldmap/  draws a PNG map of a seed
```

## Task 1: Noise, climate and terrain shape

**Files:** Create `noise.go`, `climate.go`, `terrain.go`, `generator.go`, `blocks.go`, tests, `NOTES.md` in `internal/worldgen/`; `tools/worldmap/main.go`.

**Interfaces:**
- Produces: `worldgen.New(seed int64) *Generator`, which satisfies `world.Generator`.
- Produces: `(*Generator).Climate(x, z int) Climate`, with fields `Temperature, Humidity, Continentalness, Erosion, Weirdness float64`.
- Produces: `(*Generator).SurfaceHeight(x, z int) int`.
- Produces: `go run ./tools/worldmap -seed N -size 1024 -o map.png`.

Shape: base height from a spline over continentalness, erosion and peaks-and-valleys, as the wiki's "World generation" page describes, then 3D density for overhangs. Fill solid with stone below the surface, water up to Y 63, air above. Bedrock floor comes in Task 3.

- [ ] **Step 1: Write the failing tests**

```go
func TestSameSeedSameChunk(t *testing.T)      // two generators, same seed: chunks equal
func TestDifferentSeedDiffers(t *testing.T)
func TestOrderIndependent(t *testing.T)       // chunk (5,5) generated first or after 100 others is equal
func TestConcurrentSafe(t *testing.T)         // 64 goroutines under -race
func TestNoSeamAtChunkEdges(t *testing.T)     // over 200 chunks, height step across an edge is no bigger than within a chunk's 99th percentile
func TestTerrainStatistics(t *testing.T)      // 5 seeds, 4096x4096 sampled: ocean 25-75% of area, some land above Y 120, peaks under Y 319, deep ocean below Y 45
func BenchmarkGenerateChunk(b *testing.B)     // report ms/op
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/worldgen/`
Expected: FAIL.

- [ ] **Step 3: Implement noise, climate, terrain and the map tool**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/worldgen/ -race`
Expected: PASS. Look at a generated map, and check for visible continents rather than speckle.

- [ ] **Step 5: Commit**

## Task 2: Biomes and surface

**Files:** Create `biomes.go`, `surface.go`, tests in `internal/worldgen/`.

**Interfaces:**
- Produces: `(*Generator).BiomeAt(x, y, z int) world.Biome`.

Required biomes, using Dragonfly's `biome` package: plains, sunflower plains, forest, flower forest, birch forest, dark forest, taiga, snowy taiga, snowy plains, ice spikes, desert, savanna, jungle, swamp, badlands, meadow, grove, snowy slopes, jagged peaks, frozen peaks, stony peaks, windswept hills, beach, snowy beach, stony shore, river, frozen river, mushroom fields, dripstone caves, lush caves, and warm, lukewarm, cold and frozen ocean with deep versions. The climate ranges come from the wiki's "Biome" page.

Surface blocks per biome: grass and dirt, sand and sandstone, red sand and terracotta bands, snow block, stone on peaks, gravel on stony shore, mycelium, podzol in taiga patches. Ocean floors are sand, gravel or clay.

- [ ] **Step 1: Write the failing tests**

```go
func TestEveryRequiredBiomeAppears(t *testing.T)    // across 5 seeds, 8192x8192: all surface biomes found
func TestNoHardClimateJumps(t *testing.T)           // desert is never directly beside snowy plains
func TestSurfaceMatchesBiome(t *testing.T)          // desert top is sand, snowy plains is snow-covered, peaks are stone or snow
func TestBiomesStoredInChunk(t *testing.T)          // chunk.Biome equals BiomeAt, including underground cave biomes
func TestBiomeSizeIsVanillaLike(t *testing.T)       // median connected biome region is 150-1000 blocks across
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/worldgen/ -run 'Biome|Surface|Climate'`
Expected: FAIL.

- [ ] **Step 3: Implement biomes and surface**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/worldgen/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

## Task 3: Underground

**Files:** Create `caves.go`, `ores.go`, tests in `internal/worldgen/`.

- Bedrock floor is solid at Y -64, thinning to none by Y -60, by position-seeded chance.
- Deepslate replaces stone below Y 0, with a blended border to Y 8. Patches of granite, diorite, andesite and tuff.
- Caves: "cheese" and "spaghetti" noise carve air, as the wiki's "Cave" page describes. Below Y -54, carved space fills with lava. Under oceans and rivers, caves don't break through the floor unless flooded. Simple aquifers: pockets of water at set levels.
- Ores: every ore in the wiki's "Ore" page with its Y ranges and vein sizes, with deepslate variants below Y 0.

- [ ] **Step 1: Write the failing tests**

```go
func TestBedrockFloor(t *testing.T)            // Y -64 is always bedrock, Y -59 never
func TestDeepslateLine(t *testing.T)           // Y -10 mostly deepslate, Y 20 none
func TestCavesExist(t *testing.T)              // air share at Y -40..40 is 3-20%, and caves connect across chunk edges
func TestSomeCavesReachSurface(t *testing.T)   // entrances exist on land in a 1024x1024 area
func TestLavaOnlyDeep(t *testing.T)            // generated lava only below Y -54
func TestOceanFloorHolds(t *testing.T)         // no column of open air directly under ocean water
func TestOreRanges(t *testing.T)               // every ore found only inside its table range, and diamonds found at all in 64 chunks
func TestOreCornerBlocksReplaced(t *testing.T) // ores only replace stone-type blocks, never air or water
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/worldgen/ -run 'Bedrock|Deepslate|Caves|Lava|Ocean|Ore'`
Expected: FAIL.

- [ ] **Step 3: Implement the underground**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/worldgen/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

## Task 4: Trees, plants, snow and ice

**Files:** Create `decorate.go`, tests in `internal/worldgen/`.

**Rule for chunk edges:** to decorate a chunk, work out decorations for it and its 8 neighbours from the seed and neighbour position alone, and write only the blocks inside this chunk. A decoration's position is seeded by its origin chunk, and its base height uses `SurfaceHeight`, never blocks in a real neighbour.

Decorations: oak, birch, spruce, dark oak, jungle and acacia trees per biome, using Dragonfly's blocks; short grass, ferns, flowers, double plants, cactus, sugar cane, dead bushes, seagrass and kelp where Dragonfly has them, and lily pads in swamps. Snow layers and ice are placed from the biome's temperature. List any plant Dragonfly lacks in `NOTES.md`.

- [ ] **Step 1: Write the failing tests**

```go
func TestTreesWholeAcrossEdges(t *testing.T)    // a tree crossing an edge: every log above the first has a log or leaves connected in the next chunk
func TestDecorationOrderIndependent(t *testing.T)
func TestForestHasTreesDesertHasNone(t *testing.T)
func TestTreesStandOnSoil(t *testing.T)         // the block under each trunk is dirt or grass
func TestNoFloatingPlants(t *testing.T)         // every plant sits on a valid block, including over caves
func TestFrozenOceanHasIce(t *testing.T)
func TestSnowOnlyWhereCold(t *testing.T)
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/worldgen/ -run 'Trees|Decoration|Forest|Plants|Frozen|Snow'`
Expected: FAIL.

- [ ] **Step 3: Implement decoration**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/worldgen/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

## Task 4b: Same seed as Bedrock

Owner asked on 2026-11-03. Rewrite the noise, climate, terrain and noise-cave layers to reproduce Mojang's values for a given seed, in our own Go code, from study notes. Accept: on the measured Bedrock worlds (seeds 100, 1, 12345, 987654321), biomes and height match at least as well as Java does (98.69-99.92% biomes; height within 2 blocks on 97.39% at seed 100), compared with a tool that reads reference data from outside the repo. Reference worlds stay outside the repo. Decorations keep their statistical targets.

## Task 5: Playing in the world

**Files:** Modify `internal/config/config.go` (add `Seed`, default chosen randomly on first start, then kept in the world folder), `internal/app/app.go`. Create `internal/app/world_test.go`.

Use `worldgen.New` for the Overworld in `Config.Generator`. Leave `Players.SaveData` off, since player data belongs to stage 3. `DefaultSpawn` walks outward from (0,0) to the nearest land above sea level with a solid top and at least 2 air blocks above it.

**Vanilla behaviour:** Dragonfly already runs random ticks, water flow, falling sand, leaf decay, grass spread, and the day cycle. Tests prove they work in the generated world. Failures are fixed only if the fix is small; otherwise list in `docs/stage-2-known-gaps.md`.

- [ ] **Step 1: Write the failing tests**

```go
func TestBotSpawnsOnLand(t *testing.T)         // 20 seeds, spawn block is solid and not water or leaves
func TestBotGetsTerrainChunks(t *testing.T)    // Bedrock bot joins and receives non-flat chunks
func TestSeedSurvivesRestart(t *testing.T)     // same spawn and same untouched chunk after restart
func TestPlayerChangesSurviveRestart(t *testing.T)
func TestWaterFlows(t *testing.T)              // break a block beside water: water spreads
func TestSandFalls(t *testing.T)
func TestDayCycleAdvances(t *testing.T)
func TestLeavesDecay(t *testing.T)             // remove a tree's logs: leaves go within a time limit
func TestGrassSpreads(t *testing.T)            // with raised random tick speed
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/app/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement the wiring**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/app/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

## Task 6: Safe saves and backups

**Files:** Create `internal/app/saving.go`, `backup.go`, `saving_test.go`, `backup_test.go`, `cmd/crashtest/main.go`.

**Interfaces:**
- Config: `SaveSeconds` (default 5), `BackupMinutes` (default 60), `BackupFolder` (default `backups`), `BackupsKept` (default 24).
- Produces: `app.Backup(srv *server.Server, folder string) (path string, err error)`.

**Decisions:**
- Dragonfly saves every 10 minutes by default, which breaks the spec's "last few seconds", so set `SaveInterval` to `SaveSeconds`. Read `world.go` first. If each save rewrites unchanged chunks, write only changed ones, or find what Dragonfly exposes, and measure the tick cost. If this requires changing Dragonfly, that is an allowed reason to fork.
- LevelDB writes must survive a power cut: check whether writes are synced, and set `Sync` for the save batch or sync at the end of each save.
- Backup: LevelDB table files never change once written, so hard-link them (copy if linking fails) and copy `CURRENT`, `MANIFEST` and the log during one brief pause of the world, without blocking the game for over 50 ms.
- A failed backup logs an error, leaves a partial folder clearly unused (`.tmp` until complete) and the game keeps going.

- [ ] **Step 1: Write the failing tests**

```go
func TestKillDuringSavesLosesLittle(t *testing.T) // child server changes blocks continually; hard kill at 20 random moments; world opens each time; the last change older than SaveSeconds+1 is present
func TestBackupRestores(t *testing.T)             // backup, change world, open backup: old blocks and chunks intact
func TestBackupPauseIsShort(t *testing.T)         // world pause under 50 ms with 2,000 chunks stored
func TestBackupFailureKeepsRunning(t *testing.T)  // unwritable folder: error returned, server still ticking, no .tmp treated as a backup
func TestOldBackupsRemoved(t *testing.T)          // keeps BackupsKept
func TestCleanShutdownSaves(t *testing.T)
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/app/ -run 'Kill|Backup|Clean'`
Expected: FAIL.

- [ ] **Step 3: Implement saves and backups**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/app/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

## Task 7: Load, memory and joining

**Files:** Create `internal/app/ticklog.go`; modify `tools/memtest/main.go`, `docs/memory-log.md`.

**Interfaces:**
- Config: `TickLog bool` (off by default). When on, logs measured ticks per second and the worst tick delay every 10 seconds.
- Memtest: phases as in the spec's table, up to 30 bots, printing memory and ticks per second.

**Investigate first:** the Task 1 report says `go-raknet` crashes when several bots dial at once. Find out if the crash is client-side (bot only) or server-side. Server-side is a public crash risk and must be fixed, by a `replace` if required. Also check whether Dragonfly's unlocked `PlayerByName` needs a workaround in our code.

- [ ] **Step 1: Write the failing test**

```go
func TestJoinBurst(t *testing.T) // 30 bots from separate processes join within 1 s; server survives; all or nearly all get in, and refused ones can retry
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./internal/app/ -run JoinBurst`
Expected: FAIL.

- [ ] **Step 3: Implement the tick log and any join fix**

- [ ] **Step 4: Run it to confirm it passes**

Run: `go test ./internal/app/ -run JoinBurst -race`
Expected: PASS.

- [ ] **Step 5: Run the comparison**

Run memtest with all phases, in the generated world, walking outward. Record in `docs/memory-log.md` beside Quarry.ts's numbers.
Expected: spec targets: 20 ticks per second held with 30 exploring, no stall that freezes players, and memory below Quarry.ts. If any isn't met, use a heap profile and fix the biggest cause, such as chunk unload timing, worker count or view distance handling, and then re-measure. Numbers that still miss are reported honestly.

- [ ] **Step 6: Commit**

## Task 8: Long test and hand-over

**Files:** Modify `docs/HOW-IT-FITS.md`, `docs/memory-log.md`. Create `docs/stage-2-known-gaps.md`, `docs/stage-2-production-check.md`.

- [ ] **Step 1: Start the 72-hour test on the VPS**

Cross-compile for Linux. Read the memory note `main-vps.md` for access. Use a new `/opt/overmind`, UDP port 19233 bound to 127.0.0.1, login off for bots only, and 10 bots on the VPS in a screen session or systemd unit named `overmind-soak`. Log memory and ticks every minute to a file. Leave all other services alone and don't change the firewall.
Expected: running and logging. The result won't exist for 72 hours, so record the start time.

- [ ] **Step 2: Write `docs/stage-2-production-check.md`**

One row per production-ready criterion from the spec (safe saves, backups, smooth generation, long running, lighter than Quarry.ts): passed, failed or still running, with the evidence. Also note that no comparison against a real Bedrock world has been done, because that needs the owner to make one.

- [ ] **Step 3: Run everything**

Run: `go vet ./... && go test ./... -race`
Expected: PASS.

- [ ] **Step 4: Commit**

- [ ] **Step 5: Start the server for the owner**

On the owner's PC: `go build -o overmind.exe ./cmd/overmind` then run in the background with login on, port 19132, seed random, in a `run/` folder ignored by git. Confirm it's listening. The owner joins from Bedrock at `localhost`.
