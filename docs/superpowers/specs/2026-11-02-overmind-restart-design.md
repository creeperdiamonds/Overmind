# Overmind restart: design

Status: approved by the owner on 2026-11-02. Nothing has been built yet.

## Why

Overmind is a Minecraft server that Bedrock and Java players can join together. The old repo (`creeperdiamonds/overmind`, cloned at `PROJECTS\Overmind`) was left for months, and when the owner came back they could no longer tell what the code did or how it fit together. That confusion is the reason for restarting.

Two things therefore matter equally: it has to play like real Minecraft, and the owner has to be able to understand the code after time away.

## Goals

The first release is the translation layer, a world, and what it takes to beat the game. Its test: the owner starts a new world on the server, plays normally and beats the Ender Dragon, on either edition.

- The latest Bedrock and the latest Java clients join the same server and play in the same world, seeing each other and each other's changes.
- The world feels like a normal singleplayer world: similar terrain, biomes and caves, using noise generation modelled on Mojang's.
- The world and player data survive restarts.
- Reliability comes before everything else.
- The world is production-ready, and the server is lighter on memory than Quarry.ts.

## Production-ready world and memory targets

These are the builder's definitions of the owner's goals, open to correction:

- **Safe saves:** a crash or power cut loses at most the last few seconds of play and never corrupts the world. This is tested by killing the server during saves.
- **Backups:** the server can make regular world backups while running, and a backup is tested by restoring it.
- **Smooth generation:** generating new chunks never holds up the game. The server holds 20 ticks per second with 30 players exploring in different directions.
- **Long running:** a 72-hour bot test finishes with no crash and no steady memory growth.
- **Lighter than Quarry.ts:** the memory target is Quarry.ts (`StacikM/quarry-ts`, a small Minecraft 1.8.9 server in TypeScript). Both servers run on the same machine with the same player count, view distance and bot movement, and total process memory is compared. Overmind must use less at every stage, including once it has features Quarry.ts lacks, such as mobs, hunger and inventories. Paper is also measured, and will be far above that line.
- **Baseline, measured 2026-11-02:** Quarry.ts commit `6c3a61a`, one run on the owner's Windows PC, view distance 6, bots on the same machine walking outward in different directions on 1.8.9, whole-process memory.

  | Players | At end of phase | Peak | Ticks per second |
  |---|---|---|---|
  | Nobody | 50 MB | 50 MB | 20.0 |
  | 1 walking, 1 minute | 127 MB | 161 MB | 20.0 |
  | 10 walking, 2 minutes | 158 MB | 1,093 MB | 20.0 |
  | 30 walking, 3 minutes | 870 MB | 1,209 MB | 17.3, worst stall 4.3 seconds |
  | All left, 1 minute later | 893 MB | 893 MB | 20.0 |

- **Measured in megabytes:** Overmind must beat these figures in the same test, at the same view distance, with the latest clients. Memory is checked at each stage so that a stage can't quietly break the target. The test script is rewritten for the project, since this one was a throwaway.
- **Ideas worth taking from Quarry.ts (MIT, credit it if code is reused):** saving only the blocks players changed and rebuilding the rest from the seed, and streaming chunks a few per tick, nearest first.

## What the first release must include

- Mining, building, crafting, smelting, hunger, damage, death and respawn.
- Hostile mobs and food animals, including endermen and blazes, with the drops a run needs.
- The Nether with fortresses, strongholds with an End portal, the End and the Ender Dragon.

## Later releases

The long-term goal is an ordinary vanilla survival server. These complete it: enchanting, brewing, farming, breeding, redstone, villagers, and other structures with their loot.

## Priorities

- Bedrock is the primary edition. Java is secondary.
- Long term, Java should support all versions and Bedrock only the latest. The first release supports only the latest of each, but must not block adding older Java versions later without a rewrite.

## Non-goals for the first release

- In-game commands of any kind, including `/op` and `/gamemode`.
- Plugins, add-ons and admin tools, including ban and whitelist files.
- Older Java versions.
- Anti-cheat.
- Everything under "Later releases".

## Anti-goals (failure even if it runs)

- The code is confusing: the owner cannot tell what a part does or why it exists.
- Duplicated copies of the same code, as with the old `java`/`java-dev` and `bedrock`/`bedrock-dev` folders.
- Values guessed from memory where an authoritative source exists (the old code had estimated packet and block IDs that were wrong).
- Any Mojang code or Mojang files in the repo, or any need for them to run the server.

## Decided by the owner

