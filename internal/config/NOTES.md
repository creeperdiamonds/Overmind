# config

Reads the settings file and holds the defaults.

`Seed` is a pointer so that 0, a real seed, is told apart from "not set" (a random seed is chosen and kept in the world folder, see `internal/app`). `RandomTickSpeed` 0 is Dragonfly's normal 3 blocks per sub chunk per tick; tests raise it. `ChunkWorkers` 0 means half the processors. `Profile` picks the world's placement rules, `bedrock` (default) or `overjava`; an unknown name is a start error. `TickDistance` 0 is Bedrock's default 4; `internal/app` holds it at least two under `ViewDistance` so a tick never has to generate a chunk.
