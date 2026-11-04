# app

Builds and starts the server from the settings. Starts the game, then the Java front end in `internal/javafront`. `Server.Close` stops the autosave, disconnects Java players (saving them), closes the Java store (writing what is pending), then closes the game, which saves Bedrock players and closes theirs. The name check between editions is wired here. Mojang login is on unless the settings say `JavaOffline`.

`hub.go` is the one place players of both editions join and leave through. Bedrock reaches it from the `Accept` loop and the player's `HandleQuit`; Java from the front end's `Joined` and `Left` hooks. Members are keyed by edition and account. It:

- announces joins and leaves in `chat.Global` (Dragonfly's own messages are turned off), so each client gets them in its own format;
- shows player chat to each member in its edition's format; Bedrock text going to Java loses section signs and control characters, as Java players may not send them;
- lists every player in both editions' player lists (`javafront.Front.List`, and `session.List` for Java players in Bedrock's list);
- counts players for `Server.Online`, the Java server list and the Bedrock server list (`statusProvider`);
- saves every player online each `SaveSeconds` (a snapshot on its world goroutine; the store writes it);
- tells `internal/give` of each player as it joins (`give.Join`: whether `Operators` includes it, and its edition) and leaves, and keeps each command line as typed for it (`HandleCommandExecution`, `give.Remember`). `Start` registers /give once per process (`give.Register`).

One account UUID is one player across editions, since Dragonfly, the player lists and the stores key by UUID: `Allow` refuses a Bedrock join whose UUID is a Java player's, and the Java front end asks `OtherPlayerUUID` (a Bedrock reservation or a Bedrock player online). Each side writes its claim before it reads the other's, so two joins at once never both get in.

Player data is saved per edition by account ID under `PlayersFolder` (`bedrock`, `java`) through `internal/playerstore`, which replaces Dragonfly's LevelDB provider.

Skins: the hub gives `javafront.Front.List` a Bedrock player's skin and handle, and tells the front of each Bedrock skin change (`HandleSkinChange`, `Front.BedrockSkinChanged`), so Java rows follow it. A Java player's skin change (its Mojang skin, once the Java front end has fetched it) goes to `session.SetListedSkin`, so its Bedrock list rows stop carrying the stand-in it joined with. With `skins.blockOddShapes` an odd Bedrock skin becomes `skins.StandIn` at join (Dragonfly's `JoinSkin`, a fork hook) and on each change. With `skins.uploadAccount` the uploader starts here and closes with the server.

World:

- **World:** the Overworld uses `worldgen.New(seed)`. The Nether and End are `gen.Nether()` and `gen.End()`, sharing its cache (Dragonfly builds all three worlds); with `FlatWorld` all three are Dragonfly's flat floors. A player-built Nether portal reaches the Nether; nothing generates an End portal yet, and there is no dragon fight.
- **Seed** (`seed.go`): kept in `overmind-seed.txt` in the world folder. A new world takes `Config.Seed` or a random one. An existing world keeps its own, and a different `Config.Seed` is a start error, because saved chunks would not fit the new terrain. Any int64 works; Bedrock's own handling of negative seeds and seeds of 2^31 or more is not verified.
- **Generator record** (`provenance.go`): `overmind-generator.txt` in the world folder holds the profile and whether Bedrock's feature data was read, with a SHA-256 of the files read (`BedrockData.Hash`). A start whose record differs logs a warning that new chunks will not fit the saved ones; the record then follows the start.
- **Spawn:** Dragonfly asks the generator for a spawn only when it has no world database, so on a world with no `level.dat` yet `Start` sets `DefaultSpawn` itself. The search is deterministic, so a world that stopped before its first save finds the same spawn again.
- **Block registration order:** worldgen registers its own block type (seagrass) in its `init`; the snow layer is the Dragonfly fork's. `uc.Config` opens the world database and that finalizes the block registry, and `worldgen.New` finalizes it too. So this package must import worldgen (it does), and any new custom block type must be registered in an `init` that runs before `Start`. Registering later panics.
- **Chunk workers and the tick:** `Config.ChunkWorkers`, or min(4, processors) (ruling 32: each busy worker holds its own scratch on top of the shared 30 MB cache). The generator is safe to share. Chunk generation must never run on a world's tick (ruling 26). Dragonfly runs a world's ticks and transactions on one goroutine, and a block read there of a chunk that is not loaded generates it on the spot, or waits for a worker already making it (`Tx.chunk`, `doImmediate`). Random ticks and neighbour updates read one block past the chunk they tick, so with the tick range equal to the view distance every chunk at the edge of view did that. So `Start`:
  - sets the tick range (`Config.TickDistance`, Bedrock's default 4) to at most two under the view distance, so every ticked block's neighbours are loaded. The Dragonfly fork (`third_party/dragonfly`, `tick.go`) also holds each player's tick reach, blocks and entities, two under that player's own view distance, so a client asking for a small view distance cannot cause it either (`TestSmallViewDistanceNeverGeneratesOnTick`);
  - writes the spawn into the world settings before the worlds open, since an unset spawn makes the first tick look for the top block at 0, 0;
  - loads the chunks around the spawn out to the tick range plus two on the workers before it listens.
  `offTick` wraps the generator (and passes on its strongholds, `NearestStronghold`, for eyes of ender) and counts (`GenerationsOnTick`) and logs any chunk generated on the tick goroutine; `TestGenerationDoesNotStallTicks` asserts there are none.
