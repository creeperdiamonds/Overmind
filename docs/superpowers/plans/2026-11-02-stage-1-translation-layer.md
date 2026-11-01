# Stage 1: Translation Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bedrock and Java players log in to one Dragonfly-based server and move, build, chat and manage their inventory together, through our own translator, with memory measured against Quarry.ts.

**Architecture:** Dragonfly already lets a program add its own source of players: `server.Config.Listeners` takes a `server.Listener`, whose `Accept()` returns a `session.Conn`. A `session.Conn` is simply something that reads and writes Bedrock packets. So the Java side is one listener on TCP that hands Dragonfly a fake Bedrock connection for each Java player, translating every packet in both directions. Bedrock players use Dragonfly's normal RakNet listener with no translation.

**Tech Stack:** Go 1.26; Dragonfly at commit `51dd9c1d615f17b70bbeef6ab2cbcee0545e66c0`; gophertunnel v1.63.0 (Bedrock 1.26.60, protocol 2225, also our Bedrock test bot); PrismarineJS `minecraft-data` at commit `bd8b5c6048e5af9694374b83f25f5b4aade619cc`; Node 24 with `minecraft-protocol` for the Java test bot only.

**Spec:** `docs/superpowers/specs/2026-11-02-overmind-restart-design.md`

This plan covers stage 1 only. Stages 2 to 5 each get their own plan when the previous stage is signed off.

## Global Constraints

- No Mojang code or Mojang files in the repo, and no need for them to run the server. Nothing downloads, runs or reads Mojang's server programs. Don't use decompiled code. Mojang's login web service (`sessionserver.mojang.com`) is allowed.
- No GeyserMC code or Geyser mapping files.
- Java target is **26.1, protocol 775**. This differs from the spec's "latest" because open data stops there: the latest release is 26.3 (protocol 777), but `minecraft-data` has no `pc/26.3` and `minecraft-protocol` stops at 26.1. Everything specific to a version lives in `internal/tables/java261/`, so another version means another table.
- Bedrock target is what gophertunnel v1.63.0 supports: **1.26.60, protocol 2225**.
- Values come from generated tables. Never hand-write packet IDs, block IDs or item IDs.
- Xbox and Mojang login are on by default. Turning them off is only for bots, needs an explicit setting, and logs a warning.
- Every folder has a short `NOTES.md` saying what it does. `docs/HOW-IT-FITS.md` explains how the parts connect. No duplicated code.
- Memory is whole-process, in the same test as the spec's Quarry.ts table: view distance 6, bots on the same machine walking outward in different directions. Do not keep a per-player copy of world data.
- Keep Dragonfly's copyright and license notice, and credit it and `minecraft-data` in `NOTICE.md`.
- Use Dragonfly as a pinned dependency for now. It becomes our fork (`replace` in `go.mod`) the first time a task needs to change it, and that task must say why.
- Commit only inside `Overmind-restart`. The old `PROJECTS\Overmind` repo stays untouched. Nothing is pushed.

## Review Focus

1. A public client sends junk: a bad length, a packet over 2 MiB, or an unknown packet. Only that player is disconnected, and the server keeps running. (Task 3)
2. The same name is online on both editions. The second player is refused with a clear message. (Task 5)
3. A Java client vanishes mid-join or mid-play. Other players stop seeing them, and no goroutine is left behind. (Task 6)
4. The server meets a block with no Java mapping, since the mapping table is from Bedrock 1.26.30 and the server runs 1.26.60. It shows as stone and logs once per block, and the player is not kicked. (Task 2)
5. A Java client on the wrong version joins. It is told to use 26.1 instead of hanging or crashing. (Task 4)

---

## File Structure

```text
cmd/overmind/main.go          starts the server and both listeners
internal/config/              settings file (address, Java port, auth on/off, view distance)
internal/tables/java261/      generated tables, plus the lookups over them
internal/javawire/            VarInt, frames, compression, encryption
internal/javaproto/           Java packets as Go read/write functions
internal/javalogin/           status, login, Mojang check, configuration
internal/bridge/              the fake Bedrock connection and its translators
  conn.go                     implements session.Conn
  listener.go                 implements server.Listener
  chunks.go, entities.go, blocks.go, chat.go, inventory.go, join.go
tools/gentables/              builds internal/tables from minecraft-data
tools/memtest/                the memory comparison
bots/java/                    Node Java bot
bots/bedrock/                 Go Bedrock bot
docs/HOW-IT-FITS.md, NOTICE.md
```

