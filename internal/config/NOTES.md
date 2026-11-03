# config

Reads the settings file and holds the defaults.

`Seed` is a pointer so that 0, a real seed, is told apart from "not set" (a random seed is chosen and kept in the world folder, see `internal/app`). `RandomTickSpeed` 0 is Dragonfly's normal 3 blocks per sub chunk per tick; tests raise it. `ChunkWorkers` 0 means half the processors.
