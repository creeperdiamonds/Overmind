# config

Reads the settings file and holds the defaults. `JavaOffline` turns Mojang login off for test bots; leaving it out keeps login on.

`Seed` is a pointer so that 0, a real seed, is told apart from "not set" (a random seed is chosen and kept in the world folder, see `internal/app`). `RandomTickSpeed` 0 is Dragonfly's normal 3 blocks per sub chunk per tick; tests raise it. `ChunkWorkers` 0 means min(4, processors). `Profile` picks the world's placement rules, `bedrock` (default) or `overjava`; an unknown name is a start error. `TickDistance` 0 is Bedrock's default 4; `internal/app` holds it at least two under `ViewDistance` so a tick never has to generate a chunk.

`[skins]` (keys in camel case, as the owner named them): `blockOddShapes` (off) replaces Bedrock skins whose shape goes beyond the standard player model with a default skin for everyone; `uploadAccount` (empty, so no uploads) is the account file `overmind skin-account` writes, for signing Bedrock skins for Java viewers; `uploadCache` (`skins`) keeps what Mojang signed; `uploadGapSeconds` (30) paces uploads. See `internal/javafront/NOTES.md` before setting an account: it can be suspended. `Skins.Uploader` is for tests and never read from the file.

`Operators` (empty, so nobody) may use /give, the one command players have (ruling 38): each entry a player name on either edition (any case), a Java account UUID (with or without dashes) or a Bedrock XUID (digits; such an entry is a name too, since a Java name may be digits). A UUID matches only a Java player and an XUID only a Bedrock one; a name matches either, so with Xbox and Mojang login on, a Bedrock gamertag equal to a Java operator's name is an operator too: prefer UUIDs and XUIDs. See `internal/give`.

`FlatWorld` is for tests and never read from the file: the Overworld is Dragonfly's flat world instead of worldgen's (internal/javafront's tests use it through `testserver.FlatWorld`).

`ShowCoordinates` (on by default; vanilla BDS defaults it to false) turns on Bedrock's "Show coordinates" for each Bedrock player as they join, through the `showcoordinates` game rule. Java needs nothing: F3 shows coordinates, and the Java login does not send reduced debug info.