## Task 1: Bedrock server boots, and the first memory number

**Files:**
- Create: `go.mod`, `cmd/overmind/main.go`, `internal/config/config.go`, `bots/bedrock/bot.go`, `cmd/overmind/boot_test.go`, `tools/memtest/main.go`, `NOTICE.md`, `docs/HOW-IT-FITS.md`, `.gitignore`

**Interfaces:**
- Produces: `config.Load(path string) (config.Config, error)`, with fields `BedrockAddr` (default `:19132`), `JavaAddr` (default `:25565`), `AuthEnabled` (default `true`), `ViewDistance` (default `6`), `WorldFolder` (default `world`), `MaxPlayers` (default `50`).
- Produces: `overmind.Start(cfg config.Config) (*server.Server, error)` in `cmd/overmind`.
- Produces: `bedrock.Join(addr, name string) (*bedrock.Bot, error)`, with methods `Walk(angle float64)`, `Say(text string)`, `Close() error`.
- Produces: `memtest` flags `-pid`, `-bots`, `-seconds`, printing the same columns as the spec's table.

- [ ] **Step 1: Install Go 1.26 and create the repo**

Run: `winget install GoLang.Go`, then `git init` in `Overmind-restart`.
Expected: `go version` prints 1.26 or newer.

- [ ] **Step 2: Write the failing test**

```go
func TestBedrockBotJoins(t *testing.T) {
    srv := startTestServer(t) // AuthEnabled=false, spare ports, temp world folder
    bot, err := bedrock.Join(srv.BedrockAddr, "TestBot")
    require.NoError(t, err)
    defer bot.Close()
    require.Eventually(t, func() bool {
        _, ok := srv.PlayerByName("TestBot")
        return ok
    }, 15*time.Second, 100*time.Millisecond)
}

func TestAuthIsOnByDefault(t *testing.T) {
    cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
    require.NoError(t, err)
    require.True(t, cfg.AuthEnabled)
}
```

- [ ] **Step 3: Run it to confirm it fails**

Run: `go test ./cmd/overmind/ -v`
Expected: FAIL, because `startTestServer` is undefined.

- [ ] **Step 4: Implement `config.Load`, `overmind.Start` and the Bedrock bot**

Use Dragonfly's flat generator for now. With `AuthEnabled=false`, log a warning at startup. `config.Load` writes the defaults when the file is missing.

- [ ] **Step 5: Run it to confirm it passes**

Run: `go test ./cmd/overmind/ -v`
Expected: PASS.

- [ ] **Step 6: Implement `memtest` and take the first number**

Run memtest on an empty server for 60 seconds, then with 1 and 10 Bedrock bots. Write the numbers in `docs/memory-log.md` next to Quarry.ts's 50 MB idle, 161 MB and 1,093 MB peaks.
Expected: a filled table. Whatever the numbers are, record them. If idle is above 50 MB, also record a Go heap profile showing the largest users, and tell the owner before Task 2 begins, because this is the measurement the spec calls out.

- [ ] **Step 7: Commit**

`git add -A && git commit -m "feat: boot Dragonfly server with Bedrock bot and first memory numbers"`

## Task 2: Generated tables

**Files:**
- Create: `tools/gentables/main.go`, `internal/tables/java261/tables.go`, `internal/tables/java261/data/*`, `internal/tables/java261/tables_test.go`, `internal/tables/java261/NOTES.md`

**Interfaces:**
- Produces:
  - `java261.JavaState(bedrockRuntimeID uint32) uint16`
  - `java261.PacketID(state, direction, name string) int32`
  - `java261.JavaItem(bedrockName string, meta int16) (id int32, ok bool)`, `java261.BedrockItem(javaID int32) (name string, meta int16, ok bool)`
  - `java261.JavaBiome(name string) int32`
  - `java261.Registries() []Registry`, with `Registry{ID string; Entries []string}`
  - `java261.ProtocolVersion = 775`

