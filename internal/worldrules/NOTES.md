# worldrules

The shared world mechanics that Bedrock and Java play differently. A world can run them only one way: Bedrock's under the default profile, Java's under Overjava (`Config.Profile`). `internal/app` picks the rules with `ForProfile` and hands them to the Dragonfly fork's worlds. Player-owned rules (reach, combat) are in `internal/rules` instead, and follow each player's own edition.

| Mechanic | Bedrock | Java (Overjava) | Source |
|---|---|---|---|
| Random tick speed | 1 block per sub chunk per tick | 3 | minecraft.wiki "Tick" oldid 3868751 (defaults 1 and 3, Bedrock's "relative"); "Sugar Cane" oldid 3810018 (16 random ticks: 18 min on Java, 54 on Bedrock, so Bedrock's 1 is a third of Java's 3). `Config.RandomTickSpeed`, when set, overrides both. |
| Leaf decay | A leaf stays within 4 steps of a log, face to face through leaves. Only removing a log marks leaves for the check (the 9 x 9 x 9 cube around it), or removing a leaf (3 x 3 x 3); placing or removing any other block beside a leaf marks nothing. | 6 steps; any change beside a leaf marks it | "Leaves" oldid 3873085 for 4 and 6. Bedrock's marking was measured on a Bedrock Dedicated Server 1.26.60 with console commands (Task W report). Bedrock's own trees keep leaves up to 7 steps from a log, which only this marking keeps. `world.Mechanics.JavaLeafDecay`. |

Mechanics the editions share are in the Dragonfly fork directly, with no setting: Nether lava runs 7 blocks, one every 10 game ticks ("Lava", oldid 3870569).