- No Mojang code. Only the behaviour of noise generation and the resulting world are modelled on Mojang's.
- Build on Dragonfly (`df-mc/dragonfly`, MIT license, confirmed on GitHub 2026-11-02), changing it freely. Keep its copyright and license notice and credit it.
- The project is public.
- Reuse working parts of the old repo where that speeds things up.
- Latest versions first.
- Build the Bedrock/Java translation first, as our own code. No GeyserMC: the owner rejects it. (Their reason is still to come. If it names a specific failing, the translator must avoid it.)
- **No Mojang code at all:** the repo ships no Mojang code or files, and neither the server nor its setup downloads, runs or reads Mojang's server programs. There is no setup step for game data.
- **Game data:** block and packet tables come from open projects such as PrismarineJS, and from what Dragonfly already includes. Recipes, loot, world generation settings and structures not covered there are written by us from public documentation, so structures are our own builds made to look like vanilla ones.
- **Studying Mojang's game is allowed (owner, 2026-11-03):** we may decompile and read it to learn how it works, then write our own code. No Mojang code or files are copied into the repo, and it stays outside the project folder.
- **Bedrock rules for everyone:** where the editions differ, including combat, redstone and recipes, the server follows Bedrock. Java players will notice differences, and that is accepted.
- **First release scope:** the translation layer, a world, and everything needed to beat the Ender Dragon.
- **Login:** Xbox login for Bedrock and Mojang login for Java are both required.
- **Cheating:** no anti-cheat in the first release. The owner expects to add one later, such as GrimAC.
- **Identity:** one person on both editions counts as two separate players.
- **Size:** 20-30 players at once on the owner's 12GB VPS first, growing from there.
- **Same seed as Bedrock (owner, 2026-11-03):** biomes, terrain shape and noise caves reproduce Mojang's Bedrock world for the same seed, tested against real Bedrock worlds, following Mojang's algorithms in our own Go code. Tunnels, canyons, ores, trees and plants stay statistically matched, not block-exact, because their placement is hidden inside Bedrock's program. This raises the licensing question for the review before public release.
- **Weight:** the world must be production-ready, and the server must be lighter than Quarry.ts, using megabytes rather than gigabytes of memory.

## Left to the builder (Claude's calls, open to correction)

- **One server, not two variants.** The old Overmind/Overjava split is dropped: one program, one world, with Bedrock as the native side. The owner made this choice conditional on performance and handed it over; one server avoids translating the world twice and removes the duplication.
- **Go,** because Dragonfly is written in Go. Old Java code is reference material, to be ported rather than run.
- **Java support is a translation layer** on top of the Bedrock-native game: it speaks the Java protocol and maps blocks, items, entities and actions between the editions. Protocol and block data come from generated tables, not hand-written constants, so another Java version means another table.
- **The real game is only a test reference.** Real Bedrock and Java clients join the server for testing, and a world made in the real game is the comparison for terrain. Nothing from them goes into the repo.
- **Dragonfly becomes our own fork.** We pull in its Bedrock update code when new versions come out, rather than trying to stay fully mergeable. A new client version can lock players out until the server is updated.
- **Testing:** scripted bots on both editions test each stage, and the test setup is built early. The owner plays each stage at its end, and that playthrough is what signs it off.
- **First release in five stages, each joinable and playable, and each working on both editions:**
  1. Translation layer first (owner's decision): Bedrock clients (RakNet over UDP) and Java clients (TCP) join the same Dragonfly-based server and see each other, through our own translator. Includes login, movement, breaking and placing blocks, inventory and chat.
  2. Vanilla-like Overworld generation: terrain, biomes, caves, ores, water, trees and plants.
  3. Core survival: crafting, smelting, hunger, damage, death and respawn, and saved player data.
  4. Mobs: hostile mobs and food animals in the Overworld.
  5. The dragon run: the Nether with fortresses, blazes, endermen, strongholds, the End portal, the End and the Ender Dragon. It ends when the owner beats the dragon.
- **Only what a run needs:** structures other than fortresses and strongholds wait for later releases, and so do the items that depend on later features. Eating food from animals covers hunger, so farming can wait.
- **Player data is stored by account ID per edition,** which follows from counting them as two players.
- **Readable structure:** small folders with plain names, each with a short note saying what it does. A top-level document explains how the parts connect.
- **Location:** new folder `PROJECTS\Overmind-restart`. The old repo and its uncommitted fixes stay untouched until the owner decides.

## Constraints and risks

- Go is not currently installed on the owner's Windows PC.
- The owner has a MacBook test machine and a VPS available for testing.
- The scale is very large, even with the first release limited to a dragon run. Mob behaviour and the dragon fight are the hardest parts, and each stage must be playable by itself so progress is visible long before the dragon.
- Open-project tables such as PrismarineJS, and Dragonfly's own data, were originally extracted from Mojang's games. They are data rather than code, and clients need those exact IDs to connect, but using them is not a legal guarantee. To be reviewed before any public release.
- Code written after studying Mojang's is harder to defend as independent work, and the numbers learned from it may also be questioned. Include this in the review before any public release.
- With no bans or whitelist, griefers can't be stopped, so the first release shouldn't be advertised publicly until a later release adds them.
- Memory is the target most likely to fight the feature list. Quarry.ts is a much smaller game: 1.8.9 worlds are 256 blocks tall against 384 today, and it has no mobs, inventories, hunger, login or second edition. Mobs, loaded chunks and per-player Java translation all cost memory, and Dragonfly's actual use under this load has not been measured yet. The 50 MB idle figure is the hardest to beat, and the busy peak looks the most beatable, but neither is proven. Beating Quarry.ts may not be possible with the full feature list. Measuring in stage 1 shows this early, and the owner decides what gives if it can't be done.
- GrimAC is a Java plugin for Java-based servers, so it can't simply be added to this Go server. A later anti-cheat would need porting, a rewrite or a different tool.
- Bedrock client updates can break joining without warning, so someone has to update the server soon after each release.