**Sources, from `minecraft-data`:** `pc/26.1/blocks.json`, `items.json`, `biomes.json`, `protocol.json`, `loginPacket.json` (the keys of `dimensionCodec`, in order, are the registries), and `bedrock/1.26.30/blocksB2J.json`. Record the commit in the generated header. Store the output in a compact file with `go:embed`.

**State ID rule:** `stateId = minStateId + mixed-radix index of the property values`, with the last property in the block's `states` list changing fastest. Properties missing from a mapping string use the block's `defaultState`.

**Key matching:** mapping keys look like `minecraft:oak_log[pillar_axis=x]` and values like `minecraft:oak_log[axis=x]`. Sort properties by name on both sides. Render Dragonfly bool properties (`uint8` 0/1 or Go bool) to match the mapping's form.

**Memory:** `JavaState` is a flat `[]uint16` indexed by Bedrock runtime ID and built once at startup. Do not keep maps of strings after startup, except where `BedrockItem` and `JavaItem` need them.

- [ ] **Step 1: Write the failing tests**

```go
func TestStateRule(t *testing.T) {
    require.Equal(t, uint16(0), JavaState(rid("minecraft:air", nil)))
    require.Equal(t, uint16(137), JavaState(rid("minecraft:oak_log", map[string]any{"pillar_axis": "y"})))
}
func TestEveryJavaBlockHasExactStateCount(t *testing.T) // product of num_values == maxStateId-minStateId+1, for all 1,168 blocks
func TestUnmappedBlockIsStone(t *testing.T)            // returns stone's state and logs once per block, not twice
func TestMappedShareReported(t *testing.T)             // at least 98% of Dragonfly's runtime IDs have a mapping
func TestRegistriesMatchLoginPacket(t *testing.T)      // 28 registries; the first is minecraft:worldgen/biome
func TestPacketID(t *testing.T)                        // equals the value read directly from protocol.json in testdata
func TestJavaItemRoundTrip(t *testing.T)               // diamond_pickaxe and oak_log, both directions
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/tables/...`
Expected: FAIL, because the package is empty.

- [ ] **Step 3: Implement `tools/gentables` and the lookups**

Run: `go run ./tools/gentables -data <minecraft-data checkout outside the repo>`
Expected: it prints counts of blocks, items and registries, and the number of unmapped Bedrock blocks.

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `go test ./internal/tables/...`
Expected: PASS. If the mapped share is under 98%, stop and report instead of lowering the threshold.

- [ ] **Step 5: Commit**

`git add -A && git commit -m "feat: generate Java 26.1 and Bedrock mapping tables"`

## Task 3: Java wire format

**Files:**
- Create: `internal/javawire/varint.go`, `frame.go`, `compress.go`, `crypt.go`, `wire_test.go`, `NOTES.md`

**Interfaces:**
- Produces:
  - `javawire.ReadVarInt(io.ByteReader) (int32, error)`, `AppendVarInt([]byte, int32) []byte`
  - `javawire.NewConn(net.Conn) *Conn`
  - `(*Conn).ReadFrame() ([]byte, error)`, `(*Conn).WriteFrame([]byte) error`
  - `(*Conn).EnableCompression(threshold int)`, `(*Conn).EnableEncryption(secret []byte) error`
  - `javawire.ErrTooLarge`
- Limit: `MaxFrame = 2 MiB`, for both the compressed frame and the declared uncompressed size.
- Encryption is AES-128 CFB8, with the secret as both key and IV. Go has no CFB8 mode, so implement it on `cipher.Block`.

- [ ] **Step 1: Write the failing tests**

```go
func TestVarIntVectors(t *testing.T) // 0=00, 127=7f, 128=8001, 255=ff01, 25565=ddc701, 2097151=ffff7f, 2147483647=ffffffff07, -1=ffffffff0f
func TestVarIntTooLongFails(t *testing.T)     // six continuation bytes return an error
func TestFrameSplitAcrossReads(t *testing.T)  // one byte per read still gives the whole frame
func TestManyFramesInOneRead(t *testing.T)    // three frames in one read give three frames
func TestOversizeFrameRejected(t *testing.T)  // ErrTooLarge, nothing allocated beyond the limit
func TestZipBombRejected(t *testing.T)        // declared 2 MiB but inflating past it gives ErrTooLarge
func TestCompressionThreshold(t *testing.T)   // below threshold: data-length 0, stored raw
func TestCFB8Vector(t *testing.T)             // encrypt then decrypt across uneven chunk sizes returns the input
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/javawire/`
Expected: FAIL.

