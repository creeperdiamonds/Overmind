# Memory log

Whole-process memory (Windows working set), view distance 6, bots on the same machine as the server walking outward in different directions. Measured with `tools/memtest` against `cmd/overmind` (Dragonfly v0.11.6, flat world, Xbox login off for the bots). Ticks per second are not measured yet (memtest does not read them).

## Task 1 fix, 2026-11-03 (Dragonfly flat world, no Java side yet)

Current figures. Memtest now starts sampling before the first bot joins, so the chunk burst at join time is included, and the peak is also the larger of the sampled peak and Windows' own `PeakWorkingSetSize`.

| Players | Overmind end | Overmind peak | Quarry.ts end | Quarry.ts peak |
|---|---|---|---|---|
| Nobody, 60 s | 2 MB (see note) | 61 MB | 50 MB | 50 MB |
| 1 walking, 60 s | 92 MB | 92 MB | 127 MB | 161 MB |
| 10 walking, 120 s | 205 MB | 205 MB | 158 MB | 1,093 MB |

Notes:
- Quarry.ts's figures are not measured here. They are copied from the table in the design doc (`docs/superpowers/specs/2026-11-02-overmind-restart-design.md`), taken from its earlier run, which included terrain generation. Ours still use the flat world, so this is not yet a like-for-like comparison.
- The nobody row's "end" of 2 MB is Windows trimming the working set of an idle process, not real use (a second run gave 1 MB end and 64 MB peak). Read the 61 MB peak as the idle size, 11 MB above Quarry.ts's 50 MB.
- 10 walking: 205 MB end is 47 MB above Quarry.ts's 158 MB end, but far below its 1,093 MB peak. 1 walking: below Quarry.ts on both.
- Each bot was recorded as joined (1 of 1, 10 of 10). Memtest now exits non-zero if any bot fails to join.
- Ticks per second are not measured.

### Commands used

Each row is a fresh server process, started in an empty folder with `AuthEnabled = false` in `overmind.toml` (everything else default, view distance 6):

```
go build -o overmind.exe ./cmd/overmind
go build -o memtest.exe ./tools/memtest
overmind.exe -config overmind.toml          # new world folder each time, wait 4 s
memtest.exe -pid <overmind pid> -bots 0  -seconds 60
memtest.exe -pid <overmind pid> -bots 1  -seconds 60
memtest.exe -pid <overmind pid> -bots 10 -seconds 120
```

The seconds count starts after the last bot has joined. Bots join one at a time (the RakNet dialer crashes when several dial at once). Bot i walks at angle 2*pi*i/bots.

## Task 1, 2026-11-02 (replaced)

Replaced by the figures above. Reason: memtest only began sampling after every bot had joined, so it missed the memory used while the join-time chunks were sent, and it did not read Windows' recorded peak. The old Overmind peak equal to its end in every row was a sign of this. The old numbers were: nobody 57 / 57 MB, 1 walking 95 / 95 MB, 10 walking 173 / 173 MB (end / peak). The old log also did not give the commands or the source of Quarry.ts's figures.

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
