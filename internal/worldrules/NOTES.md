# worldrules

The shared world mechanics that Bedrock and Java play differently. A world can run them only one way: Bedrock's under the default profile, Java's under Overjava (`Config.Profile`). `internal/app` picks the rules with `ForProfile` and hands them to the Dragonfly fork's worlds. Player-owned rules (reach, combat) are in `internal/rules` instead, and follow each player's own edition.

| Mechanic | Bedrock | Java (Overjava) | Source |
|---|---|---|---|
| Random tick speed | 1 block per sub chunk per tick | 3 | minecraft.wiki "Tick" oldid 3868751 (defaults 1 and 3, Bedrock's "relative"); "Sugar Cane" oldid 3810018 (16 random ticks: 18 min on Java, 54 on Bedrock, so Bedrock's 1 is a third of Java's 3). `Config.RandomTickSpeed`, when set, overrides both. |