- [ ] **Step 3: Implement `javawire`**

Reuse one read buffer per connection. Don't allocate a new buffer per frame.

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/javawire/ -fuzz=FuzzReadFrame -fuzztime=30s` and then `go test ./internal/javawire/`
Expected: PASS, with no panics from the fuzz run.

- [ ] **Step 5: Commit**

`git add -A && git commit -m "feat: add Java wire format"`

## Task 4: Java status, login and configuration

**Files:**
- Create: `internal/javaproto/reader.go`, `writer.go`, `login.go`, `internal/javalogin/login.go`, `auth.go`, `login_test.go`, `NOTES.md`, `bots/java/package.json`, `bots/java/bot.js`

**Interfaces:**
- Consumes: `javawire.Conn`, `java261.PacketID`, `java261.Registries`, `java261.ProtocolVersion`.
- Produces:
  - `javalogin.Accept(c *javawire.Conn, opts Options) (*Identity, error)`
  - `Identity{Name string; UUID uuid.UUID; Textures string}`
  - `Options{AuthEnabled bool; SessionURL string; MOTD string; MaxPlayers int; OnlinePlayers func() int}`
  - `javalogin.ServerHash(serverID string, secret, publicKey []byte) string`
- `Accept` returns once the client has finished configuration and entered play. It sends the known pack `minecraft:core` at version `26.1`, one registry packet per `java261.Registries()` with IDs and no data, then finish configuration.
- `SessionURL` defaults to `https://sessionserver.mojang.com/session/minecraft/hasJoined`, with a 10-second timeout.
- Whole login limit: 30 seconds, then close.

- [ ] **Step 1: Write the failing tests**

```go
func TestServerHashVectors(t *testing.T) {
    require.Equal(t, "4ed1f46bbe04bc756bcb17c0c7ce3e4632f06a48", hashOfName("Notch"))
    require.Equal(t, "-7c9d5b0044c130109a5d7b5fb5c317c02b4e28c1", hashOfName("jeb_"))
    require.Equal(t, "88e16a1019277b15d58faf0541e11910eb756f6", hashOfName("simon"))
}
func TestStatusReportsVersion(t *testing.T)     // status JSON has protocol 775 and name "26.1"
func TestPingEchoes(t *testing.T)               // pong has the same payload
func TestWrongProtocolIsToldVersion(t *testing.T) // protocol 777 gets a disconnect containing "26.1"
func TestAuthRejectsWhenMojangSaysNo(t *testing.T) // fake session server returns 204: disconnect
func TestAuthServerDownGivesMessage(t *testing.T)  // fake server hangs: disconnect within 11 s, no crash
func TestTruncatedLoginDoesNotHang(t *testing.T)   // client closes mid-login: Accept returns an error
func TestNodeBotReachesPlay(t *testing.T)          // bots/java/bot.js, version 26.1, AuthEnabled=false, gets to the configuration end without error
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/javalogin/`
Expected: FAIL.

- [ ] **Step 3: Implement the login flow**

