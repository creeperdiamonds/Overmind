# Overmind

A custom Minecraft server that lets **Java Edition** and **Bedrock Edition** clients play together on the same world — built from scratch, without Mojang code.

---

## Overview

Overmind is split into two variants that solve cross-play from opposite directions:

| Variant | Engine | Best For |
|---|---|---|
| **OVERMIND** | Bedrock-primary | Bedrock Add-ons, custom 3D mobs, RPG/creative servers |
| **OVERJAVA** | Java-primary | Plugin-heavy servers (LuckPerms, WorldEdit, Factions) |

Both variants share the same core infrastructure: a single Chameleon port (25565) that auto-detects Java, Bedrock, or HTTP connections, a graph-based chunk system, procedural world generation, and chunk persistence.

---

## Architecture

### Shared ("Over-Link")
- **Chameleon Gateway** — single TCP port 25565 handles Java, Bedrock RakNet, and HTTP
- **Vertex Graph** — graph-based chunk system with instant neighbor-loading
- **383 Logic** — manages the height mismatch (Bedrock 383 vs Java 384 blocks)
- **World Generators** — Overworld, Nether, and End with multi-octave simplex noise
- **World Storage** — gzip-compressed per-chunk files, auto-saved every 60s

### OVERMIND (Bedrock-primary)
- **Molang-to-Matrix Engine** — evaluates Bedrock `.geo.json` animation keyframes and drives Java `Item Display` entities to render Bedrock models on Java clients
- **Fake Cooldown** — gate-controls attack rate server-side to match Bedrock spam-click combat
- **Bedrock Engine Bridge** — full RakNet/UDP handshake + Bedrock 1.21.40 login pipeline on port 19132

### OVERJAVA (Java-primary)
- **Registry Mapper** — maps Java block state IDs to Bedrock numeric block IDs in real-time
- **Shield/Crouch Adapter** — translates Bedrock crouch (sneak) into Java shield raise/lower

---

## Module Structure

```
Overmind/
├── API/          # Shared interfaces: VertexGraph, WorldGenerators, Molang, BlockStates
├── java/         # Java Edition server logic (TCP 25565 + UDP 19132)
├── bedrock/      # Future native Bedrock extensions
├── server/
│   └── java/overworld/   # Generated chunk files (c.X.Z.bin)
├── JARS/         # Compiled output JARs
└── building-plan.md      # Detailed phase-by-phase implementation plan
```

---

## Protocol Targets

| Protocol | Version |
|---|---|
| Java Edition | 1.21.4 (protocol 774) |
| Bedrock Edition | 1.21.40+ |

---

## Building

Requires **Java 21+**. Maven is bundled — no external install needed.

run.sh — six commands:

| Command | What it does |
|---|---|
│ ./run.sh test │ Runs `mvn test` compiles and executes all unit tests |
│ ./run.sh build │ Runs `mvn package -DskipTests` builds a fat JAR into JARS/ |
│ ./run.sh start │ Builds if JAR is missing, then runs `java -jar` to start the server |
│ ./run.sh clean │ Runs `mvn clean` — cleans build artifacts |
│ ./run.sh clean-build │ Cleans then builds (`mvn clean` + `mvn package -DskipTests`) |
│ ./run.sh clean-test │ Cleans then runs tests (`mvn clean` + `mvn test`) |

Custom world seed:
```bash
java -Dovermind.seed=123456789 -jar JARS/overmind.jar
```

---

## Open Source Foundations

Overmind draws on two open-source Minecraft server projects for protocol knowledge, architecture reference, and inspiration.

### Dragonfly
> A heavily asynchronous Minecraft: Bedrock Edition server software written in Go.

- **Repository:** https://github.com/df-mc/dragonfly
- **Authors:** Dragonfly Tech
- **License:** MIT License
- **Used for:** Bedrock RakNet protocol reference, Bedrock game packet structure (login pipeline, chunk encoding, player actions)

### Krypton
> Free and open-source Minecraft: Java Edition server software, written from scratch without Mojang code.

- **Repository:** https://github.com/KryptonMC/Krypton
- **Authors:** KryptonMC contributors
- **License:** Apache License 2.0
- **Used for:** Java Edition protocol reference (1.21.x handshake, configuration state, chunk packet format, play-state packet IDs)
