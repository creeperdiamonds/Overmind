# Stage 2 known gaps

Found while testing play in the generated world (`internal/app/world_test.go`). Each is a limit we are living with, with the test that shows it.

## Behaviour

- **Leaf decay is slow and partial.** Dragonfly marks a leaf for decay only when a neighbouring block changes, then decays it on a random tick. After the logs of a tree are cut, the leaves next to them go first and the rest follow as leaves between them fall. With the random tick speed raised to 200, 99 of 172 leaves without a log were gone after 2 s and 129 of 172 after 27 s; the rest had not been marked yet. `TestLeavesDecay` asks for half of them within 60 s. Not a small fix: the rule is in Dragonfly, which we do not change.
- **A block read of a chunk that is not loaded waits inside the tick.** Chunk generation runs on Dragonfly's workers and does not hold up ticks (`TestGenerationDoesNotStallTicks`), but Dragonfly's `doImmediate` makes a tick that reads an unloaded chunk wait for that one chunk, about 7 ms warm. Water, sand and trees at the very edge of loaded ground can cause this; players do not, because their chunks are requested ahead.
- **Snow does not melt; there are no items for snow layers and seagrass** (see worldgen NOTES).
- **Nether and End are flat floors** and nothing sends players there.

## Seeds

- **Bedrock's handling of negative seeds and seeds of 2^31 or more is not verified.** Every seed in `testSeeds` (0, negatives, past 32 bits, both int64 limits) generates, spawns on land, is kept in `overmind-seed.txt` and gives the same world after a restart. Whether Bedrock itself gives the same terrain for those seeds is untested.
- **The world seed is not sent to clients.** Dragonfly's start packet is not given our seed (`defaultGameData` sets none), so a Bedrock client does not show it.

## Tests

- **A test process can hold about 20 servers.** Dragonfly adds its creative items and recipes to global lists each time a server is made, so after that many, a joining bot's session is dropped after a second. Production makes one server per process. The tests keep to fewer, and `TestBotSpawnsOnLand` uses the generator without a server.

## Fixed on the way

- **Spawn inside a tree, flower or snow layer.** `DefaultSpawn` read heights only; seed 100's spawn was inside an oak trunk and seed 12345's inside a snow layer. It now checks the finished chunk (`spawnClear`).