RSA 1024-bit key generated once at startup. With auth on, use encryption and the Mojang check, and take the name, UUID and textures from Mojang's answer. With auth off, use the offline UUID (MD5 of `OfflinePlayer:<name>`, version 3). Then set compression at threshold 256.

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/javalogin/`
Expected: PASS.

- [ ] **Step 5: Commit**

`git add -A && git commit -m "feat: add Java status, login and configuration"`

## Task 5: Java player joins and sees the world

**Files:**
- Create: `internal/bridge/conn.go`, `listener.go`, `join.go`, `chunks.go`, `bridge_test.go`, `NOTES.md`, `internal/javaproto/play.go`
- Modify: `cmd/overmind/main.go` (add the Java listener and the name-clash `Allower`)

**Interfaces:**
- Consumes: `javalogin.Accept`, `java261.JavaState`, `java261.JavaBiome`.
- Produces:
  - `bridge.NewListener(addr string, opts javalogin.Options, log *slog.Logger) (server.Listener, error)`
  - `bridge.Conn`, implementing every method of `session.Conn`.
  - In `Conn`: `translators map[uint32]func(packet.Packet) error` for Bedrock to Java, and `incoming chan packet.Packet` for Java to Bedrock, which `ReadPacket` drains.

**Behaviour decisions:**
- `IdentityData`: the Java UUID and name, with an empty XUID. Confirm that Dragonfly accepts an empty XUID. If it refuses, use a fixed format `java:<uuid>` and note it in `NOTES.md`.
- `ClientData`: a plain generated 64x64 skin in stage 1. Real Java skins shown to Bedrock players come later, and the plan's owner sign-off lists this as a known gap.
- `ClientCacheEnabled` is false. `ChunkRadius` is the configured view distance.
- `StartGameContext` sends Java Login (play), spawn position, game event "start waiting for chunks", and synchronize position, using values from `minecraft.GameData`.
- Chunks: read `session/` first to see whether Dragonfly sends whole `LevelChunk` packets or uses sub-chunk requests, and handle what it does, replying to `SubChunk` requests as the client would. Decode with Dragonfly's `chunk.NetworkDecode`, re-encode for Java (paletted sections, using `JavaState` per palette entry), then discard. Y range is -64 to 319 on both sides.
- Light: send full sky light (15) in stage 1, since real light belongs to stage 2.
- `NetworkChunkPublisherUpdate` becomes Set Center Chunk.
- Name clash: the `Allower` and the Java listener both check `srv.PlayerByName`, ignoring case. Refusal text is `A player called <name> is already online.`
- Packets with no translator are dropped and counted once per ID at debug level.

- [ ] **Step 1: Write the failing tests**

```go
func TestConnImplementsSessionConn(t *testing.T) { var _ session.Conn = (*Conn)(nil) }
func TestJavaBotJoinsAndGetsChunks(t *testing.T)  // Node bot sees at least (2*6+1)^2 columns, and a known column decodes to the flat world's blocks at the right Y
func TestJavaBotStandsOnGround(t *testing.T)      // after 5 s its spawn Y is above the top block, it isn't falling, and it's not kicked
func TestDragonflySeesJavaPlayer(t *testing.T)    // srv.PlayerByName("JavaBot") is ok
func TestNameClashRefused(t *testing.T)           // Bedrock "Same" online, Java "Same" is refused with the message, and vice versa
func TestChunkSectionEncoding(t *testing.T)       // a Go-encoded section, decoded with prismarine-chunk in bots/java, gives the same blocks
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/bridge/`
Expected: FAIL.

- [ ] **Step 3: Implement the connection, join and chunk translation**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/bridge/`
Expected: PASS.

- [ ] **Step 5: Commit**

`git add -A && git commit -m "feat: join Java players through the bridge and send chunks"`

## Task 6: Movement and seeing each other

**Files:**
- Create: `internal/bridge/entities.go`, `movement.go`, `entities_test.go`

**Interfaces:**
- Consumes: `bridge.Conn`.
- Produces: `entityMap` in `Conn`: `javaID(bedrockRuntimeID uint64) int32`, `forget(bedrockRuntimeID uint64)`.

**Behaviour decisions:**
- Java position and look packets are stored. Once per tick (50 ms), the bridge feeds `PlayerAuthInput` to `incoming`, with `Position.Y = feet + 1.62`, a rising `Tick`, and Yaw, HeadYaw and Pitch. Dragonfly expects this packet every tick. Yaw is the same convention on both sides, and the test proves it.
- Dragonfly's own teleports (`MovePlayer` for the self entity) become Java synchronize position. The client's confirm is handled in the bridge and isn't passed on.
- `AddPlayer` and `PlayerList` become player info update plus spawn entity. `MovePlayer` and `MoveActorAbsolute` become teleport entity plus head rotation. `RemoveActor` becomes remove entities, and `PlayerList` removal becomes player info remove. `Animate` becomes entity animation (arm swing).
- Java keep-alive is handled in the bridge, every 15 seconds, and a 30-second silence closes the connection. It feeds `Latency()`.
- A socket close or read error closes `Conn`, so `ReadPacket` returns an error and Dragonfly removes the player.

