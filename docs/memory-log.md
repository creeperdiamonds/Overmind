# Memory log

Whole-process memory (Windows working set), view distance 6, bots on the same machine as the server walking outward in different directions. Measured with `tools/memtest` against `cmd/overmind` (Dragonfly v0.11.6, flat world, Xbox login off for the bots). Ticks per second are not measured yet (memtest does not read them).

## Task 1, 2026-11-02 (Dragonfly flat world, no Java side yet)

| Players | Overmind end | Overmind peak | Quarry.ts end | Quarry.ts peak |
|---|---|---|---|---|
| Nobody, 60 s | 57 MB | 57 MB | 50 MB | 50 MB |
| 1 walking, 60 s | 95 MB | 95 MB | 127 MB | 161 MB |
| 10 walking, 120 s | 173 MB | 173 MB | 158 MB | 1,093 MB |

Idle is 7 MB above Quarry.ts's 50 MB. The peaks are sampled every 250 ms, so a short spike could be missed.
Each row is a fresh server process. Bots join one at a time (the RakNet dialer crashes when several dial at once).

### Idle heap profile (largest users, Go in-use heap, about 19 MB in total)

The working set (57 MB) is larger than the Go heap (19 MB) because it also counts the 30+ MB program image, runtime structures and goroutine stacks.

| Where | In use |
|---|---|
| nbt decoding (`unmarshalCompoundAny` + strings) | about 6 MB |
| `world.BasicBlockRegistry.Finalize` (block tables) | 3.5 MB |
| `world.init` (block/state tables) | 1.6 MB |
| `block.allWalls`, `allPinkPetals` (generated block states) | 1.5 MB |
| `intintmap`, `flate`, schema upgrader | 2 MB |

Almost all of it is Dragonfly's startup tables for every block state, loaded whether or not a world uses them.
Ideas for later, not done: trim the Go runtime with `GOGC`/`GOMEMLIMIT`, and check whether the block tables can be built lazily.
