# app

Builds and starts the server from the settings.

- **World:** the Overworld uses `worldgen.New(seed)`. The Nether and End are flat floors that nothing sends players to yet (Dragonfly builds all three worlds). Player data stays off until stage 3.
- **Seed** (`seed.go`): kept in `overmind-seed.txt` in the world folder. A new world takes `Config.Seed` or a random one. An existing world keeps its own, and a different `Config.Seed` is a start error, because saved chunks would not fit the new terrain. Any int64 works; Bedrock's own handling of negative seeds and seeds of 2^31 or more is not verified.
- **Spawn:** Dragonfly asks the generator for a spawn only when it has no world database, so on a world with no `level.dat` yet `Start` sets `DefaultSpawn` itself. The search is deterministic, so a world that stopped before its first save finds the same spawn again.
- **Block registration order:** worldgen registers its own block types (snow layer, seagrass) in its `init`. `uc.Config` opens the world database and that finalizes the block registry, and `worldgen.New` finalizes it too. So this package must import worldgen (it does), and any new custom block type must be registered in an `init` that runs before `Start`. Registering later panics.
- **Chunk workers:** `Config.ChunkWorkers`, or half the processors. The generator is safe to share. Chunk generation runs on those workers, off the world's tick, but a block read of an unloaded chunk inside a tick waits for that chunk (Dragonfly's `doImmediate`).