- [ ] **Step 1: Write the failing tests**

```go
func TestJavaWalksAndBedrockSees(t *testing.T)   // Java bot walks 10 blocks east; Bedrock bot sees its X change within 1 block
func TestBedrockWalksAndJavaSees(t *testing.T)   // the same in reverse, including yaw within 2 degrees
func TestTwoJavaPlayersSeeEachOther(t *testing.T)
func TestChunksFollowPlayer(t *testing.T)        // Java bot walks 64 blocks; new columns arrive and center chunk moves
func TestKilledClientDisappears(t *testing.T)    // kill Java bot mid-walk: the other player gets a removal within 35 s, PlayerByName is false
func TestDropMidJoinLeavesNothing(t *testing.T)  // 20 connections dropped mid-join: goroutine count returns to baseline within 5 s
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/bridge/ -run 'Walks|Players|Follow|Killed|Drop'`
Expected: FAIL.

- [ ] **Step 3: Implement the movement and entity translators**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/bridge/ -race`
Expected: PASS with no races.

- [ ] **Step 5: Commit**

`git add -A && git commit -m "feat: translate movement and players between editions"`

## Task 7: Breaking and placing blocks

**Files:**
- Create: `internal/bridge/blocks.go`, `blocks_test.go`

**Interfaces:**
- Consumes: `Conn`, `java261.JavaState`, `java261.JavaItem`.

**Behaviour decisions:**
- Read `handleActions` and `handleBlockActions` in Dragonfly's `handler_player_auth_input.go` first, then match what a real Bedrock client sends. Java dig start, cancel and finish become entries in the next tick's `PlayerAuthInput.BlockActions`, with the matching input flags. Java face numbers map directly, and the test proves it.
- Java use-item-on becomes `InventoryTransaction` with `UseItemOnBlock`, using the held slot's current item.
- `UpdateBlock` becomes Block Update, using `JavaState`. `UpdateSubChunkBlocks` becomes one Block Update per block.
- Every Java dig or place action gets its acknowledgement. Java clients expect the sequence number back, or blocks visibly snap back.
- `LevelEvent` for break particles and cracks becomes block break animation and world event, where simple. Missing ones are logged, not fatal.
- `AddItemActor` becomes spawn entity plus metadata, and `TakeItemActor` becomes collect item, so drops can be seen and picked up.

- [ ] **Step 1: Write the failing tests**

```go
func TestJavaBreaksBedrockSees(t *testing.T)    // Java bot digs a block; the server reports air and Bedrock bot gets the update
func TestBedrockBreaksJavaSees(t *testing.T)    // Java bot gets a Block Update to state 0
func TestJavaPlacesBedrockSees(t *testing.T)    // Java bot places a held block on each of six faces; positions are correct
func TestBedrockPlacesJavaSees(t *testing.T)    // Java state equals java261.JavaState for that block
func TestJavaOutOfReachDoesNotCrash(t *testing.T) // dig 200 blocks away: refused and block reverted, connection stays up
func TestDropIsSeenAndPickedUp(t *testing.T)    // break a block; Java bot sees the item; walks over; inventory slot fills
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/bridge/ -run 'Break|Place|Reach|Drop'`
Expected: FAIL.

- [ ] **Step 3: Implement the block translators**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/bridge/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

`git add -A && git commit -m "feat: translate block breaking, placing and drops"`

## Task 8: Chat and held items

**Files:**
- Create: `internal/bridge/chat.go`, `chat_test.go`

**Behaviour decisions:**
- Java chat becomes `packet.Text`. `packet.Text` becomes Java system chat, with text carried as a plain component. Set `enforcesSecureChat` to false in Login, and don't verify chat signatures in stage 1.
- Java commands have nowhere to go, so the client gets the same message Dragonfly gives for an unknown command.
- Java hotbar selection becomes `MobEquipment`. Bedrock `MobEquipment` for other players becomes Java entity equipment, using `JavaItem`.
- Long text: over 256 characters from a client is refused by the same check Bedrock players get.

- [ ] **Step 1: Write the failing tests**

```go
func TestChatBothWays(t *testing.T)            // includes non-ASCII: "héllo ✓" arrives unchanged
func TestFormattingCodesCannotBreakJSON(t *testing.T) // text with quotes and § arrives without a disconnect
func TestHeldItemSeenBothWays(t *testing.T)
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/bridge/ -run 'Chat|Formatting|Held'`
Expected: FAIL.

- [ ] **Step 3: Implement chat and held items**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/bridge/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

`git add -A && git commit -m "feat: translate chat and held items"`

## Task 9: Inventory

**Files:**
- Create: `internal/bridge/inventory.go`, `inventory_test.go`

**Behaviour decisions:**
- Java's window slot numbers: 36-44 hotbar, 9-35 main, 5-8 armour, 45 offhand. Bedrock's: hotbar 0-8, main 9-35. `InventoryContent` and `InventorySlot` become Java window items and set slot.
- Java window click becomes `ItemStackRequest` with Take, Place, Swap and Drop actions. Keep each Bedrock slot's `StackNetworkID`, updated from `InventoryContent`, `InventorySlot` and `ItemStackResponse`, because Dragonfly rejects a request with the wrong one.
- The carried item (Java's cursor) maps to Bedrock's cursor container.
- If Dragonfly rejects a request, resend the whole inventory so the Java client corrects itself.
- Stage 1 covers the player's own inventory: move, swap, split, drop and shift-click. Crafting grids and chests belong to stage 3.

- [ ] **Step 1: Write the failing tests**

```go
func TestInventorySentOnJoin(t *testing.T)
func TestMoveItem(t *testing.T)        // Java click moves a stack; the server's inventory agrees
func TestSplitStack(t *testing.T)      // right-click splits 64 into 32 and 32; the server's total stays 64
func TestSwapStacks(t *testing.T)
func TestDropOutsideWindow(t *testing.T) // Bedrock bot sees the dropped item
func TestShiftClick(t *testing.T)
func TestBadClickResyncs(t *testing.T) // click with a stale state: inventory resent, no duplicated items, no disconnect
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/bridge/ -run 'Inventory|Move|Split|Swap|Drop|Shift|BadClick'`
Expected: FAIL.

- [ ] **Step 3: Implement the inventory translator**

- [ ] **Step 4: Run them to confirm they pass**

Run: `go test ./internal/bridge/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

