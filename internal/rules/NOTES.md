# rules

The rules that belong to a player and differ by edition (spec: "each edition plays by its own rules"). A player follows its own edition's; what the world shares follows Bedrock.

- `Player` is one edition's rules. `Bedrock` adds nothing to what Dragonfly does and is the `Default`. `Java` is used by the Java front end.
- Block reach: Java's `player.block_interaction_range`, 4.5 blocks and 5 in creative (minecraft.wiki, "Attribute"), measured from the eyes to the block's cube, plus a 1 block allowance of ours for latency. Dragonfly's own check (8, 14 in creative, to the block's centre) still applies to everyone.
- Entity reach: Java's `player.entity_interaction_range`, 3 blocks and 5 in creative (minecraft.wiki, "Attribute"), eyes to the target's box, plus the same 1 block allowance.
- Attacks (`Combat`): Java's weapon damage and attack speed per item (each item's minecraft.wiki page; wooden to netherite and copper; a hand or anything else is 1 at speed 4) and Java's attack cooldown (minecraft.wiki, "Damage": charge `(t + 0.5) / T` with `T = 20 / speed` and `t` whole ticks, damage × `0.2 + charge² × 0.8`, critical only when charged over 0.9). It works on Dragonfly's damage for the attack, so effects and enchantments are Dragonfly's (Bedrock's) amounts, scaled in proportion. Bedrock changes nothing: no cooldown, Dragonfly's damage. The Java front end applies them through the fork's `player.AttackRules`.
