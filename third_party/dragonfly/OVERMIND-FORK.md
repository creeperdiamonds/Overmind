# Overmind's Dragonfly fork

Dragonfly v0.11.6 (`github.com/df-mc/dragonfly`, MIT, see LICENSE), copied here from the Go module cache and used through `replace` in Overmind's go.mod, as the design planned. The fork came forward from stage 1 Task 5 to Task 4c fix round 2, the first change Overmind needed inside Dragonfly.

Changes, each marked "Overmind fork" in the code:

- `server/world/tick.go`: a loader's random-tick and entity-tick reach is the world's tick range held at least two chunks under the loader's own chunk radius (`tickCentres`), with its own chunk always ticked. Before, every loader ticked the world's tick range whatever its view distance, so a client asking for a small view distance made ticks read, and generate, chunks it had not loaded. Entities now tick only within that reach, not in every viewed chunk.
- `server/world/tick.go`, `server/world/weather.go`: nothing past every loader's tick reach changes, as on Bedrock, where chunks past the simulation distance stay loaded but do not tick. Lightning strikes only ticked chunks (a bolt past reach would never tick, so never close). Scheduled ticks there stay queued (`scheduledTickQueue.tickIn`; `tick` keeps its old meaning for upstream tests) and neighbour updates wait while their chunk is loaded, so liquids, falling blocks and fire resume when a player comes near. `lightningChance` is a variable so tests can raise it (`export_test.go`). The dead `anyWithinDistance` is removed. Tested by `overmind_reach_test.go`.

  Left as they are, and why that is harmless: redstone propagates within one tick from a change in reach, through at most 15 blocks of wire, and explosions and entity drops start from entities, which tick only in reach. Either can leave an entity (lit TNT, an item) at most a chunk past reach, which is still inside view (the reach is held two chunks under it). It freezes there and resumes when a player comes near, as any entity that walks out of reach does; unlike a bolt, it closes on its own once ticked.

- `server/block/leaves.go`: a leaf stays while a log is within 6 steps through leaves (`leafLogReach`, Java's rule; was 4). Overmind generates Java's trees, whose leaves reach 6 steps, so at 4 any change beside an untouched tree decayed its outer leaves. Tested by internal/app `TestLeavesDecay`.

- `server/session/handler_request_chunk_radius.go`: a client's radius is held to at least 2 (`minChunkRadius`, `clampChunkRadius`), the smallest tick reach (0, the player's own chunk) plus 2, so a tick never reads an unloaded chunk. Tested by `overmind_radius_test.go`.

To take a new Dragonfly release: copy it over this folder, then reapply the changes above.
