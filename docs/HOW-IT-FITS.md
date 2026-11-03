# How the parts fit

Overmind is a Minecraft server where Bedrock and Java players share one world.

- `cmd/overmind` is the program you run. It reads the settings file and calls `internal/app`.
- `internal/config` reads `overmind.toml` (and writes the defaults if it is missing).
- `internal/app` builds the Dragonfly server from those settings and starts it. Everything else that needs a running server calls `app.Start`.
- `internal/testserver` starts a throwaway server on spare ports for tests in any package.
- `bots/bedrock` is a small Bedrock client used by tests and by the memory test.
- `tools/memtest` measures the server's whole-process memory while bots walk outward.
- `docs/memory-log.md` holds the memory numbers compared with Quarry.ts.

The Java side, `internal/javafront`, sits beside the Bedrock side as a second front end to the same world. A Java player is an ordinary Dragonfly player that reports through `player.Reporter` (our one change to the Dragonfly fork in `third_party/dragonfly`) and sees the world as a `world.Viewer`, in Java packets. Nothing is translated through Bedrock.