`git add -A && git commit -m "feat: translate the player inventory"`

## Task 10: Stage sign-off

**Files:**
- Modify: `tools/memtest/main.go`, `docs/memory-log.md`, `docs/HOW-IT-FITS.md`, all `NOTES.md`
- Create: `docs/stage-1-known-gaps.md`

- [ ] **Step 1: Run the Quarry.ts comparison**

Run `memtest` with the spec's phases: nobody, 1 bot for 1 minute, 10 for 2 minutes, 30 for 3 minutes, then 1 minute after all leave. Use Java bots, then a half-and-half mix. Use the flat world, and say so in the log, since Quarry.ts's run included terrain generation, so this isn't yet the final comparison.
Expected: a table in `docs/memory-log.md` beside the Quarry.ts figures, plus ticks per second. Any figure above Quarry.ts is reported to the owner, not hidden.

- [ ] **Step 2: Repeat the comparison on the VPS, with both servers**

Deploy to a new `/opt/overmind`, on a spare port, and run Quarry.ts there the same way. Leave existing services alone.
Expected: both sets of numbers logged.

- [ ] **Step 3: Write the docs**

`docs/HOW-IT-FITS.md` follows one Java packet and one Bedrock packet through every part. `docs/stage-1-known-gaps.md` lists at least: Java 26.1 rather than 26.3, plain skins for Java players, full-bright light, no crafting or chests, flat world, and every Bedrock packet the bridge drops.

- [ ] **Step 4: Run everything**

Run: `go vet ./... && go test ./... -race`
Expected: all PASS.

- [ ] **Step 5: Commit**

`git add -A && git commit -m "docs: stage 1 measurements, notes and known gaps"`

- [ ] **Step 6: Owner playtest**

The owner joins with a real Java 26.1 client and a real Bedrock client, with login on: walk, break, place, chat, move items, and see the other player.
Expected: the owner signs off. This also proves real Mojang and Xbox login, which the bots can't. Any problem found becomes a test and a fix before stage 2 is planned.
